package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/idempotency"
	"github.com/savorum/savorum/internal/shopping"
)

// ReplayShopping resolves a committed review before its sources are read again.
func (s *Store) ReplayShopping(ctx context.Context, listID, operation, digest string) (shopping.ShoppingList, bool, error) {
	if err := s.authorizeScope(ctx, access.Editor); err != nil {
		return shopping.ShoppingList{}, false, operationError("read shopping receipt", err)
	}

	var (
		previous string
		revision uint64
	)

	err := s.db.QueryRowContext(ctx, `SELECT digest,result_revision FROM shopping_operations WHERE library_id=? AND list_id=? AND operation_id=?`, scopeID(ctx), listID, shoppingOperationID(ctx, operation)).Scan(&previous, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return shopping.ShoppingList{}, false, nil
	}

	if err != nil {
		return shopping.ShoppingList{}, false, operationError("read shopping receipt", err)
	}

	if previous != digest {
		return shopping.ShoppingList{}, false, idempotency.ErrConflict
	}

	value, err := s.GetShoppingList(ctx, listID)
	value.AppliedRevision = revision

	return value, true, err
}

func shoppingOperationID(ctx context.Context, id string) string {
	if operation := shopping.RetryScope(ctx); operation != "" {
		return "retry:" + currentUserID(ctx) + ":" + operation + ":" + id
	}

	return id
}
