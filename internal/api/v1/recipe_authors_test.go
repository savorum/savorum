package v1

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestRecipeAuthorDiscoveryAndStrictFilteringAPI(t *testing.T) {
	t.Parallel()
	handler, store, _ := testAPI(t)

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	document := testfixture.Document()

	document.Authors = []recipe.Author{{Name: `Julia "Child"`}}
	if _, err := service.Create(t.Context(), document); err != nil {
		t.Fatal(err)
	}

	response := request(t, handler, http.MethodGet, "/api/v1/recipe-authors?q="+url.QueryEscape(`julia "chlid"`), nil)

	var found listRecipeAuthorsOutput

	if response.Code != http.StatusOK {
		t.Fatalf("discovery status %d: %s", response.Code, response.Body.String())
	}

	if err := json.Unmarshal(response.Body.Bytes(), &found.Body); err != nil {
		t.Fatal(err)
	}

	if len(found.Body.Items) != 1 || found.Body.Items[0].Match != "fuzzy" || found.Body.Items[0].Name != `Julia "Child"` || found.Body.Items[0].RecipeCount != 1 {
		t.Fatalf("discovery = %+v", found.Body)
	}

	for _, query := range []string{"", " ", "author:\"", "Unknown Author"} {
		path := "/api/v1/recipe-authors?q=" + url.QueryEscape(query)

		want := http.StatusOK
		if query == "" || query == " " {
			want = http.StatusUnprocessableEntity
		}

		if response := request(t, handler, http.MethodGet, path, nil); response.Code != want {
			t.Errorf("discovery query %q = %d, want %d", query, response.Code, want)
		}
	}

	for _, query := range []string{`author:`, `author:" "`, `author:"Julia`} {
		response := request(t, handler, http.MethodGet, "/api/v1/recipes?q="+url.QueryEscape(query), nil)
		if response.Code != http.StatusUnprocessableEntity {
			t.Errorf("strict query %q = %d", query, response.Code)
		}
	}
}
