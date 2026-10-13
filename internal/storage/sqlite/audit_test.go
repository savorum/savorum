package sqlite

import (
	"errors"
	"testing"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

func TestAuditAuthorizationFiltersAndAppendOnly(t *testing.T) {
	ctx := t.Context()
	s := openStore(ctx, t)

	admin, err := s.BootstrapAdmin(ctx, "admin", "Admin", "a sufficiently long admin password")
	if err != nil {
		t.Fatal(err)
	}

	owner, err := s.CreateLocalUser(ctx, admin.UserID, "owner", "Owner", "a sufficiently long owner password")
	if err != nil {
		t.Fatal(err)
	}

	adminCtx := identity.WithPrincipal(ctx, admin)
	if err := s.SetLibraryMember(adminCtx, "default", owner.UserID, access.Owner, false); err != nil {
		t.Fatal(err)
	}

	ownerCtx := identity.WithPrincipal(ctx, owner)
	if _, err := s.AuditEvents(ownerCtx, identity.AuditFilter{}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("instance history leaked: %v", err)
	}

	events, err := s.AuditEvents(ownerCtx, identity.AuditFilter{LibraryID: "default"})
	if err != nil || len(events) != 1 {
		t.Fatalf("library history: %+v %v", events, err)
	}

	if events[0].Action != "library.member.role_changed" || events[0].ActorID != admin.UserID || events[0].TargetID != owner.UserID {
		t.Fatalf("event: %+v", events[0])
	}

	if _, err := s.AuditEvents(ownerCtx, identity.AuditFilter{LibraryID: "other"}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("other library leaked: %v", err)
	}

	events, err = s.AuditEvents(adminCtx, identity.AuditFilter{Category: "user", TargetID: owner.UserID})
	if err != nil || len(events) != 1 || events[0].Action != "user.created" {
		t.Fatalf("filters: %+v %v", events, err)
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE audit_events SET actor_id='rewritten'`); err == nil {
		t.Fatal("historical events are mutable")
	}

	if err := s.DisableUser(ctx, admin.UserID, owner.UserID, true); err != nil {
		t.Fatal(err)
	}

	if _, err := s.AuditEvents(ownerCtx, identity.AuditFilter{LibraryID: "default"}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("disabled owner retained history access: %v", err)
	}
}

func TestAuditRollsBackWithOperation(t *testing.T) {
	s := openStore(t.Context(), t)

	tx, err := s.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	if err := auditIdentity(t.Context(), tx, "actor", "target", "create-user"); err != nil {
		t.Fatal(err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := s.db.QueryRowContext(t.Context(), "SELECT count(*) FROM audit_events").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatal("rolled-back operation retained audit event")
	}
}

func TestFailedLoginAuditHasGlobalPersistentBound(t *testing.T) {
	s := openStore(t.Context(), t)
	for range 100 {
		if err := s.auditLoginFailure(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	var count int
	if err := s.db.QueryRowContext(t.Context(), "SELECT count(*) FROM audit_events WHERE action='auth.login.failed'").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf("failure amplification: %d events", count)
	}
}
