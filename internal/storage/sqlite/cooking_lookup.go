package sqlite

import (
	"context"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/library"
)

// GetCooking reads one record belonging to the current reader.
func (s *Store) GetCooking(ctx context.Context, entryID, id string) (library.CookingRecord, error) {
	if err := s.scopedObject(ctx, "library_recipes", entryID, access.Viewer); err != nil {
		return library.CookingRecord{}, err
	}

	return scanCooking(s.db.QueryRowContext(ctx, `SELECT id,session_id,cooked_on,cooked_time,note,scale,created_at FROM recipe_cookings WHERE entry_id=? AND user_id=? AND id=?`, entryID, currentUserID(ctx), id))
}
