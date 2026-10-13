package webrecipe

import (
	"strings"
	"testing"
)

// These invented structural examples are available under CC0 1.0 Universal.
const semanticFixture = `<html lang="en"><head><link rel="canonical" href="https://example.test/broth">
<script type="application/ld+json">{"@type":"Article","headline":"Pantry broth",
"url":"https://example.test/broth","description":"An invented broth.","author":"Avery Cook",
"datePublished":"2025-02-03","image":{"@type":"ImageObject","url":"/broth.jpg","creditText":"Avery Cook"},
"recipeYield":"99 portions","totalTime":"PT99H","nutrition":{"calories":"999"}}</script></head>
<body><nav><p>Ingredients</p><ul><li>20 unrelated links</li></ul></nav><main><article>
<header><h1>Pantry broth</h1></header><p>A story mentions four hours and ten servings.</p>
<div><p><span><strong>Ingredients:</strong></span></p></div>
<p><strong>Base</strong></p><ul><li>2–3 carrots</li><li>1 1/2 cups water</li><li>Salt to taste</li></ul>
<p><strong>Finish</strong></p><ul><li>2 tsp oil</li></ul>
<div><p><strong>Instructions:</strong></p></div>
<ol><li><p>Combine the base in a pan.</p><p>Keep all of this continuation.</p>
<ul><li>Use a small pan.</li><li>Keep the lid close.</li></ul></li>
<li><p>Stir in the oil and serve with <a href="/bread?token=private#recipe">bread</a>.</p></li></ol>
<hr><p>Related recipes:</p><ul><li><a href="/other">Other soup</a></li></ul>
</article></main><footer><p>Instructions</p><ol><li>Browse our other recipes.</li></ol></footer></body></html>`

func TestSemanticArticleKeepsDOMGroupsAndStepOwnership(t *testing.T) {
	result := mustExtract(t, semanticFixture, "https://example.test/broth")
	if result.ExtractionMethod != "semantic-html" || result.MetadataSource != "article-json-ld" || result.Name != "Pantry broth" {
		t.Fatalf("reading provenance = %+v", result)
	}

	if len(result.IngredientGroups) != 2 || result.IngredientGroups[0].Title != "Base" || result.IngredientGroups[1].Title != "Finish" || len(allIngredients(result.IngredientGroups)) != 4 {
		t.Fatalf("groups = %+v", result.IngredientGroups)
	}

	steps := allSteps(result.Sections)
	if len(steps) != 2 || !strings.Contains(steps[0].Text, "\n\nKeep all of this continuation.") || !strings.Contains(steps[0].Text, "- Use a small pan.") {
		t.Fatalf("step boundaries = %+v", steps)
	}

	if !strings.Contains(steps[1].Text, "bread (https://example.test/bread)") || strings.Contains(result.ReviewText, "private") {
		t.Fatalf("step link = %q", steps[1].Text)
	}

	if result.Authors[0].Name != "Avery Cook" || result.Published != "2025-02-03" || result.Description != "An invented broth." || result.Images[0] != "https://example.test/broth.jpg" {
		t.Fatalf("metadata = %+v", result)
	}

	if len(result.Yields) != 0 || result.Nutrition != nil || result.Times.Total != "" || result.ImageProvenance[result.Images[0]].Credit != "Avery Cook" {
		t.Fatalf("unsupported metadata or missing image provenance = %+v", result)
	}
}

func TestSemanticArticleDoesNotReplaceStructuredRecipes(t *testing.T) {
	for _, structured := range []string{
		`<script type="application/ld+json">{"@type":"Recipe","name":"Structured winner","recipeIngredient":["water"],"recipeInstructions":["Stir."]}</script>`,
		`<div itemscope itemtype="https://schema.org/Recipe"><span itemprop="name">Structured winner</span><span itemprop="recipeIngredient">water</span><span itemprop="recipeInstructions">Stir.</span></div>`,
	} {
		body := strings.Replace(semanticFixture, "</article>", structured+"</article>", 1)

		result := mustExtract(t, body, "https://example.test/broth")
		if result.Name != "Structured winner" || result.ExtractionMethod != "" || len(allIngredients(result.IngredientGroups)) != 1 {
			t.Fatalf("structured precedence = %+v", result)
		}
	}
}

func TestSemanticArticleWeakAndCompetingCandidatesStayDrafts(t *testing.T) {
	fixtures := map[string]string{
		"missing method":      `<main><article><h1>Draft soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li><li>1 cup water</li><li>3 tsp oil</li></ul></article></main>`,
		"few ingredients":     `<main><article><h1>Draft soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li></ul><h2>Instructions</h2><ol><li>Mix the carrots well.</li><li>Serve the carrots warm.</li></ol></article></main>`,
		"metadata title only": strings.Replace(semanticFixture, `<h1>Pantry broth</h1>`, "", 1),
		"competing articles":  semanticFixture + semanticFixture,
		"split regions":       `<main><article><h1>One soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li><li>1 cup water</li><li>3 tsp oil</li></ul></article><article><h1>Another soup</h1><h2>Instructions</h2><ol><li>Mix the base well.</li><li>Serve the base warm.</li></ol></article></main>`,
		"repeated pairs":      strings.Replace(semanticFixture, "<hr>", `<h2>Ingredients</h2><ul><li>3 onions</li></ul><h2>Instructions</h2><ol><li>Mix another dish well.</li></ol><hr>`, 1),
		"unnumbered prose":    strings.Replace(semanticFixture, "<ol>", "<p>Consider the story of this dish.</p><ol>", 1),
		"plain ingredients":   `<main><article><h1>Unquantified soup</h1><h2>Ingredients</h2><ul><li>Water</li><li>Carrots</li><li>Salt</li></ul><h2>Instructions</h2><ol><li>Combine all ingredients well.</li><li>Serve the soup warm.</li></ol></article></main>`,
	}

	for name, body := range fixtures {
		t.Run(name, func(t *testing.T) {
			result := mustExtract(t, body, "https://example.test/broth")
			if result.ReviewText == "" || len(result.IngredientGroups)+len(result.Sections) != 0 || !hasWarning(result, "semantic-html-review") {
				t.Fatalf("uncertain recipe was assigned fields = %+v", result)
			}
		})
	}
}

func TestSemanticArticleExcludesNavigationHiddenAndRelatedContent(t *testing.T) {
	content := `<h1>Invented soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li><li>1 cup water</li><li>3 tsp oil</li></ul><h2>Instructions</h2><ol><li>Mix the base well.</li><li>Serve the base warm.</li></ol>`
	for _, container := range []string{"nav", "aside", "footer", `div hidden`, `div aria-hidden="true"`, `div style="display: none"`} {
		body := "<main><" + container + "><article>" + content + "</article></" + strings.Fields(container)[0] + "></main>"
		if got := extractFailure(t, body, "https://example.test/soup"); got != NoRecipeStructuredData {
			t.Fatalf("excluded %s: %s", container, got)
		}
	}

	body := `<main><article>` + content + `<h2>Related recipes</h2><ol><li>Read about another soup.</li><li>Visit our other recipes.</li></ol></article></main>`
	if result := mustExtract(t, body, "https://example.test/soup"); len(allSteps(result.Sections)) != 2 {
		t.Fatalf("related content included = %+v", result)
	}
}

func TestSemanticArticleNestedIngredientLabelsAndLocalizedMarkers(t *testing.T) {
	body := `<main><article><h1>Invented soup</h1><section><h2>Zutaten</h2><h3>Flüssigkeit</h3><ul><li>Basis<ul><li>2 Karotten</li><li>1 Liter Wasser</li><li>3 g Salz</li></ul></li></ul></section>
<section><h2>Zubereitung</h2><ol><li>Alle Zutaten gut vermischen.</li><li>Die Suppe warm servieren.</li></ol></section></article></main>`

	result := mustExtract(t, body, "https://example.test/soup")
	if len(result.IngredientGroups) != 1 || result.IngredientGroups[0].Title != "Flüssigkeit · Basis" || len(allSteps(result.Sections)) != 2 || result.Language != "" {
		t.Fatalf("nested/localized recipe = %+v", result)
	}
}

func TestSemanticArticleDoesNotBorrowMethodOutsideSection(t *testing.T) {
	body := `<main><article><h1>Invented soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li><li>1 cup water</li><li>3 tsp oil</li></ul><section><p>Instructions</p></section><ol><li>Read another story first.</li><li>Visit another recipe next.</li></ol></article></main>`

	result := mustExtract(t, body, "https://example.test/soup")
	if len(result.Sections) != 0 || !hasWarning(result, "semantic-html-review") {
		t.Fatalf("method borrowed across section = %+v", result)
	}
}

func TestSemanticArticleBoundsCandidateRegions(t *testing.T) {
	body := "<main>" + strings.Repeat("<article><h1>Story</h1></article>", maximumSemanticRoots+1) + "</main>"
	if got := extractFailure(t, body, "https://example.test/soup"); got != ExtractionLimited {
		t.Fatalf("candidate budget = %s", got)
	}
}
