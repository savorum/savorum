//go:build linux

package administration

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ProbeGeometry observes the exact mounted ext4 block device without changing permissions.
func ProbeGeometry(mount string) (Geometry, error) {
	total, err := ProbeFilesystem(mount, true)
	if err != nil {
		return Geometry{}, err
	}

	dataInfo, err := os.Lstat(mount)
	if err != nil {
		return Geometry{}, fmt.Errorf("inspect selected mount: %w", err)
	}

	dataStat, ok := dataInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return Geometry{}, errors.New("selected mount device identity unavailable")
	}

	mounts, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return Geometry{}, fmt.Errorf("read selected mount metadata: %w", err)
	}

	source, err := mountDevice(string(mounts), fmt.Sprintf("%d:%d", unix.Major(dataStat.Dev), unix.Minor(dataStat.Dev)), mount)
	if err != nil {
		return Geometry{}, err
	}

	fd, err := unix.Open(source, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return Geometry{}, errors.New("read-only selected block-device metadata access unavailable")
	}

	device := os.NewFile(uintptr(fd), "hosted-block-device")
	defer device.Close()

	info, err := device.Stat()
	if err != nil {
		return Geometry{}, fmt.Errorf("inspect selected block device: %w", err)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeDevice == 0 || info.Mode()&os.ModeCharDevice != 0 || stat.Rdev != dataStat.Dev {
		return Geometry{}, errors.New("selected block device does not match mounted filesystem")
	}

	var deviceBytes uint64
	// The Linux ioctl writes exactly one uint64; no device data is returned.
	// Fixed BLKGETSIZE64 ioctl writes exactly one uint64 into this live local.
	//nolint:gosec // Audited kernel ABI pointer, no arbitrary address or ioctl.
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.BLKGETSIZE64), uintptr(unsafe.Pointer(&deviceBytes))); errno != 0 {
		return Geometry{}, fmt.Errorf("read selected device capacity: %w", errno)
	}

	extent, block, err := ext4FilesystemExtent(device)
	if err != nil {
		return Geometry{}, err
	}

	laterTotal, err := ProbeFilesystem(mount, true)
	if err != nil {
		return Geometry{}, err
	}

	laterInfo, err := os.Lstat(mount)
	if err != nil {
		return Geometry{}, fmt.Errorf("recheck selected mount: %w", err)
	}

	laterStat, ok := laterInfo.Sys().(*syscall.Stat_t)

	laterExtent, laterBlock, err := ext4FilesystemExtent(device)
	if err != nil {
		return Geometry{}, err
	}

	if !ok || laterStat.Dev != dataStat.Dev || laterTotal != total || laterExtent != extent || laterBlock != block {
		return Geometry{}, errors.New("selected filesystem changed during observation")
	}

	var usage syscall.Statfs_t
	if err := syscall.Statfs(mount, &usage); err != nil {
		return Geometry{}, fmt.Errorf("read selected filesystem usage: %w", err)
	}

	usageTotal, err := filesystemTotalBytes(usage.Blocks, filesystemBlockSize(usage))
	if err != nil || usageTotal != total || usage.Bavail > usage.Blocks || usage.Ffree > usage.Files {
		return Geometry{}, errors.New("selected filesystem changed during usage observation")
	}

	availableBytes, err := filesystemTotalBytes(usage.Bavail, filesystemBlockSize(usage))
	if err != nil {
		return Geometry{}, err
	}

	return Geometry{DiskUsage: &FilesystemUsage{AvailableBytes: availableBytes, TotalInodes: usage.Files, AvailableInodes: usage.Ffree}, TotalBytes: total, DeviceBytes: deviceBytes, ExtentBytes: extent, BlockBytes: block}, nil
}
