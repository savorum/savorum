package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/savorum/savorum/internal/federation"
	"github.com/savorum/savorum/internal/identity"
)

// RegisterProviders synchronizes restart-bound provider availability without rebinding identities.
func (s *Store) RegisterProviders(ctx context.Context, providers []federation.Provider) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return federationStorageError("RegisterProviders", e)
	}
	defer tx.Rollback()

	disabled, e := providersBeingDisabled(ctx, tx, providers)
	if e != nil {
		return e
	}

	if _, e = tx.ExecContext(ctx, `UPDATE auth_providers SET enabled=0`); e != nil {
		return federationStorageError("RegisterProviders", e)
	}

	for _, p := range providers {
		issuer := ""
		if p.OIDC != nil {
			issuer = p.OIDC.Issuer
		}

		if p.Managed != nil {
			issuer = p.Managed.Issuer
		}

		var oldKind, oldIssuer string

		e = tx.QueryRowContext(ctx, `SELECT kind,issuer FROM auth_providers WHERE id=?`, p.ID).Scan(&oldKind, &oldIssuer)
		if e == nil && (oldKind != p.Kind || ((p.Kind == "oidc" || p.Kind == "managed") && oldIssuer != issuer)) {
			return fmt.Errorf("provider %s trust domain changed; use a new provider ID", p.ID)
		}

		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return federationStorageError("RegisterProviders", e)
		}

		if _, e = tx.ExecContext(ctx, `INSERT INTO auth_providers(id,kind,display_name,enabled,issuer,allow_local_recovery) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET display_name=excluded.display_name,enabled=excluded.enabled,allow_local_recovery=excluded.allow_local_recovery`, p.ID, p.Kind, p.DisplayName, !p.Disabled, issuer, p.AllowLocalRecovery); e != nil {
			return federationStorageError("RegisterProviders", e)
		}
	}

	if _, e = tx.ExecContext(ctx, `UPDATE browser_sessions SET revoked_at=unixepoch(),revocation_reason='provider-disabled' WHERE revoked_at IS NULL AND provider IN (SELECT id FROM auth_providers WHERE enabled=0)`); e != nil {
		return federationStorageError("RegisterProviders", e)
	}

	if _, e = tx.ExecContext(ctx, `UPDATE personal_tokens SET revoked_at=unixepoch() WHERE revoked_at=0 AND source_provider IN (SELECT id FROM auth_providers WHERE enabled=0)`); e != nil {
		return federationStorageError("RegisterProviders", e)
	}

	if _, e = tx.ExecContext(ctx, `DELETE FROM library_memberships WHERE source IN (SELECT 'mapping:'||m.id FROM provider_group_mappings m JOIN auth_providers p ON p.id=m.provider_id WHERE p.enabled=0) OR source IN (SELECT 'provider:'||id||':default' FROM auth_providers WHERE enabled=0)`); e != nil {
		return federationStorageError("RegisterProviders", e)
	}

	if e := auditDisabledProviders(ctx, tx, disabled); e != nil {
		return e
	}

	if e := tx.Commit(); e != nil {
		return fmt.Errorf("commit provider registration: %w", e)
	}

	return nil
}

// SaveFederationTransaction stores a hashed browser-bound short-lived authorization transaction.
func (s *Store) SaveFederationTransaction(ctx context.Context, token, provider, browser, payload string, expires time.Time) error {
	if _, e := s.db.ExecContext(ctx, `DELETE FROM federation_transactions WHERE expires_at<=unixepoch()`); e != nil {
		return federationStorageError("SaveFederationTransaction", e)
	}

	_, e := s.db.ExecContext(ctx, `INSERT INTO federation_transactions VALUES(?,?,?,?,?)`, tokenHash(token), provider, tokenHash(browser), payload, expires.Unix())

	return federationStorageError("SaveFederationTransaction", e)
}

// ConsumeFederationTransaction binds state to the initiating browser and atomically consumes it once.
func (s *Store) ConsumeFederationTransaction(ctx context.Context, token, provider, browser string) (string, error) {
	var payload string

	e := s.db.QueryRowContext(ctx, `DELETE FROM federation_transactions WHERE token_hash=? AND provider=? AND browser_hash=? AND expires_at>unixepoch() RETURNING payload`, tokenHash(token), provider, tokenHash(browser)).Scan(&payload)
	if errors.Is(e, sql.ErrNoRows) {
		return "", identity.ErrCredentials
	}

	return payload, federationStorageError("consume authorization transaction", e)
}

// SaveNativeGrant stores a hashed one-time sign-in result for a native app.
func (s *Store) SaveNativeGrant(ctx context.Context, code string, grant federation.NativeGrant, expires time.Time) error {
	if _, e := s.db.ExecContext(ctx, `DELETE FROM native_sign_in_grants WHERE expires_at<=unixepoch()`); e != nil {
		return federationStorageError("SaveNativeGrant", e)
	}

	result, e := s.db.ExecContext(ctx, `INSERT INTO native_sign_in_grants(code_hash,user_id,provider,challenge,redirect_uri,expires_at,identity_id) SELECT ?,i.user_id,i.provider,?,?,?,i.id FROM identities i JOIN users u ON u.id=i.user_id JOIN auth_providers a ON a.id=i.provider WHERE i.user_id=? AND i.provider=? AND i.provider_subject=? AND i.enabled=1 AND i.id<>'' AND u.status='active' AND a.enabled=1`, tokenHash(code), grant.Challenge, grant.RedirectURI, expires.Unix(), grant.UserID, grant.Provider, grant.Subject)
	if e != nil {
		return federationStorageError("SaveNativeGrant", e)
	}

	n, e := result.RowsAffected()
	if e != nil {
		return federationStorageError("SaveNativeGrant", e)
	}

	if n != 1 {
		return identity.ErrCredentials
	}

	return nil
}

// RedeemNativeGrant consumes a code on its first use, expired or not, and
// returns its account only while that account and provider remain active.
func (s *Store) RedeemNativeGrant(ctx context.Context, code string) (federation.NativeGrant, identity.Principal, error) {
	var (
		grant      federation.NativeGrant
		p          identity.Principal
		expires    int64
		identityID string
	)

	e := s.db.QueryRowContext(ctx, `DELETE FROM native_sign_in_grants WHERE code_hash=? RETURNING user_id,provider,challenge,redirect_uri,expires_at,identity_id`, tokenHash(code)).Scan(&grant.UserID, &grant.Provider, &grant.Challenge, &grant.RedirectURI, &expires, &identityID)
	if errors.Is(e, sql.ErrNoRows) {
		return grant, p, identity.ErrCredentials
	}

	if e != nil {
		return grant, p, federationStorageError("RedeemNativeGrant", e)
	}

	grant.IdentityID = identityID

	if expires <= time.Now().Unix() {
		return grant, p, identity.ErrCredentials
	}

	e = s.db.QueryRowContext(ctx, `SELECT u.id,u.display_name,u.instance_admin FROM users u JOIN auth_providers a ON a.id=? AND a.enabled=1 JOIN identities i ON i.user_id=u.id AND i.provider=a.id AND i.id=? AND i.enabled=1 WHERE u.id=? AND u.status='active'`, grant.Provider, identityID, grant.UserID).Scan(&p.UserID, &p.DisplayName, &p.InstanceAdmin)
	if errors.Is(e, sql.ErrNoRows) {
		return grant, p, identity.ErrCredentials
	}

	return grant, p, federationStorageError("RedeemNativeGrant", e)
}

// SaveNativeLinkTicket stores a hashed ticket for one native link ceremony.
func (s *Store) SaveNativeLinkTicket(ctx context.Context, ticket string, t federation.NativeLinkTicket, expires time.Time) error {
	if _, e := s.db.ExecContext(ctx, `DELETE FROM native_link_tickets WHERE expires_at<=unixepoch()`); e != nil {
		return federationStorageError("SaveNativeLinkTicket", e)
	}

	_, e := s.db.ExecContext(ctx, `INSERT INTO native_link_tickets VALUES(?,?,?,?,?,?)`, tokenHash(ticket), t.UserID, t.SessionID, t.Provider, t.Challenge, expires.Unix())

	return federationStorageError("SaveNativeLinkTicket", e)
}

// ConsumeNativeLinkTicket consumes a ticket on its first use, expired or not.
func (s *Store) ConsumeNativeLinkTicket(ctx context.Context, ticket string) (federation.NativeLinkTicket, error) {
	var (
		t       federation.NativeLinkTicket
		expires int64
	)

	e := s.db.QueryRowContext(ctx, `DELETE FROM native_link_tickets WHERE ticket_hash=? RETURNING user_id,session_id,provider,challenge,expires_at`, tokenHash(ticket)).Scan(&t.UserID, &t.SessionID, &t.Provider, &t.Challenge, &expires)
	if errors.Is(e, sql.ErrNoRows) || (e == nil && expires <= time.Now().Unix()) {
		return t, identity.ErrCredentials
	}

	return t, federationStorageError("ConsumeNativeLinkTicket", e)
}

type nativeLinkIdentity struct {
	Subject     string   `json:"subject"`
	DisplayName string   `json:"display_name"`
	Email       string   `json:"email"`
	Groups      []string `json:"groups"`
}

// SaveNativeLinkGrant stores a hashed one-time provider identity awaiting the app's confirmation.
func (s *Store) SaveNativeLinkGrant(ctx context.Context, code string, g federation.NativeLinkGrant, expires time.Time) error {
	if _, e := s.db.ExecContext(ctx, `DELETE FROM native_link_grants WHERE expires_at<=unixepoch()`); e != nil {
		return federationStorageError("SaveNativeLinkGrant", e)
	}

	held, e := json.Marshal(nativeLinkIdentity{g.Subject, g.DisplayName, g.Email, g.Groups})
	if e != nil {
		return federationStorageError("SaveNativeLinkGrant", e)
	}

	_, e = s.db.ExecContext(ctx, `INSERT INTO native_link_grants VALUES(?,?,?,?,?,?,?,?)`, tokenHash(code), g.UserID, g.SessionID, g.Provider, g.Challenge, g.RedirectURI, string(held), expires.Unix())

	return federationStorageError("SaveNativeLinkGrant", e)
}

// RedeemNativeLinkGrant consumes a code on its first use, expired or not.
func (s *Store) RedeemNativeLinkGrant(ctx context.Context, code string) (federation.NativeLinkGrant, error) {
	var (
		g       federation.NativeLinkGrant
		held    string
		expires int64
	)

	e := s.db.QueryRowContext(ctx, `DELETE FROM native_link_grants WHERE code_hash=? RETURNING user_id,session_id,provider,challenge,redirect_uri,identity,expires_at`, tokenHash(code)).Scan(&g.UserID, &g.SessionID, &g.Provider, &g.Challenge, &g.RedirectURI, &held, &expires)
	if errors.Is(e, sql.ErrNoRows) || (e == nil && expires <= time.Now().Unix()) {
		return federation.NativeLinkGrant{}, identity.ErrCredentials
	}

	if e != nil {
		return g, federationStorageError("RedeemNativeLinkGrant", e)
	}

	var v nativeLinkIdentity
	if e = json.Unmarshal([]byte(held), &v); e != nil {
		return federation.NativeLinkGrant{}, federationStorageError("RedeemNativeLinkGrant", e)
	}

	g.Subject, g.DisplayName, g.Email, g.Groups = v.Subject, v.DisplayName, v.Email, v.Groups

	return g, nil
}

// ClaimFederationAssertion atomically prevents reuse of a validated assertion ID.
func (s *Store) ClaimFederationAssertion(ctx context.Context, provider, id string, expires time.Time) error {
	if id == "" {
		return identity.ErrCredentials
	}

	if _, e := s.db.ExecContext(ctx, `DELETE FROM federation_replays WHERE expires_at<=unixepoch()`); e != nil {
		return federationStorageError("ClaimFederationAssertion", e)
	}

	_, e := s.db.ExecContext(ctx, `INSERT INTO federation_replays VALUES(?,?,?)`, provider, id, expires.Unix())

	return federationStorageError("ClaimFederationAssertion", e)
}

// FederationLinkSessionActive rechecks the initiating session at callback, including cross-site SAML POST.
func (s *Store) FederationLinkSessionActive(ctx context.Context, userID, sessionID string) bool {
	var count int

	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM browser_sessions b JOIN users u ON u.id=b.user_id WHERE b.id=? AND b.user_id=? AND b.revoked_at IS NULL AND b.idle_expires>unixepoch() AND b.absolute_expires>unixepoch() AND u.status='active' AND b.created_at>=unixepoch()-300 AND b.created_at<=unixepoch()+60`, sessionID, userID).Scan(&count)

	return err == nil && count == 1
}

func federationStorageError(operation string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", operation, err)
}

// BindFederationIssuer pins the first validated SAML metadata issuer across process restarts.
func (s *Store) BindFederationIssuer(ctx context.Context, provider, issuer string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE auth_providers SET issuer=? WHERE id=? AND enabled=1 AND (issuer='' OR issuer=?)`, issuer, provider, issuer)
	if err != nil {
		return fmt.Errorf("bind provider issuer: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count provider issuer binding: %w", err)
	}

	if count != 1 {
		return identity.ErrConflict
	}

	return nil
}

func providersBeingDisabled(ctx context.Context, tx *sql.Tx, providers []federation.Provider) ([]string, error) {
	enabled := map[string]bool{}
	for _, p := range providers {
		enabled[p.ID] = !p.Disabled
	}

	rows, err := tx.QueryContext(ctx, `SELECT id FROM auth_providers WHERE enabled=1 ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read enabled providers: %w", err)
	}
	defer rows.Close()

	disabled := []string{}

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan enabled provider: %w", err)
		}

		if !enabled[id] {
			disabled = append(disabled, id)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate enabled providers: %w", err)
	}

	return disabled, nil
}

func auditDisabledProviders(ctx context.Context, tx *sql.Tx, providers []string) error {
	if len(providers) == 0 {
		return nil
	}

	correlation, err := opaqueToken()
	if err != nil {
		return err
	}

	ctx = identity.WithCorrelation(identity.WithPrincipal(ctx, identity.Principal{System: true}), correlation)
	for _, id := range providers {
		if err := appendAudit(ctx, tx, identity.AuditEvent{ActorID: "system", Action: "auth.provider.disabled", TargetType: "provider", TargetID: id}); err != nil {
			return err
		}
	}

	return nil
}
