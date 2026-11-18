package v1

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/savorum/savorum/internal/testfixture"
)

func TestRecipeAPILinksDependenciesToLibraryEntries(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	create := func(document RecipeDocument) LibraryEntry {
		t.Helper()

		body, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}

		response := request(t, handler, http.MethodPost, "/api/v1/recipes", body)
		if response.Code != http.StatusCreated {
			t.Fatalf("create status = %d; body = %s", response.Code, response.Body.String())
		}

		var entry LibraryEntry
		if err := json.Unmarshal(response.Body.Bytes(), &entry); err != nil {
			t.Fatal(err)
		}

		return entry
	}

	pastry := fromDomain(testfixture.Document())
	pastry.ID = "flaky-pastry"
	pastry.Title = "Flaky pastry"
	pastryEntry := create(pastry)

	pies := fromDomain(testfixture.Document())
	ingredients := pies.IngredientSections[0].Ingredients
	ingredients[0].Recipe = &RecipeDependency{Title: "Flaky pastry", RecipeID: "flaky-pastry"}
	ingredients[1].Recipe = &RecipeDependency{Title: "Not in this library", RecipeID: "elsewhere"}
	piesEntry := create(pies)

	response := request(t, handler, http.MethodGet, "/api/v1/recipes/"+piesEntry.ID, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("get status = %d; body = %s", response.Code, response.Body.String())
	}

	var read LibraryEntry
	if err := json.Unmarshal(response.Body.Bytes(), &read); err != nil {
		t.Fatal(err)
	}

	got := read.Recipe.IngredientSections[0].Ingredients
	if got[0].Recipe.EntryID != pastryEntry.ID {
		t.Fatalf("linked entry = %q; want %q", got[0].Recipe.EntryID, pastryEntry.ID)
	}

	if got[1].Recipe.EntryID != "" {
		t.Fatalf("unknown dependency linked to %q", got[1].Recipe.EntryID)
	}

	body, err := json.Marshal(read.Recipe)
	if err != nil {
		t.Fatal(err)
	}

	replace := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/recipes/"+piesEntry.ID, body, response.Header().Get("ETag"))
	if replace.Code != http.StatusOK {
		t.Fatalf("replace with a linked dependency status = %d; body = %s", replace.Code, replace.Body.String())
	}
}
