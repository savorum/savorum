package recipecodec

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestRoundTripUsesExplicitSchemaVersion(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()

	encoded, version, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	if version != CurrentVersion {
		t.Fatalf("encoded schema version = %d", version)
	}

	var envelope struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}

	if envelope.SchemaVersion != CurrentVersion {
		t.Fatalf("inline schema version = %d", envelope.SchemaVersion)
	}

	decoded, err := Decode(encoded, version)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(document, decoded) {
		t.Fatalf("round trip changed document\nwritten: %#v\nread: %#v", document, decoded)
	}
}

func TestCurrentSchemaPersistsStepTitlesAndTitleOnlySteps(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	step := &document.InstructionSections[0].Steps[0]
	step.Title, step.Text = "Make the roux", ""

	encoded, version, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(encoded, version)
	if err != nil {
		t.Fatal(err)
	}

	got := decoded.InstructionSections[0].Steps[0]
	if got.Title != "Make the roux" || got.Text != "" {
		t.Fatalf("stored title-only step = %#v", got)
	}
}

func TestCurrentSchemaPersistsOpaqueFutureSREFMembers(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.SREFOpaque = map[string]any{
		"ingredient:flour": map[string]any{"preparation_style": map[string]any{"kind": "sifted"}},
	}

	encoded, version, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(encoded, version)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(decoded.SREFOpaque, document.SREFOpaque) {
		t.Fatalf("opaque SREF sidecar changed:\n have %#v\n want %#v", decoded.SREFOpaque, document.SREFOpaque)
	}
}

func TestCurrentSchemaPersistsAuthoredMetadata(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.Categories = []string{"Cake", "Celebration cake"}
	document.Courses = []string{"Dessert", "Afternoon tea"}
	document.CookingMethods = []string{"Baking", "Whipping"}
	document.Published = "2023-05-04"
	document.Modified = "2025-06-07T08:09:10Z"

	encoded, version, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(encoded, version)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(decoded, document) {
		t.Fatalf("authored metadata changed through storage\n have %#v\n want %#v", decoded, document)
	}
}

func TestCurrentSchemaPersistsRequiredEquipment(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.Equipment = []recipe.Equipment{
		{Name: "8-inch cake pan", Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: testfixture.Rational("2")}}, Note: "greased"},
		{Name: "stand mixer with dough hook"},
	}

	encoded, version, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(encoded, version)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(decoded.Equipment, document.Equipment) {
		t.Fatalf("equipment changed through storage\n have %#v\n want %#v", decoded.Equipment, document.Equipment)
	}
}

func TestCurrentDecodeRemainsStrict(t *testing.T) {
	t.Parallel()

	encoded, _, err := Encode(testfixture.Document())
	if err != nil {
		t.Fatal(err)
	}

	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}

	document["future_field"] = true

	encoded, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Decode(encoded, CurrentVersion); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("strict decode error = %v", err)
	}
}

func TestCurrentStructuredAuthorsRoundTripLosslessly(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.Authors = []recipe.Author{
		{Name: "A. Cook", Kind: recipe.AuthorPerson, URL: "https://example.test/a-cook"},
		{Name: "Collective", Kind: recipe.AuthorOther, KindLabel: "community collective"},
	}

	encoded, version, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(encoded, version)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(decoded.Authors, document.Authors) {
		t.Fatalf("stored authors = %#v, want %#v", decoded.Authors, document.Authors)
	}
}

func TestCurrentTemperaturePurposeRoundTripsLosslessly(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.IngredientSections[0].Ingredients[1].Temperature.Purpose = &recipe.TemperaturePurpose{
		Kind: recipe.TemperaturePurposeInternal, Label: "butter center",
	}
	temperature := &document.InstructionSections[0].Steps[0].Temperatures
	*temperature = []recipe.Temperature{
		{
			Amount:  recipe.Amount{Value: testfixture.Rational("350")},
			Unit:    "temperature.fahrenheit",
			Purpose: &recipe.TemperaturePurpose{Kind: recipe.TemperaturePurposeOil},
		},
		{
			Amount:  recipe.Amount{Value: testfixture.Rational("165")},
			Unit:    "temperature.fahrenheit",
			Purpose: &recipe.TemperaturePurpose{Kind: recipe.TemperaturePurposeOther, Label: "sugar syrup"},
		},
	}

	encoded, version, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(encoded, version)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(decoded.InstructionSections[0].Steps[0].Temperatures, *temperature) {
		t.Fatalf("stored temperatures = %#v, want %#v", decoded.InstructionSections[0].Steps[0].Temperatures, *temperature)
	}

	if !reflect.DeepEqual(decoded.IngredientSections[0].Ingredients[1].Temperature.Purpose, document.IngredientSections[0].Ingredients[1].Temperature.Purpose) {
		t.Fatalf("stored ingredient temperature purpose = %#v, want %#v", decoded.IngredientSections[0].Ingredients[1].Temperature.Purpose, document.IngredientSections[0].Ingredients[1].Temperature.Purpose)
	}
}

func TestDecodeRejectsMismatchedAndUnsupportedVersions(t *testing.T) {
	t.Parallel()

	encoded, _, err := Encode(testfixture.Document())
	if err != nil {
		t.Fatal(err)
	}

	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}

	document["schema_version"] = CurrentVersion + 1

	mismatched, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Decode(mismatched, CurrentVersion); !errors.Is(err, ErrSchemaVersionMismatch) {
		t.Fatalf("mismatched version error = %v", err)
	}

	_, err = Decode(encoded, CurrentVersion+1)
	if !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("unsupported version error = %v", err)
	}

	want := fmt.Sprintf("recipe requires document schema version %d; this Savorum version supports through version %d", CurrentVersion+1, CurrentVersion)
	if err.Error() != want {
		t.Fatalf("unsupported version message = %q", err)
	}
}

func TestDecodeRequiresInlineSchemaVersion(t *testing.T) {
	t.Parallel()

	encoded, _, err := Encode(testfixture.Document())
	if err != nil {
		t.Fatal(err)
	}

	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}

	delete(document, "schema_version")

	encoded, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Decode(encoded, CurrentVersion); !errors.Is(err, ErrSchemaVersionMismatch) {
		t.Fatalf("missing inline schema version error = %v", err)
	}
}

func TestEncodeRejectsNonCurrentDomainDocument(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()

	document.SchemaVersion = recipe.CurrentDocumentSchemaVersion + 1
	if _, _, err := Encode(document); !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("encode error = %v", err)
	}
}

func TestCompoundQuantityAlternativeSurvivesStorage(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.IngredientSections[0].Ingredients[0].Quantity = &recipe.Quantity{
		Kind: recipe.QuantityAlternatives,
		Options: []recipe.AlternativeOption{
			{Kind: recipe.QuantitySum, Terms: []recipe.SimpleQuantity{
				{Kind: recipe.QuantitySimple, Amount: recipe.Amount{Value: testfixture.Rational("2")}, Unit: "volume.tablespoon.us.customary"},
				{Kind: recipe.QuantitySimple, Amount: recipe.Amount{Value: testfixture.Rational("2")}, Unit: "volume.teaspoon.us.customary"},
			}},
			{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: testfixture.Rational("40")}, Unit: "volume.milliliter"},
		},
	}

	encoded, version, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(encoded, version)
	if err != nil {
		t.Fatal(err)
	}

	options := decoded.IngredientSections[0].Ingredients[0].Quantity.Options
	if len(options) != 2 || options[0].Kind != recipe.QuantitySum || len(options[0].Terms) != 2 || options[1].Amount == nil || *options[1].Amount.Value != "40" {
		t.Fatalf("stored options = %#v", options)
	}
}

func TestIngredientUseQuantitiesSurviveStorage(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.InstructionSections[0].Steps[0].IngredientUses[0].Quantity = &recipe.Quantity{
		Kind: recipe.QuantityAlternatives,
		Options: []recipe.AlternativeOption{
			{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: testfixture.Rational("3/2")}, Unit: "volume.cup.us.customary"},
			{Kind: recipe.QuantitySum, Terms: []recipe.SimpleQuantity{
				{Kind: recipe.QuantitySimple, Amount: recipe.Amount{Value: testfixture.Rational("1")}, Unit: "volume.cup.us.customary"},
				{Kind: recipe.QuantitySimple, Amount: recipe.Amount{Value: testfixture.Rational("8")}, Unit: "volume.tablespoon.us.customary"},
			}},
		},
	}

	encoded, version, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(encoded, version)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(decoded.InstructionSections[0].Steps[0].IngredientUses, document.InstructionSections[0].Steps[0].IngredientUses) {
		t.Fatalf("stored ingredient uses = %#v", decoded.InstructionSections[0].Steps[0].IngredientUses)
	}
}

func TestImageRelationshipsSurviveStorage(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.ImageRefs = []string{"finished-pies"}
	document.InstructionSections[0].Steps[0].ImageRefs = []string{"finished-pies"}

	encoded, version, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(encoded, version)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(decoded.ImageRefs, document.ImageRefs) || !reflect.DeepEqual(decoded.InstructionSections[0].Steps[0].ImageRefs, document.InstructionSections[0].Steps[0].ImageRefs) {
		t.Fatalf("stored image relationships = recipe %#v step %#v", decoded.ImageRefs, decoded.InstructionSections[0].Steps[0].ImageRefs)
	}
}

// TestRetiredUnitsUpcastToTheirCurrentIdentity checks that a stored recipe
// survives a registry rename: `volume.cup.metric` held 240 mL, so it must not
// become the 250 mL metric culinary cup.

// TestUpgradeCarriesAttentionOnlyFromTheAuthoredFlag: v1 kinds were inferred
// from prose, so only `Active` may set attention.

// TestUpgradeInvertsStepReferencesConservatively checks that every phase
// becomes exactly one assertion, in order, and look-alike phases stay two.

// A v8 snapshot predates both members, and neither can be inferred from what
// it stored. Upgrading widens rather than guessing: no recipe acquires an
// optional ingredient or a variation it never had.

// Both members survive a store and read at the current version.
func TestOptionalityAndVariantsSurviveAStorageRoundTrip(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.Variants = []recipe.Variant{
		{Title: "Richer", Text: "Add 100 ml cream with the stock."},
		{Text: "Follow the chocolate variation above but use orange zest."},
	}
	document.IngredientSections[0].Ingredients[0].Optional = true

	encoded, version, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(encoded, version)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(decoded.Variants, document.Variants) {
		t.Fatalf("variants = %#v", decoded.Variants)
	}

	if !decoded.IngredientSections[0].Ingredients[0].Optional {
		t.Fatal("optionality did not survive storage")
	}
	// Optionality belongs to the occurrence and must not spread to another
	// occurrence naming the same thing.
	for _, ingredient := range decoded.IngredientSections[0].Ingredients[1:] {
		if ingredient.Optional {
			t.Fatalf("optionality spread to %q", ingredient.Name)
		}
	}
}

func TestMicrowaveTimingsAndTranslationsSurviveStorage(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.Times = &recipe.Times{
		Total: &recipe.DurationExpression{Min: "PT4M", Max: "PT5M", Approximate: true, SourceText: "4 to 5 minutes"},
		Assertions: []recipe.TimingAssertion{{
			ID: "timing-microwave", Kind: recipe.TimingCook, Duration: recipe.DurationExpression{Text: "until steaming"},
			Microwave: &recipe.MicrowaveConditions{
				RatedOutputWatts: 1000, Power: &recipe.MicrowavePower{Percent: "50"}, ModeText: "defrost",
				SourceText: "on 50% power", Restrictions: []string{"do not raise the power"},
				Choices: []recipe.MicrowaveChoice{
					{Duration: recipe.DurationExpression{Value: "PT3M"}, RatedOutputWatts: 1100, SourceText: "3 minutes at 1100 W"},
					{Duration: recipe.DurationExpression{Value: "PT4M"}, Power: &recipe.MicrowavePower{Watts: 800}, SourceText: "4 minutes at 800 W"},
				},
			},
		}},
	}
	document.Translations = []recipe.Translation{
		{Relation: recipe.TranslationOriginal, Language: "fr", Title: "Chaussons", URL: "https://example.com/fr"},
	}

	encoded, version, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(encoded, version)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(decoded.Times, document.Times) || !reflect.DeepEqual(decoded.Translations, document.Translations) {
		t.Fatalf("round trip changed timings or translations\nwritten: %#v %#v\nread: %#v %#v", document.Times, document.Translations, decoded.Times, decoded.Translations)
	}
}
