package sqlite

import (
	"fmt"
	"testing"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestProjectionUpgradeOnlyWritesChangedValues(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openStore(ctx, t)

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	var entries []library.Entry

	for _, title := range []string{"Current projection", "Stale projection"} {
		document := testfixture.Document()
		document.ID = recipe.RecipeID(fmt.Sprintf("projection-%d", len(entries)))
		document.Title = title

		entry, err := service.Create(ctx, document)
		if err != nil {
			t.Fatal(err)
		}

		entries = append(entries, entry)
	}

	if _, err := store.db.ExecContext(ctx, `UPDATE library_recipes SET title='Obsolete title' WHERE id=?`, entries[1].ID); err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, `CREATE TABLE projection_writes(entry_id TEXT);
 CREATE TRIGGER record_projection_write AFTER UPDATE ON library_recipes BEGIN
 INSERT INTO projection_writes VALUES(new.id); END;
 PRAGMA user_version=0;`); err != nil {
		t.Fatal(err)
	}

	if err := store.rebuildProjections(ctx); err != nil {
		t.Fatal(err)
	}

	var (
		count     int
		updatedID string
	)
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*), MIN(entry_id) FROM projection_writes`).Scan(&count, &updatedID); err != nil {
		t.Fatal(err)
	}

	if count != 1 || updatedID != string(entries[1].ID) {
		t.Fatalf("projection writes = %d / %s; want only %s", count, updatedID, entries[1].ID)
	}

	found, err := service.List(ctx, library.ListOptions{Query: "Stale projection"})
	if err != nil || len(found) != 1 || found[0].ID != entries[1].ID {
		t.Fatalf("rebuilt search = %+v, %v", found, err)
	}

	for _, original := range entries {
		current, err := service.Get(ctx, original.ID)
		if err != nil {
			t.Fatal(err)
		}

		if current.Revision != original.Revision || !current.UpdatedAt.Equal(original.UpdatedAt) {
			t.Fatal("projection rebuild changed recipe history")
		}
	}

	if _, err := store.db.ExecContext(ctx, `DELETE FROM projection_writes; PRAGMA user_version=0`); err != nil {
		t.Fatal(err)
	}

	if err := store.rebuildProjections(ctx); err != nil {
		t.Fatal(err)
	}

	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projection_writes`).Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatalf("unchanged projections caused %d writes", count)
	}

	version, err := store.projectionVersion(ctx)
	if err != nil || version != library.ProjectionVersion {
		t.Fatalf("projection version = %d, %v", version, err)
	}
}
