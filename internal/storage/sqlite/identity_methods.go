package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

// ResolveExternalIdentity accepts only proof verified by a configured server-side provider.
// Email is deliberately ignored: the immutable (provider, subject) pair is the only key.
func (s *Store) ResolveExternalIdentity(ctx context.Context, providerID, subject, display, _ string, autoProvision bool, defaultLibrary string, defaultRole access.Role, invitationToken, linkUserID string) (identity.Principal, error) {
	var p identity.Principal
	if providerID == "local" || subject == "" || len(subject) > 4096 {
		return p, identity.ErrCredentials
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, fmt.Errorf("external identity: %w", err)
	}
	defer tx.Rollback()

	if err := authorizeLinkSession(ctx, tx, linkUserID); err != nil {
		return p, err
	}

	var enabled bool
	if err = tx.QueryRowContext(ctx, `SELECT enabled FROM auth_providers WHERE id=?`, providerID).Scan(&enabled); err != nil || !enabled {
		return p, identity.ErrCredentials
	}

	err = tx.QueryRowContext(ctx, `SELECT u.id,u.display_name,u.instance_admin FROM identities i JOIN users u ON u.id=i.user_id WHERE i.provider=? AND i.provider_subject=? AND i.enabled=1 AND u.status='active'`, providerID, subject).Scan(&p.UserID, &p.DisplayName, &p.InstanceAdmin)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("resolve external user: %w", err)
	}

	if p.UserID != "" && linkUserID != "" && p.UserID != linkUserID {
		return p, identity.ErrConflict
	}

	if p.UserID == "" {
		if !autoProvision && invitationToken == "" && linkUserID == "" {
			return p, identity.ErrCredentials
		}

		p, err = provisionExternalUser(ctx, tx, providerID, display, linkUserID, autoProvision, defaultLibrary, defaultRole)
		if err != nil {
			return p, err
		}

		if _, err = tx.ExecContext(ctx, `INSERT INTO identities(user_id,provider,provider_subject) VALUES(?,?,?)`, p.UserID, providerID, subject); err != nil {
			return p, identity.ErrConflict
		}

		if err = auditIdentity(ctx, tx, p.UserID, p.UserID, "identity.link"); err != nil {
			return p, err
		}

		if linkUserID != "" {
			if _, err = tx.ExecContext(ctx, `UPDATE browser_sessions SET revoked_at=unixepoch(),revocation_reason='identity-link' WHERE user_id=? AND revoked_at IS NULL`, p.UserID); err != nil {
				return p, fmt.Errorf("revoke linked sessions: %w", err)
			}
		}
	}

	if invitationToken != "" {
		if err = acceptInvitation(ctx, tx, p.UserID, invitationToken); err != nil {
			return p, err
		}
	}

	if err = tx.Commit(); err != nil {
		return p, fmt.Errorf("commit external identity: %w", err)
	}

	return p, nil
}

// SignInMethods uses stable random IDs, never mutable display labels.
func (s *Store) SignInMethods(ctx context.Context, user string) ([]identity.SignInMethod, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.id,i.provider,CASE WHEN i.provider='local' THEN 'Local password' ELSE COALESCE(p.display_name,'Unavailable provider') END,i.enabled AND (i.provider='local' OR COALESCE(p.enabled,0)) FROM identities i LEFT JOIN auth_providers p ON p.id=i.provider WHERE i.user_id=? ORDER BY i.id`, user)
	if err != nil {
		return nil, fmt.Errorf("list sign-in methods: %w", err)
	}
	defer rows.Close()

	result := []identity.SignInMethod{}

	for rows.Next() {
		var m identity.SignInMethod
		if err = rows.Scan(&m.ID, &m.Provider, &m.Name, &m.Enabled); err != nil {
			return nil, fmt.Errorf("scan sign-in method: %w", err)
		}

		result = append(result, m)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("list sign-in methods: %w", err)
	}

	return result, nil
}

// requireRemainingAuthPath must run inside the same transaction as removal.
func requireRemainingAuthPath(ctx context.Context, tx *sql.Tx, user, excludeIdentityID, excludeProvider string) error {
	var count int

	err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM identities i LEFT JOIN auth_providers p ON p.id=i.provider WHERE i.user_id=? AND i.enabled=1 AND i.id<>? AND i.provider<>? AND (i.provider='local' OR p.enabled=1)) + (SELECT count(*) FROM passkeys WHERE user_id=?)`, user, excludeIdentityID, excludeProvider, user).Scan(&count)
	if err != nil {
		return fmt.Errorf("check remaining authentication methods: %w", err)
	}

	if count == 0 {
		allowed, err := localRecoveryAllowed(ctx, tx, user)
		if err != nil {
			return fmt.Errorf("check recovery policy: %w", err)
		}

		if allowed {
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM recovery_codes WHERE user_id=?`, user).Scan(&count); err != nil {
				return fmt.Errorf("check recovery methods: %w", err)
			}
		}
	}

	if count == 0 {
		return identity.ErrConflict
	}

	return nil
}

// UnlinkIdentity permits self-service safe removal or an explicitly audited administrator override.
func (s *Store) UnlinkIdentity(ctx context.Context, actor, user, id string, force bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("unlink identity: %w", err)
	}
	defer tx.Rollback()

	if force {
		if err = requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
	} else {
		if actor != user {
			return access.ErrForbidden
		}
	}

	var provider string
	if err = tx.QueryRowContext(ctx, `SELECT provider FROM identities WHERE user_id=? AND id=?`, user, id).Scan(&provider); errors.Is(err, sql.ErrNoRows) {
		return identity.ErrConflict
	} else if err != nil {
		return fmt.Errorf("unlink identity: %w", err)
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM identities WHERE user_id=? AND id=?`, user, id)
	if err != nil {
		return fmt.Errorf("unlink identity: %w", err)
	}

	n, _ := result.RowsAffected()
	if n != 1 {
		return identity.ErrConflict
	}

	if !force {
		if err = requireRemainingAuthPath(ctx, tx, user, "", ""); err != nil {
			return err
		}
	}

	if _, err = tx.ExecContext(ctx, `UPDATE browser_sessions SET revoked_at=unixepoch(),revocation_reason='identity-unlink' WHERE user_id=? AND revoked_at IS NULL`, user); err != nil {
		return fmt.Errorf("revoke identity sessions: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM native_sign_in_grants WHERE user_id=?`, user); err != nil {
		return fmt.Errorf("revoke native identity grants: %w", err)
	}

	if err = revokeProviderAuthority(ctx, tx, user, provider); err != nil {
		return err
	}

	action := "identity.unlink"
	if force {
		action = "identity.force-unlink"
	}

	if err = auditIdentity(ctx, tx, actor, user, action); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit unlink: %w", err)
	}

	return nil
}

// revokeProviderAuthority removes what a provider's trust granted a user once no
// enabled identity at that provider remains: its default and group-mapped
// memberships and the personal tokens issued through its sessions. Manual
// memberships and tokens from other sources are untouched.
func revokeProviderAuthority(ctx context.Context, tx *sql.Tx, user, provider string) error {
	var remaining int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identities WHERE user_id=? AND provider=? AND enabled=1`, user, provider).Scan(&remaining); err != nil {
		return fmt.Errorf("check remaining provider identities: %w", err)
	}

	if remaining > 0 {
		return nil
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM library_memberships WHERE user_id=? AND (source=? OR source IN (SELECT 'mapping:'||id FROM provider_group_mappings WHERE provider_id=?))`, user, "provider:"+provider+":default", provider); err != nil {
		return fmt.Errorf("revoke provider memberships: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE personal_tokens SET revoked_at=unixepoch() WHERE user_id=? AND source_provider=? AND revoked_at=0`, user, provider); err != nil {
		return fmt.Errorf("revoke provider tokens: %w", err)
	}

	return nil
}

// ProviderDependentUsers names accounts that would lose their last available path.
func (s *Store) ProviderDependentUsers(ctx context.Context, provider string) ([]identity.User, error) {
	p, ok := identity.FromContext(ctx)
	if !ok {
		return nil, access.ErrForbidden
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("provider dependencies: %w", err)
	}
	defer tx.Rollback()

	if err = requireAdmin(ctx, tx, p.UserID); err != nil {
		return nil, err
	}

	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT u.id,u.display_name,u.instance_admin,u.status,u.created_at FROM users u JOIN identities i ON i.user_id=u.id WHERE u.status='active' AND i.provider=? AND i.enabled=1 AND NOT EXISTS(SELECT 1 FROM identities other LEFT JOIN auth_providers p ON p.id=other.provider WHERE other.user_id=u.id AND other.enabled=1 AND other.provider<>? AND (other.provider='local' OR p.enabled=1)) AND NOT EXISTS(SELECT 1 FROM passkeys WHERE user_id=u.id)`, provider, provider)
	if err != nil {
		return nil, fmt.Errorf("provider dependencies: %w", err)
	}
	defer rows.Close()

	result := []identity.User{}

	for rows.Next() {
		var (
			u       identity.User
			created int64
		)
		if err = rows.Scan(&u.UserID, &u.DisplayName, &u.InstanceAdmin, &u.Status, &created); err != nil {
			return nil, fmt.Errorf("scan provider dependency: %w", err)
		}

		result = append(result, u)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("provider dependencies: %w", err)
	}

	return result, nil
}

func provisionExternalUser(ctx context.Context, tx *sql.Tx, providerID, display, linkUserID string, autoProvision bool, defaultLibrary string, defaultRole access.Role) (identity.Principal, error) {
	var p identity.Principal
	if linkUserID != "" {
		if err := tx.QueryRowContext(ctx, `SELECT id,display_name,instance_admin FROM users WHERE id=? AND status='active'`, linkUserID).Scan(&p.UserID, &p.DisplayName, &p.InstanceAdmin); err != nil {
			return p, identity.ErrCredentials
		}

		return p, nil
	}

	var err error

	p.UserID, err = opaqueToken()
	if err != nil {
		return p, err
	}

	p.DisplayName = strings.TrimSpace(display)
	if p.DisplayName == "" {
		p.DisplayName = "New member"
	}

	if len([]rune(p.DisplayName)) > identity.MaxDisplayNameLength {
		p.DisplayName = string([]rune(p.DisplayName)[:identity.MaxDisplayNameLength])
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO users(id,display_name,created_at,updated_at) VALUES(?,?,unixepoch(),unixepoch())`, p.UserID, p.DisplayName); err != nil {
		return p, fmt.Errorf("provision external user: %w", err)
	}

	if autoProvision && defaultLibrary != "" {
		if !defaultRole.Allows(access.Viewer) {
			return p, identity.ErrConflict
		}

		if _, err = tx.ExecContext(ctx, `INSERT INTO library_memberships VALUES(?,?,?,?)`, defaultLibrary, p.UserID, defaultRole, "provider:"+providerID+":default"); err != nil {
			return p, fmt.Errorf("provision external membership: %w", err)
		}
	}

	return p, nil
}
