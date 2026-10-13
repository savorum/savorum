package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
)

// TestCachedCookingStateFollowsTheRecords: the two cached columns follow the
// cookings through every write path.
func TestCachedCookingStateFollowsTheRecords(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	seedRecipe(ctx, t, store, "rcooked")

	record := func(id, date string) library.CookingRecord {
		created, err := store.RecordCooking(ctx, "rcooked", library.DefaultUserID, library.CookingRecord{
			ID: id, CookedOn: library.Date(date), CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}

		return created
	}
	cached := func() (uint64, string) {
		t.Helper()

		var (
			times uint64
			last  sql.NullString
		)
		if err := store.db.QueryRowContext(ctx,
			"SELECT times_cooked, last_cooked FROM recipe_user_state WHERE entry_id = 'rcooked'").
			Scan(&times, &last); err != nil {
			t.Fatal(err)
		}

		return times, last.String
	}

	first := record("cone", "2026-07-12")
	record("ctwo", "2026-08-20")

	if times, last := cached(); times != 2 || last != "2026-08-20" {
		t.Fatalf("after two cookings the cache says %d times, last %q", times, last)
	}

	// Correcting a date moves the most recent cooking with it.
	first.CookedOn = "2026-09-01"
	if _, err := store.ReplaceCooking(ctx, "rcooked", library.DefaultUserID, first); err != nil {
		t.Fatal(err)
	}

	if times, last := cached(); times != 2 || last != "2026-09-01" {
		t.Fatalf("after correcting a date the cache says %d times, last %q", times, last)
	}

	if err := store.DeleteCooking(ctx, "rcooked", library.DefaultUserID, first.ID); err != nil {
		t.Fatal(err)
	}

	if times, last := cached(); times != 1 || last != "2026-08-20" {
		t.Fatalf("after forgetting one cooking the cache says %d times, last %q", times, last)
	}
}

func TestImportedCookCountBaselineAddsToActualCookingRecords(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	seedRecipe(ctx, t, store, "legacy-cooks")

	if err := store.SetImportedCookCountBaseline(ctx, "legacy-cooks", library.DefaultUserID, library.ImportedCookCountBaseline{Count: 37, Source: "CookbookManager.com", SourceKey: "12345"}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.RecordCooking(ctx, "legacy-cooks", library.DefaultUserID, library.CookingRecord{ID: "actual", CookedOn: library.Date("2026-08-29"), CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	history, err := store.ListCookings(ctx, "legacy-cooks", library.DefaultUserID, library.CookingListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if history.ImportedBaseline != 37 || history.TimesCooked != 38 || len(history.Records) != 1 {
		t.Fatalf("history = %#v", history)
	}
}

func TestCookingSessionRetryReturnsTheOriginalRecord(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	seedRecipe(ctx, t, store, "retry-cooking")

	session := "session-retry"

	first, err := store.RecordCooking(ctx, "retry-cooking", library.DefaultUserID, library.CookingRecord{
		ID: "first", SessionID: &session, CookedOn: library.Date("2026-09-04"), CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	second, err := store.RecordCooking(ctx, "retry-cooking", library.DefaultUserID, library.CookingRecord{
		ID: "second", SessionID: &session, CookedOn: library.Date("2026-09-04"), CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if first.ID != second.ID || first.ID != "first" {
		t.Fatalf("retry records = %#v and %#v", first, second)
	}

	history, err := store.ListCookings(ctx, "retry-cooking", library.DefaultUserID, library.CookingListOptions{Limit: 10})
	if err != nil || history.TimesCooked != 1 || len(history.Records) != 1 {
		t.Fatalf("history = %#v, err = %v", history, err)
	}
}

// TestRememberedBatchIsOneAnswer keeps the database from holding a wanted yield
// and a wanted factor at once, which would be two answers to one question.
func TestRememberedBatchIsOneAnswer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	seedRecipe(ctx, t, store, "rbatch")

	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO recipe_user_state (
			entry_id, user_id, favorite, rating, personal_note, last_cooked,
			times_cooked, preferred_yield, preferred_scale
		) VALUES ('rbatch', 'local', 0, NULL, '', NULL, 0, '6', '3/2')`); err == nil {
		t.Fatal("the database accepted a remembered yield and a remembered scale together")
	}
}

func openStore(ctx context.Context, t *testing.T) *Store {
	t.Helper()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	return store
}

func seedRecipe(ctx context.Context, t *testing.T, store *Store, id library.EntryID) {
	t.Helper()

	document := testfixture.Document()
	document.ID = recipe.RecipeID("d" + string(id))
	timestamp := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)

	entry := library.Entry{ID: id, Recipe: document, Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}
	if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}
}

func TestCookingLocalTimeSurvivesAndOrdersWithinDay(t *testing.T) {
	ctx := context.Background()
	store := openStore(ctx, t)
	seedRecipe(ctx, t, store, "clock-cooking")

	for i, clock := range []string{"", "08:30", "19:15"} {
		_, err := store.RecordCooking(ctx, "clock-cooking", library.DefaultUserID, library.CookingRecord{ID: fmt.Sprintf("clock-%d", i), CookedOn: library.Date("2026-08-30"), CookedTime: clock, CreatedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
	}

	history, err := store.ListCookings(ctx, "clock-cooking", library.DefaultUserID, library.CookingListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(history.Records) != 3 || history.Records[0].CookedTime != "19:15" || history.Records[2].CookedTime != "" {
		t.Fatalf("clock order/history: %+v", history.Records)
	}
}
