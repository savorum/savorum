package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/identity"
)

func opaqueToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate identity token: %w", err)
	}

	return hex.EncodeToString(b), nil
}

func tokenHash(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}

func normalizeLogin(login string) (string, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	if len(login) < 1 || len(login) > 128 || strings.ContainsAny(login, " \t\r\n") {
		return "", fmt.Errorf("login must be 1–128 characters without whitespace")
	}

	return login, nil
}

// LocalPrincipal resolves the persisted local-mode account.
func (s *Store) LocalPrincipal(ctx context.Context) (identity.Principal, error) {
	var p identity.Principal

	err := s.db.QueryRowContext(ctx, `SELECT id,display_name,instance_admin FROM users WHERE local_mode=1 AND status='active'`).Scan(&p.UserID, &p.DisplayName, &p.InstanceAdmin)
	if err != nil {
		return p, fmt.Errorf("resolve local principal: %w", err)
	}

	return p, nil
}

// User reads a provider-independent user including tombstones.
func (s *Store) User(ctx context.Context, id string) (identity.User, error) {
	var u identity.User

	var created int64

	err := s.db.QueryRowContext(ctx, `SELECT id,display_name,instance_admin,status,created_at FROM users WHERE id=?`, id).Scan(&u.UserID, &u.DisplayName, &u.InstanceAdmin, &u.Status, &created)
	if err != nil {
		return u, fmt.Errorf("read user: %w", err)
	}

	u.CreatedAt = time.Unix(created, 0).UTC()

	return u, nil
}

// Users lists application accounts for administration.
func (s *Store) Users(ctx context.Context) ([]identity.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,display_name,instance_admin,status,created_at FROM users ORDER BY created_at,id`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	users := []identity.User{}

	for rows.Next() {
		var u identity.User

		var created int64
		if err := rows.Scan(&u.UserID, &u.DisplayName, &u.InstanceAdmin, &u.Status, &created); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}

		u.CreatedAt = time.Unix(created, 0).UTC()
		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}

	return users, nil
}

// BootstrapAdmin explicitly claims the local-mode account; only instance CLI calls this.
func (s *Store) BootstrapAdmin(ctx context.Context, login, display, password string) (identity.Principal, error) {
	login, err := normalizeLogin(login)
	if err != nil {
		return identity.Principal{}, err
	}

	hash, err := identity.HashPassword(ctx, password)
	if err != nil {
		return identity.Principal{}, fmt.Errorf("hash password: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return identity.Principal{}, fmt.Errorf("begin bootstrap: %w", err)
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users u JOIN identities i ON u.id=i.user_id WHERE u.instance_admin=1 AND u.status='active' AND i.enabled=1`).Scan(&count); err != nil {
		return identity.Principal{}, fmt.Errorf("check administrators: %w", err)
	}

	if count != 0 {
		return identity.Principal{}, identity.ErrConflict
	}

	var id string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE local_mode=1 AND status='active'`).Scan(&id); err != nil {
		return identity.Principal{}, fmt.Errorf("find local account: %w", err)
	}

	if strings.TrimSpace(display) == "" {
		display = login
	}

	display, err = identity.NormalizeDisplayName(display)
	if err != nil {
		return identity.Principal{}, err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE users SET display_name=?,instance_admin=1,updated_at=unixepoch() WHERE id=?`, display, id); err != nil {
		return identity.Principal{}, fmt.Errorf("claim local account: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO identities(user_id,provider,provider_subject,password_hash,password_changed_at) VALUES(?,'local',?,?,unixepoch())`, id, login, hash); err != nil {
		return identity.Principal{}, fmt.Errorf("create administrator identity: %w", err)
	}

	if err := auditIdentity(ctx, tx, "instance-owner", id, "bootstrap"); err != nil {
		return identity.Principal{}, err
	}

	if err := tx.Commit(); err != nil {
		return identity.Principal{}, fmt.Errorf("commit bootstrap: %w", err)
	}

	return identity.Principal{UserID: id, DisplayName: display, InstanceAdmin: true}, nil
}

// CreateLocalUser creates an ordinary account; actor must be an active administrator.
func (s *Store) CreateLocalUser(ctx context.Context, actor, login, display, password string) (identity.Principal, error) {
	login, err := normalizeLogin(login)
	if err != nil {
		return identity.Principal{}, err
	}

	hash, err := identity.HashPassword(ctx, password)
	if err != nil {
		return identity.Principal{}, fmt.Errorf("hash password: %w", err)
	}

	id, err := opaqueToken()
	if err != nil {
		return identity.Principal{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return identity.Principal{}, fmt.Errorf("begin user creation: %w", err)
	}
	defer tx.Rollback()

	if err := requireAdmin(ctx, tx, actor); err != nil {
		return identity.Principal{}, err
	}

	if strings.TrimSpace(display) == "" {
		display = login
	}

	display, err = identity.NormalizeDisplayName(display)
	if err != nil {
		return identity.Principal{}, err
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO users(id,display_name,created_at,updated_at) VALUES(?,?,unixepoch(),unixepoch())`, id, display); err != nil {
		return identity.Principal{}, fmt.Errorf("create user: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO identities(user_id,provider,provider_subject,password_hash,password_changed_at) VALUES(?,'local',?,?,unixepoch())`, id, login, hash); err != nil {
		return identity.Principal{}, fmt.Errorf("create local identity: %w", err)
	}

	if err := auditIdentity(ctx, tx, actor, id, "create-user"); err != nil {
		return identity.Principal{}, err
	}

	if err := tx.Commit(); err != nil {
		return identity.Principal{}, fmt.Errorf("commit user creation: %w", err)
	}

	return identity.Principal{UserID: id, DisplayName: display}, nil
}

func requireAdmin(ctx context.Context, tx *sql.Tx, actor string) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id=? AND instance_admin=1 AND status='active'`, actor).Scan(&count); err != nil {
		return fmt.Errorf("check administrator: %w", err)
	}

	if count != 1 {
		return identity.ErrCredentials
	}

	return nil
}

func auditIdentity(ctx context.Context, tx *sql.Tx, actor, target, action string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO identity_audit(actor_id,target_id,action,created_at) VALUES(?,?,?,unixepoch())`, actor, target, action); err != nil {
		return fmt.Errorf("audit identity operation: %w", err)
	}

	event := identityAuditEvent(actor, target, action)
	if event.LibraryID == "" && (event.TargetType == "recipe" || event.TargetType == "book") {
		event.LibraryID = scopeID(ctx)
	}

	return appendAudit(ctx, tx, event)
}

// Authenticate verifies local credentials with bounded persisted failure backoff.
func (s *Store) Authenticate(ctx context.Context, login, password, clientKey string) (identity.Principal, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	key := tokenHash("account:" + login)
	clientHash := tokenHash("client:" + clientKey)

	var retry int64

	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(retry_at),0) FROM authentication_backoff WHERE key_hash IN (?,?)`, key, clientHash).Scan(&retry)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return identity.Principal{}, fmt.Errorf("read login backoff: %w", err)
	}

	if retry > time.Now().Unix() {
		return identity.Principal{}, identity.ErrCredentials
	}

	var p identity.Principal

	var hash string

	err = s.db.QueryRowContext(ctx, `SELECT u.id,u.display_name,u.instance_admin,i.password_hash FROM users u JOIN identities i ON i.user_id=u.id WHERE i.provider='local' AND i.provider_subject=? AND i.enabled=1 AND u.status='active'`, login).Scan(&p.UserID, &p.DisplayName, &p.InstanceAdmin, &hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("read credentials: %w", err)
	}

	if hash == "" {
		hash = "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	}

	valid, upgrade, err := identity.VerifyPassword(ctx, hash, password)
	if err != nil {
		return identity.Principal{}, err
	}

	if !valid {
		// A bounded delay is persisted, so process restarts do not erase backoff.
		for _, failedKey := range []string{key, clientHash} {
			if _, err := s.db.ExecContext(ctx, `INSERT INTO authentication_backoff VALUES(?,1,unixepoch()+1) ON CONFLICT(key_hash) DO UPDATE SET failures=CASE WHEN retry_at<unixepoch()-3600 THEN 1 ELSE min(failures+1,8) END,retry_at=unixepoch()+min(1 << min(failures,8),300)`, failedKey); err != nil {
				return identity.Principal{}, fmt.Errorf("record login failure: %w", err)
			}
		}

		if err := s.auditLoginFailure(ctx); err != nil {
			return identity.Principal{}, err
		}

		return identity.Principal{}, identity.ErrCredentials
	}

	if upgrade {
		updated, err := identity.HashPassword(ctx, password)
		if err != nil {
			return identity.Principal{}, fmt.Errorf("upgrade password: %w", err)
		}

		if _, err := s.db.ExecContext(ctx, `UPDATE identities SET password_hash=? WHERE provider='local' AND provider_subject=? AND password_hash=?`, updated, login, hash); err != nil {
			return identity.Principal{}, fmt.Errorf("persist password upgrade: %w", err)
		}
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM authentication_backoff WHERE key_hash=? OR retry_at<unixepoch()-86400`, key); err != nil {
		return identity.Principal{}, fmt.Errorf("clear login backoff: %w", err)
	}

	return p, nil
}

// VerifyCurrentPassword validates a user's current local credential for sensitive actions.
func (s *Store) VerifyCurrentPassword(ctx context.Context, userID, password string) error {
	var hash string

	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM identities JOIN users ON users.id=identities.user_id WHERE user_id=? AND provider='local' AND enabled=1 AND users.status='active'`, userID).Scan(&hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return identity.ErrCredentials
		}

		return fmt.Errorf("read current password: %w", err)
	}

	valid, _, err := identity.VerifyPassword(ctx, hash, password)
	if err != nil {
		return err
	}

	if !valid {
		return identity.ErrCredentials
	}

	return nil
}

// ChangePassword confirms current credentials and invalidates all browser sessions.
func (s *Store) ChangePassword(ctx context.Context, userID, current, password string) error {
	if err := s.VerifyCurrentPassword(ctx, userID, current); err != nil {
		return err
	}

	return s.replacePassword(ctx, userID, userID, password, false)
}

// RenameUser changes the display name of an active account.
func (s *Store) RenameUser(ctx context.Context, userID, name string) error {
	var err error

	name, err = identity.NormalizeDisplayName(name)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, `UPDATE users SET display_name=?,updated_at=unixepoch() WHERE id=? AND status='active'`, name, userID)
	if err != nil {
		return fmt.Errorf("rename user: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count renamed users: %w", err)
	}

	if n == 0 {
		return identity.ErrConflict
	}

	return nil
}

// ResetPassword permits an administrator to replace another account's credential.
func (s *Store) ResetPassword(ctx context.Context, actor, userID, password string) error {
	return s.replacePassword(ctx, actor, userID, password, true)
}

// RecoverPassword is the audited instance-owner recovery path with direct database access.
func (s *Store) RecoverPassword(ctx context.Context, userID, password string) error {
	return s.replacePassword(ctx, "instance-owner", userID, password, false)
}

func (s *Store) replacePassword(ctx context.Context, actor, userID, password string, admin bool) error {
	hash, err := identity.HashPassword(ctx, password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin password reset: %w", err)
	}
	defer tx.Rollback()

	if admin {
		if err := requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
	}

	result, err := tx.ExecContext(ctx, `UPDATE identities SET password_hash=?,password_changed_at=unixepoch(),enabled=1 WHERE user_id=? AND provider='local'`, hash, userID)
	if err != nil {
		return fmt.Errorf("replace password: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count password changes: %w", err)
	}

	if n == 0 {
		return identity.ErrConflict
	}

	if _, err := tx.ExecContext(ctx, `UPDATE browser_sessions SET revoked_at=unixepoch(),revocation_reason='password-change' WHERE user_id=? AND revoked_at IS NULL`, userID); err != nil {
		return fmt.Errorf("revoke password sessions: %w", err)
	}

	if err := auditIdentity(ctx, tx, actor, userID, "password-reset"); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit password reset: %w", err)
	}

	return nil
}
