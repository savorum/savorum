package importer_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"

	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/testfixture"
)

type fakeAdapter struct {
	format importer.Format
	probe  importer.ProbeResult
	title  string
}

func (adapter fakeAdapter) Format() importer.Format {
	return adapter.format
}

func (adapter fakeAdapter) Probe(context.Context, importer.Input) importer.ProbeResult {
	return adapter.probe
}

func (adapter fakeAdapter) Import(context.Context, importer.Input, importer.Options) (importer.ImportBatch, error) {
	return importer.ImportBatch{Recipes: []importer.ImportedRecipe{{Document: recipe.Document{Title: adapter.title}}}}, nil
}

func TestRegistryAcceptsFutureAdaptersWithoutChangingTheImportBoundary(t *testing.T) {
	registry := importer.NewRegistry(fakeAdapter{
		format: importer.Format{ID: "future", Name: "Future Recipe App", Version: "2", Extensions: []string{".future"}, Capabilities: importer.Capabilities{Collection: true, SourceIdentity: true}},
		probe:  importer.ProbeResult{Confidence: importer.ConfidenceContent}, title: "Plugged in",
	})
	if !registry.Supports("recipes.future") {
		t.Fatal("future adapter is not advertised")
	}

	result, err := registry.Decode([]byte("anything"), "recipes.future")
	if err != nil {
		t.Fatal(err)
	}

	if result.Source.Format.ID != "future" || len(result.Recipes) != 1 || result.Recipes[0].Document.Title != "Plugged in" {
		t.Fatalf("result = %#v", result)
	}

	formats := registry.Formats()
	if len(formats) != 1 || formats[0].Version != "2" || !formats[0].Capabilities.Collection || !formats[0].Capabilities.SourceIdentity {
		t.Fatalf("formats = %#v", formats)
	}
}

func TestRegistryPrefersContentEvidenceToAMisleadingExtension(t *testing.T) {
	extension := fakeAdapter{
		format: importer.Format{ID: "extension", Name: "Extension guess", Extensions: []string{".json"}},
		probe:  importer.ProbeResult{Confidence: importer.ConfidenceExtension}, title: "Wrong",
	}
	content := fakeAdapter{
		format: importer.Format{ID: "content", Name: "Content match", Extensions: []string{".other"}},
		probe:  importer.ProbeResult{Confidence: importer.ConfidenceExact}, title: "Right",
	}

	result, err := importer.NewRegistry(extension, content).Import(context.Background(), importer.Input{
		Name: "misleading.json", Data: []byte("content signature"),
	}, importer.Options{})
	if err != nil {
		t.Fatal(err)
	}

	if result.Source.Format.ID != "content" || result.Recipes[0].Document.Title != "Right" {
		t.Fatalf("result = %#v", result)
	}
}

func TestDefaultRegistryProbesSREFByItsRootMember(t *testing.T) {
	format, probe, err := importer.DefaultRegistry().Probe(context.Background(), importer.Input{
		Name: "misleading.html", Data: []byte(`{"sref":{"version":"0.2.0","unit_registry":"0.2.0"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	if format.ID != "sref" || probe.Confidence != importer.ConfidenceExact {
		t.Fatalf("format=%#v probe=%#v", format, probe)
	}
}

func TestCookbookManagerAdapterReportsUnresolvedIngredientLines(t *testing.T) {
	t.Parallel()

	result, err := importer.DefaultRegistry().Import(context.Background(), importer.Input{
		Name: "review_1.yml",
		Data: []byte("name: Review\ningredients:\n  - 2 cups flour\n  - 200g butter\ndirections:\n  - Mix.\n"),
	}, importer.Options{})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Recipes) != 1 || len(result.Recipes[0].Diagnostics) != 1 {
		t.Fatalf("result = %#v", result)
	}

	diagnostic := result.Recipes[0].Diagnostics[0]
	if diagnostic.Code != "ingredient-review-required" || diagnostic.Class != importer.DiagnosticActionable ||
		diagnostic.Severity != importer.SeverityWarning || diagnostic.Area != "ingredients" ||
		diagnostic.Count != 1 || len(diagnostic.Targets) != 1 || diagnostic.Targets[0].Field != "quantity" ||
		diagnostic.Message != "Review 1 ambiguous ingredient amount." {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}

	resolution := diagnostic.Targets[0].Resolution
	if resolution == nil || resolution.Kind != "unit_alias" || resolution.AuthoredForm != "cups" ||
		len(resolution.CandidateUnitIDs) != 2 || resolution.ResolutionKey == "" {
		t.Fatalf("resolution descriptor = %#v", resolution)
	}

	imported := result.Recipes[0]
	if _, leaked := imported.Document.Extensions["x-cookbookmanager"]; leaked {
		t.Fatalf("importer-private metadata leaked into the portable recipe: %#v", imported.Document.Extensions)
	}

	if imported.ImporterMetadata == nil {
		t.Fatal("CookbookManager metadata was discarded instead of retained with the import candidate")
	}
}

func TestSREFPackageAdapterReturnsVerifiedAssetsBeforeCommit(t *testing.T) {
	document := testfixture.Document()

	document.ID = "adapter-package"
	for sectionIndex := range document.InstructionSections {
		for stepIndex := range document.InstructionSections[sectionIndex].Steps {
			document.InstructionSections[sectionIndex].Steps[stepIndex].ImageRefs = nil
		}
	}

	image := []byte("candidate image")
	digest := fmt.Sprintf("%x", sha256.Sum256(image))
	document.Assets = []recipe.Asset{{
		ID: "photo", Path: "assets/photo.jpg", MediaType: "image/jpeg", SHA256: digest,
		Size: int64(len(image)), Role: recipe.AssetRoleImage,
	}}
	document.ImageRefs, document.PrimaryImage = []string{"photo"}, "photo"

	packaged, err := sref.WritePackage(document, []sref.PackageAsset{{
		ID: "photo", Path: "assets/photo.jpg", MediaType: "image/jpeg", SHA256: digest, Bytes: image,
	}})
	if err != nil {
		t.Fatal(err)
	}

	result, err := importer.DefaultRegistry().Import(context.Background(), importer.Input{
		Name: "misleading.zip", Data: packaged,
	}, importer.Options{})
	if err != nil {
		t.Fatal(err)
	}

	if result.Source.Format.ID != "sref-package" || len(result.Recipes) != 1 ||
		!result.Recipes[0].PortableAssetReferences || len(result.Recipes[0].EmbeddedAssets) != 1 ||
		string(result.Recipes[0].EmbeddedAssets[0].Bytes) != string(image) {
		t.Fatalf("result = %#v", result)
	}
}

func TestSREFPackageExtensionRetainsUnsafePackageClassification(t *testing.T) {
	_, err := importer.DefaultRegistry().Import(context.Background(), importer.Input{
		Name: "broken.sref", Data: []byte("not a package"),
	}, importer.Options{})
	if !errors.Is(err, sref.ErrUnsafePackage) {
		t.Fatalf("error = %v", err)
	}
}
