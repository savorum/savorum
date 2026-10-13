// Package sqlite is Savorum's persistence: the schema, its migrations, and the
// repository implementations the application services are constructed with. It
// depends on the domain packages and never the other way round.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/savorum/savorum/internal/access"

	"github.com/savorum/savorum/internal/assets"
)

var _ assets.Repository = (*Store)(nil)

// AuthorizeAssetIngest checks current editor authority before reading upload bytes.
func (s *Store) AuthorizeAssetIngest(ctx context.Context) error {
	return s.authorizeScope(ctx, access.Editor)
}

// RegisterStoredAsset records blob and clears its orphan status when referenced.
func (s *Store) RegisterStoredAsset(ctx context.Context, blob assets.Blob, orphanedAt time.Time) (assets.Blob, error) {
	if err := s.authorizeScope(ctx, access.Editor); err != nil {
		return assets.Blob{}, err
	}

	var phashArg any
	if blob.PHash != nil {
		phashArg = int64(*blob.PHash) //nolint:gosec // SQLite stores 64-bit integer values as signed int64, preserving bit pattern
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO assets (
			id, content_hash, media_type, size, storage_key, created_at, orphaned_at, phash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (content_hash) DO UPDATE SET
			storage_key = excluded.storage_key,
			phash = COALESCE(assets.phash, excluded.phash),
			orphaned_at = CASE
				WHEN EXISTS (
					SELECT 1 FROM recipe_revision_assets
					WHERE recipe_revision_assets.asset_id = assets.id
				) OR EXISTS (
					SELECT 1 FROM books WHERE books.cover_asset_id = assets.id
				) OR EXISTS (
					SELECT 1 FROM book_revisions WHERE book_revisions.cover_asset_id = assets.id
				) OR EXISTS (
					SELECT 1 FROM recipe_source_attachments WHERE recipe_source_attachments.asset_id = assets.id
				) OR EXISTS (
					SELECT 1 FROM recipe_source_import_baseline_assets WHERE recipe_source_import_baseline_assets.asset_id = assets.id
				) THEN NULL
				ELSE COALESCE(assets.orphaned_at, excluded.orphaned_at)
			END
		WHERE assets.media_type = excluded.media_type
		  AND assets.size = excluded.size
		  AND (assets.storage_key IS NULL OR assets.storage_key = excluded.storage_key)`,
		blob.ID, blob.ContentHash, blob.MediaType, blob.Size, blob.StorageKey,
		formatTime(blob.CreatedAt), formatTime(orphanedAt), phashArg,
	)
	if err != nil {
		return assets.Blob{}, fmt.Errorf("upsert stored asset: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return assets.Blob{}, fmt.Errorf("read stored asset upsert result: %w", err)
	}

	if affected != 1 {
		return assets.Blob{}, fmt.Errorf("asset metadata conflicts with its content hash: %w", assets.ErrInvalid)
	}

	if _, err := s.db.ExecContext(ctx, "INSERT INTO library_assets(library_id,asset_id) VALUES(?,?) ON CONFLICT DO NOTHING", scopeID(ctx), blob.ID); err != nil {
		return assets.Blob{}, fmt.Errorf("register library asset: %w", err)
	}

	return s.GetStoredAsset(ctx, blob.ID)
}

// ReuseStoredAsset authorizes ingestion without rewriting registered metadata.
func (s *Store) ReuseStoredAsset(ctx context.Context, id assets.ID) (assets.Blob, error) {
	if err := s.authorizeScope(ctx, access.Editor); err != nil {
		return assets.Blob{}, err
	}

	return s.GetStoredAsset(ctx, id)
}

// GetStoredAsset returns the stored blob identified by id.
func (s *Store) GetStoredAsset(ctx context.Context, id assets.ID) (assets.Blob, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return assets.Blob{}, err
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, content_hash, media_type, size, storage_key, created_at, orphaned_at, phash
		FROM assets WHERE id = ? AND storage_key IS NOT NULL AND EXISTS(SELECT 1 FROM library_assets WHERE asset_id=assets.id AND library_id=?)`, id, scopeID(ctx))

	blob, err := scanBlob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return assets.Blob{}, assets.ErrNotFound
	}

	if err != nil {
		return assets.Blob{}, fmt.Errorf("read stored asset: %w", err)
	}

	return blob, nil
}

// ListStoredAssets returns every blob with filesystem storage.
func (s *Store) ListStoredAssets(ctx context.Context) ([]assets.Blob, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, content_hash, media_type, size, storage_key, created_at, orphaned_at, phash
		FROM assets WHERE storage_key IS NOT NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query stored assets: %w", err)
	}
	defer rows.Close()

	result := []assets.Blob{}

	for rows.Next() {
		blob, err := scanBlob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan stored asset: %w", err)
		}

		result = append(result, blob)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stored assets: %w", err)
	}

	return result, nil
}

// ListOrphanedAssets returns unreferenced blobs orphaned no later than cutoff.
func (s *Store) ListOrphanedAssets(ctx context.Context, cutoff time.Time, limit int) ([]assets.Blob, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, content_hash, media_type, size, storage_key, created_at, orphaned_at, phash
		FROM assets
		WHERE orphaned_at IS NOT NULL
		  AND orphaned_at <= ?
		  AND NOT EXISTS (
			SELECT 1 FROM recipe_revision_assets
			WHERE recipe_revision_assets.asset_id = assets.id
		  )
		  AND NOT EXISTS (SELECT 1 FROM books WHERE books.cover_asset_id = assets.id)
		  AND NOT EXISTS (SELECT 1 FROM book_revisions WHERE book_revisions.cover_asset_id = assets.id)
		  AND NOT EXISTS (SELECT 1 FROM recipe_source_attachments WHERE recipe_source_attachments.asset_id = assets.id)
		  AND NOT EXISTS (SELECT 1 FROM recipe_source_import_baseline_assets WHERE recipe_source_import_baseline_assets.asset_id = assets.id)
		ORDER BY orphaned_at, id
		LIMIT ?`, formatTime(cutoff), limit)
	if err != nil {
		return nil, fmt.Errorf("query orphaned assets: %w", err)
	}
	defer rows.Close()

	result := []assets.Blob{}

	for rows.Next() {
		blob, err := scanBlob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan orphaned asset: %w", err)
		}

		result = append(result, blob)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orphaned assets: %w", err)
	}

	return result, nil
}

// DeleteOrphanedAsset deletes eligible metadata after deleteBlob removes its bytes.
func (s *Store) DeleteOrphanedAsset(ctx context.Context, id assets.ID, cutoff time.Time, deleteBlob func(assets.Blob) error) (bool, error) {
	ctx = capacityCleanup(ctx)

	if deleteBlob == nil {
		return false, errors.New("delete orphaned asset blob callback is required")
	}

	connection, err := s.db.Conn(ctx)
	if err != nil {
		return false, fmt.Errorf("acquire orphaned asset connection: %w", err)
	}
	defer connection.Close()

	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return false, fmt.Errorf("begin orphaned asset deletion: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_, _ = connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()

	row := connection.QueryRowContext(ctx, `
		SELECT id, content_hash, media_type, size, storage_key, created_at, orphaned_at, phash
		FROM assets
		WHERE id = ?
		  AND orphaned_at IS NOT NULL
		  AND orphaned_at <= ?
		  AND NOT EXISTS (
			SELECT 1 FROM recipe_revision_assets
			WHERE recipe_revision_assets.asset_id = assets.id
		  )
		  AND NOT EXISTS (SELECT 1 FROM books WHERE books.cover_asset_id = assets.id)
		  AND NOT EXISTS (SELECT 1 FROM book_revisions WHERE book_revisions.cover_asset_id = assets.id)
		  AND NOT EXISTS (SELECT 1 FROM recipe_source_attachments WHERE recipe_source_attachments.asset_id = assets.id)
		  AND NOT EXISTS (SELECT 1 FROM recipe_source_import_baseline_assets WHERE recipe_source_import_baseline_assets.asset_id = assets.id)`, id, formatTime(cutoff))

	blob, err := scanBlob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("recheck orphaned asset: %w", err)
	}

	if err := deleteBlob(blob); err != nil {
		return false, err
	}

	result, err := connection.ExecContext(ctx, "DELETE FROM assets WHERE id = ?", id)
	if err != nil {
		return false, fmt.Errorf("delete orphaned asset metadata: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read orphaned asset delete result: %w", err)
	}

	if affected != 1 {
		return false, errors.New("orphaned asset disappeared during deletion")
	}

	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return false, fmt.Errorf("commit orphaned asset deletion: %w", err)
	}

	committed = true

	return true, nil
}

func scanBlob(source scanner) (assets.Blob, error) {
	var (
		blob       assets.Blob
		storageKey sql.NullString
		createdAt  string
		orphanedAt sql.NullString
		phashVal   sql.NullInt64
	)
	if err := source.Scan(
		&blob.ID, &blob.ContentHash, &blob.MediaType, &blob.Size,
		&storageKey, &createdAt, &orphanedAt, &phashVal,
	); err != nil {
		return assets.Blob{}, err
	}

	if phashVal.Valid {
		val := uint64(phashVal.Int64) //nolint:gosec // SQLite signed int64 represents 64-bit uint64 pHash bit pattern
		blob.PHash = &val
	}

	if storageKey.Valid {
		blob.StorageKey = storageKey.String
	}

	var err error

	blob.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return assets.Blob{}, fmt.Errorf("parse asset created_at: %w", err)
	}

	if orphanedAt.Valid {
		value, parseErr := time.Parse(time.RFC3339Nano, orphanedAt.String)
		if parseErr != nil {
			return assets.Blob{}, fmt.Errorf("parse asset orphaned_at: %w", parseErr)
		}

		blob.OrphanedAt = &value
	}

	return blob, nil
}
