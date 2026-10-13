package administration

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestVolumeJournalInterruptedPublicationCanResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.json")

	interrupted := errors.New("interrupted before publication")
	if err := volumeAtomicWrite(path, []byte("complete immutable manifest"), func() error { return interrupted }); !errors.Is(err, interrupted) {
		t.Fatalf("interruption: %v", err)
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("interrupted write published incomplete intent")
	}

	if err := volumeWrite(path, []byte("complete immutable manifest")); err != nil {
		t.Fatal(err)
	}

	if err := volumeWrite(path, []byte("complete immutable manifest")); err != nil {
		t.Fatalf("lost publish reply: %v", err)
	}

	if err := volumeWrite(path, []byte("different manifest")); err == nil {
		t.Fatal("different intent replaced published journal")
	}
}
