package importer

import (
	"context"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/importer/webrecipe"
)

// These invented structural examples are available under CC0 1.0 Universal.
func TestSemanticWebRecipeMapsSourceGroupsAndAuthoredAmounts(t *testing.T) {
	body := `<html><head><script type="application/ld+json">{"@type":"Article","headline":"Invented soup",
"url":"https://example.test/soup","author":"Avery Cook","datePublished":"2025-02-03T09:10:11-0700"}</script></head>
<body><main><article><h1>Invented soup</h1><h2>Ingredients</h2><h3>Base</h3><ul>
<li>2–3 carrots</li><li>1 1/2 cups water</li><li>Salt to taste</li></ul><h3>Finish</h3><ul><li>2 tsp oil</li></ul>
<h2>Instructions</h2><ol><li><p>Combine all the ingredients.</p><p>Keep this paragraph in the same step.</p></li>
<li>Serve the soup with <a href="/bread">bread</a>.</li></ol></article></main></body></html>`

	batch, err := DecodeWebRecipe([]byte(body), "text/html", "https://example.test/soup")
	if err != nil || len(batch.Recipes) != 1 {
		t.Fatalf("decode = %+v %v", batch, err)
	}

	item := batch.Recipes[0]

	doc := item.Document
	if doc.Source.URL != "https://example.test/soup" || doc.Authors[0].Name != "Avery Cook" || doc.Published != "2025-02-03T09:10:11-07:00" {
		t.Fatalf("attribution = %+v", doc)
	}

	if len(doc.IngredientSections) != 2 || doc.IngredientSections[0].Title != "Base" || doc.IngredientSections[0].Ingredients[0].SourceText != "2–3 carrots" || doc.IngredientSections[0].Ingredients[2].SourceText != "Salt to taste" {
		t.Fatalf("ingredients = %+v", doc.IngredientSections)
	}

	steps := doc.InstructionSections[0].Steps
	if len(steps) != 2 || !strings.Contains(steps[0].Text, "\n\nKeep this paragraph") || !strings.Contains(steps[1].Text, "https://example.test/bread") {
		t.Fatalf("steps = %+v", steps)
	}

	if item.ImporterMetadata["extraction_method"] != "semantic-html" || item.ImporterMetadata["metadata_source"] != "article-json-ld" {
		t.Fatalf("reading provenance = %+v", item.ImporterMetadata)
	}
}

func TestSemanticWebDraftUsesEditableReviewWithoutGuessedSections(t *testing.T) {
	body := `<main><article><h1>Invented soup</h1><p>Ingredients:</p><ul><li>2 carrots</li><li>1 cup water</li><li>3 tsp oil</li></ul></article></main>`

	batch, err := DecodeWebRecipe([]byte(body), "text/html", "https://example.test/soup")
	if err != nil || len(batch.Recipes) != 1 {
		t.Fatalf("decode = %+v %v", batch, err)
	}

	item := batch.Recipes[0]
	if len(item.Document.IngredientSections)+len(item.Document.InstructionSections) != 0 || len(item.Document.Notes) != 1 || item.ImporterMetadata["source_text"] != item.Document.Notes[0] {
		t.Fatalf("draft = %+v", item)
	}

	found := false

	for _, diagnostic := range item.Diagnostics {
		if diagnostic.Code == "semantic-html-review" {
			found = diagnostic.Class == DiagnosticActionable
		}
	}

	if !found {
		t.Fatalf("actionable review diagnostic missing = %+v", item.Diagnostics)
	}
}

func TestSocialRecipeLinksSkipSemanticDrafts(t *testing.T) {
	page := webrecipe.Load([]byte(`<meta property="og:description" content="https://example.test/draft https://example.test/recipe">`), "text/html", "https://example.test/post")
	fetch := func(_ context.Context, address string) (*webrecipe.Page, error) {
		body := `<main><article><h1>Unfinished soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li><li>1 cup water</li><li>3 g salt</li></ul></article></main>`
		if address == "https://example.test/recipe" {
			body = `<script type="application/ld+json">{"@type":"Recipe","name":"Authored soup","recipeIngredient":["2 carrots"],"recipeInstructions":["Combine and serve."]}</script>`
		}

		return webrecipe.Load([]byte(body), "text/html", address), nil
	}

	batch, err := fetchSharedRecipe(context.Background(), page, fetch)
	if err != nil || len(batch.Recipes) != 1 {
		t.Fatalf("linked import = %+v %v", batch, err)
	}

	if batch.Recipes[0].Document.Title != "Authored soup" || batch.Recipes[0].Document.Source.URL != "https://example.test/recipe" {
		t.Fatalf("weak draft replaced linked recipe = %+v", batch.Recipes[0])
	}
}
