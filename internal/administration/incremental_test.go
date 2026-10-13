package administration_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	mathrand "math/rand"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"sync/atomic"
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

func incrementalSource(t *testing.T) (*sqlite.Store, *files.AssetStore, *assets.Service, string, string, library.Entry, assets.Blob) {
	t.Helper()
	root := t.TempDir()
	assetRoot := filepath.Join(root, "assets")

	store, err := sqlite.Open(t.Context(), filepath.Join(root, "source.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	blobs, err := files.NewAssetStore(assetRoot, files.DefaultMaximumAssetSize)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

	service, err := assets.NewService(store, blobs, assets.WithClock(func() time.Time { return now.Add(24 * time.Hour) }))
	if err != nil {
		t.Fatal(err)
	}

	image := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("immutable synthetic backup image")...)

	blob, err := service.Ingest(t.Context(), bytes.NewReader(image), assets.IngestOptions{MediaType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}

	document := testfixture.Document()
	document.Assets[0].SHA256, document.Assets[0].Size, document.Assets[0].MediaType = blob.ContentHash, blob.Size, blob.MediaType

	entry := library.Entry{ID: "incremental_recipe", Recipe: document, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.Create(t.Context(), entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	return store, blobs, service, assetRoot, root, entry, blob
}

func TestIncrementalSnapshotCopiesOnlyReferencedKeys(t *testing.T) {
	store, _, _, assetRoot, root, _, _ := incrementalSource(t)
	seedRecoveryExtras(t, filepath.Join(root, "source.db"))

	secrets := filepath.Join(root, "keys")
	if err := os.Mkdir(secrets, 0o700); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"key_fixture", "unreferenced"} {
		if err := os.WriteFile(filepath.Join(secrets, name), []byte("whsec_synthetic_incremental_fixture_0123456789"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	stage := filepath.Join(root, "stage")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := administration.PrepareIncremental(t.Context(), store, assetRoot, secrets, stage, time.Now(), 16<<20); err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(secrets); err != nil {
		t.Fatal(err)
	}

	keys, err := os.ReadDir(filepath.Join(stage, "webhook-secrets"))
	if err != nil || len(keys) != 1 || keys[0].Name() != "key_fixture" {
		t.Fatal("snapshot key identity differs", keys, err)
	}

	if err := administration.VerifyWebhookSecrets(administration.StateReport{WebhookSecrets: []string{"key_fixture"}}, filepath.Join(stage, "webhook-secrets")); err != nil {
		t.Fatal("captured key did not survive source removal", err)
	}
}

func TestIncrementalPreparationResourceBound(t *testing.T) {
	value := os.Getenv("SAVORUM_CAPTURE_QUALIFICATION_BYTES")
	if value == "" {
		t.Skip("set SAVORUM_CAPTURE_QUALIFICATION_BYTES for the synthetic resource qualification")
	}

	workload, err := strconv.ParseInt(value, 10, 64)
	if err != nil || workload < 1 || workload > 768<<20 {
		t.Fatal("invalid qualification workload")
	}

	store, _, _, assetRoot, root, _, _ := incrementalSource(t)

	db, err := sql.Open("sqlite", filepath.Join(root, "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	remaining := workload
	for seed := int64(1); remaining > 0; seed++ {
		size := min(remaining, int64(64<<20))

		file, err := os.CreateTemp(assetRoot, "synthetic-*")
		if err != nil {
			t.Fatal(err)
		}

		hash := sha256.New()

		random := mathrand.New(mathrand.NewSource(seed)) //nolint:gosec // Reproducible synthetic originals, not secret material.
		if _, err := io.CopyN(io.MultiWriter(file, hash), random, size); err != nil {
			t.Fatal(err)
		}

		if err := file.Close(); err != nil {
			t.Fatal(err)
		}

		digest := hex.EncodeToString(hash.Sum(nil))
		key := "sha256/" + digest[:2] + "/" + digest[2:4] + "/" + digest

		target := filepath.Join(assetRoot, filepath.FromSlash(key))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.Rename(file.Name(), target); err != nil {
			t.Fatal(err)
		}

		if _, err := db.ExecContext(t.Context(), `INSERT INTO assets(id,content_hash,media_type,size,storage_key,created_at) VALUES(?,?,?,?,?,?)`, "sha256:"+digest, digest, "image/png", size, key, time.Now().Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}

		remaining -= size
	}

	stage := filepath.Join(root, "stage")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}

	runtime.GC()

	var peak atomic.Uint64

	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)

		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()

		for {
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			peak.Store(max(peak.Load(), stats.HeapAlloc))

			select {
			case <-stop:
				return
			case <-ticker.C:
			}
		}
	}()

	started := time.Now()
	pinned, err := administration.PrepareIncremental(t.Context(), store, assetRoot, "", stage, time.Now(), 1<<30)
	duration := time.Since(started)

	close(stop)
	<-done

	if err != nil {
		t.Fatal(err)
	}

	var pinnedBytes int64
	for _, asset := range pinned {
		pinnedBytes += asset.Size
	}

	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatal(err)
	}

	resident := usage.Maxrss
	if runtime.GOOS == "linux" {
		resident *= 1024
	}

	t.Logf("synthetic_original_bytes=%d pinned_original_bytes=%d preparation_time_upper_bound_on_writer_fence=%s heap_peak_sampled_bytes=%d process_peak_rss_bytes=%d", workload, pinnedBytes, duration, peak.Load(), resident)

	if peak.Load() >= 512<<20 || resident >= 512<<20 {
		t.Fatal("synthetic snapshot preparation exceeds the 512 MiB process target")
	}
}

func zipIncrementalTree(t *testing.T, directory, archive string) {
	t.Helper()

	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}

	writer := zip.NewWriter(file)

	err = filepath.WalkDir(filepath.Join(directory, "native"), func(full string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			return nil
		}

		name, err := filepath.Rel(filepath.Join(directory, "native"), full)
		if err != nil {
			return fmt.Errorf("name fixture member: %w", err)
		}

		member, err := writer.Create(filepath.ToSlash(name))
		if err != nil {
			return fmt.Errorf("create fixture member: %w", err)
		}

		input, err := os.Open(full) //nolint:gosec // Synthetic tree owned exclusively by this test.
		if err != nil {
			return fmt.Errorf("open fixture member: %w", err)
		}

		_, copyErr := io.Copy(member, input)
		closeErr := input.Close()

		if copyErr != nil {
			return fmt.Errorf("copy fixture member: %w", copyErr)
		}

		if closeErr != nil {
			return fmt.Errorf("close fixture member: %w", closeErr)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestIncrementalSnapshotSurvivesConcurrentEditPurgeAndGarbageCollection(t *testing.T) {
	store, blobs, service, assetRoot, root, entry, blob := incrementalSource(t)
	ctx := t.Context()

	full := filepath.Join(root, "full.savorum-backup")
	if err := administration.Backup(ctx, store, assetRoot, full, time.Now()); err != nil {
		t.Fatal(err)
	}

	expected, err := administration.Verify(ctx, full)
	if err != nil {
		t.Fatal(err)
	}

	stage := filepath.Join(root, "stage")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}

	start := time.Now()

	pinned, err := administration.PrepareIncremental(ctx, store, assetRoot, "", stage, time.Now(), 16<<20)
	if err != nil || len(pinned) != 1 {
		t.Fatalf("prepare: %v %v", pinned, err)
	}

	t.Logf("snapshot_and_pin_time=%s database_writer_fence_ends_before_collection", time.Since(start))

	sourceInfo, err := os.Stat(filepath.Join(assetRoot, filepath.FromSlash(blob.StorageKey)))
	if err != nil {
		t.Fatal(err)
	}

	pinInfo, err := os.Stat(pinned[0].Path)
	if err != nil || !os.SameFile(sourceInfo, pinInfo) {
		t.Fatal("snapshot copied or failed to pin original asset", err)
	}
	// A separate connection can edit and purge while collection retains the old bytes.
	done := make(chan error, 1)

	go func() {
		changed := entry.Recipe
		changed.Title = "Written during incremental collection"

		_, err := store.Revise(ctx, library.RevisionCommand{ID: entry.ID, ExpectedRevision: 1, Recipe: changed, CurrentState: library.StateActive, TargetState: library.StateTrash, Metadata: library.RevisionMetadata{Cause: library.RevisionCauseTrash}, Timestamp: time.Now()})
		if err == nil {
			err = store.Purge(ctx, entry.ID, 2, entry.CreatedAt.Add(-48*time.Hour))
		}

		if err == nil {
			_, err = service.CollectGarbage(ctx, time.Hour)
		}

		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("collection holds the application writer fence")
	}

	if _, err := blobs.Stat(ctx, blob.ID); err == nil {
		t.Fatal("GC did not remove original source blob")
	}

	archive := filepath.Join(root, "incremental.savorum-backup")
	zipIncrementalTree(t, stage, archive)

	actual, err := administration.Verify(ctx, archive)
	if err != nil || !reflect.DeepEqual(actual.State, expected.State) {
		t.Fatalf("full/native snapshot equivalence: %v", err)
	}

	target := t.TempDir()
	if err := administration.Restore(ctx, archive, filepath.Join(target, "restored.db"), filepath.Join(target, "assets")); err != nil {
		t.Fatal(err)
	}
}

func TestIncrementalPreparationRejectsMissingUnsafeOversizedAndCancelledSources(t *testing.T) {
	for _, scenario := range []string{"missing", "symlink", "ancestor-symlink", "bound", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			store, _, _, assetRoot, root, _, blob := incrementalSource(t)
			path := filepath.Join(assetRoot, filepath.FromSlash(blob.StorageKey))
			ctx := t.Context()
			bound := int64(16 << 20)

			switch scenario {
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}

				if err := os.Symlink(filepath.Join(root, "source.db"), path); err != nil {
					t.Fatal(err)
				}
			case "ancestor-symlink":
				// Move the actual original outside AssetRoot, preserving its hash
				// and size so only the escaped ancestor makes it invalid.
				outside := filepath.Join(root, "outside-originals")
				if err := os.Rename(filepath.Join(assetRoot, "sha256"), outside); err != nil {
					t.Fatal(err)
				}

				if err := os.Symlink(outside, filepath.Join(assetRoot, "sha256")); err != nil {
					t.Fatal(err)
				}
			case "bound":
				bound = 1
			case "cancelled":
				var cancel context.CancelFunc

				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			stage := filepath.Join(root, "stage")
			if err := os.Mkdir(stage, 0o700); err != nil {
				t.Fatal(err)
			}

			if _, err := administration.PrepareIncremental(ctx, store, assetRoot, "", stage, time.Now(), bound); err == nil {
				t.Fatal("invalid source accepted")
			}

			if _, err := os.Stat(stage); !os.IsNotExist(err) {
				t.Fatal("failed snapshot workspace retained", err)
			}
		})
	}
}
