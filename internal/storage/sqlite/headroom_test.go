package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"path/filepath"
	"testing"

	sqliteDriver "modernc.org/sqlite"
)

func TestSQLiteHeadroomIncludesExistingDirtyPagesAndCheckpointGrowth(t *testing.T) {
	got, err := sqliteHeadroomBounds(100, 4096, 120)
	if err != nil {
		t.Fatal(err)
	}

	want := uint64(32 + 120*(4096+24) + 20*4096)
	if got != want {
		t.Fatalf("reservation = %d, want %d", got, want)
	}
	// A transaction with no growth still needs WAL space for updates or deletes.
	got, err = sqliteHeadroomBounds(100, 4096, 100)
	if err != nil || got != 32+100*(4096+24) {
		t.Fatalf("no-growth bounds: %d, %v", got, err)
	}
}

func TestSQLiteHeadroomRejectsInvalidAndOverflowBounds(t *testing.T) {
	for _, args := range [][3]uint64{{1, 0, 1}, {1, 513, 1}, {2, 4096, 1}, {1, 131072, 1}, {0, 65536, math.MaxUint64}, {0, 512, math.MaxUint64 / 1024}} {
		if _, err := sqliteHeadroomBounds(args[0], args[1], args[2]); err == nil {
			t.Fatalf("accepted bounds %v", args)
		}
	}
}

func TestSQLiteHeadroomFullTransactionRollsBack(t *testing.T) {
	ctx := context.Background()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bounded.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)

	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA cache_spill=OFF", "CREATE TABLE payload (value BLOB)", "PRAGMA max_page_count=8"} {
		if _, err = db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = tx.ExecContext(ctx, "INSERT INTO payload VALUES (zeroblob(1048576))")

	var full *sqliteDriver.Error
	if !errors.As(err, &full) || full.Code()&255 != 13 {
		t.Fatalf("expected SQLITE_FULL, got %v", err)
	}

	_ = tx.Rollback()

	var count int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM payload").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial transaction: count=%d err=%v", count, err)
	}

	var integrity string
	if err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity=%s err=%v", integrity, err)
	}
}
