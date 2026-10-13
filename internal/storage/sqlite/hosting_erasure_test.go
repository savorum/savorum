package sqlite

import (
	"errors"
	"testing"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestHostedErasurePreservesSharedCollectionAndRequiresSuccessor(t *testing.T) {
	ctx := t.Context()
	s := openStore(ctx, t)
	b := testBootstrap()

	owner, err := s.BootstrapHosting(ctx, b)
	if err != nil {
		t.Fatal(err)
	}

	ownerCtx := identity.WithPrincipal(ctx, identity.Principal{UserID: owner.OwnerUserID, InstanceAdmin: true})

	_, token, err := s.CreateInvitation(ownerCtx, owner.LibraryID, access.Editor, "", 1)
	if err != nil {
		t.Fatal(err)
	}

	member, err := s.BootstrapHostingMember(ctx, hosting.MemberRequest{OperationID: "accepted-member", TenantID: b.TenantID, Subject: "member", DisplayName: "Member", InvitationToken: token})
	if err != nil {
		t.Fatal(err)
	}

	session, err := s.CreateSession(ctx, owner.OwnerUserID, "cloud", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	request := hosting.Erasure{TenantID: b.TenantID, BootstrapID: b.OperationID, Subject: b.Owner.Subject, UserID: owner.OwnerUserID}
	if err := s.EraseHostedUser(ctx, request); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("unresolved owner = %v", err)
	}

	request.Successor = "member"
	wrong := request

	wrong.UserID = member.UserID
	if err := s.EraseHostedUser(ctx, wrong); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("wrong identity = %v", err)
	}

	if err := s.EraseHostedUser(ctx, request); err != nil {
		t.Fatal(err)
	}

	if err := s.EraseHostedUser(ctx, request); err != nil {
		t.Fatalf("retry: %v", err)
	}

	if _, _, err := s.ResolveSession(ctx, session.Token); err == nil {
		t.Fatal("erased session works")
	}

	var (
		role  string
		admin int
	)
	if err := s.db.QueryRowContext(ctx, `SELECT m.role,u.instance_admin FROM library_memberships m JOIN users u ON u.id=m.user_id WHERE m.library_id=? AND m.user_id=?`, owner.LibraryID, member.UserID).Scan(&role, &admin); err != nil || role != "owner" || admin != 1 {
		t.Fatalf("successor: %s %d %v", role, admin, err)
	}

	if _, err := s.BootstrapHosting(ctx, b); err != nil {
		t.Fatalf("normal restart: %v", err)
	}

	wrong = request

	wrong.BootstrapID = "different"
	if err := s.EraseHostedUser(ctx, wrong); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("wrong generation: %v", err)
	}
}

func TestHostedMemberErasurePurgesPrivateLibrary(t *testing.T) {
	ctx := t.Context()
	s := openStore(ctx, t)
	b := testBootstrap()

	owner, err := s.BootstrapHosting(ctx, b)
	if err != nil {
		t.Fatal(err)
	}

	ownerCtx := identity.WithPrincipal(ctx, identity.Principal{UserID: owner.OwnerUserID, InstanceAdmin: true})

	_, token, err := s.CreateInvitation(ownerCtx, owner.LibraryID, access.Viewer, "", 1)
	if err != nil {
		t.Fatal(err)
	}

	member, err := s.BootstrapHostingMember(ctx, hosting.MemberRequest{OperationID: "member", TenantID: b.TenantID, Subject: "member", DisplayName: "Member", InvitationToken: token})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.db.ExecContext(ctx, `INSERT INTO libraries(id,name,created_at) VALUES('private','Private notes','2026-01-01'); INSERT INTO library_memberships(library_id,user_id,role) VALUES('private',?,'owner')`, member.UserID); err != nil {
		t.Fatal(err)
	}

	service, err := library.NewService(s, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	privateCtx := access.WithLibrary(identity.WithPrincipal(ctx, identity.Principal{UserID: member.UserID}), "private")

	private, err := service.Create(privateCtx, testfixture.Document())
	if err != nil {
		t.Fatal(err)
	}

	sharedCtx := access.WithLibrary(ownerCtx, owner.LibraryID)

	shared, err := service.Create(sharedCtx, testfixture.Document())
	if err != nil {
		t.Fatal(err)
	}

	request := hosting.Erasure{TenantID: b.TenantID, BootstrapID: b.OperationID, Subject: "member", UserID: member.UserID}
	if err := s.EraseHostedUser(ctx, request); err != nil {
		t.Fatal(err)
	}

	var retained int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM library_recipes WHERE id=?`, private.ID).Scan(&retained); err != nil || retained != 0 {
		t.Fatal("private recipe retained", err)
	}

	if _, err := service.Get(sharedCtx, shared.ID); err != nil {
		t.Fatal("shared recipe removed", err)
	}

	var active int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM libraries WHERE id='private' AND deleted_at IS NULL`).Scan(&active); err != nil || active != 0 {
		t.Fatal("private library retained", err)
	}

	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM libraries WHERE id=? AND deleted_at IS NULL`, owner.LibraryID).Scan(&active); err != nil || active != 1 {
		t.Fatal("shared library lost", err)
	}
}
