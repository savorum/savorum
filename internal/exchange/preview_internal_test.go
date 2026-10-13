package exchange

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/importer/webrecipe"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

// A review stand-in for an attached photograph must share its name, not add a
// second asset whose path differs only in case.
func TestAPendingStandInIsNotASecondCopyOfAnAttachedPhotograph(t *testing.T) {
	t.Parallel()

	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("bytes")))
	embedded := importer.EmbeddedAsset{
		ID: "1FE77C1B.JPG", Path: "1FE77C1B.JPG", MediaType: "image/jpeg",
		SHA256: digest, Bytes: []byte("bytes"),
	}

	document := recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            "paprika.0011223344556677",
		Title:         "Approximate Orchard Compote",
		IngredientSections: []recipe.IngredientSection{
			{ID: "i1", Ingredients: []recipe.Ingredient{{ID: "i1-1", Name: "apples", SourceText: "apples"}}},
		},
		InstructionSections: []recipe.InstructionSection{
			{ID: "m1", Steps: []recipe.Step{{ID: "m1-1", Text: "Simmer the fruit."}}},
		},
		Assets: []recipe.Asset{{
			ID: importer.EmbeddedAssetID(embedded), Path: "assets/" + importer.EmbeddedAssetID(embedded),
			MediaType: embedded.MediaType, SHA256: embedded.SHA256, Size: int64(len(embedded.Bytes)),
			Role: recipe.AssetRoleImage,
		}},
	}
	document.ImageRefs = []string{document.Assets[0].ID}
	document.PrimaryImage = document.Assets[0].ID

	stood := withPendingAssets(document, ImportItem{EmbeddedAssets: []importer.EmbeddedAsset{embedded}})
	if len(stood.Assets) != 1 {
		t.Fatalf("assets = %#v", stood.Assets)
	}

	if err := recipe.ValidateForWrite(stood, units.DefaultCatalog()); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeFailureMessageCapitalizesUnicode(t *testing.T) {
	t.Parallel()

	got := (&Service{}).decodeFailureMessage(&webrecipe.Failure{Message: "  échec de lecture  "})
	if got != "Échec de lecture" {
		t.Fatalf("decodeFailureMessage = %q", got)
	}
}
