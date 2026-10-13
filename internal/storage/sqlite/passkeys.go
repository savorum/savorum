package sqlite

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/savorum/savorum/internal/identity"
)

// Passkeys returns only public credential material for the specified account.
func (s *Store) Passkeys(ctx context.Context, userID string) ([]identity.Passkey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,user_id,credential_json,name,created_at,last_used_at,rp_id FROM passkeys WHERE user_id=? ORDER BY created_at,id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list passkeys: %w", err)
	}
	defer rows.Close()

	out := []identity.Passkey{}

	for rows.Next() {
		var (
			p       identity.Passkey
			raw     string
			created int64
			used    sql.NullInt64
		)
		if err = rows.Scan(&p.ID, &p.UserID, &raw, &p.Name, &created, &used, &p.RPID); err != nil {
			return nil, fmt.Errorf("Passkeys: %w", err)
		}

		if err = json.Unmarshal([]byte(raw), &p.Credential); err != nil {
			return nil, fmt.Errorf("Passkeys: %w", err)
		}

		p.CreatedAt = time.Unix(created, 0).UTC()
		if used.Valid {
			v := time.Unix(used.Int64, 0).UTC()
			p.LastUsedAt = &v
		}

		out = append(out, p)
	}

	return out, credentialOperationError("iterate passkeys", rows.Err())
}

// CheckPasskeyRP refuses a hostname change while credentials remain bound to the old RP.
func (s *Store) CheckPasskeyRP(ctx context.Context, rp string) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM passkeys WHERE rp_id<>?`, rp).Scan(&n); err != nil {
		return fmt.Errorf("CheckPasskeyRP: %w", err)
	}

	if n != 0 {
		return fmt.Errorf("passkey RP ID change would strand existing credentials: %w", identity.ErrConflict)
	}

	return nil
}

// StorePasskey persists a verified credential and audits enrollment atomically.
func (s *Store) StorePasskey(ctx context.Context, userID, name, rp string, c webauthn.Credential) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return identity.ErrConflict
	}

	raw, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("StorePasskey: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("StorePasskey: %w", err)
	}
	defer tx.Rollback()

	var n int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM passkeys WHERE user_id=? OR rp_id<>?`, userID, rp).Scan(&n); err != nil {
		return fmt.Errorf("StorePasskey: %w", err)
	}

	if n >= 20 {
		return identity.ErrConflict
	}

	var mismatched int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM passkeys WHERE rp_id<>?`, rp).Scan(&mismatched); err != nil {
		return fmt.Errorf("StorePasskey: %w", err)
	}

	if mismatched > 0 {
		return identity.ErrConflict
	}

	id := base64.RawURLEncoding.EncodeToString(c.ID)

	result, err := tx.ExecContext(ctx, `INSERT INTO passkeys(id,user_id,credential_json,name,rp_id) SELECT ?,id,?,?,? FROM users WHERE id=? AND status='active'`, id, string(raw), name, rp, userID)
	if err != nil {
		return fmt.Errorf("StorePasskey: %w", err)
	}

	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return identity.ErrCredentials
	}

	if err = appendAudit(ctx, tx, identity.AuditEvent{ActorID: userID, Action: "auth.passkey.created", TargetType: "passkey", TargetID: id}); err != nil {
		return fmt.Errorf("StorePasskey: %w", err)
	}

	return credentialOperationError("commit StorePasskey", tx.Commit())
}

// UpdatePasskeyCredential records verifier-approved counter and backup state with optimistic concurrency.
func (s *Store) UpdatePasskeyCredential(ctx context.Context, userID string, old, next webauthn.Credential) error {
	before, err := json.Marshal(old)
	if err != nil {
		return fmt.Errorf("UpdatePasskeyCredential: %w", err)
	}

	after, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("UpdatePasskeyCredential: %w", err)
	}

	result, err := s.db.ExecContext(ctx, `UPDATE passkeys SET credential_json=?,last_used_at=unixepoch() WHERE id=? AND user_id=? AND credential_json=? AND EXISTS(SELECT 1 FROM users WHERE id=? AND status='active')`, string(after), base64.RawURLEncoding.EncodeToString(next.ID), userID, string(before), userID)
	if err != nil {
		return fmt.Errorf("UpdatePasskeyCredential: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("UpdatePasskeyCredential: %w", err)
	}

	if n != 1 {
		return identity.ErrCredentials
	}

	return nil
}

// ChangePasskey renames or revokes an owned passkey; revocation preserves a usable recovery path.
func (s *Store) ChangePasskey(ctx context.Context, userID, id, name string, remove bool) error {
	name = strings.TrimSpace(name)
	if !remove && (name == "" || len(name) > 100) {
		return identity.ErrConflict
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ChangePasskey: %w", err)
	}
	defer tx.Rollback()

	var result sql.Result

	action := "auth.passkey.renamed"
	if remove {
		action = "auth.passkey.revoked"
		result, err = tx.ExecContext(ctx, `DELETE FROM passkeys WHERE user_id=? AND id=?`, userID, id)
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE passkeys SET name=? WHERE user_id=? AND id=?`, name, userID, id)
	}

	if err != nil {
		return fmt.Errorf("ChangePasskey: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("ChangePasskey: %w", err)
	}

	if n != 1 {
		return identity.ErrConflict
	}

	if remove {
		if err = requireRemainingAuthPath(ctx, tx, userID, "", ""); err != nil {
			return fmt.Errorf("ChangePasskey: %w", err)
		}
	}

	if err = appendAudit(ctx, tx, identity.AuditEvent{ActorID: userID, Action: action, TargetType: "passkey", TargetID: id}); err != nil {
		return fmt.Errorf("ChangePasskey: %w", err)
	}

	return credentialOperationError("commit ChangePasskey", tx.Commit())
}

// SaveWebAuthnCeremony stores a short-lived challenge bound to the initiating browser and session.
func (s *Store) SaveWebAuthnCeremony(ctx context.Context, token, userID, sessionID, purpose, rp string, data webauthn.SessionData) error {
	if (purpose != "register" && purpose != "login") || (purpose == "register" && (userID == "" || sessionID == "")) {
		return identity.ErrConflict
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("SaveWebAuthnCeremony: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("SaveWebAuthnCeremony: %w", err)
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx, `DELETE FROM webauthn_ceremonies WHERE expires_at<=unixepoch() OR token_hash=?`, tokenHash(token)); err != nil {
		return fmt.Errorf("SaveWebAuthnCeremony: %w", err)
	}

	// A browser restart replaces its previous registration challenge. A principal
	// may hold five challenges across sessions; registrations occupy at most
	// half the shared pool, preserving capacity for sign-in.
	if purpose == "register" {
		if _, err = tx.ExecContext(ctx, `DELETE FROM webauthn_ceremonies WHERE purpose='register' AND user_id=? AND session_id=? AND rp_id=?`, userID, sessionID, rp); err != nil {
			return fmt.Errorf("replace registration ceremony: %w", err)
		}

		var principal, registrations int
		if err = tx.QueryRowContext(ctx, `SELECT count(*),count(CASE WHEN user_id=? THEN 1 END) FROM webauthn_ceremonies WHERE purpose='register'`, userID).Scan(&registrations, &principal); err != nil {
			return fmt.Errorf("count registration ceremonies: %w", err)
		}

		if principal >= 5 || registrations >= 5000 {
			return identity.ErrConflict
		}
	}

	var n int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM webauthn_ceremonies`).Scan(&n); err != nil {
		return fmt.Errorf("SaveWebAuthnCeremony: %w", err)
	}

	if n >= 10000 {
		return identity.ErrConflict
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO webauthn_ceremonies VALUES(?,?,?,?,?,?,unixepoch()+300)`, tokenHash(token), userID, sessionID, purpose, rp, string(raw)); err != nil {
		return fmt.Errorf("SaveWebAuthnCeremony: %w", err)
	}

	return credentialOperationError("commit SaveWebAuthnCeremony", tx.Commit())
}

// TakeWebAuthnCeremony consumes a challenge exactly once, even on failed verification.
func (s *Store) TakeWebAuthnCeremony(ctx context.Context, token, userID, sessionID, purpose, rp string) (webauthn.SessionData, error) {
	var (
		data webauthn.SessionData
		raw  string
	)

	err := s.db.QueryRowContext(ctx, `DELETE FROM webauthn_ceremonies WHERE token_hash=? AND user_id=? AND session_id=? AND purpose=? AND rp_id=? AND expires_at>unixepoch() RETURNING session_json`, tokenHash(token), userID, sessionID, purpose, rp).Scan(&raw)
	if err != nil {
		return data, identity.ErrCredentials
	}

	err = json.Unmarshal([]byte(raw), &data)

	return data, credentialOperationError("decode ceremony", err)
}

// CredentialRateLimit reserves a bounded attempt before any credential verification.
func (s *Store) CredentialRateLimit(ctx context.Context, scope, key string, limit int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("CredentialRateLimit: %w", err)
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx, `DELETE FROM credential_rate_limits WHERE expires_at<=unixepoch()`); err != nil {
		return fmt.Errorf("CredentialRateLimit: %w", err)
	}

	var n int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM credential_rate_limits`).Scan(&n); err != nil {
		return fmt.Errorf("CredentialRateLimit: %w", err)
	}

	if n >= 10000 {
		return identity.ErrCredentials
	}

	var attempts int

	err = tx.QueryRowContext(ctx, `INSERT INTO credential_rate_limits VALUES(?,1,unixepoch()+900) ON CONFLICT(key_hash) DO UPDATE SET attempts=attempts+1 RETURNING attempts`, tokenHash(scope+":"+key)).Scan(&attempts)
	if err != nil {
		return fmt.Errorf("CredentialRateLimit: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("CredentialRateLimit: %w", err)
	}

	if attempts > limit {
		return identity.ErrCredentials
	}

	return nil
}

func credentialOperationError(operation string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", operation, err)
}
