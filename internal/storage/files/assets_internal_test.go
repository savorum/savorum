package files

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestContextReaderPreservesEOFAndCancellation(t *testing.T) {
	t.Run("EOF", func(t *testing.T) {
		reader := &contextReader{ctx: context.Background(), reader: strings.NewReader("")}

		data, err := io.ReadAll(reader)

		if err != nil || len(data) != 0 {
			t.Fatalf("ReadAll = %q, %v; want empty successful read", data, err)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		reader := &contextReader{ctx: ctx, reader: strings.NewReader("unread")}

		_, err := reader.Read(make([]byte, 1))

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Read error = %v, want context.Canceled", err)
		}
	})
}
