package interop_test

import (
	"context"
	"os"
	"testing"

	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/units"
)

// TestAuthoredPreservationFloorSurvivesStorage holds preservation coverage for
// constructs the recipe-corpus targets no longer carry (source locale, authored
// yield, step ingredient uses, timing references, multiple sections), through
// the persistence boundary. The fixture is authored, not real-world evidence.
func TestAuthoredPreservationFloorSurvivesStorage(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/preservation-floor.recipe.json")
	if err != nil {
		t.Fatal(err)
	}

	// The fixture's own preconditions, before anything is claimed about
	// preserving them. An absent value round-tripping as absent proves nothing.
	authored, err := sref.Decode(data)
	if err != nil {
		t.Fatal(err)
	}

	assertPreservationFloor(t, "the fixture itself", authored)

	instance := newSavorum(t)

	result := instance.importOne(t, importer.Input{
		Name: "preservation-floor.recipe.json", MediaType: "application/json", Data: data,
	})

	// Read back out of SQLite rather than out of the importer's own result.
	assertPreservationFloor(t, "read back from storage", instance.stored(t, result.EntryID).Recipe)

	// The package a person downloads, written from what storage holds.
	archive, exported := instance.exportPackage(t, result.EntryID)
	assertPreservationFloor(t, "exported as a package", exported)

	// And back in through the ordinary import boundary, into a separate
	// library, so nothing is carried by the first one's memory.
	second := newSavorum(t)
	returned := second.importOne(t, importer.Input{
		Name: "preservation-floor.sref", MediaType: "application/octet-stream", Data: archive,
	})

	assertPreservationFloor(t, "re-imported and stored again", second.stored(t, returned.EntryID).Recipe)
}

// TestTheShippedMetricProfileSurvivesStorage carries the review decision
// through the boundary the preview cannot reach.
//
// The vectors and preview test do not check what is on disk afterwards.
func TestTheShippedMetricProfileSurvivesStorage(t *testing.T) {
	t.Parallel()

	instance := newSavorum(t)
	ctx := context.Background()

	preview, err := instance.inbound.PreviewFiles(ctx, []exchange.UploadedImport{{
		Filename: "metric.yml",
		Data: []byte("name: Metric storage\ningredients:\n" +
			"  - 1 cup natural yoghurt\n  - 2 teaspoons vanilla\n  - 200 grams flour\n" +
			"directions:\n  - Mix.\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	plan, err := instance.inbound.CreateMeasurementResolutionPlan(ctx, preview.ID,
		exchange.MeasurementResolutionPlanRequest{
			Interpretation: exchange.MeasurementResolutionInterpretation{
				Kind: exchange.ResolutionConvention, Convention: string(units.ConventionMetric),
			},
			ExpectedRevision: preview.Revision,
		})
	if err != nil {
		t.Fatal(err)
	}

	applied, err := instance.inbound.ApplyMeasurementResolutionPlan(ctx, preview.ID, plan.ID, preview.Revision)
	if err != nil {
		t.Fatal(err)
	}

	committed, err := instance.inbound.CommitPreview(ctx, preview.ID,
		[]string{applied.Preview.Items[0].ID}, library.DuplicateReplace)
	if err != nil {
		t.Fatal(err)
	}

	if len(committed.Items) != 1 {
		t.Fatalf("committed %d recipes", len(committed.Items))
	}

	assertMetricDecisionStored(t, "read back from storage", instance.stored(t, committed.Items[0].EntryID).Recipe)

	_, exported := instance.exportPackage(t, committed.Items[0].EntryID)
	assertMetricDecisionStored(t, "exported as a package", exported)
}

func assertMetricDecisionStored(t *testing.T, stage string, document recipe.Document) {
	t.Helper()

	ingredients := document.IngredientSections[0].Ingredients

	for index, want := range []string{
		"volume.cup.metric.culinary",
		"volume.teaspoon.metric.culinary",
		// Outside the convention's families, so its own evidence decided it.
		"mass.gram",
	} {
		got := ingredients[index]
		if got.Quantity == nil || got.Quantity.Unit != want {
			t.Fatalf("%s: %q = %#v, want %s", stage, got.SourceText, got.Quantity, want)
		}
	}

	// The provenance a decision leaves, and the authored text beside it.
	for _, index := range []int{0, 1} {
		normalization := ingredients[index].Normalization
		if normalization == nil || normalization.Method != recipe.NormalizationInferred ||
			normalization.Review != recipe.ReviewCorrected || len(normalization.Warnings) != 1 ||
			!recipe.ImportReviewInterpreted(normalization.Warnings[0]) {
			t.Fatalf("%s: %q normalization = %#v", stage, ingredients[index].SourceText, normalization)
		}

		if ingredients[index].SourceText == "" {
			t.Fatalf("%s: %q lost its authored text", stage, ingredients[index].Name)
		}
	}
}

// assertPreservationFloor checks the constructs by value and relationship
// rather than by count. Two sections and two steps is not the claim; the claim
// is that the glaze is still the glaze and still brushed on at the end.
func assertPreservationFloor(t *testing.T, stage string, document recipe.Document) {
	t.Helper()

	// A language-only locale, kept as a language without an invented region.
	if document.SourceLocale != "en" || document.Language != "en" {
		t.Fatalf("%s: locale = %q/%q", stage, document.Language, document.SourceLocale)
	}

	if document.FirstYield() == nil || document.FirstYield().Text != "makes 12 buns" {
		t.Fatalf("%s: yield = %#v", stage, document.FirstYield())
	}

	// Two ingredient sections, in order, with their titles and their contents.
	if len(document.IngredientSections) != 2 ||
		document.IngredientSections[0].ID != "dough" || document.IngredientSections[0].Title != "Dough" ||
		document.IngredientSections[1].ID != "glaze" || document.IngredientSections[1].Title != "Glaze" {
		t.Fatalf("%s: ingredient sections = %#v", stage, document.IngredientSections)
	}

	if len(document.IngredientSections[0].Ingredients) != 3 ||
		len(document.IngredientSections[1].Ingredients) != 1 {
		t.Fatalf("%s: section contents moved: %#v", stage, document.IngredientSections)
	}

	if len(document.InstructionSections) != 2 ||
		document.InstructionSections[0].Title != "Make the dough" ||
		document.InstructionSections[1].Title != "Bake and glaze" {
		t.Fatalf("%s: instruction sections = %#v", stage, document.InstructionSections)
	}

	// Timing assertions with their kind, attention and label, and the step
	// references that point at them. A reference surviving to a timing that
	// did not is not preservation.
	assertions := map[string]recipe.TimingAssertion{}
	for _, assertion := range document.Times.Assertions {
		assertions[assertion.ID] = assertion
	}

	prove, held := assertions["prove"]
	if !held || prove.Kind != "rest" || prove.Attention != "passive" ||
		prove.Duration.Value != "PT1H" || prove.Label != "First prove" {
		t.Fatalf("%s: prove assertion = %#v", stage, prove)
	}

	bake, held := assertions["bake"]
	if !held || bake.Kind != "cook" || bake.Attention != "active" || bake.Duration.Value != "PT25M" {
		t.Fatalf("%s: bake assertion = %#v", stage, bake)
	}

	proveStep := document.InstructionSections[0].Steps[1]
	if len(proveStep.TimingRefs) != 1 || proveStep.TimingRefs[0] != "prove" {
		t.Fatalf("%s: the proving step lost its timing reference: %#v", stage, proveStep)
	}

	bakeStep := document.InstructionSections[1].Steps[0]
	if len(bakeStep.TimingRefs) != 1 || bakeStep.TimingRefs[0] != "bake" ||
		len(bakeStep.Temperatures) != 1 || bakeStep.Temperatures[0].Unit != "temperature.celsius" {
		t.Fatalf("%s: the baking step = %#v", stage, bakeStep)
	}

	// A use with an independently stated portion beside uses with no quantity
	// at all, none of which sums to the ingredient's declared 60 grams.
	mixing := document.InstructionSections[0].Steps[0]
	if len(mixing.IngredientUses) != 2 {
		t.Fatalf("%s: mixing uses = %#v", stage, mixing.IngredientUses)
	}

	if mixing.IngredientUses[0].IngredientRef != "flour" || mixing.IngredientUses[0].Quantity != nil {
		t.Fatalf("%s: a quantity-less use did not survive: %#v", stage, mixing.IngredientUses[0])
	}

	half := mixing.IngredientUses[1]
	if half.IngredientRef != "butter" || half.Quantity == nil ||
		half.Quantity.Amount.Value.String() != "30" || half.Quantity.Unit != "mass.gram" {
		t.Fatalf("%s: the stated portion did not survive: %#v", stage, half)
	}

	glazing := document.InstructionSections[1].Steps[1]
	if len(glazing.IngredientUses) != 2 ||
		glazing.IngredientUses[1].IngredientRef != "butter" ||
		glazing.IngredientUses[1].Quantity != nil {
		t.Fatalf("%s: the remaining-butter use = %#v", stage, glazing.IngredientUses)
	}
}
