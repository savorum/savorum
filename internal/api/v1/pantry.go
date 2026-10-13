package v1

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/pantry"
)

// PantryEntryInput records presence and approximate stock without claiming recipe sufficiency.
type PantryEntryInput struct {
	Name       string `json:"name" minLength:"1" maxLength:"200"`
	SemanticID string `json:"semantic_id,omitempty" maxLength:"200"`
	Amount     string `json:"amount,omitempty" maxLength:"100"`
	Unit       string `json:"unit,omitempty" maxLength:"100"`
	Note       string `json:"note,omitempty" maxLength:"2000"`
	Location   string `json:"location,omitempty" maxLength:"100"`
}

type pantryOutput struct {
	ETag string `header:"ETag"`
	Body pantry.Entry
}

func registerPantry(api huma.API, repository pantry.Repository) {
	huma.Register(api, huma.Operation{OperationID: "list-pantry", Method: http.MethodGet, Path: "/api/v1/pantry", Summary: "Read pantry presence", Description: "Library-shared hints only. Presence and approximate amounts never imply sufficient stock, change shopping selections, or travel in SREF.", Tags: []string{"Pantry"}}, func(ctx context.Context, _ *struct{}) (*struct{ Body []pantry.Entry }, error) {
		entries, err := repository.PantryEntries(ctx)
		if err != nil {
			return nil, pantryError(err)
		}

		return &struct{ Body []pantry.Entry }{entries}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "replace-pantry-entry", Method: http.MethodPut, Path: "/api/v1/pantry/{id}", Summary: "Save pantry presence", Description: "Use a stable client-generated ID. If-None-Match: * creates an entry; If-Match updates an observed revision. At most 2000 entries per library.", Tags: []string{"Pantry"}, Errors: []int{400, 403, 412, 422, 428}}, func(ctx context.Context, input *struct {
		ID          string `path:"id" minLength:"1" maxLength:"100" pattern:"^[a-zA-Z0-9_-]+$"`
		IfMatch     string `header:"If-Match"`
		IfNoneMatch string `header:"If-None-Match"`
		Body        PantryEntryInput
	},
	) (*pantryOutput, error) {
		if input.IfMatch != "" && input.IfNoneMatch != "" {
			return nil, problem(400, "precondition-invalid", "Send either If-Match or If-None-Match, not both")
		}

		var expected uint64

		if input.IfNoneMatch != "*" || input.IfMatch != "" {
			var err error

			expected, err = parseRevisionPrecondition(input.IfMatch, "pantry entry")
			if err != nil {
				return nil, err
			}
		}

		body := input.Body

		entry, err := repository.SavePantryEntry(ctx, pantry.Entry{ID: input.ID, Name: body.Name, SemanticID: body.SemanticID, Amount: body.Amount, Unit: body.Unit, Note: body.Note, Location: body.Location}, expected)
		if errors.Is(err, pantry.ErrInvalid) {
			return nil, problem(422, "pantry-invalid", "Check the name and the size of this pantry entry")
		}

		if err != nil {
			return nil, pantryError(err)
		}

		return &pantryOutput{ETag: revisionETag(entry.Revision), Body: entry}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "delete-pantry-entry", Method: http.MethodDelete, Path: "/api/v1/pantry/{id}", Summary: "Remove a pantry hint", Tags: []string{"Pantry"}, DefaultStatus: 204, Errors: []int{400, 403, 404, 412, 428}}, func(ctx context.Context, input *struct {
		ID      string `path:"id"`
		IfMatch string `header:"If-Match"`
	},
	) (*noContentOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "pantry entry")
		if err != nil {
			return nil, err
		}

		if err := repository.DeletePantryEntry(ctx, input.ID, expected); err != nil {
			return nil, pantryError(err)
		}

		return &noContentOutput{}, nil
	})
}

func pantryError(err error) error {
	if conflict, ok := errors.AsType[*library.PreconditionError](err); ok {
		value := problem(412, "pantry-revision-conflict", "This pantry entry changed. Reload it before saving your changes")
		value.Params = map[string]any{"expected_revision": conflict.Expected, "current_revision": conflict.Current}

		return value
	}

	if errors.Is(err, library.ErrNotFound) {
		return problem(404, "pantry-entry-not-found", "This pantry entry is no longer available")
	}

	return mapError(err)
}
