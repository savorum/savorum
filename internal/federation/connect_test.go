package federation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/savorum/savorum/internal/httpsecurity"
)

func connectContext(t *testing.T) context.Context {
	t.Helper()

	const origin = "https://acorn-apple-basil-basket.connect.test"

	var verified *http.Request

	handler := httpsecurity.ConnectTransport(httpsecurity.NewHandler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { verified = r }), httpsecurity.Options{ListenAddress: "0.0.0.0:8080", ConnectOrigin: origin}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, origin+"/", nil))

	if verified == nil {
		t.Fatal("trusted context unavailable")
	}

	return verified.Context()
}

func TestFederationTransactionCannotMoveBetweenOrigins(t *testing.T) {
	h, _ := testHandler(t, Provider{ID: "oidc", Kind: "oidc", DisplayName: "SSO", OIDC: &OIDCConfig{Issuer: "https://issuer.example", ClientID: "client"}})
	p := h.providers["oidc"]

	remote := connectContext(t)
	for _, mismatch := range []bool{false, true} {
		start := httptest.NewRequestWithContext(remote, http.MethodPost, "https://acorn-apple-basil-basket.connect.test/api/v1/auth/oidc/login", nil)
		w := httptest.NewRecorder()

		state, err := h.saveTransaction(w, start, p, transaction{})
		if err != nil {
			t.Fatal(err)
		}

		ctx := remote
		if mismatch {
			ctx = t.Context()
		}

		callback := httptest.NewRequestWithContext(ctx, http.MethodGet, "https://recipes.example/api/v1/auth/oidc/callback", nil)
		callback.AddCookie(w.Result().Cookies()[0])

		_, err = h.consume(httptest.NewRecorder(), callback, p, state)
		if (err != nil) != mismatch {
			t.Fatalf("origin binding mismatch=%v: %v", mismatch, err)
		}
	}
}

func TestSAMLConnectMetadataUsesAssignedOrigin(t *testing.T) {
	p, _, _ := samlTestProvider(t)
	h, _ := testHandler(t, p)

	sp, err := h.samlLocalProvider(connectContext(t), p)
	if err != nil {
		t.Fatal(err)
	}

	if sp.AcsURL.String() != "https://acorn-apple-basil-basket.connect.test/api/v1/auth/"+p.ID+"/acs" || sp.EntityID != "https://recipes.example/api/v1/auth/"+p.ID+"/metadata" {
		t.Fatal(sp.AcsURL, sp.EntityID)
	}
}
