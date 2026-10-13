package account

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/savorum/savorum/internal/identity"
)

type passkeyConfigStore struct {
	unexpectedRepository
	unexpectedPasskeyRepository
}

func (passkeyConfigStore) CheckPasskeyRP(context.Context, string) error { return nil }

func TestPasskeySecureOriginAndAuthenticatorPolicy(t *testing.T) {
	for _, tc := range []struct {
		url     string
		allowed bool
	}{
		{"https://recipes.example", true},
		{"http://localhost:8123", true},
		{"http://recipes.example", false},
		{"https://user:password@recipes.example", false},
		{"https://recipes.example?origin=other", false},
		{"", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			h := &Handler{store: passkeyConfigStore{}, options: Options{Enabled: true, PublicURL: func(context.Context) (string, error) { return tc.url, nil }}}

			wa, err := h.passkeyConfig(t.Context())
			if (err == nil) != tc.allowed {
				t.Fatalf("origin accepted=%v: %v", err == nil, err)
			}

			if err != nil {
				return
			}

			if wa.Config.AuthenticatorSelection.ResidentKey != protocol.ResidentKeyRequirementRequired || wa.Config.AuthenticatorSelection.UserVerification != protocol.VerificationRequired || wa.Config.AuthenticatorSelection.AuthenticatorAttachment != "" || wa.Config.AttestationPreference != protocol.PreferNoAttestation {
				t.Fatal("passkey policy weakened")
			}

			h.options.Enabled = false
			if _, err = h.passkeyConfig(t.Context()); err == nil {
				t.Fatal("local-mode enrollment enabled")
			}
		})
	}
}

func TestRecentAuthenticationRequiresFreshBrowserSession(t *testing.T) {
	for _, tc := range []struct {
		name    string
		session identity.Session
		allowed bool
	}{
		{"missing", identity.Session{}, false},
		{"old", identity.Session{ID: "session", CreatedAt: time.Now().Add(-6 * time.Minute)}, false},
		{"fresh-passkey", identity.Session{ID: "session", CreatedAt: time.Now(), Provider: "passkey"}, true},
		{"fresh-federation", identity.Session{ID: "session", CreatedAt: time.Now(), Provider: "external"}, true},
		{"fresh-recovery-repair", identity.Session{ID: "session", CreatedAt: time.Now(), Provider: "recovery"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), "POST", "https://recipes.example/api/v1/account/recovery", nil)

			r = r.WithContext(context.WithValue(r.Context(), browserKey{}, browser{session: tc.session}))
			if got := recentAuthentication(httptest.NewRecorder(), r); got != tc.allowed {
				t.Fatalf("recent authentication=%v", got)
			}
		})
	}
}
