// Package interop_test exercises SREF round trips through the whole of
// Savorum rather than through one codec.
//
// These tests run the real stack (importer registry, exchange service, SQLite,
// content-addressed asset store, export service), including an edit to an
// unrelated field, and compare what comes out with internal/semanticdiff.
package interop_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/semanticdiff"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/srefcheckout"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
)

// savorum is one complete instance over a database and asset store that exist
// for the length of one test. Nothing here is a stub.
type savorum struct {
	recipes  *library.Service
	blobs    *assets.Service
	inbound  *exchange.Service
	outbound *export.Service
}

func newSavorum(t *testing.T) *savorum {
	t.Helper()

	ctx := context.Background()
	root := t.TempDir()

	store, err := sqlite.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	recipes, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	blobStore, err := files.NewAssetStore(filepath.Join(root, "assets"), files.DefaultMaximumAssetSize)
	if err != nil {
		t.Fatal(err)
	}

	blobs, err := assets.NewService(store, blobStore)
	if err != nil {
		t.Fatal(err)
	}

	outbound, err := export.NewService(recipes, blobs)
	if err != nil {
		t.Fatal(err)
	}

	return &savorum{recipes: recipes, blobs: blobs, inbound: exchange.NewService(recipes, blobs), outbound: outbound}
}

// importOne brings one source in through the ordinary import boundary and
// returns the library entry it became.
func (s *savorum) importOne(t *testing.T, input importer.Input) library.ImportResult {
	t.Helper()

	results, err := s.inbound.ImportSources(context.Background(), []importer.Input{input}, importer.Options{}, library.DuplicateReplace)
	if err != nil {
		t.Fatalf("import %s: %v", input.Name, err)
	}

	if len(results) != 1 {
		t.Fatalf("import %s produced %d recipes", input.Name, len(results))
	}

	return results[0]
}

// exportPackage writes one library entry as the `.sref` package a person would
// download, and returns both the archive and the recipe it carries.
func (s *savorum) exportPackage(t *testing.T, id library.EntryID) ([]byte, recipe.Document) {
	t.Helper()

	buffer := &bytes.Buffer{}

	source := export.NewRecipeSource(s.recipes, id)
	if _, err := s.outbound.Export(context.Background(), source, export.FormatSREF, buffer); err != nil {
		t.Fatalf("export %s: %v", id, err)
	}

	opened, err := sref.ReadPackage(buffer.Bytes(), sref.DefaultLimits())
	if err != nil {
		t.Fatalf("Savorum wrote a package it cannot read: %v", err)
	}

	return buffer.Bytes(), opened.Recipe
}

// stored reads back what the library holds, which is what an export is written
// from and what a later edit starts from.
func (s *savorum) stored(t *testing.T, id library.EntryID) library.Entry {
	t.Helper()

	entry, err := s.recipes.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}

	return entry
}

// same fails with the paths that disagree rather than with two documents the
// reader has to compare by eye.
func same(t *testing.T, what string, before, after recipe.Document) {
	t.Helper()

	if differences := semanticdiff.Diff(before, after); len(differences) != 0 {
		t.Fatalf("%s:\n%s", what, semanticdiff.Report(differences))
	}
}

// packageEntries reads the members of a `.sref` archive, so a test can assert
// on the bytes a photograph arrived as rather than on the recipe's claim
// about them.
func packageEntries(t *testing.T, archive []byte) map[string][]byte {
	t.Helper()

	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}

	entries := map[string][]byte{}

	for _, file := range reader.File {
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}

		content, err := io.ReadAll(opened)
		_ = opened.Close()

		if err != nil {
			t.Fatal(err)
		}

		entries[file.Name] = content
	}

	return entries
}

// specimenDirectory is the SREF project's own example corpus. Round trips are
// worth most against documents this repository did not write.

func specimens(t *testing.T) map[string][]byte {
	t.Helper()
	specimenDirectory := srefcheckout.Path(t, "examples")

	entries, err := os.ReadDir(specimenDirectory)
	if err != nil {
		t.Fatal(err)
	}

	documents := map[string][]byte{}

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(specimenDirectory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}

		documents[entry.Name()] = data
	}

	if len(documents) == 0 {
		srefcheckout.Unavailable(t, "no SREF examples found")
	}

	return documents
}

// digestOf is the identity a package asset travels under.
func digestOf(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// importWebPage reads a page the way a browser capture would and brings the
// result in through the same orchestration every other source uses.
func (s *savorum) importWebPage(t *testing.T, page []byte, documentURL string) library.ImportResult {
	t.Helper()

	batch, err := importer.DecodeWebRecipe(page, "text/html", documentURL)
	if err != nil {
		t.Fatalf("decode %s: %v", documentURL, err)
	}

	results, err := s.inbound.ImportExternal(context.Background(), []importer.ImportBatch{batch}, library.DuplicateReplace)
	if err != nil {
		t.Fatalf("import %s: %v", documentURL, err)
	}

	if len(results) != 1 {
		t.Fatalf("import %s produced %d recipes", documentURL, len(results))
	}

	return results[0]
}

// pngBytes is a real, if minimal, PNG. The asset store checks that a file is
// what its media type claims, so a photograph in a test has to be one.
func pngBytes(seed string) []byte {
	image := &bytes.Buffer{}
	if err := png.Encode(image, singlePixel{seed: seed}); err != nil {
		panic(err)
	}

	return image.Bytes()
}

type singlePixel struct{ seed string }

func (singlePixel) ColorModel() color.Model { return color.RGBAModel }
func (singlePixel) Bounds() image.Rectangle { return image.Rect(0, 0, 1, 1) }
func (p singlePixel) At(int, int) color.Color {
	return color.RGBA{R: p.seed[0], G: p.seed[1], B: p.seed[2], A: 255}
}
