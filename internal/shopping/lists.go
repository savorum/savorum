package shopping

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/savorum/savorum/internal/access"
)

// ShoppingListSummary keeps list switching independent of item downloads.
type ShoppingListSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ShoppingListInput uses a client-generated identity so creation can be retried.
type ShoppingListInput struct {
	ID   string `json:"id" minLength:"1" maxLength:"100" pattern:"^[a-zA-Z0-9_-]+$"`
	Name string `json:"name" minLength:"1" maxLength:"100"`
}

// Lists always includes the household's default Groceries list.
func (s *Service) Lists(ctx context.Context) ([]ShoppingListSummary, error) {
	lists, err := s.store.ListShoppingLists(ctx)
	if err != nil {
		return nil, err
	}

	summaries := make([]ShoppingListSummary, 0, len(lists))
	for _, list := range lists {
		summaries = append(summaries, ShoppingListSummary{ID: list.ID, Name: list.Name})
	}

	return summaries, nil
}

// CreateList leaves the default list and its contents intact.
func (s *Service) CreateList(ctx context.Context, input ShoppingListInput) (ShoppingList, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.ID == "" || input.ID == "groceries" || len(input.ID) > 100 || input.Name == "" || len(input.Name) > 100 {
		return ShoppingList{}, ErrInvalid
	}

	digest := sha256.Sum256([]byte(input.Name))

	return s.store.EditShoppingList(ctx, input.ID, Edit{
		OperationID: "create-list", Digest: hex.EncodeToString(digest[:]), Role: access.Editor, Activity: ActivityCreateList,
		Mutate: func(list *ShoppingList) error {
			if list.Revision != 0 {
				return ErrConflict
			}

			list.Name = input.Name

			return nil
		},
	})
}

// ShoppingListRename is a retryable rename.
type ShoppingListRename struct {
	ID   string `json:"id" minLength:"1" maxLength:"100"`
	Name string `json:"name" minLength:"1" maxLength:"100"`
}

// RenameList changes the selected list's name.
func (s *Service) RenameList(ctx context.Context, input ShoppingListRename) (ShoppingList, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.ID == "" || len(input.ID) > 100 || input.Name == "" || len(input.Name) > 100 {
		return ShoppingList{}, ErrInvalid
	}

	digest := sha256.Sum256([]byte(input.Name))

	return s.store.EditShoppingList(ctx, selectedList(ctx), Edit{
		OperationID: input.ID, Digest: hex.EncodeToString(digest[:]), Role: access.Editor, Activity: ActivityRenameList,
		Mutate: func(list *ShoppingList) error {
			if list.Revision == 0 && selectedList(ctx) != "groceries" {
				return ErrNotFound
			}

			list.Name = input.Name

			return nil
		},
	})
}

// DeleteList removes a named list; Groceries cannot be deleted.
func (s *Service) DeleteList(ctx context.Context) error {
	if selectedList(ctx) == "groceries" {
		return ErrInvalid
	}

	return s.store.DeleteShoppingList(ctx, selectedList(ctx))
}

// Activity lists changes to the selected list, newest first.
func (s *Service) Activity(ctx context.Context, before int64, limit int) ([]ShoppingActivity, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	return s.store.ShoppingActivity(ctx, selectedList(ctx), before, limit)
}

type listContextKey struct{}

// WithList selects a list inside the already-authorized library scope.
func WithList(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, listContextKey{}, id)
}

func selectedList(ctx context.Context) string {
	if id, ok := ctx.Value(listContextKey{}).(string); ok && id != "" {
		return id
	}

	return "groceries"
}
