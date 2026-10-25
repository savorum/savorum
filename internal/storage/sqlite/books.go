package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/savorum/savorum/internal/event"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/bookcodec"
	"github.com/savorum/savorum/internal/library"
)

const bookPreviewCandidates = `WITH preview_candidates AS (
    SELECT book_id, entry_id, 0 AS source_order, position, id FROM book_entries
    UNION ALL
    SELECT publication_book_id, entry_id, 1, 0, id FROM recipe_sources
    WHERE publication_book_id IS NOT NULL
)`

// CreateBook stores a new book and its initial revision.
func (s *Store) CreateBook(ctx context.Context, book library.Book) error {
	if book.CoverAssetID != "" {
		if err := requireAssetScope(ctx, s.db, book.CoverAssetID); err != nil {
			return operationError("CreateBook", err)
		}
	}

	if err := s.authorizeScope(ctx, access.Editor); err != nil {
		return operationError("CreateBook", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin book creation: %w", err)
	}
	defer tx.Rollback()

	if err := s.authorizeScopeWith(ctx, tx, access.Editor); err != nil {
		return err
	}

	if err := recordCreation(ctx, tx, book.ID); err != nil {
		return err
	}

	encoded, version, err := bookcodec.Encode(book)
	if err != nil {
		return fmt.Errorf("encode new book: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO books (library_id,id,kind,title,subtitle,description,cover_asset_id,author,editor,edition,isbn,publisher,publication_date,book_json,book_schema_version,revision,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, scopeID(ctx), book.ID, book.Kind, book.Title, book.Subtitle, book.Description, nullableString(book.CoverAssetID), book.Author, book.Editor, book.Edition, book.ISBN, book.Publisher, book.PublicationDate, string(encoded), version, book.Revision, formatTime(book.CreatedAt), formatTime(book.UpdatedAt)); err != nil {
		if isConstraint(err) {
			return library.ErrConflict
		}

		return fmt.Errorf("insert book: %w", err)
	}

	if err := writeBookContents(ctx, tx, book, nil); err != nil {
		return fmt.Errorf("write initial book contents: %w", err)
	}

	if err := insertBookRevision(ctx, tx, book.ID, library.RevisionMetadata{Cause: library.RevisionCauseAPI}, book.UpdatedAt); err != nil {
		return fmt.Errorf("record initial book revision: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit book creation: %w", err)
	}

	return nil
}

// GetBook returns a book in the requested lifecycle state.
func (s *Store) GetBook(ctx context.Context, id string, state library.State) (library.Book, error) {
	if err := s.scopedObject(ctx, "books", id, access.Viewer); err != nil {
		return library.Book{}, operationError("GetBook", err)
	}

	book, err := readBook(ctx, s.db, id, state)
	if err != nil {
		return library.Book{}, err
	}

	err = s.db.QueryRowContext(ctx, bookPreviewCandidates+` SELECT recipe.primary_asset_id, recipe.id
        FROM preview_candidates AS candidate
        JOIN library_recipes AS recipe ON recipe.id=candidate.entry_id
        JOIN assets AS photo ON photo.id=recipe.primary_asset_id
        WHERE candidate.book_id=? AND recipe.library_id=?
          AND recipe.deleted_at IS NULL AND photo.storage_key IS NOT NULL
        ORDER BY candidate.source_order, candidate.position, candidate.id LIMIT 1`, id, scopeID(ctx)).Scan(&book.PreviewAssetID, &book.PreviewEntryID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return library.Book{}, fmt.Errorf("read book preview: %w", err)
	}

	return book, nil
}

// ListBooks returns book summaries matching options.
func (s *Store) ListBooks(ctx context.Context, options library.BookListOptions) ([]library.BookListItem, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, operationError("ListBooks", err)
	}

	state := options.State

	// CROSS JOIN keeps this book's candidates ahead of recipe lookups.
	rows, err := s.db.QueryContext(ctx, `
		SELECT books.id, books.kind, books.title, COALESCE(books.cover_asset_id, ''),
		       books.author, COUNT(book_entries.id),
		       MAX(CASE WHEN book_entries.entry_id = ? THEN 1 ELSE 0 END),
		       books.revision, books.updated_at, books.deleted_at,
		       COALESCE((SELECT recipe.primary_asset_id FROM (
                         SELECT entry_id, 0 AS source_order, position, id
                         FROM book_entries WHERE book_id=books.id
                         UNION ALL
                         SELECT entry_id, 1, 0, id
                         FROM recipe_sources WHERE publication_book_id=books.id
                       ) AS candidate
                       CROSS JOIN library_recipes AS recipe ON recipe.id=candidate.entry_id
                       JOIN assets AS photo ON photo.id=recipe.primary_asset_id
                       WHERE recipe.library_id=books.library_id
                         AND recipe.deleted_at IS NULL AND photo.storage_key IS NOT NULL
                       ORDER BY candidate.source_order, candidate.position, candidate.id LIMIT 1), ''),
		       COALESCE((SELECT recipe.id FROM (
                         SELECT entry_id, 0 AS source_order, position, id
                         FROM book_entries WHERE book_id=books.id
                         UNION ALL
                         SELECT entry_id, 1, 0, id
                         FROM recipe_sources WHERE publication_book_id=books.id
                       ) AS candidate
                       CROSS JOIN library_recipes AS recipe ON recipe.id=candidate.entry_id
                       JOIN assets AS photo ON photo.id=recipe.primary_asset_id
                       WHERE recipe.library_id=books.library_id
                         AND recipe.deleted_at IS NULL AND photo.storage_key IS NOT NULL
                       ORDER BY candidate.source_order, candidate.position, candidate.id LIMIT 1), '')
		FROM books
		LEFT JOIN book_entries ON book_entries.book_id = books.id
		WHERE books.library_id=? AND ((?='active' AND books.deleted_at IS NULL) OR (?='trash' AND books.deleted_at IS NOT NULL))
		GROUP BY books.id
		ORDER BY books.title COLLATE NOCASE, books.id
		LIMIT ? OFFSET ?`, options.EntryID, scopeID(ctx), state, state, options.Limit, options.Offset)
	if err != nil {
		return nil, fmt.Errorf("query books: %w", err)
	}
	defer rows.Close()

	books := []library.BookListItem{}

	for rows.Next() {
		var (
			book      library.BookListItem
			updated   string
			deletedAt sql.NullString
		)
		if err := rows.Scan(&book.ID, &book.Kind, &book.Title, &book.CoverAssetID, &book.Author, &book.EntryCount, &book.ContainsEntry, &book.Revision, &updated, &deletedAt, &book.PreviewAssetID, &book.PreviewEntryID); err != nil {
			return nil, fmt.Errorf("scan book summary: %w", err)
		}

		book.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, fmt.Errorf("parse book summary updated_at: %w", err)
		}

		if deletedAt.Valid {
			value, parseErr := time.Parse(time.RFC3339Nano, deletedAt.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse book summary deleted_at: %w", parseErr)
			}

			book.DeletedAt = &value
		}

		books = append(books, book)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate books: %w", err)
	}

	return books, nil
}

// CountBooks returns the number of books in state.
func (s *Store) CountBooks(ctx context.Context, state library.State) (int, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return 0, operationError("CountBooks", err)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM books WHERE library_id=? AND ((?='active' AND deleted_at IS NULL) OR (?='trash' AND deleted_at IS NOT NULL))", scopeID(ctx), state, state).Scan(&total); err != nil {
		return 0, fmt.Errorf("count books: %w", err)
	}

	return total, nil
}

// ReplaceBook updates a book when its revision matches expectedRevision.
func (s *Store) ReplaceBook(ctx context.Context, expectedRevision uint64, book library.Book) (library.Book, error) {
	if book.CoverAssetID != "" {
		if err := requireAssetScope(ctx, s.db, book.CoverAssetID); err != nil {
			return library.Book{}, operationError("ReplaceBook", err)
		}
	}

	if err := s.scopedObject(ctx, "books", book.ID, access.Editor); err != nil {
		return library.Book{}, operationError("ReplaceBook", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return library.Book{}, fmt.Errorf("begin book replacement: %w", err)
	}
	defer tx.Rollback()

	var (
		current   uint64
		created   string
		deletedAt sql.NullString
	)
	if err := tx.QueryRowContext(ctx, "SELECT revision, created_at, deleted_at FROM books WHERE id = ?", book.ID).Scan(&current, &created, &deletedAt); errors.Is(err, sql.ErrNoRows) {
		return library.Book{}, library.ErrNotFound
	} else if err != nil {
		return library.Book{}, fmt.Errorf("read book revision: %w", err)
	}

	if err := refuseFutureBookSchema(ctx, tx, book.ID); err != nil {
		return library.Book{}, fmt.Errorf("check book schema before replacement: %w", err)
	}

	if current != expectedRevision {
		return library.Book{}, &library.PreconditionError{Expected: expectedRevision, Current: current}
	}

	if deletedAt.Valid {
		return library.Book{}, library.ErrInvalidState
	}

	retainedEntries := map[library.EntryID]struct{}{}

	rows, err := tx.QueryContext(ctx, "SELECT entry_id FROM book_entries WHERE book_id = ?", book.ID)
	if err != nil {
		return library.Book{}, fmt.Errorf("query retained book entries: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var entryID library.EntryID
		if err := rows.Scan(&entryID); err != nil {
			_ = rows.Close()
			return library.Book{}, fmt.Errorf("scan retained book entry: %w", err)
		}

		retainedEntries[entryID] = struct{}{}
	}

	if err := rows.Err(); err != nil {
		return library.Book{}, fmt.Errorf("iterate retained book entries: %w", err)
	}

	if err := rows.Close(); err != nil {
		return library.Book{}, fmt.Errorf("close retained book entries: %w", err)
	}

	if book.Kind != library.BookPublication {
		var sourceCount int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM recipe_sources WHERE publication_book_id = ?", book.ID).Scan(&sourceCount); err != nil {
			return library.Book{}, fmt.Errorf("count publication sources: %w", err)
		}

		if sourceCount != 0 {
			return library.Book{}, library.ErrConflict
		}
	}

	if err := writeBookHead(ctx, tx, book, expectedRevision, book.UpdatedAt); err != nil {
		return library.Book{}, fmt.Errorf("replace book metadata: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM book_entries WHERE book_id = ?", book.ID); err != nil {
		return library.Book{}, fmt.Errorf("clear book entries: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM book_sections WHERE book_id = ?", book.ID); err != nil {
		return library.Book{}, fmt.Errorf("clear book sections: %w", err)
	}

	if err := writeBookContents(ctx, tx, book, retainedEntries); err != nil {
		return library.Book{}, fmt.Errorf("replace book contents: %w", err)
	}

	book.Revision = current + 1

	book.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return library.Book{}, fmt.Errorf("parse book created_at: %w", err)
	}

	if err := insertBookRevision(ctx, tx, book.ID, library.RevisionMetadata{Cause: library.RevisionCauseManualEdit}, book.UpdatedAt); err != nil {
		return library.Book{}, fmt.Errorf("record book replacement revision: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return library.Book{}, fmt.Errorf("commit book replacement: %w", err)
	}

	return s.GetBook(ctx, book.ID, library.StateActive)
}

// ReviseBookState changes a book's lifecycle state at its expected revision.
func (s *Store) ReviseBookState(ctx context.Context, id string, expectedRevision uint64, currentState, targetState library.State, metadata library.RevisionMetadata, timestamp time.Time) (library.Book, error) {
	if err := s.scopedObject(ctx, "books", id, access.Editor); err != nil {
		return library.Book{}, operationError("ReviseBookState", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return library.Book{}, fmt.Errorf("begin book lifecycle change: %w", err)
	}
	defer tx.Rollback()

	book, err := readBook(ctx, tx, id, currentState)
	if err != nil {
		return library.Book{}, fmt.Errorf("read book for state transition: %w", err)
	}

	if book.Revision != expectedRevision {
		return library.Book{}, &library.PreconditionError{Expected: expectedRevision, Current: book.Revision}
	}

	if currentState == targetState {
		return library.Book{}, library.ErrInvalidState
	}

	var deletedAt any
	if targetState == library.StateTrash {
		deletedAt = formatTime(timestamp)
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE books SET revision=revision+1, updated_at=?, deleted_at=?
		WHERE id=? AND revision=?`, formatTime(timestamp), deletedAt, id, expectedRevision)
	if err != nil {
		return library.Book{}, fmt.Errorf("update book lifecycle state: %w", err)
	}

	if affected, _ := result.RowsAffected(); affected != 1 {
		return library.Book{}, &library.PreconditionError{Expected: expectedRevision, Current: book.Revision}
	}

	book.Revision++
	book.UpdatedAt = timestamp
	book.DeletedAt = nil

	if targetState == library.StateTrash {
		value := timestamp
		book.DeletedAt = &value
	}

	if err := insertBookRevision(ctx, tx, id, metadata, timestamp); err != nil {
		return library.Book{}, fmt.Errorf("record book state transition: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return library.Book{}, fmt.Errorf("commit book lifecycle change: %w", err)
	}

	return book, nil
}

// RestoreBookRevision restores a retained revision as a new current revision.
func (s *Store) RestoreBookRevision(ctx context.Context, id string, sourceRevision, expectedRevision uint64, metadata library.RevisionMetadata, timestamp time.Time) (library.Book, error) {
	if err := s.scopedObject(ctx, "books", id, access.Editor); err != nil {
		return library.Book{}, operationError("RestoreBookRevision", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return library.Book{}, fmt.Errorf("begin book revision restore: %w", err)
	}
	defer tx.Rollback()

	source, err := readBookRevision(ctx, tx, id, sourceRevision)
	if err != nil {
		return library.Book{}, fmt.Errorf("read source revision for book restore: %w", err)
	}

	var (
		currentRevision uint64
		createdAt       string
	)
	if err := tx.QueryRowContext(ctx, "SELECT revision, created_at FROM books WHERE id=?", id).Scan(&currentRevision, &createdAt); errors.Is(err, sql.ErrNoRows) {
		return library.Book{}, library.ErrNotFound
	} else if err != nil {
		return library.Book{}, fmt.Errorf("read current book for restore: %w", err)
	}

	if currentRevision != expectedRevision {
		return library.Book{}, &library.PreconditionError{Expected: expectedRevision, Current: currentRevision}
	}

	if err := refuseFutureBookSchema(ctx, tx, id); err != nil {
		return library.Book{}, fmt.Errorf("check book schema before restore: %w", err)
	}

	book := source.Book
	book.Revision, book.UpdatedAt, book.DeletedAt = currentRevision+1, timestamp, nil

	book.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return library.Book{}, fmt.Errorf("parse restored book created_at: %w", err)
	}

	if err := writeBookHead(ctx, tx, book, expectedRevision, timestamp); err != nil {
		return library.Book{}, fmt.Errorf("restore book metadata: %w", err)
	}

	retained := map[library.EntryID]struct{}{}

	rows, err := tx.QueryContext(ctx, "SELECT entry_id FROM book_entries WHERE book_id=?", id)
	if err != nil {
		return library.Book{}, fmt.Errorf("read retained book members: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var entryID library.EntryID
		if err := rows.Scan(&entryID); err != nil {
			_ = rows.Close()
			return library.Book{}, fmt.Errorf("scan retained book entry: %w", err)
		}

		retained[entryID] = struct{}{}
	}

	if err := rows.Err(); err != nil {
		return library.Book{}, fmt.Errorf("iterate retained book members: %w", err)
	}

	if err := rows.Close(); err != nil {
		return library.Book{}, fmt.Errorf("close retained book entries: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM book_entries WHERE book_id=?", id); err != nil {
		return library.Book{}, fmt.Errorf("clear book entries for restore: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM book_sections WHERE book_id=?", id); err != nil {
		return library.Book{}, fmt.Errorf("clear book sections for restore: %w", err)
	}

	if err := writeBookContents(ctx, tx, book, retained); err != nil {
		return library.Book{}, fmt.Errorf("restore book contents: %w", err)
	}

	if err := insertBookRevision(ctx, tx, id, metadata, timestamp); err != nil {
		return library.Book{}, fmt.Errorf("record restored book revision: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return library.Book{}, fmt.Errorf("commit book revision restore: %w", err)
	}

	return book, nil
}

// GetBookRevision returns one retained book revision.
func (s *Store) GetBookRevision(ctx context.Context, id string, revision uint64) (library.BookRevision, error) {
	if err := s.scopedObject(ctx, "books", id, access.Viewer); err != nil {
		return library.BookRevision{}, operationError("GetBookRevision", err)
	}

	return readBookRevision(ctx, s.db, id, revision)
}

// ListBookRevisions returns a page of retained book revision summaries.
func (s *Store) ListBookRevisions(ctx context.Context, id string, options library.RevisionListOptions) ([]library.BookRevisionSummary, error) {
	if err := s.scopedObject(ctx, "books", id, access.Viewer); err != nil {
		return nil, operationError("ListBookRevisions", err)
	}

	rows, err := s.db.QueryContext(ctx, `SELECT book_id,revision,state,cause,source_revision,created_at,actor_id,`+actorDisplaySQL+`
		FROM book_revisions WHERE book_id=? ORDER BY revision DESC LIMIT ? OFFSET ?`, id, options.Limit, options.Offset)
	if err != nil {
		return nil, fmt.Errorf("query book revisions: %w", err)
	}
	defer rows.Close()

	result := []library.BookRevisionSummary{}

	for rows.Next() {
		var (
			item    library.BookRevisionSummary
			source  sql.NullInt64
			created string
		)
		if err := rows.Scan(&item.BookID, &item.Revision, &item.State, &item.Cause, &source, &created, &item.ActorID, &item.ActorName); err != nil {
			return nil, fmt.Errorf("scan book revision: %w", err)
		}

		item.SourceRevision = countOrNil(source)

		item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, fmt.Errorf("parse book revision created_at: %w", err)
		}

		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate book revisions: %w", err)
	}

	if len(result) == 0 {
		var exists int
		if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM books WHERE id=?)", id).Scan(&exists); err != nil {
			return nil, fmt.Errorf("check book existence for history: %w", err)
		}

		if exists == 0 {
			return nil, library.ErrNotFound
		}
	}

	return result, nil
}

// PurgeBook permanently removes a trashed book at its expected revision.
func (s *Store) PurgeBook(ctx context.Context, id string, expectedRevision uint64, orphanedAt time.Time) error {
	ctx = capacityCleanup(ctx)
	if err := s.scopedObject(ctx, "books", id, access.Editor); err != nil {
		return operationError("PurgeBook", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin book purge: %w", err)
	}
	defer tx.Rollback()

	if err := purgeBookInTx(ctx, tx, id, expectedRevision, orphanedAt); err != nil {
		return fmt.Errorf("purge reviewed book: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit book purge: %w", err)
	}

	return nil
}

// purgeBookInTx removes one reviewed trashed book inside the caller's
// transaction. Every failure names the book.
func purgeBookInTx(ctx context.Context, tx *sql.Tx, id string, expectedRevision uint64, orphanedAt time.Time) error {
	err := purgeBook(ctx, tx, id, expectedRevision, orphanedAt)
	if err == nil {
		return auditIdentity(ctx, tx, attributionActor(ctx), id, "book.purge")
	}
	// Read the title while the row is still there, so the failure can be
	// explained by the name the reader knows the book under.
	var title string
	if lookup := tx.QueryRowContext(ctx, "SELECT title FROM books WHERE id = ?", id).Scan(&title); lookup != nil {
		title = ""
	}

	return &library.BookPurgeError{BookID: id, Title: title, Err: err}
}

func purgeBook(ctx context.Context, tx *sql.Tx, id string, expectedRevision uint64, orphanedAt time.Time) error {
	var (
		current   uint64
		deletedAt sql.NullString
	)
	if err := tx.QueryRowContext(ctx, "SELECT revision, deleted_at FROM books WHERE id=?", id).Scan(&current, &deletedAt); errors.Is(err, sql.ErrNoRows) {
		return library.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read book state for purge: %w", err)
	}

	if current != expectedRevision {
		return &library.PreconditionError{Expected: expectedRevision, Current: current}
	}

	if !deletedAt.Valid {
		return library.ErrInvalidState
	}

	rows, err := tx.QueryContext(ctx, `SELECT cover_asset_id FROM book_revisions WHERE book_id=? AND cover_asset_id IS NOT NULL
		UNION SELECT cover_asset_id FROM books WHERE id=? AND cover_asset_id IS NOT NULL`, id, id)
	if err != nil {
		return fmt.Errorf("query retained book covers: %w", err)
	}
	defer rows.Close()

	var covers []string

	for rows.Next() {
		var cover string
		if err := rows.Scan(&cover); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan retained book cover: %w", err)
		}

		covers = append(covers, cover)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate book covers: %w", err)
	}

	if err := rows.Close(); err != nil {
		return fmt.Errorf("close retained book covers: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM books WHERE id=? AND revision=? AND deleted_at IS NOT NULL", id, expectedRevision); err != nil {
		if isConstraint(err) {
			return library.ErrConflict
		}

		return fmt.Errorf("purge book: %w", err)
	}

	for _, cover := range covers {
		if _, err := tx.ExecContext(ctx, `UPDATE assets SET orphaned_at=COALESCE(orphaned_at, ?) WHERE id=?
			AND NOT EXISTS (SELECT 1 FROM recipe_revision_assets WHERE asset_id=assets.id)
			AND NOT EXISTS (SELECT 1 FROM books WHERE cover_asset_id=assets.id)
			AND NOT EXISTS (SELECT 1 FROM book_revisions WHERE cover_asset_id=assets.id)
			AND NOT EXISTS (SELECT 1 FROM recipe_source_import_baseline_assets WHERE asset_id=assets.id)`, formatTime(orphanedAt), cover); err != nil {
			return fmt.Errorf("mark purged book cover orphaned: %w", err)
		}
	}

	return nil
}

// insertBookRevision retains the book exactly as it currently stands by
// copying the head row rather than by re-serializing what the caller believes
// the head to be. A revision therefore cannot record anything other than the
// head it creates, and the database checks that too.
func insertBookRevision(ctx context.Context, tx *sql.Tx, bookID string, metadata library.RevisionMetadata, timestamp time.Time) error {
	result, err := tx.ExecContext(ctx, `
		INSERT INTO book_revisions (
			book_id, revision, cover_asset_id, book_json, book_schema_version,
			state, cause, source_revision, created_at, actor_id
		)
		SELECT id, revision, cover_asset_id, book_json, book_schema_version,
		       CASE WHEN deleted_at IS NULL THEN 'active' ELSE 'trash' END,
		       ?, ?, ?, ?
		FROM books WHERE id = ?`,
		metadata.Cause, nullableUint64(metadata.SourceRevision), formatTime(timestamp), attributionActor(ctx), bookID)
	if err != nil {
		return fmt.Errorf("insert book revision: %w", err)
	}

	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("read book revision insert result: %w", err)
	} else if affected != 1 {
		return library.ErrNotFound
	}

	action := map[library.RevisionCause]string{library.RevisionCauseTrash: "book.trashed", library.RevisionCauseRestore: "book.restored"}[metadata.Cause]

	var revision uint64
	if err := tx.QueryRowContext(ctx, "SELECT revision FROM books WHERE id=?", bookID).Scan(&revision); err != nil {
		return operationError("insertBookRevision", err)
	}

	kind := action
	if kind == "" {
		kind = "book.updated"
		if revision == 1 {
			kind = "book.created"
		}
	}

	if err := appendDomainEvent(ctx, tx, kind, scopeID(ctx), "book", bookID, event.Payload{Revision: revision}); err != nil {
		return operationError("insertBookRevision", err)
	}

	if action != "" {
		return auditIdentity(ctx, tx, attributionActor(ctx), bookID, action)
	}

	return nil
}

// writeBookHead replaces the current book document and advances its revision in
// one statement, which is the only way the head may change.
func writeBookHead(ctx context.Context, tx *sql.Tx, book library.Book, expectedRevision uint64, timestamp time.Time) error {
	encoded, version, err := bookcodec.Encode(book)
	if err != nil {
		return fmt.Errorf("encode current book metadata: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE books SET kind=?,title=?,subtitle=?,description=?,cover_asset_id=?,author=?,
			editor=?,edition=?,isbn=?,publisher=?,publication_date=?,
			book_json=?,book_schema_version=?,revision=revision+1,updated_at=?,deleted_at=?
		WHERE id=? AND revision=?`,
		book.Kind, book.Title, book.Subtitle, book.Description, nullableString(book.CoverAssetID),
		book.Author, book.Editor, book.Edition, book.ISBN, book.Publisher, book.PublicationDate,
		string(encoded), version, formatTime(timestamp), nullableTime(book.DeletedAt), book.ID, expectedRevision)
	if err != nil {
		return fmt.Errorf("update book: %w", err)
	}

	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("read book update result: %w", err)
	} else if affected != 1 {
		return &library.PreconditionError{Expected: expectedRevision, Current: expectedRevision}
	}

	return nil
}

// refuseFutureBookSchema stops this build from overwriting a book written by a
// later one, which it can neither read nor faithfully rewrite.
func refuseFutureBookSchema(ctx context.Context, q organizationQueryer, id string) error {
	var version int
	if err := q.QueryRowContext(ctx, "SELECT book_schema_version FROM books WHERE id = ?", id).Scan(&version); errors.Is(err, sql.ErrNoRows) {
		return library.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read book schema version: %w", err)
	}

	if version > bookcodec.CurrentVersion {
		return &bookcodec.UnsupportedSchemaVersionError{Version: version, SupportedThrough: bookcodec.CurrentVersion}
	}

	return nil
}

// readBookRevision reconstructs one retained book state. The snapshot is
// decoded by the schema version it was written under, so bytes written by an
// earlier build remain readable and bytes written by a later one fail rather
// than being misread.
func readBookRevision(ctx context.Context, q organizationQueryer, id string, revision uint64) (library.BookRevision, error) {
	var (
		result        library.BookRevision
		bookJSON      string
		schemaVersion int
		source        sql.NullInt64
		created       string
	)

	err := q.QueryRowContext(ctx, `
		SELECT book_json, book_schema_version, state, cause, source_revision, created_at, actor_id, `+actorDisplaySQL+`
		FROM book_revisions WHERE book_id=? AND revision=?`, id, revision).Scan(
		&bookJSON, &schemaVersion, &result.State, &result.Cause, &source, &created, &result.ActorID, &result.ActorName)
	if errors.Is(err, sql.ErrNoRows) {
		return library.BookRevision{}, library.ErrNotFound
	}

	if err != nil {
		return library.BookRevision{}, fmt.Errorf("read book revision: %w", err)
	}

	book, err := bookcodec.Decode([]byte(bookJSON), schemaVersion)
	if err != nil {
		return library.BookRevision{}, fmt.Errorf("read book revision: %w", err)
	}

	book.ID, book.Revision = id, revision
	if book.Sections == nil {
		book.Sections = []library.BookSection{}
	}

	if book.Entries == nil {
		book.Entries = []library.BookEntry{}
	}

	result.SourceRevision = countOrNil(source)

	if result.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return library.BookRevision{}, fmt.Errorf("read book revision: %w", err)
	}

	if err := annotateBookEntryStates(ctx, q, book.Entries); err != nil {
		return library.BookRevision{}, fmt.Errorf("read book revision: %w", err)
	}

	result.Book = book

	return result, nil
}

// bookIdentityError reports a section or entry ID held by another book. The
// book's own rows are cleared before its contents are written.
func bookIdentityError(err error, noun, id string) error {
	if err != nil {
		return fmt.Errorf("check book %s identity: %w", noun, err)
	}

	return fmt.Errorf("%w: %s %q", library.ErrBookIdentityInUse, noun, id)
}

func writeBookContents(ctx context.Context, tx *sql.Tx, book library.Book, retainedEntries map[library.EntryID]struct{}) error {
	for _, section := range book.Sections {
		if taken, err := rowExists(ctx, tx, "SELECT 1 FROM book_sections WHERE id = ?", section.ID); err != nil || taken {
			return bookIdentityError(err, "section", section.ID)
		}

		if _, err := tx.ExecContext(ctx, "INSERT INTO book_sections (id,book_id,title,position) VALUES (?,?,?,?)", section.ID, book.ID, section.Title, section.Position); err != nil {
			return fmt.Errorf("insert book section: %w", err)
		}
	}

	for _, entry := range book.Entries {
		var exists, active int
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM library_recipes WHERE id = ? AND library_id = ?),
			       EXISTS(SELECT 1 FROM library_recipes WHERE id = ? AND library_id = ? AND deleted_at IS NULL)`,
			entry.EntryID, scopeID(ctx), entry.EntryID, scopeID(ctx)).Scan(&exists, &active); err != nil {
			return fmt.Errorf("check book recipe: %w", err)
		}

		if exists == 0 {
			return library.ErrUnknownMember
		}

		if _, retained := retainedEntries[entry.EntryID]; active == 0 && !retained {
			return library.ErrInvalidState
		}

		if taken, err := rowExists(ctx, tx, "SELECT 1 FROM book_entries WHERE id = ?", entry.ID); err != nil || taken {
			return bookIdentityError(err, "entry", entry.ID)
		}

		if _, err := tx.ExecContext(ctx, "INSERT INTO book_entries (id,book_id,section_id,entry_id,position,note) VALUES (?,?,?,?,?,?)", entry.ID, book.ID, nullableString(entry.SectionID), entry.EntryID, entry.Position, entry.Note); err != nil {
			return fmt.Errorf("insert book entry: %w", err)
		}
	}

	return nil
}

// readBook returns the book its head document describes. The document is the
// book's content; book_sections and book_entries are the projection used for
// querying it, and readBookProjection reads those.
func readBook(ctx context.Context, q organizationQueryer, id string, state library.State) (library.Book, error) {
	var (
		bookJSON         string
		schemaVersion    int
		revision         uint64
		created, updated string
		deletedAt        sql.NullString
	)

	err := q.QueryRowContext(ctx, `
		SELECT book_json, book_schema_version, revision, created_at, updated_at, deleted_at
		FROM books WHERE id=? AND ((?='active' AND deleted_at IS NULL) OR (?='trash' AND deleted_at IS NOT NULL))`,
		id, state, state).Scan(&bookJSON, &schemaVersion, &revision, &created, &updated, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return library.Book{}, library.ErrNotFound
	}

	if err != nil {
		return library.Book{}, fmt.Errorf("read book: %w", err)
	}

	book, err := bookcodec.Decode([]byte(bookJSON), schemaVersion)
	if err != nil {
		return library.Book{}, fmt.Errorf("decode current book document: %w", err)
	}

	book.ID, book.Revision = id, revision
	if book.Sections == nil {
		book.Sections = []library.BookSection{}
	}

	if book.Entries == nil {
		book.Entries = []library.BookEntry{}
	}

	if book.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return library.Book{}, fmt.Errorf("parse book created_at: %w", err)
	}

	if book.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return library.Book{}, fmt.Errorf("parse book updated_at: %w", err)
	}

	if deletedAt.Valid {
		value, parseErr := time.Parse(time.RFC3339Nano, deletedAt.String)
		if parseErr != nil {
			return library.Book{}, fmt.Errorf("parse book deleted_at: %w", parseErr)
		}

		book.DeletedAt = &value
	}

	if err := annotateBookEntryStates(ctx, q, book.Entries); err != nil {
		return library.Book{}, fmt.Errorf("read book entry lifecycle states: %w", err)
	}

	return book, nil
}

// annotateBookEntryStates reports, for each member, whether the recipe it names
// is active, in trash, or gone. That is a fact about the recipe rather than
// about the book, so it is never part of a retained snapshot.
func annotateBookEntryStates(ctx context.Context, q organizationQueryer, entries []library.BookEntry) error {
	for index := range entries {
		var deletedAt sql.NullString

		err := q.QueryRowContext(ctx, "SELECT deleted_at FROM library_recipes WHERE id = ?", entries[index].EntryID).Scan(&deletedAt)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			entries[index].State = "purged"
		case err != nil:
			return fmt.Errorf("read book member state: %w", err)
		case deletedAt.Valid:
			entries[index].State = library.StateTrash
		default:
			entries[index].State = library.StateActive
		}
	}

	return nil
}

// readBookProjection reads the book from the queryable membership tables. It is
// how a new head document is built after curation or after a member is purged.
func readBookProjection(ctx context.Context, q organizationQueryer, id string, state library.State) (library.Book, error) {
	book, err := scanBook(q.QueryRowContext(ctx, `SELECT id,kind,title,subtitle,description,COALESCE(cover_asset_id,''),author,editor,edition,isbn,publisher,publication_date,revision,created_at,updated_at,deleted_at FROM books WHERE id=? AND ((?='active' AND deleted_at IS NULL) OR (?='trash' AND deleted_at IS NOT NULL))`, id, state, state))
	if errors.Is(err, sql.ErrNoRows) {
		return library.Book{}, library.ErrNotFound
	}

	if err != nil {
		return library.Book{}, fmt.Errorf("read book: %w", err)
	}

	book.Sections = []library.BookSection{}
	book.Entries = []library.BookEntry{}

	rows, err := q.QueryContext(ctx, "SELECT id,title,position FROM book_sections WHERE book_id=? ORDER BY position,id", id)
	if err != nil {
		return library.Book{}, fmt.Errorf("query book sections: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item library.BookSection
		if err := rows.Scan(&item.ID, &item.Title, &item.Position); err != nil {
			_ = rows.Close()
			return library.Book{}, fmt.Errorf("scan book section: %w", err)
		}

		book.Sections = append(book.Sections, item)
	}

	if err := rows.Err(); err != nil {
		return library.Book{}, fmt.Errorf("iterate book sections: %w", err)
	}

	if err := rows.Close(); err != nil {
		return library.Book{}, fmt.Errorf("close book sections: %w", err)
	}

	rows, err = q.QueryContext(ctx, `
		SELECT entries.id, COALESCE(entries.section_id, ''), entries.entry_id,
		       entries.position, entries.note,
		       CASE WHEN recipes.deleted_at IS NULL THEN 'active' ELSE 'trash' END
		FROM book_entries AS entries
		JOIN library_recipes AS recipes ON recipes.id = entries.entry_id
		WHERE entries.book_id = ?
		ORDER BY COALESCE(entries.section_id, ''), entries.position, entries.id`, id)
	if err != nil {
		return library.Book{}, fmt.Errorf("query book entries: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item library.BookEntry
		if err := rows.Scan(&item.ID, &item.SectionID, &item.EntryID, &item.Position, &item.Note, &item.State); err != nil {
			_ = rows.Close()
			return library.Book{}, fmt.Errorf("scan book entry: %w", err)
		}

		book.Entries = append(book.Entries, item)
	}

	if err := rows.Err(); err != nil {
		return library.Book{}, fmt.Errorf("iterate book entries: %w", err)
	}

	if err := rows.Close(); err != nil {
		return library.Book{}, fmt.Errorf("close book entries: %w", err)
	}

	return book, nil
}

func scanBook(source scanner) (library.Book, error) {
	var (
		book             library.Book
		created, updated string
		deletedAt        sql.NullString
	)
	if err := source.Scan(
		&book.ID, &book.Kind, &book.Title, &book.Subtitle, &book.Description,
		&book.CoverAssetID, &book.Author, &book.Editor, &book.Edition, &book.ISBN,
		&book.Publisher, &book.PublicationDate, &book.Revision, &created, &updated,
		&deletedAt,
	); err != nil {
		return library.Book{}, fmt.Errorf("scan book metadata: %w", err)
	}

	var err error

	book.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return library.Book{}, fmt.Errorf("parse book created_at: %w", err)
	}

	book.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return library.Book{}, fmt.Errorf("parse book updated_at: %w", err)
	}

	if deletedAt.Valid {
		value, parseErr := time.Parse(time.RFC3339Nano, deletedAt.String)
		if parseErr != nil {
			return library.Book{}, fmt.Errorf("parse book deleted_at: %w", parseErr)
		}

		book.DeletedAt = &value
	}

	return book, nil
}

// ListBookSourceRecipes returns recipes citing the publication book.
func (s *Store) ListBookSourceRecipes(ctx context.Context, id string) ([]library.BookSourceRecipe, error) {
	if err := s.scopedObject(ctx, "books", id, access.Viewer); err != nil {
		return nil, operationError("ListBookSourceRecipes", err)
	}

	var kind library.BookKind
	if err := s.db.QueryRowContext(ctx, "SELECT kind FROM books WHERE id = ? AND deleted_at IS NULL", id).Scan(&kind); errors.Is(err, sql.ErrNoRows) {
		return nil, library.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("read source book: %w", err)
	}

	result := []library.BookSourceRecipe{}
	if kind != library.BookPublication {
		return result, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT sources.entry_id, recipes.title, sources.page_start,
		       sources.page_end, sources.original_title
		FROM recipe_sources AS sources
		JOIN library_recipes AS recipes ON recipes.id = sources.entry_id
		WHERE sources.publication_book_id = ? AND recipes.deleted_at IS NULL
		ORDER BY sources.page_start, recipes.title COLLATE NOCASE, sources.entry_id`, id)
	if err != nil {
		return nil, fmt.Errorf("query sourced recipes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			item       library.BookSourceRecipe
			start, end sql.NullInt64
		)
		if err := rows.Scan(&item.EntryID, &item.Title, &start, &end, &item.OriginalTitle); err != nil {
			return nil, fmt.Errorf("scan sourced recipe: %w", err)
		}

		item.PageStart = countOrNil(start)

		item.PageEnd = countOrNil(end)

		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sourced recipes: %w", err)
	}

	return result, nil
}
