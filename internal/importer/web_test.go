package importer_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/importer/webrecipe"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func decode(t *testing.T, body string) recipe.Document {
	t.Helper()

	batch, err := importer.DecodeWebRecipe([]byte(body), "text/html", "https://example.test/recipe")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(batch.Recipes) != 1 {
		t.Fatalf("recipes = %#v", batch.Recipes)
	}

	return batch.Recipes[0].Document
}

func decodeBatch(t *testing.T, body string) importer.ImportedRecipe {
	t.Helper()

	batch, err := importer.DecodeWebRecipe([]byte(body), "text/html", "https://example.test/recipe")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	return batch.Recipes[0]
}

func TestImportedIngredientKeepsItsSourceLineWhateverIsUnderstood(t *testing.T) {
	// The line that does not fit quantity, unit, name, and note is exactly the
	// line an importer must not lose. It arrives whole either way.
	body := `{"@type":"Recipe","name":"Braise","inLanguage":"en-US","recipeInstructions":["Cook."],
		"recipeIngredient":["1 (14-ounce) can tomatoes, drained, juices reserved","2 cups flour"]}`

	document := decode(t, body)
	if document.SourceLocale != "en-US" {
		t.Fatalf("source locale = %q", document.SourceLocale)
	}

	first := document.IngredientSections[0].Ingredients[0]
	if first.SourceText != "1 (14-ounce) can tomatoes, drained, juices reserved" {
		t.Fatalf("source line lost: %#v", first)
	}

	second := document.IngredientSections[0].Ingredients[1]
	if second.Quantity == nil || second.Quantity.Unit != "volume.cup.us.customary" || second.SourceText != "2 cups flour" {
		t.Fatalf("a readable line should keep both readings: %#v", second)
	}
}

func TestIngredientGroupsBecomeIngredientSections(t *testing.T) {
	body := `{"@type":"Recipe","name":"Layer cake","recipeInstructions":["Bake."],
		"recipeIngredient":["For the cake","2 cups flour","For the frosting","200 g butter"]}`

	document := decode(t, body)
	if len(document.IngredientSections) != 2 {
		t.Fatalf("sections = %#v", document.IngredientSections)
	}

	if document.IngredientSections[0].Title != "For the cake" || document.IngredientSections[1].Title != "For the frosting" {
		t.Fatalf("titles = %#v", document.IngredientSections)
	}
}

func TestStatedStepIngredientsBecomeReferences(t *testing.T) {
	// A page's own statement of which ingredients a step uses is kept, not
	// replaced by inference from the prose.
	body := `{"@type":"Recipe","name":"Biryani","recipeIngredient":["3.5 tbsp Ghee","2 pcs Cloves","1 kg rice"],
		"recipeInstructions":[{"@type":"HowToSection","name":"Gravy Preparation",
		"itemListElement":[{"@type":"HowToStep","text":"Heat the ghee and add the cloves.",
		"supply":["3.5 tbsp Ghee","2 pcs Cloves"]}]}]}`

	document := decode(t, body)
	if len(document.InstructionSections) != 1 || document.InstructionSections[0].Title != "Gravy Preparation" {
		t.Fatalf("sections = %#v", document.InstructionSections)
	}

	step := document.InstructionSections[0].Steps[0]
	if len(step.IngredientUses) != 2 {
		t.Fatalf("stated ingredient references lost: %#v", step)
	}

	byID := map[string]string{}
	for _, ingredient := range document.IngredientSections[0].Ingredients {
		byID[ingredient.ID] = ingredient.SourceText
	}

	for _, use := range step.IngredientUses {
		if byID[use.IngredientRef] == "" {
			t.Fatalf("reference %q points at no ingredient", use.IngredientRef)
		}

		if strings.Contains(strings.ToLower(byID[use.IngredientRef]), "rice") {
			t.Fatalf("a step was given an ingredient the page did not name: %q", byID[use.IngredientRef])
		}
	}
}

func TestSchemaOrgToolsBecomeEquipmentWithoutSupplies(t *testing.T) {
	body := `{"@context":"https://schema.org","@type":"Recipe","name":"Layer cake",
		"recipeIngredient":["flour"],"recipeInstructions":[{"@type":"HowToStep","text":"Bake.",
		"tool":["2 cake pans",{"@type":"HowToTool","name":"stand mixer with paddle"}],
		"supply":["parchment paper"]}]}`
	document := decode(t, body)

	want := []recipe.Equipment{{Name: "2 cake pans"}, {Name: "stand mixer with paddle"}}
	if !reflect.DeepEqual(document.Equipment, want) {
		t.Fatalf("equipment = %#v, want %#v", document.Equipment, want)
	}

	if strings.Contains(fmt.Sprint(document.Equipment), "parchment") {
		t.Fatalf("consumable supply became equipment: %#v", document.Equipment)
	}

	for _, note := range document.Notes {
		if strings.HasPrefix(note, "Equipment:") {
			t.Fatalf("typed equipment was also hidden in notes: %q", note)
		}
	}
}

func TestNestedSectionTitlesSurviveFlattening(t *testing.T) {
	// Savorum's instruction sections do not nest, so the parent's title is kept
	// alongside the child's rather than one of them being discarded.
	body := `{"@type":"Recipe","name":"Nested","recipeIngredient":["flour"],
		"recipeInstructions":[{"@type":"HowToSection","name":"Cake",
		"itemListElement":[{"@type":"HowToStep","text":"Bake the cake."},
		{"@type":"HowToSection","name":"Filling",
		 "itemListElement":[{"@type":"HowToStep","text":"Whip the cream."}]}]}]}`
	document := decode(t, body)

	titles := make([]string, 0, len(document.InstructionSections))
	for _, section := range document.InstructionSections {
		titles = append(titles, section.Title)
	}

	if len(titles) != 2 || titles[0] != "Cake" || titles[1] != "Cake · Filling" {
		t.Fatalf("titles = %#v", titles)
	}
}

func TestNamedStepRetainsItsAuthoredTitleWithoutInventingASection(t *testing.T) {
	body := `{"@type":"Recipe","name":"Named","recipeIngredient":["flour"],
		"recipeInstructions":[{"@type":"HowToStep","name":"Shape the loaf","text":"Roll it into a tight cylinder."},
		{"@type":"HowToStep","text":"Bake for 40 minutes."}]}`

	document := decode(t, body)
	if len(document.InstructionSections) != 1 || len(document.InstructionSections[0].Steps) != 2 {
		t.Fatalf("sections = %#v", document.InstructionSections)
	}

	if document.InstructionSections[0].Steps[0].Title != "Shape the loaf" {
		t.Fatalf("step title lost: %#v", document.InstructionSections[0])
	}

	if got := document.InstructionSections[0].Steps[0].Text; got != "Roll it into a tight cylinder." {
		t.Fatalf("the name should not be repeated into the step: %q", got)
	}
}

func TestTitleOnlyHowToStepRemainsTitleOnly(t *testing.T) {
	body := `{"@type":"Recipe","name":"Named","recipeIngredient":["flour"],
		"recipeInstructions":[{"@type":"HowToStep","name":"Shape the loaf"}]}`

	step := decode(t, body).InstructionSections[0].Steps[0]
	if step.Title != "Shape the loaf" || step.Text != "" {
		t.Fatalf("title-only step = %#v", step)
	}
}

func TestImporterDoesNotInventTemperaturePurposeFromProse(t *testing.T) {
	t.Parallel()

	body := `{"@type":"Recipe","name":"Roast","recipeIngredient":["chicken"],
		"recipeInstructions":["Preheat the oven to 350°F and cook until the chicken is 165°F internally."]}`
	document := decode(t, body)

	step := document.InstructionSections[0].Steps[0]
	if len(step.Temperatures) != 0 {
		t.Fatalf("unstructured prose acquired inferred temperatures or purposes: %#v", step.Temperatures)
	}
}

func TestStepIDsAreUniqueAcrossSections(t *testing.T) {
	body := `{"@type":"Recipe","name":"Two parts","recipeIngredient":["flour"],
		"recipeInstructions":[
		{"@type":"HowToSection","name":"Dough","itemListElement":[{"@type":"HowToStep","text":"Mix."},{"@type":"HowToStep","text":"Rest."}]},
		{"@type":"HowToSection","name":"To cook","itemListElement":[{"@type":"HowToStep","text":"Fry."}]}]}`
	document := decode(t, body)
	seen := map[string]bool{}

	for _, section := range document.InstructionSections {
		for _, step := range section.Steps {
			if seen[step.ID] {
				t.Fatalf("step id %q is used twice", step.ID)
			}

			seen[step.ID] = true
		}
	}

	if len(seen) != 3 {
		t.Fatalf("steps = %d", len(seen))
	}
}

func TestProvenanceIsRecorded(t *testing.T) {
	body := `{"@type":"Recipe","name":"Sourced","recipeIngredient":["flour"],"recipeInstructions":["Bake."],
		"author":{"@type":"Person","name":"A Cook"}}`
	item := decodeBatch(t, body)

	source := item.Document.Source
	if source == nil || source.URL != "https://example.test/recipe" || source.Name != "example.test" {
		t.Fatalf("source = %#v", source)
	}

	if source.RetrievedAt == "" {
		t.Fatalf("retrieval time is part of what makes an import explainable: %#v", source)
	}

	if source.SourceID != "" {
		t.Fatalf("web URL was duplicated into an unscoped source ID: %#v", source)
	}

	if len(item.Document.Authors) != 1 || item.Document.Authors[0] != (recipe.Author{Name: "A Cook", Kind: recipe.AuthorPerson}) {
		t.Fatalf("authors = %#v", item.Document.Authors)
	}

	if source.Attribution != "" {
		t.Fatalf("recipe authors were conflated with source attribution: %#v", source)
	}

	if item.SourceKey != "https://example.test/recipe" {
		t.Fatalf("source key = %q", item.SourceKey)
	}
}

func TestStructuredSchemaOrgAuthorsPreserveExplicitKindsURLsAndOrder(t *testing.T) {
	body := `{"@type":"Recipe","name":"Authored","recipeIngredient":["flour"],"recipeInstructions":["Bake."],
		"author":[
			{"@type":"Person","name":"A. Cook","url":"https://example.test/authors/a-cook"},
			{"@type":"Organization","name":"Example Test Kitchen","@id":"https://example.test/test-kitchen"},
			{"name":"Unclassified Credit"},
			"Name Only"
		]}`
	document := decode(t, body)

	want := []recipe.Author{
		{Name: "A. Cook", Kind: recipe.AuthorPerson, URL: "https://example.test/authors/a-cook"},
		{Name: "Example Test Kitchen", Kind: recipe.AuthorOrganization, URL: "https://example.test/test-kitchen"},
		{Name: "Unclassified Credit"},
		{Name: "Name Only"},
	}
	if !reflect.DeepEqual(document.Authors, want) {
		t.Fatalf("authors = %#v, want %#v", document.Authors, want)
	}
}

func TestSchemaOrgAuthoredMetadataMapsWithoutInventedClassifications(t *testing.T) {
	t.Parallel()

	body := `{"@type":"Recipe","name":"Metadata","recipeIngredient":["flour"],"recipeInstructions":["Bake."],
		"recipeCategory":["Cake","Celebration cake"],
		"recipeCuisine":["British","Australian"],
		"keywords":"layered, make-ahead; layered",
		"cookingMethod":["Baking","Whipping"],
		"datePublished":"2023-05-04",
		"dateModified":"2025-06-07T08:09:10-06:00"}`

	document := decode(t, body)
	if got, want := document.Categories, []string{"Cake", "Celebration cake"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("categories = %#v, want %#v", got, want)
	}

	if len(document.Courses) != 0 {
		t.Fatalf("undifferentiated recipeCategory invented courses: %#v", document.Courses)
	}

	if got, want := document.Cuisines, []string{"British", "Australian"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cuisines = %#v, want %#v", got, want)
	}

	if got, want := document.Keywords, []string{"layered", "make-ahead"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("keywords = %#v, want %#v", got, want)
	}

	if got, want := document.CookingMethods, []string{"Baking", "Whipping"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cooking methods = %#v, want %#v", got, want)
	}

	if document.Published != "2023-05-04" || document.Modified != "2025-06-07T08:09:10-06:00" {
		t.Fatalf("source chronology changed: published=%q modified=%q", document.Published, document.Modified)
	}
}

func TestWarningsTravelWithTheImport(t *testing.T) {
	body := `<!doctype html><html><head>
		<script type="application/ld+json">{"@type":"Recipe", broken</script>
		<script type="application/ld+json">{"@type":"Recipe","name":"Good","recipeIngredient":["flour"],"recipeInstructions":["Bake."]}</script>
		</head><body></body></html>`

	item := decodeBatch(t, body)
	if len(item.Diagnostics) == 0 {
		t.Fatalf("an import that worked around a broken block should say so")
	}

	found := false

	for _, warning := range item.Diagnostics {
		if warning.Code == "structured-data-unreadable" {
			found = true
		}
	}

	if !found {
		t.Fatalf("diagnostics = %#v", item.Diagnostics)
	}
}

func TestIncompleteRecipeIsReportedAsSuchThroughTheRegistry(t *testing.T) {
	body := `<!doctype html><html><head><script type="application/ld+json">
		{"@type":"Recipe","name":"Chocolate Cake","image":"cake.jpg"}</script></head><body></body></html>`

	_, err := importer.DefaultRegistry().Decode([]byte(body), "recipe.html")
	if err == nil {
		t.Fatal("expected the incomplete recipe to be refused")
	}

	if got := webrecipe.CodeOf(err); got != webrecipe.RecipeIncomplete {
		t.Fatalf("code = %q, want %q", got, webrecipe.RecipeIncomplete)
	}
}

func TestRecipeCardSuppliesGroupingSchemaOrgFlattened(t *testing.T) {
	// The structured data is a flat list; the card on the same page knows the
	// groups and the parts of each line.
	body := `<!doctype html><html><head><script type="application/ld+json">
		{"@type":"Recipe","name":"Blog cake","recipeIngredient":["2 cups flour","200 g butter"],
		"recipeInstructions":["Bake."]}</script></head><body>
		<div class="wprm-recipe">
		  <div class="wprm-recipe-ingredient-group">
		    <h4 class="wprm-recipe-group-name">Cake</h4>
		    <li class="wprm-recipe-ingredient"><span class="wprm-recipe-ingredient-amount">2</span>
		      <span class="wprm-recipe-ingredient-unit">cups</span>
		      <span class="wprm-recipe-ingredient-name">flour</span></li>
		  </div>
		  <div class="wprm-recipe-ingredient-group">
		    <h4 class="wprm-recipe-group-name">Frosting</h4>
		    <li class="wprm-recipe-ingredient"><span class="wprm-recipe-ingredient-amount">200</span>
		      <span class="wprm-recipe-ingredient-unit">g</span>
		      <span class="wprm-recipe-ingredient-name">butter</span>
		      <span class="wprm-recipe-ingredient-notes">softened</span></li>
		  </div>
		</div></body></html>`

	item := decodeBatch(t, body)
	if len(item.Document.IngredientSections) != 2 {
		t.Fatalf("sections = %#v", item.Document.IngredientSections)
	}

	if item.Document.IngredientSections[0].Title != "Cake" || item.Document.IngredientSections[1].Title != "Frosting" {
		t.Fatalf("titles = %#v", item.Document.IngredientSections)
	}
	// The method came from the structured data and was left alone: an enricher
	// repairs fields, it does not take the recipe over.
	if len(item.Document.InstructionSections) != 1 || len(item.Document.InstructionSections[0].Steps) != 1 {
		t.Fatalf("instructions = %#v", item.Document.InstructionSections)
	}
	// Reading the card is how the import went, not something left to decide.
	reported := false

	for _, diagnostic := range item.Diagnostics {
		if diagnostic.Code == "ingredients-enriched" {
			reported = true

			if diagnostic.EffectiveClass() != importer.DiagnosticInformational {
				t.Fatalf("enrichment asks for review: %#v", diagnostic)
			}
		}
	}

	if !reported {
		t.Fatalf("diagnostics = %#v", item.Diagnostics)
	}
}

func TestRecipeCardDoesNotReplaceARicherStructuredReading(t *testing.T) {
	// A card that says less than the structured data must not win. Losing
	// ingredients is never an improvement.
	body := `<!doctype html><html><head><script type="application/ld+json">
		{"@type":"Recipe","name":"Blog cake","recipeIngredient":["2 cups flour","200 g butter","1 tsp salt"],
		"recipeInstructions":["Bake."]}</script></head><body>
		<div class="wprm-recipe"><li class="wprm-recipe-ingredient">2 cups flour</li></div>
		</body></html>`
	document := decode(t, body)

	total := 0
	for _, section := range document.IngredientSections {
		total += len(section.Ingredients)
	}

	if total != 3 {
		t.Fatalf("ingredients = %d, want the structured data's 3", total)
	}
}

func TestSREFJSONIsNotClaimedByTheWebDecoder(t *testing.T) {
	// A .json upload may be a portable SREF document. The web decoder has to
	// decline it clearly so the SREF path still gets its turn.
	body := `{"sref":"1.0","id":"r1","title":"A recipe","ingredient_sections":[],"instruction_sections":[]}`

	_, err := importer.DecodeWebRecipe([]byte(body), "application/json", "")
	if got := webrecipe.CodeOf(err); got != webrecipe.NoRecipeStructuredData {
		t.Fatalf("code = %q, want %q", got, webrecipe.NoRecipeStructuredData)
	}
}

func TestStepPhotographsAreRegisteredAndReferenced(t *testing.T) {
	body := `{"@type":"Recipe","name":"Illustrated","recipeIngredient":["flour"],
		"recipeInstructions":[{"@type":"HowToStep","text":"Fold the dough.",
		"image":["https://images.example/fold.jpg"]},
		{"@type":"HowToStep","text":"Bake it."}]}`
	item := decodeBatch(t, body)

	steps := item.Document.InstructionSections[0].Steps
	if len(steps) != 2 || len(steps[0].ImageRefs) != 1 || len(steps[1].ImageRefs) != 0 {
		t.Fatalf("steps = %#v", steps)
	}
	// The reference names the asset that will be stored rather than the address
	// it came from, and the asset says so under the same identifier.
	var found bool

	for _, asset := range item.Assets {
		if asset.LogicalID == steps[0].ImageRefs[0] {
			found = true

			if asset.URL != "https://images.example/fold.jpg" || asset.Role != recipe.AssetRoleImage || asset.RecipeImage {
				t.Fatalf("asset = %#v", asset)
			}
		}
	}

	if !found {
		t.Fatalf("step image reference %q matches no asset: %#v", steps[0].ImageRefs[0], item.Assets)
	}

	if strings.HasPrefix(steps[0].ImageRefs[0], "http") {
		t.Fatalf("a step kept a raw address instead of an asset identifier: %q", steps[0].ImageRefs[0])
	}
}

func TestRecipePhotographsPreserveSourceOrder(t *testing.T) {
	body := `{"@type":"Recipe","name":"Gallery","image":[
		"https://images.example/finished.jpg",
		"https://images.example/process.jpg"
	],"recipeIngredient":["flour"],"recipeInstructions":["Bake."]}`

	item := decodeBatch(t, body)
	if len(item.Assets) != 2 {
		t.Fatalf("assets = %#v", item.Assets)
	}

	for index, want := range []string{"https://images.example/finished.jpg", "https://images.example/process.jpg"} {
		asset := item.Assets[index]
		if asset.URL != want || asset.LogicalID != fmt.Sprintf("recipe-image-%d", index+1) || !asset.RecipeImage || asset.Role != recipe.AssetRoleImage {
			t.Fatalf("asset %d = %#v", index, asset)
		}
	}
}

// A page that states "20 minutes" instead of PT20M keeps it as an opaque
// duration.
func TestAuthoredDurationsAreKeptRatherThanRejected(t *testing.T) {
	batch, err := importer.DecodeWebRecipe([]byte(`{"@context":"https://schema.org","@type":"Recipe","name":"Slow braise",
		"prepTime":"20 minutes","cookTime":"PT1H","totalTime":"about 3 hours",
		"recipeIngredient":["1 kg beef"],"recipeInstructions":["Braise."]}`), "text/html", "https://example.test/recipe")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	document := batch.Recipes[0].Document
	if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); err != nil {
		t.Fatalf("a page with an authored duration could not be imported: %v", err)
	}

	if document.Times.Cook == nil || document.Times.Cook.Value != "PT1H" {
		t.Fatalf("an exact duration stopped being exact: %#v", document.Times.Cook)
	}
	// The wording is kept as written. Reading "about 3 hours" as three hours
	// would assert a precision the page did not offer.
	if document.Times.Prep == nil || document.Times.Prep.Text != "20 minutes" || document.Times.Prep.Value != "" {
		t.Fatalf("prep = %#v", document.Times.Prep)
	}

	if document.Times.Total == nil || document.Times.Total.Text != "about 3 hours" {
		t.Fatalf("total = %#v", document.Times.Total)
	}
	// And the reader is told, because a duration kept as text is a fidelity
	// result rather than a silent downgrade.
	var told bool

	for _, diagnostic := range batch.Recipes[0].Diagnostics {
		if diagnostic.Code == "duration-not-iso" {
			told = true

			if !strings.Contains(diagnostic.Message, "20 minutes") ||
				!strings.Contains(diagnostic.Message, "about 3 hours") {
				t.Fatalf("diagnostic did not name the wording: %q", diagnostic.Message)
			}
		}
	}

	if !told {
		t.Fatalf("diagnostics = %#v", batch.Recipes[0].Diagnostics)
	}
}

func TestPartialWebRecipeAbsenceAndLossRemainDistinct(t *testing.T) {
	for _, body := range []string{
		`{"@type":"Recipe","name":"Formula","recipeIngredient":["salt"]}`,
		`{"@type":"Recipe","name":"Technique","recipeInstructions":["Rest."]}`,
	} {
		item := decodeBatch(t, body)
		if err := recipe.ValidateForWrite(item.Document, units.DefaultCatalog()); err != nil {
			t.Fatal(err)
		}

		for _, section := range item.Document.IngredientSections {
			if len(section.Ingredients) == 0 {
				t.Fatal("fabricated ingredient section")
			}
		}

		for _, section := range item.Document.InstructionSections {
			if len(section.Steps) == 0 {
				t.Fatal("fabricated instruction section")
			}
		}

		for _, diagnostic := range item.Diagnostics {
			if strings.HasPrefix(diagnostic.Code, "no-") && diagnostic.EffectiveClass() != importer.DiagnosticInformational {
				t.Fatal("absence classified as loss")
			}
		}
	}

	item := decodeBatch(t, `{"@type":"Recipe","name":"Damaged method","recipeIngredient":["salt"],"recipeInstructions":[{"@type":"HowToStep"}]}`)
	found := false

	for _, diagnostic := range item.Diagnostics {
		if diagnostic.Code == "empty-step" {
			found = true

			if diagnostic.EffectiveClass() != importer.DiagnosticActionable {
				t.Fatal("loss not actionable")
			}
		}
	}

	if !found {
		t.Fatal("source step loss went unreported")
	}
}

func TestRecipeReadsTheLanguageThePageDeclares(t *testing.T) {
	// The language is often stated on the page and WebPage node but not the
	// Recipe; without it, cups and tablespoons stay unresolved.
	body := `<!doctype html><html lang="en-US"><head><script type="application/ld+json">
		{"@graph":[{"@type":"WebPage","inLanguage":"en-US"},
		{"@type":"Recipe","name":"Blog cake","recipeIngredient":["2 cups flour","1 tbsp oil"],
		"recipeInstructions":["Bake."]}]}</script></head><body></body></html>`

	document := decode(t, body)
	if document.SourceLocale != "en-US" {
		t.Fatalf("source locale = %q", document.SourceLocale)
	}

	first := document.IngredientSections[0].Ingredients[0]
	if first.Quantity == nil || first.Quantity.Unit != "volume.cup.us.customary" {
		t.Fatalf("cups were not resolved: %#v", first)
	}
}

func TestStructuredTextIsDecodedRatherThanQuotedBack(t *testing.T) {
	// Syndicated JSON-LD often carries HTML-escaped text.
	body := `<!doctype html><html lang="en-US"><head><script type="application/ld+json">
		{"@type":"Recipe","name":"Blog cake","inLanguage":"en-US",
		"recipeIngredient":["2 cups flour"],
		"recipeInstructions":["Chop the porcini finely (about 3mm/1/8&quot;)."]}</script>
		</head><body></body></html>`

	step := decode(t, body).InstructionSections[0].Steps[0]
	if !strings.Contains(step.Text, `1/8"`) || strings.Contains(step.Text, "&quot;") {
		t.Fatalf("step text = %q", step.Text)
	}
}

func TestRecipeCardRowKeepsOnlyWhatTheRecipeSays(t *testing.T) {
	// The box a reader ticks off is a control rendered as a character, and the
	// note element opens with the punctuation that joined it to the name. Left
	// in place the glyph stops the amount at the front of the line from being
	// read at all, and the note reads ", finely diced".
	body := `<!doctype html><html lang="en-US"><head><script type="application/ld+json">
		{"@type":"Recipe","name":"Ragu","inLanguage":"en-US",
		"recipeIngredient":["1 brown onion","2 tbsp olive oil"],
		"recipeInstructions":["Cook."]}</script></head><body>
		<div class="wprm-recipe"><div class="wprm-recipe-ingredient-group">
		  <li class="wprm-recipe-ingredient"><span class="wprm-checkbox">▢</span>
		    <span class="wprm-recipe-ingredient-amount">1</span>
		    <span class="wprm-recipe-ingredient-name">brown onion</span>
		    <span class="wprm-recipe-ingredient-notes">, finely diced</span></li>
		  <li class="wprm-recipe-ingredient"><span class="wprm-checkbox">▢</span>
		    <span class="wprm-recipe-ingredient-amount">2</span>
		    <span class="wprm-recipe-ingredient-unit">tbsp</span>
		    <span class="wprm-recipe-ingredient-name">olive oil</span></li>
		</div></div></body></html>`

	ingredients := decode(t, body).IngredientSections[0].Ingredients
	if len(ingredients) != 2 {
		t.Fatalf("ingredients = %#v", ingredients)
	}

	if ingredients[0].Name != "brown onion" || ingredients[0].Note != "finely diced" {
		t.Fatalf("first ingredient = %#v", ingredients[0])
	}

	if ingredients[1].Quantity == nil || ingredients[1].Quantity.Unit != "volume.tablespoon.us.customary" {
		t.Fatalf("the checkbox swallowed the amount: %#v", ingredients[1])
	}
}

func TestStepNameRepeatingItsOwnTextIsNotKept(t *testing.T) {
	// Recipe plugins write the whole instruction into both name and text.
	body := `<!doctype html><html lang="en-US"><head><script type="application/ld+json">
		{"@type":"Recipe","name":"Ragu","inLanguage":"en-US","recipeIngredient":["2 cups flour"],
		"recipeInstructions":[
		{"@type":"HowToStep","name":"Soak the porcini in wine.","text":"Soak the porcini in wine."},
		{"@type":"HowToStep","name":"Reduce","text":"Reduce the wine by half."}]}</script>
		</head><body></body></html>`

	steps := decode(t, body).InstructionSections[0].Steps
	if len(steps) != 2 || steps[0].Title != "" || steps[0].Text != "Soak the porcini in wine." {
		t.Fatalf("first step = %#v", steps)
	}
	// A name that genuinely labels the step is still a label.
	if steps[1].Title != "Reduce" {
		t.Fatalf("second step = %#v", steps[1])
	}
}

// Many recipe pages state schema.org's date-times as a wall-clock time with no
// time zone.
func TestAuthoredDatesWithoutATimeZoneKeepTheirDateRatherThanFailingTheImport(t *testing.T) {
	item := decodeBatch(t, `{"@context":"https://schema.org","@type":"Recipe","name":"One-pot pasta",
		"datePublished":"2017-10-30T16:28:45","dateModified":"2024-03-27T17:14:04",
		"recipeIngredient":["8 oz fettuccine"],"recipeInstructions":["Simmer."]}`)

	if err := recipe.ValidateForWrite(item.Document, units.DefaultCatalog()); err != nil {
		t.Fatalf("a page stating a local publication time could not be imported: %v", err)
	}
	// The date is what the page asserted and is kept. The time is not, because
	// SREF forbids inventing the missing zone, and pinning it to UTC would
	// claim an instant the page never named.
	if item.Document.Published != "2017-10-30" || item.Document.Modified != "2024-03-27" {
		t.Fatalf("published=%q modified=%q", item.Document.Published, item.Document.Modified)
	}

	// Both fields are named by one diagnostic, since diagnostics sharing a
	// code are aggregated.
	var told *importer.Diagnostic

	for index, diagnostic := range item.Diagnostics {
		if diagnostic.Code == "chronology-without-time-zone" {
			if told != nil {
				t.Fatalf("one loss was reported twice: %#v", item.Diagnostics)
			}

			told = &item.Diagnostics[index]
		}
	}

	if told == nil || told.Class == importer.DiagnosticBlocking {
		t.Fatalf("diagnostics = %#v", item.Diagnostics)
	}

	if !strings.Contains(told.Message, "publication date") || !strings.Contains(told.Message, "modification date") {
		t.Fatalf("diagnostic did not name both fields: %q", told.Message)
	}

	if len(told.Targets) != 2 || told.Targets[0].Preserved != "2017-10-30" || told.Targets[1].Preserved != "2024-03-27" {
		t.Fatalf("targets = %#v", told.Targets)
	}
}

// An exact date-time is exact, and nothing about it is rewritten.
func TestAuthoredDatesCarryingTheirOffsetSurviveExactly(t *testing.T) {
	for _, stated := range []string{"2024-03-27", "2024-03-27T17:14:04Z", "2024-03-27T17:14:04+05:30"} {
		item := decodeBatch(t, fmt.Sprintf(`{"@type":"Recipe","name":"Exact","datePublished":%q,
			"recipeIngredient":["salt"],"recipeInstructions":["Rest."]}`, stated))
		if item.Document.Published != stated {
			t.Fatalf("published = %q, want %q", item.Document.Published, stated)
		}

		if err := recipe.ValidateForWrite(item.Document, units.DefaultCatalog()); err != nil {
			t.Fatalf("%q could not be imported: %v", stated, err)
		}

		for _, diagnostic := range item.Diagnostics {
			if strings.HasPrefix(diagnostic.Code, "chronology-") {
				t.Fatalf("%q was reported as a loss: %#v", stated, diagnostic)
			}
		}
	}
}

// A byline that is not a date at all costs the recipe its date and nothing
// else. It is not a reason to refuse the ingredients and the steps.
func TestADateThatIsNotADateIsLeftOutAndReported(t *testing.T) {
	item := decodeBatch(t, `{"@type":"Recipe","name":"Prose","datePublished":"March 10, 2024",
		"dateModified":"2024-02-30","recipeIngredient":["salt"],"recipeInstructions":["Rest."]}`)

	if err := recipe.ValidateForWrite(item.Document, units.DefaultCatalog()); err != nil {
		t.Fatalf("a page with an unreadable date could not be imported: %v", err)
	}

	if item.Document.Published != "" || item.Document.Modified != "" {
		t.Fatalf("published=%q modified=%q", item.Document.Published, item.Document.Modified)
	}

	var told *importer.Diagnostic

	for index, diagnostic := range item.Diagnostics {
		if diagnostic.Code == "chronology-unreadable" {
			told = &item.Diagnostics[index]
		}
	}

	if told == nil {
		t.Fatalf("diagnostics = %#v", item.Diagnostics)
	}
	// Both values are named, because a reader cannot check a date the report
	// does not quote back to them.
	if !strings.Contains(told.Message, "March 10, 2024") || !strings.Contains(told.Message, "2024-02-30") {
		t.Fatalf("diagnostic did not name the values: %q", told.Message)
	}
}
