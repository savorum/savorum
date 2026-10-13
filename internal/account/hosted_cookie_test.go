package account

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

type cookieCeremonyStore struct {
	unexpectedRepository
	unexpectedPasskeyRepository
	taken string
}

func (s *cookieCeremonyStore) TakeWebAuthnCeremony(_ context.Context, token, _, _, _, _ string) (webauthn.SessionData, error) {
	s.taken = token

	return webauthn.SessionData{Challenge: "verified"}, nil
}

func TestHostedPasskeyCeremonyNamespace(t *testing.T) {
	for _, test := range []struct {
		host     bool
		cookie   string
		accepted bool
	}{
		{true, "savorum_webauthn_login=legacy", false},
		{true, "__Host-savorum_webauthn_login=valid; __Host-savorum_webauthn_login=duplicate", false},
		{true, "__Host-savorum_webauthn_login=valid; savorum_webauthn_login=parent-forgery", true},
		{false, "savorum_webauthn_login=valid", true},
	} {
		store := &cookieCeremonyStore{}
		h := &Handler{store: store, options: Options{Cookies: identity.BrowserCookies{HostPrefix: test.host}}}
		r := httptest.NewRequestWithContext(t.Context(), "POST", "https://tenant.example", nil)
		r.Header.Set("Cookie", test.cookie)

		w := httptest.NewRecorder()

		_, err := h.takeCeremony(w, r, "login", "tenant.example")
		if test.accepted {
			if err != nil || store.taken != "valid" {
				t.Fatalf("ceremony=%s err=%v", store.taken, err)
			}
		} else if !errors.Is(err, identity.ErrCredentials) || store.taken != "" {
			t.Fatalf("invalid cookie consumed ceremony: %s %v", store.taken, err)
		}
	}
}

func TestHostedCookiesRejectLegacyAndAmbiguousCredentials(t *testing.T) {
	for _, test := range []struct {
		cookie string
		status int
	}{
		{"savorum_session=parent-forgery", 401},
		{"__Host-savorum_session=valid; savorum_session=parent-forgery", 204},
		{"savorum_session=parent-forgery; __Host-savorum_session=valid", 204},
		{"__Host-savorum_session=valid; __Host-savorum_session=forgery", 401},
		{"__Host-savorum_session=valid; __Host-savorum_library=one; __Host-savorum_library=two", 401},
	} {
		fake := &boundaryRepository{acceptedCSRF: "csrf", principal: identity.Principal{UserID: "user"}, session: identity.Session{ID: "session", UserID: "user"}, libraries: []access.Library{{ID: "library"}}}
		handler := New(fake, Options{Enabled: true, Cookies: identity.BrowserCookies{HostPrefix: true}}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v1/recipes", nil)
		r.Header.Set("Cookie", test.cookie)

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != test.status {
			t.Fatalf("%q status=%d body=%s", test.cookie, w.Code, w.Body.String())
		}
	}
}

func TestHostedLoginRotationAndLogoutUseOneNamespace(t *testing.T) {
	fake := &boundaryRepository{acceptedCSRF: "csrf", principal: identity.Principal{UserID: "user"}, session: identity.Session{ID: "old", UserID: "user"}, authenticate: identity.Principal{UserID: "user"}, credentials: identity.SessionCredentials{Session: identity.Session{ID: "new", AbsoluteExpires: time.Now().Add(time.Hour)}, Token: "fresh", CSRF: "csrf"}}
	h := New(fake, Options{Enabled: true, Cookies: identity.BrowserCookies{HostPrefix: true}, PublicURL: func(context.Context) (string, error) { return "https://tenant.example", nil }}, http.NotFoundHandler())
	r := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v1/account/login", strings.NewReader(`{"login":"reader","password":"password"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Cookie", "savorum_session=parent; __Host-savorum_session=old")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != 200 || len(fake.calls) != 2 || fake.calls[0] != "revoke:old" || fake.calls[1] != "create:user" {
		t.Fatalf("rotation status=%d calls=%v", w.Code, fake.calls)
	}

	for _, c := range w.Result().Cookies() {
		if c.Name == identity.SessionCookie && c.MaxAge != -1 {
			t.Fatal("legacy session minted")
		}

		if strings.HasPrefix(c.Name, "__Host-") && (!c.Secure || c.Path != "/" || c.Domain != "") {
			t.Fatalf("host cookie requirements: %#v", c)
		}
	}

	r = httptest.NewRequestWithContext(t.Context(), "POST", "/api/v1/account/logout", nil)
	r.Header.Set("Cookie", "__Host-savorum_session=fresh")
	r.Header.Set("X-CSRF-Token", "csrf")

	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("logout=%d %s", w.Code, w.Body.String())
	}

	for _, c := range w.Result().Cookies() {
		if c.MaxAge != -1 {
			t.Fatalf("logout cookie %#v", c)
		}
	}
}
