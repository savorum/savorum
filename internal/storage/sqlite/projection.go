package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipecodec"
)

// rebuildProjections brings every stored projection up to the presentation
// policy this build applies. Documents are read, not written: no revision is
// recorded and no edit time changes.
func (s *Store) rebuildProjections(ctx context.Context) error {
	version, err := s.projectionVersion(ctx)
	if err != nil {
		return err
	}

	if version >= library.ProjectionVersion {
		return nil
	}

	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin projection rebuild: %w", err)
	}

	defer func() { _ = transaction.Rollback() }()

	rows, err := transaction.QueryContext(ctx, `
		SELECT id, document_json, document_schema_version,
               title, description, authors_json, active_time, total_time,
               COALESCE(primary_asset_id, ''), search_text, language, translations_json
		FROM library_recipes`)
	if err != nil {
		return fmt.Errorf("read recipes to reproject: %w", err)
	}
	defer rows.Close()

	type rebuilt struct {
		id         string
		projection library.Projection
	}

	var updates []rebuilt

	for rows.Next() {
		var (
			id            string
			documentJSON  []byte
			schemaVersion int
			previous      library.Projection
		)
		if err := rows.Scan(&id, &documentJSON, &schemaVersion,
			&previous.Title, &previous.Description, &previous.AuthorsJSON,
			&previous.ActiveTime, &previous.TotalTime, &previous.PrimaryAssetID, &previous.SearchText,
			&previous.Language, &previous.TranslationsJSON); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read a recipe to reproject: %w", err)
		}
		// A document from a later Savorum keeps its stale projection.
		if schemaVersion > recipecodec.CurrentVersion {
			continue
		}

		document, err := recipecodec.Decode(documentJSON, schemaVersion)
		if err != nil {
			_ = rows.Close()
			return fmt.Errorf("decode recipe %s to reproject: %w", id, err)
		}

		projection, err := library.BuildProjection(document)
		if err != nil {
			_ = rows.Close()
			return fmt.Errorf("reproject recipe %s: %w", id, err)
		}

		// A new projection policy need not change every recipe. Avoid writing
		// identical values, which also avoids rebuilding its FTS row again.
		if projection != previous {
			updates = append(updates, rebuilt{id: id, projection: projection})
		}
	}

	if err := rows.Close(); err != nil {
		return fmt.Errorf("close recipes after reprojection: %w", err)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("read recipes to reproject: %w", err)
	}

	for _, update := range updates {
		if _, err := transaction.ExecContext(ctx, `
			UPDATE library_recipes
			SET title = ?, description = ?, authors_json = ?,
			    active_time = ?, total_time = ?,
			    primary_asset_id = ?, search_text = ?, language = ?, translations_json = ?
			WHERE id = ?`,
			update.projection.Title,
			update.projection.Description,
			update.projection.AuthorsJSON,
			update.projection.ActiveTime,
			update.projection.TotalTime,
			nullableString(update.projection.PrimaryAssetID),
			update.projection.SearchText,
			update.projection.Language,
			update.projection.TranslationsJSON,
			update.id,
		); err != nil {
			return fmt.Errorf("store the rebuilt projection for %s: %w", update.id, err)
		}
	}
	// PRAGMA does not accept a parameter, and the value is a constant of this
	// build rather than anything a caller supplies.
	if _, err := transaction.ExecContext(ctx,
		fmt.Sprintf("PRAGMA user_version = %d", library.ProjectionVersion),
	); err != nil {
		return fmt.Errorf("record the projection version: %w", err)
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit projection rebuild: %w", err)
	}

	return nil
}

// projectionVersion reads the policy version the stored projection was built
// with, from SQLite's user_version slot, which nothing else uses; the schema
// is tracked by the migration ledger.
func (s *Store) projectionVersion(ctx context.Context) (int, error) {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}

		return 0, fmt.Errorf("read the projection version: %w", err)
	}

	return version, nil
}
