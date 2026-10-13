// Package meal models meals as a collection of dishes the household intends to
// prepare together. A meal groups recipe portions and plain-text items with
// independent scales. It may be dated for a specific day or held in an
// unscheduled queue. Saved meals are reusable combinations that instantiate
// fresh occurrences when planned.
//
// Meals are a library-scoped peripheral aggregate. They reference recipe
// library entries but do not modify them. Cooking sessions and shopping lists
// are independent; a meal supplies inputs to both without becoming either.
package meal

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
)

var (
	// ErrNotFound reports that the requested meal does not exist.
	ErrNotFound = errors.New("meal not found")
	// ErrPreconditionFailed reports a failed meal revision precondition.
	ErrPreconditionFailed = errors.New("meal revision precondition failed")
	// ErrInvalidMeal reports invalid meal data.
	ErrInvalidMeal = errors.New("meal data is invalid")
	// ErrUnknownRecipe reports a dish that names a recipe this library does not hold.
	ErrUnknownRecipe = errors.New("a dish names a recipe this library does not hold")
)

// PreconditionError reports the expected and current revisions of a stale write.
type PreconditionError struct {
	MealID   string
	Expected uint64
	Current  uint64
}

func (*PreconditionError) Error() string {
	return "meal revision does not match the required revision"
}

func (*PreconditionError) Unwrap() error {
	return ErrPreconditionFailed
}

// MealType labels familiar meal occasions without requiring selection.
type MealType string

const (
	// MealTypeBreakfast identifies a breakfast occurrence.
	MealTypeBreakfast MealType = "breakfast"
	// MealTypeLunch identifies a lunch occurrence.
	MealTypeLunch MealType = "lunch"
	// MealTypeDinner identifies a dinner occurrence.
	MealTypeDinner MealType = "dinner"
	// MealTypeSnack identifies a snack occurrence.
	MealTypeSnack MealType = "snack"
)

// Meal is a collection of dishes the household intends to serve together. It
// may be dated (planned for a specific day) or unscheduled (an intention
// without a committed day).
type Meal struct {
	ID            string
	LibraryID     string
	Name          string        // optional; display derived from dishes when empty
	Date          *library.Date // nil = unscheduled
	MealType      MealType      // empty = unspecified
	Skipped       bool
	Notes         string
	Dishes        []Dish
	Revision      uint64
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CreatedBy     string
	CreatedByName string
	UpdatedBy     string
	UpdatedByName string
}

// RecipeStatus says whether a recipe dish can still be opened.
type RecipeStatus string

// Recipe statuses.
const (
	RecipeAvailable RecipeStatus = "available"
	RecipeTrashed   RecipeStatus = "trashed"
	RecipeRemoved   RecipeStatus = "removed"
)

// DisplayTitle returns the meal's name or, when unnamed, a comma-separated
// list of its dish display names.
func (m *Meal) DisplayTitle() string {
	if m.Name != "" {
		return m.Name
	}

	switch len(m.Dishes) {
	case 0:
		return ""
	case 1:
		return m.Dishes[0].DisplayName
	default:
		var title strings.Builder
		title.WriteString(m.Dishes[0].DisplayName)

		for _, d := range m.Dishes[1:] {
			title.WriteString(", " + d.DisplayName)
		}

		return title.String()
	}
}

// Dish is one recipe or plain-text item within a meal, with its own scale.
// A recipe dish references a library entry; a plain-text dish has no entry
// reference and may carry manual shopping items.
type Dish struct {
	ID              string
	MealID          string
	EntryID         *library.EntryID // nil for plain-text dishes
	RecipeRevision  *uint64          // pinned when groceries are added
	ShoppingChoices []string         // reviewed alternatives for this occurrence
	LeftoverOf      string           // original meal/dish identity; never expands its recipe
	DisplayName     string           // derived from recipe title or user text
	Scale           *recipe.Rational // per-dish portion; nil = recipe default
	Position        int
	Notes           string
	PlainItems      []PlainItem // shopping items for plain-text dishes
	RecipeStatus    RecipeStatus
	RecipeImage     string // primary asset of an available recipe
}

// IsRecipeDish reports whether this dish references a saved recipe.
func (d *Dish) IsRecipeDish() bool {
	return d.EntryID != nil
}

// PlainItem is a manually entered shopping need for a non-recipe dish.
// "Bagged salad" could carry PlainItem{Name: "1 bag mixed salad"}.
type PlainItem struct {
	ID       string
	Name     string
	Quantity string // human-readable, not parsed
}

// ListOptions controls meal list filtering.
type ListOptions struct {
	// DateFrom and DateTo bound a date range for dated meals (inclusive).
	DateFrom *library.Date
	DateTo   *library.Date
	// Unscheduled includes meals without a date.
	Unscheduled bool
	Limit       int
	Offset      int
}

// Persistence is the repository contract the meal service requires.
type Persistence interface {
	CreateMeal(ctx context.Context, libraryID string, meal Meal) error
	GetMeal(ctx context.Context, libraryID, id string) (Meal, error)
	ListMeals(ctx context.Context, libraryID string, options ListOptions) ([]Meal, int, error)
	UpdateMeal(ctx context.Context, libraryID string, meal Meal, expectedRevision uint64) error
	DeleteMeal(ctx context.Context, libraryID, id string, expectedRevision uint64) error

	AddDish(ctx context.Context, libraryID, mealID string, dish Dish) error
	UpdateDish(ctx context.Context, libraryID, mealID string, dish Dish) error
	RemoveDish(ctx context.Context, libraryID, mealID, dishID string) error
	ReorderDishes(ctx context.Context, libraryID, mealID string, dishIDs []string) error
}
