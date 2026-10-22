package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

const tokenProviderAuthority = `(source_provider='' OR EXISTS(SELECT 1 FROM auth_providers WHERE id=source_provider AND enabled=1))`

// CreatePersonalToken creates a secret once, retaining only its SHA-256 digest.
func (s *Store) CreatePersonalToken(ctx context.Context, in identity.TokenRequest, policy identity.TokenPolicy) (identity.TokenGrant, error) {
	return s.createPersonalToken(ctx, in, policy, nil)
}

func (s *Store) createPersonalToken(ctx context.Context, in identity.TokenRequest, policy identity.TokenPolicy, bind func(*sql.Tx, string) error) (identity.TokenGrant, error) {
	p, ok := identity.FromContext(ctx)
	if !ok || strings.HasPrefix(p.CredentialID, "pat:") {
		return identity.TokenGrant{}, access.ErrForbidden
	}

	var err error

	in, err = s.validateTokenRequest(ctx, in, policy)
	if err != nil {
		return identity.TokenGrant{}, operationError("CreatePersonalToken", err)
	}

	secret, err := opaqueToken()
	if err != nil {
		return identity.TokenGrant{}, operationError("CreatePersonalToken", err)
	}

	id, err := opaqueToken()
	if err != nil {
		return identity.TokenGrant{}, operationError("CreatePersonalToken", err)
	}

	now := time.Now().Unix()

	grant := identity.TokenGrant{ID: "pat:" + id, Name: strings.TrimSpace(in.Name), CreatedAt: now, Scopes: in.Scopes, Libraries: in.Libraries, Secret: "savorum_pat_" + secret}
	if !in.Nonexpiring {
		grant.ExpiresAt = now + int64(in.Days)*86400
	}

	if grant.Libraries == nil {
		grant.Libraries = []string{}
	}

	scopeJSON, err := json.Marshal(grant.Scopes)
	if err != nil {
		return identity.TokenGrant{}, fmt.Errorf("encode token scopes: %w", err)
	}

	librariesJSON, err := json.Marshal(grant.Libraries)
	if err != nil {
		return identity.TokenGrant{}, fmt.Errorf("encode token libraries: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return identity.TokenGrant{}, fmt.Errorf("begin token creation: %w", err)
	}
	defer tx.Rollback()

	source := ""
	if err := tx.QueryRowContext(ctx, `SELECT id FROM auth_providers WHERE id=? AND enabled=1`, p.Method).Scan(&source); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return identity.TokenGrant{}, fmt.Errorf("check token provider: %w", err)
	}

	if p.Method != "" && p.Method != "local" && p.Method != "passkey" && p.Method != "recovery" && source == "" {
		return identity.TokenGrant{}, access.ErrForbidden
	}

	if source != "" {
		var enabled bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM identities WHERE user_id=? AND provider=? AND enabled=1)`, p.UserID, source).Scan(&enabled); err != nil {
			return identity.TokenGrant{}, fmt.Errorf("check token identity: %w", err)
		}

		if !enabled {
			return identity.TokenGrant{}, access.ErrForbidden
		}
	}

	if p.CredentialID != "" {
		if err := requireRecentSession(ctx, tx, p.UserID, p.CredentialID); err != nil {
			return identity.TokenGrant{}, err
		}
	}

	result, err := tx.ExecContext(ctx, `INSERT INTO personal_tokens(id,user_id,token_hash,name,created_at,expires_at,scopes,libraries,source_provider) SELECT ?,id,?,?,?,?,?,?,? FROM users WHERE id=? AND status='active'`, grant.ID, tokenHash(grant.Secret), grant.Name, now, grant.ExpiresAt, string(scopeJSON), string(librariesJSON), source, p.UserID)
	if err != nil {
		return identity.TokenGrant{}, fmt.Errorf("create token: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return identity.TokenGrant{}, fmt.Errorf("count token creation: %w", err)
	}

	if count != 1 {
		return identity.TokenGrant{}, access.ErrForbidden
	}

	if bind != nil {
		if err := bind(tx, grant.ID); err != nil {
			return identity.TokenGrant{}, err
		}
	}

	if err := appendAudit(ctx, tx, identity.AuditEvent{ActorID: p.UserID, Action: "token.created", TargetType: "token", TargetID: grant.ID}); err != nil {
		return identity.TokenGrant{}, operationError("CreatePersonalToken", err)
	}

	if err := tx.Commit(); err != nil {
		return identity.TokenGrant{}, fmt.Errorf("commit token creation: %w", err)
	}

	return grant, nil
}

// PersonalTokens lists safe token metadata for the current user only.
func (s *Store) PersonalTokens(ctx context.Context) ([]identity.PersonalToken, error) {
	p, ok := identity.FromContext(ctx)
	if !ok || strings.HasPrefix(p.CredentialID, "pat:") {
		return nil, access.ErrForbidden
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id,name,created_at,expires_at,last_used_at,revoked_at,scopes,libraries FROM personal_tokens WHERE user_id=? AND NOT EXISTS(SELECT 1 FROM assistant_connections c WHERE c.token_id=personal_tokens.id) ORDER BY created_at DESC,id`, p.UserID)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer rows.Close()

	out := []identity.PersonalToken{}

	for rows.Next() {
		var (
			t                 identity.PersonalToken
			scopes, libraries string
		)
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt, &t.RevokedAt, &scopes, &libraries); err != nil {
			return nil, fmt.Errorf("scan token: %w", err)
		}

		if err := decodeTokenRestrictions(&t, scopes, libraries); err != nil {
			return nil, operationError("PersonalTokens", err)
		}

		out = append(out, t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tokens: %w", err)
	}

	return out, nil
}

func decodeTokenRestrictions(t *identity.PersonalToken, scopes, libraries string) error {
	if err := json.Unmarshal([]byte(scopes), &t.Scopes); err != nil {
		return fmt.Errorf("decode token scopes: %w", err)
	}

	if err := json.Unmarshal([]byte(libraries), &t.Libraries); err != nil {
		return fmt.Errorf("decode token libraries: %w", err)
	}

	return nil
}

// ResolvePersonalToken authenticates a bearer credential without creating a session.
func (s *Store) ResolvePersonalToken(ctx context.Context, secret string) (identity.Principal, identity.PersonalToken, error) {
	var (
		p identity.Principal
		t identity.PersonalToken
	)
	if !strings.HasPrefix(secret, "savorum_pat_") || len(secret) != 76 {
		return p, t, identity.ErrCredentials
	}

	var scopes, libraries string

	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.display_name,t.id,t.scopes,t.libraries,t.expires_at,COALESCE((SELECT resource FROM assistant_connections WHERE token_id=t.id),'') FROM personal_tokens t JOIN users u ON u.id=t.user_id WHERE token_hash=? AND revoked_at=0 AND (expires_at=0 OR expires_at>unixepoch()) AND u.status='active' AND `+tokenProviderAuthority, tokenHash(secret)).Scan(&p.UserID, &p.DisplayName, &t.ID, &scopes, &libraries, &t.ExpiresAt, &t.Audience)
	if errors.Is(err, sql.ErrNoRows) {
		return p, t, identity.ErrCredentials
	}

	if err != nil {
		return p, t, fmt.Errorf("resolve token: %w", err)
	}

	if err := decodeTokenRestrictions(&t, scopes, libraries); err != nil {
		return p, t, operationError("ResolvePersonalToken", err)
	}

	p.CredentialID = t.ID

	p.Method = "token"
	if _, err := s.db.ExecContext(ctx, `UPDATE personal_tokens SET last_used_at=unixepoch() WHERE id=? AND last_used_at<unixepoch()-60`, t.ID); err != nil && !isCapacityError(err) {
		return p, t, fmt.Errorf("record token usage: %w", err)
	}

	return p, t, nil
}

// RevokePersonalToken invalidates a credential immediately and transactionally audits it.
func (s *Store) RevokePersonalToken(ctx context.Context, id string) error {
	p, ok := identity.FromContext(ctx)
	if !ok || strings.HasPrefix(p.CredentialID, "pat:") {
		return access.ErrForbidden
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin token revocation: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `UPDATE personal_tokens SET revoked_at=unixepoch() WHERE id=? AND user_id=? AND revoked_at=0`, id, p.UserID)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count token revocation: %w", err)
	}

	if count > 0 {
		if err := appendAudit(ctx, tx, identity.AuditEvent{ActorID: p.UserID, Action: "token.revoked", TargetType: "token", TargetID: id}); err != nil {
			return operationError("RevokePersonalToken", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit token revocation: %w", err)
	}

	return nil
}

// Recheck persisted token authority for delayed work as well as immediate requests.
func (*Store) authorizeTokenLibrary(ctx context.Context, queryer accessQueryer, p identity.Principal, id string, required access.Role) error {
	if !strings.HasPrefix(p.CredentialID, "pat:") {
		return nil
	}

	var scopes, libraries string

	err := queryer.QueryRowContext(ctx, `SELECT scopes,libraries FROM personal_tokens WHERE id=? AND user_id=? AND revoked_at=0 AND (expires_at=0 OR expires_at>unixepoch()) AND `+tokenProviderAuthority, p.CredentialID, p.UserID).Scan(&scopes, &libraries)
	if errors.Is(err, sql.ErrNoRows) {
		return access.ErrForbidden
	}

	if err != nil {
		return fmt.Errorf("check token authority: %w", err)
	}

	var t identity.PersonalToken
	if err := decodeTokenRestrictions(&t, scopes, libraries); err != nil {
		return operationError("authorizeTokenLibrary", err)
	}

	if len(t.Libraries) > 0 && !slices.Contains(t.Libraries, id) {
		return access.ErrForbidden
	}

	if required == access.Owner {
		return access.ErrForbidden
	}

	if required == access.Editor && !slices.Contains(t.Scopes, "recipes:write") && !slices.Contains(t.Scopes, "library:write") && !slices.Contains(t.Scopes, "imports:write") {
		return access.ErrForbidden
	}

	return nil
}

func (s *Store) validateTokenRequest(ctx context.Context, in identity.TokenRequest, policy identity.TokenPolicy) (identity.TokenRequest, error) {
	if policy.DefaultDays == 0 {
		policy.DefaultDays = 90
	}

	if policy.MaximumDays == 0 {
		policy.MaximumDays = 365
	}

	if in.Days == 0 {
		in.Days = policy.DefaultDays
	}

	if strings.TrimSpace(in.Name) == "" || len([]rune(in.Name)) > 100 || len(in.Scopes) == 0 || len(in.Scopes) > 5 || len(in.Libraries) > 100 {
		return identity.TokenRequest{}, identity.ErrConflict
	}

	if in.Nonexpiring && !policy.AllowNonexpiring || !in.Nonexpiring && (in.Days < 1 || in.Days > policy.MaximumDays) {
		return identity.TokenRequest{}, identity.ErrConflict
	}

	for _, scope := range in.Scopes {
		if !identity.ValidTokenScope(scope) {
			return identity.TokenRequest{}, identity.ErrConflict
		}
	}

	for _, id := range in.Libraries {
		if err := s.AuthorizeLibrary(ctx, id, access.Viewer); err != nil {
			return identity.TokenRequest{}, operationError("validateTokenRequest", err)
		}
	}

	libs, err := s.Libraries(ctx)
	if err != nil {
		return identity.TokenRequest{}, operationError("validateTokenRequest", err)
	}

	readable, writable := false, false

	for _, lib := range libs {
		if len(in.Libraries) > 0 && !slices.Contains(in.Libraries, lib.ID) {
			continue
		}

		readable = true
		writable = writable || lib.Role.Allows(access.Editor)
	}

	if !readable {
		return identity.TokenRequest{}, access.ErrForbidden
	}

	for _, scope := range in.Scopes {
		if strings.HasSuffix(scope, ":write") && !writable {
			return identity.TokenRequest{}, access.ErrForbidden
		}
	}

	return in, nil
}

// tokenScopes revalidates a credential; nil means an interactive/local principal.
func tokenScopes(ctx context.Context, queryer accessQueryer) ([]string, error) {
	p, ok := identity.FromContext(ctx)
	if !ok || !strings.HasPrefix(p.CredentialID, "pat:") {
		return nil, nil
	}

	var data string
	if err := queryer.QueryRowContext(ctx, `SELECT scopes FROM personal_tokens WHERE id=? AND user_id=? AND revoked_at=0 AND (expires_at=0 OR expires_at>unixepoch()) AND `+tokenProviderAuthority, p.CredentialID, p.UserID).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, access.ErrForbidden
		}

		return nil, operationError("tokenScopes", err)
	}

	var scopes []string
	if err := json.Unmarshal([]byte(data), &scopes); err != nil {
		return nil, operationError("tokenScopes", err)
	}

	return scopes, nil
}

func authorizeTokenCapability(ctx context.Context, queryer accessQueryer, capability string) error {
	scopes, err := tokenScopes(ctx, queryer)
	if err != nil {
		return operationError("authorizeTokenCapability", err)
	}

	if scopes != nil && !slices.Contains(scopes, capability) {
		return access.ErrForbidden
	}

	return nil
}
