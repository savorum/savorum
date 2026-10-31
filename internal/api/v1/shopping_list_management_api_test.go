package v1

import (
	"net/http"
	"testing"
)

func TestShoppingListsAreRenamedAndRemoved(t *testing.T) {
	t.Parallel()

	handler, _, _ := testAPI(t)

	send(t, handler, http.MethodPost, "/api/v1/shopping/lists", map[string]any{"id": "picnic", "name": "Picnic"}, http.StatusCreated)

	renamed := send(t, handler, http.MethodPatch, "/api/v1/shopping/lists/picnic", map[string]any{"id": "rename-1", "name": "Beach picnic"}, http.StatusOK)

	if list := decode[map[string]any](t, renamed); list["name"] != "Beach picnic" {
		t.Fatalf("renamed list = %v", list)
	}

	send(t, handler, http.MethodGet, "/api/v1/shopping/lists", nil, http.StatusOK)
	send(t, handler, http.MethodDelete, "/api/v1/shopping/lists/picnic", nil, http.StatusNoContent)
}
