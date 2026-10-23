package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/savorum/savorum/internal/event"
	"github.com/savorum/savorum/internal/meal"
	"github.com/savorum/savorum/internal/shopping"
)

func (s *Store) annotateMealShopping(ctx context.Context, planned *meal.Meal) error {
	lists, err := readShoppingLists(ctx, s.db, scopeID(ctx))
	if err != nil {
		return err
	}

	applyMealShopping(planned, lists)

	return nil
}

func applyMealShopping(planned *meal.Meal, lists []shopping.ShoppingList) {
	for i := range planned.Dishes {
		dish := &planned.Dishes[i]
		list := latestDishShopping(lists, planned.ID, dish.ID)

		for id, requirement := range list.Requirements {
			source := requirement.Source
			if source.MealID != planned.ID || source.DishID != dish.ID || dish.EntryID == nil || source.RecipeID != string(*dish.EntryID) {
				continue
			}

			selection, ok := list.Selections[id]
			if !ok || len(requirement.Choices) < 2 || selection.ChoiceID == requirement.Choices[0].ID {
				continue
			}

			for _, choice := range requirement.Choices {
				if choice.ID == selection.ChoiceID {
					dish.ShoppingChoices = append(dish.ShoppingChoices, choice.Name+" · "+choice.Quantity)
				}
			}
		}

		sort.Strings(dish.ShoppingChoices)
	}
}

func latestDishShopping(lists []shopping.ShoppingList, mealID, dishID string) shopping.ShoppingList {
	var (
		latest     shopping.ShoppingList
		reviewedAt time.Time
	)

	for _, list := range lists {
		for _, requirement := range list.Requirements {
			if requirement.Source.MealID == mealID && requirement.Source.DishID == dishID && (latest.ID == "" || requirement.ReviewedAt.After(reviewedAt)) {
				latest = list
				reviewedAt = requirement.ReviewedAt
			}
		}
	}

	return latest
}

// pinMealDishes records the recipe revisions a shopping review read on the
// planned dishes it read them for, and advances each changed meal.
func pinMealDishes(ctx context.Context, tx *sql.Tx, libraryID string, pins []shopping.MealPin) error {
	changed := []string{}
	seen := map[string]bool{}

	for _, pin := range pins {
		result, err := tx.ExecContext(ctx, `
			UPDATE meal_dishes SET recipe_revision = ?
			WHERE id = ? AND meal_id = ? AND entry_id = ? AND recipe_revision IS NOT ?
			  AND meal_id IN (SELECT id FROM meals WHERE library_id = ?)`,
			pin.RecipeRevision, pin.DishID, pin.MealID, pin.RecipeID, pin.RecipeRevision, libraryID)
		if err != nil {
			return fmt.Errorf("pin meal dish: %w", err)
		}

		count, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("pin meal dish: %w", err)
		}

		if count > 0 && !seen[pin.MealID] {
			seen[pin.MealID] = true
			changed = append(changed, pin.MealID)
		}
	}

	for _, mealID := range changed {
		var revision uint64
		if err := tx.QueryRowContext(ctx, "UPDATE meals SET revision = revision + 1, updated_at = ?, updated_by = ? WHERE id = ? RETURNING revision",
			formatTime(time.Now()), attributionActor(ctx), mealID).Scan(&revision); err != nil {
			return fmt.Errorf("advance pinned meal: %w", err)
		}

		if err := appendDomainEvent(ctx, tx, "meal_plan.entry.updated", libraryID, "meal", mealID, event.Payload{Revision: revision}); err != nil {
			return err
		}
	}

	return nil
}

func checkReviewedMeals(ctx context.Context, tx *sql.Tx, libraryID string, revisions map[string]uint64) error {
	for id, expected := range revisions {
		var revision uint64

		err := tx.QueryRowContext(ctx, "SELECT revision FROM meals WHERE id=? AND library_id=?", id, libraryID).Scan(&revision)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("read reviewed meal: %w", err)
		}

		if revision != expected {
			return shopping.ErrConflict
		}
	}

	return nil
}
