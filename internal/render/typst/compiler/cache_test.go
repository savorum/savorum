//go:build !race

package compiler

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestACompilationCacheIsReusedAndADamagedEntryIsReplaced(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	config := Config{MemoryLimitPages: 1024, CloseOnContextDone: true, CacheDirectory: directory}

	stale := filepath.Join(directory, "0123456789abcdef")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatal(err)
	}

	compile := func() *Compiler {
		t.Helper()

		instance, err := NewCompiler(ctx, config)
		if err != nil {
			t.Fatal(err)
		}

		if err := instance.Close(ctx); err != nil {
			t.Fatal(err)
		}

		return instance
	}

	if !compile().Cached() {
		t.Fatal("the first compiler did not write the cache")
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("another module's entries were kept: %v", err)
	}

	entries, err := filepath.Glob(filepath.Join(directory, moduleDigest(), "*", "*"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("cache entries = %v, %v; want one", entries, err)
	}

	if !compile().Cached() {
		t.Fatal("the second compiler did not load the cache")
	}

	// Longer than any entry header, so the damage is detected for every wazero version.
	if err := os.WriteFile(entries[0], bytes.Repeat([]byte("damaged "), 64), 0o600); err != nil {
		t.Fatal(err)
	}

	if compile().Cached() {
		t.Fatal("a damaged entry was used")
	}

	if _, err := os.Stat(entries[0]); !os.IsNotExist(err) {
		t.Fatalf("the damaged entry was kept: %v", err)
	}
}
