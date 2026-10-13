package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/savorum/savorum/internal/preferences"
)

// LoadPreferences reads one application user's explicit preference overrides.
func (s *Store) LoadPreferences(ctx context.Context, userID string) (preferences.Record, error) {
	var (
		record preferences.Record
		data   []byte
	)

	err := s.db.QueryRowContext(ctx, `SELECT schema_version, revision, preferences_json FROM user_preferences WHERE user_id = ?`, userID).Scan(&record.Version, &record.Revision, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return preferences.Record{Version: 1}, nil
	}

	if err != nil {
		return record, fmt.Errorf("load preferences: %w", err)
	}

	if err = json.Unmarshal(data, &record.Overrides); err != nil {
		return record, fmt.Errorf("decode preferences: %w", err)
	}

	return record, nil
}

// ReplacePreferences atomically checks and advances the personal revision.
func (s *Store) ReplacePreferences(ctx context.Context, userID string, revision uint64, value preferences.Values) (preferences.Record, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return preferences.Record{}, fmt.Errorf("encode preferences: %w", err)
	}

	var result sql.Result
	if revision == 0 {
		result, err = s.db.ExecContext(ctx, `INSERT INTO user_preferences(user_id,schema_version,revision,preferences_json) VALUES (?,1,1,?) ON CONFLICT(user_id) DO NOTHING`, userID, string(data))
	} else {
		result, err = s.db.ExecContext(ctx, `UPDATE user_preferences SET revision=revision+1, preferences_json=? WHERE user_id=? AND revision=?`, string(data), userID, revision)
	}

	if err != nil {
		return preferences.Record{}, fmt.Errorf("replace preferences: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return preferences.Record{}, fmt.Errorf("count preferences: %w", err)
	}

	if count != 1 {
		return preferences.Record{}, preferences.ErrConflict
	}

	return s.LoadPreferences(ctx, userID)
}
