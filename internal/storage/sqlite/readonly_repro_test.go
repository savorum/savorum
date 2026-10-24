//go:build stress

package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	sqliteDriver "modernc.org/sqlite"
)

func TestReadCancellationDoesNotPoisonWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cancel.db")
	db := sql.OpenDB(&capacityConnector{dsn: path, path: path, base: &sqliteDriver.Driver{}})

	db.SetMaxOpenConns(1)
	defer db.Close()

	if _, err := db.ExecContext(t.Context(), "CREATE TABLE probe (value INTEGER)"); err != nil {
		t.Fatal(err)
	}

	for i := range 20000 {
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() { time.Sleep(time.Duration(i%30) * time.Microsecond); cancel(); close(done) }()

		var n int

		_ = db.QueryRowContext(ctx, "SELECT 1").Scan(&n)

		<-done

		if _, err := db.ExecContext(t.Context(), "INSERT INTO probe VALUES (1)"); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
}
