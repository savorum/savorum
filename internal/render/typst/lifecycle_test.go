//go:build !race

package typst

import (
	"context"
	"testing"
	"time"
)

func TestACachedRendererReleasesItsCompilerWhenIdleAndLoadsItAgain(t *testing.T) {
	renderer := NewCached(t.TempDir())
	renderer.idle = 10 * time.Millisecond

	t.Cleanup(func() { _ = renderer.Close() })

	if renderer.cache == "" {
		t.Fatal("the cache directory was not used")
	}

	for range 2 {
		if err := renderer.Warm(context.Background()); err != nil {
			t.Fatal(err)
		}

		deadline := time.Now().Add(10 * time.Second)
		for loaded(renderer) {
			if time.Now().After(deadline) {
				t.Fatal("an idle compiler was kept")
			}

			time.Sleep(10 * time.Millisecond)
		}
	}
}

// Closing unmaps guest memory, so it must never happen under a running
// compilation. Close cancels active renders and waits for them to end.
func TestClosingARendererWaitsForItsActiveRenders(t *testing.T) {
	renderer := New()

	_, ctx, release, err := renderer.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	closed := make(chan error, 1)

	go func() { closed <- renderer.Close() }()

	select {
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("closing did not cancel the active render")
	}

	select {
	case err := <-closed:
		t.Fatalf("the compiler was closed while a render held it: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	release()

	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return after the render ended")
	}

	if _, _, _, err := renderer.acquire(context.Background()); err == nil {
		t.Fatal("a closed renderer handed out its compiler")
	}
}

func loaded(renderer *Renderer) bool {
	renderer.mutex.Lock()
	defer renderer.mutex.Unlock()

	return renderer.compiler != nil
}
