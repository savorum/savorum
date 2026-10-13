package webrecipe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
)

// page wraps a fixture as though it had been fetched from the given address.
func page(body, address string) *Page {
	contentType := "text/html"
	if strings.HasPrefix(strings.TrimSpace(body), "{") || strings.HasPrefix(strings.TrimSpace(body), "[") {
		contentType = "application/ld+json"
	}

	return &Page{URL: address, ContentType: contentType, Body: []byte(body)}
}

func htmlWith(scripts ...string) string {
	var builder strings.Builder
	builder.WriteString(`<!doctype html><html><head><title>Fixture</title>`)

	for _, script := range scripts {
		builder.WriteString(`<script type="application/ld+json">` + script + `</script>`)
	}

	builder.WriteString(`</head><body><h1>Fixture</h1></body></html>`)

	return builder.String()
}

func mustExtract(t *testing.T, body, address string) *Recipe {
	t.Helper()

	result, err := Extract(page(body, address))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	return result
}

func extractFailure(t *testing.T, body, address string) FailureCode {
	t.Helper()

	result, err := Extract(page(body, address))
	if err == nil {
		t.Fatalf("expected a failure, got %#v", result)
	}

	return CodeOf(err)
}

func allSteps(sections []InstructionSection) []Step {
	var result []Step
	for _, section := range sections {
		result = append(result, section.Steps...)
		result = append(result, allSteps(section.Sections)...)
	}

	return result
}

func allIngredients(groups []IngredientGroup) []string {
	var result []string

	for _, group := range groups {
		for _, ingredient := range group.Ingredients {
			result = append(result, ingredient.Text)
		}
	}

	return result
}

const simpleRecipe = `{"@context":"https://schema.org","@type":"Recipe","name":"Bolo de cenoura",
	"inLanguage":"pt-BR","recipeIngredient":["1,5 l leite","2 ovos"],
	"recipeInstructions":["Misture.","Asse."],"recipeYield":"6 porções",
	"image":"https://images.example/cake.jpg"}`

func TestSimpleJSONLDRecipeImports(t *testing.T) {
	result := mustExtract(t, simpleRecipe, "https://example.test/bolo")
	if result.Name != "Bolo de cenoura" || result.Language != "pt-BR" {
		t.Fatalf("recipe = %#v", result)
	}
	// The source line survives whatever is or is not understood about it. A
	// comma decimal is not Savorum's to correct on the way in.
	if got := allIngredients(result.IngredientGroups); len(got) != 2 || got[0] != "1,5 l leite" {
		t.Fatalf("ingredients = %#v", got)
	}

	if got := allSteps(result.Sections); len(got) != 2 || got[1].Text != "Asse." {
		t.Fatalf("steps = %#v", got)
	}

	if result.Yields[0].Text != "6 porções" || len(result.Images) != 1 {
		t.Fatalf("yield = %#v images = %#v", result.Yields[0], result.Images)
	}
}

func TestRecipeInsideGraphImports(t *testing.T) {
	body := htmlWith(`{"@context":"https://schema.org","@graph":[
		{"@type":"WebSite","name":"A blog"},
		{"@type":"Recipe","name":"Graph cake","recipeIngredient":["flour"],"recipeInstructions":["Bake."]}]}`)
	if got := mustExtract(t, body, "https://example.test/x").Name; got != "Graph cake" {
		t.Fatalf("name = %q", got)
	}
}

func TestGarbageInsideGraphIsIgnored(t *testing.T) {
	// A real recipe site has shipped a literal false in its @graph. One bad
	// member of a list is not a reason to abandon the list.
	body := htmlWith(`{"@context":"https://schema.org","@graph":[false,null,42,"nonsense",
		{"@type":"Recipe","name":"Survivor","recipeIngredient":["flour"],"recipeInstructions":["Bake."]}]}`)
	if got := mustExtract(t, body, "https://example.test/x").Name; got != "Survivor" {
		t.Fatalf("name = %q", got)
	}
}

func TestBrokenBlockDoesNotPoisonAGoodOne(t *testing.T) {
	body := htmlWith(
		`{"@type":"Recipe","name":"Broken", this is not JSON at all`,
		`{"@type":"Recipe","name":"Good","recipeIngredient":["flour"],"recipeInstructions":["Bake."]}`)

	result := mustExtract(t, body, "https://example.test/x")
	if result.Name != "Good" {
		t.Fatalf("name = %q", result.Name)
	}

	if !hasWarning(result, "structured-data-unreadable") {
		t.Fatalf("expected a warning about the unreadable block, got %#v", result.Warnings)
	}
}

func TestMalformedJSONRecoveryKeepsOrdinaryConcatenatedValues(t *testing.T) {
	body := htmlWith(`<!-- {"@type":"Thing","name":"aside"}
		{"@type":"Recipe","name":"Recovered","recipeIngredient":["flour"],"recipeInstructions":["Bake."]} -->`)
	if got := mustExtract(t, body, "https://example.test/x").Name; got != "Recovered" {
		t.Fatalf("name = %q", got)
	}
}

func TestUnmatchedJSONOpenersUseOneScannerPass(t *testing.T) {
	input := strings.Repeat("{", 100) + strings.Repeat("x", 32<<10)
	budget := &workBudget{ctx: context.Background(), remaining: len(input)}

	spans, err := balancedJSONSpans(context.Background(), budget, input)
	if err != nil || len(spans) != 0 {
		t.Fatalf("spans=%d err=%v", len(spans), err)
	}

	if budget.remaining != 0 {
		t.Fatalf("scanner charged %d of %d bytes", len(input)-budget.remaining, len(input))
	}
}

func TestParserLimitsAndCancellationAreControlled(t *testing.T) {
	tooDeepJSON := strings.Repeat("[", maximumJSONNesting+1) + strings.Repeat("]", maximumJSONNesting+1)
	if got := extractFailure(t, tooDeepJSON, ""); got != ExtractionLimited {
		t.Fatalf("JSON depth code=%q", got)
	}

	tooDeepDOM := `<div itemscope itemtype="https://schema.org/Recipe">` +
		strings.Repeat(`<div itemprop="description">`, maximumDOMNesting+1) + "text" +
		strings.Repeat(`</div>`, maximumDOMNesting+2)
	if got := extractFailure(t, tooDeepDOM, ""); got != ExtractionLimited {
		t.Fatalf("DOM depth code=%q", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := ExtractContext(ctx, page(simpleRecipe, "")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled extraction error=%v", err)
	}
}

func TestConcurrentExtractionAdmissionHonorsCancellation(t *testing.T) {
	original := extractionSlots

	extractionSlots = make(chan struct{}, maximumConcurrentExtractions)
	defer func() { extractionSlots = original }()

	for range maximumConcurrentExtractions {
		extractionSlots <- struct{}{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := ExtractContext(ctx, page(simpleRecipe, "")); !errors.Is(err, context.Canceled) {
		t.Fatalf("admission cancellation error=%v", err)
	}
}

func BenchmarkMalformedJSONUnmatchedOpeners(b *testing.B) {
	input := strings.Repeat("{", 100) + strings.Repeat("x", 256<<10)

	b.ReportAllocs()

	for b.Loop() {
		budget := &workBudget{ctx: context.Background(), remaining: len(input)}
		_, _ = balancedJSONSpans(context.Background(), budget, input)
	}
}

func BenchmarkNestedMicrodataText(b *testing.B) {
	body := `<div itemscope itemtype="https://schema.org/Recipe"><meta itemprop="name" content="Nested">` +
		strings.Repeat(`<div itemprop="description">`, 120) +
		`<span itemprop="recipeIngredient">flour</span><span itemprop="recipeInstructions">Bake.</span>` +
		strings.Repeat(`</div>`, 120) + `</div>`
	loaded := page(body, "")

	b.ReportAllocs()

	for b.Loop() {
		_, _ = Extract(loaded)
	}
}

func TestRecipeThroughMainEntityImports(t *testing.T) {
	body := htmlWith(`{"@context":"https://schema.org","@type":"WebPage","name":"A page",
		"mainEntity":{"@type":"Recipe","name":"Main entity cake","recipeIngredient":["flour"],
		"recipeInstructions":["Bake."]}}`)
	if got := mustExtract(t, body, "https://example.test/x").Name; got != "Main entity cake" {
		t.Fatalf("name = %q", got)
	}
}

func TestMultipleRecipesSelectRatherThanMerge(t *testing.T) {
	body := htmlWith(`{"@context":"https://schema.org","@graph":[
		{"@type":"Recipe","name":"Related recipe","recipeIngredient":["sugar","cream"],
		 "recipeInstructions":["Whip."],"url":"https://example.test/other"},
		{"@type":"WebPage","@id":"https://example.test/wanted",
		 "mainEntity":{"@type":"Recipe","name":"The one asked for","recipeIngredient":["flour"],
		  "recipeInstructions":["Bake."],"url":"https://example.test/wanted"}}]}`)

	result := mustExtract(t, body, "https://example.test/wanted")
	if result.Name != "The one asked for" {
		t.Fatalf("name = %q", result.Name)
	}
	// Never merged: the other recipe's ingredients must not have come along.
	for _, text := range allIngredients(result.IngredientGroups) {
		if text == "sugar" || text == "cream" {
			t.Fatalf("ingredients merged across recipes: %#v", allIngredients(result.IngredientGroups))
		}
	}

	if !hasWarning(result, "multiple-recipes") {
		t.Fatalf("expected the reader to be told a choice was made: %#v", result.Warnings)
	}
}

func TestIngredientsAsPropertyValue(t *testing.T) {
	body := `{"@type":"Recipe","name":"Structured","recipeInstructions":["Mix."],
		"recipeIngredient":[{"@type":"PropertyValue","value":1.5,"unitText":"cup","name":"flour"},
		"2 eggs"]}`
	result := mustExtract(t, body, "https://example.test/x")

	first := result.IngredientGroups[0].Ingredients[0]
	if first.Amount != "1.5" || first.Unit != "cup" || first.Name != "flour" {
		t.Fatalf("structured parts lost: %#v", first)
	}
	// A line is reconstructed so nothing downstream has to special-case the
	// structured form, and the parts stay beside it rather than instead of it.
	if first.Text != "1.5 cup flour" {
		t.Fatalf("text = %q", first.Text)
	}

	if result.IngredientGroups[0].Ingredients[1].Text != "2 eggs" {
		t.Fatalf("plain string ingredient lost: %#v", result.IngredientGroups[0].Ingredients)
	}
}

func TestIngredientsAsItemList(t *testing.T) {
	body := `{"@type":"Recipe","name":"Listed","recipeInstructions":["Mix."],
		"recipeIngredient":{"@type":"ItemList","itemListElement":["flour","salt"]}}`
	if got := allIngredients(mustExtract(t, body, "").IngredientGroups); len(got) != 2 || got[1] != "salt" {
		t.Fatalf("ingredients = %#v", got)
	}
}

func TestIngredientHeadingsBecomeGroups(t *testing.T) {
	// Group headings emitted inside recipeIngredient are read as headings.
	body := `{"@type":"Recipe","name":"Layer cake","recipeInstructions":["Bake."],
		"recipeIngredient":["For the cake","2 cups flour","1 cup sugar","For the frosting","200 g butter"]}`

	result := mustExtract(t, body, "")
	if len(result.IngredientGroups) != 2 {
		t.Fatalf("groups = %#v", result.IngredientGroups)
	}

	if result.IngredientGroups[0].Title != "For the cake" || len(result.IngredientGroups[0].Ingredients) != 2 {
		t.Fatalf("first group = %#v", result.IngredientGroups[0])
	}

	if result.IngredientGroups[1].Title != "For the frosting" || len(result.IngredientGroups[1].Ingredients) != 1 {
		t.Fatalf("second group = %#v", result.IngredientGroups[1])
	}

	for _, text := range allIngredients(result.IngredientGroups) {
		if strings.HasPrefix(text, "For the") {
			t.Fatalf("a heading was imported as food: %q", text)
		}
	}
}

func TestIngredientHeadingWithNothingUnderItStaysAnIngredient(t *testing.T) {
	body := `{"@type":"Recipe","name":"Odd","recipeInstructions":["Bake."],
		"recipeIngredient":["2 cups flour","Optional:"]}`
	if got := allIngredients(mustExtract(t, body, "").IngredientGroups); len(got) != 2 {
		t.Fatalf("a trailing heading is a line of the recipe, not an empty group: %#v", got)
	}
}

func TestInstructionsAsPlainString(t *testing.T) {
	body := `{"@type":"Recipe","name":"Prose","recipeIngredient":["flour"],
		"recipeInstructions":"Mix the batter.\nBake for 20 minutes.\nCool."}`
	if got := allSteps(mustExtract(t, body, "").Sections); len(got) != 3 || got[2].Text != "Cool." {
		t.Fatalf("steps = %#v", got)
	}
}

func TestInstructionsAsOneUnbrokenStringStayOneStep(t *testing.T) {
	// A sentence is not a step. Splitting on full stops turns "Heat to 350 F."
	// into two, so only breaks the page itself made are honoured.
	body := `{"@type":"Recipe","name":"Blob","recipeIngredient":["flour"],
		"recipeInstructions":"Heat the oven to 350 F. Mix and bake."}`

	got := allSteps(mustExtract(t, body, "").Sections)
	if len(got) != 1 || got[0].Text != "Heat the oven to 350 F. Mix and bake." {
		t.Fatalf("steps = %#v", got)
	}
}

func TestInstructionsAsNumberedStringSplit(t *testing.T) {
	body := `{"@type":"Recipe","name":"Numbered","recipeIngredient":["flour"],
		"recipeInstructions":"1. Mix the batter. 2. Bake it. 3. Cool it."}`
	if got := allSteps(mustExtract(t, body, "").Sections); len(got) != 3 {
		t.Fatalf("steps = %#v", got)
	}
}

func TestSingleHowToStepObject(t *testing.T) {
	body := `{"@type":"Recipe","name":"One step","recipeIngredient":["flour"],
		"recipeInstructions":{"@type":"HowToStep","text":"Do the thing."}}`
	if got := allSteps(mustExtract(t, body, "").Sections); len(got) != 1 || got[0].Text != "Do the thing." {
		t.Fatalf("steps = %#v", got)
	}
}

func TestHowToStepWithNullTextDoesNotCrash(t *testing.T) {
	body := `{"@type":"Recipe","name":"Holes","recipeIngredient":["flour"],
		"recipeInstructions":[{"@type":"HowToStep","text":null},
		{"@type":"HowToStep"},
		{"@type":"HowToStep","text":"The only real step."}]}`
	result := mustExtract(t, body, "")

	got := allSteps(result.Sections)
	if len(got) != 1 || got[0].Text != "The only real step." {
		t.Fatalf("steps = %#v", got)
	}

	if !hasWarning(result, "empty-step") {
		t.Fatalf("expected the empty steps to be reported: %#v", result.Warnings)
	}
}

func TestHowToStepItemListElementRetainsDirectionsAndTips(t *testing.T) {
	body := `{"@type":"Recipe","name":"Detailed","recipeIngredient":["flour"],
		"recipeInstructions":[{"@type":"HowToStep","name":"Shape",
		"itemListElement":[{"@type":"HowToDirection","text":"Roll it thin."},
		{"@type":"HowToTip","text":"Chill it first if it tears."}]}]}`

	steps := allSteps(mustExtract(t, body, "").Sections)
	if len(steps) != 1 {
		t.Fatalf("steps = %#v", steps)
	}

	if len(steps[0].Directions) != 1 || steps[0].Directions[0] != "Roll it thin." {
		t.Fatalf("directions = %#v", steps[0].Directions)
	}

	if len(steps[0].Tips) != 1 || steps[0].Tips[0] != "Chill it first if it tears." {
		t.Fatalf("tips = %#v", steps[0].Tips)
	}

	if steps[0].Name != "Shape" {
		t.Fatalf("a step name is a heading, not another paragraph: %#v", steps[0])
	}
}

func TestNestedHowToSectionsPreserveHierarchy(t *testing.T) {
	body := `{"@type":"Recipe","name":"Nested","recipeIngredient":["flour"],
		"recipeInstructions":[{"@type":"HowToSection","name":"Cake",
		"itemListElement":[{"@type":"HowToStep","text":"Bake the cake."},
		{"@type":"HowToSection","name":"Filling",
		 "itemListElement":[{"@type":"HowToStep","text":"Whip the cream."}]}]}]}`

	sections := mustExtract(t, body, "").Sections
	if len(sections) != 1 || sections[0].Title != "Cake" {
		t.Fatalf("sections = %#v", sections)
	}

	if len(sections[0].Sections) != 1 || sections[0].Sections[0].Title != "Filling" {
		t.Fatalf("nesting lost: %#v", sections[0])
	}

	if len(sections[0].Sections[0].Steps) != 1 {
		t.Fatalf("nested steps = %#v", sections[0].Sections[0].Steps)
	}
}

func TestHowToStepSupplyIsPreserved(t *testing.T) {
	// The association a page states is the thing a generic importer most often
	// throws away and then tries to guess back out of the prose.
	body := `{"@type":"Recipe","name":"Biryani","recipeIngredient":["3.5 tbsp Ghee","2 pcs Cloves"],
		"recipeInstructions":[{"@type":"HowToSection","name":"Gravy Preparation",
		"itemListElement":[{"@type":"HowToStep","text":"Heat the ghee and add the cloves.",
		"supply":["3.5 tbsp Ghee","2 pcs Cloves"]}]}]}`

	steps := allSteps(mustExtract(t, body, "").Sections)
	if len(steps) != 1 {
		t.Fatalf("steps = %#v", steps)
	}

	if len(steps[0].Supply) != 2 || steps[0].Supply[0] != "3.5 tbsp Ghee" {
		t.Fatalf("supply = %#v", steps[0].Supply)
	}
}

func TestImageInEveryShape(t *testing.T) {
	for name, fixture := range map[string]string{
		"string": `"https://images.example/a.jpg"`,
		"object": `{"@type":"ImageObject","url":"https://images.example/a.jpg"}`,
		"array":  `["https://images.example/a.jpg","https://images.example/b.jpg"]`,
		"nested": `[{"@type":"ImageObject","contentUrl":"https://images.example/a.jpg"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `{"@type":"Recipe","name":"Pictured","recipeIngredient":["flour"],
				"recipeInstructions":["Bake."],"image":` + fixture + `}`

			images := mustExtract(t, body, "").Images
			if len(images) == 0 || images[0] != "https://images.example/a.jpg" {
				t.Fatalf("images = %#v", images)
			}
		})
	}
}

func TestYieldInEveryShape(t *testing.T) {
	for name, fixture := range map[string]struct{ json, want string }{
		"string":            {`"6 servings"`, "6 servings"},
		"number":            {`12`, "12"},
		"list":              {`["12","12 rolls"]`, "12 rolls"},
		"quantitativeValue": {`{"@type":"QuantitativeValue","value":6,"unitText":"servings"}`, "6 servings"},
	} {
		t.Run(name, func(t *testing.T) {
			body := `{"@type":"Recipe","name":"Yielded","recipeIngredient":["flour"],
				"recipeInstructions":["Bake."],"recipeYield":` + fixture.json + `}`
			if got := mustExtract(t, body, "").Yields[0].Text; got != fixture.want {
				t.Fatalf("yield = %q, want %q", got, fixture.want)
			}
		})
	}
}

func TestRecipeWithOnlyNameAndImageIsIncompleteNotInvalid(t *testing.T) {
	// Name and image alone are valid markup with nothing to import.
	body := htmlWith(`{"@type":"Recipe","name":"Chocolate Cake","image":"cake.jpg"}`)
	if got := extractFailure(t, body, "https://example.test/x"); got != RecipeIncomplete {
		t.Fatalf("code = %q, want %q", got, RecipeIncomplete)
	}
}

func TestPageWithoutStructuredDataSaysSo(t *testing.T) {
	body := `<!doctype html><html><body><h1>A blog post</h1><p>No recipe here.</p></body></html>`
	if got := extractFailure(t, body, "https://example.test/x"); got != NoRecipeStructuredData {
		t.Fatalf("code = %q", got)
	}
}

func TestEveryBlockUnreadableIsMalformedRatherThanAbsent(t *testing.T) {
	body := htmlWith(`{"@type":"Recipe", "name": broken`, `also not json`)
	if got := extractFailure(t, body, "https://example.test/x"); got != MalformedStructuredData {
		t.Fatalf("code = %q", got)
	}
}

func TestMicrodataIsReadWhenThereIsNoJSONLD(t *testing.T) {
	body := `<!doctype html><html><body><div itemscope itemtype="https://schema.org/Recipe">
		<h1 itemprop="name">Microdata loaf</h1>
		<li itemprop="recipeIngredient">500 g flour</li>
		<li itemprop="recipeIngredient">10 g salt</li>
		<div itemprop="recipeInstructions">Mix and bake.</div>
		<meta itemprop="recipeYield" content="1 loaf"></div></body></html>`

	result := mustExtract(t, body, "https://example.test/x")
	if result.Name != "Microdata loaf" || len(allIngredients(result.IngredientGroups)) != 2 {
		t.Fatalf("recipe = %#v", result)
	}

	if result.Yields[0].Text != "1 loaf" {
		t.Fatalf("yield = %#v", result.Yields[0])
	}
}

func TestJSONLDBeatsMicrodataOnTheSamePage(t *testing.T) {
	body := `<!doctype html><html><body><div itemscope itemtype="https://schema.org/Recipe">
		<span itemprop="name">Microdata title</span>
		<span itemprop="recipeIngredient">flour</span>
		<span itemprop="recipeInstructions">Mix.</span></div>
		<script type="application/ld+json">{"@type":"Recipe","name":"JSON-LD title",
		"recipeIngredient":["flour"],"recipeInstructions":["Mix."]}</script></body></html>`
	if got := mustExtract(t, body, "https://example.test/x").Name; got != "JSON-LD title" {
		t.Fatalf("name = %q", got)
	}
}

func hasWarning(result *Recipe, code string) bool {
	for _, warning := range result.Warnings {
		if warning.Code == code {
			return true
		}
	}

	return false
}

// allowLoopback relaxes only the loopback rule, so a test can reach an
// httptest server while every other destination stays refused. Private space
// in particular remains blocked, which is what the redirect test relies on.
func allowLoopback(t *testing.T) {
	t.Helper()

	previous := blockedDestination
	blockedDestination = func(ip net.IP) bool {
		if ip.IsLoopback() {
			return false
		}

		return blockedIP(ip)
	}

	t.Cleanup(func() { blockedDestination = previous })
}

func TestFetchRejectsLoopbackByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("should never be read"))
	}))
	defer server.Close()

	_, err := Fetch(context.Background(), server.URL)
	if CodeOf(err) != FetchRejected {
		t.Fatalf("err = %v, code = %q", err, CodeOf(err))
	}
}

func TestFetchRejectsNonHTTPSchemes(t *testing.T) {
	for _, address := range []string{"file:///etc/passwd", "ftp://example.test/recipe", "not a url at all", "https://user:secret@example.test/recipe"} {
		if got := CodeOf(mustFail(t, address)); got != FetchRejected {
			t.Fatalf("%q gave code %q", address, got)
		}
	}
}

func TestFetchErrorsNeverQuoteTheUntrustedAddress(t *testing.T) {
	const secret = "error-log-secret"

	address := "https://example.test/%zz?token=" + secret

	err := mustFail(t, address)
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), address) {
		t.Fatalf("fetch error reproduced the untrusted URL: %v", err)
	}
}

func mustFail(t *testing.T, address string) error {
	t.Helper()

	_, err := Fetch(context.Background(), address)
	if err == nil {
		t.Fatalf("expected %q to be refused", address)
	}

	return err
}

func TestRedirectIntoPrivateSpaceIsRefused(t *testing.T) {
	allowLoopback(t)

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		// The classic shape: a reachable host handing back an internal one.
		http.Redirect(response, &http.Request{}, "http://10.0.0.1/metadata", http.StatusFound)
	}))
	defer server.Close()

	_, err := Fetch(context.Background(), server.URL)
	if CodeOf(err) != FetchRejected {
		t.Fatalf("err = %v, code = %q", err, CodeOf(err))
	}
}

func TestBlockedDestinationsCoverTheRangesThatMatter(t *testing.T) {
	for _, address := range []string{
		"127.0.0.1", "::1", "10.1.2.3", "192.168.1.1", "172.16.0.1",
		"169.254.169.254", // cloud metadata
		"100.64.0.1",      // carrier-grade NAT
		"0.0.0.0", "255.255.255.255", "::ffff:127.0.0.1", "fd00::1",
		"192.88.99.1", "2001:db8::1", "2002:0a00:1::1", "3fff::1", "5f00::1",
	} {
		if !blockedIP(net.ParseIP(address)) {
			t.Errorf("%s should be refused", address)
		}
	}

	for _, address := range []string{"93.184.216.34", "8.8.8.8", "2606:2800:220:1:248:1893:25c8:1946"} {
		if blockedIP(net.ParseIP(address)) {
			t.Errorf("%s should be allowed", address)
		}
	}
}

func TestRedirectPolicyBoundsHopsAndDropsCredentials(t *testing.T) {
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.test/next", nil)
	request.Header.Set("Authorization", "Bearer savorum-session")
	request.Header.Set("Proxy-Authorization", "Basic proxy-secret")
	request.Header.Set("Cookie", "session=savorum-session")
	request.Header.Set("Referer", "https://origin.test/recipe?token=redirect-secret#oauth")

	via := make([]*http.Request, MaximumRedirects)
	if err := SafeRedirectPolicy(request, via); err != nil {
		t.Fatalf("valid redirect: %v", err)
	}

	for _, name := range []string{"Authorization", "Proxy-Authorization", "Cookie", "Referer"} {
		if value := request.Header.Get(name); value != "" {
			t.Errorf("%s was forwarded: %q", name, value)
		}
	}

	overLimit := make([]*http.Request, MaximumRedirects+1)
	if err := SafeRedirectPolicy(request, overLimit); CodeOf(err) != FetchRejected {
		t.Fatalf("redirect limit error = %v, code = %q", err, CodeOf(err))
	}

	credentialURL, err := url.Parse("https://user:secret@example.test/next")
	if err != nil {
		t.Fatal(err)
	}

	request.URL = credentialURL
	if err := SafeRedirectPolicy(request, nil); CodeOf(err) != FetchRejected {
		t.Fatalf("credential redirect error = %v, code = %q", err, CodeOf(err))
	}
}

func TestRedirectPolicyDropsRefererAcrossOriginsAndSchemeChanges(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
	}{
		{name: "same origin", from: "https://example.test/start?token=secret", to: "https://example.test/next"},
		{name: "cross origin", from: "https://example.test/start?token=secret", to: "https://elsewhere.test/next"},
		{name: "http to https", from: "http://example.test/start?token=secret", to: "https://example.test/next"},
		{name: "https to http", from: "https://example.test/start?token=secret", to: "http://example.test/next"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			previous := httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.from, nil)
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.to, nil)
			request.Header.Set("Referer", test.from)

			if err := SafeRedirectPolicy(request, []*http.Request{previous}); err != nil {
				t.Fatal(err)
			}

			if got := request.Header.Get("Referer"); got != "" {
				t.Fatalf("redirect retained Referer %q", got)
			}
		})
	}
}

func TestEveryRecipeProvenanceCandidateUsesTheSameSafeURLPolicy(t *testing.T) {
	const secret = "query-secret"

	tests := []struct {
		name string
		body string
		page string
		want string
	}{
		{
			name: "json ld url",
			body: `{"@context":"https://schema.org","@type":"Recipe","name":"Soup","url":"https://example.test/json?token=` + secret + `#oauth","recipeIngredient":["water"],"recipeInstructions":["Heat."]}`,
			page: "https://example.test/fallback?token=" + secret,
			want: "https://example.test/json",
		},
		{
			name: "json ld id",
			body: `{"@context":"https://schema.org","@type":"Recipe","@id":"https://example.test/id?token=` + secret + `#oauth","name":"Soup","recipeIngredient":["water"],"recipeInstructions":["Heat."]}`,
			page: "https://example.test/fallback?token=" + secret,
			want: "https://example.test/id",
		},
		{
			name: "canonical metadata",
			body: `<link rel="canonical" href="/canonical?token=` + secret + `#oauth"><script type="application/ld+json">{"@context":"https://schema.org","@type":"Recipe","name":"Soup","recipeIngredient":["water"],"recipeInstructions":["Heat."]}</script>`,
			page: "https://example.test/fallback?token=" + secret,
			want: "https://example.test/canonical",
		},
		{
			name: "page fallback",
			body: `{"@context":"https://schema.org","@type":"Recipe","name":"Soup","recipeIngredient":["water"],"recipeInstructions":["Heat."]}`,
			page: "https://example.test/fallback?token=" + secret + "#oauth",
			want: "https://example.test/fallback",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := mustExtract(t, test.body, test.page)
			if result.URL != test.want {
				t.Fatalf("URL = %q, want %q", result.URL, test.want)
			}

			if strings.Contains(result.URL, secret) || strings.Contains(result.SourceID, secret) {
				t.Fatalf("secret survived in provenance: %#v", result)
			}
		})
	}
}

func TestAuthorProvenanceUsesSafeURLPolicy(t *testing.T) {
	result := mustExtract(t, `{"@context":"https://schema.org","@type":"Recipe","name":"Soup","author":{"@type":"Person","name":"A Cook","url":"/authors/cook?reset=author-secret#oauth"},"recipeIngredient":["water"],"recipeInstructions":["Heat."]}`, "https://example.test/recipe?token=page-secret")
	if len(result.Authors) != 1 || result.Authors[0].URL != "https://example.test/authors/cook" {
		t.Fatalf("authors = %#v", result.Authors)
	}
}

func TestBlockedStatusIsAFetchFailureNotAnExtractionFailure(t *testing.T) {
	allowLoopback(t)

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		// What a bot wall actually looks like.
		response.WriteHeader(http.StatusForbidden)
		_, _ = response.Write([]byte("<html><title>Just a moment...</title></html>"))
	}))
	defer server.Close()

	_, err := Fetch(context.Background(), server.URL)
	if CodeOf(err) != FetchBlocked {
		t.Fatalf("err = %v, code = %q", err, CodeOf(err))
	}

	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("the reader should be told what the site said: %v", err)
	}
}

func TestEnormousResponseIsAbortedWhileStreaming(t *testing.T) {
	allowLoopback(t)
	// No Content-Length: the ceiling has to hold on the stream itself rather
	// than on a header the server chose not to send.
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html")

		chunk := strings.Repeat("a", 64<<10)
		for written := 0; written < MaximumPageBytes+(1<<20); written += len(chunk) {
			if _, err := response.Write([]byte(chunk)); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	_, err := Fetch(context.Background(), server.URL)
	if CodeOf(err) != FetchTooLarge {
		t.Fatalf("err = %v, code = %q", err, CodeOf(err))
	}
}

func TestUnsupportedContentTypeIsRefused(t *testing.T) {
	allowLoopback(t)

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/pdf")
		_, _ = response.Write([]byte("%PDF-1.7"))
	}))
	defer server.Close()

	_, err := Fetch(context.Background(), server.URL)
	if CodeOf(err) != UnsupportedContentType {
		t.Fatalf("err = %v, code = %q", err, CodeOf(err))
	}
}

func TestFetchedPageExtractsThroughTheSamePipeline(t *testing.T) {
	allowLoopback(t)

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(response, htmlWith(simpleRecipe))
	}))
	defer server.Close()

	result, err := FetchAndExtract(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("fetch and extract: %v", err)
	}

	if result.Name != "Bolo de cenoura" {
		t.Fatalf("recipe = %#v", result)
	}
}

// Schema.org models keywords, cuisines, and categories as lists, which
// publishers often serialize as one delimited string.
func TestDelimitedListsAreRecoveredForEveryListField(t *testing.T) {
	recipe := mustExtract(t, `{"@type":"Recipe","name":"Cassoulet",
		"keywords":"cake, dessert; easy","recipeCuisine":"British, French",
		"recipeCategory":"Dessert, Baking","recipeIngredient":["x"],"recipeInstructions":["y"]}`,
		"https://example.test/r")
	for _, expectation := range []struct {
		field string
		got   []string
		want  []string
	}{
		{"keywords", recipe.Keywords, []string{"cake", "dessert", "easy"}},
		{"cuisines", recipe.Cuisines, []string{"British", "French"}},
		{"categories", recipe.Categories, []string{"Dessert", "Baking"}},
	} {
		if len(expectation.got) != len(expectation.want) {
			t.Fatalf("%s = %#v, want %#v", expectation.field, expectation.got, expectation.want)
		}

		for index, value := range expectation.want {
			if expectation.got[index] != value {
				t.Fatalf("%s = %#v, want %#v", expectation.field, expectation.got, expectation.want)
			}
		}
	}
}

// A value with no delimiter is one value, whatever it contains.
func TestAnUndelimitedListValueIsLeftExactlyAsAuthored(t *testing.T) {
	recipe := mustExtract(t, `{"@type":"Recipe","name":"Mapo tofu",
		"recipeCuisine":"Chinese (Sichuan)","recipeIngredient":["x"],"recipeInstructions":["y"]}`,
		"https://example.test/r")
	if len(recipe.Cuisines) != 1 || recipe.Cuisines[0] != "Chinese (Sichuan)" {
		t.Fatalf("cuisines = %#v", recipe.Cuisines)
	}
}

// Every author shape Schema.org permits, read as one model.
func TestAuthorsInEveryShape(t *testing.T) {
	for name, expectation := range map[string]struct {
		body  string
		names []string
		kinds []string
	}{
		"string":       {`"author":"Nigel Slater"`, []string{"Nigel Slater"}, []string{""}},
		"person":       {`"author":{"@type":"Person","name":"Nigel Slater"}`, []string{"Nigel Slater"}, []string{"person"}},
		"organization": {`"author":{"@type":"Organization","name":"BBC Food"}`, []string{"BBC Food"}, []string{"organization"}},
		"mixed array":  {`"author":[{"@type":"Person","name":"One"},"Two"]`, []string{"One", "Two"}, []string{"person", ""}},
		"untyped":      {`"author":{"name":"No Type"}`, []string{"No Type"}, []string{""}},
	} {
		body := `{"@type":"Recipe","name":"A",` + expectation.body +
			`,"recipeIngredient":["x"],"recipeInstructions":["y"]}`

		recipe := mustExtract(t, body, "https://example.test/r")
		if len(recipe.Authors) != len(expectation.names) {
			t.Fatalf("%s: authors = %#v", name, recipe.Authors)
		}

		for index, author := range recipe.Authors {
			if author.Name != expectation.names[index] || author.Kind != expectation.kinds[index] {
				t.Fatalf("%s: authors = %#v", name, recipe.Authors)
			}
		}
	}
}

// A page whose @type lists Recipe among other types is still a recipe.
func TestTypeAsAnArrayStillSelectsTheRecipe(t *testing.T) {
	recipe := mustExtract(t, `{"@type":["Recipe","NewsArticle"],"name":"Both",
		"recipeIngredient":["x"],"recipeInstructions":["y"]}`, "https://example.test/r")
	if recipe.Name != "Both" {
		t.Fatalf("recipe = %#v", recipe)
	}
}

// The generic importer must carry no publisher-specific branches. This reads
// the package's own source.
func TestExtractionCarriesNoPublisherSpecificBranches(t *testing.T) {
	host := regexp.MustCompile(`(?i)"[a-z0-9-]+\.(?:com|net|org|io|co\.uk|de|fr)"`)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}

		if found := host.FindAllString(string(body), -1); found != nil {
			t.Errorf("%s names publishers: %v", name, found)
		}
	}
}

// Extraction never runs page scripts, which would execute untrusted code.
func TestExtractionNeverExecutesPageScripts(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	engines := []string{"chromedp", "playwright", "github.com/dop251/goja", "robertkrimen/otto", "go-rod"}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}

		for _, engine := range engines {
			if strings.Contains(string(body), engine) {
				t.Errorf("%s reaches for %s; extraction must not run page scripts", name, engine)
			}
		}
	}
}
