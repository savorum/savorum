package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"modernc.org/libc"
	sqliteDriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func TestSQLiteAllocatorSubprocess(t *testing.T) {
	for _, mode := range []string{"default", "accounting", "late-accounting", "autocommit", "prepared", "explicit", "manual", "returning", "explicit-returning", "delayed-next", "delayed-close"} {
		t.Run(mode, func(t *testing.T) {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			//nolint:gosec // The subprocess runs this test executable with fixed arguments.
			command := exec.CommandContext(t.Context(), executable, "-test.run=^TestSQLiteAllocatorChild$")

			command.Env = append(os.Environ(), "SAVORUM_SQLITE_MEMORY_TEST="+mode)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, output)
			}
		})
	}
}

func TestSQLiteAllocatorChild(t *testing.T) {
	mode := os.Getenv("SAVORUM_SQLITE_MEMORY_TEST")
	if mode == "" {
		t.Skip("fresh process helper")
	}

	ctx := t.Context()

	tls := libc.NewTLS()
	defer tls.Close()

	if mode == "default" {
		if limit := sqlite3.Xsqlite3_hard_heap_limit64(tls, -1); limit != 0 {
			t.Fatalf("unconfigured process limit=%d", limit)
		}

		return
	}

	if mode == "accounting" {
		if err := ConfigureMemoryBudget(8 << 20); err != nil {
			t.Fatal(err)
		}

		if err := ConfigureMemoryBudget(8 << 20); err != nil {
			t.Fatal(err)
		}

		if err := ConfigureMemoryBudget(16 << 20); err == nil {
			t.Fatal("conflicting process budget accepted")
		}

		before := sqlite3.Xsqlite3_memory_used(tls)

		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()

		if _, err := db.ExecContext(ctx, "CREATE TABLE payload(v); INSERT INTO payload VALUES(zeroblob(1048576))"); err != nil {
			t.Fatal(err)
		}

		if after := sqlite3.Xsqlite3_memory_used(tls); after <= before+(1<<19) {
			t.Fatalf("accounting did not rise: %d -> %d", before, after)
		}

		if actual := sqlite3.Xsqlite3_hard_heap_limit64(tls, -1); actual != 8<<20 {
			t.Fatalf("heap limit=%d", actual)
		}

		return
	}

	if mode == "late-accounting" {
		sqlite3.Xsqlite3_initialize(tls)

		allocator.accountingError = enableMemoryAccounting()
		if allocator.accountingError == nil {
			t.Fatal("late accounting configuration accepted")
		}

		if err := ConfigureMemoryBudget(8 << 20); err == nil {
			t.Fatal("failed accounting initialization accepted")
		}

		return
	}

	path := filepath.Join(t.TempDir(), "memory.db")

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}

	raw.SetMaxOpenConns(1)

	if _, err := raw.ExecContext(ctx, "PRAGMA journal_mode=WAL; CREATE TABLE payload(id INTEGER PRIMARY KEY,v BLOB)"); err != nil {
		t.Fatal(err)
	}

	seed, err := raw.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	for range 4096 {
		if _, err := seed.ExecContext(ctx, "INSERT INTO payload(v) VALUES(zeroblob(3072))"); err != nil {
			t.Fatal(err)
		}
	}

	if err := seed.Commit(); err != nil {
		t.Fatal(err)
	}

	if _, err := raw.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}

	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := ConfigureMemoryBudget(8 << 20); err != nil {
		t.Fatal(err)
	}

	reservations := 0
	db := sql.OpenDB(&capacityConnector{dsn: path, path: path, base: &sqliteDriver.Driver{}, headroom: true, reserve: func(string, uint64, uint64, uint64) (func(), error) {
		reservations++
		return func() { reservations-- }, nil
	}})

	db.SetMaxOpenConns(1)
	defer db.Close()

	before := memorySnapshot(t, db)
	update := "UPDATE payload SET v=randomblob(3072)"

	var failure error

	switch mode {
	case "prepared":
		stmt, err := db.PrepareContext(ctx, update)
		if err != nil {
			t.Fatal(err)
		}

		_, failure = stmt.ExecContext(ctx)
		defer func() {
			if err := stmt.Close(); err != nil {
				t.Error(err)
			}
		}()
	case "autocommit":
		_, failure = db.ExecContext(ctx, update)
	case "returning":
		failure = consumeMemoryRows(db.QueryContext(ctx, update+" RETURNING id"))
	case "explicit", "explicit-returning":
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}

		if _, err = tx.ExecContext(ctx, "UPDATE payload SET v=randomblob(3072) WHERE id=1"); err != nil {
			t.Fatal(err)
		}

		if mode == "explicit-returning" {
			failure = consumeMemoryRows(tx.QueryContext(ctx, update+" RETURNING id"))
		} else {
			_, failure = tx.ExecContext(ctx, update)
		}

		if _, err = tx.ExecContext(ctx, "UPDATE payload SET v=randomblob(3072) WHERE id=2"); !errors.Is(err, ErrMemoryCapacity) {
			t.Fatalf("poisoned write: %v", err)
		}

		if err = tx.Commit(); !errors.Is(err, ErrMemoryCapacity) {
			t.Fatalf("poisoned commit: %v", err)
		}
	case "delayed-next", "delayed-close":
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}

		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}

		if _, err = tx.ExecContext(ctx, "UPDATE payload SET v=randomblob(3072) WHERE id=1"); err != nil {
			t.Fatal(err)
		}

		if err = conn.Raw(func(raw any) error {
			capacity, ok := raw.(*capacityConn)
			if !ok {
				return fmt.Errorf("unexpected connection type %T", raw)
			}

			rows := &transactionRows{Rows: &memoryFailureRows{closeFailure: mode == "delayed-close"}, c: capacity}
			if mode == "delayed-next" {
				failure = rows.Next(make([]driver.Value, 1))
				if err := rows.Close(); !errors.Is(err, ErrMemoryCapacity) {
					return fmt.Errorf("close failed memory rows: %w", err)
				}
			} else {
				failure = rows.Close()
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		if err = tx.Commit(); !errors.Is(err, ErrMemoryCapacity) {
			t.Fatalf("delayed failed commit: %v", err)
		}

		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	case "manual":
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
			t.Fatal(err)
		}

		if _, err = conn.ExecContext(ctx, "UPDATE payload SET v=randomblob(3072) WHERE id=1"); err != nil {
			t.Fatal(err)
		}

		_, failure = conn.ExecContext(ctx, update)
		if _, err = conn.ExecContext(ctx, "UPDATE payload SET v=randomblob(3072) WHERE id=2"); !errors.Is(err, ErrMemoryCapacity) {
			t.Fatalf("poisoned manual write: %v", err)
		}

		if _, err = conn.ExecContext(ctx, "COMMIT"); !errors.Is(err, ErrMemoryCapacity) {
			t.Fatalf("poisoned manual commit: %v", err)
		}

		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	}

	var coded interface{ Code() int }
	if !errors.Is(failure, ErrMemoryCapacity) || !errors.As(failure, &coded) || coded.Code()&255 != 7 {
		t.Fatalf("want recoverable SQLITE_NOMEM, got %v", failure)
	}

	if reservations != 0 {
		t.Fatalf("reservation leak: %d", reservations)
	}

	if after := memorySnapshot(t, db); after != before {
		t.Fatal("failed transaction changed full dataset")
	}

	recovery, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}

	if snapshot := memorySnapshot(t, recovery); snapshot != before {
		t.Fatal("reopened full dataset changed")
	}

	if err := recovery.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err = db.ExecContext(ctx, "UPDATE payload SET v=randomblob(3072) WHERE id=1"); err != nil {
		t.Fatalf("small retry: %v", err)
	}

	if reservations != 0 {
		t.Fatalf("retry reservation leak: %d", reservations)
	}

	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	var integrity string
	if err = reopened.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity=%s error=%v", integrity, err)
	}
}

func consumeMemoryRows(rows *sql.Rows, err error) error {
	if err != nil {
		return err
	}

	for rows.Next() {
		var value int64
		if err = rows.Scan(&value); err != nil {
			break
		}
	}

	return errors.Join(err, rows.Err(), rows.Close())
}

func memorySnapshot(t *testing.T, db *sql.DB) [32]byte {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), "SELECT id,v FROM payload ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	digest := sha256.New()

	for rows.Next() {
		var (
			id   int64
			data []byte
		)
		if err := rows.Scan(&id, &data); err != nil {
			t.Fatal(err)
		}

		fmt.Fprintf(digest, "%d:", id)
		digest.Write(data)
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	var result [32]byte
	copy(result[:], digest.Sum(nil))

	return result
}

type memoryTestNoMemory struct{}

func (memoryTestNoMemory) Error() string { return "injected SQLite allocator refusal" }
func (memoryTestNoMemory) Code() int     { return 7 }

type memoryFailureRows struct{ closeFailure bool }

func (*memoryFailureRows) Columns() []string { return []string{"id"} }
func (r *memoryFailureRows) Next([]driver.Value) error {
	if r.closeFailure {
		return io.EOF
	}

	return memoryTestNoMemory{}
}

func (r *memoryFailureRows) Close() error {
	if r.closeFailure {
		return memoryTestNoMemory{}
	}

	return nil
}

func TestSQLiteCommitAccountingNoMemoryDiscardsUncertainConnection(t *testing.T) {
	releases := 0
	conn := &capacityConn{policy: &capacityConnector{}, queryer: memoryCheckFailure{}, release: func() { releases++ }}
	tx := &capacityTx{Tx: memoryRollbackFailure{}, c: conn, ctx: context.Background()}
	err := tx.Commit()

	var coded interface{ Code() int }
	if !errors.Is(err, ErrMemoryCapacity) || !errors.As(err, &coded) || coded.Code() != 7 {
		t.Fatalf("commit accounting refusal: %v", err)
	}

	if conn.IsValid() || releases != 1 || conn.release != nil {
		t.Fatalf("uncertain connection reused: valid=%v releases=%d", conn.IsValid(), releases)
	}

	conn.finish()

	if releases != 1 {
		t.Fatal("reservation released twice")
	}
}

type memoryCheckFailure struct{}

func (memoryCheckFailure) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, memoryTestNoMemory{}
}

type memoryRollbackFailure struct{}

func (memoryRollbackFailure) Commit() error   { return nil }
func (memoryRollbackFailure) Rollback() error { return errors.New("rollback failed") }

func TestSQLiteManualCommitAccountingNoMemoryIsRecoverable(t *testing.T) {
	for _, command := range []string{"COMMIT", "END"} {
		t.Run(command, func(t *testing.T) {
			releases := 0
			conn := &capacityConn{policy: &capacityConnector{}, queryer: memoryCheckFailure{}, execer: memoryRollbackExecer{}, release: func() { releases++ }}
			_, handled, err := conn.controlExec(context.Background(), command, command, nil)

			var coded interface{ Code() int }
			if !handled || !errors.Is(err, ErrMemoryCapacity) || !errors.As(err, &coded) || coded.Code() != 7 {
				t.Fatalf("manual commit accounting refusal: %v", err)
			}

			if conn.IsValid() || releases != 1 || conn.release != nil {
				t.Fatalf("uncertain connection reused: valid=%v releases=%d", conn.IsValid(), releases)
			}
		})
	}
}

type memoryRollbackExecer struct{}

func (memoryRollbackExecer) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errors.New("rollback failed")
}
