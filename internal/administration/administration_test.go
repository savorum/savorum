package administration

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/storage/filepolicy"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

func TestIndependentAdministrationSnapshotAndMaintenance(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	database, assets := filepath.Join(root, "local.db"), filepath.Join(root, "originals")
	if err := os.Mkdir(assets, 0o700); err != nil {
		t.Fatal(err)
	}

	store, err := sqlite.Open(t.Context(), database)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	instance := Instance{Database: database, Assets: assets, Policy: filepolicy.OwnerPolicy()}

	output := filepath.Join(root, "snapshot")
	if _, err := instance.Snapshot(t.Context(), output, time.Now(), 8<<30); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(output, "native", "manifest.json")); err != nil {
		t.Fatal(err)
	}

	if _, err := instance.Snapshot(t.Context(), output, time.Now(), 8<<30); err == nil {
		t.Fatal("existing snapshot replaced")
	}

	reopened, closeStore, err := instance.OpenExisting(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore()

	if err := reopened.IntegrityCheck(t.Context()); err != nil {
		t.Fatal(err)
	}

	if _, _, err := instance.OpenExisting(t.Context()); err == nil {
		t.Fatal("concurrent offline writer admitted")
	}

	report, err := ReadDiskCapacity(t.Context(), "", root, func() (uint64, error) { return ProbeFilesystem(root, false) })
	if err != nil || report.TotalBytes == 0 || report.MountPath != root || report.TenantID != "" {
		t.Fatalf("local report: %+v %v", report, err)
	}
}
