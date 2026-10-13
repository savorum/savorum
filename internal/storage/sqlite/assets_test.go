package sqlite

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/storage/files"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/library"
)

func TestDeleteOrphanedAssetRechecksReachabilityBeforeDeletingBlob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	createdAt := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	digest := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	blob := assets.Blob{
		ID: "sha256:" + assets.ID(digest), ContentHash: digest, MediaType: "image/png",
		Size: 12, StorageKey: "sha256/bb/bb/" + digest, CreatedAt: createdAt,
	}
	if _, err := store.RegisterStoredAsset(ctx, blob, createdAt); err != nil {
		t.Fatal(err)
	}

	if err := store.CreateBook(ctx, library.Book{
		ID: "breachable", Kind: library.BookPublication, Title: "Referenced book",
		CoverAssetID: string(blob.ID), Revision: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
	}); err != nil {
		t.Fatal(err)
	}

	callbackCalled := false

	deleted, err := store.DeleteOrphanedAsset(ctx, blob.ID, createdAt.Add(time.Hour), func(assets.Blob) error {
		callbackCalled = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if deleted || callbackCalled {
		t.Fatalf("reachable asset deleted=%t callback-called=%t", deleted, callbackCalled)
	}

	if _, err := store.GetStoredAsset(ctx, blob.ID); err != nil {
		t.Fatalf("reachable asset metadata was removed: %v", err)
	}
}

func TestDeleteOrphanedAssetSerializesConcurrentAttachment(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "library.db")

	collector, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = collector.Close() })

	server, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = server.Close() })

	createdAt := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	cutoff := createdAt.Add(time.Hour)
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	blob := assets.Blob{
		ID: "sha256:" + assets.ID(digest), ContentHash: digest, MediaType: "image/png",
		Size: 12, StorageKey: "sha256/aa/aa/" + digest, CreatedAt: createdAt,
	}
	if _, err := collector.RegisterStoredAsset(ctx, blob, createdAt); err != nil {
		t.Fatal(err)
	}

	deletionStarted := make(chan struct{})
	allowDeletion := make(chan struct{})
	collectionDone := make(chan error, 1)

	go func() {
		deleted, err := collector.DeleteOrphanedAsset(ctx, blob.ID, cutoff, func(current assets.Blob) error {
			if current.ID != blob.ID || current.StorageKey != blob.StorageKey {
				return errors.New("collector received the wrong blob")
			}

			close(deletionStarted)
			<-allowDeletion

			return nil
		})
		if err == nil && !deleted {
			err = errors.New("eligible asset was not deleted")
		}

		collectionDone <- err
	}()

	<-deletionStarted

	attachmentDone := make(chan error, 1)
	go func() {
		attachmentDone <- server.CreateBook(ctx, library.Book{
			ID: "bconcurrent", Kind: library.BookPublication, Title: "Concurrent book",
			CoverAssetID: string(blob.ID), Revision: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
		})
	}()

	// The attachment is a second store over the same file, so it parks
	// inside SQLite's busy wait rather than on the pool. Its connection
	// staying checked out proves the write is in flight against the
	// collector's lock, at which point an early result would be a bug.
	expires := time.Now().Add(2 * time.Second)
	for server.db.Stats().InUse == 0 {
		if time.Now().After(expires) {
			t.Fatal("attachment never reached the SQLite write")
		}

		time.Sleep(time.Millisecond)
	}

	select {
	case err := <-attachmentDone:
		t.Fatalf("attachment completed while collector held the SQLite writer slot: %v", err)
	default:
	}

	close(allowDeletion)

	if err := <-collectionDone; err != nil {
		t.Fatal(err)
	}

	if err := <-attachmentDone; !errors.Is(err, library.ErrConflict) {
		t.Fatalf("concurrent attachment error = %v, want a retained-data conflict", err)
	}

	if _, err := collector.GetStoredAsset(ctx, blob.ID); !errors.Is(err, assets.ErrNotFound) {
		t.Fatalf("collected asset remains readable: %v", err)
	}
}

func TestDeleteOrphanedAssetRollsBackWhenContextIsCancelledAfterByteMovement(t *testing.T) {
	t.Parallel()

	base := context.Background()

	store, err := Open(base, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	createdAt := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	digest := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

	blob := assets.Blob{
		ID: "sha256:" + assets.ID(digest), ContentHash: digest, MediaType: "image/png",
		Size: 12, StorageKey: "sha256/cc/cc/" + digest, CreatedAt: createdAt,
	}
	if _, err := store.RegisterStoredAsset(base, blob, createdAt); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(base)
	callbackCalled := false

	deleted, err := store.DeleteOrphanedAsset(ctx, blob.ID, createdAt.Add(time.Hour), func(assets.Blob) error {
		callbackCalled = true

		cancel()

		return nil
	})
	if err == nil || deleted || !callbackCalled {
		t.Fatalf("cancelled delete = deleted %t callback %t err %v", deleted, callbackCalled, err)
	}

	if _, err := store.GetStoredAsset(base, blob.ID); err != nil {
		t.Fatalf("cancelled deletion did not roll metadata back: %v", err)
	}
}

func TestAssetIngestRejectsViewerBeforeReadingOrStoring(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	owner, err := store.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	ownerCtx := access.WithLibrary(identity.WithPrincipal(ctx, owner), "default")

	viewer, err := store.CreateLocalUser(ctx, owner.UserID, "upload-viewer", "Viewer", "viewer password phrase")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.SetLibraryMember(ownerCtx, "default", viewer.UserID, access.Viewer, false); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "assets")

	blobs, err := files.NewAssetStore(root, 1024)
	if err != nil {
		t.Fatal(err)
	}

	service, err := assets.NewService(store, blobs)
	if err != nil {
		t.Fatal(err)
	}

	data := bytes.NewReader([]byte("unread upload"))

	viewerCtx := access.WithLibrary(identity.WithPrincipal(ctx, viewer), "default")
	if _, err := service.Ingest(viewerCtx, data, assets.IngestOptions{MediaType: "image/png"}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("ingest: %v", err)
	}

	if data.Len() != len("unread upload") {
		t.Fatal("unauthorized upload body was read")
	}

	if _, err := os.Stat(filepath.Join(root, "sha256")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unauthorized upload created blob tree: %v", err)
	}
}
