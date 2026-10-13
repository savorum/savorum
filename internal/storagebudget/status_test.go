package storagebudget

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/diskspace"
)

func TestStatusMeasuresCombinedUsageAndWarningHysteresis(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sha256"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "sha256", "file"), make([]byte, 25), 0o600); err != nil {
		t.Fatal(err)
	}

	c := New(root, 1000<<20)
	c.statusInspect = func(string) (diskspace.Space, error) {
		return diskspace.Space{TotalBytes: 10 << 30, AvailableBytes: 9 << 30}, nil
	}
	usage := int64(750<<20) - 25

	c.SetDatabaseUsage(func(context.Context) (int64, error) { return usage, nil })

	report := c.Status(t.Context())
	if report.State != "warning" || report.UsedBytes == nil || *report.UsedBytes != 750<<20 || report.AvailableBytes == nil {
		t.Fatalf("status=%+v", report)
	}

	usage = (710 << 20) - 25 // 71% remains a display warning, with writes unaffected.

	if report = c.Status(t.Context()); report.State != "warning" {
		t.Fatalf("71%% status=%+v", report)
	}

	usage = (699 << 20) - 25

	if report = c.Status(t.Context()); report.State != "normal" {
		t.Fatalf("69.9%% status=%+v", report)
	}

	usage = (1000 << 20)

	if report = c.Status(t.Context()); report.State != "limited" || *report.UsedBytes != (1000<<20)+25 {
		t.Fatalf("over limit status=%+v", report)
	}

	c.SetDatabaseUsage(func(context.Context) (int64, error) { return 0, errors.New("unavailable") })

	if report = c.Status(t.Context()); report.State != "unknown" || report.UsedBytes != nil {
		t.Fatalf("unknown status=%+v", report)
	}
}

func TestStatusCancelledLeaseIsUnknown(t *testing.T) {
	c := New(t.TempDir(), testDurableLimit)

	_, release, err := c.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if report := c.Status(ctx); report.State != "unknown" || report.UsedBytes != nil {
		t.Fatalf("status=%+v", report)
	}
}

func TestStatusReportsMetadataReserveGapAsLimited(t *testing.T) {
	c := New(t.TempDir(), testDurableLimit)
	c.statusInspect = func(string) (diskspace.Space, error) {
		return diskspace.Space{TotalBytes: 10 << 30, AvailableBytes: 9 << 30}, nil
	}
	usage := testDurableLimit - ManifestAllowance - 1

	c.SetDatabaseUsage(func(context.Context) (int64, error) { return usage, nil })

	if report := c.Status(t.Context()); report.State != "warning" {
		t.Fatalf("before reserve status=%+v", report)
	}

	usage++

	report := c.Status(t.Context())
	if report.State != "limited" || report.LimitBytes == nil || *report.LimitBytes != testDurableLimit || *report.UsedBytes != usage {
		t.Fatalf("reserve status=%+v", report)
	}
}

func TestStatusUsesWeightedPhysicalPressure(t *testing.T) {
	c := New(t.TempDir(), 200<<30)
	usage := int64(20 << 30)
	free := uint64(25 << 30)

	c.SetDatabaseUsage(func(context.Context) (int64, error) { return usage, nil })
	c.statusInspect = func(string) (diskspace.Space, error) {
		return diskspace.Space{TotalBytes: 100 << 30, AvailableBytes: free}, nil
	}

	for _, tc := range []struct {
		database int64
		free     uint64
		state    string
	}{
		{20 << 30, 25 << 30, "normal"},
		{10 << 30, 8 << 30, "warning"},
		{10 << 30, 15 << 30, "warning"},
		{10 << 30, 18 << 30, "normal"},
		{10 << 30, 15 << 30, "normal"},
		{10 << 30, diskspace.ReserveBytes, "limited"},
		{10 << 30, 18 << 30, "normal"},
	} {
		usage, free = tc.database, tc.free

		report := c.Status(t.Context())
		if report.State != tc.state || report.UsedBytes == nil || *report.UsedBytes != usage || report.AvailableBytes == nil || *report.AvailableBytes != free {
			t.Fatalf("database=%d free=%d status=%+v, want %s", usage, free, report, tc.state)
		}
	}
}
