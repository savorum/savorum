package export_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/sref/bundle"
)

// openBundle reads a bundle the way anything that is not Savorum would: as a
// ZIP with a manifest in it.
func openBundle(t *testing.T, data []byte) (bundle.Manifest, map[string][]byte, []string) {
	t.Helper()

	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("the bundle is not a readable archive: %v", err)
	}

	contents := map[string][]byte{}

	names := make([]string, 0, len(archive.File))
	for _, entry := range archive.File {
		names = append(names, entry.Name)

		opened, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}

		body, err := io.ReadAll(opened)
		_ = opened.Close()

		if err != nil {
			t.Fatal(err)
		}

		contents[entry.Name] = body
	}

	manifestBytes, present := contents[bundle.ManifestPath]
	if !present {
		t.Fatalf("the bundle has no manifest; it holds %v", names)
	}

	var manifest bundle.Manifest

	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("the manifest could not be read: %v", err)
	}

	return manifest, contents, names
}

// threeRecipes fills a library with recipes that differ, so an ordering
// assertion means something.
func threeRecipes(t *testing.T) (*fakeLibrary, *fakeAssets) {
	t.Helper()

	recipes := newLibrary()
	blobs := newAssets()
	image := []byte("a photograph shared by two recipes")
	digest := blobs.hold(image)

	for index, name := range []string{"apple-pie", "chicken-tikka", "sourdough"} {
		document := plainDocument()
		document.ID = recipe.RecipeID(name)

		document.Title = strings.ToUpper(name[:1]) + name[1:]
		if index != 1 {
			// The same photograph in two recipes travels twice. Independence
			// is worth more than the bytes deduplication would save.
			document.Assets = []recipe.Asset{{
				ID: "hero", Path: "assets/hero.jpg", MediaType: "image/jpeg",
				SHA256: digest, Size: int64(len(image)), Role: recipe.AssetRoleImage,
			}}
			document.ImageRefs = []string{"hero"}
			document.PrimaryImage = "hero"
		}

		recipes.put(library.EntryID("entry-"+name), uint64(index+1), document)
	}

	return recipes, blobs
}

func TestABundleIsOrderedAndDeclaresExactlyWhatItCarries(t *testing.T) {
	t.Parallel()
	recipes, blobs := threeRecipes(t)
	exporter := service(t, recipes, blobs)
	// The order asked for is the order the bundle means, not alphabetical and
	// not the library's.
	selected := []library.EntryID{"entry-sourdough", "entry-apple-pie", "entry-chicken-tikka"}

	data, plan := exportOne(t, exporter,
		export.NewSelectionSource(recipes, selected), export.FormatSREFBundle)
	if plan.MediaType != "application/zip" || !strings.HasSuffix(plan.Filename, ".srefbundle") {
		t.Fatalf("bundle plan = %#v", plan)
	}

	manifest, contents, names := openBundle(t, data)
	if manifest.Format != "sref-bundle" || manifest.Version != 1 {
		t.Fatalf("manifest identifies %q version %d", manifest.Format, manifest.Version)
	}

	wantOrder := []string{"sourdough", "apple-pie", "chicken-tikka"}
	for index, want := range wantOrder {
		if manifest.Recipes[index].RecipeID != want {
			t.Fatalf("member %d is %q, want %q", index, manifest.Recipes[index].RecipeID, want)
		}
	}

	declared := map[string]struct{}{bundle.ManifestPath: {}}
	seenMembers := map[string]struct{}{}

	for index, entry := range manifest.Recipes {
		if entry.MemberID != fmt.Sprintf("r%06d", index+1) {
			t.Fatalf("member %d has transport identity %q", index, entry.MemberID)
		}

		if entry.Path != bundle.MemberPath(entry.MemberID) {
			t.Fatalf("member path %q is not named after member %q", entry.Path, entry.MemberID)
		}

		if _, repeated := declared[entry.Path]; repeated {
			t.Fatalf("member path %q declared twice", entry.Path)
		}

		if _, repeated := seenMembers[entry.MemberID]; repeated {
			t.Fatalf("member %q declared twice", entry.MemberID)
		}

		declared[entry.Path] = struct{}{}
		seenMembers[entry.MemberID] = struct{}{}

		member, carried := contents[entry.Path]
		if !carried {
			t.Fatalf("the manifest declares %q and the bundle does not carry it", entry.Path)
		}

		if int64(len(member)) != entry.Size {
			t.Fatalf("%q is %d bytes and the manifest says %d", entry.Path, len(member), entry.Size)
		}

		sum := sha256.Sum256(member)
		if hex.EncodeToString(sum[:]) != entry.SHA256 {
			t.Fatalf("%q does not match its declared digest", entry.Path)
		}
	}
	// Version 1 carries the manifest and the members, and nothing else.
	for _, name := range names {
		if _, expected := declared[name]; !expected {
			t.Fatalf("the bundle carries undeclared entry %q", name)
		}
	}
}

func TestEveryBundleMemberIsTheStandaloneExportOfThatRecipe(t *testing.T) {
	t.Parallel()
	recipes, blobs := threeRecipes(t)
	exporter := service(t, recipes, blobs)
	selected := []library.EntryID{"entry-apple-pie", "entry-chicken-tikka", "entry-sourdough"}

	data, _ := exportOne(t, exporter, export.NewSelectionSource(recipes, selected), export.FormatSREFBundle)
	manifest, contents, _ := openBundle(t, data)

	for _, id := range selected {
		alone, _ := exportOne(t, exporter, export.NewRecipeSource(recipes, id), export.FormatSREF)
		document := recipes.entries[id].Recipe

		var member []byte

		for _, entry := range manifest.Recipes {
			if entry.RecipeID == string(document.ID) {
				member = contents[entry.Path]
				break
			}
		}

		if !bytes.Equal(member, alone) {
			t.Fatalf("the bundle member for %q is not the file exporting it alone produces", document.ID)
		}
		// The member has to stand on its own: taken out of the bundle it is an
		// ordinary package, with its own manifest and its own photographs.
		opened, err := sref.ReadPackage(member, sref.DefaultLimits())
		if err != nil {
			t.Fatalf("an extracted member is not a valid package: %v", err)
		}

		if opened.Recipe.ID != document.ID {
			t.Fatalf("member carries %q, want %q", opened.Recipe.ID, document.ID)
		}

		if len(opened.Assets) != len(document.Assets) {
			t.Fatalf("an extracted member lost its photographs")
		}
	}
}

func TestABundleAvoidsUnnecessaryNondeterminismForExactRecipes(t *testing.T) {
	t.Parallel()
	recipes, blobs := threeRecipes(t)
	exporter := service(t, recipes, blobs)
	selected := []library.EntryID{"entry-apple-pie", "entry-sourdough"}

	first, _ := exportOne(t, exporter, export.NewSelectionSource(recipes, selected), export.FormatSREFBundle)

	second, _ := exportOne(t, exporter, export.NewSelectionSource(recipes, selected), export.FormatSREFBundle)
	if !bytes.Equal(first, second) {
		t.Fatal("two exports of the same recipes in the same order produced different bytes")
	}
	// Nothing dated is recorded. A digest identifies these exact artifact
	// bytes; it does not establish canonical or semantic bundle identity.
	manifest, _, _ := openBundle(t, first)

	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	for _, dated := range []string{"generated", "created", "exported_at", "savorum"} {
		if strings.Contains(strings.ToLower(string(encoded)), dated) {
			t.Fatalf("the manifest records %q", dated)
		}
	}

	// A different order is a different bundle: order is what the manifest is
	// for.
	reversed, _ := exportOne(t, exporter,
		export.NewSelectionSource(recipes, []library.EntryID{"entry-sourdough", "entry-apple-pie"}),
		export.FormatSREFBundle)
	if bytes.Equal(first, reversed) {
		t.Fatal("reordering the selection did not change the bundle")
	}
}

func TestABundleCarriesNothingAboutHowTheLibraryIsOrganized(t *testing.T) {
	t.Parallel()
	recipes, blobs := threeRecipes(t)
	exporter := service(t, recipes, blobs)
	data, _ := exportOne(t, exporter,
		export.NewSelectionSource(recipes, []library.EntryID{"entry-apple-pie"}), export.FormatSREFBundle)

	manifest, _, _ := openBundle(t, data)

	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	for _, local := range []string{"entry-apple-pie", "book", "tag", "rating", "favorite", "revision", "cooked"} {
		if strings.Contains(string(encoded), local) {
			t.Fatalf("the bundle manifest carries %q", local)
		}
	}
}

func TestOneFailingRecipeFailsTheWholeBundle(t *testing.T) {
	t.Parallel()
	recipes, blobs := threeRecipes(t)
	// The photograph two of the three recipes reference is no longer here.
	blobs.bytes = map[string][]byte{}
	exporter := service(t, recipes, blobs)

	buffer := &bytes.Buffer{}

	_, err := exporter.Export(context.Background(), export.NewSelectionSource(recipes,
		[]library.EntryID{"entry-apple-pie", "entry-chicken-tikka", "entry-sourdough"}),
		export.FormatSREFBundle, buffer)
	if !errors.Is(err, export.ErrAssetUnavailable) {
		t.Fatalf("error = %v, want the bundle to fail", err)
	}

	if buffer.Len() != 0 {
		t.Fatal("a partial bundle was written")
	}

	var unavailable *export.AssetError
	if !errors.As(err, &unavailable) || unavailable.RecipeTitle == "" {
		t.Fatalf("the failure does not name the recipe that failed: %v", err)
	}
}

func TestAnEmptySelectionIsRefused(t *testing.T) {
	t.Parallel()
	recipes, blobs := threeRecipes(t)
	exporter := service(t, recipes, blobs)

	_, err := exporter.Prepare(context.Background(),
		export.NewSelectionSource(recipes, nil), export.FormatSREFBundle)
	if !errors.Is(err, export.ErrNoRecipes) {
		t.Fatalf("error = %v, want an empty bundle to be refused", err)
	}
}

func TestASelectionBeyondTheLimitIsRefusedRatherThanTruncated(t *testing.T) {
	t.Parallel()

	recipes := newLibrary()

	ids := []library.EntryID{"entry-first", "entry-second"}
	for index, id := range ids {
		document := plainDocument()
		document.ID = recipe.RecipeID(fmt.Sprintf("recipe-%d", index+1))
		recipes.put(id, 1, document)
	}

	exporter, err := export.NewService(recipes, newAssets(), export.WithLimits(export.Limits{CollectionRecipes: 1}))
	if err != nil {
		t.Fatal(err)
	}

	_, err = exporter.Prepare(context.Background(),
		export.NewSelectionSource(recipes, ids), export.FormatSREFBundle)
	if !errors.Is(err, export.ErrTooLarge) {
		t.Fatalf("error = %v, want the selection to be refused", err)
	}
}

func TestSelectionSourceRejectsRawInvalidInputsBeforeLookupOrAllocation(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		ids  []library.EntryID
		want error
	}{
		{"251 repeated", repeatedEntryIDs("entry", 251), export.ErrTooLarge},
		{"251 unique", uniqueEntryIDs(251), export.ErrTooLarge},
		{"blank", []library.EntryID{"entry", "   "}, export.ErrInvalidSelection},
		{"long", []library.EntryID{library.EntryID(strings.Repeat("x", export.MaximumEntryIDLength+1))}, export.ErrInvalidSelection},
		{"nonexistent after valid preflight", []library.EntryID{"missing"}, library.ErrNotFound},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recipes := newLibrary()

			_, err := export.NewSelectionSource(recipes, testCase.ids).Resolve(context.Background())
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error=%v want=%v", err, testCase.want)
			}

			if !errors.Is(testCase.want, library.ErrNotFound) && recipes.reads != 0 {
				t.Fatalf("invalid selection performed %d library reads", recipes.reads)
			}
		})
	}
}

func TestOversizedSelectionFailureHasBoundedAllocations(t *testing.T) {
	ids := repeatedEntryIDs("x", 250_000)
	recipes := newLibrary()
	source := export.NewSelectionSource(recipes, ids)

	allocations := testing.AllocsPerRun(100, func() {
		_, err := source.Resolve(context.Background())
		if !errors.Is(err, export.ErrTooLarge) {
			panic(err)
		}
	})
	if allocations > 8 {
		t.Fatalf("oversized rejection allocated %.1f objects per call", allocations)
	}

	if recipes.reads != 0 {
		t.Fatalf("oversized selection performed %d reads", recipes.reads)
	}
}

func repeatedEntryIDs(id library.EntryID, count int) []library.EntryID {
	result := make([]library.EntryID, count)
	for index := range result {
		result[index] = id
	}

	return result
}

func uniqueEntryIDs(count int) []library.EntryID {
	result := make([]library.EntryID, count)
	for index := range result {
		result[index] = library.EntryID(fmt.Sprintf("entry-%d", index))
	}

	return result
}

func TestPrepareCountsLogicalAssetOccurrencesButVerifiesEachDigestOnce(t *testing.T) {
	t.Parallel()

	recipes := newLibrary()
	blobs := newAssets()
	content := []byte("one immutable photograph")
	digest := blobs.hold(content)

	ids := make([]library.EntryID, 250)
	for index := range ids {
		document := plainDocument()
		document.ID = recipe.RecipeID(fmt.Sprintf("recipe-%03d", index))
		document.Assets = []recipe.Asset{{
			ID: "hero", Path: "assets/hero.jpg", MediaType: "image/jpeg",
			SHA256: digest, Size: int64(len(content)), Role: recipe.AssetRoleImage,
		}}
		document.ImageRefs, document.PrimaryImage = []string{"hero"}, "hero"
		ids[index] = library.EntryID(fmt.Sprintf("entry-%03d", index))
		recipes.put(ids[index], 1, document)
	}

	exporter := service(t, recipes, blobs)
	if _, err := exporter.Prepare(context.Background(), export.NewSelectionSource(recipes, ids), export.FormatSREFBundle); err != nil {
		t.Fatal(err)
	}

	if blobs.opens[digest] != 1 {
		t.Fatalf("shared digest was verified %d times, want once", blobs.opens[digest])
	}
}

func TestPrepareRefusesRepeatedLogicalBytesBeforeOpeningAssets(t *testing.T) {
	t.Parallel()

	recipes := newLibrary()
	blobs := newAssets()
	content := []byte("shared bytes")
	digest := blobs.hold(content)
	document := plainDocument()
	document.Assets = []recipe.Asset{
		{ID: "first", Path: "assets/first.jpg", MediaType: "image/jpeg", SHA256: digest, Size: int64(len(content)), Role: recipe.AssetRoleImage},
		{ID: "second", Path: "assets/second.jpg", MediaType: "image/jpeg", SHA256: digest, Size: int64(len(content)), Role: recipe.AssetRoleImage},
	}
	recipes.put("entry", 1, document)

	exporter, err := export.NewService(recipes, blobs, export.WithLimits(export.Limits{LogicalAssetBytes: int64(len(content))}))
	if err != nil {
		t.Fatal(err)
	}

	_, err = exporter.Prepare(context.Background(), export.NewRecipeSource(recipes, "entry"), export.FormatSREF)
	if !errors.Is(err, export.ErrTooLarge) {
		t.Fatalf("error = %v, want logical byte limit", err)
	}

	if blobs.opens[digest] != 0 {
		t.Fatalf("asset was opened %d times after preflight refusal", blobs.opens[digest])
	}
}

func TestWriteHonorsCancellationBeforeEmittingBytes(t *testing.T) {
	t.Parallel()

	recipes := newLibrary()
	document := plainDocument()
	recipes.put("entry", 1, document)
	exporter := service(t, recipes, newAssets())

	plan, err := exporter.Prepare(context.Background(), export.NewRecipeSource(recipes, "entry"), export.FormatSREF)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	buffer := &bytes.Buffer{}
	if err := exporter.Write(ctx, plan, buffer); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}

	if buffer.Len() != 0 {
		t.Fatalf("canceled export wrote %d bytes", buffer.Len())
	}
}

func TestABookExportsItsRecipesInBookOrder(t *testing.T) {
	t.Parallel()
	recipes, blobs := threeRecipes(t)
	// A trashed recipe is retained membership rather than part of the book a
	// reader would export today.
	trashed := plainDocument()
	trashed.ID = "removed-recipe"
	recipes.put("entry-removed", 1, trashed)
	recipes.books["book-family"] = library.Book{
		ID:    "book-family",
		Title: "Family favourites",
		Sections: []library.BookSection{
			{ID: "mains", Title: "Mains", Position: 1},
			{ID: "puddings", Title: "Puddings", Position: 2},
		},
		Entries: []library.BookEntry{
			{ID: "e3", SectionID: "puddings", EntryID: "entry-apple-pie", Position: 1, State: library.StateActive},
			{ID: "e1", SectionID: "mains", EntryID: "entry-chicken-tikka", Position: 1, State: library.StateActive},
			{ID: "e2", SectionID: "mains", EntryID: "entry-removed", Position: 2, State: library.StateTrash},
			{ID: "e0", EntryID: "entry-sourdough", Position: 1, State: library.StateActive},
		},
	}
	exporter := service(t, recipes, blobs)

	data, plan := exportOne(t, exporter,
		export.NewBookSource(recipes, "book-family"), export.FormatSREFBundle)
	if plan.Filename != "family-favourites.srefbundle" {
		t.Fatalf("filename = %q", plan.Filename)
	}

	manifest, _, _ := openBundle(t, data)
	// Outside every chapter first, then chapter by chapter, exactly as the
	// book reads.
	want := []string{"sourdough", "chicken-tikka", "apple-pie"}
	if len(manifest.Recipes) != len(want) {
		t.Fatalf("the bundle holds %d recipes, want %d", len(manifest.Recipes), len(want))
	}

	for index, id := range want {
		if manifest.Recipes[index].RecipeID != id {
			t.Fatalf("member %d is %q, want %q", index, manifest.Recipes[index].RecipeID, id)
		}
	}

	// The book itself does not travel.
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	for _, local := range []string{"book-family", "mains", "puddings", "Family favourites"} {
		if strings.Contains(string(encoded), local) {
			t.Fatalf("the bundle carries the book's %q", local)
		}
	}
}

func TestAnEmptyBookIsRefusedRatherThanExportedEmpty(t *testing.T) {
	t.Parallel()
	recipes, blobs := threeRecipes(t)
	recipes.books["book-empty"] = library.Book{ID: "book-empty", Title: "Nothing yet"}

	_, err := service(t, recipes, blobs).Prepare(context.Background(),
		export.NewBookSource(recipes, "book-empty"), export.FormatSREFBundle)
	if !errors.Is(err, export.ErrNoRecipes) {
		t.Fatalf("error = %v, want an empty book to be refused", err)
	}
}

func TestABundleFreezesRevisionsBeforeItIsWritten(t *testing.T) {
	t.Parallel()
	recipes, blobs := threeRecipes(t)
	exporter := service(t, recipes, blobs)
	selected := []library.EntryID{"entry-apple-pie", "entry-sourdough"}

	plan, err := exporter.Prepare(context.Background(),
		export.NewSelectionSource(recipes, selected), export.FormatSREFBundle)
	if err != nil {
		t.Fatal(err)
	}
	// Everything the bundle will contain is already resolved. An edit landing
	// now belongs to the next export, not to this one.
	changed := plainDocument()
	changed.ID = "apple-pie"
	changed.Title = "Edited while the bundle was being written"
	recipes.put("entry-apple-pie", 99, changed)
	readsBefore := recipes.reads

	buffer := &bytes.Buffer{}
	if err := exporter.Write(context.Background(), plan, buffer); err != nil {
		t.Fatal(err)
	}

	if recipes.reads != readsBefore {
		t.Fatalf("writing the bundle read the library %d more times", recipes.reads-readsBefore)
	}

	manifest, contents, _ := openBundle(t, buffer.Bytes())

	opened, err := sref.ReadPackage(contents[manifest.Recipes[0].Path], sref.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}

	if opened.Recipe.Title == changed.Title {
		t.Fatal("the bundle picked up an edit made after its revisions were frozen")
	}
}

func TestBundleExportKeepsDuplicateRecipeIDsInIndependentMembers(t *testing.T) {
	t.Parallel()

	recipes := newLibrary()
	first := plainDocument()
	first.ID, first.Title = "shared-portable-id", "First recipe"
	second := plainDocument()
	second.ID, second.Title = "shared-portable-id", "Second recipe"

	recipes.put("entry-first", 1, first)
	recipes.put("entry-second", 1, second)

	data, _ := exportOne(t, service(t, recipes, newAssets()),
		export.NewSelectionSource(recipes, []library.EntryID{"entry-first", "entry-second"}),
		export.FormatSREFBundle)

	manifest, contents, _ := openBundle(t, data)
	if len(manifest.Recipes) != 2 || manifest.Recipes[0].RecipeID != "shared-portable-id" ||
		manifest.Recipes[1].RecipeID != "shared-portable-id" ||
		manifest.Recipes[0].MemberID == manifest.Recipes[1].MemberID {
		t.Fatalf("duplicate recipe IDs = %#v", manifest.Recipes)
	}

	for index, entry := range manifest.Recipes {
		packaged, err := sref.ReadPackage(contents[entry.Path], sref.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}

		if packaged.Recipe.ID != "shared-portable-id" {
			t.Fatalf("member %d rewrote recipe ID to %q", index, packaged.Recipe.ID)
		}
	}
}
