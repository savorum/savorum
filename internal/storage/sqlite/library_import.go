package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipecodec"
)

// ImportMany applies every create and replacement in one SQLite transaction.
// Preflight belongs to the library service; the repository rechecks identities
// and revisions under the write transaction so a concurrent change rolls the
// entire import back instead of producing a partial batch.
func (s *Store) ImportMany(ctx context.Context, mutations []library.ImportMutation) ([]library.Entry, error) {
	if err := s.authorizeScope(ctx, access.Editor); err != nil {
		return nil, err
	}

	if len(mutations) == 0 {
		return []library.Entry{}, nil
	}

	prepared, err := prepareImportMutations(mutations)
	if err != nil {
		return nil, err
	}

	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin recipe import: %w", err)
	}
	defer transaction.Rollback()

	if err := s.checkImportScope(ctx, transaction, mutations); err != nil {
		return nil, err
	}

	entries := make([]library.Entry, len(prepared))
	for index, item := range prepared {
		if item.command.ExpectedRevision == 0 {
			entry, err := importCreate(ctx, transaction, item.command, item.documentJSON, item.schemaVersion, item.projection)
			if err != nil {
				return nil, err
			}

			entries[index] = entry
			if err := insertImportSources(ctx, transaction, item.command, entry); err != nil {
				return nil, err
			}

			continue
		}

		entry, err := importReplace(ctx, transaction, item.command, item.documentJSON, item.expectedJSON, item.schemaVersion, item.projection)
		if err != nil {
			return nil, err
		}

		entries[index] = entry
		if err := insertImportSources(ctx, transaction, item.command, entry); err != nil {
			return nil, err
		}
	}

	for index, entry := range entries {
		if err := insertImportEvent(ctx, transaction, prepared[index].command, entry, prepared[index].documentJSON); err != nil {
			return nil, err
		}
	}

	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit recipe import: %w", err)
	}

	return entries, nil
}

type preparedImportMutation struct {
	command       library.ImportMutation
	documentJSON  []byte
	schemaVersion int
	projection    library.Projection
	expectedJSON  []byte
}

func prepareImportMutations(mutations []library.ImportMutation) ([]preparedImportMutation, error) {
	prepared := make([]preparedImportMutation, len(mutations))
	for index, mutation := range mutations {
		documentJSON, schemaVersion, projection, err := encodeRecipeDocument(mutation.Recipe)
		if err != nil {
			return nil, err
		}

		var expectedJSON []byte
		if mutation.ExpectedRecipe != nil {
			expectedJSON, _, err = recipecodec.Encode(*mutation.ExpectedRecipe)
			if err != nil {
				return nil, fmt.Errorf("encode expected imported recipe %q: %w", mutation.ExpectedRecipe.ID, err)
			}
		}

		prepared[index] = preparedImportMutation{command: mutation, documentJSON: documentJSON, schemaVersion: schemaVersion, projection: projection, expectedJSON: expectedJSON}
	}

	return prepared, nil
}

func importCreate(ctx context.Context, transaction *sql.Tx, command library.ImportMutation, documentJSON []byte, schemaVersion int, projection library.Projection) (library.Entry, error) {
	write := recipeWrite{
		entryID:       command.ID,
		revision:      1,
		document:      command.Recipe,
		documentJSON:  documentJSON,
		schemaVersion: schemaVersion,
		projection:    projection,
		state:         library.StateActive,
		metadata:      library.RevisionMetadata{Cause: library.RevisionCauseImport},
		timestamp:     command.Timestamp,
	}
	if err := write.insertHead(ctx, transaction, command.Timestamp); err != nil {
		return library.Entry{}, err
	}

	if err := write.record(ctx, transaction, command.Timestamp); err != nil {
		return library.Entry{}, err
	}

	return library.Entry{ID: command.ID, Recipe: command.Recipe, Revision: 1, CreatedAt: command.Timestamp, UpdatedAt: command.Timestamp}, nil
}

func importReplace(ctx context.Context, transaction *sql.Tx, command library.ImportMutation, documentJSON, expectedJSON []byte, schemaVersion int, projection library.Projection) (library.Entry, error) {
	head, err := readRecipeHead(ctx, transaction, command.ID)
	if err != nil {
		return library.Entry{}, err
	}

	if err := head.requireActive(); err != nil {
		return library.Entry{}, err
	}

	if err := head.requireRevision(command.ExpectedRevision); err != nil {
		return library.Entry{}, err
	}

	if expectedJSON != nil && !bytes.Equal(head.documentJSON, expectedJSON) {
		return library.Entry{}, library.ErrConflict
	}

	if head.documentID != command.Recipe.ID {
		return library.Entry{}, library.ErrDocumentIdentityChange
	}

	created, err := head.created()
	if err != nil {
		return library.Entry{}, err
	}

	write := recipeWrite{
		entryID:       command.ID,
		revision:      head.revision + 1,
		document:      command.Recipe,
		documentJSON:  documentJSON,
		schemaVersion: schemaVersion,
		projection:    projection,
		state:         library.StateActive,
		metadata:      library.RevisionMetadata{Cause: library.RevisionCauseImport},
		timestamp:     command.Timestamp,
	}
	if err := write.updateHead(ctx, transaction, command.ExpectedRevision, nil); err != nil {
		return library.Entry{}, err
	}

	if err := write.record(ctx, transaction, command.Timestamp); err != nil {
		return library.Entry{}, err
	}

	return library.Entry{ID: command.ID, Recipe: command.Recipe, Revision: write.revision, CreatedAt: created, UpdatedAt: command.Timestamp}, nil
}

func (s *Store) checkImportScope(ctx context.Context, queryer accessQueryer, mutations []library.ImportMutation) error {
	if err := s.authorizeScopeWith(ctx, queryer, access.Editor); err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	ids := make([]string, 0, len(mutations))
	for _, m := range mutations {
		if m.ExpectedRevision != 0 {
			ids = append(ids, string(m.ID))
		}
	}

	return s.scopedObjectsWith(ctx, queryer, "library_recipes", ids)
}
