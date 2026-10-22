package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/federation"
	"github.com/savorum/savorum/internal/identity"
)

func freshLinkContext(ctx context.Context, t *testing.T, s *Store, userID string) context.Context {
	t.Helper()

	session, err := s.CreateSession(ctx, userID, "local", time.Hour, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	return identity.WithPrincipal(ctx, identity.Principal{UserID: userID, CredentialID: session.ID})
}

func TestDurableCredentialsRequireRecentActiveSession(t *testing.T) {
	for _, tc := range []struct {
		name, change string
		allowed      bool
	}{
		{"fresh", `UPDATE browser_sessions SET created_at=unixepoch() WHERE id=?`, true},
		{"old", `UPDATE browser_sessions SET created_at=unixepoch()-301 WHERE id=?`, false},
		{"future", `UPDATE browser_sessions SET created_at=unixepoch()+120 WHERE id=?`, false},
		{"revoked", `UPDATE browser_sessions SET revoked_at=unixepoch() WHERE id=?`, false},
		{"expired", `UPDATE browser_sessions SET absolute_expires=unixepoch()-1 WHERE id=?`, false},
		{"missing", `DELETE FROM browser_sessions WHERE id=?`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, ctx, _ := integrationFixture(t)

			user, _ := identity.FromContext(ctx)
			if err := s.RegisterProviders(ctx, []federation.Provider{{ID: "company", Kind: "oidc", DisplayName: "Company"}}); err != nil {
				t.Fatal(err)
			}

			work := freshLinkContext(ctx, t, s, user.UserID)

			principal, _ := identity.FromContext(work)
			if _, err := s.db.ExecContext(ctx, tc.change, principal.CredentialID); err != nil {
				t.Fatal(err)
			}

			if got := s.FederationLinkSessionActive(ctx, user.UserID, principal.CredentialID); got != tc.allowed {
				t.Fatalf("callback freshness=%v", got)
			}

			_, tokenErr := s.CreatePersonalToken(work, identity.TokenRequest{Name: "Integration", Scopes: []string{"recipes:read"}}, identity.TokenPolicy{})

			_, linkErr := s.ResolveExternalIdentity(work, "company", "subject", "User", "", false, "", access.Viewer, "", user.UserID)
			if tc.allowed {
				if tokenErr != nil || linkErr != nil {
					t.Fatalf("token=%v link=%v", tokenErr, linkErr)
				}

				return
			}

			if !errors.Is(tokenErr, identity.ErrCredentials) || !errors.Is(linkErr, identity.ErrCredentials) {
				t.Fatalf("token=%v link=%v", tokenErr, linkErr)
			}

			var credentials int
			if err := s.db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM personal_tokens)+(SELECT count(*) FROM identities WHERE provider='company')`).Scan(&credentials); err != nil {
				t.Fatal(err)
			}

			if credentials != 0 {
				t.Fatalf("stale session created %d credentials", credentials)
			}
		})
	}
}
