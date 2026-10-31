package app_test

import (
	"net/http"
	"testing"
)

func TestPantryEntriesAreSavedChangedAndRemoved(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	created := h.send(http.MethodPut, "/api/v1/pantry/flour", map[string]any{"name": "Flour"}, http.StatusOK, "If-None-Match", "*")
	changed := h.send(http.MethodPut, "/api/v1/pantry/flour", map[string]any{"name": "Plain flour"}, http.StatusOK, "If-Match", created.Header().Get("ETag"))

	h.send(http.MethodGet, "/api/v1/pantry", nil, http.StatusOK)
	h.send(http.MethodDelete, "/api/v1/pantry/flour", nil, http.StatusNoContent, "If-Match", changed.Header().Get("ETag"))
}

func TestPreferencesAreReplaced(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	current := decode[struct {
		Revision uint64 `json:"revision"`
	}](t, h.send(http.MethodGet, "/api/v1/me/preferences", nil, http.StatusOK))

	h.send(http.MethodPut, "/api/v1/me/preferences", map[string]any{"overrides": map[string]any{"appearance": "dark"}, "revision": current.Revision}, http.StatusOK)
}
