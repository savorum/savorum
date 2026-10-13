package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/identity"
)

func hostedAuthority(ctx context.Context, tx dbQueryer, m hosting.MemberAuthority) (hosting.Tenant, error) {
	if err := m.Validate(); err != nil {
		return hosting.Tenant{}, err
	}

	t, err := hostingTenant(ctx, tx)
	if err != nil {
		return t, err
	}

	if t.TenantID != m.TenantID || t.OperationID != m.BootstrapID {
		return t, identity.ErrConflict
	}

	var mapped bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM identities WHERE provider=? AND provider_subject=? AND user_id=?)`, t.ProviderID, m.Subject, m.UserID).Scan(&mapped); err != nil {
		return t, fmt.Errorf("check hosting mapping: %w", err)
	}

	if !mapped {
		return t, identity.ErrConflict
	}

	return t, nil
}

// CheckHostedOwner requires local authority for whole-instance recovery and ownership.
func (s *Store) CheckHostedOwner(ctx context.Context, m hosting.MemberAuthority) error {
	if err := m.Validate(); err != nil {
		return err
	}

	var allowed bool

	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM hosting_tenant h JOIN users u ON u.id=? JOIN identities i ON i.user_id=u.id AND i.provider=h.provider_id JOIN library_memberships lm ON lm.user_id=u.id AND lm.library_id=h.library_id JOIN libraries l ON l.id=lm.library_id JOIN auth_providers p ON p.id=i.provider WHERE h.singleton=1 AND h.tenant_id=? AND h.operation_id=? AND h.library_id=? AND i.provider_subject=? AND u.status='active' AND u.instance_admin=1 AND i.enabled=1 AND p.enabled=1 AND lm.role='owner' AND l.deleted_at IS NULL)`, m.UserID, m.TenantID, m.BootstrapID, m.LibraryID, m.Subject).Scan(&allowed)
	if err != nil {
		return fmt.Errorf("check hosting owner: %w", err)
	}

	if !allowed {
		return identity.ErrConflict
	}

	return nil
}

// RevokeHostedMember ends credentials and household access without deleting recipes.
func (s *Store) RevokeHostedMember(ctx context.Context, m hosting.MemberAuthority) error {
	if err := m.Validate(); err != nil {
		return err
	}

	tenantBefore, err := hostingTenant(ctx, s.db)
	if err != nil {
		return err
	}

	if tenantBefore.TenantID != m.TenantID || tenantBefore.OperationID != m.BootstrapID {
		return identity.ErrConflict
	}

	encoded, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode hosting revocation: %w", err)
	}

	var prior string

	err = s.db.QueryRowContext(ctx, `SELECT request FROM hosting_member_revocations WHERE operation_id=?`, m.OperationID).Scan(&prior)
	if err == nil {
		if prior != string(encoded) {
			return identity.ErrConflict
		}

		return nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read hosting revocation receipt: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin hosted member revocation: %w", err)
	}
	defer tx.Rollback()

	t, err := hostedAuthority(ctx, tx, m)
	if err != nil {
		return err
	}

	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode hosting revocation: %w", err)
	}

	var previous string

	err = tx.QueryRowContext(ctx, `SELECT request FROM hosting_member_revocations WHERE operation_id=?`, m.OperationID).Scan(&previous)
	if err == nil {
		if previous != string(data) {
			return identity.ErrConflict
		}

		return nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read hosting revocation: %w", err)
	}

	if err := preserveAdministrator(ctx, tx, m.UserID); err != nil {
		return err
	}

	var soleOwner bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM library_memberships WHERE user_id=? AND library_id=? AND role='owner') AND NOT EXISTS(SELECT 1 FROM library_memberships lm JOIN users u ON u.id=lm.user_id WHERE lm.library_id=? AND lm.user_id<>? AND lm.role='owner' AND u.status='active')`, m.UserID, m.LibraryID, m.LibraryID, m.UserID).Scan(&soleOwner); err != nil {
		return fmt.Errorf("check remaining hosting owner: %w", err)
	}

	if soleOwner {
		return identity.ErrConflict
	}

	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE identities SET enabled=0 WHERE provider=? AND provider_subject=? AND user_id=?`, []any{t.ProviderID, m.Subject, m.UserID}},
		{`UPDATE users SET instance_admin=0 WHERE id=?`, []any{m.UserID}},
		{`UPDATE browser_sessions SET revoked_at=unixepoch(),revocation_reason='hosting-member-removed' WHERE user_id=? AND revoked_at IS NULL`, []any{m.UserID}},
		{`UPDATE personal_tokens SET revoked_at=unixepoch() WHERE user_id=? AND revoked_at=0`, []any{m.UserID}},
		{`DELETE FROM library_memberships WHERE library_id=? AND user_id=?`, []any{m.LibraryID, m.UserID}},
		{`DELETE FROM native_sign_in_grants WHERE user_id=?`, []any{m.UserID}},
		{`DELETE FROM native_link_grants WHERE user_id=?`, []any{m.UserID}},
		{`DELETE FROM native_link_tickets WHERE user_id=?`, []any{m.UserID}},
		{`INSERT INTO hosting_member_revocations VALUES(?,?)`, []any{m.OperationID, string(data)}},
	} {
		if _, err := tx.ExecContext(ctx, q.sql, q.args...); err != nil {
			return fmt.Errorf("revoke hosted member: %w", err)
		}
	}

	if err := auditIdentity(ctx, tx, "hosting", m.UserID, "hosting-member-removed"); err != nil {
		return err
	}

	return operationError("commit hosted member revocation", tx.Commit())
}
