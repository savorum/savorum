// Package assets stores the images a recipe carries. Blobs are content
// addressed by SHA-256, so the same photograph arriving twice is stored once
// and a reference to it can never silently mean different bytes later.
package assets

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

// Asset errors distinguish validation, integrity, capacity, and lookup failures.
var (
	// ErrNotFound reports an asset identity absent from storage.
	ErrNotFound   = errors.New("asset not found")
	ErrInvalid    = errors.New("asset is invalid")
	ErrDigest     = errors.New("asset digest does not match the bytes")
	ErrSize       = errors.New("asset size does not match the bytes")
	ErrMediaType  = errors.New("asset media type does not match the bytes")
	ErrTooLarge   = errors.New("asset exceeds the configured size limit")
	ErrStillInUse = errors.New("asset is still in use")
	ErrIntegrity  = errors.New("stored asset integrity failure")
	ErrCapacity   = errors.New("asset storage capacity is exhausted")
	ErrBusy       = errors.New("asset upload admission is busy")
)

// IntegrityIssueKind categorizes a detected asset-integrity issue.
type IntegrityIssueKind string

// Integrity issue kinds distinguish missing metadata, missing bytes, and mismatches.
const (
	// IntegrityMetadataOnly means metadata exists without stored bytes.
	IntegrityMetadataOnly   IntegrityIssueKind = "metadata-only"
	IntegrityUnregistered   IntegrityIssueKind = "unregistered"
	IntegrityDigestMismatch IntegrityIssueKind = "digest-mismatch"
	IntegritySizeMismatch   IntegrityIssueKind = "size-mismatch"
)

// ID uniquely identifies a stored asset.
type ID string

// InlineSafe reports whether browsers may render a stored asset in the
// application's origin. Text formats and SVG remain downloadable, but never
// execute in the trusted origin.
func InlineSafe(mediaType string) bool {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image/avif", "image/gif", "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

// Blob describes a stored asset blob.
type Blob struct {
	ID          ID
	ContentHash string
	PHash       *uint64
	MediaType   string
	Size        int64
	StorageKey  string
	CreatedAt   time.Time
	OrphanedAt  *time.Time
}

// IngestOptions controls how an asset is ingested.
type IngestOptions struct {
	SHA256    string
	Size      *int64
	MediaType string
}

// StoredObject is one content-addressed file found on disk. Its identity is
// derived from its path; its bytes are verified separately.
type StoredObject struct {
	ID   ID
	Size int64
}

// IntegrityIssue describes one detected asset-integrity problem.
type IntegrityIssue struct {
	Kind IntegrityIssueKind
	ID   ID
}

// IntegrityReport summarizes an asset-integrity check.
type IntegrityReport struct {
	Checked int
	Issues  []IntegrityIssue
}

// BlobStore persists asset blobs.
type BlobStore interface {
	Put(ctx context.Context, source io.Reader, options IngestOptions) (Blob, error)
	Stat(ctx context.Context, id ID) (StoredObject, error)
	Open(ctx context.Context, id ID, expectedSize int64) (io.ReadCloser, error)
	Delete(ctx context.Context, id ID) error
	Inventory(ctx context.Context) ([]StoredObject, error)
	Quarantine(ctx context.Context, id ID) error
	ListQuarantined(ctx context.Context) ([]ID, error)
	RestoreQuarantined(ctx context.Context, id ID) error
	PurgeQuarantined(ctx context.Context, id ID) error
}

// Repository provides the durable asset repository.
type Repository interface {
	AuthorizeAssetIngest(ctx context.Context) error
	RegisterStoredAsset(ctx context.Context, blob Blob, orphanedAt time.Time) (Blob, error)
	GetStoredAsset(ctx context.Context, id ID) (Blob, error)
	ListStoredAssets(ctx context.Context) ([]Blob, error)
	ListOrphanedAssets(ctx context.Context, cutoff time.Time, limit int) ([]Blob, error)
	DeleteOrphanedAsset(ctx context.Context, id ID, cutoff time.Time, deleteBlob func(Blob) error) (bool, error)
}
