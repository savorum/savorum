package v1

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
)

func TestCompoundAlternativeMapsThroughAPIModel(t *testing.T) {
	value := "40"
	wire := &Quantity{Kind: "alternatives", Options: []AlternativeOption{
		{Kind: "sum", Terms: []SimpleQuantity{
			{Kind: "simple", Amount: Amount{Value: new("2")}, Unit: "volume.tablespoon.us.customary"},
			{Kind: "simple", Amount: Amount{Value: new("2")}, Unit: "volume.teaspoon.us.customary"},
		}},
		{Kind: "simple", Amount: &Amount{Value: &value}, Unit: "volume.milliliter"},
	}}

	domain := toDomainQuantity(wire)
	if len(domain.Options) != 2 || domain.Options[0].Kind != "sum" || len(domain.Options[0].Terms) != 2 {
		t.Fatalf("domain options = %#v", domain.Options)
	}

	roundTrip := fromDomainQuantity(domain)
	if len(roundTrip.Options) != 2 || roundTrip.Options[1].Amount == nil || roundTrip.Options[1].Amount.Value == nil || *roundTrip.Options[1].Amount.Value != "40" {
		t.Fatalf("wire options = %#v", roundTrip.Options)
	}
}

func TestTemperaturePurposeMapsThroughAPIModel(t *testing.T) {
	value := "165"
	wire := &Temperature{
		Amount:  TemperatureAmount{Value: &value},
		Unit:    "temperature.fahrenheit",
		Purpose: &TemperaturePurpose{Kind: "internal", Label: "chicken breast"},
	}
	domain := toDomainTemperature(wire)

	want := &recipe.TemperaturePurpose{Kind: recipe.TemperaturePurposeInternal, Label: "chicken breast"}
	if !reflect.DeepEqual(domain.Purpose, want) {
		t.Fatalf("domain purpose = %#v, want %#v", domain.Purpose, want)
	}

	if roundTrip := fromDomainTemperature(domain); !reflect.DeepEqual(roundTrip.Purpose, wire.Purpose) {
		t.Fatalf("API purpose = %#v, want %#v", roundTrip.Purpose, wire.Purpose)
	}
}

func TestOpaqueTemperatureMapsThroughAPIModel(t *testing.T) {
	wire := &Temperature{Text: "very slow oven", Purpose: &TemperaturePurpose{Kind: "oven"}}

	domain := toDomainTemperature(wire)
	if domain.Text != wire.Text || domain.Unit != "" || domain.Amount.Value != nil {
		t.Fatalf("domain opaque temperature = %#v", domain)
	}

	if roundTrip := fromDomainTemperature(domain); !reflect.DeepEqual(roundTrip, wire) {
		t.Fatalf("API opaque temperature = %#v, want %#v", roundTrip, wire)
	}
}

func TestIngredientChoiceMapsAllBranchesThroughAPI(t *testing.T) {
	wire := []IngredientSection{{ID: "i", Ingredients: []Ingredient{{ID: "fat", Relation: "or", Optional: true, Alternatives: []Ingredient{
		{ID: "butter", Name: "butter", Quantity: &Quantity{Kind: "simple", Amount: &Amount{Value: new("1")}}},
		{ID: "oil", Name: "oil", Note: "neutral", Quantity: &Quantity{Kind: "simple", Amount: &Amount{Value: new("3/4")}}},
	}}}}}
	domain := toDomainIngredientSections(wire)
	roundTrip := fromDomainIngredientSections(domain)

	got, err := json.Marshal(roundTrip)
	if err != nil {
		t.Fatal(err)
	}

	want, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != string(want) {
		t.Fatalf("choice API roundtrip = %s, want %s", got, want)
	}
}

func TestOpaqueTemperatureAPIJSONOmitsEmptyNumericAmount(t *testing.T) {
	encoded, err := json.Marshal(fromDomainTemperature(&recipe.Temperature{Text: "moderate oven"}))
	if err != nil {
		t.Fatal(err)
	}

	if string(encoded) != `{"text":"moderate oven"}` {
		t.Fatalf("opaque API JSON = %s", encoded)
	}

	numeric, err := json.Marshal(fromDomainTemperature(&recipe.Temperature{Amount: recipe.Amount{Value: toDomainRational(new("0"))}, Unit: "temperature.celsius"}))
	if err != nil {
		t.Fatal(err)
	}

	if string(numeric) != `{"amount":{"value":"0"},"unit":"temperature.celsius"}` {
		t.Fatalf("zero temperature API JSON = %s", numeric)
	}
}

func TestUnrelatedRecipeEditPreservesImportedMeasurementInference(t *testing.T) {
	amount := &recipe.Amount{Value: new(recipe.Rational("1/2"))}
	for name, quantity := range map[string]*recipe.Quantity{
		"simple": {Kind: recipe.QuantitySimple, Amount: amount, Unit: "volume.cup.us.customary"},
		"sum": {Kind: recipe.QuantitySum, Terms: []recipe.SimpleQuantity{
			{Kind: recipe.QuantitySimple, Amount: *amount, Unit: "volume.cup.us.customary"},
			{Kind: recipe.QuantitySimple, Amount: *amount, Unit: "volume.tablespoon.us.customary"},
		}},
		"alternatives": {Kind: recipe.QuantityAlternatives, Options: []recipe.AlternativeOption{
			{Kind: recipe.QuantitySimple, Amount: amount, Unit: "volume.cup.us.customary"},
			{Kind: recipe.QuantitySimple, Amount: amount, Unit: "mass.gram"},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			inference := &recipe.Normalization{
				Method: recipe.NormalizationInferred, Review: recipe.ReviewCorrected,
				Warnings: []string{recipe.ImportReviewUnitPrefix + " US measures were selected during import."},
			}
			current := recipe.Document{Title: "Imported recipe", IngredientSections: []recipe.IngredientSection{{
				ID: "ingredients", Ingredients: []recipe.Ingredient{{
					ID: "flour", Name: "flour", SourceText: "1/2 cup flour", Quantity: quantity, Normalization: inference,
				}},
			}}}

			encoded, err := json.Marshal(fromDomain(current))
			if err != nil {
				t.Fatal(err)
			}

			var body RecipeDocument
			if err := json.Unmarshal(encoded, &body); err != nil {
				t.Fatal(err)
			}

			body.Title = "Cleaned title"
			replacement := recipe.ReconcileIngredientDiagnostics(current, toDomain(body))

			got := replacement.IngredientSections[0].Ingredients[0].Normalization
			if !reflect.DeepEqual(got, inference) {
				t.Fatalf("unrelated edit rewrote import inference: got %#v, want %#v", got, inference)
			}
		})
	}
}
