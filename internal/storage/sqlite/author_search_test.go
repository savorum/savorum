package sqlite

import (
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestAuthorFilterUsesCreditsBeforePaginationAndGrouping(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openStore(ctx, t)

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	entries := []library.Entry{}

	for index, authors := range [][]recipe.Author{
		{{Name: "Julia Child"}, {Name: "Simone Beck"}, {Name: "Julia Child"}},
		{{Name: "JULIA Child"}},
		{{Name: "Someone else", URL: "https://example.test/Julia-Child"}},
		nil,
	} {
		document := testfixture.Document()
		document.ID = recipe.RecipeID("author-page-" + string(rune('a'+index)))
		document.Title, document.Description = "Rice supper", "Julia Child and Simone Beck"
		document.Authors = authors
		document.Language = []string{"fr", "en", "ar", "en"}[index]
		document.Source = &recipe.Source{Name: "Julia Child"}

		entry, err := service.Create(ctx, document)
		if err != nil {
			t.Fatal(err)
		}

		entries = append(entries, entry)
	}

	for _, check := range []struct {
		query string
		total int
	}{
		{`author:"Julia Child"`, 2},
		{`rice author:"julia   child"`, 2},
		{`author:"Julia Child" author:"Simone Beck"`, 1},
		{`author:Julia`, 0},
		{`author:"Julia Child%"`, 0},
		{`author:unknown`, 0},
	} {
		options := library.ListOptions{Query: check.query, Limit: 1, Sort: library.ListSortTitleAsc}

		count, err := service.Count(ctx, options)
		if err != nil || count != check.total {
			t.Fatalf("count %q = %d, %v", check.query, count, err)
		}

		for offset := 0; offset <= check.total; offset++ {
			options.Offset = offset

			items, total, err := service.ListPage(ctx, options)
			if err != nil || total != check.total || len(items) != min(1, check.total-offset) {
				t.Fatalf("page %q offset %d = %+v, %d, %v", check.query, offset, items, total, err)
			}
		}
	}

	if _, err := service.LinkTranslation(ctx, entries[0].ID, entries[2].ID, recipe.TranslationOriginal); err != nil {
		t.Fatal(err)
	}

	items, total, err := service.ListPage(ctx, library.ListOptions{
		Query: `author:"Julia Child" author:"Simone Beck"`, GroupTranslations: true, PreferredLanguages: []string{"ar", "fr"},
	})
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != entries[0].ID {
		t.Fatalf("unmatched preferred rendition selected: %+v, %d, %v", items, total, err)
	}

	owner, err := store.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	ownerCtx := access.WithLibrary(identity.WithPrincipal(ctx, owner), "default")

	other, err := store.CreateLibrary(ownerCtx, "Another library")
	if err != nil {
		t.Fatal(err)
	}

	otherCtx := access.WithLibrary(ownerCtx, other.ID)
	if results, err := service.FindRecipeAuthors(otherCtx, "Julia Child", 20); err != nil || len(results.Items) != 0 {
		t.Fatalf("author discovery crossed libraries: %+v, %v", results, err)
	}

	denied := access.WithLibrary(identity.WithPrincipal(ctx, identity.Principal{UserID: "unrelated"}), "default")
	if _, err := service.FindRecipeAuthors(denied, "Julia Child", 20); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("unauthorized discovery = %v", err)
	}

	if _, err := service.Trash(ctx, entries[1].ID, entries[1].Revision); err != nil {
		t.Fatal(err)
	}

	result, err := service.FindRecipeAuthors(ctx, "Julia Chlid", 20)
	if err != nil || len(result.Items) != 1 || result.Items[0].Name != "Julia Child" || result.Items[0].RecipeCount != 1 || result.Items[0].Match != "fuzzy" {
		t.Fatalf("author candidate/count = %+v, %v", result, err)
	}

	if err := store.RebuildSearchIndex(ctx); err != nil {
		t.Fatal(err)
	}

	after, err := service.FindRecipeAuthors(ctx, "Julia Chlid", 20)
	if err != nil || !slices.Equal(result.Items, after.Items) {
		t.Fatalf("rebuild changed author candidates: %+v, %v", after, err)
	}
}

func TestRecipeAuthorMigrationBackfillsAndRevisionsRefreshNames(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	path := filepath.Join(t.TempDir(), "upgrade.db")
	if err := registerSearchFunctions(); err != nil {
		t.Fatal(err)
	}

	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}

	partial := &Store{db: &writerDB{raw: database}, dsn: path, path: path}

	t.Cleanup(func() { _ = partial.Close() })

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}

	for index, migration := range migrations[:14] {
		if err := partial.applyMigration(ctx, migration, index == 0); err != nil {
			t.Fatal(err)
		}
	}

	document := testfixture.Document()
	document.Authors = []recipe.Author{{Name: "Renée"}}
	now := time.Now().UTC()

	entry := library.Entry{ID: "rauthor-upgrade", Recipe: document, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := partial.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	if err := partial.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.FindRecipeAuthors(ctx, "Renee", 20)
	if err != nil || len(result.Items) != 1 || result.Items[0].Name != "Renée" || result.Items[0].Match != "accent" {
		t.Fatalf("backfilled author = %+v, %v", result, err)
	}

	document.Authors = []recipe.Author{{Name: "Nguyễn"}}

	updated, err := service.Update(ctx, entry.ID, 1, document)
	if err != nil {
		t.Fatal(err)
	}

	old, err := service.FindRecipeAuthors(ctx, "Renee", 20)
	if err != nil || len(old.Items) != 0 {
		t.Fatalf("old head credit remains discoverable: %+v, %v", old, err)
	}

	if _, err := service.Trash(ctx, entry.ID, updated.Revision); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Restore(ctx, entry.ID, updated.Revision+1); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Trash(ctx, entry.ID, updated.Revision+2); err != nil {
		t.Fatal(err)
	}

	if err := service.Purge(ctx, entry.ID, updated.Revision+3); err != nil {
		t.Fatal(err)
	}

	var retainedNames int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM recipe_author_names").Scan(&retainedNames); err != nil || retainedNames != 0 {
		t.Fatalf("deleted recipe left author names: %d, %v", retainedNames, err)
	}
}
