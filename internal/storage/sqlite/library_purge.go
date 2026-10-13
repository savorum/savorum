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
)

func advanceBooksForRecipeEntries(ctx context.Context, transaction *sql.Tx, entryIDs []library.EntryID, timestamp time.Time) error {
	bookIDs, err := bookIDsForRecipeEntries(ctx, transaction, entryIDs)
	if err != nil {
		return err
	}

	return advanceBooksByID(ctx, transaction, bookIDs, timestamp)
}

func bookIDsForRecipeEntries(ctx context.Context, transaction *sql.Tx, entryIDs []library.EntryID) ([]string, error) {
	if len(entryIDs) == 0 {
		return []string{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(entryIDs)), ",")

	args := make([]any, 0, len(entryIDs))
	for _, entryID := range entryIDs {
		args = append(args, entryID)
	}

	rows, err := transaction.QueryContext(ctx, `SELECT DISTINCT book_id FROM book_entries WHERE entry_id IN (`+placeholders+`) ORDER BY book_id`, args...) //nolint:gosec // the concatenated text is placeholders and column constants; every value is a bound argument
	if err != nil {
		return nil, fmt.Errorf("query books affected by recipe lifecycle: %w", err)
	}
	defer rows.Close()

	result := []string{}

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan book affected by recipe lifecycle: %w", err)
		}

		result = append(result, id)
	}

	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("iterate books affected by recipe lifecycle: %w", err)
	}

	return result, nil
}

func advanceBooksByID(ctx context.Context, transaction *sql.Tx, bookIDs []string, timestamp time.Time) error {
	// Each affected book needs its projection rewritten and one immutable
	// revision inserted, so the transaction deliberately does O(bookIDs) work;
	// callers deduplicate IDs before this function.
	for _, bookID := range bookIDs {
		state := library.StateActive

		book, err := readBookProjection(ctx, transaction, bookID, state)
		if errors.Is(err, library.ErrNotFound) {
			state = library.StateTrash
			book, err = readBookProjection(ctx, transaction, bookID, state)
		}

		if err != nil {
			return fmt.Errorf("read book affected by recipe lifecycle: %w", err)
		}
		// A purged member has left book_entries but is still named by the book
		// document, so the document is rewritten from what the book now holds
		// rather than merely having its revision advanced.
		if err := writeBookHead(ctx, transaction, book, book.Revision, timestamp); err != nil {
			return fmt.Errorf("advance book for recipe lifecycle: %w", err)
		}

		if err := insertBookRevision(ctx, transaction, bookID, library.RevisionMetadata{Cause: library.RevisionCauseRecipeLifecycle}, timestamp); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) checkPurgeScope(ctx context.Context, targets []library.PurgeTarget, books []library.BookPurgeTarget) error {
	if err := s.authorizeScope(ctx, access.Editor); err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	recipeIDs := make([]string, 0, len(targets))
	for _, t := range targets {
		recipeIDs = append(recipeIDs, string(t.ID))
	}

	if err := s.scopedObjectsWith(ctx, s.db, "library_recipes", recipeIDs); err != nil {
		return err
	}

	bookIDs := make([]string, 0, len(books))
	for _, t := range books {
		bookIDs = append(bookIDs, t.ID)
	}

	return s.scopedObjectsWith(ctx, s.db, "books", bookIDs)
}

func auditRecipePurges(ctx context.Context, tx *sql.Tx, targets []library.PurgeTarget) error {
	for _, target := range targets {
		if err := auditIdentity(ctx, tx, attributionActor(ctx), string(target.ID), "recipe.purge"); err != nil {
			return err
		}
	}

	return nil
}
