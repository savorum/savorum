package export_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestSREFExportExcludesRetainedOriginalSource(t *testing.T) {
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

	recipes, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	document := testfixture.Document()
	document.ID = "export-source"
	document.Assets = nil
	document.ImageRefs = nil

	document.PrimaryImage = ""
	for index := range document.InstructionSections {
		for step := range document.InstructionSections[index].Steps {
			document.InstructionSections[index].Steps[step].ImageRefs = nil
		}
	}

	entry, err := recipes.Create(ctx, document)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("copyrighted source")
	size := int64(len(data))

	blob, err := assetService.Ingest(ctx, bytes.NewReader(data), assets.IngestOptions{Size: &size, MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.AttachSource(ctx, library.SourceAttachment{EntryID: entry.ID, Revision: entry.Revision, AssetID: string(blob.ID), Filename: "source.txt", MediaType: blob.MediaType, Size: blob.Size}); err != nil {
		t.Fatal(err)
	}

	exporter, err := export.NewService(recipes, assetService)
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if _, err := exporter.Export(ctx, export.NewRecipeSource(recipes, entry.ID), export.FormatSREF, &output); err != nil {
		t.Fatal(err)
	}

	archive, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}

	for _, file := range archive.File {
		if file.Name == "recipe.json" || file.Name == "manifest.json" {
			continue
		}

		body, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}

		_, _ = io.ReadAll(body)
		_ = body.Close()

		t.Fatalf("SREF export included non-recipe source member %q", file.Name)
	}
}
