package automation

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/idempotency"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/importjob"
	"github.com/savorum/savorum/internal/library"
)

// Service has one bounded worker for these explicitly supported workflows.
// Parsing/compression never runs inside a SQLite transaction.
type Service struct {
	repository Repository
	library    Library
	exchange   Exchange
	imports    ImportJobs
	exporter   *export.Service
	receipts   *idempotency.Coordinator
	artifacts  *os.Root
	mu         sync.Mutex
}

// New opens an owner-only artifact root and composes the existing domain services.
func New(repository Repository, libraryService Library, exchanger Exchange, imports ImportJobs, exporter *export.Service, receipts *idempotency.Coordinator, directory string) (*Service, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, operationError("New", err)
	}

	if err := os.Chmod(directory, 0o700); err != nil { //nolint:gosec // Owner-only directory requires search permission.
		return nil, operationError("New", err)
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, operationError("New", err)
	}

	return &Service{repository: repository, library: libraryService, exchange: exchanger, imports: imports, exporter: exporter, receipts: receipts, artifacts: root}, nil
}

// Close releases the private artifact root after workers have stopped.
func (s *Service) Close() error { return operationError("close private root", s.artifacts.Close()) }

func (s *Service) create(ctx context.Context, kind string, input any) (Record, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return Record{}, operationError("create", err)
	}

	actor, ok := identity.FromContext(ctx)
	if !ok {
		return Record{}, access.ErrForbidden
	}

	now := time.Now().UTC()

	record := Record{Job: Job{ID: "job_" + rand.Text(), Revision: 1, Kind: kind, State: "pending", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}, Actor: actor, LibraryID: access.LibraryID(ctx), Input: data, Results: []Result{}}
	if err := s.repository.CreateAutomationJob(ctx, record); err != nil {
		return Record{}, operationError("create", err)
	}

	return record, nil
}

// StartBulk validates typed input before creating independently authorized work.
func (s *Service) StartBulk(ctx context.Context, input BulkRequest) (Record, error) {
	if err := ValidateBulk(input); err != nil {
		return Record{}, operationError("StartBulk", err)
	}

	return s.create(ctx, "bulk-"+input.Action, input)
}

// StartImport holds private input for shared parsing under an explicit commit policy.
func (s *Service) StartImport(ctx context.Context, input ImportInput) (Record, error) {
	if s.exchange == nil || s.imports == nil {
		return Record{}, ErrInvalid
	}

	if !slices.Contains([]string{"preview-only", "require-no-warnings", "allow-warnings"}, input.Policy) || !slices.Contains([]string{"reject", "skip"}, input.OnDuplicate) {
		return Record{}, ErrInvalid
	}

	switch input.Kind {
	case "url":
		if input.URL == "" || input.Filename != "" || len(input.Data) != 0 {
			return Record{}, ErrInvalid
		}
	case "file", "text":
		if input.URL != "" || len(input.Data) == 0 || len(input.Data) > 64<<20 || input.Filename == "" || strings.ContainsAny(input.Filename, "/\\") || len(input.Filename) > 255 {
			return Record{}, ErrInvalid
		}
	default:
		return Record{}, ErrInvalid
	}

	return s.create(ctx, "import", input)
}

// StartExport queues a bounded immutable snapshot using the ordinary export service.
func (s *Service) StartExport(ctx context.Context, input ExportInput) (Record, error) {
	if !slices.Contains([]string{string(export.FormatSREF), string(export.FormatSREFBundle)}, input.Format) || (input.Library && len(input.Recipes) != 0) || (!input.Library && len(input.Recipes) == 0) || len(input.Recipes) > 10000 {
		return Record{}, ErrInvalid
	}

	if input.Format == string(export.FormatSREF) && (input.Library || len(input.Recipes) != 1) {
		return Record{}, ErrInvalid
	}

	return s.create(ctx, "export", input)
}

// Get rechecks the caller and projects linked import progress into the common lifecycle.
func (s *Service) Get(ctx context.Context, id string) (Record, error) {
	record, err := s.repository.AutomationJob(ctx, id)
	if err != nil {
		return record, operationError("Get", err)
	}

	if record.Job.ImportJobID != "" {
		job, err := s.imports.Get(ctx, record.Job.ImportJobID)
		if err != nil {
			return record, operationError("Get", err)
		}

		record.Job.State = string(job.State)
		record.Job.Total = job.TotalItems
		record.Job.Finished = job.Progress.Imported + job.Progress.Skipped + job.Progress.Failed + job.Progress.Cancelled
	}

	return record, nil
}

// Run recovers conservatively, cleans expired data and processes two queued
// records at a time. The owner cancels and joins it before closing the database.
func (s *Service) Run(ctx context.Context) error {
	if err := s.repository.RecoverAutomationJobs(ctx); err != nil {
		return operationError("Run", err)
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		if err := s.ProcessPending(ctx); err != nil && ctx.Err() == nil {
			return operationError("Run", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// ProcessPending advances a bounded set of jobs and expires private retained artifacts.
func (s *Service) ProcessPending(ctx context.Context) error {
	if _, err := s.sweepArtifacts(); err != nil {
		return operationError("ProcessPending", err)
	}

	ids, err := s.repository.ExpireAutomationJobs(ctx)
	if err != nil {
		return operationError("ProcessPending", err)
	}

	for _, id := range ids {
		if err := s.artifacts.Remove(id + ".artifact"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return operationError("ProcessPending", err)
		}
	}

	records, err := s.repository.PendingAutomationJobs(ctx)
	if err != nil {
		return operationError("ProcessPending", err)
	}

	for _, record := range records {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		work := identity.WithPrincipal(access.WithLibrary(ctx, record.LibraryID), record.Actor)
		if err := s.process(work, &record); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			if errors.Is(err, access.ErrForbidden) {
				if e := s.repository.FailAutomationJob(ctx, record.Job.ID, record.Job.Revision, "forbidden"); e != nil && !errors.Is(e, ErrState) {
					return e
				}
			} else if !errors.Is(err, ErrState) {
				return operationError("ProcessPending", err)
			}
		}
	}

	return nil
}

func (s *Service) process(ctx context.Context, record *Record) error {
	record.Job.State = "running"
	if err := s.repository.SaveAutomationJob(ctx, record); err != nil {
		return operationError("process", err)
	}

	var err error

	switch {
	case strings.HasPrefix(record.Job.Kind, "bulk-"):
		err = s.runBulk(ctx, record)
	case record.Job.Kind == "export":
		err = s.runExport(ctx, record)
	case record.Job.Kind == "import":
		err = s.prepareImport(ctx, record)
	default:
		err = ErrInvalid
	}

	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		record.Job.State = "failed"

		record.Job.Code = "operation-failed"
		if errors.Is(err, access.ErrForbidden) {
			record.Job.Code = "forbidden"
		}

		if errors.Is(err, ErrCapacity) {
			record.Job.Code = "artifact-capacity"
		}

		if errors.Is(err, export.ErrTooLarge) {
			record.Job.Code = "export-too-large"
		}

		if errors.Is(err, export.ErrAssetUnavailable) {
			record.Job.Code = "export-asset-unavailable"
		}

		if errors.Is(err, ErrInvalid) {
			record.Job.Code = "request-invalid"
		}
	}

	return s.repository.SaveAutomationJob(ctx, record)
}

func (s *Service) runBulk(ctx context.Context, record *Record) error {
	var input BulkRequest
	if err := json.Unmarshal(record.Input, &input); err != nil {
		return operationError("runBulk", err)
	}

	record.Job.Total = len(input.Items)
	for _, item := range input.Items[len(record.Results):] {
		if err := ctx.Err(); err != nil {
			return operationError("runBulk", err)
		}

		current, err := s.repository.AutomationJob(ctx, record.Job.ID)
		if err != nil {
			return operationError("runBulk", err)
		}

		if current.Job.State != "running" {
			return ErrState
		}

		record.InFlightID = item.ID
		if err := s.repository.SaveAutomationJob(ctx, record); err != nil {
			return operationError("runBulk", err)
		}

		result := BulkItem(ctx, s.library, input, item)
		record.InFlightID = ""
		record.Results = append(record.Results, result)
		record.Job.Finished++

		if result.Status == "succeeded" && input.Action == "book" {
			input.BookRevision = result.BookRevision
		}

		record.Input, err = json.Marshal(input)
		if err != nil {
			return operationError("runBulk", err)
		}

		if err := s.repository.SaveAutomationJob(ctx, record); err != nil {
			return operationError("runBulk", err)
		}
	}

	record.Job.State = "completed"
	if slices.ContainsFunc(record.Results, func(r Result) bool { return r.Status == "failed" }) {
		record.Job.State = "completed-with-errors"
	}

	return nil
}

func (s *Service) runExport(ctx context.Context, record *Record) error {
	used, err := s.sweepArtifacts()
	if err != nil {
		return operationError("runExport", err)
	}

	if used > (2<<30)-(512<<20) {
		return ErrCapacity
	}

	var input ExportInput
	if err := json.Unmarshal(record.Input, &input); err != nil {
		return operationError("runExport", err)
	}

	ids := make([]library.EntryID, 0, len(input.Recipes))
	for _, id := range input.Recipes {
		ids = append(ids, library.EntryID(id))
	}

	if input.Library {
		refs, err := s.library.ListReferences(ctx, library.StateActive)
		if err != nil {
			return operationError("runExport", err)
		}

		for _, ref := range refs {
			ids = append(ids, ref.ID)
		}
	}

	source := export.NewBoundedSelectionSource(s.exporter.Library(), ids, 10000)
	if input.Format == string(export.FormatSREF) {
		source = export.NewRecipeSource(s.exporter.Library(), ids[0])
	}

	plan, err := s.exporter.Prepare(ctx, source, export.Format(input.Format))
	if err != nil {
		return operationError("runExport", err)
	}

	file, err := s.artifacts.OpenFile(record.Job.ID+".artifact", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return operationError("runExport", err)
	}

	err = s.exporter.Write(ctx, plan, file)

	err = errors.Join(err, file.Sync(), file.Close())
	if err != nil {
		_ = s.artifacts.Remove(record.Job.ID + ".artifact")
		return operationError("runExport", err)
	}

	record.Job.State = "completed"
	record.Job.Total = len(plan.Recipes)
	record.Job.Finished = len(plan.Recipes)
	record.Job.Filename = plan.Filename
	record.Job.MediaType = plan.MediaType
	record.Job.DownloadHandle = rand.Text()

	return nil
}

func (s *Service) prepareImport(ctx context.Context, record *Record) error {
	var input ImportInput
	if err := json.Unmarshal(record.Input, &input); err != nil {
		return operationError("prepareImport", err)
	}

	var (
		preview exchange.ImportPreview
		err     error
	)
	if input.Kind == "url" {
		preview, err = s.exchange.PreviewURL(ctx, input.URL)
	} else {
		preview, err = s.exchange.PreviewFiles(ctx, []exchange.UploadedImport{{Filename: input.Filename, Data: input.Data}})
	}

	if err != nil {
		return operationError("prepareImport", err)
	}

	defer func() { _ = s.exchange.CancelPreviewScoped(ctx, preview.ID) }()

	record.Preview = &preview
	record.Job.Total = len(preview.Items)
	selected := []exchange.PreviewResolution{}

	for _, item := range preview.Items {
		if item.CanCommit {
			selected = append(selected, exchange.PreviewResolution{ItemID: item.ID})
		}
	}

	if len(selected) > 0 {
		plan, err := s.exchange.PlanPreviewImport(ctx, preview.ID, selected, false)
		if err != nil {
			return operationError("prepareImport", err)
		}

		for _, item := range plan.Items {
			if !item.Resumable() {
				return ErrInvalid
			}

			record.Payloads = append(record.Payloads, item.Payload())
		}
	}
	// The frozen plan replaces held upload bytes. The preview result and its
	// fidelity diagnostics survive a restart without refetching a changing URL.
	input.Data = nil

	record.Input, err = json.Marshal(input)
	if err != nil {
		return operationError("prepareImport", err)
	}

	record.Job.State = "awaiting-review"
	if err := s.repository.SaveAutomationJob(ctx, record); err != nil {
		return operationError("prepareImport", err)
	}

	if input.Policy != "preview-only" {
		updated, err := s.Commit(ctx, record.Job.ID, input.Policy)
		if err != nil {
			return operationError("prepareImport", err)
		}

		*record = updated
	}

	return nil
}

func (s *Service) commitPrepared(ctx context.Context, record *Record, policy string) error {
	if record.Preview == nil || len(record.Payloads) == 0 {
		return ErrState
	}

	if policy != "require-no-warnings" && policy != "allow-warnings" {
		return ErrInvalid
	}

	for _, item := range record.Preview.Items {
		if !item.CanCommit {
			record.Job.Code = "import-errors-require-review"
			return nil
		}

		for _, diagnostic := range item.Diagnostics {
			if diagnostic.Severity == exchange.SeverityError || (policy == "require-no-warnings" && diagnostic.Severity == exchange.SeverityWarning) {
				record.Job.Code = "import-diagnostics-require-review"
				return nil
			}
		}
	}

	data := []byte(record.Job.ID)

	work, existing, release, err := s.receipts.Begin(ctx, "automation-import-commit", record.Job.ID, data)
	if err != nil {
		return operationError("commitPrepared", err)
	}
	defer release()

	if existing != "" {
		record.Job.ImportJobID = existing
		record.Job.State = "committing"

		return nil
	}

	items := make([]exchange.PlannedImport, 0, len(record.Payloads))
	for _, payload := range record.Payloads {
		item, err := exchange.RestorePlanned(payload)
		if err != nil {
			return operationError("commitPrepared", err)
		}

		items = append(items, item)
	}

	var input ImportInput
	if err := json.Unmarshal(record.Input, &input); err != nil {
		return operationError("commitPrepared", err)
	}

	job, err := s.imports.Start(work, importjob.StartRequest{Importer: "automation", Source: input.Filename, Policy: library.DuplicatePolicy(input.OnDuplicate), Items: items})
	if err != nil {
		return operationError("commitPrepared", err)
	}

	record.Job.ImportJobID = job.ID
	record.Job.State = "committing"
	record.Job.Code = ""
	record.Payloads = nil

	return nil
}

// Commit preserves the frozen parse and explicitly applies the caller's policy.
func (s *Service) Commit(ctx context.Context, id, policy string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, err := s.repository.AutomationJob(ctx, id)
	if err != nil {
		return record, operationError("Commit", err)
	}

	if record.Job.ImportJobID != "" {
		return s.Get(ctx, id)
	}

	if record.Job.Kind != "import" || record.Job.State != "awaiting-review" {
		return record, ErrState
	}

	if err := s.commitPrepared(ctx, &record, policy); err != nil {
		return record, operationError("Commit", err)
	}

	if err := s.repository.SaveAutomationJob(ctx, &record); err != nil {
		return record, operationError("Commit", err)
	}

	return record, nil
}

// Cancel preserves completed outcomes and prevents further queued mutations.
func (s *Service) Cancel(ctx context.Context, id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, err := s.repository.AutomationJob(ctx, id)
	if err != nil {
		return record, operationError("Cancel", err)
	}

	if record.Job.ImportJobID != "" {
		if _, err := s.imports.Cancel(ctx, record.Job.ImportJobID); err != nil {
			return record, operationError("Cancel", err)
		}

		return s.Get(ctx, id)
	}

	// A finished job keeps the outcome it reported.
	if slices.Contains([]string{"completed", "completed-with-errors", "failed", "cancelled"}, record.Job.State) {
		return record, nil
	}

	record.Job.State = "cancelled"
	if record.InFlightID != "" {
		record.Job.Code = "outcome-unknown"
		record.Results = append(record.Results, Result{ID: record.InFlightID, Status: "failed", Code: "outcome-unknown"})
		record.InFlightID = ""
		record.Job.Finished++
	}

	record.Payloads = nil

	record.Input = nil
	if err := s.repository.SaveAutomationJob(ctx, &record); err != nil {
		return record, operationError("Cancel", err)
	}

	return record, nil
}

// Download requires current creator authorization and an unexpired opaque handle.
func (s *Service) Download(ctx context.Context, id, handle string) (Job, io.ReadCloser, error) {
	record, err := s.repository.AutomationJob(ctx, id)
	if err != nil {
		return Job{}, nil, operationError("Download", err)
	}

	if record.Job.Kind != "export" || record.Job.State != "completed" || handle == "" || handle != record.Job.DownloadHandle {
		return Job{}, nil, ErrNotFound
	}

	file, err := s.artifacts.Open(record.Job.ID + ".artifact")
	if err != nil {
		return Job{}, nil, ErrNotFound
	}

	return record.Job, file, nil
}

// ArtifactDirectory is a private sibling of the instance's database, not a
// client-selected input path or a static-file serving root.
func ArtifactDirectory(database string) string {
	return database + ".automation-artifacts"
}

// Resume never guesses whether an interrupted mutation committed. That one item
// receives an explicit unknown outcome; all other items retain their preconditions.
func (s *Service) Resume(ctx context.Context, id string, revision uint64) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, err := s.repository.AutomationJob(ctx, id)
	if err != nil {
		return record, operationError("Resume", err)
	}

	if record.Job.Revision != revision {
		return record, &RevisionConflict{Expected: revision, Current: record.Job.Revision}
	}

	if record.Job.ImportJobID != "" {
		if _, err = s.imports.Resume(ctx, record.Job.ImportJobID); err != nil {
			return record, operationError("Resume", err)
		}

		return s.Get(ctx, id)
	}

	if record.Job.State != "interrupted" {
		return record, ErrState
	}

	if record.InFlightID != "" {
		record.Results = append(record.Results, Result{ID: record.InFlightID, Status: "failed", Code: "outcome-unknown"})
		record.Job.Finished++
		record.InFlightID = ""
	}

	record.Job.Code = ""
	if record.Job.Kind == "import" && record.Preview != nil {
		record.Job.State = "awaiting-review"
	} else {
		record.Job.State = "pending"
	}

	if record.Job.Kind == "export" {
		if err = s.artifacts.Remove(id + ".artifact"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return record, operationError("Resume", err)
		}
	}

	err = s.repository.SaveAutomationJob(ctx, &record)

	return record, operationError("Resume", err)
}

// Results reads an authorized bounded page without loading other recipe snapshots.
func (s *Service) Results(ctx context.Context, id string, offset, limit int) ([]Result, error) {
	return s.repository.AutomationResults(ctx, id, offset, limit)
}
