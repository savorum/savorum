package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/app"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/configuration"
	"github.com/savorum/savorum/internal/contracttest"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
)

// harness is the assembled application in local mode with every optional
// capability enabled and its background work running. Responses are checked
// against the contract.
type harness struct {
	t           *testing.T
	handler     http.Handler
	store       *sqlite.Store
	application *app.Application
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	return newHarnessWith(t, nil)
}

// newHarnessWith lets a test adjust the application options.
func newHarnessWith(t *testing.T, adjust func(*app.Options)) *harness {
	t.Helper()

	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	blobs, err := files.NewAssetStore(filepath.Join(t.TempDir(), "assets"), files.DefaultMaximumAssetSize)
	if err != nil {
		t.Fatal(err)
	}

	assetService, err := assets.NewService(store, blobs)
	if err != nil {
		t.Fatal(err)
	}

	settings, err := configuration.NewService(store, configuration.InstanceOverrides{})
	if err != nil {
		t.Fatal(err)
	}

	options := app.Options{
		Assets:              assetService,
		Configuration:       settings,
		WebhookDirectory:    t.TempDir(),
		AutomationDirectory: t.TempDir(),
		StoragePath:         t.TempDir(),
	}
	if adjust != nil {
		adjust(&options)
	}

	application, err := app.New(store, units.DefaultCatalog(), options)
	if err != nil {
		t.Fatal(err)
	}

	worker, stop := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = application.Automation.Run(worker)
	}()

	t.Cleanup(func() {
		stop()
		<-done
	})

	return &harness{t: t, handler: contracttest.Handler(t, application.Handler), store: store, application: application}
}

// send calls an operation with a JSON body and requires the given status.
func (h *harness) send(method, target string, body any, want int, headers ...string) *httptest.ResponseRecorder {
	h.t.Helper()

	return h.raw(method, target, jsonBody(h.t, body), "application/json", want, headers...)
}

// raw calls an operation with a body of any media type.
func (h *harness) raw(method, target string, body []byte, contentType string, want int, headers ...string) *httptest.ResponseRecorder {
	h.t.Helper()

	response := h.attempt(method, target, body, contentType, headers...)
	if response.Code != want {
		h.t.Fatalf("%s %s status = %d, want %d; body = %s", method, target, response.Code, want, response.Body.String())
	}

	return response
}

// attempt calls an operation and returns the response whatever its status.
func (h *harness) attempt(method, target string, body []byte, contentType string, headers ...string) *httptest.ResponseRecorder {
	h.t.Helper()

	request := httptest.NewRequestWithContext(h.t.Context(), method, target, bytes.NewReader(body))
	request.Host = "example.com"
	request.Header.Set("Origin", "http://example.com")

	if len(body) > 0 {
		request.Header.Set("Content-Type", contentType)
	}

	for index := 0; index+1 < len(headers); index += 2 {
		request.Header.Set(headers[index], headers[index+1])
	}

	response := httptest.NewRecorder()
	h.handler.ServeHTTP(response, request)

	return response
}

func jsonBody(t *testing.T, body any) []byte {
	t.Helper()

	if body == nil {
		return nil
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	return encoded
}

func decode[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()

	var value T
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode %s: %v", response.Body.String(), err)
	}

	return value
}

// recipe creates a plain recipe titled Soup and returns its entry identifier.
func (h *harness) recipe() string {
	h.t.Helper()

	response := h.send(http.MethodPost, "/api/v1/recipes", map[string]any{
		"schema_version": 1, "id": "soup", "title": "Soup", "language": "en",
		"ingredient_sections":  []any{map[string]any{"id": "ingredients", "ingredients": []any{map[string]any{"id": "water", "name": "water", "source_text": "1 cup water"}}}},
		"instruction_sections": []any{map[string]any{"id": "method", "steps": []any{map[string]any{"id": "boil", "text": "Boil the water."}}}},
	}, http.StatusCreated)

	return decode[struct {
		ID string `json:"id"`
	}](h.t, response).ID
}

// waitFor polls a job until it leaves the queued and running states.
func (h *harness) waitFor(path string) map[string]any {
	h.t.Helper()

	for range 200 {
		job := decode[map[string]any](h.t, h.send(http.MethodGet, path, nil, http.StatusOK))
		if state, _ := job["state"].(string); state != "pending" && state != "queued" && state != "running" {
			return job
		}

		time.Sleep(25 * time.Millisecond)
	}

	h.t.Fatalf("%s never settled", path)

	return nil
}

// upload stages one file as an import preview.
func (h *harness) upload(filename string, content []byte) *httptest.ResponseRecorder {
	h.t.Helper()

	var body bytes.Buffer

	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("files", filename)
	_, _ = part.Write(content)
	_ = writer.Close()

	return h.raw(http.MethodPost, "/api/v1/recipes/import-preview", body.Bytes(), writer.FormDataContentType(), http.StatusOK)
}

// text reads a string field from a decoded document.
func text(t *testing.T, document map[string]any, name string) string {
	t.Helper()

	value, ok := document[name].(string)
	if !ok {
		t.Fatalf("%s is not a string in %v", name, document)
	}

	return value
}

// uploadAll stages several files as one import preview.
func (h *harness) uploadAll(files map[string][]byte, want int) *httptest.ResponseRecorder {
	h.t.Helper()

	var body bytes.Buffer

	writer := multipart.NewWriter(&body)

	for name, content := range files {
		part, _ := writer.CreateFormFile("files", name)
		_, _ = part.Write(content)
	}

	_ = writer.Close()

	return h.raw(http.MethodPost, "/api/v1/recipes/import-preview", body.Bytes(), writer.FormDataContentType(), want)
}
