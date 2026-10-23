package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

type storedAssistantRequest struct {
	ClientID    string `json:"client_id"`
	Name        string `json:"name"`
	RedirectURI string `json:"redirect_uri"`
	Challenge   string `json:"challenge"`
	State       string `json:"state"`
	Resource    string `json:"resource"`
	Scope       string `json:"scope"`
	Kind        string `json:"kind"`
}

// RegisterAssistantClient stores validated public OAuth client metadata.
func (s *Store) RegisterAssistantClient(ctx context.Context, c identity.OAuthClient) (identity.OAuthClient, error) {
	id, err := opaqueToken()
	if err != nil {
		return c, operationError("assistant connection", err)
	}

	c.ClientID = id

	data, err := json.Marshal(c)
	if err != nil {
		return c, operationError("assistant connection", err)
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO assistant_clients(id,metadata,created_at) VALUES(?,?,unixepoch())`, id, string(data))

	return c, operationError("assistant connection", err)
}

// AssistantClient resolves client metadata.
func (s *Store) AssistantClient(ctx context.Context, id string) (identity.OAuthClient, error) {
	var c identity.OAuthClient

	if id == "savorum-device" {
		return identity.OAuthClient{ClientID: id, ClientName: "Assistant"}, nil
	}

	var data string

	err := s.db.QueryRowContext(ctx, `SELECT metadata FROM assistant_clients WHERE id=?`, id).Scan(&data)
	if err == nil {
		err = json.Unmarshal([]byte(data), &c)
	}

	return c, operationError("assistant connection", err)
}

// CreateAssistantRequest retains only the hash of the exchange secret.
func (s *Store) CreateAssistantRequest(ctx context.Context, in identity.AuthorizationRequest) (identity.AuthorizationRequest, string, error) {
	secret, err := opaqueToken()
	if err != nil {
		return in, "", operationError("assistant connection", err)
	}

	code, err := opaqueToken()
	if err != nil {
		return in, "", operationError("assistant connection", err)
	}

	in.ID = tokenHash(secret)
	in.UserCode = strings.ToUpper(code[:12])
	in.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
	// Internal fields are intentionally absent from the browser representation.
	data, err := json.Marshal(storedAssistantRequest{in.ClientID, in.Name, in.RedirectURI, in.Challenge, in.State, in.Resource, in.Scope, in.Kind})
	if err != nil {
		return in, "", operationError("assistant connection", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return in, "", operationError("assistant connection", err)
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx, `DELETE FROM assistant_requests WHERE expires_at<unixepoch()-86400`); err != nil {
		return in, "", operationError("assistant connection", err)
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO assistant_requests(id,user_code,request,expires_at) VALUES(?,?,?,?)`, in.ID, in.UserCode, string(data), in.ExpiresAt)
	if err == nil {
		err = tx.Commit()
	}

	return in, secret, operationError("assistant connection", err)
}

// AssistantRequest returns an unexpired approval request.
func (s *Store) AssistantRequest(ctx context.Context, code string) (identity.AuthorizationRequest, error) {
	return assistantRequest(ctx, s.db, strings.ToUpper(strings.ReplaceAll(code, "-", "")), false)
}

func assistantRequest(ctx context.Context, q accessQueryer, key string, exchange bool) (identity.AuthorizationRequest, error) {
	var (
		in   identity.AuthorizationRequest
		data string
	)

	column := "user_code"
	if exchange {
		column = "id"
	}

	err := q.QueryRowContext(ctx, `SELECT id,user_code,request,expires_at,status,COALESCE(token_id,'') FROM assistant_requests WHERE `+column+`=?`+assistantRequestExpiry(exchange), key).Scan(&in.ID, &in.UserCode, &data, &in.ExpiresAt, &in.Status, &in.TokenID)
	if err != nil {
		return in, operationError("assistant connection", err)
	}

	var stored storedAssistantRequest
	if err = json.Unmarshal([]byte(data), &stored); err != nil {
		return in, operationError("assistant connection", err)
	}

	in.ClientID = stored.ClientID
	in.Name = stored.Name
	in.RedirectURI = stored.RedirectURI
	in.Challenge = stored.Challenge
	in.State = stored.State
	in.Resource = stored.Resource
	in.Scope = stored.Scope
	in.Kind = stored.Kind

	return in, nil
}

func assistantRequestExpiry(exchange bool) string {
	if exchange {
		return ""
	}

	return " AND expires_at>unixepoch()"
}

// ApproveAssistantRequest requires the same recent browser authority as token creation.
func (s *Store) ApproveAssistantRequest(ctx context.Context, code, library string, write, remember, deny bool, policy identity.TokenPolicy) (identity.AuthorizationRequest, string, error) {
	in, err := s.AssistantRequest(ctx, code)
	if err != nil || in.Status != "pending" {
		return in, "", identity.ErrConflict
	}

	p, ok := identity.FromContext(ctx)
	if !ok || strings.HasPrefix(p.CredentialID, "pat:") {
		return in, "", access.ErrForbidden
	}

	if deny {
		result, err := s.db.ExecContext(ctx, `UPDATE assistant_requests SET status='denied' WHERE id=? AND status='pending' AND expires_at>unixepoch()`, in.ID)
		if err != nil {
			return in, "", operationError("assistant connection", err)
		}

		changed, err := result.RowsAffected()
		if err != nil {
			return in, "", operationError("assistant connection", err)
		}

		if changed != 1 {
			return in, "", identity.ErrConflict
		}

		return in, "", nil
	}

	requested := strings.Fields(in.Scope)
	if library == "" || !slices.Contains(requested, "read") || write && !slices.Contains(requested, "write") {
		return in, "", identity.ErrConflict
	}

	scopes := []string{"recipes:read", "library:read"}
	if write {
		scopes = append(scopes, "recipes:write", "library:write", "imports:write")
	}

	authCode := ""
	_, err = s.createPersonalToken(ctx, identity.TokenRequest{Name: in.Name, Days: 1, Scopes: scopes, Libraries: []string{library}}, policy, func(tx *sql.Tx, id string) error {
		var bindErr error

		authCode, bindErr = bindAssistantApproval(ctx, tx, in, id, remember, policy)

		return bindErr
	})

	return in, authCode, err
}

func bindAssistantApproval(ctx context.Context, tx *sql.Tx, in identity.AuthorizationRequest, id string, remember bool, policy identity.TokenPolicy) (string, error) {
	result, err := tx.ExecContext(ctx, `UPDATE assistant_requests SET status='approved',token_id=? WHERE id=? AND status='pending' AND expires_at>unixepoch()`, id, in.ID)
	if err != nil {
		return "", operationError("assistant connection", err)
	}

	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return "", identity.ErrConflict
	}

	days := policy.ConnectionDays()

	expiry := time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
	if !remember {
		expiry = time.Now().Add(15 * time.Minute).Unix()
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO assistant_connections(token_id,client_id,resource,expires_at,renewable) VALUES(?,?,?,?,?)`, id, in.ClientID, in.Resource, expiry, remember)
	if err != nil {
		return "", operationError("assistant connection", err)
	}

	_, err = tx.ExecContext(ctx, `UPDATE personal_tokens SET expires_at=unixepoch()+900 WHERE id=?`, id)
	if err != nil {
		return "", operationError("assistant connection", err)
	}
	// Authorization codes use a separate one-time secret, delivered only via the registered redirect.
	authCode := ""
	if in.Kind == "code" {
		authCode, err = opaqueToken()
		if err != nil {
			return "", operationError("assistant connection", err)
		}

		_, err = tx.ExecContext(ctx, `UPDATE assistant_requests SET id=? WHERE id=?`, tokenHash(authCode), in.ID)
		if err != nil {
			return "", operationError("assistant connection", err)
		}
	}

	return authCode, nil
}

// ExchangeAssistantCredential consumes codes and rotates refresh tokens atomically.
func (s *Store) ExchangeAssistantCredential(ctx context.Context, kind, secret, client, resource, redirect, verifier string) (identity.ConnectionCredentials, string, error) {
	var out identity.ConnectionCredentials

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, "server_error", operationError("assistant connection", err)
	}
	defer tx.Rollback()

	id, code, err := consumeAssistantCredential(ctx, tx, kind, secret, client, resource, redirect, verifier)
	if code != "" || err != nil {
		return out, code, err
	}

	var (
		scopeJSON, libraryJSON string
		renewable              bool
		remaining              int
	)

	err = tx.QueryRowContext(ctx, `SELECT t.scopes,t.libraries,c.renewable,MIN(900,c.expires_at-unixepoch()) FROM personal_tokens t JOIN assistant_connections c ON c.token_id=t.id JOIN users u ON u.id=t.user_id WHERE t.id=? AND t.revoked_at=0 AND c.expires_at>unixepoch() AND u.status='active' AND `+tokenProviderAuthority, id).Scan(&scopeJSON, &libraryJSON, &renewable, &remaining)
	if errors.Is(err, sql.ErrNoRows) {
		return out, "invalid_grant", nil
	}

	if err != nil {
		return out, "server_error", operationError("assistant connection", err)
	}

	var token identity.PersonalToken
	if err = decodeTokenRestrictions(&token, scopeJSON, libraryJSON); err != nil {
		return out, "server_error", operationError("assistant connection", err)
	}

	if kind == "refresh_token" && !renewable {
		return out, "invalid_grant", nil
	}

	accessSecret, err := opaqueToken()
	if err != nil {
		return out, "server_error", operationError("assistant connection", err)
	}

	out.AccessToken = "savorum_pat_" + accessSecret
	out.TokenType = "Bearer"
	out.ExpiresIn = remaining

	out.Scope = "read"
	if len(token.Scopes) > 2 {
		out.Scope = "read write"
	}

	out.LibraryID = token.Libraries[0]

	if renewable {
		refresh, err := opaqueToken()
		if err != nil {
			return out, "server_error", operationError("assistant connection", err)
		}

		out.RefreshToken = "savorum_refresh_" + refresh
		if _, err = tx.ExecContext(ctx, `INSERT INTO assistant_refresh_tokens(hash,token_id) VALUES(?,?)`, tokenHash(out.RefreshToken), id); err != nil {
			return out, "server_error", operationError("assistant connection", err)
		}
	}

	_, err = tx.ExecContext(ctx, `UPDATE personal_tokens SET token_hash=?,expires_at=MIN(unixepoch()+900,(SELECT expires_at FROM assistant_connections WHERE token_id=?)) WHERE id=?`, tokenHash(out.AccessToken), id, id)
	if err == nil {
		err = tx.Commit()
	}

	return out, "", operationError("assistant connection", err)
}

func consumeAssistantCredential(ctx context.Context, tx *sql.Tx, kind, secret, client, resource, redirect, verifier string) (string, string, error) {
	var err error

	id := ""

	if kind == "refresh_token" {
		var used bool

		err = tx.QueryRowContext(ctx, `SELECT r.token_id,r.used FROM assistant_refresh_tokens r JOIN assistant_connections c ON c.token_id=r.token_id WHERE r.hash=? AND c.client_id=? AND c.resource=?`, tokenHash(secret), client, resource).Scan(&id, &used)
		if errors.Is(err, sql.ErrNoRows) {
			return "", "invalid_grant", nil
		}

		if err != nil {
			return "", "server_error", operationError("assistant connection", err)
		}

		if used {
			if _, err = tx.ExecContext(ctx, `UPDATE personal_tokens SET revoked_at=unixepoch() WHERE id=?`, id); err != nil {
				return "", "server_error", operationError("assistant connection", err)
			}

			err = tx.Commit()

			return "", "invalid_grant", operationError("assistant connection", err)
		}

		result, err := tx.ExecContext(ctx, `UPDATE assistant_refresh_tokens SET used=1 WHERE hash=? AND used=0`, tokenHash(secret))
		if err != nil {
			return "", "server_error", operationError("assistant connection", err)
		}

		changed, err := result.RowsAffected()
		if err != nil {
			return "", "server_error", operationError("assistant connection", err)
		}

		if changed != 1 {
			return "", "invalid_grant", nil
		}
	} else {
		return consumeAssistantCode(ctx, tx, kind, secret, client, resource, redirect, verifier)
	}

	return id, "", nil
}

func consumeAssistantCode(ctx context.Context, tx *sql.Tx, kind, secret, client, resource, redirect, verifier string) (string, string, error) {
	var err error

	in, e := assistantRequest(ctx, tx, tokenHash(secret), true)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return "", "server_error", e
	}

	if e != nil || in.ClientID != client || in.Resource != resource {
		return "", "invalid_grant", nil
	}

	if in.ExpiresAt <= time.Now().Unix() {
		if kind == "urn:ietf:params:oauth:grant-type:device_code" {
			return "", "expired_token", nil
		}

		return "", "invalid_grant", nil
	}

	switch kind {
	case "authorization_code":
		digest := sha256.Sum256([]byte(verifier))
		if in.Kind != "code" || len(verifier) < 43 || len(verifier) > 128 || in.RedirectURI != redirect || in.Challenge != base64.RawURLEncoding.EncodeToString(digest[:]) {
			return "", "invalid_grant", nil
		}
	case "urn:ietf:params:oauth:grant-type:device_code":
		if in.Kind != "device" {
			return "", "invalid_grant", nil
		}

		var last, interval int64
		if err = tx.QueryRowContext(ctx, `SELECT last_poll,poll_interval FROM assistant_requests WHERE id=?`, in.ID).Scan(&last, &interval); err != nil {
			return "", "server_error", operationError("assistant connection", err)
		}

		if last > time.Now().Unix()-interval {
			if _, err = tx.ExecContext(ctx, `UPDATE assistant_requests SET last_poll=unixepoch(),poll_interval=poll_interval+5 WHERE id=?`, in.ID); err != nil {
				return "", "server_error", operationError("slow polling", err)
			}

			return "", "slow_down", operationError("slow polling", tx.Commit())
		}

		if _, err = tx.ExecContext(ctx, `UPDATE assistant_requests SET last_poll=unixepoch() WHERE id=?`, in.ID); err != nil {
			return "", "server_error", operationError("assistant connection", err)
		}
	default:
		return "", "unsupported_grant_type", nil
	}

	if in.Status != "approved" {
		err = tx.Commit()

		status := "authorization_pending"
		if in.Status == "denied" {
			status = "access_denied"
		}

		if in.Status == "consumed" {
			status = "invalid_grant"
		}

		return "", status, operationError("assistant connection", err)
	}

	id := in.TokenID

	result, err := tx.ExecContext(ctx, `UPDATE assistant_requests SET status='consumed' WHERE id=? AND status='approved'`, in.ID)
	if err != nil {
		return "", "server_error", operationError("assistant connection", err)
	}

	changed, err := result.RowsAffected()
	if err != nil {
		return "", "server_error", operationError("assistant connection", err)
	}

	if changed != 1 {
		return "", "invalid_grant", nil
	}

	return id, "", nil
}

// AssistantConnections lists current grants for the signed-in account.
func (s *Store) AssistantConnections(ctx context.Context) ([]identity.Connection, error) {
	p, ok := identity.FromContext(ctx)
	if !ok || strings.HasPrefix(p.CredentialID, "pat:") {
		return nil, access.ErrForbidden
	}

	rows, err := s.db.QueryContext(ctx, `SELECT t.id,t.name,t.created_at,c.expires_at,t.last_used_at,t.revoked_at,t.scopes,t.libraries,c.client_id FROM personal_tokens t JOIN assistant_connections c ON c.token_id=t.id WHERE t.user_id=? AND t.revoked_at=0 AND c.expires_at>unixepoch() ORDER BY t.created_at DESC`, p.UserID)
	if err != nil {
		return nil, operationError("assistant connection", err)
	}
	defer rows.Close()

	out := []identity.Connection{}

	for rows.Next() {
		var (
			c            identity.Connection
			scopes, libs string
		)
		if err = rows.Scan(&c.ID, &c.Name, &c.CreatedAt, &c.ExpiresAt, &c.LastUsedAt, &c.RevokedAt, &scopes, &libs, &c.ClientID); err != nil {
			return nil, operationError("assistant connection", err)
		}

		if err = decodeTokenRestrictions(&c.PersonalToken, scopes, libs); err != nil {
			return nil, operationError("assistant connection", err)
		}

		out = append(out, c)
	}

	return out, operationError("list assistant connections", rows.Err())
}
