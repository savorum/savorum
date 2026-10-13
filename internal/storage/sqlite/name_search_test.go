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

func TestNameTyposKeepExactRankingAllWordsAndStructuredFilters(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openStore(ctx, t)
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	service, err := library.NewService(store, units.DefaultCatalog(), library.WithClock(func() time.Time {
		now = now.Add(time.Second)
		return now
	}))
	if err != nil {
		t.Fatal(err)
	}

	entries := []library.Entry{}

	for index, title := range []string{"Chocolate cake", "Chocplate cake", "Broth", "Simple broth"} {
		d := testfixture.Document()
		d.ID, d.Title = recipe.RecipeID(fmt.Sprintf("name-search-%d", index)), title

		d.Description = "A rich cake"
		if index >= 2 {
			d.Description = "Chocolate flour Julia Child are mentioned here"
		}

		d.Authors = nil
		if index == 3 {
			d.Authors = []recipe.Author{{Name: "Julia Child"}}
		}

		entry, err := service.Create(ctx, d)
		if err != nil {
			t.Fatal(err)
		}

		entries = append(entries, entry)
	}

	items, total, err := service.ListPage(ctx, library.ListOptions{Query: "chocolate", Limit: 10})
	if err != nil || total != 4 || len(items) != 4 || items[0].ID != entries[0].ID || items[3].ID != entries[1].ID {
		t.Fatalf("exact-first order = %+v, %d, %v", items, total, err)
	}

	for _, check := range []struct {
		query string
		total int
	}{
		{"choclate", 2},
		{"choclate flour", 2},
		{"choclate missingterm", 0},
		{"julix child", 1},
		{`author:"Julix Child"`, 0},
		{"Јulia", 0},
		{"Weekned", 0},
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
				if err != nil || total != check.total || len(page) != min(1, check.total-offset) {
					t.Fatalf("page %q sort %s offset %d = %+v, %d, %v", check.query, sort, offset, page, total, err)
				}
			}
		}
	}

	tag, err := service.CreateTag(ctx, library.Tag{Name: "Weekend"})
	if err != nil {
		t.Fatal(err)
	}

	organization, err := service.GetOrganization(ctx, string(entries[0].ID))
	if err != nil {
		t.Fatal(err)
	}

	organization.TagIDs = []string{tag.ID}
	if _, err := service.ReplaceOrganization(ctx, string(entries[0].ID), organization.Revision, organization, nil); err != nil {
		t.Fatal(err)
	}

	items, total, err = service.ListPage(ctx, library.ListOptions{Query: "choclate tag:" + tag.ID})
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != entries[0].ID {
		t.Fatalf("exact tag plus fuzzy title = %+v, %d, %v", items, total, err)
	}

	if items, total, err := service.ListPage(ctx, library.ListOptions{Query: "choclate tag:Weekned"}); err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("tag gained approximate matching: %+v, %d, %v", items, total, err)
	}

	before, _, err := service.ListPage(ctx, library.ListOptions{Query: "choclate"})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.RebuildSearchIndex(ctx); err != nil {
		t.Fatal(err)
	}

	after, _, err := service.ListPage(ctx, library.ListOptions{Query: "choclate"})
	if err != nil || !slices.EqualFunc(before, after, func(left, right library.Summary) bool { return left.ID == right.ID }) {
		t.Fatalf("rebuild changed typo matches: %+v, %v", after, err)
	}
}

func TestUnspacedAndMarkedTitleRunsUseRuneEdits(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openStore(ctx, t)

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	for index, pair := range [][2]string{
		{"香草巧克力蛋糕", "香草巧古力蛋糕"},
		{"Σοκολάτα κέικ", "Σοκοπάτα"},
		{"Шоколадный торт", "Шокопадный"},
		{"ขนมช็อกโกแลต", "ขนมช็อกโกแลท"},
		{"حلويات شوكولاتة", "شوكولاته"},
		{"עוגת שוקולד", "שוקולט"},
		{"चॉकलेट केक", "चॉकलेत"},
	} {
		d := testfixture.Document()
		d.ID, d.Title, d.Authors = recipe.RecipeID(fmt.Sprintf("script-search-%d", index)), pair[0], nil

		entry, err := service.Create(ctx, d)
		if err != nil {
			t.Fatal(err)
		}

		items, total, err := service.ListPage(ctx, library.ListOptions{Query: pair[1]})
		if err != nil || total != 1 || len(items) != 1 || items[0].ID != entry.ID {
			t.Fatalf("script query %q = %+v, %d, %v", pair[1], items, total, err)
		}
	}
}
