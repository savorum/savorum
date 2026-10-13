package main

import (
	"database/sql"
	"testing"
)

func TestLegacyFixtureCopyKeepsPersonalStateWithTheReleasedLocalUser(t *testing.T) {
	db := legacyCopyDatabase(t)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO source.recipe_user_state VALUES('recipe','opaque-local-user',1)`); err != nil {
		t.Fatal(err)
	}

	if err := copyLegacyTables(t.Context(), db, []string{"recipe_user_state"}); err != nil {
		t.Fatal(err)
	}

	var (
		actor    string
		favorite int
	)
	if err := db.QueryRowContext(t.Context(), `SELECT user_id,favorite FROM recipe_user_state WHERE entry_id='recipe'`).Scan(&actor, &favorite); err != nil {
		t.Fatal(err)
	}

	if actor != "released-local-user" || favorite != 1 {
		t.Fatalf("released personal state = %q / %d", actor, favorite)
	}
}

func TestLegacyFixtureCopyRejectsMultipleUserState(t *testing.T) {
	db := legacyCopyDatabase(t)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO source.recipe_user_state VALUES('recipe','another-user',1)`); err != nil {
		t.Fatal(err)
	}

	if err := copyLegacyTables(t.Context(), db, []string{"recipe_user_state"}); err == nil {
		t.Fatal("multi-user state silently collapsed into released local identity")
	}

	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM recipe_user_state`).Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatal("failed fixture copy retained partial state")
	}
}

func legacyCopyDatabase(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	for _, query := range []string{
		`CREATE TABLE users(id TEXT PRIMARY KEY,local_mode INTEGER)`,
		`INSERT INTO users VALUES('released-local-user',1)`,
		`CREATE TABLE recipe_user_state(entry_id TEXT,user_id TEXT,favorite INTEGER,PRIMARY KEY(entry_id,user_id))`,
		`ATTACH DATABASE ':memory:' AS source`,
		`CREATE TABLE source.users(id TEXT PRIMARY KEY,local_mode INTEGER)`,
		`INSERT INTO source.users VALUES('opaque-local-user',1)`,
		`CREATE TABLE source.recipe_user_state(entry_id TEXT,user_id TEXT,favorite INTEGER,PRIMARY KEY(entry_id,user_id))`,
	} {
		if _, err := db.ExecContext(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}

	return db
}
