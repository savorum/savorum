// Package capacity accounts for permanent history without changing retention.
package capacity

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/storagebudget"
)

// ReserveBytes is the storage capacity retained for safe operation.
const ReserveBytes uint64 = diskspace.ReserveBytes

// Filesystem reports available space for one filesystem.
type Filesystem = diskspace.Space

// Report summarizes storage capacity across Savorum's filesystems.
type Report struct {
	sqlite.StorageUsage
	WALBytes                    int64      `json:"wal_bytes"`
	OriginalFilesBytes          int64      `json:"original_files_bytes"`
	StagingBytes                int64      `json:"staging_bytes"`
	QuarantineBytes             int64      `json:"quarantine_bytes"`
	DerivativeDiskBytes         int64      `json:"derivative_disk_bytes"`
	DerivativeMemoryBudgetBytes int64      `json:"derivative_memory_budget_bytes"`
	BackupUpperBoundBytes       uint64     `json:"backup_upper_bound_bytes"`
	BackupWorkspaceBytes        uint64     `json:"backup_workspace_bytes"`
	DatabaseSpace               Filesystem `json:"database_filesystem"`
	AssetSpace                  Filesystem `json:"asset_filesystem"`
	BackupSpace                 Filesystem `json:"backup_filesystem"`
	Warnings                    []string   `json:"warnings"`
}

// Space reports capacity details for the filesystem containing path.
func Space(path string) (Filesystem, error) { return diskspace.Inspect(path) }

// Inspect reports capacity details for the configured Savorum storage paths.
func Inspect(ctx context.Context, store *sqlite.Store, databasePath, assetRoot, backupDirectory string) (Report, error) {
	var (
		r   Report
		err error
	)

	r.StorageUsage, err = store.StorageUsage(ctx)
	if err != nil {
		return r, err
	}

	r.DerivativeMemoryBudgetBytes = 32 << 20 // Derivatives are disposable, bounded RAM only.
	if info, e := os.Stat(databasePath + "-wal"); e == nil {
		r.WALBytes = info.Size()
	} else if !errors.Is(e, os.ErrNotExist) {
		return r, fmt.Errorf("inspect database WAL: %w", e)
	}

	if err := inventoryAssetFiles(ctx, assetRoot, &r); err != nil {
		return r, err
	}

	originals := r.CurrentAssetBytes + r.RetainedOnlyAssetBytes + r.OrphanAssetBytes
	r.BackupUpperBoundBytes, r.BackupWorkspaceBytes = BackupRequirements(r.StorageUsage)

	r.DatabaseSpace, err = Space(filepath.Dir(databasePath))
	if err != nil {
		return r, err
	}

	r.AssetSpace, err = Space(assetRoot)
	if err != nil {
		return r, err
	}

	r.BackupSpace, err = Space(backupDirectory)
	if err != nil {
		return r, err
	}

	for _, volume := range []struct {
		name  string
		space Filesystem
	}{{"database", r.DatabaseSpace}, {"assets", r.AssetSpace}} {
		state, _ := storagebudget.PhysicalState(volume.space, footprintForUsage(r.StorageUsage), false)
		switch state {
		case "limited":
			r.Warnings = append(r.Warnings, volume.name+" filesystem is at its safety reserve")
		case "warning":
			r.Warnings = append(r.Warnings, volume.name+" filesystem is running low on space relative to retained data")
		case "unknown":
			r.Warnings = append(r.Warnings, volume.name+" filesystem capacity is unavailable")
		}
	}

	if err := CheckBackupSpace(r.BackupWorkspaceBytes, r.BackupSpace.AvailableBytes); err != nil {
		r.Warnings = append(r.Warnings, err.Error())
	}

	if r.OriginalFilesBytes < originals {
		r.Warnings = append(r.Warnings, "tracked originals exceed files on disk; run scrub-assets to inspect missing content")
	}

	return r, nil
}

func inventoryAssetFiles(ctx context.Context, assetRoot string, r *Report) error {
	err := filepath.WalkDir(assetRoot, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			if errors.Is(e, os.ErrNotExist) && path == assetRoot {
				return nil
			}

			return fmt.Errorf("inspect asset inventory entry: %w", e)
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("asset inventory contains a symlink: %s", path)
		}

		if d.IsDir() {
			return nil
		}

		info, e := d.Info()
		if e != nil {
			return fmt.Errorf("inspect asset inventory entry: %w", e)
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		rel, _ := filepath.Rel(assetRoot, path)
		switch {
		case strings.HasPrefix(rel, ".staging"+string(os.PathSeparator)):
			r.StagingBytes += info.Size()
		case strings.HasPrefix(rel, ".quarantine"+string(os.PathSeparator)):
			r.QuarantineBytes += info.Size()
		default:
			r.OriginalFilesBytes += info.Size()
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("inventory asset storage: %w", err)
	}

	return nil
}

// CheckBackupSpace verifies that available storage can accommodate a backup.
func CheckBackupSpace(workspace, available uint64) error {
	if available < ReserveBytes || workspace > available-ReserveBytes {
		return fmt.Errorf("backup needs %d bytes of staging space plus a %d-byte free-space reserve; %d bytes available", workspace, ReserveBytes, available)
	}

	return nil
}

// BackupRequirements reserves an uncompressed archive, ZIP overhead and the
// simultaneous coherent database snapshot. Compression is never required to fit.
func BackupRequirements(u sqlite.StorageUsage) (archive, workspace uint64) {
	return footprintForUsage(u).BackupRequirements()
}

func footprintForUsage(u sqlite.StorageUsage) diskspace.Footprint {
	return diskspace.Footprint{
		DatabaseBytes: unsigned(u.DatabaseBytes),
		OriginalBytes: capacitySum(unsigned(u.CurrentAssetBytes), unsigned(u.RetainedOnlyAssetBytes), unsigned(u.OrphanAssetBytes)),
		OriginalFiles: unsigned(u.StoredAssets),
	}
}

// unsigned converts a counted quantity for the unsigned arithmetic the capacity
// figures are stated in. Every caller passes a SQL COUNT or SUM, which cannot be
// negative; clamping makes that an enforced property rather than an assumption
// that would wrap into an enormous requirement if it ever stopped holding.
func unsigned(value int64) uint64 {
	return uint64(max(value, 0))
}

func capacitySum(values ...uint64) uint64 {
	var sum uint64
	for _, value := range values {
		if value > math.MaxUint64-sum {
			return math.MaxUint64
		}

		sum += value
	}

	return sum
}
