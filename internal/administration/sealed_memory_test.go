package administration

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/memoryspace"
)

func TestSealedMemoryRefusalLeavesFilesUntouched(t *testing.T) {
	old := memoryspace.Shared

	memoryspace.Shared = memoryspace.NewManagerWithProbe(func() (memoryspace.Space, error) {
		return memoryspace.Space{TotalBytes: 512 << 20, AvailableBytes: 512 << 20}, nil
	})
	defer func() { memoryspace.Shared = old }()

	root := t.TempDir()
	archive := filepath.Join(root, "archive")

	original := []byte("native archive remains unchanged")
	if err := os.WriteFile(archive, original, 0o600); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(root, "sealed.age")
	if err := Seal(t.Context(), archive, "", output, "password", time.Now()); !errors.Is(err, ErrResourceMemory) {
		t.Fatalf("seal refusal: %v", err)
	}

	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output changed: %v", err)
	}

	got, err := os.ReadFile(archive)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("archive changed")
	}
	// A syntactically shaped but unauthenticated header is sufficient to prove
	// that admission runs before age's expensive KDF and staging side effects.
	input := filepath.Join(root, "input.age")

	header := []byte("age-encryption.org/v1\n-> scrypt AAAAAAAAAAAAAAAAAAAAAA 18\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n--- AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n")
	if err := os.WriteFile(input, header, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TMPDIR", root)

	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := OpenSealed(t.Context(), input, "password", DefaultLimits()); !errors.Is(err, ErrResourceMemory) {
		t.Fatalf("open refusal: %v", err)
	}

	after, err := os.ReadDir(root)
	if err != nil || len(before) != len(after) {
		t.Fatal("refusal created staging")
	}

	got, err = os.ReadFile(input)
	if err != nil || !bytes.Equal(got, header) {
		t.Fatal("input changed")
	}
}

func TestSealedHeaderWorkFactorBound(t *testing.T) {
	for _, test := range []struct {
		name, header string
		want         int
	}{
		{"default", "age-encryption.org/v1\n-> scrypt salt 18\nbody\n--- mac\n", 18},
		{"ceiling", "age-encryption.org/v1\n-> scrypt salt 22\nbody\n--- mac\n", 22},
		{"above ceiling", "age-encryption.org/v1\n-> scrypt salt 23\nbody\n--- mac\n", 0},
		{"noncanonical", "age-encryption.org/v1\n-> scrypt salt 018\nbody\n--- mac\n", 0},
		{"negative", "age-encryption.org/v1\n-> scrypt salt -1\nbody\n--- mac\n", 0},
		{"unknown", "age-encryption.org/v1\n-> X25519 key\nbody\n--- mac\n", 0},
		{"multiple", "age-encryption.org/v1\n-> scrypt salt 18\nbody\n-> scrypt salt 18\nbody\n--- mac\n", 0},
		{"oversized", "age-encryption.org/v1\n" + strings.Repeat("x", 4096) + "\n--- mac\n", 0},
		{"truncated", "age-encryption.org/v1\n-> scrypt salt 18\n", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := sealedHeaderWorkFactor(bufio.NewReaderSize(strings.NewReader(test.header), 4096))
			if got != test.want || (err != nil) != (test.want == 0) {
				t.Fatalf("got %d %v", got, err)
			}
		})
	}
}

func TestSealedMemoryWorkFactorBounds(t *testing.T) {
	old := memoryspace.Shared
	defer func() { memoryspace.Shared = old }()

	memoryspace.Shared = memoryspace.NewManagerWithProbe(func() (memoryspace.Space, error) {
		return memoryspace.Space{TotalBytes: 4 << 30, AvailableBytes: 4 << 30}, nil
	})

	if _, err := reserveSealedMemory(maximumSealedWorkFactor); !errors.Is(err, ErrResourceMemory) {
		t.Fatalf("4GiB KDF plus safety must not fit 4GiB: %v", err)
	}

	for _, factor := range []int{0, 23, 64} {
		if _, err := reserveSealedMemory(factor); err == nil {
			t.Fatalf("invalid factor %d admitted", factor)
		}
	}

	memoryspace.Shared = memoryspace.NewManagerWithProbe(func() (memoryspace.Space, error) {
		return memoryspace.Space{TotalBytes: 1 << 30, AvailableBytes: 1 << 30}, nil
	})

	release, err := reserveSealedMemory(sealedWorkFactor)
	if err != nil {
		t.Fatal(err)
	}

	release()
}
