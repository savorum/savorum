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
