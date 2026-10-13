package sqlite_test

import (
	"context"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/savorum/savorum/internal/storage/sqlite"
)

var ulidPattern = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

func TestInstanceIDPersistence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "test.db")

	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	id1, err := store.InstanceID(ctx)
	if err != nil {
		t.Fatalf("InstanceID failed: %v", err)
	}

	if !ulidPattern.MatchString(id1) {
		t.Fatalf("InstanceID %q does not match ULID pattern", id1)
	}

	id2, err := store.InstanceID(ctx)
	if err != nil {
		t.Fatalf("InstanceID second call failed: %v", err)
	}

	if id1 != id2 {
		t.Fatalf("InstanceID changed within session: %q vs %q", id1, id2)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	id3, err := reopened.InstanceID(ctx)
	if err != nil {
		t.Fatalf("InstanceID reopened call failed: %v", err)
	}

	if id1 != id3 {
		t.Fatalf("InstanceID changed across reopen: %q vs %q", id1, id3)
	}

	dbPath2 := filepath.Join(dbDir, "test2.db")

	store2, err := sqlite.Open(ctx, dbPath2)
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()

	idOther, err := store2.InstanceID(ctx)
	if err != nil {
		t.Fatalf("InstanceID store2 failed: %v", err)
	}

	if id1 == idOther {
		t.Fatalf("Different databases generated identical instance ID: %q", id1)
	}
}
