package v1

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
)

// TranslationService relates library recipes that are renditions of one another.
type TranslationService interface {
	Translations(ctx context.Context, id library.EntryID) (library.TranslationSet, error)
	LinkTranslation(ctx context.Context, id, counterpart library.EntryID, relation recipe.TranslationRelation) ([]library.Entry, error)
	UnlinkTranslation(ctx context.Context, id, counterpart library.EntryID) ([]library.Entry, error)
	DraftTranslation(ctx context.Context, id library.EntryID, request library.TranslationDraft) (recipe.Document, error)
	CreateTranslation(ctx context.Context, id library.EntryID, document recipe.Document) (library.Entry, error)
}

// RecipeTranslations lists the other renditions of a recipe in this library.
type RecipeTranslations struct {
	Renditions []TranslationRendition `json:"renditions" doc:"Other recipes in this library that are renditions of this one"`
	Original   string                 `json:"original,omitempty" doc:"Library entry the renditions declare as their original, when they agree on one"`
	Unresolved []Translation          `json:"unresolved" doc:"This recipe's own declarations that name no recipe in this library"`
}

// TranslationRendition is another library recipe related to the one requested.
type TranslationRendition struct {
	ID       string `json:"id" doc:"Library entry identifier"`
	Title    string `json:"title"`
	Language string `json:"language" doc:"BCP 47 tag of the rendition's primary language"`
	Relation string `json:"relation" enum:"original,translation,alternate" doc:"How the rendition relates to the requested recipe"`
	Linked   bool   `json:"linked" doc:"One of the two recipes names the other, so the relationship can be removed from either"`
}

type translationsOutput struct {
	Body RecipeTranslations
}

type linkTranslationInput struct {
	ID   string `path:"id" doc:"Library entry identifier"`
	Body struct {
		Counterpart string `json:"counterpart" minLength:"1" doc:"Library entry of the other rendition"`
		Relation    string `json:"relation" enum:"original,translation,alternate" doc:"How the counterpart relates to this recipe"`
	}
}

type unlinkTranslationInput struct {
	ID          string `path:"id" doc:"Library entry identifier"`
	Counterpart string `path:"counterpart" doc:"Library entry of the other rendition"`
}

type changedEntriesOutput struct {
	Body struct {
		Items []LibraryEntry `json:"items" doc:"The recipes that received a new revision"`
	}
}

type draftTranslationInput struct {
	ID   string `path:"id" doc:"Library entry identifier"`
	Body struct {
		Language    string `json:"language" minLength:"2" maxLength:"255" doc:"BCP 47 tag of the new rendition's language"`
		Title       string `json:"title,omitempty" maxLength:"1000"`
		CopyContent bool   `json:"copy_content,omitempty" doc:"Start from a copy of this recipe's content, as ordinary editable text in its original language"`
	}
}

type draftTranslationOutput struct {
	Body RecipeDocument
}

type createTranslationInput struct {
	ID   string `path:"id" doc:"Library entry identifier of the recipe being translated"`
	Body RecipeDocument
}

func registerTranslations(api huma.API, service TranslationService) {
	huma.Register(api, huma.Operation{
		OperationID: "list-recipe-translations",
		Method:      http.MethodGet,
		Path:        "/api/v1/recipes/{id}/translations",
		Summary:     "List a recipe's translations",
		Description: "Renditions are separate recipes, each with its own address. Opening one never redirects to another.",
		Tags:        []string{"Recipes"},
		Errors:      []int{http.StatusNotFound},
	}, func(ctx context.Context, input *getRecipeInput) (*translationsOutput, error) {
		set, err := service.Translations(ctx, library.EntryID(input.ID))
		if err != nil {
			return nil, mapError(err)
		}

		output := &translationsOutput{}
		output.Body.Original = string(set.Original)
		output.Body.Renditions = make([]TranslationRendition, len(set.Renditions))

		for index, rendition := range set.Renditions {
			output.Body.Renditions[index] = TranslationRendition{
				ID: string(rendition.ID), Title: rendition.Title, Language: rendition.Language,
				Relation: string(rendition.Relation), Linked: rendition.Linked,
			}
		}

		output.Body.Unresolved = fromDomainTranslations(set.Unresolved)
		if output.Body.Unresolved == nil {
			output.Body.Unresolved = []Translation{}
		}

		return output, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-recipe-translation-link",
		Method:      http.MethodPost,
		Path:        "/api/v1/recipes/{id}/translations",
		Summary:     "Link a recipe as a translation",
		Description: "Both recipes record the relationship in a new revision. Neither recipe's content changes.",
		Tags:        []string{"Recipes"},
		Errors:      []int{http.StatusNotFound, http.StatusPreconditionFailed, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, input *linkTranslationInput) (*changedEntriesOutput, error) {
		entries, err := service.LinkTranslation(ctx, library.EntryID(input.ID), library.EntryID(input.Body.Counterpart), recipe.TranslationRelation(input.Body.Relation))
		if err != nil {
			return nil, mapError(err)
		}

		return changedEntries(entries), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "unlink-recipe-translation",
		Method:      http.MethodDelete,
		Path:        "/api/v1/recipes/{id}/translations/{counterpart}",
		Summary:     "Unlink a translation",
		Description: "Removes the declarations the two recipes make about each other. Both recipes remain.",
		Tags:        []string{"Recipes"},
		Errors:      []int{http.StatusNotFound, http.StatusConflict, http.StatusPreconditionFailed},
	}, func(ctx context.Context, input *unlinkTranslationInput) (*changedEntriesOutput, error) {
		entries, err := service.UnlinkTranslation(ctx, library.EntryID(input.ID), library.EntryID(input.Counterpart))
		if err != nil {
			return nil, mapError(err)
		}

		return changedEntries(entries), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "start-recipe-translation",
		Method:      http.MethodPost,
		Path:        "/api/v1/recipes/{id}/translations/draft",
		Summary:     "Start a translation",
		Description: "Returns an unsaved document for a new rendition. Nothing is stored. Copied content is ordinary text with no binding to this recipe.",
		Tags:        []string{"Recipes"},
		Errors:      []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, input *draftTranslationInput) (*draftTranslationOutput, error) {
		document, err := service.DraftTranslation(ctx, library.EntryID(input.ID), library.TranslationDraft{
			Language: input.Body.Language, Title: input.Body.Title, CopyContent: input.Body.CopyContent,
		})
		if err != nil {
			return nil, mapError(err)
		}

		return &draftTranslationOutput{Body: fromDomain(document)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:     "create-recipe-translation",
		Method:          http.MethodPost,
		Path:            "/api/v1/recipes/{id}/translations/new",
		Summary:         "Save a new translation",
		Description:     "Creates the new recipe and relates it to the recipe it translates.",
		Tags:            []string{"Recipes"},
		Errors:          []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
		DefaultStatus:   http.StatusCreated,
		MaxBodyBytes:    16 * 1024 * 1024,
		BodyReadTimeout: 15 * time.Second,
	}, func(ctx context.Context, input *createTranslationInput) (*createRecipeOutput, error) {
		entry, err := retryCreate(ctx, api, "create-recipe-translation", input, func(work context.Context) (library.Entry, error) {
			return service.CreateTranslation(work, library.EntryID(input.ID), toDomain(input.Body))
		}, func(work context.Context, id string) (library.Entry, error) {
			configured, ok := api.(*retryAPI)
			if !ok {
				return library.Entry{}, library.ErrNotFound
			}

			return configured.recipes.Get(work, library.EntryID(id))
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
}

func changedEntries(entries []library.Entry) *changedEntriesOutput {
	output := &changedEntriesOutput{}

	output.Body.Items = make([]LibraryEntry, len(entries))
	for index, entry := range entries {
		output.Body.Items[index] = fromLibrary(entry)
	}

	return output
}
