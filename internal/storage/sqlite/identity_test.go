package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/identity"
)

func TestIdentityBootstrapClaimAndSessionSecurity(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	local, err := store.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	admin, err := store.BootstrapAdmin(ctx, " Chef ", "Chef", "long passphrase for admin")
	if err != nil {
		t.Fatal(err)
	}

	if admin.UserID != local.UserID || admin.UserID == "local" {
		t.Fatal("bootstrap did not claim persisted local identity")
	}

	if _, err := store.BootstrapAdmin(ctx, "other", "Other", "another long passphrase"); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("second bootstrap: %v", err)
	}

	principal, err := store.Authenticate(ctx, "CHEF", "long passphrase for admin", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	if principal.UserID != admin.UserID {
		t.Fatal("login changed application user")
	}

	session, err := store.CreateSession(ctx, admin.UserID, "local", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	var stored string
	if err := store.db.QueryRowContext(ctx, `SELECT token_hash FROM browser_sessions WHERE id=?`, session.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}

	if stored == session.Token || stored == "" {
		t.Fatal("raw session token persisted")
	}

	if !store.CheckCSRF(ctx, session.Token, session.CSRF) || store.CheckCSRF(ctx, session.Token, "wrong") {
		t.Fatal("CSRF binding failed")
	}

	if _, _, err := store.ResolveSession(ctx, session.Token); err != nil {
		t.Fatal(err)
	}

	if err := store.ChangePassword(ctx, admin.UserID, "long passphrase for admin", "new long passphrase for admin"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.ResolveSession(ctx, session.Token); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("password change retained session: %v", err)
	}

	var audits int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM identity_audit`).Scan(&audits); err != nil {
		t.Fatal(err)
	}

	if audits != 2 {
		t.Fatalf("audit count=%d", audits)
	}
}

func TestIdentityUserDisableAndExpiry(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	admin, err := store.BootstrapAdmin(ctx, "admin", "Admin", "long passphrase for admin")
	if err != nil {
		t.Fatal(err)
	}

	user, err := store.CreateLocalUser(ctx, admin.UserID, "cook", "Cook", "long passphrase for cook")
	if err != nil {
		t.Fatal(err)
	}

	session, err := store.CreateSession(ctx, user.UserID, "local", time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.DisableUser(ctx, admin.UserID, admin.UserID, true); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("disabled last administrator: %v", err)
	}

	if err := store.DisableUser(ctx, admin.UserID, user.UserID, true); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.ResolveSession(ctx, session.Token); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("disabled user retained session: %v", err)
	}

	if _, err := store.Authenticate(ctx, "cook", "long passphrase for cook", "client"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("disabled login: %v", err)
	}

	if err := store.DisableUser(ctx, admin.UserID, user.UserID, false); err != nil {
		t.Fatal(err)
	}

	session, err = store.CreateSession(ctx, user.UserID, "local", time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, `UPDATE browser_sessions SET absolute_expires=unixepoch()-1 WHERE id=?`, session.ID); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.ResolveSession(ctx, session.Token); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("expired session accepted: %v", err)
	}
}

func TestIdentityDeletionPreservesHistoricalActorAndRequiresOwnershipTransfer(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	admin, err := store.BootstrapAdmin(ctx, "admin", "Admin", "long passphrase for admin")
	if err != nil {
		t.Fatal(err)
	}

	user, err := store.CreateLocalUser(ctx, admin.UserID, "cook", "Cook", "long passphrase for cook")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, `INSERT INTO libraries(id,name,created_at) VALUES('owned','Owned',datetime('now')); INSERT INTO library_memberships VALUES('owned',?,'owner','manual')`, user.UserID); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteUser(ctx, user.UserID, user.UserID); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("sole owner deletion: %v", err)
	}

	if _, err := store.db.ExecContext(ctx, `INSERT INTO library_memberships VALUES('owned',?,'owner','manual')`, admin.UserID); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteUser(ctx, user.UserID, user.UserID); err != nil {
		t.Fatal(err)
	}

	tombstone, err := store.User(ctx, user.UserID)
	if err != nil {
		t.Fatal(err)
	}

	if tombstone.Status != "deleted" || tombstone.DisplayName != "Former user" || tombstone.UserID != user.UserID {
		t.Fatal("actor was not tombstoned in place")
	}

	var identities, libraries int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM identities WHERE user_id=?`, user.UserID).Scan(&identities); err != nil {
		t.Fatal(err)
	}

	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM libraries WHERE id='owned'`).Scan(&libraries); err != nil {
		t.Fatal(err)
	}

	if identities != 0 || libraries != 1 {
		t.Fatal("deletion retained credentials or deleted shared library")
	}
}

func TestIdentityRenameUser(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	admin, err := store.BootstrapAdmin(ctx, "chef", "Chef", "long passphrase for admin")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.RenameUser(ctx, admin.UserID, "Head Chef"); err != nil {
		t.Fatal(err)
	}

	user, err := store.User(ctx, admin.UserID)
	if err != nil || user.DisplayName != "Head Chef" {
		t.Fatalf("renamed user=%+v err=%v", user, err)
	}

	if err := store.RenameUser(ctx, "missing", "Nobody"); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("rename missing user: %v", err)
	}
}
