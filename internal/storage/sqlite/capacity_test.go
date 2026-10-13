package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/recipe"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestFullDiskSavePreservesCurrentAndHistory(t *testing.T) {
	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	service, _ := library.NewService(store, units.DefaultCatalog())

	entry, err := service.Create(ctx, testfixture.Document())
	if err != nil {
		t.Fatal(err)
	}

	before, err := store.StorageUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var pages int
	if err := store.db.QueryRowContext(t.Context(), `PRAGMA page_count`).Scan(&pages); err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(t.Context(), `PRAGMA max_page_count = `+strconv.Itoa(pages)); err != nil {
		t.Fatal(err)
	}

	document := entry.Recipe
	document.Description = strings.Repeat("A large edit that must not displace history. ", 2000)
	_, err = service.Update(ctx, entry.ID, entry.Revision, document)

	var coded interface{ Code() int }
	if !errors.As(err, &coded) || coded.Code() != 13 {
		t.Fatalf("want SQLITE_FULL, got %v", err)
	}

	after, err := store.StorageUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}

	current, err := service.Get(ctx, entry.ID)
	if err != nil {
		t.Fatal(err)
	}

	if current.Revision != entry.Revision || current.Recipe.Description != entry.Recipe.Description || after.RecipeRevisions != before.RecipeRevisions || after.RecipeRevisionJSONBytes != before.RecipeRevisionJSONBytes {
		t.Fatal("failed save changed current recipe or immutable history")
	}

	if err := store.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestStorageUsageSeparatesSharedCurrentHistoricalAndOrphanOriginals(t *testing.T) {
	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	service, _ := library.NewService(store, units.DefaultCatalog())

	for i, letter := range []string{"a", "b", "c"} {
		digest := strings.Repeat(letter, 64)

		_, err := store.RegisterStoredAsset(ctx, assets.Blob{ID: assets.ID("sha256:" + digest), ContentHash: digest, MediaType: "image/jpeg", Size: int64((i + 1) * 10), StorageKey: letter + "/" + digest, CreatedAt: time.Now()}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
	}

	d := testfixture.Document()
	for i := range d.InstructionSections {
		for j := range d.InstructionSections[i].Steps {
			d.InstructionSections[i].Steps[j].ImageRefs = nil
		}
	}

	d.Assets = []recipe.Asset{{ID: "photo", Path: "assets/photo.jpg", SHA256: strings.Repeat("a", 64), Size: 10, MediaType: "image/jpeg"}}
	d.ImageRefs = []string{"photo"}
	d.PrimaryImage = "photo"

	first, err := service.Create(ctx, d)
	if err != nil {
		t.Fatal(err)
	}

	d.Assets[0].SHA256 = strings.Repeat("b", 64)

	d.Assets[0].Size = 20
	if _, err := service.Update(ctx, first.ID, first.Revision, d); err != nil {
		t.Fatal(err)
	}

	d.ID = "shared-photo-recipe"
	if _, err := service.Create(ctx, d); err != nil {
		t.Fatal(err)
	}

	usage, err := store.StorageUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if usage.CurrentAssetBytes != 20 || usage.RetainedOnlyAssetBytes != 10 || usage.OrphanAssetBytes != 30 || usage.RecipeRevisions != 3 {
		t.Fatalf("categories count hashes once: %+v", usage)
	}
}
