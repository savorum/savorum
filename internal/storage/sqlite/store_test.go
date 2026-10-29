package sqlite_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/storage/filepolicy"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestStoreRoundTripsRecipeSemanticsAcrossReopen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "library.db")

	store, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}

	timestamp := time.Date(2026, time.August, 26, 20, 0, 0, 123456789, time.UTC)

	service, err := library.NewService(
		store,
		units.DefaultCatalog(),
		library.WithIDGenerator(func() (library.EntryID, error) { return "rroundtrip", nil }),
		library.WithClock(func() time.Time { return timestamp }),
	)
	if err != nil {
		t.Fatal(err)
	}

	written, err := service.Create(ctx, testfixture.Document())
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = reopened.Close() })

	read, err := reopened.Get(ctx, written.ID, library.StateActive)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(written, read) {
		t.Fatalf("round trip changed entry\nwritten: %#v\nread: %#v", written, read)
	}
}

func TestDatabaseAndSidecarsIgnorePermissiveUmask(t *testing.T) {
	previous := syscall.Umask(0o022)
	defer syscall.Umask(previous)

	for _, testCase := range []struct {
		name   string
		policy filepolicy.Policy
		mode   os.FileMode
	}{
		{name: "owner only", policy: filepolicy.OwnerPolicy(), mode: 0o600},
		{name: "explicit shared group", policy: filepolicy.Policy{Access: filepolicy.SharedGroup}, mode: 0o660},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "state")
			path := filepath.Join(parent, "library.db")

			store, err := sqlite.Open(context.Background(), path, sqlite.WithFilePolicy(testCase.policy))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()

			parentInfo, err := os.Stat(parent)
			if err != nil {
				t.Fatal(err)
			}

			wantDirectory := os.FileMode(0o700)
			if testCase.policy.Access == filepolicy.SharedGroup {
				wantDirectory = 0o770
			}

			if got := parentInfo.Mode().Perm(); got != wantDirectory {
				t.Errorf("database parent mode = %#o, want %#o", got, wantDirectory)
			}

			for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
				info, err := os.Stat(candidate)
				if err != nil {
					t.Fatalf("stat %s: %v", filepath.Base(candidate), err)
				}

				if got := info.Mode().Perm(); got != testCase.mode {
					t.Errorf("%s mode = %#o, want %#o", filepath.Base(candidate), got, testCase.mode)
				}
			}
		})
	}
}

func TestOpenMigratesAnInsecureExistingDatabaseMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	if err := os.WriteFile(path, nil, 0o644); err != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
		t.Fatal(err)
	}

	if err := os.Chmod(path, 0o644); err != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
		t.Fatal(err)
	}

	store, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("database mode = %#o, want 0600", got)
	}
}

func TestStoreCreateDetectsConflict(t *testing.T) {
	t.Parallel()
	store := openStore(t)

	entry := library.Entry{
		ID:        "rduplicate",
		Recipe:    testfixture.Document(),
		Revision:  1,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.Create(context.Background(), entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	if err := store.Create(context.Background(), entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); !errors.Is(err, library.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestStoreAllowsOnlyOneEntryPerPortableRecipeIdentity(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	timestamp := time.Now().UTC()
	document := testfixture.Document()
	first := library.Entry{ID: "rfirst", Recipe: document, Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}
	second := library.Entry{ID: "rsecond", Recipe: document, Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}

	if err := store.Create(context.Background(), first, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	if err := store.Create(context.Background(), second, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); !errors.Is(err, library.ErrConflict) {
		t.Fatalf("duplicate portable identity error = %v", err)
	}
}

func TestImportManyRollsBackTheWholeBatchOnConflict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	timestamp := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	first := testfixture.Document()
	first.ID = "document-first"
	second := testfixture.Document()
	second.ID = "document-second"

	_, err := store.ImportMany(ctx, []library.ImportMutation{
		{ID: "rsame", Recipe: first, Timestamp: timestamp},
		{ID: "rsame", Recipe: second, Timestamp: timestamp},
	})
	if !errors.Is(err, library.ErrConflict) {
		t.Fatalf("batch conflict error = %v", err)
	}

	if _, _, _, err := store.FindByDocumentID(ctx, first.ID); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("first recipe survived a failed atomic import: %v", err)
	}
}

func TestReconciledImportAtomicallyRejectsAPurgedAndRecreatedTarget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	original := testfixture.Document()
	original.ID = "original-document"

	entry := library.Entry{ID: "rreused", Recipe: original, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	trashed, err := store.Revise(ctx, library.RevisionCommand{
		ID: entry.ID, ExpectedRevision: 1, Recipe: original,
		CurrentState: library.StateActive, TargetState: library.StateTrash,
		Metadata: library.RevisionMetadata{Cause: library.RevisionCauseTrash}, Timestamp: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Purge(ctx, entry.ID, trashed.Revision, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	recreated := original

	recreated.ID, recreated.Title = "replacement-document", "Replacement"
	if err := store.Create(ctx, library.Entry{ID: entry.ID, Recipe: recreated, Revision: 1, CreatedAt: now.Add(3 * time.Minute), UpdatedAt: now.Add(3 * time.Minute)}, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	incoming := original
	incoming.Title = "Stale imported update"

	_, err = store.ImportMany(ctx, []library.ImportMutation{{
		ID: entry.ID, ExpectedRevision: 1, ExpectedRecipe: &original,
		Recipe: incoming, Timestamp: now.Add(4 * time.Minute),
	}})
	if !errors.Is(err, library.ErrConflict) {
		t.Fatalf("recreated-target error = %v, want conflict", err)
	}

	got, err := store.Get(ctx, entry.ID, library.StateActive)
	if err != nil {
		t.Fatal(err)
	}

	if got.Recipe.ID != recreated.ID || got.Recipe.Title != recreated.Title || got.Revision != 1 {
		t.Fatalf("stale import changed recreated target: %#v", got)
	}
}

func TestStoreListUsesStableNewestFirstOrder(t *testing.T) {
	t.Parallel()
	store := openStore(t)

	base := time.Date(2026, time.August, 26, 0, 0, 0, 0, time.UTC)
	for index, id := range []string{"ra", "rb", "rc"} {
		entry := library.Entry{ID: library.EntryID(id), Recipe: testfixture.Document(), Revision: 1, CreatedAt: base, UpdatedAt: base.Add(time.Duration(index) * time.Minute)}

		entry.Recipe.ID = recipe.RecipeID("document-" + id)
		if err := store.Create(context.Background(), entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := store.List(context.Background(), library.ListOptions{Limit: 2, Offset: 1, State: library.StateActive, Sort: library.ListSortUpdatedDesc})
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 || entries[0].ID != "rb" || entries[1].ID != "ra" {
		t.Fatalf("unexpected order: %#v", entries)
	}
}

func TestStoreListUsesBatchedSummaryProjectionsAndSupportedSorts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	base := time.Date(2026, time.August, 26, 1, 0, 0, 0, time.UTC)
	titles := map[library.EntryID]string{"ra": "Alpha", "rb": "Beta", "rz": "Zulu"}
	updated := map[library.EntryID]time.Time{
		"ra": base,
		"rb": base.Add(time.Minute),
		"rz": base.Add(2 * time.Minute),
	}

	for _, id := range []library.EntryID{"ra", "rb", "rz"} {
		document := testfixture.Document()
		document.ID = recipe.RecipeID("document-" + id)
		document.Title = titles[id]

		entry := library.Entry{ID: id, Recipe: document, Revision: 1, CreatedAt: base, UpdatedAt: updated[id]}
		if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}
	}

	for _, tag := range []library.Tag{
		{ID: "t-alpha", Name: "Alpha tag", Revision: 1, CreatedAt: base, UpdatedAt: base},
		{ID: "t-zulu", Name: "Zulu tag", Revision: 1, CreatedAt: base, UpdatedAt: base},
	} {
		if err := store.CreateTag(ctx, tag); err != nil {
			t.Fatal(err)
		}
	}

	ratingTwo, ratingFive := uint8(2), uint8(5)

	organizations := map[library.EntryID]library.Organization{
		"ra": {State: library.UserState{}},
		"rb": {TagIDs: []string{"t-alpha"}, State: library.UserState{Rating: &ratingFive}},
		"rz": {TagIDs: []string{"t-zulu"}, State: library.UserState{Favorite: true, Rating: &ratingTwo}},
	}
	for id, organization := range organizations {
		if _, err := store.ReplaceOrganization(ctx, string(id), library.DefaultUserID, 1, organization, nil, base); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.CreateBook(ctx, library.Book{
		ID: "b-projection", Kind: library.BookPersonal, Title: "Projection book",
		Sections: []library.BookSection{}, Entries: []library.BookEntry{
			{ID: "be-beta", EntryID: "rb", Position: 0},
			{ID: "be-zulu", EntryID: "rz", Position: 1},
		}, Revision: 1, CreatedAt: base, UpdatedAt: base,
	}); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		sort library.ListSort
		want []library.EntryID
	}{
		{sort: library.ListSortUpdatedDesc, want: []library.EntryID{"rz", "rb", "ra"}},
		{sort: library.ListSortTitleAsc, want: []library.EntryID{"ra", "rb", "rz"}},
		{sort: library.ListSortRatingDesc, want: []library.EntryID{"rb", "rz", "ra"}},
	} {
		items, err := store.List(ctx, library.ListOptions{Limit: 10, State: library.StateActive, Sort: test.sort})
		if err != nil {
			t.Fatal(err)
		}

		got := make([]library.EntryID, len(items))
		for index := range items {
			got[index] = items[index].ID
		}

		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("sort %q order = %#v", test.sort, got)
		}

		if items[0].ID == "rb" && (len(items[0].Tags) != 1 || items[0].Tags[0].ID != "t-alpha") {
			t.Fatalf("beta tags = %#v", items[0].Tags)
		}
	}

	items, err := store.List(ctx, library.ListOptions{Limit: 10, State: library.StateActive, Sort: library.ListSortTitleAsc})
	if err != nil {
		t.Fatal(err)
	}

	if len(items[0].Tags) != 0 || len(items[0].Books) != 0 || items[0].Favorite {
		t.Fatalf("alpha associations leaked from another summary: %#v", items[0])
	}

	if len(items[1].Tags) != 1 || items[1].Tags[0].ID != "t-alpha" || len(items[1].Books) != 1 {
		t.Fatalf("beta projection = %#v", items[1])
	}

	if len(items[2].Tags) != 1 || items[2].Tags[0].ID != "t-zulu" || len(items[2].Books) != 1 || !items[2].Favorite {
		t.Fatalf("zulu projection = %#v", items[2])
	}

	books, err := store.ListBooks(ctx, library.BookListOptions{Limit: 10, State: library.StateActive})
	if err != nil {
		t.Fatal(err)
	}

	if len(books) != 1 || books[0].EntryCount != 2 {
		t.Fatalf("book summary projection = %#v", books)
	}
}

func TestStoreListKeepsBatchedBookRowsWithTheirRecipes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	base := time.Date(2026, time.August, 26, 2, 0, 0, 0, time.UTC)

	for _, titled := range []struct {
		id    library.EntryID
		title string
	}{
		{"ralpha", "Alpha"},
		{"rbeta", "Beta"},
		{"rgamma", "Gamma"},
	} {
		document := testfixture.Document()
		document.ID = recipe.RecipeID("document-" + string(titled.id))
		document.Title = titled.title

		entry := library.Entry{ID: titled.id, Recipe: document, Revision: 1, CreatedAt: base, UpdatedAt: base}
		if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}
	}

	for _, book := range []library.Book{
		{
			ID: "b-alpha", Kind: library.BookPersonal, Title: "Alpha collection", Revision: 1, CreatedAt: base, UpdatedAt: base,
			Entries: []library.BookEntry{{ID: "be-alpha-beta", EntryID: "rbeta", Position: 0}},
		},
		{
			ID: "b-shared", Kind: library.BookPersonal, Title: "Shared collection", Revision: 1, CreatedAt: base, UpdatedAt: base,
			Entries: []library.BookEntry{
				{ID: "be-shared-beta", EntryID: "rbeta", Position: 0},
				{ID: "be-shared-gamma", EntryID: "rgamma", Position: 1},
			},
		},
		{ID: "b-source", Kind: library.BookPublication, Title: "Source collection", Revision: 1, CreatedAt: base, UpdatedAt: base},
	} {
		if err := store.CreateBook(ctx, book); err != nil {
			t.Fatal(err)
		}
	}

	organizations := map[library.EntryID]library.Organization{
		"ralpha": {},
		"rbeta": {Sources: []library.RecipeSource{
			{ID: "s-beta-one", Kind: library.SourcePublication, PublicationBookID: "b-source", OriginalTitle: "Beta, page one"},
			{ID: "s-beta-two", Kind: library.SourcePublication, PublicationBookID: "b-source", OriginalTitle: "Beta, page two"},
		}},
		"rgamma": {Sources: []library.RecipeSource{
			{ID: "s-gamma", Kind: library.SourcePublication, PublicationBookID: "b-source", OriginalTitle: "Gamma"},
		}},
	}
	for _, id := range []library.EntryID{"ralpha", "rbeta", "rgamma"} {
		if _, err := store.ReplaceOrganization(ctx, string(id), library.DefaultUserID, 1, organizations[id], nil, base); err != nil {
			t.Fatal(err)
		}
	}

	items, err := store.List(ctx, library.ListOptions{Limit: 10, State: library.StateActive, Sort: library.ListSortTitleAsc})
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 3 || items[0].ID != "ralpha" || items[1].ID != "rbeta" || items[2].ID != "rgamma" {
		t.Fatalf("title-ascending summaries = %#v", items)
	}

	bookIDs := func(summaries []library.BookSummary) []string {
		ids := make([]string, len(summaries))
		for index := range summaries {
			ids[index] = summaries[index].ID
		}

		return ids
	}

	if len(items[0].Books) != 0 || len(items[0].SourceBooks) != 0 {
		t.Fatalf("alpha book rows leaked: %#v", items[0])
	}

	if got := bookIDs(items[1].Books); !reflect.DeepEqual(got, []string{"b-alpha", "b-shared"}) {
		t.Fatalf("beta books = %#v", items[1].Books)
	}

	if got := bookIDs(items[1].SourceBooks); !reflect.DeepEqual(got, []string{"b-source"}) {
		t.Fatalf("beta source books = %#v", items[1].SourceBooks)
	}

	if got := bookIDs(items[2].Books); !reflect.DeepEqual(got, []string{"b-shared"}) {
		t.Fatalf("gamma books = %#v", items[2].Books)
	}

	if got := bookIDs(items[2].SourceBooks); !reflect.DeepEqual(got, []string{"b-source"}) {
		t.Fatalf("gamma source books = %#v", items[2].SourceBooks)
	}
}

func TestStoreSearchesRecipeContentWithPlainTextTerms(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	base := time.Date(2026, time.August, 26, 0, 0, 0, 0, time.UTC)
	entries := []library.Entry{
		{ID: "rsoup", Recipe: testfixture.Document(), Revision: 1, CreatedAt: base, UpdatedAt: base},
		{ID: "rbread", Recipe: testfixture.Document(), Revision: 1, CreatedAt: base, UpdatedAt: base},
	}
	entries[0].Recipe.ID = "cafe-soup"
	entries[0].Recipe.Title = "Café tomato soup"
	entries[0].Recipe.Categories = []string{"Comfort food"}
	entries[0].Recipe.Courses = []string{"Starter"}
	entries[0].Recipe.CookingMethods = []string{"Simmering"}
	entries[1].Recipe.ID = "chickpea-bread"
	entries[1].Recipe.Title = "Seeded bread"

	entries[1].Recipe.IngredientSections[0].Ingredients[0].Name = "chickpea flour"
	for _, entry := range entries {
		if err := store.Create(context.Background(), entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}
	}

	for _, test := range []struct {
		query string
		want  string
	}{
		{query: "cafe tom", want: "rsoup"},
		{query: "seeded chick", want: "rbread"},
		{query: "comfort simmering", want: "rsoup"},
	} {
		options := library.ListOptions{
			Limit: 10, State: library.StateActive, Sort: library.ListSortUpdatedDesc,
			Search: library.SearchCriteria{Text: test.query},
		}

		found, err := store.List(context.Background(), options)
		if err != nil {
			t.Fatal(err)
		}

		if len(found) != 1 || found[0].ID != library.EntryID(test.want) {
			t.Fatalf("search %q returned %#v", test.query, found)
		}

		total, err := store.Count(context.Background(), options)
		if err != nil {
			t.Fatal(err)
		}

		if total != 1 {
			t.Fatalf("search count for %q = %d", test.query, total)
		}
	}

	total, err := store.Count(context.Background(), library.ListOptions{State: library.StateActive})
	if err != nil {
		t.Fatal(err)
	}

	if total != len(entries) {
		t.Fatalf("unfiltered count = %d", total)
	}

	punctuation := library.ListOptions{
		Limit: 10, State: library.StateActive, Sort: library.ListSortUpdatedDesc,
		Search: library.SearchCriteria{Text: "---"},
	}

	found, err := store.List(context.Background(), punctuation)
	if err != nil {
		t.Fatal(err)
	}

	if len(found) != 0 {
		t.Fatalf("punctuation-only search returned %#v", found)
	}

	total, err = store.Count(context.Background(), punctuation)
	if err != nil {
		t.Fatal(err)
	}

	if total != 0 {
		t.Fatalf("punctuation-only count = %d", total)
	}
}

func TestStoreClassificationFiltersQueryPortableMetadataWithoutAssigningFacets(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.August, 28, 0, 0, 0, 0, time.UTC)

	facets := []library.Facet{
		{ID: "fdessert", Kind: library.FacetCourse, Value: "Dessert", Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "fitalian", Kind: library.FacetCuisine, Value: "Italian", Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "fbaking", Kind: library.FacetMethod, Value: "Baking", Revision: 1, CreatedAt: now, UpdatedAt: now},
	}
	for _, facet := range facets {
		if err := store.CreateFacet(ctx, facet); err != nil {
			t.Fatal(err)
		}
	}

	document := testfixture.Document()
	document.ID = "portable-italian"
	document.Courses = []string{"Dessert"}
	document.Cuisines = []string{"Italian"}
	document.CookingMethods = []string{"Baking"}

	entry := library.Entry{ID: "rportable", Recipe: document, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	for _, facet := range facets {
		for _, value := range []string{facet.ID, strings.ToLower(facet.Value)} {
			options := library.ListOptions{
				Limit: 10, State: library.StateActive, Sort: library.ListSortUpdatedDesc,
				Search: library.SearchCriteria{Facets: map[library.FacetKind][]string{facet.Kind: {value}}},
			}

			found, err := store.List(ctx, options)
			if err != nil {
				t.Fatal(err)
			}

			if len(found) != 1 || found[0].ID != entry.ID {
				t.Fatalf("%s filter %q returned %#v, want portable metadata recipe", facet.Kind, value, found)
			}
		}
	}

	organization, err := store.GetOrganization(ctx, string(entry.ID), library.DefaultUserID)
	if err != nil {
		t.Fatal(err)
	}

	if len(organization.FacetIDs) != 0 || len(organization.TagIDs) != 0 || len(organization.Sources) != 0 {
		t.Fatalf("portable metadata created library organization: %#v", organization)
	}
}

func TestStoreFiltersCurrentIngredientReviewState(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.September, 3, 0, 0, 0, 0, time.UTC)
	needsReview := testfixture.Document()
	needsReview.ID = "needs-review"
	clean := testfixture.Document()
	clean.ID = "clean"
	reviewed := testfixture.Document()
	reviewed.ID = "reviewed"
	confirmed := testfixture.Document()
	confirmed.ID = "confirmed"
	partial := testfixture.Document()

	partial.ID = "partial"
	for _, document := range []*recipe.Document{&reviewed, &confirmed, &partial} {
		for sectionIndex := range document.IngredientSections {
			for ingredientIndex := range document.IngredientSections[sectionIndex].Ingredients {
				document.IngredientSections[sectionIndex].Ingredients[ingredientIndex].Normalization = nil
			}
		}
	}

	reviewed.IngredientSections[0].Ingredients[0].Normalization = &recipe.Normalization{
		Method: recipe.NormalizationInferred, Review: recipe.ReviewCorrected,
		Warnings: []string{recipe.ImportReviewUnitPrefix + " The source did not identify a regional measurement convention. It was chosen for this measurement."},
	}
	confirmed.IngredientSections[0].Ingredients[0].Normalization = &recipe.Normalization{
		Method: recipe.NormalizationUnresolved, Review: recipe.ReviewConfirmed,
		Warnings: []string{"ambiguous"},
	}

	partial.IngredientSections[0].Ingredients[0].Normalization = &recipe.Normalization{
		Method: recipe.NormalizationInferred, Review: recipe.ReviewCorrected,
		Warnings: []string{recipe.MissingIngredientMeasureWarning},
	}
	for sectionIndex := range clean.IngredientSections {
		for ingredientIndex := range clean.IngredientSections[sectionIndex].Ingredients {
			clean.IngredientSections[sectionIndex].Ingredients[ingredientIndex].Normalization = nil
		}
	}

	for _, entry := range []library.Entry{
		{ID: "rreview", Recipe: needsReview, Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "rclean", Recipe: clean, Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "rreviewed", Recipe: reviewed, Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "rconfirmed", Recipe: confirmed, Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "rpartial", Recipe: partial, Revision: 1, CreatedAt: now, UpdatedAt: now},
	} {
		if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}
	}

	required := true
	options := library.ListOptions{
		Limit: 10, State: library.StateActive, Sort: library.ListSortUpdatedDesc,
		Search: library.SearchCriteria{ReviewRequired: &required},
	}

	items, err := store.List(ctx, options)
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 2 || (items[0].ID != "rreview" && items[1].ID != "rreview") || (items[0].ID != "rpartial" && items[1].ID != "rpartial") {
		t.Fatalf("review filter returned %#v", items)
	}

	if total, err := store.Count(ctx, options); err != nil || total != 2 {
		t.Fatalf("review count=%d err=%v", total, err)
	}
}

func TestStoreMissingEntry(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	if _, err := store.Get(context.Background(), "rmissing", library.StateActive); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func openStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	return store
}

func TestPartialRecipesAndLocalStubs(t *testing.T) {
	ctx := context.Background()

	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	for _, kind := range []string{"ingredients", "instructions", "stub", "complete"} {
		document := testfixture.Document()
		document.Title = kind
		document.ID = recipe.RecipeID(kind)

		document.InstructionSections = []recipe.InstructionSection{{ID: "method", Steps: []recipe.Step{{ID: "rest", Text: "Rest."}}}}
		if kind == "instructions" || kind == "stub" {
			document.IngredientSections = nil
		}

		if kind == "ingredients" || kind == "stub" {
			document.InstructionSections = nil
		}

		if _, err := service.Create(ctx, document); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
	}

	for query, count := range map[string]int{"missing:ingredients": 2, "missing:instructions": 2, "missing:either": 3, "missing:both": 1} {
		criteria, err := library.ParseSearchQuery(query)
		if err != nil {
			t.Fatal(err)
		}

		results, err := store.List(ctx, library.ListOptions{Limit: 10, State: library.StateActive, Search: criteria, Sort: library.ListSortUpdatedDesc})
		if err != nil || len(results) != count {
			t.Fatalf("%s: got %d, want %d: %v", query, len(results), count, err)
		}
	}

	malformed := testfixture.Document()
	malformed.InstructionSections = nil

	malformed.IngredientSections[0].Ingredients = nil
	if _, err := service.Create(ctx, malformed); err == nil {
		t.Fatal("accepted empty ingredient section")
	}
}

// Microwave reanalysis must write a revision cause the schema admits.
func TestMicrowaveReanalysisPersistsThroughTheRealStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	// A step whose prose states a duration the document has no assertion for,
	// which is what gives reconciliation something to add. Creating through
	// the service would reconcile it on the way in, so this is stored as a
	// recipe written before the inference existed.
	document := testfixture.Document()
	document.Times = nil
	document.InstructionSections = []recipe.InstructionSection{
		{ID: "method", Steps: []recipe.Step{{ID: "warm", Text: "Microwave for 3 minutes at 1250 W."}}},
	}

	timestamp := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)
	entry := library.Entry{ID: "rmicrowave", Recipe: document, Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}

	if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseImport}); err != nil {
		t.Fatal(err)
	}

	reanalyzed, err := service.ReanalyzeMicrowave(ctx, entry.ID, entry.Revision)
	if err != nil {
		t.Fatalf("microwave reanalysis: %v", err)
	}

	if reanalyzed.Revision == entry.Revision {
		t.Fatal("reanalysis found nothing to reconcile, so it proves nothing about writing one")
	}

	if len(reanalyzed.Recipe.Times.Assertions) == 0 {
		t.Fatalf("reanalysis kept no timing: %#v", reanalyzed.Recipe.Times)
	}

	revisions, err := store.ListRevisions(ctx, entry.ID, library.RevisionListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(revisions) != 2 || revisions[0].Cause != library.RevisionCauseManualEdit {
		t.Fatalf("retained history = %#v", revisions)
	}
}

func TestStoreListsTagAndFacetRecipeCounts(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)

	if err := store.CreateTag(ctx, library.Tag{ID: "tquick", Name: "Quick", Revision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	if err := store.CreateFacet(ctx, library.Facet{ID: "fthai", Kind: library.FacetCuisine, Value: "Thai", Revision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	for index, ids := range []struct{ tags, facets []string }{
		{[]string{"tquick"}, []string{"fthai"}},
		{[]string{"tquick"}, nil},
	} {
		document := testfixture.Document()
		document.ID = recipe.RecipeID("counted-" + string(rune(0x30+index)))
		id := library.EntryID("rcounted" + string(rune(0x30+index)))

		entry := library.Entry{ID: id, Recipe: document, Revision: 1, CreatedAt: now, UpdatedAt: now}
		if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}

		organization := library.Organization{EntryID: id, TagIDs: ids.tags, FacetIDs: ids.facets, Sources: []library.RecipeSource{}}
		if _, err := store.ReplaceOrganization(ctx, string(id), library.DefaultUserID, 1, organization, nil, now); err != nil {
			t.Fatal(err)
		}
	}

	tags, err := store.ListTags(ctx)
	if err != nil || len(tags) != 1 || tags[0].RecipeCount != 2 {
		t.Fatalf("tags = %#v, %v; want one tag on two recipes", tags, err)
	}

	facets, err := store.ListFacets(ctx, "")
	if err != nil || len(facets) != 1 || facets[0].RecipeCount != 1 {
		t.Fatalf("facets = %#v, %v; want one facet on one recipe", facets, err)
	}
}
