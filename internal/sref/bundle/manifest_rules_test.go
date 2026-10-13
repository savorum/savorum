package bundle_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"testing"

	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/sref/bundle"
)

// `format` and `version` are constants in the bundle manifest schema; a
// violation is a manifest failure, not `invalid-recipe`.
func TestBundleManifestFixedMembers(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		alter       func(manifest map[string]any)
		requirement string
		category    sref.FailureCategory
	}{
		"another format":  {func(m map[string]any) { m["format"] = "sref-collection" }, "malformed-package-json", sref.InvalidArtifact},
		"no format":       {func(m map[string]any) { delete(m, "format") }, "malformed-package-json", sref.InvalidArtifact},
		"another version": {func(m map[string]any) { m["version"] = 2 }, "unsupported-bundle-version", sref.UnsupportedFormatVersion},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := bundle.Read(bundleWithManifest(t, test.alter), bundle.DefaultLimits())
			if err == nil {
				t.Fatal("the bundle was accepted")
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

func bundleWithManifest(t *testing.T, alter func(manifest map[string]any)) []byte {
	t.Helper()

	manifest := map[string]any{
		"format":  "sref-bundle",
		"version": 1,
		"recipes": []any{},
	}
	alter(manifest)

	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	buffer := &bytes.Buffer{}
	archive := zip.NewWriter(buffer)

	entry, err := archive.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := entry.Write(encoded); err != nil {
		t.Fatal(err)
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}
