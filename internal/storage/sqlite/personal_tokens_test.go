package sqlite

import (
	"errors"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

func TestPersonalTokenAuthorityRevocationAndSecretStorage(t *testing.T) {
	s := openStore(t.Context(), t)

	admin, err := s.BootstrapAdmin(t.Context(), "admin", "Admin", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}

	ctx := identity.WithPrincipal(t.Context(), admin)

	grant, err := s.CreatePersonalToken(ctx, identity.TokenRequest{Name: "Kitchen integration", Scopes: []string{"recipes:read"}, Libraries: []string{"default"}}, identity.TokenPolicy{})
	if err != nil {
		t.Fatal(err)
	}

	if len(grant.Secret) != 76 || !strings.HasPrefix(grant.Secret, "savorum_pat_") {
		t.Fatal("weak token")
	}

	var stored string
	if err := s.db.QueryRowContext(ctx, "SELECT token_hash FROM personal_tokens WHERE id=?", grant.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}

	if stored == grant.Secret || stored != tokenHash(grant.Secret) {
		t.Fatal("credential storage")
	}

	p, token, err := s.ResolvePersonalToken(ctx, grant.Secret)
	if err != nil {
		t.Fatal(err)
	}

	if p.InstanceAdmin || p.UserID != admin.UserID || token.ID != grant.ID {
		t.Fatal("token principal elevated")
	}

	tokenCtx := identity.WithPrincipal(t.Context(), p)
	if err := s.AuthorizeLibrary(tokenCtx, "default", access.Viewer); err != nil {
		t.Fatal(err)
	}

	if err := s.AuthorizeLibrary(tokenCtx, "default", access.Editor); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("read token wrote: %v", err)
	}

	if _, err := s.CreatePersonalToken(tokenCtx, identity.TokenRequest{Name: "elevate", Scopes: []string{"recipes:write"}}, identity.TokenPolicy{}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("token minted credential: %v", err)
	}

	other, err := s.CreateLibrary(ctx, "Other")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.AuthorizeLibrary(tokenCtx, other.ID, access.Viewer); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("library restriction: %v", err)
	}

	if err := s.RevokePersonalToken(ctx, grant.ID); err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.ResolvePersonalToken(ctx, grant.Secret); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("revoked token resolved: %v", err)
	}

	if err := s.AuthorizeLibrary(tokenCtx, "default", access.Viewer); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("delayed token authority: %v", err)
	}
}

func TestPersonalTokenImmediatelyLosesMembershipAndEnforcesPolicy(t *testing.T) {
	s := openStore(t.Context(), t)

	admin, err := s.BootstrapAdmin(t.Context(), "admin", "Admin", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}

	adminCtx := identity.WithPrincipal(t.Context(), admin)

	user, err := s.CreateLocalUser(t.Context(), admin.UserID, "user", "User", "a sufficiently long user password")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetLibraryMember(adminCtx, "default", user.UserID, access.Editor, false); err != nil {
		t.Fatal(err)
	}

	ctx := identity.WithPrincipal(t.Context(), user)
	request := identity.TokenRequest{Name: "CLI", Scopes: []string{"recipes:write"}}

	grant, err := s.CreatePersonalToken(ctx, request, identity.TokenPolicy{})
	if err != nil {
		t.Fatal(err)
	}

	p, _, err := s.ResolvePersonalToken(ctx, grant.Secret)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetLibraryMember(adminCtx, "default", user.UserID, "", false); err != nil {
		t.Fatal(err)
	}

	if err := s.AuthorizeLibrary(identity.WithPrincipal(ctx, p), "default", access.Editor); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("removed membership still authorized: %v", err)
	}

	if _, err := s.db.ExecContext(ctx, "UPDATE personal_tokens SET expires_at=1 WHERE id=?", grant.ID); err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.ResolvePersonalToken(ctx, grant.Secret); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("expired token: %v", err)
	}

	request.Nonexpiring = true
	if _, err := s.CreatePersonalToken(ctx, request, identity.TokenPolicy{}); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("nonexpiring policy: %v", err)
	}

	request.Nonexpiring = false

	request.Days = 366
	if _, err := s.CreatePersonalToken(ctx, request, identity.TokenPolicy{}); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("maximum lifetime: %v", err)
	}
}

func TestAccountDeletionErasesNewAuthenticationMaterial(t *testing.T) {
	s := openStore(t.Context(), t)

	admin, err := s.BootstrapAdmin(t.Context(), "admin", "Admin", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}

	user, err := s.CreateLocalUser(t.Context(), admin.UserID, "user", "User", "a sufficiently long user password")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetLibraryMember(identity.WithPrincipal(t.Context(), admin), "default", user.UserID, access.Editor, false); err != nil {
		t.Fatal(err)
	}

	ctx := identity.WithPrincipal(t.Context(), user)
	if _, err := s.CreatePersonalToken(ctx, identity.TokenRequest{Name: "CLI", Scopes: []string{"recipes:read"}}, identity.TokenPolicy{}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.GenerateRecoveryCodes(ctx, user.UserID); err != nil {
		t.Fatal(err)
	}

	if _, err := s.db.ExecContext(ctx, `INSERT INTO passkeys(id,user_id,credential_json,name,rp_id) VALUES('key',?,'{}','private name','example.test')`, user.UserID); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteUser(ctx, user.UserID, user.UserID); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"passkeys", "recovery_codes", "personal_tokens", "webauthn_ceremonies"} {
		var count int
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE user_id=?", user.UserID).Scan(&count); err != nil {
			t.Fatal(err)
		}

		if count != 0 {
			t.Fatalf("account deletion retained %s", table)
		}
	}
}
