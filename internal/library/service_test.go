package library

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestServiceCreateValidatesAndAddsLibraryMetadata(t *testing.T) {
	t.Parallel()

	repository := &memoryRepository{}
	timestamp := time.Date(2026, time.August, 26, 18, 30, 0, 123, time.FixedZone("test", -6*60*60))

	service, err := NewService(
		repository,
		units.DefaultCatalog(),
		WithIDGenerator(func() (EntryID, error) { return "rtestentry", nil }),
		WithClock(func() time.Time { return timestamp }),
	)
	if err != nil {
		t.Fatal(err)
	}

	document := testfixture.Document()

	entry, err := service.Create(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}

	if entry.ID != "rtestentry" || entry.Revision != 1 {
		t.Fatalf("unexpected metadata: %#v", entry)
	}

	if !entry.CreatedAt.Equal(timestamp) || entry.CreatedAt.Location() != time.UTC {
		t.Fatalf("creation time was not normalized to UTC: %v", entry.CreatedAt)
	}

	if !reflect.DeepEqual(entry, repository.created) {
		t.Fatal("repository did not receive the returned entry")
	}
}

func TestServiceRejectsInvalidRecipeBeforeStorage(t *testing.T) {
	t.Parallel()

	repository := &memoryRepository{}

	service, err := NewService(repository, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	document := testfixture.Document()

	document.InstructionSections[0].Steps[0].IngredientUses = []recipe.IngredientUse{{IngredientRef: "missing"}}
	if _, err := service.Create(context.Background(), document); err == nil {
		t.Fatal("invalid recipe unexpectedly succeeded")
	}

	if repository.createCalls != 0 {
		t.Fatal("invalid recipe reached the repository")
	}
}

func TestServiceRejectsDocumentIdentityChangeBeforeRevision(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	repository := &memoryRepository{created: Entry{ID: "ridentity", Recipe: document, Revision: 4}}

	service, err := NewService(repository, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	replacement := document

	replacement.ID = "independent-copy"
	if _, err := service.Update(context.Background(), repository.created.ID, 4, replacement); !errors.Is(err, ErrDocumentIdentityChange) {
		t.Fatalf("identity-changing update error = %v", err)
	}

	if repository.reviseCalls != 0 {
		t.Fatal("identity-changing update reached repository revision")
	}

	replacement.ID = document.ID
	replacement.Title = "Revised title"

	updated, err := service.Update(context.Background(), repository.created.ID, 4, replacement)
	if err != nil {
		t.Fatal(err)
	}

	if updated.Recipe.ID != document.ID || repository.reviseCalls != 1 {
		t.Fatalf("ordinary update changed identity or skipped revision: %#v", updated)
	}
}

func TestServicePreservesOpaqueImportedDataAcrossOrdinaryEdits(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.Extensions = map[string]any{"x-example": map[string]any{"kept": true}}
	document.SREFOpaque = map[string]any{"ingredient:flour": map[string]any{"future_detail": "kept"}}
	repository := &memoryRepository{created: Entry{ID: "ropaque", Recipe: document, Revision: 4}}

	service, err := NewService(repository, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	replacement := document
	replacement.Title = "Revised title"
	replacement.Extensions = nil
	replacement.SREFOpaque = map[string]any{"ingredient:flour": map[string]any{"future_detail": "changed"}}

	updated, err := service.Update(context.Background(), repository.created.ID, 4, replacement)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(updated.Recipe.Extensions, document.Extensions) || !reflect.DeepEqual(updated.Recipe.SREFOpaque, document.SREFOpaque) {
		t.Fatalf("opaque imported data changed: extensions=%#v SREF=%#v", updated.Recipe.Extensions, updated.Recipe.SREFOpaque)
	}
}

func TestImportMatchingUsesStrongEvidenceBeforeSimilarity(t *testing.T) {
	t.Parallel()

	existing := testfixture.Document()
	existing.ID, existing.Title = "held-portable-id", "Weeknight beans"
	existing.Source = &recipe.Source{
		Application: "Kitchen Export", SourceID: "recipe-42",
		URL: "https://Example.test/recipes/beans/?utm_source=newsletter",
	}

	service, err := NewService(&memoryRepository{created: Entry{ID: "rheld", Recipe: existing, Revision: 3}}, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name      string
		candidate recipe.Document
		evidence  ImportMatchEvidence
	}{
		{name: "source identity", candidate: func() recipe.Document {
			value := existing
			value.ID = "different-portable-id"
			source := *existing.Source
			source.URL = "https://somewhere-else.test/recipe"
			value.Source = &source

			return value
		}(), evidence: ImportMatchSourceIdentity},
		{name: "canonical URL", candidate: func() recipe.Document {
			value := existing
			value.ID = "different-portable-id"
			value.Source = &recipe.Source{URL: "https://example.test:443/recipes/beans?fbclid=tracking"}

			return value
		}(), evidence: ImportMatchCanonicalURL},
		{name: "portable ID", candidate: func() recipe.Document {
			value := existing
			value.Source = nil

			return value
		}(), evidence: ImportMatchPortableID},
	} {
		t.Run(test.name, func(t *testing.T) {
			analyses, err := service.AnalyzeImportCandidates(context.Background(), []recipe.Document{test.candidate})
			if err != nil {
				t.Fatal(err)
			}

			if len(analyses) != 1 || analyses[0].Status != ImportDefiniteMatch || len(analyses[0].Matches) != 1 || analyses[0].Matches[0].Evidence != test.evidence {
				t.Fatalf("analysis = %#v", analyses)
			}
		})
	}
}

func TestImportSimilaritySuggestsCorroboratedContentButNeverTitleAlone(t *testing.T) {
	t.Parallel()

	existing := testfixture.Document()
	existing.ID, existing.Title, existing.Source = "held-cake", "Chocolate cake", nil

	service, err := NewService(&memoryRepository{created: Entry{ID: "rheld", Recipe: existing, Revision: 1}}, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	likely := existing
	likely.ID = "other-cake"
	likely.Title = "Easy chocolate cake"

	analyses, err := service.AnalyzeImportCandidates(context.Background(), []recipe.Document{likely})
	if err != nil {
		t.Fatal(err)
	}

	if analyses[0].Status != ImportLikelyDuplicate || analyses[0].Matches[0].Evidence != ImportMatchSimilarity {
		t.Fatalf("corroborated analysis = %#v", analyses[0])
	}

	titleOnly := testfixture.Document()
	titleOnly.ID, titleOnly.Title, titleOnly.Source = "unrelated-cake", existing.Title, nil
	titleOnly.Authors = []recipe.Author{{Name: "Someone else"}}

	titleOnly.Yields = []recipe.Yield{{Text: "one loaf"}}
	for sectionIndex := range titleOnly.IngredientSections {
		for ingredientIndex := range titleOnly.IngredientSections[sectionIndex].Ingredients {
			titleOnly.IngredientSections[sectionIndex].Ingredients[ingredientIndex].Name = "unrelated ingredient"
		}
	}

	for sectionIndex := range titleOnly.InstructionSections {
		for stepIndex := range titleOnly.InstructionSections[sectionIndex].Steps {
			titleOnly.InstructionSections[sectionIndex].Steps[stepIndex].Text = "Do something unrelated."
		}
	}

	analyses, err = service.AnalyzeImportCandidates(context.Background(), []recipe.Document{titleOnly})
	if err != nil {
		t.Fatal(err)
	}

	if analyses[0].Status != ImportDefinitelyNew {
		t.Fatalf("title-only analysis = %#v", analyses[0])
	}
}

func TestReconciledImportIsBoundToThePreviewedTargetSnapshot(t *testing.T) {
	t.Parallel()

	existing := testfixture.Document()
	existing.ID = "held-id"
	existing.Source = &recipe.Source{Application: "Kitchen", SourceID: "recipe-42"}
	incoming := existing
	incoming.ID, incoming.Title = "incoming-id", "Imported update"
	base := Entry{ID: "rheld", Recipe: existing, Revision: 3}
	resolution := ImportResolution{
		Action: ImportUpdate, TargetEntryID: base.ID, TargetRevision: base.Revision,
		TargetRecipeID: base.Recipe.ID, MatchEvidence: ImportMatchSourceIdentity,
	}

	for _, test := range []struct {
		name  string
		entry Entry
		err   error
	}{
		{name: "edited", entry: func() Entry { value := base; value.Revision++; value.Recipe.Title = "Edited elsewhere"; return value }()},
		{name: "purged and recreated", entry: func() Entry { value := base; value.Recipe.ID = "replacement-id"; return value }()},
		{name: "identity changed", entry: func() Entry {
			value := base
			value.Recipe.Source = &recipe.Source{Application: "Kitchen", SourceID: "other"}

			return value
		}()},
		{name: "trashed", err: ErrInvalidState},
		{name: "purged", err: ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &memoryRepository{created: test.entry, error: test.err}

			service, err := NewService(repository, units.DefaultCatalog())
			if err != nil {
				t.Fatal(err)
			}

			if _, err := service.ReconcileImportRecipes(context.Background(), []recipe.Document{incoming}, []ImportResolution{resolution}); !errors.Is(err, ErrConflict) {
				t.Fatalf("error = %v, want conflict", err)
			}

			if repository.importCalls != 0 {
				t.Fatal("stale reconciliation reached atomic storage")
			}
		})
	}
}

func TestConcurrentReconciliationsCannotBothReplaceOneTarget(t *testing.T) {
	existing := testfixture.Document()
	existing.ID = "held-id"
	existing.Source = &recipe.Source{Application: "Kitchen", SourceID: "recipe-42"}
	repository := &memoryRepository{created: Entry{ID: "rheld", Recipe: existing, Revision: 3}}

	service, err := NewService(repository, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	resolution := ImportResolution{
		Action: ImportUpdate, TargetEntryID: "rheld", TargetRevision: 3,
		TargetRecipeID: existing.ID, MatchEvidence: ImportMatchSourceIdentity,
	}
	results := make(chan error, 2)

	for index := range 2 {
		incoming := testfixture.Document()
		incoming.ID = recipe.RecipeID("incoming-" + string(rune('a'+index)))
		incoming.Title = "Concurrent update"

		incoming.Source = &recipe.Source{Application: "Kitchen", SourceID: "recipe-42"}
		go func() {
			_, reconcileErr := service.ReconcileImportRecipes(context.Background(), []recipe.Document{incoming}, []ImportResolution{resolution})
			results <- reconcileErr
		}()
	}

	var succeeded, conflicted int

	for range 2 {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrConflict):
			conflicted++
		default:
			t.Fatalf("unexpected reconciliation error: %v", err)
		}
	}

	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("succeeded=%d conflicted=%d", succeeded, conflicted)
	}
}

func TestServiceDoesNotApplyCurrentWritePolicyWhenReadingStoredData(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.Title = strings.Repeat("x", 1001)
	document.IngredientSections[0].Ingredients[0].Quantity.Terms[0].Unit = "volume.tablespoon.retired"
	repository := &memoryRepository{created: Entry{ID: "rhistorical", Recipe: document, Revision: 1}}

	service, err := NewService(repository, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	entry, err := service.Get(context.Background(), repository.created.ID)
	if err != nil {
		t.Fatalf("historical read applied current write validation: %v", err)
	}

	if entry.Recipe.Title != document.Title {
		t.Fatal("historical read changed stored data")
	}

	entries, err := service.List(context.Background(), ListOptions{})
	if err != nil || len(entries) != 1 {
		t.Fatalf("historical list failed: entries=%#v err=%v", entries, err)
	}
}

func TestServiceListAppliesBounds(t *testing.T) {
	t.Parallel()

	repository := &memoryRepository{}

	service, err := NewService(repository, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.List(context.Background(), ListOptions{}); err != nil {
		t.Fatal(err)
	}

	if repository.lastOptions.Limit != DefaultListLimit {
		t.Fatalf("default limit = %d", repository.lastOptions.Limit)
	}

	if _, err := service.List(context.Background(), ListOptions{Query: "  pumpkin  "}); err != nil {
		t.Fatal(err)
	}

	if repository.lastOptions.Query != "pumpkin" {
		t.Fatalf("normalized query = %q", repository.lastOptions.Query)
	}

	for _, options := range []ListOptions{
		{Limit: -1},
		{Limit: MaximumListLimit + 1},
		{Limit: 1, Offset: -1},
		{Query: strings.Repeat("x", MaximumSearchLength+1)},
		{Sort: "unsupported"},
	} {
		if _, err := service.List(context.Background(), options); err == nil {
			t.Fatalf("invalid options %#v unexpectedly succeeded", options)
		}
	}

	if _, err := service.List(context.Background(), ListOptions{Sort: ListSortTitleAsc}); err != nil {
		t.Fatalf("supported title sort failed: %v", err)
	}

	if repository.lastOptions.Sort != ListSortTitleAsc {
		t.Fatalf("recipe sort = %q", repository.lastOptions.Sort)
	}
}

func TestServiceListPageReturnsPrePageTotal(t *testing.T) {
	t.Parallel()

	repository := &memoryRepository{
		created: Entry{ID: "rpaged", Recipe: recipe.Document{Title: "Pie"}, Revision: 1},
		count:   3,
	}

	service, err := NewService(repository, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	entries, total, err := service.ListPage(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 || total != 3 {
		t.Fatalf("entries = %d, total = %d", len(entries), total)
	}
}

func TestServiceCountNormalizesAndBoundsSearch(t *testing.T) {
	t.Parallel()

	repository := &memoryRepository{count: 3}

	service, err := NewService(repository, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	total, err := service.Count(context.Background(), ListOptions{Query: "  pumpkin  "})
	if err != nil {
		t.Fatal(err)
	}

	if total != 3 || repository.lastQuery != "pumpkin" {
		t.Fatalf("count = %d, normalized query = %q", total, repository.lastQuery)
	}

	if _, err := service.Count(context.Background(), ListOptions{Query: strings.Repeat("x", MaximumSearchLength+1)}); err == nil {
		t.Fatal("oversized count query unexpectedly succeeded")
	}
}

func TestRandomIDIsPortableAndUnique(t *testing.T) {
	t.Parallel()

	pattern := regexp.MustCompile(`^r[a-z2-7]{26}$`)
	seen := make(map[string]struct{})

	for range 100 {
		id, err := randomID()
		if err != nil {
			t.Fatal(err)
		}

		if !pattern.MatchString(string(id)) {
			t.Fatalf("generated ID %q is not portable", id)
		}

		if _, duplicate := seen[string(id)]; duplicate {
			t.Fatalf("generated duplicate ID %q", id)
		}

		seen[string(id)] = struct{}{}
	}
}

func TestParseSearchQueryKeepsOrganizationSemanticsDistinct(t *testing.T) {
	t.Parallel()

	criteria, err := ParseSearchQuery(`chicken tag:weeknight cuisine:italian book:"Family Favorites" favorite:true rating:>=4 made:never review:required import:job-7`)
	if err != nil {
		t.Fatal(err)
	}

	if criteria.Text != "chicken" || len(criteria.Tags) != 1 || criteria.Tags[0] != "weeknight" {
		t.Fatalf("unexpected text or tags: %#v", criteria)
	}

	if len(criteria.Facets[FacetCuisine]) != 1 || criteria.Facets[FacetCuisine][0] != "italian" {
		t.Fatalf("unexpected cuisine facet: %#v", criteria.Facets)
	}

	if len(criteria.Books) != 1 || criteria.Books[0] != "Family Favorites" || criteria.Favorite == nil || !*criteria.Favorite || criteria.MinRating == nil || *criteria.MinRating != 4 || !criteria.MadeNever || criteria.ReviewRequired == nil || !*criteria.ReviewRequired {
		t.Fatalf("unexpected organization qualifiers: %#v", criteria)
	}

	if len(criteria.Imports) != 1 || criteria.Imports[0] != "job-7" {
		t.Fatalf("unexpected import qualifier: %#v", criteria.Imports)
	}
	// There is no generic category qualifier. An unrecognized prefix is
	// ordinary text, so it narrows the search without ever standing in for a
	// typed facet.
	generic, err := ParseSearchQuery("category:dinner")
	if err != nil {
		t.Fatal(err)
	}

	if generic.Text != "category:dinner" || len(generic.Facets[FacetCourse]) != 0 || len(generic.Tags) != 0 {
		t.Fatalf("generic category qualifier should stay plain text: %#v", generic)
	}
}

func TestParseSearchQueryTreatsOrdinaryColonsAsText(t *testing.T) {
	t.Parallel()

	for _, query := range []string{"ratio 1:1", "note:keep the zest", "https://example.com/buns"} {
		criteria, err := ParseSearchQuery(query)
		if err != nil {
			t.Fatalf("%q should be an ordinary search: %v", query, err)
		}

		if len(criteria.Tags) != 0 || len(criteria.Books) != 0 || criteria.Favorite != nil {
			t.Fatalf("%q was read as a filter: %#v", query, criteria)
		}

		if criteria.Text == "" {
			t.Fatalf("%q lost its search text", query)
		}
	}
}

func TestParseSearchQueryQuotingSuppressesQualifiers(t *testing.T) {
	t.Parallel()

	criteria, err := ParseSearchQuery(`"tag:weeknight"`)
	if err != nil {
		t.Fatal(err)
	}

	if len(criteria.Tags) != 0 || criteria.Text != "tag:weeknight" {
		t.Fatalf("quoted text should never filter: %#v", criteria)
	}

	escaped, err := ParseSearchQuery(`tag:"Bob\"s pick"`)
	if err != nil {
		t.Fatal(err)
	}

	if len(escaped.Tags) != 1 || escaped.Tags[0] != `Bob"s pick` {
		t.Fatalf("escaped quote lost inside a taxonomy name: %#v", escaped)
	}
}

// The exact string the bundled client builds for a taxonomy name containing
// quotes and a backslash, next to free text containing a colon. Both sides of
// the wire must agree about escaping.
func TestParseSearchQueryMatchesTheClientEscaping(t *testing.T) {
	t.Parallel()

	criteria, err := ParseSearchQuery(`"ratio 1:1" tag:"Bob\"s \"pick\" \\ done"`)
	if err != nil {
		t.Fatal(err)
	}

	if criteria.Text != "ratio 1:1" {
		t.Fatalf("free text did not survive quoting: %q", criteria.Text)
	}

	if len(criteria.Tags) != 1 || criteria.Tags[0] != `Bob"s "pick" \ done` {
		t.Fatalf("escaped taxonomy name did not survive: %#v", criteria.Tags)
	}
}

func TestParseSearchQueryAcceptsStableIdentifiers(t *testing.T) {
	t.Parallel()

	criteria, err := ParseSearchQuery("tag:tabc123 book:babc123")
	if err != nil {
		t.Fatal(err)
	}

	if len(criteria.Tags) != 1 || criteria.Tags[0] != "tabc123" {
		t.Fatalf("tag identifier was not preserved: %#v", criteria)
	}

	if len(criteria.Books) != 1 || criteria.Books[0] != "babc123" {
		t.Fatalf("book identifier was not preserved: %#v", criteria)
	}
}

type memoryRepository struct {
	OrganizationRepository
	CookingRepository
	created     Entry
	createCalls int
	reviseCalls int
	lastOptions ListOptions
	lastQuery   string
	count       int
	error       error
	importCalls int
	mutex       sync.Mutex
}

func (repository *memoryRepository) Create(_ context.Context, entry Entry, _ RevisionMetadata) error {
	repository.createCalls++
	repository.created = entry

	return repository.error
}

func (*memoryRepository) FindByDocumentID(context.Context, recipe.RecipeID) (EntryID, uint64, State, error) {
	return "", 0, "", ErrNotFound
}

func (repository *memoryRepository) RegisteredAssets(context.Context, []string) (map[string]bool, error) {
	return map[string]bool{}, repository.error
}

func (repository *memoryRepository) ImportMany(_ context.Context, mutations []ImportMutation) ([]Entry, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()

	repository.importCalls++
	if repository.error != nil {
		return nil, repository.error
	}

	entries := make([]Entry, len(mutations))
	for index, mutation := range mutations {
		if mutation.ExpectedRevision > 0 && repository.created.Revision != mutation.ExpectedRevision {
			return nil, ErrConflict
		}

		revision := mutation.ExpectedRevision + 1

		createdAt := mutation.Timestamp
		if mutation.ExpectedRevision > 0 && !repository.created.CreatedAt.IsZero() {
			createdAt = repository.created.CreatedAt
		}

		entry := Entry{ID: mutation.ID, Recipe: mutation.Recipe, Revision: revision, CreatedAt: createdAt, UpdatedAt: mutation.Timestamp}
		repository.created = entry
		entries[index] = entry
	}

	return entries, nil
}

func (repository *memoryRepository) Get(context.Context, EntryID, State) (Entry, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()

	if repository.error != nil {
		return Entry{}, repository.error
	}

	if repository.created.ID == "" {
		return Entry{}, ErrNotFound
	}

	return repository.created, nil
}

func (repository *memoryRepository) List(_ context.Context, options ListOptions) ([]Summary, error) {
	repository.lastOptions = options
	if repository.error != nil {
		return nil, repository.error
	}

	if repository.created.ID == "" {
		return []Summary{}, nil
	}

	return []Summary{{
		ID: repository.created.ID, RecipeID: repository.created.Recipe.ID,
		Title: repository.created.Recipe.Title, Revision: repository.created.Revision,
	}}, nil
}

func (repository *memoryRepository) Count(_ context.Context, options ListOptions) (int, error) {
	repository.lastQuery = options.Query
	if repository.error != nil {
		return 0, repository.error
	}

	return repository.count, nil
}

func (repository *memoryRepository) ListPage(ctx context.Context, options ListOptions) ([]Summary, int, error) {
	entries, err := repository.List(ctx, options)
	if err != nil {
		return nil, 0, err
	}

	total := repository.count
	if total == 0 {
		total = len(entries)
	}

	return entries, total, nil
}

func (repository *memoryRepository) ListReferences(_ context.Context, _ State) ([]RecipeReference, error) {
	if repository.error != nil {
		return nil, repository.error
	}

	if repository.created.ID == "" {
		return []RecipeReference{}, nil
	}

	return []RecipeReference{{
		ID: repository.created.ID, Title: repository.created.Recipe.Title,
		Revision: repository.created.Revision,
	}}, nil
}

func (repository *memoryRepository) Revise(_ context.Context, command RevisionCommand) (Entry, error) {
	repository.reviseCalls++
	if repository.error != nil {
		return Entry{}, repository.error
	}

	if repository.created.ID == "" {
		return Entry{}, ErrNotFound
	}

	if repository.created.Revision != command.ExpectedRevision {
		return Entry{}, &PreconditionError{Expected: command.ExpectedRevision, Current: repository.created.Revision}
	}

	repository.created.Recipe = command.Recipe
	repository.created.Revision++

	repository.created.UpdatedAt = command.Timestamp
	if command.TargetState == StateTrash {
		deletedAt := command.Timestamp
		repository.created.DeletedAt = &deletedAt
	} else {
		repository.created.DeletedAt = nil
	}

	return repository.created, nil
}

func (*memoryRepository) GetRevision(context.Context, EntryID, uint64) (Revision, error) {
	return Revision{}, ErrNotFound
}

func (*memoryRepository) ListRevisions(context.Context, EntryID, RevisionListOptions) ([]RevisionSummary, error) {
	return nil, ErrNotFound
}

func (repository *memoryRepository) Purge(context.Context, EntryID, uint64, time.Time) error {
	return repository.error
}

func (repository *memoryRepository) PurgeMany(context.Context, []PurgeTarget, []BookPurgeTarget, time.Time) error {
	return repository.error
}

func TestPermanentRecipeDeletionRequiresReviewedRevision(t *testing.T) {
	t.Parallel()

	repository := &memoryRepository{}

	service, err := NewService(repository, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	if err := service.Purge(context.Background(), "rtrash", 0); err == nil {
		t.Fatal("permanent deletion without a reviewed revision unexpectedly succeeded")
	}
}

func TestServicePreservesRepositoryErrorIdentity(t *testing.T) {
	t.Parallel()

	repository := &memoryRepository{error: ErrNotFound}

	service, err := NewService(repository, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Get(context.Background(), "rmissing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// A collection address every recipe in a Paprika export carries is not
// identity evidence.
func TestAnAddressManyRecipesShareIsNotIdentityEvidence(t *testing.T) {
	t.Parallel()

	const collection = "https://example.test/collection"

	shared := func(id, title string) recipe.Document {
		return recipe.Document{ID: recipe.RecipeID("paprika." + id), Title: title, Source: &recipe.Source{URL: collection}}
	}

	var existing []Entry
	for _, title := range []string{"Compote", "Pancakes", "Focaccia", "Risotto", "Cordial"} {
		existing = append(existing, Entry{ID: EntryID(title), Recipe: shared(title, title)})
	}

	tokens := tokenTable{}

	analysis := analyzeImportCandidate(tokens.fingerprint(shared("new", "Something Else")), fingerprintEntries(tokens, existing))
	if analysis.Status != ImportDefinitelyNew || len(analysis.Matches) != 0 {
		t.Fatalf("analysis = %#v", analysis)
	}
}

// An address two or three recipes share still identifies something worth a
// decision, so the rule narrows what counts as evidence rather than removing it.
func TestAnAddressTwoRecipesShareStillNeedsADecision(t *testing.T) {
	t.Parallel()

	const page = "https://example.test/recipes/compote"

	shared := func(id string) recipe.Document {
		return recipe.Document{ID: recipe.RecipeID("paprika." + id), Title: "Compote", Source: &recipe.Source{URL: page}}
	}

	existing := []Entry{
		{ID: "one", Recipe: shared("one")},
		{ID: "two", Recipe: shared("two")},
	}

	tokens := tokenTable{}

	analysis := analyzeImportCandidate(tokens.fingerprint(shared("three")), fingerprintEntries(tokens, existing))
	if analysis.Status != ImportAmbiguous || len(analysis.Matches) != 2 {
		t.Fatalf("analysis = %#v", analysis)
	}
}

func fingerprintEntries(tokens tokenTable, entries []Entry) []importFingerprint {
	result := make([]importFingerprint, len(entries))
	for index, entry := range entries {
		result[index] = tokens.fingerprint(entry.Recipe)
		result[index].match = comparison(entry, "", 0)
	}

	return result
}

func TestImportSuggestionsKeepBestFiveWithStableTies(t *testing.T) {
	candidate := testfixture.Document()
	candidate.ID, candidate.Title, candidate.Source = "candidate", "Chocolate cake", nil

	var entries []Entry

	for index := range 9 {
		document := candidate

		document.ID, document.Title = recipe.RecipeID(fmt.Sprint(index)), "Easy chocolate cake"
		if index == 8 {
			document.Title = candidate.Title
		}

		entries = append(entries, Entry{ID: EntryID(fmt.Sprint(index)), Recipe: document, Revision: 1})
	}

	tokens := tokenTable{}

	result := analyzeImportCandidate(tokens.fingerprint(candidate), fingerprintEntries(tokens, entries))
	if result.Status != ImportLikelyDuplicate || len(result.Matches) != 5 {
		t.Fatalf("analysis: %+v", result)
	}

	for index, id := range []EntryID{"8", "0", "1", "2", "3"} {
		if result.Matches[index].EntryID != id {
			t.Fatalf("match %d: got %s, want %s", index, result.Matches[index].EntryID, id)
		}
	}
}
