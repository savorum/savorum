package print

import (
	"math/big"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func rational(value string) *recipe.Rational {
	amount := recipe.Rational(value)

	return &amount
}

// SREF requires an amount that cannot be recalculated to be reported as
// unscaled, on paper as on screen.
func TestPrintedBatchReportsTheAmountsItCouldNotRecalculate(t *testing.T) {
	t.Parallel()

	document := recipe.Document{
		Title: "Cookies",
		IngredientSections: []recipe.IngredientSection{{Ingredients: []recipe.Ingredient{
			{
				ID:       "butter",
				Name:     "unsalted butter",
				Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: rational("1")}, Unit: "cup"},
				Note:     "227 grams, 2 sticks",
			},
			{
				ID:       "oats",
				Name:     "rolled oats",
				Quantity: &recipe.Quantity{Kind: recipe.QuantityOpaque, Text: "a good handful"},
			},
			{ID: "source", SourceText: "1 1/2 cups raisins"},
			{ID: "salt", Name: "salt and pepper"},
		}}},
	}

	doubled := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(2, 1), "en").
		present(RecipeSnapshot{Recipe: document}, Content{})

	rows := doubled.IngredientGroups[0].Ingredients
	for index, want := range []string{"wording as written", "amount as written", "amount as written", ""} {
		if rows[index].AsWritten != want {
			t.Errorf("row %d (%q) marked %q, want %q", index, rows[index].Text, rows[index].AsWritten, want)
		}
	}

	if doubled.KeptAsWritten == "" {
		t.Error("a batch with retained amounts printed no report that it had any")
	}
	// Nothing was multiplied at the recipe's own batch, so there is nothing to
	// reconcile and no row is singled out.
	own := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(1, 1), "en").
		present(RecipeSnapshot{Recipe: document}, Content{})
	if own.KeptAsWritten != "" {
		t.Errorf("the recipe's own batch reported %q", own.KeptAsWritten)
	}

	for _, row := range own.IngredientGroups[0].Ingredients {
		if row.AsWritten != "" {
			t.Errorf("%q was marked at the recipe's own batch", row.Text)
		}
	}
}

func TestPrintedBatchMarksOnlyWordingThatStatesAnAmount(t *testing.T) {
	t.Parallel()

	ounce := func(value string) *recipe.Quantity {
		return &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: rational(value)}, Unit: "mass.ounce.avoirdupois"}
	}
	ingredients := []recipe.Ingredient{
		{ID: "ginger", Name: "ginger", Quantity: ounce("2"), Note: "cut into 1-inch pieces"},
		{ID: "tomatoes", Name: "tomatoes", Quantity: ounce("1"), PackageSize: &recipe.SimpleQuantity{Amount: recipe.Amount{Value: rational("14")}, Unit: "mass.ounce.avoirdupois"}},
		{ID: "onion", Name: "onion", Quantity: ounce("4"), Note: "about 1 cup"},
	}
	doubled := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(2, 1), "en").
		present(RecipeSnapshot{Recipe: recipe.Document{Title: "Stew", IngredientSections: []recipe.IngredientSection{{Ingredients: ingredients}}}}, Content{})

	for index, want := range []string{"", "", "wording as written"} {
		if row := doubled.IngredientGroups[0].Ingredients[index]; row.AsWritten != want {
			t.Errorf("%q marked %q, want %q", row.Text, row.AsWritten, want)
		}
	}
}

func TestPrintedOptionalIngredientsSayTheyAreOptional(t *testing.T) {
	t.Parallel()

	ingredients := []recipe.Ingredient{
		{ID: "chives", Name: "chives", Note: "for garnish", Optional: true, SourceText: "Chives, for garnish (optional)"},
		{ID: "nuts", Name: "pine nuts", Optional: true, Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: rational("30")}, Unit: "mass.gram"}},
	}
	printed := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(1, 1), "en").
		present(RecipeSnapshot{Recipe: recipe.Document{Title: "Salad", IngredientSections: []recipe.IngredientSection{{Ingredients: ingredients}}}}, Content{})

	for index, want := range []string{"chives, for garnish (optional)", "pine nuts (optional)"} {
		if got := printed.IngredientGroups[0].Ingredients[index].Text; got != want {
			t.Errorf("row %d = %q, want %q", index, got, want)
		}
	}
}
