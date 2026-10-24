package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"testing"

	sqliteDriver "modernc.org/sqlite"
)

// pragmaFailure models cancellation reported after SQLite applies a pragma.
type pragmaFailure struct {
	driver.ExecerContext
	failEnable bool
	failReset  bool
}

func (e *pragmaFailure) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if query == "PRAGMA query_only=OFF" && e.failReset {
		return nil, context.Canceled
	}

	result, err := e.ExecerContext.ExecContext(ctx, query, args)
	if err == nil && query == "PRAGMA query_only=ON" && e.failEnable {
		return nil, context.Canceled
	}

	return result, operationError("execute pragma fixture", err)
}

func TestReadOnlyPragmaCleanup(t *testing.T) {
	for _, operation := range []string{"query", "exec"} {
		for _, failEnable := range []bool{false, true} {
			for _, failReset := range []bool{false, true} {
				name := operation
				if failEnable {
					name += "/enable-error"
				} else {
					name += "/enable-success"
				}

				if failReset {
					name += "/reset-error"
				} else {
					name += "/reset-success"
				}

				t.Run(name, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "cleanup.db")
					db := sql.OpenDB(&capacityConnector{dsn: path, path: path, base: &sqliteDriver.Driver{}})

					db.SetMaxOpenConns(1)
					defer db.Close()

					if _, err := db.ExecContext(t.Context(), "CREATE TABLE probe (value INTEGER)"); err != nil {
						t.Fatal(err)
					}

					conn, err := db.Conn(t.Context())
					if err != nil {
						t.Fatal(err)
					}

					var guarded *capacityConn

					if err = conn.Raw(func(raw any) error {
						var ok bool

						guarded, ok = raw.(*capacityConn)
						if !ok {
							return errors.New("expected capacity connection")
						}

						guarded.execer = &pragmaFailure{ExecerContext: guarded.execer, failEnable: failEnable, failReset: failReset}

						return nil
					}); err != nil {
						t.Fatal(err)
					}

					if err = conn.Close(); err != nil {
						t.Fatal(err)
					}

					if operation == "query" {
						var value int

						err = db.QueryRowContext(t.Context(), "SELECT 1").Scan(&value)
					} else {
						_, err = db.ExecContext(t.Context(), "SELECT 1")
					}

					if failEnable && !errors.Is(err, context.Canceled) {
						t.Fatalf("expected cancellation, got %v", err)
					}

					if !failEnable && (!failReset || operation == "exec") && err != nil {
						t.Fatalf("read: %v", err)
					}

					if failReset && guarded.IsValid() {
						t.Fatal("connection with failed cleanup remains reusable")
					}

					if _, err = db.ExecContext(t.Context(), "INSERT INTO probe VALUES (1)"); err != nil {
						t.Fatalf("write after read: %v", err)
					}
				})
			}
		}
	}
}
