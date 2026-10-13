package sqlite

import (
	"database/sql"
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

func TestIngredientFiltersUseLeafNamesBeforePaginationAndGrouping(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openStore(ctx, t)

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	entries := []library.Entry{}

	for index, ingredients := range [][]recipe.Ingredient{
		{{ID: "chicken", Name: "chicken breast", Optional: true}, {ID: "rice", Name: "rice"}},
		{{ID: "choice", Relation: "preferred", SourceText: "lamb choice wording", Alternatives: []recipe.Ingredient{{ID: "tofu", Name: "tofu"}, {ID: "chicken", Name: "chicken thighs"}}}},
		{{ID: "lentils", Name: "lentils", Note: "chicken note", SourceText: "chicken original line"}},
		{{ID: "chicken", Name: "chicken breast"}, {ID: "broth", Name: "vegetable broth"}},
		{{ID: "broth", Name: "chicken broth"}},
		{{ID: "raw", SourceText: "2 handfuls unparsed chicken flakes"}},
	} {
		d := testfixture.Document()
		d.ID, d.Title, d.Description = recipe.RecipeID("ingredient-"+string(rune('a'+index))), "Chocolate soup", "Chicken is mentioned in the recipe body"
		d.Language = []string{"fr", "en", "ar", "en", "en", "en"}[index]
		d.Authors = []recipe.Author{{Name: "Julia Child"}}
		d.IngredientSections = []recipe.IngredientSection{{ID: "ingredients", Title: "Chicken heading", Ingredients: ingredients}}
		d.InstructionSections = nil

		var entry library.Entry
		if index == 5 {
			// Current writes require names; storage seeds an unnamed retained line.
			entry = library.Entry{ID: "ingredient-raw-fixture", Recipe: d, Revision: 1, CreatedAt: entries[0].CreatedAt, UpdatedAt: entries[0].UpdatedAt}
			err = store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI})
		} else {
			entry, err = service.Create(ctx, d)
		}

		if err != nil {
			t.Fatal(err)
		}

		entries = append(entries, entry)
	}

	for _, check := range []struct {
		query string
		total int
	}{
		{`ingredient:CHICKEN`, 5},
		{`ingredient:"chicken broth"`, 1},
		{`ingredient:chicken ingredient:broth`, 2},
		{`ingredient:chicken ingredient:rice`, 1},
		{`ingredient:tofu`, 1},
		{`ingredient:lamb`, 0},
		{`ingredient:chikcen`, 0},
		{`ingredient:"---"`, 0},
		{`choclate ingredient:chicken author:"Julia Child"`, 5},
	} {
		for _, sort := range []library.ListSort{library.ListSortRelevance, library.ListSortUpdatedDesc, library.ListSortTitleAsc, library.ListSortRatingDesc} {
			options := library.ListOptions{Query: check.query, Sort: sort, Limit: 1}

			count, err := service.Count(ctx, options)
			if err != nil || count != check.total {
				t.Fatalf("count %q = %d, %v", check.query, count, err)
			}

			for offset := 0; offset <= check.total; offset++ {
				options.Offset = offset

				page, total, err := service.ListPage(ctx, options)
				if err != nil || total != count || len(page) != min(1, count-offset) {
					t.Fatalf("page %q offset %d = %+v, %d, %v", check.query, offset, page, total, err)
				}
			}
		}
	}

	if _, err := service.LinkTranslation(ctx, entries[0].ID, entries[2].ID, recipe.TranslationOriginal); err != nil {
		t.Fatal(err)
	}

	items, total, err := service.ListPage(ctx, library.ListOptions{Query: "ingredient:chicken", GroupTranslations: true, PreferredLanguages: []string{"ar", "fr"}})
	if err != nil || total != 5 || !slices.ContainsFunc(items, func(item library.Summary) bool { return item.ID == entries[0].ID }) ||
		slices.ContainsFunc(items, func(item library.Summary) bool { return item.ID == entries[2].ID }) {
		t.Fatalf("grouped ingredient filter = %+v, %d, %v", items, total, err)
	}

	principal, err := store.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	ownerCtx := access.WithLibrary(identity.WithPrincipal(ctx, principal), initialLibraryID)

	other, err := store.CreateLibrary(ownerCtx, "Other")
	if err != nil {
		t.Fatal(err)
	}

	if items, total, err := service.ListPage(access.WithLibrary(ownerCtx, other.ID), library.ListOptions{Query: "ingredient:chicken"}); err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("other-library ingredients = %+v, %d, %v", items, total, err)
	}

	d := entries[4].Recipe
	d.IngredientSections[0].Ingredients[0].Name = "vegetable broth"

	updated, err := service.Update(ctx, entries[4].ID, entries[4].Revision, d)
	if err != nil {
		t.Fatal(err)
	}

	if count, err := service.Count(ctx, library.ListOptions{Query: `ingredient:"chicken broth"`}); err != nil || count != 0 {
		t.Fatalf("updated ingredient count = %d, %v", count, err)
	}

	restored, err := service.RestoreRevision(ctx, updated.ID, 1, updated.Revision)
	if err != nil {
		t.Fatal(err)
	}

	trashed, err := service.Trash(ctx, restored.ID, restored.Revision)
	if err != nil {
		t.Fatal(err)
	}

	if count, err := service.Count(ctx, library.ListOptions{Query: `ingredient:"chicken broth"`, State: library.StateTrash}); err != nil || count != 1 {
		t.Fatalf("trash ingredient count = %d, %v", count, err)
	}

	if err := service.Purge(ctx, trashed.ID, trashed.Revision); err != nil {
		t.Fatal(err)
	}

	if err := store.RebuildSearchIndex(ctx); err != nil {
		t.Fatal(err)
	}

	if count, err := service.Count(ctx, library.ListOptions{Query: `ingredient:chicken`}); err != nil || count != 4 {
		t.Fatalf("rebuilt ingredient count = %d, %v", count, err)
	}
}

func TestIngredientMigrationBackfillsRetainedNamesWithoutNewRevisions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	if err := registerSearchFunctions(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "ingredient-upgrade.db")

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

	for index, migration := range migrations[:16] {
		if err := partial.applyMigration(ctx, migration, index == 0); err != nil {
			t.Fatal(err)
		}
	}

	document := testfixture.Document()
	document.IngredientSections = []recipe.IngredientSection{{ID: "ingredients", Ingredients: []recipe.Ingredient{{ID: "raw", SourceText: "unparsed chicken line"}}}}
	document.InstructionSections = nil
	now := time.Now().UTC()

	entry := library.Entry{ID: "ingredient-backfill-fixture", Recipe: document, Revision: 1, CreatedAt: now, UpdatedAt: now}
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

	items, total, err := service.ListPage(ctx, library.ListOptions{Query: "ingredient:chicken"})
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != entry.ID || items[0].Revision != 1 {
		t.Fatalf("ingredient backfill = %+v, %d, %v", items, total, err)
	}
}
