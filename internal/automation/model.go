// Package automation coordinates typed bulk and transfer work over the existing
// library, exchange, import-job, and export services. It is not an operation RPC
// or an adapter execution framework.
package automation

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/importjob"
	"github.com/savorum/savorum/internal/library"
)

var (
	// ErrNotFound includes expired jobs; callers must not disclose another creator’s work.
	ErrNotFound = errors.New("job not found or expired")
	// ErrState requires an explicit lifecycle transition.
	ErrState = errors.New("job is not in the required state")
	// ErrInvalid rejects unsupported input before creating work.
	ErrInvalid = errors.New("invalid automation request")
	// ErrCapacity bounds retained jobs and private artifacts.
	ErrCapacity = errors.New("automation queue is full")
)

// Target carries the resource's own precondition, not a batch-wide revision.
type Target struct {
	ID       string `json:"id" minLength:"1" maxLength:"255"`
	Revision uint64 `json:"revision"`
}

// BulkRequest is internal orchestration input; HTTP exposes distinct operations.
type BulkRequest struct {
	Action       string   `json:"action"`
	Items        []Target `json:"items" minItems:"1" maxItems:"10000"`
	AddTags      []string `json:"add_tags,omitempty" maxItems:"100"`
	RemoveTags   []string `json:"remove_tags,omitempty" maxItems:"100"`
	BookID       string   `json:"book_id,omitempty"`
	BookRevision uint64   `json:"book_revision,omitempty"`
	Member       bool     `json:"member,omitempty"`
}

// Result describes one independent item. Recipe snapshots are stored separately
// from progress checkpoints and mapped explicitly at the API boundary.
type Result struct {
	ID              string         `json:"id"`
	Status          string         `json:"status"`
	Code            string         `json:"code,omitempty"`
	Revision        uint64         `json:"revision,omitempty"`
	BookRevision    uint64         `json:"book_revision,omitempty"`
	CurrentRevision uint64         `json:"current_revision,omitempty"`
	Entry           *library.Entry `json:"-"`
}

// ImportInput admits remote URLs, held file bytes, or supported text, never a
// server filesystem path. Bytes are private job input, never a response field.
type ImportInput struct {
	Kind        string `json:"kind"`
	URL         string `json:"url,omitempty"`
	Filename    string `json:"filename,omitempty"`
	Data        []byte `json:"data,omitempty"`
	Policy      string `json:"policy"`
	OnDuplicate string `json:"on_duplicate"`
}

// ExportInput selects a bounded snapshot and a registered interchange format.
type ExportInput struct {
	Format  string   `json:"format" enum:"sref,sref-bundle"`
	Recipes []string `json:"recipes,omitempty" maxItems:"10000"`
	Library bool     `json:"library,omitempty"`
}

// Job is one durable lifecycle. Artifacts expire independently of normal data.
type Job struct {
	ID             string    `json:"id"`
	Revision       uint64    `json:"revision"`
	Kind           string    `json:"kind"`
	State          string    `json:"state"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	Finished       int       `json:"finished"`
	Total          int       `json:"total"`
	Code           string    `json:"code,omitempty"`
	ImportJobID    string    `json:"import_job_id,omitempty"`
	DownloadHandle string    `json:"download_handle,omitempty"`
	Filename       string    `json:"filename,omitempty"`
	MediaType      string    `json:"media_type,omitempty"`
}

// Record is private persistence, including recoverable plans and input.
type Record struct {
	Job        Job                     `json:"job"`
	LibraryID  string                  `json:"library_id"`
	Actor      identity.Principal      `json:"actor"`
	Input      json.RawMessage         `json:"input"`
	InFlightID string                  `json:"in_flight_id"`
	Results    []Result                `json:"results"`
	Preview    *exchange.ImportPreview `json:"preview"`
	Payloads   [][]byte                `json:"payloads"`
}

// Repository owns authorization, atomic creation/receipt, and bounded retention.
type Repository interface {
	CreateAutomationJob(context.Context, Record) error
	AutomationJob(context.Context, string) (Record, error)
	AutomationResults(context.Context, string, int, int) ([]Result, error)
	SaveAutomationJob(context.Context, *Record) error
	PendingAutomationJobs(context.Context) ([]Record, error)
	RecoverAutomationJobs(context.Context) error
	FailAutomationJob(context.Context, string, uint64, string) error
	ExpireAutomationJobs(context.Context) ([]string, error)
}

// Library is the existing resource service boundary used for each bulk item.
type Library interface {
	GetBook(context.Context, string) (library.Book, error)
	Get(context.Context, library.EntryID) (library.Entry, error)
	Trash(context.Context, library.EntryID, uint64) (library.Entry, error)
	Restore(context.Context, library.EntryID, uint64) (library.Entry, error)
	GetOrganization(context.Context, string) (library.Organization, error)
	ReplaceOrganization(context.Context, string, uint64, library.Organization, []library.BookMembershipChange) (library.Organization, error)
	ListReferences(context.Context, library.State) ([]library.RecipeReference, error)
}

// Exchange shares importer selection, preview diagnostics, and reviewed plans with interactive imports.
type Exchange interface {
	PreviewURL(context.Context, string) (exchange.ImportPreview, error)
	PreviewFiles(context.Context, []exchange.UploadedImport) (exchange.ImportPreview, error)
	PlanPreviewImport(context.Context, string, []exchange.PreviewResolution, bool) (exchange.ImportPlan, error)
	CancelPreviewScoped(context.Context, string) error
}

// ImportJobs owns resumable commits after the shared import plan has been accepted.
type ImportJobs interface {
	Start(context.Context, importjob.StartRequest) (importjob.Job, error)
	Get(context.Context, string) (importjob.Job, error)
	Cancel(context.Context, string) (importjob.Job, error)
	Resume(context.Context, string) (importjob.Job, error)
}

// RevisionConflict preserves the observed job revision for explicit recovery.
type RevisionConflict struct{ Expected, Current uint64 }

func (*RevisionConflict) Error() string { return "job revision changed" }
