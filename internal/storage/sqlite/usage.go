package sqlite

import (
	"context"
	"fmt"

	"github.com/savorum/savorum/internal/diskspace"
)

// StorageUsage reports database storage consumed by library content and metadata.
type StorageUsage struct {
	StoredAssets            int64 `json:"stored_assets"`
	Recipes                 int64 `json:"recipes"`
	RecipeRevisions         int64 `json:"recipe_revisions"`
	BookRevisions           int64 `json:"book_revisions"`
	CurrentRecipeJSONBytes  int64 `json:"current_recipe_json_bytes"`
	RecipeRevisionJSONBytes int64 `json:"recipe_revision_json_bytes"`
	BookRevisionJSONBytes   int64 `json:"book_revision_json_bytes"`
	DatabaseBytes           int64 `json:"database_bytes"`
	FreeDatabaseBytes       int64 `json:"free_database_bytes"`
	CurrentAssetBytes       int64 `json:"current_asset_bytes"`
	RetainedOnlyAssetBytes  int64 `json:"retained_only_asset_bytes"`
	OrphanAssetBytes        int64 `json:"orphan_asset_bytes"`
}

// StorageUsage reports stored bytes, not estimates based on recipe counts.
// Asset categories are mutually exclusive and content hashes are counted once.
func (s *Store) StorageUsage(ctx context.Context) (StorageUsage, error) {
	var u StorageUsage

	err := s.db.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM assets WHERE storage_key IS NOT NULL),
 (SELECT count(*) FROM library_recipes),
 (SELECT count(*) FROM recipe_revisions),
 (SELECT count(*) FROM book_revisions),
 (SELECT COALESCE(sum(length(CAST(document_json AS BLOB))),0) FROM library_recipes),
 (SELECT COALESCE(sum(length(CAST(document_json AS BLOB))),0) FROM recipe_revisions),
 (SELECT COALESCE(sum(length(CAST(book_json AS BLOB))),0) FROM book_revisions)`).Scan(&u.StoredAssets, &u.Recipes, &u.RecipeRevisions, &u.BookRevisions, &u.CurrentRecipeJSONBytes, &u.RecipeRevisionJSONBytes, &u.BookRevisionJSONBytes)
	if err != nil {
		return u, fmt.Errorf("read storage usage: %w", err)
	}

	var pages, pageSize, free int64
	for query, dest := range map[string]*int64{"PRAGMA page_count": &pages, "PRAGMA page_size": &pageSize, "PRAGMA freelist_count": &free} {
		if err := s.db.QueryRowContext(ctx, query).Scan(dest); err != nil {
			return u, fmt.Errorf("read storage usage: %w", err)
		}
	}

	u.DatabaseBytes = pages * pageSize
	u.FreeDatabaseBytes = free * pageSize

	err = s.db.QueryRowContext(ctx, `WITH current_refs AS (
 SELECT a.asset_id FROM recipe_revision_assets a JOIN library_recipes r ON r.id=a.entry_id AND r.revision=a.revision WHERE r.deleted_at IS NULL
 UNION SELECT cover_asset_id FROM books WHERE deleted_at IS NULL AND cover_asset_id IS NOT NULL
 ), retained_refs AS (
 SELECT asset_id FROM recipe_revision_assets UNION SELECT cover_asset_id FROM books
 UNION SELECT cover_asset_id FROM book_revisions UNION SELECT asset_id FROM recipe_source_attachments
 UNION SELECT asset_id FROM recipe_source_import_baseline_assets
 ) SELECT
 COALESCE(sum(CASE WHEN id IN current_refs THEN size ELSE 0 END),0),
 COALESCE(sum(CASE WHEN id NOT IN current_refs AND id IN (SELECT asset_id FROM retained_refs WHERE asset_id IS NOT NULL) THEN size ELSE 0 END),0),
 COALESCE(sum(CASE WHEN id NOT IN (SELECT asset_id FROM retained_refs WHERE asset_id IS NOT NULL) THEN size ELSE 0 END),0)
 FROM assets WHERE storage_key IS NOT NULL`).Scan(&u.CurrentAssetBytes, &u.RetainedOnlyAssetBytes, &u.OrphanAssetBytes)
	if err != nil {
		return u, fmt.Errorf("read asset storage usage: %w", err)
	}

	return u, nil
}

// StorageFootprint reads scalar storage metadata without loading documents.
func (s *Store) StorageFootprint(ctx context.Context) (diskspace.Footprint, error) {
	var (
		footprint       diskspace.Footprint
		pages, pageSize uint64
	)
	if err := s.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
		return footprint, fmt.Errorf("read allocated database pages: %w", err)
	}

	if err := s.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return footprint, fmt.Errorf("read database page size: %w", err)
	}

	footprint.DatabaseBytes = pages * pageSize
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(sum(size),0), count(*) FROM assets WHERE storage_key IS NOT NULL`).Scan(&footprint.OriginalBytes, &footprint.OriginalFiles); err != nil {
		return footprint, fmt.Errorf("read original storage footprint: %w", err)
	}

	return footprint, nil
}
