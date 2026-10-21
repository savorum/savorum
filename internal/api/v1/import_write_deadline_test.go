package v1

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/library"
)

type slowPreviewCommit struct {
	ExchangeService
	hold time.Duration
}

func (s slowPreviewCommit) CommitPreview(context.Context, string, []string, library.DuplicatePolicy) (exchange.ImportCommitResult, error) {
	time.Sleep(s.hold)

	return exchange.ImportCommitResult{}, nil
}

func TestImportPreviewCommitAnswersAfterTheServerWriteTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/recipes/import-previews/{preview_id}/commit", streamImportPreviewCommit(slowPreviewCommit{hold: 300 * time.Millisecond}))

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	server := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second, WriteTimeout: 100 * time.Millisecond}
	go func() { _ = server.Serve(listener) }()

	t.Cleanup(func() { _ = server.Close() })

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+listener.Addr().String()+"/api/v1/recipes/import-previews/preview/commit", strings.NewReader(`{"item_ids":["one"]}`))
	if err != nil {
		t.Fatal(err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("commit lost its response after the write timeout: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
}
