package print

import (
	"math/big"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func TestPrintWritesANameThatIsItsCountUnitOnce(t *testing.T) {
	t.Parallel()

	one := recipe.Rational("1")
	presenter := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(1, 1))

	egg := presenter.presentIngredient(recipe.Ingredient{
		ID: "egg", Name: "egg", Note: "large",
		Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: &one}, Unit: "count.egg"},
	})
	if egg.Amount.Text != "1" || egg.Text != "egg, large" {
		t.Fatalf("egg = %q %q", egg.Amount.Text, egg.Text)
	}

	tripled := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(3, 1)).presentIngredient(recipe.Ingredient{
		ID: "egg", Name: "egg",
		Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: &one}, Unit: "count.egg"},
	})
	if tripled.Amount.Text != "3" || tripled.Text != "eggs" {
		t.Fatalf("tripled egg = %q %q", tripled.Amount.Text, tripled.Text)
	}
}

func TestPrintSetsTheSourcesEquivalentApart(t *testing.T) {
	t.Parallel()

	two, grams := recipe.Rational("2"), recipe.Rational("25")
	amount := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(1, 1)).presentQuantity(recipe.Quantity{
		Kind: recipe.QuantityAlternatives,
		Options: []recipe.AlternativeOption{
			{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: &two}, Unit: "volume.tablespoon.us.customary"},
			{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: &grams}, Unit: "mass.gram"},
		},
	})

	first, last := amount.Segments[0], amount.Segments[len(amount.Segments)-1]
	if first.Alternative || !last.Alternative || amount.Text != "2 tbsp or 25g" {
		t.Fatalf("amount = %#v", amount)
	}
}
