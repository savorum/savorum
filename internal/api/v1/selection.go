package v1

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/selection"
)

// SelectionService supplies bounded projections without loading resource documents.
type SelectionService interface {
	SuggestRecipes(context.Context, string, int, int) ([]library.Summary, error)
	LookupRecipes(context.Context, []string) ([]library.Summary, error)
	SuggestBooks(context.Context, string, int, int) ([]selection.BookSelectionSummary, error)
	LookupBooks(context.Context, []string) ([]selection.BookSelectionSummary, error)
	SuggestSavedMeals(context.Context, string, int, int) ([]selection.SavedMealSummary, error)
}
type suggestionInput struct {
	Query  string `query:"q" maxLength:"200"`
	Limit  int    `query:"limit" default:"10" minimum:"1"`
	Offset int    `query:"offset" default:"0" minimum:"0"`
}
type (
	suggestionPage[T any] struct {
		Items   []T  `json:"items"`
		HasMore bool `json:"has_more"`
		Limit   int  `json:"limit"`
		Offset  int  `json:"offset"`
	}
	suggestionOutput[T any] struct{ Body suggestionPage[T] }
	lookupInput             struct {
		Body struct {
			IDs []string `json:"ids" minItems:"1"`
		}
	}
	lookupOutput[T any] struct {
		Body struct {
			Items         []T      `json:"items"`
			UnresolvedIDs []string `json:"unresolved_ids"`
		}
	}
)

func registerSuggestions[T any](api huma.API, resource string, maximum int, read func(context.Context, string, int, int) ([]T, error)) {
	huma.Register(api, huma.Operation{OperationID: "list-" + resource + "-suggestions", Method: "GET", Path: "/api/v1/" + resource + "/suggestions", Summary: "Suggest " + strings.ReplaceAll(resource, "-", " "), Tags: []string{selectionTag(resource)}}, func(ctx context.Context, input *suggestionInput) (*suggestionOutput[T], error) {
		if input.Limit > maximum {
			return nil, problem(422, "selection-limit", "The requested page exceeds the advertised limit")
		}

		values, err := read(ctx, input.Query, input.Limit+1, input.Offset)
		if err != nil {
			return nil, mapError(err)
		}

		more := len(values) > input.Limit
		if more {
			values = values[:input.Limit]
		}

		return &suggestionOutput[T]{Body: suggestionPage[T]{Items: values, HasMore: more, Limit: input.Limit, Offset: input.Offset}}, nil
	})
}

func registerLookup[T any](api huma.API, resource string, maximum int, read func(context.Context, []string) ([]T, error), identify func(T) string) {
	huma.Register(api, huma.Operation{OperationID: "get-" + resource + "-summaries", Method: "POST", Path: "/api/v1/" + resource + "/lookup", Summary: "Resolve " + resource + " selections", Tags: []string{selectionTag(resource)}}, func(ctx context.Context, input *lookupInput) (*lookupOutput[T], error) {
		if len(input.Body.IDs) > maximum {
			return nil, problem(422, "selection-limit", "The selection exceeds the advertised lookup limit")
		}

		ids := []string{}
		seen := map[string]bool{}

		for _, id := range input.Body.IDs {
			if id == "" || len(id) > 200 {
				return nil, problem(422, "selection-invalid", "Use valid resource IDs")
			}

			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}

		values, err := read(ctx, ids)
		if err != nil {
			return nil, mapError(err)
		}

		byID := map[string]T{}
		for _, v := range values {
			byID[identify(v)] = v
		}

		out := &lookupOutput[T]{}
		out.Body.Items = []T{}
		out.Body.UnresolvedIDs = []string{}

		for _, id := range ids {
			if v, ok := byID[id]; ok {
				out.Body.Items = append(out.Body.Items, v)
			} else {
				out.Body.UnresolvedIDs = append(out.Body.UnresolvedIDs, id)
			}
		}

		return out, nil
	})
}

func registerSelections(api huma.API, service SelectionService, options Options) {
	recipes := func(ctx context.Context, q string, limit, offset int) ([]RecipeSummary, error) {
		values, err := service.SuggestRecipes(ctx, q, limit, offset)
		return selectionRecipes(values), err
	}
	recipeLookup := func(ctx context.Context, ids []string) ([]RecipeSummary, error) {
		values, err := service.LookupRecipes(ctx, ids)
		return selectionRecipes(values), err
	}

	registerSuggestions(api, "recipes", options.SuggestionLimit, recipes)
	registerSuggestions(api, "books", options.SuggestionLimit, service.SuggestBooks)
	registerSuggestions(api, "saved-meals", options.SuggestionLimit, service.SuggestSavedMeals)
	registerLookup(api, "recipes", options.LookupLimit, recipeLookup, func(v RecipeSummary) string { return v.ID })
	registerLookup(api, "books", options.LookupLimit, service.LookupBooks, func(v selection.BookSelectionSummary) string { return v.ID })
}

func selectionRecipes(values []library.Summary) []RecipeSummary {
	out := make([]RecipeSummary, len(values))
	for i, v := range values {
		out[i] = fromSummary(v)
	}

	return out
}

func selectionTag(resource string) string {
	switch resource {
	case "recipes":
		return "Recipes"
	case "books":
		return "Books"
	default:
		return "Plan"
	}
}
