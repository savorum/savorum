package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/idempotency"
)

// recordCreation reserves a receipt in the mutation transaction before insertion.
func recordCreation(ctx context.Context, tx *sql.Tx, id string) error {
	r, ok := idempotency.FromContext(ctx)
	if !ok {
		return nil
	}

	var previous, digest string

	err := tx.QueryRowContext(ctx, `SELECT resource_id,digest FROM api_idempotency WHERE library_id=? AND actor_id=? AND operation=? AND key_hash=? AND expires_at>unixepoch()`, scopeID(ctx), currentUserID(ctx), r.Operation, r.Key).Scan(&previous, &digest)
	if err == nil {
		if digest != r.Digest {
			return idempotency.ErrConflict
		}

		return &idempotency.ReplayError{ID: previous}
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return operationError("recordCreation", err)
	}

	return recordIdempotency(ctx, tx, id)
}

// AuthorizeCreation rechecks creation authority before resolving a receipt.
func (s *Store) AuthorizeCreation(ctx context.Context, operation string) error {
	switch operation {
	case "create-webhook":
		return s.authorizeWebhook(ctx, s.db, scopeID(ctx))
	case "create-instance-webhook":
		return s.authorizeWebhook(ctx, s.db, "")
	case "create-calendar-feed", "create-saved-view", "create-recipe-cooking":
		return s.authorizeScope(ctx, access.Viewer)
	default:
		return s.authorizeScope(ctx, access.Editor)
	}
}
