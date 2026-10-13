package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
)

func TestDisabledAdministratorCannotUseStalePolicyContext(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	admin, err := store.BootstrapAdmin(ctx, "admin", "Admin", "long administrator password")
	if err != nil {
		t.Fatal(err)
	}

	other, err := store.CreateLocalUser(ctx, admin.UserID, "other", "Other", "long ordinary user password")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, `UPDATE users SET instance_admin=1 WHERE id=?`, other.UserID); err != nil {
		t.Fatal(err)
	}

	stale := access.WithLibrary(identity.WithPrincipal(ctx, admin), "default")
	if err := store.DisableUser(ctx, other.UserID, admin.UserID, true); err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateLibrary(stale, "Should be denied"); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("disabled administrator created library: %v", err)
	}

	if err := store.SetLibraryMember(stale, "default", other.UserID, access.Owner, true); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("disabled administrator recovered ownership: %v", err)
	}
}

func TestCookingDeletionCannotImpersonateAnotherMember(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)
	seedRecipe(ctx, t, store, "shared-cooking")

	owner, err := store.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	other, err := store.CreateLocalUser(ctx, owner.UserID, "other", "Other", "long ordinary user password")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, `INSERT INTO library_memberships VALUES('default',?,'viewer','manual')`, other.UserID); err != nil {
		t.Fatal(err)
	}

	ownerCtx := access.WithLibrary(identity.WithPrincipal(ctx, owner), "default")
	if _, err := store.RecordCooking(ownerCtx, "shared-cooking", owner.UserID, library.CookingRecord{ID: "owners-cooking", CookedOn: "2026-09-01", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	otherCtx := access.WithLibrary(identity.WithPrincipal(ctx, other), "default")
	if err := store.DeleteCooking(otherCtx, "shared-cooking", owner.UserID, "owners-cooking"); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("member deleted another user's cooking: %v", err)
	}

	history, err := store.ListCookings(ownerCtx, "shared-cooking", owner.UserID, library.CookingListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(history.Records) != 1 {
		t.Fatal("other user's cooking was lost")
	}
}
