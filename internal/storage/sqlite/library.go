package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
)

var _ library.Repository = (*Store)(nil)

// Create stores a new library entry and its initial revision.
func (s *Store) Create(ctx context.Context, entry library.Entry, metadata library.RevisionMetadata) error {
	if err := s.authorizeScope(ctx, access.Editor); err != nil {
		return err
	}

	documentJSON, documentSchemaVersion, projection, err := encodeRecipeDocument(entry.Recipe)
	if err != nil {
		return err
	}

	write := recipeWrite{
		entryID:       entry.ID,
		revision:      entry.Revision,
		document:      entry.Recipe,
		documentJSON:  documentJSON,
		schemaVersion: documentSchemaVersion,
		projection:    projection,
		state:         library.StateActive,
		metadata:      metadata,
		timestamp:     entry.UpdatedAt,
	}

	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin library recipe creation: %w", err)
	}
	defer transaction.Rollback()

	if err := s.authorizeScopeWith(ctx, transaction, access.Editor); err != nil {
		return err
	}

	if err := recordCreation(ctx, transaction, string(entry.ID)); err != nil {
		return err
	}

	if err := write.insertHead(ctx, transaction, entry.CreatedAt); err != nil {
		return err
	}

	if err := write.record(ctx, transaction, entry.CreatedAt); err != nil {
		return err
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit library recipe creation: %w", err)
	}

	return nil
}

// Get returns an entry in the requested lifecycle state.
func (s *Store) Get(ctx context.Context, id library.EntryID, state library.State) (library.Entry, error) {
	if err := s.scopedObject(ctx, "library_recipes", string(id), access.Viewer); err != nil {
		return library.Entry{}, err
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, document_json, document_schema_version, revision, created_at, updated_at, deleted_at
		FROM library_recipes
		WHERE id = ?
		  AND ((? = 'active' AND deleted_at IS NULL) OR (? = 'trash' AND deleted_at IS NOT NULL))`, id, state, state)

	entry, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return library.Entry{}, library.ErrNotFound
	}

	if err != nil {
		return library.Entry{}, fmt.Errorf("read library recipe: %w", err)
	}

	return entry, nil
}

// List returns library summaries matching options.
func (s *Store) List(ctx context.Context, options library.ListOptions) ([]library.Summary, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, err
	}

	entries, _, err := s.listRecipePage(ctx, options, false)

	return entries, err
}

// ListPage returns one bounded page of library summaries and the exact total
// match count.
func (s *Store) ListPage(ctx context.Context, options library.ListOptions) ([]library.Summary, int, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, 0, err
	}

	return s.listRecipePage(ctx, options, true)
}

// Count returns the number of library entries matching options.
func (s *Store) Count(ctx context.Context, options library.ListOptions) (int, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return 0, err
	}

	return s.countSummaries(ctx, options.Search, options.State)
}

// ListReferences names every recipe in one lifecycle state for the selected
// library, reading neither summaries nor documents.
func (s *Store) ListReferences(ctx context.Context, state library.State) ([]library.RecipeReference, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, err
	}

	var stateCondition string

	switch state {
	case library.StateActive:
		stateCondition = "deleted_at IS NULL"
	case library.StateTrash:
		stateCondition = "deleted_at IS NOT NULL"
	default:
		return []library.RecipeReference{}, nil
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id, title, revision FROM library_recipes WHERE `+stateCondition+` AND library_id = ? ORDER BY title COLLATE NOCASE, id`, scopeID(ctx))
	if err != nil {
		return nil, fmt.Errorf("query recipe references: %w", err)
	}
	defer rows.Close()

	references := []library.RecipeReference{}

	for rows.Next() {
		var reference library.RecipeReference
		if err := rows.Scan(&reference.ID, &reference.Title, &reference.Revision); err != nil {
			return nil, fmt.Errorf("scan recipe reference: %w", err)
		}

		references = append(references, reference)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recipe references: %w", err)
	}

	return references, nil
}

// Revise applies a revision command and records its resulting entry revision.
func (s *Store) Revise(ctx context.Context, command library.RevisionCommand) (library.Entry, error) {
	entries, err := s.ReviseMany(ctx, []library.RevisionCommand{command})
	if err != nil {
		return library.Entry{}, err
	}

	return entries[0], nil
}

// ReviseMany applies several revision commands in one transaction, so either
// every entry advances or none does.
func (s *Store) ReviseMany(ctx context.Context, commands []library.RevisionCommand) ([]library.Entry, error) {
	for _, command := range commands {
		if err := s.scopedObject(ctx, "library_recipes", string(command.ID), access.Editor); err != nil {
			return nil, err
		}
	}

	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin recipe revision: %w", err)
	}
	defer transaction.Rollback()

	entries := make([]library.Entry, 0, len(commands))

	for _, command := range commands {
		entry, err := reviseIn(ctx, transaction, command)
		if err != nil {
			return nil, err
		}

		entries = append(entries, entry)
	}

	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit recipe revision: %w", err)
	}

	return entries, nil
}

func reviseIn(ctx context.Context, transaction *sql.Tx, command library.RevisionCommand) (library.Entry, error) {
	documentJSON, documentSchemaVersion, projection, err := encodeRecipeDocument(command.Recipe)
	if err != nil {
		return library.Entry{}, err
	}

	head, err := readRecipeHead(ctx, transaction, command.ID)
	if err != nil {
		return library.Entry{}, err
	}

	if err := head.requireSupportedSchema(); err != nil {
		return library.Entry{}, err
	}

	if err := head.requireRevision(command.ExpectedRevision); err != nil {
		return library.Entry{}, err
	}

	if command.Recipe.ID != head.documentID {
		return library.Entry{}, library.ErrDocumentIdentityChange
	}

	if head.state() != command.CurrentState {
		return library.Entry{}, library.ErrInvalidState
	}

	parsedCreatedAt, err := head.created()
	if err != nil {
		return library.Entry{}, err
	}

	var nextDeletedAt any
	if command.TargetState == library.StateTrash {
		nextDeletedAt = formatTime(command.Timestamp)
	}

	write := recipeWrite{
		entryID:       command.ID,
		revision:      head.revision + 1,
		document:      command.Recipe,
		documentJSON:  documentJSON,
		schemaVersion: documentSchemaVersion,
		projection:    projection,
		state:         command.TargetState,
		metadata:      command.Metadata,
		timestamp:     command.Timestamp,
	}
	if err := write.updateHead(ctx, transaction, command.ExpectedRevision, nextDeletedAt); err != nil {
		return library.Entry{}, err
	}

	if command.CurrentState != command.TargetState {
		if err := advanceBooksForRecipeEntries(ctx, transaction, []library.EntryID{command.ID}, command.Timestamp); err != nil {
			return library.Entry{}, err
		}
	}

	if err := write.record(ctx, transaction, command.Timestamp); err != nil {
		return library.Entry{}, err
	}

	entry := library.Entry{
		ID:        command.ID,
		Recipe:    command.Recipe,
		Revision:  write.revision,
		CreatedAt: parsedCreatedAt,
		UpdatedAt: command.Timestamp,
	}
	if command.TargetState == library.StateTrash {
		deleted := command.Timestamp
		entry.DeletedAt = &deleted
	}

	return entry, nil
}

// GetRevision returns one retained revision of an entry.
func (s *Store) GetRevision(ctx context.Context, id library.EntryID, revision uint64) (library.Revision, error) {
	if err := s.scopedObject(ctx, "library_recipes", string(id), access.Viewer); err != nil {
		return library.Revision{}, err
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT entry_id, revision, document_json, document_schema_version, state, cause, source_revision, created_at, actor_id, `+actorDisplaySQL+`
		FROM recipe_revisions
		WHERE entry_id = ? AND revision = ?`, id, revision)

	result, err := scanRevision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return library.Revision{}, library.ErrNotFound
	}

	if err != nil {
		return library.Revision{}, fmt.Errorf("read recipe revision: %w", err)
	}

	return result, nil
}

// ListRevisionDocuments returns a page of an entry's retained revisions with
// their documents, so history can report what each revision changed.
func (s *Store) ListRevisionDocuments(ctx context.Context, id library.EntryID, options library.RevisionListOptions) ([]library.Revision, error) {
	if err := s.scopedObject(ctx, "library_recipes", string(id), access.Viewer); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT entry_id, revision, document_json, document_schema_version, state, cause, source_revision, created_at, actor_id, `+actorDisplaySQL+`
		FROM recipe_revisions
		WHERE entry_id = ?
		ORDER BY revision DESC
		LIMIT ? OFFSET ?`, id, options.Limit, options.Offset)
	if err != nil {
		return nil, fmt.Errorf("query recipe revision documents: %w", err)
	}
	defer rows.Close()

	revisions := make([]library.Revision, 0, options.Limit)

	for rows.Next() {
		revision, err := scanRevision(rows)
		if err != nil {
			return nil, fmt.Errorf("scan recipe revision document: %w", err)
		}

		revisions = append(revisions, revision)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recipe revision documents: %w", err)
	}

	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close recipe revision documents: %w", err)
	}

	return revisions, nil
}

// ListRevisions returns a page of an entry's retained revision summaries.
func (s *Store) ListRevisions(ctx context.Context, id library.EntryID, options library.RevisionListOptions) ([]library.RevisionSummary, error) {
	if err := s.scopedObject(ctx, "library_recipes", string(id), access.Viewer); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT entry_id, revision, state, cause, source_revision, created_at, actor_id, `+actorDisplaySQL+`
		FROM recipe_revisions
		WHERE entry_id = ?
		ORDER BY revision DESC
		LIMIT ? OFFSET ?`, id, options.Limit, options.Offset)
	if err != nil {
		return nil, fmt.Errorf("query recipe revisions: %w", err)
	}
	defer rows.Close()

	revisions := make([]library.RevisionSummary, 0)

	for rows.Next() {
		revision, err := scanRevisionSummary(rows)
		if err != nil {
			return nil, fmt.Errorf("scan recipe revision: %w", err)
		}

		revisions = append(revisions, revision)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recipe revisions: %w", err)
	}

	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close recipe revisions: %w", err)
	}

	if len(revisions) == 0 {
		var exists int
		if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM library_recipes WHERE id = ?)", id).Scan(&exists); err != nil {
			return nil, fmt.Errorf("check recipe revision owner: %w", err)
		}

		if exists == 0 {
			return nil, library.ErrNotFound
		}
	}

	return revisions, nil
}

// Purge permanently removes a trashed entry at its expected revision.
func (s *Store) Purge(ctx context.Context, id library.EntryID, expectedRevision uint64, orphanedAt time.Time) error {
	return s.PurgeMany(ctx, []library.PurgeTarget{{ID: id, Revision: expectedRevision}}, nil, orphanedAt)
}

// PurgeMany permanently removes the supplied trashed entries and books atomically.
func (s *Store) PurgeMany(ctx context.Context, targets []library.PurgeTarget, books []library.BookPurgeTarget, orphanedAt time.Time) error {
	ctx = capacityCleanup(ctx)
	if err := s.checkPurgeScope(ctx, targets, books); err != nil {
		return err
	}

	if len(targets) == 0 && len(books) == 0 {
		return errors.New("at least one recipe or book is required for purge")
	}

	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin recipe purge batch: %w", err)
	}
	defer transaction.Rollback()

	for _, target := range targets {
		head, err := readRecipeHead(ctx, transaction, target.ID)
		if err != nil {
			return err
		}

		if err := head.requireSupportedSchema(); err != nil {
			return err
		}

		if err := head.requireRevision(target.Revision); err != nil {
			return err
		}

		if err := head.requireTrash(); err != nil {
			return err
		}
	}
	// Books are purged first so a recipe purge never has to advance a book
	// revision that this same batch is about to remove.
	for _, book := range books {
		if err := purgeBookInTx(ctx, transaction, book.ID, book.Revision, orphanedAt); err != nil {
			return err
		}
	}

	entryIDs := make([]library.EntryID, len(targets))
	for index, target := range targets {
		entryIDs[index] = target.ID
	}

	affectedBookIDs, err := bookIDsForRecipeEntries(ctx, transaction, entryIDs)
	if err != nil {
		return err
	}

	for _, target := range targets {
		result, err := transaction.ExecContext(ctx, `
			DELETE FROM library_recipes
			WHERE id = ? AND revision = ? AND deleted_at IS NOT NULL`, target.ID, target.Revision)
		if err != nil {
			return fmt.Errorf("delete purged recipe %q: %w", target.ID, err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read purge result for recipe %q: %w", target.ID, err)
		}

		if affected != 1 {
			return &library.PreconditionError{EntryID: target.ID, Expected: target.Revision, Current: target.Revision}
		}
	}

	if err := advanceBooksByID(ctx, transaction, affectedBookIDs, orphanedAt); err != nil {
		return err
	}

	if _, err := transaction.ExecContext(ctx, `
		UPDATE assets
		SET orphaned_at = COALESCE(orphaned_at, ?)
		WHERE NOT EXISTS (
			SELECT 1
			FROM recipe_revision_assets
			WHERE recipe_revision_assets.asset_id = assets.id
		)
		AND NOT EXISTS (SELECT 1 FROM books WHERE books.cover_asset_id = assets.id)
		AND NOT EXISTS (SELECT 1 FROM book_revisions WHERE book_revisions.cover_asset_id = assets.id)
		AND NOT EXISTS (SELECT 1 FROM recipe_source_attachments WHERE recipe_source_attachments.asset_id = assets.id)
		AND NOT EXISTS (SELECT 1 FROM recipe_source_import_baseline_assets WHERE recipe_source_import_baseline_assets.asset_id = assets.id)`, formatTime(orphanedAt)); err != nil {
		return fmt.Errorf("mark unreachable assets: %w", err)
	}

	if err := auditRecipePurges(ctx, transaction, targets); err != nil {
		return err
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit recipe purge batch: %w", err)
	}

	return nil
}

// FindByDocumentID reports the entry already carrying a portable recipe
// document. A library holds at most one entry per document ID, so this is the
// question an import asks before deciding what to do.
func (s *Store) FindByDocumentID(ctx context.Context, documentID recipe.RecipeID) (library.EntryID, uint64, library.State, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return "", 0, "", err
	}

	var (
		id        library.EntryID
		revision  uint64
		deletedAt sql.NullString
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT id, revision, deleted_at FROM library_recipes WHERE document_id = ? AND library_id = ?`, documentID, scopeID(ctx)).
		Scan(&id, &revision, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, "", library.ErrNotFound
	}

	if err != nil {
		return "", 0, "", fmt.Errorf("find recipe by document identity: %w", err)
	}

	state := library.StateActive
	if deletedAt.Valid {
		state = library.StateTrash
	}

	return id, revision, state, nil
}

// RegisteredAssets reports which of the named content hashes this library holds
// bytes for.
func (s *Store) RegisteredAssets(ctx context.Context, hashes []string) (map[string]bool, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, err
	}

	present := make(map[string]bool, len(hashes))
	if len(hashes) == 0 {
		return present, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(hashes)), ",")

	args := make([]any, len(hashes)+1)
	for index, hash := range hashes {
		args[index] = "sha256:" + hash
	}

	args[len(hashes)] = scopeID(ctx)

	rows, err := s.db.QueryContext(ctx,
		`SELECT content_hash FROM assets WHERE id IN (`+placeholders+`) AND storage_key IS NOT NULL AND EXISTS(SELECT 1 FROM library_assets WHERE asset_id=assets.id AND library_id=?)`, args...)
	if err != nil {
		return nil, fmt.Errorf("read registered assets: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, fmt.Errorf("scan registered asset: %w", err)
		}

		present[hash] = true
	}

	if err := rows.Err(); err != nil {
		return present, fmt.Errorf("iterate registered assets: %w", err)
	}

	return present, nil
}
