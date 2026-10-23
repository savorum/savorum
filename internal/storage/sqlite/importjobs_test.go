package sqlite

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/importjob"
	"github.com/savorum/savorum/internal/library"
)

func seedImportJob(ctx context.Context, t *testing.T, store *Store, id string, items []importjob.Item) {
	t.Helper()

	job := importjob.Job{
		ID: id, Importer: "CookbookManager", Source: "export.zip", State: importjob.StatePending,
		DuplicatePolicy: library.DuplicateSkip, TotalItems: len(items), BatchSize: 25,
		CreatedAt: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}
	for index := range items {
		items[index].JobID = id
		items[index].Position = index
	}

	if err := store.CreateImportJob(ctx, job, items); err != nil {
		t.Fatal(err)
	}
}

func pending(title string, payload []byte) importjob.Item {
	return importjob.Item{
		SourceItemID: "item-" + title, Title: title, Filename: "export.zip",
		State: importjob.ItemPending, Payload: payload,
	}
}

// "Review required" cuts across item state.
func TestImportJobItemFiltersFollowTheReaderNotTheState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	seedImportJob(ctx, t, store, "job-filters", []importjob.Item{
		pending("clean", nil), pending("short", nil), pending("existing", nil), pending("broken", nil), pending("preserved", nil),
	})

	now := time.Now().UTC()
	if err := store.SaveImportJobItems(ctx, "job-filters", []importjob.Item{
		{Position: 0, State: importjob.ItemImported, Outcome: library.ImportCreated, EntryID: "entry-1", Revision: 1, FinishedAt: &now},
		{
			Position: 1, State: importjob.ItemImported, Outcome: library.ImportCreated, EntryID: "entry-2", Revision: 1,
			ReviewRequired: true, DroppedAssets: 1, Diagnostics: []importer.Diagnostic{{
				Code: "photograph-unavailable", Class: importer.DiagnosticActionable,
				Message: "One photograph could not be downloaded.", Count: 1,
			}}, FinishedAt: &now,
		},
		{Position: 2, State: importjob.ItemSkipped, Outcome: library.ImportSkipped, FinishedAt: &now},
		{Position: 3, State: importjob.ItemFailed, ReviewRequired: true, Error: "invalid recipe", FinishedAt: &now},
		{
			Position: 4, State: importjob.ItemImported, Outcome: library.ImportCreated, EntryID: "entry-3", Revision: 1,
			Diagnostics: []importer.Diagnostic{{
				Code: "duration-not-iso", Class: importer.DiagnosticInformational,
				Message: "Duration wording was kept.", Count: 3,
			}}, FinishedAt: &now,
		},
	}); err != nil {
		t.Fatal(err)
	}

	for _, expectation := range []struct {
		filter    importjob.ItemFilter
		positions []int
	}{
		{importjob.ItemsAll, []int{0, 1, 2, 3, 4}},
		{importjob.ItemsImported, []int{0, 1, 4}},
		{importjob.ItemsSkipped, []int{2}},
		{importjob.ItemsFailed, []int{3}},
		// The imported-but-short recipe appears here as well as under imported.
		{importjob.ItemsReviewRequired, []int{1, 3}},
		{importjob.ItemsInformational, []int{4}},
	} {
		items, total, err := store.ListImportJobItems(ctx, "job-filters", importjob.ItemListOptions{
			Limit: 10, Filter: expectation.filter,
		})
		if err != nil {
			t.Fatal(err)
		}

		if total != len(expectation.positions) || len(items) != len(expectation.positions) {
			t.Fatalf("filter %q returned %d of %d", expectation.filter, len(items), total)
		}

		for index, item := range items {
			if item.Position != expectation.positions[index] {
				t.Fatalf("filter %q item %d = position %d, want %d", expectation.filter, index, item.Position, expectation.positions[index])
			}
		}
	}

	job, err := store.GetImportJob(ctx, "job-filters")
	if err != nil {
		t.Fatal(err)
	}
	// Progress is counted from the items on every read, so it cannot claim
	// something they do not say.
	want := importjob.Progress{
		Total: 5, Imported: 3, Skipped: 1, Failed: 1, ReviewRequired: 2,
		InformationalPreservation: 1, WithoutDiagnostics: 2, DiagnosticOccurrences: 4,
		Diagnostics: []importjob.DiagnosticSummary{
			{Code: "duration-not-iso", Class: importer.DiagnosticInformational, Message: "Duration wording was kept.", Occurrences: 3, Recipes: 1},
			{Code: "photograph-unavailable", Class: importer.DiagnosticActionable, Message: "One photograph could not be downloaded.", Occurrences: 1, Recipes: 1},
		},
	}
	if !reflect.DeepEqual(job.Progress, want) {
		t.Fatalf("progress = %#v, want %#v", job.Progress, want)
	}

	if job.Progress.Finished() != 5 {
		t.Fatalf("finished = %d", job.Progress.Finished())
	}
}

// A payload is large, it is nobody's business but recovery's, and an ordinary
// read of a job must not carry it around.
func TestOnlyRecoveryReadsAnImportPayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	seedImportJob(ctx, t, store, "job-payload", []importjob.Item{
		pending("resumable", []byte(`{"item_id":"item-1"}`)),
		pending("from-an-archive", nil),
	})

	items, _, err := store.ListImportJobItems(ctx, "job-payload", importjob.ItemListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	for _, item := range items {
		if len(item.Payload) != 0 {
			t.Fatalf("listing an import returned a payload for %q", item.Title)
		}
	}

	loaded, err := store.LoadImportJobItems(ctx, "job-payload", []importjob.ItemState{importjob.ItemPending})
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded) != 2 || !loaded[0].Resumable() || loaded[1].Resumable() {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestSaveImportJobItemsRejectsRevisionOutsideSQLiteIntegerRange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	seedImportJob(ctx, t, store, "job-oversize-revision", []importjob.Item{pending("cake", nil)})

	err := store.SaveImportJobItems(ctx, "job-oversize-revision", []importjob.Item{{
		Position: 0, State: importjob.ItemImported, Outcome: library.ImportCreated,
		EntryID: "entry-1", Revision: math.MaxInt64 + 1,
	}})
	if err == nil {
		t.Fatal("SaveImportJobItems accepted a revision outside SQLite's signed integer range")
	}
}

func TestImportJobRetainsMeasurementResolutionDecisions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)

	job := importjob.Job{
		ID: "job-resolution", Importer: "CookbookManager", Source: "export.zip",
		State: importjob.StatePending, DuplicatePolicy: library.DuplicateSkip, TotalItems: 1, BatchSize: 25,
		CreatedAt: time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC),
		ResolutionDecisions: []exchange.MeasurementResolutionRecord{{
			ResolutionKind: "unit_alias", InterpretationKind: "convention", Convention: "au",
			ParserVersion: "1", UnitRegistryVersion: "0.2.0", AffectedOccurrenceIDs: []string{"occurrence-1"},
			AffectedRecipeCount: 1, OriginalActionableCount: 3, ResultingActionableCount: 2, ImpactDigest: "digest",
		}},
	}
	if err := store.CreateImportJob(ctx, job, []importjob.Item{pending("cake", nil)}); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetImportJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(got.ResolutionDecisions, job.ResolutionDecisions) {
		t.Fatalf("decisions = %#v, want %#v", got.ResolutionDecisions, job.ResolutionDecisions)
	}
}

func TestImportProgressSeparatesRecipeAndDiagnosticCounts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	seedImportJob(ctx, t, store, "job-multiple-diagnostics", []importjob.Item{pending("mixed", nil)})

	now := time.Now().UTC()
	if err := store.SaveImportJobItems(ctx, "job-multiple-diagnostics", []importjob.Item{{
		Position: 0, State: importjob.ItemImported, ReviewRequired: true, FinishedAt: &now,
		Diagnostics: []importer.Diagnostic{
			{Code: "ingredient-review-required", Class: importer.DiagnosticActionable, Count: 4, Message: "Review ingredient amounts."},
			{Code: "duration-not-iso", Class: importer.DiagnosticInformational, Count: 2, Message: "Duration wording was kept."},
		},
	}}); err != nil {
		t.Fatal(err)
	}

	job, err := store.GetImportJob(ctx, "job-multiple-diagnostics")
	if err != nil {
		t.Fatal(err)
	}

	if job.Progress.ReviewRequired != 1 || job.Progress.InformationalPreservation != 0 ||
		job.Progress.DiagnosticOccurrences != 6 || len(job.Progress.Diagnostics) != 2 {
		t.Fatalf("mixed progress = %#v", job.Progress)
	}

	for _, summary := range job.Progress.Diagnostics {
		if summary.Recipes != 1 {
			t.Fatalf("summary double-counted its recipe: %#v", summary)
		}
	}
}

func TestImportJobKeepsTheBatchSizeItWasCreatedWith(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	seedImportJob(ctx, t, store, "job-batches", []importjob.Item{pending("one", nil)})

	job, err := store.GetImportJob(ctx, "job-batches")
	if err != nil {
		t.Fatal(err)
	}

	if job.BatchSize != 25 {
		t.Fatalf("batch size = %d, want 25", job.BatchSize)
	}
}

// Recording what happened to a recipe must not rewrite what the recipe is, or
// where it came from, or the copy kept in case the import has to be run again.
func TestRecordingProgressLeavesAnItemsIdentityAlone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	seedImportJob(ctx, t, store, "job-identity", []importjob.Item{pending("Lemon tart", []byte(`{"item_id":"item-1"}`))})

	started := time.Now().UTC()
	if err := store.SaveImportJobItems(ctx, "job-identity", []importjob.Item{
		{Position: 0, State: importjob.ItemRunning, StartedAt: &started},
	}); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadImportJobItems(ctx, "job-identity", []importjob.ItemState{importjob.ItemRunning})
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded) != 1 || loaded[0].Title != "Lemon tart" || loaded[0].SourceItemID != "item-Lemon tart" ||
		loaded[0].Filename != "export.zip" || !loaded[0].Resumable() {
		t.Fatalf("item after progress = %#v", loaded[0])
	}

	if loaded[0].StartedAt == nil || loaded[0].FinishedAt != nil {
		t.Fatalf("running item timestamps = %#v", loaded[0])
	}
}

func TestRecoveryInterruptsUnfinishedJobsAndLeavesFinishedOnesAlone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	seedImportJob(ctx, t, store, "job-running", []importjob.Item{pending("one", nil), pending("two", nil)})
	seedImportJob(ctx, t, store, "job-done", []importjob.Item{pending("three", nil)})

	started := time.Now().UTC()
	if err := store.SaveImportJobItems(ctx, "job-running", []importjob.Item{
		{Position: 0, State: importjob.ItemImported, Outcome: library.ImportCreated, EntryID: "entry-1", Revision: 1, FinishedAt: &started},
		{Position: 1, State: importjob.ItemRunning, StartedAt: &started},
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.UpdateImportJobState(ctx, "job-running", importjob.JobStateUpdate{State: importjob.StateRunning, StartedAt: &started}); err != nil {
		t.Fatal(err)
	}

	if err := store.SaveImportJobItems(ctx, "job-done", []importjob.Item{
		{Position: 0, State: importjob.ItemImported, Outcome: library.ImportCreated, EntryID: "entry-3", Revision: 1, FinishedAt: &started},
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.UpdateImportJobState(ctx, "job-done", importjob.JobStateUpdate{State: importjob.StateCompleted, FinishedAt: &started}); err != nil {
		t.Fatal(err)
	}

	interrupted, err := store.InterruptImportJobs(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	if interrupted != 1 {
		t.Fatalf("interrupted %d jobs, want 1", interrupted)
	}

	running, err := store.GetImportJob(ctx, "job-running")
	if err != nil {
		t.Fatal(err)
	}
	// The recipe that was mid-commit goes back to pending; the one already
	// committed is left exactly as it was.
	if running.State != importjob.StateInterrupted || running.Progress.Pending != 1 ||
		running.Progress.Running != 0 || running.Progress.Imported != 1 {
		t.Fatalf("interrupted job = %#v", running)
	}

	done, err := store.GetImportJob(ctx, "job-done")
	if err != nil {
		t.Fatal(err)
	}

	if done.State != importjob.StateCompleted || done.Progress.Imported != 1 {
		t.Fatalf("finished job = %#v", done)
	}
}

func TestCancellationTimestampIsRecordedOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	seedImportJob(ctx, t, store, "job-cancel", []importjob.Item{pending("one", nil)})

	first := time.Date(2026, 9, 1, 13, 0, 0, 0, time.UTC)

	later := first.Add(time.Hour)
	if err := store.RequestImportJobCancellation(ctx, "job-cancel", first); err != nil {
		t.Fatal(err)
	}

	if err := store.RequestImportJobCancellation(ctx, "job-cancel", later); err != nil {
		t.Fatal(err)
	}

	job, err := store.GetImportJob(ctx, "job-cancel")
	if err != nil {
		t.Fatal(err)
	}
	// When somebody asked is a fact about the first time they asked.
	if job.CancellationRequestedAt == nil || !job.CancellationRequestedAt.Equal(first) {
		t.Fatalf("cancellation requested at = %v", job.CancellationRequestedAt)
	}
}

func TestUnknownImportJobIsNotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store := openStore(ctx, t)
	if _, err := store.GetImportJob(ctx, "absent"); !errors.Is(err, importjob.ErrNotFound) {
		t.Fatalf("get error = %v", err)
	}

	if _, _, err := store.ListImportJobItems(ctx, "absent", importjob.ItemListOptions{Limit: 10}); !errors.Is(err, importjob.ErrNotFound) {
		t.Fatalf("list items error = %v", err)
	}

	if err := store.UpdateImportJobState(ctx, "absent", importjob.JobStateUpdate{State: importjob.StateCompleted}); !errors.Is(err, importjob.ErrNotFound) {
		t.Fatalf("update error = %v", err)
	}

	if err := store.RequestImportJobCancellation(ctx, "absent", time.Now().UTC()); !errors.Is(err, importjob.ErrNotFound) {
		t.Fatalf("cancel error = %v", err)
	}
}

func TestImportJobListingIsNewestFirstAndPages(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)

	for index := range 5 {
		job := importjob.Job{
			ID: string(rune('a'+index)) + "-job", Importer: "SREF recipe", Source: "recipes.json",
			State: importjob.StatePending, DuplicatePolicy: library.DuplicateSkip, TotalItems: 1, BatchSize: 25,
			CreatedAt: time.Date(2026, 9, 1, 12, index, 0, 0, time.UTC),
		}
		if err := store.CreateImportJob(ctx, job, []importjob.Item{{
			JobID: job.ID, Position: 0, Title: "one", State: importjob.ItemPending,
		}}); err != nil {
			t.Fatal(err)
		}
	}

	jobs, total, err := store.ListImportJobs(ctx, importjob.ListOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}

	if total != 5 || len(jobs) != 2 || jobs[0].ID != "e-job" || jobs[1].ID != "d-job" {
		t.Fatalf("first page = %#v (total %d)", jobs, total)
	}

	if jobs[0].Progress.Total != 1 || jobs[0].Progress.Pending != 1 {
		t.Fatalf("listed progress = %#v", jobs[0].Progress)
	}

	page, _, err := store.ListImportJobs(ctx, importjob.ListOptions{Limit: 2, Offset: 4})
	if err != nil {
		t.Fatal(err)
	}

	if len(page) != 1 || page[0].ID != "a-job" {
		t.Fatalf("last page = %#v", page)
	}

	filtered, total, err := store.ListImportJobs(ctx, importjob.ListOptions{Limit: 10, State: importjob.StateCompleted})
	if err != nil {
		t.Fatal(err)
	}

	if total != 0 || len(filtered) != 0 {
		t.Fatalf("completed jobs = %#v", filtered)
	}
}

// An import's report can lead back to exactly the recipes it saved.
func TestLibrarySearchScopesToOneImport(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)

	for _, id := range []library.EntryID{"entry-first", "entry-second", "entry-untouched"} {
		seedRecipe(ctx, t, store, id)
	}

	seedImportJob(ctx, t, store, "job-first", []importjob.Item{pending("first", nil), pending("skipped", nil)})
	seedImportJob(ctx, t, store, "job-second", []importjob.Item{pending("second", nil)})

	now := time.Now().UTC()
	if err := store.SaveImportJobItems(ctx, "job-first", []importjob.Item{
		{Position: 0, State: importjob.ItemImported, Outcome: library.ImportCreated, EntryID: "entry-first", Revision: 1, FinishedAt: &now},
		// A skipped item names an entry it recognized but did not save, and
		// must not be reported as a recipe this import produced.
		{Position: 1, State: importjob.ItemSkipped, Outcome: library.ImportSkipped, EntryID: "entry-untouched", Revision: 1, FinishedAt: &now},
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.SaveImportJobItems(ctx, "job-second", []importjob.Item{
		{Position: 0, State: importjob.ItemImported, Outcome: library.ImportCreated, EntryID: "entry-second", Revision: 1, FinishedAt: &now},
	}); err != nil {
		t.Fatal(err)
	}

	for job, want := range map[string]library.EntryID{"job-first": "entry-first", "job-second": "entry-second"} {
		options := library.ListOptions{
			Limit: 10, State: library.StateActive, Sort: library.ListSortUpdatedDesc,
			Search: library.SearchCriteria{Imports: []string{job}},
		}

		entries, err := store.List(ctx, options)
		if err != nil {
			t.Fatal(err)
		}

		if len(entries) != 1 || entries[0].ID != want {
			t.Fatalf("%s returned %#v, want only %s", job, entries, want)
		}

		if total, err := store.Count(ctx, options); err != nil || total != 1 {
			t.Fatalf("%s count=%d err=%v", job, total, err)
		}
	}
}

// An import is named by its first recipe, not the file it travelled in.
func TestImportJobIsNamedByItsFirstRecipe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)

	job := importjob.Job{
		ID: "named-job", Importer: "Recipe text", Source: "pasted-recipe.txt",
		State: importjob.StatePending, DuplicatePolicy: library.DuplicateSkip, TotalItems: 3, BatchSize: 25,
		CreatedAt: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}
	// Written out of order on purpose: the first recipe is the first by
	// position, which is the order the reader chose and the job works through.
	if err := store.CreateImportJob(ctx, job, []importjob.Item{
		{JobID: job.ID, Position: 2, Title: "Sticky toffee pudding", State: importjob.ItemPending},
		{JobID: job.ID, Position: 0, Title: "Buttermilk pancakes", State: importjob.ItemPending},
		{JobID: job.ID, Position: 1, Title: "Herb rice", State: importjob.ItemPending},
	}); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.GetImportJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.LeadTitle != "Buttermilk pancakes" {
		t.Fatalf("lead title = %q", loaded.LeadTitle)
	}

	listed, _, err := store.ListImportJobs(ctx, importjob.ListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(listed) != 1 || listed[0].LeadTitle != "Buttermilk pancakes" {
		t.Fatalf("listed = %#v", listed)
	}

	// The counts the same pass produces are unchanged by reading titles beside them.
	if listed[0].Progress.Total != 3 || listed[0].Progress.Pending != 3 {
		t.Fatalf("listed progress = %#v", listed[0].Progress)
	}
}

func TestResumingImportClearsCancellationRequest(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)
	seedImportJob(ctx, t, store, "resumed", []importjob.Item{pending("one", nil)})

	if err := store.RequestImportJobCancellation(ctx, "resumed", time.Now()); err != nil {
		t.Fatal(err)
	}

	assertCancellation := func(want bool) {
		t.Helper()

		job, err := store.GetImportJob(ctx, "resumed")
		if err != nil {
			t.Fatal(err)
		}

		if (job.CancellationRequestedAt != nil) != want {
			t.Fatalf("state=%s cancellation timestamp=%v, want present=%t", job.State, job.CancellationRequestedAt, want)
		}
	}
	assertCancellation(true)

	for _, state := range []importjob.State{importjob.StateCancelled, importjob.StatePending, importjob.StateRunning} {
		if err := store.UpdateImportJobState(ctx, "resumed", importjob.JobStateUpdate{State: state}); err != nil {
			t.Fatal(err)
		}

		assertCancellation(state == importjob.StateCancelled)
	}
}
