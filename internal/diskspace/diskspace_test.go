package diskspace

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestReservationsShareDeviceAndReleaseOnce(t *testing.T) {
	m := NewManager()
	m.inspect = func(string) (Space, error) {
		return Space{Device: 1, AvailableBytes: 100, AvailableInodes: 40, TotalInodes: 100}, nil
	}

	release, err := m.Reserve("assets", 60, 4, 10)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := m.Reserve("backups", 31, 1, 10); !errors.Is(err, ErrCapacity) {
		t.Fatalf("overlapping bytes accepted: %v", err)
	}

	if _, err := m.Reserve("backups", 1, 5, 10); !errors.Is(err, ErrCapacity) {
		t.Fatalf("overlapping inodes accepted: %v", err)
	}

	release()
	release()

	second, err := m.Reserve("backups", 90, 8, 10)
	if err != nil {
		t.Fatal(err)
	}

	second()
}

func TestConcurrentReservationsRespectUpperBound(t *testing.T) {
	m := NewManager()
	m.inspect = func(string) (Space, error) { return Space{Device: 1, AvailableBytes: 100}, nil }

	var (
		successes atomic.Int32
		group     sync.WaitGroup
	)

	releases := make(chan func(), 20)

	for range 20 {
		group.Go(func() {
			release, err := m.Reserve("shared", 10, 0, 0)
			if err == nil {
				successes.Add(1)

				releases <- release
			}
		})
	}

	group.Wait()
	close(releases)

	if successes.Load() != 10 {
		t.Fatalf("admitted %d reservations", successes.Load())
	}

	for release := range releases {
		release()
	}
}

func TestDifferentFilesystemsAndOverflow(t *testing.T) {
	m := NewManager()
	m.inspect = func(path string) (Space, error) {
		device := uint64(1)
		if path == "other" {
			device = 2
		}

		return Space{Device: device, AvailableBytes: ^uint64(0)}, nil
	}

	release, err := m.Reserve("first", ^uint64(0)-1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if _, err := m.Reserve("first", 2, 0, 0); !errors.Is(err, ErrCapacity) {
		t.Fatalf("overflow accepted: %v", err)
	}

	other, err := m.Reserve("other", 2, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	other()
}

func TestInspectMissingPath(t *testing.T) {
	space, err := Inspect(t.TempDir() + "/missing/child")
	if err != nil {
		t.Fatal(err)
	}

	if space.TotalBytes == 0 || space.AvailableBytes > space.TotalBytes {
		t.Fatalf("invalid capacity: %+v", space)
	}
}
