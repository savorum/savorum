package sqlite

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/identity"
)

func TestReleasedDatabaseUpgradePreservesNativeSignIn(t *testing.T) {
	data, err := os.ReadFile("testdata/migrations/0.3.0.db")
	if err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = root.Close() })

	if err = root.WriteFile("released.db", data, 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(t.Context(), filepath.Join(directory, "released.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = s.Close() })

	user, browser, err := s.ResolveSession(t.Context(), "fixture-browser-token")
	if err != nil || user.DisplayName != "Release fixture" || browser.ID != "fixture-browser" || browser.BrowserHandoff {
		t.Fatalf("browser session changed during upgrade: %+v %+v %v", user, browser, err)
	}

	payload, err := s.ConsumeFederationTransaction(t.Context(), "fixture-state", "fixture-oidc", "fixture-browser-binding")
	if err != nil || payload != `{"nonce":"fixture-nonce"}` {
		t.Fatalf("pending provider transaction changed during upgrade: %q %v", payload, err)
	}

	grant, principal, err := s.RedeemNativeGrant(t.Context(), "fixture-native-code")
	if err != nil || principal.UserID != user.UserID || grant.Provider != "fixture-oidc" || grant.IdentityID != "fixture-identity" || grant.SourceSessionID != "" || grant.Challenge != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || grant.RedirectURI != "savorum://auth/callback" {
		t.Fatalf("pending native grant changed during upgrade: %+v %+v %v", grant, principal, err)
	}

	if _, _, err = s.RedeemNativeGrant(t.Context(), "fixture-native-code"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("native grant replay accepted: %v", err)
	}

	app, err := s.CreateSession(identity.WithBrowserSession(t.Context(), browser.ID), user.UserID, browser.Provider, 0, 0)
	if err != nil || !app.BrowserHandoff {
		t.Fatalf("browser session handoff failed after upgrade: %+v %v", app, err)
	}

	if _, _, err = s.ResolveSession(t.Context(), "fixture-browser-token"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("browser session remains active after handoff: %v", err)
	}
}
