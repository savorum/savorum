package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

func TestIntegrationWorkerSurvivesExternalBackupWriter(t *testing.T) {
	for _, cancelWhileWaiting := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelWhileWaiting), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			path := filepath.Join(t.TempDir(), "savorum.db")

			writer, err := sqlite.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()

			reader, err := sqlite.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()

			locked, release := make(chan struct{}), make(chan struct{})

			var once sync.Once

			unlock := func() { once.Do(func() { close(release) }) }
			defer unlock()

			snapshot := make(chan error, 1)

			destination := filepath.Join(t.TempDir(), "snapshot.db")
			go func() {
				snapshot <- writer.CreateSnapshot(t.Context(), destination, func(int, []assets.Blob) error { close(locked); <-release; return nil })
			}()

			select {
			case <-locked:
			case err := <-snapshot:
				t.Fatalf("snapshot failed: %v", err)
			case <-time.After(10 * time.Second):
				t.Fatal("snapshot did not acquire writer")
			}

			blocked := make(chan error, 1)

			done := make(chan error, 1)
			go func() {
				done <- runIntegrationWorker(ctx, func(ctx context.Context) error {
					err := reader.EnqueueWebhookEvents(ctx)
					if err != nil {
						select {
						case blocked <- err:
						default:
						}
					}

					return err
				})
			}()

			select {
			case err := <-blocked:
				if !sqlite.IsBusy(fmt.Errorf("wrapped: %w", err)) {
					t.Fatalf("wrong contention error: %v", err)
				}
			case err := <-done:
				t.Fatalf("worker exited before contention: %v", err)
			case <-time.After(10 * time.Second):
				t.Fatal("worker did not observe contention")
			}

			if cancelWhileWaiting {
				cancel()
			}

			unlock()

			if err := <-snapshot; err != nil {
				t.Fatal(err)
			}

			select {
			case err := <-done:
				if cancelWhileWaiting && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation: %v", err)
				}

				if !cancelWhileWaiting && err != nil {
					t.Fatalf("worker did not recover: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("worker did not finish")
			}
		})
	}
}

func TestIntegrationWorkerPreservesPermanentFailure(t *testing.T) {
	want := errors.New("permanent failure")
	calls := 0

	err := runIntegrationWorker(t.Context(), func(context.Context) error { calls++; return want })
	if !errors.Is(err, want) || calls != 1 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}
