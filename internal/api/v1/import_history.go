package v1

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/library"
)

// ImportHistoryService provides source-import history to API handlers.
type ImportHistoryService interface {
	ListImportEvents(context.Context, library.EntryID, library.RevisionListOptions) ([]library.ImportEvent, error)
}

// ImportEventResource is the API representation of an import-history event.
type ImportEventResource struct {
	ActorID   string `json:"actor_id,omitempty"`
	ActorName string `json:"actor_name,omitempty"`
	ID        string `json:"id"`
	library.ImportContext
	EntryID          string          `json:"entry_id"`
	RecipeID         string          `json:"recipe_id"`
	Revision         uint64          `json:"revision"`
	Timestamp        time.Time       `json:"timestamp"`
	Operation        string          `json:"operation"`
	BaselineRevision uint64          `json:"baseline_revision"`
	ContentSHA256    string          `json:"content_sha256"`
	SourceSnapshot   *RecipeDocument `json:"source_snapshot,omitempty"`
}
type importHistoryOutput struct {
	Body struct {
		Items  []ImportEventResource `json:"items"`
		Limit  int                   `json:"limit"`
		Offset int                   `json:"offset"`
	}
}

func registerImportHistory(api huma.API, service ImportHistoryService) {
	huma.Register(api, huma.Operation{
		OperationID: "list-recipe-import-events", Method: http.MethodGet,
		Path: "/api/v1/recipes/{id}/import-events", Summary: "List import events", Tags: []string{"Recipes"},
		Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	},
		func(ctx context.Context, input *listRevisionsInput) (*importHistoryOutput, error) {
			events, err := service.ListImportEvents(ctx, library.EntryID(input.ID), library.RevisionListOptions{Limit: input.Limit, Offset: input.Offset})
			if err != nil {
				return nil, mapError(err)
			}

			output := &importHistoryOutput{}

			for _, event := range events {
				item := ImportEventResource{ActorID: event.ActorID, ActorName: event.ActorName, ID: event.ID, ImportContext: event.ImportContext, EntryID: string(event.EntryID), RecipeID: event.RecipeID, Revision: event.Revision, Timestamp: event.Timestamp, Operation: event.Operation, BaselineRevision: event.BaselineRevision, ContentSHA256: event.ContentSHA256}
				if event.SourceSnapshot != nil {
					value := fromDomain(*event.SourceSnapshot)
					item.SourceSnapshot = &value
				}

				output.Body.Items = append(output.Body.Items, item)
			}

			output.Body.Limit, output.Body.Offset = input.Limit, input.Offset

			return output, nil
		})
}
