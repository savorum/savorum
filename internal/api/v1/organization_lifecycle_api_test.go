package v1

import (
	"net/http"
	"testing"
)

func TestTagsAndFacetsAreReadAndRenamed(t *testing.T) {
	t.Parallel()

	handler, _, _ := testAPI(t)

	tag := createTag(t, handler, "Weeknight", "#53745b")
	read := send(t, handler, http.MethodGet, "/api/v1/tags/"+tag.ID, nil, http.StatusOK)
	send(t, handler, http.MethodPut, "/api/v1/tags/"+tag.ID, map[string]any{"name": "Midweek", "color": "#53745b"}, http.StatusOK, "If-Match", read.Header().Get("ETag"))

	facet := createFacet(t, handler, "course", "Main")
	read = send(t, handler, http.MethodGet, "/api/v1/facets/"+facet.ID, nil, http.StatusOK)
	send(t, handler, http.MethodPut, "/api/v1/facets/"+facet.ID, map[string]any{"kind": "course", "value": "Mains"}, http.StatusOK, "If-Match", read.Header().Get("ETag"))
}

func TestBookRevisionsAreReadRestoredAndExported(t *testing.T) {
	t.Parallel()

	handler, _, _ := testAPI(t)

	book := createBook(t, handler, map[string]any{"kind": "personal", "title": "Weeknights", "sections": []any{}, "entries": []any{}})
	path := "/api/v1/books/" + book.ID

	send(t, handler, http.MethodGet, path+"/revisions/1", nil, http.StatusOK)

	read := send(t, handler, http.MethodGet, path, nil, http.StatusOK)
	send(t, handler, http.MethodDelete, path, nil, http.StatusNoContent, "If-Match", read.Header().Get("ETag"))

	trashed := send(t, handler, http.MethodGet, "/api/v1/trash/books/"+book.ID, nil, http.StatusOK)
	send(t, handler, http.MethodPost, path+"/restore", nil, http.StatusOK, "If-Match", trashed.Header().Get("ETag"))
}
