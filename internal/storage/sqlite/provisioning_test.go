package sqlite

import (
	"errors"
	"testing"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

func TestInvitationsSingleUseAndTransactionalRegistration(t *testing.T) {
	ctx := t.Context()
	s := openStore(ctx, t)

	admin, err := s.BootstrapAdmin(ctx, "admin", "Admin", "long administrator password")
	if err != nil {
		t.Fatal(err)
	}

	ctx = identity.WithPrincipal(ctx, admin)
	if _, err := s.RegisterInvitedLocalUser(ctx, "invalid", "unused", "Unused", "short"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("invalid token hashed: %v", err)
	}

	v, token, err := s.CreateInvitation(ctx, "default", access.Editor, "someone@example.com", 0)
	if err != nil {
		t.Fatal(err)
	}

	var stored string
	if err = s.db.QueryRowContext(ctx, `SELECT token_hash FROM invitations WHERE id=?`, v.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}

	if stored == token || stored != tokenHash(token) {
		t.Fatal("invitation secret not hashed")
	}

	user, err := s.RegisterInvitedLocalUser(ctx, token, "invited", "Invited", "long invited user password")
	if err != nil {
		t.Fatal(err)
	}

	if user.InstanceAdmin {
		t.Fatal("invitation granted administrator")
	}

	userCtx := identity.WithPrincipal(ctx, user)
	if err = s.AuthorizeLibrary(userCtx, "default", access.Editor); err != nil {
		t.Fatal(err)
	}

	if err = s.AcceptInvitation(userCtx, token); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("replayed invitation: %v", err)
	}

	if _, err = s.RegisterInvitedLocalUser(ctx, token, "duplicate", "Duplicate", "short"); err == nil {
		t.Fatal("consumed invitation accepted")
	}

	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM identities WHERE provider_subject='duplicate'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("registration rollback: %d %v", count, err)
	}

	items, err := s.Invitations(ctx, "default")
	if err != nil || len(items) != 1 || items[0].AcceptedAt == nil {
		t.Fatalf("invitation list: %+v %v", items, err)
	}

	for _, mode := range []string{"revoked", "expired"} {
		v, token, err = s.CreateInvitation(ctx, "default", access.Viewer, "", 1)
		if err != nil {
			t.Fatal(err)
		}

		if mode == "revoked" {
			err = s.RevokeInvitation(ctx, v.ID)
		} else {
			_, err = s.db.ExecContext(ctx, `UPDATE invitations SET expires_at=0 WHERE id=?`, v.ID)
		}

		if err != nil {
			t.Fatal(err)
		}

		if err = s.AcceptInvitation(userCtx, token); !errors.Is(err, identity.ErrCredentials) {
			t.Fatalf("%s accepted: %v", mode, err)
		}

		if _, err := s.RegisterInvitedLocalUser(ctx, token, "unused", "Unused", "short"); !errors.Is(err, identity.ErrCredentials) {
			t.Fatalf("%s hashed before validation: %v", mode, err)
		}
	}

	_, token, err = s.CreateInvitation(ctx, "default", access.Viewer, "", 1)
	if err != nil {
		t.Fatal(err)
	}

	if err = s.AcceptInvitation(userCtx, token); err != nil {
		t.Fatal(err)
	}

	if err = s.AuthorizeLibrary(userCtx, "default", access.Editor); err != nil {
		t.Fatal("lesser invitation downgraded member")
	}

	if _, _, err = s.CreateInvitation(userCtx, "default", access.Owner, "", 7); err == nil {
		t.Fatal("editor created owner invitation")
	}
}

func TestExternalIdentityLinkingAndManagedGroupProvenance(t *testing.T) {
	ctx := t.Context()
	s := openStore(ctx, t)

	admin, err := s.BootstrapAdmin(ctx, "admin", "Admin", "long administrator password")
	if err != nil {
		t.Fatal(err)
	}

	ctx = identity.WithPrincipal(ctx, admin)
	for _, id := range []string{"company", "other"} {
		if _, err = s.db.ExecContext(ctx, `INSERT INTO auth_providers(id,kind,display_name,enabled) VALUES(?,'oidc',?,1)`, id, id); err != nil {
			t.Fatal(err)
		}
	}

	first, err := s.ResolveExternalIdentity(ctx, "company", "subject-one", "One", "same@example.com", true, "", access.Viewer, "", "")
	if err != nil {
		t.Fatal(err)
	}

	second, err := s.ResolveExternalIdentity(ctx, "company", "subject-two", "Two", "same@example.com", true, "", access.Viewer, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if first.UserID == second.UserID {
		t.Fatal("email automatically linked")
	}

	if _, err = s.ResolveExternalIdentity(ctx, "company", "subject-one", "One", "", false, "", access.Viewer, "", second.UserID); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("stole identity: %v", err)
	}

	if _, err = s.ResolveExternalIdentity(ctx, "other", "subject-one", "One", "", false, "", access.Viewer, "", first.UserID); err != nil {
		t.Fatal(err)
	}

	methods, err := s.SignInMethods(ctx, first.UserID)
	if err != nil || len(methods) != 2 {
		t.Fatalf("methods: %+v %v", methods, err)
	}

	if err = s.UnlinkIdentity(ctx, first.UserID, first.UserID, methods[0].ID, false); err != nil {
		t.Fatal(err)
	}

	if err = s.UnlinkIdentity(ctx, first.UserID, first.UserID, methods[1].ID, false); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("removed last path: %v", err)
	}

	if _, err = s.ResolveExternalIdentity(ctx, "company", "subject-one", "One", "", false, "", access.Viewer, "", first.UserID); err != nil {
		t.Fatal(err)
	}

	if err = s.SetLibraryMember(ctx, "default", first.UserID, access.Viewer, false); err != nil {
		t.Fatal(err)
	}

	for _, mapping := range []identity.GroupMapping{{ProviderID: "company", Group: "cooks", LibraryID: "default", Role: access.Editor}, {ProviderID: "company", Group: "owners", LibraryID: "default", Role: access.Owner}} {
		if _, err = s.SetGroupMapping(ctx, mapping); err != nil {
			t.Fatal(err)
		}
	}

	if err = s.ReconcileProviderGroups(ctx, first.UserID, "company", []string{"cooks", "owners"}); err != nil {
		t.Fatal(err)
	}

	firstCtx := identity.WithPrincipal(ctx, first)
	if err = s.AuthorizeLibrary(firstCtx, "default", access.Owner); err != nil {
		t.Fatal(err)
	}

	if err = s.ReconcileProviderGroups(ctx, first.UserID, "company", []string{"cooks"}); err != nil {
		t.Fatal(err)
	}

	if err = s.AuthorizeLibrary(firstCtx, "default", access.Owner); err == nil {
		t.Fatal("removed owner group retained owner")
	}

	if err = s.AuthorizeLibrary(firstCtx, "default", access.Editor); err != nil {
		t.Fatal(err)
	}

	if err = s.ReconcileProviderGroups(ctx, first.UserID, "company", nil); err != nil {
		t.Fatal(err)
	}

	if err = s.AuthorizeLibrary(firstCtx, "default", access.Viewer); err != nil {
		t.Fatal("manual access removed")
	}

	if err = s.AuthorizeLibrary(firstCtx, "default", access.Editor); err == nil {
		t.Fatal("managed editor retained")
	}

	if err = s.UnlinkIdentity(ctx, admin.UserID, second.UserID, "999999", true); !errors.Is(err, identity.ErrConflict) {
		t.Fatal(err)
	}

	methods, err = s.SignInMethods(ctx, second.UserID)
	if err != nil {
		t.Fatal(err)
	}

	if err = s.UnlinkIdentity(ctx, admin.UserID, second.UserID, methods[0].ID, true); err != nil {
		t.Fatal(err)
	}
}

func TestForcedIdentityUnlinkAuthorizationAndSessionRevocation(t *testing.T) {
	ctx := t.Context()
	s := openStore(ctx, t)

	admin, err := s.BootstrapAdmin(ctx, "admin", "Admin", "long administrator password")
	if err != nil {
		t.Fatal(err)
	}

	user, err := s.CreateLocalUser(ctx, admin.UserID, "member", "Member", "long ordinary member password")
	if err != nil {
		t.Fatal(err)
	}

	session, err := s.CreateSession(ctx, user.UserID, "local", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	methods, err := s.SignInMethods(ctx, user.UserID)
	if err != nil {
		t.Fatal(err)
	}

	if err = s.UnlinkIdentity(ctx, user.UserID, user.UserID, methods[0].ID, true); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("non-admin force unlink: %v", err)
	}

	if err = s.UnlinkIdentity(ctx, user.UserID, user.UserID, methods[0].ID, false); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("last method self unlink: %v", err)
	}

	if _, _, err = s.ResolveSession(ctx, session.Token); err != nil {
		t.Fatalf("failed unlink revoked session: %v", err)
	}

	if err = s.UnlinkIdentity(ctx, admin.UserID, user.UserID, methods[0].ID, true); err != nil {
		t.Fatal(err)
	}

	if _, _, err = s.ResolveSession(ctx, session.Token); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("forced unlink session remained valid: %v", err)
	}

	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM identity_audit WHERE actor_id=? AND target_id=? AND action='identity.force-unlink'`, admin.UserID, user.UserID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("forced unlink audit %d: %v", count, err)
	}
}

func TestIdentityHandlesAreNotReusedAfterUnlink(t *testing.T) {
	ctx := t.Context()
	s := openStore(ctx, t)

	admin, err := s.BootstrapAdmin(ctx, "admin", "Admin", "long administrator password")
	if err != nil {
		t.Fatal(err)
	}

	if _, err = s.db.ExecContext(ctx, `INSERT INTO auth_providers(id,kind,display_name,enabled) VALUES('company','oidc','Company',1)`); err != nil {
		t.Fatal(err)
	}

	if _, err = s.ResolveExternalIdentity(ctx, "company", "old-subject", "Admin", "", false, "", access.Viewer, "", admin.UserID); err != nil {
		t.Fatal(err)
	}

	methods, err := s.SignInMethods(ctx, admin.UserID)
	if err != nil {
		t.Fatal(err)
	}

	var oldID string

	for _, m := range methods {
		if m.Provider == "company" {
			oldID = m.ID
		}
	}

	if oldID == "" {
		t.Fatal("missing external identity handle")
	}

	if err = s.UnlinkIdentity(ctx, admin.UserID, admin.UserID, oldID, false); err != nil {
		t.Fatal(err)
	}

	if _, err = s.ResolveExternalIdentity(ctx, "company", "new-subject", "Admin", "", false, "", access.Viewer, "", admin.UserID); err != nil {
		t.Fatal(err)
	}

	if err = s.UnlinkIdentity(ctx, admin.UserID, admin.UserID, oldID, false); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("stale identity handle accepted: %v", err)
	}

	methods, err = s.SignInMethods(ctx, admin.UserID)
	if err != nil || len(methods) != 2 {
		t.Fatalf("stale request removed new identity: %+v %v", methods, err)
	}
}
