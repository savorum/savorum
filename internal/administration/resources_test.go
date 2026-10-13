package administration

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

func TestHostedDiskCapacityReadsOnlyIdentityAndTotalBytes(t *testing.T) {
	directory := t.TempDir()
	database := filepath.Join(directory, "savorum.db")

	store, err := sqlite.Open(t.Context(), database)
	if err != nil {
		t.Fatal(err)
	}

	bootstrap := hosting.Bootstrap{
		OperationID: "boot_1", TenantID: "hh_one", PublicURL: "https://one.example.test", ProviderID: "cloud",
		Issuer: "https://cloud.example.test", Owner: hosting.Owner{Subject: "acct_owner", DisplayName: "Alex"}, LibraryName: "Recipes",
	}

	receipt, err := store.BootstrapHosting(t.Context(), bootstrap)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	report, err := ReadDiskCapacity(t.Context(), database, "/data", func() (uint64, error) { return 4096, nil })
	if err != nil {
		t.Fatal(err)
	}

	if report.Version != 1 || report.TenantID != bootstrap.TenantID || report.InstanceID != receipt.InstanceID || report.MountPath != "/data" || report.TotalBytes != 4096 {
		t.Fatalf("unexpected capacity report: %+v", report)
	}

	geometryReport, err := ReadDiskGeometry(t.Context(), database, "/data", func() (Geometry, error) {
		return Geometry{TotalBytes: 2092908544, DeviceBytes: 2 << 30, ExtentBytes: 2 << 30, BlockBytes: 4096}, nil
	})
	if err != nil || geometryReport.Version != 2 || geometryReport.TotalBytes >= geometryReport.DeviceBytes || geometryReport.FilesystemExtentBytes != geometryReport.DeviceBytes || geometryReport.FilesystemType != "ext4" || geometryReport.TenantID != bootstrap.TenantID || geometryReport.InstanceID != receipt.InstanceID {
		t.Fatalf("geometry report %+v err=%v", geometryReport, err)
	}

	for _, usage := range []*FilesystemUsage{
		{AvailableBytes: 512, TotalInodes: 2048, AvailableInodes: 1024},
		{AvailableBytes: 2092908545},
		{TotalInodes: 1, AvailableInodes: 2},
	} {
		got, e := ReadDiskGeometry(t.Context(), database, "/data", func() (Geometry, error) {
			return Geometry{TotalBytes: 2092908544, DeviceBytes: 2 << 30, ExtentBytes: 2 << 30, BlockBytes: 4096, DiskUsage: usage}, nil
		})

		valid := usage.AvailableBytes <= 2092908544 && usage.AvailableInodes <= usage.TotalInodes
		if valid && (e != nil || got.DiskUsage != usage) {
			t.Fatalf("usage not exposed: %+v %v", got, e)
		}

		if !valid && e == nil {
			t.Fatal("invalid usage accepted")
		}
	}

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]any

	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}

	if len(fields) != 5 || fields["total_bytes"] == nil || fields["available_bytes"] != nil || strings.Contains(string(encoded), "acct_owner") {
		t.Fatalf("unexpected output fields: %s", encoded)
	}
}

func TestHostedDiskCapacityRejectsInvalidDirectory(t *testing.T) {
	directory := t.TempDir()

	link := filepath.Join(directory, "linked-data")

	if err := os.Symlink(directory, link); err != nil {
		t.Fatal(err)
	}

	if _, err := ProbeFilesystem(link, true); err == nil || !strings.Contains(err.Error(), "must be a directory") {
		t.Fatalf("symlink directory accepted: %v", err)
	}

	if _, err := ProbeFilesystem("/", true); err == nil || !strings.Contains(err.Error(), "not a distinct mounted filesystem") {
		t.Fatalf("root filesystem directory accepted: %v", err)
	}
}

func TestFilesystemTotalBytesRejectsInvalidAndOverflowingValues(t *testing.T) {
	for _, test := range []struct {
		blocks uint64
		size   int64
		want   uint64
		ok     bool
	}{
		{1024, 4096, 4194304, true},
		{0, 4096, 0, false},
		{1, 0, 0, false},
		{1, -1, 0, false},
		{math.MaxUint64, 2, 0, false},
	} {
		got, err := filesystemTotalBytes(test.blocks, test.size)
		if got != test.want || (err == nil) != test.ok {
			t.Fatalf("filesystemTotalBytes(%d, %d) = %d, %v", test.blocks, test.size, got, err)
		}
	}
}
