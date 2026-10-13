package administration

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"

	"github.com/savorum/savorum/internal/storage/sqlite"
)

// DiskCapacity reports filesystem capacity with optional instance identity.
type DiskCapacity struct {
	Version               int              `json:"version"`
	TenantID              string           `json:"tenant_id"`
	InstanceID            string           `json:"instance_id"`
	MountPath             string           `json:"mount_path"`
	TotalBytes            uint64           `json:"total_bytes"`
	DeviceBytes           uint64           `json:"device_bytes,omitempty"`
	FilesystemExtentBytes uint64           `json:"filesystem_extent_bytes,omitempty"`
	FilesystemBlockBytes  uint64           `json:"filesystem_block_bytes,omitempty"`
	FilesystemType        string           `json:"filesystem_type,omitempty"`
	DiskUsage             *FilesystemUsage `json:"disk_usage,omitempty"`
}

// FilesystemUsage reports currently available bytes and inodes.
type FilesystemUsage struct {
	AvailableBytes  uint64 `json:"available_bytes"`
	TotalInodes     uint64 `json:"total_inodes"`
	AvailableInodes uint64 `json:"available_inodes"`
}

// ReadDiskCapacity reads identity without migration and observes the selected filesystem.
func ReadDiskCapacity(ctx context.Context, databasePath, mount string, probe func() (uint64, error)) (DiskCapacity, error) {
	identity, err := readResourceIdentity(ctx, databasePath)
	if err != nil {
		return DiskCapacity{}, err
	}

	if databasePath != "" && !validResourceIdentity(identity) {
		return DiskCapacity{}, errors.New("selected identity is missing or invalid")
	}

	total, err := probe()
	if err != nil {
		return DiskCapacity{}, err
	}

	return DiskCapacity{Version: 1, TenantID: identity.TenantID, InstanceID: identity.InstanceID, MountPath: mount, TotalBytes: total}, nil
}

// ProbeFilesystem rejects links and optionally requires a filesystem separate from root.
func ProbeFilesystem(dataDirectory string, distinct bool) (uint64, error) {
	info, err := os.Lstat(dataDirectory)
	if err != nil {
		return 0, fmt.Errorf("inspect selected data directory: %w", err)
	}

	if !info.IsDir() {
		return 0, errors.New("selected data path must be a directory")
	}

	absolute, err := filepath.Abs(dataDirectory)
	if err != nil {
		return 0, fmt.Errorf("resolve filesystem directory: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil || resolved != absolute {
		return 0, errors.New("filesystem directory cannot traverse symlinks")
	}

	rootInfo, err := os.Lstat("/")
	if err != nil {
		return 0, fmt.Errorf("inspect root filesystem: %w", err)
	}

	dataStat, dataOK := info.Sys().(*syscall.Stat_t)

	rootStat, rootOK := rootInfo.Sys().(*syscall.Stat_t)
	if !dataOK || !rootOK || distinct && dataStat.Dev == rootStat.Dev {
		return 0, errors.New("selected data directory is not a distinct mounted filesystem")
	}

	var stats syscall.Statfs_t

	if err := syscall.Statfs(dataDirectory, &stats); err != nil {
		return 0, fmt.Errorf("read selected filesystem capacity: %w", err)
	}

	total, err := filesystemTotalBytes(stats.Blocks, filesystemBlockSize(stats))
	if err != nil {
		return 0, err
	}

	return total, nil
}

func validResourceIdentity(identity sqlite.HostingIdentity) bool {
	return opaqueIdentity(identity.TenantID) && opaqueIdentity(identity.InstanceID)
}

func readResourceIdentity(ctx context.Context, database string) (sqlite.HostingIdentity, error) {
	if database == "" {
		return sqlite.HostingIdentity{}, nil
	}

	return sqlite.ReadHostingIdentity(ctx, database)
}

func filesystemTotalBytes(blocks uint64, blockSize int64) (uint64, error) {
	if blocks == 0 || blockSize <= 0 || blocks > math.MaxUint64/uint64(blockSize) {
		return 0, errors.New("selected filesystem reported invalid or overflowing capacity")
	}

	return blocks * uint64(blockSize), nil
}
