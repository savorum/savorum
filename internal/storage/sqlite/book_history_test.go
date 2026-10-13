package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/bookcodec"
	"github.com/savorum/savorum/internal/library"
)

// TestBookHistoryInvariantsHoldInTheDatabase gives books the same guarantees
// recipes have: the current book cannot change without a new revision, a
// retained revision may only record the head it creates, and the tracked head
// must be the current revision. Each is attempted directly in SQL.
func TestBookHistoryInvariantsHoldInTheDatabase(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	book := seedBook(ctx, t, store, "bhistory")

	t.Run("content cannot change without a new revision", func(t *testing.T) {
		_, err := store.db.ExecContext(ctx,
			`UPDATE books SET book_json = json_set(book_json, '$.title', 'Quietly renamed') WHERE id = ?`, book.ID)
		if err == nil {
			t.Fatal("the current book was rewritten without advancing its revision")
		}
	})

	t.Run("state cannot change without a new revision", func(t *testing.T) {
		_, err := store.db.ExecContext(ctx,
			`UPDATE books SET deleted_at = '2026-08-27T00:00:00.000Z' WHERE id = ?`, book.ID)
		if err == nil {
			t.Fatal("the current book was trashed without advancing its revision")
		}
	})

	t.Run("a revision may only record the head it creates", func(t *testing.T) {
		_, err := store.db.ExecContext(ctx, `
			INSERT INTO book_revisions (
				book_id, revision, cover_asset_id, book_json, book_schema_version,
				state, cause, source_revision, created_at
			) VALUES (?, ?, NULL, '{"schema_version":1,"id":"bhistory","kind":"personal","title":"Invented","sections":[],"entries":[]}', 1,
				'active', 'api', NULL, '2026-08-27T00:00:00.000Z')`, book.ID, book.Revision+1)
		if err == nil {
			t.Fatal("a revision recorded a book that was never the current book")
		}
	})

	t.Run("the tracked head must be the current revision", func(t *testing.T) {
		_, err := store.db.ExecContext(ctx,
			`UPDATE book_head_revisions SET revision = revision + 5 WHERE book_id = ?`, book.ID)
		if err == nil {
			t.Fatal("the head pointer was moved off the current revision")
		}
	})

	t.Run("revisions advance one at a time", func(t *testing.T) {
		_, err := store.db.ExecContext(ctx, `UPDATE books SET revision = revision + 3 WHERE id = ?`, book.ID)
		if err == nil {
			t.Fatal("a book revision jumped more than one step")
		}
	})

	t.Run("a retained revision cannot be rewritten", func(t *testing.T) {
		_, err := store.db.ExecContext(ctx, `UPDATE book_revisions SET cause = 'restore' WHERE book_id = ?`, book.ID)
		if err == nil {
			t.Fatal("a retained book revision was rewritten")
		}
	})
}

// TestBookRevisionRefusesAFutureSchema keeps this build from misreading, or
// silently overwriting, a book written by a later one.
func TestBookRevisionRefusesAFutureSchema(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	book := seedBook(ctx, t, store, "bfuture")

	// This is the state a later build would legitimately leave behind: a head
	// written under a schema this build does not know, with its own matching
	// revision. Both moves happen together, exactly as a real write would.
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := transaction.ExecContext(ctx,
		`UPDATE books SET book_schema_version = 2, revision = revision + 1 WHERE id = ?`, book.ID); err != nil {
		t.Fatalf("seeding a future-schema book: %v", err)
	}

	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO book_revisions (
			book_id, revision, cover_asset_id, book_json, book_schema_version,
			state, cause, source_revision, created_at
		)
		SELECT id, revision, cover_asset_id, book_json, book_schema_version, 'active', 'api', NULL,
		       '2026-08-27T00:00:00.000Z'
		FROM books WHERE id = ?`, book.ID); err != nil {
		t.Fatalf("seeding a future-schema book revision: %v", err)
	}

	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}

	if _, err := store.GetBook(ctx, book.ID, library.StateActive); !errors.Is(err, bookcodec.ErrUnsupportedSchemaVersion) {
		t.Fatalf("reading a future-schema book = %v", err)
	}

	book.Title = "Attempted downgrade overwrite"

	_, err = store.ReplaceBook(ctx, book.Revision+1, book)
	if !errors.Is(err, bookcodec.ErrUnsupportedSchemaVersion) {
		t.Fatalf("overwriting a future-schema book = %v", err)
	}

	var title string
	if err := store.db.QueryRowContext(ctx, "SELECT title FROM books WHERE id = ?", book.ID).Scan(&title); err != nil {
		t.Fatal(err)
	}

	if title == "Attempted downgrade overwrite" {
		t.Fatal("a future-schema book was overwritten by a build that cannot read it")
	}
}

func seedBook(ctx context.Context, t *testing.T, store *Store, id string) library.Book {
	t.Helper()

	timestamp := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)

	book := library.Book{
		ID: id, Kind: library.BookPersonal, Title: "Weeknight Favorites",
		Sections: []library.BookSection{{ID: id + "s1", Title: "Quick", Position: 0}},
		Entries:  []library.BookEntry{},
		Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp,
	}
	if err := store.CreateBook(ctx, book); err != nil {
		t.Fatal(err)
	}

	return book
}
