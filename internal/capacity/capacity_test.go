package capacity

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/storagebudget"
)

func TestBackupReserveAndInventory(t *testing.T) {
	for _, tc := range []struct {
		available uint64
		fail      bool
	}{{0, true}, {ReserveBytes - 1, true}, {ReserveBytes + 99, true}, {ReserveBytes + 100, false}} {
		if err := CheckBackupSpace(100, tc.available); (err != nil) != tc.fail {
			t.Fatalf("available %d: %v", tc.available, err)
		}
	}

	dir := t.TempDir()
	db := filepath.Join(dir, "library.db")

	store, err := sqlite.Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	assets := filepath.Join(dir, "assets")
	for name, data := range map[string]string{"original": "original bytes", ".staging/upload": "staged", ".quarantine/bad": "bad"} {
		path := filepath.Join(assets, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	report, err := Inspect(context.Background(), store, db, assets, dir)
	if err != nil {
		t.Fatal(err)
	}

	footprint, err := store.StorageFootprint(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if footprint.DatabaseBytes != unsigned(report.DatabaseBytes) || footprint.OriginalBytes != 0 || footprint.OriginalFiles != 0 {
		t.Fatalf("scalar footprint differs from inventory: %+v", footprint)
	}

	if report.OriginalFilesBytes != 14 || report.StagingBytes != 6 || report.QuarantineBytes != 3 || report.DerivativeDiskBytes != 0 || report.DerivativeMemoryBudgetBytes != 32<<20 {
		t.Fatalf("inventory: %+v", report)
	}

	if report.BackupWorkspaceBytes <= unsigned(report.DatabaseBytes)*2 {
		t.Fatalf("snapshot + uncompressed backup not reserved: %+v", report)
	}

	if _, err := os.Stat(filepath.Join(assets, ".staging/upload")); err != nil {
		t.Fatal("inspection changed staging", err)
	}

	if err := os.Symlink(db, filepath.Join(assets, "link")); err != nil {
		t.Fatal(err)
	}

	if _, err := Inspect(context.Background(), store, db, assets, dir); err == nil {
		t.Fatal("inventory followed symlink")
	}
}

func TestBackupCapacityArithmeticCannotWrap(t *testing.T) {
	archive, workspace := BackupRequirements(sqlite.StorageUsage{DatabaseBytes: math.MaxInt64, CurrentAssetBytes: math.MaxInt64, RetainedOnlyAssetBytes: math.MaxInt64, StoredAssets: math.MaxInt64})
	if archive != math.MaxUint64 || workspace != math.MaxUint64 {
		t.Fatal("overflow reduced backup requirements", archive, workspace)
	}
}

func TestLargeDatabaseBackupNeedsMoreThanEightGiB(t *testing.T) {
	_, workspace := BackupRequirements(sqlite.StorageUsage{DatabaseBytes: 10 << 30})
	if err := CheckBackupSpace(workspace, 8<<30); err == nil {
		t.Fatal("backup admitted without room for snapshot and archive")
	}

	if err := CheckBackupSpace(workspace, workspace+ReserveBytes); err != nil {
		t.Fatal(err)
	}
}

func TestBackupAdmissionIsIndependentOfGeneralWarning(t *testing.T) {
	usage := sqlite.StorageUsage{DatabaseBytes: 20 << 30}
	space := Filesystem{TotalBytes: 100 << 30, AvailableBytes: 31 << 30}

	state, _ := storagebudget.PhysicalState(space, footprintForUsage(usage), false)
	if state != "normal" {
		t.Fatalf("general pressure state=%s", state)
	}

	_, workspace := BackupRequirements(usage)
	if err := CheckBackupSpace(workspace, space.AvailableBytes); err == nil {
		t.Fatal("normal general pressure admitted a backup without enough workspace")
	}
}
