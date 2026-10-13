package library

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/recipe"
)

// ImportContext contains application history only; it is never recipe content.
// The allowlist deliberately excludes request headers, raw payloads and arbitrary
// importer metadata. ActorID is reserved for an authenticated actor, not an upload field.
type ImportContext struct {
	Importer        string             `json:"importer"`
	ImporterVersion string             `json:"importer_version"`
	JobID           string             `json:"job_id,omitempty"`
	ActorID         string             `json:"actor_id,omitempty"`
	InputKind       string             `json:"input_kind"`
	DisplayName     string             `json:"display_name"`
	SourceURL       string             `json:"source_url,omitempty"`
	SourceID        string             `json:"source_id,omitempty"`
	Diagnostics     []ImportDiagnostic `json:"diagnostics"`
}

// ImportDiagnostic records a classified import warning and its occurrence count.
type ImportDiagnostic struct {
	Code    string `json:"code"`
	Class   string `json:"class"`
	Area    string `json:"area,omitempty"`
	Message string `json:"message"`
	Count   int    `json:"count"`
}

// ImportEvent records an import outcome and the source snapshot used for later comparison.
type ImportEvent struct {
	ActorID   string `json:"actor_id,omitempty"`
	ActorName string `json:"actor_name,omitempty"`
	ID        string `json:"id"`
	ImportContext
	EntryID          EntryID          `json:"entry_id"`
	RecipeID         string           `json:"recipe_id"`
	Revision         uint64           `json:"revision"`
	Timestamp        time.Time        `json:"timestamp"`
	Operation        string           `json:"operation"`
	BaselineRevision uint64           `json:"baseline_revision"`
	ContentSHA256    string           `json:"content_sha256"`
	SourceSnapshot   *recipe.Document `json:"source_snapshot,omitempty"`
}

type (
	importContextKey struct{}
	importJobKey     struct{}
)

// WithImportContexts attaches per-recipe provenance to a batch operation's context.
func WithImportContexts(ctx context.Context, inputs []ImportContext) context.Context {
	return context.WithValue(ctx, importContextKey{}, inputs)
}

// WithImportJob attaches the originating background-job identity to import history.
func WithImportJob(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, importJobKey{}, id)
}

func importContextAt(ctx context.Context, index int) ImportContext {
	inputs, _ := ctx.Value(importContextKey{}).([]ImportContext)

	var input ImportContext
	if index < len(inputs) {
		input = inputs[index]
	}

	if id, ok := ctx.Value(importJobKey{}).(string); ok {
		input.JobID = id
	}

	return input
}

// ImportEventID derives the stable identifier for an import event.
func ImportEventID(id EntryID, revision uint64) string { return fmt.Sprintf("%s:%d", id, revision) }

// SafeImportURL is a display identity, never a fetch credential. Query and
// fragment are discarded wholesale, including unfamiliar signed URL schemes.
func SafeImportURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}

	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	// Path segments can themselves be signed or encode a private source key.
	// A hostname is enough to make local history recognisable without treating
	// a fetch URL as durable provenance.
	u.Path = ""
	u.RawPath = ""
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	return u.String()
}

var (
	historyURL          = regexp.MustCompile(`https?://[^\s<>"']+`)
	historySecret       = regexp.MustCompile(`(?i)\b(authorization|cookie|token|secret|password|api[-_]?key)\s*[:=]\s*(?:bearer\s+)?[^\s,;]+`)
	historyHeaderSecret = regexp.MustCompile(`(?im)\b(authorization|cookie)\s*[:=]\s*[^\r\n]*`)
)

func safeHistoryText(value string) string {
	value = historyURL.ReplaceAllStringFunc(value, SafeImportURL)
	value = historyHeaderSecret.ReplaceAllString(value, "$1=[redacted]")
	value = historySecret.ReplaceAllString(value, "$1=[redacted]")

	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}

		return r
	}, value)
}

// SanitizeImportContext removes sensitive values from import context metadata.
func SanitizeImportContext(input ImportContext) ImportContext {
	input.SourceURL = SafeImportURL(input.SourceURL)
	input.Importer = safeHistoryText(input.Importer)
	input.ImporterVersion = safeHistoryText(input.ImporterVersion)
	input.SourceID = safeHistoryText(input.SourceID)
	// A URL-shaped upload name must lose its query before its final path segment
	// is selected, otherwise a signed filename becomes durable history.
	if displayURL, err := url.Parse(strings.TrimSpace(input.DisplayName)); err == nil && displayURL.Hostname() != "" && (displayURL.Scheme == "https" || displayURL.Scheme == "http") {
		input.DisplayName = displayURL.Hostname()
	}

	input.DisplayName = path.Base(strings.ReplaceAll(input.DisplayName, `\`, "/"))

	input.DisplayName = safeHistoryText(input.DisplayName)
	if input.DisplayName == "." || input.DisplayName == "/" {
		input.DisplayName = "Imported recipe"
	}

	if input.Importer == "" {
		input.Importer = "savorum-normalized"
	}

	if input.ImporterVersion == "" {
		input.ImporterVersion = "1"
	}

	if input.InputKind == "" {
		input.InputKind = "document"
	}

	input.Diagnostics = append([]ImportDiagnostic{}, input.Diagnostics...)
	for i := range input.Diagnostics {
		d := &input.Diagnostics[i]
		d.Message = safeHistoryText(d.Message)
		d.Code = safeHistoryText(d.Code)
		d.Area = safeHistoryText(d.Area)
	}

	return input
}

// ImportHistoryRepository reads the paged provenance log for a library entry.
type ImportHistoryRepository interface {
	ListImportEvents(context.Context, EntryID, RevisionListOptions) ([]ImportEvent, error)
}

// SourceImportBaseline holds the last imported document for comparison with upstream changes.
type SourceImportBaseline struct {
	EntryID  EntryID
	Revision uint64
	Document recipe.Document
}

// SourceImportBaselineRepository reads the latest source snapshot independently
// of history pagination, so refresh does not depend on an arbitrary log limit.
type SourceImportBaselineRepository interface {
	GetSourceImportBaseline(context.Context, EntryID) (SourceImportBaseline, error)
}

// ListImportEvents returns a bounded provenance log for an active or trashed entry.
func (s *Service) ListImportEvents(ctx context.Context, id EntryID, options RevisionListOptions) ([]ImportEvent, error) {
	if _, err := s.repository.Get(ctx, id, StateActive); err != nil {
		if _, trashErr := s.repository.Get(ctx, id, StateTrash); trashErr != nil {
			return nil, err
		}
	}

	if options.Limit <= 0 {
		options.Limit = 50
	}

	if options.Limit > 100 {
		options.Limit = 100
	}

	if options.Offset < 0 {
		options.Offset = 0
	}

	repository, ok := s.repository.(ImportHistoryRepository)
	if !ok {
		return nil, fmt.Errorf("store does not support import history")
	}

	return repository.ListImportEvents(ctx, id, options)
}
