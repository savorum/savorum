package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
)

// Reconstruct the released v10 schema with the fixture's compatible columns.
// This is a disposable benchmark input, never an application downgrade path.
func migrationCheck(ctx context.Context, source, target, directory string, measure func(string, int, func() error) error) error {
	db, err := sql.Open("sqlite", target)
	if err != nil {
		return fmt.Errorf("open legacy fixture database: %w", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)

	if err := installLegacySchema(ctx, db, directory); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `ATTACH DATABASE ? AS source`, source); err != nil {
		return fmt.Errorf("attach scale fixture source: %w", err)
	}

	var recipes, revisions, books, favorites int64
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM source.library_recipes),(SELECT count(*) FROM source.recipe_revisions),(SELECT count(*) FROM source.book_revisions),(SELECT count(*) FROM source.recipe_user_state WHERE favorite=1 AND user_id=(SELECT id FROM source.users WHERE local_mode=1))`).Scan(&recipes, &revisions, &books, &favorites); err != nil {
		return fmt.Errorf("count legacy fixture history: %w", err)
	}

	tables, err := queryNames(ctx, db, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'library_recipes_fts%' AND name<>'schema_migrations'`)
	if err != nil {
		return err
	}

	// Loading historical snapshots is not a sequence of new application edits.
	// Suspend insert guards only in this disposable input, then restore the exact
	// released trigger definitions before exercising the real upgrade.
	triggers, err := queryTriggers(ctx, db)
	if err != nil {
		return err
	}

	for _, t := range triggers {
		if _, err := db.ExecContext(ctx, `DROP TRIGGER "`+t.name+`"`); err != nil {
			return fmt.Errorf("drop legacy trigger %q: %w", t.name, err)
		}
	}

	if err := copyLegacyTables(ctx, db, tables); err != nil {
		return err
	}

	for _, t := range triggers {
		if _, err := db.ExecContext(ctx, t.statement); err != nil {
			return fmt.Errorf("restore legacy trigger %q: %w", t.name, err)
		}
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO library_recipes_fts(library_recipes_fts) VALUES('rebuild')`); err != nil {
		return fmt.Errorf("rebuild legacy full-text index: %w", err)
	}

	if err := db.Close(); err != nil {
		return fmt.Errorf("close legacy fixture database: %w", err)
	}

	var upgraded *sqlite.Store

	if err := measure("migrations", 1, func() error { var e error; upgraded, e = sqlite.Open(ctx, target); return e }); err != nil {
		return err
	}

	defer upgraded.Close()

	if err := upgraded.IntegrityCheck(ctx); err != nil {
		return err
	}

	usage, err := upgraded.StorageUsage(ctx)
	if err != nil {
		return err
	}

	if usage.Recipes != recipes || usage.RecipeRevisions != revisions || usage.BookRevisions != books {
		return fmt.Errorf("upgrade changed retained history counts")
	}

	service, err := library.NewService(upgraded, units.DefaultCatalog())
	if err != nil {
		return fmt.Errorf("create library service for upgraded fixture: %w", err)
	}

	principal, err := upgraded.LocalPrincipal(ctx)
	if err != nil {
		return fmt.Errorf("resolve upgraded local actor: %w", err)
	}

	scoped := access.WithLibrary(identity.WithPrincipal(ctx, principal), "default")

	found, err := service.Count(scoped, library.ListOptions{Query: "saffron"})
	if err != nil {
		return err
	}

	if int64(found) != recipes {
		return fmt.Errorf("upgraded index found %d of %d recipes", found, recipes)
	}

	found, err = service.Count(scoped, library.ListOptions{Query: "favorite:true"})
	if err != nil {
		return err
	}

	if int64(found) != favorites {
		return fmt.Errorf("upgrade retained %d of %d local favorites", found, favorites)
	}

	return nil
}

func installLegacySchema(ctx context.Context, db *sql.DB, directory string) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,checksum TEXT NOT NULL,applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create legacy migration ledger: %w", err)
	}

	paths, err := filepath.Glob(filepath.Join(directory, "*.sql"))
	if err != nil {
		return fmt.Errorf("list migration files: %w", err)
	}

	for _, path := range paths {
		name := filepath.Base(path)

		var version int
		if _, err := fmt.Sscanf(name, "%d_", &version); err != nil {
			return fmt.Errorf("parse migration filename %q: %w", name, err)
		}

		if version > 10 {
			continue
		}

		data, err := os.ReadFile(path) //nolint:gosec // paths come from the configured migration directory glob
		if err != nil {
			return fmt.Errorf("read migration %q: %w", name, err)
		}

		if _, err := db.ExecContext(ctx, string(data)); err != nil {
			return fmt.Errorf("apply migration %q: %w", name, err)
		}

		ledgerName := strings.TrimSuffix(strings.SplitN(name, "_", 2)[1], ".sql")
		if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations VALUES(?,?,?,?)`, version, ledgerName, fmt.Sprintf("%x", sha256.Sum256(data)), time.Now().UTC().Format(time.RFC3339)); err != nil {
			return fmt.Errorf("record migration %q: %w", name, err)
		}
	}

	return nil
}

type rowQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func queryNames(ctx context.Context, queryer rowQueryer, query string, arguments ...any) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query schema names: %w", err)
	}
	defer rows.Close()

	var names []string

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan schema name: %w", err)
		}

		names = append(names, name)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema names: %w", err)
	}

	return names, nil
}

type trigger struct{ name, statement string }

func queryTriggers(ctx context.Context, db *sql.DB) ([]trigger, error) {
	rows, err := db.QueryContext(ctx, `SELECT name,sql FROM sqlite_master WHERE type='trigger'`)
	if err != nil {
		return nil, fmt.Errorf("query legacy triggers: %w", err)
	}
	defer rows.Close()

	var triggers []trigger

	for rows.Next() {
		var found trigger
		if err := rows.Scan(&found.name, &found.statement); err != nil {
			return nil, fmt.Errorf("scan legacy trigger: %w", err)
		}

		triggers = append(triggers, found)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate legacy triggers: %w", err)
	}

	return triggers, nil
}

func copyLegacyTables(ctx context.Context, db *sql.DB, tables []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy fixture copy: %w", err)
	}
	defer tx.Rollback()

	var localUserID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM source.users WHERE local_mode=1`).Scan(&localUserID); err != nil {
		return fmt.Errorf("find source fixture local actor: %w", err)
	}

	var legacyUserID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM main.users WHERE local_mode=1`).Scan(&legacyUserID); err != nil {
		return fmt.Errorf("find legacy fixture local actor: %w", err)
	}

	for _, table := range tables {
		// The released schema seeds its own local user and membership.
		if table == "users" || table == "library_memberships" {
			continue
		}

		names, err := queryNames(ctx, tx, `SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			return fmt.Errorf("read columns for %q: %w", table, err)
		}

		selected := make([]string, len(names))
		for index := range names {
			column := names[index]
			names[index] = `"` + column + `"`
			selected[index] = names[index]

			if column == "user_id" {
				// Personal state belongs to the released schema's own local user.
				var incompatible int
				if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM source."`+table+`" WHERE user_id<>?`, localUserID).Scan(&incompatible); err != nil {
					return fmt.Errorf("check legacy personal state: %w", err)
				}

				if incompatible != 0 {
					return fmt.Errorf("cannot reconstruct single-user fixture from non-local personal state in %s", table)
				}

				selected[index] = `'` + strings.ReplaceAll(legacyUserID, `'`, `''`) + `'`
			}
		}

		list := strings.Join(names, ",")
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO "`+table+`" (`+list+`) SELECT `+strings.Join(selected, ",")+` FROM source."`+table+`"`); err != nil { //nolint:gosec // table and column identifiers came from SQLite's own schema
			return fmt.Errorf("copy legacy fixture table %q: %w", table, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy fixture copy: %w", err)
	}

	return nil
}
