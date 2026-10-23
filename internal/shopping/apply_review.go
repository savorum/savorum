package shopping

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/units"
)

type shoppingNeed struct {
	source ShoppingSource
	choice ShoppingChoice
}

// ApplyReview preserves bought allocations and manual fields when refreshing needs.
func (s *Service) ApplyReview(ctx context.Context, input ShoppingApplyInput) (ShoppingList, error) {
	if input.ID == "" || len(input.ID) > 100 {
		return ShoppingList{}, ErrInvalid
	}

	data, err := json.Marshal(input)
	if err != nil {
		return ShoppingList{}, fmt.Errorf("encode shopping confirmation: %w", err)
	}

	digest := sha256.Sum256(data)

	replay := func() (ShoppingList, bool, error) {
		if reader, ok := s.store.(interface {
			ReplayShopping(context.Context, string, string, string) (ShoppingList, bool, error)
		}); ok {
			return reader.ReplayShopping(ctx, selectedList(ctx), input.ID, hex.EncodeToString(digest[:]))
		}

		return ShoppingList{}, false, nil
	}
	if value, found, err := replay(); found || err != nil {
		return value, err
	}

	review, err := s.Review(ctx, input.Input)
	if err != nil {
		if value, found, replayErr := replay(); found || replayErr != nil {
			return value, replayErr
		}

		return ShoppingList{}, err
	}

	return s.store.EditShoppingList(ctx, selectedList(ctx), Edit{OperationID: input.ID, Digest: hex.EncodeToString(digest[:]), Role: access.Editor, Activity: ActivityReview, MealPins: mealPins(review.Requirements), MealRevisions: review.mealRevisions, Mutate: func(list *ShoppingList) error {
		if input.Token != review.Token || list.Revision != review.Revision {
			return ErrConflict
		}

		desired, err := reviewNeeds(list, input, review)
		if err != nil {
			return err
		}

		subtractBought(list, desired)
		refreshOpenItems(list, input.Input, desired)

		// Preserve source order rather than map iteration order in the visible list.
		for _, requirement := range review.Requirements {
			need, ok := desired[requirement.ID]
			if !ok {
				continue
			}

			if mergeNeed(list, need) {
				continue
			}

			digest := sha256.Sum256([]byte(input.ID + "/" + requirement.ID))
			list.Items = append(list.Items, ShoppingItem{ID: hex.EncodeToString(digest[:]), Name: need.choice.Name, Note: need.choice.Note, Revision: list.Revision + 1, Fields: map[string]uint64{}, Sources: []ShoppingSource{need.source}})
		}

		return nil
	}})
}

func mealPins(requirements []ShoppingRequirement) []MealPin {
	pins := []MealPin{}
	seen := map[MealPin]bool{}

	for _, requirement := range requirements {
		source := requirement.Source
		if source.MealID == "" || source.RecipeID == "" || source.RecipeRevision == 0 {
			continue
		}

		pin := MealPin{MealID: source.MealID, DishID: source.DishID, RecipeID: source.RecipeID, RecipeRevision: source.RecipeRevision}
		if !seen[pin] {
			seen[pin] = true
			pins = append(pins, pin)
		}
	}

	return pins
}

func reviewNeeds(list *ShoppingList, input ShoppingApplyInput, review ShoppingReview) (map[string]shoppingNeed, error) {
	selections := map[string]ShoppingSelection{}
	for _, selection := range input.Selections {
		if _, exists := selections[selection.ID]; exists {
			return nil, ErrInvalid
		}

		selections[selection.ID] = selection
	}

	if len(selections) != len(review.Requirements) {
		return nil, ErrInvalid
	}

	if list.Selections == nil {
		list.Selections = map[string]ShoppingSelection{}
	}

	if list.Requirements == nil {
		list.Requirements = map[string]ShoppingRequirement{}
	}

	for id, requirement := range list.Requirements {
		if affectedSource(input.Input, requirement.Source) {
			delete(list.Requirements, id)
			delete(list.Selections, id)
		}
	}

	desired := map[string]shoppingNeed{}
	reviewedAt := time.Now().UTC()

	for _, requirement := range review.Requirements {
		selection, ok := selections[requirement.ID]
		if !ok {
			return nil, ErrInvalid
		}

		var chosen *ShoppingChoice

		for i := range requirement.Choices {
			if requirement.Choices[i].ID == selection.ChoiceID {
				chosen = &requirement.Choices[i]
			}
		}

		if chosen == nil {
			return nil, ErrInvalid
		}

		list.Selections[requirement.ID] = selection
		requirement.ReviewedAt = reviewedAt
		list.Requirements[requirement.ID] = requirement

		if !selection.Selected {
			continue
		}

		source := requirement.Source
		source.IngredientID = chosen.ID
		source.Name = chosen.Name
		source.Note = chosen.Note
		source.Quantity = chosen.Quantity
		source.Amount = chosen.Amount
		source.Unit = chosen.Unit
		desired[requirement.ID] = shoppingNeed{source, *chosen}
	}

	return desired, nil
}

func subtractBought(list *ShoppingList, desired map[string]shoppingNeed) {
	// Bought quantities cover only the same occurrence, ingredient choice and unit.
	for id, need := range desired {
		remaining, exact, covered := remainingNeed(list, need)

		if covered || (exact && remaining.Sign() <= 0) {
			delete(desired, id)
			continue
		}

		if exact {
			need.source.Amount = remaining.RatString()
			need.source.Quantity = strings.TrimSpace(remaining.RatString() + " " + units.LabelFor(need.source.Unit, remaining))
			desired[id] = need
		}
	}
}

func affectedSource(input ShoppingReviewInput, source ShoppingSource) bool {
	if slices.Contains(input.MealIDs, source.MealID) && source.Again == input.Again {
		return true
	}

	for _, portion := range input.Portions {
		if strings.HasPrefix(source.ID, portion.ID+"/") {
			return true
		}
	}

	return false
}

func refreshOpenItems(list *ShoppingList, input ShoppingReviewInput, desired map[string]shoppingNeed) {
	for i := range list.Items {
		item := &list.Items[i]
		if item.Bought || item.Deleted {
			continue
		}

		retained := []ShoppingSource{}
		changed := false

		for _, source := range item.Sources {
			if !affectedSource(input, source) {
				retained = append(retained, source)
				continue
			}

			changed = true

			if need, ok := desired[source.ID]; ok && (len(item.Fields) > 0 || item.Arranged || sameIngredient(*item, need.choice)) {
				retained = append(retained, need.source)
				if item.Fields["name"] == 0 {
					item.Name = need.choice.Name
				}

				if item.Fields["note"] == 0 {
					item.Note = need.choice.Note
				}

				delete(desired, source.ID)
			}
		}

		if !changed {
			continue
		}

		item.Sources = retained

		item.Revision = list.Revision + 1
		if len(retained) == 0 && len(item.Fields) == 0 {
			item.Deleted = true
		}
	}
}

func remainingNeed(list *ShoppingList, need shoppingNeed) (*big.Rat, bool, bool) {
	remaining, exact := new(big.Rat).SetString(need.source.Amount)
	covered := false

	for _, item := range list.Items {
		if !item.Bought {
			continue
		}

		for _, source := range item.Sources {
			if source.ID != need.source.ID || source.IngredientID != need.source.IngredientID {
				continue
			}

			if exact && source.Unit == need.source.Unit {
				if purchased, ok := new(big.Rat).SetString(source.Amount); ok {
					remaining.Sub(remaining, purchased)
				}
			} else if source.Quantity == need.source.Quantity {
				covered = true
			}
		}
	}

	return remaining, exact, covered
}
