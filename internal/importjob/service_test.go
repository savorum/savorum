package importjob_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/importjob"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

// The suite runs against the real SQLite repository, since durability is the
// point of a job. Load tests live in scale_test.go, outside race
// instrumentation.
func newStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	return store
}

// harness builds the real import path, so a planned item is exactly what
// production would hand a job and a committed recipe is really in a library.
type harness struct {
	store     *sqlite.Store
	recipes   *library.Service
	exchanger *exchange.Service
}

func newHarness(t *testing.T) harness {
	t.Helper()
	store := newStore(t)

	recipes, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	blobStore, err := files.NewAssetStore(t.TempDir(), files.DefaultMaximumAssetSize)
	if err != nil {
		t.Fatal(err)
	}

	blobs, err := assets.NewService(store, blobStore)
	if err != nil {
		t.Fatal(err)
	}

	return harness{store: store, recipes: recipes, exchanger: exchange.NewService(recipes, blobs)}
}

func (h harness) libraryCount(t *testing.T) int {
	t.Helper()

	count, err := h.recipes.Count(context.Background(), library.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	return count
}

// plan stages a preview of count recipes and hands back what a job would run.
func (h harness) plan(t *testing.T, count int) []exchange.PlannedImport {
	t.Helper()

	uploads := make([]exchange.UploadedImport, count)
	for index := range uploads {
		uploads[index] = exchange.UploadedImport{
			Filename: fmt.Sprintf("recipe-%04d.json", index+1),
			Data:     bareRecipe(t, index+1),
		}
	}

	return h.planUploads(t, uploads)
}

// planPackages stages `.sref` packages, whose photographs exist only as the
// uploaded bytes, making them unresumable.
func (h harness) planPackages(t *testing.T, count int) []exchange.PlannedImport {
	t.Helper()

	uploads := make([]exchange.UploadedImport, count)
	for index := range uploads {
		document, err := sref.Decode(bareRecipe(t, index+1))
		if err != nil {
			t.Fatal(err)
		}

		image := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("p", 32))
		digest := sha256.Sum256(image)
		document.Assets = []recipe.Asset{{
			ID: "photo", Path: "assets/photo.png", MediaType: "image/png",
			SHA256: hex.EncodeToString(digest[:]), Size: int64(len(image)), Role: recipe.AssetRoleImage,
		}}

		packaged, err := sref.WritePackage(document, []sref.PackageAsset{{
			ID: "photo", Path: "assets/photo.png", MediaType: "image/png",
			SHA256: hex.EncodeToString(digest[:]), Bytes: image,
		}})
		if err != nil {
			t.Fatal(err)
		}

		uploads[index] = exchange.UploadedImport{Filename: fmt.Sprintf("recipe-%04d.sref", index+1), Data: packaged}
	}

	return h.planUploads(t, uploads)
}

func (h harness) planUploads(t *testing.T, uploads []exchange.UploadedImport) []exchange.PlannedImport {
	t.Helper()

	preview, err := h.exchanger.PreviewFiles(context.Background(), uploads)
	if err != nil {
		t.Fatal(err)
	}

	selections := make([]exchange.PreviewResolution, 0, len(preview.Items))
	for _, item := range preview.Items {
		if !item.CanCommit {
			t.Fatalf("staged candidate cannot commit: %#v", item)
		}

		selections = append(selections, exchange.PreviewResolution{ItemID: item.ID})
	}

	staged, err := h.exchanger.PlanPreviewImport(context.Background(), preview.ID, selections, false)
	if err != nil {
		t.Fatal(err)
	}

	if len(staged.Items) != len(uploads) {
		t.Fatalf("planned %d items, want %d", len(staged.Items), len(uploads))
	}

	return staged.Items
}

// bareRecipe is one valid SREF document that names no photographs, so an
// ordinary planned item is resumable.
func bareRecipe(t *testing.T, number int) []byte {
	t.Helper()

	document := testfixture.Document()
	document.ID = recipe.RecipeID(fmt.Sprintf("job.recipe.%04d", number))
	document.Title = fmt.Sprintf("Job recipe %d", number)

	document.Assets, document.ImageRefs, document.PrimaryImage = nil, nil, ""
	for sectionIndex := range document.InstructionSections {
		for stepIndex := range document.InstructionSections[sectionIndex].Steps {
			document.InstructionSections[sectionIndex].Steps[stepIndex].ImageRefs = nil
		}
	}

	encoded, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	return encoded
}

// waitFor polls a job until it satisfies the condition. Jobs run in the
// background by design, so a test that wants an outcome has to wait for one.
func waitFor(t *testing.T, service *importjob.Service, id string, want func(importjob.Job) bool) importjob.Job {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)

	for {
		job, err := service.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}

		if want(job) {
			return job
		}

		if time.Now().After(deadline) {
			t.Fatalf("import job %s never reached the expected state: %#v", id, job)
		}

		time.Sleep(5 * time.Millisecond)
	}
}

func finished(job importjob.Job) bool { return job.State.Terminal() }

func TestBulkImportOutlivesTheRequestAndRecordsEveryRecipe(t *testing.T) {
	suite := newHarness(t)

	service, err := importjob.NewService(suite.store, suite.exchanger, importjob.WithLimits(importjob.Limits{BatchSize: 4}))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(service.Close)

	planned := suite.plan(t, 10)
	// The request that starts an import ends here; the rest is the job's.
	started, err := service.Start(context.Background(), importjob.StartRequest{
		Importer: "SREF recipe", Source: "recipes.json", Policy: library.DuplicateSkip, Items: planned,
	})
	if err != nil {
		t.Fatal(err)
	}

	if started.State != importjob.StatePending || started.TotalItems != 10 {
		t.Fatalf("started job = %#v", started)
	}

	job := waitFor(t, service, started.ID, finished)
	if job.State != importjob.StateCompleted {
		t.Fatalf("job state = %q (%s)", job.State, job.Error)
	}

	if job.Progress.Imported != 10 || job.Progress.Failed != 0 || job.Progress.Finished() != 10 {
		t.Fatalf("progress = %#v", job.Progress)
	}

	if job.StartedAt == nil || job.FinishedAt == nil {
		t.Fatalf("job timestamps = %#v", job)
	}

	items, total, err := service.Items(context.Background(), job.ID, importjob.ItemListOptions{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	if total != 10 || len(items) != 10 {
		t.Fatalf("items total=%d len=%d", total, len(items))
	}

	for index, item := range items {
		if item.Position != index || item.State != importjob.ItemImported || item.EntryID == "" || item.Revision == 0 {
			t.Fatalf("item %d = %#v", index, item)
		}
	}
}

func TestPartialSuccessIsPreservedRecipeByRecipe(t *testing.T) {
	suite := newHarness(t)
	planned := suite.plan(t, 6)
	// One recipe in the middle of a batch cannot be imported. The other five
	// were valid before it and are still valid after it.
	failing := &selectiveImporter{inner: suite.exchanger, refuse: map[string]bool{planned[2].Title: true}}

	service, err := importjob.NewService(suite.store, failing, importjob.WithLimits(importjob.Limits{BatchSize: 6}))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(service.Close)

	started, err := service.Start(context.Background(), importjob.StartRequest{
		Importer: "SREF recipe", Source: "recipes.json", Items: planned,
	})
	if err != nil {
		t.Fatal(err)
	}

	job := waitFor(t, service, started.ID, finished)
	if job.State != importjob.StateCompletedWithErrors {
		t.Fatalf("job state = %q (%s)", job.State, job.Error)
	}

	if job.Progress.Imported != 5 || job.Progress.Failed != 1 {
		t.Fatalf("progress = %#v", job.Progress)
	}
	// A whole-batch failure costs the recipe that caused it, not the ones that
	// happened to share an atomic commit with it.
	failed, total, err := service.Items(context.Background(), job.ID, importjob.ItemListOptions{
		Limit: 10, Filter: importjob.ItemsFailed,
	})
	if err != nil {
		t.Fatal(err)
	}

	if total != 1 || len(failed) != 1 || failed[0].Position != 2 || failed[0].Error == "" || !failed[0].ReviewRequired {
		t.Fatalf("failed items = %#v", failed)
	}

	imported, _, err := service.Items(context.Background(), job.ID, importjob.ItemListOptions{
		Limit: 10, Filter: importjob.ItemsImported,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(imported) != 5 {
		t.Fatalf("imported items = %d", len(imported))
	}
}

func TestCancellationStopsFurtherWorkAndKeepsWhatWasCommitted(t *testing.T) {
	suite := newHarness(t)
	planned := suite.plan(t, 20)
	gate := &gatedImporter{inner: suite.exchanger, entered: make(chan struct{}, 8), release: make(chan struct{})}

	service, err := importjob.NewService(suite.store, gate, importjob.WithLimits(importjob.Limits{BatchSize: 5}))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(service.Close)

	started, err := service.Start(context.Background(), importjob.StartRequest{
		Importer: "SREF recipe", Source: "recipes.json", Items: planned,
	})
	if err != nil {
		t.Fatal(err)
	}

	<-gate.entered

	if _, err := service.Cancel(context.Background(), started.ID); err != nil {
		t.Fatal(err)
	}
	// The batch already inside a commit finishes safely; nothing after it is
	// scheduled. Cancellation is not a rollback.
	close(gate.release)

	job := waitFor(t, service, started.ID, finished)
	if job.State != importjob.StateCancelled || job.CancellationRequestedAt == nil {
		t.Fatalf("job = %#v", job)
	}

	if job.Progress.Imported != 5 || job.Progress.Cancelled != 15 {
		t.Fatalf("progress = %#v", job.Progress)
	}

	if _, err := service.Cancel(context.Background(), started.ID); !errors.Is(err, importjob.ErrJobState) {
		t.Fatalf("cancelling a finished job error = %v", err)
	}
	// What was committed before the cancellation is still in the library.
	if entries := suite.libraryCount(t); entries != 5 {
		t.Fatalf("library holds %d recipes after cancellation, want the 5 already committed", entries)
	}
}

func TestRestartRecognizesAnInterruptedJobAndResumesIt(t *testing.T) {
	suite := newHarness(t)
	planned := suite.plan(t, 8)
	gate := &gatedImporter{inner: suite.exchanger, entered: make(chan struct{}, 8), release: make(chan struct{})}

	first, err := importjob.NewService(suite.store, gate, importjob.WithLimits(importjob.Limits{BatchSize: 4}))
	if err != nil {
		t.Fatal(err)
	}

	started, err := first.Start(context.Background(), importjob.StartRequest{
		Importer: "SREF recipe", Source: "recipes.json", Items: planned,
	})
	if err != nil {
		t.Fatal(err)
	}

	<-gate.entered
	// The process stops with a batch in flight.
	close(gate.release)
	first.Close()

	// A second process starts. Nothing may be found running by a process that
	// is not running it.
	next, err := importjob.NewService(suite.store, suite.exchanger, importjob.WithLimits(importjob.Limits{BatchSize: 4}))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(next.Close)

	recovered, err := next.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	job, err := next.Get(context.Background(), started.ID)
	if err != nil {
		t.Fatal(err)
	}

	if job.State != importjob.StateInterrupted {
		t.Fatalf("recovered %d jobs; state = %q", recovered, job.State)
	}

	if job.Progress.Running != 0 {
		t.Fatalf("an item was left running by a process that stopped: %#v", job.Progress)
	}

	if _, err := next.Resume(context.Background(), started.ID); err != nil {
		t.Fatal(err)
	}

	job = waitFor(t, next, started.ID, finished)
	if job.State != importjob.StateCompleted || job.Progress.Imported+job.Progress.Skipped != 8 {
		t.Fatalf("resumed job = %#v", job)
	}
	// A resume rewrites the same rows rather than appending a second history.
	if job.Progress.Total != 8 {
		t.Fatalf("resume changed the item count: %#v", job.Progress)
	}

	if entries := suite.libraryCount(t); entries != 8 {
		t.Fatalf("library holds %d recipes, want 8 with no duplicates from the resume", entries)
	}
}

func TestAnItemWithoutItsUploadedFileIsReportedRatherThanRetried(t *testing.T) {
	suite := newHarness(t)
	// A package's photographs live only in the uploaded bytes, so nothing
	// durable can be written down for one.
	unresumable := suite.planPackages(t, 2)
	gate := &gatedImporter{inner: suite.exchanger, entered: make(chan struct{}, 4), release: make(chan struct{})}

	first, err := importjob.NewService(suite.store, gate, importjob.WithLimits(importjob.Limits{BatchSize: 1}))
	if err != nil {
		t.Fatal(err)
	}

	started, err := first.Start(context.Background(), importjob.StartRequest{
		Importer: "CookbookManager", Source: "export.zip", Items: unresumable,
	})
	if err != nil {
		t.Fatal(err)
	}

	<-gate.entered
	first.Close()

	next, err := importjob.NewService(suite.store, suite.exchanger)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(next.Close)

	if _, err := next.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := next.Resume(context.Background(), started.ID); err != nil && !errors.Is(err, importjob.ErrNotResumable) {
		t.Fatal(err)
	}

	job := waitFor(t, next, started.ID, finished)

	items, _, err := next.Items(context.Background(), job.ID, importjob.ItemListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	lost := items[0]
	// The sentence has to name the file, because "choose the file again" is an
	// instruction only if the reader can tell which file is meant.
	if lost.State != importjob.ItemFailed || !strings.Contains(lost.Error, lost.Filename) ||
		!strings.Contains(lost.Error, "Choose it again") {
		t.Fatalf("unresumable item = %#v", lost)
	}
}

func TestResumeDoesNotRepeatARecipeTheLibraryAlreadyHolds(t *testing.T) {
	suite := newHarness(t)
	planned := suite.plan(t, 3)

	service, err := importjob.NewService(suite.store, suite.exchanger, importjob.WithLimits(importjob.Limits{BatchSize: 3}))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(service.Close)

	started, err := service.Start(context.Background(), importjob.StartRequest{
		Importer: "SREF recipe", Source: "recipes.json", Items: planned,
	})
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, service, started.ID, finished)
	// Ask for the finished recipes again the way a resume of an uncertain
	// interruption would. Meeting the same portable recipe twice is reported,
	// never repeated.
	repeat, err := importjob.NewService(suite.store, suite.exchanger)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(repeat.Close)

	if _, err := repeat.Start(context.Background(), importjob.StartRequest{
		Importer: "SREF recipe", Source: "recipes.json", Items: planned,
	}); err != nil {
		t.Fatal(err)
	}

	if entries := suite.libraryCount(t); entries != 3 {
		t.Fatalf("library holds %d recipes, want 3", entries)
	}
}

func TestUnknownJobIsNotFound(t *testing.T) {
	suite := newHarness(t)

	service, err := importjob.NewService(suite.store, suite.exchanger)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(service.Close)

	if _, err := service.Get(context.Background(), "missing"); !errors.Is(err, importjob.ErrNotFound) {
		t.Fatalf("get error = %v", err)
	}

	if _, _, err := service.Items(context.Background(), "missing", importjob.ItemListOptions{Limit: 10}); !errors.Is(err, importjob.ErrNotFound) {
		t.Fatalf("items error = %v", err)
	}

	if _, err := service.Cancel(context.Background(), "missing"); !errors.Is(err, importjob.ErrNotFound) {
		t.Fatalf("cancel error = %v", err)
	}
}

// selectiveImporter refuses one recipe by title, the way an invalid document
// fails an atomic commit that other recipes were sharing.
type selectiveImporter struct {
	inner  *exchange.Service
	refuse map[string]bool
}

func (importerStub *selectiveImporter) ImportPlanned(ctx, staging context.Context, planned []exchange.PlannedImport, policy library.DuplicatePolicy) ([]library.ImportResult, error) {
	for _, entry := range planned {
		if importerStub.refuse[entry.Title] {
			return nil, fmt.Errorf("recipe %q cannot be imported", entry.Title)
		}
	}

	return importerStub.inner.ImportPlanned(ctx, staging, planned, policy)
}

// gatedImporter lets a test hold a batch inside its commit, which is where
// cancellation and an interrupted process both have to behave.
type gatedImporter struct {
	inner   *exchange.Service
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (gate *gatedImporter) ImportPlanned(ctx, staging context.Context, planned []exchange.PlannedImport, policy library.DuplicatePolicy) ([]library.ImportResult, error) {
	gate.once.Do(func() {
		select {
		case gate.entered <- struct{}{}:
		case <-ctx.Done():
		}

		select {
		case <-gate.release:
		case <-ctx.Done():
		}
	})
	// A stopping process ends the batch here, which is what leaves the job's
	// rows to be recovered rather than decided.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return gate.inner.ImportPlanned(ctx, staging, planned, policy)
}

// Continuing an import is offered from what Savorum still holds, not from how
// many recipes failed.
func TestProgressCountsWhatCanActuallyBeRunAgain(t *testing.T) {
	suite := newHarness(t)
	// Two packages: their photographs exist only in the uploaded bytes, so
	// nothing durable was written down for either.
	unresumable := suite.planPackages(t, 2)
	gate := &gatedImporter{inner: suite.exchanger, entered: make(chan struct{}, 4), release: make(chan struct{})}

	first, err := importjob.NewService(suite.store, gate, importjob.WithLimits(importjob.Limits{BatchSize: 1}))
	if err != nil {
		t.Fatal(err)
	}

	started, err := first.Start(context.Background(), importjob.StartRequest{
		Importer: "SREF", Source: "recipes.sref", Items: unresumable,
	})
	if err != nil {
		t.Fatal(err)
	}

	<-gate.entered
	first.Close()

	next, err := importjob.NewService(suite.store, suite.exchanger)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(next.Close)

	if _, err := next.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}

	job, err := next.Get(context.Background(), started.ID)
	if err != nil {
		t.Fatal(err)
	}

	if job.Progress.Pending+job.Progress.Cancelled+job.Progress.Failed == 0 {
		t.Fatalf("nothing was left unfinished: %#v", job.Progress)
	}

	if job.Progress.Resumable != 0 {
		t.Fatalf("resumable = %d, want none: no unfinished recipe kept a payload", job.Progress.Resumable)
	}
	// And a job whose unfinished recipes did keep theirs reports them.
	resumable := suite.plan(t, 2)

	kept, err := next.Start(context.Background(), importjob.StartRequest{
		Importer: "SREF", Source: "recipes.sref", Items: resumable,
	})
	if err != nil {
		t.Fatal(err)
	}

	finished := waitFor(t, next, kept.ID, finished)
	if finished.Progress.Resumable != 0 {
		t.Fatalf("a finished import reported %d recipes to run again", finished.Progress.Resumable)
	}
}

func TestJobAdmissionBoundsCountAndBytesBeforePersistence(t *testing.T) {
	for _, kind := range []string{"count", "bytes", "actor-count", "actor-bytes"} {
		t.Run(kind, func(t *testing.T) {
			suite := newHarness(t)
			planned := suite.plan(t, 1)
			charge := exchange.PlannedRetainedBytes(planned)
			limits := importjob.Limits{ConcurrentJobs: 1, AcceptedJobs: 4, ActorJobs: 4, RetainedBytes: charge * 4, ActorBytes: charge * 4}

			switch kind {
			case "count":
				limits.AcceptedJobs = 1
			case "bytes":
				limits.RetainedBytes = charge
			case "actor-count":
				limits.ActorJobs = 1
			case "actor-bytes":
				limits.ActorBytes = charge
			}

			gate := &gatedImporter{inner: suite.exchanger, entered: make(chan struct{}, 1), release: make(chan struct{})}

			service, err := importjob.NewService(suite.store, gate, importjob.WithLimits(limits))
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(service.Close)

			first, err := service.Start(t.Context(), importjob.StartRequest{Items: planned})
			if err != nil {
				t.Fatal(err)
			}

			<-gate.entered

			if _, err := service.Start(t.Context(), importjob.StartRequest{Items: planned}); !errors.Is(err, importjob.ErrCapacity) {
				t.Fatalf("admission=%v", err)
			}

			_, total, err := service.List(t.Context(), importjob.ListOptions{})
			if err != nil || total != 1 {
				t.Fatalf("rejected job persisted: total=%d err=%v", total, err)
			}

			close(gate.release)
			waitFor(t, service, first.ID, finished)
		})
	}
}

type failingJobCreation struct {
	importjob.Repository
	fail bool
}

func (r *failingJobCreation) CreateImportJob(ctx context.Context, job importjob.Job, items []importjob.Item) error {
	if r.fail {
		r.fail = false
		return errors.New("temporary storage failure")
	}

	return r.Repository.CreateImportJob(ctx, job, items)
}

func TestFailedPersistenceReleasesJobAdmission(t *testing.T) {
	suite := newHarness(t)
	planned := suite.plan(t, 1)
	repository := &failingJobCreation{Repository: suite.store, fail: true}

	service, err := importjob.NewService(repository, suite.exchanger, importjob.WithLimits(importjob.Limits{AcceptedJobs: 1}))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(service.Close)

	if _, err := service.Start(t.Context(), importjob.StartRequest{Items: planned}); err == nil {
		t.Fatal("storage failure was lost")
	}

	job, err := service.Start(t.Context(), importjob.StartRequest{Items: planned})
	if err != nil {
		t.Fatalf("failed write retained admission: %v", err)
	}

	waitFor(t, service, job.ID, finished)
}

func TestOversizedJobIsRejectedBeforePersistence(t *testing.T) {
	suite := newHarness(t)
	planned := suite.plan(t, 1)

	service, err := importjob.NewService(suite.store, suite.exchanger, importjob.WithLimits(importjob.Limits{RetainedBytes: exchange.PlannedRetainedBytes(planned) - 1}))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(service.Close)

	if _, err := service.Start(t.Context(), importjob.StartRequest{Items: planned}); !errors.Is(err, importjob.ErrTooLarge) {
		t.Fatalf("oversized selection=%v", err)
	}

	_, total, err := service.List(t.Context(), importjob.ListOptions{})
	if err != nil || total != 0 {
		t.Fatalf("oversized job persisted: total=%d error=%v", total, err)
	}
}
