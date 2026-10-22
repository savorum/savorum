package account

import (
	"net/http/httptest"
	"testing"

	"github.com/savorum/savorum/internal/identity"
)

func TestTokenCapabilitiesFailClosedAtAccountBoundary(t *testing.T) {
	for _, test := range []struct {
		method, path, scope string
		allowed             bool
	}{
		{"GET", "/api/v1/recipes", "recipes:read", true},
		{"POST", "/api/v1/recipes/lookup", "recipes:read", true},
		{"POST", "/api/v1/books/lookup", "library:read", true},
		{"POST", "/api/v1/recipes/lookup", "library:read", false},
		{"PUT", "/api/v1/recipes/lookup", "recipes:read", false},
		{"PUT", "/api/v1/books/lookup", "library:read", false},
		{"GET", "/api/v1/recipe-authors", "recipes:read", true},
		{"GET", "/api/v1/recipe-authors", "library:read", false},
		{"POST", "/api/v1/recipe-authors", "recipes:write", false},
		{"POST", "/api/v1/ingredient-lines/parse", "recipes:read", true},
		{"POST", "/api/v1/ingredient-lines/parse", "library:read", false},
		{"POST", "/api/v1/ingredient-lines/parse", "recipes:write", false},
		{"PUT", "/api/v1/ingredient-lines/parse", "recipes:read", false},
		{"POST", "/api/v1/ingredient-lines/future-operation", "recipes:read", false},
		{"GET", "/api/v1/books/book/export/sref-bundle", "library:read", false},
		{"GET", "/api/v1/books/book/export/sref-bundle", "recipes:read", true},
		{"POST", "/api/v1/exports/sref-bundle", "recipes:read", true},
		{"POST", "/api/v1/recipes", "recipes:read", false},
		{"POST", "/api/v1/recipes/import-url", "recipes:write", false},
		{"POST", "/api/v1/recipes/import-url", "imports:write", true},
		{"GET", "/api/v1/account", "recipes:read", false},
		{"POST", "/api/v1/account/tokens", "library:write", false},
		{"GET", "/api/v1/users", "library:read", false},
		{"PUT", "/api/v1/configuration", "library:write", false},
		{"PUT", "/api/v1/libraries/default/members/user", "library:write", false},
		{"GET", "/api/v1/libraries", "library:read", true},
		{"DELETE", "/api/v1/trash/empty", "recipes:write", false},
		{"GET", "/api/v1/future-admin-feature", "library:read", false},
	} {
		r := httptest.NewRequestWithContext(t.Context(), test.method, test.path, nil)
		if got := tokenAllows(r, identity.PersonalToken{Scopes: []string{test.scope}}); got != test.allowed {
			t.Errorf("%s %s with %s: %v", test.method, test.path, test.scope, got)
		}
	}
}
