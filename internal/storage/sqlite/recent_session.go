package sqlite

import (
	"context"

	"github.com/savorum/savorum/internal/identity"
)

func requireRecentSession(ctx context.Context, q accessQueryer, userID, sessionID string) error {
	var count int

	err := q.QueryRowContext(ctx, `SELECT count(*) FROM browser_sessions b JOIN users u ON u.id=b.user_id WHERE b.id=? AND b.user_id=? AND b.revoked_at IS NULL AND b.idle_expires>unixepoch() AND b.absolute_expires>unixepoch() AND b.created_at>=unixepoch()-300 AND b.created_at<=unixepoch()+60 AND u.status='active'`, sessionID, userID).Scan(&count)
	if err != nil {
		return operationError("requireRecentSession", err)
	}

	if count != 1 {
		return identity.ErrCredentials
	}

	return nil
}

func authorizeLinkSession(ctx context.Context, q accessQueryer, userID string) error {
	if userID == "" {
		return nil
	}

	principal, ok := identity.FromContext(ctx)
	if !ok || principal.UserID != userID {
		return identity.ErrCredentials
	}

	return requireRecentSession(ctx, q, principal.UserID, principal.CredentialID)
}
