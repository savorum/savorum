package sqlite

import (
	"context"
	"time"

	"github.com/savorum/savorum/internal/federation"
	"github.com/savorum/savorum/internal/identity"
)

func (s *Store) saveBrowserNativeGrant(ctx context.Context, code string, grant federation.NativeGrant, expires time.Time) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM native_sign_in_grants WHERE expires_at<=unixepoch()`); err != nil {
		return federationStorageError("expire native grants", err)
	}

	result, err := s.db.ExecContext(ctx, `INSERT INTO native_sign_in_grants(code_hash,user_id,provider,challenge,redirect_uri,expires_at,identity_id,source_session_id) SELECT ?,b.user_id,NULL,?,?,?,'',b.id FROM browser_sessions b JOIN users u ON u.id=b.user_id WHERE b.id=? AND b.user_id=? AND b.revoked_at IS NULL AND b.idle_expires>unixepoch() AND b.absolute_expires>unixepoch() AND u.status='active'`, tokenHash(code), grant.Challenge, grant.RedirectURI, expires.Unix(), grant.SourceSessionID, grant.UserID)
	if err != nil {
		return federationStorageError("save browser grant", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return federationStorageError("count browser grant", err)
	}

	if n != 1 {
		return identity.ErrCredentials
	}

	return nil
}
