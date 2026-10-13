package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"github.com/savorum/savorum/internal/identity"
)

func TestIngredientFilterCapabilityAndInvalidQueryHTTPContract(t *testing.T) {
	t.Parallel()
	handler, store, _ := testAPI(t)

	principal, err := store.LocalPrincipal(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	ctx := identity.WithPrincipal(t.Context(), principal)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/capabilities", nil))

	var capabilities Capabilities
	if err := json.Unmarshal(response.Body.Bytes(), &capabilities); err != nil {
		t.Fatal(err)
	}

	if response.Code != http.StatusOK || !slices.Contains(capabilities.Features, "recipes.search.ingredient") {
		t.Fatalf("ingredient capability = %d, %+v", response.Code, capabilities.Features)
	}

	for _, query := range []string{`ingredient:`, `ingredient:" "`, `ingredient:"chicken`} {
		response := request(t, handler, http.MethodGet, "/api/v1/recipes?q="+url.QueryEscape(query), nil)
		if response.Code != http.StatusUnprocessableEntity {
			t.Errorf("ingredient query %q = %d", query, response.Code)
		}
	}

	response = request(t, handler, http.MethodGet, "/api/v1/recipes?q=ingredient%3Aunknown", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("unknown ingredient = %d: %s", response.Code, response.Body.String())
	}
}
