package app_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/app"
	"github.com/savorum/savorum/internal/importer/webrecipe"
)

const soupAddress = "https://recipes.test/soup"

// pageHarness serves recipePage for every address, so imports by address run
// without the network and without weakening the outbound policy.
func pageHarness(t *testing.T, page *string) *harness {
	t.Helper()

	return newHarnessWith(t, func(options *app.Options) {
		options.PageFetcher = func(_ context.Context, address string) (*webrecipe.Page, error) {
			return &webrecipe.Page{URL: address, ContentType: "text/html", Body: []byte(*page)}, nil
		}
	})
}

func TestRecipesAreImportedFromAnAddress(t *testing.T) {
	t.Parallel()

	page := recipePage
	h := pageHarness(t, &page)

	h.send(http.MethodPost, "/api/v1/recipes/import-url/preview", map[string]any{"url": soupAddress}, http.StatusOK)
	h.send(http.MethodPost, "/api/v1/recipes/import-url", map[string]any{"url": soupAddress}, http.StatusOK)
}

func TestARecipeFromAnAddressIsRefreshedWhenThePageChanges(t *testing.T) {
	t.Parallel()

	page := recipePage
	h := pageHarness(t, &page)

	imported := decode[struct {
		Items []struct {
			EntryID string `json:"library_entry_id"`
		} `json:"items"`
	}](t, h.send(http.MethodPost, "/api/v1/recipes/import-url", map[string]any{"url": soupAddress}, http.StatusOK))

	path := "/api/v1/recipes/" + imported.Items[0].EntryID

	page = strings.Replace(recipePage, "Blend until smooth.", "Blend until smooth and season.", 1)

	current := decode[struct {
		Revision uint64         `json:"revision"`
		Recipe   map[string]any `json:"recipe"`
	}](t, h.send(http.MethodGet, path, nil, http.StatusOK))

	fetched := decode[struct {
		Remote map[string]any `json:"remote"`
	}](t, h.send(http.MethodPost, path+"/source-refresh/preview-url", map[string]any{"url": soupAddress, "expected_revision": current.Revision}, http.StatusOK))

	h.send(http.MethodPost, path+"/source-refresh/preview", map[string]any{"expected_revision": current.Revision, "remote": fetched.Remote}, http.StatusOK)
	h.send(http.MethodPost, path+"/source-refresh", map[string]any{"expected_revision": current.Revision, "remote": fetched.Remote, "resolutions": []any{}}, http.StatusOK)

	if stored := h.send(http.MethodGet, path, nil, http.StatusOK); !strings.Contains(stored.Body.String(), "Blend until smooth and season.") {
		t.Fatalf("stored recipe was not refreshed: %s", stored.Body.String())
	}
}
