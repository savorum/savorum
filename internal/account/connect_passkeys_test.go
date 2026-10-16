package account

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/identity"
)

type originPasskeyStore struct{ passkeyConfigStore }

func (originPasskeyStore) User(context.Context, string) (identity.User, error) {
	return identity.User{UserID: "chef", Status: "active"}, nil
}

func (originPasskeyStore) Passkeys(context.Context, string) ([]identity.Passkey, error) {
	return []identity.Passkey{{RPID: "recipes.example", Credential: webauthn.Credential{ID: []byte("local")}}, {RPID: "acorn-apple-basil-basket.connect.test", Credential: webauthn.Credential{ID: []byte("remote")}}}, nil
}

func TestPasskeysSelectVerifiedConnectOriginAndMatchingCredentials(t *testing.T) {
	const remote = "https://acorn-apple-basil-basket.connect.test"

	account := &Handler{store: originPasskeyStore{}, options: Options{Enabled: true, PublicURL: func(context.Context) (string, error) { return "https://recipes.example", nil }}}

	var rp, credential string

	boundary := httpsecurity.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wa, err := account.passkeyConfig(r.Context())
		if err != nil {
			t.Error(err)
			return
		}

		user, err := account.loadPasskeyUser(r.Context(), "chef", wa.Config.RPID)
		if err != nil || len(user.credentials) != 1 {
			t.Errorf("credentials: %v %v", user.credentials, err)
			return
		}

		rp, credential = wa.Config.RPID, string(user.credentials[0].ID)
	}), httpsecurity.Options{ListenAddress: "0.0.0.0:8080", ConnectOrigin: remote, ApplicationOrigin: account.options.PublicURL})
	for _, test := range []struct {
		origin, rp, key string
		trusted         bool
	}{
		{"https://recipes.example", "recipes.example", "local", false},
		{remote, "acorn-apple-basil-basket.connect.test", "remote", true},
	} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.origin+"/api/v1/account", nil)
		r.RemoteAddr = "198.51.100.2:43120"
		w := httptest.NewRecorder()

		handler := boundary
		if test.trusted {
			handler = httpsecurity.ConnectTransport(boundary)
		}

		handler.ServeHTTP(w, r)

		if w.Code != 200 || rp != test.rp || credential != test.key {
			t.Fatalf("origin selection: %d %s %s", w.Code, rp, credential)
		}
	}
}
