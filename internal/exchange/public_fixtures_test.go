package exchange_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
)

func TestPublicExchangeFixtures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		file, format string
		assert       func(*testing.T, recipe.Document)
	}{
		{"authored-timings.recipe.json", "sref", assertPublicAuthoredTimings},
		{"language-without-yield.recipe.json", "sref", assertPublicLanguageWithoutYield},
		{"nested-steps.recipe.jsonld", "web-recipe", assertPublicNestedSteps},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatal(err)
			}

			ctx := context.Background()

			store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "library.db"))
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = store.Close() })

			recipes, err := library.NewService(store, units.DefaultCatalog())
			if err != nil {
				t.Fatal(err)
			}

			service := exchange.NewService(recipes, corpusNoAssets{})
			inputs := []importer.Input{{Name: tc.file, Data: data}}

			formats, err := service.ProbeSources(ctx, inputs)
			if err != nil || len(formats) != 1 || formats[0].ID != tc.format {
				t.Fatalf("formats = %#v, error = %v", formats, err)
			}

			results, err := service.ImportSources(ctx, inputs, importer.Options{}, library.DuplicateReject)
			if err != nil || len(results) != 1 {
				t.Fatalf("results = %#v, error = %v", results, err)
			}

			entry, err := recipes.Get(ctx, results[0].EntryID)
			if err != nil {
				t.Fatal(err)
			}

			tc.assert(t, entry.Recipe)

			encoded, err := sref.Encode(entry.Recipe)
			if err != nil {
				t.Fatal(err)
			}

			if tc.format == "sref" {
				authored, err := sref.Decode(data)
				if err != nil {
					t.Fatal(err)
				}

				want, err := sref.Encode(authored)
				if err != nil {
					t.Fatal(err)
				}

				if !bytes.Equal(want, encoded) {
					t.Fatalf("stored SREF differs from authored fixture:\ngot %s\nwant %s", encoded, want)
				}
			}

			returned, err := sref.Decode(encoded)
			if err != nil {
				t.Fatal(err)
			}

			tc.assert(t, returned)
		})
	}
}

func assertPublicAuthoredTimings(t *testing.T, doc recipe.Document) {
	t.Helper()

	if doc.Title != "Slow oat pudding" || doc.Language != "en" || doc.SourceLocale != "" ||
		doc.FirstYield() == nil || doc.FirstYield().Text != "2 bowls" || doc.Description != "A small timing preservation fixture." ||
		doc.Source == nil || doc.Source.URL != "https://example.com/slow-oats" ||
		!reflect.DeepEqual(doc.Notes, []string{"Serve in small bowls."}) {
		t.Fatalf("metadata = %#v", doc)
	}

	if doc.Times == nil || len(doc.Times.Assertions) != 3 ||
		len(doc.InstructionSections) != 1 || len(doc.InstructionSections[0].Steps) != 2 {
		t.Fatalf("timing structure = %#v", doc)
	}

	assertAuthoredTimings(t, doc.Title, doc,
		authoredTiming{"thirty minutes", "PT30M"}, authoredTiming{"one and one-half hours", "PT1H30M"})
	assertNoAuthoredTiming(t, doc.Title, doc, "half hours")
	assertAuthoredRange(t, doc.Title, doc, "50–60 minutes", "PT50M", "PT60M")
	steps := doc.InstructionSections[0].Steps
	assertStepTimingRefs(t, doc.Title, steps[0], "soak", "rest")
	assertStepTimingRefs(t, doc.Title, steps[1], "bake")

	if len(steps[1].Temperatures) != 4 {
		t.Fatalf("temperatures = %#v", steps[1].Temperatures)
	}

	for i, want := range []struct{ value, unit string }{
		{"350", "temperature.fahrenheit"},
		{"175", "temperature.celsius"},
		{"200", "temperature.fahrenheit"},
		{"95", "temperature.celsius"},
	} {
		got := steps[1].Temperatures[i]
		if got.Amount.Value == nil || got.Amount.Value.String() != want.value || got.Unit != want.unit {
			t.Fatalf("temperature %d = %#v", i, got)
		}
	}
}

func assertPublicLanguageWithoutYield(t *testing.T, doc recipe.Document) {
	t.Helper()

	if doc.Language != "de" || doc.SourceLocale != "" || doc.FirstYield() != nil ||
		doc.Times == nil || len(doc.Times.Assertions) != 1 || !doc.Times.Assertions[0].Duration.Approximate ||
		len(doc.InstructionSections) != 1 || len(doc.InstructionSections[0].Steps) != 1 {
		t.Fatalf("language, optional yield or timing = %#v", doc)
	}

	assertAuthoredRange(t, doc.Title, doc, "ca. 4-6 Stunden", "PT4H", "PT6H")
	assertStepTimingRefs(t, doc.Title, doc.InstructionSections[0].Steps[0], "rest")
}

func assertPublicNestedSteps(t *testing.T, doc recipe.Document) {
	t.Helper()

	if doc.Title != "Mint oat bowl" || doc.Language != "en-GB" || doc.SourceLocale != "en-GB" ||
		doc.FirstYield() == nil || doc.FirstYield().Text != "2 bowls" || doc.Source == nil || doc.Source.URL != "https://example.com/oat-bowl" ||
		len(doc.Authors) != 1 || doc.Authors[0].Name != "Fixture Kitchen" || doc.Authors[0].Kind != recipe.AuthorOrganization {
		t.Fatalf("graph metadata = %#v", doc)
	}

	if doc.Times == nil || doc.Times.Prep == nil || doc.Times.Prep.Value != "PT10M" ||
		doc.Times.Cook == nil || doc.Times.Cook.Value != "PT5M" || doc.Times.Total == nil || doc.Times.Total.Value != "PT20M" ||
		len(doc.Times.Assertions) != 1 || doc.Times.Assertions[0].Kind != recipe.TimingRest || doc.Times.Assertions[0].Duration.Value != "PT5M" {
		t.Fatalf("graph times = %#v", doc.Times)
	}

	if len(doc.IngredientSections) != 1 || len(doc.IngredientSections[0].Ingredients) != 2 ||
		len(doc.InstructionSections) != 2 || doc.InstructionSections[0].Title != "Prepare · Steep" || doc.InstructionSections[1].Title != "Finish" {
		t.Fatalf("graph sections = %#v", doc)
	}

	for i, want := range []struct{ line, value, unit, ref string }{
		{"100 g oats", "100", "mass.gram", "i1"}, {"200 ml mint water", "200", "volume.milliliter", "i2"},
	} {
		ingredient := doc.IngredientSections[0].Ingredients[i]
		if ingredient.ID != want.ref || ingredient.SourceText != want.line || ingredient.Quantity == nil ||
			ingredient.Quantity.Unit != want.unit || ingredient.Quantity.Amount == nil ||
			ingredient.Quantity.Amount.Value == nil || ingredient.Quantity.Amount.Value.String() != want.value {
			t.Fatalf("ingredient %d = %#v", i, ingredient)
		}

		steps := doc.InstructionSections[i].Steps
		if len(steps) != 1 || len(steps[0].IngredientUses) != 1 || steps[0].IngredientUses[0].IngredientRef != want.ref {
			t.Fatalf("step supplies = %#v", steps)
		}
	}
}
