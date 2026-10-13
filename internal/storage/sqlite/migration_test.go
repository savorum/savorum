package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipecodec"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestFreshDatabaseIsTheCompleteCurrentSchema(t *testing.T) {
	t.Parallel()

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}

	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	var (
		version int
		name    string
	)
	if err := store.db.QueryRowContext(t.Context(), "SELECT version, name FROM schema_migrations ORDER BY version DESC LIMIT 1").Scan(&version, &name); err != nil {
		t.Fatal(err)
	}

	latest := migrations[len(migrations)-1]
	if version != latest.version || name != latest.name {
		t.Fatalf("migration ledger = version %d name %q", version, name)
	}

	rows, err := store.db.QueryContext(t.Context(), `
		SELECT name FROM sqlite_master
		WHERE type = 'table'
		  AND name NOT LIKE 'sqlite_%'
		  AND (name = 'library_recipes_fts' OR name NOT LIKE 'library_recipes_fts_%')
		  AND (name = 'recipe_ingredient_names_fts' OR name NOT LIKE 'recipe_ingredient_names_fts_%')
		ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var tables []string

	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}

		tables = append(tables, table)
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"hosting_tenant", "hosting_erasures", "hosting_member_operations",
		"api_idempotency", "automation_jobs", "automation_results", "domain_events", "event_consumers", "event_horizons", "pantry_entries", "webhook_attempts", "webhook_deliveries", "webhook_subscriptions",
		"assets", "audit_events", "auth_providers", "federation_transactions", "federation_replays", "native_sign_in_grants", "native_link_tickets", "native_link_grants", "passkeys", "webauthn_ceremonies", "recovery_codes", "credential_rate_limits", "invitations", "provider_group_mappings", "personal_tokens", "book_entries", "book_head_revisions", "book_revisions",
		"book_sections", "books", "facets", "import_events", "import_job_items", "import_jobs", "instance_identity", "instance_settings",
		"library_recipes", "library_recipes_fts", "recipe_cookings", "recipe_facets", "recipe_imported_cook_count_baselines", "recipe_source_import_baseline_assets", "recipe_source_import_baselines",
		"recipe_head_revisions", "recipe_organization_state", "recipe_revision_assets",
		"recipe_revisions", "recipe_source_attachments", "recipe_sources", "recipe_tags", "recipe_user_state",
		"recipe_author_names", "recipe_author_entries", "recipe_author_search_keys",
		"recipe_name_terms", "recipe_name_term_entries", "recipe_name_term_keys",
		"recipe_ingredient_names", "recipe_ingredient_names_fts",
		"saved_views", "schema_migrations", "tags", "shopping_lists", "shopping_operations", "shopping_activity", "calendar_feeds", "hosting_member_revocations",
		"users", "identities", "browser_sessions", "authentication_backoff", "identity_audit", "libraries", "library_memberships", "library_assets", "user_preferences",
		"meals", "meal_dishes", "meal_dish_plain_items", "saved_meals", "saved_meal_dishes", "saved_meal_dish_plain_items",
	}
	sort.Strings(want)

	if !reflect.DeepEqual(tables, want) {
		t.Fatalf("fresh schema tables = %#v, want %#v", tables, want)
	}

	for table, wantColumns := range map[string][]string{
		"instance_identity": {
			"created_at", "instance_id", "singleton",
		},
		"instance_settings": {
			"allow_external_fetch", "allow_file_uploads", "default_locale",
			"default_measurement", "default_print_image", "default_print_page_size",
			"display_name", "public_url", "revision", "singleton", "updated_at",
		},
		"library_recipes": {
			"active_time", "authors_json", "created_at", "deleted_at", "description",
			"document_id", "document_json", "document_schema_version", "id", "language", "library_id",
			"primary_asset_id", "revision", "search_text", "title", "total_time", "translations_json", "updated_at",
		},
		"recipe_user_state": {
			"entry_id", "favorite", "last_cooked", "personal_note", "preferred_scale",
			"preferred_yield", "preferred_yield_assertion", "rating", "times_cooked", "user_id",
		},
		"recipe_cookings": {
			"cooked_on", "cooked_time", "created_at", "entry_id", "id", "note", "scale", "session_id", "user_id",
		},
		"recipe_revisions": {
			"actor_id", "cause", "created_at", "document_json", "document_schema_version", "entry_id",
			"revision", "source_revision", "state",
		},
		"recipe_source_attachments": {
			"asset_id", "entry_id", "filename", "media_type", "revision", "size",
		},
		"recipe_source_import_baselines": {
			"baseline_revision", "entry_id", "source_json", "source_sha256", "updated_at",
		},
		"recipe_source_import_baseline_assets": {
			"asset_id", "baseline_revision", "entry_id",
		},
		"books": {
			"author", "book_json", "book_schema_version", "cover_asset_id", "created_at",
			"deleted_at", "description", "edition", "editor", "id", "isbn", "kind", "library_id",
			"publication_date", "publisher", "revision", "subtitle", "title", "updated_at",
		},
		"book_revisions": {
			"actor_id", "book_id", "book_json", "book_schema_version", "cause", "cover_asset_id",
			"created_at", "revision", "source_revision", "state",
		},
	} {
		columns, err := tableColumns(t.Context(), store, table)
		if err != nil {
			t.Fatal(err)
		}

		gotColumns := make([]string, 0, len(columns))
		for column := range columns {
			gotColumns = append(gotColumns, column)
		}

		sort.Strings(gotColumns)
		sort.Strings(wantColumns)

		if !reflect.DeepEqual(gotColumns, wantColumns) {
			t.Errorf("fresh %s columns = %#v, want %#v", table, gotColumns, wantColumns)
		}
	}

	var integrity string
	if err := store.db.QueryRowContext(t.Context(), "PRAGMA integrity_check").Scan(&integrity); err != nil {
		t.Fatal(err)
	}

	if integrity != "ok" {
		t.Fatalf("fresh database integrity check = %q", integrity)
	}

	foreignKeyRows, err := store.db.QueryContext(t.Context(), "PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer foreignKeyRows.Close()

	if foreignKeyRows.Next() {
		t.Fatal("fresh database contains a foreign-key violation")
	}

	if err := foreignKeyRows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestReleasedMigrationChecksumsAndSequenceAreFrozen(t *testing.T) {
	t.Parallel()

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}

	if migrations[0].version != 1 || migrations[0].name != "library" {
		t.Fatalf("first migration = %#v", migrations[0])
	}

	for index, migration := range migrations {
		if migration.version != index+1 {
			t.Fatalf("migration %d has version %d", index, migration.version)
		}

		if len(migration.checksum) != sha256.Size*2 {
			t.Fatalf("migration %04d checksum = %q", migration.version, migration.checksum)
		}
	}
}

func TestFailedInitialMigrationRollsBackSchemaAndLedger(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "initial-failure.db")

	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}

	store := &Store{db: &writerDB{raw: database}, dsn: path}

	t.Cleanup(func() { _ = store.Close() })

	contents := `
		CREATE TABLE migration_failure_probe (id INTEGER PRIMARY KEY);
		INSERT INTO table_that_does_not_exist (id) VALUES (1);`
	digest := sha256.Sum256([]byte(contents))

	err = store.applyMigration(ctx, migration{
		version: 1, name: "initial_failure", contents: contents,
		checksum: hex.EncodeToString(digest[:]),
	}, true)
	if err == nil {
		t.Fatal("initial migration unexpectedly succeeded")
	}

	empty, err := databaseHasNoSchema(ctx, database)
	if err != nil {
		t.Fatal(err)
	}

	if !empty {
		t.Fatalf("failed initial migration left schema behind: %#v", databaseSchema(t, database))
	}
}

func TestFailedMigrationRollsBackSchemaAndLedger(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "failure.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}

	version := migrations[len(migrations)-1].version + 1
	contents := `
		CREATE TABLE migration_failure_probe (id INTEGER PRIMARY KEY);
		INSERT INTO migration_failure_probe (id) VALUES (1);
		INSERT INTO table_that_does_not_exist (id) VALUES (1);`
	digest := sha256.Sum256([]byte(contents))

	err = store.applyMigration(ctx, migration{
		version: version, name: "failure_probe", contents: contents,
		checksum: hex.EncodeToString(digest[:]),
	}, false)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("apply migration %04d", version)) {
		t.Fatalf("migration error = %v", err)
	}

	var tableCount int
	if err := store.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_schema
		WHERE type = 'table' AND name = 'migration_failure_probe'`).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}

	if tableCount != 0 {
		t.Fatal("failed migration left its table behind")
	}

	var ledgerCount int
	if err := store.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version,
	).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}

	if ledgerCount != 0 {
		t.Fatal("failed migration was recorded as applied")
	}
}

func TestStoreRefusesFutureDatabaseWithoutWriting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "future.db")

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}

	futureVersion := migrations[len(migrations)-1].version + 1
	if _, err := database.ExecContext(ctx, `
		INSERT INTO schema_migrations (version, name, checksum, applied_at)
		VALUES (?, 'future', 'future', '2099-01-01T00:00:00Z')`, futureVersion); err != nil {
		_ = database.Close()

		t.Fatal(err)
	}

	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	before := databaseDigest(t, path)

	opened, err := Open(ctx, path)
	if opened != nil {
		_ = opened.Close()

		t.Fatal("future database unexpectedly opened")
	}

	if !errors.Is(err, ErrDatabaseTooNew) {
		t.Fatalf("future database error = %v", err)
	}

	after := databaseDigest(t, path)
	if before != after {
		t.Fatalf("future database changed during refusal: before %s after %s", before, after)
	}
}

func TestStoreRefusesUnversionedSchemaWithoutWriting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "unversioned.db")

	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := database.ExecContext(ctx, "CREATE TABLE foreign_schema (id INTEGER PRIMARY KEY)"); err != nil {
		_ = database.Close()

		t.Fatal(err)
	}

	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	before := databaseDigest(t, path)

	opened, err := Open(ctx, path)
	if opened != nil {
		_ = opened.Close()

		t.Fatal("unversioned database unexpectedly opened")
	}

	if err == nil || !strings.Contains(err.Error(), "no migration ledger") {
		t.Fatalf("unversioned database error = %v", err)
	}

	if after := databaseDigest(t, path); before != after {
		t.Fatalf("unversioned database changed during refusal: before %s after %s", before, after)
	}
}

func databaseDigest(t *testing.T, path string) string {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	digest := sha256.Sum256(contents)

	return hex.EncodeToString(digest[:])
}

func databaseSchema(t *testing.T, database interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
},
) []string {
	t.Helper()

	rows, err := database.QueryContext(t.Context(), `
		SELECT type || ':' || name || ':' || COALESCE(sql, '')
		FROM sqlite_schema
		WHERE name NOT LIKE 'sqlite_%'
		ORDER BY type, name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var schema []string

	for rows.Next() {
		var definition string
		if err := rows.Scan(&definition); err != nil {
			t.Fatal(err)
		}

		schema = append(schema, definition)
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	return schema
}

func tableColumns(ctx context.Context, store *Store, table string) (map[string]bool, error) {
	rows, err := store.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return nil, fmt.Errorf("inspect table %q columns: %w", table, err)
	}
	defer rows.Close()

	columns := map[string]bool{}

	for rows.Next() {
		var (
			cid, notNull, primaryKey int
			name, kind               string
			defaultValue             any
		)
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, fmt.Errorf("read column for table %q: %w", table, err)
		}

		columns[name] = true
	}

	if err := rows.Err(); err != nil {
		return columns, fmt.Errorf("iterate columns for table %q: %w", table, err)
	}

	return columns, nil
}

func TestStoreFailsSafelyWithoutOverwritingFutureSchema(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "future.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	timestamp := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	document := testfixture.Document()

	entry := library.Entry{ID: "rfuture", Recipe: document, Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}
	if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	encoded, _, err := recipecodec.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	future := advanceToUnsupportedSchemaRevision(ctx, t, store, entry.ID, string(encoded), recipecodec.CurrentVersion+1, timestamp.Add(time.Minute))

	if _, err := store.Get(ctx, entry.ID, library.StateActive); !errors.Is(err, recipecodec.ErrUnsupportedSchemaVersion) {
		t.Fatalf("future-schema read error = %v", err)
	}

	document.Title = "Attempted downgrade overwrite"

	_, err = store.Revise(ctx, library.RevisionCommand{
		ID: entry.ID, ExpectedRevision: future, Recipe: document,
		CurrentState: library.StateActive, TargetState: library.StateActive,
		Timestamp: timestamp.Add(2 * time.Minute),
	})
	if !errors.Is(err, recipecodec.ErrUnsupportedSchemaVersion) {
		t.Fatalf("future-schema overwrite error = %v", err)
	}

	var (
		title         string
		storedVersion int
		revision      uint64
	)
	if err := store.db.QueryRowContext(ctx, `SELECT title, document_schema_version, revision FROM library_recipes WHERE id = ?`, entry.ID).Scan(&title, &storedVersion, &revision); err != nil {
		t.Fatal(err)
	}

	if title != entry.Recipe.Title || storedVersion != recipecodec.CurrentVersion+1 || revision != future {
		t.Fatalf("future-schema row changed: title=%q version=%d revision=%d", title, storedVersion, revision)
	}
}

func TestPartiallyMigratedDatabaseUpgradesToCurrentSchema(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	fresh, err := Open(ctx, filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = fresh.Close() })
	wantSchema := databaseSchema(t, fresh.db)

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}

	for applied := 1; applied < len(migrations); applied++ {
		t.Run(fmt.Sprintf("from %04d", applied), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "partial.db")

			database, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}

			partial := &Store{db: &writerDB{raw: database}, dsn: path, path: path}
			for index, migration := range migrations[:applied] {
				if err := partial.applyMigration(ctx, migration, index == 0); err != nil {
					_ = database.Close()

					t.Fatal(err)
				}
			}

			if err := database.Close(); err != nil {
				t.Fatal(err)
			}

			store, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()

			version, err := store.SchemaVersion(ctx)
			if err != nil {
				t.Fatal(err)
			}

			if version != migrations[len(migrations)-1].version {
				t.Fatalf("schema version = %d, want %d", version, migrations[len(migrations)-1].version)
			}

			if got := databaseSchema(t, store.db); !reflect.DeepEqual(got, wantSchema) {
				t.Fatalf("migrated schema differs from fresh schema\nmigrated: %#v\nfresh: %#v", got, wantSchema)
			}
		})
	}
}
