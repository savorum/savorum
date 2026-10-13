package federation

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/identity"
)

const (
	testVerifier    = "verifier-verifier-verifier-verifier-verifier"
	testNativeState = "state-state-state-state"
)

func nativeEntryQuery(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))

	return url.Values{"redirect_uri": {NativeRedirectURIs[0]}, "state": {testNativeState}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}.Encode()
}

// nativeReturnQuery checks a callback response returns to the app with its
// state and no browser session.
func nativeReturnQuery(t *testing.T, w *httptest.ResponseRecorder) url.Values {
	t.Helper()

	location, err := url.Parse(w.Header().Get("Location"))
	if w.Code != http.StatusSeeOther || err != nil || !strings.HasPrefix(location.String(), NativeRedirectURIs[0]+"?") || location.Query().Get("state") != testNativeState {
		t.Fatalf("status=%d location=%q", w.Code, w.Header().Get("Location"))
	}

	for _, c := range w.Result().Cookies() {
		if c.Name == "savorum_session" {
			t.Fatal("native callback set a browser session")
		}
	}

	return location.Query()
}

func TestNativeEntryAcceptsOnlyRedirectProviders(t *testing.T) {
	h, _ := testHandler(t, proxyProvider(false))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/proxy/native?"+nativeEntryQuery(testVerifier), nil))

	if w.Code != http.StatusNotFound || len(w.Result().Cookies()) != 0 {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestNativeRedemptionChecksTheReturnTarget(t *testing.T) {
	h, f := testHandler(t, proxyProvider(false))
	sum := sha256.Sum256([]byte(testVerifier))
	f.grants = map[string]NativeGrant{"code": {UserID: "user", Provider: "proxy", Challenge: base64.RawURLEncoding.EncodeToString(sum[:]), RedirectURI: NativeRedirectURIs[0]}}

	body, err := json.Marshal(map[string]string{"code": "code", "code_verifier": testVerifier, "redirect_uri": "savorum://other"})
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/native-sessions", strings.NewReader(string(body)))
	r.Header.Set("Origin", "https://recipes.example")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized || len(f.grants) != 0 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("other return target: %d", w.Code)
	}
}

func TestVerifierMatchesOnlyS256OfAWellFormedVerifier(t *testing.T) {
	sum := sha256.Sum256([]byte(testVerifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	if !verifierMatches(testVerifier, challenge) || verifierMatches(testVerifier+"x", challenge) || verifierMatches(challenge, challenge) {
		t.Fatal("verifier check")
	}

	short := sha256.Sum256([]byte("short"))
	if verifierMatches("short", base64.RawURLEncoding.EncodeToString(short[:])) {
		t.Fatal("short verifier accepted")
	}
}

func (*fakeStore) SaveNativeLinkTicket(context.Context, string, NativeLinkTicket, time.Time) error {
	return nil
}

func (*fakeStore) ConsumeNativeLinkTicket(context.Context, string) (NativeLinkTicket, error) {
	return NativeLinkTicket{}, identity.ErrCredentials
}

func (*fakeStore) SaveNativeLinkGrant(context.Context, string, NativeLinkGrant, time.Time) error {
	return nil
}

func (*fakeStore) RedeemNativeLinkGrant(context.Context, string) (NativeLinkGrant, error) {
	return NativeLinkGrant{}, identity.ErrCredentials
}

func TestNativeLinkTicketAcceptsOnlyRedirectProviders(t *testing.T) {
	h, _ := testHandler(t, proxyProvider(false))
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/proxy/native-link-tickets", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://recipes.example")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d", w.Code)
	}
}
