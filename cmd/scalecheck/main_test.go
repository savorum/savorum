package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/app"
	"github.com/savorum/savorum/internal/scalefixture"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
)

func TestSeededFavoritesBelongToTheHTTPUser(t *testing.T) {
	ctx := context.Background()

	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	blobs, err := files.NewAssetStore(t.TempDir(), 64<<20)
	if err != nil {
		t.Fatal(err)
	}

	if err := scalefixture.Seed(ctx, store, blobs, 20); err != nil {
		t.Fatal(err)
	}

	application, err := app.New(store, units.DefaultCatalog(), app.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer application.Renderer.Close()
	// Exercise the request resolver, rather than passing the seeder's principal
	// to the read: both sides must independently choose the same persisted user.
	if err := requestScaleEndpoint(ctx, application, 20, "/api/v1/recipes?q=favorite%3Atrue&limit=50"); err != nil {
		t.Fatal(err)
	}
}
