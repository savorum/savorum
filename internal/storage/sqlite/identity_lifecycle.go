package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/savorum/savorum/internal/identity"
)

func preserveAdministrator(ctx context.Context, tx *sql.Tx, userID string) error {
	var remaining, target int
	if err := tx.QueryRowContext(ctx, `SELECT instance_admin FROM users WHERE id=? AND status<>'deleted'`, userID).Scan(&target); err != nil {
		return fmt.Errorf("read lifecycle user: %w", err)
	}

	if target == 0 {
		return nil
	}

	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE instance_admin=1 AND status='active' AND id<>?`, userID).Scan(&remaining); err != nil {
		return fmt.Errorf("count remaining administrators: %w", err)
	}

	if remaining == 0 {
		return identity.ErrConflict
	}

	return nil
}

// DisableUser revokes sessions immediately while retaining memberships and history.
func (s *Store) DisableUser(ctx context.Context, actor, userID string, disabled bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin user disable: %w", err)
	}
	defer tx.Rollback()

	if err := requireAdmin(ctx, tx, actor); err != nil {
		return err
	}

	status := "active"
	action := "enable-user"

	if disabled {
		status = "disabled"
		action = "disable-user"

		if err := preserveAdministrator(ctx, tx, userID); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `UPDATE users SET status=?,updated_at=unixepoch() WHERE id=? AND status<>'deleted'`, status, userID); err != nil {
		return fmt.Errorf("update user status: %w", err)
	}

	if disabled {
		if _, err := tx.ExecContext(ctx, `UPDATE browser_sessions SET revoked_at=unixepoch(),revocation_reason='user-disabled' WHERE user_id=? AND revoked_at IS NULL`, userID); err != nil {
			return fmt.Errorf("revoke disabled user sessions: %w", err)
		}
	}

	if err := auditIdentity(ctx, tx, actor, userID, action); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit user status: %w", err)
	}

	return nil
}

// DeleteUser leaves a tombstoned actor and shared history after ownership is resolved.
// HTTP callers must additionally enforce recent authentication and destructive confirmation.
func (s *Store) DeleteUser(ctx context.Context, actor, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin user deletion: %w", err)
	}
	defer tx.Rollback()

	if actor != userID {
		if err := requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
	}

	if err := preserveAdministrator(ctx, tx, userID); err != nil {
		return err
	}

	var sole int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM library_memberships m JOIN libraries l ON l.id=m.library_id WHERE m.user_id=? AND m.role='owner' AND l.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM library_memberships other JOIN users u ON u.id=other.user_id WHERE other.library_id=m.library_id AND other.role='owner' AND other.user_id<>? AND u.status='active')`, userID, userID).Scan(&sole); err != nil {
		return fmt.Errorf("check library ownership: %w", err)
	}

	if sole > 0 {
		return identity.ErrConflict
	}

	if err := eraseUserState(ctx, tx, actor, userID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit user deletion: %w", err)
	}

	return nil
}

func eraseUserState(ctx context.Context, tx *sql.Tx, actor, userID string) error {
	for _, query := range []string{`DELETE FROM passkeys WHERE user_id=?`, `DELETE FROM recovery_codes WHERE user_id=?`, `DELETE FROM webauthn_ceremonies WHERE user_id=?`, `DELETE FROM personal_tokens WHERE user_id=?`, `DELETE FROM federation_transactions WHERE json_extract(payload,'$.LinkUserID')=?`, `DELETE FROM native_sign_in_grants WHERE user_id=?`, `DELETE FROM native_link_tickets WHERE user_id=?`, `DELETE FROM native_link_grants WHERE user_id=?`, `DELETE FROM browser_sessions WHERE user_id=?`, `DELETE FROM identities WHERE user_id=?`, `DELETE FROM recipe_user_state WHERE user_id=?`, `DELETE FROM user_preferences WHERE user_id=?`, `DELETE FROM saved_views WHERE user_id=?`, `DELETE FROM library_memberships WHERE user_id=?`} {
		if _, err := tx.ExecContext(ctx, query, userID); err != nil {
			return fmt.Errorf("delete personal account state: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `UPDATE users SET display_name='Former user',status='deleted',instance_admin=0,local_mode=0,updated_at=unixepoch() WHERE id=?`, userID); err != nil {
		return fmt.Errorf("tombstone user: %w", err)
	}

	if err := auditIdentity(ctx, tx, actor, userID, "delete-user"); err != nil {
		return err
	}

	return nil
}
