package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/savorum/savorum/internal/event"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/pantry"
)

// PantryEntries returns only the selected library's hints.
func (s *Store) PantryEntries(ctx context.Context) ([]pantry.Entry, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, operationError("PantryEntries", err)
	}

	rows, err := s.db.QueryContext(ctx, `SELECT entry_json FROM pantry_entries WHERE library_id=? ORDER BY json_extract(entry_json,'$.name') COLLATE NOCASE,id LIMIT 2000`, scopeID(ctx))
	if err != nil {
		return nil, operationError("PantryEntries", err)
	}
	defer rows.Close()

	result := []pantry.Entry{}

	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, operationError("PantryEntries", err)
		}

		var entry pantry.Entry
		if err := json.Unmarshal([]byte(data), &entry); err != nil {
			return nil, operationError("PantryEntries", err)
		}

		result = append(result, entry)
	}

	return result, operationError("PantryEntries", rows.Err())
}

// SavePantryEntry checks and writes the revision within one transaction.
func (s *Store) SavePantryEntry(ctx context.Context, entry pantry.Entry, expected uint64) (pantry.Entry, error) {
	if err := pantry.Validate(entry); err != nil {
		return pantry.Entry{}, operationError("SavePantryEntry", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pantry.Entry{}, operationError("SavePantryEntry", err)
	}
	defer tx.Rollback()

	if err := s.authorizeScopeWith(ctx, tx, access.Editor); err != nil {
		return pantry.Entry{}, operationError("SavePantryEntry", err)
	}

	var current uint64

	err = tx.QueryRowContext(ctx, `SELECT revision FROM pantry_entries WHERE library_id=? AND id=?`, scopeID(ctx), entry.ID).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return pantry.Entry{}, operationError("SavePantryEntry", err)
	}

	if current != expected {
		return pantry.Entry{}, &library.PreconditionError{Expected: expected, Current: current}
	}

	if current == 0 {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pantry_entries WHERE library_id=?`, scopeID(ctx)).Scan(&count); err != nil {
			return pantry.Entry{}, operationError("SavePantryEntry", err)
		}

		if count >= 2000 {
			return pantry.Entry{}, pantry.ErrInvalid
		}
	}

	entry.Revision = current + 1
	entry.UpdatedAt = time.Now().UTC()
	entry.UpdatedBy = attributionActor(ctx)

	data, err := json.Marshal(entry)
	if err != nil {
		return pantry.Entry{}, operationError("SavePantryEntry", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO pantry_entries(library_id,id,revision,entry_json) VALUES(?,?,?,?) ON CONFLICT(library_id,id) DO UPDATE SET revision=excluded.revision,entry_json=excluded.entry_json`, scopeID(ctx), entry.ID, entry.Revision, string(data)); err != nil {
		return pantry.Entry{}, operationError("SavePantryEntry", err)
	}

	if err := appendDomainEvent(ctx, tx, "pantry.entry.updated", scopeID(ctx), "pantry_entry", entry.ID, event.Payload{Revision: entry.Revision}); err != nil {
		return pantry.Entry{}, operationError("SavePantryEntry", err)
	}

	if err := tx.Commit(); err != nil {
		return pantry.Entry{}, fmt.Errorf("save pantry entry: %w", err)
	}

	return entry, nil
}

// DeletePantryEntry cannot erase a newer household edit.
func (s *Store) DeletePantryEntry(ctx context.Context, id string, expected uint64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operationError("DeletePantryEntry", err)
	}
	defer tx.Rollback()

	if err := s.authorizeScopeWith(ctx, tx, access.Editor); err != nil {
		return operationError("DeletePantryEntry", err)
	}

	var current uint64

	err = tx.QueryRowContext(ctx, `SELECT revision FROM pantry_entries WHERE library_id=? AND id=?`, scopeID(ctx), id).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return library.ErrNotFound
	}

	if err != nil {
		return operationError("DeletePantryEntry", err)
	}

	if current != expected {
		return &library.PreconditionError{Expected: expected, Current: current}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM pantry_entries WHERE library_id=? AND id=?`, scopeID(ctx), id); err != nil {
		return operationError("DeletePantryEntry", err)
	}

	if err := appendDomainEvent(ctx, tx, "pantry.entry.deleted", scopeID(ctx), "pantry_entry", id, event.Payload{}); err != nil {
		return operationError("DeletePantryEntry", err)
	}

	return operationError("DeletePantryEntry", tx.Commit())
}
