package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/importer/webrecipe"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
)

// SourceRefreshService previews and applies refreshes from retained source data.
type SourceRefreshService interface {
	CheckSourceUpdates(context.Context, library.EntryID, recipe.Document, uint64) (library.SourceRefreshPreview, error)
	ApplySourceRefresh(context.Context, library.SourceRefreshPreview, []library.RefreshResolution) (library.Entry, error)
}

// SourceRefreshURLService previews refreshes by fetching the recipe source URL.
type SourceRefreshURLService interface {
	PreviewSourceRefreshURL(context.Context, library.EntryID, string, uint64) (library.SourceRefreshPreview, error)
}

type sourceRefreshPreviewInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision uint64         `json:"expected_revision" minimum:"1"`
		Remote           RecipeDocument `json:"remote"`
	}
}
type sourceRefreshCommitInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision uint64                      `json:"expected_revision" minimum:"1"`
		Remote           RecipeDocument              `json:"remote"`
		Resolutions      []library.RefreshResolution `json:"resolutions,omitempty"`
	}
}
type sourceRefreshConflictWire struct {
	Path   string `json:"path"`
	Area   string `json:"area"`
	Base   string `json:"base,omitempty"`
	Local  string `json:"local,omitempty"`
	Remote string `json:"remote,omitempty"`
}
type sourceRefreshPreviewWire struct {
	ID               string                      `json:"id"`
	EntryID          string                      `json:"entry_id"`
	ExpectedRevision uint64                      `json:"expected_revision"`
	BaseRevision     uint64                      `json:"base_revision"`
	Base             RecipeDocument              `json:"base"`
	Local            RecipeDocument              `json:"local"`
	Remote           RecipeDocument              `json:"remote"`
	Merged           RecipeDocument              `json:"merged"`
	Conflicts        []sourceRefreshConflictWire `json:"conflicts"`
	Summary          []string                    `json:"summary"`
}
type (
	sourceRefreshPreviewOutput struct{ Body sourceRefreshPreviewWire }
	sourceRefreshCommitOutput  struct {
		ETag string `header:"ETag"`
		Body LibraryEntry
	}
)

type sourceRefreshURLInput struct {
	ID   string `path:"id"`
	Body struct {
		URL              string `json:"url" format:"uri"`
		ExpectedRevision uint64 `json:"expected_revision" minimum:"1"`
	}
}

func registerSourceRefresh(api huma.API, service SourceRefreshService) {
	huma.Register(api, huma.Operation{OperationID: "preview-source-refresh", Method: http.MethodPost, Path: "/api/v1/recipes/{id}/source-refresh/preview", Summary: "Check a recipe source", Tags: []string{"Recipes"}, Errors: []int{404, 409, 412, 422}}, func(ctx context.Context, input *sourceRefreshPreviewInput) (*sourceRefreshPreviewOutput, error) {
		preview, err := service.CheckSourceUpdates(ctx, library.EntryID(input.ID), toDomain(input.Body.Remote), input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapError(err)
		}

		return &sourceRefreshPreviewOutput{Body: sourceRefreshWire(preview)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "commit-source-refresh", Method: http.MethodPost, Path: "/api/v1/recipes/{id}/source-refresh", Summary: "Apply reviewed source updates", Tags: []string{"Recipes"}, Errors: []int{404, 409, 412, 422}}, func(ctx context.Context, input *sourceRefreshCommitInput) (*sourceRefreshCommitOutput, error) {
		preview, err := service.CheckSourceUpdates(ctx, library.EntryID(input.ID), toDomain(input.Body.Remote), input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapError(err)
		}

		entry, err := service.ApplySourceRefresh(ctx, preview, input.Body.Resolutions)
		if err != nil {
			return nil, mapError(err)
		}

		return &sourceRefreshCommitOutput{ETag: revisionETag(entry.Revision), Body: fromLibrary(entry)}, nil
	})
}

func registerSourceRefreshURL(api huma.API, service SourceRefreshURLService) {
	huma.Register(api, huma.Operation{OperationID: "preview-recipe-source-refresh", Method: http.MethodPost, Path: "/api/v1/recipes/{id}/source-refresh/preview-url", Summary: "Check a source URL", Tags: []string{"Recipes"}, Errors: []int{404, 409, 422, 502, 504}}, func(ctx context.Context, input *sourceRefreshURLInput) (*sourceRefreshPreviewOutput, error) {
		preview, err := service.PreviewSourceRefreshURL(ctx, library.EntryID(input.ID), input.Body.URL, input.Body.ExpectedRevision)
		if err != nil {
			if _, fetch := errors.AsType[*webrecipe.Failure](err); fetch {
				return nil, mapImportError(err)
			}

			return nil, mapError(err)
		}

		return &sourceRefreshPreviewOutput{Body: sourceRefreshWire(preview)}, nil
	})
}

func sourceRefreshWire(value library.SourceRefreshPreview) sourceRefreshPreviewWire {
	result := sourceRefreshPreviewWire{ID: value.ID, EntryID: string(value.EntryID), ExpectedRevision: value.ExpectedRevision, BaseRevision: value.BaseRevision, Base: fromDomain(value.Base), Local: fromDomain(value.Local), Remote: fromDomain(value.Remote), Merged: fromDomain(value.Merged), Summary: value.Summary, Conflicts: make([]sourceRefreshConflictWire, 0, len(value.Conflicts))}
	for _, conflict := range value.Conflicts {
		result.Conflicts = append(result.Conflicts, sourceRefreshConflictWire{Path: conflict.Path, Area: conflict.Area, Base: wireJSON(conflict.Base), Local: wireJSON(conflict.Local), Remote: wireJSON(conflict.Remote)})
	}

	return result
}

func wireJSON(value any) string {
	if value == nil {
		return ""
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}

	return string(encoded)
}
