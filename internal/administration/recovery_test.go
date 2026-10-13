package administration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/administration"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestBackupAndRestoreUseOwnerOnlyModesUnderPermissiveUmask(t *testing.T) {
	previous := syscall.Umask(0o022)
	defer syscall.Umask(previous)

	ctx := context.Background()
	root := t.TempDir()
	databasePath := filepath.Join(root, "source", "savorum.db")
	assetRoot := filepath.Join(root, "source", "assets")

	store, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := files.NewAssetStore(assetRoot, files.DefaultMaximumAssetSize); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(root, "backup", "snapshot.savorum-backup")
	if err := administration.Backup(ctx, store, assetRoot, archive, time.Now()); err != nil {
		t.Fatal(err)
	}

	restoredDatabase := filepath.Join(root, "restored", "savorum.db")

	restoredAssets := filepath.Join(root, "restored", "assets")
	if err := administration.Restore(ctx, archive, restoredDatabase, restoredAssets); err != nil {
		t.Fatal(err)
	}

	for path, want := range map[string]os.FileMode{
		archive:          0o600,
		restoredDatabase: 0o600,
		restoredAssets:   0o700,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}

		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %#o, want %#o", path, got, want)
		}
	}
}

func TestCIRecoveryDrillBacksUpDestroysRestoresAndVerifiesSemantics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	databasePath := filepath.Join(root, "source", "savorum.db")
	assetRoot := filepath.Join(root, "source", "assets")

	if err := os.MkdirAll(filepath.Dir(databasePath), 0o750); err != nil {
		t.Fatal(err)
	}

	store, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	blobStore, err := files.NewAssetStore(assetRoot, files.DefaultMaximumAssetSize)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, time.August, 26, 19, 0, 0, 0, time.UTC)

	assetService, err := assets.NewService(store, blobStore, assets.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}

	image := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("backup-image")...)

	blob, err := assetService.Ingest(ctx, bytes.NewReader(image), assets.IngestOptions{MediaType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}

	digestBytes := sha256.Sum256(image)
	digest := hex.EncodeToString(digestBytes[:])
	document := testfixture.Document()
	document.Assets[0].SHA256, document.Assets[0].Size, document.Assets[0].MediaType = digest, int64(len(image)), "image/png"

	entry := library.Entry{ID: "rbackup", Recipe: document, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Revise(ctx, library.RevisionCommand{ID: entry.ID, ExpectedRevision: 1, Recipe: document, CurrentState: library.StateActive, TargetState: library.StateTrash, Metadata: library.RevisionMetadata{Cause: library.RevisionCauseTrash}, Timestamp: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}

	if err := store.CreateBook(ctx, library.Book{ID: "bbackup", Kind: library.BookPersonal, Title: "Backup book", Revision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(root, "snapshot.savorum-backup")
	if err := administration.Backup(ctx, store, assetRoot, archive, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	report, err := administration.Verify(ctx, archive)
	if err != nil {
		t.Fatalf("verify backup before recovery drill: %v", err)
	}

	if report.SavorumVersion == "" || report.DatabaseSchemaVersion == 0 || report.Files != 2 {
		t.Fatalf("verification report = %#v", report)
	}
	// Model the CI recovery drill explicitly: the source instance is gone, so
	// semantic assertions below can only succeed from archive contents.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(filepath.Join(root, "source")); err != nil {
		t.Fatal(err)
	}

	restoredDatabase := filepath.Join(root, "restored", "savorum.db")

	restoredAssets := filepath.Join(root, "restored", "assets")
	if err := administration.Restore(ctx, archive, restoredDatabase, restoredAssets); err != nil {
		t.Fatal(err)
	}

	restored, err := sqlite.Open(ctx, restoredDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()

	trashed, err := restored.Get(ctx, entry.ID, library.StateTrash)
	if err != nil || trashed.Revision != 2 {
		t.Fatalf("restored trash head = %#v err=%v", trashed, err)
	}

	revisions, err := restored.ListRevisions(ctx, entry.ID, library.RevisionListOptions{Limit: 10})
	if err != nil || len(revisions) != 2 || revisions[1].Cause != library.RevisionCauseAPI {
		t.Fatalf("restored revisions = %#v err=%v", revisions, err)
	}

	if _, err := restored.GetBook(ctx, "bbackup", library.StateActive); err != nil {
		t.Fatalf("restored book: %v", err)
	}

	assetPath := filepath.Join(restoredAssets, "sha256", digest[:2], digest[2:4], digest)

	got, err := os.ReadFile(assetPath)
	if err != nil || !bytes.Equal(got, image) || string(blob.ID) != "sha256:"+digest {
		t.Fatalf("restored asset bytes=%x err=%v", got, err)
	}
}

func TestFailedRestoreDoesNotChangeExistingTargets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	database := filepath.Join(root, "existing.db")
	if err := os.WriteFile(database, []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := administration.Restore(context.Background(), filepath.Join(root, "missing"), database, filepath.Join(root, "assets")); err == nil {
		t.Fatal("restore unexpectedly overwrote an existing target")
	}
}

func TestBackupFailsClosedOnCorruptRegisteredAssetBytes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	databasePath := filepath.Join(root, "savorum.db")
	assetRoot := filepath.Join(root, "assets")

	store, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	blobStore, err := files.NewAssetStore(assetRoot, files.DefaultMaximumAssetSize)
	if err != nil {
		t.Fatal(err)
	}

	service, err := assets.NewService(store, blobStore)
	if err != nil {
		t.Fatal(err)
	}

	original := []byte("registered backup bytes")

	blob, err := service.Ingest(ctx, bytes.NewReader(original), assets.IngestOptions{MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}

	corrupt := append([]byte(nil), original...)
	corrupt[len(corrupt)-1] ^= 0xff

	path := filepath.Join(assetRoot, "sha256", blob.ContentHash[:2], blob.ContentHash[2:4], blob.ContentHash)
	if err := os.WriteFile(path, corrupt, 0o640); err != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
		t.Fatal(err)
	}

	archive := filepath.Join(root, "corrupt.savorum-backup")
	if err := administration.Backup(ctx, store, assetRoot, archive, time.Now()); err == nil {
		t.Fatal("backup accepted corrupt bytes under an immutable asset identity")
	}

	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatalf("failed backup published an archive: %v", err)
	}
}
