package sref_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/savorum/savorum/internal/sref"
)

// The package manifest's fixed members: the package version, the recipe
// entry's constant path and media type, and the required `assets` array. Each
// violation fails as `malformed-package-json`, the manifest not satisfying
// section 14's schema.
func TestPackageManifestFixedMembers(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		alter       func(manifest map[string]any)
		requirement string
		category    sref.FailureCategory
	}{
		"no assets array": {alter: func(manifest map[string]any) {
			delete(manifest, "assets")
		}, requirement: "malformed-package-json", category: sref.InvalidArtifact},
		"no package header": {alter: func(manifest map[string]any) {
			delete(manifest, "sref_package")
		}, requirement: "malformed-package-json", category: sref.InvalidArtifact},
		"a package version this build does not implement": {alter: func(manifest map[string]any) {
			manifest["sref_package"] = map[string]any{"version": "9.9.9"}
		}, requirement: "unsupported-package-version", category: sref.UnsupportedFormatVersion},
		"a recipe entry naming another file": {alter: func(manifest map[string]any) {
			mustType[map[string]any](manifest["recipe"])["path"] = "not-recipe.json"
		}, requirement: "malformed-package-json", category: sref.InvalidArtifact},
		"a recipe entry claiming another media type": {alter: func(manifest map[string]any) {
			mustType[map[string]any](manifest["recipe"])["media_type"] = "text/plain"
		}, requirement: "malformed-package-json", category: sref.InvalidArtifact},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := sref.ReadPackage(packageWithManifest(t, test.alter), sref.DefaultLimits())
			if err == nil {
				t.Fatal("the package was accepted")
			}

			if requirement := sref.RequirementID(err); requirement != test.requirement {
				t.Fatalf("requirement = %q, want %q: %v", requirement, test.requirement, err)
			}

			if category := sref.FailureCategoryOf(err); category != test.category {
				t.Fatalf("category = %q, want %q: %v", category, test.category, err)
			}
		})
	}
}

func TestPackageManifestWithAnEmptyAssetsArrayIsRead(t *testing.T) {
	// The ordinary case, and the one the missing-member rule must not catch.
	t.Parallel()

	if _, err := sref.ReadPackage(packageWithManifest(t, nil), sref.DefaultLimits()); err != nil {
		t.Fatalf("a package for a recipe with no assets was refused: %v", err)
	}
}

// packageWithManifest builds a valid asset-free package and then alters its
// manifest, so each case differs from a package that works by exactly one
// member.
func packageWithManifest(t *testing.T, alter func(manifest map[string]any)) []byte {
	t.Helper()

	recipe := []byte(`{"sref":{"version":"0.2.0","unit_registry":"0.2.0"},` +
		`"id":"minimal","title":"Minimal",` +
		`"ingredient_sections":[{"id":"i","ingredients":[{"id":"salt","name":"salt"}]}],` +
		`"instruction_sections":[{"id":"m","steps":[{"id":"s","text":"Cook."}]}]}`)
	digest := sha256.Sum256(recipe)

	manifest := map[string]any{
		"sref_package": map[string]any{"version": "0.1.0"},
		"recipe": map[string]any{
			"path": "recipe.json", "media_type": "application/json",
			"sha256": hex.EncodeToString(digest[:]), "size": len(recipe),
		},
		"assets": []any{},
	}
	if alter != nil {
		alter(manifest)
	}

	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	buffer := &bytes.Buffer{}

	archive := zip.NewWriter(buffer)
	for name, content := range map[string][]byte{"manifest.json": encoded, "recipe.json": recipe} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}
