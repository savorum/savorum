// Package importjob provides durable execution for bulk recipe imports.
//
// Jobs run in-process over SQLite without external worker queues or leases.
// A single process manages job execution, progress reporting, and recovery
// across restarts.
package importjob

import (
	"context"
	"errors"
	"time"

	"github.com/savorum/savorum/internal/identity"

	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
)

var (
	// ErrCapacity reports a full in-process import queue.
	ErrCapacity = errors.New("import queue capacity exhausted")
	// ErrTooLarge reports a selection larger than the retained-work allowance.
	ErrTooLarge = errors.New("import selection exceeds the retained-work allowance")
	// ErrNotFound reports an import job identity absent from the repository.
	ErrNotFound = errors.New("import job not found")
	// ErrJobState is the answer to asking a job for something its current
	// state cannot give: cancelling a finished job, resuming a running one.
	ErrJobState = errors.New("import job is not in a state that allows this")
	// ErrNotResumable means every unfinished item lost the input it needed.
	// The recipes are not gone; the uploaded file has to be chosen again.
	ErrNotResumable = errors.New("no unfinished item in this import can be resumed")
)

// State is the job's own lifecycle. `interrupted` marks a job whose process
// stopped mid-import; it is not `failed`, because nothing was decided.
type State string

// Job states distinguish queued, active, completed, cancelled, and interrupted work.
const (
	// StatePending means a durable job is waiting for a worker slot.
	StatePending             State = "pending"
	StateRunning             State = "running"
	StateCompleted           State = "completed"
	StateCompletedWithErrors State = "completed-with-errors"
	StateCancelled           State = "cancelled"
	StateFailed              State = "failed"
	StateInterrupted         State = "interrupted"
)

// Terminal reports whether a job has stopped for good. An interrupted job has
// stopped but is not finished: it is the one terminal state that can be
// resumed.
func (state State) Terminal() bool {
	switch state {
	case StatePending, StateRunning:
		return false
	case StateCompleted, StateCompletedWithErrors, StateCancelled, StateFailed, StateInterrupted:
		return true
	default:
		return true
	}
}

func (state State) valid() bool {
	switch state {
	case StatePending, StateRunning, StateCompleted, StateCompletedWithErrors,
		StateCancelled, StateFailed, StateInterrupted:
		return true
	default:
		return false
	}
}

// ItemState is what became of one recipe. Whether it still needs a look is
// Item.ReviewRequired.
type ItemState string

// Item states record the durable outcome of each recipe in a job.
const (
	// ItemPending means an import item has not started processing.
	ItemPending   ItemState = "pending"
	ItemRunning   ItemState = "running"
	ItemImported  ItemState = "imported"
	ItemSkipped   ItemState = "skipped"
	ItemFailed    ItemState = "failed"
	ItemCancelled ItemState = "cancelled"
)

// Job is a bulk import whose progress is derived from its items.
type Job struct {
	Actor           identity.Principal
	LibraryID       string
	ID              string
	Importer        string
	Source          string
	State           State
	DuplicatePolicy library.DuplicatePolicy
	TotalItems      int
	// BatchSize is persisted because a later process may be configured
	// differently from the one that ran this job.
	BatchSize               int
	Error                   string
	LeadTitle               string
	CreatedAt               time.Time
	StartedAt               *time.Time
	FinishedAt              *time.Time
	CancellationRequestedAt *time.Time
	Progress                Progress
	ResolutionDecisions     []exchange.MeasurementResolutionRecord
}

// Progress includes AlreadyInLibrary in Failed. Review and diagnostic counts
// overlap the item-state counts and must not be added to Total.
type Progress struct {
	Total            int
	Pending          int
	Running          int
	Imported         int
	Skipped          int
	AlreadyInLibrary int
	Failed           int
	Cancelled        int
	ReviewRequired   int
	// Resumable excludes recognized duplicates even when their payloads remain.
	Resumable                 int
	InformationalPreservation int
	WithoutDiagnostics        int
	DiagnosticOccurrences     int
	Diagnostics               []DiagnosticSummary
}

// DiagnosticSummary counts occurrences separately from recipes; one recipe
// can contribute multiple occurrences of the same diagnostic.
type DiagnosticSummary struct {
	Code        string
	Class       importer.DiagnosticClass
	Area        string
	Message     string
	Occurrences int
	Recipes     int
}

// Finished returns the number of items in a terminal state.
func (progress Progress) Finished() int {
	return progress.Imported + progress.Skipped + progress.Failed + progress.Cancelled
}

// Item records one recipe's outcome. Only recovery reads include Payload.
type Item struct {
	ImportEventID  string
	JobID          string
	Position       int
	SourceItemID   string
	Title          string
	Filename       string
	SourceURL      string
	State          ItemState
	ReviewRequired bool
	Outcome        library.ImportOutcome
	EntryID        library.EntryID
	RecipeID       string
	Revision       uint64
	DroppedAssets  int
	DeferredAssets int
	Diagnostics    []importer.Diagnostic
	Error          string
	Payload        []byte
	StartedAt      *time.Time
	FinishedAt     *time.Time
}

// Resumable reports whether this item still carries what a later attempt would
// need. Bytes that existed only inside an uploaded archive are not kept.
func (item Item) Resumable() bool { return len(item.Payload) > 0 }

// ListOptions pages the job list newest first.
type ListOptions struct {
	Limit  int
	Offset int
	State  State
}

// ItemFilter is the reader's vocabulary rather than the storage vocabulary.
// "Review required" is not an ItemState, so it cannot be expressed as one.
type ItemFilter string

// Item filters select job rows by outcome or review significance.
const (
	// ItemsAll selects every item regardless of its outcome.
	ItemsAll            ItemFilter = ""
	ItemsImported       ItemFilter = "imported"
	ItemsSkipped        ItemFilter = "skipped"
	ItemsReviewRequired ItemFilter = "review-required"
	ItemsInformational  ItemFilter = "informational"
	ItemsFailed         ItemFilter = "failed"
	ItemsPending        ItemFilter = "pending"
	ItemsCancelled      ItemFilter = "cancelled"
)

func (filter ItemFilter) valid() bool {
	switch filter {
	case ItemsAll, ItemsImported, ItemsSkipped, ItemsReviewRequired, ItemsInformational, ItemsFailed, ItemsPending, ItemsCancelled:
		return true
	default:
		return false
	}
}

// ItemListOptions pages and filters the items in one job.
type ItemListOptions struct {
	Limit  int
	Offset int
	Filter ItemFilter
}

// JobStateUpdate records one lifecycle transition. The timestamps are separate
// pointers because a transition sets some of them and must leave the rest
// exactly as they were.
type JobStateUpdate struct {
	State      State
	Error      string
	StartedAt  *time.Time
	FinishedAt *time.Time
}

type workerKey struct{}

// WorkerJobID identifies the single job whose outcome the internal worker may record.
// This capability never authorizes content mutations or unrelated jobs.
func WorkerJobID(ctx context.Context) string { id, _ := ctx.Value(workerKey{}).(string); return id }
