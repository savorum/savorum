package shopping

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/meal"
)

// Service applies scoped shopping commands through an atomic repository.
type Service struct {
	store   Persistence
	recipes RecipeReader
	meals   MealReader
}

// RecipeReader resolves explicitly pinned ingredient snapshots.
type RecipeReader interface {
	Get(context.Context, library.EntryID) (library.Entry, error)
	GetRevision(context.Context, library.EntryID, uint64) (library.Revision, error)
}

// MealReader preserves occurrence identities across shopping reviews.
type MealReader interface {
	GetMeal(context.Context, string) (meal.Meal, error)
}

// NewService binds shopping operations to their persistence boundary.
func NewService(store Persistence) *Service { return &Service{store: store} }

// WithSources connects ingredient review to the shared recipe and meal services.
func (s *Service) WithSources(recipes RecipeReader, meals MealReader) *Service {
	s.recipes = recipes
	s.meals = meals

	return s
}

// Get returns the selected list in the authorized library.
func (s *Service) Get(ctx context.Context) (ShoppingList, error) {
	list, err := s.store.GetShoppingList(ctx, selectedList(ctx))
	if err != nil {
		return list, err
	}

	if list.Revision == 0 && selectedList(ctx) != "groceries" {
		return ShoppingList{}, ErrNotFound
	}

	if s.meals == nil {
		return list, nil
	}

	s.refreshLabels(ctx, &list)

	return list, nil
}

// Change deduplicates retries without suppressing a deliberately new edit.
func (s *Service) Change(ctx context.Context, change ShoppingChange) (ShoppingList, error) {
	data, err := json.Marshal(change)
	if err != nil {
		return ShoppingList{}, ErrInvalid
	}

	digest := sha256.Sum256(data)

	return s.store.EditShoppingList(ctx, selectedList(ctx), Edit{
		OperationID: change.ID, Digest: hex.EncodeToString(digest[:]), Role: change.RequiredRole(), Activity: change.ActivityKind(),
		Mutate: func(list *ShoppingList) error {
			if list.Revision == 0 && selectedList(ctx) != "groceries" {
				return ErrNotFound
			}

			return Apply(list, change)
		},
	})
}
