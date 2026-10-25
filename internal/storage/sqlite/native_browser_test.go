package sqlite

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/federation"
	"github.com/savorum/savorum/internal/identity"
)

func TestNativeBrowserGrantRechecksSessionAtCreation(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "revoked"}[revoked], func(t *testing.T) {
			s := openStore(t.Context(), t)

			user, err := s.LocalPrincipal(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			browser, err := s.CreateSession(t.Context(), user.UserID, "passkey", 0, 0)
			if err != nil {
				t.Fatal(err)
			}

			grant := federation.NativeGrant{UserID: user.UserID, SourceSessionID: browser.ID, Challenge: "challenge", RedirectURI: "savorum://auth/callback"}
			if err = s.SaveNativeGrant(t.Context(), "code", grant, time.Now().Add(time.Minute)); err != nil {
				t.Fatal(err)
			}

			if _, _, err = s.RedeemNativeGrant(t.Context(), "code"); err != nil {
				t.Fatal(err)
			}

			if revoked {
				if err = s.RevokeSession(t.Context(), user.UserID, browser.ID); err != nil {
					t.Fatal(err)
				}
			}

			ctx := identity.WithBrowserSession(t.Context(), browser.ID)

			_, err = s.CreateSession(ctx, user.UserID, "passkey", 0, 0)
			if revoked {
				if !errors.Is(err, identity.ErrCredentials) {
					t.Fatalf("revoked source accepted: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}

				if _, err = s.CreateSession(ctx, user.UserID, "passkey", 0, 0); !errors.Is(err, identity.ErrCredentials) {
					t.Fatal("source session reused", err)
				}
			}
		})
	}
}

func TestNativeGrantKindConstraints(t *testing.T) {
	s := openStore(t.Context(), t)

	user, err := s.LocalPrincipal(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if err = s.RegisterProviders(t.Context(), []federation.Provider{{ID: "sso", Kind: "saml", DisplayName: "SSO"}}); err != nil {
		t.Fatal(err)
	}

	browser, err := s.CreateSession(t.Context(), user.UserID, "local", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name             string
		provider, source any
		identity         string
		valid            bool
	}{
		{"neither", nil, nil, "", false},
		{"both", "sso", browser.ID, "", false},
		{"browser-identity", nil, browser.ID, "provider-identity", false},
		{"provider", "sso", nil, "provider-identity", true},
		{"browser", nil, browser.ID, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, e := s.db.ExecContext(t.Context(), `INSERT INTO native_sign_in_grants(code_hash,user_id,provider,challenge,redirect_uri,expires_at,identity_id,source_session_id) VALUES(?,?,?,'challenge','savorum://auth/callback',unixepoch()+60,?,?)`, tc.name, user.UserID, tc.provider, tc.identity, tc.source)
			if tc.valid {
				if e != nil {
					t.Fatal(e)
				}
			} else if e == nil || !strings.Contains(e.Error(), "CHECK constraint failed") {
				t.Fatalf("expected grant CHECK failure, got %v", e)
			}
		})
	}
}

func TestBrowserHandoffSessionIsListedAndRevocable(t *testing.T) {
	s := openStore(t.Context(), t)

	user, err := s.LocalPrincipal(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	source, err := s.CreateSession(t.Context(), user.UserID, "local", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	app, err := s.CreateSession(identity.WithBrowserSession(t.Context(), source.ID), user.UserID, "local", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := s.Sessions(t.Context(), user.UserID)
	if err != nil {
		t.Fatal(err)
	}

	if len(sessions) != 1 || sessions[0].ID != app.ID || !sessions[0].BrowserHandoff || sessions[0].Provider != "local" {
		t.Fatalf("incorrect session provenance: %+v", sessions)
	}

	_, resolved, err := s.ResolveSession(t.Context(), app.Token)
	if err != nil || !resolved.BrowserHandoff {
		t.Fatal("provenance not persisted", err)
	}

	if err = s.RevokeSession(t.Context(), user.UserID, app.ID); err != nil {
		t.Fatal(err)
	}

	if _, _, err = s.ResolveSession(t.Context(), app.Token); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("app session not revoked", err)
	}
}
