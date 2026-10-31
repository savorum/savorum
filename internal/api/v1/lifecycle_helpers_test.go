package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
)

// send calls an operation with a JSON body and requires the given status. It
// returns the recorder so a test can read the body and headers.
func send(t *testing.T, handler http.Handler, method, target string, body any, want int, headers ...string) *httptest.ResponseRecorder {
	t.Helper()

	var encoded []byte

	if body != nil {
		var err error
		if encoded, err = json.Marshal(body); err != nil {
			t.Fatal(err)
		}
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), method, target, nil)

	if encoded != nil {
		request = httptest.NewRequestWithContext(t.Context(), method, target, bytes.NewReader(encoded))
		request.Header.Set("Content-Type", "application/json")
	}

	for index := 0; index+1 < len(headers); index += 2 {
		request.Header.Set(headers[index], headers[index+1])
	}

	handler.ServeHTTP(response, request)

	if response.Code != want {
		t.Fatalf("%s %s status = %d, want %d; body = %s", method, target, response.Code, want, response.Body.String())
	}

	return response
}

func decode[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()

	var value T
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode %s: %v", response.Body.String(), err)
	}

	return value
}

// recipeNamed creates a plain library recipe under its own document ID, title
// and language.
func recipeNamed(t *testing.T, handler http.Handler, id, title, language string) (LibraryEntry, string) {
	t.Helper()

	document := recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            recipe.RecipeID(id),
		Title:         title,
		Language:      language,
		IngredientSections: []recipe.IngredientSection{{ID: "ingredients", Ingredients: []recipe.Ingredient{
			{ID: "water", Name: "water", SourceText: "1 cup water"},
		}}},
		InstructionSections: []recipe.InstructionSection{{ID: "method", Steps: []recipe.Step{{ID: "boil", Text: "Boil the water."}}}},
	}

	encoded, err := json.Marshal(fromDomain(document))
	if err != nil {
		t.Fatal(err)
	}

	response := request(t, handler, http.MethodPost, "/api/v1/recipes", encoded)
	if response.Code != http.StatusCreated {
		t.Fatalf("create %s status = %d; body = %s", id, response.Code, response.Body.String())
	}

	return decode[LibraryEntry](t, response), response.Header().Get("ETag")
}

func itoa(value uint64) string { return strconv.FormatUint(value, 10) }
