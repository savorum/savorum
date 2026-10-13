package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/savorum/savorum/internal/access"

	"github.com/savorum/savorum/internal/library"
)

var _ library.SourceAttachmentStore = (*Store)(nil)

// insertImportSources is deliberately transaction-scoped. Source assets are
// ingested before ImportMany, but a recipe must never commit without the rows
// that keep its opted-in originals reachable.
func insertImportSources(ctx context.Context, tx *sql.Tx, mutation library.ImportMutation, entry library.Entry) error {
	for _, attachment := range mutation.SourceAttachments {
		if strings.TrimSpace(attachment.AssetID) == "" || strings.TrimSpace(attachment.Filename) == "" || strings.TrimSpace(attachment.MediaType) == "" || attachment.Size < 0 {
			return fmt.Errorf("invalid source attachment: %w", library.ErrInvalidOrganization)
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO recipe_source_attachments
				(entry_id, revision, asset_id, filename, media_type, size)
			SELECT ?, ?, a.id, ?, a.media_type, a.size
			FROM assets AS a
			WHERE a.id = ? AND a.storage_key IS NOT NULL AND a.media_type = ? AND a.size = ?
			ON CONFLICT (entry_id, revision, asset_id) DO UPDATE SET filename = excluded.filename`,
			string(entry.ID), entry.Revision, attachment.Filename, attachment.AssetID,
			attachment.MediaType, attachment.Size)
		if err != nil {
			return fmt.Errorf("attach imported source: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read imported source result: %w", err)
		}

		if affected != 1 {
			return fmt.Errorf("imported source references missing or mismatched asset %q: %w", attachment.AssetID, library.ErrNotFound)
		}

		if _, err := tx.ExecContext(ctx, "UPDATE assets SET orphaned_at = NULL WHERE id = ?", attachment.AssetID); err != nil {
			return fmt.Errorf("mark imported source reachable: %w", err)
		}
	}

	return nil
}

// AttachSource records an asset attachment for a source revision.
func (s *Store) AttachSource(ctx context.Context, attachment library.SourceAttachment) error {
	if err := s.scopedObject(ctx, "library_recipes", string(attachment.EntryID), access.Editor); err != nil {
		return err
	}

	if attachment.EntryID == "" || attachment.Revision == 0 || strings.TrimSpace(attachment.AssetID) == "" ||
		strings.TrimSpace(attachment.Filename) == "" || strings.TrimSpace(attachment.MediaType) == "" || attachment.Size < 0 {
		return fmt.Errorf("invalid source attachment: %w", library.ErrInvalidOrganization)
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO recipe_source_attachments
			(entry_id, revision, asset_id, filename, media_type, size)
		SELECT ?, ?, a.id, ?, a.media_type, a.size
		FROM assets AS a
		JOIN recipe_revisions AS r ON r.entry_id = ? AND r.revision = ?
		WHERE a.id = ? AND a.storage_key IS NOT NULL AND a.media_type = ? AND a.size = ?
		ON CONFLICT (entry_id, revision, asset_id) DO UPDATE SET
			filename = excluded.filename`,
		string(attachment.EntryID), attachment.Revision, attachment.Filename,
		string(attachment.EntryID), attachment.Revision, attachment.AssetID,
		attachment.MediaType, attachment.Size)
	if err != nil {
		return fmt.Errorf("attach recipe source: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read source attachment result: %w", err)
	}

	if affected != 1 {
		return fmt.Errorf("source attachment references missing or mismatched recipe revision or asset: %w", library.ErrNotFound)
	}

	if _, err := s.db.ExecContext(ctx, "UPDATE assets SET orphaned_at = NULL WHERE id = ?", attachment.AssetID); err != nil {
		return fmt.Errorf("mark attached source reachable: %w", err)
	}

	return nil
}

// ListSourceAttachments returns attachments for an entry revision.
func (s *Store) ListSourceAttachments(ctx context.Context, entryID library.EntryID, revision uint64) ([]library.SourceAttachment, error) {
	if err := s.scopedObject(ctx, "library_recipes", string(entryID), access.Viewer); err != nil {
		return nil, err
	}

	query := `
		SELECT entry_id, revision, asset_id, filename, media_type, size
		FROM recipe_source_attachments WHERE entry_id = ?`
	args := []any{string(entryID)}

	if revision != 0 {
		query += " AND revision = ?"

		args = append(args, revision)
	}

	query += " ORDER BY revision DESC, asset_id"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query recipe sources: %w", err)
	}
	defer rows.Close()

	result := []library.SourceAttachment{}

	for rows.Next() {
		var value library.SourceAttachment
		if err := rows.Scan(&value.EntryID, &value.Revision, &value.AssetID, &value.Filename, &value.MediaType, &value.Size); err != nil {
			return nil, fmt.Errorf("scan recipe source: %w", err)
		}

		result = append(result, value)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recipe sources: %w", err)
	}

	return result, nil
}

// GetSourceAttachment returns one attachment for an entry revision.
func (s *Store) GetSourceAttachment(ctx context.Context, entryID library.EntryID, revision uint64, assetID string) (library.SourceAttachment, error) {
	if err := s.scopedObject(ctx, "library_recipes", string(entryID), access.Viewer); err != nil {
		return library.SourceAttachment{}, err
	}

	query := `
		SELECT entry_id, revision, asset_id, filename, media_type, size
		FROM recipe_source_attachments WHERE entry_id = ? AND asset_id = ?`
	args := []any{string(entryID), assetID}

	if revision != 0 {
		query += " AND revision = ?"

		args = append(args, revision)
	}

	query += " ORDER BY revision DESC LIMIT 1"

	var value library.SourceAttachment

	err := s.db.QueryRowContext(ctx, query, args...).Scan(&value.EntryID, &value.Revision, &value.AssetID, &value.Filename, &value.MediaType, &value.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return library.SourceAttachment{}, library.ErrNotFound
	}

	if err != nil {
		return library.SourceAttachment{}, fmt.Errorf("read recipe source: %w", err)
	}

	return value, nil
}
