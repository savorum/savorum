package sqlite

import (
	"errors"
	"testing"

	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/identity"
)

func TestAutomaticCloudMappingRequiresLocalOwnerAndNeverRestoresRevocation(t *testing.T) {
	ctx := t.Context()
	db := openStore(ctx, t)
	b := testBootstrap()

	owner, err := db.BootstrapHosting(ctx, b)
	if err != nil {
		t.Fatal(err)
	}

	m := hosting.MemberRequest{OperationID: "auto_member", TenantID: b.TenantID, Subject: "verified-cloud-account", DisplayName: "Cloud member", Issuer: b.Issuer, BootstrapID: b.OperationID, OwnerSubject: b.Owner.Subject, OwnerUserID: owner.OwnerUserID, LibraryID: owner.LibraryID}
	for _, mutate := range []func(*hosting.MemberRequest){func(m *hosting.MemberRequest) { m.Issuer = "https://other.test" }, func(m *hosting.MemberRequest) { m.BootstrapID = "other" }, func(m *hosting.MemberRequest) { m.OwnerSubject = "stranger" }, func(m *hosting.MemberRequest) { m.OwnerUserID = "other" }, func(m *hosting.MemberRequest) { m.LibraryID = "other" }} {
		bad := m
		mutate(&bad)

		if _, err := db.BootstrapHostingMember(ctx, bad); err == nil {
			t.Fatal("unauthorized mapping accepted")
		}
	}

	result, err := db.BootstrapHostingMember(ctx, m)
	if err != nil {
		t.Fatal(err)
	}

	var (
		role  string
		admin bool
	)
	if err := db.db.QueryRowContext(ctx, `SELECT lm.role,u.instance_admin FROM library_memberships lm JOIN users u ON u.id=lm.user_id WHERE lm.user_id=? AND lm.library_id=?`, result.UserID, result.LibraryID).Scan(&role, &admin); err != nil {
		t.Fatal(err)
	}

	if role != "viewer" || admin || result.UserID == owner.OwnerUserID {
		t.Fatal("mapping elevated permissions")
	}

	if _, err := db.db.ExecContext(ctx, `DELETE FROM library_memberships WHERE user_id=?`, result.UserID); err != nil {
		t.Fatal(err)
	}

	again, err := db.BootstrapHostingMember(ctx, m)
	if err != nil || again != result {
		t.Fatal("retry changed receipt", err)
	}

	if _, err := db.ResolveManagedIdentity(ctx, b.ProviderID, m.Subject, result.UserID, result.LibraryID); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("retry restored removed membership", err)
	}

	bad := m

	bad.Subject = "other-account"
	if _, err := db.BootstrapHostingMember(ctx, bad); !errors.Is(err, identity.ErrConflict) {
		t.Fatal("operation reused for another account", err)
	}
}
