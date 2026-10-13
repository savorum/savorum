package sqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/automation"
	"github.com/savorum/savorum/internal/event"
	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/idempotency"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/pantry"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
	"github.com/savorum/savorum/internal/webhook"
)

func integrationFixture(t *testing.T) (*Store, context.Context, *library.Service) {
	t.Helper()
	s := openStore(t.Context(), t)

	owner, err := s.BootstrapAdmin(t.Context(), "integration-owner", "Owner", "a long integration password")
	if err != nil {
		t.Fatal(err)
	}

	ctx := access.WithLibrary(identity.WithPrincipal(t.Context(), owner), "default")

	recipes, err := library.NewService(s, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	return s, ctx, recipes
}

func TestDomainEventsCommitRollbackFilteringAndRevocation(t *testing.T) {
	s, ctx, recipes := integrationFixture(t)

	before, err := s.ReadEvents(ctx, "", 100)
	if err != nil || !before.Resync {
		t.Fatalf("initial resync: %+v %v", before, err)
	}

	entry, err := recipes.Create(ctx, testfixture.Document())
	if err != nil {
		t.Fatal(err)
	}

	page, err := s.ReadEvents(ctx, before.Cursor, 100)
	if err != nil || len(page.Events) != 1 {
		t.Fatalf("committed fact: %+v %v", page, err)
	}

	fact := page.Events[0]
	if fact.Type != "recipe.created" || fact.Version != 1 || fact.LibraryID != "default" || fact.Resource.ID != string(entry.ID) || fact.Payload.Revision != 1 {
		t.Fatalf("envelope: %+v", fact)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err = appendDomainEvent(ctx, tx, "recipe.updated", "default", "recipe", string(entry.ID), event.Payload{Revision: 99}); err != nil {
		t.Fatal(err)
	}

	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	if _, err = recipes.Trash(ctx, entry.ID, 99); !errors.Is(err, library.ErrPreconditionFailed) {
		t.Fatalf("stale write: %v", err)
	}

	unchanged, err := s.ReadEvents(ctx, page.Cursor, 100)
	if err != nil || len(unchanged.Events) != 0 {
		t.Fatalf("rolled-back fact escaped: %+v %v", unchanged, err)
	}

	grant, err := s.CreatePersonalToken(ctx, identity.TokenRequest{Name: "Read recipes", Scopes: []string{"recipes:read"}, Libraries: []string{"default"}}, identity.TokenPolicy{})
	if err != nil {
		t.Fatal(err)
	}

	principal, _, err := s.ResolvePersonalToken(ctx, grant.Secret)
	if err != nil {
		t.Fatal(err)
	}

	tokenCtx := access.WithLibrary(identity.WithPrincipal(t.Context(), principal), "default")
	if _, err = s.SavePantryEntry(ctx, pantry.Entry{ID: "salt", Name: "Salt", Amount: "about half"}, 0); err != nil {
		t.Fatal(err)
	}

	filtered, err := s.ReadEvents(tokenCtx, page.Cursor, 100)
	if err != nil || len(filtered.Events) != 0 || filtered.Cursor == page.Cursor {
		t.Fatalf("capability filtering/cursor: %+v %v", filtered, err)
	}

	if err = s.RevokePersonalToken(ctx, grant.ID); err != nil {
		t.Fatal(err)
	}

	if _, err = s.ReadEvents(tokenCtx, filtered.Cursor, 100); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("revoked stream: %v", err)
	}

	missing, err := s.ReadEvents(ctx, "evt_no-longer-retained", 100)
	if err != nil || !missing.Resync || missing.Cursor == "" {
		t.Fatalf("expired cursor: %+v %v", missing, err)
	}

	raw, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(raw, []byte(entry.Recipe.Title)) || bytes.Contains(raw, []byte(grant.Secret)) {
		t.Fatal("event included private content")
	}
}

func TestPantryRemainsApproximateScopedAndRevisionProtected(t *testing.T) {
	s, ctx, _ := integrationFixture(t)
	input := pantry.Entry{ID: "flour", Name: "Flour", Amount: "perhaps half a bag", Unit: "bag", Note: "Check before shopping", Location: "top shelf"}

	first, err := s.SavePantryEntry(ctx, input, 0)
	if err != nil || first.Revision != 1 || first.Amount != input.Amount {
		t.Fatalf("approximate presence: %+v %v", first, err)
	}

	first.Note = "Another member checked"

	next, err := s.SavePantryEntry(ctx, first, 1)
	if err != nil || next.Revision != 2 {
		t.Fatalf("second edit: %+v %v", next, err)
	}

	if _, err = s.SavePantryEntry(ctx, input, 1); !errors.Is(err, library.ErrPreconditionFailed) {
		t.Fatalf("lost update: %v", err)
	}

	if err = s.DeletePantryEntry(ctx, input.ID, 1); !errors.Is(err, library.ErrPreconditionFailed) {
		t.Fatalf("stale deletion: %v", err)
	}

	other, err := s.CreateLibrary(ctx, "Other pantry")
	if err != nil {
		t.Fatal(err)
	}

	entries, err := s.PantryEntries(access.WithLibrary(ctx, other.ID))
	if err != nil || len(entries) != 0 {
		t.Fatalf("cross-library pantry: %+v %v", entries, err)
	}

	owner, _ := identity.FromContext(ctx)

	viewer, err := s.CreateLocalUser(ctx, owner.UserID, "pantry-reader", "Reader", "a sufficiently long reader password")
	if err != nil {
		t.Fatal(err)
	}

	if err = s.SetLibraryMember(ctx, "default", viewer.UserID, access.Viewer, false); err != nil {
		t.Fatal(err)
	}

	readerCtx := access.WithLibrary(identity.WithPrincipal(t.Context(), viewer), "default")
	if _, err = s.SavePantryEntry(readerCtx, input, 2); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer wrote pantry: %v", err)
	}

	entries, err = s.PantryEntries(readerCtx)
	if err != nil || len(entries) != 1 || entries[0].Note != next.Note {
		t.Fatalf("preserved household edit: %+v %v", entries, err)
	}
}

func TestBulkPartialSuccessAndBookRevisionProgression(t *testing.T) {
	_, ctx, recipes := integrationFixture(t)

	entry, err := recipes.Create(ctx, testfixture.Document())
	if err != nil {
		t.Fatal(err)
	}

	stale := automation.BulkItem(ctx, recipes, automation.BulkRequest{Action: "trash"}, automation.Target{ID: string(entry.ID), Revision: 99})
	if stale.Code != "revision-conflict" || stale.CurrentRevision != 1 {
		t.Fatalf("stale item: %+v", stale)
	}

	missing := automation.BulkItem(ctx, recipes, automation.BulkRequest{Action: "trash"}, automation.Target{ID: "missing", Revision: 1})
	if missing.Code != "not-found" {
		t.Fatalf("missing item: %+v", missing)
	}

	book, err := recipes.CreateBook(ctx, library.Book{Title: "Dinner", Kind: library.BookPersonal})
	if err != nil {
		t.Fatal(err)
	}

	organization, err := recipes.GetOrganization(ctx, string(entry.ID))
	if err != nil {
		t.Fatal(err)
	}

	input := automation.BulkRequest{Action: "book", BookID: book.ID, BookRevision: book.Revision, Member: true}

	added := automation.BulkItem(ctx, recipes, input, automation.Target{ID: string(entry.ID), Revision: organization.Revision})
	if added.Status != "succeeded" || added.BookRevision != book.Revision+1 {
		t.Fatalf("book addition: %+v", added)
	}

	input.BookRevision = added.BookRevision

	unchanged := automation.BulkItem(ctx, recipes, input, automation.Target{ID: string(entry.ID), Revision: added.Revision})
	if unchanged.Status != "succeeded" || unchanged.BookRevision != added.BookRevision {
		t.Fatalf("no-op membership advanced book: %+v", unchanged)
	}

	trashed := automation.BulkItem(ctx, recipes, automation.BulkRequest{Action: "trash"}, automation.Target{ID: string(entry.ID), Revision: 1})
	if trashed.Status != "succeeded" || trashed.Revision != 2 {
		t.Fatalf("valid item after failures: %+v", trashed)
	}
}

func TestAutomationRetryReceiptRecoveryAndPrivateArtifact(t *testing.T) {
	s, ctx, recipes := integrationFixture(t)

	document := testfixture.Document()
	document.Assets = nil
	document.ImageRefs = nil

	document.PrimaryImage = ""
	for i := range document.InstructionSections {
		for j := range document.InstructionSections[i].Steps {
			document.InstructionSections[i].Steps[j].ImageRefs = nil
		}
	}

	entry, err := recipes.Create(ctx, document)
	if err != nil {
		t.Fatal(err)
	}

	exporter, err := export.NewService(recipes, nil)
	if err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "artifacts")
	receipts := idempotency.New(s)

	jobs, err := automation.New(s, recipes, nil, nil, exporter, receipts, root)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = jobs.Close() })

	input := automation.ExportInput{Format: string(export.FormatSREF), Recipes: []string{string(entry.ID)}}

	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}

	work, id, release, err := receipts.Begin(ctx, "export-job", "stable-key", body)
	if err != nil || id != "" {
		t.Fatalf("new receipt: %s %v", id, err)
	}

	record, err := jobs.StartExport(work, input)

	release()

	if err != nil {
		t.Fatal(err)
	}

	_, id, release, err = receipts.Begin(ctx, "export-job", "stable-key", body)
	release()

	if err != nil || id != record.Job.ID {
		t.Fatalf("retry produced another job: %s %v", id, err)
	}

	_, _, release, err = receipts.Begin(ctx, "export-job", "stable-key", []byte("different"))
	release()

	if !errors.Is(err, idempotency.ErrConflict) {
		t.Fatalf("key collision accepted: %v", err)
	}

	if err = jobs.ProcessPending(ctx); err != nil {
		t.Fatal(err)
	}

	done, err := jobs.Get(ctx, id)
	if err != nil || done.Job.State != "completed" {
		t.Fatalf("export: %+v %v", done.Job, err)
	}

	_, file, err := jobs.Download(ctx, id, done.Job.DownloadHandle)
	if err != nil {
		t.Fatal(err)
	}

	data, err := io.ReadAll(file)
	if closeErr := file.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	if err != nil {
		t.Fatal(err)
	}

	opened, err := sref.ReadPackage(data, sref.DefaultLimits())
	if err != nil || opened.Recipe.ID != document.ID {
		t.Fatalf("portable export artifact: %v", err)
	}

	info, err := os.Stat(filepath.Join(root, id+".artifact"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("artifact permissions: %v %v", info, err)
	}

	if _, _, err = jobs.Download(ctx, id, "guessed"); !errors.Is(err, automation.ErrNotFound) {
		t.Fatalf("unguarded artifact: %v", err)
	}

	bulk, err := jobs.StartBulk(ctx, automation.BulkRequest{Action: "trash", Items: []automation.Target{{ID: string(entry.ID), Revision: 1}}})
	if err != nil {
		t.Fatal(err)
	}

	bulk.Job.State = "running"

	bulk.InFlightID = string(entry.ID)
	if err = s.SaveAutomationJob(ctx, &bulk); err != nil {
		t.Fatal(err)
	}

	if err = s.RecoverAutomationJobs(ctx); err != nil {
		t.Fatal(err)
	}

	interrupted, err := jobs.Get(ctx, bulk.Job.ID)
	if err != nil {
		t.Fatal(err)
	}

	resumed, err := jobs.Resume(ctx, bulk.Job.ID, interrupted.Job.Revision)
	if err != nil || len(resumed.Results) != 1 || resumed.Results[0].Code != "outcome-unknown" {
		t.Fatalf("uncertain item replayed: %+v %v", resumed, err)
	}

	if err = jobs.ProcessPending(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err = recipes.Get(ctx, entry.ID); err != nil {
		t.Fatalf("unknown mutation repeated: %v", err)
	}

	if _, err = s.db.ExecContext(ctx, `UPDATE automation_jobs SET expires_at=0 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}

	if _, _, err = jobs.Download(ctx, id, done.Job.DownloadHandle); !errors.Is(err, automation.ErrNotFound) {
		t.Fatalf("expired artifact served: %v", err)
	}

	if err = jobs.ProcessPending(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err = os.Stat(filepath.Join(root, id+".artifact")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired artifact retained: %v", err)
	}
}

func TestWebhookOutboxHistoryRedeliveryAndFailureControl(t *testing.T) {
	s, ctx, recipes := integrationFixture(t)

	subscription, err := s.SaveWebhook(ctx, "default", webhook.Subscription{ID: "hook", Name: "Household", URL: "https://receiver.example/events", EventTypes: []string{"recipe.created"}, Enabled: true, SecretRef: "private"}, 0, "created")
	if err != nil {
		t.Fatal(err)
	}

	if _, err = recipes.Create(ctx, testfixture.Document()); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err = s.EnqueueWebhookEvents(ctx); err != nil {
			t.Fatal(err)
		}
	}

	deliveries, err := s.WebhookDeliveries(ctx, "default", subscription.ID, 0, 25)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("outbox replay duplicated delivery: %+v %v", deliveries, err)
	}

	delivery := deliveries[0]
	original := bytes.Clone(delivery.Body)

	attempt := webhook.Attempt{ID: "attempt-1", At: time.Now(), Status: 400, Code: "receiver-rejected", Excerpt: "bounded"}
	if err = s.FinishWebhookAttempt(ctx, delivery, attempt, "failed", nil, 1); err != nil {
		t.Fatal(err)
	}

	paused, err := s.WebhookSubscription(ctx, "default", subscription.ID)
	if err != nil || paused.Enabled || paused.PauseReason != "repeated-failures" {
		t.Fatalf("failure control: %+v %v", paused, err)
	}

	if err = s.RetryWebhookDelivery(ctx, "default", subscription.ID, delivery.ID); !errors.Is(err, webhook.ErrState) {
		t.Fatalf("paused redelivery: %v", err)
	}

	paused.Enabled = true
	if _, err = s.SaveWebhook(ctx, "default", paused, paused.Revision, "updated"); err != nil {
		t.Fatal(err)
	}

	if err = s.RetryWebhookDelivery(ctx, "default", subscription.ID, delivery.ID); err != nil {
		t.Fatal(err)
	}

	history, err := s.WebhookDelivery(ctx, "default", subscription.ID, delivery.ID)
	if err != nil || len(history.Attempts) != 1 || history.Delivery.State != "pending" || !bytes.Equal(original, history.Delivery.Body) {
		t.Fatalf("redelivery changed identity/body/history: %+v %v", history, err)
	}

	if _, err = s.SaveWebhook(ctx, "default", subscription, 1, "updated"); !errors.Is(err, webhook.ErrConflict) {
		t.Fatalf("stale settings: %v", err)
	}

	fresh, err := s.WebhookSubscription(ctx, "default", subscription.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err = s.DeleteWebhook(ctx, "default", subscription.ID, fresh.Revision); err != nil {
		t.Fatal(err)
	}

	due, err := s.DueWebhookDeliveries(ctx)
	if err != nil || len(due) != 0 {
		t.Fatalf("deleted subscription delivered: %+v %v", due, err)
	}
}

func TestBulkReadSnapshotsAreSeparateFromProgress(t *testing.T) {
	s, ctx, recipes := integrationFixture(t)

	entry, err := recipes.Create(ctx, testfixture.Document())
	if err != nil {
		t.Fatal(err)
	}

	jobs, err := automation.New(s, recipes, nil, nil, nil, idempotency.New(s), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = jobs.Close() })

	job, err := jobs.StartBulk(ctx, automation.BulkRequest{Action: "read", Items: []automation.Target{{ID: string(entry.ID)}}})
	if err != nil {
		t.Fatal(err)
	}

	if err = jobs.ProcessPending(ctx); err != nil {
		t.Fatal(err)
	}

	progress, err := jobs.Get(ctx, job.Job.ID)
	if err != nil || len(progress.Results) != 1 || progress.Results[0].Entry != nil {
		t.Fatalf("full recipe retained in progress: %+v %v", progress, err)
	}

	document := entry.Recipe

	document.Title = "Changed after the snapshot"
	if _, err = recipes.Update(ctx, entry.ID, entry.Revision, document); err != nil {
		t.Fatal(err)
	}

	results, err := jobs.Results(ctx, job.Job.ID, 0, 1)
	if err != nil || len(results) != 1 || results[0].Entry == nil || results[0].Entry.Recipe.Title != entry.Recipe.Title {
		t.Fatalf("immutable paged result: %+v %v", results, err)
	}

	if _, err = s.db.ExecContext(ctx, `UPDATE automation_jobs SET expires_at=0 WHERE id=?`, job.Job.ID); err != nil {
		t.Fatal(err)
	}

	if err = jobs.ProcessPending(ctx); err != nil {
		t.Fatal(err)
	}

	var retained int
	if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM automation_results WHERE job_id=?`, job.Job.ID).Scan(&retained); err != nil {
		t.Fatal(err)
	}

	if retained != 0 {
		t.Fatal("expired snapshots retained")
	}
}

func TestWebhookEventsReachOnlyTheLibraryOwner(t *testing.T) {
	s, ctx, _ := integrationFixture(t)

	owner, _ := identity.FromContext(ctx)

	viewer, err := s.CreateLocalUser(ctx, owner.UserID, "event-viewer", "Viewer", "a long viewer password phrase")
	if err != nil {
		t.Fatal(err)
	}

	if err = s.SetLibraryMember(ctx, "default", viewer.UserID, access.Viewer, false); err != nil {
		t.Fatal(err)
	}

	viewerCtx := access.WithLibrary(identity.WithPrincipal(t.Context(), viewer), "default")

	grant, err := s.CreatePersonalToken(ctx, identity.TokenRequest{Name: "Read library", Scopes: []string{"library:read"}, Libraries: []string{"default"}}, identity.TokenPolicy{})
	if err != nil {
		t.Fatal(err)
	}

	token, _, err := s.ResolvePersonalToken(ctx, grant.Secret)
	if err != nil {
		t.Fatal(err)
	}

	tokenCtx := access.WithLibrary(identity.WithPrincipal(t.Context(), token), "default")

	start, err := s.ReadEvents(ctx, "", 100)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err = appendDomainEvent(ctx, tx, "webhook.test", "default", "webhook", "wh_1", event.Payload{Test: true}); err != nil {
		t.Fatal(err)
	}

	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}

	readers := map[string]context.Context{"owner": ctx, "viewer": viewerCtx, "library:read token": tokenCtx}
	for name, want := range map[string]int{"owner": 1, "viewer": 0, "library:read token": 0} {
		page, err := s.ReadEvents(readers[name], start.Cursor, 100)
		if err != nil || len(page.Events) != want {
			t.Fatalf("%s saw %d webhook events: %+v %v", name, len(page.Events), page, err)
		}
	}
}

func TestEventCursorResyncsOnlyForMissedHistory(t *testing.T) {
	appendEvent := func(t *testing.T, s *Store, ctx context.Context, library string) {
		t.Helper()

		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}

		if err = appendDomainEvent(ctx, tx, "pantry.entry.updated", library, "pantry_entry", "salt", event.Payload{Revision: 1}); err != nil {
			t.Fatal(err)
		}

		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	expire := func(t *testing.T, s *Store) {
		t.Helper()

		if _, err := s.db.ExecContext(t.Context(), `UPDATE domain_events SET occurred_at=unixepoch()-700000 WHERE library_id='default'`); err != nil {
			t.Fatal(err)
		}
	}

	read := func(t *testing.T, s *Store, ctx context.Context, after string, events int, resync bool) event.Page {
		t.Helper()

		page, err := s.ReadEvents(ctx, after, 100)
		if err != nil || len(page.Events) != events || page.Resync != resync {
			t.Fatalf("after %q: %d events, resync %v: %+v %v", after, events, resync, page, err)
		}

		return page
	}

	t.Run("another library's earlier sequence is not missed history", func(t *testing.T) {
		s, ctx, _ := integrationFixture(t)
		appendEvent(t, s, ctx, "other")
		appendEvent(t, s, ctx, "default")
		read(t, s, ctx, "0", 1, false)
	})

	t.Run("an expired event after the cursor is missed history", func(t *testing.T) {
		s, ctx, _ := integrationFixture(t)
		appendEvent(t, s, ctx, "default")
		expire(t, s)
		appendEvent(t, s, ctx, "default")
		read(t, s, ctx, "0", 0, true)
	})

	t.Run("a cursor at an expired event with nothing after it stays put", func(t *testing.T) {
		s, ctx, _ := integrationFixture(t)
		appendEvent(t, s, ctx, "default")
		expire(t, s)
		resynced := read(t, s, ctx, "", 0, true)
		read(t, s, ctx, resynced.Cursor, 0, false)
	})

	t.Run("pruning records the library's horizon", func(t *testing.T) {
		s, ctx, _ := integrationFixture(t)
		appendEvent(t, s, ctx, "default")
		expire(t, s)

		if err := s.EnqueueWebhookEvents(ctx); err != nil {
			t.Fatal(err)
		}

		var remaining, horizon int
		if err := s.db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM domain_events WHERE library_id='default'), (SELECT COALESCE(MAX(sequence),0) FROM event_horizons WHERE library_id='default')`).Scan(&remaining, &horizon); err != nil {
			t.Fatal(err)
		}

		if remaining != 0 || horizon == 0 {
			t.Fatalf("after pruning: %d events remain, horizon %d", remaining, horizon)
		}

		read(t, s, ctx, "0", 0, true)
	})

	t.Run("pruned history resyncs once", func(t *testing.T) {
		s, ctx, _ := integrationFixture(t)
		appendEvent(t, s, ctx, "default")

		if _, err := s.db.ExecContext(t.Context(), `INSERT INTO event_horizons(library_id,sequence) SELECT library_id,MAX(sequence) FROM domain_events GROUP BY library_id`); err != nil {
			t.Fatal(err)
		}

		if _, err := s.db.ExecContext(t.Context(), `DELETE FROM domain_events`); err != nil {
			t.Fatal(err)
		}

		resynced := read(t, s, ctx, "0", 0, true)
		read(t, s, ctx, resynced.Cursor, 0, false)
		appendEvent(t, s, ctx, "default")
		read(t, s, ctx, resynced.Cursor, 1, false)
	})
}
