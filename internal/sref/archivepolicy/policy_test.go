package archivepolicy

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"testing"
)

func TestPreflightRefusesEntryCountBeforeArchiveReaderAllocation(t *testing.T) {
	var data bytes.Buffer

	writer := zip.NewWriter(&data)
	for index := range 200 {
		entry, err := writer.Create(fmt.Sprintf("entry-%03d", index))
		if err != nil {
			t.Fatal(err)
		}

		if _, err := entry.Write(nil); err != nil {
			t.Fatal(err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err := Preflight(data.Bytes(), 10, 1<<20); !errors.Is(err, ErrLimit) {
		t.Fatalf("preflight = %v, want entry-count refusal", err)
	}
}

func TestPreflightAcceptsOrdinaryArchive(t *testing.T) {
	var data bytes.Buffer

	writer := zip.NewWriter(&data)

	entry, err := writer.Create("recipe.json")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := entry.Write([]byte("{}")); err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err := Preflight(data.Bytes(), 2, 1<<10); err != nil {
		t.Fatalf("ordinary archive refused: %v", err)
	}
}

func TestPreflightSeparatesMalformedBytesFromResourceExhaustion(t *testing.T) {
	if err := Preflight([]byte("not a zip"), 2, 1<<10); !errors.Is(err, ErrInvalid) {
		t.Fatalf("preflight = %v, want malformed archive", err)
	}
}
