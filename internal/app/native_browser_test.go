package app_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

func TestBrowserPasskeyHandoff(t *testing.T) {
	for _, scenario := range []string{"success", "wrong-verifier", "wrong-return", "revoked-source", "password-session", "wrong-csrf", "missing-browser-cookie", "malformed-json", "existing-session"} {
		t.Run(scenario, func(t *testing.T) {
			f := newNativeFixture(t)

			store, err := sqlite.Open(t.Context(), f.database)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()

			user, err := store.BootstrapAdmin(t.Context(), "owner", "Owner", "a long enough owner passphrase")
			if err != nil {
				t.Fatal(err)
			}

			c := newCeremony(t)
			sum := sha256.Sum256([]byte(c.verifier))
			query := url.Values{"redirect_uri": {"savorum://auth/callback"}, "state": {c.state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}
			browser := f.browser()

			var source identity.SessionCredentials
			if scenario == "existing-session" {
				source, err = store.CreateSession(t.Context(), user.UserID, "passkey", 0, 0)
				if err != nil {
					t.Fatal(err)
				}

				origin, e := url.Parse(f.server.URL)
				if e != nil {
					t.Fatal(e)
				}

				browser.Jar.SetCookies(origin, []*http.Cookie{{Name: identity.SessionCookie, Value: source.Token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}})
			}

			browser.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

			entry, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.server.URL+"/api/v1/auth/native-browser?"+query.Encode(), nil)
			if err != nil {
				t.Fatal(err)
			}

			response, err := browser.Do(entry)
			if err != nil {
				t.Fatal(err)
			}

			if err = response.Body.Close(); err != nil {
				t.Fatal(err)
			}

			location, err := response.Location()
			if err != nil || response.StatusCode != http.StatusSeeOther || location.Path != "/" {
				t.Fatalf("entry: %d %v", response.StatusCode, err)
			}

			state := location.Query().Get("native_browser")

			method := "passkey"
			if scenario == "password-session" {
				method = "local"
			}

			if source.ID == "" {
				source, err = store.CreateSession(t.Context(), user.UserID, method, 0, 0)
				if err != nil {
					t.Fatal(err)
				}
			}

			origin, _ := url.Parse(f.server.URL)
			browser.Jar.SetCookies(origin, []*http.Cookie{{Name: identity.SessionCookie, Value: source.Token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}})

			if scenario == "missing-browser-cookie" {
				browser = f.browser()
			}

			body := `{"state":"` + state + `"}`
			if scenario == "malformed-json" {
				body = `{"state":`
			}

			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.server.URL+"/api/v1/auth/native-browser/finish", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}

			request.Header.Set("Origin", f.server.URL)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-CSRF-Token", source.CSRF)

			if scenario == "wrong-csrf" {
				request.Header.Set("X-CSRF-Token", "wrong")
			}

			response, err = browser.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()

			if scenario == "wrong-csrf" || scenario == "missing-browser-cookie" || scenario == "malformed-json" || scenario == "existing-session" {
				if response.StatusCode < 400 {
					t.Fatalf("invalid completion accepted: %d", response.StatusCode)
				}

				return
			}

			var completed struct {
				RedirectURI string `json:"redirect_uri"`
			}
			if err = json.NewDecoder(response.Body).Decode(&completed); err != nil || response.StatusCode != http.StatusOK {
				t.Fatalf("completion: %d %v", response.StatusCode, err)
			}

			callback, err := url.Parse(completed.RedirectURI)
			if err != nil || callback.Query().Get("state") != c.state || callback.Query().Get("code") == "" {
				t.Fatalf("callback: %q %v", completed.RedirectURI, err)
			}

			code := callback.Query().Get("code")

			verifier, redirect := c.verifier, "savorum://auth/callback"
			if scenario == "wrong-verifier" {
				verifier += "x"
			}

			if scenario == "wrong-return" {
				redirect = "savorum://other"
			}

			if scenario == "revoked-source" {
				if err = store.RevokeSession(t.Context(), user.UserID, source.ID); err != nil {
					t.Fatal(err)
				}
			}

			redeem := func() *http.Response {
				t.Helper()

				data, _ := json.Marshal(map[string]string{"code": code, "code_verifier": verifier, "redirect_uri": redirect})

				req, e := http.NewRequestWithContext(t.Context(), http.MethodPost, f.server.URL+"/api/v1/auth/native-sessions", strings.NewReader(string(data)))
				if e != nil {
					t.Fatal(e)
				}

				req.Header.Set("Origin", f.server.URL)
				req.Header.Set("Content-Type", "application/json")

				out, e := f.native().Do(req)
				if e != nil {
					t.Fatal(e)
				}

				return out
			}

			result := redeem()
			if err = result.Body.Close(); err != nil {
				t.Fatal(err)
			}

			if scenario != "success" && scenario != "password-session" {
				if result.StatusCode != http.StatusUnauthorized {
					t.Fatalf("invalid exchange: %d", result.StatusCode)
				}
			} else {
				if result.StatusCode != http.StatusOK {
					t.Fatalf("exchange: %d", result.StatusCode)
				}

				var token string

				for _, cookie := range result.Cookies() {
					if cookie.Name == identity.SessionCookie {
						token = cookie.Value
					}
				}

				if token == "" || f.account(token) != "Owner" {
					t.Fatal("missing app session")
				}

				if _, _, err = store.ResolveSession(t.Context(), source.Token); err == nil {
					t.Fatal("source browser session remains active")
				}
			}

			replay := redeem()
			if err = replay.Body.Close(); err != nil {
				t.Fatal(err)
			}

			if replay.StatusCode != http.StatusUnauthorized {
				t.Fatalf("code replay: %d", replay.StatusCode)
			}
		})
	}
}
