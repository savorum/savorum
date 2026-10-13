package sqlite_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestSourceAttachmentRemainsReachableThroughHistoryUntilPurge(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := sqlite.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	blobStore, err := files.NewAssetStore(filepath.Join(root, "assets"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	assetService, err := assets.NewService(store, blobStore)
	if err != nil {
		t.Fatal(err)
	}

	recipeService, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	document := testfixture.Document()
	document.ID = "source-retention"
	document.Title = "Source retention"

	created, err := recipeService.Create(ctx, document)
	if err != nil {
		t.Fatal(err)
	}

	source := []byte("original cookbook source")
	size := int64(len(source))

	blob, err := assetService.Ingest(ctx, bytes.NewReader(source), assets.IngestOptions{Size: &size, MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.AttachSource(ctx, library.SourceAttachment{EntryID: created.ID, Revision: created.Revision, AssetID: string(blob.ID), Filename: "cookbook.txt", MediaType: blob.MediaType, Size: blob.Size}); err != nil {
		t.Fatal(err)
	}

	listed, err := store.ListSourceAttachments(ctx, created.ID, created.Revision)
	if err != nil || len(listed) != 1 || listed[0].Filename != "cookbook.txt" {
		t.Fatalf("sources=%#v err=%v", listed, err)
	}

	now := time.Now().UTC()

	collector, err := assets.NewService(store, blobStore, assets.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(48 * time.Hour)

	removed, err := collector.CollectGarbage(ctx, 24*time.Hour)
	if err != nil || removed != 0 {
		t.Fatalf("source attachment was collected while reachable: removed=%d err=%v", removed, err)
	}

	trashed, err := recipeService.Trash(ctx, created.ID, created.Revision)
	if err != nil {
		t.Fatal(err)
	}

	if err := recipeService.Purge(ctx, created.ID, trashed.Revision); err != nil {
		t.Fatal(err)
	}

	removed, err = collector.CollectGarbage(ctx, 24*time.Hour)
	if err != nil || removed < 1 {
		t.Fatalf("purged source was not collected: removed=%d err=%v", removed, err)
	}

	if _, err := store.GetSourceAttachment(ctx, created.ID, 0, string(blob.ID)); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("source attachment after purge err=%v", err)
	}

	if _, err := assetService.Metadata(ctx, blob.ID); !errors.Is(err, assets.ErrNotFound) {
		t.Fatalf("source blob after purge err=%v", err)
	}
}

func TestImportManyRollsBackRecipeWhenSourceAttachmentFails(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := sqlite.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	blobStore, err := files.NewAssetStore(filepath.Join(root, "assets"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	assetService, err := assets.NewService(store, blobStore)
	if err != nil {
		t.Fatal(err)
	}

	source := []byte("original source bytes")
	size := int64(len(source))

	blob, err := assetService.Ingest(ctx, bytes.NewReader(source), assets.IngestOptions{Size: &size, MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}

	document := testfixture.Document()
	document.ID = "atomic-source-document"
	timestamp := time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC)

	_, err = store.ImportMany(ctx, []library.ImportMutation{{
		ID: "atomic-source-entry", Recipe: document, Timestamp: timestamp,
		SourceAttachments: []library.SourceAttachment{{
			AssetID: string(blob.ID), Filename: "cookbook.pdf", MediaType: "image/png", Size: blob.Size,
		}},
	}})
	if !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("invalid source attachment error = %v, want missing asset error", err)
	}

	if _, _, _, err := store.FindByDocumentID(ctx, document.ID); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("recipe survived failed atomic source attachment: %v", err)
	}

	if _, err := store.GetSourceAttachment(ctx, "atomic-source-entry", 1, string(blob.ID)); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("source attachment survived failed atomic import: %v", err)
	}
}

func TestImportRecipesWithSourcesCommitsAttachmentWithRecipe(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := sqlite.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	blobStore, err := files.NewAssetStore(filepath.Join(root, "assets"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	assetService, err := assets.NewService(store, blobStore)
	if err != nil {
		t.Fatal(err)
	}

	source := []byte("original source bytes")
	size := int64(len(source))

	blob, err := assetService.Ingest(ctx, bytes.NewReader(source), assets.IngestOptions{Size: &size, MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}

	recipeService, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	document := testfixture.Document()
	document.ID = "atomic-source-success"

	results, err := recipeService.ImportRecipesWithSources(ctx, []recipe.Document{document}, library.DuplicateSkip, [][]library.SourceAttachment{{{
		AssetID: string(blob.ID), Filename: "cookbook.txt", MediaType: blob.MediaType, Size: blob.Size,
	}}})
	if err != nil || len(results) != 1 {
		t.Fatalf("atomic import results=%#v err=%v", results, err)
	}

	attachments, err := store.ListSourceAttachments(ctx, results[0].EntryID, results[0].Revision)
	if err != nil || len(attachments) != 1 || attachments[0].Filename != "cookbook.txt" {
		t.Fatalf("atomic source attachments=%#v err=%v", attachments, err)
	}
}
