package sref_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/srefcheckout"
)

// TestHostilePackagesAreRefused runs the specification's own package fixtures
// through Savorum's reader. Every fixture but the valid one is a way a package
// can be dangerous or dishonest, and each has to be refused rather than
// half-read.
func TestHostilePackagesAreRefused(t *testing.T) {
	t.Parallel()
	packageDirectory := srefcheckout.Path(t, "tests/packages")

	entries, err := os.ReadDir(packageDirectory)
	if err != nil {
		t.Fatal(err)
	}

	var checked int

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".sref" {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".sref")

		data, err := readSREFFixture(filepath.Join(packageDirectory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}

		opened, err := sref.ReadPackage(data, sref.DefaultLimits())
		checked++

		if strings.HasPrefix(name, "valid") || name == "minimum-ingredients-only" || name == "minimum-instructions-only" {
			if err != nil {
				t.Errorf("%s: a valid package was refused: %v", name, err)
				continue
			}

			if opened.Recipe.Title == "" {
				t.Errorf("%s: a valid package produced no recipe", name)
			}

			continue
		}

		if err == nil {
			t.Errorf("%s: a hostile package was accepted", name)
		}
	}

	if checked == 0 {
		srefcheckout.Unavailable(t, "no package fixtures found")
	}

	t.Logf("checked %d package fixtures", checked)
}

func TestHighRatioValidPackageFailsInsideProductionBudget(t *testing.T) {
	assetBytes := bytes.Repeat([]byte("a"), 2<<20)
	digest := sha256.Sum256(assetBytes)
	document := recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            "high-ratio", Title: "High ratio",
		IngredientSections:  []recipe.IngredientSection{{ID: "ingredients", Ingredients: []recipe.Ingredient{{ID: "salt", Name: "salt"}}}},
		InstructionSections: []recipe.InstructionSection{{ID: "method", Steps: []recipe.Step{{ID: "mix", Text: "Mix."}}}},
		Assets:              []recipe.Asset{{ID: "asset", Path: "assets/asset.txt", MediaType: "text/plain", SHA256: hex.EncodeToString(digest[:]), Size: int64(len(assetBytes))}},
	}

	data, err := sref.WritePackage(document, []sref.PackageAsset{{
		ID: "asset", Path: "assets/asset.txt", MediaType: "text/plain",
		SHA256: hex.EncodeToString(digest[:]), Bytes: assetBytes,
	}})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sref.ReadPackage(data, sref.DefaultLimits()); !errors.Is(err, sref.ErrUnsafePackage) {
		t.Fatalf("high-ratio package = %v, want controlled resource refusal", err)
	}

	limits := sref.DefaultLimits()
	limits.CompressionRate = 0

	limits.WorkBytes = int64(len(data)) + 1<<20
	if _, err := sref.ReadPackage(data, limits); !errors.Is(err, sref.ErrUnsafePackage) ||
		!strings.Contains(err.Error(), "working-set budget") {
		t.Fatalf("aggregate working-set budget = %v, want controlled refusal", err)
	}
}

func TestAPackageSavorumWritesIsOneItReads(t *testing.T) {
	t.Parallel()

	data, err := readSREFFixture(filepath.Join(srefcheckout.Path(t, "tests/packages"), "valid-with-asset.sref"))
	if err != nil {
		t.Fatal(err)
	}

	opened, err := sref.ReadPackage(data, sref.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}

	written, err := sref.WritePackage(opened.Recipe, opened.Assets)
	if err != nil {
		t.Fatal(err)
	}

	reopened, err := sref.ReadPackage(written, sref.DefaultLimits())
	if err != nil {
		t.Fatalf("Savorum cannot read the package it wrote: %v", err)
	}

	if reopened.Recipe.ID != opened.Recipe.ID || len(reopened.Assets) != len(opened.Assets) {
		t.Fatalf("a write and read cycle changed the package")
	}

	for index := range reopened.Assets {
		if reopened.Assets[index].SHA256 != opened.Assets[index].SHA256 {
			t.Fatalf("asset %q changed through a write", reopened.Assets[index].ID)
		}
	}
}

func TestPackageReaderRejectsCaseCollidingEntries(t *testing.T) {
	t.Parallel()

	buffer := &bytes.Buffer{}

	archive := zip.NewWriter(buffer)
	for _, name := range []string{"manifest.json", "Manifest.json"} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := entry.Write([]byte("{}")); err != nil {
			t.Fatal(err)
		}
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := sref.ReadPackage(buffer.Bytes(), sref.DefaultLimits())
	if !errors.Is(err, sref.ErrUnsafePackage) || !strings.Contains(err.Error(), "differ only by case") {
		t.Fatalf("case collision = %v, want unsafe package", err)
	}
}
