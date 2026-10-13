package storagebudget

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
)

func TestWebhookKeyEnvelopeAdmission(t *testing.T) {
	prior := debug.SetMemoryLimit(16 << 20)
	defer debug.SetMemoryLimit(prior)

	root := t.TempDir()

	keys := filepath.Join(root, "keys")
	if err := os.Mkdir(keys, 0o700); err != nil {
		t.Fatal(err)
	}

	c := New(root, testDurableLimit)
	c.AddRetainedRoot(keys)
	c.SetDatabaseUsage(func(context.Context) (int64, error) { return 0, nil })

	ctx, release, err := c.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	for i := range NativeWebhookKeys() - 1 {
		if err := os.WriteFile(filepath.Join(keys, fmt.Sprintf("key_%d", i)), []byte("k"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := c.AdmitWebhookKey(ctx, SealedWebhookKeySize); err != nil {
		t.Fatal(err)
	}

	last := filepath.Join(keys, "last")
	if err := os.WriteFile(last, []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := c.AdmitWebhookKey(ctx, 1); !errors.Is(err, ErrCapacity) {
		t.Fatalf("count admission: %v", err)
	}

	if err := os.Remove(last); err != nil {
		t.Fatal(err)
	}

	if err := c.AdmitWebhookKey(ctx, SealedWebhookKeySize+1); !errors.Is(err, ErrCapacity) {
		t.Fatalf("size admission: %v", err)
	}

	if err := os.WriteFile(last, make([]byte, SealedWebhookKeySize+1), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := c.AdmitWebhookKey(ctx, 1); !errors.Is(err, ErrCapacity) {
		t.Fatalf("orphan size admission: %v", err)
	}

	if err := os.Remove(last); err != nil {
		t.Fatal(err)
	}

	if err := c.AdmitWebhookKey(ctx, 1); err != nil {
		t.Fatalf("cleanup restores admission: %v", err)
	}
}
