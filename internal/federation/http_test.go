package federation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

type fakeTransaction struct {
	provider, browser, payload string
	expires                    time.Time
}
type fakeStore struct {
	transactions map[string]fakeTransaction
	grants       map[string]NativeGrant
	subjects     []string
	replays      map[string]bool
	registered   []Provider
}

func (f *fakeStore) SaveNativeGrant(_ context.Context, code string, grant NativeGrant, _ time.Time) error {
	if f.grants == nil {
		f.grants = map[string]NativeGrant{}
	}

	f.grants[code] = grant

	return nil
}

func (f *fakeStore) RedeemNativeGrant(_ context.Context, code string) (NativeGrant, identity.Principal, error) {
	grant, ok := f.grants[code]
	if !ok {
		return grant, identity.Principal{}, identity.ErrCredentials
	}

	delete(f.grants, code)

	return grant, identity.Principal{UserID: grant.UserID, DisplayName: "User"}, nil
}

func (f *fakeStore) RegisterProviders(_ context.Context, p []Provider) error {
	f.registered = p
	return nil
}

func (f *fakeStore) SaveFederationTransaction(_ context.Context, state, provider, browser, payload string, expires time.Time) error {
	if f.transactions == nil {
		f.transactions = map[string]fakeTransaction{}
	}

	f.transactions[state] = fakeTransaction{provider, browser, payload, expires}

	return nil
}

func (f *fakeStore) ConsumeFederationTransaction(_ context.Context, state, provider, browser string) (string, error) {
	v, ok := f.transactions[state]
	if !ok || v.provider != provider || v.browser != browser || !v.expires.After(time.Now()) {
		return "", identity.ErrCredentials
	}

	delete(f.transactions, state)

	return v.payload, nil
}

func (f *fakeStore) ClaimFederationAssertion(_ context.Context, provider, id string, _ time.Time) error {
	if f.replays == nil {
		f.replays = map[string]bool{}
	}

	key := provider + ":" + id
	if f.replays[key] {
		return identity.ErrCredentials
	}

	f.replays[key] = true

	return nil
}

func (f *fakeStore) ResolveExternalIdentity(_ context.Context, provider, subject, _, _ string, _ bool, _ string, _ access.Role, _, _ string) (identity.Principal, error) {
	f.subjects = append(f.subjects, provider+":"+subject)
	return identity.Principal{UserID: "user", DisplayName: "User"}, nil
}

func (*fakeStore) ReconcileProviderGroups(context.Context, string, string, []string) error {
	return nil
}

func (*fakeStore) CreateSession(context.Context, string, string, time.Duration, time.Duration) (identity.SessionCredentials, error) {
	return identity.SessionCredentials{AbsoluteExpires: time.Now().Add(time.Hour), Token: "new-session", CSRF: "csrf"}, nil
}

func (*fakeStore) ResolveSession(context.Context, string) (identity.Principal, identity.Session, error) {
	return identity.Principal{}, identity.Session{}, identity.ErrCredentials
}
func (*fakeStore) CheckCSRF(context.Context, string, string) bool      { return false }
func (*fakeStore) RevokeSession(context.Context, string, string) error { return nil }
func testHandler(t *testing.T, p Provider) (*Handler, *fakeStore) {
	t.Helper()

	f := &fakeStore{}

	h, e := New(f, Config{Providers: []Provider{p}}, Options{PublicURL: "https://recipes.example"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	if e != nil {
		t.Fatal(e)
	}

	return h, f
}

func proxyProvider(strict bool) Provider {
	return Provider{ID: "proxy", Kind: "proxy", DisplayName: "Proxy", Proxy: &ProxyConfig{TrustedPeers: []string{"192.0.2.0/24"}, SubjectHeader: "Remote-User", EmailHeader: "Remote-Email", Strict: &strict}}
}

func TestProxyTrustBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, peer string
		strict     bool
		header     []string
		status     int
	}{{"spoofed direct client", "203.0.113.1:1000", false, []string{"victim"}, 401}, {"strict direct client", "203.0.113.1:1000", true, []string{"victim"}, 403}, {"trusted subject", "192.0.2.7:1000", true, []string{"opaque-id"}, 200}, {"duplicate header", "192.0.2.7:1000", true, []string{"attacker", "victim"}, 401}, {"missing subject", "192.0.2.7:1000", true, nil, 401}} {
		t.Run(tc.name, func(t *testing.T) {
			h, f := testHandler(t, proxyProvider(tc.strict))
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://recipes.example/api/v1/auth/proxy/login", strings.NewReader(`{}`))
			r.RemoteAddr = tc.peer
			r.Header.Set("Origin", "https://recipes.example")
			r.Header["Remote-User"] = tc.header
			r.Header.Set("X-Forwarded-For", "192.0.2.1")
			r.Header.Set("Remote-Email", "victim@example.com")

			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != tc.status {
				t.Fatalf("status %d, body %s", w.Code, w.Body.String())
			}

			if tc.status != 200 && len(f.subjects) != 0 {
				t.Fatal("untrusted identity provisioned")
			}

			if tc.status == 200 && f.subjects[0] != "proxy:opaque-id" {
				t.Fatal(f.subjects)
			}
		})
	}
}

func TestProxyStrictCoversStaticExceptHealth(t *testing.T) {
	h, _ := testHandler(t, proxyProvider(true))

	for _, path := range []string{"/", "/assets/app.js", "/api/v1/health"} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		r.RemoteAddr = "203.0.113.1:123"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		want := 403
		if path == "/api/v1/health" {
			want = 204
		}

		if w.Code != want {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
}

func TestLoginRejectsCrossOriginAndUnprovenLink(t *testing.T) {
	for _, tc := range []struct {
		origin, body string
		status       int
	}{{"https://evil.example", `{}`, 403}, {"https://recipes.example", `{"link":true}`, 401}} {
		h, f := testHandler(t, proxyProvider(false))
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/proxy/login", strings.NewReader(tc.body))
		r.RemoteAddr = "192.0.2.2:123"
		r.Header.Set("Remote-User", "subject")
		r.Header.Set("Origin", tc.origin)

		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		if w.Code != tc.status || len(f.subjects) != 0 {
			t.Fatalf("got %d and %v", w.Code, f.subjects)
		}
	}
}

func TestTransactionBrowserBindingExpiryAndReplay(t *testing.T) {
	h, f := testHandler(t, proxyProvider(false))
	p := h.providers["proxy"]
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
	w := httptest.NewRecorder()

	state, e := h.saveTransaction(w, r, p, transaction{Nonce: "nonce"})
	if e != nil {
		t.Fatal(e)
	}

	cookie := w.Result().Cookies()[0]
	wrong := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	wrong.AddCookie(&http.Cookie{Name: cookie.Name, Value: "wrong", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})

	if _, e = h.consume(httptest.NewRecorder(), wrong, p, state); e == nil {
		t.Fatal("wrong browser accepted")
	}

	valid := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	valid.AddCookie(cookie)

	got, e := h.consume(httptest.NewRecorder(), valid, p, state)
	if e != nil || got.Nonce != "nonce" {
		t.Fatal(got, e)
	}

	if _, e = h.consume(httptest.NewRecorder(), valid, p, state); e == nil {
		t.Fatal("replay accepted")
	}

	f.transactions[state] = fakeTransaction{provider: p.ID, browser: cookie.Value, payload: `{}`, expires: time.Now().Add(-time.Second)}
	if _, e = h.consume(httptest.NewRecorder(), valid, p, state); e == nil {
		t.Fatal("expired transaction accepted")
	}
}

func TestProviderListNeverExposesSecrets(t *testing.T) {
	p := Provider{ID: "oidc", Kind: "oidc", DisplayName: "Work", OIDC: &OIDCConfig{Issuer: "https://id.example", ClientID: "client", ClientSecretFile: "/secret/location"}}
	h, _ := testHandler(t, p)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/providers", nil))

	if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "client") {
		t.Fatal(w.Body.String())
	}

	var body map[string]any
	if json.Unmarshal(w.Body.Bytes(), &body) != nil {
		t.Fatal("invalid JSON")
	}
}

func TestDirectoryStableID(t *testing.T) {
	for _, dn := range []string{"cn=Old,ou=People,dc=example", "cn=New,ou=Moved,dc=example"} {
		e := &ldap.Entry{DN: dn, Attributes: []*ldap.EntryAttribute{{Name: "objectGUID", ByteValues: [][]byte{{0x33, 0x22, 0x11, 0, 0x55, 0x44, 0x77, 0x66, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}}}}}

		id, err := directorySubject(e, "objectGUID")
		if err != nil || id != "00112233-4455-6677-8899-aabbccddeeff" {
			t.Fatal(id, err)
		}
	}

	if _, err := directorySubject(&ldap.Entry{}, "entryUUID"); err == nil {
		t.Fatal("missing ID accepted")
	}
}

func (*fakeStore) FederationLinkSessionActive(context.Context, string, string) bool { return false }

func TestRevokedLinkSessionCannotComplete(t *testing.T) {
	h, f := testHandler(t, proxyProvider(false))
	w := httptest.NewRecorder()
	h.complete(w, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil), h.providers["proxy"], transaction{LinkUserID: "user", LinkSessionID: "revoked"}, "subject", "User", "", nil, false)

	if w.Code != 401 || len(f.subjects) != 0 {
		t.Fatal("revoked session linked an identity")
	}
}

func (*fakeStore) CredentialRateLimit(context.Context, string, string, int) error { return nil }

func (*fakeStore) BindFederationIssuer(context.Context, string, string) error { return nil }

func TestBearerCannotAuthenticateOrLinkThroughFederation(t *testing.T) {
	for _, link := range []bool{false, true} {
		h, f := testHandler(t, proxyProvider(false))

		body := `{}`
		if link {
			body = `{"link":true}`
		}

		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/proxy/login", strings.NewReader(body))
		r.RemoteAddr = "192.0.2.7:1000"
		r.Header.Set("Origin", "https://recipes.example")
		r.Header.Set("Remote-User", "proxy-subject")
		r.Header.Set("Authorization", "Bearer read-only-token")
		r.Header.Set("X-CSRF-Token", "csrf")
		r.AddCookie(&http.Cookie{Name: "savorum_session", Value: "browser-session", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})

		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		if w.Code != http.StatusForbidden || len(f.subjects) != 0 || len(w.Result().Cookies()) != 0 {
			t.Fatalf("link=%t bearer crossed federation boundary: status=%d subjects=%v cookies=%v", link, w.Code, f.subjects, w.Result().Cookies())
		}
	}
}

func TestMultipleStrictGatewaysPreserveProviderTrust(t *testing.T) {
	first := proxyProvider(true)
	second := proxyProvider(true)
	second.ID = "second"
	second.Proxy.TrustedPeers = []string{"198.51.100.0/24"}
	f := &fakeStore{}

	h, err := New(f, Config{Providers: []Provider{first, second}}, Options{PublicURL: "https://recipes.example"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	if err != nil {
		t.Fatal(err)
	}

	for _, peer := range []string{"192.0.2.5:1000", "198.51.100.5:1000"} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		r.RemoteAddr = peer
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		if w.Code != http.StatusNoContent {
			t.Fatal("configured gateway rejected", peer, w.Code)
		}
	}

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/second/login", strings.NewReader(`{}`))
	r.RemoteAddr = "192.0.2.5:1000"
	r.Header.Set("Origin", "https://recipes.example")
	r.Header.Set("Remote-User", "subject")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized || len(f.subjects) != 0 {
		t.Fatal("first gateway impersonated second provider", w.Code, f.subjects)
	}
}

func TestDirectSignInReturnsTheAccountUserShape(t *testing.T) {
	h, _ := testHandler(t, proxyProvider(false))
	w := httptest.NewRecorder()
	h.complete(w, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil), h.providers["proxy"], transaction{}, "subject", "User", "", nil, false)

	var body struct {
		User struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"user"`
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != http.StatusOK || body.User.ID != "user" || body.User.DisplayName != "User" || body.CSRFToken != "csrf" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func (f *fakeStore) ResolveManagedIdentity(_ context.Context, provider, subject, user, library string) (identity.Principal, error) {
	if user != "user" || library != "default" {
		return identity.Principal{}, identity.ErrCredentials
	}

	f.subjects = append(f.subjects, provider+":"+subject)

	return identity.Principal{UserID: user, DisplayName: "Member"}, nil
}
