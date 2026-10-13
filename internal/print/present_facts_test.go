package print

import (
	"math/big"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func TestOnlyAWholeRecipeStatementFollowsTheBatch(t *testing.T) {
	t.Parallel()

	value := recipe.Rational("400")
	statement := func(kind recipe.NutritionBasisKind) recipe.NutritionStatement {
		return recipe.NutritionStatement{
			Basis:     recipe.NutritionBasis{Kind: kind},
			Nutrients: []recipe.Nutrient{{Nutrient: recipe.NutrientEnergy, Amount: &recipe.Amount{Value: &value}, Unit: recipe.NutrientUnitKilocalorie}},
		}
	}
	document := recipe.Document{Nutrition: []recipe.NutritionStatement{
		statement(recipe.NutritionBasisRecipe), statement(recipe.NutritionBasisServing), statement(recipe.NutritionBasisUnspecified),
	}}

	printed := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(2, 1)).presentNutrition(document)
	if printed[0].Values[0].Value != "800 kcal" || printed[0].AsWritten {
		t.Fatalf("whole recipe = %#v", printed[0])
	}

	for _, kept := range printed[1:] {
		if kept.Values[0].Value != "400 kcal" || !kept.AsWritten {
			t.Fatalf("a statement that does not follow the batch changed: %#v", kept)
		}
	}
}

func TestConditionalAndScopedClaimsKeepTheirLimits(t *testing.T) {
	t.Parallel()

	document := recipe.Document{
		Variants:      []recipe.Variant{{ID: "oat", Title: "Oat version", Text: "Use oats."}},
		DietaryClaims: []recipe.DietaryClaim{{Text: "Gluten-free", Suitability: recipe.SuitabilitySuitable, Condition: "with certified oats", VariantRef: "oat"}},
	}

	lines := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, nil).presentClaims(document)
	if len(lines) != 1 || lines[0] != "Gluten-free (with certified oats) — Oat version" {
		t.Fatalf("claims = %#v", lines)
	}
}

func TestNutritionPreservesExactSmallValues(t *testing.T) {
	t.Parallel()

	p := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, nil)

	for _, test := range []struct{ amount, want string }{
		{"0", "0 mg"}, {"1/1000", "0.001 mg"}, {"1/3", "1/3 mg"}, {"2469/20", "123.45 mg"},
	} {
		amount := recipe.Rational(test.amount)

		nutrient := recipe.Nutrient{Nutrient: recipe.NutrientSodium, Amount: &recipe.Amount{Value: &amount}, Unit: recipe.NutrientUnit("mg")}
		if got := p.nutrientValue(nutrient, big.NewRat(1, 1)); got != test.want {
			t.Errorf("%s: got %q, want %q", test.amount, got, test.want)
		}
	}
}
