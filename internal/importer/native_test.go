package importer

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref/archivepolicy"
	"github.com/savorum/savorum/internal/units"
)

func nativeJSON(t *testing.T, value any) []byte {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func zipBytes(t *testing.T, files map[string][]byte) []byte {
	t.Helper()

	var b bytes.Buffer

	z := zip.NewWriter(&b)
	for n, v := range files {
		w, e := z.Create(n)
		if e != nil {
			t.Fatal(e)
		}

		if _, e = w.Write(v); e != nil {
			t.Fatal(e)
		}
	}

	if e := z.Close(); e != nil {
		t.Fatal(e)
	}

	return b.Bytes()
}

type nativeZIPMember struct {
	name string
	data []byte
}

func orderedNativeZIP(t *testing.T, members ...nativeZIPMember) []byte {
	t.Helper()

	var data bytes.Buffer

	writer := zip.NewWriter(&data)
	for _, member := range members {
		entry, err := writer.Create(member.name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := entry.Write(member.data); err != nil {
			t.Fatal(err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return data.Bytes()
}

func gzipBytes(t *testing.T, b []byte) []byte {
	t.Helper()

	var out bytes.Buffer

	g := gzip.NewWriter(&out)
	_, _ = g.Write(b)
	_ = g.Close()

	return out.Bytes()
}

func assertImage(t *testing.T, item ImportedRecipe, want []byte) {
	t.Helper()

	if len(item.EmbeddedAssets) != 1 || len(item.Document.Assets) != 1 || len(item.Document.ImageRefs) != 1 || item.Document.PrimaryImage == "" {
		t.Fatalf("image refs missing: embedded=%d assets=%d refs=%v primary=%q", len(item.EmbeddedAssets), len(item.Document.Assets), item.Document.ImageRefs, item.Document.PrimaryImage)
	}

	a := item.EmbeddedAssets[0]
	if !bytes.Equal(a.Bytes, want) || a.MediaType != "image/png" || item.Document.Assets[0].SHA256 != a.SHA256 {
		t.Fatalf("image mismatch: %#v", a)
	}
}

func fixture(t *testing.T, root, pattern string) []byte {
	t.Helper()

	pattern = filepath.Join(root, pattern)

	p, err := filepath.Glob(pattern)
	if err != nil || len(p) == 0 {
		t.Fatalf("fixture unavailable: %s", pattern)
	}

	b, err := os.ReadFile(p[0]) //nolint:gosec // the path is this test's own fixture root, not request input
	if err != nil {
		t.Fatal(err)
	}

	return b
}

func TestNativeCorpus(t *testing.T) {
	const revision = "fec20ecf1bc350598df7c86e91e902708c8a2e4b"

	root := os.Getenv("SAVORUM_RECIPE_CORPUS")
	if root == "" {
		t.Skip("set SAVORUM_RECIPE_CORPUS to run the private recipe-corpus suite")
	}

	if !filepath.IsAbs(root) {
		root = filepath.Join("../..", root)
	}

	commit, err := exec.CommandContext(t.Context(), "git", "-C", root, "rev-parse", "HEAD").Output() //nolint:gosec // the maintainer selects this local test checkout
	if err != nil {
		t.Fatalf("read native corpus revision at %s: %v", root, err)
	}

	if actual := strings.TrimSpace(string(commit)); actual != revision {
		t.Fatalf("native corpus revision = %s, want pinned %s", actual, revision)
	}

	cases := []struct {
		name, pattern string
		adapter       Registry
		count         int
	}{
		{"paprika", "app-fixtures/paprika/windows-3.3.2/batch-01/*.paprikarecipes", DefaultRegistry(), 42},
		{"mealie", "app-fixtures/mealie/v3.24.0/*.zip", DefaultRegistry(), 120},
		{"tandoor", "app-fixtures/tandoor/v2.6.13/*.zip", DefaultRegistry(), 120},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := fixture(t, root, tc.pattern)

			got, err := tc.adapter.Import(context.Background(), Input{Name: "renamed.zip", Data: b}, Options{})
			if err != nil {
				t.Fatal(err)
			}

			if len(got.Recipes) != tc.count {
				t.Fatalf("format=%s recipes=%d want %d", got.Source.Format.ID, len(got.Recipes), tc.count)
			}

			for _, item := range got.Recipes {
				if item.Document.Title == "" || item.Document.Source == nil || item.SourceKey == "" {
					t.Fatal("missing identity")
				}

				if err := recipe.ValidateForWrite(item.Document, units.DefaultCatalog()); err != nil {
					if ve, ok := errors.AsType[*recipe.ValidationError](err); ok {
						t.Fatalf("violations=%#v", ve.Violations)
					}

					t.Fatalf("%v (assets=%#v refs=%#v)", err, item.Document.Assets, item.Document.ImageRefs)
				}
			}

			if tc.name == "paprika" && got.Recipes[0].Document.Source.URL == "" {
				t.Fatal("Paprika source URL was lost")
			}

			if tc.name == "mealie" && len(got.Recipes[0].Document.IngredientSections) == 0 {
				t.Fatal("Mealie ingredients were lost")
			}

			if tc.name == "tandoor" && len(got.Recipes[0].Document.InstructionSections) == 0 {
				t.Fatal("Tandoor instructions were lost")
			}
		})
	}
}

func TestNativeRegistryImages(t *testing.T) {
	tiny := []byte("\x89PNG\r\n\x1a\nsynthetic")
	paprika := map[string]any{"uid": "p1", "name": "P", "ingredients": "1 cup flour", "directions": "Mix", "photos": []any{map[string]any{"filename": "p.png", "data": base64.StdEncoding.EncodeToString(tiny)}}}
	pb := nativeJSON(t, paprika)
	mealie := map[string]any{"id": "m1", "slug": "m", "name": "M", "image": "m", "recipe_ingredient": []any{map[string]any{"display": "1 cup flour"}}, "recipe_instructions": []any{map[string]any{"text": "Mix"}}}
	mb := nativeJSON(t, mealie)
	tandoor := map[string]any{"name": "T", "steps": []any{map[string]any{"name": "S", "instruction": "Mix", "ingredients": []any{}}}}
	tb := nativeJSON(t, tandoor)

	tests := []struct {
		name   string
		data   []byte
		format string
	}{
		{"paprika", zipBytes(t, map[string][]byte{"r.paprikarecipe": gzipBytes(t, pb)}), "paprika"},
		{"mealie", zipBytes(t, map[string][]byte{"recipes/m/m.json": mb, "recipes/m/m.png": tiny}), "mealie"},
		{"tandoor", zipBytes(t, map[string][]byte{"7.zip": zipBytes(t, map[string][]byte{"recipe.json": tb, "image.png": tiny})}), "tandoor"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, e := DefaultRegistry().Import(context.Background(), Input{Name: "renamed.zip", Data: tc.data}, Options{})
			if e != nil {
				t.Fatal(e)
			}

			if len(got.Recipes) != 1 {
				t.Fatalf("recipes=%d", len(got.Recipes))
			}

			if got.Source.Format.ID != tc.format {
				t.Fatalf("format=%q", got.Source.Format.ID)
			}

			assertImage(t, got.Recipes[0], tiny)
		})
	}
}

func TestTandoorImagesDoNotDependOnArchiveMemberOrder(t *testing.T) {
	recipeJSON := nativeJSON(t, map[string]any{
		"name":  "T",
		"steps": []any{map[string]any{"name": "S", "instruction": "Mix", "ingredients": []any{}}},
	})
	firstImage := []byte("\x89PNG\r\n\x1a\nfirst")
	secondImage := []byte("\x89PNG\r\n\x1a\nsecond")

	tests := []struct {
		name    string
		members []nativeZIPMember
	}{
		{
			name: "images before and after recipe",
			members: []nativeZIPMember{
				{name: "first.png", data: firstImage},
				{name: "recipe.json", data: recipeJSON},
				{name: "second.png", data: secondImage},
			},
		},
		{
			name: "reverse image order around recipe",
			members: []nativeZIPMember{
				{name: "second.png", data: secondImage},
				{name: "recipe.json", data: recipeJSON},
				{name: "first.png", data: firstImage},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inner := orderedNativeZIP(t, test.members...)
			outer := orderedNativeZIP(t, nativeZIPMember{name: "7.zip", data: inner})

			batch, err := DefaultRegistry().Import(context.Background(), Input{Name: "export.zip", Data: outer}, Options{})
			if err != nil {
				t.Fatal(err)
			}

			if len(batch.Recipes) != 1 {
				t.Fatalf("recipes=%d", len(batch.Recipes))
			}

			assertTandoorImageSet(t, batch.Recipes[0], firstImage, secondImage)
		})
	}
}

func TestTandoorBundleRecipesShareTheCompleteImageSet(t *testing.T) {
	firstRecipe := nativeJSON(t, map[string]any{"name": "First", "steps": []any{}})
	secondRecipe := nativeJSON(t, map[string]any{"name": "Second", "steps": []any{}})
	firstImage := []byte("\x89PNG\r\n\x1a\nfirst")
	secondImage := []byte("\x89PNG\r\n\x1a\nsecond")
	inner := orderedNativeZIP(t,
		nativeZIPMember{name: "first.png", data: firstImage},
		nativeZIPMember{name: "first.json", data: firstRecipe},
		nativeZIPMember{name: "second.json", data: secondRecipe},
		nativeZIPMember{name: "second.png", data: secondImage},
	)
	outer := orderedNativeZIP(t, nativeZIPMember{name: "7.zip", data: inner})

	batch, err := DefaultRegistry().Import(context.Background(), Input{Name: "export.zip", Data: outer}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	if len(batch.Recipes) != 2 {
		t.Fatalf("recipes=%d", len(batch.Recipes))
	}

	for _, item := range batch.Recipes {
		assertTandoorImageSet(t, item, firstImage, secondImage)
	}
}

func TestTandoorPartialResultKeepsImagesWhenLaterMemberFails(t *testing.T) {
	recipeJSON := nativeJSON(t, map[string]any{"name": "First", "steps": []any{}})
	image := []byte("\x89PNG\r\n\x1a\nfirst")
	inner := orderedNativeZIP(t,
		nativeZIPMember{name: "image.png", data: image},
		nativeZIPMember{name: "recipe.json", data: recipeJSON},
		nativeZIPMember{name: "broken.json", data: []byte("{")},
	)
	outer := orderedNativeZIP(t, nativeZIPMember{name: "7.zip", data: inner})

	batch, err := (tandoorAdapter{}).Import(context.Background(), Input{Name: "export.zip", Data: outer}, Options{})
	if err == nil {
		t.Fatal("invalid later member was accepted")
	}

	if len(batch.Recipes) != 1 {
		t.Fatalf("partial recipes=%d", len(batch.Recipes))
	}

	assertTandoorImageSet(t, batch.Recipes[0], image)
}

func assertTandoorImageSet(t *testing.T, item ImportedRecipe, expected ...[]byte) {
	t.Helper()

	if len(item.EmbeddedAssets) != len(expected) || len(item.Document.Assets) != len(expected) || len(item.Document.ImageRefs) != len(expected) {
		t.Fatalf("image refs missing: embedded=%d assets=%d refs=%d", len(item.EmbeddedAssets), len(item.Document.Assets), len(item.Document.ImageRefs))
	}

	want := make(map[string]bool, len(expected))
	for _, data := range expected {
		want[string(data)] = true
	}

	for _, asset := range item.EmbeddedAssets {
		if !want[string(asset.Bytes)] {
			t.Fatalf("unexpected embedded image %q", asset.Bytes)
		}

		delete(want, string(asset.Bytes))
	}

	if len(want) != 0 {
		t.Fatalf("missing images: %d", len(want))
	}
}

func TestTandoorIngredientHeadings(t *testing.T) {
	data := []byte(`{"name":"Layer cake","steps":[{"name":"Prepare","instruction":"Mix separately.","ingredients":[{"is_header":true,"food":{"name":"Cake"}},{"amount":2,"unit":{"name":"cups"},"food":{"name":"flour"}},{"is_header":true,"food":{"name":"Icing"}},{"amount":1,"unit":{"name":"cup"},"food":{"name":"sugar"}}]}]}`)

	batch, err := DefaultRegistry().Import(context.Background(), Input{Name: "export.zip", Data: zipBytes(t, map[string][]byte{"42.zip": zipBytes(t, map[string][]byte{"recipe.json": data})})}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	sections := batch.Recipes[0].Document.IngredientSections
	if len(sections) != 2 || sections[0].Title != "Cake" || sections[1].Title != "Icing" || len(sections[0].Ingredients) != 1 || len(sections[1].Ingredients) != 1 {
		t.Fatalf("ingredient headings lost: %+v", sections)
	}
}

func TestNativeDurationRetainsCompoundText(t *testing.T) {
	if got := duration("2 days"); got == nil || got.Text != "2 days" || got.Value != "" {
		t.Fatalf("unknown duration unit was changed: %#v", got)
	}

	if got := duration("1 hr"); got == nil || got.Value != "PT1H" {
		t.Fatalf("hour abbreviation was changed: %#v", got)
	}

	got := duration("1 hour 30 minutes")
	if got == nil || got.Text != "1 hour 30 minutes" || got.Value != "" {
		t.Fatalf("compound duration was partially parsed: %#v", got)
	}

	if got := duration("90 minutes"); got == nil || got.Value != "PT90M" {
		t.Fatalf("simple duration changed: %#v", got)
	}
}

func TestMealiePublishesYieldAndEquipment(t *testing.T) {
	data, err := json.Marshal(map[string]any{
		"id": "m1", "slug": "m", "name": "M",
		"recipe_ingredient":     []any{map[string]any{"display": "1 cup flour"}},
		"recipe_instructions":   []any{map[string]any{"text": "Mix"}},
		"recipe_yield_quantity": 2, "recipe_yield": "servings",
		"tools": []any{map[string]any{"name": "Whisk"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := DefaultRegistry().Import(context.Background(), Input{Name: "export.zip", Data: zipBytes(t, map[string][]byte{"recipes/m/m.json": data})}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Recipes) != 1 {
		t.Fatalf("recipes=%d", len(got.Recipes))
	}

	doc := got.Recipes[0].Document
	if doc.FirstYield() == nil || doc.FirstYield().Text != "2 servings" {
		t.Fatalf("Mealie yield lost: %#v", doc.FirstYield())
	}

	if len(doc.Equipment) != 1 || doc.Equipment[0].Name != "Whisk" {
		t.Fatalf("Mealie equipment lost: %#v", doc.Equipment)
	}
}

func TestNativeUnknownFieldsAndExportVersion(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "paprika",
			data: func() []byte {
				b := nativeJSON(t, map[string]any{"uid": "p1", "name": "P", "future_field": "future", "export_version": "9"})
				return zipBytes(t, map[string][]byte{"p.paprikarecipe": gzipBytes(t, b)})
			}(),
		},
		{
			name: "mealie",
			data: func() []byte {
				b := nativeJSON(t, map[string]any{"id": "m1", "name": "M", "recipe_ingredient": []any{}, "recipe_instructions": []any{}, "future_field": "future", "export_version": "9"})
				return zipBytes(t, map[string][]byte{"recipes/m/m.json": b})
			}(),
		},
		{
			name: "tandoor",
			data: func() []byte {
				b := nativeJSON(t, map[string]any{"name": "T", "steps": []any{}, "future_field": "future", "export_version": "9"})
				return zipBytes(t, map[string][]byte{"42.zip": zipBytes(t, map[string][]byte{"recipe.json": b})})
			}(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DefaultRegistry().Import(context.Background(), Input{Name: "export.zip", Data: tc.data}, Options{})
			if err != nil {
				t.Fatal(err)
			}

			if len(got.Recipes) != 1 {
				t.Fatalf("recipes=%d", len(got.Recipes))
			}

			item := got.Recipes[0]
			if item.ImporterMetadata["export_version"] != "9" {
				t.Fatalf("export version metadata=%#v", item.ImporterMetadata)
			}

			found := false

			for _, diagnostic := range item.Diagnostics {
				if diagnostic.Code == "unsupported_field" && strings.HasSuffix(diagnostic.Area, "future_field") {
					found = true
					break
				}
			}

			if !found {
				t.Fatalf("unknown field diagnostic missing: %#v", item.Diagnostics)
			}
		})
	}
}

// Paprika spells "not rated" as a zero; the library holds one to five.
func TestPaprikaRatingStaysInsideWhatTheLibraryHolds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		rating     any
		wantRating *uint8
		wantReport bool
	}{
		{name: "unrated", rating: 0.0},
		{name: "rated", rating: 4.0, wantRating: new(uint8(4))},
		{name: "out of range", rating: 9.0, wantReport: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			item := paprikaRecipe(map[string]any{"uid": "p1", "name": "P", "rating": tc.rating})

			switch {
			case tc.wantRating == nil && item.LibraryMetadata.Rating != nil:
				t.Fatalf("rating = %d, want none", *item.LibraryMetadata.Rating)
			case tc.wantRating != nil && (item.LibraryMetadata.Rating == nil || *item.LibraryMetadata.Rating != *tc.wantRating):
				t.Fatalf("rating = %v, want %d", item.LibraryMetadata.Rating, *tc.wantRating)
			}

			if reported := hasUnsupportedField(item, "rating"); reported != tc.wantReport {
				t.Fatalf("rating reported as unmapped = %t, want %t", reported, tc.wantReport)
			}
		})
	}
}

// A JSON null is an unset field, not unrepresented content.
func TestNativeFieldsTheSourceLeftUnsetAreNotReportedAsLost(t *testing.T) {
	t.Parallel()

	item := paprikaRecipe(map[string]any{
		"uid": "p1", "name": "P",
		"difficulty": nil, "nutritional_info": nil, "photo": nil, "photo_hash": nil,
		"future_field": nil, "empty_list": []any{},
	})

	if len(item.Diagnostics) != 0 {
		t.Fatalf("unset fields reported as unmapped: %#v", item.Diagnostics)
	}
}

// Paprika's bookkeeping fields are not reported, and its photograph
// (`photo_data`) is carried as an asset.
func TestPaprikaCarriesASinglePhotographAndKeepsItsBookkeepingQuiet(t *testing.T) {
	t.Parallel()

	tiny := []byte("\x89PNG\r\n\x1a\nsynthetic")
	item := paprikaRecipe(map[string]any{
		"uid": "p1", "name": "P",
		"photo":      "P1.PNG",
		"photo_hash": "ABCD",
		"hash":       "EFGH",
		"photo_data": base64.StdEncoding.EncodeToString(tiny),
		"created":    "2026-08-31 20:39:20",
	})

	assertImage(t, item, tiny)

	if path := item.Document.Assets[0].Path; path != "assets/p1.png" {
		t.Fatalf("asset path = %q", path)
	}

	for _, field := range []string{"photo", "photo_data", "photo_hash", "hash"} {
		if hasUnsupportedField(item, field) {
			t.Fatalf("%q reported as unmapped content", field)
		}
	}

	// What the source genuinely holds and Savorum does not is still reported.
	if !hasUnsupportedField(item, "created") {
		t.Fatalf("source.created went unreported: %#v", item.Diagnostics)
	}
}

// Two photographs exported under one filename are one asset to a recipe
// document, whose asset paths are unique without regard to case.
func TestPaprikaDoesNotAttachOnePhotographTwice(t *testing.T) {
	t.Parallel()

	tiny := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nsynthetic"))
	item := paprikaRecipe(map[string]any{
		"uid": "p1", "name": "P",
		"photos": []any{
			map[string]any{"filename": "Shot.png", "data": tiny},
			map[string]any{"filename": "shot.png", "data": tiny},
		},
	})

	if len(item.Document.Assets) != 1 {
		t.Fatalf("document assets = %#v", item.Document.Assets)
	}

	if err := recipe.ValidateForWrite(item.Document, units.DefaultCatalog()); err != nil {
		t.Fatal(err)
	}
}

func hasUnsupportedField(item ImportedRecipe, field string) bool {
	for _, diagnostic := range item.Diagnostics {
		if diagnostic.Code == "unsupported_field" && strings.HasSuffix(diagnostic.Area, field) {
			return true
		}
	}

	return false
}

func TestNativeSourcesShareExpansionBudget(t *testing.T) {
	image := append([]byte{0xff, 0xd8, 0xff}, bytes.Repeat([]byte("x"), 4096)...)
	data := zipBytes(t, map[string][]byte{"recipe.json": []byte(`{"name":"Soup","recipe_ingredient":[],"recipe_instructions":[]}`), "image.jpg": image})
	options := Options{WorkBudget: archivepolicy.NewBudget(6500)}

	registry := DefaultRegistry()
	if _, err := registry.Import(t.Context(), Input{Name: "mealie.zip", Data: data}, options); err != nil {
		t.Fatalf("first source: %v", err)
	}

	if _, err := registry.Import(t.Context(), Input{Name: "mealie.zip", Data: data}, options); !errors.Is(err, archivepolicy.ErrLimit) {
		t.Fatalf("second source gets a fresh budget: %v", err)
	}
}
