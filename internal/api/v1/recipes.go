package v1

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/library"
)

type createRecipeInput struct {
	Body RecipeDocument
}

type createRecipeOutput struct {
	Location string       `header:"Location"`
	ETag     string       `header:"ETag"`
	Body     LibraryEntry `json:"body"`
}

type getRecipeInput struct {
	ID string `path:"id" doc:"Library entry identifier"`
}

type getRecipeOutput struct {
	ETag string `header:"ETag"`
	Body LibraryEntry
}

type listRecipesInput struct {
	Limit  int    `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Maximum number of entries to return"`
	Offset int    `query:"offset" default:"0" minimum:"0" doc:"Number of entries to skip"`
	Query  string `query:"q" maxLength:"200" doc:"Recipe text with bounded title/author typo matching and structured filters. author: requires full author-name membership with Unicode case, NFC, and whitespace normalization; accents and punctuation are preserved. ingredient: requires literal word-prefix matches within one ingredient name, including optional and alternative names, with original-line fallback only when that name is absent"`
	State  string `query:"state" default:"active" enum:"active,trash" doc:"Library state to list"`
	Sort   string `query:"sort" default:"relevance" enum:"relevance,updated-desc,title-asc,rating-desc,total-time-asc,cooked-desc" doc:"Recipe list ordering"`
	// Huma reads query parameters from direct fields only, so these repeat on
	// each input that groups translations.
	GroupTranslations bool   `query:"group_translations" doc:"Present each set of translated recipes once, as the rendition the preferred languages choose among those that match"`
	Languages         string `query:"languages" maxLength:"1000" doc:"Comma-separated BCP 47 content languages in order of preference, used when grouping translations"`
}

// groupTranslations applies a request's translation grouping to list options.
func groupTranslations(options library.ListOptions, group bool, languages string) library.ListOptions {
	options.GroupTranslations = group
	for value := range strings.SplitSeq(languages, ",") {
		if value = strings.TrimSpace(value); value != "" && len(options.PreferredLanguages) < 20 {
			options.PreferredLanguages = append(options.PreferredLanguages, value)
		}
	}

	return options
}

type listRecipesOutput struct {
	Body struct {
		Items  []RecipeSummary `json:"items"`
		Total  int             `json:"total" minimum:"0"`
		Limit  int             `json:"limit"`
		Offset int             `json:"offset"`
	}
}

func registerRecipes(api huma.API, service RecipeService) {
	huma.Register(api, huma.Operation{
		OperationID:     "create-recipe",
		Method:          http.MethodPost,
		Path:            "/api/v1/recipes",
		Summary:         "Create a recipe",
		Tags:            []string{"Recipes"},
		Errors:          []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
		DefaultStatus:   http.StatusCreated,
		MaxBodyBytes:    16 * 1024 * 1024,
		BodyReadTimeout: 15 * time.Second,
	}, func(ctx context.Context, input *createRecipeInput) (*createRecipeOutput, error) {
		entry, err := retryCreate(ctx, api, "create-recipe", input, func(work context.Context) (library.Entry, error) { return service.Create(work, toDomain(input.Body)) }, func(work context.Context, id string) (library.Entry, error) {
			return service.Get(work, library.EntryID(id))
		})
		if err != nil {
			return nil, mapError(err)
		}

		return &createRecipeOutput{
			Location: "/api/v1/recipes/" + string(entry.ID),
			ETag:     revisionETag(entry.Revision),
			Body:     fromLibrary(entry),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-recipe",
		Method:      http.MethodGet,
		Path:        "/api/v1/recipes/{id}",
		Summary:     "Get a recipe",
		Tags:        []string{"Recipes"},
		Errors:      []int{http.StatusNotFound},
	}, func(ctx context.Context, input *getRecipeInput) (*getRecipeOutput, error) {
		entry, err := service.Get(ctx, library.EntryID(input.ID))
		if err != nil {
			return nil, mapError(err)
		}

		return &getRecipeOutput{ETag: revisionETag(entry.Revision), Body: fromLibrary(entry)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-trashed-recipe",
		Method:      http.MethodGet,
		Path:        "/api/v1/trash/recipes/{id}",
		Summary:     "Get a trashed recipe",
		Tags:        []string{"Recipes"},
		Errors:      []int{http.StatusNotFound},
	}, func(ctx context.Context, input *getRecipeInput) (*getRecipeOutput, error) {
		entry, err := service.GetTrashed(ctx, library.EntryID(input.ID))
		if err != nil {
			return nil, mapError(err)
		}

		return &getRecipeOutput{ETag: revisionETag(entry.Revision), Body: fromLibrary(entry)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-recipes",
		Method:      http.MethodGet,
		Path:        "/api/v1/recipes",
		Summary:     "List recipes",
		Tags:        []string{"Recipes"},
		Errors:      []int{http.StatusUnprocessableEntity},
	}, func(ctx context.Context, input *listRecipesInput) (*listRecipesOutput, error) {
		options := library.ListOptions{
			Limit: input.Limit, Offset: input.Offset, Query: input.Query,
			State: library.State(input.State), Sort: library.ListSort(input.Sort),
		}

		return readRecipePage(ctx, service, groupTranslations(options, input.GroupTranslations, input.Languages))
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-recipe-references",
		Method:      http.MethodGet,
		Path:        "/api/v1/recipe-references",
		Summary:     "List recipe references",
		Tags:        []string{"Recipes"},
		Errors:      []int{http.StatusUnprocessableEntity},
	}, func(ctx context.Context, input *listRecipeReferencesInput) (*listRecipeReferencesOutput, error) {
		references, err := service.ListReferences(ctx, library.State(input.State))
		if err != nil {
			return nil, mapError(err)
		}

		output := &listRecipeReferencesOutput{}

		output.Body.Items = make([]RecipeReference, len(references))
		for index, reference := range references {
			output.Body.Items[index] = RecipeReference{
				ID:       string(reference.ID),
				Title:    reference.Title,
				Revision: reference.Revision,
			}
		}

		return output, nil
	})

	registerRecipeLifecycle(api, service)

	if translations, ok := service.(TranslationService); ok {
		registerTranslations(api, translations)
	}
}

type listRecipeReferencesInput struct {
	State string `query:"state" default:"active" enum:"active,trash" doc:"Library state to list"`
}

type listRecipeReferencesOutput struct {
	Body struct {
		Items []RecipeReference `json:"items"`
	}
}

func readRecipePage(ctx context.Context, service RecipeService, options library.ListOptions) (*listRecipesOutput, error) {
	entries, total, err := service.ListPage(ctx, options)
	if err != nil {
		return nil, mapError(err)
	}

	output := &listRecipesOutput{}

	output.Body.Items = make([]RecipeSummary, len(entries))
	for index, entry := range entries {
		output.Body.Items[index] = fromSummary(entry)
	}

	output.Body.Total = total
	output.Body.Limit = options.Limit
	output.Body.Offset = options.Offset

	return output, nil
}

type reviseRecipeInput struct {
	ID      string `path:"id" doc:"Library entry identifier"`
	IfMatch string `header:"If-Match" doc:"Strong ETag of the revision being changed"`
	Body    RecipeDocument
}

type preconditionedRecipeInput struct {
	ID      string `path:"id" doc:"Library entry identifier"`
	IfMatch string `header:"If-Match" doc:"Strong ETag of the revision being changed"`
}

type revisionInput struct {
	ID       string `path:"id" doc:"Library entry identifier"`
	Revision uint64 `path:"revision" minimum:"1" doc:"Historical revision number"`
}

type restoreRevisionInput struct {
	ID       string `path:"id" doc:"Library entry identifier"`
	Revision uint64 `path:"revision" minimum:"1" doc:"Historical revision number"`
	IfMatch  string `header:"If-Match" doc:"Strong ETag of the current revision"`
}

type listRevisionsInput struct {
	ID     string `path:"id" doc:"Library entry identifier"`
	Limit  int    `query:"limit" default:"50" minimum:"1" maximum:"200"`
	Offset int    `query:"offset" default:"0" minimum:"0"`
}

type revisionOutput struct {
	Body RecipeRevision
}

type listRevisionsOutput struct {
	Body struct {
		Items  []RecipeRevisionSummary `json:"items"`
		Limit  int                     `json:"limit"`
		Offset int                     `json:"offset"`
	}
}

type purgeRecipeOutput struct{}

type emptyTrashTarget struct {
	ID       string `json:"id" minLength:"1"`
	Revision uint64 `json:"revision" minimum:"1"`
}

type emptyTrashInput struct {
	Body struct {
		Recipes []emptyTrashTarget `json:"recipes,omitempty" maxItems:"1000" doc:"Trashed recipes reviewed for permanent removal"`
		Books   []emptyTrashTarget `json:"books,omitempty" maxItems:"1000" doc:"Trashed books reviewed for permanent removal"`
	}
}

func registerRecipeLifecycle(api huma.API, service RecipeService) {
	registerRecipeMutations(api, service)
	registerRecipeHistory(api, service)
	registerTrash(api, service)
}

func registerRecipeMutations(api huma.API, service RecipeService) {
	huma.Register(api, huma.Operation{
		OperationID:  "replace-recipe",
		Method:       http.MethodPut,
		Path:         "/api/v1/recipes/{id}",
		Summary:      "Replace a recipe",
		Tags:         []string{"Recipes"},
		Errors:       []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusPreconditionFailed, http.StatusUnprocessableEntity, http.StatusPreconditionRequired},
		MaxBodyBytes: 16 * 1024 * 1024,
	}, func(ctx context.Context, input *reviseRecipeInput) (*getRecipeOutput, error) {
		expected, err := parseIfMatch(input.IfMatch)
		if err != nil {
			return nil, err
		}

		entry, err := service.Update(ctx, library.EntryID(input.ID), expected, toDomain(input.Body))
		if err != nil {
			return nil, mapError(err)
		}

		return &getRecipeOutput{ETag: revisionETag(entry.Revision), Body: fromLibrary(entry)}, nil
	})

	huma.Register(api, huma.Operation{OperationID: "update-recipe-microwave-analysis", Method: http.MethodPost, Path: "/api/v1/recipes/{id}/reanalyze/microwave", Summary: "Reanalyze microwave timings", Tags: []string{"Recipes"}, Errors: []int{http.StatusNotFound, http.StatusPreconditionFailed, http.StatusPreconditionRequired}}, func(ctx context.Context, input *preconditionedRecipeInput) (*getRecipeOutput, error) {
		expected, err := parseIfMatch(input.IfMatch)
		if err != nil {
			return nil, err
		}

		reanalysis, ok := service.(interface {
			ReanalyzeMicrowave(context.Context, library.EntryID, uint64) (library.Entry, error)
		})
		if !ok {
			return nil, huma.Error501NotImplemented("Microwave reanalysis is unavailable")
		}

		entry, err := reanalysis.ReanalyzeMicrowave(ctx, library.EntryID(input.ID), expected)
		if err != nil {
			return nil, mapError(err)
		}

		return &getRecipeOutput{ETag: revisionETag(entry.Revision), Body: fromLibrary(entry)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "trash-recipe",
		Method:      http.MethodDelete,
		Path:        "/api/v1/recipes/{id}",
		Summary:     "Move a recipe to trash",
		Tags:        []string{"Recipes"},
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusPreconditionFailed, http.StatusPreconditionRequired},
	}, func(ctx context.Context, input *preconditionedRecipeInput) (*getRecipeOutput, error) {
		expected, err := parseIfMatch(input.IfMatch)
		if err != nil {
			return nil, err
		}

		entry, err := service.Trash(ctx, library.EntryID(input.ID), expected)
		if err != nil {
			return nil, mapError(err)
		}

		return &getRecipeOutput{ETag: revisionETag(entry.Revision), Body: fromLibrary(entry)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "restore-recipe",
		Method:      http.MethodPost,
		Path:        "/api/v1/recipes/{id}/restore",
		Summary:     "Restore a recipe from trash",
		Tags:        []string{"Recipes"},
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusPreconditionFailed, http.StatusPreconditionRequired},
	}, func(ctx context.Context, input *preconditionedRecipeInput) (*getRecipeOutput, error) {
		expected, err := parseIfMatch(input.IfMatch)
		if err != nil {
			return nil, err
		}

		entry, err := service.Restore(ctx, library.EntryID(input.ID), expected)
		if err != nil {
			return nil, mapError(err)
		}

		return &getRecipeOutput{ETag: revisionETag(entry.Revision), Body: fromLibrary(entry)}, nil
	})
}

func registerRecipeHistory(api huma.API, service RecipeService) {
	huma.Register(api, huma.Operation{
		OperationID: "list-recipe-revisions",
		Method:      http.MethodGet,
		Path:        "/api/v1/recipes/{id}/revisions",
		Summary:     "List recipe revisions",
		Tags:        []string{"Recipes"},
		Errors:      []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, input *listRevisionsInput) (*listRevisionsOutput, error) {
		revisions, err := service.ListRevisions(ctx, library.EntryID(input.ID), library.RevisionListOptions{Limit: input.Limit, Offset: input.Offset})
		if err != nil {
			return nil, mapError(err)
		}

		output := &listRevisionsOutput{}

		output.Body.Items = make([]RecipeRevisionSummary, len(revisions))
		for index, revision := range revisions {
			output.Body.Items[index] = fromRevisionSummary(revision)
		}

		output.Body.Limit = input.Limit
		output.Body.Offset = input.Offset

		return output, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-recipe-revision",
		Method:      http.MethodGet,
		Path:        "/api/v1/recipes/{id}/revisions/{revision}",
		Summary:     "Get a recipe revision",
		Tags:        []string{"Recipes"},
		Errors:      []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, input *revisionInput) (*revisionOutput, error) {
		revision, err := service.GetRevision(ctx, library.EntryID(input.ID), input.Revision)
		if err != nil {
			return nil, mapError(err)
		}

		return &revisionOutput{Body: fromRevision(revision)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "restore-recipe-revision",
		Method:      http.MethodPost,
		Path:        "/api/v1/recipes/{id}/revisions/{revision}/restore",
		Summary:     "Restore a recipe revision",
		Tags:        []string{"Recipes"},
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusPreconditionFailed, http.StatusPreconditionRequired},
	}, func(ctx context.Context, input *restoreRevisionInput) (*getRecipeOutput, error) {
		expected, err := parseIfMatch(input.IfMatch)
		if err != nil {
			return nil, err
		}

		entry, err := service.RestoreRevision(ctx, library.EntryID(input.ID), input.Revision, expected)
		if err != nil {
			return nil, mapError(err)
		}

		return &getRecipeOutput{ETag: revisionETag(entry.Revision), Body: fromLibrary(entry)}, nil
	})
}

func registerTrash(api huma.API, service RecipeService) {
	huma.Register(api, huma.Operation{
		OperationID:   "delete-trashed-recipe",
		Method:        http.MethodDelete,
		Path:          "/api/v1/trash/recipes/{id}",
		Summary:       "Delete a trashed recipe",
		Description:   "Permanently deletes the recipe, its retained revisions, and data owned solely by it. Referenced assets remain until reference-aware garbage collection proves that no retained object uses them.",
		Tags:          []string{"Recipes"},
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusPreconditionFailed, http.StatusPreconditionRequired},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *preconditionedRecipeInput) (*purgeRecipeOutput, error) {
		expected, err := parseIfMatch(input.IfMatch)
		if err != nil {
			return nil, err
		}

		if err := service.Purge(ctx, library.EntryID(input.ID), expected); err != nil {
			return nil, mapError(err)
		}

		return &purgeRecipeOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "empty-trash",
		Method:        http.MethodPost,
		Path:          "/api/v1/trash/empty",
		Summary:       "Empty the trash",
		Description:   "Removes the named trashed recipes and books in one transaction. Every entry names the revision it was reviewed at, and the whole batch fails if any no longer matches.",
		Tags:          []string{"Recipes"},
		Errors:        []int{http.StatusNotFound, http.StatusConflict, http.StatusPreconditionFailed, http.StatusUnprocessableEntity},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *emptyTrashInput) (*purgeRecipeOutput, error) {
		targets := make([]library.PurgeTarget, len(input.Body.Recipes))
		for index, target := range input.Body.Recipes {
			targets[index] = library.PurgeTarget{ID: library.EntryID(target.ID), Revision: target.Revision}
		}

		books := make([]library.BookPurgeTarget, len(input.Body.Books))
		for index, target := range input.Body.Books {
			books[index] = library.BookPurgeTarget{ID: target.ID, Revision: target.Revision}
		}

		if err := service.EmptyTrash(ctx, targets, books); err != nil {
			return nil, mapTrashError(err)
		}

		return &purgeRecipeOutput{}, nil
	})
}

func parseIfMatch(value string) (uint64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, problem(http.StatusPreconditionRequired, "precondition-required", "If-Match is required for changes to an existing recipe")
	}

	if strings.HasPrefix(value, "W/") || len(value) < 3 || value[0] != '"' || value[len(value)-1] != '"' {
		return 0, problem(http.StatusBadRequest, "invalid-if-match", "If-Match must contain one strong recipe revision ETag")
	}

	revision, err := strconv.ParseUint(value[1:len(value)-1], 10, 64)
	if err != nil || revision == 0 {
		return 0, problem(http.StatusBadRequest, "invalid-if-match", "If-Match must contain one strong recipe revision ETag")
	}

	return revision, nil
}

func revisionETag(revision uint64) string {
	return `"` + strconv.FormatUint(revision, 10) + `"`
}

// mapTrashError reports a mixed batch in the words of whichever object stopped
// it. A book that library data still cites must not be described as a recipe.
func mapTrashError(err error) error {
	if exhausted := storageProblem(err); exhausted != nil {
		return exhausted
	}

	if denied := accessProblem(err); denied != nil {
		return denied
	}

	var book *library.BookPurgeError
	if !errors.As(err, &book) {
		return mapError(err)
	}

	switch {
	case errors.Is(err, library.ErrConflict):
		return problem(http.StatusConflict, "book-conflict", fmt.Sprintf("%q is still recorded as the source of a recipe, so it cannot be deleted permanently yet", book.Name()))
	case errors.Is(err, library.ErrNotFound):
		return problem(http.StatusNotFound, "book-not-found", fmt.Sprintf("Book %s does not exist", book.BookID))
	case errors.Is(err, library.ErrInvalidState):
		return problem(http.StatusConflict, "book-state-conflict", fmt.Sprintf("%q is not in the trash", book.Name()))
	case errors.Is(err, library.ErrPreconditionFailed):
		if precondition, ok := errors.AsType[*library.PreconditionError](err); ok {
			return problem(http.StatusPreconditionFailed, "book-revision-conflict", fmt.Sprintf("%q changed since it was loaded. Expected revision %d, current revision is %d", book.Name(), precondition.Expected, precondition.Current))
		}

		return problem(http.StatusPreconditionFailed, "book-revision-conflict", fmt.Sprintf("%q changed since it was loaded", book.Name()))
	default:
		return problem(http.StatusInternalServerError, "internal-error", "The request could not be completed")
	}
}
