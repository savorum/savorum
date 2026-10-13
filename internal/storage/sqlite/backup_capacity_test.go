package sqlite

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/assets"
)

func TestSnapshotRejectsDatabaseGrowthAfterCapacityMeasurement(t *testing.T) {
	root := t.TempDir()

	store, err := Open(t.Context(), filepath.Join(root, "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	usage, err := store.StorageUsage(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(t.Context(), "CREATE TABLE snapshot_growth(payload BLOB); INSERT INTO snapshot_growth VALUES(zeroblob(1048576))"); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(root, "snapshot.db")
	called := false

	err = store.CreateBoundedSnapshot(t.Context(), destination, usage.DatabaseBytes, 1, func(int, []assets.Blob) error { called = true; return nil })
	if err == nil || called {
		t.Fatalf("growth reached copy or callback: %v, %v", err, called)
	}

	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot wrote beyond measured bound: %v", err)
	}
}
