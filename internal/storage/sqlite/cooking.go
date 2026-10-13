package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/savorum/savorum/internal/access"

	"github.com/savorum/savorum/internal/library"
)

var _ library.CookingRepository = (*Store)(nil)

// ListCookings reads a recipe's remembered cookings most recent first. The
// actual-record count is added to provenance-bearing imported baselines. The
// most recent date always comes from actual records, so an aggregate import
// never invents a cooking date.
func (s *Store) ListCookings(ctx context.Context, recipeID, userID string, options library.CookingListOptions) (library.CookingHistory, error) {
	if err := requirePersonalUser(ctx, userID); err != nil {
		return library.CookingHistory{}, err
	}

	if err := s.scopedObject(ctx, "library_recipes", recipeID, access.Viewer); err != nil {
		return library.CookingHistory{}, err
	}

	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM library_recipes WHERE id = ?)", recipeID).Scan(&exists); err != nil {
		return library.CookingHistory{}, fmt.Errorf("check recipe for cookings: %w", err)
	}

	if exists == 0 {
		return library.CookingHistory{}, library.ErrNotFound
	}

	history := library.CookingHistory{EntryID: library.EntryID(recipeID), Records: []library.CookingRecord{}}

	var lastCooked sql.NullString
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), MAX(cooked_on) FROM recipe_cookings
		WHERE entry_id = ? AND user_id = ?`, recipeID, userID).Scan(&history.TimesCooked, &lastCooked); err != nil {
		return library.CookingHistory{}, fmt.Errorf("count cookings: %w", err)
	}

	if lastCooked.Valid {
		value := library.Date(lastCooked.String)
		history.LastCooked = &value
	}

	var baseline uint64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(count), 0) FROM recipe_imported_cook_count_baselines WHERE entry_id = ? AND user_id = ?`, recipeID, userID).Scan(&baseline); err != nil {
		return library.CookingHistory{}, fmt.Errorf("count imported cook-count baselines: %w", err)
	}

	history.ImportedBaseline = baseline
	history.TimesCooked += baseline

	baselineRows, err := s.db.QueryContext(ctx, `
		SELECT count, source, source_key FROM recipe_imported_cook_count_baselines
		WHERE entry_id = ? AND user_id = ? ORDER BY source, source_key`, recipeID, userID)
	if err != nil {
		return library.CookingHistory{}, fmt.Errorf("list imported cook-count baselines: %w", err)
	}
	defer baselineRows.Close()

	for baselineRows.Next() {
		var item library.ImportedCookCountBaseline
		if err := baselineRows.Scan(&item.Count, &item.Source, &item.SourceKey); err != nil {
			return library.CookingHistory{}, fmt.Errorf("scan imported cook-count baseline: %w", err)
		}

		history.ImportedBaselines = append(history.ImportedBaselines, item)
	}

	if err := baselineRows.Err(); err != nil {
		return library.CookingHistory{}, fmt.Errorf("iterate imported cook-count baselines: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, cooked_on, cooked_time, note, scale, created_at
		FROM recipe_cookings
		WHERE entry_id = ? AND user_id = ?
		ORDER BY cooked_on DESC, cooked_time DESC, id DESC
		LIMIT ? OFFSET ?`, recipeID, userID, options.Limit, options.Offset)
	if err != nil {
		return library.CookingHistory{}, fmt.Errorf("query cookings: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		record, err := scanCooking(rows)
		if err != nil {
			return library.CookingHistory{}, err
		}

		history.Records = append(history.Records, record)
	}

	if err := rows.Err(); err != nil {
		return library.CookingHistory{}, fmt.Errorf("iterate cookings: %w", err)
	}

	return history, nil
}

// SetImportedCookCountBaseline records an imported aggregate cooking count.
func (s *Store) SetImportedCookCountBaseline(ctx context.Context, recipeID, userID string, baseline library.ImportedCookCountBaseline) error {
	if err := requirePersonalUser(ctx, userID); err != nil {
		return err
	}

	if err := s.scopedObject(ctx, "library_recipes", recipeID, access.Editor); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO recipe_imported_cook_count_baselines (entry_id, user_id, source, source_key, count)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (entry_id, user_id, source, source_key) DO UPDATE SET count = excluded.count`,
		recipeID, userID, baseline.Source, baseline.SourceKey, baseline.Count)
	if err != nil {
		return fmt.Errorf("upsert imported cook-count baseline: %w", err)
	}

	return nil
}

// RecordCooking adds a cooking record, returning an existing session record when retried.
func (s *Store) RecordCooking(ctx context.Context, recipeID, userID string, record library.CookingRecord) (library.CookingRecord, error) {
	if err := requirePersonalUser(ctx, userID); err != nil {
		return library.CookingRecord{}, err
	}

	if err := s.scopedObject(ctx, "library_recipes", recipeID, access.Viewer); err != nil {
		return library.CookingRecord{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return library.CookingRecord{}, err
	}
	defer tx.Rollback()

	if err := s.authorizeScopeWith(ctx, tx, access.Viewer); err != nil {
		return library.CookingRecord{}, err
	}

	if record.SessionID != nil {
		existing, readErr := scanCooking(tx.QueryRowContext(ctx, `SELECT id,session_id,cooked_on,cooked_time,note,scale,created_at FROM recipe_cookings WHERE entry_id=? AND user_id=? AND session_id=?`, recipeID, userID, *record.SessionID))
		if readErr == nil {
			if err := recordCreation(ctx, tx, existing.ID); err != nil {
				return library.CookingRecord{}, err
			}

			return existing, operationError("commit cooking", tx.Commit())
		}

		if !errors.Is(readErr, library.ErrNotFound) {
			return library.CookingRecord{}, readErr
		}
	}

	if err := recordCreation(ctx, tx, record.ID); err != nil {
		return library.CookingRecord{}, err
	}

	var deletedAt sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT deleted_at FROM library_recipes WHERE id = ?", recipeID).Scan(&deletedAt); errors.Is(err, sql.ErrNoRows) {
		return library.CookingRecord{}, library.ErrNotFound
	} else if err != nil {
		return library.CookingRecord{}, fmt.Errorf("read recipe for cooking: %w", err)
	}

	if deletedAt.Valid {
		return library.CookingRecord{}, library.ErrInvalidState
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO recipe_cookings (id, entry_id, user_id, session_id, cooked_on, cooked_time, note, scale, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, recipeID, userID, nullableSessionID(record.SessionID), record.CookedOn.String(), record.CookedTime, record.Note,
		nullableRational(record.Scale), formatTime(record.CreatedAt)); err != nil {
		if record.SessionID != nil {
			row := tx.QueryRowContext(ctx, `
				SELECT id, session_id, cooked_on, cooked_time, note, scale, created_at
				FROM recipe_cookings WHERE entry_id = ? AND user_id = ? AND session_id = ?`, recipeID, userID, *record.SessionID)
			if existing, readErr := scanCooking(row); readErr == nil {
				return existing, nil
			}
		}

		return library.CookingRecord{}, fmt.Errorf("insert cooking: %w", err)
	}

	return record, operationError("commit cooking", tx.Commit())
}

// ReplaceCooking updates a stored cooking record.
func (s *Store) ReplaceCooking(ctx context.Context, recipeID, userID string, record library.CookingRecord) (library.CookingRecord, error) {
	if err := requirePersonalUser(ctx, userID); err != nil {
		return library.CookingRecord{}, err
	}

	if err := s.scopedObject(ctx, "library_recipes", recipeID, access.Viewer); err != nil {
		return library.CookingRecord{}, err
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE recipe_cookings SET cooked_on = ?, cooked_time = ?, note = ?, scale = ?
		WHERE id = ? AND entry_id = ? AND user_id = ?`,
		record.CookedOn.String(), record.CookedTime, record.Note, nullableRational(record.Scale), record.ID, recipeID, userID)
	if err != nil {
		return library.CookingRecord{}, fmt.Errorf("update cooking: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return library.CookingRecord{}, fmt.Errorf("read cooking update result: %w", err)
	}

	if affected == 0 {
		return library.CookingRecord{}, library.ErrNotFound
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, session_id, cooked_on, cooked_time, note, scale, created_at FROM recipe_cookings WHERE id = ?`, record.ID)

	return scanCooking(row)
}

// DeleteCooking removes a cooking record belonging to the recipe and user.
func (s *Store) DeleteCooking(ctx context.Context, recipeID, userID, cookingID string) error {
	if err := requirePersonalUser(ctx, userID); err != nil {
		return err
	}

	if err := s.scopedObject(ctx, "library_recipes", recipeID, access.Viewer); err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, `
		DELETE FROM recipe_cookings WHERE id = ? AND entry_id = ? AND user_id = ?`, cookingID, recipeID, userID)
	if err != nil {
		return fmt.Errorf("delete cooking: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read cooking delete result: %w", err)
	}

	if affected == 0 {
		return library.ErrNotFound
	}

	return nil
}

type rowScanner interface {
	Scan(destination ...any) error
}

func scanCooking(row rowScanner) (library.CookingRecord, error) {
	var (
		record                         library.CookingRecord
		sessionID, cookedOn, createdAt sql.NullString
		scale                          sql.NullString
	)
	if err := row.Scan(&record.ID, &sessionID, &cookedOn, &record.CookedTime, &record.Note, &scale, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return library.CookingRecord{}, library.ErrNotFound
		}

		return library.CookingRecord{}, fmt.Errorf("scan cooking: %w", err)
	}

	if sessionID.Valid {
		record.SessionID = &sessionID.String
	}

	record.CookedOn = library.Date(cookedOn.String)
	record.Scale = rationalOrNil(scale)

	parsed, err := time.Parse(time.RFC3339Nano, createdAt.String)
	if err != nil {
		return library.CookingRecord{}, fmt.Errorf("parse cooking created_at: %w", err)
	}

	record.CreatedAt = parsed

	return record, nil
}
