package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/app"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/scalefixture"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
)

func TestSeededBrowseWorkload(t *testing.T) {
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

	if err := scalefixture.Seed(ctx, store, blobs, 100); err != nil {
		t.Fatal(err)
	}

	assetService, err := assets.NewService(store, blobs)
	if err != nil {
		t.Fatal(err)
	}

	application, err := app.New(store, units.DefaultCatalog(), app.Options{Assets: assetService})
	if err != nil {
		t.Fatal(err)
	}
	defer application.Renderer.Close()
	defer application.ImportJobs.Close()
	// Exercise the request resolver, rather than passing the seeder's principal
	// to the read: both sides must independently choose the same persisted user.
	if err := requestScaleEndpoint(ctx, application, 100, "/api/v1/recipes?q=favorite%3Atrue&limit=50", fixtureLabelCounts(100)); err != nil {
		t.Fatal(err)
	}

	principal, err := store.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	ctx = access.WithLibrary(identity.WithPrincipal(ctx, principal), "default")
	if err := verifyFixture(ctx, store, 100, scalefixture.Policy()); err != nil {
		t.Fatal(err)
	}

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	var measured []string

	measure := func(name string, _ int, fn func() error) error {
		measured = append(measured, name)
		return fn()
	}
	if _, err := benchmarkReads(ctx, application, service, 100, 1, true, measure); err != nil {
		t.Fatal(err)
	}

	for _, required := range []string{"list", "search", "filter", "pagination", "tags", "facets", "library_total", "image_card"} {
		if !slices.Contains(measured, required) {
			t.Errorf("browse omits %s", required)
		}
	}

	for _, excluded := range []string{"open", "image_original", "image_reader"} {
		if slices.Contains(measured, excluded) {
			t.Errorf("browse includes %s", excluded)
		}
	}

	if err := store.DeleteFacet(ctx, "fscale-course-0", 1, time.Now()); err != nil {
		t.Fatal(err)
	}

	if err := verifyFixture(ctx, store, 100, scalefixture.Policy()); err == nil {
		t.Error("accepted a fixture with missing facets")
	}

	if err := requestScaleEndpoint(ctx, application, 100, "/api/v1/recipes?limit=50", fixtureLabelCounts(100)); err == nil {
		t.Error("accepted recipe summaries with missing facets")
	}
}

func TestTaxonomyResponseRequiresRecipeCounts(t *testing.T) {
	c := scalefixture.Policy()
	for path, size := range map[string]int{"/api/v1/tags": c.Taxonomy.Tags + 3, "/api/v1/facets": 4 * c.Taxonomy.FacetsPerKind} {
		body, err := json.Marshal(map[string]any{"items": make([]struct{}, size)})
		if err != nil {
			t.Fatal(err)
		}

		if err := verifyTaxonomyResponse(body, path, 100, fixtureLabelCounts(100)); err == nil {
			t.Errorf("%s accepted missing counts", path)
		}
	}
}
