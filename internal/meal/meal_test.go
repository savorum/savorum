package meal_test

import (
	"errors"
	"testing"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/meal"
)

func TestMealTitlePreservesNamesAndDishOrder(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value meal.Meal
		want  string
	}{
		{"empty", meal.Meal{}, ""},
		{"one dish", meal.Meal{Dishes: []meal.Dish{{DisplayName: "Soup"}}}, "Soup"},
		{"ordered dishes", meal.Meal{Dishes: []meal.Dish{{DisplayName: "Soup"}, {DisplayName: "Bread"}}}, "Soup, Bread"},
		{"authored name", meal.Meal{Name: "晚餐", Dishes: []meal.Dish{{DisplayName: "Soup"}}}, "晚餐"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.value.DisplayTitle(); got != tc.want {
				t.Fatalf("title = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDishKindDependsOnReferenceAndConflictRetainsRevision(t *testing.T) {
	id := library.EntryID("recipe")

	referenced, plain := meal.Dish{EntryID: &id}, meal.Dish{DisplayName: "Bread"}
	if !referenced.IsRecipeDish() || plain.IsRecipeDish() {
		t.Fatal("dish kind disagrees with recipe reference")
	}

	err := &meal.PreconditionError{MealID: "meal", Expected: 2, Current: 3}
	if !errors.Is(err, meal.ErrPreconditionFailed) {
		t.Fatal("revision conflict cannot be classified")
	}
}
