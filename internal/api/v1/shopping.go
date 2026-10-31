package v1

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/idempotency"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/shopping"
)

// ShoppingService supplies the library's shared grocery list.
type ShoppingService interface {
	Lists(context.Context) ([]shopping.ShoppingListSummary, error)
	CreateList(context.Context, shopping.ShoppingListInput) (shopping.ShoppingList, error)
	Get(context.Context) (shopping.ShoppingList, error)
	Change(context.Context, shopping.ShoppingChange) (shopping.ShoppingList, error)
	Review(context.Context, shopping.ShoppingReviewInput) (shopping.ShoppingReview, error)
	ApplyReview(context.Context, shopping.ShoppingApplyInput) (shopping.ShoppingList, error)
	RenameList(context.Context, shopping.ShoppingListRename) (shopping.ShoppingList, error)
	DeleteList(context.Context) error
	Activity(ctx context.Context, before int64, limit int) ([]shopping.ShoppingActivity, error)
}

// ShoppingScopeHeaders binds offline work to its original library and principal.
type ShoppingScopeHeaders struct {
	LibraryID   string `header:"X-Savorum-Library" required:"false"`
	PrincipalID string `header:"X-Savorum-Principal" required:"false"`
}

// ShoppingListHeaders selects a list within the bound account and library.
type ShoppingListHeaders struct {
	ShoppingScopeHeaders
	ListID string `path:"list_id" minLength:"1" maxLength:"100" pattern:"^[a-zA-Z0-9_-]+$"`
}

type shoppingListsOutput struct {
	Body []shopping.ShoppingListSummary
}

type createShoppingListInput struct {
	ShoppingScopeHeaders
	IdempotencyKey string `header:"Idempotency-Key" maxLength:"200"`
	Body           shopping.ShoppingListInput
}

type shoppingOutput struct{ Body shopping.ShoppingList }

type createdShoppingListOutput struct {
	Location string `header:"Location"`
	Body     shopping.ShoppingList
}

type shoppingActivityInput struct {
	ShoppingListHeaders
	Before int64 `query:"before" minimum:"0" doc:"Return entries older than this entry identity"`
	Limit  int   `query:"limit" default:"50" minimum:"1" maximum:"100"`
}

type shoppingActivityOutput struct {
	Body struct {
		Items []shopping.ShoppingActivity `json:"items"`
	}
}

func registerShopping(api huma.API, service ShoppingService) {
	huma.Register(api, huma.Operation{OperationID: "list-shopping-lists", Method: http.MethodGet, Path: "/api/v1/shopping/lists", Summary: "List household shopping lists", Tags: []string{"Shopping"}}, func(ctx context.Context, input *ShoppingScopeHeaders) (*shoppingListsOutput, error) {
		if err := input.validate(ctx); err != nil {
			return nil, err
		}

		lists, err := service.Lists(ctx)
		if err != nil {
			return nil, mapShoppingError(err)
		}

		return &shoppingListsOutput{Body: lists}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "create-shopping-list", Method: http.MethodPost, Path: "/api/v1/shopping/lists", Summary: "Create a named shopping list", Description: "The client chooses the list ID, so a retry with the same ID and name returns the same list.", Tags: []string{"Shopping"}, DefaultStatus: http.StatusCreated}, func(ctx context.Context, input *createShoppingListInput) (*createdShoppingListOutput, error) {
		if err := input.validate(ctx); err != nil {
			return nil, err
		}

		list, err := retryCreate(ctx, api, "create-shopping-list", input.Body, func(work context.Context) (shopping.ShoppingList, error) { return service.CreateList(work, input.Body) }, func(work context.Context, id string) (shopping.ShoppingList, error) {
			return service.Get(shopping.WithList(work, id))
		})
		if err != nil {
			return nil, mapShoppingError(err)
		}

		return &createdShoppingListOutput{Location: "/api/v1/shopping/lists/" + url.PathEscape(list.ID), Body: list}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-groceries", Method: http.MethodGet, Path: "/api/v1/shopping/lists/{list_id}", Summary: "Read the shared grocery list", Tags: []string{"Shopping"}}, func(ctx context.Context, input *ShoppingListHeaders) (*shoppingOutput, error) {
		if err := input.validate(ctx); err != nil {
			return nil, err
		}

		list, err := service.Get(shopping.WithList(ctx, input.ListID))
		if err != nil {
			return nil, mapShoppingError(err)
		}

		return &shoppingOutput{Body: list}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "change-groceries", Method: http.MethodPost, Path: "/api/v1/shopping/lists/{list_id}/changes", Summary: "Apply a retryable grocery edit", Tags: []string{"Shopping"}}, func(ctx context.Context, input *struct {
		ShoppingListHeaders
		IdempotencyKey string `header:"Idempotency-Key" maxLength:"100"`
		Body           shopping.ShoppingChange
	},
	) (*shoppingOutput, error) {
		if err := input.validate(ctx); err != nil {
			return nil, err
		}

		work, key, err := shoppingRetryContext(ctx, input.Body.ID, input.IdempotencyKey, "change-groceries")
		if err != nil {
			return nil, err
		}

		input.Body.ID = key

		list, err := service.Change(shopping.WithList(work, input.ListID), input.Body)
		if err != nil {
			return nil, mapShoppingWriteError(err, input.IdempotencyKey)
		}

		return &shoppingOutput{Body: list}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "create-grocery-review", Method: http.MethodPost, Path: "/api/v1/shopping/lists/{list_id}/review", Summary: "Review grocery requirements", Tags: []string{"Shopping"}}, func(ctx context.Context, input *struct {
		ShoppingListHeaders
		Body shopping.ShoppingReviewInput
	},
	) (*struct{ Body shopping.ShoppingReview }, error) {
		if err := input.validate(ctx); err != nil {
			return nil, err
		}

		result, err := service.Review(shopping.WithList(ctx, input.ListID), input.Body)
		if err != nil {
			return nil, mapShoppingError(err)
		}

		return &struct{ Body shopping.ShoppingReview }{Body: result}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "apply-grocery-review", Method: http.MethodPost, Path: "/api/v1/shopping/lists/{list_id}/apply", Summary: "Apply grocery choices", Tags: []string{"Shopping"}}, func(ctx context.Context, input *struct {
		ShoppingListHeaders
		IdempotencyKey string `header:"Idempotency-Key" maxLength:"100"`
		Body           shopping.ShoppingApplyInput
	},
	) (*shoppingOutput, error) {
		if err := input.validate(ctx); err != nil {
			return nil, err
		}

		work, key, err := shoppingRetryContext(ctx, input.Body.ID, input.IdempotencyKey, "apply-grocery-review")
		if err != nil {
			return nil, err
		}

		input.Body.ID = key

		result, err := service.ApplyReview(shopping.WithList(work, input.ListID), input.Body)
		if err != nil {
			return nil, mapShoppingWriteError(err, input.IdempotencyKey)
		}

		return &shoppingOutput{Body: result}, nil
	})
}

func registerGroceryAdditions(api huma.API, service ShoppingService, maximum int) {
	huma.Register(api, huma.Operation{OperationID: "create-grocery-items", Method: "POST", Path: "/api/v1/shopping/lists/{list_id}/items", Summary: "Add groceries atomically", Tags: []string{"Shopping"}, Errors: []int{404, 409, 422}}, func(ctx context.Context, input *struct {
		ShoppingListHeaders
		IdempotencyKey string `header:"Idempotency-Key" required:"true" maxLength:"200"`
		Body           struct {
			Items []shopping.Addition `json:"items" minItems:"1"`
		}
	},
	) (*shoppingOutput, error) {
		if err := input.validate(ctx); err != nil {
			return nil, err
		}

		if len(input.Body.Items) > maximum {
			return nil, problem(422, "shopping-limit", "The addition exceeds the advertised item limit")
		}

		adder, ok := service.(interface {
			AddItems(context.Context, string, []shopping.Addition) (shopping.ShoppingList, error)
		})
		if !ok {
			return nil, problem(503, "shopping-unavailable", "Atomic additions are unavailable")
		}

		list, err := adder.AddItems(shopping.WithList(ctx, input.ListID), input.IdempotencyKey, input.Body.Items)
		if err != nil {
			return nil, mapShoppingError(err)
		}

		return &shoppingOutput{Body: list}, nil
	})
}

func registerShoppingListManagement(api huma.API, service ShoppingService) {
	huma.Register(api, huma.Operation{OperationID: "rename-shopping-list", Method: http.MethodPatch, Path: "/api/v1/shopping/lists/{list_id}", Summary: "Rename a shopping list", Tags: []string{"Shopping"}}, func(ctx context.Context, input *struct {
		ShoppingListHeaders
		IdempotencyKey string `header:"Idempotency-Key" maxLength:"100"`
		Body           shopping.ShoppingListRename
	},
	) (*shoppingOutput, error) {
		if err := input.validate(ctx); err != nil {
			return nil, err
		}

		work, key, err := shoppingRetryContext(ctx, input.Body.ID, input.IdempotencyKey, "rename-shopping-list")
		if err != nil {
			return nil, err
		}

		input.Body.ID = key

		list, err := service.RenameList(shopping.WithList(work, input.ListID), input.Body)
		if err != nil {
			return nil, mapShoppingWriteError(err, input.IdempotencyKey)
		}

		return &shoppingOutput{Body: list}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "delete-shopping-list", Method: http.MethodDelete, Path: "/api/v1/shopping/lists/{list_id}", Summary: "Delete a named shopping list", Description: "Removes the list, its items and its history. The household's Groceries list cannot be deleted.", Tags: []string{"Shopping"}, DefaultStatus: http.StatusNoContent}, func(ctx context.Context, input *ShoppingListHeaders) (*noContentOutput, error) {
		if err := input.validate(ctx); err != nil {
			return nil, err
		}

		if err := service.DeleteList(shopping.WithList(ctx, input.ListID)); err != nil {
			return nil, mapShoppingError(err)
		}

		return &noContentOutput{}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "list-shopping-activity", Method: http.MethodGet, Path: "/api/v1/shopping/lists/{list_id}/activity", Summary: "List shopping list activity", Tags: []string{"Shopping"}}, func(ctx context.Context, input *shoppingActivityInput) (*shoppingActivityOutput, error) {
		if err := input.validate(ctx); err != nil {
			return nil, err
		}

		items, err := service.Activity(shopping.WithList(ctx, input.ListID), input.Before, input.Limit)
		if err != nil {
			return nil, mapShoppingError(err)
		}

		output := &shoppingActivityOutput{}
		output.Body.Items = items

		return output, nil
	})
}

func (headers ShoppingScopeHeaders) validate(ctx context.Context) error {
	if headers.PrincipalID == "" {
		return nil
	}

	principal := "local"
	if value, ok := identity.FromContext(ctx); ok {
		principal = value.UserID
	}

	if headers.PrincipalID != principal {
		return problem(403, "shopping-account-changed", "Sign in to the account that owns these saved changes.")
	}

	return nil
}

func mapShoppingError(err error) error {
	if !errors.Is(err, shopping.ErrConflict) {
		if mapped := retryProblem(err); !errors.Is(mapped, err) {
			return mapped
		}
	}

	if exhausted := storageProblem(err); exhausted != nil {
		return exhausted
	}

	if denied := accessProblem(err); denied != nil {
		return denied
	}

	switch {
	case errors.Is(err, shopping.ErrUnknownRecipe):
		result := problem(404, "recipe-not-found", "A recipe or recipe version in this review is not in the library.")
		if unknown, ok := errors.AsType[*shopping.UnknownRecipeError](err); ok {
			result.Params = map[string]any{"source": unknown.SourceID}
		}

		return result
	case errors.Is(err, shopping.ErrNotFound):
		return problem(404, "shopping-list-not-found", "This shopping list could not be found.")
	case errors.Is(err, shopping.ErrConflict):
		return problem(409, "shopping-conflict", "Someone changed this item. Review their change before replacing it.")
	case errors.Is(err, shopping.ErrDeleted):
		return problem(409, "shopping-removed", "This item was removed. You can add it as a new item.")
	case errors.Is(err, shopping.ErrInvalid):
		return problem(422, "shopping-invalid", "Check the item name and quantities.")
	default:
		return problem(500, "internal-error", "The grocery list could not be updated.")
	}
}

func shoppingRetryContext(ctx context.Context, body, key, operation string) (context.Context, string, error) {
	id, err := shopping.RetryIdentity(body, key)
	if err != nil {
		return ctx, "", retryProblem(err)
	}

	if key != "" {
		ctx = shopping.WithRetryScope(ctx, operation)
	}

	return ctx, id, nil
}

func mapShoppingWriteError(err error, key string) error {
	if key != "" && errors.Is(err, idempotency.ErrConflict) {
		return retryProblem(err)
	}

	return mapShoppingError(err)
}
