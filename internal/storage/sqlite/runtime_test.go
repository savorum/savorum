package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestRuntimeConnectionReplacement(t *testing.T) {
	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	store.db.SetConnMaxLifetime(time.Nanosecond)

	for range 2 {
		for name, want := range map[string]int{"foreign_keys": 1, "busy_timeout": 2000, "synchronous": 2, "wal_autocheckpoint": 1000} {
			var got int
			if err := store.db.QueryRowContext(ctx, "PRAGMA "+name).Scan(&got); err != nil || got != want {
				t.Fatalf("%s=%d want %d: %v", name, got, want, err)
			}
		}
	}
}

func TestRuntimeReadersAndBoundedExternalWriter(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.db")

	writer, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	reader, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	service, _ := library.NewService(writer, units.DefaultCatalog())

	entry, err := service.Create(ctx, testfixture.Document())
	if err != nil {
		t.Fatal(err)
	}

	tx, err := writer.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "UPDATE library_recipes SET description='uncommitted' WHERE id=?", entry.ID); err != nil {
		t.Fatal(err)
	}

	readCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	found, err := reader.Get(readCtx, entry.ID, library.StateActive)
	if err != nil || found.Recipe.Description != entry.Recipe.Description {
		t.Fatalf("reader did not retain committed snapshot: %v", err)
	}

	started := time.Now()

	blocked, err := reader.db.BeginTx(ctx, nil)
	if blocked != nil {
		_ = blocked.Rollback()
	}

	if err == nil {
		t.Fatal("second writer was not blocked")
	}

	var coded interface{ Code() int }
	if !errors.As(err, &coded) || coded.Code()&255 != 5 {
		t.Fatalf("expected SQLITE_BUSY: %v", err)
	}

	elapsed := time.Since(started)
	if elapsed < BusyTimeout/2 || elapsed > BusyTimeout+2*time.Second {
		t.Fatalf("busy wait not bounded: %s", elapsed)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	if err := reader.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeConcurrentRecipes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	service, _ := library.NewService(store, units.DefaultCatalog())

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			d := testfixture.Document()

			d.ID = recipe.RecipeID(fmt.Sprintf("runtime-%d", i))
			if _, err := service.Create(ctx, d); err != nil {
				t.Error(err)
			}

			if _, err := service.List(ctx, library.ListOptions{Query: "pumpkin", Limit: 25}); err != nil {
				t.Error(err)
			}
		}(i)
	}

	wg.Wait()

	count, err := service.Count(ctx, library.ListOptions{})
	if err != nil || count != 20 {
		t.Fatalf("writes=%d: %v", count, err)
	}

	if err := store.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeShutdownDrainsTransaction(t *testing.T) {
	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	finished := make(chan error, 1)

	deadline, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Shutdown acquires the store's only connection, and the open
	// transaction is holding it. WaitCount rising proves the drain is
	// parked on that acquisition, so a missing result below is evidence
	// rather than a race with a slow goroutine. The baseline is read before
	// the goroutine starts: database/sql counts a wait when it begins, so a
	// goroutine that queues first would leave nothing to observe.
	waited := store.db.Stats().WaitCount

	go func() { finished <- store.Shutdown(deadline) }()

	expires := time.Now().Add(2 * time.Second)
	for store.db.Stats().WaitCount == waited {
		if time.Now().After(expires) {
			t.Fatal("shutdown never started draining the connection pool")
		}

		time.Sleep(time.Millisecond)
	}

	select {
	case err := <-finished:
		t.Fatalf("shutdown bypassed active transaction: %v", err)
	default:
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := <-finished; err != nil {
		t.Fatal(err)
	}

	if err := store.Ping(ctx); err == nil {
		t.Fatal("shutdown accepted new work")
	}
}
