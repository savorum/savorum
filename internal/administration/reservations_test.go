package administration

import (
	"bytes"
	"errors"
	"testing"

	"github.com/savorum/savorum/internal/diskspace"
)

func TestStagingReservationsShareFilesystemAndProtectInodes(t *testing.T) {
	directory := t.TempDir()
	space := diskspace.Space{Device: 1, AvailableBytes: diskspace.ReserveBytes + 4096, TotalInodes: 128, AvailableInodes: diskspace.ReserveInodes + 32}
	manager := diskspace.NewManagerWithInspector(func(string) (diskspace.Space, error) { return space, nil })

	backup := &inspectedBackup{manifest: manifest{Database: fileRecord{Size: 1024}}}

	release, err := manager.Reserve(directory, space.AvailableBytes-diskspace.ReserveBytes-1024, 0, diskspace.ReserveBytes)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := reserveStagingCapacity(manager, backup, directory, directory); !errors.Is(err, diskspace.ErrCapacity) {
		release()
		t.Fatalf("overlapping extraction accepted: %v", err)
	}

	release()

	release, err = manager.Reserve(directory, 0, space.AvailableInodes-diskspace.ReserveInodes-4, diskspace.ReserveBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if _, err := reserveStagingCapacity(manager, backup, directory, directory); !errors.Is(err, diskspace.ErrCapacity) {
		t.Fatalf("inode-exhausted extraction accepted: %v", err)
	}
}

func TestSealedOutputCannotExceedReservation(t *testing.T) {
	var output bytes.Buffer

	writer := &capacityWriter{writer: &output, remaining: 4}
	if _, err := writer.Write([]byte("1234")); err != nil {
		t.Fatal(err)
	}

	if _, err := writer.Write([]byte("5")); err == nil {
		t.Fatal("reservation bound accepted excess output")
	}

	if output.String() != "1234" {
		t.Fatal("excess output reached filesystem")
	}
}
