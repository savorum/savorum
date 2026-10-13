package sref_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/recipecodec"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/srefcheckout"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

// specimens lists the SREF example corpus used to verify specification
// compliance.
func specimens(t *testing.T) []string {
	t.Helper()
	specimenDirectory := srefcheckout.Path(t, "examples")

	entries, err := os.ReadDir(specimenDirectory)
	if err != nil {
		t.Fatal(err)
	}

	var paths []string

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			paths = append(paths, filepath.Join(specimenDirectory, entry.Name()))
		}
	}

	if len(paths) == 0 {
		srefcheckout.Unavailable(t, "no SREF examples found")
	}

	return paths
}

func TestSavorumReadsTheSpecificationsOwnExamples(t *testing.T) {
	t.Parallel()

	for _, path := range specimens(t) {
		data, err := readSREFFixture(path)
		if err != nil {
			t.Fatal(err)
		}

		document, err := sref.Decode(data)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(path), err)
			continue
		}
		// Reading it is not enough: what comes out has to be a recipe this
		// library would accept as its own.
		if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); err != nil {
			t.Errorf("%s: read but not storable: %v", filepath.Base(path), err)
		}
	}
}

func TestSpecificationExamplesExerciseTimeSummaryBoundaries(t *testing.T) {
	t.Parallel()
	specimenDirectory := srefcheckout.Path(t, "examples")
	read := func(name string) recipe.Document {
		t.Helper()

		data, err := readSREFFixture(filepath.Join(specimenDirectory, name))
		if err != nil {
			t.Fatal(err)
		}

		document, err := sref.Decode(data)
		if err != nil {
			t.Fatal(err)
		}

		return document
	}

	coarse := read("sourdough.recipe.json")
	if coarse.Times == nil || len(coarse.Times.Assertions) != 0 || recipe.SummarizeTimes(coarse).Elapsed == nil {
		t.Fatalf("coarse-only timing example lost its authored summary: %#v", coarse.Times)
	}

	partial := read("creme-anglaise.recipe.json")
	if partial.Times == nil || len(partial.Times.Assertions) == 0 || recipe.SummarizeTimes(partial).Elapsed != nil {
		t.Fatalf("partial phases were mistaken for an elapsed total: %#v", partial.Times)
	}

	complete := read("roast-chicken.recipe.json")
	if complete.Times == nil || len(complete.Times.Assertions) < 4 || len(stepsReferencing(complete, complete.Times.Assertions[1].ID)) == 0 {
		t.Fatalf("complete phase example lost its breakdown or step links: %#v", complete.Times)
	}
}

func TestReadingAndWritingKeepsTheRecipe(t *testing.T) {
	t.Parallel()

	for _, path := range specimens(t) {
		data, err := readSREFFixture(path)
		if err != nil {
			t.Fatal(err)
		}

		first, err := sref.Decode(data)
		if err != nil {
			continue
		}

		written, err := sref.Encode(first)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(path), err)
			continue
		}

		second, err := sref.Decode(written)
		if err != nil {
			t.Errorf("%s: what Savorum wrote it cannot read back: %v", filepath.Base(path), err)
			continue
		}

		if second.ID != first.ID || second.Title != first.Title {
			t.Errorf("%s: identity changed through a write", filepath.Base(path))
		}

		if len(second.IngredientSections) != len(first.IngredientSections) ||
			len(second.InstructionSections) != len(first.InstructionSections) {
			t.Errorf("%s: structure changed through a write", filepath.Base(path))
		}
	}
}

func TestTitleOnlySavorumStepUsesTitleAsRequiredSREFText(t *testing.T) {
	t.Parallel()

	document := recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            "title-only", Title: "Title only",
		IngredientSections:  []recipe.IngredientSection{{ID: "main", Ingredients: []recipe.Ingredient{{ID: "salt", Name: "salt"}}}},
		InstructionSections: []recipe.InstructionSection{{ID: "method", Steps: []recipe.Step{{ID: "season", Title: "Season to taste"}}}},
	}

	encoded, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := sref.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}

	step := decoded.InstructionSections[0].Steps[0]
	if step.Title != "Season to taste" || step.Text != "Season to taste" {
		t.Fatalf("SREF-compatible title-only step = %#v", step)
	}
}

func TestStructuredAuthorsRoundTripWithoutInference(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"sref":{"version":"0.2.0","unit_registry":"0.2.0"},
		"id":"authored","title":"Authored",
		"authors":[
			{"name":"A. Cook","kind":"person","url":"https://example.test/a-cook"},
			{"name":"Example Test Kitchen","kind":"organization"},
			{"name":"Unclassified Credit"}
		],
		"ingredient_sections":[{"id":"main","ingredients":[{"id":"salt","name":"salt"}]}],
		"instruction_sections":[{"id":"method","steps":[{"id":"season","text":"Season."}]}]
	}`)

	document, err := sref.Decode(input)
	if err != nil {
		t.Fatal(err)
	}

	want := []recipe.Author{
		{Name: "A. Cook", Kind: recipe.AuthorPerson, URL: "https://example.test/a-cook"},
		{Name: "Example Test Kitchen", Kind: recipe.AuthorOrganization},
		{Name: "Unclassified Credit"},
	}
	if !reflect.DeepEqual(document.Authors, want) {
		t.Fatalf("decoded authors = %#v, want %#v", document.Authors, want)
	}

	written, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	reread, err := sref.Decode(written)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(reread.Authors, want) {
		t.Fatalf("round-trip authors = %#v, want %#v", reread.Authors, want)
	}
}

func TestAuthoredMetadataRoundTripsInSourceOrderAndPrecision(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"sref":{"version":"0.2.0","unit_registry":"0.2.0"},
		"id":"metadata","title":"Metadata",
		"categories":["Cake","Celebration cake"],
		"courses":["Dessert","Afternoon tea"],
		"keywords":["layered","make-ahead"],
		"cuisines":["British","Australian"],
		"cooking_methods":["Baking","Whipping"],
		"published":"2023-05-04",
		"modified":"2025-06-07T08:09:10-06:00",
		"ingredient_sections":[{"id":"main","ingredients":[{"id":"flour","name":"flour"}]}],
		"instruction_sections":[{"id":"method","steps":[{"id":"bake","text":"Bake."}]}]
	}`)

	document, err := sref.Decode(input)
	if err != nil {
		t.Fatal(err)
	}

	written, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	reread, err := sref.Decode(written)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(reread, document) {
		t.Fatalf("authored metadata changed through SREF\n have %#v\n want %#v", reread, document)
	}

	if reread.Published != "2023-05-04" || reread.Modified != "2025-06-07T08:09:10-06:00" {
		t.Fatalf("source precision changed: published=%q modified=%q", reread.Published, reread.Modified)
	}
}

func TestTemperaturePurposesRoundTripIndependently(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"sref":{"version":"0.2.0","unit_registry":"0.2.0"},
		"id":"fried-chicken","title":"Fried chicken",
		"ingredient_sections":[{"id":"main","ingredients":[{"id":"chicken","name":"chicken"}]}],
		"instruction_sections":[{"id":"method","steps":[{
			"id":"fry","text":"Heat the oil and cook the chicken.",
			"temperatures":[
				{"amount":{"value":"350"},"unit":"temperature.fahrenheit","purpose":{"kind":"oil"}},
				{"amount":{"value":"165"},"unit":"temperature.fahrenheit","purpose":{"kind":"internal","label":"breast"}},
				{"amount":{"value":"240"},"unit":"temperature.fahrenheit","purpose":{"kind":"other","label":"sugar syrup"}}
			]
		}]}]
	}`)

	document, err := sref.Decode(input)
	if err != nil {
		t.Fatal(err)
	}

	temperatures := document.InstructionSections[0].Steps[0].Temperatures

	want := []*recipe.TemperaturePurpose{
		{Kind: recipe.TemperaturePurposeOil},
		{Kind: recipe.TemperaturePurposeInternal, Label: "breast"},
		{Kind: recipe.TemperaturePurposeOther, Label: "sugar syrup"},
	}
	for index := range want {
		if !reflect.DeepEqual(temperatures[index].Purpose, want[index]) {
			t.Fatalf("temperature %d purpose = %#v, want %#v", index, temperatures[index].Purpose, want[index])
		}
	}

	written, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	reread, err := sref.Decode(written)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(reread.InstructionSections[0].Steps[0].Temperatures, temperatures) {
		t.Fatalf("round-trip temperatures = %#v, want %#v", reread.InstructionSections[0].Steps[0].Temperatures, temperatures)
	}
}

func TestOpaqueTemperaturesRoundTripWithoutNumericInference(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"sref":{"version":"0.2.0","unit_registry":"0.2.0"},
		"id":"slow-bread","title":"Slow bread",
		"ingredient_sections":[{"id":"main","ingredients":[{"id":"flour","name":"flour"}]}],
		"instruction_sections":[{"id":"method","steps":[{
			"id":"bake","text":"Bake in a moderate oven.",
			"temperatures":[{"text":"moderate oven","purpose":{"kind":"oven"}},{"text":"quick oven"},{"text":"very slow oven"}]
		}]}]
	}`)

	document, err := sref.Decode(input)
	if err != nil {
		t.Fatal(err)
	}

	for _, temperature := range document.InstructionSections[0].Steps[0].Temperatures {
		if temperature.Text == "" || temperature.Unit != "" || temperature.Amount.Value != nil || temperature.Amount.Min != nil || temperature.Amount.Max != nil {
			t.Fatalf("opaque temperature gained a numeric interpretation: %#v", temperature)
		}
	}

	written, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	reread, err := sref.Decode(written)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(reread, document) {
		t.Fatalf("opaque temperatures changed through SREF\n have %#v\n want %#v", reread, document)
	}
}

func TestSREFRejectsOtherTemperaturePurposeWithoutLabel(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"sref":{"version":"0.2.0","unit_registry":"0.2.0"},
		"id":"unlabeled","title":"Unlabeled",
		"ingredient_sections":[{"id":"main","ingredients":[{"id":"syrup","name":"syrup"}]}],
		"instruction_sections":[{"id":"method","steps":[{
			"id":"heat","text":"Heat the syrup.",
			"temperatures":[{"amount":{"value":"240"},"unit":"temperature.fahrenheit","purpose":{"kind":"other"}}]
		}]}]
	}`)
	if _, err := sref.Decode(input); err == nil {
		t.Fatal("other temperature purpose without label was accepted")
	}
}

func TestSREFRejectsLegacyStringAuthors(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"sref":{"version":"0.2.0","unit_registry":"0.2.0"},
		"id":"legacy","title":"Legacy","authors":["A. Cook"],
		"ingredient_sections":[{"id":"main","ingredients":[{"id":"salt","name":"salt"}]}],
		"instruction_sections":[{"id":"method","steps":[{"id":"season","text":"Season."}]}]
	}`)
	if _, err := sref.Decode(input); err == nil {
		t.Fatal("legacy string author was accepted as current SREF")
	}
}

func TestCompoundQuantityAlternativeRoundTripsLosslessly(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"sref":{"version":"0.2.0","unit_registry":"0.2.0"},
		"id":"compound","title":"Compound alternative",
		"ingredient_sections":[{"id":"main","ingredients":[{"id":"oil","name":"oil","quantity":{"kind":"alternatives","options":[
			{"kind":"sum","terms":[
				{"kind":"simple","amount":{"value":"2"},"unit":"volume.tablespoon.us.customary"},
				{"kind":"simple","amount":{"value":"2"},"unit":"volume.teaspoon.us.customary"}
			]},
			{"kind":"simple","amount":{"value":"40"},"unit":"volume.milliliter"}
		]}}]}],
		"instruction_sections":[{"id":"method","steps":[{"id":"add","text":"Add the oil."}]}]
	}`)

	document, err := sref.Decode(input)
	if err != nil {
		t.Fatal(err)
	}

	options := document.IngredientSections[0].Ingredients[0].Quantity.Options
	if len(options) != 2 || options[0].Kind != recipe.QuantitySum || len(options[0].Terms) != 2 {
		t.Fatalf("decoded options = %#v", options)
	}

	written, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	reread, err := sref.Decode(written)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(reread.IngredientSections[0].Ingredients[0].Quantity.Options, options) {
		t.Fatalf("round-trip options = %#v, want %#v", reread.IngredientSections[0].Ingredients[0].Quantity.Options, options)
	}
}

func TestNestedQuantityAlternativesAreRejected(t *testing.T) {
	t.Parallel()

	input := []byte(`{"sref":{"version":"0.2.0","unit_registry":"0.2.0"},"id":"nested","title":"Nested","ingredient_sections":[{"id":"main","ingredients":[{"id":"oil","name":"oil","quantity":{"kind":"alternatives","options":[{"kind":"alternatives","options":[{"kind":"simple","amount":{"value":"1"}},{"kind":"simple","amount":{"value":"2"}}]},{"kind":"simple","amount":{"value":"3"}}]}}]}],"instruction_sections":[{"id":"method","steps":[{"id":"add","text":"Add."}]}]}`)
	if _, err := sref.Decode(input); err == nil {
		t.Fatal("nested alternative was accepted")
	}
}

func TestIngredientUsesRoundTripLosslessly(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"sref":{"version":"0.2.0","unit_registry":"0.2.0"},
		"id":"divided-sugar","title":"Divided sugar",
		"ingredient_sections":[{"id":"main","ingredients":[{"id":"sugar","name":"sugar","quantity":{"kind":"simple","amount":{"value":"2"},"unit":"volume.cup.us.customary"},"note":"divided"}]}],
		"instruction_sections":[{"id":"method","steps":[
			{"id":"mix","text":"Mix 1 1/2 cups sugar.","ingredient_uses":[{"ingredient_ref":"sugar","quantity":{"kind":"simple","amount":{"value":"3/2"},"unit":"volume.cup.us.customary"}}]},
			{"id":"finish","text":"Finish with 1/2 cup sugar.","ingredient_uses":[{"ingredient_ref":"sugar","quantity":{"kind":"simple","amount":{"value":"1/2"},"unit":"volume.cup.us.customary"}}]},
			{"id":"reserve","text":"Reserve a little for serving.","ingredient_uses":[{"ingredient_ref":"sugar","quantity":{"kind":"opaque","text":"a little"}}]}
		]}]
	}`)

	document, err := sref.Decode(input)
	if err != nil {
		t.Fatal(err)
	}

	uses := document.InstructionSections[0].Steps
	if len(uses[0].IngredientUses) != 1 || uses[0].IngredientUses[0].Quantity == nil || *uses[0].IngredientUses[0].Quantity.Amount.Value != "3/2" {
		t.Fatalf("simple ingredient use = %#v", uses[0].IngredientUses)
	}

	if uses[1].IngredientUses[0].Quantity == nil || *uses[1].IngredientUses[0].Quantity.Amount.Value != "1/2" {
		t.Fatalf("second divided ingredient use = %#v", uses[1].IngredientUses)
	}

	if uses[2].IngredientUses[0].Quantity == nil || uses[2].IngredientUses[0].Quantity.Kind != recipe.QuantityOpaque || uses[2].IngredientUses[0].Quantity.Text != "a little" {
		t.Fatalf("opaque ingredient use = %#v", uses[2].IngredientUses)
	}

	written, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(written, []byte(`ingredient_refs`)) || !bytes.Contains(written, []byte(`ingredient_uses`)) {
		t.Fatalf("wire relationship shape is not released SREF: %s", written)
	}

	decoded, err := sref.Decode(written)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(decoded.InstructionSections[0].Steps, document.InstructionSections[0].Steps) {
		t.Fatalf("ingredient uses changed on round trip:\n have %#v\n want %#v", decoded.InstructionSections[0].Steps, document.InstructionSections[0].Steps)
	}
}

func TestIngredientUseExtensionsFollowTheirReferenceWhenReordered(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"sref":{"version":"0.2.0","unit_registry":"0.2.0"},
		"id":"use-extensions","title":"Use extensions",
		"ingredient_sections":[{"id":"main","ingredients":[{"id":"sugar","name":"sugar"},{"id":"flour","name":"flour"}]}],
		"instruction_sections":[{"id":"method","steps":[{"id":"mix","text":"Mix.","ingredient_uses":[
			{"ingredient_ref":"sugar","x-org.example.note":"first addition"},
			{"ingredient_ref":"flour"}
		]}]}]
	}`)

	document, err := sref.Decode(input)
	if err != nil {
		t.Fatal(err)
	}

	uses := document.InstructionSections[0].Steps[0].IngredientUses
	uses[0], uses[1] = uses[1], uses[0]

	written, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	var output struct {
		InstructionSections []struct {
			Steps []struct {
				IngredientUses []map[string]any `json:"ingredient_uses"`
			} `json:"steps"`
		} `json:"instruction_sections"`
	}
	if err := json.Unmarshal(written, &output); err != nil {
		t.Fatal(err)
	}

	got := output.InstructionSections[0].Steps[0].IngredientUses
	if got[0]["ingredient_ref"] != "flour" || got[0]["x-org.example.note"] != nil {
		t.Fatalf("flour use received the sugar extension: %#v", got[0])
	}

	if got[1]["ingredient_ref"] != "sugar" || got[1]["x-org.example.note"] != "first addition" {
		t.Fatalf("sugar extension did not follow its reference: %#v", got[1])
	}
}

func TestAVersionThisBuildDoesNotImplementIsRefused(t *testing.T) {
	t.Parallel()

	future := []byte(`{"sref":{"version":"9.0.0","unit_registry":"9.0.0"},"id":"r","title":"T",
		"ingredient_sections":[],"instruction_sections":[]}`)
	if _, err := sref.Decode(future); err == nil {
		t.Fatal("a future SREF version was read as though it were this one")
	}
}

func TestCompatibleFutureMembersSurviveStorageEditingAndExport(t *testing.T) {
	t.Parallel()

	future := []byte(`{
		"sref":{"version":"0.5.0","unit_registry":"0.3.0","compatibility_notes":{"introduced_by":"0.5.0"}},
		"id":"future","title":"Future recipe","publication":{"status":"draft","sequence":184467440737095516160},
		"ingredient_sections":[{"id":"main","ingredients":[
			{"id":"salt","name":"salt"},
			{"id":"herbs","name":"herbs","quantity":{"kind":"simple","amount":{"value":"1"},"unit":"volume.measure.future"},"preparation_style":{"kind":"roughly-chopped"},"x-org.example.marker":{"kept":true,"large":184467440737095516160}}
		]}],
		"instruction_sections":[{"id":"method","steps":[
			{"id":"wait","text":"Wait."},
			{"id":"prepare","title":"Prepare the herbs","text":"Roughly chop the herbs.","ingredient_refs":["herbs"]}
		]}]
	}`)

	document, err := sref.Decode(future)
	if err != nil {
		t.Fatal(err)
	}

	if document.SREFOpaque == nil || document.Extensions == nil {
		t.Fatalf("future standard data or extensions were not retained: opaque=%#v extensions=%#v", document.SREFOpaque, document.Extensions)
	}

	if document.IngredientSections[0].Ingredients[1].Quantity.Unit != "volume.measure.future" {
		t.Fatalf("unknown registry ID changed: %#v", document.IngredientSections[0].Ingredients[1].Quantity)
	}

	if _, err := units.DefaultCatalog().Convert("1", "volume.measure.future", "volume.milliliter"); !errors.Is(err, units.ErrUnknownUnit) {
		t.Fatalf("unknown future unit conversion error = %v", err)
	}

	if _, err := units.DefaultCatalog().PresentNormalized("1", "volume.measure.future", units.MeasureMetric); !errors.Is(err, units.ErrUnknownUnit) {
		t.Fatalf("unknown future unit normalization error = %v", err)
	}

	stored, schemaVersion, err := recipecodec.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	document, err = recipecodec.Decode(stored, schemaVersion)
	if err != nil {
		t.Fatal(err)
	}

	// Reordering identified objects must not move their opaque data.
	document.IngredientSections[0].Ingredients[0], document.IngredientSections[0].Ingredients[1] = document.IngredientSections[0].Ingredients[1], document.IngredientSections[0].Ingredients[0]
	document.InstructionSections[0].Steps[0], document.InstructionSections[0].Steps[1] = document.InstructionSections[0].Steps[1], document.InstructionSections[0].Steps[0]

	written, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	var output map[string]any
	if err := json.Unmarshal(written, &output); err != nil {
		t.Fatal(err)
	}

	publication := mustType[map[string]any](output["publication"])
	if publication["status"] != "draft" {
		t.Fatalf("top-level future member was lost: %s", written)
	}

	header := mustType[map[string]any](output["sref"])
	if header["version"] != "0.5.0" || header["unit_registry"] != "0.3.0" {
		t.Fatalf("writer did not preserve the declared future versions: %s", written)
	}

	if header["compatibility_notes"] == nil {
		t.Fatalf("future version-envelope member was lost: %s", written)
	}

	if !strings.Contains(string(written), "184467440737095516160") {
		t.Fatalf("opaque future number changed through storage: %s", written)
	}

	ingredientSections := mustType[[]any](output["ingredient_sections"])
	ingredientSection := mustType[map[string]any](ingredientSections[0])
	ingredients := mustType[[]any](ingredientSection["ingredients"])

	firstIngredient := mustType[map[string]any](ingredients[0])
	if firstIngredient["id"] != "herbs" || firstIngredient["preparation_style"] == nil || firstIngredient["x-org.example.marker"] == nil {
		t.Fatalf("ingredient-scoped future data moved or disappeared: %s", written)
	}

	instructionSections := mustType[[]any](output["instruction_sections"])
	instructionSection := mustType[map[string]any](instructionSections[0])
	steps := mustType[[]any](instructionSection["steps"])

	firstStep := mustType[map[string]any](steps[0])
	if firstStep["id"] != "prepare" || firstStep["title"] != "Prepare the herbs" {
		t.Fatalf("step-scoped future data moved or disappeared: %s", written)
	}

	if _, err := sref.Decode(written); err != nil {
		t.Fatalf("rewritten future document cannot be read back: %v", err)
	}
}

func TestAnUnknownUnitRequiresANewerCompatibleRegistry(t *testing.T) {
	t.Parallel()

	current := []byte(`{"sref":{"version":"0.2.0","unit_registry":"0.2.0"},"id":"r","title":"T","ingredient_sections":[{"id":"s","ingredients":[{"id":"i","name":"salt","quantity":{"kind":"simple","amount":{"value":"1"},"unit":"volume.measure.future"}}]}],"instruction_sections":[{"id":"n","steps":[{"id":"p","text":"Wait."}]}]}`)
	if _, err := sref.Decode(current); err == nil || !strings.Contains(err.Error(), "unknown unit") {
		t.Fatalf("exact registry unknown-unit error = %v", err)
	}
}

func TestDetachedFutureDataMakesExportFail(t *testing.T) {
	t.Parallel()

	future := []byte(`{"sref":{"version":"0.5.0","unit_registry":"0.2.0"},"id":"r","title":"T","ingredient_sections":[{"id":"s","ingredients":[{"id":"keep","name":"salt"},{"id":"drop","name":"herbs","future_detail":true}]}],"instruction_sections":[{"id":"n","steps":[{"id":"p","text":"Wait."}]}]}`)

	document, err := sref.Decode(future)
	if err != nil {
		t.Fatal(err)
	}

	document.IngredientSections[0].Ingredients = document.IngredientSections[0].Ingredients[:1]
	validationErr := recipe.ValidateForWrite(document, units.DefaultCatalog())

	var validation *recipe.ValidationError
	if !errors.As(validationErr, &validation) {
		t.Fatalf("detached opaque data validation error = %v", validationErr)
	}

	found := false
	for _, violation := range validation.Violations {
		found = found || violation.Code == "sref_opaque.detached"
	}

	if !found {
		t.Fatalf("detached opaque data violations = %#v", validation.Violations)
	}

	_, err = sref.Encode(document)
	if err == nil {
		t.Fatalf("detached opaque data error = %v", err)
	}
}

func TestPreservedFutureDataCannotOverwriteAnUnderstoodMember(t *testing.T) {
	t.Parallel()

	document := recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            "r",
		Title:         "Original title",
		IngredientSections: []recipe.IngredientSection{
			{ID: "s", Ingredients: []recipe.Ingredient{{ID: "i", Name: "salt"}}},
		},
		InstructionSections: []recipe.InstructionSection{
			{ID: "n", Steps: []recipe.Step{{ID: "p", Text: "Wait."}}},
		},
		SREFOpaque: map[string]any{
			"document": map[string]any{"title": "Opaque title"},
		},
	}

	_, err := sref.Encode(document)
	if !errors.Is(err, sref.ErrOpaqueDataDetached) {
		t.Fatalf("opaque member collision error = %v", err)
	}
}

func TestAMemberSREFDoesNotDefineIsInvalid(t *testing.T) {
	t.Parallel()
	// Savorum's own storage discriminator is not part of the format, and a
	// member that is neither standard nor an extension makes a document
	// invalid rather than being quietly ignored.
	strange := []byte(`{"sref":{"version":"0.2.0","unit_registry":"0.2.0"},"id":"r","title":"T",
		"schema_version":1,"ingredient_sections":[],"instruction_sections":[]}`)
	if _, err := sref.Decode(strange); err == nil {
		t.Fatal("a member SREF does not define was accepted")
	}
}

func TestReaderRejectsDuplicateMembersBeforeTheyCollapse(t *testing.T) {
	t.Parallel()

	duplicate := []byte(`{"sref":{"version":"0.2.0","unit_registry":"0.2.0"},"id":"first","id":"second","title":"T","ingredient_sections":[{"id":"s","ingredients":[{"id":"i","name":"salt"}]}],"instruction_sections":[{"id":"n","steps":[{"id":"p","text":"Wait."}]}]}`)
	if _, err := sref.Decode(duplicate); err == nil || !strings.Contains(err.Error(), "duplicate JSON member") {
		t.Fatalf("duplicate member error = %v", err)
	}
}

func TestReaderEnforcesRecipeSemanticsAtTheBoundary(t *testing.T) {
	t.Parallel()

	invalid := []byte(`{"sref":{"version":"0.2.0","unit_registry":"0.2.0"},"id":"r","title":"T","times":{"phases":[{"kind":"rest","duration":"PT10M","step_refs":["missing"]}]},"ingredient_sections":[{"id":"s","ingredients":[{"id":"i","name":"salt"}]}],"instruction_sections":[{"id":"n","steps":[{"id":"p","text":"Wait."}]}]}`)
	if _, err := sref.Decode(invalid); err == nil {
		t.Fatal("a dangling time-phase step reference was accepted")
	}
}

func TestReaderRejectsAnExplicitlyEmptyOptionalArray(t *testing.T) {
	t.Parallel()

	invalid := []byte(`{"sref":{"version":"0.2.0","unit_registry":"0.2.0"},"id":"r","title":"T","authors":[],"ingredient_sections":[{"id":"s","ingredients":[{"id":"i","name":"salt"}]}],"instruction_sections":[{"id":"n","steps":[{"id":"p","text":"Wait."}]}]}`)
	if _, err := sref.Decode(invalid); err == nil || !strings.Contains(err.Error(), "authors must contain") {
		t.Fatalf("empty optional array error = %v", err)
	}
}

func TestUnknownExtensionNumbersRemainExact(t *testing.T) {
	t.Parallel()

	document := []byte(`{"sref":{"version":"0.2.0","unit_registry":"0.2.0"},"id":"r","title":"T","x-org.example.marker":{"large":184467440737095516160,"exponent":1e400},"ingredient_sections":[{"id":"s","ingredients":[{"id":"i","name":"salt","x-org.example.marker":{"large":184467440737095516160}}]}],"instruction_sections":[{"id":"n","steps":[{"id":"p","text":"Wait."}]}]}`)

	decoded, err := sref.Decode(document)
	if err != nil {
		t.Fatal(err)
	}

	written, err := sref.Encode(decoded)
	if err != nil {
		t.Fatal(err)
	}

	for _, exact := range []string{"184467440737095516160", "1e400"} {
		if !strings.Contains(string(written), exact) {
			t.Fatalf("extension number %s changed during round trip: %s", exact, written)
		}
	}
}

func TestTimedPhasesTravelAsAStandardMember(t *testing.T) {
	t.Parallel()

	document := recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            "rphases", Title: "Overnight",
		Times:               &recipe.Times{Assertions: []recipe.TimingAssertion{{ID: "t1", Kind: recipe.TimingMarinate, Duration: recipe.DurationExpression{Value: "PT8H"}}}},
		Extensions:          map[string]any{"x-org.example.other-tool": map[string]any{"kept": true}},
		IngredientSections:  []recipe.IngredientSection{{ID: "s", Ingredients: []recipe.Ingredient{{ID: "i", Name: "salt"}}}},
		InstructionSections: []recipe.InstructionSection{{ID: "n", Steps: []recipe.Step{{ID: "p", Text: "Wait.", TimingRefs: []string{"t1"}}}}},
	}

	written, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	// Timing assertions are a standard SREF member, so they travel as
	// themselves rather than in one application's namespace.
	if !contains(written, `"assertions"`) || !contains(written, `"timing_refs"`) {
		t.Fatalf("timing assertions did not travel as a standard member: %s", written)
	}

	if contains(written, `"phases"`) {
		t.Fatalf("the retired phase representation was written: %s", written)
	}

	if contains(written, "x-savorum") {
		t.Fatalf("timed phases still travel in a Savorum namespace: %s", written)
	}

	read, err := sref.Decode(written)
	if err != nil {
		t.Fatal(err)
	}

	if read.Times == nil || len(read.Times.Assertions) != 1 || read.Times.Assertions[0].Kind != recipe.TimingMarinate {
		t.Fatalf("timing assertions did not survive: %#v", read.Times)
	}

	if got := stepsReferencing(read, read.Times.Assertions[0].ID); len(got) != 1 || got[0] != "p" {
		t.Fatalf("step timing references did not survive: %#v", got)
	}
	// Another application's extension is not ours to discard.
	if _, kept := read.Extensions["x-org.example.other-tool"]; !kept {
		t.Fatalf("an extension from another application was dropped: %#v", read.Extensions)
	}
}

func TestSREFRejectsTheRetiredStepDuration(t *testing.T) {
	t.Parallel()
	// SREF 0.1 replaced a scalar duration on a step with references into
	// times.assertions. A document still carrying the old member is not a
	// document to translate: reading it would mean guessing which canonical
	// event it was meant to be, and the format says what to write instead.
	legacy := []byte(`{"sref":{"version":"0.2.0","unit_registry":"0.2.0"},"id":"r","title":"T","ingredient_sections":[{"id":"s","ingredients":[{"id":"i","name":"salt"}]}],"instruction_sections":[{"id":"n","steps":[{"id":"p","text":"Wait.","duration":"PT10M"}]}]}`)
	if _, err := sref.Decode(legacy); err == nil {
		t.Fatal("a step carrying the retired duration member was accepted")
	}
}

func TestSREFWritesOpaqueAndRangedDurations(t *testing.T) {
	t.Parallel()
	// The three duration shapes have to survive a round trip, including the
	// opaque one, which is the whole of what some recipes say about time.
	document := recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            "durations", Title: "Durations",
		Times: &recipe.Times{
			Additional: &recipe.DurationExpression{Text: "overnight"},
			Cook:       &recipe.DurationExpression{Min: "PT40M", Max: "PT50M", Approximate: true, SourceText: "about 40–50 minutes"},
			Assertions: []recipe.TimingAssertion{
				{ID: "soak", Kind: recipe.TimingSoak, Duration: recipe.DurationExpression{Text: "until softened"}, Attention: recipe.AttentionPassive},
				{ID: "bake", Kind: recipe.TimingCook, Label: "Bake", Duration: recipe.DurationExpression{Min: "PT20M", Max: "PT25M", Approximate: true}},
			},
		},
		IngredientSections:  []recipe.IngredientSection{{ID: "s", Ingredients: []recipe.Ingredient{{ID: "i", Name: "beans"}}}},
		InstructionSections: []recipe.InstructionSection{{ID: "n", Steps: []recipe.Step{{ID: "p", Text: "Soak, then bake.", TimingRefs: []string{"soak", "bake"}}}}},
	}

	written, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	read, err := sref.Decode(written)
	if err != nil {
		t.Fatal(err)
	}

	if read.Times.Additional == nil || read.Times.Additional.Text != "overnight" {
		t.Errorf("opaque summary lost: %#v", read.Times.Additional)
	}

	if read.Times.Cook == nil || read.Times.Cook.Min != "PT40M" || read.Times.Cook.Max != "PT50M" || !read.Times.Cook.Approximate {
		t.Errorf("ranged summary lost: %#v", read.Times.Cook)
	}

	if len(read.Times.Assertions) != 2 || read.Times.Assertions[0].Duration.Text != "until softened" {
		t.Fatalf("opaque assertion lost: %#v", read.Times.Assertions)
	}

	if read.Times.Assertions[0].Attention != recipe.AttentionPassive {
		t.Errorf("attention lost: %#v", read.Times.Assertions[0])
	}

	if read.Times.Assertions[1].Label != "Bake" || read.Times.Assertions[1].Duration.Max != "PT25M" {
		t.Errorf("ranged assertion lost: %#v", read.Times.Assertions[1])
	}

	if got := stepsReferencing(read, "bake"); len(got) != 1 || got[0] != "p" {
		t.Errorf("one step referring to two assertions did not survive: %#v", got)
	}
	// Provenance is Savorum's own record and must not appear in the document.
	if bytes.Contains(written, []byte("provenance")) {
		t.Errorf("Savorum-local provenance was exported: %s", written)
	}
}

func contains(data []byte, needle string) bool {
	return len(data) > 0 && len(needle) > 0 && string(data) != "" &&
		bytesContains(string(data), needle)
}

func bytesContains(haystack, needle string) bool {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}

	return false
}

// stepsReferencing lists the steps pointing at one assertion.
func stepsReferencing(document recipe.Document, assertionID string) []string {
	var steps []string

	for _, section := range document.InstructionSections {
		for _, step := range section.Steps {
			for _, ref := range step.TimingRefs {
				if ref == assertionID {
					steps = append(steps, step.ID)
				}
			}
		}
	}

	return steps
}

// Both members SREF standardized after 0.1 travel through interchange. The
// obligation differs between them: dropping optional: true while keeping
// "(optional)" in source_text is lossy, whereas preserving a variant's title
// and text without interpreting it loses nothing the format claims to carry.
func TestOptionalityAndVariantsSurviveInterchange(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.IngredientSections[0].Ingredients[0].Optional = true
	document.Variants = []recipe.Variant{
		{Title: "Richer", Text: "Add 100 ml cream with the stock."},
		{Text: "Follow the chocolate variation above but use orange zest."},
	}

	encoded, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Contains(encoded, []byte(`"optional"`)) || !bytes.Contains(encoded, []byte(`"variants"`)) {
		t.Fatalf("encoded document did not carry both members: %s", encoded)
	}

	decoded, err := sref.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if !decoded.IngredientSections[0].Ingredients[0].Optional {
		t.Fatal("optionality did not survive interchange")
	}
	// Order is part of what a variant is, so it is preserved rather than
	// treated as a set.
	if !reflect.DeepEqual(decoded.Variants, document.Variants) {
		t.Fatalf("variants = %#v", decoded.Variants)
	}
}

func TestRequiredEquipmentSurvivesInterchangeWithoutInventedIdentity(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.Equipment = []recipe.Equipment{
		{Name: "heavy-bottomed saucepan", Note: "Use the largest one."},
		{Name: "ramekins", Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: testfixture.Rational("6")}}},
	}

	encoded, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := sref.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(decoded.Equipment, document.Equipment) {
		t.Fatalf("equipment = %#v", decoded.Equipment)
	}

	if bytes.Contains(encoded, []byte(`"equipment_ref"`)) || bytes.Contains(encoded, []byte(`"equipment_id"`)) {
		t.Fatalf("interchange invented equipment identity: %s", encoded)
	}
}

// A variant is not a nested recipe and does not restructure the one it belongs
// to. Savorum's strict reader refuses a document that says otherwise rather
// than carrying a shape the format forbids.
func TestAVariantMayNotNestOrRestructureTheRecipe(t *testing.T) {
	t.Parallel()

	for key, member := range map[string]any{
		"variants":             []any{map[string]any{"text": "inner"}},
		"ingredient_sections":  []any{},
		"instruction_sections": []any{},
		"yield":                map[string]any{"text": "4"},
		"times":                map[string]any{"total": map[string]any{"iso8601": "PT1H"}},
	} {
		document := testfixture.Document()
		document.Variants = []recipe.Variant{{Text: "placeholder"}}

		encoded, err := sref.Encode(document)
		if err != nil {
			t.Fatal(err)
		}

		var shaped map[string]any
		if err := json.Unmarshal(encoded, &shaped); err != nil {
			t.Fatal(err)
		}

		variants, ok := shaped["variants"].([]any)
		if !ok || len(variants) != 1 {
			t.Fatalf("%s: encoded document has no variant to tamper with", key)
		}

		mustType[map[string]any](variants[0])[key] = member

		tampered, err := json.Marshal(shaped)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := sref.Decode(tampered); err == nil {
			t.Fatalf("%s: a variant carrying it was accepted", key)
		}
	}
}

// TestAMemberIsAdmittedByTheVersionThatDeclaresIt: an unknown standard member
// is refused at the declared version and preserved only under a newer
// compatible one, because a new standardized field requires a version
// increment (SREF sections 5 and 13).
func TestAMemberIsAdmittedByTheVersionThatDeclaresIt(t *testing.T) {
	t.Parallel()

	for name, at := range map[string]func(map[string]any){
		"root": func(shaped map[string]any) {
			shaped["future_standard_field"] = map[string]any{"value": 4}
		},
		"ingredient": func(shaped map[string]any) {
			sections := mustType[[]any](shaped["ingredient_sections"])
			firstSection := mustType[map[string]any](sections[0])
			ingredients := mustType[[]any](firstSection["ingredients"])
			mustType[map[string]any](ingredients[0])["future_member"] = true
		},
	} {
		encoded, err := sref.Encode(testfixture.Document())
		if err != nil {
			t.Fatal(err)
		}

		var shaped map[string]any
		if err := json.Unmarshal(encoded, &shaped); err != nil {
			t.Fatal(err)
		}

		at(shaped)

		exact, err := json.Marshal(shaped)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := sref.Decode(exact); err == nil {
			t.Errorf("%s: a member the declared version does not define was accepted", name)
		}

		// The same member under a newer version in the same line is exactly
		// what forward-compatible reading is for, and must survive.
		mustType[map[string]any](shaped["sref"])["version"] = "0.5.0"

		newer, err := json.Marshal(shaped)
		if err != nil {
			t.Fatal(err)
		}

		document, err := sref.Decode(newer)
		if err != nil {
			t.Errorf("%s: a compatible newer document carrying it was refused: %v", name, err)
			continue
		}

		rewritten, err := sref.Encode(document)
		if err != nil {
			t.Fatal(err)
		}

		var round map[string]any
		if err := json.Unmarshal(rewritten, &round); err != nil {
			t.Fatal(err)
		}

		if mustType[map[string]any](round["sref"])["version"] != "0.5.0" {
			t.Errorf("%s: the declared newer version was not preserved", name)
		}

		if !strings.Contains(string(rewritten), "future_") {
			t.Errorf("%s: the unknown member did not survive the round trip", name)
		}
	}
}

func TestExtensionNamesCarryADomainAndAtLeastOneLocalLabel(t *testing.T) {
	t.Parallel()
	// A name identifies its publisher by a reversed domain and then says which
	// of that publisher's extensions it is. Two labels do the first and not the
	// second, and there is no cap on how deep the second part goes.
	valid := []string{
		"x-org.example.marker",
		"x-org.example.import.paprika",
		"x-com.example.recipe.display.compact",
		"x-sref.spec.round-trip.ordinal",
		"x-a1.b2.c3",
		"x-org.ex-ample.marker",
	}
	invalid := []string{
		"x-org.example",
		"x-vendor",
		"x-",
		"x-org..example.marker",
		"x-org.-example.marker",
		"x-org.example-.marker",
		"x-Org.Example.Marker",
		"x-org.example.marker.",
		"x-" + strings.Repeat("a", 64) + ".example.marker",
		"x-org.example." + strings.Repeat("a", 120),
	}

	for _, name := range valid {
		document := recipe.Document{
			SchemaVersion: recipe.CurrentDocumentSchemaVersion,
			ID:            "extension-names", Title: "Extension names",
			IngredientSections:  []recipe.IngredientSection{{ID: "s", Ingredients: []recipe.Ingredient{{ID: "i", Name: "salt"}}}},
			InstructionSections: []recipe.InstructionSection{{ID: "m", Steps: []recipe.Step{{ID: "p", Text: "Cook."}}}},
			Extensions:          map[string]any{name: map[string]any{"kept": true}},
		}

		written, err := sref.Encode(document)
		if err != nil {
			t.Errorf("%s should be writable: %v", name, err)
			continue
		}

		if _, err := sref.Decode(written); err != nil {
			t.Errorf("%s should be readable: %v", name, err)
		}
	}

	for _, name := range invalid {
		body := []byte(`{"sref":{"version":"0.2.0","unit_registry":"0.2.0"},"id":"r","title":"T",
			"ingredient_sections":[{"id":"s","ingredients":[{"id":"i","name":"salt"}]}],
			"instruction_sections":[{"id":"m","steps":[{"id":"p","text":"Cook."}]}],
			"` + name + `":{"kept":true}}`)
		if _, err := sref.Decode(body); err == nil {
			t.Errorf("%s should not be readable", name)
		}
	}
}

// A document controls what sits under "extensions", and the name walk reads it
// before any shape check does, so a scalar there must be refused, not panic.
func TestANonObjectExtensionsContainerIsRefusedRatherThanPanicking(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"5", `"text"`, "[]", "null", "true"} {
		body := []byte(`{"sref":{"version":"0.2.0","unit_registry":"0.2.0"},"id":"r","title":"T",
			"ingredient_sections":[{"id":"s","ingredients":[{"id":"i","name":"salt"}]}],
			"instruction_sections":[{"id":"m","steps":[{"id":"p","text":"Cook."}]}],
			"extensions":` + value + `}`)
		if _, err := sref.Decode(body); err == nil {
			t.Errorf("extensions=%s should not be readable", value)
		}
	}
}

func TestMinimumRecipeRoundTrip(t *testing.T) {
	for _, member := range []string{"ingredient_sections", "instruction_sections"} {
		t.Run(member, func(t *testing.T) {
			document := testfixture.Document()
			// This fixture's steps reference ingredients, so use an independent procedure.
			document.InstructionSections = []recipe.InstructionSection{{ID: "method", Steps: []recipe.Step{{ID: "rest", Text: "Rest."}}}}
			if member == "ingredient_sections" {
				document.IngredientSections = nil
			} else {
				document.InstructionSections = nil
			}

			data, err := sref.Encode(document)
			if err != nil {
				t.Fatal(err)
			}

			var wire map[string]json.RawMessage
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}

			if _, present := wire[member]; present {
				t.Fatalf("absent %s was fabricated", member)
			}

			decoded, err := sref.Decode(data)
			if err != nil {
				t.Fatal(err)
			}

			if len(decoded.IngredientSections) != len(document.IngredientSections) || len(decoded.InstructionSections) != len(document.InstructionSections) {
				t.Fatal("changed collection presence")
			}

			wire[member] = json.RawMessage(`[]`)

			invalid, _ := json.Marshal(wire)
			if _, err := sref.Decode(invalid); err == nil {
				t.Fatal("accepted empty collection")
			}
		})
	}

	stub := testfixture.Document()
	stub.IngredientSections = nil

	stub.InstructionSections = nil
	if _, err := sref.Encode(stub); err == nil {
		t.Fatal("exported local stub as SREF")
	}

	if _, err := sref.Decode([]byte(`{"sref":{"version":"0.2.0","unit_registry":"0.2.0"},"id":"stub","title":"Stub","notes":["Remember this recipe"]}`)); err == nil {
		t.Fatal("imported metadata-only SREF")
	}
}

func TestAuthoredChoicesSurviveStorageAndScopedExtensionReordering(t *testing.T) {
	data, err := readSREFFixture(srefcheckout.Path(t, "tests/roundtrip/ingredient-choices.recipe.json"))
	if err != nil {
		t.Fatal(err)
	}

	document, err := sref.Decode(data)
	if err != nil {
		t.Fatal(err)
	}

	encoded, version, err := recipecodec.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	stored, err := recipecodec.Decode(encoded, version)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(stored.IngredientSections, document.IngredientSections) {
		t.Fatal("storage changed choice semantics")
	}

	branches := stored.IngredientSections[0].Ingredients[0].Alternatives
	branches[0], branches[1] = branches[1], branches[0]

	exchanged, err := sref.Encode(stored)
	if err != nil {
		t.Fatal(err)
	}

	var wire map[string]any
	if err := json.Unmarshal(exchanged, &wire); err != nil {
		t.Fatal(err)
	}

	sections := mustType[[]any](wire["ingredient_sections"])
	section := mustType[map[string]any](sections[0])
	ingredients := mustType[[]any](section["ingredients"])
	choice := mustType[map[string]any](ingredients[0])

	reordered := mustType[[]any](choice["alternatives"])
	if _, ok := mustType[map[string]any](reordered[1])["x-org.example.choice"]; !ok {
		t.Fatal("branch extension lost its identity")
	}

	if _, ok := mustType[map[string]any](reordered[0])["x-org.example.choice"]; ok {
		t.Fatal("branch extension moved to another ingredient")
	}

	if _, ok := choice["name"]; ok {
		t.Fatal("choice acquired an ordinary ingredient name")
	}
}

func readSREFFixture(path string) ([]byte, error) {
	// Callers resolve paths from the trusted sibling SREF checkout or fixed
	// repository source paths, never from runtime or request input.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read SREF fixture: %w", err)
	}

	return data, nil
}

func mustType[T any](value any) T {
	typed, ok := value.(T)
	if !ok {
		panic(fmt.Sprintf("test fixture value has type %T", value))
	}

	return typed
}

func TestCompatibleCompoundAlternativeKeepsSupportedTermsAndUnknownMembers(t *testing.T) {
	t.Parallel()

	input := []byte(`{"sref":{"version":"0.5.0","unit_registry":"0.2.0"},"id":"future-compound","title":"Compound recipe",
 "ingredient_sections":[{"id":"main","ingredients":[{"id":"oil","name":"oil","quantity":{"kind":"alternatives","options":[
 {"kind":"sum","terms":[{"kind":"simple","amount":{"value":"2"},"unit":"volume.tablespoon.us.customary"},{"kind":"simple","amount":{"value":"2"},"unit":"volume.teaspoon.us.customary"}],"future_note":{"blank":"","empty":[],"missing":null}},
 {"kind":"simple","amount":{"value":"40"},"unit":"volume.milliliter"}
 ]}}]}],"instruction_sections":[{"id":"method","steps":[{"id":"add","text":"Add the oil."}]}]}`)

	document, err := sref.Decode(input)
	if err != nil {
		t.Fatal(err)
	}

	if got := len(document.IngredientSections[0].Ingredients[0].Quantity.Options[0].Terms); got != 2 {
		t.Fatalf("sum terms = %d, want 2", got)
	}

	document.Title = "Edited compound recipe"

	encoded, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	var before, after map[string]any
	if err := json.Unmarshal(input, &before); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(encoded, &after); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(before["ingredient_sections"], after["ingredient_sections"]) {
		t.Fatalf("quantity or unknown members changed: %s", encoded)
	}
}
