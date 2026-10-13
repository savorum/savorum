package v1

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/savorum/savorum/internal/print"
)

type delayedPrintService struct{ headPrintService }

func (*delayedPrintService) Render(context.Context, print.Request) (print.Artifact, error) {
	time.Sleep(100 * time.Millisecond)
	return print.Artifact{}, print.ErrTooLarge
}

func TestPrintReturnsItsResultBeyondTheOrdinaryWriteDeadline(t *testing.T) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("deadline test", "1"))
	registerPrint(api, &delayedPrintService{})

	server := httptest.NewUnstartedServer(mux)
	server.Config.WriteTimeout = 20 * time.Millisecond

	server.Start()
	defer server.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/api/v1/print/documents", strings.NewReader(`{"recipes":["r1"]}`))
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("Content-Type", "application/json")

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}

	if response.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(string(body), "print-too-large") {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
}
