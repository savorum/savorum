package sqlite

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/preferences"
)

func TestPreferencesPersistPerUserAndRejectStaleWrites(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "preferences.db")

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = store.Close() }()

	for _, id := range []string{"alice", "bob"} {
		if _, err := store.db.ExecContext(ctx, `INSERT INTO users(id,display_name,created_at,updated_at) VALUES (?,?,unixepoch(),unixepoch())`, id, id); err != nil {
			t.Fatal(err)
		}
	}

	first, err := store.ReplacePreferences(ctx, "alice", 0, preferences.Values{Measurement: "metric", UILocale: "fr", ContentLanguages: []string{"fr-CA", "en"}})
	if err != nil || first.Revision != 1 {
		t.Fatalf("save alice: %#v %v", first, err)
	}

	if _, err := store.ReplacePreferences(ctx, "alice", 0, preferences.Values{}); !errors.Is(err, preferences.ErrConflict) {
		t.Fatalf("stale insert: %v", err)
	}

	if _, err := store.ReplacePreferences(ctx, "alice", 2, preferences.Values{}); !errors.Is(err, preferences.ErrConflict) {
		t.Fatalf("stale update: %v", err)
	}

	if _, err := store.ReplacePreferences(ctx, "bob", 0, preferences.Values{Measurement: "us"}); err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	alice, err := store.LoadPreferences(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}

	bob, err := store.LoadPreferences(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}

	if alice.Overrides.Measurement != "metric" || bob.Overrides.Measurement != "us" || alice.Overrides.ContentLanguages[0] != "fr-CA" {
		t.Fatalf("personal preferences crossed: %#v %#v", alice, bob)
	}

	if _, err := store.db.ExecContext(ctx, `DELETE FROM users WHERE id='alice'`); err != nil {
		t.Fatal(err)
	}

	erased, err := store.LoadPreferences(ctx, "alice")
	if err != nil || erased.Revision != 0 {
		t.Fatalf("deleted user's preferences retained: %#v %v", erased, err)
	}
}
