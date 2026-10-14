package sqlite

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/savorum/savorum/internal/identity"
)

// CreateSession issues independent random bearer and CSRF secrets, storing only hashes.
func (s *Store) CreateSession(ctx context.Context, userID, provider string, idle, absolute time.Duration) (identity.SessionCredentials, error) {
	if idle == 0 {
		idle = 30 * 24 * time.Hour
	}

	if absolute == 0 {
		absolute = 180 * 24 * time.Hour
	}

	if idle < time.Minute || idle > 180*24*time.Hour || absolute < idle || absolute > 365*24*time.Hour {
		return identity.SessionCredentials{}, fmt.Errorf("invalid session lifetimes")
	}

	token, err := opaqueToken()
	if err != nil {
		return identity.SessionCredentials{}, err
	}

	csrf := identity.CSRFToken(token)

	id, err := opaqueToken()
	if err != nil {
		return identity.SessionCredentials{}, err
	}

	now := time.Now().UTC().Truncate(time.Second)
	session := identity.SessionCredentials{ID: id, UserID: userID, CreatedAt: now, LastSeen: now, IdleExpires: now.Add(idle), AbsoluteExpires: now.Add(absolute), Provider: provider, Token: token, CSRF: csrf}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return identity.SessionCredentials{}, fmt.Errorf("begin session: %w", err)
	}
	defer tx.Rollback()

	var managed int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM auth_providers WHERE id=? AND kind='managed'`, provider).Scan(&managed); err != nil {
		return identity.SessionCredentials{}, fmt.Errorf("check session provider: %w", err)
	}

	if managed > 0 {
		var allowed int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM identities i JOIN auth_providers a ON a.id=i.provider JOIN hosting_tenant h ON h.provider_id=a.id WHERE i.user_id=? AND a.id=? AND i.enabled=1 AND a.enabled=1 AND EXISTS(SELECT 1 FROM library_memberships m JOIN libraries l ON l.id=m.library_id WHERE m.user_id=i.user_id AND l.deleted_at IS NULL)`, userID, provider).Scan(&allowed); err != nil {
			return identity.SessionCredentials{}, fmt.Errorf("check managed access: %w", err)
		}

		if allowed == 0 {
			return identity.SessionCredentials{}, identity.ErrCredentials
		}
	}

	// Native code redemption and session creation are separate operations. The
	// stable identity binding closes unlink/relink between those operations.
	if identityID, bound := identity.SessionIdentity(ctx); bound {
		if identityID == "" {
			return identity.SessionCredentials{}, identity.ErrCredentials
		}

		var active int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM identities i JOIN auth_providers a ON a.id=i.provider WHERE i.id=? AND i.user_id=? AND i.provider=? AND i.enabled=1 AND a.enabled=1`, identityID, userID, provider).Scan(&active); err != nil {
			return identity.SessionCredentials{}, fmt.Errorf("check native session identity: %w", err)
		}

		if active != 1 {
			return identity.SessionCredentials{}, identity.ErrCredentials
		}
	}

	result, err := tx.ExecContext(ctx, `INSERT INTO browser_sessions(id,user_id,token_hash,csrf_hash,created_at,last_seen,idle_expires,absolute_expires,idle_seconds,provider) SELECT ?,id,?,?,?,?,?,?,?,? FROM users WHERE id=? AND status='active'`, id, tokenHash(token), tokenHash(csrf), now.Unix(), now.Unix(), session.IdleExpires.Unix(), session.AbsoluteExpires.Unix(), int64(idle/time.Second), provider, userID)
	if err != nil {
		return identity.SessionCredentials{}, fmt.Errorf("create session: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return identity.SessionCredentials{}, fmt.Errorf("count session creation: %w", err)
	}

	if n != 1 {
		return identity.SessionCredentials{}, identity.ErrCredentials
	}

	if err := appendAudit(ctx, tx, identity.AuditEvent{ActorID: userID, Action: "auth.login.succeeded", TargetType: "session", TargetID: id}); err != nil {
		return identity.SessionCredentials{}, err
	}

	if err := tx.Commit(); err != nil {
		return identity.SessionCredentials{}, fmt.Errorf("commit session: %w", err)
	}

	return session, nil
}

// ResolveSession checks both deadlines and revocation before refreshing idle expiration.
func (s *Store) ResolveSession(ctx context.Context, token string) (identity.Principal, identity.Session, error) {
	var p identity.Principal

	var session identity.Session

	var created, seen, idle, absolute int64

	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.display_name,u.instance_admin,b.id,b.created_at,b.last_seen,b.idle_expires,b.absolute_expires,b.provider FROM browser_sessions b JOIN users u ON u.id=b.user_id WHERE b.token_hash=? AND b.revoked_at IS NULL AND b.idle_expires>unixepoch() AND b.absolute_expires>unixepoch() AND u.status='active'`, tokenHash(token)).Scan(&p.UserID, &p.DisplayName, &p.InstanceAdmin, &session.ID, &created, &seen, &idle, &absolute, &session.Provider)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return p, session, identity.ErrCredentials
		}

		return p, session, fmt.Errorf("resolve session: %w", err)
	}

	p.CredentialID = session.ID
	p.Method = session.Provider
	session.UserID = p.UserID
	session.CreatedAt = time.Unix(created, 0).UTC()
	session.LastSeen = time.Unix(seen, 0).UTC()
	session.IdleExpires = time.Unix(idle, 0).UTC()

	session.AbsoluteExpires = time.Unix(absolute, 0).UTC()
	if _, err := s.db.ExecContext(ctx, `UPDATE browser_sessions SET last_seen=unixepoch(),idle_expires=min(unixepoch()+idle_seconds,absolute_expires) WHERE id=? AND revoked_at IS NULL`, session.ID); err != nil && !isCapacityError(err) {
		return p, session, fmt.Errorf("refresh session: %w", err)
	}

	return p, session, nil
}

// CheckCSRF compares the session-bound secret in constant time.
func (s *Store) CheckCSRF(ctx context.Context, token, csrf string) bool {
	if token == "" || csrf == "" {
		return false
	}

	var hash string

	err := s.db.QueryRowContext(ctx, `SELECT csrf_hash FROM browser_sessions WHERE token_hash=? AND revoked_at IS NULL AND idle_expires>unixepoch() AND absolute_expires>unixepoch()`, tokenHash(token)).Scan(&hash)

	return err == nil && subtle.ConstantTimeCompare([]byte(hash), []byte(tokenHash(csrf))) == 1
}

// Sessions lists active sessions without any authentication secrets.
func (s *Store) Sessions(ctx context.Context, userID string) ([]identity.Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,created_at,last_seen,idle_expires,absolute_expires,provider FROM browser_sessions WHERE user_id=? AND revoked_at IS NULL AND idle_expires>unixepoch() AND absolute_expires>unixepoch() ORDER BY created_at DESC,id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	sessions := []identity.Session{}

	for rows.Next() {
		var session identity.Session

		var created, seen, idle, absolute int64
		if err := rows.Scan(&session.ID, &created, &seen, &idle, &absolute, &session.Provider); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}

		session.UserID = userID
		session.CreatedAt = time.Unix(created, 0).UTC()
		session.LastSeen = time.Unix(seen, 0).UTC()
		session.IdleExpires = time.Unix(idle, 0).UTC()
		session.AbsoluteExpires = time.Unix(absolute, 0).UTC()
		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}

	return sessions, nil
}

// RevokeSession revokes only a session belonging to the supplied user.
func (s *Store) RevokeSession(ctx context.Context, userID, sessionID string) error {
	return s.revokeSessions(ctx, userID, sessionID, false)
}

// RevokeOtherSessions retains only the caller's current session.
func (s *Store) RevokeOtherSessions(ctx context.Context, userID, keepID string) error {
	return s.revokeSessions(ctx, userID, keepID, true)
}

func (s *Store) revokeSessions(ctx context.Context, userID, id string, others bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin session revocation: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `UPDATE browser_sessions SET revoked_at=unixepoch(),revocation_reason='user-revoked' WHERE user_id=? AND revoked_at IS NULL AND ((?=0 AND id=?) OR (?=1 AND id<>?))`, userID, others, id, others, id)
	if err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count revocations: %w", err)
	}

	if count > 0 {
		action := "auth.session.revoked"
		if others {
			action = "auth.sessions.revoked"
		}

		if err := appendAudit(ctx, tx, identity.AuditEvent{ActorID: userID, Action: action, TargetType: "user", TargetID: userID}); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit session revocation: %w", err)
	}

	return nil
}
