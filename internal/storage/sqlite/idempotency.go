package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/savorum/savorum/internal/idempotency"
)

// FindIdempotency never exposes another principal's request or result.
func (s *Store) FindIdempotency(ctx context.Context, r idempotency.Receipt) (string, error) {
	var id, digest string

	err := s.db.QueryRowContext(ctx, `SELECT resource_id,digest FROM api_idempotency WHERE library_id=? AND actor_id=? AND operation=? AND key_hash=? AND expires_at>unixepoch()`, scopeID(ctx), currentUserID(ctx), r.Operation, r.Key).Scan(&id, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	if err != nil {
		return "", operationError("FindIdempotency", err)
	}

	if digest != r.Digest {
		return "", idempotency.ErrConflict
	}

	return id, nil
}

func recordIdempotency(ctx context.Context, tx *sql.Tx, id string) error {
	r, ok := idempotency.FromContext(ctx)
	if !ok {
		return nil
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM api_idempotency WHERE expires_at<=unixepoch()`); err != nil {
		return operationError("recordIdempotency", err)
	}

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM api_idempotency WHERE library_id=? AND actor_id=?`, scopeID(ctx), currentUserID(ctx)).Scan(&count); err != nil {
		return operationError("recordIdempotency", err)
	}

	if count >= 10000 {
		return idempotency.ErrCapacity
	}

	_, err := tx.ExecContext(ctx, `INSERT INTO api_idempotency(library_id,actor_id,operation,key_hash,digest,resource_id,expires_at) VALUES(?,?,?,?,?,?,unixepoch()+86400)`, scopeID(ctx), currentUserID(ctx), r.Operation, r.Key, r.Digest, id)

	return operationError("recordIdempotency", err)
}
