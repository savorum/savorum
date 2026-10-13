package shopping

import (
	"math/big"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
)

func TestShoppingExactAmountsAndSeparatePurchaseInstructions(t *testing.T) {
	amount := recipe.Rational("3/2")
	ingredient := recipe.Ingredient{ID: "flour", Name: "Flour", Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: &amount}, Unit: "volume.us.cup"}}

	choice, err := ingredientChoice(ingredient, big.NewRat(2, 3))
	if err != nil || choice.Amount != "1" {
		t.Fatalf("scaled amount = %q", choice.Amount)
	}

	list := ShoppingList{Items: []ShoppingItem{{ID: "one", Name: "Onion", Fields: map[string]uint64{}, Sources: []ShoppingSource{{ID: "meal/dish/onion", IngredientID: "onion", Amount: "2"}}}}}
	if !mergeNeed(&list, shoppingNeed{source: ShoppingSource{ID: "second/dish/onion", IngredientID: "onion", Amount: "3"}, choice: ShoppingChoice{Name: "onion"}}) {
		t.Fatal("identical counted onions should group")
	}

	RefreshAmounts(&list)

	if list.Items[0].Needed != "5" || len(list.Items[0].Sources) != 2 {
		t.Fatalf("grouped onions: %#v", list.Items[0])
	}

	if mergeNeed(&list, shoppingNeed{source: ShoppingSource{Amount: "100", Unit: "mass.gram"}, choice: ShoppingChoice{Name: "Onion"}}) {
		t.Fatal("count and mass must not be combined")
	}

	list.Items[0].Buy = "1 bag"
	RefreshAmounts(&list)

	if list.Items[0].Buy != "1 bag" || list.Items[0].Needed != "5" {
		t.Fatal("needed and buy quantities were conflated")
	}
}

func TestShoppingBoughtAmountsSurviveChangesAndArchiving(t *testing.T) {
	source := ShoppingSource{ID: "meal/dish/onion", IngredientID: "onion", Amount: "2", Quantity: "2"}
	list := ShoppingList{Items: []ShoppingItem{{ID: "bought", Bought: true, Deleted: true, Sources: []ShoppingSource{source}}}}
	source.Amount = "3"
	source.Quantity = "3"
	desired := map[string]shoppingNeed{source.ID: {source: source, choice: ShoppingChoice{Name: "Onion"}}}
	subtractBought(&list, desired)

	if desired[source.ID].source.Amount != "1" {
		t.Fatalf("remaining = %#v", desired)
	}

	source.Amount = "1"
	desired[source.ID] = shoppingNeed{source: source}
	subtractBought(&list, desired)

	if len(desired) != 0 || list.Items[0].Sources[0].Amount != "2" {
		t.Fatal("reducing a plan must not erase purchases")
	}
}

func TestShoppingRefreshSplitsChangedIngredientsAndKeepsManualRows(t *testing.T) {
	original := ShoppingSource{ID: "meal/dish/ingredient", MealID: "meal", IngredientID: "ingredient", Amount: "2"}
	list := ShoppingList{Items: []ShoppingItem{{ID: "auto", Name: "Onion", Fields: map[string]uint64{}, Sources: []ShoppingSource{original}}, {ID: "manual", Name: "Bread", Buy: "1 loaf", Fields: map[string]uint64{"name": 1}}}}
	desired := map[string]shoppingNeed{original.ID: {source: original, choice: ShoppingChoice{Name: "Potato"}}}
	refreshOpenItems(&list, ShoppingReviewInput{MealIDs: []string{"meal"}}, desired)

	if !list.Items[0].Deleted || len(desired) != 1 || list.Items[1].Deleted {
		t.Fatalf("refresh = %#v", list)
	}
}
