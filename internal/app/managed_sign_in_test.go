package app_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/app"
	"github.com/savorum/savorum/internal/configuration"
	"github.com/savorum/savorum/internal/federation"
	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
	"golang.org/x/oauth2"
)

func TestManagedWebAndNativeSignInUseOrdinaryLocalSessions(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()

	db, err := sqlite.Open(ctx, filepath.Join(dir, "tenant.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: key}, (&jose.SignerOptions{}).WithType(federation.ManagedGrantType).WithHeader("kid", "key"))
	if err != nil {
		t.Fatal(err)
	}

	var (
		mu                                         sync.Mutex
		expectedChallenge, subject, userID, issuer string
	)

	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/redeem" || r.Method != http.MethodPost || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("exchange leaked credentials or wrong route")
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		var in struct {
			Code     string `json:"code"`
			Verifier string `json:"code_verifier"`
			Audience string `json:"audience"`
			Redirect string `json:"redirect_uri"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		mu.Lock()
		defer mu.Unlock()

		if in.Code != "one-time-code" || in.Audience != "household" || in.Redirect != "https://tenant.example/api/v1/auth/cloud/callback" || oauth2.S256ChallengeFromVerifier(in.Verifier) != expectedChallenge {
			t.Error("exchange binding mismatch")
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		now := time.Now()
		grant := federation.ManagedGrant{Issuer: issuer, Subject: subject, Audience: jwt.Audience{"household"}, IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(time.Minute)), ID: oauth2.GenerateVerifier(), Origin: "https://tenant.example", CodeChallenge: expectedChallenge, LocalUserID: userID, LibraryID: "default"}

		raw, err := jwt.Signed(signer).Claims(grant).Serialize()
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"grant": raw})
	}))
	defer provider.Close()

	issuer = provider.URL
	originalTransport := http.DefaultTransport
	http.DefaultTransport = provider.Client().Transport

	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	b := hosting.Bootstrap{OperationID: "bootstrap", TenantID: "household", PublicURL: "https://tenant.example", ProviderID: "cloud", Issuer: issuer, Owner: hosting.Owner{Subject: "owner", DisplayName: "Owner"}, LibraryName: "Recipes"}

	owner, err := db.BootstrapHosting(ctx, b)
	if err != nil {
		t.Fatal(err)
	}

	ownerCtx := identity.WithPrincipal(ctx, identity.Principal{UserID: owner.OwnerUserID, InstanceAdmin: true})

	_, invitation, err := db.CreateInvitation(ownerCtx, "default", access.Viewer, "", 1)
	if err != nil {
		t.Fatal(err)
	}

	member, err := db.BootstrapHostingMember(ctx, hosting.MemberRequest{OperationID: "member", TenantID: "household", Subject: "member", DisplayName: "Member", InvitationToken: invitation})
	if err != nil {
		t.Fatal(err)
	}

	keys, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: public, KeyID: "key", Algorithm: "EdDSA", Use: "sig"}}})
	if err != nil {
		t.Fatal(err)
	}

	keyPath := filepath.Join(dir, "keys.json")
	if err = os.WriteFile(keyPath, keys, 0o600); err != nil {
		t.Fatal(err)
	}

	providers := federation.Config{Providers: []federation.Provider{{ID: "cloud", Kind: "managed", DisplayName: "Cloud", Managed: &federation.ManagedConfig{Issuer: issuer, Audience: "household", AuthorizeURL: issuer + "/authorize", TokenURL: issuer + "/redeem", KeysFile: keyPath}}}}
	origin := b.PublicURL

	settings, err := configuration.NewService(db, configuration.InstanceOverrides{PublicURL: &origin})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = app.New(db, units.DefaultCatalog(), app.Options{Configuration: settings}); err == nil {
		t.Fatal("hosted instance admitted unauthenticated mode")
	}

	application, err := app.New(db, units.DefaultCatalog(), app.Options{AuthEnabled: true, Providers: providers, Configuration: settings})
	if err != nil {
		t.Fatal(err)
	}

	if application.OpenAPI.OpenAPI().Components.SecuritySchemes["browserSession"].Name != "__Host-savorum_session" {
		t.Fatal("hosted OpenAPI selected legacy cookie")
	}

	handler := application.Handler
	perform := func(method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
		request.Header.Set("Origin", origin)
		request.Header.Set("Content-Type", "application/json")

		for _, c := range cookies {
			request.AddCookie(c)
		}

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, request)

		return w
	}

	for _, who := range []struct{ subject, user string }{{"owner", owner.OwnerUserID}, {"member", member.UserID}} {
		verifier := oauth2.GenerateVerifier()
		now := time.Now()
		grant := federation.ManagedGrant{Issuer: issuer, Subject: who.subject, Audience: jwt.Audience{"household"}, IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(time.Minute)), ID: oauth2.GenerateVerifier(), Origin: origin, CodeChallenge: oauth2.S256ChallengeFromVerifier(verifier), LocalUserID: who.user, LibraryID: "default"}

		raw, err := jwt.Signed(signer).Claims(grant).Serialize()
		if err != nil {
			t.Fatal(err)
		}

		body, err := json.Marshal(map[string]string{"grant": raw, "code_verifier": verifier})
		if err != nil {
			t.Fatal(err)
		}

		prior, err := db.CreateSession(ctx, who.user, "cloud", time.Hour, time.Hour)
		if err != nil {
			t.Fatal(err)
		}

		completed := perform("POST", "/api/v1/auth/cloud/managed-sessions", string(body),
			&http.Cookie{Name: "savorum_session", Value: "forged-parent", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode},
			&http.Cookie{Name: "__Host-savorum_session", Value: prior.Token, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		if completed.Code != http.StatusOK {
			t.Fatalf("direct managed session: %d %s", completed.Code, completed.Body.String())
		}

		if _, _, err := db.ResolveSession(ctx, prior.Token); !errors.Is(err, identity.ErrCredentials) {
			t.Fatal("managed session did not revoke protected predecessor", err)
		}

		var localToken string

		for _, c := range completed.Result().Cookies() {
			if c.Name == "__Host-savorum_session" {
				localToken = c.Value
			}
		}

		principal, _, err := db.ResolveSession(ctx, localToken)
		if err != nil || principal.UserID != who.user {
			t.Fatal("direct handoff bypassed local session", err)
		}

		if perform("POST", "/api/v1/auth/cloud/managed-sessions", string(body)).Code != http.StatusUnauthorized {
			t.Fatal("direct grant replay")
		}

		for _, native := range []bool{false, true} {
			mu.Lock()
			subject, userID = who.subject, who.user
			mu.Unlock()

			verifier := oauth2.GenerateVerifier()

			entry := "/api/v1/auth/cloud/start"
			if native {
				entry = "/api/v1/auth/cloud/native?" + url.Values{"redirect_uri": {nativeReturnTarget}, "state": {"native-state-native-state"}, "code_challenge": {oauth2.S256ChallengeFromVerifier(verifier)}, "code_challenge_method": {"S256"}}.Encode()
			}

			started := perform("GET", entry, "")

			location, err := url.Parse(started.Header().Get("Location"))
			if err != nil || started.Code != 303 {
				t.Fatalf("start %d %s", started.Code, started.Body.String())
			}

			mu.Lock()
			expectedChallenge = location.Query().Get("code_challenge")
			mu.Unlock()

			callback := "/api/v1/auth/cloud/callback?" + url.Values{"state": {location.Query().Get("state")}, "code": {"one-time-code"}}.Encode()

			transaction := started.Result().Cookies()[0]
			if transaction.Name != "__Host-savorum_auth_cloud" || !transaction.Secure || transaction.Path != "/" || transaction.Domain != "" {
				t.Fatalf("unsafe hosted transaction cookie: %#v", transaction)
			}

			if rejected := perform("GET", callback, "", transaction, &http.Cookie{Name: transaction.Name, Value: transaction.Value, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}); rejected.Code != 401 {
				t.Fatal("duplicate transaction cookie accepted")
			}

			if rejected := perform("GET", callback, "", &http.Cookie{Name: "savorum_auth_cloud", Value: transaction.Value, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}); rejected.Code != 401 {
				t.Fatal("legacy transaction cookie accepted")
			}

			completed := perform("GET", callback, "", transaction, &http.Cookie{Name: "savorum_session", Value: "parent-forgery", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			if completed.Code != 303 {
				t.Fatalf("callback %d %s", completed.Code, completed.Body.String())
			}

			if again := perform("GET", callback, "", started.Result().Cookies()...); again.Code != 401 {
				t.Fatal("callback replay")
			}

			if native {
				target, err := url.Parse(completed.Header().Get("Location"))
				if err != nil || target.Scheme != "savorum" || target.Query().Get("state") != "native-state-native-state" {
					t.Fatal("native return")
				}

				if len(completed.Result().Cookies()) > 1 {
					t.Fatal("native callback minted browser session")
				}

				body, err := json.Marshal(map[string]string{"code": target.Query().Get("code"), "code_verifier": verifier, "redirect_uri": nativeReturnTarget})
				if err != nil {
					t.Fatal(err)
				}

				completed = perform("POST", "/api/v1/auth/native-sessions", string(body))
				if completed.Code != 200 {
					t.Fatalf("native session %s", completed.Body.String())
				}

				if again := perform("POST", "/api/v1/auth/native-sessions", string(body)); again.Code != 401 {
					t.Fatal("native replay")
				}
			}

			var sessionToken string

			for _, cookie := range completed.Result().Cookies() {
				if cookie.Name == "__Host-savorum_session" {
					sessionToken = cookie.Value
					if cookie.Domain != "" || cookie.Path != "/" || !cookie.Secure || !cookie.HttpOnly {
						t.Fatal("tenant cookie domain")
					}
				}
			}

			p, session, err := db.ResolveSession(ctx, sessionToken)
			if err != nil || p.UserID != who.user {
				t.Fatalf("local identity %s %v", p.UserID, err)
			}

			if err = db.RevokeSession(ctx, p.UserID, session.ID); err != nil {
				t.Fatal(err)
			}

			if _, _, err = db.ResolveSession(ctx, sessionToken); !errors.Is(err, identity.ErrCredentials) {
				t.Fatal("local revocation ineffective")
			}
		}
	}
	// Ordinary recipe authorization and local sessions do not consult the provider.
	provider.Close()

	credentials, err := db.CreateSession(ctx, member.UserID, "cloud", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err = db.ResolveSession(ctx, credentials.Token); err != nil {
		t.Fatal(err)
	}

	if err = db.DisableUser(ctx, owner.OwnerUserID, member.UserID, true); err != nil {
		t.Fatal(err)
	}

	if _, _, err = db.ResolveSession(ctx, credentials.Token); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("disabled member session survives")
	}
}
