package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRecipesAreBulkedIntoABookExportedAndImportedElsewhere(t *testing.T) {
	t.Parallel()

	source := newHarness(t)

	entry := source.recipe()
	book := source.send(http.MethodPost, "/api/v1/books", map[string]any{"kind": "personal", "title": "Weeknights", "sections": []any{}, "entries": []any{}}, http.StatusCreated)
	bookID := decode[struct {
		ID       string `json:"id"`
		Revision uint64 `json:"revision"`
	}](t, book)

	current := decode[struct {
		Revision uint64 `json:"revision"`
	}](t, source.send(http.MethodGet, "/api/v1/recipes/"+entry, nil, http.StatusOK))

	source.send(http.MethodPost, "/api/v1/bulk/recipes/book", map[string]any{
		"items":   []any{map[string]any{"id": entry, "revision": current.Revision}},
		"book_id": bookID.ID, "book_revision": bookID.Revision, "member": true,
	}, http.StatusOK)

	source.send(http.MethodGet, "/api/v1/books/"+bookID.ID+"/export/sref-bundle", nil, http.StatusOK)

	bundle := source.send(http.MethodPost, "/api/v1/exports/sref-bundle", map[string]any{"recipes": []string{entry}}, http.StatusOK)

	elsewhere := newHarness(t)
	elsewhere.raw(http.MethodPost, "/api/v1/recipes/import-package", bundle.Body.Bytes(), "application/zip", http.StatusOK)
}

func TestTheEventStreamOpensForALiveClient(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.recipe()

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/events", nil)
	request.Host = "example.com"

	response := httptest.NewRecorder()
	h.handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("event stream status = %d; body = %s", response.Code, response.Body.String())
	}
}
