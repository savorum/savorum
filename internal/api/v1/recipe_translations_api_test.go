package v1

import (
	"net/http"
	"testing"
)

func TestTranslationsAreStartedRelatedAndUnrelated(t *testing.T) {
	t.Parallel()

	handler, _, _ := testAPI(t)

	original, originalTag := recipeNamed(t, handler, "soup", "Soup", "en")
	counterpart, _ := recipeNamed(t, handler, "soupe", "Soupe", "fr")
	path := "/api/v1/recipes/" + original.ID

	send(t, handler, http.MethodPost, path+"/translations/draft", map[string]any{"language": "de", "title": "Suppe", "copy_content": true}, http.StatusOK)

	untitled := decode[map[string]any](t, send(t, handler, http.MethodPost, path+"/translations/draft", map[string]any{"language": "de"}, http.StatusOK))
	if untitled["title"] != "Soup" {
		t.Fatalf("untitled draft title = %v", untitled["title"])
	}

	send(t, handler, http.MethodPost, path+"/translations", map[string]any{"counterpart": counterpart.ID, "relation": "translation"}, http.StatusOK, "If-Match", originalTag)

	refreshed := request(t, handler, http.MethodGet, path, nil)
	send(t, handler, http.MethodDelete, path+"/translations/"+counterpart.ID, nil, http.StatusOK, "If-Match", refreshed.Header().Get("ETag"))
}
