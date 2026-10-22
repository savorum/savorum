package importjob

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
)

// Repository is the durable half of a job.
type Repository interface {
	CreateImportJob(ctx context.Context, job Job, items []Item) error
	GetImportJob(ctx context.Context, id string) (Job, error)
	ListImportJobs(ctx context.Context, options ListOptions) ([]Job, int, error)
	ListImportJobItems(ctx context.Context, jobID string, options ItemListOptions) ([]Item, int, error)
	UpdateImportJobState(ctx context.Context, id string, update JobStateUpdate) error
	RequestImportJobCancellation(ctx context.Context, id string, at time.Time) error
	SaveImportJobItems(ctx context.Context, jobID string, items []Item) error
	// InterruptImportJobs is startup recovery. It reports how many jobs were
	// left mid-flight by a process that stopped.
	InterruptImportJobs(ctx context.Context, at time.Time) (int, error)
	// LoadImportJobItems returns items in the named states with their stored
	// payloads, the only read that does.
	LoadImportJobItems(ctx context.Context, jobID string, states []ItemState) ([]Item, error)
}

// Importer commits one batch of planned recipes. It is exchange.Service in
// production; the interface exists so the job lifecycle can be tested without
// a library, an asset store, and the public network.
type Importer interface {
	ImportPlanned(ctx, staging context.Context, planned []exchange.PlannedImport, policy library.DuplicatePolicy) ([]library.ImportResult, error)
}

// Limits bound the work this process will do at once.
//
// ConcurrentJobs is the worker pool. Within one job the batches are
// sequential, because each is an atomic commit whose duplicate decisions must
// not race another batch from the same selection.
//
// BatchSize is how many recipes share a commit, and so arrive in the library
// together.
type Limits struct {
	ConcurrentJobs int
	BatchSize      int
	// AcceptedJobs and RetainedBytes include running and waiting jobs.
	AcceptedJobs  int
	RetainedBytes int64
	ActorJobs     int
	ActorBytes    int64
}

// DefaultLimits bounds concurrent and retained import work.
func DefaultLimits() Limits {
	return Limits{ConcurrentJobs: 2, BatchSize: 25, AcceptedJobs: 8, RetainedBytes: 256 << 20, ActorJobs: 4, ActorBytes: 128 << 20}
}

func (limits Limits) normalized() Limits {
	defaults := DefaultLimits()
	if limits.ConcurrentJobs <= 0 {
		limits.ConcurrentJobs = defaults.ConcurrentJobs
	}

	if limits.BatchSize <= 0 {
		limits.BatchSize = defaults.BatchSize
	}

	if limits.AcceptedJobs <= 0 {
		limits.AcceptedJobs = defaults.AcceptedJobs
	}

	if limits.RetainedBytes <= 0 {
		limits.RetainedBytes = defaults.RetainedBytes
	}

	if limits.ActorJobs <= 0 {
		limits.ActorJobs = defaults.ActorJobs
	}

	if limits.ActorBytes <= 0 {
		limits.ActorBytes = defaults.ActorBytes
	}

	return limits
}

// Option configures an import-job service.
type Option func(*Service)

// WithLimits configures job concurrency and batch size.
func WithLimits(limits Limits) Option {
	return func(service *Service) { service.limits = limits }
}

// WithClock configures the clock used for persisted lifecycle timestamps.
func WithClock(now func() time.Time) Option {
	return func(service *Service) { service.now = now }
}

// WithLogger configures the logger used by background jobs.
func WithLogger(logger *slog.Logger) Option {
	return func(service *Service) { service.logger = logger }
}

// Service persists and executes asynchronous recipe-import jobs.
type Service struct {
	repository Repository
	importer   Importer
	limits     Limits
	slots      chan struct{}
	now        func() time.Time
	logger     *slog.Logger

	mu            sync.Mutex
	active        map[string]*activeRun
	retainedBytes int64
	closed        bool
	running       sync.WaitGroup

	// lifetime is cancelled when the service closes. It bounds the commit
	// contexts of background runs, which have no request to inherit from.
	//nolint:containedctx // this is the explicit service lifetime, not a retained request context
	lifetime context.Context
	stop     context.CancelFunc
}

// activeRun is the process-local handle on a job that is running now.
// Cancellation is cooperative: the flag stops the next batch from being
// scheduled, and cancelStaging stops the current batch reaching out to the
// network without touching the commit it is inside.
type activeRun struct {
	bytes         int64
	actor         string
	cancelled     atomic.Bool
	mu            sync.Mutex
	cancelStaging context.CancelFunc
}

func (run *activeRun) requestCancellation() {
	run.cancelled.Store(true)
	run.mu.Lock()
	defer run.mu.Unlock()

	if run.cancelStaging != nil {
		run.cancelStaging()
	}
}

func (run *activeRun) setStagingCancel(cancel context.CancelFunc) bool {
	run.mu.Lock()
	defer run.mu.Unlock()

	if run.cancelled.Load() {
		return false
	}

	run.cancelStaging = cancel

	return true
}

// NewService creates an import-job service backed by repository and importer.
func NewService(repository Repository, importer Importer, options ...Option) (*Service, error) {
	if repository == nil {
		return nil, errors.New("import job repository is required")
	}

	if importer == nil {
		return nil, errors.New("import job importer is required")
	}

	service := &Service{
		repository: repository, importer: importer, limits: DefaultLimits(),
		now: func() time.Time { return time.Now().UTC() }, logger: slog.Default(),
		active: map[string]*activeRun{},
	}
	for _, option := range options {
		option(service)
	}

	if service.now == nil {
		service.now = func() time.Time { return time.Now().UTC() }
	}

	if service.logger == nil {
		service.logger = slog.Default()
	}

	service.limits = service.limits.normalized()
	service.slots = make(chan struct{}, service.limits.ConcurrentJobs)
	service.lifetime, service.stop = context.WithCancel(context.Background())

	return service, nil
}

// StartRequest is a reviewed selection handed over as durable work.
type StartRequest struct {
	Importer string
	Source   string
	Policy   library.DuplicatePolicy
	Items    []exchange.PlannedImport
}

// Start records the job and its items, then runs it. The returned job is the
// record as written, so the caller can answer the browser with a location to
// watch rather than a result to wait for.
func (s *Service) Start(ctx context.Context, request StartRequest) (Job, error) {
	if len(request.Items) == 0 {
		return Job{}, fmt.Errorf("%w: an import job carries at least one recipe", ErrJobState)
	}

	if len(request.Items) > library.MaximumImportRecipes {
		return Job{}, fmt.Errorf("%w: an import job carries at most %d recipes", ErrJobState, library.MaximumImportRecipes)
	}

	policy := request.Policy
	if policy == "" {
		policy = library.DuplicateSkip
	}
	// A reconciled selection has already answered the duplicate question per
	// recipe, so the job's policy is what a restored item falls back to rather
	// than what the first attempt uses.
	if request.Items[0].Resolution != nil {
		policy = library.DuplicateSkip
	}

	id, err := randomJobID()
	if err != nil {
		return Job{}, err
	}

	now := s.now()

	actor, _ := identity.FromContext(ctx)
	if actor.UserID == "" {
		if resolver, ok := s.repository.(interface {
			LocalPrincipal(context.Context) (identity.Principal, error)
		}); ok {
			actor, err = resolver.LocalPrincipal(ctx)
			if err != nil {
				return Job{}, fmt.Errorf("resolve import actor: %w", err)
			}
		}
	}

	libraryID := access.LibraryID(ctx)
	if libraryID == "" {
		libraryID = "default"
	}

	job := Job{
		Actor: actor, LibraryID: libraryID,
		ID: id, Importer: request.Importer, Source: request.Source, State: StatePending,
		DuplicatePolicy: policy, TotalItems: len(request.Items), BatchSize: s.limits.BatchSize, CreatedAt: now,
	}
	seenDecisions := map[string]bool{}

	for _, planned := range request.Items {
		for _, decision := range planned.MeasurementResolutionRecords() {
			if !seenDecisions[decision.ImpactDigest] {
				job.ResolutionDecisions = append(job.ResolutionDecisions, decision)
				seenDecisions[decision.ImpactDigest] = true
			}
		}
	}

	run, err := s.reserve(job, request.Items)
	if err != nil {
		return Job{}, err
	}

	submitted := false
	defer func() {
		if !submitted {
			s.release(job.ID, run)
		}
	}()

	items := make([]Item, 0, len(request.Items))
	for position, planned := range request.Items {
		items = append(items, Item{
			JobID: id, Position: position, SourceItemID: planned.ItemID,
			Title: planned.Title, Filename: planned.Source.Filename, SourceURL: planned.Source.URL,
			State: ItemPending, ReviewRequired: planned.ReviewRequired,
			Diagnostics: planned.Diagnostics(), Payload: planned.Payload(),
		})
	}

	if err := s.repository.CreateImportJob(ctx, job, items); err != nil {
		return Job{}, err
	}

	positions := make([]int, len(request.Items))
	for i := range positions {
		positions[i] = i
	}

	submitted = true

	go s.execute(job, request.Items, positions, run)

	resumable := 0

	for _, item := range items {
		if item.Resumable() {
			resumable++
		}
	}

	job.Progress = Progress{Total: len(items), Pending: len(items), Resumable: resumable}

	return job, nil
}

func (s *Service) reserve(job Job, planned []exchange.PlannedImport) (*activeRun, error) {
	charged := exchange.PlannedRetainedBytes(planned)
	if charged > min(s.limits.RetainedBytes, s.limits.ActorBytes) {
		return nil, ErrTooLarge
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, fmt.Errorf("%w: Savorum is shutting down", ErrJobState)
	}

	if _, exists := s.active[job.ID]; exists {
		return nil, fmt.Errorf("%w: this import is already running", ErrJobState)
	}

	actorJobs, actorBytes := 0, int64(0)

	for _, run := range s.active {
		if run.actor == job.Actor.UserID {
			actorJobs++
			actorBytes += run.bytes
		}
	}

	if len(s.active) >= s.limits.AcceptedJobs || charged > s.limits.RetainedBytes-s.retainedBytes || actorJobs >= s.limits.ActorJobs || charged > s.limits.ActorBytes-actorBytes {
		return nil, ErrCapacity
	}

	run := &activeRun{bytes: charged, actor: job.Actor.UserID}
	s.active[job.ID] = run
	s.retainedBytes += charged
	s.running.Add(1)

	return run, nil
}

func (s *Service) charge(run *activeRun, charged int64) error {
	if charged > min(s.limits.RetainedBytes, s.limits.ActorBytes) {
		return ErrTooLarge
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var actorBytes int64

	for _, current := range s.active {
		if current.actor == run.actor {
			actorBytes += current.bytes
		}
	}

	if charged > s.limits.RetainedBytes-s.retainedBytes || charged > s.limits.ActorBytes-actorBytes {
		return ErrCapacity
	}

	run.bytes += charged
	s.retainedBytes += charged

	return nil
}

func (s *Service) release(id string, run *activeRun) {
	s.mu.Lock()
	delete(s.active, id)
	s.retainedBytes -= run.bytes
	s.mu.Unlock()
	s.running.Done()
}

// execute is the whole life of one run, running planned[i] as the job's item
// at positions[i]. A resume runs a subset and keeps each recipe's original
// row. It returns no error: a job's outcome is recorded, not returned.
func (s *Service) execute(job Job, planned []exchange.PlannedImport, positions []int, run *activeRun) {
	defer s.release(job.ID, run)

	select {
	case s.slots <- struct{}{}:
	case <-s.lifetime.Done():
		s.finish(job, StateInterrupted, "Savorum stopped before this import started.")
		return
	}

	defer func() { <-s.slots }()

	if run.cancelled.Load() {
		_ = s.markRange(job, planned, positions, ItemCancelled, "This import was cancelled before it started.")
		s.finish(job, StateCancelled, "")

		return
	}

	started := s.now()
	startCtx, cancelStart := s.durableContext(job)
	err := s.repository.UpdateImportJobState(startCtx, job.ID, JobStateUpdate{State: StateRunning, StartedAt: &started})

	cancelStart()

	if err != nil {
		s.logger.Error("record import job start", "job", job.ID, "error", err)
		s.finish(job, StateFailed, err.Error())

		return
	}

	failures := 0

	var runError string

	stopped := false

	position := 0
	for position < len(planned) {
		if run.cancelled.Load() || s.lifetime.Err() != nil {
			break
		}

		size := min(s.limits.BatchSize, len(planned)-position)
		batch := planned[position : position+size]
		batchFailures, err := s.runBatch(job, batch, positions[position:position+size], run)
		failures += batchFailures

		if err != nil {
			if errors.Is(err, errStopping) {
				stopped = true
			} else {
				runError = err.Error()
			}

			break
		}

		position += size
	}

	// A process that stopped is checked first and records nothing about the
	// recipes it did not reach. Their rows stay as they are, and recovery reads
	// them as work to pick up rather than as decisions anybody made.
	switch {
	case stopped || s.lifetime.Err() != nil:
		s.finish(job, StateInterrupted, "Savorum stopped while this import was running.")
	case runError != "":
		_ = s.markRange(job, planned[position:], positions[position:], ItemFailed, runError)
		s.finish(job, StateFailed, runError)
	case run.cancelled.Load():
		_ = s.markRange(job, planned[position:], positions[position:], ItemCancelled,
			"This import was cancelled before this recipe was reached.")
		s.finish(job, StateCancelled, "")
	case failures > 0:
		s.finish(job, StateCompletedWithErrors, "")
	default:
		s.finish(job, StateCompleted, "")
	}
}

// errStopping separates a process that stopped from work that failed. Its text
// is shown to the reader beside the other import item errors.
//
//nolint:staticcheck // Proper noun.
var errStopping = errors.New("Savorum stopped while this import was running")

// runBatch commits one batch and records what became of every recipe in it. A
// whole-batch failure is retried one recipe at a time so one unusable recipe
// does not fail its neighbours. The returned error is a failure to record
// progress, the only kind a run cannot continue past.
func (s *Service) runBatch(job Job, batch []exchange.PlannedImport, positions []int, run *activeRun) (int, error) {
	if err := s.markRunning(job, batch, positions); err != nil {
		return 0, err
	}

	results, err := s.commit(job, batch, run)
	if err == nil {
		return 0, s.recordResults(job, batch, positions, results)
	}

	if s.lifetime.Err() != nil {
		return 0, errStopping
	}

	if len(batch) == 1 {
		return 1, s.recordItemFailure(job, batch[0], positions[0], err)
	}

	failures := 0

	for index, planned := range batch {
		if s.lifetime.Err() != nil {
			return failures, errStopping
		}

		if run.cancelled.Load() {
			return failures, s.markRange(job, batch[index:], positions[index:], ItemCancelled,
				"This import was cancelled before this recipe was reached.")
		}

		single := batch[index : index+1]

		results, singleErr := s.commit(job, single, run)
		if singleErr != nil {
			if s.lifetime.Err() != nil {
				return failures, errStopping
			}

			failures++
			if recordErr := s.recordItemFailure(job, planned, positions[index], singleErr); recordErr != nil {
				return failures, recordErr
			}

			continue
		}

		if recordErr := s.recordResults(job, single, positions[index:index+1], results); recordErr != nil {
			return failures, recordErr
		}
	}

	return failures, nil
}

func (s *Service) commit(job Job, batch []exchange.PlannedImport, run *activeRun) ([]library.ImportResult, error) {
	staging, cancel := context.WithCancel(s.actorContext(job))
	defer cancel()

	if !run.setStagingCancel(cancel) {
		cancel()
	}

	defer run.setStagingCancel(nil)

	results, err := s.importer.ImportPlanned(library.WithImportJob(s.actorContext(job), job.ID), staging, batch, job.DuplicatePolicy)
	if err != nil {
		return nil, err
	}

	if len(results) != len(batch) {
		return nil, fmt.Errorf("import returned %d results for %d recipes", len(results), len(batch))
	}

	return results, nil
}

func (s *Service) markRunning(job Job, batch []exchange.PlannedImport, positions []int) error {
	now := s.now()

	items := make([]Item, 0, len(batch))
	for index, planned := range batch {
		items = append(items, Item{
			JobID: job.ID, Position: positions[index], Title: planned.Title, State: ItemRunning,
			ReviewRequired: planned.ReviewRequired, Diagnostics: planned.Diagnostics(), StartedAt: &now,
		})
	}

	return s.save(job, items)
}

func (s *Service) recordResults(job Job, batch []exchange.PlannedImport, positions []int, results []library.ImportResult) error {
	now := s.now()

	items := make([]Item, 0, len(batch))
	for index, result := range results {
		planned := batch[index]

		state := ItemImported
		if result.Outcome == library.ImportSkipped {
			state = ItemSkipped
		}

		title := result.Title
		if title == "" {
			title = planned.Title
		}

		items = append(items, Item{
			JobID: job.ID, Position: positions[index], State: state,
			// A dropped photograph needs review; one merely not fetched yet
			// does not.
			ReviewRequired: planned.ReviewRequired || result.DroppedAssets > 0,
			Outcome:        result.Outcome, EntryID: result.EntryID, RecipeID: string(result.RecipeID),
			ImportEventID: result.ImportEventID, Revision: result.Revision, DroppedAssets: result.DroppedAssets, DeferredAssets: result.DeferredAssets,
			Diagnostics: resultDiagnostics(planned.Diagnostics(), result), Title: title, FinishedAt: &now,
		})
	}

	return s.save(job, items)
}

func resultDiagnostics(planned []importer.Diagnostic, result library.ImportResult) []importer.Diagnostic {
	diagnostics := append([]importer.Diagnostic(nil), planned...)

	plannedMessages := make(map[string]struct{}, len(planned))
	for _, diagnostic := range planned {
		plannedMessages[diagnostic.Message] = struct{}{}
	}

	for _, warning := range result.Warnings {
		// Keep genuinely new preservation warnings without duplicating issues
		// that already crossed the commit boundary.
		if _, exists := plannedMessages[warning]; exists {
			continue
		}

		diagnostics = append(diagnostics, importer.Diagnostic{
			Code: "import-preservation", Class: importer.DiagnosticActionable,
			Severity: importer.SeverityWarning, Area: "content", Message: warning, Count: 1,
		})
	}

	if result.DroppedAssets > 0 {
		diagnostics = append(diagnostics, importer.Diagnostic{
			Code: "photograph-unavailable", Class: importer.DiagnosticActionable,
			Severity: importer.SeverityWarning, Area: "assets", Count: result.DroppedAssets,
			Message: fmt.Sprintf("%d %s unavailable.", result.DroppedAssets, pluralWord(result.DroppedAssets, "photograph", "photographs")),
		})
	}

	if result.DeferredAssets > 0 {
		diagnostics = append(diagnostics, importer.Diagnostic{
			Code: "photograph-deferred", Class: importer.DiagnosticInformational,
			Severity: importer.SeverityInfo, Area: "assets", Count: result.DeferredAssets,
			Message: fmt.Sprintf("%d %s will be fetched later.", result.DeferredAssets, pluralWord(result.DeferredAssets, "photograph", "photographs")),
		})
	}

	return importer.AggregateDiagnostics(diagnostics)
}

func pluralWord(count int, one, many string) string {
	if count == 1 {
		return one
	}

	return many
}

func (s *Service) recordItemFailure(job Job, planned exchange.PlannedImport, position int, cause error) error {
	now := s.now()

	return s.save(job, []Item{{
		JobID: job.ID, Position: position, Title: planned.Title, State: ItemFailed, ReviewRequired: true,
		Diagnostics: planned.Diagnostics(), Error: cause.Error(), FinishedAt: &now,
	}})
}

func (s *Service) markRange(job Job, remaining []exchange.PlannedImport, positions []int, state ItemState, reason string) error {
	if len(remaining) == 0 {
		return nil
	}

	now := s.now()

	items := make([]Item, 0, len(remaining))
	for index, planned := range remaining {
		items = append(items, Item{
			JobID: job.ID, Position: positions[index], Title: planned.Title, State: state,
			ReviewRequired: state == ItemFailed, Error: reason,
			Diagnostics: planned.Diagnostics(), FinishedAt: &now,
		})
	}

	return s.save(job, items)
}

// durableContext is for writes a stopping process must still complete. A job
// that stopped is exactly the one whose rows have to be accurate, so recording
// what happened to it cannot itself be abandoned by the shutdown.
func (s *Service) durableContext(job Job) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithValue(context.WithoutCancel(s.actorContext(job)), workerKey{}, job.ID), 15*time.Second)
}

func (s *Service) save(job Job, items []Item) error {
	jobID := job.ID

	ctx, cancel := s.durableContext(job)
	defer cancel()

	if err := s.repository.SaveImportJobItems(ctx, jobID, items); err != nil {
		s.logger.Error("record import job progress", "job", jobID, "error", err)
		return err
	}

	return nil
}

func (s *Service) finish(job Job, state State, cause string) {
	finished := s.now()

	ctx, cancel := s.durableContext(job)
	defer cancel()

	if err := s.repository.UpdateImportJobState(ctx, job.ID, JobStateUpdate{
		State: state, Error: cause, FinishedAt: &finished,
	}); err != nil {
		s.logger.Error("record import job outcome", "job", job.ID, "state", state, "error", err)
	}
}

// Get returns one import job by identity.
func (s *Service) Get(ctx context.Context, id string) (Job, error) {
	return s.repository.GetImportJob(ctx, id)
}

// List returns a page of import jobs and the total matching count.
func (s *Service) List(ctx context.Context, options ListOptions) ([]Job, int, error) {
	if options.Limit <= 0 {
		options.Limit = 25
	}

	if options.State != "" && !options.State.valid() {
		return nil, 0, fmt.Errorf("%w: %q is not an import job state", ErrJobState, options.State)
	}

	return s.repository.ListImportJobs(ctx, options)
}

// Items returns a page of items belonging to an import job.
func (s *Service) Items(ctx context.Context, jobID string, options ItemListOptions) ([]Item, int, error) {
	if options.Limit <= 0 {
		options.Limit = 50
	}

	if !options.Filter.valid() {
		return nil, 0, fmt.Errorf("%w: %q is not an import item filter", ErrJobState, options.Filter)
	}

	return s.repository.ListImportJobItems(ctx, jobID, options)
}

// Cancel stops a job from starting anything further. It is not a rollback:
// recipes already committed stay in the library, because undoing them would
// throw away the part of the import that worked.
func (s *Service) Cancel(ctx context.Context, id string) (Job, error) {
	job, err := s.repository.GetImportJob(ctx, id)
	if err != nil {
		return Job{}, err
	}

	if job.State.Terminal() {
		return Job{}, fmt.Errorf("%w: this import has already finished", ErrJobState)
	}

	if err := s.repository.RequestImportJobCancellation(ctx, id, s.now()); err != nil {
		return Job{}, err
	}

	s.mu.Lock()
	run := s.active[id]
	s.mu.Unlock()

	if run != nil {
		run.requestCancellation()
	} else {
		// A job with no run in this process was left behind by a stopped one.
		// Cancelling it is a bookkeeping change, so it happens here.
		if err := s.cancelUnattended(ctx, job); err != nil {
			return Job{}, err
		}
	}

	return s.repository.GetImportJob(ctx, id)
}

// cancelUnattended stops a job no run in this process owns. Recovery makes that
// rare, so this is mostly a guard against the moment between a run recording its
// outcome and removing itself: re-reading the job is what stops a cancellation
// from overwriting an import that had just finished.
func (s *Service) cancelUnattended(ctx context.Context, job Job) error {
	current, err := s.repository.GetImportJob(ctx, job.ID)
	if err != nil {
		return err
	}

	if current.State.Terminal() {
		return nil
	}

	items, err := s.repository.LoadImportJobItems(ctx, job.ID, []ItemState{ItemPending, ItemRunning})
	if err != nil {
		return err
	}

	stopped := make([]Item, 0, len(items))

	now := s.now()
	for _, item := range items {
		stopped = append(stopped, Item{
			JobID: job.ID, Position: item.Position, State: ItemCancelled,
			Diagnostics: item.Diagnostics, Error: "This import was cancelled.", FinishedAt: &now,
		})
	}

	if len(stopped) > 0 {
		if err := s.repository.SaveImportJobItems(ctx, job.ID, stopped); err != nil {
			return err
		}
	}

	return s.repository.UpdateImportJobState(ctx, job.ID, JobStateUpdate{State: StateCancelled, FinishedAt: &now})
}

// Resume runs a stopped job's unfinished recipes again from what was written
// down for them. An item whose bytes only ever existed in the uploaded file is
// reported as needing that file again rather than quietly retried against
// nothing.
func (s *Service) Resume(ctx context.Context, id string) (Job, error) {
	job, err := s.repository.GetImportJob(ctx, id)
	if err != nil {
		return Job{}, err
	}

	if !job.State.Terminal() {
		return Job{}, fmt.Errorf("%w: this import is still running", ErrJobState)
	}

	run, err := s.reserve(job, nil)
	if err != nil {
		return Job{}, err
	}

	submitted := false
	defer func() {
		if !submitted {
			s.release(job.ID, run)
		}
	}()

	items, err := s.repository.LoadImportJobItems(ctx, id, []ItemState{
		ItemPending, ItemRunning, ItemCancelled, ItemFailed,
	})
	if err != nil {
		return Job{}, err
	}

	if len(items) == 0 {
		return Job{}, fmt.Errorf("%w: every recipe in this import has finished", ErrJobState)
	}

	now := s.now()
	planned := make([]exchange.PlannedImport, 0, len(items))
	positions := make([]int, 0, len(items))

	var lost []Item

	for _, item := range items {
		if !item.Resumable() {
			lost = append(lost, Item{
				JobID: id, Position: item.Position, State: ItemFailed, ReviewRequired: true,
				Diagnostics: item.Diagnostics, FinishedAt: &now,
				Error: unresumableReason(item),
			})

			continue
		}

		restored, err := exchange.RestorePlanned(item.Payload)
		if err != nil {
			lost = append(lost, Item{
				JobID: id, Position: item.Position, State: ItemFailed, ReviewRequired: true,
				Diagnostics: item.Diagnostics, FinishedAt: &now,
				Error: "The stored copy of this recipe could not be read: " + err.Error(),
			})

			continue
		}

		restored.ReviewRequired = item.ReviewRequired
		planned = append(planned, restored)
		positions = append(positions, item.Position)
	}

	if len(lost) > 0 {
		if err := s.repository.SaveImportJobItems(ctx, id, lost); err != nil {
			return Job{}, err
		}
	}

	if len(planned) == 0 {
		if err := s.repository.UpdateImportJobState(ctx, id, JobStateUpdate{
			State: StateCompletedWithErrors, FinishedAt: &now,
		}); err != nil {
			return Job{}, err
		}

		return Job{}, ErrNotResumable
	}

	if err := s.charge(run, exchange.PlannedRetainedBytes(planned)); err != nil {
		return Job{}, err
	}

	if err := s.repository.UpdateImportJobState(ctx, id, JobStateUpdate{State: StatePending}); err != nil {
		return Job{}, err
	}
	// The resumed run addresses its items by their original positions, so a
	// second attempt writes to the same rows rather than appending a parallel
	// history of the same recipes.
	submitted = true

	go s.execute(job, planned, positions, run)

	return s.repository.GetImportJob(ctx, id)
}

// unresumableReason says what this particular recipe would need; a recipe
// imported from a link never came from a file to choose again.
func unresumableReason(item Item) string {
	switch {
	case item.Filename != "":
		return "Savorum no longer holds " + item.Filename + ", the file this recipe came from. Choose it again to import this recipe."
	case item.SourceURL != "":
		return "Savorum did not keep a copy of this recipe, so it cannot be run again from here. Import " + item.SourceURL + " again."
	default:
		return "Savorum did not keep a copy of this recipe, so it cannot be run again from here. Import it again from its source."
	}
}

// Recover marks every job a stopped process left mid-flight. It runs once at
// startup, before the first request, so a job can never be found running by a
// process that is not running it.
func (s *Service) Recover(ctx context.Context) (int, error) {
	interrupted, err := s.repository.InterruptImportJobs(ctx, s.now())
	if err != nil {
		return 0, err
	}

	if interrupted > 0 {
		s.logger.Info("recovered interrupted import jobs", "jobs", interrupted)
	}

	return interrupted, nil
}

// Close stops scheduling and waits for the batch in flight. A run that has not
// finished records itself as interrupted, which is the same state a crash
// leaves behind and is resumed the same way.
func (s *Service) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}

	s.closed = true

	runs := make([]*activeRun, 0, len(s.active))
	for _, run := range s.active {
		runs = append(runs, run)
	}
	s.mu.Unlock()
	s.stop()

	for _, run := range runs {
		run.requestCancellation()
	}

	s.running.Wait()
}

func randomJobID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("create import job identifier: %w", err)
	}

	return hex.EncodeToString(data), nil
}

func (s *Service) actorContext(job Job) context.Context {
	ctx := access.WithLibrary(identity.WithCorrelation(s.lifetime, job.ID), job.LibraryID)
	if job.Actor.UserID != "" {
		ctx = identity.WithPrincipal(ctx, job.Actor)
	}

	return ctx
}
