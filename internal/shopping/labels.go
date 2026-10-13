package shopping

import (
	"context"

	"github.com/savorum/savorum/internal/meal"
)

func (s *Service) refreshLabels(ctx context.Context, list *ShoppingList) {
	meals := map[string]meal.Meal{}

	for i := range list.Items {
		for j := range list.Items[i].Sources {
			source := &list.Items[i].Sources[j]
			if source.MealID == "" {
				continue
			}

			planned, ok := meals[source.MealID]
			if !ok {
				value, err := s.meals.GetMeal(ctx, source.MealID)
				if err != nil {
					meals[source.MealID] = meal.Meal{}
					continue
				}

				planned = value
				meals[source.MealID] = value
			}

			for _, dish := range planned.Dishes {
				if dish.ID == source.DishID {
					source.Label = mealLabel(planned, dish)
					source.MealDate = mealDate(planned)

					break
				}
			}
		}
	}
}

func mealLabel(planned meal.Meal, dish meal.Dish) string {
	if planned.Name != "" && planned.Name != dish.DisplayName {
		return planned.Name + " · " + dish.DisplayName
	}

	return dish.DisplayName
}

// mealDate is left to clients to write in the reader's language.
func mealDate(planned meal.Meal) string {
	if planned.Date == nil {
		return ""
	}

	return planned.Date.String()
}
