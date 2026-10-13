package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/savorum/savorum/internal/event"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/recipecodec"
)

// recipeHead is the stored head row a guarded write validates against before
// it advances the entry's revision. The command reads it inside its own
// transaction, so the checks below and the guarded update observe the same row.
type recipeHead struct {
	id            library.EntryID
	revision      uint64
	documentID    recipe.RecipeID
	documentJSON  []byte
	schemaVersion int
	createdAt     string
	deletedAt     sql.NullString
}

func readRecipeHead(ctx context.Context, transaction *sql.Tx, id library.EntryID) (recipeHead, error) {
	head := recipeHead{id: id}

	err := transaction.QueryRowContext(ctx, `
		SELECT revision, document_id, document_json, document_schema_version, created_at, deleted_at
		FROM library_recipes
		WHERE id = ?`, id).
		Scan(&head.revision, &head.documentID, &head.documentJSON, &head.schemaVersion, &head.createdAt, &head.deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return recipeHead{}, library.ErrNotFound
	}

	if err != nil {
		return recipeHead{}, fmt.Errorf("read recipe %q head: %w", id, err)
	}

	return head, nil
}

func (h recipeHead) state() library.State {
	if h.deletedAt.Valid {
		return library.StateTrash
	}

	return library.StateActive
}

// requireSupportedSchema stops this build from rewriting an entry a later one
// wrote, which it can neither read nor faithfully replace.
func (h recipeHead) requireSupportedSchema() error {
	if h.schemaVersion > recipecodec.CurrentVersion {
		return &recipecodec.UnsupportedSchemaVersionError{Version: h.schemaVersion, SupportedThrough: recipecodec.CurrentVersion}
	}

	return nil
}

func (h recipeHead) requireRevision(expected uint64) error {
	if h.revision != expected {
		return &library.PreconditionError{EntryID: h.id, Expected: expected, Current: h.revision}
	}

	return nil
}

func (h recipeHead) requireActive() error {
	if h.deletedAt.Valid {
		return library.ErrInvalidState
	}

	return nil
}

func (h recipeHead) requireTrash() error {
	if !h.deletedAt.Valid {
		return library.ErrInvalidState
	}

	return nil
}

func (h recipeHead) created() (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, h.createdAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse recipe %q created_at: %w", h.id, err)
	}

	return parsed, nil
}

// recipeWrite carries one encoded recipe document through the shared write
// path: the head row mutation, the immutable revision recording it, and the
// revision's asset links.
type recipeWrite struct {
	entryID       library.EntryID
	revision      uint64
	document      recipe.Document
	documentJSON  []byte
	schemaVersion int
	projection    library.Projection
	state         library.State
	metadata      library.RevisionMetadata
	timestamp     time.Time
}

// encodeRecipeDocument renders a document for storage together with the
// library projection its head row carries.
func encodeRecipeDocument(document recipe.Document) ([]byte, int, library.Projection, error) {
	documentJSON, schemaVersion, err := recipecodec.Encode(document)
	if err != nil {
		return nil, 0, library.Projection{}, fmt.Errorf("encode recipe document %q: %w", document.ID, err)
	}

	projection, err := library.BuildProjection(document)
	if err != nil {
		return nil, 0, library.Projection{}, fmt.Errorf("build library projection for %q: %w", document.ID, err)
	}

	return documentJSON, schemaVersion, projection, nil
}

// insertHead stores a new head row, reporting ErrConflict when the entry ID is
// already taken.
func (w recipeWrite) insertHead(ctx context.Context, transaction *sql.Tx, createdAt time.Time) error {
	result, err := transaction.ExecContext(ctx, `
		INSERT INTO library_recipes (
			library_id, id, document_id, title, description, authors_json, active_time, total_time,
			primary_asset_id, document_json, document_schema_version, search_text,
			language, translations_json, revision, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		scopeID(ctx), w.entryID,
		w.document.ID,
		w.projection.Title,
		w.projection.Description,
		w.projection.AuthorsJSON,
		w.projection.ActiveTime,
		w.projection.TotalTime,
		nullableString(w.projection.PrimaryAssetID),
		string(w.documentJSON),
		w.schemaVersion,
		w.projection.SearchText,
		w.projection.Language,
		w.projection.TranslationsJSON,
		w.revision,
		formatTime(createdAt),
		formatTime(w.timestamp),
	)
	if err != nil {
		return fmt.Errorf("insert library recipe %q: %w", w.entryID, err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read library recipe insert result: %w", err)
	}

	if affected != 1 {
		return library.ErrConflict
	}

	return nil
}

// updateHead rewrites the head row guarded by the revision the caller already
// verified against the stored head, so a row that moved on reports a
// precondition failure rather than being overwritten.
func (w recipeWrite) updateHead(ctx context.Context, transaction *sql.Tx, expectedRevision uint64, deletedAt any) error {
	result, err := transaction.ExecContext(ctx, `
		UPDATE library_recipes
		SET title = ?, description = ?, authors_json = ?, active_time = ?, total_time = ?,
		    primary_asset_id = ?, document_json = ?, document_schema_version = ?, search_text = ?,
		    language = ?, translations_json = ?, revision = ?, updated_at = ?, deleted_at = ?
		WHERE id = ? AND revision = ?`,
		w.projection.Title, w.projection.Description, w.projection.AuthorsJSON,
		w.projection.ActiveTime, w.projection.TotalTime, nullableString(w.projection.PrimaryAssetID),
		string(w.documentJSON), w.schemaVersion, w.projection.SearchText,
		w.projection.Language, w.projection.TranslationsJSON, w.revision, formatTime(w.timestamp), deletedAt, w.entryID, expectedRevision)
	if err != nil {
		return fmt.Errorf("update library recipe %q: %w", w.entryID, err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read library recipe update result: %w", err)
	}

	if affected != 1 {
		return &library.PreconditionError{EntryID: w.entryID, Expected: expectedRevision, Current: expectedRevision}
	}

	return nil
}

// record stores the immutable revision and its asset links; the head mutation
// and this record are the two halves of one committed change.
func (w recipeWrite) record(ctx context.Context, transaction *sql.Tx, assetsRegisteredAt time.Time) error {
	if err := insertRevision(ctx, transaction, w.entryID, w.revision, w.documentJSON, w.schemaVersion, w.state, w.metadata, w.timestamp); err != nil {
		return err
	}

	return syncRevisionAssets(ctx, transaction, w.entryID, w.revision, w.document, assetsRegisteredAt)
}

func insertRevision(
	ctx context.Context,
	transaction *sql.Tx,
	entryID library.EntryID,
	revision uint64,
	documentJSON []byte,
	documentSchemaVersion int,
	state library.State,
	metadata library.RevisionMetadata,
	createdAt time.Time,
) error {
	var sourceRevision any
	if metadata.SourceRevision != nil {
		sourceRevision = *metadata.SourceRevision
	}

	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO recipe_revisions (
			entry_id, revision, document_json, document_schema_version, state, cause, source_revision, created_at, actor_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entryID,
		revision,
		string(documentJSON),
		documentSchemaVersion,
		state,
		metadata.Cause,
		sourceRevision,
		formatTime(createdAt),
		attributionActor(ctx),
	); err != nil {
		return fmt.Errorf("insert recipe revision: %w", err)
	}

	action := map[library.RevisionCause]string{library.RevisionCauseTrash: "recipe.trashed", library.RevisionCauseRestore: "recipe.restored", library.RevisionCauseMerge: "recipe.merged"}[metadata.Cause]

	kind := action
	if kind == "" {
		kind = "recipe.updated"
		if revision == 1 {
			kind = "recipe.created"
		}
	}

	if err := appendDomainEvent(ctx, transaction, kind, scopeID(ctx), "recipe", string(entryID), event.Payload{Revision: revision}); err != nil {
		return err
	}

	if action != "" {
		return auditIdentity(ctx, transaction, attributionActor(ctx), string(entryID), action)
	}

	return nil
}

func syncRevisionAssets(
	ctx context.Context,
	transaction *sql.Tx,
	entryID library.EntryID,
	revision uint64,
	document recipe.Document,
	createdAt time.Time,
) error {
	for _, asset := range document.Assets {
		assetID := "sha256:" + asset.SHA256

		result, err := transaction.ExecContext(ctx, `
			INSERT INTO assets (
				id, content_hash, media_type, size, storage_key, created_at, orphaned_at
			)
			SELECT ?, ?, ?, ?, NULL, ?, NULL
			WHERE NOT EXISTS (
				SELECT 1 FROM assets AS existing
				WHERE existing.id = ?
				  AND existing.storage_key IS NOT NULL
				  AND NOT EXISTS (
					SELECT 1 FROM library_assets
					WHERE library_assets.asset_id = existing.id AND library_assets.library_id = ?
				  )
			)
			ON CONFLICT (content_hash) DO UPDATE SET
				orphaned_at = NULL
			WHERE assets.media_type = excluded.media_type
			  AND assets.size = excluded.size`,
			assetID,
			asset.SHA256,
			asset.MediaType,
			asset.Size,
			formatTime(createdAt),
			assetID,
			scopeID(ctx),
		)
		if err != nil {
			return fmt.Errorf("register recipe asset %q: %w", asset.ID, err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read recipe asset registration result: %w", err)
		}

		if affected != 1 {
			if err := requireAssetScope(ctx, transaction, assetID); err != nil {
				return err
			}

			return fmt.Errorf("asset content hash %s has conflicting metadata: %w", asset.SHA256, library.ErrConflict)
		}

		isPrimary := 0
		if asset.ID == document.PrimaryImage {
			isPrimary = 1
		}

		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO recipe_revision_assets (
				entry_id, revision, asset_id, local_asset_id, role, path, alt, caption, is_primary
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			entryID,
			revision,
			assetID,
			asset.ID,
			nullableString(string(asset.Role)),
			asset.Path,
			nullableString(asset.Alt),
			nullableString(asset.Caption),
			isPrimary,
		); err != nil {
			return fmt.Errorf("link recipe revision asset %q: %w", asset.ID, err)
		}
	}

	return nil
}
