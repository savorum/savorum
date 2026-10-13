//go:build !race

package typst_test

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"unicode"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/preferences"
	"github.com/savorum/savorum/internal/print"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/render/pdfinspect"
	"github.com/savorum/savorum/internal/render/typst"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

// The compiler is prepared once for the whole package. Preparing it is the
// expensive part of printing; typesetting a document is not.
var (
	shared     *typst.Renderer
	sharedOnce sync.Once
)

func renderer(t *testing.T) *typst.Renderer {
	t.Helper()
	sharedOnce.Do(func() { shared = typst.New() })

	if err := shared.Warm(context.Background()); err != nil {
		t.Fatalf("prepare renderer: %v", err)
	}

	return shared
}

type stubLibrary struct {
	documents map[library.EntryID]recipe.Document
	preferred map[library.EntryID]recipe.Rational
	revisions map[library.EntryID]uint64
	books     map[string]library.Book
}

func newStubLibrary(documents ...recipe.Document) *stubLibrary {
	stub := &stubLibrary{
		documents: map[library.EntryID]recipe.Document{},
		preferred: map[library.EntryID]recipe.Rational{},
		revisions: map[library.EntryID]uint64{},
		books:     map[string]library.Book{},
	}
	for index, document := range documents {
		id := library.EntryID(document.ID)
		stub.documents[id] = document
		stub.revisions[id] = uint64(index + 1)
	}

	return stub
}

func (s *stubLibrary) Get(_ context.Context, id library.EntryID) (library.Entry, error) {
	document, found := s.documents[id]
	if !found {
		return library.Entry{}, library.ErrNotFound
	}

	return library.Entry{ID: id, Recipe: document, Revision: s.revisions[id]}, nil
}

func (s *stubLibrary) GetOrganization(_ context.Context, recipeID string) (library.Organization, error) {
	organization := library.Organization{EntryID: library.EntryID(recipeID)}
	if value, found := s.preferred[library.EntryID(recipeID)]; found {
		copied := value
		organization.State.PreferredScale = &copied
	}

	return organization, nil
}

func (s *stubLibrary) GetBook(_ context.Context, id string) (library.Book, error) {
	book, found := s.books[id]
	if !found {
		return library.Book{}, library.ErrNotFound
	}

	return book, nil
}

type stubAssets struct{ blobs map[assets.ID][]byte }

func (s *stubAssets) Open(_ context.Context, id assets.ID) (assets.Blob, io.ReadCloser, error) {
	payload, found := s.blobs[id]
	if !found {
		return assets.Blob{}, nil, assets.ErrNotFound
	}

	return assets.Blob{ID: id, Size: int64(len(payload))},
		io.NopCloser(bytes.NewReader(payload)), nil
}

func service(t *testing.T, opener print.AssetOpener, documents ...recipe.Document) (*print.Service, *stubLibrary) {
	t.Helper()

	stub := newStubLibrary(documents...)

	printer, err := print.NewService(renderer(t), units.DefaultCatalog(), opener, stub, stub)
	if err != nil {
		t.Fatalf("compose print service: %v", err)
	}

	return printer, stub
}

func render(t *testing.T, printer *print.Service, source print.PrintSource, options print.Options) print.Artifact {
	t.Helper()

	artifact, err := printer.Render(context.Background(), print.Request{Source: source, Options: options})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	return artifact
}

func renderOne(t *testing.T, document recipe.Document, options print.Options) print.Artifact {
	t.Helper()
	printer, stub := service(t, nil, document)

	return render(t, printer, print.NewRecipeSource(stub, library.EntryID(document.ID)), options)
}

func optionsFor(size print.Size) print.Options {
	options := print.DefaultOptions()
	options.Size = size

	return options
}

func corpus(t *testing.T, title string) recipe.Document {
	t.Helper()

	for _, document := range testfixture.PrintCorpus() {
		if document.Title == title {
			return document
		}
	}

	t.Fatalf("the print corpus has no recipe titled %q", title)

	return recipe.Document{}
}

func documentText(t *testing.T, artifact print.Artifact) string {
	t.Helper()

	text, err := pdfinspect.Text(artifact.PDF)
	if err != nil {
		t.Fatalf("read the generated document: %v", err)
	}

	return text
}

// requireText fails naming what went missing, because the point of these tests
// is that a recipe survives being typeset rather than that a PDF parses.
func requireText(t *testing.T, printed, wanted string) {
	t.Helper()

	if !printedContains(printed, wanted) {
		t.Errorf("the printed document does not contain %q", wanted)
	}
}

// printedContains reports whether printed text holds wanted. Right-to-left text
// is extracted glyph by glyph in drawing order, and a glyph drawn as a letter
// with separate dots maps back to a different letter, so only its Latin words
// and the presence of its script can be checked.
func printedContains(printed, wanted string) bool {
	if recipe.TextDirection(wanted) != recipe.RightToLeft {
		return strings.Contains(printed, wanted)
	}

	for _, word := range strings.FieldsFunc(wanted, func(character rune) bool {
		return !unicode.In(character, unicode.Latin)
	}) {
		if !strings.Contains(printed, word) {
			return false
		}
	}

	return strings.ContainsFunc(printed, func(character rune) bool {
		return unicode.In(character, unicode.Arabic, unicode.Hebrew)
	})
}

func TestEveryFormatPrintsAtItsExactPhysicalSize(t *testing.T) {
	document := corpus(t, "Chicken tikka masala")

	cases := []struct {
		size          print.Size
		width, height float64
	}{
		{print.SizeLetter, 612, 792},
		{print.SizeA4, 595.276, 841.89},
		{print.SizeCard35, 360, 216},
		{print.SizeCard46, 432, 288},
		{print.SizeCard58, 576, 360},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.size), func(t *testing.T) {
			artifact := renderOne(t, document, optionsFor(testCase.size))

			pages, err := pdfinspect.Pages(artifact.PDF)
			if err != nil {
				t.Fatalf("read the generated pages: %v", err)
			}

			if len(pages) == 0 {
				t.Fatal("the document has no pages")
			}

			for index, page := range pages {
				if math.Abs(page.Width-testCase.width) > 0.05 || math.Abs(page.Height-testCase.height) > 0.05 {
					t.Errorf("page %d is %.3f by %.3f pt, want %.3f by %.3f",
						index+1, page.Width, page.Height, testCase.width, testCase.height)
				}
			}
		})
	}
}

func TestAShortRecipeFitsOnOneCard(t *testing.T) {
	artifact := renderOne(t, corpus(t, "Buttered toast"), optionsFor(print.SizeCard46))
	if artifact.Pages != 1 {
		t.Fatalf("a two-ingredient recipe took %d cards", artifact.Pages)
	}

	printed := documentText(t, artifact)
	requireText(t, printed, "thick-cut bread")
	requireText(t, printed, "Butter it while it is still hot.")
}

func TestALongRecipeContinuesOntoFurtherCardsWithoutLosingAnything(t *testing.T) {
	document := corpus(t, "Country sourdough")

	artifact := renderOne(t, document, optionsFor(print.SizeCard35))
	if artifact.Pages < 2 {
		t.Fatalf("a four-day bread recipe fitted on %d card(s), which cannot be right", artifact.Pages)
	}

	printed := documentText(t, artifact)
	assertRecipeSurvived(t, printed, document)
	// Every card says which card it is and how many there are.
	for index := 1; index <= artifact.Pages; index++ {
		requireText(t, printed, fmt.Sprintf("%d / %d", index, artifact.Pages))
	}
}

func TestALongTitleIsPrintedInFull(t *testing.T) {
	document := corpus(t, "Slow-roasted shoulder of lamb with preserved lemon, green olives, and a great deal of garlic")
	for _, size := range []print.Size{print.SizeCard35, print.SizeLetter} {
		printed := documentText(t, renderOne(t, document, optionsFor(size)))
		requireText(t, printed, "preserved lemon, green olives, and a great deal of garlic")
	}
}

func TestManyIngredientGroupsAllReachThePage(t *testing.T) {
	document := corpus(t, "Layered celebration cake")
	for _, size := range []print.Size{print.SizeLetter, print.SizeCard46} {
		t.Run(string(size), func(t *testing.T) {
			printed := documentText(t, renderOne(t, document, optionsFor(size)))
			assertRecipeSurvived(t, printed, document)
		})
	}
}

func TestAStepLargerThanACardIsDividedRatherThanLost(t *testing.T) {
	document := corpus(t, "Cassoulet")
	artifact := renderOne(t, document, optionsFor(print.SizeCard35))

	printed := documentText(t, artifact)
	if artifact.Pages < 2 {
		t.Fatalf("a step longer than a card fitted on %d card(s)", artifact.Pages)
	}
	// The step is divided, so it is checked in the pieces a cook would read
	// rather than as one string that a card break interrupts.
	requireText(t, printed, "Keep the surface barely trembling")
	requireText(t, printed, "the repeated crust rather than any single one of them.")
	requireText(t, printed, "Rest the finished dish for twenty minutes")
}

func TestNotesAndSourceArePrinted(t *testing.T) {
	document := corpus(t, "Grandmother's apple cake")
	printed := documentText(t, renderOne(t, document, optionsFor(print.SizeLetter)))
	requireText(t, printed, "It is better on the second day.")
	requireText(t, printed, "Family notebook")
	requireText(t, printed, "Ada Whitfield")
}

func TestCourseAndCuisineStayInThePrintedMetadataLine(t *testing.T) {
	document := corpus(t, "Buttered toast")
	document.Categories = []string{"Broad classification sentinel"}
	document.Courses = []string{"Breakfast", "Snack"}
	document.Cuisines = []string{"British"}
	document.CookingMethods = []string{"Method sentinel"}
	document.Keywords = []string{"Keyword sentinel"}
	printed := documentText(t, renderOne(t, document, optionsFor(print.SizeLetter)))
	requireText(t, printed, "Course Breakfast, Snack")
	requireText(t, printed, "Cuisine British")

	for _, deliberatelyOmitted := range []string{"Broad classification sentinel", "Method sentinel", "Keyword sentinel"} {
		if strings.Contains(printed, deliberatelyOmitted) {
			t.Errorf("print metadata became a broad classification header containing %q", deliberatelyOmitted)
		}
	}
}

func TestContentChoicesRemoveOnlyWhatTheyName(t *testing.T) {
	document := corpus(t, "Grandmother's apple cake")
	options := optionsFor(print.SizeLetter)
	options.Content = print.Content{}

	printed := documentText(t, renderOne(t, document, options))
	if strings.Contains(printed, "It is better on the second day.") {
		t.Error("a note was printed after notes were turned off")
	}

	if strings.Contains(printed, "Family notebook") {
		t.Error("the source was printed after it was turned off")
	}
	// What a cook cannot work without is never optional.
	assertCoreContentSurvived(t, printed, document)
}

func TestAScaledBatchIsPrintedWithoutChangingTheRecipe(t *testing.T) {
	document := corpus(t, "Grandmother's apple cake")
	options := optionsFor(print.SizeLetter)
	options.Scale = print.ScaleCustom
	options.CustomScale = "2"
	artifact := renderOne(t, document, options)
	printed := documentText(t, artifact)
	requireText(t, printed, "12 tart apples, peeled and sliced")
	requireText(t, printed, "4 cups all-purpose flour")
	requireText(t, printed, "20 servings")

	if document.IngredientSections[0].Ingredients[0].Quantity.Amount.Value.String() != "6" {
		t.Error("printing a batch changed the recipe it was printed from")
	}
}

func TestARememberedBatchIsResolvedPerRecipe(t *testing.T) {
	document := corpus(t, "Grandmother's apple cake")
	printer, stub := service(t, nil, document)
	stub.preferred[library.EntryID(document.ID)] = "3"
	options := optionsFor(print.SizeLetter)
	options.Scale = print.ScalePreferred
	printed := documentText(t, render(t, printer,
		print.NewRecipeSource(stub, library.EntryID(document.ID)), options))
	requireText(t, printed, "18 tart apples, peeled and sliced")
}

func TestAlternativeMeasurementsAreResolvedBeforeTypesetting(t *testing.T) {
	document := corpus(t, "Lemon drizzle loaf")
	options := optionsFor(print.SizeLetter)
	options.Measurement = units.MeasureUS
	printed := documentText(t, renderOne(t, document, options))
	// 225 g of butter is just under eight ounces, rounded to a quarter ounce.
	requireText(t, printed, "oz unsalted butter, softened")

	if strings.Contains(printed, "225g") {
		t.Error("an amount was printed in the measures the reader did not ask for")
	}
}

func TestTextOutsideASCIIIsPrintedAsWritten(t *testing.T) {
	document := corpus(t, "Crème brûlée à la vanille")
	printed := documentText(t, renderOne(t, document, optionsFor(print.SizeCard46)))
	requireText(t, printed, "Crème brûlée à la vanille")
	requireText(t, printed, "jaunes d'œufs")
	requireText(t, printed, "brûlez la surface au chalumeau")
}

func TestACancelledRequestStopsWaitingForItsDocument(t *testing.T) {
	document := corpus(t, "Country sourdough")
	printer, stub := service(t, nil, document)
	// The renderer is warmed by the shared instance, so this measures the
	// render rather than the one-off preparation of the compiler.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := printer.Render(ctx, print.Request{
		Source:  print.NewRecipeSource(stub, library.EntryID(document.ID)),
		Options: optionsFor(print.SizeLetter),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want the caller's own cancellation", err)
	}
}

// Without a Go memory limit, printing assumes a 768 MiB budget.
func TestRendererStatesAnExplicitWASMMemoryCeiling(t *testing.T) {
	if limit := typst.New().MemoryLimitBytes(); limit != 512<<20 {
		t.Fatalf("WASM memory limit = %d, want 512 MiB", limit)
	}
}

func TestCJKTextPrintsWithTheEmbeddedFallbackFont(t *testing.T) {
	document := corpus(t, "Buttered toast")
	document.Title = "Rendang 中文 日本語 한국어"
	printer, stub := service(t, nil, document)

	artifact, err := printer.Render(context.Background(), print.Request{
		Source:  print.NewRecipeSource(stub, library.EntryID(document.ID)),
		Options: optionsFor(print.SizeLetter),
	})
	if err != nil {
		t.Fatal(err)
	}

	requireText(t, documentText(t, artifact), document.Title)
}

func TestEmojiAndSymbolsSurvivePrintExtraction(t *testing.T) {
	document := corpus(t, "🍞 Toast by 👩🏽‍🍳")
	artifact := renderOne(t, document, optionsFor(print.SizeLetter))

	printed := documentText(t, artifact)
	for _, text := range []string{"👩🏽‍🍳", "☑", "☐", "🌶", "🍞"} {
		if !strings.Contains(printed, text) {
			t.Errorf("PDF text lost %q: %s", text, printed)
		}
	}
}

func TestEverySupportedPrintLocaleRendersItsLabelsAndKeepsAuthoredText(t *testing.T) {
	for _, locale := range preferences.SupportedLocales {
		t.Run(locale, func(t *testing.T) {
			document := corpus(t, "شاي بالنعناع")
			options := optionsFor(print.SizeLetter)
			options.Locale = locale
			artifact := renderOne(t, document, options)
			printed := documentText(t, artifact)
			requireText(t, printed, "Gunpowder")

			if locale != "en" && (strings.Contains(printed, "Ingredients") || strings.Contains(printed, "servings") || strings.Contains(printed, "bunch")) {
				t.Errorf("English application words in %s: %s", locale, printed)
			}

			if len(artifact.Warnings) != 0 {
				t.Fatalf("localized PDF warnings: %v", artifact.Warnings)
			}
		})
	}
}

func TestAPhotographIsPrintedFromTheResolvedRevision(t *testing.T) {
	document, opener := recipeWithPhotograph(t)
	options := optionsFor(print.SizeLetter)
	options.Image = print.ImageInclude
	printer, stub := service(t, opener, document)

	artifact := render(t, printer, print.NewRecipeSource(stub, library.EntryID(document.ID)), options)
	if len(artifact.Warnings) > 0 {
		t.Fatalf("printing a photograph warned: %v", artifact.Warnings)
	}

	if !bytes.Contains(artifact.PDF, []byte("/Subtype /Image")) {
		t.Error("the document embeds no image")
	}

	assertCoreContentSurvived(t, documentText(t, artifact), document)
}

func TestAMissingPhotographDegradesToAWarningRatherThanAFailure(t *testing.T) {
	document, _ := recipeWithPhotograph(t)
	options := optionsFor(print.SizeLetter)
	options.Image = print.ImageInclude
	printer, stub := service(t, &stubAssets{blobs: map[assets.ID][]byte{}}, document)

	artifact := render(t, printer, print.NewRecipeSource(stub, library.EntryID(document.ID)), options)
	if len(artifact.Warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", artifact.Warnings)
	}

	assertCoreContentSurvived(t, documentText(t, artifact), document)
}

func TestOmittingThePhotographPrintsTheRecipeWithoutIt(t *testing.T) {
	document, opener := recipeWithPhotograph(t)
	options := optionsFor(print.SizeLetter)
	options.Image = print.ImageOmit
	printer, stub := service(t, opener, document)

	artifact := render(t, printer, print.NewRecipeSource(stub, library.EntryID(document.ID)), options)
	if bytes.Contains(artifact.PDF, []byte("/Subtype /Image")) {
		t.Error("a photograph was printed after it was turned off")
	}
}

func TestACollectionPrintsEveryRecipeInTheOrderItWasSelected(t *testing.T) {
	documents := testfixture.PrintCorpus()
	printer, stub := service(t, nil, documents...)

	ids := make([]library.EntryID, 0, len(documents))
	for _, document := range documents {
		ids = append(ids, library.EntryID(document.ID))
	}

	artifact := render(t, printer, print.NewSelectionSource(stub, ids), optionsFor(print.SizeLetter))
	if artifact.Pages < len(documents) {
		t.Fatalf("a collection of %d recipes produced %d page(s)", len(documents), artifact.Pages)
	}

	pages, err := pdfinspect.Pages(artifact.PDF)
	if err != nil {
		t.Fatal(err)
	}

	// Every corpus recipe fits on one page, so the nth page holds the nth recipe.
	for index, document := range documents {
		if index >= len(pages) || !printedContains(pages[index].Text, document.Title) {
			t.Errorf("%q is missing from page %d of the collection", document.Title, index+1)
		}
	}
}

func TestANewRecipeDoesNotInheritThePreviousRecipesContinuedHeader(t *testing.T) {
	documents := testfixture.PrintCorpus()[:2]
	printer, stub := service(t, nil, documents...)
	ids := []library.EntryID{library.EntryID(documents[0].ID), library.EntryID(documents[1].ID)}

	artifact := render(t, printer, print.NewSelectionSource(stub, ids), optionsFor(print.SizeLetter))

	pages, err := pdfinspect.Pages(artifact.PDF)
	if err != nil {
		t.Fatalf("read the generated pages: %v", err)
	}

	if len(pages) != 2 {
		t.Fatalf("two one-page recipes produced %d pages, want 2", len(pages))
	}

	if strings.Contains(pages[1].Text, documents[0].Title+" continued") {
		t.Errorf("the second recipe inherited %q as a continued header", documents[0].Title)
	}

	requireText(t, pages[1].Text, documents[1].Title)
}

func TestACollectionRecordsTheRevisionOfEveryRecipeItFroze(t *testing.T) {
	documents := testfixture.PrintCorpus()[:3]
	printer, stub := service(t, nil, documents...)
	ids := []library.EntryID{
		library.EntryID(documents[2].ID),
		library.EntryID(documents[0].ID),
		library.EntryID(documents[1].ID),
	}

	artifact := render(t, printer, print.NewSelectionSource(stub, ids), optionsFor(print.SizeLetter))
	if len(artifact.Recipes) != 3 {
		t.Fatalf("the artifact records %d recipes, want 3", len(artifact.Recipes))
	}

	for index, id := range ids {
		if artifact.Recipes[index].EntryID != string(id) {
			t.Errorf("recipe %d is %q, want %q", index, artifact.Recipes[index].EntryID, id)
		}

		if artifact.Recipes[index].Revision != stub.revisions[id] {
			t.Errorf("recipe %q recorded revision %d, want %d",
				id, artifact.Recipes[index].Revision, stub.revisions[id])
		}
	}
}

func TestARecipeEditedAfterResolutionDoesNotChangeTheDocumentBeingGenerated(t *testing.T) {
	documents := testfixture.PrintCorpus()[:2]
	_, stub := service(t, nil, documents...)
	ids := []library.EntryID{library.EntryID(documents[0].ID), library.EntryID(documents[1].ID)}
	source := print.NewSelectionSource(stub, ids)

	snapshots, err := source.Resolve(context.Background(), print.DefaultCollectionRecipes)
	if err != nil {
		t.Fatalf("resolve the collection: %v", err)
	}
	// The library changes after the revisions were frozen. The document must
	// describe what was frozen, not a mixture of the two.
	changed := documents[1]
	changed.Title = "A different recipe entirely"
	stub.documents[ids[1]] = changed
	stub.revisions[ids[1]] = 99

	if snapshots[1].Recipe.Title != documents[1].Title {
		t.Fatal("a frozen snapshot changed under the renderer")
	}

	if snapshots[1].Revision == 99 {
		t.Fatal("a frozen snapshot took a later revision")
	}
}

func TestCardsAreImposedOnSheetsAtTheirExactSize(t *testing.T) {
	document := corpus(t, "Country sourdough")

	cases := []struct {
		output        print.Output
		size          print.Size
		width, height float64
	}{
		{print.OutputSheetLetter, print.SizeCard46, 612, 792},
		{print.OutputSheetA4, print.SizeCard46, 595.276, 841.89},
		{print.OutputSheetLetter, print.SizeCard35, 612, 792},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.size)+"-"+string(testCase.output), func(t *testing.T) {
			options := optionsFor(testCase.size)
			options.Output = testCase.output
			artifact := renderOne(t, document, options)

			pages, err := pdfinspect.Pages(artifact.PDF)
			if err != nil {
				t.Fatalf("read the generated sheets: %v", err)
			}

			for index, page := range pages {
				if math.Abs(page.Width-testCase.width) > 0.05 || math.Abs(page.Height-testCase.height) > 0.05 {
					t.Errorf("sheet %d is %.3f by %.3f pt, want %.3f by %.3f",
						index+1, page.Width, page.Height, testCase.width, testCase.height)
				}
			}

			if artifact.Format.Card == nil {
				t.Fatal("the artifact does not state the exact card size it imposed")
			}

			card := testCase.size.Dimensions()
			if artifact.Format.Card.Width != card.Width || artifact.Format.Card.Height != card.Height {
				t.Errorf("imposed card is %v by %v, want %v by %v",
					artifact.Format.Card.Width, artifact.Format.Card.Height, card.Width, card.Height)
			}

			assertRecipeSurvived(t, documentText(t, artifact), document)
		})
	}
}

func TestSheetsCarryTheSameCardsTheCardOutputDoes(t *testing.T) {
	document := corpus(t, "Country sourdough")
	native := renderOne(t, document, optionsFor(print.SizeCard46))
	options := optionsFor(print.SizeCard46)
	options.Output = print.OutputSheetLetter

	imposed := renderOne(t, document, options)
	if imposed.Pages > native.Pages {
		t.Errorf("imposing %d cards took %d sheets, which cannot be fewer cards per sheet than one",
			native.Pages, imposed.Pages)
	}

	nativeText := documentText(t, native)

	imposedText := documentText(t, imposed)
	for _, wanted := range []string{"strong white bread flour", "Score each loaf decisively"} {
		requireText(t, nativeText, wanted)
		requireText(t, imposedText, wanted)
	}
}

func TestABookPrintsInItsOwnOrder(t *testing.T) {
	documents := testfixture.PrintCorpus()[:3]
	printer, stub := service(t, nil, documents...)
	stub.books["weeknights"] = library.Book{
		ID:    "weeknights",
		Title: "Weeknights",
		Sections: []library.BookSection{
			{ID: "later", Title: "Later", Position: 2},
			{ID: "first", Title: "First", Position: 1},
		},
		Entries: []library.BookEntry{
			{ID: "c", SectionID: "later", EntryID: library.EntryID(documents[0].ID), Position: 1},
			{ID: "a", SectionID: "first", EntryID: library.EntryID(documents[2].ID), Position: 1},
			{ID: "b", SectionID: "first", EntryID: library.EntryID(documents[1].ID), Position: 2},
			{ID: "d", SectionID: "first", EntryID: library.EntryID(documents[0].ID), Position: 3, State: library.StateTrash},
		},
	}

	artifact := render(t, printer, print.NewBookSource(stub, stub, "weeknights"), optionsFor(print.SizeLetter))
	if artifact.Title != "Weeknights" {
		t.Errorf("the document is titled %q, want the book's own title", artifact.Title)
	}

	order := []string{documents[2].Title, documents[1].Title, documents[0].Title}
	printed := documentText(t, artifact)
	previous := -1

	for _, title := range order {
		position := strings.Index(printed, title)
		if position < 0 {
			t.Fatalf("%q is missing from the book", title)
		}

		if position < previous {
			t.Errorf("%q is out of the book's order", title)
		}

		previous = position
	}

	if len(artifact.Recipes) != 3 {
		t.Errorf("the book printed %d recipes, want 3 with the trashed membership left out", len(artifact.Recipes))
	}
}

func TestTheSameSpecificationProducesTheSameBytes(t *testing.T) {
	document := corpus(t, "Chicken tikka masala")
	first := renderOne(t, document, optionsFor(print.SizeCard46))

	second := renderOne(t, document, optionsFor(print.SizeCard46))
	if first.ID != second.ID {
		t.Errorf("signature = %q and %q, want one identifier for one specification", first.ID, second.ID)
	}

	if !bytes.Equal(first.PDF, second.PDF) {
		t.Error("the same recipe and the same options produced different bytes")
	}
}

func TestAGeneratedDocumentIsHeldForTheVisitThatMadeIt(t *testing.T) {
	document := corpus(t, "Chicken tikka masala")
	printer, stub := service(t, nil, document)
	artifact := render(t, printer, print.NewRecipeSource(stub, library.EntryID(document.ID)),
		optionsFor(print.SizeCard46))

	held, found, err := printer.Held(artifact.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !found {
		t.Fatal("the generated document was not held for the download that follows the preview")
	}

	if !bytes.Equal(held.PDF, artifact.PDF) {
		t.Error("the held document is not the document that was generated")
	}

	if _, found, err := printer.Held(strings.Repeat("0", 64)); err != nil || found {
		t.Error("an unknown identifier returned a document")
	}
}

func TestGeneratedDocumentsAreTaggedAndDescribeThemselves(t *testing.T) {
	document := corpus(t, "Buttered toast")

	artifact := renderOne(t, document, optionsFor(print.SizeLetter))
	for _, wanted := range []string{"/Marked true", "/Title (Buttered toast)", "/Lang (en-US)", "Savorum"} {
		if !bytes.Contains(artifact.PDF, []byte(wanted)) {
			t.Errorf("the generated document does not contain %q", wanted)
		}
	}
}

func TestNoDocumentEndsWithAnEmptyPage(t *testing.T) {
	documents := testfixture.PrintCorpus()

	sizes := []print.Size{print.SizeLetter, print.SizeCard35, print.SizeCard46, print.SizeCard58}
	for _, document := range documents {
		for _, size := range sizes {
			artifact := renderOne(t, document, optionsFor(size))

			pages, err := pdfinspect.Pages(artifact.PDF)
			if err != nil {
				t.Fatalf("%s as %s: read the generated pages: %v", document.Title, size, err)
			}

			last := pages[len(pages)-1]
			if strings.TrimSpace(last.Text) == "" {
				t.Errorf("%s as %s ends with a blank page", document.Title, size)
			}
		}
	}
}

func TestBodyTextNeverShrinksToForceARecipeOntoOneCard(t *testing.T) {
	// The same recipe is set on a small card and a large one. If the engine
	// were shrinking type to fit, the smaller card would hold proportionally
	// more of the recipe rather than simply running to more cards.
	document := corpus(t, "Country sourdough")
	small := renderOne(t, document, optionsFor(print.SizeCard35))

	large := renderOne(t, document, optionsFor(print.SizeCard58))
	if small.Pages <= large.Pages {
		t.Errorf("a 3x5 card took %d cards and a 5x8 card took %d; the type was shrunk rather than the recipe continued",
			small.Pages, large.Pages)
	}

	assertRecipeSurvived(t, documentText(t, small), document)
	assertRecipeSurvived(t, documentText(t, large), document)
}

func TestBothFullPageLayoutsKeepEveryIngredientAndStep(t *testing.T) {
	document := corpus(t, "Country sourdough")
	for _, layout := range []print.Layout{print.LayoutAutomatic, print.LayoutSideBySide, print.LayoutStacked} {
		t.Run(string(layout), func(t *testing.T) {
			options := optionsFor(print.SizeLetter)
			options.Layout = layout
			assertRecipeSurvived(t, documentText(t, renderOne(t, document, options)), document)
		})
	}
}

// assertRecipeSurvived checks that nothing a cook needs, and nothing optional
// that was asked for, went missing between the library and the page.
func assertRecipeSurvived(t *testing.T, printed string, document recipe.Document) {
	t.Helper()
	assertCoreContentSurvived(t, printed, document)

	for _, note := range document.Notes {
		requireText(t, printed, firstSentence(note))
	}
}

func assertCoreContentSurvived(t *testing.T, printed string, document recipe.Document) {
	t.Helper()
	requireText(t, printed, document.Title)

	for _, section := range document.IngredientSections {
		if section.Title != "" {
			requireText(t, printed, section.Title)
		}

		for _, ingredient := range section.Ingredients {
			requireText(t, printed, ingredient.Name)
		}
	}

	for _, section := range document.InstructionSections {
		if section.Title != "" {
			requireText(t, printed, section.Title)
		}

		for _, step := range section.Steps {
			requireText(t, printed, firstSentence(step.Text))
		}
	}
}

// firstSentence is what a step can be recognised by even when pagination has
// divided it across a card boundary.
func firstSentence(text string) string {
	if stop := strings.Index(text, "."); stop > 0 && stop+1 < len(text) {
		return text[:stop+1]
	}

	return text
}

// recipeWithPhotograph builds a recipe whose primary image this library holds
// bytes for, so image handling is exercised against real pixels.
func recipeWithPhotograph(t *testing.T) (recipe.Document, print.AssetOpener) {
	t.Helper()

	return recipeWithPhotographOfSize(t, 1600, 1200)
}

func recipeWithPhotographOfSize(t *testing.T, width, height int) (recipe.Document, print.AssetOpener) {
	t.Helper()

	picture := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := range height {
		for x := range width {
			picture.Set(x, y, color.RGBA{R: uint8(x % 251), G: uint8(y % 241), B: 120, A: 255})
		}
	}

	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, picture, nil); err != nil {
		t.Fatalf("encode the test photograph: %v", err)
	}

	const digest = "b7e2c1a4d5f60718293a4b5c6d7e8f900112233445566778899aabbccddeeff00"

	document := corpus(t, "Chicken tikka masala")
	document.Assets = []recipe.Asset{{
		ID: "hero", Path: "hero.jpg", MediaType: "image/jpeg",
		SHA256: digest, Size: int64(encoded.Len()), Role: recipe.AssetRoleImage,
		Alt: "The finished dish",
	}, {
		ID: "gallery", Path: "gallery.jpg", MediaType: "image/jpeg",
		SHA256: "a7e2c1a4d5f60718293a4b5c6d7e8f900112233445566778899aabbccddeeff01", Size: 1, Role: recipe.AssetRoleImage,
		Alt: "A preparation detail",
	}}
	document.PrimaryImage = "hero"
	document.ImageRefs = []string{"gallery", "hero"}

	return document, &stubAssets{blobs: map[assets.ID][]byte{
		assets.ID("sha256:" + digest): encoded.Bytes(),
	}}
}

// A photograph keeps the pixels its printed box needs and no more: about 300
// per inch where it is drawn, at every size. A page photograph can be drawn
// up to 10 percent smaller than its largest box.
func TestPhotographsArePrintedAtThePrintResolution(t *testing.T) {
	document, opener := recipeWithPhotographOfSize(t, 3000, 2250)

	for _, size := range []print.Size{print.SizeLetter, print.SizeA4, print.SizeCard35, print.SizeCard46, print.SizeCard58} {
		t.Run(string(size), func(t *testing.T) {
			options := optionsFor(size)
			options.Image = print.ImageInclude
			printer, stub := service(t, opener, document)

			artifact := render(t, printer, print.NewRecipeSource(stub, library.EntryID(document.ID)), options)

			placed, err := pdfinspect.PlacedImages(artifact.PDF)
			if err != nil {
				t.Fatal(err)
			}

			if len(placed) != 1 {
				t.Fatalf("the document draws %d images, want 1", len(placed))
			}

			drawn := placed[0]
			for _, resolution := range []float64{
				float64(drawn.Width) / (drawn.DrawnWidth / 72),
				float64(drawn.Height) / (drawn.DrawnHeight / 72),
			} {
				if resolution < 294 || resolution > 337 {
					t.Errorf("a %dx%d photograph drawn at %.1fx%.1f pt prints at %.0f pixels per inch",
						drawn.Width, drawn.Height, drawn.DrawnWidth, drawn.DrawnHeight, resolution)
				}
			}
		})
	}
}

func TestAShortRecipeGrowsIntoThePageItHas(t *testing.T) {
	// A two-ingredient recipe set at the size a four-day loaf needs leaves
	// most of a letter page empty for no reason. It is set larger, and the
	// long recipe it is compared against is not made smaller to allow it.
	short := renderOne(t, corpus(t, "Buttered toast"), optionsFor(print.SizeLetter))
	long := renderOne(t, corpus(t, "Country sourdough"), optionsFor(print.SizeLetter))
	shortBody := commonestTextSize(t, short.PDF)

	longBody := commonestTextSize(t, long.PDF)
	if shortBody <= longBody {
		t.Errorf("the short recipe is set at %.2fpt and the long one at %.2fpt; the empty page was not used",
			shortBody, longBody)
	}

	if longBody < baselineBodySize {
		t.Errorf("the long recipe is set at %.2fpt, below the %.2fpt baseline; type was shrunk to fit",
			longBody, baselineBodySize)
	}
}

func TestExpandingAPageNeverCostsTheCookAnother(t *testing.T) {
	// Growing a recipe into its page is only worth it while the recipe still
	// fits on that page, and never at the cost of any of its content.
	for _, size := range []print.Size{print.SizeLetter, print.SizeA4} {
		for _, document := range testfixture.PrintCorpus() {
			artifact := renderOne(t, document, optionsFor(size))
			if artifact.Pages > 1 {
				t.Errorf("%s as %s took %d pages; the corpus sets one recipe per page",
					document.Title, size, artifact.Pages)
			}

			body := commonestTextSize(t, artifact.PDF)
			if body < baselineBodySize {
				t.Errorf("%s as %s is set at %.2fpt, below the %.2fpt baseline",
					document.Title, size, body, baselineBodySize)
			}

			assertRecipeSurvived(t, documentText(t, artifact), document)
		}
	}
}

// baselineBodySize is the full-page body size the templates establish. No
// recipe is ever set below it; a page with room to spare is set above it.
const baselineBodySize = 10.5

// commonestTextSize is the size most of a document's text is drawn at, which
// is its body size rather than a heading or a caption.
func commonestTextSize(t *testing.T, pdf []byte) float64 {
	t.Helper()

	sizes, err := pdfinspect.TextSizes(pdf)
	if err != nil {
		t.Fatalf("read the type sizes: %v", err)
	}

	counts := map[float64]int{}
	for _, size := range sizes {
		counts[size]++
	}

	var (
		commonest float64
		best      int
	)
	for size, count := range counts {
		if count > best || (count == best && size > commonest) {
			commonest, best = size, count
		}
	}

	if best == 0 {
		t.Fatal("the document draws no text")
	}

	return commonest
}

func TestPrintedListsSeparateTheirRowsMoreThanTheirLines(t *testing.T) {
	// "Inside beats outside": within one ingredient, step, or note, lines sit
	// closer together than the space to the next one. Measured on the finished
	// page, because template numbers are not the distances that come out.
	document := corpus(t, "Country sourdough")
	// Keep one ingredient long enough to wrap even when the page layout gives
	// ingredients a generous reading measure. The assertion needs both an
	// intra-row line gap and a following inter-row gap to compare.
	document.IngredientSections[0].Ingredients[0].Name = "mature sourdough starter fed with wholegrain rye flour and kept at room temperature"
	// Not every list wraps on every format — a short note fits one line on a
	// wide card — so the rule is checked wherever it can be measured, and the
	// tally afterwards proves all three were measured somewhere.
	measured := map[string]int{}

	for _, size := range []print.Size{print.SizeLetter, print.SizeA4, print.SizeCard58} {
		t.Run(string(size), func(t *testing.T) {
			pages := allTextRuns(t, renderOne(t, document, optionsFor(size)))
			for _, runs := range pages {
				for _, list := range []struct {
					name    string
					leaders []string
				}{
					// Each row of a list is introduced by something in a column
					// of its own: a measure before an ingredient, a number
					// before a step. That marks where one row ends and the next
					// begins without the test needing to know the recipe.
					{"ingredients", []string{"40g", "120g", "120ml", "800g", "200g", "740ml", "22g", "240g"}},
					{"steps", []string{"1.", "2.", "3.", "4."}},
				} {
					inside, between := listGaps(runs, list.leaders)
					if len(inside) > 0 && len(between) > 0 {
						measured[list.name]++
						assertInsideBeatsOutside(t, list.name, inside, between)
					}
				}
				// Notes carry no marker of their own, so each is found by the
				// words it opens with.
				inside, between := paragraphGaps(runs, []string{
					"A cooler kitchen needs", "The loaf keeps for three days",
				})
				if len(inside) > 0 && len(between) > 0 {
					measured["notes"]++

					assertInsideBeatsOutside(t, "notes", inside, between)
				}
			}
		})
	}
	// A silent pass because nothing was found would defeat the point.
	for _, name := range []string{"ingredients", "steps", "notes"} {
		if measured[name] == 0 {
			t.Errorf("%s were never measured on any format; the rule is not being checked", name)
		}
	}
}

func assertInsideBeatsOutside(t *testing.T, name string, inside, between []float64) {
	t.Helper()

	widestInside := slices.Max(inside)

	closestBetween := slices.Min(between)
	if closestBetween <= widestInside {
		t.Errorf("%s: rows are %.2fpt apart at their closest and lines within a row are %.2fpt apart at their widest; one row reads as two",
			name, closestBetween, widestInside)
	}
}

// allTextRuns reads the drawn text of every page, a page at a time.
func allTextRuns(t *testing.T, artifact print.Artifact) [][]pdfinspect.TextRun {
	t.Helper()

	pages := make([][]pdfinspect.TextRun, 0, artifact.Pages)
	for page := 0; page < artifact.Pages; page++ {
		runs, err := pdfinspect.TextRuns(artifact.PDF, page)
		if err != nil {
			t.Fatalf("read the drawn text of page %d: %v", page+1, err)
		}

		pages = append(pages, runs)
	}

	return pages
}

const sameColumn = 0.75

// listGaps measures a list whose rows each begin beside a leader — a measure,
// or a step number. Only the column the rows' own text is set in is measured,
// so a heading between two groups is not mistaken for a wrapped line.
func listGaps(runs []pdfinspect.TextRun, leaders []string) (inside, between []float64) {
	isLeader := func(text string) bool { return slices.Contains(leaders, strings.TrimSpace(text)) }

	// The column is wherever the text beside the first leader begins.
	column := math.NaN()

	for index, run := range runs {
		if isLeader(run.Text) && index+1 < len(runs) {
			column = runs[index+1].X
			break
		}
	}

	if math.IsNaN(column) {
		return nil, nil
	}

	var (
		previous  *pdfinspect.TextRun
		startsRow bool
	)

	for index := range runs {
		run := runs[index]
		if isLeader(run.Text) {
			startsRow = true
			continue
		}

		if math.Abs(run.X-column) > sameColumn {
			// Something set elsewhere — a heading, or the other column of the
			// page — ends whatever row was being measured.
			previous = nil
			continue
		}

		if previous != nil {
			gap := previous.Y - run.Y
			if gap > 0 {
				if startsRow {
					between = append(between, gap)
				} else {
					inside = append(inside, gap)
				}
			}
		}

		copied := run
		previous = &copied
		startsRow = false
	}

	return inside, between
}

// paragraphGaps measures paragraphs recognized by the words they open with.
func paragraphGaps(runs []pdfinspect.TextRun, openings []string) (inside, between []float64) {
	opens := func(text string) bool {
		for _, opening := range openings {
			if strings.HasPrefix(strings.TrimSpace(text), opening) {
				return true
			}
		}

		return false
	}
	column := math.NaN()

	for _, run := range runs {
		if opens(run.Text) {
			column = run.X
			break
		}
	}

	if math.IsNaN(column) {
		return nil, nil
	}

	var (
		previous *pdfinspect.TextRun
		reading  bool
	)

	for index := range runs {
		run := runs[index]
		if math.Abs(run.X-column) > sameColumn {
			reading = false
			previous = nil

			continue
		}

		switch {
		case opens(run.Text):
			if reading && previous != nil {
				between = append(between, previous.Y-run.Y)
			}

			reading = true
		case reading && previous != nil:
			inside = append(inside, previous.Y-run.Y)
		}

		copied := run
		previous = &copied
	}

	return inside, between
}

func TestLocaleFormatsStructuredAmountsWithoutChangingAuthoredText(t *testing.T) {
	document := corpus(t, "Buttered toast")
	// Without a recipe language the requested locale supplies the labels.
	document.Language = ""
	amount := recipe.Rational("127/100")
	document.IngredientSections[0].Ingredients[0].Quantity = &recipe.Quantity{
		Kind:   recipe.QuantitySimple,
		Amount: &recipe.Amount{Value: &amount},
		Unit:   "mass.gram",
	}
	options := optionsFor(print.SizeLetter)
	options.Locale = "de-DE"
	printed := documentText(t, renderOne(t, document, options))
	requireText(t, printed, "1,27")
	requireText(t, printed, "Zutaten")
	requireText(t, printed, "Zubereitung")
	requireText(t, printed, "Butter it while it is still hot.")

	if got := *document.IngredientSections[0].Ingredients[0].Quantity.Amount.Value; got != "127/100" {
		t.Fatalf("printing mutated canonical amount: %s", got)
	}
}

// An instruction prints as written; the ingredients it references are not
// repeated beneath it.
func TestAStepDoesNotRepeatItsIngredients(t *testing.T) {
	document := corpus(t, "Buttered toast")
	document.InstructionSections[0].Steps[1].IngredientUses = []recipe.IngredientUse{
		{IngredientRef: "butter"},
	}
	artifact := renderOne(t, document, optionsFor(print.SizeLetter))

	if text := documentText(t, artifact); strings.Contains(text, "Uses salted butter") {
		t.Errorf("the step repeats its ingredients: %q", text)
	}
}

// A printed batch reports its unscaled amounts, as SREF requires.
func TestPrintedBatchMarksTheAmountsItCouldNotRecalculate(t *testing.T) {
	document := recipe.Document{
		ID: "partly-scalable", Title: "Partly scalable supper",
		IngredientSections: []recipe.IngredientSection{{ID: "main", Ingredients: []recipe.Ingredient{
			{
				ID: "butter", Name: "unsalted butter", Note: "softened, 227 grams",
				Quantity: &recipe.Quantity{
					Kind:   recipe.QuantitySimple,
					Amount: &recipe.Amount{Value: ratio("1")},
					Unit:   "volume.cup.us.customary",
				},
			},
			{ID: "sugar", SourceText: "0.33 cup/66 grams granulated sugar"},
			{ID: "seasoning", Name: "salt and pepper"},
		}}},
		InstructionSections: []recipe.InstructionSection{{
			ID: "method", Steps: []recipe.Step{{ID: "bake", Text: "Bake until golden."}},
		}},
	}

	options := print.DefaultOptions()
	options.Scale = print.ScaleCustom
	options.CustomScale = "2"

	printed := documentText(t, renderOne(t, document, options))
	requireText(t, printed, "Some amounts could not be recalculated")
	requireText(t, printed, "amount as written")
	requireText(t, printed, "wording as written")

	own := documentText(t, renderOne(t, document, print.DefaultOptions()))
	if strings.Contains(own, "as written") {
		t.Error("the recipe's own batch marked rows it had not multiplied")
	}
}

func ratio(value string) *recipe.Rational {
	amount := recipe.Rational(value)

	return &amount
}

func TestARightToLeftRecipePrintsInItsOwnDirection(t *testing.T) {
	document := corpus(t, "Buttered toast")
	document.Title = "شاي بالنعناع"
	document.Language = "ar"
	printer, stub := service(t, nil, document)

	artifact, err := printer.Render(context.Background(), print.Request{
		Source:  print.NewRecipeSource(stub, library.EntryID(document.ID)),
		Options: optionsFor(print.SizeLetter),
	})
	if err != nil {
		t.Fatal(err)
	}

	pages, err := pdfinspect.Pages(artifact.PDF)
	if err != nil {
		t.Fatal(err)
	}

	runs, err := pdfinspect.TextRuns(artifact.PDF, 0)
	if err != nil {
		t.Fatal(err)
	}

	arabic := 0

	for _, run := range runs {
		if !strings.ContainsAny(run.Text, "شاينعب") {
			continue
		}

		arabic++

		// The title is set from the right margin, so none of it reaches the
		// left half of the page.
		if run.X < pages[0].Width/2 {
			t.Errorf("Arabic title glyphs at x=%.0f of %.0f", run.X, pages[0].Width)
		}
	}

	if arabic == 0 {
		t.Fatal("the Arabic title was not printed with an Arabic font")
	}

	// The recipe's language, not the interface's, supplies the headings.
	if strings.Contains(documentText(t, artifact), "Ingredients") {
		t.Error("an Arabic recipe printed English headings")
	}
}

func TestHeadingsFollowTheRecipeLanguage(t *testing.T) {
	document := corpus(t, "Buttered toast")
	document.Language = "ja"
	options := optionsFor(print.SizeLetter)
	options.Locale = "en-US"

	printed := documentText(t, renderOne(t, document, options))
	requireText(t, printed, "材料")

	if strings.Contains(printed, "Ingredients") || strings.Contains(printed, "Instructions") {
		t.Error("a Japanese recipe printed English headings")
	}
}

func TestAnInterfaceAmountKeepsItsOrderInARecipeWithoutALanguage(t *testing.T) {
	document := corpus(t, "Buttered toast")
	document.Title = "شاي بالنعناع"
	document.Language = ""

	requireText(t, documentText(t, renderOne(t, document, optionsFor(print.SizeLetter))), "1 tbsp")
}

func TestHanTextPrintsWithTheGlyphFormsOfItsRegion(t *testing.T) {
	glyphs := func(language string) []string {
		document := corpus(t, "Buttered toast")
		document.Title = "骨 令 直 角 誤"
		document.Language = language

		return fontPrograms(t, renderOne(t, document, optionsFor(print.SizeLetter)).PDF)
	}

	simplified := glyphs("zh")
	if !slices.Equal(simplified, glyphs("zh-CN")) {
		t.Fatal("zh and zh-CN printed different glyph forms")
	}

	for _, language := range []string{"zh-Hant", "zh-TW", "ja", "ko"} {
		if slices.Equal(simplified, glyphs(language)) {
			t.Errorf("%s printed the Simplified Chinese glyph forms", language)
		}
	}
}

// fontPrograms returns the embedded CFF font programs, which hold the glyph
// outlines a document actually uses.
func fontPrograms(t *testing.T, pdf []byte) []string {
	t.Helper()

	var programs []string

	for rest := pdf; ; {
		start := bytes.Index(rest, []byte("stream\n"))
		if start < 0 {
			break
		}

		rest = rest[start+len("stream\n"):]

		end := bytes.Index(rest, []byte("endstream"))
		if end < 0 {
			break
		}

		reader, err := zlib.NewReader(bytes.NewReader(rest[:end]))
		if err == nil {
			data, err := io.ReadAll(reader)
			if err == nil && bytes.HasPrefix(data, []byte{1, 0, 4}) {
				programs = append(programs, string(data))
			}
		}

		rest = rest[end+len("endstream"):]
	}

	if len(programs) == 0 {
		t.Fatal("the document embeds no CFF font program")
	}

	slices.Sort(programs)

	return programs
}
