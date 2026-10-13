package v1

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/library"
)

// RecipeAuthorService resolves author names without searching recipe text.
type RecipeAuthorService interface {
	FindRecipeAuthors(context.Context, string, int) (library.AuthorCandidates, error)
}

// RecipeAuthorCandidate is an original recipe credit available for filtering.
type RecipeAuthorCandidate struct {
	Name        string `json:"name" doc:"Original author name to quote in an author: qualifier"`
	RecipeCount int    `json:"recipe_count" minimum:"1" doc:"Active recipes with this exact credited spelling in the selected library"`
	Match       string `json:"match" enum:"exact,accent,fuzzy" doc:"Best available tier: exact normalized name, Latin accent variant, or one edit or adjacent transposition"`
}

type listRecipeAuthorsInput struct {
	Query string `query:"q" required:"true" minLength:"1" maxLength:"200" doc:"Complete author name to resolve; only recipe author fields are searched"`
	Limit int    `query:"limit" default:"20" minimum:"1" maximum:"50" doc:"Maximum number of author names to return"`
}

type listRecipeAuthorsOutput struct {
	Body struct {
		Items                 []RecipeAuthorCandidate `json:"items"`
		HasMore               bool                    `json:"has_more" doc:"Matching names in the selected tier exceed the requested limit"`
		CandidateLimitReached bool                    `json:"candidate_limit_reached" doc:"Typo candidate retrieval exceeded 128 names; additional suggestions may exist"`
	}
}

func registerRecipeAuthors(api huma.API, service RecipeAuthorService) {
	huma.Register(api, huma.Operation{
		OperationID: "list-recipe-authors", Method: http.MethodGet, Path: "/api/v1/recipe-authors",
		Summary: "Find recipe authors", Tags: []string{"Recipe authors"},
		Description: "Resolve a complete author name from active recipes in the selected library. Returns only the best available tier: Unicode case-folded NFC and whitespace equality, then Latin accent variants, then one edit or adjacent transposition for 5 to 64 rune names with at least four letters. Punctuation is literal in exact matching. Non-Latin marks are preserved; scripts are not transliterated. Names are ordered by their original Unicode spelling. Exact and accent matches have no candidate cap; typo comparisons evaluate at most 128 indexed candidate names. Choose a returned name before applying the exact author: recipe filter, and ask for a choice when suggestions are ambiguous.",
		Errors:      []int{http.StatusForbidden, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, input *listRecipeAuthorsInput) (*listRecipeAuthorsOutput, error) {
		result, err := service.FindRecipeAuthors(ctx, input.Query, input.Limit)
		if err != nil {
			return nil, mapError(err)
		}

		output := &listRecipeAuthorsOutput{}

		output.Body.Items = make([]RecipeAuthorCandidate, len(result.Items))
		for index, item := range result.Items {
			output.Body.Items[index] = RecipeAuthorCandidate{Name: item.Name, RecipeCount: item.RecipeCount, Match: item.Match}
		}

		output.Body.HasMore, output.Body.CandidateLimitReached = result.HasMore, result.CandidateLimitReached

		return output, nil
	})
}
