//nolint:wrapcheck,sqlclosecheck // This database boundary returns driver errors and row ownership to its callers.
package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"

	"github.com/savorum/savorum/internal/storagebudget"
)

// writerDB is the sole runtime database entry point. Writers acquire the shared
// lease before acquiring a pooled connection; readers do not need that lease.
type writerDB struct {
	raw    *sql.DB
	budget *storagebudget.Coordinator
}

func (d *writerDB) lease(ctx context.Context) (context.Context, func(), error) {
	return d.budget.Acquire(ctx)
}

func (d *writerDB) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	ctx, release, err := d.lease(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	return d.raw.ExecContext(ctx, q, args...)
}

func (d *writerDB) BeginTx(ctx context.Context, o *sql.TxOptions) (*sql.Tx, error) {
	ctx, release, err := d.lease(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	return d.raw.BeginTx(ctx, o)
}

func (d *writerDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	if readSQL(q) {
		return d.raw.QueryContext(ctx, q, args...)
	}

	ctx, release, err := d.lease(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	return d.raw.QueryContext(ctx, q, args...)
}

func (d *writerDB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	if readSQL(q) {
		return d.raw.QueryRowContext(ctx, q, args...)
	}

	held, release, err := d.lease(ctx)
	if err != nil {
		return d.raw.QueryRowContext(ctx, q, args...)
	}
	defer release()

	return d.raw.QueryRowContext(held, q, args...)
}

func (d *writerDB) Conn(ctx context.Context) (*writerConn, error) {
	ctx, release, err := d.lease(ctx)
	if err != nil {
		return nil, err
	}

	conn, err := d.raw.Conn(ctx)
	if err != nil {
		release()
		return nil, err
	}

	return &writerConn{Conn: conn, ctx: ctx, release: release}, nil
}

//nolint:containedctx // The connection retains its operation lease until Close.
type writerConn struct {
	*sql.Conn
	ctx         context.Context
	release     func()
	releaseOnce sync.Once
}

func (c *writerConn) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return c.Conn.ExecContext(storagebudget.WithLease(ctx, c.ctx), q, args...)
}

func (c *writerConn) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return c.Conn.QueryContext(storagebudget.WithLease(ctx, c.ctx), q, args...)
}

func (c *writerConn) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return c.Conn.QueryRowContext(storagebudget.WithLease(ctx, c.ctx), q, args...)
}

func (c *writerConn) Close() error                        { err := c.Conn.Close(); c.releaseOnce.Do(c.release); return err }
func (d *writerDB) Close() error                          { return d.raw.Close() }
func (d *writerDB) PingContext(ctx context.Context) error { return d.raw.PingContext(ctx) }
func (d *writerDB) SetConnMaxLifetime(t time.Duration)    { d.raw.SetConnMaxLifetime(t) }
func (d *writerDB) Stats() sql.DBStats                    { return d.raw.Stats() }
func readSQL(q string) bool {
	q = controlSQL(q)
	if strings.Contains(q, ";") {
		return false
	}

	if strings.HasPrefix(q, "PRAGMA WAL_CHECKPOINT") {
		return false
	}

	wordEnd := 0
	for wordEnd < len(q) && q[wordEnd] >= 'A' && q[wordEnd] <= 'Z' {
		wordEnd++
	}

	first := q[:wordEnd]
	if first == "SELECT" || first == "VALUES" {
		return true
	}

	if first == "PRAGMA" {
		return !strings.ContainsAny(q, "=(")
	}

	if first != "WITH" {
		return false
	}

	return withReadSQL(q)
}

func skipSQLQuote(q string, start int) int {
	closingQuote := q[start]
	if closingQuote == '[' {
		closingQuote = ']'
	}

	for i := start + 1; i < len(q); i++ {
		if q[i] != closingQuote {
			continue
		}

		if i+1 < len(q) && q[i+1] == closingQuote {
			i++
			continue
		}

		return i
	}

	return len(q) - 1
}

func withReadSQL(q string) bool {
	depth := 0

	for i := len("WITH "); i < len(q); i++ {
		switch q[i] {
		case '\'', '"', '`', '[':
			i = skipSQLQuote(q, i)
		case '(':
			depth++
		case ')':
			depth--
		default:
			if depth != 0 || q[i] < 'A' || q[i] > 'Z' {
				continue
			}

			start := i
			for i+1 < len(q) && ((q[i+1] >= 'A' && q[i+1] <= 'Z') || q[i+1] == '_') {
				i++
			}

			switch q[start : i+1] {
			case "SELECT", "VALUES":
				return true
			case "INSERT", "UPDATE", "DELETE", "REPLACE":
				return false
			}
		}
	}

	return false
}
