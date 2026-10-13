package importer

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/sref/bundle"
)

func fuzzArchive(files ...nativeZIPMember) []byte {
	var output bytes.Buffer

	archive := zip.NewWriter(&output)
	for _, file := range files {
		member, err := archive.Create(file.name)
		if err != nil {
			panic(err)
		}

		if _, err := member.Write(file.data); err != nil {
			panic(err)
		}
	}

	if err := archive.Close(); err != nil {
		panic(err)
	}

	return output.Bytes()
}

func fuzzGzip(data []byte) []byte {
	var output bytes.Buffer

	compressed := gzip.NewWriter(&output)
	if _, err := compressed.Write(data); err != nil {
		panic(err)
	}

	if err := compressed.Close(); err != nil {
		panic(err)
	}

	return output.Bytes()
}

func FuzzImportBytes(f *testing.F) {
	document := recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            "fuzz-recipe", Title: "Fuzz recipe",
		IngredientSections: []recipe.IngredientSection{{ID: "ingredients", Ingredients: []recipe.Ingredient{{ID: "salt", Name: "salt"}}}},
	}

	encoded, err := sref.Encode(document)
	if err != nil {
		f.Fatal(err)
	}

	packaged, err := sref.WritePackage(document, nil)
	if err != nil {
		f.Fatal(err)
	}

	var bundleBytes bytes.Buffer

	writer := bundle.NewWriter(&bundleBytes)
	if err := writer.Add(string(document.ID), func(destination io.Writer) error {
		_, err := destination.Write(packaged)
		if err != nil {
			return fmt.Errorf("write fuzz package: %w", err)
		}

		return nil
	}); err != nil {
		f.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		f.Fatal(err)
	}

	mealie, err := os.ReadFile("testdata/mealie/sections.json")
	if err != nil {
		f.Fatal(err)
	}

	tandoor, err := os.ReadFile("testdata/tandoor/sections.json")
	if err != nil {
		f.Fatal(err)
	}

	paprika, err := os.ReadFile("testdata/paprika/lines.json")
	if err != nil {
		f.Fatal(err)
	}

	seeds := []struct {
		kind uint8
		data []byte
	}{
		{0, encoded},
		{1, packaged},
		{2, bundleBytes.Bytes()},
		{3, []byte("Soup\nIngredients\n1 onion\nInstructions\nCook onion")},
		{4, []byte(`<script type="application/ld+json">{"@context":"https://schema.org","@type":"Recipe","name":"Soup","recipeIngredient":["1 onion"]}</script>`)},
		{5, []byte("name: Soup\ningredients: onion\ndirections: Cook onion\n")},
		{6, fuzzArchive(nativeZIPMember{name: "soup.yaml", data: []byte("name: Soup\ningredients: onion\ndirections: Cook onion\n")})},
		{7, fuzzGzip(paprika)},
		{8, fuzzArchive(nativeZIPMember{name: "soup.paprikarecipe", data: fuzzGzip(paprika)})},
		{9, fuzzArchive(nativeZIPMember{name: "soup.json", data: mealie})},
		{10, fuzzArchive(nativeZIPMember{name: "soup.zip", data: fuzzArchive(nativeZIPMember{name: "recipe.json", data: tandoor})})},
	}
	for _, seed := range seeds {
		f.Add(seed.kind, seed.data)
	}

	f.Add(uint8(10), []byte("PK\x03\x04"))
	f.Add(uint8(3), []byte{0xff, 0xfe, 0x00})

	f.Fuzz(func(t *testing.T, kind uint8, data []byte) {
		if len(data) > 64<<10 {
			return
		}

		var (
			adapter Adapter
			name    string
		)

		switch kind % 11 {
		case 0:
			adapter, name = newSREFAdapter(), "recipe.json"
		case 1:
			adapter, name = newSREFAdapter(), "recipe.sref"
		case 2:
			adapter, name = newSREFAdapter(), "recipes.srefbundle"
		case 3:
			adapter, name = plainTextAdapter{}, "recipe.txt"
		case 4:
			adapter, name = webRecipeAdapter{}, "recipe.html"
		case 5:
			adapter, name = cookbookManagerAdapter{}, "recipe.yaml"
		case 6:
			adapter, name = cookbookManagerAdapter{}, "recipes.zip"
		case 7:
			adapter, name = paprikaAdapter{}, "recipe.paprikarecipe"
		case 8:
			adapter, name = paprikaAdapter{}, "recipes.paprikarecipes"
		case 9:
			adapter, name = mealieAdapter{}, "recipes.zip"
		default:
			adapter, name = tandoorAdapter{}, "recipes.zip"
		}

		input := Input{Name: name, Data: data}
		_ = adapter.Probe(context.Background(), input)

		batch, err := adapter.Import(context.Background(), input, Options{})
		if err != nil {
			return
		}

		if len(batch.Recipes) > 1000 {
			t.Fatalf("accepted %s input with invalid batch shape", name)
		}

		for _, item := range batch.Recipes {
			for _, asset := range item.EmbeddedAssets {
				if asset.Path != "" && !filepath.IsLocal(asset.Path) {
					t.Fatalf("accepted asset path outside import: %q", asset.Path)
				}
			}
		}
	})
}

func FuzzImageHeader(f *testing.F) {
	valid, err := os.ReadFile("testdata/document-scan.png")
	if err != nil {
		f.Fatal(err)
	}

	f.Add(valid)
	f.Add([]byte("\x89PNG\r\n\x1a\n"))
	f.Add([]byte("\xff\xd8\xff\xd9"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			return
		}

		_ = validateDocumentImage(data)
	})
}
