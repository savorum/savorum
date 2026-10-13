package httpsecurity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOnlyConfiguredSAMLACSAllowsCrossSitePOST(t *testing.T) {
	h := NewHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), Options{SAMLACSPaths: []string{"/api/v1/auth/work/acs"}, ApplicationOrigin: func(context.Context) (string, error) { return "https://recipes.example", nil }})

	for _, tc := range []struct {
		method, path string
		want         int
	}{{"POST", "/api/v1/auth/work/acs", 204}, {"PUT", "/api/v1/auth/work/acs", 403}, {"POST", "/api/v1/auth/other/acs", 403}, {"POST", "/api/v1/auth/work/login", 403}, {"POST", "/api/v1/recipes", 403}} {
		r := httptest.NewRequestWithContext(t.Context(), tc.method, "https://recipes.example"+tc.path, nil)
		r.Header.Set("Origin", "https://idp.example")

		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		if w.Code != tc.want {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
	}
}

func TestImmediatePeerSurvivesForwarding(t *testing.T) {
	peers, e := ParseTrustedProxies([]string{"192.0.2.0/24"})
	if e != nil {
		t.Fatal(e)
	}

	called := false
	h := NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true

		if ImmediatePeer(r) != "192.0.2.5:123" {
			t.Fatal(ImmediatePeer(r))
		}

		if r.RemoteAddr == ImmediatePeer(r) {
			t.Fatal("forwarded client was not resolved")
		}

		w.WriteHeader(http.StatusNoContent)
	}), Options{TrustedProxies: peers})
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://recipes.example/", nil)
	r.RemoteAddr = "192.0.2.5:123"
	r.Header.Set("X-Forwarded-For", "203.0.113.5")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if !called {
		t.Fatal(w.Code, w.Body.String())
	}
}
