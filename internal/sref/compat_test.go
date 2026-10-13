package sref_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
)

// releasedDocument is an untouched example from the published SREF v0.1.0
// release archive, carrying an already normalized
// `volume.teaspoon.metric.culinary` quantity. Its version declaration is
// deliberately not edited: rewriting a fixture's declared version tests
// nothing about compatibility.
func releasedDocument(t *testing.T) []byte {
	t.Helper()

	data, err := os.ReadFile("testdata/released-0.1.0.recipe.json")
	if err != nil {
		t.Fatal(err)
	}

	return data
}

// TestAReleasedEarlierDocumentIsStillReadable: every SREF file Savorum wrote
// before the 0.2.0 registry declares 0.1.0, whose unit identities mean the
// same thing, so it must still be read through the public path.
func TestAReleasedEarlierDocumentIsStillReadable(t *testing.T) {
	t.Parallel()

	document, err := sref.Decode(releasedDocument(t))
	if err != nil {
		t.Fatalf("a published SREF 0.1.0 recipe was refused: %v", err)
	}

	peppercorns := peppercorns(t, document)
	if peppercorns.Quantity == nil || peppercorns.Quantity.Unit != "volume.teaspoon.metric.culinary" {
		t.Fatalf("the stored identity changed: %#v", peppercorns.Quantity)
	}

	if peppercorns.Quantity.Amount == nil || peppercorns.Quantity.Amount.Value.String() != "1" {
		t.Fatalf("the stored amount changed: %#v", peppercorns.Quantity.Amount)
	}

	// Reading is not normalization. The alias `teaspoon` no longer resolves to
	// this identity, and that must not reach back into a document that already
	// carries it.
	if peppercorns.Normalization != nil && peppercorns.Normalization.Method == recipe.NormalizationUnresolved {
		t.Fatalf("reading re-ran alias resolution: %#v", peppercorns.Normalization)
	}
}

// TestRewritingAnEarlierDocumentEmitsThisBuildsVersion pins the half of the
// version rule that points the other way.
//
// A newer document keeps the version it declared; an earlier one is written as
// this build's version, which SREF's versioning rules require. The content,
// including authored text and resolved units, must survive unchanged.
func TestRewritingAnEarlierDocumentEmitsThisBuildsVersion(t *testing.T) {
	t.Parallel()

	document, err := sref.Decode(releasedDocument(t))
	if err != nil {
		t.Fatal(err)
	}

	before := peppercorns(t, document)

	written, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	var header struct {
		SREF struct {
			Version      string `json:"version"`
			UnitRegistry string `json:"unit_registry"`
		} `json:"sref"`
	}
	if err := json.Unmarshal(written, &header); err != nil {
		t.Fatal(err)
	}

	if header.SREF.Version != sref.Version || header.SREF.UnitRegistry != sref.UnitRegistryVersion {
		t.Fatalf("a %s writer emitted %s/%s", sref.Version,
			header.SREF.Version, header.SREF.UnitRegistry)
	}

	returned, err := sref.Decode(written)
	if err != nil {
		t.Fatal(err)
	}

	after := peppercorns(t, returned)
	if after.Quantity == nil || after.Quantity.Unit != before.Quantity.Unit ||
		after.Quantity.Amount.Value.String() != before.Quantity.Amount.Value.String() ||
		after.SourceText != before.SourceText || after.Name != before.Name {
		t.Fatalf("the upgrade changed the ingredient: %#v", after)
	}
}

func peppercorns(t *testing.T, document recipe.Document) recipe.Ingredient {
	t.Helper()

	for _, section := range document.IngredientSections {
		for _, ingredient := range section.Ingredients {
			if ingredient.ID == "peppercorns" {
				return ingredient
			}
		}
	}

	t.Fatal("the fixture has no peppercorns")

	return recipe.Ingredient{}
}

// TestAnEarlierVersionOutsideTheAdmittedListIsRefused: earlier releases are
// admitted individually, not as "anything older in the line".
func TestAnEarlierVersionOutsideTheAdmittedListIsRefused(t *testing.T) {
	t.Parallel()

	body := map[string]any{}
	if err := json.Unmarshal(releasedDocument(t), &body); err != nil {
		t.Fatal(err)
	}

	body["sref"] = map[string]any{"version": "0.0.9", "unit_registry": "0.0.9"}

	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sref.Decode(data); err == nil {
		t.Fatal("an unreleased earlier version was read as though this build knew its rules")
	}
}

// TestAPackageOfUntouchedEarlierBytesIsReadable carries the guarantee through
// the archive path. The archive wraps the original bytes, because re-encoding
// would stamp this build's version on them.
func TestAPackageOfUntouchedEarlierBytesIsReadable(t *testing.T) {
	t.Parallel()

	original := releasedDocument(t)

	// The precondition, asserted rather than assumed.
	var header struct {
		SREF struct {
			Version      string `json:"version"`
			UnitRegistry string `json:"unit_registry"`
		} `json:"sref"`
	}
	if err := json.Unmarshal(original, &header); err != nil {
		t.Fatal(err)
	}

	if header.SREF.Version != "0.1.0" || header.SREF.UnitRegistry != "0.1.0" {
		t.Fatalf("the fixture declares %s/%s", header.SREF.Version, header.SREF.UnitRegistry)
	}

	packaged := packageAround(t, original)

	// And the bytes inside the archive, before anything reads it.
	if embedded := recipeInPackage(t, packaged); !bytes.Equal(embedded, original) {
		t.Fatal("the archive does not carry the original bytes")
	}

	opened, err := sref.ReadPackage(packaged, sref.DefaultLimits())
	if err != nil {
		t.Fatalf("a package carrying a released earlier recipe was refused: %v", err)
	}

	if peppercorns(t, opened.Recipe).Quantity.Unit != "volume.teaspoon.metric.culinary" {
		t.Fatal("the stored identity changed through the archive path")
	}
}

// TestEarlierNormalizationProvenanceSurvivesReading: an unresolved measure and
// its explanation, written under an earlier registry, must not be rewritten
// by an alias change.
func TestEarlierNormalizationProvenanceSurvivesReading(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/released-0.1.0-with-provenance.recipe.json")
	if err != nil {
		t.Fatal(err)
	}

	document, err := sref.Decode(data)
	if err != nil {
		t.Fatalf("a published SREF 0.1.0 recipe with provenance was refused: %v", err)
	}

	juice := ingredientByID(t, document, "cranberry-juice")
	if juice.Normalization == nil || juice.Normalization.Method != recipe.NormalizationUnresolved ||
		len(juice.Normalization.Warnings) != 1 ||
		juice.Normalization.Warnings[0] != "The source locale alone does not establish which cup convention the author used." {
		t.Fatalf("the authored provenance changed: %#v", juice.Normalization)
	}

	// An opaque quantity is what the earlier registry left it as, and reading
	// under a registry that resolves the word differently must not touch it.
	if juice.Quantity == nil || juice.Quantity.Kind != recipe.QuantityOpaque || juice.Quantity.Text != "2 cups" {
		t.Fatalf("the opaque quantity changed: %#v", juice.Quantity)
	}

	written, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	returned, err := sref.Decode(written)
	if err != nil {
		t.Fatal(err)
	}

	after := ingredientByID(t, returned, "cranberry-juice")
	if after.Normalization == nil || len(after.Normalization.Warnings) != 1 ||
		after.Normalization.Warnings[0] != juice.Normalization.Warnings[0] ||
		after.Quantity.Text != "2 cups" {
		t.Fatalf("provenance did not survive the round trip: %#v", after)
	}
}

// packageAround builds a `.sref` archive whose recipe member is exactly the
// bytes given, with a manifest describing those bytes.
func packageAround(t *testing.T, document []byte) []byte {
	t.Helper()

	digest := sha256.Sum256(document)
	manifest := fmt.Sprintf(
		`{"sref_package":{"version":%q},"recipe":{"path":"recipe.json","media_type":"application/json","sha256":%q,"size":%d},"assets":[]}`,
		sref.PackageVersion, hex.EncodeToString(digest[:]), len(document))

	buffer := &bytes.Buffer{}
	archive := zip.NewWriter(buffer)

	for _, member := range []struct {
		name string
		data []byte
	}{{"recipe.json", document}, {"manifest.json", []byte(manifest)}} {
		writer, err := archive.Create(member.name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := writer.Write(member.data); err != nil {
			t.Fatal(err)
		}
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

// recipeInPackage reads the recipe member back out of an archive, so what the
// archive carries can be checked before a reader interprets it.
func recipeInPackage(t *testing.T, packaged []byte) []byte {
	t.Helper()

	archive, err := zip.NewReader(bytes.NewReader(packaged), int64(len(packaged)))
	if err != nil {
		t.Fatal(err)
	}

	for _, member := range archive.File {
		if member.Name != "recipe.json" {
			continue
		}

		opened, err := member.Open()
		if err != nil {
			t.Fatal(err)
		}

		defer func() { _ = opened.Close() }()

		data, err := io.ReadAll(opened)
		if err != nil {
			t.Fatal(err)
		}

		return data
	}

	t.Fatal("the archive has no recipe member")

	return nil
}

func ingredientByID(t *testing.T, document recipe.Document, id string) recipe.Ingredient {
	t.Helper()

	for _, section := range document.IngredientSections {
		for _, ingredient := range section.Ingredients {
			if ingredient.ID == id {
				return ingredient
			}
		}
	}

	t.Fatalf("no ingredient %q", id)

	return recipe.Ingredient{}
}
