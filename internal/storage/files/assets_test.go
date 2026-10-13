package files_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/storage/filepolicy"
	"github.com/savorum/savorum/internal/storage/files"
)

func TestAssetTreeIgnoresPermissiveUmaskAndMigratesExistingModes(t *testing.T) {
	previous := syscall.Umask(0o022)
	defer syscall.Umask(previous)

	root := filepath.Join(t.TempDir(), "assets")

	legacy := filepath.Join(root, "legacy")
	if err := os.MkdirAll(root, 0o755); err != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
		t.Fatal(err)
	}

	if err := os.WriteFile(legacy, []byte("legacy"), 0o644); err != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
		t.Fatal(err)
	}

	if err := os.Chmod(root, 0o755); err != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
		t.Fatal(err)
	}

	if err := os.Chmod(legacy, 0o644); err != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
		t.Fatal(err)
	}

	store, err := files.NewAssetStore(root, files.DefaultMaximumAssetSize)
	if err != nil {
		t.Fatal(err)
	}

	image := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("private-image")...)

	blob, err := store.Put(context.Background(), bytes.NewReader(image), assets.IngestOptions{MediaType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}

	assetPath := filepath.Join(root, filepath.FromSlash(blob.StorageKey))

	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk asset tree: %w", walkErr)
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect asset tree entry: %w", err)
		}

		want := os.FileMode(0o600)
		if info.IsDir() {
			want = 0o700
		}

		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %#o, want %#o", path, got, want)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(assetPath); err != nil {
		t.Fatal(err)
	}
}

func TestAssetLifecycleRoundTripsThroughQuarantineAndDelete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "assets")

	store, err := files.NewAssetStore(root, files.DefaultMaximumAssetSize)
	if err != nil {
		t.Fatal(err)
	}

	image := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("lifecycle-pixels")...)

	blob, err := store.Put(ctx, bytes.NewReader(image), assets.IngestOptions{MediaType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}

	stat, err := store.Stat(ctx, blob.ID)
	if err != nil || stat.Size != int64(len(image)) {
		t.Fatalf("stat = %#v, %v", stat, err)
	}

	reader, err := store.Open(ctx, blob.ID, blob.Size)
	if err != nil {
		t.Fatal(err)
	}

	readBack, err := io.ReadAll(reader)
	_ = reader.Close()

	if err != nil || !bytes.Equal(readBack, image) {
		t.Fatalf("open read %d bytes, %v", len(readBack), err)
	}

	inventory, err := store.Inventory(ctx)
	if err != nil || len(inventory) != 1 || inventory[0].ID != blob.ID {
		t.Fatalf("inventory = %#v, %v", inventory, err)
	}

	if err := store.Quarantine(ctx, blob.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Stat(ctx, blob.ID); !errors.Is(err, assets.ErrNotFound) {
		t.Fatalf("quarantined stat = %v", err)
	}

	if _, err := store.Open(ctx, blob.ID, blob.Size); !errors.Is(err, assets.ErrNotFound) {
		t.Fatalf("quarantined open = %v", err)
	}

	if err := store.Quarantine(ctx, blob.ID); err != nil {
		t.Fatalf("repeated quarantine = %v", err)
	}

	quarantined, err := store.ListQuarantined(ctx)
	if err != nil || len(quarantined) != 1 || quarantined[0] != blob.ID {
		t.Fatalf("quarantined list = %#v, %v", quarantined, err)
	}

	if err := store.RestoreQuarantined(ctx, blob.ID); err != nil {
		t.Fatal(err)
	}

	reader, err = store.Open(ctx, blob.ID, blob.Size)
	if err != nil {
		t.Fatal(err)
	}

	readBack, err = io.ReadAll(reader)
	_ = reader.Close()

	if err != nil || !bytes.Equal(readBack, image) {
		t.Fatalf("restored read %d bytes, %v", len(readBack), err)
	}

	if quarantined, err := store.ListQuarantined(ctx); err != nil || len(quarantined) != 0 {
		t.Fatalf("quarantine after restore = %#v, %v", quarantined, err)
	}

	if err := store.Delete(ctx, blob.ID); err != nil {
		t.Fatal(err)
	}

	if err := store.Delete(ctx, blob.ID); !errors.Is(err, assets.ErrNotFound) {
		t.Fatalf("repeated delete = %v", err)
	}

	sentinel := filepath.Join(filepath.Dir(root), "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("do not touch"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, id := range []assets.ID{"", "not-an-id", "sha256:not-hex", "sha256:../../sentinel.txt"} {
		if err := store.Delete(ctx, id); err == nil {
			t.Errorf("Delete(%q) succeeded", id)
		}

		if err := store.Quarantine(ctx, id); err == nil {
			t.Errorf("Quarantine(%q) succeeded", id)
		}

		if err := store.RestoreQuarantined(ctx, id); err == nil {
			t.Errorf("RestoreQuarantined(%q) succeeded", id)
		}

		if err := store.PurgeQuarantined(ctx, id); err == nil {
			t.Errorf("PurgeQuarantined(%q) succeeded", id)
		}
	}

	content, err := os.ReadFile(sentinel)
	if err != nil || string(content) != "do not touch" {
		t.Fatalf("invalid IDs touched the sentinel: %q, %v", content, err)
	}
}

func TestRestoreQuarantinedResolvesReingestAndCorruptionSafely(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	image := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("reingest-pixels")...)

	t.Run("reingest keeps the verified final blob", func(t *testing.T) {
		store, err := files.NewAssetStore(filepath.Join(t.TempDir(), "assets"), files.DefaultMaximumAssetSize)
		if err != nil {
			t.Fatal(err)
		}

		blob, err := store.Put(ctx, bytes.NewReader(image), assets.IngestOptions{MediaType: "image/png"})
		if err != nil {
			t.Fatal(err)
		}

		if err := store.Quarantine(ctx, blob.ID); err != nil {
			t.Fatal(err)
		}

		if _, err := store.Put(ctx, bytes.NewReader(image), assets.IngestOptions{MediaType: "image/png"}); err != nil {
			t.Fatal(err)
		}

		if err := store.RestoreQuarantined(ctx, blob.ID); err != nil {
			t.Fatal(err)
		}

		reader, err := store.Open(ctx, blob.ID, blob.Size)
		if err != nil {
			t.Fatal(err)
		}

		readBack, err := io.ReadAll(reader)
		_ = reader.Close()

		if err != nil || !bytes.Equal(readBack, image) {
			t.Fatalf("reingested read %d bytes, %v", len(readBack), err)
		}

		if quarantined, err := store.ListQuarantined(ctx); err != nil || len(quarantined) != 0 {
			t.Fatalf("stale quarantine copy survived: %#v, %v", quarantined, err)
		}
	})

	t.Run("corrupt final bytes are replaced by verified quarantined bytes", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "assets")

		store, err := files.NewAssetStore(root, files.DefaultMaximumAssetSize)
		if err != nil {
			t.Fatal(err)
		}

		blob, err := store.Put(ctx, bytes.NewReader(image), assets.IngestOptions{MediaType: "image/png"})
		if err != nil {
			t.Fatal(err)
		}

		if err := store.Quarantine(ctx, blob.ID); err != nil {
			t.Fatal(err)
		}

		finalPath := filepath.Join(root, filepath.FromSlash(blob.StorageKey))
		if err := os.WriteFile(finalPath, []byte("corrupt"), 0o600); err != nil {
			t.Fatal(err)
		}

		if err := store.RestoreQuarantined(ctx, blob.ID); err != nil {
			t.Fatal(err)
		}

		reader, err := store.Open(ctx, blob.ID, blob.Size)
		if err != nil {
			t.Fatal(err)
		}

		readBack, err := io.ReadAll(reader)
		_ = reader.Close()

		if err != nil || !bytes.Equal(readBack, image) {
			t.Fatalf("restored read %d bytes, %v", len(readBack), err)
		}

		if quarantined, err := store.ListQuarantined(ctx); err != nil || len(quarantined) != 0 {
			t.Fatalf("quarantine after corruption restore = %#v, %v", quarantined, err)
		}
	})
}

func TestAssetPublicationConfinesReplacedDirectories(t *testing.T) {
	for _, directory := range []string{"sha256", ".staging"} {
		t.Run(directory, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "assets")

			store, err := files.NewAssetStore(root, 1024, files.WithFilePolicy(filepolicy.Policy{Access: filepolicy.SharedGroup}))
			if err != nil {
				t.Fatal(err)
			}

			outside := t.TempDir()
			if err := os.Chmod(outside, 0o700); err != nil { //nolint:gosec // The fixture needs an exact directory mode.
				t.Fatal(err)
			}

			if err := os.RemoveAll(filepath.Join(root, directory)); err != nil {
				t.Fatal(err)
			}

			if err := os.Symlink(outside, filepath.Join(root, directory)); err != nil {
				t.Fatal(err)
			}

			data := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("confined")...)
			if _, err := store.Put(t.Context(), bytes.NewReader(data), assets.IngestOptions{MediaType: "image/png"}); err == nil {
				t.Fatal("publication followed external symlink")
			}

			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("external writes: %v %v", entries, err)
			}

			info, err := os.Stat(outside)
			if err != nil || info.Mode().Perm() != 0o700 {
				t.Fatalf("external permissions changed: %v", err)
			}
		})
	}
}

func TestAssetPublicationKeepsItsOpenedRootAfterReplacement(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "assets")

	store, err := files.NewAssetStore(root, 1024, files.WithFilePolicy(filepolicy.Policy{Access: filepolicy.SharedGroup}))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	original := filepath.Join(parent, "original-assets")
	if err := os.Rename(root, original); err != nil {
		t.Fatal(err)
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}

	data := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("confined root")...)

	blob, err := store.Put(t.Context(), bytes.NewReader(data), assets.IngestOptions{MediaType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(original, blob.StorageKey)); err != nil {
		t.Fatalf("original root lacks upload: %v", err)
	}

	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("external root was written: %v %v", entries, err)
	}
}
