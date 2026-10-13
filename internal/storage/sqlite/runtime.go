package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	sqliteDriver "modernc.org/sqlite"
)

// BusyTimeout bounds SQLite lock acquisition before it returns SQLITE_BUSY.
const BusyTimeout = 2 * time.Second

// IsBusy identifies transient SQLite writer contention, including extended codes.
func IsBusy(err error) bool {
	var failure *sqliteDriver.Error
	return errors.As(err, &failure) && failure.Code()&255 == 5
}

// runtimeDSN applies connection-scoped policy to every replacement connection,
// including the independent online-backup reader. Preflight uses the bare DSN.
func runtimeDSN(base string) string {
	values := url.Values{}
	values.Set("_txlock", "immediate")

	for _, pragma := range []string{"foreign_keys(1)", "busy_timeout(2000)", "synchronous(FULL)", "wal_autocheckpoint(1000)", "journal_size_limit(67108864)"} {
		values.Add("_pragma", pragma)
	}

	return base + "?" + values.Encode()
}

// CheckpointResult reports the page counts returned by SQLite's WAL checkpoint.
type CheckpointResult struct {
	Busy              int `json:"busy"`
	WALPages          int `json:"wal_pages"`
	CheckpointedPages int `json:"checkpointed_pages"`
}

// Checkpoint never waits for readers or removes history. SQLite retains any
// WAL pages still needed by another reader; a later checkpoint can reclaim them.
func (s *Store) Checkpoint(ctx context.Context) (CheckpointResult, error) {
	var result CheckpointResult

	err := s.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)").Scan(&result.Busy, &result.WALPages, &result.CheckpointedPages)
	if err != nil {
		return CheckpointResult{}, fmt.Errorf("checkpoint SQLite WAL: %w", err)
	}

	return result, nil
}

// IntegrityCheck checks SQLite pages and foreign keys. It diagnoses damage;
// recovery is a verified restore, never an automatic rewrite or history purge.
func (s *Store) IntegrityCheck(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return fmt.Errorf("start SQLite integrity check: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read SQLite integrity result: %w", err)
		}

		if value != "ok" {
			_ = rows.Close()
			return fmt.Errorf("SQLite integrity: %s", value)
		}
	}

	err = rows.Err()
	closeErr := rows.Close()

	if err != nil {
		return fmt.Errorf("iterate SQLite integrity results: %w", err)
	}

	if closeErr != nil {
		return fmt.Errorf("close SQLite integrity results: %w", closeErr)
	}

	rows, err = s.db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("start SQLite foreign-key check: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		var (
			table, parent string
			rowID         sql.NullInt64
			constraint    int
		)

		if err := rows.Scan(&table, &rowID, &parent, &constraint); err != nil {
			return fmt.Errorf("read SQLite foreign-key result: %w", err)
		}

		return fmt.Errorf("SQLite foreign-key integrity check failed: %s rows reference missing %s rows", table, parent)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate SQLite foreign-key results: %w", err)
	}

	return nil
}

// Shutdown is called after HTTP handlers and background jobs stop accepting
// work. Acquiring the sole connection drains the current transaction within
// the caller's deadline. On timeout the pool still closes to new work; owners
// must cancel their transaction contexts, which database/sql rolls back.
func (s *Store) Shutdown(ctx context.Context) error {
	connection, err := s.db.Conn(ctx)
	if err == nil {
		var result CheckpointResult

		err = connection.QueryRowContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)").Scan(&result.Busy, &result.WALPages, &result.CheckpointedPages)
		err = errors.Join(err, connection.Close())
	}

	return errors.Join(err, s.db.Close())
}
