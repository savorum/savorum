package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/library"
)

var emptyTestDatabase = sync.OnceValues(func() ([]byte, error) {
	directory, err := os.MkdirTemp("", "savorum-sqlite-fixture-")
	if err != nil {
		return nil, fmt.Errorf("create fixture directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(directory) }()

	path := filepath.Join(directory, "library.db")

	store, err := Open(context.Background(), path)
	if err != nil {
		return nil, err
	}

	if err := store.Close(); err != nil {
		return nil, err
	}

	image, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fixture database: %w", err)
	}

	return image, nil
})

// OpenTestStore opens an isolated copy of a migrated empty database.
// Migration and file-policy tests use Open directly.
func OpenTestStore(ctx context.Context, t testing.TB) *Store {
	t.Helper()

	image, err := emptyTestDatabase()
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "library.db")
	if err := os.WriteFile(path, image, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	return store
}

func TestStoreFixturesKeepIndependentState(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	first := OpenTestStore(ctx, t)
	second := OpenTestStore(ctx, t)

	firstID, err := first.InstanceID(ctx)
	if err != nil {
		t.Fatal(err)
	}

	secondID, err := second.InstanceID(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if firstID == secondID {
		t.Fatal("fixtures share an instance identity")
	}

	stamp := time.Now()
	if err := first.CreateTag(ctx, library.Tag{ID: "fixture-only", Name: "First fixture", Revision: 1, CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
		t.Fatal(err)
	}

	for _, store := range []*Store{second, OpenTestStore(ctx, t)} {
		tags, err := store.ListTags(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if len(tags) != 0 {
			t.Fatal("fixture mutation leaked into another database")
		}
	}
}
