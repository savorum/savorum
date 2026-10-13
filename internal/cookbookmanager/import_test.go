package cookbookmanager_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/cookbookmanager"
)

const exported = `name: Test Rolls
description: Soft rolls.
servings: 12 rolls
source: https://example.com/rolls
prep_time: PT20M
cook_time: PT15M
favorite: yes
cook_count: 37
tags: [Bread, Baking]
ingredients:
  - 1 cup flour
  - 1 cup flour
directions:
  - Knead for 5 minutes.
  - Let rest for 30 minutes.
exportedBy: Shared from CookBook
`

func TestDecodePreservesMetadataAndAllowsRepeatedIngredientLines(t *testing.T) {
	document, err := cookbookmanager.Decode([]byte(exported), "Test Rolls_12345.yml")
	if err != nil {
		t.Fatal(err)
	}

	if document.ID != "cookbook.12345" || document.Source.Application != "CookbookManager.com" || document.FirstYield().Text != "12 rolls" {
		t.Fatalf("metadata = %#v", document)
	}

	if document.Language != "" || document.SourceLocale != "" {
		t.Fatalf("CookbookManager export must not invent language or locale: %#v", document)
	}

	ingredients := document.IngredientSections[0].Ingredients
	if len(ingredients) != 2 || ingredients[0].ID == ingredients[1].ID {
		t.Fatalf("ingredients = %#v", ingredients)
	}

	for _, ingredient := range ingredients {
		if ingredient.Quantity != nil || ingredient.SourceText != "1 cup flour" {
			t.Fatalf("a locale-free cup must remain opaque: %#v", ingredient)
		}
	}

	if len(document.Times.Assertions) != 2 {
		t.Fatalf("derived timings = %#v", document.Times.Assertions)
	}

	extension, ok := document.Extensions["x-cookbookmanager"].(map[string]any)
	if !ok {
		t.Fatalf("CookbookManager extension = %#v, want object", document.Extensions["x-cookbookmanager"])
	}

	if extension["favorite"] != nil || extension["cook_count"] != nil {
		t.Fatalf("library metadata leaked into extension: %#v", extension)
	}

	favorite, cookCount, err := cookbookmanager.LibraryMetadata([]byte(exported), "Test Rolls_12345.yml")
	if err != nil || !favorite || cookCount != 37 {
		t.Fatalf("library metadata = %v, %d, %v", favorite, cookCount, err)
	}
}

func TestDecodeStructuresOnlyCookbookManagerUnitsWithEnoughEvidence(t *testing.T) {
	data := []byte(`name: Ingredient evidence
ingredients:
  - 67g granulated sugar
  - 1 1/2 teaspoons vanilla
  - 2 cups flour
  - 2 (14.5 oz) cans diced tomatoes, drained
directions:
  - Mix.
`)

	document, err := cookbookmanager.Decode(data, "Ingredient evidence_9.yml")
	if err != nil {
		t.Fatal(err)
	}

	ingredients := document.IngredientSections[0].Ingredients
	if len(ingredients) != 4 {
		t.Fatalf("ingredients = %#v", ingredients)
	}

	if ingredients[0].Quantity == nil || ingredients[0].Quantity.Unit != "mass.gram" || ingredients[0].SourceText != "67g granulated sugar" {
		t.Errorf("grams = %#v", ingredients[0])
	}

	// A CookbookManager export states no locale, so its teaspoon stays as
	// unresolved as its cup.
	for _, index := range []int{1, 2} {
		if ingredients[index].Quantity != nil || ingredients[index].Normalization == nil ||
			ingredients[index].Normalization.Method != "unresolved" {
			t.Errorf("region-bound measure %d = %#v", index, ingredients[index])
		}
	}

	if ingredients[3].Quantity == nil || ingredients[3].Quantity.Unit != "container.can" || ingredients[3].PackageSize == nil {
		t.Errorf("package = %#v", ingredients[3])
	}
}

func TestArchiveResourcePolicyRejectsBeforeUnboundedDecode(t *testing.T) {
	t.Run("recipe count", func(t *testing.T) {
		data := zipMembers(t, 501, func(int) []byte { return []byte(exported) })
		if _, err := cookbookmanager.DecodeArchive(data); err == nil || !strings.Contains(err.Error(), "more than 500 recipes") {
			t.Fatalf("recipe count = %v", err)
		}
	})

	t.Run("central directory", func(t *testing.T) {
		data := zipNamedMembers(t, 1_001, func(index int) string { return fmt.Sprintf("ignored-%04d.txt", index) }, func(int) []byte { return nil })
		if _, err := cookbookmanager.DecodeArchive(data); err == nil || !strings.Contains(err.Error(), "directory exceeds") {
			t.Fatalf("directory count = %v", err)
		}
	})

	t.Run("compression ratio", func(t *testing.T) {
		data := zipMembers(t, 1, func(int) []byte { return bytes.Repeat([]byte("x"), 1<<20) })
		if _, err := cookbookmanager.DecodeArchive(data); err == nil || !strings.Contains(err.Error(), "expands beyond") {
			t.Fatalf("compression ratio = %v", err)
		}
	})

	// Seventeen members, each one individually legal: at the per-member size
	// limit and nowhere near the per-member compression ratio. Only their
	// total is over the policy, which is the check this exercises.
	//
	// Half of each member is incompressible and half is a repeated phrase, so
	// the archive declares 68 MiB of content from a few tens of megabytes of
	// input; incompressible members would hit the input limit first.
	t.Run("aggregate expansion", func(t *testing.T) {
		noise := make([]byte, 2<<20)
		if _, err := rand.NewChaCha8([32]byte{15, 9, 20, 26}).Read(noise); err != nil {
			t.Fatal(err)
		}

		member := make([]byte, 0, maximumMemberBytes)
		member = append(member, noise...)
		member = append(member, bytes.Repeat([]byte("cookbookmanager recipe padding  "), (2<<20)/32)...)

		if len(member) != maximumMemberBytes {
			t.Fatalf("member is %d bytes, want the per-member limit %d", len(member), maximumMemberBytes)
		}

		data := zipMembers(t, 17, func(int) []byte { return member })
		if len(data) > maximumInputBytes {
			t.Fatalf("fixture archive is %d bytes, which the input policy refuses before the aggregate check", len(data))
		}

		if _, err := cookbookmanager.DecodeArchive(data); err == nil || !strings.Contains(err.Error(), "ZIP expands beyond") {
			t.Fatalf("aggregate expansion = %v", err)
		}
	})
}

func TestArchiveDecodeObservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	data := zipMembers(t, 1, func(int) []byte { return []byte(exported) })
	if _, err := cookbookmanager.DecodeArchiveImportsContext(ctx, data); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled decode = %v", err)
	}
}

func TestArchiveRejectsUnsafeAndAmbiguousRecipePaths(t *testing.T) {
	for _, names := range [][]string{
		{"../recipe.yml"},
		{"folder/../recipe.yml"},
		{`C:\recipe.yml`},
		{"Recipe.yml", "recipe.yml"},
		{"caf\u00e9.yml", "cafe\u0301.yml"},
		{"recipe.yml", "./recipe.yml"},
	} {
		archive := zipNamedMembers(t, len(names), func(index int) string { return names[index] }, func(int) []byte { return []byte(exported) })
		if _, err := cookbookmanager.DecodeArchive(archive); err == nil {
			t.Fatalf("accepted unsafe or ambiguous archive names %q", names)
		}
	}
}

// The import policy's limits, restated so a fixture can say which one it is
// aiming at. They are deliberately equal, so an archive that does not compress
// cannot expand past the aggregate limit without failing the input limit first.
const (
	maximumInputBytes  = 64 << 20
	maximumMemberBytes = 4 << 20
)

func zipMembers(t *testing.T, count int, body func(int) []byte) []byte {
	t.Helper()
	return zipNamedMembers(t, count, func(index int) string { return fmt.Sprintf("recipe_%04d.yml", index) }, body)
}

func zipNamedMembers(t *testing.T, count int, name func(int) string, body func(int) []byte) []byte {
	t.Helper()

	var data bytes.Buffer

	writer := zip.NewWriter(&data)
	for index := range count {
		entry, err := writer.Create(name(index))
		if err != nil {
			t.Fatal(err)
		}

		if _, err := entry.Write(body(index)); err != nil {
			t.Fatal(err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return data.Bytes()
}

func TestDecodeArchiveReadsEveryYAMLMember(t *testing.T) {
	var body bytes.Buffer

	w := zip.NewWriter(&body)
	for _, name := range []string{"one_1.yml", "two_2.yaml"} {
		member, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := member.Write([]byte(exported)); err != nil {
			t.Fatal(err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	documents, err := cookbookmanager.DecodeArchive(body.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if len(documents) != 2 {
		t.Fatalf("documents = %d", len(documents))
	}
}
