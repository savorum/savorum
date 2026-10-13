package exchange_test

import (
	"context"
	"testing"

	"github.com/savorum/savorum/internal/exchange"
)

// This invented article is available under CC0 1.0 Universal.
func TestSemanticArticleDraftRetainsAttributionAfterTextReview(t *testing.T) {
	body := `<html lang="en"><head><script type="application/ld+json">{"@type":"Article","headline":"Invented broth",
"url":"https://example.test/broth","author":"Avery Cook","description":"An invented article.",
"datePublished":"2025-02-03T09:10:11-0700"}</script></head><body><main><article><h1>Invented broth</h1>
<p>Ingredients:</p><ul><li>2 carrots</li><li>1 cup water</li><li>3 g salt</li></ul></article></main></body></html>`
	service := exchange.NewService(&importLibrary{}, &sourceAssets{})

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{Filename: "broth.html", Data: []byte(body)}})
	if err != nil {
		t.Fatal(err)
	}

	if len(preview.Items) != 1 || preview.Items[0].Source.ExtractedText == "" || len(preview.Items[0].Recipe.IngredientSections) != 0 {
		t.Fatalf("draft preview = %+v", preview)
	}

	updated, err := service.EditPreviewText(context.Background(), preview.ID, preview.Items[0].ID,
		"Reviewed broth\nIngredients\n2 carrots\n1 cup water\n3 g salt\nInstructions\nCombine the ingredients.\nServe the broth warm.")
	if err != nil {
		t.Fatal(err)
	}

	doc := updated.Items[0].Recipe
	if doc.Title != "Reviewed broth" || len(doc.IngredientSections) != 1 || len(doc.InstructionSections) != 1 || len(doc.InstructionSections[0].Steps) != 2 {
		t.Fatalf("reviewed sections = %+v", doc)
	}

	if doc.Source.URL != "https://example.test/broth" || len(doc.Authors) != 1 || doc.Authors[0].Name != "Avery Cook" || doc.Description != "An invented article." || doc.Published != "2025-02-03T09:10:11-07:00" {
		t.Fatalf("article attribution lost = %+v", doc)
	}
}
