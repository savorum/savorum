package v1

import (
	"net/http"
	"testing"
)

func TestSavedMealsAreReadChangedAndDeleted(t *testing.T) {
	t.Parallel()

	handler, _, _ := testAPI(t)

	created := send(t, handler, http.MethodPost, "/api/v1/saved-meals", map[string]any{"name": "Brunch", "dishes": []any{map[string]any{"display_name": "Eggs"}}}, http.StatusCreated)
	saved := decode[SavedMeal](t, created)
	path := "/api/v1/saved-meals/" + saved.ID

	send(t, handler, http.MethodGet, path, nil, http.StatusOK)

	changed := send(t, handler, http.MethodPut, path, map[string]any{"name": "Late brunch", "dishes": []any{map[string]any{"display_name": "Eggs"}, map[string]any{"display_name": "Toast"}}}, http.StatusOK, "If-Match", `"`+itoa(saved.Revision)+`"`)
	revised := decode[SavedMeal](t, changed)

	send(t, handler, http.MethodDelete, path, nil, http.StatusNoContent, "If-Match", `"`+itoa(revised.Revision)+`"`)
}
