package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/event"
	"github.com/savorum/savorum/internal/identity"

	"github.com/savorum/savorum/internal/access"

	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/importjob"
	"github.com/savorum/savorum/internal/library"
)

var _ importjob.Repository = (*Store)(nil)

const importJobColumns = `id, importer, source, state, duplicate_policy, total_items, batch_size,
	error, created_at, started_at, finished_at, cancellation_requested_at, resolution_decisions_json, library_id, actor_id, credential_id`

// CreateImportJob records a job and its pending items atomically.
func (s *Store) CreateImportJob(ctx context.Context, job importjob.Job, items []importjob.Item) error {
	if job.Actor.UserID == "" {
		actor, err := s.LocalPrincipal(ctx)
		if err != nil {
			return operationError("CreateImportJob", err)
		}

		job.Actor = actor
	}

	if err := s.authorizeScope(ctx, access.Editor); err != nil {
		return operationError("CreateImportJob", err)
	}

	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import job: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	decisions, err := json.Marshal(job.ResolutionDecisions)
	if err != nil {
		return fmt.Errorf("encode import resolution decisions: %w", err)
	}

	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO import_jobs (`+importJobColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Importer, job.Source, string(job.State), string(job.DuplicatePolicy), job.TotalItems,
		job.BatchSize, nullableText(job.Error), formatInstant(job.CreatedAt), nullableInstant(job.StartedAt),
		nullableInstant(job.FinishedAt), nullableInstant(job.CancellationRequestedAt), string(decisions), scopeID(ctx), job.Actor.UserID, job.Actor.CredentialID,
	); err != nil {
		return fmt.Errorf("record import job: %w", err)
	}

	for _, item := range items {
		diagnostics, err := encodeDiagnostics(item.Diagnostics)
		if err != nil {
			return operationError("CreateImportJob", err)
		}

		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO import_job_items (
				job_id, position, source_item_id, title, filename, source_url, state, review_required,
				outcome, entry_id, recipe_id, revision, dropped_assets, deferred_assets,
				diagnostics_json, error, payload_json, started_at, finished_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, NULL, 0, 0, ?, NULL, ?, NULL, NULL)`,
			job.ID, item.Position, item.SourceItemID, item.Title, item.Filename, item.SourceURL,
			string(item.State), boolToInt(item.ReviewRequired), diagnostics, nullablePayload(item.Payload),
		); err != nil {
			return fmt.Errorf("record import job item %d: %w", item.Position, err)
		}
	}

	if err := recordIdempotency(ctx, transaction, job.ID); err != nil {
		return operationError("CreateImportJob", err)
	}

	if err := appendImportJobEvent(ctx, transaction, job.ID, "import.started", string(job.State)); err != nil {
		return operationError("CreateImportJob", err)
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit import job: %w", err)
	}

	return nil
}

// GetImportJob returns a job and its derived item progress.
func (s *Store) GetImportJob(ctx context.Context, id string) (importjob.Job, error) {
	if err := s.scopedImportJob(ctx, id, access.Viewer); err != nil {
		return importjob.Job{}, operationError("GetImportJob", err)
	}

	row := s.db.QueryRowContext(ctx, `SELECT `+importJobColumns+` FROM import_jobs WHERE id = ?`, id)

	job, err := scanImportJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return importjob.Job{}, importjob.ErrNotFound
	}

	if err != nil {
		return importjob.Job{}, operationError("GetImportJob", err)
	}

	summaries, err := s.importJobSummaries(ctx, []string{id})
	if err != nil {
		return importjob.Job{}, operationError("GetImportJob", err)
	}

	job.Progress = summaries[id].progress
	job.LeadTitle = summaries[id].leadTitle

	return job, nil
}

// ListImportJobs returns a page of jobs and the matching total.
func (s *Store) ListImportJobs(ctx context.Context, options importjob.ListOptions) ([]importjob.Job, int, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, 0, operationError("ListImportJobs", err)
	}

	where, arguments := " WHERE library_id = ?", []any{scopeID(ctx)}
	if options.State != "" {
		where += " AND state = ?"

		arguments = append(arguments, string(options.State))
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM import_jobs`+where, arguments...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count import jobs: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT `+importJobColumns+` FROM import_jobs`+where+`
		ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		append(append([]any{}, arguments...), options.Limit, options.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list import jobs: %w", err)
	}
	defer rows.Close()

	jobs := []importjob.Job{}
	identifiers := []string{}

	for rows.Next() {
		job, err := scanImportJob(rows)
		if err != nil {
			return nil, 0, operationError("ListImportJobs", err)
		}

		jobs = append(jobs, job)
		identifiers = append(identifiers, job.ID)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate import jobs: %w", err)
	}

	summaries, err := s.importJobSummaries(ctx, identifiers)
	if err != nil {
		return nil, 0, operationError("ListImportJobs", err)
	}

	for index := range jobs {
		jobs[index].Progress = summaries[jobs[index].ID].progress
		jobs[index].LeadTitle = summaries[jobs[index].ID].leadTitle
	}

	return jobs, total, nil
}

type importJobSummary struct {
	progress     importjob.Progress
	leadTitle    string
	leadPosition int
	named        bool
}

// importJobSummaries reads every listed job's items in one pass.
func (s *Store) importJobSummaries(ctx context.Context, identifiers []string) (map[string]importJobSummary, error) {
	details := map[string]importJobSummary{}
	progress := map[string]importjob.Progress{}
	summaries := map[string]map[string]importjob.DiagnosticSummary{}

	if len(identifiers) == 0 {
		return details, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(identifiers)), ", ")

	arguments := make([]any, 0, len(identifiers))
	for _, identifier := range identifiers {
		arguments = append(arguments, identifier)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT job_id, state, review_required, diagnostics_json,
		       payload_json IS NOT NULL AND payload_json <> '', title, position, COALESCE(error, '')
		FROM import_job_items
		WHERE job_id IN (`+placeholders+`)`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("count import job items: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			jobID, state, encoded, title, itemError string
			review, retained, position              int
		)
		if err := rows.Scan(&jobID, &state, &review, &encoded, &retained, &title, &position, &itemError); err != nil {
			return nil, fmt.Errorf("scan import job progress: %w", err)
		}

		// The first recipe by position, which is the order the reader chose
		// them in and the order the job works through them.
		if detail := details[jobID]; !detail.named || position < detail.leadPosition {
			detail.leadTitle, detail.leadPosition, detail.named = title, position, true
			details[jobID] = detail
		}

		var diagnostics []importer.Diagnostic
		if err := json.Unmarshal([]byte(encoded), &diagnostics); err != nil {
			return nil, fmt.Errorf("read import job progress diagnostics: %w", err)
		}

		progress[jobID], summaries[jobID] = accumulateImportJobItemProgress(progress[jobID], summaries[jobID], importjob.ItemState(state), review, retained == 1, diagnostics)
		if state == string(importjob.ItemFailed) && importjob.ClassifyFailure(itemError) == importjob.FailureAlreadyInLibrary {
			current := progress[jobID]

			current.AlreadyInLibrary++
			if retained == 1 {
				current.Resumable--
			}

			progress[jobID] = current
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate import job progress: %w", err)
	}

	for jobID, grouped := range summaries {
		current := progress[jobID]

		keys := make([]string, 0, len(grouped))
		for key := range grouped {
			keys = append(keys, key)
		}

		sort.Strings(keys)

		for _, key := range keys {
			current.Diagnostics = append(current.Diagnostics, grouped[key])
		}

		progress[jobID] = current
	}

	for jobID, counted := range progress {
		detail := details[jobID]
		detail.progress = counted
		details[jobID] = detail
	}

	return details, nil
}

func accumulateImportJobItemProgress(progress importjob.Progress, summaries map[string]importjob.DiagnosticSummary, state importjob.ItemState, review int, retained bool, diagnostics []importer.Diagnostic) (importjob.Progress, map[string]importjob.DiagnosticSummary) {
	progress.Total++
	progress.ReviewRequired += review

	// A resumable recipe is an unfinished one that kept its payload. An
	// imported or skipped recipe has nowhere left to go, however much of it
	// Savorum still holds.
	switch state {
	case importjob.ItemPending, importjob.ItemRunning, importjob.ItemFailed, importjob.ItemCancelled:
		if retained {
			progress.Resumable++
		}
	case importjob.ItemImported, importjob.ItemSkipped:
	}

	var hasInformation bool

	progress, summaries, hasInformation = accumulateProgressDiagnostics(progress, summaries, diagnostics)
	countImportJobItemState(&progress, state)

	finishedRecipe := state == importjob.ItemImported || state == importjob.ItemSkipped
	if finishedRecipe && review == 0 && hasInformation {
		progress.InformationalPreservation++
	}

	if finishedRecipe && review == 0 && len(diagnostics) == 0 {
		progress.WithoutDiagnostics++
	}

	return progress, summaries
}

func accumulateProgressDiagnostics(progress importjob.Progress, summaries map[string]importjob.DiagnosticSummary, diagnostics []importer.Diagnostic) (importjob.Progress, map[string]importjob.DiagnosticSummary, bool) {
	hasInformation := false
	seenSummaries := map[string]bool{}

	for _, diagnostic := range diagnostics {
		progress.DiagnosticOccurrences += diagnostic.Occurrences()

		class := diagnostic.EffectiveClass()
		hasInformation = hasInformation || class == importer.DiagnosticInformational

		if summaries == nil {
			summaries = map[string]importjob.DiagnosticSummary{}
		}

		key := diagnostic.Code + "\x00" + string(class) + "\x00" + diagnostic.Area

		summary := summaries[key]
		if summary.Code == "" {
			summary = importjob.DiagnosticSummary{
				Code: diagnostic.Code, Class: class, Area: diagnostic.Area, Message: diagnostic.Message,
			}
		}

		summary.Occurrences += diagnostic.Occurrences()
		if !seenSummaries[key] {
			summary.Recipes++
			seenSummaries[key] = true
		}

		summaries[key] = summary
	}

	return progress, summaries, hasInformation
}

func countImportJobItemState(progress *importjob.Progress, state importjob.ItemState) {
	switch state {
	case importjob.ItemPending:
		progress.Pending++
	case importjob.ItemRunning:
		progress.Running++
	case importjob.ItemImported:
		progress.Imported++
	case importjob.ItemSkipped:
		progress.Skipped++
	case importjob.ItemFailed:
		progress.Failed++
	case importjob.ItemCancelled:
		progress.Cancelled++
	}
}

const importJobItemColumns = `job_id, position, source_item_id, title, filename, source_url, state,
	review_required, outcome, entry_id, recipe_id, revision, dropped_assets, deferred_assets,
	diagnostics_json, error, started_at, finished_at,
 COALESCE((SELECT id FROM import_events WHERE import_events.entry_id = import_job_items.entry_id AND import_events.revision = import_job_items.revision AND import_job_items.state = 'imported'), '')`

// ListImportJobItems returns a filtered page of a job's items and its total.
func (s *Store) ListImportJobItems(ctx context.Context, jobID string, options importjob.ItemListOptions) ([]importjob.Item, int, error) {
	if err := s.scopedImportJob(ctx, jobID, access.Viewer); err != nil {
		return nil, 0, operationError("ListImportJobItems", err)
	}

	if err := s.requireImportJob(ctx, jobID); err != nil {
		return nil, 0, operationError("ListImportJobItems", err)
	}

	where, arguments := importJobItemFilter(jobID, options.Filter)

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM import_job_items`+where, arguments...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count import job items: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT `+importJobItemColumns+` FROM import_job_items`+where+`
		ORDER BY position LIMIT ? OFFSET ?`,
		append(append([]any{}, arguments...), options.Limit, options.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list import job items: %w", err)
	}
	defer rows.Close()

	items := []importjob.Item{}

	for rows.Next() {
		item, err := scanImportJobItem(rows)
		if err != nil {
			return nil, 0, operationError("ListImportJobItems", err)
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate import job items: %w", err)
	}

	return items, total, nil
}

// importJobItemFilter translates the reader's vocabulary, where "review
// required" is not a state.
func importJobItemFilter(jobID string, filter importjob.ItemFilter) (string, []any) {
	arguments := []any{jobID}

	switch filter {
	case importjob.ItemsReviewRequired:
		return " WHERE job_id = ? AND review_required = 1", arguments
	case importjob.ItemsInformational:
		return " WHERE job_id = ? AND review_required = 0 AND state IN ('imported', 'skipped') AND json_array_length(diagnostics_json) > 0", arguments
	case importjob.ItemsAll:
		return " WHERE job_id = ?", arguments
	case importjob.ItemsImported, importjob.ItemsSkipped, importjob.ItemsFailed,
		importjob.ItemsPending, importjob.ItemsCancelled:
		// These filters name a stored state, so they are the state itself.
		return " WHERE job_id = ? AND state = ?", append(arguments, string(filter))
	default:
		return " WHERE job_id = ? AND state = ?", append(arguments, string(filter))
	}
}

func (s *Store) requireImportJob(ctx context.Context, jobID string) error {
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM import_jobs WHERE id = ?)", jobID).Scan(&exists); err != nil {
		return fmt.Errorf("check import job: %w", err)
	}

	if exists == 0 {
		return importjob.ErrNotFound
	}

	return nil
}

// UpdateImportJobState records a job state transition.
func (s *Store) UpdateImportJobState(ctx context.Context, id string, update importjob.JobStateUpdate) error {
	if err := s.authorizeJobOutcome(ctx, id); err != nil {
		return operationError("UpdateImportJobState", err)
	}

	// started_at only ever moves forward from nothing, so a resumed job keeps
	// the moment it first began. finished_at is written as given, because a
	// job that starts again is not finished any more.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operationError("UpdateImportJobState", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE import_jobs
		SET cancellation_requested_at = CASE WHEN ? = 'pending' THEN NULL ELSE cancellation_requested_at END,
            state = ?, error = ?, started_at = COALESCE(?, started_at), finished_at = ?
		WHERE id = ?`,
		string(update.State), string(update.State), nullableText(update.Error),
		nullableInstant(update.StartedAt), nullableInstant(update.FinishedAt), id)
	if err != nil {
		return fmt.Errorf("record import job state: %w", err)
	}

	if err := requireImportJobRow(result); err != nil {
		return operationError("UpdateImportJobState", err)
	}

	kind := "import.updated"

	switch update.State {
	case importjob.StatePending, importjob.StateRunning, importjob.StateInterrupted:
		// Progress and interruption retain the normal update event.
	case importjob.StateCompleted, importjob.StateCompletedWithErrors:
		kind = "import.completed"
	case importjob.StateFailed:
		kind = "import.failed"
	case importjob.StateCancelled:
		kind = "import.cancelled"
	}

	if err := appendImportJobEvent(ctx, tx, id, kind, string(update.State)); err != nil {
		return operationError("UpdateImportJobState", err)
	}

	return operationError("UpdateImportJobState", tx.Commit())
}

// RequestImportJobCancellation records the first requested cancellation time.
func (s *Store) RequestImportJobCancellation(ctx context.Context, id string, at time.Time) error {
	if err := s.scopedImportJob(ctx, id, access.Editor); err != nil {
		return operationError("RequestImportJobCancellation", err)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE import_jobs SET cancellation_requested_at = COALESCE(cancellation_requested_at, ?)
		WHERE id = ?`, formatInstant(at), id)
	if err != nil {
		return fmt.Errorf("record import job cancellation: %w", err)
	}

	return requireImportJobRow(result)
}

// SaveImportJobItems records what became of recipes the job has reached. It
// writes outcome and progress only: identity, source, and the stored payload
// belong to the item's creation and are never rewritten by a run.
func (s *Store) SaveImportJobItems(ctx context.Context, jobID string, items []importjob.Item) error {
	if err := s.authorizeJobOutcome(ctx, jobID); err != nil {
		return operationError("SaveImportJobItems", err)
	}

	if len(items) == 0 {
		return nil
	}

	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import job progress: %w", err)
	}

	defer func() { _ = transaction.Rollback() }()

	for _, item := range items {
		diagnostics, err := encodeDiagnostics(item.Diagnostics)
		if err != nil {
			return operationError("SaveImportJobItems", err)
		}

		result, err := transaction.ExecContext(ctx, `
			UPDATE import_job_items
			SET state = ?, review_required = ?, outcome = ?, entry_id = ?, recipe_id = ?, revision = ?,
			    dropped_assets = ?, deferred_assets = ?, diagnostics_json = ?, error = ?,
			    title = CASE WHEN ? <> '' THEN ? ELSE title END,
			    started_at = COALESCE(?, started_at), finished_at = ?
			WHERE job_id = ? AND position = ?`,
			string(item.State), boolToInt(item.ReviewRequired), nullableText(string(item.Outcome)),
			nullableText(string(item.EntryID)), nullableText(item.RecipeID), nullableRevision(item.Revision),
			item.DroppedAssets, item.DeferredAssets, diagnostics, nullableText(item.Error),
			item.Title, item.Title,
			nullableInstant(item.StartedAt), nullableInstant(item.FinishedAt),
			jobID, item.Position)
		if err != nil {
			return fmt.Errorf("record import job item %d: %w", item.Position, err)
		}

		if err := requireImportJobRow(result); err != nil {
			return operationError("SaveImportJobItems", err)
		}
	}

	if err := appendImportJobEvent(ctx, transaction, jobID, "import.updated", ""); err != nil {
		return operationError("SaveImportJobItems", err)
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit import job progress: %w", err)
	}

	return nil
}

// InterruptImportJobs is startup recovery. A job left `running` is
// interrupted, and an item that was mid-commit goes back to pending because
// its atomic commit either landed or did not.
//
// Items are normalized for already-interrupted jobs too, since a process that
// noticed its shutdown may not have finished tidying them. Recovery runs
// before the listener opens.
func (s *Store) InterruptImportJobs(ctx context.Context, at time.Time) (int, error) {
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin import job recovery: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	if _, err := transaction.ExecContext(ctx, `
		UPDATE import_job_items SET state = 'pending', started_at = NULL
		WHERE state = 'running'
		  AND job_id IN (SELECT id FROM import_jobs WHERE state IN ('pending', 'running', 'interrupted'))`,
	); err != nil {
		return 0, fmt.Errorf("recover import job items: %w", err)
	}

	result, err := transaction.ExecContext(ctx, `
		UPDATE import_jobs SET state = 'interrupted', finished_at = ?
		WHERE state IN ('pending', 'running')`, formatInstant(at))
	if err != nil {
		return 0, fmt.Errorf("recover import jobs: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count recovered import jobs: %w", err)
	}

	if err := transaction.Commit(); err != nil {
		return 0, fmt.Errorf("commit import job recovery: %w", err)
	}

	return int(affected), nil
}

// LoadImportJobItems returns items in the named states with their stored
// payloads, the only read that does.
func (s *Store) LoadImportJobItems(ctx context.Context, jobID string, states []importjob.ItemState) ([]importjob.Item, error) {
	if err := s.scopedImportJob(ctx, jobID, access.Viewer); err != nil {
		return nil, operationError("LoadImportJobItems", err)
	}

	if err := s.requireImportJob(ctx, jobID); err != nil {
		return nil, operationError("LoadImportJobItems", err)
	}

	if len(states) == 0 {
		return []importjob.Item{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(states)), ", ")

	arguments := []any{jobID}
	for _, state := range states {
		arguments = append(arguments, string(state))
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT `+importJobItemColumns+`, payload_json FROM import_job_items
		WHERE job_id = ? AND state IN (`+placeholders+`) ORDER BY position`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("load import job items: %w", err)
	}
	defer rows.Close()

	items := []importjob.Item{}

	for rows.Next() {
		item, err := scanImportJobItemWithPayload(rows)
		if err != nil {
			return nil, operationError("LoadImportJobItems", err)
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate import job items: %w", err)
	}

	return items, nil
}

type importJobScanner interface {
	Scan(destination ...any) error
}

func scanImportJob(row importJobScanner) (importjob.Job, error) {
	var (
		job                                importjob.Job
		state, policy                      string
		failure                            sql.NullString
		createdAt                          string
		startedAt, finishedAt, cancelledAt sql.NullString
		resolutionDecisions                string
	)
	if err := row.Scan(&job.ID, &job.Importer, &job.Source, &state, &policy, &job.TotalItems, &job.BatchSize,
		&failure, &createdAt, &startedAt, &finishedAt, &cancelledAt, &resolutionDecisions, &job.LibraryID, &job.Actor.UserID, &job.Actor.CredentialID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return importjob.Job{}, operationError("scanImportJob", err)
		}

		return importjob.Job{}, fmt.Errorf("scan import job: %w", err)
	}

	job.State = importjob.State(state)
	job.DuplicatePolicy = library.DuplicatePolicy(policy)

	job.Error = failure.String
	if err := json.Unmarshal([]byte(resolutionDecisions), &job.ResolutionDecisions); err != nil {
		return importjob.Job{}, fmt.Errorf("decode import resolution decisions: %w", err)
	}

	parsed, err := parseInstant(createdAt)
	if err != nil {
		return importjob.Job{}, operationError("scanImportJob", err)
	}

	job.CreatedAt = parsed
	if job.StartedAt, err = parseNullableInstant(startedAt); err != nil {
		return importjob.Job{}, operationError("scanImportJob", err)
	}

	if job.FinishedAt, err = parseNullableInstant(finishedAt); err != nil {
		return importjob.Job{}, operationError("scanImportJob", err)
	}

	if job.CancellationRequestedAt, err = parseNullableInstant(cancelledAt); err != nil {
		return importjob.Job{}, operationError("scanImportJob", err)
	}

	return job, nil
}

func scanImportJobItem(row importJobScanner) (importjob.Item, error) {
	return scanImportJobItemRow(row, nil)
}

func scanImportJobItemWithPayload(row importJobScanner) (importjob.Item, error) {
	var payload sql.NullString
	return scanImportJobItemRow(row, &payload)
}

func scanImportJobItemRow(row importJobScanner, payload *sql.NullString) (importjob.Item, error) {
	var (
		item                                importjob.Item
		state                               string
		review                              int
		outcome, entryID, recipeID, failure sql.NullString
		revision                            sql.NullInt64
		diagnostics                         string
		startedAt, finishedAt               sql.NullString
	)

	destination := []any{
		&item.JobID, &item.Position, &item.SourceItemID, &item.Title, &item.Filename, &item.SourceURL,
		&state, &review, &outcome, &entryID, &recipeID, &revision, &item.DroppedAssets, &item.DeferredAssets,
		&diagnostics, &failure, &startedAt, &finishedAt, &item.ImportEventID,
	}
	if payload != nil {
		destination = append(destination, payload)
	}

	if err := row.Scan(destination...); err != nil {
		return importjob.Item{}, fmt.Errorf("scan import job item: %w", err)
	}

	item.State = importjob.ItemState(state)
	item.ReviewRequired = review != 0
	item.Outcome = library.ImportOutcome(outcome.String)
	item.EntryID = library.EntryID(entryID.String)

	item.RecipeID = recipeID.String
	if revision.Valid && revision.Int64 > 0 {
		item.Revision = uint64(revision.Int64)
	}

	item.Error = failure.String
	if err := json.Unmarshal([]byte(diagnostics), &item.Diagnostics); err != nil {
		return importjob.Item{}, fmt.Errorf("read import job item diagnostics: %w", err)
	}

	var err error
	if item.StartedAt, err = parseNullableInstant(startedAt); err != nil {
		return importjob.Item{}, operationError("scanImportJobItemRow", err)
	}

	if item.FinishedAt, err = parseNullableInstant(finishedAt); err != nil {
		return importjob.Item{}, operationError("scanImportJobItemRow", err)
	}

	if payload != nil && payload.Valid {
		item.Payload = []byte(payload.String)
	}

	return item, nil
}

func encodeDiagnostics(diagnostics []importer.Diagnostic) (string, error) {
	if diagnostics == nil {
		diagnostics = []importer.Diagnostic{}
	}

	encoded, err := json.Marshal(diagnostics)
	if err != nil {
		return "", fmt.Errorf("encode import job item diagnostics: %w", err)
	}

	return string(encoded), nil
}

func requireImportJobRow(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("confirm import job write: %w", err)
	}

	if affected == 0 {
		return importjob.ErrNotFound
	}

	return nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}

	return value
}

func nullablePayload(payload []byte) any {
	if len(payload) == 0 {
		return nil
	}

	return string(payload)
}

func nullableRevision(revision uint64) any {
	if revision == 0 {
		return nil
	}

	return revision
}

func formatInstant(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func nullableInstant(value *time.Time) any {
	if value == nil {
		return nil
	}

	return formatInstant(*value)
}

func parseInstant(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("read import job timestamp %q: %w", value, err)
	}

	return parsed.UTC(), nil
}

func parseNullableInstant(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}

	parsed, err := parseInstant(value.String)
	if err != nil {
		return nil, operationError("parseNullableInstant", err)
	}

	return &parsed, nil
}

func (s *Store) authorizeJobOutcome(ctx context.Context, id string) error {
	if importjob.WorkerJobID(ctx) == id {
		var found bool
		if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM import_jobs WHERE id=? AND library_id=?)", id, scopeID(ctx)).Scan(&found); err != nil {
			return fmt.Errorf("check worker job: %w", err)
		}

		if !found {
			return importjob.ErrNotFound
		}

		return nil
	}

	return s.scopedImportJob(ctx, id, access.Editor)
}

func (s *Store) scopedImportJob(ctx context.Context, id string, role access.Role) error {
	if err := authorizeTokenCapability(ctx, s.db, "imports:write"); err != nil {
		return operationError("scopedImportJob", err)
	}

	err := s.scopedObject(ctx, "import_jobs", id, role)
	if errors.Is(err, library.ErrNotFound) {
		return importjob.ErrNotFound
	}

	return operationError("scopedImportJob", err)
}

func appendImportJobEvent(ctx context.Context, tx *sql.Tx, id, kind, state string) error {
	var libraryID, actorID string
	if err := tx.QueryRowContext(ctx, `SELECT library_id,actor_id FROM import_jobs WHERE id=?`, id).Scan(&libraryID, &actorID); err != nil {
		return operationError("appendImportJobEvent", err)
	}

	ctx = identity.WithPrincipal(ctx, identity.Principal{UserID: actorID})

	return appendDomainEvent(ctx, tx, kind, libraryID, "import_job", id, event.Payload{State: state})
}
