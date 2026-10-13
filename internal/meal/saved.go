package meal

import (
	"context"
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
)

// SavedMeal is a reusable combination like "Pancake breakfast" that can be
// instantiated as a fresh meal occurrence. A book collects recipes; a saved
// meal specifies a combination to prepare.
type SavedMeal struct {
	ID        string
	LibraryID string
	Name      string
	Notes     string
	Dishes    []SavedDish
	Revision  uint64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SavedDish is a dish template within a saved meal.
type SavedDish struct {
	ID           string
	SavedMealID  string
	EntryID      *library.EntryID
	DisplayName  string
	Scale        *recipe.Rational
	Position     int
	Notes        string
	PlainItems   []PlainItem
	RecipeStatus RecipeStatus
	RecipeImage  string
}

// SavedMealPersistence is the repository contract for saved meals.
type SavedMealPersistence interface {
	CreateSavedMeal(ctx context.Context, libraryID string, meal SavedMeal) error
	GetSavedMeal(ctx context.Context, libraryID, id string) (SavedMeal, error)
	ListSavedMeals(ctx context.Context, libraryID string, limit, offset int) ([]SavedMeal, int, error)
	UpdateSavedMeal(ctx context.Context, libraryID string, meal SavedMeal, expectedRevision uint64) error
	DeleteSavedMeal(ctx context.Context, libraryID, id string, expectedRevision uint64) error
}
