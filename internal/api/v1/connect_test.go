package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/savorum/savorum/internal/connect"
	"github.com/savorum/savorum/internal/identity"
)

func TestConnectAdministrationAuthorizationAndSecretRedaction(t *testing.T) {
	for _, admin := range []bool{false, true} {
		t.Run(map[bool]string{false: "member", true: "administrator"}[admin], func(t *testing.T) {
			relay := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") == "" {
					t.Error("missing Connect credential")
				}

				switch {
				case strings.HasSuffix(r.URL.Path, "/claim"):
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"credential":"` + strings.Repeat("b", 43) + `"}`))
				case strings.HasSuffix(r.URL.Path, "/revoke"):
					w.WriteHeader(http.StatusNoContent)
				default:
					http.NotFound(w, r)
				}
			}))
			defer relay.Close()

			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("test", "1"))
			service := connect.NewWithCredentialClient(filepath.Join(t.TempDir(), "connect"), true, relay.Client())
			RegisterConnect(api, service, true)

			setup := connect.Setup{Version: 1, Relay: relay.URL, Instance: "acorn-apple-basil-basket", RemoteOrigin: "https://acorn-apple-basil-basket.via.example.test", Credential: strings.Repeat("a", 43)}

			body, _ := json.Marshal(setup)
			for _, method := range []string{"PUT", "GET", "DELETE"} {
				r := httptest.NewRequestWithContext(t.Context(), method, "/api/v1/instance/connect", bytes.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				r = r.WithContext(identity.WithPrincipal(r.Context(), identity.Principal{UserID: "user", InstanceAdmin: admin}))
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)

				if !admin && w.Code != 403 {
					t.Fatalf("%s: %d %s", method, w.Code, w.Body.String())
				}

				if admin && w.Code != 200 {
					t.Fatalf("%s: %d %s", method, w.Code, w.Body.String())
				}

				if strings.Contains(w.Body.String(), setup.Credential) {
					t.Fatal("credential exposed")
				}
			}
		})
	}
}
