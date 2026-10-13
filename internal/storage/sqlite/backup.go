package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/memoryspace"
	sqliteDriver "modernc.org/sqlite"
)

type onlineBackuper interface {
	NewBackup(string) (*sqliteDriver.Backup, error)
}

// CreateSnapshot creates an online SQLite backup and invokes finishSnapshot
// with its schema version and reachable assets while holding the writer slot.
// Asset ingestion and garbage collection therefore cannot change database
// reachability or remove bytes until the callback has copied every blob.
func (s *Store) CreateSnapshot(ctx context.Context, databaseDestination string, finishSnapshot func(int, []assets.Blob) error) error {
	return s.createSnapshot(ctx, databaseDestination, 0, 0, finishSnapshot)
}

// CreateBoundedSnapshot bounds both database pages and asset rows before copying.
// The callback and SQLite copy share the same writer fence as CreateSnapshot.
func (s *Store) CreateBoundedSnapshot(ctx context.Context, databaseDestination string, maxBytes int64, maxAssets int, finishSnapshot func(int, []assets.Blob) error) error {
	if maxBytes < 1 || maxAssets < 1 {
		return errors.New("snapshot byte and asset bounds must be positive")
	}

	return s.createSnapshot(ctx, databaseDestination, maxBytes, maxAssets, finishSnapshot)
}

func (s *Store) createSnapshot(ctx context.Context, databaseDestination string, maxBytes int64, maxAssets int, finishSnapshot func(int, []assets.Blob) error) error {
	ctx = capacityCleanup(ctx)

	if finishSnapshot == nil {
		return errors.New("snapshot asset callback is required")
	}

	connection, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire snapshot connection: %w", err)
	}
	defer connection.Close()

	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin coherent snapshot: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_, _ = connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()

	if err := snapshotPageBound(ctx, connection, maxBytes); err != nil {
		return err
	}

	var schemaVersion int
	if err := connection.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&schemaVersion); err != nil {
		return fmt.Errorf("read snapshot schema version: %w", err)
	}

	var inventoryBytes uint64
	if err := connection.QueryRowContext(ctx, `SELECT COALESCE(SUM(2048+4*(length(id)+length(content_hash)+length(media_type)+length(storage_key))),0) FROM assets WHERE storage_key IS NOT NULL`).Scan(&inventoryBytes); err != nil {
		return fmt.Errorf("inspect snapshot metadata capacity: %w", err)
	}

	releaseInventory, err := memoryspace.Shared.Reserve(inventoryBytes, 16<<20)
	if err != nil {
		return fmt.Errorf("reserve snapshot metadata: %w", err)
	}
	defer releaseInventory()

	rows, err := connection.QueryContext(ctx, `
		SELECT id, content_hash, media_type, size, storage_key, created_at, orphaned_at, phash
		FROM assets WHERE storage_key IS NOT NULL ORDER BY id`)
	if err != nil {
		return fmt.Errorf("query snapshot assets: %w", err)
	}

	blobs := []assets.Blob{}

	defer rows.Close()

	for rows.Next() {
		if maxAssets > 0 && len(blobs) >= maxAssets {
			return errors.New("SQLite snapshot exceeds its asset count bound")
		}

		blob, err := scanBlob(rows)
		if err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan snapshot asset: %w", err)
		}

		blobs = append(blobs, blob)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate snapshot assets: %w", err)
	}

	if err := rows.Close(); err != nil {
		return fmt.Errorf("close snapshot assets: %w", err)
	}

	if err := s.copySnapshotDatabase(ctx, databaseDestination, maxBytes); err != nil {
		return err
	}

	if err := finishSnapshot(schemaVersion, blobs); err != nil {
		return err
	}

	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("finish coherent snapshot: %w", err)
	}

	committed = true

	return nil
}

func snapshotPageBound(ctx context.Context, connection *writerConn, maxBytes int64) error {
	if maxBytes > 0 {
		var pages, size int64
		if err := connection.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
			return fmt.Errorf("read snapshot page count: %w", err)
		}

		if err := connection.QueryRowContext(ctx, "PRAGMA page_size").Scan(&size); err != nil {
			return fmt.Errorf("read snapshot page size: %w", err)
		}

		if pages < 0 || size < 1 || pages > maxBytes/size {
			return errors.New("SQLite snapshot exceeds its byte bound")
		}
	}

	return nil
}

func (s *Store) copySnapshotDatabase(ctx context.Context, destination string, maxBytes int64) error {
	backupDatabase, err := sql.Open("sqlite", s.dsn)
	if err != nil {
		return fmt.Errorf("open snapshot reader: %w", err)
	}
	defer backupDatabase.Close()

	backupConnection, err := backupDatabase.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire snapshot reader: %w", err)
	}
	defer backupConnection.Close()

	if err := backupConnection.Raw(func(raw any) error {
		backuper, ok := raw.(onlineBackuper)
		if !ok {
			return errors.New("SQLite driver does not support online backup")
		}

		backup, err := backuper.NewBackup(destination)
		if err != nil {
			return fmt.Errorf("start online SQLite backup: %w", err)
		}

		pages := int32(-1)
		if maxBytes > 0 {
			pages = 128
		}

		for {
			if err := ctx.Err(); err != nil {
				_ = backup.Finish()
				return err
			}

			more, err := backup.Step(pages)
			if err != nil {
				_ = backup.Finish()
				return fmt.Errorf("copy SQLite backup pages: %w", err)
			}

			if !more {
				break
			}
		}

		return backup.Finish()
	}); err != nil {
		return fmt.Errorf("create online SQLite backup: %w", err)
	}

	return nil
}
