package v1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/idempotency"
	"github.com/savorum/savorum/internal/library"
)

type (
	retryKeyContext struct{}
	retryAPI        struct {
		huma.API
		authority interface {
			AuthorizeCreation(context.Context, string) error
		}
		receipts *idempotency.Coordinator
		recipes  RecipeService
		cooking  interface {
			GetCooking(context.Context, string, string) (library.CookingRecord, error)
		}
	}
)

// retryCreate resolves committed identities before executing creation again.
func retryCreate[T any](ctx context.Context, api huma.API, operation string, input any, create func(context.Context) (T, error), read func(context.Context, string) (T, error)) (T, error) {
	var zero T

	key, _ := ctx.Value(retryKeyContext{}).(string)
	if key == "" {
		return create(ctx)
	}

	configured, ok := api.(*retryAPI)
	if !ok || configured.receipts == nil {
		return zero, problem(503, "idempotency-unavailable", "Retry receipts are unavailable")
	}

	if configured.authority != nil {
		if err := configured.authority.AuthorizeCreation(ctx, operation); err != nil {
			return zero, err
		}
	}

	data, err := json.Marshal(input)
	if err != nil {
		return zero, fmt.Errorf("encode creation request: %w", err)
	}

	work, id, release, err := configured.receipts.Begin(ctx, creationOperation(operation, input), key, data)
	if err != nil {
		return zero, retryProblem(err)
	}
	defer release()

	if id != "" {
		return read(ctx, id)
	}

	value, err := create(work)

	if replay, ok := errors.AsType[*idempotency.ReplayError](err); ok {
		return read(ctx, replay.ID)
	}

	return value, retryProblem(err)
}

func retryProblem(err error) error {
	switch {
	case errors.Is(err, idempotency.ErrConflict):
		return problem(409, "idempotency-conflict", "Use the same key only for the same request")
	case errors.Is(err, idempotency.ErrInvalid):
		return problem(422, "idempotency-invalid", "Use a retry key of at most 200 bytes")
	case errors.Is(err, idempotency.ErrCapacity):
		return problem(429, "idempotency-capacity", "Too many retained retry keys")
	default:
		return err
	}
}

func describeRetry(api huma.API) {
	operations := map[string]bool{}
	for _, name := range []string{"create-meal", "create-meal-copy", "create-saved-meal", "create-meal-from-saved", "create-recipe", "create-recipe-translation", "create-tag", "create-facet", "create-book", "create-saved-view", "create-recipe-cooking", "create-calendar-feed", "create-shopping-list", "create-webhook", "create-instance-webhook"} {
		operations[name] = true
	}

	for _, path := range api.OpenAPI().Paths {
		op := path.Post
		if op == nil || !operations[op.OperationID] {
			continue
		}

		hasHeader := false

		for _, parameter := range op.Parameters {
			if parameter.In == "header" && parameter.Name == "Idempotency-Key" {
				hasHeader = true
			}
		}

		if !hasHeader {
			op.Parameters = append(op.Parameters, &huma.Param{Name: "Idempotency-Key", In: "header", Description: "Optional retry key. Receipts retain the created identity for at least 24 hours. A replay returns the current authorized resource; one-time secrets are omitted.", Schema: &huma.Schema{Type: "string", MaxLength: func() *int { n := 200; return &n }()}})
		}

		for _, status := range []string{"409", "422", "429", "503"} {
			if op.Responses[status] == nil {
				op.Responses[status] = &huma.Response{Description: "Creation retry refused", Content: map[string]*huma.MediaType{"application/problem+json": {Schema: &huma.Schema{Ref: "#/components/schemas/ErrorModel"}}}}
			}
		}

		op.Description += " Creation accepts Idempotency-Key; reuse it only with the same request. Receipts last at least 24 hours."
	}
}

func creationOperation(operation string, input any) string {
	switch value := input.(type) {
	case *copyMealInput:
		return operation + ":" + value.ID
	case *instantiateSavedMealInput:
		return operation + ":" + value.ID
	case *createTranslationInput:
		return operation + ":" + value.ID
	case *recordCookingInput:
		return operation + ":" + value.ID
	default:
		return operation
	}
}
