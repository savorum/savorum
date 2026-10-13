package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestSearchRankingOrganizationAndRebuild(t *testing.T) {
	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	var entries []library.Entry

	for i, title := range []string{"Saffron supper", "Rice supper", "Simple supper", "Garden supper"} {
		d := testfixture.Document()
		d.ID = recipe.RecipeID(fmt.Sprintf("search-fixture-%d", i+1))

		d.Title = title
		if i == 1 {
			d.IngredientSections[0].Ingredients[0].Name = "saffron"
		}

		if i == 2 {
			d.Description = "Serve after a discussion about saffron."
		}

		d.Cuisines = []string{"Mediterranean"}
		d.Authors = []recipe.Author{{Name: "Renée"}}

		e, err := service.Create(ctx, d)
		if err != nil {
			t.Fatal(err)
		}

		entries = append(entries, e)
	}

	query := func(q string) []library.Summary {
		t.Helper()

		v, err := service.List(ctx, library.ListOptions{Query: q, Limit: 25})
		if err != nil {
			t.Fatal(err)
		}

		return v
	}

	got := query("saffron")
	if len(got) != 3 || got[0].ID != entries[0].ID || got[1].ID != entries[1].ID {
		t.Fatalf("title/ingredient ranking: %+v", got)
	}

	if got := query("renee Mediterranean"); len(got) != 4 {
		t.Fatalf("diacritics/multi-field tokens: %d", len(got))
	}

	if _, err := store.db.ExecContext(t.Context(), `INSERT INTO tags(id,name,normalized_name,revision,created_at,updated_at) VALUES('tag-search','Weekend','weekend',1,'2026-09-05T00:00:00Z','2026-09-05T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(t.Context(), `INSERT INTO recipe_tags(entry_id,tag_id) VALUES(?,'tag-search')`, entries[0].ID); err != nil {
		t.Fatal(err)
	}

	if got := query("saffron tag:tag-search"); len(got) != 1 || got[0].ID != entries[0].ID {
		t.Fatal(got)
	}

	if got := query("Weekend"); len(got) != 1 {
		t.Fatal(got)
	}

	if _, err := store.db.ExecContext(t.Context(), `UPDATE tags SET name='Celebration',normalized_name='celebration' WHERE id='tag-search'`); err != nil {
		t.Fatal(err)
	}

	if len(query("Weekend")) != 0 || len(query("Celebration")) != 1 {
		t.Fatal("renamed organization index stale")
	}

	if _, err := service.Trash(ctx, entries[0].ID, 1); err != nil {
		t.Fatal(err)
	}

	if len(query("Celebration")) != 0 {
		t.Fatal("trash appeared in search")
	}

	before := query("saffron")

	if _, err := store.db.ExecContext(t.Context(), `DELETE FROM library_recipes_fts`); err != nil {
		t.Fatal(err)
	}

	if len(query("saffron")) != 0 {
		t.Fatal("test did not clear index")
	}

	if err := store.RebuildSearchIndex(ctx); err != nil {
		t.Fatal(err)
	}

	after := query("saffron")
	if len(after) != len(before) || after[0].ID != before[0].ID {
		t.Fatalf("rebuild changed results: %+v", after)
	}

	for _, e := range after {
		if e.Revision != 1 {
			t.Fatal("rebuilding created recipe revision")
		}
	}
}

func TestSearchBookLifecycleAndStablePages(t *testing.T) {
	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	service, _ := library.NewService(store, units.DefaultCatalog())

	var members []library.BookEntry

	for i := range 3 {
		d := testfixture.Document()
		d.ID = recipe.RecipeID(fmt.Sprintf("book-search-%d", i))

		entry, err := service.Create(ctx, d)
		if err != nil {
			t.Fatal(err)
		}

		members = append(members, library.BookEntry{EntryID: entry.ID, Position: i + 1})
	}

	book, err := service.CreateBook(ctx, library.Book{Kind: library.BookPersonal, Title: "Weekends", Entries: members})
	if err != nil {
		t.Fatal(err)
	}

	query := func(text string, offset, limit int) []library.Summary {
		t.Helper()

		items, err := service.List(ctx, library.ListOptions{Query: text, Offset: offset, Limit: limit})
		if err != nil {
			t.Fatal(err)
		}

		return items
	}

	all := query("Weekends", 0, 3)
	if len(all) != 3 {
		t.Fatal(all)
	}

	for i := range 3 {
		page := query("Weekends", i, 1)
		if len(page) != 1 || page[0].ID != all[i].ID {
			t.Fatal("unstable relevance pagination", page)
		}
	}

	book.Title = "Celebrations"

	book, err = service.ReplaceBook(ctx, book.ID, book.Revision, book)
	if err != nil {
		t.Fatal(err)
	}

	if len(query("Weekends", 0, 3)) != 0 || len(query("Celebrations", 0, 3)) != 3 {
		t.Fatal("book rename left stale search projection")
	}

	if err := service.DeleteBook(ctx, book.ID, book.Revision); err != nil {
		t.Fatal(err)
	}

	if len(query("Celebrations", 0, 3)) != 0 || len(query("book:"+book.ID, 0, 3)) != 0 {
		t.Fatal("trashed curated book still searchable")
	}

	if _, err := service.RestoreBook(ctx, book.ID, book.Revision+1); err != nil {
		t.Fatal(err)
	}

	if len(query("Celebrations", 0, 3)) != 3 {
		t.Fatal("book restoration not reindexed")
	}
}
