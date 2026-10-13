package administration

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestExt4FilesystemExtentIncludesMetadata(t *testing.T) {
	data := make([]byte, 2048)
	super := data[1024:]
	binary.LittleEndian.PutUint16(super[0x38:], 0xef53)
	binary.LittleEndian.PutUint32(super[0x60:], 0x40)
	binary.LittleEndian.PutUint32(super[0x18:], 2)
	binary.LittleEndian.PutUint32(super[0x04:], 524288)

	extent, block, err := ext4FilesystemExtent(bytes.NewReader(data))
	if err != nil || extent != 2<<30 || block != 4096 {
		t.Fatalf("extent=%d block=%d err=%v", extent, block, err)
	}

	for _, scenario := range []string{"magic", "not-ext4", "block-size", "zero-blocks", "overflow", "short"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := bytes.Clone(data)
			super := fixture[1024:]

			switch scenario {
			case "magic":
				binary.LittleEndian.PutUint16(super[0x38:], 0)
			case "not-ext4":
				binary.LittleEndian.PutUint32(super[0x60:], 0)
			case "block-size":
				binary.LittleEndian.PutUint32(super[0x18:], 7)
			case "zero-blocks":
				binary.LittleEndian.PutUint32(super[0x04:], 0)
			case "overflow":
				binary.LittleEndian.PutUint32(super[0x60:], 0xc0)
				binary.LittleEndian.PutUint32(super[0x150:], math.MaxUint32)
				binary.LittleEndian.PutUint32(super[0x04:], math.MaxUint32)
			case "short":
				fixture = fixture[:1500]
			}

			if _, _, err := ext4FilesystemExtent(bytes.NewReader(fixture)); err == nil {
				t.Fatal("invalid geometry accepted")
			}
		})
	}
}

func TestHostedMountDeviceRequiresExactDistinctExt4Device(t *testing.T) {
	good := "36 35 252:1 / /data rw,noatime - ext4 /dev/vdb rw\n"

	source, err := mountDevice(good, "252:1", "/data")
	if err != nil || source != "/dev/vdb" {
		t.Fatalf("source=%q err=%v", source, err)
	}

	for _, raw := range []string{good + good, "36 35 252:2 / /data rw - ext4 /dev/vdb rw", "36 35 252:1 /subdir /data rw - ext4 /dev/vdb rw", "36 35 252:1 / /data rw - tmpfs none rw", "36 35 252:1 / /data rw - ext4 /etc/shadow rw", "36 35 252:1 / /other rw - ext4 /dev/vdb rw"} {
		if _, err := mountDevice(raw, "252:1", "/data"); err == nil {
			t.Fatalf("unsafe mount accepted: %q", raw)
		}
	}
}
