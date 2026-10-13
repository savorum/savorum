package exchange_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/sref/archivepolicy"
)

func TestNativeFileSelectionSharesWorkingBudgetBeforePersistence(t *testing.T) {
	var encoded bytes.Buffer

	writer := zip.NewWriter(&encoded)

	entry, err := writer.Create("recipe.json")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := entry.Write([]byte(`{"name":"Soup","recipe_ingredient":[],"recipe_instructions":[]}`)); err != nil {
		t.Fatal(err)
	}

	image := append([]byte{0xff, 0xd8, 0xff}, bytes.Repeat([]byte("x"), 7<<20)...)

	for index := range 8 {
		entry, err := writer.Create(fmt.Sprintf("image-%d.jpg", index))
		if err != nil {
			t.Fatal(err)
		}

		if _, err := entry.Write(image); err != nil {
			t.Fatal(err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	data := encoded.Bytes()

	for _, preview := range []bool{false, true} {
		t.Run(fmt.Sprintf("preview=%t", preview), func(t *testing.T) {
			recipes, blobs := &importLibrary{}, &countingAssets{}
			service := exchange.NewService(recipes, blobs)

			var err error
			if preview {
				_, err = service.PreviewFiles(t.Context(), []exchange.UploadedImport{{Filename: "first.zip", Data: data}, {Filename: "second.zip", Data: data}})
				if !errors.Is(err, exchange.ErrPreviewTooLarge) {
					t.Fatalf("preview limit: %v", err)
				}
			} else {
				_, err = service.ImportSources(t.Context(), []importer.Input{{Name: "first.zip", Data: data}, {Name: "second.zip", Data: data}}, importer.Options{}, library.DuplicateSkip)
				if !errors.Is(err, archivepolicy.ErrLimit) {
					t.Fatalf("import limit: %v", err)
				}
			}

			if len(recipes.imported) != 0 || blobs.ingests != 0 {
				t.Fatal("over-budget selection persisted data")
			}
		})
	}
}
