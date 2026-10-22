package account_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/account"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

func TestBearerRequestsNeverFallBackToCookiesOrAcquireAdminPower(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "tokens.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	user, err := store.BootstrapAdmin(t.Context(), "owner", "Owner", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}

	ctx := identity.WithPrincipal(t.Context(), user)

	grant, err := store.CreatePersonalToken(ctx, identity.TokenRequest{Name: "automation", Scopes: []string{"recipes:read"}}, identity.TokenPolicy{})
	if err != nil {
		t.Fatal(err)
	}

	session, err := store.CreateSession(t.Context(), user.UserID, "local", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	handler := account.New(store, account.Options{Enabled: true}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, test := range []struct {
		method, path, token string
		want                int
	}{
		{"GET", "/api/v1/recipes", grant.Secret, 204},
		{"POST", "/api/v1/recipes", grant.Secret, 403},
		{"POST", "/api/v1/exports/sref-bundle", grant.Secret, 204},
		{"POST", "/api/v1/ingredient-lines/parse", grant.Secret, 204},
		{"GET", "/api/v1/users", grant.Secret, 403},
		{"PUT", "/api/v1/configuration", grant.Secret, 403},
		{"GET", "/api/v1/account", grant.Secret, 403},
		{"GET", "/api/v1/recipes", "invalid", 401},
	} {
		r := httptest.NewRequestWithContext(t.Context(), test.method, test.path, nil)
		r.Header.Set("Authorization", "Bearer "+test.token)
		r.AddCookie(&http.Cookie{Name: "savorum_session", Value: session.Token, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != test.want {
			t.Errorf("%s %s: %d want %d", test.method, test.path, w.Code, test.want)
		}

		if len(w.Result().Cookies()) != 0 {
			t.Fatal("bearer request established browser state")
		}
	}

	if err := store.RevokePersonalToken(ctx, grant.ID); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/recipes", nil)
	r.Header.Set("Authorization", "Bearer "+grant.Secret)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != 401 {
		t.Fatalf("revoked bearer HTTP status %d", w.Code)
	}
}
