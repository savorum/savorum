package importer

import (
	"testing"

	"github.com/savorum/savorum/internal/importer/webrecipe"
	"github.com/savorum/savorum/internal/recipe"
)

func TestSchemaOrgNutritionKeepsItsBasisAndWording(t *testing.T) {
	t.Parallel()

	unspecified := webNutrition(&webrecipe.Nutrition{Values: []webrecipe.NutrientValue{
		{Property: "calories", Text: "420 calories"},
		{Property: "fatContent", Text: "about 12 g"},
		{Property: "sugarContent", Text: "<1 g"},
		{Property: "proteinContent", Text: "8-10g"},
		{Property: "sodiumContent", Text: "300 kcal"},
	}})
	if len(unspecified) != 1 || unspecified[0].Basis.Kind != recipe.NutritionBasisUnspecified {
		t.Fatalf("statement = %#v", unspecified)
	}

	nutrients := unspecified[0].Nutrients
	if nutrients[0].Unit != recipe.NutrientUnitKilocalorie || *nutrients[0].Amount.Value != "420" {
		t.Fatalf("energy = %#v", nutrients[0])
	}

	if !nutrients[1].Amount.Approximate || nutrients[1].SourceText != "about 12 g" {
		t.Fatalf("approximate fat = %#v", nutrients[1])
	}

	if nutrients[2].Text != "<1 g" || nutrients[2].Amount != nil {
		t.Fatalf("opaque sugars = %#v", nutrients[2])
	}

	if *nutrients[3].Amount.Min != "8" || *nutrients[3].Amount.Max != "10" {
		t.Fatalf("ranged protein = %#v", nutrients[3])
	}

	if nutrients[4].Text != "300 kcal" {
		t.Fatalf("an energy unit on sodium was not kept as wording: %#v", nutrients[4])
	}

	serving := webNutrition(&webrecipe.Nutrition{ServingSize: "1 slice", Values: []webrecipe.NutrientValue{{Property: "calories", Text: "210"}}})
	if serving[0].Basis.Kind != recipe.NutritionBasisServing || serving[0].Basis.Text != "1 slice" || serving[0].Nutrients[0].Unit != "" {
		t.Fatalf("serving statement = %#v", serving[0])
	}
}

func TestSuitableForDietBecomesAuthoredClaims(t *testing.T) {
	t.Parallel()

	claims := webDietaryClaims([]string{"VeganDiet", "PaleoDiet"})
	if claims[0].Diet != recipe.DietVegan || claims[0].Text != "Vegan" || claims[0].Suitability != recipe.SuitabilitySuitable {
		t.Fatalf("vegan = %#v", claims[0])
	}

	if claims[1].Diet != "" || claims[1].Text != "PaleoDiet" {
		t.Fatalf("an unknown diet gained a subject: %#v", claims[1])
	}
}
