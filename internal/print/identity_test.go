package print

import (
	"context"
	"errors"
	"testing"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

type identityLibrary struct {
	entry library.Entry
	books map[string]library.Book
}

func (l *identityLibrary) Get(_ context.Context, id library.EntryID) (library.Entry, error) {
	if id != l.entry.ID {
		return library.Entry{}, library.ErrNotFound
	}

	return l.entry, nil
}

func (*identityLibrary) GetOrganization(context.Context, string) (library.Organization, error) {
	return library.Organization{}, library.ErrNotFound
}

func (l *identityLibrary) GetBook(_ context.Context, id string) (library.Book, error) {
	book, found := l.books[id]
	if !found {
		return library.Book{}, library.ErrNotFound
	}

	return book, nil
}

type titleRenderer struct{}

func (titleRenderer) Version() string { return "title-v1" }
func (titleRenderer) Render(_ context.Context, document PrintDocument) ([]byte, error) {
	return []byte("%PDF-1.4\n" + document.Title + "\n%%EOF"), nil
}

func identityFixture() *identityLibrary {
	entryID := library.EntryID("entry-one")

	return &identityLibrary{
		entry: library.Entry{ID: entryID, Revision: 7, Recipe: recipe.Document{
			SchemaVersion: recipe.CurrentDocumentSchemaVersion,
			ID:            "recipe-one", Title: "Recipe", Language: "en",
		}},
		books: map[string]library.Book{
			"alpha": {ID: "alpha", Title: "Alpha", Revision: 1, Entries: []library.BookEntry{{ID: "a", EntryID: entryID}}},
			"beta":  {ID: "beta", Title: "Beta", Revision: 1, Entries: []library.BookEntry{{ID: "b", EntryID: entryID}}},
		},
	}
}

func identityService(t *testing.T, scope string, values *identityLibrary) *Service {
	t.Helper()

	service, err := NewService(titleRenderer{}, units.DefaultCatalog(), nil, values, values, WithAuthorizationScope(scope))
	if err != nil {
		t.Fatal(err)
	}

	return service
}

func TestEqualRecipesWithDifferentEffectiveTitlesHaveDifferentArtifacts(t *testing.T) {
	values := identityFixture()
	service := identityService(t, "library-one", values)

	alpha, err := service.Render(context.Background(), Request{Source: NewBookSource(values, values, "alpha"), Options: DefaultOptions()})
	if err != nil {
		t.Fatal(err)
	}

	beta, err := service.Render(context.Background(), Request{Source: NewBookSource(values, values, "beta"), Options: DefaultOptions()})
	if err != nil {
		t.Fatal(err)
	}

	if alpha.ID == beta.ID {
		t.Fatal("books with equal recipe revisions but different titles shared an artifact identity")
	}

	if string(alpha.PDF) == string(beta.PDF) || alpha.Title != "Alpha" || beta.Title != "Beta" {
		t.Fatalf("artifacts do not preserve their effective titles: %q, %q", alpha.Title, beta.Title)
	}
}

func TestBookRenameCreatesANewArtifactIdentity(t *testing.T) {
	values := identityFixture()
	service := identityService(t, "library-one", values)

	before, err := service.Render(context.Background(), Request{Source: NewBookSource(values, values, "alpha"), Options: DefaultOptions()})
	if err != nil {
		t.Fatal(err)
	}

	book := values.books["alpha"]
	book.Title, book.Revision = "Renamed", 2
	values.books["alpha"] = book

	after, err := service.Render(context.Background(), Request{Source: NewBookSource(values, values, "alpha"), Options: DefaultOptions()})
	if err != nil {
		t.Fatal(err)
	}

	if before.ID == after.ID || after.Title != "Renamed" || string(before.PDF) == string(after.PDF) {
		t.Fatalf("rename reused stale artifact: before=%s after=%s title=%q", before.ID, after.ID, after.Title)
	}
}

func TestArtifactIdentityAndHeldReadsAreBoundToAuthorizationScope(t *testing.T) {
	values := identityFixture()
	first := identityService(t, "library-one", values)
	second := identityService(t, "library-two", values)
	request := func() Request {
		return Request{Source: NewBookSource(values, values, "alpha"), Options: DefaultOptions()}
	}

	left, err := first.Render(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}

	right, err := second.Render(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}

	if left.ID == right.ID {
		t.Fatal("different authorization scopes shared an artifact identity")
	}

	if _, found, err := second.Held(left.ID); err != nil || found {
		t.Fatalf("second scope read first scope's held artifact: found=%v err=%v", found, err)
	}

	if held, found, err := first.Held(left.ID); err != nil || !found || held.ID != left.ID {
		t.Fatalf("originating scope could not read its artifact: found=%v err=%v", found, err)
	}
}

func TestArtifactCacheRejectsKeyAndContentMismatches(t *testing.T) {
	cache := newArtifactCache(2, 1024)
	if err := cache.put("key", Artifact{ID: "other", PDF: []byte("one"), authorizationScope: "scope"}); !errors.Is(err, ErrArtifactInvariant) {
		t.Fatalf("key mismatch error = %v", err)
	}

	first := Artifact{ID: "key", PDF: []byte("one"), Title: "First", authorizationScope: "scope"}
	if err := cache.put("key", first); err != nil {
		t.Fatal(err)
	}

	second := first

	second.PDF = []byte("two")
	if err := cache.put("key", second); !errors.Is(err, ErrArtifactInvariant) {
		t.Fatalf("content mismatch error = %v", err)
	}

	if _, found, err := cache.get("key", "another-scope"); err != nil || found {
		t.Fatalf("scope mismatch was disclosed: found=%v err=%v", found, err)
	}
}
