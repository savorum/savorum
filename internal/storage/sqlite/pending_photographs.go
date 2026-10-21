package sqlite

import (
	"context"
	"fmt"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
)

// ListPendingPhotographs lists recipes with deferred photographs across every
// library, so it refuses any request with a principal.
func (s *Store) ListPendingPhotographs(ctx context.Context, after library.EntryID, limit int) ([]library.PendingPhotographs, error) {
	if _, ok := identity.FromContext(ctx); ok {
		return nil, access.ErrForbidden
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT r.library_id, r.id
		FROM library_recipes r JOIN libraries l ON l.id = r.library_id
		WHERE r.deleted_at IS NULL AND l.deleted_at IS NULL AND r.id > ?
		  AND json_array_length(r.document_json, '$.extensions_raw."x-org.savorum.pending-images".deferred') > 0
		ORDER BY r.id
		LIMIT ?`, string(after), limit)
	if err != nil {
		return nil, fmt.Errorf("query recipes with pending photographs: %w", err)
	}
	defer rows.Close()

	pending := []library.PendingPhotographs{}

	for rows.Next() {
		var entry library.PendingPhotographs
		if err := rows.Scan(&entry.LibraryID, &entry.EntryID); err != nil {
			return nil, fmt.Errorf("scan recipe with pending photographs: %w", err)
		}

		pending = append(pending, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read recipes with pending photographs: %w", err)
	}

	return pending, nil
}
