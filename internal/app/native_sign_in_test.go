package app_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/savorum/savorum/internal/app"
	"github.com/savorum/savorum/internal/configuration"
	"github.com/savorum/savorum/internal/contracttest"
	"github.com/savorum/savorum/internal/federation"
	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
)

const nativeReturnTarget = "savorum://auth/callback"

// fakeIdentityProvider signs in whichever subject the test chooses next.
type fakeIdentityProvider struct {
	t      *testing.T
	server *httptest.Server
	key    *rsa.PrivateKey

	mu      sync.Mutex
	subject string
	cancel  bool
	tamper  bool
	codes   map[string][3]string
}

func newFakeIdentityProvider(t *testing.T) *fakeIdentityProvider {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	p := &fakeIdentityProvider{t: t, key: key, subject: "alice", codes: map[string][3]string{}}
	p.server = httptest.NewTLSServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.server.Close)

	return p
}

func (p *fakeIdentityProvider) next(subject string, cancel, tamper bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.subject, p.cancel, p.tamper = subject, cancel, tamper
}

func (p *fakeIdentityProvider) serve(w http.ResponseWriter, r *http.Request) {
	issuer := p.server.URL

	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		writeTestJSON(w, map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
	case "/keys":
		writeTestJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "key", Algorithm: "RS256", Use: "sig"}}})
	case "/authorize":
		q := r.URL.Query()

		p.mu.Lock()
		subject, cancel, tamper := p.subject, p.cancel, p.tamper
		p.mu.Unlock()

		back := url.Values{"state": {q.Get("state")}}
		if tamper {
			back.Set("state", q.Get("state")+"x")
		}

		if cancel {
			back.Set("error", "access_denied")
		} else {
			code := randomTestValue(p.t)

			p.mu.Lock()
			p.codes[code] = [3]string{subject, q.Get("nonce"), q.Get("code_challenge")}
			p.mu.Unlock()
			back.Set("code", code)
		}

		//nolint:gosec // Test provider; Savorum's callback is the only requested return.
		http.Redirect(w, r, q.Get("redirect_uri")+"?"+back.Encode(), http.StatusFound)
	case "/token":
		if r.ParseForm() != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}

		p.mu.Lock()
		grant, ok := p.codes[r.Form.Get("code")]
		delete(p.codes, r.Form.Get("code"))
		p.mu.Unlock()

		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != grant[2] {
			http.Error(w, "invalid_grant", http.StatusBadRequest)
			return
		}

		claims, err := json.Marshal(struct {
			Issuer   string `json:"iss"`
			Subject  string `json:"sub"`
			Audience string `json:"aud"`
			Nonce    string `json:"nonce"`
			Name     string `json:"name"`
			Expiry   int64  `json:"exp"`
			IssuedAt int64  `json:"iat"`
		}{issuer, grant[0], "client", grant[1], strings.ToUpper(grant[0][:1]) + grant[0][1:], time.Now().Add(time.Hour).Unix(), time.Now().Unix()})
		if err != nil {
			p.t.Error(err)
			return
		}

		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: p.key, KeyID: "key"}}, nil)
		if err != nil {
			p.t.Error(err)
			return
		}

		signed, err := signer.Sign(claims)
		if err != nil {
			p.t.Error(err)
			return
		}

		raw, err := signed.CompactSerialize()
		if err != nil {
			p.t.Error(err)
			return
		}

		writeTestJSON(w, map[string]any{"access_token": "discarded", "token_type": "Bearer", "id_token": raw})
	default:
		http.NotFound(w, r)
	}
}

type nativeFixture struct {
	t        *testing.T
	idp      *fakeIdentityProvider
	server   *httptest.Server
	database string
}

// newNativeFixture serves the complete application, including the outer
// security handler, over TLS at its canonical public URL.
func newNativeFixture(t *testing.T) *nativeFixture {
	t.Helper()

	idp := newFakeIdentityProvider(t)

	// The provider client uses the default transport; trust the test certificate.
	original := http.DefaultTransport
	http.DefaultTransport = idp.server.Client().Transport

	t.Cleanup(func() { http.DefaultTransport = original })

	var handler atomic.Pointer[http.Handler]

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { (*handler.Load()).ServeHTTP(w, r) }))
	server.StartTLS()
	t.Cleanup(server.Close)

	directory := t.TempDir()
	database := filepath.Join(directory, "library.db")

	store, err := sqlite.Open(t.Context(), database)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	secret := filepath.Join(directory, "client-secret")
	if err = os.WriteFile(secret, []byte("client-secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	publicURL := server.URL

	settings, err := configuration.NewService(store, configuration.InstanceOverrides{PublicURL: &publicURL})
	if err != nil {
		t.Fatal(err)
	}

	providers := federation.Config{Providers: []federation.Provider{{ID: "work", Kind: "oidc", DisplayName: "Work", AutoProvision: true, OIDC: &federation.OIDCConfig{Issuer: idp.server.URL, ClientID: "client", ClientSecretFile: secret}}}}

	application, err := app.New(store, units.DefaultCatalog(), app.Options{AuthEnabled: true, Providers: providers, Configuration: settings})
	if err != nil {
		t.Fatal(err)
	}

	secured := httpsecurity.NewHandler(contracttest.Handler(t, application.Handler), httpsecurity.Options{ApplicationOrigin: func(context.Context) (string, error) { return publicURL, nil }})
	handler.Store(&secured)

	return &nativeFixture{t: t, idp: idp, server: server, database: database}
}

// client copies the shared test client, which trusts the test certificate.
func (f *nativeFixture) client() *http.Client {
	c := *f.server.Client()
	return &c
}

// browser is the authentication browser: its own cookie jar, following
// redirects until the app's custom scheme would take over.
func (f *nativeFixture) browser() *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		f.t.Fatal(err)
	}

	client := f.client()
	client.Jar = jar
	client.CheckRedirect = func(r *http.Request, _ []*http.Request) error {
		if r.URL.Scheme == "savorum" {
			return http.ErrUseLastResponse
		}

		return nil
	}

	return client
}

// native is the app's HTTP client: no cookie jar and no redirects, like the
// ephemeral production transport.
func (f *nativeFixture) native() *http.Client {
	client := f.client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	return client
}

type ceremony struct {
	state, verifier string
}

func newCeremony(t *testing.T) ceremony {
	t.Helper()

	return ceremony{state: randomTestValue(t)[:32], verifier: randomTestValue(t)}
}

func (c ceremony) entry(base string) string {
	sum := sha256.Sum256([]byte(c.verifier))

	return base + "/api/v1/auth/work/native?" + url.Values{
		"redirect_uri": {nativeReturnTarget}, "state": {c.state},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
	}.Encode()
}

// visit follows a browser navigation and returns the final response.
type visited struct {
	StatusCode int
	Header     http.Header
}

func visit(t *testing.T, client *http.Client, target string) visited {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	_ = response.Body.Close()

	return visited{response.StatusCode, response.Header}
}

// returned completes a browser ceremony and returns the app's callback query.
func (f *nativeFixture) returned(browser *http.Client, c ceremony) url.Values {
	f.t.Helper()

	response := visit(f.t, browser, c.entry(f.server.URL))
	location, err := url.Parse(response.Header.Get("Location"))

	if response.StatusCode != http.StatusSeeOther || err != nil || location.Scheme+"://"+location.Host+location.Path != nativeReturnTarget {
		f.t.Fatalf("ceremony did not return to the app: status=%d location=%q", response.StatusCode, response.Header.Get("Location"))
	}

	if location.Query().Get("state") != c.state {
		f.t.Fatalf("state changed: %q", location.Query().Get("state"))
	}

	return location.Query()
}

type redemption struct {
	status   int
	session  string
	csrf     string
	userName string
}

func (f *nativeFixture) redeem(code, verifier, origin, previous string) redemption {
	f.t.Helper()

	body, err := json.Marshal(map[string]string{"code": code, "code_verifier": verifier, "redirect_uri": nativeReturnTarget})
	if err != nil {
		f.t.Fatal(err)
	}

	request, err := http.NewRequestWithContext(f.t.Context(), http.MethodPost, f.server.URL+"/api/v1/auth/native-sessions", strings.NewReader(string(body)))
	if err != nil {
		f.t.Fatal(err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)

	if previous != "" {
		request.Header.Set("Cookie", "savorum_session="+previous)
	}

	response, err := f.native().Do(request)
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()

	out := redemption{status: response.StatusCode}

	var value struct {
		User struct {
			DisplayName string `json:"display_name"`
		} `json:"user"`
		CSRFToken string `json:"csrf_token"`
	}

	if response.StatusCode == http.StatusOK {
		if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
			f.t.Fatal(err)
		}
	}

	for _, cookie := range response.Cookies() {
		if cookie.Name == "savorum_session" && cookie.MaxAge > 0 {
			out.session = cookie.Value
		}
	}

	out.csrf, out.userName = value.CSRFToken, value.User.DisplayName

	return out
}

// account reads the signed-in user with only the native session header.
func (f *nativeFixture) account(session string) string {
	f.t.Helper()

	request, err := http.NewRequestWithContext(f.t.Context(), http.MethodGet, f.server.URL+"/api/v1/account", nil)
	if err != nil {
		f.t.Fatal(err)
	}

	request.Header.Set("Cookie", "savorum_session="+session)

	response, err := f.native().Do(request)
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()

	var value struct {
		User *struct {
			DisplayName string `json:"display_name"`
		} `json:"user"`
	}
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		f.t.Fatal(err)
	}

	if value.User == nil {
		return ""
	}

	return value.User.DisplayName
}

func browserSession(t *testing.T, client *http.Client, base string) string {
	t.Helper()

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}

	for _, cookie := range client.Jar.Cookies(u) {
		if cookie.Name == "savorum_session" {
			return cookie.Value
		}
	}

	return ""
}

func TestNativeProviderSignInOldFlowLosesTheTransactionCookie(t *testing.T) {
	f := newNativeFixture(t)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.server.URL+"/api/v1/auth/work/login", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", f.server.URL)

	response, err := f.native().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var start struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	if err := json.NewDecoder(response.Body).Decode(&start); err != nil || start.AuthorizationURL == "" {
		t.Fatalf("native start: status=%d err=%v", response.StatusCode, err)
	}

	// The transaction cookie went to the native client, which keeps no cookies.
	browser := f.browser()
	if final := visit(t, browser, start.AuthorizationURL); final.StatusCode != http.StatusUnauthorized {
		t.Fatalf("callback without the transaction cookie: status %d", final.StatusCode)
	}

	if browserSession(t, browser, f.server.URL) != "" {
		t.Fatal("browser received a session")
	}
}

func TestNativeProviderSignInCompletesInTheBrowserAndRedeemsNatively(t *testing.T) {
	f := newNativeFixture(t)
	browser := f.browser()
	c := newCeremony(t)

	back := f.returned(browser, c)
	if back.Get("code") == "" || back.Get("error") != "" {
		t.Fatalf("no code returned: %v", back)
	}

	if browserSession(t, browser, f.server.URL) != "" {
		t.Fatal("native ceremony left a browser session")
	}

	got := f.redeem(back.Get("code"), c.verifier, f.server.URL, "")
	if got.status != http.StatusOK || got.session == "" || got.csrf == "" || got.userName != "Alice" {
		t.Fatalf("redemption: %+v", got)
	}

	if name := f.account(got.session); name != "Alice" {
		t.Fatalf("session belongs to %q", name)
	}

	if replay := f.redeem(back.Get("code"), c.verifier, f.server.URL, ""); replay.status != http.StatusUnauthorized || replay.session != "" {
		t.Fatalf("replayed code accepted: %+v", replay)
	}
}

func TestNativeProviderSignInRejectsUnboundEntries(t *testing.T) {
	f := newNativeFixture(t)
	c := newCeremony(t)

	valid, err := url.Parse(c.entry(f.server.URL))
	if err != nil {
		t.Fatal(err)
	}

	for name, change := range map[string]func(url.Values){
		"missing state":        func(q url.Values) { q.Del("state") },
		"short state":          func(q url.Values) { q.Set("state", "short") },
		"missing challenge":    func(q url.Values) { q.Del("code_challenge") },
		"plain challenge":      func(q url.Values) { q.Set("code_challenge_method", "plain") },
		"other return target":  func(q url.Values) { q.Set("redirect_uri", "https://evil.example/callback") },
		"extended return path": func(q url.Values) { q.Set("redirect_uri", nativeReturnTarget+"/more") },
		"repeated target":      func(q url.Values) { q.Add("redirect_uri", "savorum://other") },
	} {
		t.Run(name, func(t *testing.T) {
			q := valid.Query()
			change(q)

			u := *valid
			u.RawQuery = q.Encode()

			response := visit(t, f.browser(), u.String())
			if response.StatusCode != http.StatusBadRequest || response.Header.Get("Location") != "" {
				t.Fatalf("status=%d location=%q", response.StatusCode, response.Header.Get("Location"))
			}
		})
	}
}

func TestNativeProviderSignInRejectsChangedState(t *testing.T) {
	f := newNativeFixture(t)
	f.idp.next("alice", false, true)

	response := visit(t, f.browser(), newCeremony(t).entry(f.server.URL))
	if response.StatusCode != http.StatusUnauthorized || response.Header.Get("Location") != "" {
		t.Fatalf("changed state: status=%d location=%q", response.StatusCode, response.Header.Get("Location"))
	}
}

func TestNativeProviderSignInRequiresTheAuthenticationBrowser(t *testing.T) {
	f := newNativeFixture(t)
	c := newCeremony(t)

	// A different cookie jar cannot finish a ceremony another browser began.
	started := f.browser()
	started.CheckRedirect = func(r *http.Request, _ []*http.Request) error {
		if r.URL.Path == "/authorize" {
			return http.ErrUseLastResponse
		}

		return nil
	}

	provider := visit(t, started, c.entry(f.server.URL)).Header.Get("Location")
	if response := visit(t, f.browser(), provider); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("other browser completed the ceremony: %d", response.StatusCode)
	}
}

func TestNativeProviderSignInRedemptionProof(t *testing.T) {
	f := newNativeFixture(t)

	t.Run("wrong verifier consumes the code", func(t *testing.T) {
		c := newCeremony(t)
		code := f.returned(f.browser(), c).Get("code")

		if got := f.redeem(code, newCeremony(t).verifier, f.server.URL, ""); got.status != http.StatusUnauthorized || got.session != "" {
			t.Fatalf("wrong verifier: %+v", got)
		}

		if got := f.redeem(code, c.verifier, f.server.URL, ""); got.status != http.StatusUnauthorized {
			t.Fatalf("code survived a failed proof: %+v", got)
		}
	})

	t.Run("wrong origin", func(t *testing.T) {
		c := newCeremony(t)
		code := f.returned(f.browser(), c).Get("code")

		if got := f.redeem(code, c.verifier, "https://evil.example", ""); got.status != http.StatusForbidden || got.session != "" {
			t.Fatalf("cross-origin redemption: %+v", got)
		}

		if got := f.redeem(code, c.verifier, f.server.URL, ""); got.status != http.StatusOK {
			t.Fatalf("rejected origin consumed the code: %+v", got)
		}
	})

	t.Run("expired code", func(t *testing.T) {
		c := newCeremony(t)
		code := f.returned(f.browser(), c).Get("code")
		f.expireGrants()

		if got := f.redeem(code, c.verifier, f.server.URL, ""); got.status != http.StatusUnauthorized || got.session != "" {
			t.Fatalf("expired code: %+v", got)
		}
	})

	t.Run("unknown code", func(t *testing.T) {
		if got := f.redeem(randomTestValue(t), newCeremony(t).verifier, f.server.URL, ""); got.status != http.StatusUnauthorized {
			t.Fatalf("unknown code: %+v", got)
		}
	})
}

func TestNativeProviderSignInSwitchesAccounts(t *testing.T) {
	f := newNativeFixture(t)
	browser := f.browser()

	// The same browser also holds a web session for Alice.
	webRequest, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.server.URL+"/api/v1/auth/work/login", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}

	webRequest.Header.Set("Content-Type", "application/json")
	webRequest.Header.Set("Origin", f.server.URL)

	webStart, err := browser.Do(webRequest)
	if err != nil {
		t.Fatal(err)
	}

	var start struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	if err := json.NewDecoder(webStart.Body).Decode(&start); err != nil {
		t.Fatal(err)
	}

	_ = webStart.Body.Close()

	visit(t, browser, start.AuthorizationURL)

	web := browserSession(t, browser, f.server.URL)
	if f.account(web) != "Alice" {
		t.Fatal("web sign-in did not complete")
	}

	first := newCeremony(t)
	alice := f.redeem(f.returned(browser, first).Get("code"), first.verifier, f.server.URL, "")

	f.idp.next("bob", false, false)

	second := newCeremony(t)
	bob := f.redeem(f.returned(browser, second).Get("code"), second.verifier, f.server.URL, alice.session)

	if bob.status != http.StatusOK || bob.userName != "Bob" || f.account(bob.session) != "Bob" {
		t.Fatalf("switched redemption: %+v", bob)
	}

	if f.account(alice.session) != "" {
		t.Fatal("the app's previous session survived the switch")
	}

	if browserSession(t, browser, f.server.URL) != web || f.account(web) != "Alice" {
		t.Fatal("native sign-in changed the browser's web session")
	}
}

func TestNativeProviderSignInCancellationReturnsWithoutACode(t *testing.T) {
	f := newNativeFixture(t)
	f.idp.next("alice", true, false)

	browser := f.browser()
	c := newCeremony(t)

	back := f.returned(browser, c)
	if back.Get("error") != "access_denied" || back.Get("code") != "" {
		t.Fatalf("cancelled ceremony returned %v", back)
	}

	if browserSession(t, browser, f.server.URL) != "" {
		t.Fatal("cancelled ceremony created a session")
	}

	f.idp.next("alice", false, false)

	// The cancelled transaction is gone; a new ceremony still works.
	retry := newCeremony(t)
	if got := f.redeem(f.returned(browser, retry).Get("code"), retry.verifier, f.server.URL, ""); got.status != http.StatusOK {
		t.Fatalf("retry after cancellation: %+v", got)
	}
}

func TestNativeProviderSignInIsAdvertised(t *testing.T) {
	f := newNativeFixture(t)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.server.URL+"/api/v1/capabilities", nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := f.native().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var value struct {
		AuthFeatures []string `json:"auth_features"`
	}
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil || strings.Join(value.AuthFeatures, ",") != "auth.native_sign_in,auth.native_link,auth.native_browser_sign_in" {
		t.Fatalf("auth features: %v %v", value.AuthFeatures, err)
	}
}

func (f *nativeFixture) expireGrants() {
	f.t.Helper()

	database, err := sql.Open("sqlite", f.database)
	if err != nil {
		f.t.Fatal(err)
	}
	defer database.Close()

	result, err := database.ExecContext(f.t.Context(), `UPDATE native_sign_in_grants SET expires_at=unixepoch()-1`)
	if err != nil {
		f.t.Fatal(err)
	}

	if count, err := result.RowsAffected(); err != nil || count == 0 {
		f.t.Fatal(errors.Join(err, errors.New("no grant to expire")))
	}
}

func randomTestValue(t *testing.T) string {
	t.Helper()

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}

	return hex.EncodeToString(b)
}

func writeTestJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	//nolint:errchkjson // Test fixture responses are plain maps and key sets.
	_ = json.NewEncoder(w).Encode(v)
}
