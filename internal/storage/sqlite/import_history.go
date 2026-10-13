package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/savorum/savorum/internal/access"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/recipecodec"
)

func insertImportEvent(ctx context.Context, tx *sql.Tx, command library.ImportMutation, entry library.Entry, documentJSON []byte) error {
	input := command.History
	if entry.Recipe.Source != nil {
		if input.SourceURL == "" {
			input.SourceURL = entry.Recipe.Source.URL
		}

		if input.SourceID == "" {
			input.SourceID = entry.Recipe.Source.SourceID
		}
	}

	input = library.SanitizeImportContext(input)

	operation := "create"
	if command.ExpectedRevision != 0 {
		operation = "refresh"
	}

	if input.InputKind == "merge" {
		operation = "merge"
	}

	sum := sha256.Sum256(documentJSON)

	baselineRevision := entry.Revision
	if command.ExpectedRevision != 0 {
		baselineRevision = command.ExpectedRevision
	}

	event := library.ImportEvent{
		ActorID: attributionActor(ctx),
		ID:      library.ImportEventID(entry.ID, entry.Revision), ImportContext: input,
		EntryID: entry.ID, RecipeID: string(entry.Recipe.ID), Revision: entry.Revision,
		Timestamp: command.Timestamp, Operation: operation, BaselineRevision: baselineRevision, ContentSHA256: hex.EncodeToString(sum[:]), SourceSnapshot: command.SourceSnapshot,
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode import event: %w", err)
	}

	_, err = tx.ExecContext(ctx, "INSERT INTO import_events (id, entry_id, revision, event_json, actor_id) VALUES (?, ?, ?, ?, ?)", event.ID, event.EntryID, event.Revision, string(encoded), event.ActorID)
	if err != nil {
		return fmt.Errorf("record import event: %w", err)
	}

	if command.SourceSnapshot != nil {
		sourceJSON, err := json.Marshal(*command.SourceSnapshot)
		if err != nil {
			return fmt.Errorf("encode source baseline: %w", err)
		}

		sourceSum := sha256.Sum256(sourceJSON)
		if _, err := tx.ExecContext(ctx, `INSERT INTO recipe_source_import_baselines(entry_id,baseline_revision,source_json,source_sha256,updated_at) VALUES (?,?,?,?,?) ON CONFLICT(entry_id) DO UPDATE SET baseline_revision=excluded.baseline_revision,source_json=excluded.source_json,source_sha256=excluded.source_sha256,updated_at=excluded.updated_at`, entry.ID, entry.Revision, string(sourceJSON), hex.EncodeToString(sourceSum[:]), formatTime(command.Timestamp)); err != nil {
			return fmt.Errorf("record source baseline: %w", err)
		}

		if err := syncSourceBaselineAssets(ctx, tx, entry.ID, entry.Revision, command.SourceSnapshot); err != nil {
			return err
		}
	}

	return auditIdentity(ctx, tx, attributionActor(ctx), string(entry.ID), "import.completed")
}

func syncSourceBaselineAssets(ctx context.Context, tx *sql.Tx, entryID library.EntryID, revision uint64, document *recipe.Document) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM recipe_source_import_baseline_assets WHERE entry_id = ?", entryID); err != nil {
		return fmt.Errorf("replace source baseline asset references: %w", err)
	}

	for _, asset := range document.Assets {
		assetID := "sha256:" + asset.SHA256

		var present int
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM assets WHERE id = ?", assetID).Scan(&present); errors.Is(err, sql.ErrNoRows) {
			continue
		} else if err != nil {
			return fmt.Errorf("check source baseline asset %q: %w", asset.ID, err)
		}

		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO recipe_source_import_baseline_assets(entry_id,baseline_revision,asset_id) VALUES (?,?,?)", entryID, revision, assetID); err != nil {
			return fmt.Errorf("link source baseline asset %q: %w", asset.ID, err)
		}
	}

	return nil
}

// GetSourceImportBaseline returns the source snapshot retained for an imported entry.
func (s *Store) GetSourceImportBaseline(ctx context.Context, id library.EntryID) (library.SourceImportBaseline, error) {
	if err := s.scopedObject(ctx, "library_recipes", string(id), access.Viewer); err != nil {
		return library.SourceImportBaseline{}, err
	}

	var (
		revision   uint64
		sourceJSON string
	)
	if err := s.db.QueryRowContext(ctx, "SELECT baseline_revision, source_json FROM recipe_source_import_baselines WHERE entry_id = ?", id).Scan(&revision, &sourceJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return library.SourceImportBaseline{}, library.ErrNoSourceBaseline
		}

		return library.SourceImportBaseline{}, fmt.Errorf("read source baseline: %w", err)
	}

	var header struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal([]byte(sourceJSON), &header); err != nil {
		return library.SourceImportBaseline{}, fmt.Errorf("decode source baseline header: %w", err)
	}

	document, err := recipecodec.Decode([]byte(sourceJSON), header.SchemaVersion)
	if err != nil {
		return library.SourceImportBaseline{}, fmt.Errorf("decode source baseline: %w", err)
	}

	return library.SourceImportBaseline{EntryID: id, Revision: revision, Document: document}, nil
}

// ListImportEvents returns an entry's import events in descending revision order.
func (s *Store) ListImportEvents(ctx context.Context, id library.EntryID, options library.RevisionListOptions) ([]library.ImportEvent, error) {
	if err := s.scopedObject(ctx, "library_recipes", string(id), access.Viewer); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, "SELECT event_json, actor_id, "+actorDisplaySQL+" FROM import_events WHERE entry_id = ? ORDER BY revision DESC LIMIT ? OFFSET ?", id, options.Limit, options.Offset)
	if err != nil {
		return nil, fmt.Errorf("list import events: %w", err)
	}
	defer rows.Close()

	result := []library.ImportEvent{}

	for rows.Next() {
		var encoded, actorID, actorName string
		if err := rows.Scan(&encoded, &actorID, &actorName); err != nil {
			return nil, fmt.Errorf("list import events: %w", err)
		}

		var event library.ImportEvent
		if err := json.Unmarshal([]byte(encoded), &event); err != nil {
			return nil, fmt.Errorf("list import events: %w", err)
		}

		event.ActorID, event.ActorName = actorID, actorName
		result = append(result, event)
	}

	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("iterate import events: %w", err)
	}

	return result, nil
}
