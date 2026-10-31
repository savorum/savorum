package v1

import (
	"net/http"
	"testing"
)

func TestRecipeRevisionsExportAndImportEventsAreRead(t *testing.T) {
	t.Parallel()

	handler, _, _ := testAPI(t)

	entry, _ := recipeNamed(t, handler, "soup", "Soup", "en")
	path := "/api/v1/recipes/" + entry.ID

	send(t, handler, http.MethodGet, path+"/revisions/1/export/sref", nil, http.StatusOK)
	send(t, handler, http.MethodGet, path+"/import-events", nil, http.StatusOK)
}

func TestCookingsAreRecordedAndCorrected(t *testing.T) {
	t.Parallel()

	handler, _, _ := testAPI(t)

	entry, _ := recipeNamed(t, handler, "soup", "Soup", "en")
	cooking := recordCooking(t, handler, entry.ID, map[string]any{"cooked_on": "2026-08-01"})

	send(t, handler, http.MethodPut, "/api/v1/recipes/"+entry.ID+"/cookings/"+cooking.ID, map[string]any{"cooked_on": "2026-08-02", "note": "Needed salt."}, http.StatusOK)
}

func TestMicrowaveAnalysisIsRefreshed(t *testing.T) {
	t.Parallel()

	handler, _, _ := testAPI(t)

	entry, tag := recipeNamed(t, handler, "soup", "Soup", "en")
	send(t, handler, http.MethodPost, "/api/v1/recipes/"+entry.ID+"/reanalyze/microwave", nil, http.StatusOK, "If-Match", tag)
}
