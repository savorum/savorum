package federation

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

func TestOIDCCodePKCEAndTokenValidation(t *testing.T) {
	for _, scenario := range []string{"valid", "wrong_nonce", "wrong_audience", "wrong_issuer", "expired", "future_nbf", "wrong_azp", "unsigned"} {
		t.Run(scenario, func(t *testing.T) {
			key, e := rsa.GenerateKey(rand.Reader, 2048)
			if e != nil {
				t.Fatal(e)
			}

			var issuer, nonce, challenge string

			provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
				case "/keys":
					_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "key", Algorithm: "RS256", Use: "sig"}}})
				case "/token":
					if r.ParseForm() != nil {
						t.Error("bad exchange form")
					}

					sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
					if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("redirect_uri") != "https://recipes.example/api/v1/auth/oidc/callback" {
						t.Error("PKCE or exact redirect absent")
					}

					claims := map[string]any{"iss": issuer, "sub": "opaque-subject", "aud": "client", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": nonce, "email": "existing@example.com"}

					switch scenario {
					case "wrong_nonce":
						claims["nonce"] = "different"
					case "wrong_audience":
						claims["aud"] = "other-client"
					case "wrong_issuer":
						claims["iss"] = "https://evil.example"
					case "expired":
						claims["exp"] = time.Now().Add(-time.Hour).Unix()
					case "future_nbf":
						claims["nbf"] = time.Now().Add(time.Hour).Unix()
					case "wrong_azp":
						claims["azp"] = "other-client"
					}

					signer, e := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "key"}}, nil)
					if e != nil {
						t.Error(e)
						return
					}

					payload, _ := json.Marshal(claims)

					signed, e := signer.Sign(payload)
					if e != nil {
						t.Error(e)
						return
					}

					raw, e := signed.CompactSerialize()
					if e != nil {
						t.Error(e)
						return
					}

					if scenario == "unsigned" {
						raw = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
					}

					_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "discarded", "token_type": "Bearer", "id_token": raw})
				default:
					http.NotFound(w, r)
				}
			}))
			defer provider.Close()

			issuer = provider.URL
			h, f := testHandler(t, Provider{ID: "oidc", Kind: "oidc", DisplayName: "Work", OIDC: &OIDCConfig{Issuer: issuer, ClientID: "client"}})
			h.client = provider.Client()
			start := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/oidc/login", strings.NewReader(`{}`))
			start.Header.Set("Origin", "https://recipes.example")

			w := httptest.NewRecorder()
			h.ServeHTTP(w, start)

			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}

			var body map[string]string
			if json.Unmarshal(w.Body.Bytes(), &body) != nil {
				t.Fatal("bad authorization response")
			}

			auth, e := url.Parse(body["authorization_url"])
			if e != nil {
				t.Fatal(e)
			}

			q := auth.Query()
			nonce = q.Get("nonce")

			challenge = q.Get("code_challenge")
			if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || nonce == "" || q.Get("state") == "" {
				t.Fatal(q)
			}

			callback := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://recipes.example/api/v1/auth/oidc/callback?code=code&state="+q.Get("state"), nil)
			callback.AddCookie(w.Result().Cookies()[0])

			result := httptest.NewRecorder()
			h.ServeHTTP(result, callback)

			if scenario == "valid" {
				if result.Code != 303 || len(f.subjects) != 1 || f.subjects[0] != "oidc:opaque-subject" {
					t.Fatal(result.Code, result.Body.String(), f.subjects)
				}

				replay := httptest.NewRecorder()
				h.ServeHTTP(replay, callback)

				if replay.Code != 401 {
					t.Fatal("callback replay accepted")
				}
			} else if result.Code != 401 || len(f.subjects) != 0 {
				t.Fatalf("invalid token accepted: %d %v", result.Code, f.subjects)
			}
		})
	}
}

func TestOIDCGroupClaimPaths(t *testing.T) {
	claims := map[string]any{"organization": map[string]any{"groups": []any{"cooks", "readers"}}, "groups": []any{"unconfigured-admin"}, "malformed": []any{123}}

	groups, err := claimGroups(claims, "organization.groups")
	if err != nil || len(groups) != 2 || groups[0] != "cooks" {
		t.Fatal(groups, err)
	}

	groups, err = claimGroups(claims, "")
	if err != nil || len(groups) != 0 {
		t.Fatal("unconfigured groups trusted", groups, err)
	}

	if _, err := claimGroups(claims, "malformed"); err == nil {
		t.Fatal("non-string group trusted")
	}
}

func TestOIDCRejectsPlaintextDiscoveryEndpoints(t *testing.T) {
	var issuer string

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		//nolint:gosec // Deliberately insecure fixture endpoint, not a credential.
		if err := json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": "http://insecure.example/token", "jwks_uri": issuer + "/keys"}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	issuer = server.URL
	p := Provider{ID: "oidc", Kind: "oidc", DisplayName: "SSO", OIDC: &OIDCConfig{Issuer: issuer, ClientID: "client"}}
	h, _ := testHandler(t, p)

	h.client = server.Client()
	if _, _, err := h.oidcClient(t.Context(), p); err == nil {
		t.Fatal("plaintext token endpoint accepted")
	}
}
