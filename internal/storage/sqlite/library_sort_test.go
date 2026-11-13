package sqlite

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func TestLibrarySortsKeepTheirOrderAcrossFilteredPages(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openStore(ctx, t)
	base := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)

	times := []*recipe.DurationExpression{
		{Value: "PT10M"},
		{Value: "PT1H"},
		{Value: "PT20M"},
		nil,
		{Min: "PT10M", Max: "PT20M"},
	}
	for index, duration := range times {
		id := library.EntryID(string(rune('a' + index)))

		document := recipe.Document{
			SchemaVersion: recipe.CurrentDocumentSchemaVersion, ID: recipe.RecipeID(id), Title: "Rice " + string(id),
			Times: &recipe.Times{Prep: duration},
		}
		if err := store.Create(ctx, library.Entry{
			ID: id, Recipe: document, Revision: 1,
			CreatedAt: base.Add(time.Duration(index) * time.Hour), UpdatedAt: base.Add(time.Duration(5-index) * time.Hour),
		}, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}
	}

	for id, dates := range map[string][]library.Date{
		"a": {"2026-09-10"}, "b": {"2026-08-01", "2026-08-02", "2026-09-01"},
	} {
		for index, date := range dates {
			if _, err := store.RecordCooking(ctx, id, library.DefaultUserID, library.CookingRecord{
				ID: fmt.Sprintf("%s-%d", id, index), CookedOn: date, CreatedAt: base,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}

	for _, value := range []struct {
		id, user string
		count    uint64
	}{
		{"c", library.DefaultUserID, 7}, {"d", "other", 100},
	} {
		if err := store.SetImportedCookCountBaseline(ctx, value.id, value.user, library.ImportedCookCountBaseline{Count: value.count, Source: "test"}); err != nil {
			t.Fatal(err)
		}
	}

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	for _, check := range []struct {
		sort library.ListSort
		want []library.EntryID
	}{
		{library.ListSortCreatedDesc, []library.EntryID{"e", "d", "c", "b", "a"}},
		{library.ListSortCookedAsc, []library.EntryID{"b", "a", "c", "d", "e"}},
		{library.ListSortActiveTimeAsc, []library.EntryID{"a", "c", "b", "d", "e"}},
		{library.ListSortTimesCookedDesc, []library.EntryID{"c", "b", "a", "d", "e"}},
	} {
		for _, query := range []string{"", "Rice"} {
			t.Run(string(check.sort)+"/"+query, func(t *testing.T) {
				var got []library.EntryID

				for offset := 0; offset < len(times); offset += 2 {
					options := library.ListOptions{Sort: check.sort, Query: query, Limit: 2, Offset: offset}

					page, total, err := service.ListPage(ctx, options)
					if err != nil || total != len(times) {
						t.Fatalf("page total = %d, %v", total, err)
					}

					plain, err := service.List(ctx, options)
					if err != nil {
						t.Fatal(err)
					}

					for i, summary := range page {
						if summary.ID != plain[i].ID {
							t.Fatalf("page and list disagree: %v, %v", page, plain)
						}

						got = append(got, summary.ID)
					}
				}

				if !slices.Equal(got, check.want) {
					t.Fatalf("order = %v, want %v", got, check.want)
				}
			})
		}
	}
}

func TestShuffleIsStableAcrossPagesAndChangesWithItsSeed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	store := openStore(ctx, t)
	for index := range 12 {
		seedRecipe(ctx, t, store, library.EntryID(fmt.Sprintf("shuffle-%02d", index)))
	}

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	read := func(seed int64, size int) []library.EntryID {
		t.Helper()

		var ids []library.EntryID

		for offset := 0; offset < 12; offset += size {
			page, total, err := service.ListPage(ctx, library.ListOptions{Sort: library.ListSortRandom, Seed: seed, Limit: size, Offset: offset})
			if err != nil || total != 12 {
				t.Fatalf("shuffle total = %d, %v", total, err)
			}

			for _, summary := range page {
				ids = append(ids, summary.ID)
			}
		}

		return ids
	}

	first := read(123, 12)
	if paged := read(123, 3); !slices.Equal(first, paged) {
		t.Fatalf("shuffle pagination = %v, want %v", paged, first)
	}

	if again := read(123, 12); !slices.Equal(first, again) {
		t.Fatalf("shuffle changed on reread: %v", again)
	}

	if other := read(456, 12); slices.Equal(first, other) {
		t.Fatal("changing the seed kept the same shuffle")
	}

	unique := slices.Clone(first)
	slices.Sort(unique)

	if len(slices.Compact(unique)) != 12 {
		t.Fatalf("shuffle repeats recipes: %v", first)
	}
}
