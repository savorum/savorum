package sqlite

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestRankedPagesFilterBeforeLimitingAndKeepSummaryState(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openStore(ctx, t)
	base := time.Date(2026, time.September, 6, 0, 0, 0, 0, time.UTC)

	for index := range 10 {
		id := library.EntryID(fmt.Sprintf("rank-%02d", index))
		document := testfixture.Document()
		document.ID = recipe.RecipeID(id)
		document.Title = "Saffron rice"

		entry := library.Entry{ID: id, Recipe: document, Revision: 1, CreatedAt: base, UpdatedAt: base}
		if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}

		if _, err := store.ReplaceOrganization(ctx, string(id), library.DefaultUserID, 1, library.Organization{State: library.UserState{Favorite: index%2 == 0}}, nil, base); err != nil {
			t.Fatal(err)
		}
	}

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	trashed, err := service.Get(ctx, "rank-02")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Trash(ctx, trashed.ID, trashed.Revision); err != nil {
		t.Fatal(err)
	}

	options := library.ListOptions{Query: "saffron favorite:true", Sort: library.ListSortRelevance, Limit: 2}

	total, err := service.Count(ctx, options)
	if err != nil || total != 4 {
		t.Fatalf("count = %d, %v; want 4", total, err)
	}

	want := []library.EntryID{"rank-00", "rank-04", "rank-06", "rank-08"}

	var got []library.EntryID

	for offset := 0; offset <= total; offset += options.Limit {
		options.Offset = offset

		page, pageTotal, err := service.ListPage(ctx, options)
		if err != nil {
			t.Fatal(err)
		}

		if pageTotal != total {
			t.Fatalf("page total at offset %d = %d, want %d", offset, pageTotal, total)
		}

		for _, summary := range page {
			if !summary.Favorite || summary.Title != "Saffron rice" {
				t.Fatalf("lost summary state: %+v", summary)
			}

			got = append(got, summary.ID)
		}
	}

	if !slices.Equal(got, want) {
		t.Fatalf("pages = %v, want %v", got, want)
	}

	unranked, unrankedTotal, err := service.ListPage(ctx, library.ListOptions{
		Query: "saffron", Sort: library.ListSortUpdatedDesc, Limit: 3,
	})
	if err != nil || len(unranked) != 3 || unrankedTotal != 9 {
		t.Fatalf("unranked page = %d items, total %d, %v; want 3 items, total 9", len(unranked), unrankedTotal, err)
	}

	trashOptions := library.ListOptions{Query: "saffron", State: library.StateTrash, Sort: library.ListSortRelevance, Limit: 2}

	trash, trashTotal, err := service.ListPage(ctx, trashOptions)
	if err != nil || len(trash) != 1 || trash[0].ID != "rank-02" || trash[0].Favorite || trashTotal != 1 {
		t.Fatalf("ranked trash page = %+v total %d, %v", trash, trashTotal, err)
	}
}
