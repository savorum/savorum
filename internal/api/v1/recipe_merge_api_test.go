package v1

import (
	"net/http"
	"testing"
)

func TestDuplicateRecipesAreComparedAndMerged(t *testing.T) {
	t.Parallel()

	handler, _, _ := testAPI(t)

	survivor, _ := recipeNamed(t, handler, "stew", "Stew", "en")
	duplicate, _ := recipeNamed(t, handler, "stew-again", "Stew again", "en")

	send(t, handler, http.MethodPost, "/api/v1/recipes/merge/preview", map[string]any{"left_id": survivor.ID, "right_id": duplicate.ID}, http.StatusOK)

	merged := decode[LibraryEntry](t, request(t, handler, http.MethodGet, "/api/v1/recipes/"+survivor.ID, nil)).Recipe

	send(t, handler, http.MethodPost, "/api/v1/recipes/merge", map[string]any{
		"survivor": survivor.ID, "duplicate": duplicate.ID,
		"expected_survivor_revision": survivor.Revision, "expected_duplicate_revision": duplicate.Revision,
		"recipe": merged,
	}, http.StatusOK)
}
