package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"

	"github.com/savorum/savorum/internal/identity"
)

// localRecoveryAllowed implements provider-enforced recovery policy. A usable local identity
// permits local recovery; otherwise every enabled linked provider must explicitly opt in.
func localRecoveryAllowed(ctx context.Context, tx *sql.Tx, userID string) (bool, error) {
	var local, denied int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identities WHERE user_id=? AND provider='local' AND enabled=1 AND password_hash<>''`, userID).Scan(&local); err != nil {
		return false, fmt.Errorf("localRecoveryAllowed: %w", err)
	}

	if local > 0 {
		return true, nil
	}

	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identities i LEFT JOIN auth_providers p ON p.id=i.provider WHERE i.user_id=? AND i.provider<>'local' AND i.enabled=1 AND (p.id IS NULL OR p.allow_local_recovery=0)`, userID).Scan(&denied); err != nil {
		return false, fmt.Errorf("localRecoveryAllowed: %w", err)
	}

	return denied == 0, nil
}

// RecoveryStatus exposes only remaining count and policy, never plaintext or hashes.
func (s *Store) RecoveryStatus(ctx context.Context, userID string) (int, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, fmt.Errorf("RecoveryStatus: %w", err)
	}
	defer tx.Rollback()

	allowed, err := localRecoveryAllowed(ctx, tx, userID)
	if err != nil {
		return 0, false, fmt.Errorf("RecoveryStatus: %w", err)
	}

	var n int

	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM recovery_codes WHERE user_id=?`, userID).Scan(&n)

	return n, allowed, credentialOperationError("read recovery status", err)
}

// GenerateRecoveryCodes replaces the entire set atomically. 160 independent random bits per
// code make SHA-256 one-way storage resistant to offline guessing without password stretching.
func (s *Store) GenerateRecoveryCodes(ctx context.Context, userID string) ([]string, error) {
	codes := make([]string, 10)

	hashes := make([]string, 10)
	for i := range codes {
		b := make([]byte, 20)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("GenerateRecoveryCodes: %w", err)
		}

		raw := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)

		parts := []string{}
		for start := 0; start < len(raw); start += 4 {
			parts = append(parts, raw[start:start+4])
		}

		codes[i] = strings.Join(parts, "-")
		hashes[i] = recoveryHash(codes[i])
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("GenerateRecoveryCodes: %w", err)
	}
	defer tx.Rollback()

	allowed, err := localRecoveryAllowed(ctx, tx, userID)
	if err != nil {
		return nil, fmt.Errorf("GenerateRecoveryCodes: %w", err)
	}

	if !allowed {
		return nil, identity.ErrConflict
	}

	var active int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id=? AND status='active'`, userID).Scan(&active); err != nil {
		return nil, fmt.Errorf("GenerateRecoveryCodes: %w", err)
	}

	if active != 1 {
		return nil, identity.ErrCredentials
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id=?`, userID); err != nil {
		return nil, fmt.Errorf("GenerateRecoveryCodes: %w", err)
	}

	for _, hash := range hashes {
		if _, err = tx.ExecContext(ctx, `INSERT INTO recovery_codes(code_hash,user_id) VALUES(?,?)`, hash, userID); err != nil {
			return nil, fmt.Errorf("GenerateRecoveryCodes: %w", err)
		}
	}

	if err = appendAudit(ctx, tx, identity.AuditEvent{ActorID: userID, Action: "auth.recovery.regenerated", TargetType: "user", TargetID: userID}); err != nil {
		return nil, fmt.Errorf("GenerateRecoveryCodes: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("GenerateRecoveryCodes: %w", err)
	}

	return codes, nil
}

func recoveryHash(code string) string {
	return tokenHash("recovery:" + strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", "")))
}

// AuthenticateRecovery consumes a matching code before session creation. Every failure is
// indistinguishable, bounded per network client and account identifier, and audited.
func (s *Store) AuthenticateRecovery(ctx context.Context, login, code, client string) (identity.Principal, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	if len(login) > 128 || len(code) > 100 {
		return identity.Principal{}, identity.ErrCredentials
	}

	if err := s.CredentialRateLimit(ctx, "recovery-client", client, 10); err != nil {
		return identity.Principal{}, fmt.Errorf("AuthenticateRecovery: %w", err)
	}

	if err := s.CredentialRateLimit(ctx, "recovery-account", login, 5); err != nil {
		return identity.Principal{}, fmt.Errorf("AuthenticateRecovery: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return identity.Principal{}, fmt.Errorf("AuthenticateRecovery: %w", err)
	}
	defer tx.Rollback()

	var p identity.Principal

	err = tx.QueryRowContext(ctx, `SELECT id,display_name,instance_admin FROM users WHERE status='active' AND id IN(SELECT id FROM users WHERE id=? UNION SELECT user_id FROM identities WHERE lower(provider_subject)=? AND enabled=1) AND (SELECT count(DISTINCT id) FROM users WHERE status='active' AND id IN(SELECT id FROM users WHERE id=? UNION SELECT user_id FROM identities WHERE lower(provider_subject)=? AND enabled=1))=1`, login, login, login, login).Scan(&p.UserID, &p.DisplayName, &p.InstanceAdmin)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return identity.Principal{}, fmt.Errorf("resolve recovery account: %w", err)
	}

	valid := err == nil
	if valid {
		allowed, e := localRecoveryAllowed(ctx, tx, p.UserID)
		if e != nil {
			return identity.Principal{}, fmt.Errorf("AuthenticateRecovery: %w", e)
		}

		valid = allowed
	}

	if valid {
		result, e := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id=? AND code_hash=?`, p.UserID, recoveryHash(code))
		if e != nil {
			return identity.Principal{}, fmt.Errorf("AuthenticateRecovery: %w", e)
		}

		n, e := result.RowsAffected()
		if e != nil {
			return identity.Principal{}, fmt.Errorf("AuthenticateRecovery: %w", e)
		}

		valid = n == 1
	}

	if !valid {
		if err := tx.Rollback(); err != nil {
			return identity.Principal{}, fmt.Errorf("rollback failed recovery: %w", err)
		}

		if err := s.RecordAuthenticationFailure(ctx); err != nil {
			return identity.Principal{}, fmt.Errorf("AuthenticateRecovery: %w", err)
		}

		return identity.Principal{}, identity.ErrCredentials
	}

	event := identity.AuditEvent{Action: "auth.recovery.login", TargetType: "user", Outcome: "succeeded", ActorID: p.UserID, TargetID: p.UserID}
	if err = appendAudit(ctx, tx, event); err != nil {
		return identity.Principal{}, fmt.Errorf("AuthenticateRecovery: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return identity.Principal{}, fmt.Errorf("AuthenticateRecovery: %w", err)
	}

	if !valid {
		return identity.Principal{}, identity.ErrCredentials
	}

	p.Method = "recovery"

	return p, nil
}
