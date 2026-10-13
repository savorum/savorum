package shopping

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/meal"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

// ShoppingPortion identifies a deliberate recipe occurrence outside the plan.
type ShoppingPortion struct {
	ID       string `json:"id" minLength:"1" maxLength:"100"`
	RecipeID string `json:"recipe_id" minLength:"1" maxLength:"100"`
	Revision uint64 `json:"revision,omitempty"`
	Scale    string `json:"scale,omitempty" maxLength:"100"`
}

// ShoppingReviewInput names sources without modifying the grocery list.
type ShoppingReviewInput struct {
	MealIDs        []string          `json:"meal_ids,omitempty" maxItems:"50"`
	Portions       []ShoppingPortion `json:"portions,omitempty" maxItems:"50"`
	RefreshRecipes bool              `json:"refresh_recipes,omitempty"`
	// Again adds the meals as a further contribution instead of updating
	// the one already on the list.
	Again string `json:"again,omitempty" maxLength:"100" pattern:"^[a-zA-Z0-9_-]*$"`
}

// ShoppingChoice is one authored ingredient alternative, never an inferred substitution.
type ShoppingChoice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Note     string `json:"note"`
	Quantity string `json:"quantity"`
	Amount   string `json:"amount,omitempty"`
	Unit     string `json:"unit,omitempty"`
}

// ShoppingRequirement keeps optional and alternative selections explicit.
type ShoppingRequirement struct {
	ID               string           `json:"id"`
	Source           ShoppingSource   `json:"source"`
	Choices          []ShoppingChoice `json:"choices"`
	Selected         bool             `json:"selected"`
	ChoiceID         string           `json:"choice_id"`
	PreviousName     string           `json:"previous_name,omitempty"`
	PreviousQuantity string           `json:"previous_quantity,omitempty"`
	ReviewedAt       time.Time        `json:"reviewed_at,omitzero" readOnly:"true"`
}

// ShoppingReview binds the confirmation to the exact source and list revisions.
type ShoppingReview struct {
	Revision     uint64                `json:"revision"`
	Token        string                `json:"token"`
	Requirements []ShoppingRequirement `json:"requirements"`
	Removed      []ShoppingSource      `json:"removed"`
	// Outdated reports that a requirement was read from an earlier version of
	// its recipe than the library now holds.
	Outdated bool `json:"outdated,omitempty"`
}

// ShoppingSelection records exclusions as well as additions.
type ShoppingSelection struct {
	ID       string `json:"id"`
	Selected bool   `json:"selected"`
	ChoiceID string `json:"choice_id"`
}

// ShoppingApplyInput submits a reviewed change with a stable retry identity.
type ShoppingApplyInput struct {
	ID         string              `json:"id" minLength:"1" maxLength:"100"`
	Input      ShoppingReviewInput `json:"input"`
	Token      string              `json:"token"`
	Selections []ShoppingSelection `json:"selections" maxItems:"2000"`
}

// Review expands declared ingredients centrally, excluding instruction references.
func (s *Service) Review(ctx context.Context, input ShoppingReviewInput) (ShoppingReview, error) {
	result := ShoppingReview{Requirements: []ShoppingRequirement{}}
	if len(input.MealIDs)+len(input.Portions) == 0 || len(input.MealIDs) > 50 || len(input.Portions) > 50 {
		return result, ErrInvalid
	}

	for _, id := range input.MealIDs {
		planned, err := s.meals.GetMeal(ctx, id)
		if errors.Is(err, meal.ErrNotFound) || errors.Is(err, library.ErrNotFound) {
			continue
		}

		if err != nil {
			return result, err
		}

		if planned.Skipped {
			continue
		}

		if input.RefreshRecipes {
			for i := range planned.Dishes {
				planned.Dishes[i].RecipeRevision = nil
			}
		}

		rows, err := s.expandMeal(ctx, planned, input.Again)
		if err != nil {
			return result, err
		}

		result.Requirements = append(result.Requirements, rows...)
	}

	for _, portion := range input.Portions {
		if input.RefreshRecipes {
			portion.Revision = 0
		}

		rows, err := s.expand(ctx, portion, ShoppingSource{})
		if err != nil {
			return result, err
		}

		result.Requirements = append(result.Requirements, rows...)
	}

	// Applying a review selects each requirement by ID, so a meal or portion
	// named twice would produce a review that can never be applied.
	seen := map[string]bool{}
	for _, requirement := range result.Requirements {
		if seen[requirement.ID] {
			return result, ErrInvalid
		}

		seen[requirement.ID] = true
	}

	if !input.RefreshRecipes {
		outdated, err := s.outdated(ctx, result.Requirements)
		if err != nil {
			return result, err
		}

		result.Outdated = outdated
	}

	list, err := s.Get(ctx)
	if err != nil {
		return result, err
	}

	previousSelections(result.Requirements, list)
	result.Removed = reviewDifferences(result.Requirements, list, input)

	data, err := json.Marshal(struct {
		Input        ShoppingReviewInput   `json:"input"`
		Requirements []ShoppingRequirement `json:"requirements"`
		Revision     uint64                `json:"revision"`
	}{input, result.Requirements, list.Revision})
	if err != nil {
		return result, fmt.Errorf("encode ingredient review: %w", err)
	}

	result.Revision = list.Revision
	digest := sha256.Sum256(data)
	result.Token = hex.EncodeToString(digest[:])

	return result, nil
}

func previousSelections(requirements []ShoppingRequirement, list ShoppingList) {
	for i := range requirements {
		if selection, ok := list.Selections[requirements[i].ID]; ok {
			requirements[i].Selected = selection.Selected
			for _, choice := range requirements[i].Choices {
				if choice.ID == selection.ChoiceID {
					requirements[i].ChoiceID = selection.ChoiceID
				}
			}
		}
	}
}

// outdated reports whether any requirement was read from a recipe version
// older than the one the library holds now.
func (s *Service) outdated(ctx context.Context, requirements []ShoppingRequirement) (bool, error) {
	current := map[string]uint64{}

	for _, requirement := range requirements {
		source := requirement.Source
		if source.RecipeID == "" || source.RecipeRevision == 0 {
			continue
		}

		revision, seen := current[source.RecipeID]
		if !seen {
			entry, err := s.recipes.Get(ctx, library.EntryID(source.RecipeID))
			if errors.Is(err, library.ErrNotFound) {
				continue
			}

			if err != nil {
				return false, err
			}

			revision = entry.Revision
			current[source.RecipeID] = revision
		}

		if revision > source.RecipeRevision {
			return true, nil
		}
	}

	return false, nil
}

func (s *Service) expandMeal(ctx context.Context, planned meal.Meal, again string) ([]ShoppingRequirement, error) {
	rows := []ShoppingRequirement{}
	prefix := planned.ID

	if again != "" {
		prefix = again + "/" + planned.ID
	}

	for _, dish := range planned.Dishes {
		source := ShoppingSource{
			MealID: planned.ID, DishID: dish.ID, Again: again,
			Label: mealLabel(planned, dish), MealDate: mealDate(planned),
		}
		if dish.EntryID == nil {
			for _, item := range dish.PlainItems {
				source.ID = prefix + "/" + dish.ID + "/" + item.ID
				source.Quantity = item.Quantity
				rows = append(rows, ShoppingRequirement{ID: source.ID, Source: source, Choices: []ShoppingChoice{{ID: item.ID, Name: item.Name, Quantity: item.Quantity}}, Selected: true, ChoiceID: item.ID})
			}

			continue
		}

		portion := ShoppingPortion{ID: prefix + "/" + dish.ID, RecipeID: string(*dish.EntryID)}
		if dish.Scale != nil {
			portion.Scale = string(*dish.Scale)
		}

		if dish.RecipeRevision != nil {
			portion.Revision = *dish.RecipeRevision
		}

		expanded, err := s.expand(ctx, portion, source)
		if err != nil {
			return nil, err
		}

		rows = append(rows, expanded...)
	}

	return rows, nil
}

// UnknownRecipeError names the review source whose recipe or revision is gone.
type UnknownRecipeError struct{ SourceID string }

func (e *UnknownRecipeError) Error() string {
	return ErrUnknownRecipe.Error() + ": " + e.SourceID
}

func (*UnknownRecipeError) Unwrap() error { return ErrUnknownRecipe }

func unknownRecipe(err error, portion ShoppingPortion) error {
	if errors.Is(err, library.ErrNotFound) {
		return &UnknownRecipeError{SourceID: portion.ID}
	}

	return err
}

func (s *Service) expand(ctx context.Context, portion ShoppingPortion, source ShoppingSource) ([]ShoppingRequirement, error) {
	if portion.ID == "" || portion.RecipeID == "" {
		return nil, ErrInvalid
	}

	scale := big.NewRat(1, 1)

	if portion.Scale != "" {
		parsed, ok := new(big.Rat).SetString(portion.Scale)
		if !ok || parsed.Sign() <= 0 || parsed.Num().BitLen() > 64 || parsed.Denom().BitLen() > 64 {
			return nil, ErrInvalid
		}

		scale = parsed
	}

	var document recipe.Document

	if portion.Revision == 0 {
		entry, err := s.recipes.Get(ctx, library.EntryID(portion.RecipeID))
		if err != nil {
			return nil, unknownRecipe(err, portion)
		}

		document = entry.Recipe
		portion.Revision = entry.Revision
	} else {
		revision, err := s.recipes.GetRevision(ctx, library.EntryID(portion.RecipeID), portion.Revision)
		if err != nil {
			return nil, unknownRecipe(err, portion)
		}

		document = revision.Recipe
	}

	source.RecipeID = portion.RecipeID

	source.RecipeRevision = portion.Revision
	if source.Label == "" {
		source.Label = document.Title
	}

	rows := []ShoppingRequirement{}

	for _, section := range document.IngredientSections {
		for _, ingredient := range section.Ingredients {
			source.ID = portion.ID + "/" + ingredient.ID
			source.IngredientID = ingredient.ID

			choice, err := ingredientChoice(ingredient, scale)
			if err != nil {
				return nil, err
			}

			choices := []ShoppingChoice{choice}

			for _, alternative := range ingredient.Alternatives {
				choice, err := ingredientChoice(alternative, scale)
				if err != nil {
					return nil, err
				}

				choices = append(choices, choice)
			}

			rows = append(rows, ShoppingRequirement{ID: source.ID, Source: source, Choices: choices, Selected: !ingredient.Optional, ChoiceID: ingredient.ID})
		}
	}

	return rows, nil
}

func ingredientChoice(ingredient recipe.Ingredient, scale *big.Rat) (ShoppingChoice, error) {
	choice := ShoppingChoice{ID: ingredient.ID, Name: ingredient.Name, Note: ingredientNote(ingredient)}
	if choice.Name == "" {
		choice.Name = ingredient.SourceText
	}

	if ingredient.Quantity == nil {
		return choice, nil
	}

	scaled, err := ingredient.Quantity.Scaled(scale)
	if err != nil {
		return choice, fmt.Errorf("scale %s: %w", ingredient.ID, err)
	}

	choice.Quantity = quantityText(scaled)
	if scaled.Kind == recipe.QuantitySimple && scaled.Amount != nil && scaled.Amount.Value != nil && !scaled.Amount.Approximate {
		choice.Amount = string(*scaled.Amount.Value)
		choice.Unit = scaled.Unit
	}

	return choice, nil
}

func ingredientNote(ingredient recipe.Ingredient) string {
	parts := []string{}
	if ingredient.Note != "" {
		parts = append(parts, ingredient.Note)
	}

	if size := ingredient.PackageSize; size != nil {
		parts = append(parts, amountText(size.Amount, size.Unit)+" per package")
	}

	if temperature := ingredient.Temperature; temperature != nil {
		text := temperature.Text
		if text == "" {
			text = amountText(temperature.Amount, temperature.Unit)
		}

		if text != "" {
			parts = append(parts, text)
		}
	}

	return strings.Join(parts, " · ")
}

func quantityText(quantity recipe.Quantity) string {
	switch quantity.Kind {
	case recipe.QuantityOpaque:
		return quantity.Text
	case recipe.QuantitySimple:
		if quantity.Amount == nil {
			return ""
		}

		return amountText(*quantity.Amount, quantity.Unit)
	case recipe.QuantitySum:
		parts := []string{}
		for _, term := range quantity.Terms {
			parts = append(parts, amountText(term.Amount, term.Unit))
		}

		return strings.Join(parts, " plus ")
	case recipe.QuantityAlternatives:
		parts := []string{}
		for _, option := range quantity.Options {
			parts = append(parts, quantityText(recipe.Quantity{Kind: option.Kind, Amount: option.Amount, Unit: option.Unit, Terms: option.Terms}))
		}

		return strings.Join(parts, " or ")
	default:
		return ""
	}
}

func amountText(amount recipe.Amount, unit string) string {
	format := func(value *recipe.Rational) string {
		if value == nil {
			return ""
		}

		return string(*value)
	}

	text := format(amount.Value)
	if amount.Value == nil && (amount.Min != nil || amount.Max != nil) {
		text = format(amount.Min) + "–" + format(amount.Max)
	}

	if amount.Approximate {
		text = "about " + text
	}

	var value *big.Rat
	if amount.Value != nil {
		value, _ = amount.Value.Rat()
	}

	return strings.TrimSpace(text + " " + units.LabelFor(unit, value))
}
