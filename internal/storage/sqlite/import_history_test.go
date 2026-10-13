package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestImportEventsAreAtomicImmutableAndSanitized(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	now := time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC)
	document := testfixture.Document()
	document.ID = "import-history-document"

	entry, err := store.ImportMany(ctx, []library.ImportMutation{{
		ID: "rimporthistory", Recipe: document, Timestamp: now,
		History: library.ImportContext{ //nolint:gosec // the credentials are the fixture: this asserts they are stripped
			Importer: "example", ImporterVersion: "2.0", JobID: "job-1", InputKind: "url",
			DisplayName: "https://example.test/recipes/dinner.json?token=private",
			SourceURL:   "https://user:password@example.test/recipes/dinner?signature=private#secret",
			Diagnostics: []library.ImportDiagnostic{{Code: "loss", Class: "warning", Message: "cookie=private; see https://example.test/a?token=private", Count: 1}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	events, err := store.ListImportEvents(ctx, entry[0].ID, library.RevisionListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 1 || events[0].ID != "rimporthistory:1" || events[0].BaselineRevision != 1 || events[0].JobID != "job-1" {
		t.Fatalf("event = %#v", events)
	}

	if strings.Contains(events[0].SourceURL+events[0].DisplayName+events[0].Diagnostics[0].Message, "private") || strings.Contains(events[0].SourceURL, "user:") {
		t.Fatalf("event persisted sensitive input: %#v", events[0])
	}

	if _, err := store.db.ExecContext(t.Context(), "UPDATE import_events SET event_json = '{}' WHERE id = ?", events[0].ID); err == nil {
		t.Fatal("import event unexpectedly changed")
	}

	document.Title = "Updated imported recipe"

	updated, err := store.ImportMany(ctx, []library.ImportMutation{{
		ID: entry[0].ID, ExpectedRevision: 1, Recipe: document, Timestamp: now.Add(time.Hour),
	}})
	if err != nil {
		t.Fatal(err)
	}

	if updated[0].Revision != 2 {
		t.Fatalf("updated revision = %d", updated[0].Revision)
	}

	events, err = store.ListImportEvents(ctx, entry[0].ID, library.RevisionListOptions{Limit: 10})
	if err != nil || len(events) != 2 || events[1].BaselineRevision != 1 {
		t.Fatalf("events after refresh = %#v, %v", events, err)
	}
}

func TestFailedImportDoesNotLeaveAnEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	now := time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC)
	first, second := testfixture.Document(), testfixture.Document()
	first.ID, second.ID = "failed-event-first", "failed-event-second"

	_, err := store.ImportMany(ctx, []library.ImportMutation{
		{ID: "same-entry", Recipe: first, Timestamp: now},
		{ID: "same-entry", Recipe: second, Timestamp: now},
	})
	if err == nil {
		t.Fatal("conflicting import unexpectedly succeeded")
	}

	var events int
	if err := store.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM import_events").Scan(&events); err != nil {
		t.Fatal(err)
	}

	if events != 0 {
		t.Fatalf("failed import left %d event(s)", events)
	}
}

func TestSourceBaselineIsReadFromDedicatedStoreAndKeepsAssetsReachable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	document := testfixture.Document()
	document.ID = "baseline-reader"
	document.Assets = []recipe.Asset{{ID: "hero", Path: "assets/hero.jpg", MediaType: "image/jpeg", SHA256: strings.Repeat("a", 64), Size: 3}}
	snapshot := document
	// This source photograph is deliberately absent from the chosen recipe.
	document.Assets = nil
	now := time.Now().UTC()

	hash := strings.Repeat("a", 64)
	if _, err := store.db.ExecContext(ctx, "INSERT INTO assets(id,content_hash,media_type,size,storage_key,created_at,orphaned_at) VALUES(?,?,?,?,?,?,?)", "sha256:"+hash, hash, "image/jpeg", 3, "baseline-image", formatTime(now), formatTime(now)); err != nil {
		t.Fatal(err)
	}

	if _, err := store.ImportMany(ctx, []library.ImportMutation{{ID: "baseline-entry", Recipe: document, SourceSnapshot: &snapshot, Timestamp: now}}); err != nil {
		t.Fatal(err)
	}

	baseline, err := store.GetSourceImportBaseline(ctx, "baseline-entry")
	if err != nil || baseline.Revision != 1 || baseline.Document.ID != document.ID {
		t.Fatalf("baseline=%#v err=%v", baseline, err)
	}

	var references int
	if err := store.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM recipe_source_import_baseline_assets WHERE entry_id = ?", "baseline-entry").Scan(&references); err != nil {
		t.Fatal(err)
	}

	if references != 1 {
		t.Fatalf("baseline asset references=%d", references)
	}

	orphaned, err := store.ListOrphanedAssets(ctx, now.Add(time.Hour), 10)
	if err != nil || len(orphaned) != 0 {
		t.Fatalf("source-only image became collectible: %+v %v", orphaned, err)
	}
}
