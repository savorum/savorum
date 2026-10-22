package federation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/identity"
)

type linkingFreshnessStore struct {
	fakeStore
	session identity.Session
}

func (s *linkingFreshnessStore) ResolveSession(context.Context, string) (identity.Principal, identity.Session, error) {
	return identity.Principal{UserID: "user", CredentialID: s.session.ID}, s.session, nil
}
func (*linkingFreshnessStore) CheckCSRF(context.Context, string, string) bool { return true }

func TestLinkEntryAndNativeFinishRejectOldSessions(t *testing.T) {
	store := &linkingFreshnessStore{session: identity.Session{ID: "old", CreatedAt: time.Now().Add(-6 * time.Minute)}}
	provider := Provider{ID: "oidc", Kind: "oidc", DisplayName: "Company", OIDC: &OIDCConfig{Issuer: "https://idp.example", ClientID: "client"}}

	handler, err := New(store, Config{Providers: []Provider{provider}}, Options{PublicURL: "https://recipes.example"}, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ path, body string }{
		{"/api/v1/auth/oidc/login", `{"link":true}`},
		{"/api/v1/auth/oidc/native-link-tickets", `{"code_challenge":"` + strings.Repeat("x", 43) + `","code_challenge_method":"S256"}`},
		{"/api/v1/auth/native-links", `{"code":"held"}`},
	} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://recipes.example"+tc.path, strings.NewReader(tc.body))
		request.Header.Set("Origin", "https://recipes.example")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", "csrf")
		request.AddCookie(&http.Cookie{Name: identity.SessionCookie, Value: "cookie", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "recent-authentication-required") || len(store.subjects) != 0 {
			t.Fatalf("path=%s status=%d body=%s", tc.path, response.Code, response.Body.String())
		}
	}
}
