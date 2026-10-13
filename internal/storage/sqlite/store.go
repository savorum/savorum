package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/savorum/savorum/internal/storage/filepolicy"
	"github.com/savorum/savorum/internal/storagebudget"

	// Registers the cgo-free SQLite driver this package opens by name.
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql migration_checksums.txt
var migrationFiles embed.FS

var migrationNamePattern = regexp.MustCompile(`^([0-9]{4})_([a-z0-9_]+)\.sql$`)

// ErrDatabaseTooNew means opening stopped during the read-only preflight
// because the database contains a migration this binary does not understand.
var ErrDatabaseTooNew = errors.New("database schema is newer than this Savorum version")

const createMigrationLedger = `
	CREATE TABLE schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		checksum   TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`

// Store persists Savorum's library data in one SQLite database.
type Store struct {
	db     *writerDB
	dsn    string
	path   string
	policy filepolicy.Policy
}

// OpenOption configures a Store while it is opened.
type OpenOption func(*openOptions)

type openOptions struct {
	budget   *storagebudget.Coordinator
	headroom bool
	reserve  func(string, uint64, uint64, uint64) (func(), error)
	policy   filepolicy.Policy
}

// WithFilePolicy applies policy to the database and its SQLite sidecar files.
func WithFilePolicy(policy filepolicy.Policy) OpenOption {
	return func(options *openOptions) { options.policy = policy }
}

// WithDurableBudget applies one shared hosted database and originals ceiling.
func WithDurableBudget(budget *storagebudget.Coordinator) OpenOption {
	return func(options *openOptions) { options.budget = budget }
}

// WithSQLiteHeadroom bounds SQLite WAL and checkpoint scratch reservations.
func WithSQLiteHeadroom() OpenOption {
	return func(options *openOptions) { options.headroom = true }
}

// Open secures, migrates, and opens the SQLite database at path.
func Open(ctx context.Context, path string, options ...OpenOption) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is required")
	}

	if err := registerSearchFunctions(); err != nil {
		return nil, err
	}

	settings := openOptions{policy: filepolicy.OwnerPolicy()}
	for _, option := range options {
		option(&settings)
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}

	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String()

	migrations, err := loadMigrations()
	if err != nil {
		return nil, err
	}

	if err := settings.policy.PrepareFile(absolute); err != nil {
		return nil, fmt.Errorf("secure SQLite database: %w", err)
	}

	if err := preflightDatabase(ctx, absolute, dsn, migrations); err != nil {
		return nil, err
	}

	dsn = runtimeDSN(dsn)

	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	// A single connection makes connection-scoped pragmas deterministic and is
	// sufficient for the expected write profile. WAL still permits readers in
	// other processes while this connection is writing.
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	store := &Store{db: &writerDB{raw: database}, dsn: dsn, path: absolute, policy: settings.policy}
	if err := store.configureConnection(ctx); err != nil {
		_ = database.Close()
		return nil, err
	}

	if settings.budget != nil || settings.headroom {
		if err := store.configureDurability(ctx); err != nil {
			_ = database.Close()
			return nil, err
		}
	}

	if err := store.installCapacityFence(settings); err != nil {
		_ = store.db.Close()
		return nil, err
	}

	if err := store.migrate(ctx, migrations); err != nil {
		_ = store.db.Close()
		return nil, err
	}

	if err := store.configureDurability(ctx); err != nil {
		_ = store.db.Close()
		return nil, err
	}

	if err := store.secureDatabaseFiles(); err != nil {
		_ = store.db.Close()
		return nil, err
	}

	if err := store.rebuildProjections(ctx); err != nil {
		_ = store.db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) installCapacityFence(settings openOptions) error {
	if settings.budget != nil || settings.headroom {
		if err := s.db.raw.Close(); err != nil {
			return operationError("install SQLite capacity fence", err)
		}

		guarded := sql.OpenDB(&capacityConnector{dsn: s.dsn, path: s.path, base: s.db.raw.Driver(), budget: settings.budget, headroom: settings.headroom, reserve: settings.reserve})
		guarded.SetMaxOpenConns(1)
		guarded.SetMaxIdleConns(1)

		s.db = &writerDB{raw: guarded, budget: settings.budget}
		if settings.budget != nil {
			settings.budget.SetDatabaseUsage(func(ctx context.Context) (int64, error) {
				var pages, size int64
				if err := s.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
					return 0, operationError("read SQLite allocated bytes", err)
				}

				if err := s.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&size); err != nil {
					return 0, operationError("read SQLite allocated bytes", err)
				}

				return pages * size, nil
			})
		}
	}

	return nil
}

func (s *Store) secureDatabaseFiles() error {
	for _, path := range []string{s.path, s.path + "-wal", s.path + "-shm", s.path + "-journal"} {
		if err := s.policy.SecureFile(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("secure SQLite file %q: %w", path, err)
		}
	}

	return nil
}

// Close releases the database and its connections.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close SQLite store: %w", err)
	}

	return nil
}

// Ping verifies that the database can serve a request with ctx.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping SQLite database: %w", err)
	}

	return nil
}

// SchemaVersion reports the newest migration recorded in this database.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return 0, fmt.Errorf("read database schema version: %w", err)
	}

	return version, nil
}

// CurrentSchemaVersion reports the newest database schema understood by this
// build without opening or changing an instance database.
func CurrentSchemaVersion() (int, error) {
	migrations, err := loadMigrations()
	if err != nil {
		return 0, err
	}

	return migrations[len(migrations)-1].version, nil
}

func (s *Store) configureConnection(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 2000",
	}
	for _, statement := range pragmas {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure SQLite with %q: %w", statement, err)
		}
	}

	return nil
}

func (s *Store) configureDurability(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = FULL",
	}
	for _, statement := range pragmas {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure SQLite with %q: %w", statement, err)
		}
	}

	return nil
}

type migration struct {
	version  int
	name     string
	contents string
	checksum string
}

func (s *Store) migrate(ctx context.Context, migrations []migration) error {
	ledgerExists, err := migrationLedgerExists(ctx, s.db)
	if err != nil {
		return err
	}

	if !ledgerExists {
		empty, err := databaseHasNoSchema(ctx, s.db)
		if err != nil {
			return err
		}

		if !empty {
			return errors.New("database contains schema objects but no migration ledger")
		}

		if err := s.applyMigration(ctx, migrations[0], true); err != nil {
			return err
		}
	}

	applied, err := s.appliedMigrations(ctx)
	if err != nil {
		return err
	}

	if err := validateAppliedMigrations(applied, migrations); err != nil {
		return err
	}

	for _, migration := range migrations {
		if _, ok := applied[migration.version]; ok {
			continue
		}

		if err := s.applyMigration(ctx, migration, false); err != nil {
			return err
		}
	}

	return nil
}

func loadMigrations() ([]migration, error) {
	checksums, err := loadMigrationChecksums(migrationFiles)
	if err != nil {
		return nil, err
	}

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	migrations := make([]migration, 0, len(entries))

	seen := make(map[int]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			return nil, fmt.Errorf("migration directory contains unexpected directory %q", entry.Name())
		}

		matches := migrationNamePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			return nil, fmt.Errorf("migration filename %q is invalid", entry.Name())
		}

		version, err := strconv.Atoi(matches[1])
		if err != nil || version == 0 {
			return nil, fmt.Errorf("migration filename %q has an invalid version", entry.Name())
		}

		if _, duplicate := seen[version]; duplicate {
			return nil, fmt.Errorf("migration version %04d is duplicated", version)
		}

		seen[version] = struct{}{}

		contents, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}

		digest := sha256.Sum256(contents)
		checksum := hex.EncodeToString(digest[:])

		expectedChecksum, frozen := checksums[entry.Name()]
		if !frozen {
			return nil, fmt.Errorf("migration %q has no frozen checksum", entry.Name())
		}

		if checksum != expectedChecksum {
			return nil, fmt.Errorf("released migration %q differs from its frozen checksum", entry.Name())
		}

		delete(checksums, entry.Name())

		migrations = append(migrations, migration{
			version:  version,
			name:     matches[2],
			contents: string(contents),
			checksum: checksum,
		})
	}

	if len(checksums) != 0 {
		var missing []string
		for name := range checksums {
			missing = append(missing, name)
		}

		sort.Strings(missing)

		return nil, fmt.Errorf("frozen migration checksums have no SQL files: %s", strings.Join(missing, ", "))
	}

	sort.Slice(migrations, func(left, right int) bool {
		return migrations[left].version < migrations[right].version
	})

	if len(migrations) == 0 {
		return nil, errors.New("no database migrations are embedded")
	}

	for index, migration := range migrations {
		expectedVersion := index + 1
		if migration.version != expectedVersion {
			return nil, fmt.Errorf("migration sequence skips version %04d", expectedVersion)
		}
	}

	return migrations, nil
}

func loadMigrationChecksums(source fs.FS) (map[string]string, error) {
	contents, err := fs.ReadFile(source, "migration_checksums.txt")
	if err != nil {
		return nil, fmt.Errorf("read frozen migration checksums: %w", err)
	}

	checksums := make(map[string]string)

	for lineNumber, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 2 || migrationNamePattern.FindStringSubmatch(fields[0]) == nil || len(fields[1]) != sha256.Size*2 {
			return nil, fmt.Errorf("migration checksum line %d is invalid", lineNumber+1)
		}

		if _, duplicate := checksums[fields[0]]; duplicate {
			return nil, fmt.Errorf("migration checksum for %q is duplicated", fields[0])
		}

		if _, err := hex.DecodeString(fields[1]); err != nil {
			return nil, fmt.Errorf("migration checksum line %d is invalid: %w", lineNumber+1, err)
		}

		checksums[fields[0]] = fields[1]
	}

	return checksums, nil
}

type appliedMigration struct {
	name     string
	checksum string
}

func (s *Store) appliedMigrations(ctx context.Context) (map[int]appliedMigration, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT version, name, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("read migration ledger: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]appliedMigration)

	for rows.Next() {
		var (
			version  int
			name     string
			checksum string
		)
		if err := rows.Scan(&version, &name, &checksum); err != nil {
			return nil, fmt.Errorf("scan migration ledger: %w", err)
		}

		applied[version] = appliedMigration{name: name, checksum: checksum}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate migration ledger: %w", err)
	}

	return applied, nil
}

func (s *Store) applyMigration(ctx context.Context, migration migration, createLedger bool) error {
	// SQLite's documented table-rebuild procedure requires foreign keys disabled
	// before the transaction. The rebuilt schema is checked before committing.
	if migration.name == "libraries" {
		if _, err := s.db.ExecContext(ctx, "PRAGMA foreign_keys=OFF; PRAGMA legacy_alter_table=ON"); err != nil {
			return fmt.Errorf("prepare library schema rebuild: %w", err)
		}
		defer func() {
			_, _ = s.db.ExecContext(context.WithoutCancel(ctx), "PRAGMA legacy_alter_table=OFF; PRAGMA foreign_keys=ON")
		}()
	}

	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %04d: %w", migration.version, err)
	}
	defer transaction.Rollback()

	if createLedger {
		if _, err := transaction.ExecContext(ctx, createMigrationLedger); err != nil {
			return fmt.Errorf("create migration ledger: %w", err)
		}
	}

	if _, err := transaction.ExecContext(ctx, migration.contents); err != nil {
		return fmt.Errorf("apply migration %04d: %w", migration.version, err)
	}

	if _, err := transaction.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))",
		migration.version,
		migration.name,
		migration.checksum,
	); err != nil {
		return fmt.Errorf("record migration %04d: %w", migration.version, err)
	}

	if migration.name == "libraries" {
		if err := checkMigrationForeignKeys(ctx, transaction); err != nil {
			return err
		}
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit migration %04d: %w", migration.version, err)
	}

	return nil
}

func preflightDatabase(ctx context.Context, path, dsn string, migrations []migration) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("inspect SQLite database: %w", err)
	}

	if info.IsDir() {
		return errors.New("database path is a directory")
	}

	if info.Size() == 0 {
		return nil
	}

	readonlyURL, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("construct read-only SQLite database URL: %w", err)
	}

	query := readonlyURL.Query()
	query.Set("mode", "ro")
	readonlyURL.RawQuery = query.Encode()

	database, err := sql.Open("sqlite", readonlyURL.String())
	if err != nil {
		return fmt.Errorf("open SQLite database read-only: %w", err)
	}

	database.SetMaxOpenConns(1)

	database.SetMaxIdleConns(1)
	defer database.Close()

	ledgerExists, err := migrationLedgerExists(ctx, database)
	if err != nil {
		return err
	}

	if !ledgerExists {
		empty, err := databaseHasNoSchema(ctx, database)
		if err != nil {
			return err
		}

		if empty {
			return nil
		}

		return errors.New("database contains schema objects but no migration ledger")
	}

	store := &Store{db: &writerDB{raw: database}, dsn: readonlyURL.String()}

	applied, err := store.appliedMigrations(ctx)
	if err != nil {
		return err
	}

	return validateAppliedMigrations(applied, migrations)
}

type migrationQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func migrationLedgerExists(ctx context.Context, queryer migrationQueryer) (bool, error) {
	var exists bool
	if err := queryer.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM sqlite_schema
			WHERE type = 'table' AND name = 'schema_migrations'
		)`).Scan(&exists); err != nil {
		return false, fmt.Errorf("inspect migration ledger: %w", err)
	}

	return exists, nil
}

func databaseHasNoSchema(ctx context.Context, queryer migrationQueryer) (bool, error) {
	var count int
	if err := queryer.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_schema
		WHERE name NOT LIKE 'sqlite_%'`).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect database schema: %w", err)
	}

	return count == 0, nil
}

func validateAppliedMigrations(applied map[int]appliedMigration, migrations []migration) error {
	known := make(map[int]migration, len(migrations))
	for _, migration := range migrations {
		known[migration.version] = migration
	}

	versions := make([]int, 0, len(applied))
	for version := range applied {
		versions = append(versions, version)
	}

	sort.Ints(versions)

	for index, version := range versions {
		if version != index+1 {
			return fmt.Errorf("database migration ledger skips version %04d", index+1)
		}

		migration, ok := known[version]
		if !ok {
			if version > migrations[len(migrations)-1].version {
				return fmt.Errorf("%w: database contains migration %04d, supported through %04d", ErrDatabaseTooNew, version, migrations[len(migrations)-1].version)
			}

			return fmt.Errorf("database contains unknown migration %04d", version)
		}

		record := applied[version]
		if record.name != migration.name {
			return fmt.Errorf("migration %04d name differs from the applied migration", version)
		}

		if record.checksum != migration.checksum {
			return fmt.Errorf("migration %04d checksum differs from the applied migration", version)
		}
	}

	return nil
}

func checkMigrationForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("check migrated foreign keys: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		return errors.New("library migration violates foreign keys")
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("read foreign key check: %w", err)
	}

	return nil
}
