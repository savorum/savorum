package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

func requireInvitationOwner(ctx context.Context, tx *sql.Tx, actor, libraryID string) error {
	if err := requireAdmin(ctx, tx, actor); err == nil {
		return nil
	}

	return requireLibraryOwner(ctx, tx, actor, libraryID)
}

// CreateInvitation returns its bearer token once. Hints are never identity keys.
func (s *Store) CreateInvitation(ctx context.Context, libraryID string, role access.Role, hint string, days int) (identity.Invitation, string, error) {
	var v identity.Invitation

	p, ok := identity.FromContext(ctx)
	if !ok {
		return v, "", access.ErrForbidden
	}

	if !role.Allows(access.Viewer) || len(hint) > 320 {
		return v, "", identity.ErrConflict
	}

	if days == 0 {
		days = 7
	}

	if days < 1 || days > 30 {
		return v, "", identity.ErrConflict
	}

	id, err := opaqueToken()
	if err != nil {
		return v, "", err
	}

	token, err := opaqueToken()
	if err != nil {
		return v, "", err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, "", fmt.Errorf("begin invitation: %w", err)
	}
	defer tx.Rollback()

	if err = requireInvitationOwner(ctx, tx, p.UserID, libraryID); err != nil {
		return v, "", err
	}

	var activeLibrary bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM libraries WHERE id=? AND deleted_at IS NULL)`, libraryID).Scan(&activeLibrary); err != nil {
		return v, "", fmt.Errorf("check invitation library: %w", err)
	}

	if !activeLibrary {
		return v, "", identity.ErrConflict
	}

	now := time.Now().Unix()
	v = identity.Invitation{ID: id, LibraryID: libraryID, Role: role, Hint: strings.TrimSpace(hint), Creator: p.UserID, CreatedAt: now, ExpiresAt: now + int64(days)*86400}

	_, err = tx.ExecContext(ctx, `INSERT INTO invitations(id,library_id,role,hint,token_hash,creator,created_at,expires_at) SELECT ?,?,?,?,?,?,?,? WHERE EXISTS(SELECT 1 FROM libraries WHERE id=? AND deleted_at IS NULL)`, id, libraryID, role, v.Hint, tokenHash(token), p.UserID, now, v.ExpiresAt, libraryID)
	if err != nil {
		return v, "", fmt.Errorf("create invitation: %w", err)
	}

	if err = appendAudit(ctx, tx, identity.AuditEvent{ActorID: p.UserID, Action: "invitation.create", TargetType: "invitation", TargetID: id, LibraryID: libraryID, Outcome: "success"}); err != nil {
		return v, "", err
	}

	if err = tx.Commit(); err != nil {
		return v, "", fmt.Errorf("commit invitation: %w", err)
	}

	return v, token, nil
}

// Invitations returns no bearer secrets or hashes.
func (s *Store) Invitations(ctx context.Context, libraryID string) ([]identity.Invitation, error) {
	p, ok := identity.FromContext(ctx)
	if !ok {
		return nil, access.ErrForbidden
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	defer tx.Rollback()

	if err = requireInvitationOwner(ctx, tx, p.UserID, libraryID); err != nil {
		return nil, err
	}

	rows, err := tx.QueryContext(ctx, `SELECT id,library_id,role,hint,creator,created_at,expires_at,accepted_at,revoked_at FROM invitations WHERE library_id=? ORDER BY created_at DESC,id LIMIT 100`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	defer rows.Close()

	result := []identity.Invitation{}

	for rows.Next() {
		var v identity.Invitation
		if err = rows.Scan(&v.ID, &v.LibraryID, &v.Role, &v.Hint, &v.Creator, &v.CreatedAt, &v.ExpiresAt, &v.AcceptedAt, &v.RevokedAt); err != nil {
			return nil, fmt.Errorf("scan invitation: %w", err)
		}

		result = append(result, v)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}

	return result, nil
}

// RevokeInvitation invalidates an unused invitation.
func (s *Store) RevokeInvitation(ctx context.Context, id string) error {
	p, ok := identity.FromContext(ctx)
	if !ok {
		return access.ErrForbidden
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("revoke invitation: %w", err)
	}
	defer tx.Rollback()

	var libraryID string
	if err = tx.QueryRowContext(ctx, `SELECT library_id FROM invitations WHERE id=?`, id).Scan(&libraryID); err != nil {
		return identity.ErrConflict
	}

	if err = requireInvitationOwner(ctx, tx, p.UserID, libraryID); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `UPDATE invitations SET revoked_at=unixepoch() WHERE id=? AND accepted_at IS NULL AND revoked_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("revoke invitation: %w", err)
	}

	n, _ := result.RowsAffected()
	if n != 1 {
		return identity.ErrConflict
	}

	if err = appendAudit(ctx, tx, identity.AuditEvent{ActorID: p.UserID, Action: "invitation.revoke", TargetType: "invitation", TargetID: id, LibraryID: libraryID, Outcome: "success"}); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit invitation revoke: %w", err)
	}

	return nil
}

func acceptInvitation(ctx context.Context, tx *sql.Tx, user, token string) error {
	var (
		id, libraryID string
		role          access.Role
	)

	err := tx.QueryRowContext(ctx, `SELECT i.id,i.library_id,i.role FROM invitations i JOIN libraries l ON l.id=i.library_id WHERE token_hash=? AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at>unixepoch() AND l.deleted_at IS NULL AND EXISTS(SELECT 1 FROM users WHERE id=? AND status='active')`, tokenHash(token), user).Scan(&id, &libraryID, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return identity.ErrCredentials
	}

	if err != nil {
		return fmt.Errorf("read invitation: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO library_memberships VALUES(?,?,?,'manual') ON CONFLICT(library_id,user_id,source) DO UPDATE SET role=CASE WHEN library_memberships.role='owner' OR excluded.role='owner' THEN 'owner' WHEN library_memberships.role='editor' OR excluded.role='editor' THEN 'editor' ELSE 'viewer' END`, libraryID, user, role); err != nil {
		return fmt.Errorf("accept membership: %w", err)
	}

	result, err := tx.ExecContext(ctx, `UPDATE invitations SET accepted_at=unixepoch(),accepted_by=? WHERE id=? AND accepted_at IS NULL AND revoked_at IS NULL`, user, id)
	if err != nil {
		return fmt.Errorf("consume invitation: %w", err)
	}

	n, _ := result.RowsAffected()
	if n != 1 {
		return identity.ErrConflict
	}

	return appendAudit(ctx, tx, identity.AuditEvent{ActorID: user, Action: "invitation.accept", TargetType: "invitation", TargetID: id, LibraryID: libraryID, Outcome: "success"})
}

// AcceptInvitation grants only the named library role to the authenticated user.
func (s *Store) AcceptInvitation(ctx context.Context, token string) error {
	p, ok := identity.FromContext(ctx)
	if !ok {
		return access.ErrForbidden
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("accept invitation: %w", err)
	}
	defer tx.Rollback()

	if err = acceptInvitation(ctx, tx, p.UserID, token); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit invitation: %w", err)
	}

	return nil
}

// RegisterInvitedLocalUser atomically creates an ordinary local account and consumes an invitation.
func (s *Store) RegisterInvitedLocalUser(ctx context.Context, token, login, display, password string) (identity.Principal, error) {
	var p identity.Principal

	login, err := normalizeLogin(login)
	if err != nil {
		return p, err
	}

	// Recheck and consume inside the creation transaction after hashing.
	var usable bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM invitations i JOIN libraries l ON l.id=i.library_id WHERE token_hash=? AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at>unixepoch() AND l.deleted_at IS NULL)`, tokenHash(token)).Scan(&usable); err != nil {
		return p, fmt.Errorf("validate invitation: %w", err)
	}

	if !usable {
		return p, identity.ErrCredentials
	}

	hash, err := identity.HashPassword(ctx, password)
	if err != nil {
		return p, fmt.Errorf("hash invited password: %w", err)
	}

	id, err := opaqueToken()
	if err != nil {
		return p, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, fmt.Errorf("register invited account: %w", err)
	}
	defer tx.Rollback()

	display = strings.TrimSpace(display)
	if display == "" {
		display = login
	}

	display, err = identity.NormalizeDisplayName(display)
	if err != nil {
		return p, err
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO users(id,display_name,created_at,updated_at) VALUES(?,?,unixepoch(),unixepoch())`, id, display); err != nil {
		return p, fmt.Errorf("create invited user: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO identities(user_id,provider,provider_subject,password_hash,password_changed_at) VALUES(?,'local',?,?,unixepoch())`, id, login, hash); err != nil {
		return p, identity.ErrConflict
	}

	if err = acceptInvitation(ctx, tx, id, token); err != nil {
		return p, err
	}

	if err = auditIdentity(ctx, tx, id, id, "create-user"); err != nil {
		return p, err
	}

	if err = tx.Commit(); err != nil {
		return p, fmt.Errorf("commit invited user: %w", err)
	}

	return identity.Principal{UserID: id, DisplayName: display}, nil
}
