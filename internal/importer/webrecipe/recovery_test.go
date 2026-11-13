package webrecipe

import (
	"context"
	"strings"
	"testing"
)

func TestJSONLDLiteralWhitespaceKeepsAuthoredStrings(t *testing.T) {
	body := "{\"@type\":\"Recipe\",\"name\":\"Invented soup\",\"recipeIngredient\":[\"2 carrots\"],\"recipeInstructions\":[\"Mix the carrots.\nKeep stirring.\r\nServe\twarm.\"]}"
	for _, source := range []string{body, htmlWith(body), htmlWith("<!--" + body + "-->")} {
		result := mustExtract(t, source, "https://example.test/soup")
		if steps := allSteps(result.Sections); len(steps) != 3 || steps[1].Text != "Keep stirring." || steps[2].Text != "Serve warm." {
			t.Fatalf("steps = %#v", steps)
		}
	}

	var decoded struct{ RecipeInstructions []string }

	fixed, err := escapeJSONWhitespace(t.Context(), newWorkBudget(t.Context()), body)
	if err != nil {
		t.Fatal(err)
	}

	if err = decodeJSON(t.Context(), fixed, &decoded); err != nil {
		t.Fatal(err)
	}

	got := decoded.RecipeInstructions[0]
	if got != "Mix the carrots.\nKeep stirring.\r\nServe\twarm." {
		t.Fatalf("authored text = %q", got)
	}
}

func TestJSONLDWhitespaceRepairKeepsLimitsAndInvalidSyntax(t *testing.T) {
	for _, body := range []string{`{"@type":"Recipe", "name":"Bad" "recipeIngredient":["water"]}`, `{"@type":"Recipe","name":'Bad',"recipeIngredient":["water"]}`} {
		if code := extractFailure(t, htmlWith(body), "https://example.test/r"); code != MalformedStructuredData {
			t.Fatalf("invalid syntax = %s", code)
		}
	}

	if _, err := escapeJSONWhitespace(context.Background(), &workBudget{ctx: context.Background(), remaining: 1}, `{"a":"b`+"\n"+`"}`); err == nil {
		t.Fatal("work limit was ignored")
	}

	body := `{"a":"` + strings.Repeat("\n", maximumStructuredBytes/2) + `"}`
	if _, err := escapeJSONWhitespace(t.Context(), newWorkBudget(t.Context()), body); err == nil {
		t.Fatal("expanded JSON limit was ignored")
	}
}

func TestSemanticLegacyPostAndInlineSectionLabels(t *testing.T) {
	body := `<html lang="en"><body><div class="hentry"><h1>Invented soup</h1><div class="post-body"><p><b>Ingredients</b><br>2 carrots<br>1 litre water<br>3 g salt</p><p><b>Instructions</b><br>1. Combine the ingredients in a pan.<br>2. Cook the soup and serve warm.</p></div><div itemprop="comment"><p>Ingredients</p><ul><li>99 unrelated berries</li></ul></div></div></body></html>`

	result := mustExtract(t, body, "https://example.test/soup")
	if result.ReviewText != "" || len(allIngredients(result.IngredientGroups)) != 3 || len(allSteps(result.Sections)) != 2 {
		t.Fatalf("recipe = %#v", result)
	}
}

func TestSemanticMainDoesNotLoseRecipeToRelatedArticles(t *testing.T) {
	body := `<main><h1>Invented soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li><li>1 litre water</li><li>3 g salt</li></ul><h2>Instructions</h2><ol><li>Combine the ingredients in a pan.</li><li>Cook the soup and serve warm.</li></ol><article><h2>Related soup</h2><p>Read another recipe.</p></article></main>`

	result := mustExtract(t, body, "https://example.test/soup")
	if result.Name != "Invented soup" || len(allIngredients(result.IngredientGroups)) != 3 || len(allSteps(result.Sections)) != 2 {
		t.Fatalf("recipe = %#v", result)
	}
}

func TestSemanticRejectsNumericMetadataAndRecipeLinks(t *testing.T) {
	fixtures := []string{
		`<main><h1>Privacy policy</h1><p>1.1. We describe how we process information.</p><p>1.2. We describe how we store information.</p><p>2.1. We describe the applicable retention terms.</p><p>1. Read all of the terms carefully.</p><p>2. Contact us with any questions.</p></main>`,
		`<main><h1>Recipe index</h1><div><a href="/a">15 easy recipes</a><hr><a href="/b">20 quick dinners</a><hr><a href="/c">30 summer meals</a><hr><a href="/d">40 winter meals</a><hr><a href="/e">50 party recipes</a></div><p>Mix and cook recipes from our collection.</p></main>`,
		`<main><h1>Cooking news</h1><div>11 Nov 2026, 10:52 WIB</div><div>11 Nov 2026, 11:00 WIB</div><ul><li>05 / 11 / 2026 17:00 WIB</li><li>10 / 11 / 2026 12:00 WIB</li><li>06 / 11 / 2026 09:00 WIB</li></ul><p>Mix and cook ingredients over a low heat.</p></main>`,
		`<main><h1>Members' soup</h1><p>Subscribe to read the ingredients and method.</p><ul><li>178kcal</li><li>24.1g</li><li>6g</li><li>7.8g</li><li>1.8g</li></ul><p>Mix and cook with our exclusive recipes.</p></main>`,
	}
	for _, body := range fixtures {
		if code := extractFailure(t, body, "https://example.test/page"); code != NoRecipeStructuredData {
			t.Fatalf("non-recipe = %s", code)
		}
	}
}

func TestSemanticLegacyLabelsAndOrphanListItems(t *testing.T) {
	body := `<div class="hentry"><h1>Invented soup</h1><div class="post-body"><a href="/photo"><img src="/photo.jpg"></a><a href="/photo2"><img src="/photo2.jpg"><br><span>Ingredients:</span></a><li>2 carrots</li><li>1 litre water</li><li>3 g salt</li><b>Instructions:</b><br>Mix everything in the pan and simmer gently.</div></div>`

	result := mustExtract(t, body, "https://example.test/soup")
	if result.ReviewText == "" || !strings.Contains(result.ReviewText, "2 carrots") || !strings.Contains(result.ReviewText, "simmer") {
		t.Fatalf("legacy recipe = %#v", result)
	}
}

func TestStructuredRecipeSurvivesOversizedImageAttributes(t *testing.T) {
	image := `<img src="data:image/jpeg;base64,` + strings.Repeat("a", 70<<10) + `">`
	body := htmlWith(`{"@type":"Recipe","name":"Invented soup","recipeIngredient":["2 carrots"],"recipeInstructions":["Cook the carrots."]}`) + image

	result := mustExtract(t, body, "https://example.test/soup")
	if result.Name != "Invented soup" || len(allIngredients(result.IngredientGroups)) != 1 || !hasWarning(result, "structured-data-only") {
		t.Fatalf("structured recovery = %#v", result)
	}

	if code := extractFailure(t, htmlWith(`{"@type":"Recipe","name":"Metadata only"}`)+image, "https://example.test/soup"); code != RecipeIncomplete {
		t.Fatalf("metadata-only recovery = %s", code)
	}

	if code := extractFailure(t, body+`<p>`+strings.Repeat("x", maximumHTMLTokenBytes+1)+`</p>`, "https://example.test/soup"); code != ExtractionLimited {
		t.Fatalf("token budget = %s", code)
	}
}

func TestSemanticEntryProtocols(t *testing.T) {
	for _, attributes := range []string{`class="hentry"`, `class="h-entry"`, `itemprop="articleBody"`} {
		body := `<div ` + attributes + `><h1>Invented soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li><li>1 litre water</li><li>3 g salt</li></ul><h2>Instructions</h2><ol><li>Mix everything in the pan.</li><li>Cook the soup and serve warm.</li></ol></div>`

		result := mustExtract(t, body, "https://example.test/soup")
		if result.ReviewText != "" || len(allIngredients(result.IngredientGroups)) != 3 {
			t.Fatalf("entry %s = %#v", attributes, result)
		}
	}
}

func TestSemanticInlineTextPreservesAuthoredBoundaries(t *testing.T) {
	body := `<main><h1>Invented soup</h1><div><b>Ingredients:</b><br>1<span>0</span><!-- note --><sup>0</sup><script>ignored()</script>0 g carrots<br>2 g ל<em>י</em>מון<br>3 g cafe<span>́</span></div><h2>Instructions</h2><ol><li>Combine everything in a pan.</li><li>Cook the soup and serve warm.</li></ol></main>`
	result := mustExtract(t, body, "https://example.test/soup")

	ingredients := allIngredients(result.IngredientGroups)
	if len(ingredients) != 3 || ingredients[0] != "1000 g carrots" || ingredients[1] != "2 g לימון" || ingredients[2] != "3 g café" {
		t.Fatalf("inline text = %#v", ingredients)
	}
}

func TestSemanticRequiresExplicitRecipeStructure(t *testing.T) {
	for _, body := range []string{
		`<main><h1>Invented soup</h1><ul><li>2 carrots</li><li>1 litre water</li><li>3 g salt</li><li>1 onion</li><li>2 tsp oil</li></ul><p>Mix everything in the pan and simmer gently.</p></main>`,
		`<div class="layout"><h1>Invented soup</h1><ul><li>2 carrots</li><li>1 litre water</li><li>3 g salt</li></ul><ol><li>Mix everything in the pan.</li><li>Cook the soup and serve warm.</li></ol></div>`,
		`<div class="layout"><h1>Invented soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li></ul><h2>Instructions</h2><p>Serve.</p></div>`,
	} {
		if code := extractFailure(t, body, "https://example.test/soup"); code != NoRecipeStructuredData {
			t.Fatalf("unscoped or unlabelled text = %s", code)
		}
	}
}

func TestSemanticLabelsBoundARecipeOnAPageWithoutAnArticle(t *testing.T) {
	recipe := `<h1>Invented soup</h1><div class="row"><div class="ingredients"><h2>Ingredients</h2><ul><li>2 carrots</li><li>1 litre water</li><li>3 g salt</li></ul></div>` +
		`<div class="method"><h2>Instructions</h2><ol><li>Mix everything in the pan.</li><li>Cook the soup and serve warm.</li></ol></div></div>`
	body := `<div class="menu"><h4>Ingredients</h4><ul><li><a href="/rice">Rice</a></li></ul></div><div class="layout">` + recipe + `</div>` +
		`<div class="related"><p>More soups</p><ul><li><a href="/other">Other soup</a></li></ul></div>`

	result := mustExtract(t, body, "https://example.test/soup")
	if result.Name != "Invented soup" || len(allIngredients(result.IngredientGroups)) != 3 || len(allSteps(result.Sections)) != 2 {
		t.Fatalf("bounded recipe = %+v", result)
	}
}

func TestSemanticLabelsInOneParagraphBoundItsSiblings(t *testing.T) {
	body := `<html><head><title>Madeleines</title></head><body><div class="detail"><p><strong>Madeleines</strong><br><br>Ingredients<br>130 g flour<br>2 eggs<br>155 g sugar<br>Equipment<br>A madeleine tray<br>Method</p>` +
		`<p>1. Sift the flour into a bowl.<br>2. Whisk the eggs with the sugar.<br><strong>Recipe by the bakery</strong></p></div></body></html>`

	result := mustExtract(t, body, "https://example.test/madeleines")
	if ingredients := allIngredients(result.IngredientGroups); len(ingredients) != 3 {
		t.Fatalf("ingredients = %#v", ingredients)
	}

	if steps := allSteps(result.Sections); len(steps) != 2 {
		t.Fatalf("steps = %+v", steps)
	}
}

func TestSemanticLabelsKeepTheRecipeInThePageLanguage(t *testing.T) {
	tab := func(ingredients, method string) string {
		return `<section><div><h3>` + ingredients + `</h3><hr><ul><li><p>Beer<span>250 ml</span></p></li><li><p>Flour<span>100 g</span></p></li><li><p>Sugar<span>1 tsp</span></p></li></ul><p>Share Recipe</p></div>` +
			`<div><h3>` + method + `</h3><hr><p><span>1.</span>Mix the flour, sugar, and beer.</p><p><span>2.</span>Fry the batter until golden.</p><hr><div><a href="/tag/batter">Batter</a></div></div></section>`
	}
	body := `<html lang="en"><head><title>Beer batter</title></head><body><div class="tabs"><div id="thai">` + tab("ส่วนประกอบ", "วิธีทำ") + `</div><div id="english">` + tab("Ingredients", "How to cook") + `</div></div></body></html>`

	result := mustExtract(t, body, "https://example.test/batter")
	if ingredients := allIngredients(result.IngredientGroups); len(ingredients) != 3 || ingredients[0] != "Beer 250 ml" {
		t.Fatalf("ingredients = %#v", ingredients)
	}

	if steps := allSteps(result.Sections); len(steps) != 2 || steps[0].Text != "Mix the flour, sugar, and beer." {
		t.Fatalf("steps = %+v", steps)
	}

	body = strings.Replace(body, `lang="en"`, `lang="fr"`, 1)
	if code := extractFailure(t, body, "https://example.test/batter"); code != NoRecipeStructuredData {
		t.Fatalf("two recipes in other languages = %s", code)
	}
}

func TestSemanticNestedArticlesDoNotConsumeRecipeCandidateCapacity(t *testing.T) {
	body := `<main><h1>Invented soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li><li>1 litre water</li><li>3 g salt</li></ul><h2>Instructions</h2><ol><li>Mix everything in the pan.</li><li>Cook the soup and serve warm.</li></ol>` + strings.Repeat(`<article><h2><a href="/other">Other story</a></h2></article>`, maximumSemanticCandidates+1) + `</main>`

	result := mustExtract(t, body, "https://example.test/soup")
	if result.ReviewText != "" || len(allIngredients(result.IngredientGroups)) != 3 {
		t.Fatalf("nested articles = %#v", result)
	}
}

func TestIncompleteStructuredRecipeDoesNotBorrowAnotherRecipe(t *testing.T) {
	body := `<script type="application/ld+json">{"@type":"Recipe","name":"First soup"}</script><main><h1>Second soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li><li>1 litre water</li><li>3 g salt</li></ul><h2>Instructions</h2><ol><li>Mix everything in the pan.</li><li>Cook the soup and serve warm.</li></ol></main>`
	if code := extractFailure(t, body, "https://example.test/soup"); code != RecipeIncomplete {
		t.Fatalf("unrelated body = %s", code)
	}

	body = `<script type="application/ld+json">{"@type":"Recipe","name":"First soup"}</script><main><article><h1>First soup</h1><h2>Ingredients</h2><p>2 carrots</p></article><article><h1>Second soup</h1><h2>Ingredients</h2><p>3 onions</p></article></main>`
	if code := extractFailure(t, body, "https://example.test/soup"); code != RecipeIncomplete {
		t.Fatalf("competing bodies = %s", code)
	}
}

func TestStructuredRecipeDoesNotDependOnArticleCandidates(t *testing.T) {
	body := htmlWith(`{"@type":"Recipe","name":"First soup","recipeIngredient":["1 carrot"]}`) + `<main>` + strings.Repeat(`<article><h1>Other soup</h1><h2>Ingredients</h2><p>2 onions</p></article>`, maximumSemanticCandidates+1) + `</main>`

	result := mustExtract(t, body, "https://example.test/soup")
	if ingredients := allIngredients(result.IngredientGroups); len(ingredients) != 1 || ingredients[0] != "1 carrot" || result.ReviewText != "" {
		t.Fatalf("structured recipe = %#v", result)
	}
}
