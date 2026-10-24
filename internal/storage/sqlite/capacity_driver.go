//nolint:wrapcheck // Driver adapters preserve SQLite errors and io.EOF contracts.
package sqlite

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/savorum/savorum/internal/storagebudget"
)

func controlSQL(q string) string {
	var out strings.Builder

	quoted := byte(0)

	for i := 0; i < len(q); i++ {
		ch := q[i]
		if quoted != 0 {
			out.WriteByte(ch)

			if ch == quoted {
				if i+1 < len(q) && q[i+1] == quoted {
					i++
					out.WriteByte(q[i])
				} else {
					quoted = 0
				}
			}

			continue
		}

		if ch == '\'' || ch == '"' || ch == '`' {
			quoted = ch
			out.WriteByte(ch)

			continue
		}

		if ch == '-' && i+1 < len(q) && q[i+1] == '-' {
			for i < len(q) && q[i] != '\n' {
				i++
			}

			out.WriteByte(' ')

			continue
		}

		if ch == '/' && i+1 < len(q) && q[i+1] == '*' {
			i += 2
			for i+1 < len(q) && (q[i] != '*' || q[i+1] != '/') {
				i++
			}

			i++

			out.WriteByte(' ')

			continue
		}

		out.WriteByte(ch)
	}

	return strings.TrimSpace(strings.TrimSuffix(strings.Join(strings.Fields(strings.ToUpper(out.String())), " "), ";"))
}

func pragmaName(statement string) string {
	statement = strings.TrimSpace(statement)
	if !strings.HasPrefix(statement, "PRAGMA ") {
		return ""
	}

	name := strings.TrimSpace(strings.TrimPrefix(statement, "PRAGMA "))
	if i := strings.IndexAny(name, "=( "); i >= 0 {
		name = name[:i]
	}

	return name
}

func connectionPragmas(q string) bool {
	for statement := range strings.SplitSeq(q, ";") {
		name := pragmaName(statement)
		valid := slices.Contains([]string{"FOREIGN_KEYS", "LEGACY_ALTER_TABLE", "BUSY_TIMEOUT", "SYNCHRONOUS", "WAL_AUTOCHECKPOINT", "JOURNAL_SIZE_LIMIT", "JOURNAL_MODE"}, name)

		if !valid {
			return false
		}
	}

	return true
}

func policyPragma(q string) bool {
	for statement := range strings.SplitSeq(q, ";") {
		name := pragmaName(statement)
		if (name == "MAX_PAGE_COUNT" || name == "CACHE_SPILL" || name == "TEMP_STORE" || name == "TEMP_STORE_DIRECTORY" || name == "HARD_HEAP_LIMIT") && strings.ContainsAny(statement, "=(") {
			return true
		}
	}

	return false
}

type cleanupKey struct{}

func capacityCleanup(ctx context.Context) context.Context {
	return context.WithValue(ctx, cleanupKey{}, true)
}

type capacityConnector struct {
	dsn, path string
	base      driver.Driver
	budget    *storagebudget.Coordinator
	headroom  bool
	reserve   func(string, uint64, uint64, uint64) (func(), error)
}

func (c *capacityConnector) Driver() driver.Driver { return c.base }
func (c *capacityConnector) Connect(ctx context.Context) (driver.Conn, error) {
	raw, err := c.Driver().Open(c.dsn)
	if err != nil {
		return nil, operationError("open SQLite connection", err)
	}

	queryer, ok := raw.(driver.QueryerContext)
	if !ok {
		_ = raw.Close()
		return nil, errors.New("SQLite driver requires context queries")
	}

	execer, ok := raw.(driver.ExecerContext)
	if !ok {
		_ = raw.Close()
		return nil, errors.New("SQLite driver requires context execution")
	}

	beginner, ok := raw.(driver.ConnBeginTx)
	if !ok {
		_ = raw.Close()
		return nil, errors.New("SQLite driver requires context transactions")
	}

	return &capacityConn{Conn: raw, policy: c, queryer: queryer, execer: execer, beginner: beginner}, nil
}

type capacityConn struct {
	driver.Conn
	policy                       *capacityConnector
	queryer                      driver.QueryerContext
	execer                       driver.ExecerContext
	beginner                     driver.ConnBeginTx
	release                      func()
	baseline, maxPages, pageSize int64
	cleanup                      bool
	freeBaseline                 int64
	usedBaseline                 int64
	failure                      error
	discard                      bool
}

func (c *capacityConn) query(ctx context.Context, q string) (driver.Rows, error) {
	return c.queryer.QueryContext(ctx, q, nil)
}

func (c *capacityConn) exec(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return c.execer.ExecContext(ctx, q, args)
}

func (c *capacityConn) number(ctx context.Context, q string) (int64, error) {
	r, err := c.query(ctx, q)
	if err != nil {
		return 0, err
	}
	defer r.Close()

	v := make([]driver.Value, 1)
	if err = r.Next(v); err != nil {
		return 0, err
	}

	n, ok := v[0].(int64)
	if !ok {
		return 0, fmt.Errorf("invalid SQLite page accounting %v", v[0])
	}

	return n, nil
}

func (c *capacityConn) start(ctx context.Context) error {
	if c.failure != nil {
		return c.failure
	}

	if c.release != nil {
		return nil
	}

	held, release, err := c.policy.budget.Acquire(ctx)
	if err != nil {
		return err
	}

	c.release = release
	fail := func(err error) error { c.finish(); return c.translate(err) }

	c.baseline, err = c.number(held, "PRAGMA page_count")
	if err != nil {
		return fail(err)
	}

	c.pageSize, err = c.number(held, "PRAGMA page_size")
	if err != nil {
		return fail(err)
	}

	if c.pageSize <= 0 || c.baseline < 0 {
		return fail(errors.New("invalid SQLite page size"))
	}

	cleanupRequested, _ := ctx.Value(cleanupKey{}).(bool)
	c.cleanup = false

	c.maxPages = 2147483646
	if c.policy.budget != nil {
		if err := c.applyDurableLimit(held, cleanupRequested); err != nil {
			return fail(err)
		}
	}

	if c.cleanup {
		c.maxPages = c.baseline
	}

	if c.policy.headroom {
		if err := c.configureHeadroom(held, cleanupRequested, release); err != nil {
			return fail(err)
		}
	}

	if c.cleanup {
		c.freeBaseline, err = c.number(held, "PRAGMA freelist_count")
		if err != nil {
			return fail(err)
		}

		if c.policy.budget != nil {
			c.usedBaseline, err = c.number(held, "SELECT COALESCE(sum(pgsize-unused),0) FROM dbstat")
			if err != nil {
				return fail(err)
			}
		}
	}

	if c.policy.budget != nil || c.policy.headroom {
		if _, err = c.exec(held, "PRAGMA max_page_count="+strconv.FormatInt(c.maxPages, 10), nil); err != nil {
			return fail(err)
		}
	}

	return nil
}

func (c *capacityConn) applyDurableLimit(ctx context.Context, cleanupRequested bool) error {
	remaining, err := c.policy.budget.RemainingDatabase(ctx)
	if err != nil && !errors.Is(err, storagebudget.ErrCapacity) {
		return err
	}

	if remaining < c.baseline*c.pageSize {
		if !cleanupRequested {
			return storagebudget.ErrCapacity
		}

		c.cleanup = true
	}

	c.maxPages = max(c.baseline, remaining/c.pageSize)

	return nil
}

func (c *capacityConn) configureHeadroom(ctx context.Context, cleanupRequested bool, release func()) error {
	c.maxPages = min(c.maxPages, c.baseline+int64(sqliteTransactionGrowthBytes)/c.pageSize)

	physical, err := beginSQLiteHeadroom(c.policy.path, uint64(c.baseline), uint64(c.pageSize), uint64(c.maxPages), c.cleanup, c.policy.reserve) //nolint:gosec // start validates nonnegative SQLite page accounting.
	if err != nil && isCapacityError(err) && cleanupRequested && !c.cleanup {
		c.cleanup = true
		c.maxPages = c.baseline
		physical, err = beginSQLiteHeadroom(c.policy.path, uint64(c.baseline), uint64(c.pageSize), uint64(c.maxPages), true, c.policy.reserve) //nolint:gosec // start validates nonnegative SQLite page accounting.
	}

	if err != nil {
		return err
	}

	c.release = func() { physical(); release() }
	if _, err = c.exec(ctx, "PRAGMA cache_spill=OFF", nil); err != nil {
		return err
	}

	return nil
}

func (c *capacityConn) finish() {
	if c.release != nil {
		c.release()
		c.release = nil
	}
}

func (c *capacityConn) check(ctx context.Context) error {
	pages, err := c.number(ctx, "PRAGMA page_count")
	if err != nil {
		return err
	}

	if pages > c.maxPages {
		return storagebudget.ErrCapacity
	}

	if c.policy.budget != nil && !c.cleanup {
		if err := c.manifestBound(ctx); err != nil {
			return err
		}
	}

	if c.cleanup && c.policy.budget != nil {
		used, err := c.number(ctx, "SELECT COALESCE(sum(pgsize-unused),0) FROM dbstat")
		if err != nil {
			return err
		}

		if used > c.usedBaseline {
			return storagebudget.ErrCapacity
		}
	}

	if c.cleanup {
		free, err := c.number(ctx, "PRAGMA freelist_count")
		if err != nil {
			return err
		}

		if free < c.freeBaseline {
			return storagebudget.ErrCapacity
		}
	}

	return nil
}

func (c *capacityConn) manifestBound(ctx context.Context) error {
	rows, err := c.query(ctx, "SELECT id,storage_key,content_hash,size,media_type FROM assets WHERE storage_key IS NOT NULL ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()

	total := storagebudget.NativeManifestHeader

	value := make([]driver.Value, 5)
	for {
		err = rows.Next(value)
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return err
		}

		id, ok := value[0].(string)
		if !ok {
			return errors.New("invalid asset identity encoding")
		}

		key, ok := value[1].(string)
		if !ok {
			return errors.New("invalid asset path encoding")
		}

		hash, ok := value[2].(string)
		if !ok {
			return errors.New("invalid asset digest encoding")
		}

		size, ok := value[3].(int64)
		if !ok {
			return errors.New("invalid asset size encoding")
		}

		media, ok := value[4].(string)
		if !ok {
			return errors.New("invalid asset media type encoding")
		}

		bytes, err := storagebudget.NativeAssetEncodedBytes(storagebudget.NativeAssetRecord{ID: id, Path: "assets/" + key, SHA256: hash, Size: size, MediaType: media})
		if err != nil {
			return err
		}

		if bytes > storagebudget.NativeManifestBytes()-total {
			return storagebudget.ErrCapacity
		}

		total += bytes
	}
}

func (c *capacityConn) rollbackResult(err error) {
	if err != nil && !strings.Contains(err.Error(), "no transaction is active") {
		c.discard = true
	}
}

func (c *capacityConn) rollback(ctx context.Context) {
	_, err := c.exec(context.WithoutCancel(ctx), "ROLLBACK", nil)
	c.rollbackResult(err)
}

func (c *capacityConn) memoryFailure(err error) error {
	var coded interface{ Code() int }

	aborted := errors.As(err, &coded) && (coded.Code()&255 == 7 || coded.Code()&255 == 9 || coded.Code()&255 == 13)
	if aborted || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		translated := c.translate(err)
		if c.release != nil {
			c.failure = translated
			c.rollback(context.Background())
			c.finish()
		}

		return translated
	}

	return c.translate(err)
}

func (c *capacityConn) translate(err error) error {
	if err == nil {
		return nil
	}

	var coded interface{ Code() int }
	if errors.As(err, &coded) && coded.Code()&255 == 7 {
		return fmt.Errorf("%w: %w", ErrMemoryCapacity, err)
	}

	if (c.policy.budget != nil || c.policy.headroom) && errors.As(err, &coded) && coded.Code()&255 == 13 {
		return fmt.Errorf("%w: %w", storagebudget.ErrCapacity, err)
	}

	return err
}

func (c *capacityConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *capacityConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	if err := c.start(ctx); err != nil {
		return nil, c.translate(err)
	}

	tx, err := c.beginner.BeginTx(ctx, o)
	if err != nil {
		c.finish()
		return nil, c.translate(err)
	}

	return &capacityTx{Tx: tx, c: c, ctx: ctx}, nil
}

//nolint:containedctx // Commit retains transaction cancellation through the final accounting check.
type capacityTx struct {
	driver.Tx
	c   *capacityConn
	ctx context.Context
}

func (t *capacityTx) Commit() error {
	defer t.c.finish()
	defer func() { t.c.failure = nil }()

	if t.c.failure != nil {
		t.c.rollbackResult(t.Tx.Rollback())
		return t.c.failure
	}

	if err := t.c.check(t.ctx); err != nil {
		t.c.rollbackResult(t.Tx.Rollback())
		return t.c.translate(err)
	}

	err := t.Tx.Commit()
	if err != nil {
		t.c.rollback(t.ctx)
	}

	return t.c.translate(err)
}

func (t *capacityTx) Rollback() error {
	defer t.c.finish()
	defer func() { t.c.failure = nil }()

	err := t.Tx.Rollback()
	t.c.rollbackResult(err)

	if t.c.failure != nil {
		return nil
	}

	return err
}

func (c *capacityConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	command := controlSQL(q)
	if c.failure != nil && command != "ROLLBACK" && command != "ROLLBACK TRANSACTION" && command != "COMMIT" && command != "COMMIT TRANSACTION" && command != "END" && command != "END TRANSACTION" {
		return nil, c.failure
	}

	if policyPragma(command) {
		return nil, errors.New("SQLite capacity pragmas are managed by the writer fence")
	}

	if connectionPragmas(command) {
		return c.exec(ctx, q, args)
	}

	if (strings.HasPrefix(command, "BEGIN") || strings.HasPrefix(command, "COMMIT") || strings.HasPrefix(command, "ROLLBACK") || strings.HasPrefix(command, "END")) && strings.Contains(command, ";") {
		return nil, errors.New("multiple transaction control statements are not supported")
	}

	if result, handled, err := c.controlExec(ctx, command, q, args); handled {
		return result, err
	}

	if c.release != nil || readSQL(q) {
		if c.release == nil {
			if err := c.enableReadOnly(ctx); err != nil {
				return nil, c.translate(err)
			}
			defer func() { _ = c.resetReadOnly() }()
		}

		r, err := c.exec(ctx, q, args)

		return r, c.memoryFailure(err)
	}

	if err := c.start(ctx); err != nil {
		return nil, c.translate(err)
	}
	defer c.finish()

	if _, err := c.exec(ctx, "BEGIN IMMEDIATE", nil); err != nil {
		return nil, c.translate(err)
	}

	r, err := c.exec(ctx, q, args)
	if err == nil {
		err = c.check(ctx)
	}

	if err != nil {
		c.rollback(ctx)
		return nil, c.translate(err)
	}

	_, err = c.exec(ctx, "COMMIT", nil)
	if err != nil {
		c.rollback(ctx)
	}

	return r, c.translate(err)
}

func (c *capacityConn) controlExec(ctx context.Context, command, q string, args []driver.NamedValue) (driver.Result, bool, error) {
	if strings.HasPrefix(command, "VACUUM") || strings.HasPrefix(command, "PRAGMA WAL_CHECKPOINT") {
		r, err := c.maintenanceExec(ctx, command, q, args)
		return r, true, err
	}

	if strings.HasPrefix(command, "BEGIN") {
		if err := c.start(ctx); err != nil {
			return nil, true, err
		}

		r, err := c.exec(ctx, q, args)
		if err != nil {
			c.finish()
		}

		return r, true, c.translate(err)
	}

	if command == "COMMIT" || command == "COMMIT TRANSACTION" || command == "END" || command == "END TRANSACTION" {
		defer c.finish()
		defer func() { c.failure = nil }()

		if c.failure != nil {
			return nil, true, c.failure
		}

		if err := c.check(ctx); err != nil {
			c.rollback(ctx)
			return nil, true, c.translate(err)
		}

		r, err := c.exec(ctx, q, args)
		if err != nil {
			c.rollback(ctx)
		}

		return r, true, c.translate(err)
	}

	if command == "ROLLBACK" || command == "ROLLBACK TRANSACTION" {
		defer c.finish()
		defer func() { c.failure = nil }()

		if c.failure != nil {
			return driver.RowsAffected(0), true, nil
		}

		r, err := c.exec(ctx, q, args)

		return r, true, err
	}

	return nil, false, nil
}

func (c *capacityConn) maintenanceExec(ctx context.Context, command, q string, args []driver.NamedValue) (driver.Result, error) {
	ctx = capacityCleanup(ctx)
	if err := c.start(ctx); err != nil {
		return nil, c.translate(err)
	}
	defer c.finish()

	if strings.HasPrefix(command, "VACUUM") && command != "VACUUM" {
		return nil, errors.New("only plain VACUUM is supported by capacity admission")
	}

	if command == "VACUUM" && c.policy.headroom {
		temp, err := c.vacuumTempDirectory(ctx)
		if err != nil {
			return nil, c.translate(err)
		}

		release, err := beginSQLiteVacuumHeadroom(c.policy.path, uint64(c.baseline), uint64(c.pageSize), c.cleanup, temp) //nolint:gosec // start validates nonnegative SQLite page accounting.
		if err != nil {
			return nil, c.translate(err)
		}
		defer release()
	}

	r, err := c.exec(ctx, q, args)

	return r, c.translate(err)
}

func (c *capacityConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if c.failure != nil {
		return nil, c.failure
	}

	command := controlSQL(q)
	if policyPragma(command) {
		return nil, errors.New("SQLite capacity pragmas are managed by the writer fence")
	}

	if strings.HasPrefix(command, "BEGIN") || strings.HasPrefix(command, "COMMIT") || strings.HasPrefix(command, "ROLLBACK") || strings.HasPrefix(command, "END") {
		return nil, errors.New("transaction control requires ExecContext")
	}

	if strings.HasPrefix(command, "PRAGMA WAL_CHECKPOINT") {
		ctx = capacityCleanup(ctx)
		if err := c.start(ctx); err != nil {
			return nil, c.translate(err)
		}

		r, err := c.queryer.QueryContext(ctx, q, args)
		if err != nil {
			c.finish()
			return nil, c.translate(err)
		}

		return &releaseRows{Rows: r, release: c.finish}, nil
	}

	if c.release == nil && readSQL(q) {
		return c.readQuery(ctx, q, args)
	}

	if c.release != nil {
		r, err := c.queryer.QueryContext(ctx, q, args)
		if err != nil {
			return nil, c.memoryFailure(err)
		}

		return &transactionRows{Rows: r, c: c}, nil
	}

	if err := c.start(ctx); err != nil {
		return nil, c.translate(err)
	}

	if _, err := c.exec(ctx, "BEGIN IMMEDIATE", nil); err != nil {
		c.finish()
		return nil, c.translate(err)
	}

	r, err := c.queryer.QueryContext(ctx, q, args)
	if err != nil {
		c.rollback(ctx)
		c.finish()

		return nil, c.translate(err)
	}

	return &capacityRows{Rows: r, c: c, ctx: ctx}, nil
}

func (c *capacityConn) vacuumTempDirectory(ctx context.Context) (string, error) {
	rows, err := c.query(ctx, "PRAGMA temp_store_directory")
	if err != nil {
		return "", err
	}
	defer rows.Close()

	value := make([]driver.Value, 1)
	if err = rows.Next(value); errors.Is(err, io.EOF) {
		return "", nil
	} else if err != nil {
		return "", err
	}

	path, ok := value[0].(string)
	if !ok {
		return "", errors.New("invalid SQLite temporary directory")
	}

	return path, nil
}

func (c *capacityConn) enableReadOnly(ctx context.Context) error {
	if _, err := c.exec(ctx, "PRAGMA query_only=ON", nil); err != nil {
		// Cancellation can report failure after SQLite applies the pragma.
		return errors.Join(err, c.resetReadOnly())
	}

	return nil
}

func (c *capacityConn) resetReadOnly() error {
	_, err := c.exec(context.Background(), "PRAGMA query_only=OFF", nil)
	if err != nil {
		c.discard = true
	}

	return err
}

func (c *capacityConn) readQuery(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.enableReadOnly(ctx); err != nil {
		return nil, c.translate(err)
	}

	rows, err := c.queryer.QueryContext(ctx, q, args)
	if err != nil {
		_ = c.resetReadOnly()
		return nil, c.translate(err)
	}

	return &readonlyRows{Rows: rows, c: c}, nil
}

type transactionRows struct {
	driver.Rows
	c *capacityConn
}

func (r *transactionRows) Next(v []driver.Value) error {
	if r.c.failure != nil {
		return r.c.failure
	}

	return r.c.memoryFailure(r.Rows.Next(v))
}

func (r *transactionRows) Close() error {
	err := r.c.memoryFailure(r.Rows.Close())
	return errors.Join(r.c.failure, err)
}

type readonlyRows struct {
	driver.Rows
	c *capacityConn
}

func (r *readonlyRows) Close() error {
	err := r.Rows.Close()

	reset := r.c.resetReadOnly()

	return r.c.translate(errors.Join(err, reset))
}

func (r *readonlyRows) Next(v []driver.Value) error { return r.c.translate(r.Rows.Next(v)) }

type releaseRows struct {
	driver.Rows
	release func()
}

func (r *releaseRows) Close() error { defer r.release(); return r.Rows.Close() }

//nolint:containedctx // RETURNING rows retain transaction cancellation until Close.
type capacityRows struct {
	driver.Rows
	c       *capacityConn
	ctx     context.Context
	failure error
}

func (r *capacityRows) Next(v []driver.Value) error {
	err := r.Rows.Next(v)
	if err != nil && !errors.Is(err, io.EOF) {
		r.failure = err
	}

	return r.c.translate(err)
}

func (r *capacityRows) Close() error {
	defer r.c.finish()
	defer func() { r.c.failure = nil }()

	err := errors.Join(r.failure, r.Rows.Close())
	if err == nil {
		err = r.c.check(r.ctx)
	}

	if err != nil {
		r.c.rollback(r.ctx)
		return r.c.translate(err)
	}

	_, err = r.c.exec(r.ctx, "COMMIT", nil)
	if err != nil {
		r.c.rollback(r.ctx)
	}

	return r.c.translate(err)
}

func (c *capacityConn) Prepare(q string) (driver.Stmt, error) { return &capacityStmt{c: c, q: q}, nil }

func (c *capacityConn) PrepareContext(_ context.Context, q string) (driver.Stmt, error) {
	return c.Prepare(q)
}

type capacityStmt struct {
	c *capacityConn
	q string
}

func (*capacityStmt) Close() error  { return nil }
func (*capacityStmt) NumInput() int { return -1 }
func named(v []driver.Value) []driver.NamedValue {
	r := make([]driver.NamedValue, len(v))
	for i, x := range v {
		r[i] = driver.NamedValue{Ordinal: i + 1, Value: x}
	}

	return r
}

func (s *capacityStmt) Exec(v []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), named(v))
}

func (s *capacityStmt) Query(v []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), named(v))
}

func (s *capacityStmt) ExecContext(ctx context.Context, v []driver.NamedValue) (driver.Result, error) {
	return s.c.ExecContext(ctx, s.q, v)
}

func (s *capacityStmt) QueryContext(ctx context.Context, v []driver.NamedValue) (driver.Rows, error) {
	return s.c.QueryContext(ctx, s.q, v)
}
func (c *capacityConn) IsValid() bool { return !c.discard && c.failure == nil }
func (c *capacityConn) Close() error  { defer c.finish(); return c.Conn.Close() }
