package assets_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

func TestVerifiedIngestionAndGracePeriodGarbageCollection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	repository, err := sqlite.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = repository.Close() })

	store, err := files.NewAssetStore(filepath.Join(root, "assets"), 1024)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)

	service, err := assets.NewService(repository, store, assets.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}

	data := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("asset")...)
	hash := sha256.Sum256(data)
	digest := hex.EncodeToString(hash[:])
	size := int64(len(data))

	blob, err := service.Ingest(ctx, bytes.NewReader(data), assets.IngestOptions{SHA256: digest, Size: &size, MediaType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}

	if blob.ID != assets.ID("sha256:"+digest) || blob.OrphanedAt == nil {
		t.Fatalf("staged blob metadata = %#v", blob)
	}

	expectedPath := filepath.Join(root, "assets", "sha256", digest[:2], digest[2:4], digest)
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("content-addressed blob path: %v", err)
	}

	metadata, err := service.Metadata(ctx, blob.ID)
	if err != nil || metadata.ID != blob.ID || metadata.Size != int64(len(data)) {
		t.Fatalf("asset metadata = %#v, err = %v", metadata, err)
	}

	if _, _, err := service.Open(ctx, blob.ID); err != nil {
		t.Fatal(err)
	}

	if removed, err := service.CollectGarbage(ctx, 24*time.Hour); err != nil || removed != 0 {
		t.Fatalf("early GC removed=%d err=%v", removed, err)
	}

	now = now.Add(25 * time.Hour)
	if removed, err := service.CollectGarbage(ctx, 24*time.Hour); err != nil || removed != 1 {
		t.Fatalf("mature GC removed=%d err=%v", removed, err)
	}

	if _, _, err := service.Open(ctx, blob.ID); !errors.Is(err, assets.ErrNotFound) {
		t.Fatalf("garbage-collected asset open error = %v", err)
	}

	if _, err := service.Metadata(ctx, blob.ID); !errors.Is(err, assets.ErrNotFound) {
		t.Fatalf("garbage-collected asset metadata error = %v", err)
	}

	if _, err := os.Stat(expectedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("garbage-collected blob still exists: %v", err)
	}

	if removed, err := service.CollectGarbage(ctx, 24*time.Hour); err != nil || removed != 0 {
		t.Fatalf("idempotent GC removed=%d err=%v", removed, err)
	}
}

func TestCapacityPolicyBoundsSequentialOrphanVariationsAndPreservesDeduplication(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	repository, err := sqlite.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = repository.Close() })

	store, err := files.NewAssetStore(filepath.Join(root, "assets"), 16)
	if err != nil {
		t.Fatal(err)
	}

	policy := assets.DefaultCapacityPolicy()
	policy.TotalBytes, policy.OrphanBytes, policy.Objects = 10, 10, 2
	policy.StagingBytes, policy.MaximumUploadBytes, policy.MinimumFreeBytes = 16, 16, 0

	service, err := assets.NewService(repository, store, assets.WithCapacityPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}

	for _, data := range [][]byte{[]byte("aaaaa"), []byte("aaaab")} {
		size := int64(len(data))
		if _, err := service.Ingest(ctx, bytes.NewReader(data), assets.IngestOptions{Size: &size, MediaType: "text/plain"}); err != nil {
			t.Fatal(err)
		}
	}

	third := []byte("aaaac")

	size := int64(len(third))
	if _, err := service.Ingest(ctx, bytes.NewReader(third), assets.IngestOptions{Size: &size, MediaType: "text/plain"}); !errors.Is(err, assets.ErrCapacity) {
		t.Fatalf("third variation error=%v", err)
	}
	// A deduplicated upload consumes request/rate work but no additional durable
	// bytes or object allowance.
	data := []byte("aaaaa")

	size = int64(len(data))
	if _, err := service.Ingest(ctx, bytes.NewReader(data), assets.IngestOptions{Size: &size, MediaType: "text/plain"}); err != nil {
		t.Fatalf("deduplicated upload: %v", err)
	}

	usage, err := service.Usage(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if usage.TotalBytes != 10 || usage.OrphanBytes != 10 || usage.Objects != 2 || usage.StagingBytes != 0 || usage.Rejected != 1 {
		t.Fatalf("usage=%+v", usage)
	}
}

type blockingReader struct {
	entered chan struct{}
	release chan struct{}
}

func (r *blockingReader) Read(p []byte) (int, error) {
	close(r.entered)
	<-r.release
	copy(p, "data")

	return 4, io.EOF
}

func TestConcurrentStagingReservationRejectsBeforeStorage(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	repository, err := sqlite.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = repository.Close() })

	store, err := files.NewAssetStore(filepath.Join(root, "assets"), 16)
	if err != nil {
		t.Fatal(err)
	}

	policy := assets.DefaultCapacityPolicy()
	policy.StagingBytes = 6
	policy.MaximumUploadBytes = 6
	policy.MinimumFreeBytes = 0

	service, err := assets.NewService(repository, store, assets.WithCapacityPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}

	reader := &blockingReader{entered: make(chan struct{}), release: make(chan struct{})}
	size := int64(4)
	first := make(chan error, 1)

	go func() {
		_, err := service.Ingest(ctx, reader, assets.IngestOptions{Size: &size, MediaType: "text/plain"})
		first <- err
	}()

	<-reader.entered

	if _, err := service.Ingest(ctx, bytes.NewReader([]byte("more")), assets.IngestOptions{Size: &size, MediaType: "text/plain"}); !errors.Is(err, assets.ErrCapacity) {
		t.Fatalf("concurrent error=%v", err)
	}

	close(reader.release)

	if err := <-first; err != nil {
		t.Fatal(err)
	}

	usage, err := service.Usage(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if usage.StagingBytes != 0 || usage.ActiveUploads != 0 || usage.Rejected != 1 {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestCanceledUploadReleasesAllAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	root := t.TempDir()

	repository, err := sqlite.Open(context.Background(), filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = repository.Close() })

	store, err := files.NewAssetStore(filepath.Join(root, "assets"), 16)
	if err != nil {
		t.Fatal(err)
	}

	policy := assets.DefaultCapacityPolicy()
	policy.MinimumFreeBytes = 0

	service, err := assets.NewService(repository, store, assets.WithCapacityPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}

	size := int64(4)
	if _, err := service.Ingest(ctx, bytes.NewReader([]byte("data")), assets.IngestOptions{Size: &size, MediaType: "text/plain"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v", err)
	}

	usage, err := service.Usage(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if usage.StagingBytes != 0 || usage.Objects != 0 {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestAssetStoreCleansAbandonedStagingOnRestart(t *testing.T) {
	root := t.TempDir()
	if _, err := files.NewAssetStore(root, 16); err != nil {
		t.Fatal(err)
	}

	abandoned := filepath.Join(root, ".staging", "upload-abandoned")
	if err := os.WriteFile(abandoned, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := files.NewAssetStore(root, 16); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(abandoned); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("abandoned staging remains: %v", err)
	}
}

type lowCapacityStore struct {
	assets.BlobStore
	available uint64
}

func (s lowCapacityStore) AvailableBytes(context.Context) (uint64, error) { return s.available, nil }

func TestFreeSpaceWatermarkRejectsBeforePromotion(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	repository, err := sqlite.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = repository.Close() })

	store, err := files.NewAssetStore(filepath.Join(root, "assets"), 16)
	if err != nil {
		t.Fatal(err)
	}

	policy := assets.DefaultCapacityPolicy()
	policy.MinimumFreeBytes = 10

	service, err := assets.NewService(repository, lowCapacityStore{BlobStore: store, available: 10}, assets.WithCapacityPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}

	size := int64(1)
	if _, err := service.Ingest(ctx, bytes.NewReader([]byte("x")), assets.IngestOptions{Size: &size, MediaType: "text/plain"}); !errors.Is(err, assets.ErrCapacity) {
		t.Fatalf("watermark error=%v", err)
	}

	objects, err := store.Inventory(ctx)
	if err != nil {
		t.Fatal(err)
	}

	registered, err := repository.ListStoredAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(objects) != 0 || len(registered) != 0 {
		t.Fatalf("rejected upload promoted objects=%d rows=%d", len(objects), len(registered))
	}
}

func TestUploadRateLimitIsControlledAndRecoversNextWindow(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	repository, err := sqlite.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = repository.Close() })

	store, err := files.NewAssetStore(filepath.Join(root, "assets"), 16)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, time.August, 31, 9, 0, 0, 0, time.UTC)
	policy := assets.DefaultCapacityPolicy()
	policy.UploadsPerMinute = 1
	policy.MinimumFreeBytes = 0

	service, err := assets.NewService(repository, store, assets.WithCapacityPolicy(policy), assets.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}

	size := int64(1)
	if _, err := service.Ingest(ctx, bytes.NewReader([]byte("a")), assets.IngestOptions{Size: &size, MediaType: "text/plain"}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Ingest(ctx, bytes.NewReader([]byte("b")), assets.IngestOptions{Size: &size, MediaType: "text/plain"}); !errors.Is(err, assets.ErrBusy) {
		t.Fatalf("rate error=%v", err)
	}

	now = now.Add(time.Minute)

	if _, err := service.Ingest(ctx, bytes.NewReader([]byte("b")), assets.IngestOptions{Size: &size, MediaType: "text/plain"}); err != nil {
		t.Fatalf("next window: %v", err)
	}
}

func TestCapacityIsReconstructedFromInventoryAfterRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	repository, err := sqlite.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = repository.Close() })

	store, err := files.NewAssetStore(filepath.Join(root, "assets"), 16)
	if err != nil {
		t.Fatal(err)
	}

	policy := assets.DefaultCapacityPolicy()
	policy.Objects = 1
	policy.MinimumFreeBytes = 0

	first, err := assets.NewService(repository, store, assets.WithCapacityPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}

	size := int64(1)
	if _, err := first.Ingest(ctx, bytes.NewReader([]byte("a")), assets.IngestOptions{Size: &size, MediaType: "text/plain"}); err != nil {
		t.Fatal(err)
	}

	restarted, err := assets.NewService(repository, store, assets.WithCapacityPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := restarted.Ingest(ctx, bytes.NewReader([]byte("b")), assets.IngestOptions{Size: &size, MediaType: "text/plain"}); !errors.Is(err, assets.ErrCapacity) {
		t.Fatalf("restart capacity error=%v", err)
	}
}

func TestIngestionRejectsUnverifiedMetadataBeforeRegistration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	repository, err := sqlite.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = repository.Close() })

	store, err := files.NewAssetStore(filepath.Join(root, "assets"), 1024)
	if err != nil {
		t.Fatal(err)
	}

	service, err := assets.NewService(repository, store)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("not a jpeg")
	if _, err := service.Ingest(ctx, bytes.NewReader(data), assets.IngestOptions{MediaType: "image/jpeg"}); !errors.Is(err, assets.ErrMediaType) {
		t.Fatalf("media mismatch error = %v", err)
	}
}

func TestCorruptContentIsNotServedAndVerifiedIngestionRepairsIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	repository, err := sqlite.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = repository.Close() })

	store, err := files.NewAssetStore(filepath.Join(root, "assets"), 1024)
	if err != nil {
		t.Fatal(err)
	}

	service, err := assets.NewService(repository, store)
	if err != nil {
		t.Fatal(err)
	}

	data := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("integrity")...)

	blob, err := service.Ingest(ctx, bytes.NewReader(data), assets.IngestOptions{MediaType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}

	path := assetPath(root, blob.ContentHash)

	corrupt := append([]byte(nil), data...)

	corrupt[len(corrupt)-1] ^= 0xff
	if err := os.WriteFile(path, corrupt, 0o640); err != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
		t.Fatal(err)
	}

	if _, _, err := service.Open(ctx, blob.ID); !errors.Is(err, assets.ErrIntegrity) || !errors.Is(err, assets.ErrDigest) {
		t.Fatalf("corrupt open error = %v, want digest failure", err)
	}

	report, err := service.Scrub(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !hasIntegrityIssue(report, assets.IntegrityDigestMismatch, blob.ID) {
		t.Fatalf("scrub report = %#v, want digest mismatch", report)
	}

	if _, err := service.Ingest(ctx, bytes.NewReader(data), assets.IngestOptions{MediaType: "image/png"}); err != nil {
		t.Fatalf("verified reingestion did not repair corrupt content: %v", err)
	}

	_, opened, err := service.Open(ctx, blob.ID)
	if err != nil {
		t.Fatal(err)
	}

	actual, err := io.ReadAll(opened)
	_ = opened.Close()

	if err != nil || !bytes.Equal(actual, data) {
		t.Fatalf("repaired bytes = %x err=%v", actual, err)
	}
}

func TestScrubFindsMetadataOnlyAndUnregisteredAssets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	repository, err := sqlite.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = repository.Close() })

	store, err := files.NewAssetStore(filepath.Join(root, "assets"), 1024)
	if err != nil {
		t.Fatal(err)
	}

	service, err := assets.NewService(repository, store)
	if err != nil {
		t.Fatal(err)
	}

	registered, err := service.Ingest(ctx, bytes.NewReader([]byte("registered text")), assets.IngestOptions{MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(assetPath(root, registered.ContentHash)); err != nil {
		t.Fatal(err)
	}

	unregisteredBytes := []byte("unregistered text")
	unregisteredSum := sha256.Sum256(unregisteredBytes)
	unregisteredDigest := hex.EncodeToString(unregisteredSum[:])

	unregisteredPath := assetPath(root, unregisteredDigest)
	if err := os.MkdirAll(filepath.Dir(unregisteredPath), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(unregisteredPath, unregisteredBytes, 0o640); err != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
		t.Fatal(err)
	}

	report, err := service.Scrub(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !hasIntegrityIssue(report, assets.IntegrityMetadataOnly, registered.ID) ||
		!hasIntegrityIssue(report, assets.IntegrityUnregistered, assets.ID("sha256:"+unregisteredDigest)) {
		t.Fatalf("scrub report = %#v", report)
	}
}

type failingDeleteRepository struct {
	assets.Repository
	err error
}

func (r failingDeleteRepository) DeleteOrphanedAsset(ctx context.Context, id assets.ID, _ time.Time, move func(assets.Blob) error) (bool, error) {
	blob, err := r.GetStoredAsset(ctx, id)
	if err != nil {
		return false, err
	}

	if err := move(blob); err != nil {
		return false, err
	}

	return false, r.err
}

func TestGarbageCollectionRestoresBytesWhenMetadataCommitFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	repository, err := sqlite.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = repository.Close() })

	store, err := files.NewAssetStore(filepath.Join(root, "assets"), 1024)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	commitFailure := errors.New("injected metadata commit failure")

	service, err := assets.NewService(failingDeleteRepository{Repository: repository, err: commitFailure}, store,
		assets.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}

	blob, err := service.Ingest(ctx, bytes.NewReader([]byte("recoverable bytes")), assets.IngestOptions{MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(25 * time.Hour)
	if _, err := service.CollectGarbage(ctx, 24*time.Hour); !errors.Is(err, commitFailure) {
		t.Fatalf("collection error = %v, want injected commit failure", err)
	}

	if _, err := os.Stat(assetPath(root, blob.ContentHash)); err != nil {
		t.Fatalf("rollback did not restore asset bytes: %v", err)
	}

	quarantined, err := store.ListQuarantined(ctx)
	if err != nil || len(quarantined) != 0 {
		t.Fatalf("quarantine after rollback = %v err=%v", quarantined, err)
	}

	if _, err := repository.GetStoredAsset(ctx, blob.ID); err != nil {
		t.Fatalf("metadata did not survive failed deletion: %v", err)
	}
}

func TestScrubRecoversInterruptedQuarantineOnEitherSideOfCommit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	repository, err := sqlite.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = repository.Close() })

	store, err := files.NewAssetStore(filepath.Join(root, "assets"), 1024)
	if err != nil {
		t.Fatal(err)
	}

	service, err := assets.NewService(repository, store)
	if err != nil {
		t.Fatal(err)
	}

	blob, err := service.Ingest(ctx, bytes.NewReader([]byte("interrupted collection")), assets.IngestOptions{MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Quarantine(ctx, blob.ID); err != nil {
		t.Fatal(err)
	}

	if report, err := service.Scrub(ctx); err != nil || len(report.Issues) != 0 {
		t.Fatalf("pre-commit recovery report=%#v err=%v", report, err)
	}

	if _, err := os.Stat(assetPath(root, blob.ContentHash)); err != nil {
		t.Fatalf("pre-commit quarantine was not restored: %v", err)
	}

	if err := store.Quarantine(ctx, blob.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Ingest(ctx, bytes.NewReader([]byte("interrupted collection")), assets.IngestOptions{MediaType: "text/plain"}); err != nil {
		t.Fatalf("concurrent verified reingestion: %v", err)
	}

	if report, err := service.Scrub(ctx); err != nil || len(report.Issues) != 0 {
		t.Fatalf("recreated-final recovery report=%#v err=%v", report, err)
	}

	if quarantined, err := store.ListQuarantined(ctx); err != nil || len(quarantined) != 0 {
		t.Fatalf("recreated final left quarantine = %v err=%v", quarantined, err)
	}

	if err := store.Quarantine(ctx, blob.ID); err != nil {
		t.Fatal(err)
	}

	deleted, err := repository.DeleteOrphanedAsset(ctx, blob.ID, time.Now().Add(time.Hour), func(assets.Blob) error { return nil })
	if err != nil || !deleted {
		t.Fatalf("simulate committed metadata deletion: deleted=%t err=%v", deleted, err)
	}

	if report, err := service.Scrub(ctx); err != nil || len(report.Issues) != 0 {
		t.Fatalf("post-commit recovery report=%#v err=%v", report, err)
	}

	quarantined, err := store.ListQuarantined(ctx)
	if err != nil || len(quarantined) != 0 {
		t.Fatalf("post-commit quarantine = %v err=%v", quarantined, err)
	}
}

func assetPath(root, digest string) string {
	return filepath.Join(root, "assets", "sha256", digest[:2], digest[2:4], digest)
}

func hasIntegrityIssue(report assets.IntegrityReport, kind assets.IntegrityIssueKind, id assets.ID) bool {
	for _, issue := range report.Issues {
		if issue.Kind == kind && issue.ID == id {
			return true
		}
	}

	return false
}
