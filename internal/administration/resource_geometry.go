package administration

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Geometry holds read-only block-device and filesystem measurements.
type Geometry struct {
	TotalBytes  uint64
	DeviceBytes uint64
	ExtentBytes uint64
	BlockBytes  uint64
	DiskUsage   *FilesystemUsage
}

var blockDevicePath = regexp.MustCompile(`^/dev/[a-zA-Z0-9_-]+$`)

func mountDevice(raw, device, mount string) (string, error) {
	if len(raw) > 1<<20 {
		return "", errors.New("selected mount metadata exceeds limit")
	}

	source := ""

	for line := range strings.SplitSeq(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 || fields[4] != mount {
			continue
		}

		if source != "" || fields[2] != device || fields[3] != "/" {
			return "", errors.New("selected mount identity is ambiguous")
		}

		split := -1

		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				split = i
				break
			}
		}

		if split < 0 || len(fields) != split+4 || fields[split+1] != "ext4" || !blockDevicePath.MatchString(fields[split+2]) {
			return "", errors.New("selected geometry requires an exact ext4 block-device mount")
		}

		source = fields[split+2]
	}

	if source == "" {
		return "", errors.New("selected data mount is unavailable")
	}

	return source, nil
}

func ext4FilesystemExtent(reader io.ReaderAt) (uint64, uint64, error) {
	var super [1024]byte
	if _, err := reader.ReadAt(super[:], 1024); err != nil {
		return 0, 0, fmt.Errorf("read selected filesystem metadata: %w", err)
	}

	if binary.LittleEndian.Uint16(super[0x38:]) != 0xef53 || binary.LittleEndian.Uint32(super[0x60:])&0x40 == 0 {
		return 0, 0, errors.New("selected filesystem is not supported ext4")
	}

	logBlock := binary.LittleEndian.Uint32(super[0x18:])
	if logBlock > 6 {
		return 0, 0, errors.New("selected filesystem block size is invalid")
	}

	blockBytes := uint64(1024) << logBlock

	blocks := uint64(binary.LittleEndian.Uint32(super[0x04:]))
	if binary.LittleEndian.Uint32(super[0x60:])&0x80 != 0 {
		blocks |= uint64(binary.LittleEndian.Uint32(super[0x150:])) << 32
	}

	extent, err := filesystemTotalBytes(blocks, int64(blockBytes))

	return extent, blockBytes, err
}

// ReadDiskGeometry validates extent, device, and usable filesystem observations.
func ReadDiskGeometry(ctx context.Context, database, mount string, probe func() (Geometry, error)) (DiskCapacity, error) {
	identity, err := readResourceIdentity(ctx, database)
	if err != nil {
		return DiskCapacity{}, err
	}

	if database != "" && !validResourceIdentity(identity) {
		return DiskCapacity{}, errors.New("selected identity is missing or invalid")
	}

	geometry, err := probe()
	if err != nil {
		return DiskCapacity{}, err
	}

	if geometry.TotalBytes == 0 || geometry.DeviceBytes == 0 || geometry.ExtentBytes == 0 || geometry.BlockBytes == 0 || geometry.TotalBytes > geometry.ExtentBytes || geometry.ExtentBytes > geometry.DeviceBytes {
		return DiskCapacity{}, errors.New("selected filesystem geometry is invalid")
	}

	if geometry.DiskUsage != nil && (geometry.DiskUsage.AvailableBytes > geometry.TotalBytes || geometry.DiskUsage.AvailableInodes > geometry.DiskUsage.TotalInodes) {
		return DiskCapacity{}, errors.New("selected filesystem usage is invalid")
	}

	return DiskCapacity{Version: 2, TenantID: identity.TenantID, InstanceID: identity.InstanceID, MountPath: mount, TotalBytes: geometry.TotalBytes, DeviceBytes: geometry.DeviceBytes, FilesystemExtentBytes: geometry.ExtentBytes, FilesystemBlockBytes: geometry.BlockBytes, FilesystemType: "ext4", DiskUsage: geometry.DiskUsage}, nil
}
