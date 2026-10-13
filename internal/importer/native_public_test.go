package importer

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func TestNativePublicFixtures(t *testing.T) {
	t.Parallel()

	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, fixture, filename, format, title, sourceKey, sourceURL string
		wrap                                                         func(*testing.T, []byte) []byte
		assert                                                       func(*testing.T, ImportedRecipe)
	}{
		{
			name: "paprika single", fixture: "paprika/lines.json", filename: "sample.paprikarecipe",
			format: "paprika", title: "Cumin oat pot", sourceKey: "paprika-lines",
			sourceURL: "https://example.com/recipes/cumin-oats", wrap: gzipBytes,
			assert: assertPublicPaprikaLines,
		},
		{
			name: "paprika archive", fixture: "paprika/lines.json", filename: "renamed.zip",
			format: "paprika", title: "Cumin oat pot", sourceKey: "paprika-lines",
			sourceURL: "https://example.com/recipes/cumin-oats",
			wrap: func(t *testing.T, data []byte) []byte {
				t.Helper()
				return zipBytes(t, map[string][]byte{"sample.paprikarecipe": gzipBytes(t, data)})
			},
			assert: assertPublicPaprikaLines,
		},
		{
			name: "mealie sections and sidecar", fixture: "mealie/sections.json", filename: "renamed.zip",
			format: "mealie", title: "Lime bean bowl", sourceKey: "mealie-sections",
			sourceURL: "https://example.com/recipes/lime-beans",
			wrap: func(t *testing.T, data []byte) []byte {
				t.Helper()

				return orderedNativeZIP(t,
					nativeZIPMember{"recipes/lime-beans/recipe.json", data},
					nativeZIPMember{"recipes/lime-beans/lime-beans.png", imageBytes.Bytes()},
				)
			},
			assert: func(t *testing.T, item ImportedRecipe) {
				t.Helper()
				assertPublicNativeSections(t, item.Document,
					[]string{"Beans", "Dressing"}, []string{"100 g beans", "20 ml lime juice"},
					[]string{"Warm", "Dress"}, []string{"Warm the beans.", "Stir in the lime juice."})
				assertImage(t, item, imageBytes.Bytes())
			},
		},
		{
			name: "tandoor nested archive", fixture: "tandoor/sections.json", filename: "renamed.zip",
			format: "tandoor", title: "Toasted seed bowl", sourceKey: "seed-bowl",
			sourceURL: "https://example.com/recipes/toasted-seeds",
			wrap: func(t *testing.T, data []byte) []byte {
				t.Helper()

				return zipBytes(t, map[string][]byte{"seed-bowl.zip": orderedNativeZIP(t,
					nativeZIPMember{"recipe.json", data}, nativeZIPMember{"image.png", imageBytes.Bytes()},
				)})
			},
			assert: func(t *testing.T, item ImportedRecipe) {
				t.Helper()
				assertPublicNativeSections(t, item.Document,
					[]string{"Seeds", "Finish"}, []string{"25 g seeds", "salt, to taste"},
					[]string{"Toast", "Finish"}, []string{"Toast the seeds.", "Season the seeds."})
				assertImage(t, item, imageBytes.Bytes())
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(filepath.Join("testdata", tc.fixture))
			if err != nil {
				t.Fatal(err)
			}

			input := Input{Name: tc.filename, Data: tc.wrap(t, data)}
			registry := DefaultRegistry()

			format, _, err := registry.Probe(context.Background(), input)
			if err != nil || format.ID != tc.format {
				t.Fatalf("format = %q, error = %v; want %q", format.ID, err, tc.format)
			}

			batch, err := registry.Import(context.Background(), input, Options{})
			if err != nil || len(batch.Recipes) != 1 {
				t.Fatalf("import = %#v, error = %v", batch, err)
			}

			item := batch.Recipes[0]
			if item.Document.Title != tc.title || item.SourceKey != tc.sourceKey ||
				item.Document.Source == nil || item.Document.Source.URL != tc.sourceURL {
				t.Fatalf("identity = %#v, source key = %q", item.Document, item.SourceKey)
			}

			tc.assert(t, item)

			if err := recipe.ValidateForWrite(item.Document, units.DefaultCatalog()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func assertPublicPaprikaLines(t *testing.T, item ImportedRecipe) {
	t.Helper()
	assertPublicNativeSections(t, item.Document,
		[]string{""}, []string{"100 g oats", "20 ml oil, divided"},
		[]string{""}, []string{"Stir the oats with half the oil.", "Finish with the remaining oil."})

	if item.Document.FirstYield() == nil || item.Document.FirstYield().Text != "2 bowls" ||
		item.Document.Times == nil || item.Document.Times.Prep == nil || item.Document.Times.Prep.Value != "PT5M" ||
		item.Document.Times.Cook == nil || item.Document.Times.Cook.Text != "1 hour 30 minutes" || item.Document.Times.Cook.Value != "" {
		t.Fatalf("yield or authored times = %#v", item.Document)
	}

	if item.Document.SourceLocale != "" {
		t.Fatalf("invented source locale = %q", item.Document.SourceLocale)
	}
}

func assertPublicNativeSections(t *testing.T, doc recipe.Document, ingredientTitles, lines, instructionTitles, steps []string) {
	t.Helper()

	var gotIngredientTitles, gotLines, gotInstructionTitles, gotSteps []string
	for _, section := range doc.IngredientSections {
		gotIngredientTitles = append(gotIngredientTitles, section.Title)
		for _, ingredient := range section.Ingredients {
			gotLines = append(gotLines, ingredient.SourceText)
		}
	}

	for _, section := range doc.InstructionSections {
		gotInstructionTitles = append(gotInstructionTitles, section.Title)
		for _, step := range section.Steps {
			gotSteps = append(gotSteps, step.Text)
		}
	}

	if !reflect.DeepEqual(gotIngredientTitles, ingredientTitles) || !reflect.DeepEqual(gotLines, lines) ||
		!reflect.DeepEqual(gotInstructionTitles, instructionTitles) || !reflect.DeepEqual(gotSteps, steps) {
		t.Fatalf("sections = %v / %v; ingredients = %v; steps = %v", gotIngredientTitles, gotInstructionTitles, gotLines, gotSteps)
	}
}

func TestNativeMalformedMembers(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"bad gzip.paprikarecipe", []byte("invalid gzip")},
		{"bad json.paprikarecipe", gzipBytes(t, []byte(`{"uid":`))},
		{"mealie.zip", zipBytes(t, map[string][]byte{"recipe.json": []byte(`{"recipe_ingredient":`)})},
		{"tandoor.zip", zipBytes(t, map[string][]byte{"one.zip": zipBytes(t, map[string][]byte{"recipe.json": []byte(`{"steps":`)})})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DefaultRegistry().Import(context.Background(), Input{Name: tc.name, Data: tc.data}, Options{}); err == nil {
				t.Fatal("accepted malformed recipe member")
			}
		})
	}
}

func TestNativeCollectionIdentities(t *testing.T) {
	for _, format := range []string{"paprika", "mealie", "tandoor"} {
		t.Run(format, func(t *testing.T) {
			members := make(map[string][]byte)

			for i := range 2 {
				id := fmt.Sprintf("recipe-%d", i)
				fields := map[string]any{"name": "Bowl " + id}

				switch format {
				case "paprika":
					fields["uid"] = id
					fields["ingredients"] = "1 g salt"
					members[id+".paprikarecipe"] = gzipBytes(t, nativeJSON(t, fields))
				case "mealie":
					fields["id"] = id
					fields["recipe_ingredient"] = []any{map[string]any{"display": "1 g salt"}}
					members[id+".json"] = nativeJSON(t, fields)
				case "tandoor":
					fields["steps"] = []any{map[string]any{"instruction": "Season."}}
					members[id+".zip"] = zipBytes(t, map[string][]byte{"recipe.json": nativeJSON(t, fields)})
				}
			}

			batch, err := DefaultRegistry().Import(context.Background(), Input{Name: "renamed.zip", Data: zipBytes(t, members)}, Options{})
			if err != nil || len(batch.Recipes) != 2 {
				t.Fatalf("collection = %#v, error = %v", batch, err)
			}

			seen := map[string]bool{}
			for _, item := range batch.Recipes {
				if seen[item.SourceKey] || (item.SourceKey != "recipe-0" && item.SourceKey != "recipe-1") ||
					item.Document.Title != "Bowl "+item.SourceKey {
					t.Fatalf("collection identity = %#v", item)
				}

				seen[item.SourceKey] = true
				if err := recipe.ValidateForWrite(item.Document, units.DefaultCatalog()); err != nil {
					t.Fatal(err)
				}
			}

			if batch.Recipes[0].Document.ID == batch.Recipes[1].Document.ID {
				t.Fatal("distinct source identities collapsed")
			}
		})
	}
}
