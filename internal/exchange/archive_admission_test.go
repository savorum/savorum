package exchange

import (
	"context"
	"errors"
	"testing"
)

func TestArchiveReaderAdmissionIsGloballyBounded(t *testing.T) {
	service := NewService(nil, nil, WithConcurrentArchiveReaders(1))

	release, err := service.acquireArchiveReader(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.acquireArchiveReader(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("second reader admission = %v, want cancellation while the only slot is held", err)
	}
}
