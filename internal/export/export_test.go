package export_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/testfixture"
)

// fakeLibrary holds exactly what export reads, so a test can state the library
// it wants without a database.
type fakeLibrary struct {
	entries   map[library.EntryID]library.Entry
	trashed   map[library.EntryID]library.Entry
	revisions map[string]library.Revision
	books     map[string]library.Book
	reads     int
}

func newLibrary() *fakeLibrary {
	return &fakeLibrary{
		entries:   map[library.EntryID]library.Entry{},
		trashed:   map[library.EntryID]library.Entry{},
		revisions: map[string]library.Revision{},
		books:     map[string]library.Book{},
	}
}

func (f *fakeLibrary) put(id library.EntryID, revision uint64, document recipe.Document) {
	f.entries[id] = library.Entry{ID: id, Revision: revision, Recipe: document}
}

func (f *fakeLibrary) putRevision(id library.EntryID, number uint64, document recipe.Document) {
	f.revisions[fmt.Sprintf("%s/%d", id, number)] = library.Revision{
		EntryID: id, Number: number, Recipe: document, State: library.StateActive,
	}
}

func (f *fakeLibrary) Get(_ context.Context, id library.EntryID) (library.Entry, error) {
	f.reads++

	entry, held := f.entries[id]
	if !held {
		return library.Entry{}, library.ErrNotFound
	}

	return entry, nil
}

func (f *fakeLibrary) GetTrashed(_ context.Context, id library.EntryID) (library.Entry, error) {
	entry, held := f.trashed[id]
	if !held {
		return library.Entry{}, library.ErrNotFound
	}

	return entry, nil
}

func (f *fakeLibrary) GetRevision(_ context.Context, id library.EntryID, number uint64) (library.Revision, error) {
	retained, held := f.revisions[fmt.Sprintf("%s/%d", id, number)]
	if !held {
		return library.Revision{}, library.ErrNotFound
	}

	return retained, nil
}

func (f *fakeLibrary) GetBook(_ context.Context, id string) (library.Book, error) {
	book, held := f.books[id]
	if !held {
		return library.Book{}, library.ErrNotFound
	}

	return book, nil
}

// fakeAssets is a content-addressed store a test can lie in, so the cases
// where a photograph is missing or is not what the recipe says it is can be
// exercised rather than assumed.
type fakeAssets struct {
	bytes map[string][]byte
	// recordedSize overrides what the store claims about a blob, which is how
	// a store that disagrees with its own files is simulated.
	recordedSize map[string]int64
	opens        map[string]int
}

func newAssets() *fakeAssets {
	return &fakeAssets{bytes: map[string][]byte{}, recordedSize: map[string]int64{}, opens: map[string]int{}}
}

// hold registers bytes under their real digest and reports it.
func (f *fakeAssets) hold(content []byte) string {
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	f.bytes[digest] = content

	return digest
}

func (f *fakeAssets) Open(_ context.Context, id assets.ID) (assets.Blob, io.ReadCloser, error) {
	digest := strings.TrimPrefix(string(id), "sha256:")

	content, held := f.bytes[digest]
	if !held {
		return assets.Blob{}, nil, assets.ErrNotFound
	}

	f.opens[digest]++

	size := int64(len(content))
	if recorded, stated := f.recordedSize[digest]; stated {
		size = recorded
	}

	return assets.Blob{ID: id, ContentHash: digest, Size: size},
		io.NopCloser(bytes.NewReader(content)), nil
}

func service(t *testing.T, recipes *fakeLibrary, blobs export.Assets) *export.Service {
	t.Helper()

	exporter, err := export.NewService(recipes, blobs)
	if err != nil {
		t.Fatal(err)
	}

	return exporter
}

// plainDocument is the fixture recipe with its photograph removed, for the
// cases that are about the recipe rather than about files.
func plainDocument() recipe.Document {
	document := testfixture.Document()

	document.Assets, document.ImageRefs, document.PrimaryImage = nil, nil, ""
	for sectionIndex := range document.InstructionSections {
		for stepIndex := range document.InstructionSections[sectionIndex].Steps {
			document.InstructionSections[sectionIndex].Steps[stepIndex].ImageRefs = nil
		}
	}

	return document
}

func exportOne(t *testing.T, exporter *export.Service, source export.Source, format export.Format) ([]byte, export.Plan) {
	t.Helper()

	buffer := &bytes.Buffer{}

	plan, err := exporter.Export(context.Background(), source, format, buffer)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	return buffer.Bytes(), plan
}

func recipeJSON(t *testing.T, packaged []byte) string {
	t.Helper()

	archive, err := zip.NewReader(bytes.NewReader(packaged), int64(len(packaged)))
	if err != nil {
		t.Fatal(err)
	}

	file, err := archive.Open("recipe.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}

	return string(content)
}

func TestExportKeepsThePortableIdentityAndLeavesTheLocalOneBehind(t *testing.T) {
	t.Parallel()

	recipes := newLibrary()
	document := plainDocument()
	recipes.put("entry-local-9182", 17, document)
	exporter := service(t, recipes, newAssets())

	packaged, plan := exportOne(t, exporter,
		export.NewRecipeSource(recipes, "entry-local-9182"), export.FormatSREF)

	opened, err := sref.ReadPackage(packaged, sref.DefaultLimits())
	if err != nil {
		t.Fatalf("Savorum cannot read what it wrote: %v", err)
	}

	if opened.Recipe.ID != document.ID {
		t.Fatalf("portable identity became %q, want %q", opened.Recipe.ID, document.ID)
	}

	body := recipeJSON(t, packaged)
	for _, local := range []string{"entry-local-9182", "revision", "schema_version"} {
		if strings.Contains(body, local) {
			t.Fatalf("the export carries this library's %q", local)
		}
	}

	if plan.Recipes[0].Revision != 17 {
		t.Fatalf("the export froze revision %d, want 17", plan.Recipes[0].Revision)
	}

	// Exporting again is a read. It cannot mint a new identity, and it cannot
	// produce a different file.
	again, _ := exportOne(t, exporter,
		export.NewRecipeSource(recipes, "entry-local-9182"), export.FormatSREF)
	if !bytes.Equal(packaged, again) {
		t.Fatal("two exports of one unchanged recipe produced different bytes")
	}
}

func TestExportPreservesWhatTheRecipeActuallySays(t *testing.T) {
	t.Parallel()

	recipes := newLibrary()
	document := plainDocument()
	// Replacing the clock orphans the fixture's step references, which point
	// at the assertions being removed.
	for sectionIndex := range document.InstructionSections {
		for stepIndex := range document.InstructionSections[sectionIndex].Steps {
			document.InstructionSections[sectionIndex].Steps[stepIndex].TimingRefs = nil
		}
	}

	document.Times.Assertions = []recipe.TimingAssertion{
		{ID: "t1", Kind: recipe.TimingChill, Duration: recipe.DurationExpression{Value: "PT1H"}, Label: "chill the dough"},
		{ID: "t2", Kind: recipe.TimingCook, Duration: recipe.DurationExpression{Value: "PT30M"}},
	}
	document.Notes = append(document.Notes, "Freeze unbaked pies on a tray, then bag them.")
	document.Title = "Australian vegetable hand pies — crème fraîche pastry"
	recipes.put("entry-a", 1, document)
	exporter := service(t, recipes, newAssets())

	packaged, _ := exportOne(t, exporter, export.NewRecipeSource(recipes, "entry-a"), export.FormatSREF)

	opened, err := sref.ReadPackage(packaged, sref.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}

	written := opened.Recipe

	if len(written.IngredientSections) != len(document.IngredientSections) {
		t.Fatalf("ingredient groups: %d, want %d", len(written.IngredientSections), len(document.IngredientSections))
	}

	if written.IngredientSections[0].Title != "Pastry" || written.IngredientSections[1].Title != "Filling" {
		t.Fatal("ingredient group titles did not survive")
	}

	if len(written.InstructionSections) != 2 || written.InstructionSections[1].Title != "Fill and bake" {
		t.Fatal("instruction groups did not survive")
	}

	if len(written.Times.Assertions) != 2 || written.Times.Assertions[0].Label != "chill the dough" {
		t.Fatalf("time phases did not survive: %#v", written.Times)
	}

	if written.Times.Total.Value != "PT2H15M" || written.Times.Additional.Value != "PT1H" {
		t.Fatal("the summary clock did not survive alongside the phases")
	}

	// An exact amount is exact. A sum, a range, alternatives, and an amount
	// the source never made measurable each mean something different, and each
	// has to arrive unchanged.
	flour := written.IngredientSections[0].Ingredients[0]
	if flour.Quantity.Kind != recipe.QuantitySum || len(flour.Quantity.Terms) != 2 {
		t.Fatalf("a sum quantity was flattened: %#v", flour.Quantity)
	}

	if *flour.Quantity.Terms[0].Amount.Value != "2" || flour.Quantity.Terms[0].Unit != "volume.cup.au" {
		t.Fatal("an Australian cup was not exported as an Australian cup")
	}

	if flour.SourceText != document.IngredientSections[0].Ingredients[0].SourceText {
		t.Fatal("the authored ingredient line did not survive")
	}

	pumpkin := written.IngredientSections[1].Ingredients[0]
	if *pumpkin.Quantity.Amount.Min != "450" || *pumpkin.Quantity.Amount.Max != "500" || !pumpkin.Quantity.Amount.Approximate {
		t.Fatalf("a range lost its bounds: %#v", pumpkin.Quantity.Amount)
	}

	salt := written.IngredientSections[1].Ingredients[2]
	if salt.Quantity.Kind != recipe.QuantityOpaque || salt.Quantity.Text != "to taste" {
		t.Fatal("an unmeasurable amount was invented into a number")
	}

	if salt.Normalization == nil || len(salt.Normalization.Warnings) != 1 {
		t.Fatal("the warning that says why it is unmeasurable did not survive")
	}

	if written.Source == nil || written.Source.Name != "Kitchen notebook" {
		t.Fatal("provenance did not survive")
	}

	if written.SourceLocale != "en-AU" || written.Language != "en-AU" {
		t.Fatal("language and source locale did not survive")
	}

	if len(written.Notes) != 2 || written.Notes[1] != "Freeze unbaked pies on a tray, then bag them." {
		t.Fatal("portable notes did not survive")
	}

	if written.Title != document.Title {
		t.Fatalf("Unicode in the title did not survive: %q", written.Title)
	}
}

// What is written is the stored recipe, not a presentation of it.
func TestExportWritesTheStoredRecipeRatherThanADisplayOfIt(t *testing.T) {
	t.Parallel()

	recipes := newLibrary()
	document := plainDocument()
	recipes.put("entry-a", 1, document)
	exporter := service(t, recipes, newAssets())

	packaged, _ := exportOne(t, exporter, export.NewRecipeSource(recipes, "entry-a"), export.FormatSREF)

	opened, err := sref.ReadPackage(packaged, sref.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}

	if opened.Recipe.FirstYield().Text != "8 hand pies" || *opened.Recipe.FirstYield().Quantity.Amount.Value != "8" {
		t.Fatalf("the yield was rewritten: %#v", opened.Recipe.FirstYield())
	}

	butter := opened.Recipe.IngredientSections[0].Ingredients[1]
	if butter.Quantity.Options[0].Unit != "mass.gram" || *butter.Quantity.Options[0].Amount.Value != "225" {
		t.Fatal("a measurement was converted on the way out")
	}

	if opened.Recipe.IngredientSections[1].Ingredients[0].Quantity.Unit != "mass.gram" {
		t.Fatal("a metric amount was replaced by a display conversion")
	}
}

func TestATrashedRecipeIsStillExportableUntilItIsPurged(t *testing.T) {
	t.Parallel()

	recipes := newLibrary()
	document := plainDocument()
	recipes.trashed["entry-gone"] = library.Entry{ID: "entry-gone", Revision: 4, Recipe: document}
	exporter := service(t, recipes, newAssets())

	packaged, _ := exportOne(t, exporter, export.NewRecipeSource(recipes, "entry-gone"), export.FormatSREF)

	opened, err := sref.ReadPackage(packaged, sref.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}

	if opened.Recipe.ID != document.ID {
		t.Fatal("a recipe in the trash exported as something else")
	}

	if strings.Contains(recipeJSON(t, packaged), "trash") {
		t.Fatal("trash is this library's state and must not travel")
	}
}

func TestARetainedVersionExportsExactlyAsItReadThen(t *testing.T) {
	t.Parallel()

	recipes := newLibrary()
	first := plainDocument()
	first.Title = "Title A"
	second := plainDocument()
	second.Title = "Title B"
	current := plainDocument()
	current.Title = "Title C"

	recipes.putRevision("entry-a", 1, first)
	recipes.putRevision("entry-a", 2, second)
	recipes.put("entry-a", 3, current)
	exporter := service(t, recipes, newAssets())

	packaged, plan := exportOne(t, exporter,
		export.NewRevisionSource(recipes, "entry-a", 1), export.FormatSREF)

	opened, err := sref.ReadPackage(packaged, sref.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}

	if opened.Recipe.Title != "Title A" {
		t.Fatalf("revision 1 exported as %q", opened.Recipe.Title)
	}

	if opened.Recipe.ID != current.ID {
		t.Fatal("an earlier version has a different portable identity")
	}

	if plan.Recipes[0].Revision != 1 {
		t.Fatalf("the plan froze revision %d", plan.Recipes[0].Revision)
	}

	if strings.Contains(recipeJSON(t, packaged), `"revision"`) {
		t.Fatal("the revision number travelled with the recipe")
	}

	// Exporting history is a read: the current version is untouched.
	if recipes.entries["entry-a"].Recipe.Title != "Title C" {
		t.Fatal("exporting an earlier version changed the current one")
	}

	if retained := recipes.revisions["entry-a/1"]; retained.Recipe.Title != "Title A" {
		t.Fatal("exporting an earlier version changed history")
	}
}

func TestExportCarriesEveryFileTheRecipeReferences(t *testing.T) {
	t.Parallel()

	blobs := newAssets()
	hero := []byte("a photograph of four hand pies")
	step := []byte("a photograph of the sealed edge")
	note := []byte("scanned from a notebook page")
	heroDigest, stepDigest, noteDigest := blobs.hold(hero), blobs.hold(step), blobs.hold(note)

	document := plainDocument()
	document.Assets = []recipe.Asset{
		{
			ID: "hero", Path: "assets/hero.jpg", MediaType: "image/jpeg", SHA256: heroDigest,
			Size: int64(len(hero)), Role: recipe.AssetRoleImage, Alt: "Four baked hand pies",
		},
		{
			ID: "seal", Path: "assets/seal.png", MediaType: "image/png", SHA256: stepDigest,
			Size: int64(len(step)), Role: recipe.AssetRoleImage,
		},
		{
			ID: "notebook", Path: "assets/notebook.txt", MediaType: "text/plain", SHA256: noteDigest,
			Size: int64(len(note)), Role: recipe.AssetRoleSource,
		},
	}
	document.PrimaryImage = "hero"
	document.ImageRefs = []string{"seal", "hero"}
	// One asset referenced by two steps must be carried once and referenced
	// twice, not duplicated into two archive entries.
	document.InstructionSections[1].Steps[0].ImageRefs = []string{"seal"}
	document.InstructionSections[1].Steps[1].ImageRefs = []string{"seal", "hero"}

	recipes := newLibrary()
	recipes.put("entry-a", 1, document)
	exporter := service(t, recipes, blobs)

	packaged, _ := exportOne(t, exporter, export.NewRecipeSource(recipes, "entry-a"), export.FormatSREF)

	opened, err := sref.ReadPackage(packaged, sref.DefaultLimits())
	if err != nil {
		t.Fatalf("the package Savorum wrote is not a package it reads: %v", err)
	}

	if len(opened.Assets) != 3 {
		t.Fatalf("the package carries %d files, want 3", len(opened.Assets))
	}

	byID := map[string][]byte{}
	for _, asset := range opened.Assets {
		byID[asset.ID] = asset.Bytes
	}

	for id, want := range map[string][]byte{"hero": hero, "seal": step, "notebook": note} {
		if !bytes.Equal(byID[id], want) {
			t.Fatalf("%q did not travel as the bytes this library holds", id)
		}
	}

	archive, err := zip.NewReader(bytes.NewReader(packaged), int64(len(packaged)))
	if err != nil {
		t.Fatal(err)
	}

	if len(archive.File) != 5 {
		t.Fatalf("the package holds %d entries, want recipe, manifest, and three files", len(archive.File))
	}
}

func TestARecipeWithNoFilesStillExportsAsAPackage(t *testing.T) {
	t.Parallel()

	recipes := newLibrary()
	recipes.put("entry-a", 1, plainDocument())
	exporter := service(t, recipes, nil)

	packaged, plan := exportOne(t, exporter, export.NewRecipeSource(recipes, "entry-a"), export.FormatSREF)
	if !strings.HasSuffix(plan.Filename, ".sref") {
		t.Fatalf("filename %q", plan.Filename)
	}

	if _, err := sref.ReadPackage(packaged, sref.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
}

func TestAnUnavailableFileFailsTheExportRatherThanShrinkingIt(t *testing.T) {
	t.Parallel()

	stored := []byte("a photograph of four hand pies")
	digest := sha256.Sum256(stored)
	declared := hex.EncodeToString(digest[:])

	withAsset := func() recipe.Document {
		document := plainDocument()
		document.Assets = []recipe.Asset{{
			ID: "hero", Path: "assets/hero.jpg", MediaType: "image/jpeg",
			SHA256: declared, Size: int64(len(stored)), Role: recipe.AssetRoleImage,
		}}
		document.PrimaryImage = "hero"
		document.ImageRefs = []string{"hero"}

		return document
	}

	cases := map[string]func() *fakeAssets{
		"the bytes are gone": newAssets,
		"the bytes are not the ones the recipe names": func() *fakeAssets {
			blobs := newAssets()
			blobs.bytes[declared] = []byte("a photograph of something else")

			return blobs
		},
		"the store disagrees about the size": func() *fakeAssets {
			blobs := newAssets()
			blobs.hold(stored)
			blobs.recordedSize[declared] = 9

			return blobs
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			recipes := newLibrary()
			recipes.put("entry-a", 1, withAsset())
			exporter := service(t, recipes, build())

			buffer := &bytes.Buffer{}

			_, err := exporter.Export(context.Background(),
				export.NewRecipeSource(recipes, "entry-a"), export.FormatSREF, buffer)
			if err == nil {
				t.Fatal("an incomplete export succeeded")
			}

			if !errors.Is(err, export.ErrAssetUnavailable) {
				t.Fatalf("error = %v, want an unavailable file", err)
			}

			var unavailable *export.AssetError
			if !errors.As(err, &unavailable) || unavailable.RecipeTitle == "" || unavailable.ReasonCode == "" {
				t.Fatalf("the failure does not say which recipe: %v", err)
			}
			// The reader is told what is wrong, not where Savorum keeps files.
			if strings.Contains(err.Error(), "/") {
				t.Fatalf("the failure exposes a storage path: %v", err)
			}

			if buffer.Len() != 0 {
				t.Fatal("bytes were written before the export failed")
			}
		})
	}
}

func TestASizeThatDisagreesWithTheBytesFailsTheExport(t *testing.T) {
	t.Parallel()

	content := []byte("a photograph of four hand pies")
	blobs := newAssets()
	digest := blobs.hold(content)
	document := plainDocument()
	document.Assets = []recipe.Asset{{
		ID: "hero", Path: "assets/hero.jpg", MediaType: "image/jpeg",
		SHA256: digest, Size: int64(len(content)) + 5, Role: recipe.AssetRoleImage,
	}}
	document.PrimaryImage = "hero"
	document.ImageRefs = []string{"hero"}
	recipes := newLibrary()
	recipes.put("entry-a", 1, document)

	_, err := service(t, recipes, blobs).Export(context.Background(),
		export.NewRecipeSource(recipes, "entry-a"), export.FormatSREF, &bytes.Buffer{})
	if !errors.Is(err, export.ErrAssetUnavailable) {
		t.Fatalf("error = %v, want the size disagreement to fail the export", err)
	}
}

func TestFilenamesAreReadableAndSafe(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, title, fallback, want string
	}{
		{"an ordinary title", "Chicken Tikka Masala", "recipe-x", "chicken-tikka-masala.sref"},
		{"accents are kept", "Crème Brûlée", "recipe-x", "crème-brûlée.sref"},
		{"a title in another script", "麻婆豆腐", "recipe-x", "麻婆豆腐.sref"},
		{"separators cannot escape", "../../etc/passwd", "recipe-x", "etc-passwd.sref"},
		{"a windows path cannot escape", `C:\Windows\System32`, "recipe-x", "c-windows-system32.sref"},
		{"punctuation collapses", "Grandma's  ✽ Best   Pie!!!", "recipe-x", "grandma-s-best-pie.sref"},
		{"a title of only punctuation falls back", "!!!", "recipe-x", "recipe-x.sref"},
		{"nothing usable at all still names the file", "", "", "recipe.sref"},
		{"a leading dot cannot make a hidden file", "...hidden", "recipe-x", "hidden.sref"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := export.Filename(testCase.title, testCase.fallback, ".sref")
			if got != testCase.want {
				t.Fatalf("Filename(%q) = %q, want %q", testCase.title, got, testCase.want)
			}

			for _, forbidden := range []string{"/", `\`, "..", ":"} {
				if strings.Contains(strings.TrimSuffix(got, ".sref"), forbidden) {
					t.Fatalf("%q contains %q", got, forbidden)
				}
			}
		})
	}
}

func TestAVeryLongTitleIsCutRatherThanRefused(t *testing.T) {
	t.Parallel()

	name := export.Filename(strings.Repeat("a very long recipe title ", 20), "", ".sref")
	if len(name) > 80 {
		t.Fatalf("filename is %d bytes: %q", len(name), name)
	}

	if !strings.HasSuffix(name, ".sref") || strings.HasSuffix(name, "-.sref") {
		t.Fatalf("filename %q", name)
	}
}

func TestAFormatThatCannotCarryTheRequestIsRefused(t *testing.T) {
	t.Parallel()

	recipes := newLibrary()
	recipes.put("entry-a", 1, plainDocument())
	exporter := service(t, recipes, newAssets())

	_, err := exporter.Prepare(context.Background(),
		export.NewSelectionSource(recipes, []library.EntryID{"entry-a"}), export.FormatSREF)
	if !errors.Is(err, export.ErrUnsupportedFormat) {
		t.Fatalf("a collection asked for as a single recipe: %v", err)
	}

	_, err = exporter.Prepare(context.Background(),
		export.NewRecipeSource(recipes, "entry-a"), export.FormatSREFBundle)
	if !errors.Is(err, export.ErrUnsupportedFormat) {
		t.Fatalf("one recipe asked for as a bundle: %v", err)
	}

	_, err = exporter.Prepare(context.Background(),
		export.NewRecipeSource(recipes, "entry-a"), export.Format("paprika"))
	if !errors.Is(err, export.ErrUnsupportedFormat) {
		t.Fatalf("an unknown format: %v", err)
	}
}

func TestTheOfferedFormatsAreTheOnesThatExist(t *testing.T) {
	t.Parallel()

	formats := export.Formats()
	if len(formats) != 2 {
		t.Fatalf("offered formats: %#v", formats)
	}

	if !formats[0].SupportsSingle || formats[0].SupportsCollection {
		t.Fatal("SREF is the single-recipe format")
	}

	if formats[1].SupportsSingle || !formats[1].SupportsCollection {
		t.Fatal("SREF Bundle is the collection format")
	}

	for _, descriptor := range formats {
		if descriptor.Label == "" || descriptor.Extension == "" || descriptor.MediaType == "" {
			t.Fatalf("incomplete descriptor: %#v", descriptor)
		}
	}
}
