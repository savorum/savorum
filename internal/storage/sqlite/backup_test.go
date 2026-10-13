package sqlite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/assets"
)

func TestBoundedSnapshotRejectsAssetCountBeforeCopyAndCallback(t *testing.T) {
	root := t.TempDir()

	store, err := Open(t.Context(), filepath.Join(root, "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for index := range 3 {
		hash := fmt.Sprintf("%064x", index)
		if _, err := store.db.ExecContext(t.Context(), `INSERT INTO assets(id,content_hash,media_type,size,storage_key,created_at) VALUES(?,?,?,?,?,?)`, "sha256:"+hash, hash, "image/png", 1, "sha256/"+hash[:2]+"/"+hash[2:4]+"/"+hash, time.Now().Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}

	destination := filepath.Join(root, "snapshot.db")
	called := false

	err = store.CreateBoundedSnapshot(t.Context(), destination, 16<<20, 2, func(int, []assets.Blob) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatal("excess asset rows reached copy/callback", err, called)
	}

	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("database copied before enforcing asset bound", err)
	}

	if err := store.CreateBoundedSnapshot(t.Context(), destination, 16<<20, 3, func(_ int, blobs []assets.Blob) error {
		if len(blobs) != 3 {
			return fmt.Errorf("accepted snapshot has %d blobs", len(blobs))
		}

		return nil
	}); err != nil {
		t.Fatal("exact bound or writer fence release failed", err)
	}
}
