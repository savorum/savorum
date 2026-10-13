package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/savorum/savorum/internal/imaging"
	"github.com/savorum/savorum/internal/storagebudget"
)

// DefaultGCBatchSize is the default number of candidates considered per cleanup batch.
const DefaultGCBatchSize = 200

// Clock returns the current time for asset operations.
type Clock func() time.Time

// Service coordinates asset ingestion, lookup, and cleanup.
type Service struct {
	budget        *storagebudget.Coordinator
	repository    Repository
	store         BlobStore
	now           Clock
	policy        CapacityPolicy
	slots         chan struct{}
	admissionMu   sync.Mutex
	stagingBytes  int64
	windowStart   time.Time
	windowUploads int
	rejected      uint64
}

// CapacityPolicy protects the shared asset filesystem and the database that
// refers to it. Every new standalone blob begins as an orphan.
type CapacityPolicy struct {
	TotalBytes         int64
	OrphanBytes        int64
	Objects            int
	StagingBytes       int64
	MaximumUploadBytes int64
	MinimumFreeBytes   uint64
	ConcurrentUploads  int
	UploadsPerMinute   int
}

// DefaultCapacityPolicy returns the standard storage and ingestion limits.
func DefaultCapacityPolicy() CapacityPolicy {
	return CapacityPolicy{
		TotalBytes: 10 << 30, OrphanBytes: 2 << 30, Objects: 50_000,
		StagingBytes: 128 << 20, MaximumUploadBytes: 64 << 20, MinimumFreeBytes: 512 << 20,
		ConcurrentUploads: 2, UploadsPerMinute: 120,
	}
}

// Usage reports asset storage consumption.
type Usage struct {
	TotalBytes    int64
	OrphanBytes   int64
	Objects       int
	StagingBytes  int64
	ActiveUploads int
	Rejected      uint64
}

// Option configures an asset service.
type Option func(*Service)

// WithClock configures the clock used for asset lifecycle timestamps.
func WithClock(clock Clock) Option {
	return func(service *Service) { service.now = clock }
}

// WithCapacityPolicy configures storage and ingestion limits.
func WithCapacityPolicy(policy CapacityPolicy) Option {
	return func(service *Service) { service.policy = policy }
}

// WithDurableBudget holds the database writer fence through original registration.
func WithDurableBudget(budget *storagebudget.Coordinator) Option {
	return func(service *Service) { service.budget = budget }
}

// NewService creates an asset service over durable metadata and blob storage.
func NewService(repository Repository, store BlobStore, options ...Option) (*Service, error) {
	if repository == nil {
		return nil, errors.New("asset repository is required")
	}

	if store == nil {
		return nil, errors.New("asset blob store is required")
	}

	service := &Service{repository: repository, store: store, now: time.Now, policy: DefaultCapacityPolicy()}
	for _, option := range options {
		option(service)
	}

	if service.now == nil {
		return nil, errors.New("asset clock is required")
	}

	defaults := DefaultCapacityPolicy()
	if service.policy.TotalBytes <= 0 {
		service.policy.TotalBytes = defaults.TotalBytes
	}

	if service.policy.OrphanBytes <= 0 {
		service.policy.OrphanBytes = defaults.OrphanBytes
	}

	if service.policy.Objects <= 0 {
		service.policy.Objects = defaults.Objects
	}

	if service.policy.StagingBytes <= 0 {
		service.policy.StagingBytes = defaults.StagingBytes
	}

	if service.policy.MaximumUploadBytes <= 0 {
		service.policy.MaximumUploadBytes = defaults.MaximumUploadBytes
	}

	if service.policy.ConcurrentUploads <= 0 {
		service.policy.ConcurrentUploads = defaults.ConcurrentUploads
	}

	if service.policy.UploadsPerMinute <= 0 {
		service.policy.UploadsPerMinute = defaults.UploadsPerMinute
	}

	service.slots = make(chan struct{}, service.policy.ConcurrentUploads)

	return service, nil
}

// Ingest verifies, stores, and registers one asset stream.
func (s *Service) Ingest(ctx context.Context, source io.Reader, options IngestOptions) (Blob, error) {
	if err := s.repository.AuthorizeAssetIngest(ctx); err != nil {
		return Blob{}, err
	}

	if source == nil {
		return Blob{}, fmt.Errorf("asset bytes are required: %w", ErrInvalid)
	}

	if strings.TrimSpace(options.MediaType) == "" {
		return Blob{}, fmt.Errorf("asset media type is required: %w", ErrInvalid)
	}

	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		s.recordRejection()
		return Blob{}, ErrBusy
	}

	reservation := s.policy.MaximumUploadBytes
	if options.Size != nil {
		if *options.Size < 0 || *options.Size > s.policy.MaximumUploadBytes {
			return Blob{}, ErrTooLarge
		}

		reservation = *options.Size
	}

	if err := s.reserveStaging(reservation); err != nil {
		return Blob{}, err
	}
	defer s.releaseStaging(reservation)

	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: source}, s.policy.MaximumUploadBytes+1))
	if err != nil {
		return Blob{}, fmt.Errorf("stage asset upload: %w", err)
	}

	if int64(len(data)) > s.policy.MaximumUploadBytes {
		return Blob{}, ErrTooLarge
	}

	if options.Size != nil && int64(len(data)) != *options.Size {
		return Blob{}, ErrSize
	}

	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])

	ctx, durableRelease, err := s.budget.Acquire(ctx)
	if err != nil {
		return Blob{}, err
	}
	defer durableRelease()

	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()

	err = s.admitDurable(ctx, ID("sha256:"+digest), int64(len(data)))
	if err != nil {
		return Blob{}, err
	}

	blob, err := s.store.Put(ctx, bytes.NewReader(data), options)
	if err != nil {
		return Blob{}, fmt.Errorf("store verified asset: %w", err)
	}

	if existing, reused, err := s.reuseStoredAsset(ctx, blob); err != nil {
		return Blob{}, err
	} else if reused {
		return existing, nil
	}

	now := s.now().UTC()
	blob.CreatedAt = now

	if imaging.Decodable(options.MediaType) {
		if phash, _, _, err := ComputeBytesPHash(data); err == nil {
			blob.PHash = &phash
		}
	}

	registered, err := s.repository.RegisterStoredAsset(ctx, blob, now)
	if err != nil {
		// The safe failure direction is an unreferenced content-addressed file.
		// A later successful ingestion of the same digest reuses it.
		return Blob{}, fmt.Errorf("register stored asset: %w", err)
	}

	return registered, nil
}

func (s *Service) reuseStoredAsset(ctx context.Context, blob Blob) (Blob, bool, error) {
	if s.budget == nil {
		return Blob{}, false, nil
	}

	repository, ok := s.repository.(interface {
		ReuseStoredAsset(context.Context, ID) (Blob, error)
	})
	if !ok {
		return Blob{}, false, nil
	}

	existing, err := repository.ReuseStoredAsset(ctx, blob.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Blob{}, false, err
	}

	matches := err == nil && existing.StorageKey == blob.StorageKey && existing.ContentHash == blob.ContentHash && existing.Size == blob.Size && existing.MediaType == blob.MediaType

	return existing, matches, nil
}

// contextReader belongs to one Ingest call and lets io.Copy observe its
// cancellation even though io.Reader.Read cannot accept a context.
//
//nolint:containedctx // the context is request-scoped adapter state, never retained by Service
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	return r.reader.Read(p) //nolint:wrapcheck // transparent Reader adapters must preserve io.EOF exactly
}

func (s *Service) reserveStaging(bytes int64) error {
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()

	now := s.now().UTC()
	if s.windowStart.IsZero() || now.Sub(s.windowStart) >= time.Minute {
		s.windowStart, s.windowUploads = now, 0
	}

	if s.windowUploads >= s.policy.UploadsPerMinute {
		s.rejected++
		return ErrBusy
	}

	if bytes > s.policy.StagingBytes-s.stagingBytes {
		s.rejected++
		return ErrCapacity
	}

	s.windowUploads++
	s.stagingBytes += bytes

	return nil
}

func (s *Service) releaseStaging(bytes int64) {
	s.admissionMu.Lock()
	s.stagingBytes -= bytes
	s.admissionMu.Unlock()
}
func (s *Service) recordRejection() { s.admissionMu.Lock(); s.rejected++; s.admissionMu.Unlock() }

func (s *Service) admitDurable(ctx context.Context, id ID, size int64) error {
	usage, stored, err := s.currentUsage(ctx)
	if err != nil {
		return err
	}

	if _, exists := stored[id]; exists {
		return nil
	}

	if s.budget == nil && (usage.Objects >= s.policy.Objects || size > s.policy.TotalBytes-usage.TotalBytes || size > s.policy.OrphanBytes-usage.OrphanBytes) {
		s.rejected++
		return ErrCapacity
	}

	if reporter, ok := s.store.(interface {
		AvailableBytes(context.Context) (uint64, error)
	}); ok {
		available, err := reporter.AvailableBytes(ctx)
		if err != nil {
			return fmt.Errorf("read asset filesystem capacity: %w", err)
		}

		needed := uint64(max(size, 0)) + s.policy.MinimumFreeBytes
		if available < needed {
			s.rejected++
			return ErrCapacity
		}
	}

	return nil
}

func (s *Service) currentUsage(ctx context.Context) (Usage, map[ID]StoredObject, error) {
	objects, err := s.store.Inventory(ctx)
	if err != nil {
		return Usage{}, nil, fmt.Errorf("inventory asset capacity: %w", err)
	}

	registered, err := s.repository.ListStoredAssets(ctx)
	if err != nil {
		return Usage{}, nil, fmt.Errorf("list asset capacity: %w", err)
	}

	registeredByID := make(map[ID]Blob, len(registered))
	for _, blob := range registered {
		registeredByID[blob.ID] = blob
	}

	stored := make(map[ID]StoredObject, len(objects))

	usage := Usage{Objects: len(objects)}
	for _, object := range objects {
		if object.Size < 0 || usage.TotalBytes > math.MaxInt64-object.Size {
			return Usage{}, nil, fmt.Errorf("asset inventory size exceeds accounting range: %w", ErrCapacity)
		}

		stored[object.ID] = object
		usage.TotalBytes += object.Size

		blob, known := registeredByID[object.ID]
		if !known || blob.OrphanedAt != nil {
			if usage.OrphanBytes > math.MaxInt64-object.Size {
				return Usage{}, nil, fmt.Errorf("orphan inventory size exceeds accounting range: %w", ErrCapacity)
			}

			usage.OrphanBytes += object.Size
		}
	}

	return usage, stored, nil
}

// Usage reports current durable and in-flight asset consumption.
func (s *Service) Usage(ctx context.Context) (Usage, error) {
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()

	usage, _, err := s.currentUsage(ctx)
	if err != nil {
		return Usage{}, err
	}

	usage.StagingBytes = s.stagingBytes
	usage.ActiveUploads = len(s.slots)
	usage.Rejected = s.rejected

	return usage, nil
}

// Open verifies an asset's metadata and returns its body.
func (s *Service) Open(ctx context.Context, id ID) (Blob, io.ReadCloser, error) {
	blob, err := s.Metadata(ctx, id)
	if err != nil {
		return Blob{}, nil, err
	}

	reader, err := s.store.Open(ctx, id, blob.Size)
	if err != nil {
		if errors.Is(err, ErrDigest) || errors.Is(err, ErrSize) || errors.Is(err, ErrInvalid) {
			return Blob{}, nil, fmt.Errorf("%w: %w", ErrIntegrity, err)
		}

		return Blob{}, nil, fmt.Errorf("open stored asset: %w", err)
	}

	return blob, reader, nil
}

// Metadata returns the verified database identity and representation metadata
// without opening, hashing, or reading the stored body. It is used for HEAD;
// GET still calls Open and verifies every byte before serving it.
func (s *Service) Metadata(ctx context.Context, id ID) (Blob, error) {
	blob, err := s.repository.GetStoredAsset(ctx, id)
	if err != nil {
		return Blob{}, fmt.Errorf("get stored asset: %w", err)
	}

	if blob.ID != id || blob.ContentHash == "" || id != ID("sha256:"+blob.ContentHash) {
		return Blob{}, fmt.Errorf("stored asset identity is inconsistent: %w", ErrInvalid)
	}

	stored, err := s.store.Stat(ctx, id)
	if err != nil {
		return Blob{}, fmt.Errorf("inspect stored asset: %w", err)
	}

	if stored.ID != id || stored.Size != blob.Size {
		return Blob{}, fmt.Errorf("stored asset metadata differs from its body: %w", ErrIntegrity)
	}

	return blob, nil
}

// Scrub compares registered metadata with the content-addressed store and
// verifies every registered file before reporting divergence. It does not
// silently repair integrity failures; operators can act on the complete
// report without any suspect bytes having been served.
func (s *Service) Scrub(ctx context.Context) (IntegrityReport, error) {
	if err := s.recoverQuarantine(ctx); err != nil {
		return IntegrityReport{}, err
	}

	registered, err := s.repository.ListStoredAssets(ctx)
	if err != nil {
		return IntegrityReport{}, fmt.Errorf("list registered assets: %w", err)
	}

	stored, err := s.store.Inventory(ctx)
	if err != nil {
		return IntegrityReport{}, fmt.Errorf("inventory stored assets: %w", err)
	}

	byID := make(map[ID]StoredObject, len(stored))
	for _, object := range stored {
		byID[object.ID] = object
	}

	registeredIDs := make(map[ID]struct{}, len(registered))

	report := IntegrityReport{Checked: len(registered)}
	for _, blob := range registered {
		registeredIDs[blob.ID] = struct{}{}

		object, present := byID[blob.ID]
		if !present {
			report.Issues = append(report.Issues, IntegrityIssue{Kind: IntegrityMetadataOnly, ID: blob.ID})
			continue
		}

		if object.Size != blob.Size {
			report.Issues = append(report.Issues, IntegrityIssue{Kind: IntegritySizeMismatch, ID: blob.ID})
			continue
		}

		reader, openErr := s.store.Open(ctx, blob.ID, blob.Size)
		if openErr != nil {
			kind := IntegrityDigestMismatch
			if errors.Is(openErr, ErrNotFound) {
				kind = IntegrityMetadataOnly
			}

			report.Issues = append(report.Issues, IntegrityIssue{Kind: kind, ID: blob.ID})

			continue
		}

		_ = reader.Close()
	}

	for _, object := range stored {
		if _, present := registeredIDs[object.ID]; !present {
			report.Issues = append(report.Issues, IntegrityIssue{Kind: IntegrityUnregistered, ID: object.ID})
		}
	}

	return report, nil
}

// CollectGarbage permanently removes blobs that have remained unreferenced
// through the grace period. Callers should run this as a maintenance operation.
func (s *Service) CollectGarbage(ctx context.Context, grace time.Duration) (int, error) {
	if grace < 0 {
		return 0, errors.New("asset garbage-collection grace period cannot be negative")
	}

	if err := s.recoverQuarantine(ctx); err != nil {
		return 0, err
	}

	cutoff := s.now().UTC().Add(-grace)
	removed := 0

	for {
		orphans, err := s.repository.ListOrphanedAssets(ctx, cutoff, DefaultGCBatchSize)
		if err != nil {
			return removed, fmt.Errorf("list orphaned assets: %w", err)
		}

		if len(orphans) == 0 {
			return removed, nil
		}

		for _, orphan := range orphans {
			deleted, err := s.collectOrphan(ctx, orphan, cutoff)
			if deleted {
				removed++
			}

			if err != nil {
				return removed, err
			}
		}

		if len(orphans) < DefaultGCBatchSize {
			return removed, nil
		}
	}
}

func (s *Service) collectOrphan(ctx context.Context, orphan Blob, cutoff time.Time) (bool, error) {
	quarantined := false

	deleted, err := s.repository.DeleteOrphanedAsset(ctx, orphan.ID, cutoff, func(current Blob) error {
		if current.StorageKey == "" {
			return nil
		}

		if err := s.store.Quarantine(ctx, current.ID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil
			}

			return fmt.Errorf("quarantine orphaned blob %q: %w", current.ID, err)
		}

		quarantined = true

		return nil
	})
	if err != nil {
		if quarantined {
			if restoreErr := s.store.RestoreQuarantined(context.WithoutCancel(ctx), orphan.ID); restoreErr != nil {
				return false, fmt.Errorf("collect orphaned asset %q: %w; restore quarantined bytes: %w", orphan.ID, err, restoreErr)
			}
		}

		return false, fmt.Errorf("collect orphaned asset %q: %w", orphan.ID, err)
	}

	if deleted && quarantined {
		if err := s.store.PurgeQuarantined(context.WithoutCancel(ctx), orphan.ID); err != nil && !errors.Is(err, ErrNotFound) {
			return true, fmt.Errorf("purge quarantined asset %q: %w", orphan.ID, err)
		}
	}

	return deleted, nil
}

// recoverQuarantine resolves work interrupted on either side of the metadata
// commit. Registered assets are restored; committed deletions are purged.
func (s *Service) recoverQuarantine(ctx context.Context) error {
	ids, err := s.store.ListQuarantined(ctx)
	if err != nil {
		return fmt.Errorf("list quarantined assets: %w", err)
	}

	for _, id := range ids {
		_, metadataErr := s.repository.GetStoredAsset(ctx, id)
		switch {
		case metadataErr == nil:
			if err := s.store.RestoreQuarantined(context.WithoutCancel(ctx), id); err != nil {
				return fmt.Errorf("restore quarantined asset %q: %w", id, err)
			}
		case errors.Is(metadataErr, ErrNotFound):
			if err := s.store.PurgeQuarantined(context.WithoutCancel(ctx), id); err != nil && !errors.Is(err, ErrNotFound) {
				return fmt.Errorf("purge quarantined asset %q: %w", id, err)
			}
		default:
			return fmt.Errorf("read metadata for quarantined asset %q: %w", id, metadataErr)
		}
	}

	return nil
}
