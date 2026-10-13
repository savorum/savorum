package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/federation"
	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/identity"
)

func hostedMember(t *testing.T) (*Store, context.Context, hosting.Result, hosting.MemberAuthority) {
	t.Helper()
	s := openStore(t.Context(), t)

	owner, err := s.BootstrapHosting(t.Context(), testBootstrap())
	if err != nil {
		t.Fatal(err)
	}

	ownerCtx := identity.WithPrincipal(t.Context(), identity.Principal{UserID: owner.OwnerUserID, InstanceAdmin: true})

	_, token, err := s.CreateInvitation(ownerCtx, owner.LibraryID, access.Viewer, "", 1)
	if err != nil {
		t.Fatal(err)
	}

	member, err := s.BootstrapHostingMember(t.Context(), hosting.MemberRequest{OperationID: "member", TenantID: owner.TenantID, Subject: "acct_member", DisplayName: "Member", InvitationToken: token})
	if err != nil {
		t.Fatal(err)
	}

	return s, ownerCtx, owner, hosting.MemberAuthority{OperationID: "revoke_1", TenantID: owner.TenantID, BootstrapID: "boot_1", Subject: "acct_member", UserID: member.UserID, LibraryID: member.LibraryID}
}

func TestDisabledProviderTokensRemainRevokedAfterReenabling(t *testing.T) {
	s := openStore(t.Context(), t)

	owner, err := s.BootstrapHosting(t.Context(), testBootstrap())
	if err != nil {
		t.Fatal(err)
	}

	ctx := identity.WithPrincipal(t.Context(), identity.Principal{UserID: owner.OwnerUserID, Method: "cloud"})

	grant, err := s.CreatePersonalToken(ctx, identity.TokenRequest{Name: "integration", Scopes: []string{"recipes:read"}}, identity.TokenPolicy{})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.RegisterProviders(t.Context(), nil); err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.ResolvePersonalToken(t.Context(), grant.Secret); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("disabled provider token survived: %v", err)
	}

	if err := s.RegisterProviders(t.Context(), []federation.Provider{{ID: "cloud", Kind: "managed", DisplayName: "Hosting", Managed: &federation.ManagedConfig{Issuer: testBootstrap().Issuer}}}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.ResolvePersonalToken(t.Context(), grant.Secret); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("reenabling resurrected token: %v", err)
	}
}

func TestHostedMemberRevocationIsExactIdempotentAndPreservesOtherLibraries(t *testing.T) {
	s, ownerCtx, _, m := hostedMember(t)

	session, err := s.CreateSession(t.Context(), m.UserID, "cloud", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	p, _, err := s.ResolveSession(t.Context(), session.Token)
	if err != nil {
		t.Fatal(err)
	}

	memberCtx := identity.WithPrincipal(t.Context(), p)

	grant, err := s.CreatePersonalToken(memberCtx, identity.TokenRequest{Name: "integration", Scopes: []string{"recipes:read"}}, identity.TokenPolicy{})
	if err != nil {
		t.Fatal(err)
	}

	other, err := s.CreateLibrary(ownerCtx, "Other")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetLibraryMember(ownerCtx, other.ID, m.UserID, access.Viewer, false); err != nil {
		t.Fatal(err)
	}

	bad := m

	bad.Subject = "another-account"
	if !errors.Is(s.RevokeHostedMember(t.Context(), bad), identity.ErrConflict) {
		t.Fatal("wrong identity revoked")
	}

	if err := s.RevokeHostedMember(t.Context(), m); err != nil {
		t.Fatal(err)
	}

	if err := s.RevokeHostedMember(t.Context(), m); err != nil {
		t.Fatalf("retry: %v", err)
	}

	bad = m
	bad.OperationID = m.OperationID

	bad.BootstrapID = "other"
	if !errors.Is(s.RevokeHostedMember(t.Context(), bad), identity.ErrConflict) {
		t.Fatal("mismatched retry accepted")
	}

	if _, _, err := s.ResolveSession(t.Context(), session.Token); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("session survived: %v", err)
	}

	if _, _, err := s.ResolvePersonalToken(t.Context(), grant.Secret); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("token survived: %v", err)
	}

	if _, err := s.CreateSession(t.Context(), m.UserID, "cloud", 0, 0); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("managed sign-in survived: %v", err)
	}

	if err := s.AuthorizeLibrary(memberCtx, m.LibraryID, access.Viewer); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("membership survived: %v", err)
	}

	if err := s.AuthorizeLibrary(memberCtx, other.ID, access.Viewer); err != nil {
		t.Fatalf("unrelated library changed: %v", err)
	}

	if _, err := s.CreatePersonalToken(memberCtx, identity.TokenRequest{Name: "stale request", Scopes: []string{"recipes:read"}, Libraries: []string{other.ID}}, identity.TokenPolicy{}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("revoked identity minted token: %v", err)
	}
}

func TestHostingOwnershipRequiresAdminAndInitialLibraryOwner(t *testing.T) {
	s, ownerCtx, owner, m := hostedMember(t)
	if !errors.Is(s.CheckHostedOwner(t.Context(), m), identity.ErrConflict) {
		t.Fatal("viewer eligible")
	}

	if err := s.SetLibraryMember(ownerCtx, m.LibraryID, m.UserID, access.Owner, false); err != nil {
		t.Fatal(err)
	}

	if !errors.Is(s.CheckHostedOwner(t.Context(), m), identity.ErrConflict) {
		t.Fatal("non-administrator eligible")
	}

	if _, err := s.db.ExecContext(t.Context(), `UPDATE users SET instance_admin=1 WHERE id=?`, m.UserID); err != nil {
		t.Fatal(err)
	}

	if err := s.CheckHostedOwner(t.Context(), m); err != nil {
		t.Fatal(err)
	}

	if err := s.SetLibraryMember(ownerCtx, m.LibraryID, m.UserID, access.Viewer, false); err != nil {
		t.Fatal(err)
	}

	if !errors.Is(s.CheckHostedOwner(t.Context(), m), identity.ErrConflict) {
		t.Fatal("downgraded owner eligible")
	}

	original := m
	original.UserID = owner.OwnerUserID

	original.Subject = "acct_owner"
	if err := s.RevokeHostedMember(t.Context(), original); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("last owner removed: %v", err)
	}
}

func TestHostingReleaseRevokesManagedTokensAndPreservesLocalTokens(t *testing.T) {
	s := openStore(t.Context(), t)

	owner, err := s.BootstrapHosting(t.Context(), testBootstrap())
	if err != nil {
		t.Fatal(err)
	}

	managedCtx := identity.WithPrincipal(t.Context(), identity.Principal{UserID: owner.OwnerUserID, Method: "cloud"})
	request := identity.TokenRequest{Name: "managed", Scopes: []string{"recipes:read"}}

	managed, err := s.CreatePersonalToken(managedCtx, request, identity.TokenPolicy{})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.EstablishLocalPassword(t.Context(), owner.OwnerUserID, "owner", "long passphrase for owner"); err != nil {
		t.Fatal(err)
	}

	localCtx := identity.WithPrincipal(t.Context(), identity.Principal{UserID: owner.OwnerUserID, Method: "local"})

	local, err := s.CreatePersonalToken(localCtx, request, identity.TokenPolicy{})
	if err != nil {
		t.Fatal(err)
	}

	principal, _, err := s.ResolvePersonalToken(t.Context(), managed.Secret)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.ReleaseHosting(t.Context()); err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.ResolvePersonalToken(t.Context(), managed.Secret); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("managed token survived: %v", err)
	}

	if err := s.AuthorizeLibrary(identity.WithPrincipal(t.Context(), principal), owner.LibraryID, access.Viewer); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("cached token survived: %v", err)
	}

	if _, _, err := s.ResolvePersonalToken(t.Context(), local.Secret); err != nil {
		t.Fatalf("local token revoked: %v", err)
	}
}
