package v1

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/library"
)

// RecipeMergeService performs revision-aware recipe merges for API handlers.
type RecipeMergeService interface {
	CompareRecipes(context.Context, library.EntryID, library.EntryID) (library.RecipeMergePreview, error)
	MergeRecipes(context.Context, library.RecipeMergeRequest) (library.Entry, error)
}
type mergePreviewInput struct {
	Body struct {
		LeftID  string `json:"left_id"`
		RightID string `json:"right_id"`
	}
}
type mergeInput struct {
	Body struct {
		Survivor                  string         `json:"survivor"`
		Duplicate                 string         `json:"duplicate"`
		ExpectedSurvivorRevision  uint64         `json:"expected_survivor_revision" minimum:"1"`
		ExpectedDuplicateRevision uint64         `json:"expected_duplicate_revision" minimum:"1"`
		Recipe                    RecipeDocument `json:"recipe"`
	}
}
type mergeConflictWire struct {
	Path  string `json:"path"`
	Left  string `json:"left,omitempty"`
	Right string `json:"right,omitempty"`
}
type mergePreviewWire struct {
	Left      LibraryEntry        `json:"left"`
	Right     LibraryEntry        `json:"right"`
	Suggested RecipeDocument      `json:"suggested"`
	Conflicts []mergeConflictWire `json:"conflicts"`
}
type (
	mergePreviewOutput struct{ Body mergePreviewWire }
	mergeOutput        struct {
		ETag string `header:"ETag"`
		Body LibraryEntry
	}
)

func registerRecipeMerge(api huma.API, service RecipeMergeService) {
	huma.Register(api, huma.Operation{OperationID: "preview-recipe-merge", Method: http.MethodPost, Path: "/api/v1/recipes/merge/preview", Summary: "Compare duplicate recipes", Tags: []string{"Recipes"}, Errors: []int{404, 409}}, func(ctx context.Context, input *mergePreviewInput) (*mergePreviewOutput, error) {
		result, err := service.CompareRecipes(ctx, library.EntryID(input.Body.LeftID), library.EntryID(input.Body.RightID))
		if err != nil {
			return nil, mapError(err)
		}

		return &mergePreviewOutput{Body: mergePreviewWire{Left: fromLibrary(result.Left), Right: fromLibrary(result.Right), Suggested: fromDomain(result.Suggested), Conflicts: mergeConflictWires(result.Conflicts)}}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "merge-recipes", Method: http.MethodPost, Path: "/api/v1/recipes/merge", Summary: "Merge duplicate recipes", Tags: []string{"Recipes"}, Errors: []int{404, 409, 412, 422}}, func(ctx context.Context, input *mergeInput) (*mergeOutput, error) {
		result, err := service.MergeRecipes(ctx, library.RecipeMergeRequest{Survivor: library.EntryID(input.Body.Survivor), Duplicate: library.EntryID(input.Body.Duplicate), ExpectedSurvivorRevision: input.Body.ExpectedSurvivorRevision, ExpectedDuplicateRevision: input.Body.ExpectedDuplicateRevision, Recipe: toDomain(input.Body.Recipe)})
		if err != nil {
			return nil, mapError(err)
		}

		return &mergeOutput{ETag: revisionETag(result.Revision), Body: fromLibrary(result)}, nil
	})
}

func mergeConflictWires(values []library.MergeConflict) []mergeConflictWire {
	result := make([]mergeConflictWire, 0, len(values))
	for _, value := range values {
		result = append(result, mergeConflictWire{Path: value.Path, Left: wireJSON(value.Left), Right: wireJSON(value.Right)})
	}

	return result
}
