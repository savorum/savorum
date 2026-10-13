package administration_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/administration"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestBackupIncludesRetainedOriginalSourceBytes(t *testing.T) {
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
	document.ID = "backup-source"

	entry, err := recipes.Create(ctx, document)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("retained original")
	size := int64(len(data))

	blob, err := assetService.Ingest(ctx, bytes.NewReader(data), assets.IngestOptions{Size: &size, MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.AttachSource(ctx, library.SourceAttachment{EntryID: entry.ID, Revision: entry.Revision, AssetID: string(blob.ID), Filename: "original.txt", MediaType: blob.MediaType, Size: blob.Size}); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(root, "backup.zip")
	if err := administration.Backup(ctx, store, filepath.Join(root, "assets"), archive, entry.CreatedAt); err != nil {
		t.Fatal(err)
	}

	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	want := "assets/" + blob.StorageKey
	for _, file := range reader.File {
		if file.Name != want {
			continue
		}

		body, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}

		got, err := io.ReadAll(body)
		_ = body.Close()

		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(got, data) {
			t.Fatalf("backup source bytes=%q", got)
		}

		return
	}

	t.Fatalf("backup did not include retained source %q", want)
}
