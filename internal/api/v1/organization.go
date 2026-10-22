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
	"github.com/savorum/savorum/internal/recipe"
)

// OrganizationService provides organization operations to API handlers.
type OrganizationService interface {
	GetOrganization(context.Context, string) (library.Organization, error)
	ReplaceOrganization(context.Context, string, uint64, library.Organization, []library.BookMembershipChange) (library.Organization, error)
	CreateTag(context.Context, library.Tag) (library.Tag, error)
	GetTag(context.Context, string) (library.Tag, error)
	ListTags(context.Context) ([]library.Tag, error)
	ReplaceTag(context.Context, string, uint64, library.Tag) (library.Tag, error)
	DeleteTag(context.Context, string, uint64) error
	MergeTag(context.Context, string, uint64, string) error
	CreateFacet(context.Context, library.Facet) (library.Facet, error)
	GetFacet(context.Context, string) (library.Facet, error)
	ListFacets(context.Context, library.FacetKind) ([]library.Facet, error)
	ReplaceFacet(context.Context, string, uint64, library.Facet) (library.Facet, error)
	DeleteFacet(context.Context, string, uint64) error
	MergeFacet(context.Context, string, uint64, string) error
	CreateBook(context.Context, library.Book) (library.Book, error)
	GetBook(context.Context, string) (library.Book, error)
	GetTrashedBook(context.Context, string) (library.Book, error)
	ListBooks(context.Context, library.BookListOptions) ([]library.BookListItem, int, error)
	ListBookSourceRecipes(context.Context, string) ([]library.BookSourceRecipe, error)
	ReplaceBook(context.Context, string, uint64, library.Book) (library.Book, error)
	DeleteBook(context.Context, string, uint64) error
	RestoreBook(context.Context, string, uint64) (library.Book, error)
	GetBookRevision(context.Context, string, uint64) (library.BookRevision, error)
	ListBookRevisions(context.Context, string, library.RevisionListOptions) ([]library.BookRevisionSummary, error)
	RestoreBookRevision(context.Context, string, uint64, uint64) (library.Book, error)
	PurgeBook(context.Context, string, uint64) error
	CreateSavedView(context.Context, library.SavedView) (library.SavedView, error)
	GetSavedView(context.Context, string) (library.SavedView, error)
	ListSavedViews(context.Context) ([]library.SavedView, error)
	ReplaceSavedView(context.Context, string, uint64, library.SavedView) (library.SavedView, error)
	DeleteSavedView(context.Context, string, uint64) error
	ListCookings(context.Context, string, library.CookingListOptions) (library.CookingHistory, error)
	RecordCooking(context.Context, string, library.CookingRecord) (library.CookingRecord, error)
	ReplaceCooking(context.Context, string, string, library.CookingRecord) (library.CookingRecord, error)
	DeleteCooking(context.Context, string, string) error
}

// Facet is the API representation of an organization facet.
type Facet struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind" enum:"course,cuisine,method,dietary"`
	Value     string    `json:"value"`
	Revision  uint64    `json:"revision" minimum:"1"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Tag is the API representation of a recipe tag.
type Tag struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color,omitempty"`
	Revision  uint64    `json:"revision" minimum:"1"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// FacetInput supplies a facet to create or replace.
type FacetInput struct {
	Kind  string `json:"kind" enum:"course,cuisine,method,dietary"`
	Value string `json:"value" minLength:"1" maxLength:"100"`
}

// TagInput supplies a tag to create or replace.
type TagInput struct {
	Name  string `json:"name" minLength:"1" maxLength:"100"`
	Color string `json:"color,omitempty" pattern:"^#[A-Fa-f0-9]{6}$"`
}

// UserState is what one person decided about a recipe. Cooking counts come from
// the cookings resource.
type UserState struct {
	Favorite     bool   `json:"favorite"`
	Rating       *uint8 `json:"rating,omitempty" minimum:"1" maximum:"5"`
	PersonalNote string `json:"personal_note,omitempty" maxLength:"1000000"`
	// PreferredYield is the amount of the recipe's own yield this reader
	// normally makes. Savorum divides it by the recipe's yield to open at the
	// right batch, so correcting the recipe still produces the wanted amount.
	PreferredYield *string `json:"preferred_yield,omitempty" pattern:"^([1-9][0-9]*|[1-9][0-9]*/[1-9][0-9]*)$" maxLength:"128" doc:"Preferred yield as an exact amount of the recipe's own yield"`
	// PreferredScale is the fallback for a recipe whose yield is only text.
	PreferredScale          *string `json:"preferred_scale,omitempty" pattern:"^([1-9][0-9]*|[1-9][0-9]*/[1-9][0-9]*)$" maxLength:"128" doc:"Preferred batch as an exact factor, for a yield that is not a number"`
	PreferredYieldAssertion *int    `json:"preferred_yield_assertion,omitempty" minimum:"0" maximum:"99" doc:"Position in the recipe's yields of the yield this reader shows first and scales by. It does not reorder the recipe's yields."`
}

// CookingRecord is one remembered occasion on which a recipe was cooked.
type CookingRecord struct {
	CookedTime string    `json:"cooked_time,omitempty"`
	ID         string    `json:"id"`
	SessionID  *string   `json:"session_id,omitempty"`
	CookedOn   string    `json:"cooked_on" format:"date"`
	Note       string    `json:"note,omitempty" maxLength:"10000"`
	Scale      *string   `json:"scale,omitempty" pattern:"^([1-9][0-9]*|[1-9][0-9]*/[1-9][0-9]*)$" maxLength:"128" doc:"Batch actually made, as a factor of the recipe as written"`
	CreatedAt  time.Time `json:"created_at"`
}

// CookingInput supplies a recorded cooking event.
type CookingInput struct {
	CookedTime string  `json:"cooked_time,omitempty" pattern:"^([01][0-9]|2[0-3]):[0-5][0-9]$" doc:"Optional local clock time; omitted for day-only history"`
	CookedOn   string  `json:"cooked_on,omitempty" format:"date" doc:"Defaults to today"`
	Note       string  `json:"note,omitempty" maxLength:"10000"`
	Scale      *string `json:"scale,omitempty" pattern:"^([1-9][0-9]*|[1-9][0-9]*/[1-9][0-9]*)$" maxLength:"128"`
	SessionID  *string `json:"session_id,omitempty" pattern:"^[A-Za-z0-9._:-]{1,128}$" maxLength:"128" doc:"Cooking session key used to make retries idempotent"`
}

// CookingHistory reports actual records plus any provenance-bearing imported
// baseline. An imported aggregate never authors a last_cooked date.
type CookingHistory struct {
	LibraryEntryID    string                              `json:"library_entry_id"`
	TimesCooked       uint64                              `json:"times_cooked" minimum:"0"`
	ImportedBaselines []library.ImportedCookCountBaseline `json:"imported_baselines,omitempty"`
	LastCooked        *string                             `json:"last_cooked,omitempty" format:"date"`
	Items             []CookingRecord                     `json:"items"`
}

// RecipeSource describes the source of a recipe in an organization view.
type RecipeSource struct {
	ID                string     `json:"id,omitempty"`
	Kind              string     `json:"kind" enum:"url,publication,other"`
	URL               string     `json:"url,omitempty"`
	PublicationBookID string     `json:"publication_book_id,omitempty"`
	PageStart         *uint64    `json:"page_start,omitempty"`
	PageEnd           *uint64    `json:"page_end,omitempty"`
	OriginalTitle     string     `json:"original_title,omitempty"`
	Attribution       string     `json:"attribution,omitempty"`
	AccessedAt        *time.Time `json:"accessed_at,omitempty"`
}

// RecipeOrganization describes a recipe's organization metadata.
type RecipeOrganization struct {
	LibraryEntryID string         `json:"library_entry_id" doc:"Identifier of the organized entry in the local Savorum library"`
	Revision       uint64         `json:"revision"`
	FacetIDs       []string       `json:"facet_ids"`
	TagIDs         []string       `json:"tag_ids"`
	UserState      UserState      `json:"user_state"`
	Sources        []RecipeSource `json:"sources"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// BookMembershipInput places this recipe in a book, or takes it out, as part
// of the same write. Each change names the book revision it expects so a stale
// membership edit is reported rather than applied.
type BookMembershipInput struct {
	BookID   string `json:"book_id" minLength:"1"`
	Revision uint64 `json:"revision" minimum:"1" doc:"Book revision this change expects"`
	Member   bool   `json:"member" doc:"True to place the recipe in the book, false to take it out"`
}

// OrganizationInput supplies organization metadata for a recipe.
type OrganizationInput struct {
	FacetIDs       []string              `json:"facet_ids" maxItems:"100"`
	TagIDs         []string              `json:"tag_ids" maxItems:"100"`
	UserState      UserState             `json:"user_state"`
	Sources        []RecipeSource        `json:"sources" maxItems:"100"`
	BookMembership []BookMembershipInput `json:"book_membership,omitempty" maxItems:"100" doc:"Book membership changes applied in the same transaction"`
}

// BookSection is one ordered section of a book.
type BookSection struct {
	ID       string `json:"id,omitempty"`
	Title    string `json:"title"`
	Position int    `json:"position"`
}

// BookEntry is a recipe entry in a book.
type BookEntry struct {
	ID             string `json:"id,omitempty"`
	SectionID      string `json:"section_id,omitempty"`
	LibraryEntryID string `json:"library_entry_id" doc:"Identifier of the member entry in the local Savorum library"`
	Position       int    `json:"position"`
	Note           string `json:"note,omitempty"`
	RecipeState    string `json:"recipe_state" enum:"active,trash" readOnly:"true" doc:"Derived recipe state. Omit this field from book write entries."`
}

// BookEntryInput supplies a recipe entry for a book.
type BookEntryInput struct {
	ID             string `json:"id,omitempty"`
	SectionID      string `json:"section_id,omitempty"`
	LibraryEntryID string `json:"library_entry_id" doc:"Identifier of the member entry in the local Savorum library"`
	Position       int    `json:"position"`
	Note           string `json:"note,omitempty"`
}

// BookSourceRecipe identifies the source recipe for a book entry.
type BookSourceRecipe struct {
	LibraryEntryID string  `json:"library_entry_id" doc:"Identifier of the sourced entry in the local Savorum library"`
	Title          string  `json:"title"`
	PageStart      *uint64 `json:"page_start,omitempty"`
	PageEnd        *uint64 `json:"page_end,omitempty"`
	OriginalTitle  string  `json:"original_title,omitempty"`
}

// Book is the API representation of a recipe book.
type Book struct {
	ID              string        `json:"id"`
	Kind            string        `json:"kind" enum:"personal,publication"`
	Title           string        `json:"title"`
	Subtitle        string        `json:"subtitle,omitempty"`
	Description     string        `json:"description,omitempty"`
	CoverAssetID    string        `json:"cover_asset_id,omitempty"`
	Author          string        `json:"author,omitempty"`
	Editor          string        `json:"editor,omitempty"`
	Edition         string        `json:"edition,omitempty"`
	ISBN            string        `json:"isbn,omitempty"`
	Publisher       string        `json:"publisher,omitempty"`
	PublicationDate string        `json:"publication_date,omitempty"`
	Sections        []BookSection `json:"sections"`
	Entries         []BookEntry   `json:"entries"`
	Revision        uint64        `json:"revision"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	DeletedAt       *time.Time    `json:"deleted_at,omitempty"`
}

// BookRevision represents a historical recipe-book revision.
type BookRevision struct {
	ActorID        string    `json:"actor_id,omitempty"`
	ActorName      string    `json:"actor_name,omitempty"`
	Book           Book      `json:"book"`
	State          string    `json:"state" enum:"active,trash"`
	Cause          string    `json:"cause" enum:"api,manual_edit,restore,trash,recipe_lifecycle"`
	SourceRevision *uint64   `json:"source_revision,omitempty" minimum:"1"`
	CreatedAt      time.Time `json:"created_at"`
}

// BookRevisionSummary provides a compact book revision representation.
type BookRevisionSummary struct {
	ActorID        string    `json:"actor_id,omitempty"`
	ActorName      string    `json:"actor_name,omitempty"`
	BookID         string    `json:"book_id"`
	Revision       uint64    `json:"revision" minimum:"1"`
	State          string    `json:"state" enum:"active,trash"`
	Cause          string    `json:"cause" enum:"api,manual_edit,restore,trash,recipe_lifecycle"`
	SourceRevision *uint64   `json:"source_revision,omitempty" minimum:"1"`
	CreatedAt      time.Time `json:"created_at"`
}

// BookSummary provides a compact recipe-book representation.
type BookSummary struct {
	ID            string     `json:"id"`
	Kind          string     `json:"kind" enum:"personal,publication"`
	Title         string     `json:"title"`
	CoverAssetID  string     `json:"cover_asset_id,omitempty"`
	Author        string     `json:"author,omitempty"`
	EntryCount    int        `json:"entry_count" minimum:"0"`
	ContainsEntry bool       `json:"contains_entry,omitempty"`
	Revision      uint64     `json:"revision" minimum:"1"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
}

// BookInput supplies a recipe book to create or replace.
type BookInput struct {
	Kind            string           `json:"kind" enum:"personal,publication"`
	Title           string           `json:"title" minLength:"1" maxLength:"1000"`
	Subtitle        string           `json:"subtitle,omitempty"`
	Description     string           `json:"description,omitempty"`
	CoverAssetID    string           `json:"cover_asset_id,omitempty"`
	Author          string           `json:"author,omitempty"`
	Editor          string           `json:"editor,omitempty"`
	Edition         string           `json:"edition,omitempty"`
	ISBN            string           `json:"isbn,omitempty"`
	Publisher       string           `json:"publisher,omitempty"`
	PublicationDate string           `json:"publication_date,omitempty"`
	Sections        []BookSection    `json:"sections" maxItems:"100"`
	Entries         []BookEntryInput `json:"entries" maxItems:"1000"`
}

// SavedView is the API representation of a saved recipe view.
type SavedView struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Query     string    `json:"query"`
	Sort      string    `json:"sort,omitempty" enum:"relevance,updated-desc,title-asc,rating-desc"`
	Revision  uint64    `json:"revision"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SavedViewInput supplies a saved recipe view to create or replace.
type SavedViewInput struct {
	Title string `json:"title" minLength:"1" maxLength:"1000"`
	Query string `json:"query" maxLength:"200"`
	Sort  string `json:"sort,omitempty" maxLength:"100" enum:"relevance,updated-desc,title-asc,rating-desc"`
}

type organizationPathInput struct {
	ID string `path:"id"`
}
type replaceOrganizationInput struct {
	ID      string `path:"id"`
	IfMatch string `header:"If-Match"`
	Body    OrganizationInput
}
type organizationOutput struct {
	ETag string `header:"ETag"`
	Body RecipeOrganization
}
type taxonomyPathInput struct {
	ID string `path:"id"`
}
type preconditionedTaxonomyInput struct {
	ID      string `path:"id"`
	IfMatch string `header:"If-Match"`
}
type (
	createTagInput  struct{ Body TagInput }
	replaceTagInput struct {
		ID      string `path:"id"`
		IfMatch string `header:"If-Match"`
		Body    TagInput
	}
)

type (
	createFacetInput  struct{ Body FacetInput }
	replaceFacetInput struct {
		ID      string `path:"id"`
		IfMatch string `header:"If-Match"`
		Body    FacetInput
	}
)

type listFacetsInput struct {
	Kind string `query:"kind" enum:"course,cuisine,method,dietary"`
}
type mergeTaxonomyInput struct {
	ID      string `path:"id"`
	IfMatch string `header:"If-Match"`
	Body    struct {
		TargetID string `json:"target_id"`
	}
}
type taxonomyOutput[T any] struct {
	Location string `header:"Location"`
	ETag     string `header:"ETag"`
	Body     T
}
type tagsOutput struct {
	Body struct {
		Items []Tag `json:"items"`
	}
}
type facetsOutput struct {
	Body struct {
		Items []Facet `json:"items"`
	}
}
type (
	createBookInput struct{ Body BookInput }
	bookPathInput   struct {
		ID string `path:"id"`
	}
)

type replaceBookInput struct {
	ID      string `path:"id"`
	IfMatch string `header:"If-Match"`
	Body    BookInput
}
type preconditionedBookInput struct {
	ID      string `path:"id"`
	IfMatch string `header:"If-Match"`
}
type bookRevisionPathInput struct {
	ID       string `path:"id"`
	Revision uint64 `path:"revision" minimum:"1"`
}
type restoreBookRevisionInput struct {
	ID       string `path:"id"`
	Revision uint64 `path:"revision" minimum:"1"`
	IfMatch  string `header:"If-Match"`
}
type listBookRevisionsInput struct {
	ID     string `path:"id"`
	Limit  int    `query:"limit" default:"50" minimum:"1" maximum:"200"`
	Offset int    `query:"offset" default:"0" minimum:"0"`
}
type bookOutput struct {
	Location string `header:"Location"`
	ETag     string `header:"ETag"`
	Body     Book
}
type (
	bookRevisionOutput  struct{ Body BookRevision }
	bookRevisionsOutput struct {
		Body struct {
			Items []BookRevisionSummary `json:"items"`
		}
	}
)

type booksOutput struct {
	Body struct {
		Items  []BookSummary `json:"items"`
		Total  int           `json:"total" minimum:"0"`
		Limit  int           `json:"limit"`
		Offset int           `json:"offset"`
	}
}
type listBooksInput struct {
	Limit   int    `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Maximum number of books to return"`
	Offset  int    `query:"offset" default:"0" minimum:"0" doc:"Number of books to skip"`
	EntryID string `query:"entry_id" doc:"Report whether each book contains this library recipe entry"`
	State   string `query:"state" default:"active" enum:"active,trash" doc:"Book lifecycle state to list"`
}
type bookSourceRecipesOutput struct {
	Body struct {
		Items []BookSourceRecipe `json:"items"`
	}
}
type (
	createSavedViewInput struct{ Body SavedViewInput }
	savedViewPathInput   struct {
		ID string `path:"id"`
	}
)

type savedViewRecipesInput struct {
	ID                string `path:"id"`
	Limit             int    `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Maximum number of entries to return"`
	Offset            int    `query:"offset" default:"0" minimum:"0" doc:"Number of entries to skip"`
	GroupTranslations bool   `query:"group_translations" doc:"Present each set of translated recipes once, as the rendition the preferred languages choose among those that match"`
	Languages         string `query:"languages" maxLength:"1000" doc:"Comma-separated BCP 47 content languages in order of preference, used when grouping translations"`
}
type replaceSavedViewInput struct {
	ID      string `path:"id"`
	IfMatch string `header:"If-Match"`
	Body    SavedViewInput
}
type preconditionedSavedViewInput struct {
	ID      string `path:"id"`
	IfMatch string `header:"If-Match"`
}
type savedViewOutput struct {
	Location string `header:"Location"`
	ETag     string `header:"ETag"`
	Body     SavedView
}
type savedViewsOutput struct {
	Body struct {
		Items []SavedView `json:"items"`
	}
}
type listCookingsInput struct {
	ID     string `path:"id"`
	Limit  int    `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Maximum number of cookings to return"`
	Offset int    `query:"offset" default:"0" minimum:"0" doc:"Number of cookings to skip"`
}
type recordCookingInput struct {
	ID   string `path:"id"`
	Body CookingInput
}
type cookingPathInput struct {
	ID        string `path:"id"`
	CookingID string `path:"cooking_id"`
}
type replaceCookingInput struct {
	ID        string `path:"id"`
	CookingID string `path:"cooking_id"`
	Body      CookingInput
}
type cookingOutput struct {
	Location string `header:"Location"`
	Body     CookingRecord
}
type cookingHistoryOutput struct{ Body CookingHistory }

type noContentOutput struct{}

func registerTaxonomy(api huma.API, service OrganizationService) {
	registerTags(api, service)
	registerFacets(api, service)
}

func registerTags(api huma.API, service OrganizationService) {
	huma.Register(api, huma.Operation{OperationID: "create-tag", Method: http.MethodPost, Path: "/api/v1/tags", Summary: "Create a tag", Tags: []string{"Tags"}, DefaultStatus: 201, Errors: []int{409, 422}}, func(ctx context.Context, input *createTagInput) (*taxonomyOutput[Tag], error) {
		value, err := retryCreate(ctx, api, "create-tag", input, func(work context.Context) (library.Tag, error) { return service.CreateTag(work, toTag(input.Body)) }, service.GetTag)
		if err != nil {
			return nil, mapOrganizationError(err, "tag")
		}

		return &taxonomyOutput[Tag]{Location: "/api/v1/tags/" + value.ID, ETag: revisionETag(value.Revision), Body: fromTag(value)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "list-tags", Method: http.MethodGet, Path: "/api/v1/tags", Summary: "List tags", Tags: []string{"Tags"}}, func(ctx context.Context, _ *struct{}) (*tagsOutput, error) {
		values, err := service.ListTags(ctx)
		if err != nil {
			return nil, mapOrganizationError(err, "tag")
		}

		output := &tagsOutput{}

		output.Body.Items = make([]Tag, len(values))
		for index, value := range values {
			output.Body.Items[index] = fromTag(value)
		}

		return output, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-tag", Method: http.MethodGet, Path: "/api/v1/tags/{id}", Summary: "Get a tag", Tags: []string{"Tags"}, Errors: []int{404}}, func(ctx context.Context, input *taxonomyPathInput) (*taxonomyOutput[Tag], error) {
		value, err := service.GetTag(ctx, input.ID)
		if err != nil {
			return nil, mapOrganizationError(err, "tag")
		}

		return &taxonomyOutput[Tag]{ETag: revisionETag(value.Revision), Body: fromTag(value)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "replace-tag", Method: http.MethodPut, Path: "/api/v1/tags/{id}", Summary: "Replace a tag", Tags: []string{"Tags"}, Errors: []int{400, 404, 409, 412, 422, 428}}, func(ctx context.Context, input *replaceTagInput) (*taxonomyOutput[Tag], error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "tag")
		if err != nil {
			return nil, err
		}

		value, err := service.ReplaceTag(ctx, input.ID, expected, toTag(input.Body))
		if err != nil {
			return nil, mapOrganizationError(err, "tag")
		}

		return &taxonomyOutput[Tag]{ETag: revisionETag(value.Revision), Body: fromTag(value)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "delete-tag", Method: http.MethodDelete, Path: "/api/v1/tags/{id}", Summary: "Delete a tag", Tags: []string{"Tags"}, DefaultStatus: 204, Errors: []int{400, 404, 409, 412, 428}}, func(ctx context.Context, input *preconditionedTaxonomyInput) (*noContentOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "tag")
		if err != nil {
			return nil, err
		}

		if err := service.DeleteTag(ctx, input.ID, expected); err != nil {
			return nil, mapOrganizationError(err, "tag")
		}

		return &noContentOutput{}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "merge-tag", Method: http.MethodPost, Path: "/api/v1/tags/{id}/merge", Summary: "Merge a tag", Tags: []string{"Tags"}, DefaultStatus: 204, Errors: []int{400, 404, 409, 412, 422, 428}}, func(ctx context.Context, input *mergeTaxonomyInput) (*noContentOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "tag")
		if err != nil {
			return nil, err
		}

		if err := service.MergeTag(ctx, input.ID, expected, input.Body.TargetID); err != nil {
			return nil, mapOrganizationError(err, "tag")
		}

		return &noContentOutput{}, nil
	})
}

func registerFacets(api huma.API, service OrganizationService) {
	huma.Register(api, huma.Operation{OperationID: "create-facet", Method: http.MethodPost, Path: "/api/v1/facets", Summary: "Create a facet", Tags: []string{"Facets"}, DefaultStatus: 201, Errors: []int{409, 422}}, func(ctx context.Context, input *createFacetInput) (*taxonomyOutput[Facet], error) {
		value, err := retryCreate(ctx, api, "create-facet", input, func(work context.Context) (library.Facet, error) {
			return service.CreateFacet(work, toFacet(input.Body))
		}, service.GetFacet)
		if err != nil {
			return nil, mapOrganizationError(err, "facet")
		}

		return &taxonomyOutput[Facet]{Location: "/api/v1/facets/" + value.ID, ETag: revisionETag(value.Revision), Body: fromFacet(value)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "list-facets", Method: http.MethodGet, Path: "/api/v1/facets", Summary: "List facets", Tags: []string{"Facets"}, Errors: []int{422}}, func(ctx context.Context, input *listFacetsInput) (*facetsOutput, error) {
		values, err := service.ListFacets(ctx, library.FacetKind(input.Kind))
		if err != nil {
			return nil, mapOrganizationError(err, "facet")
		}

		output := &facetsOutput{}

		output.Body.Items = make([]Facet, len(values))
		for index, value := range values {
			output.Body.Items[index] = fromFacet(value)
		}

		return output, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-facet", Method: http.MethodGet, Path: "/api/v1/facets/{id}", Summary: "Get a facet", Tags: []string{"Facets"}, Errors: []int{404}}, func(ctx context.Context, input *taxonomyPathInput) (*taxonomyOutput[Facet], error) {
		value, err := service.GetFacet(ctx, input.ID)
		if err != nil {
			return nil, mapOrganizationError(err, "facet")
		}

		return &taxonomyOutput[Facet]{ETag: revisionETag(value.Revision), Body: fromFacet(value)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "replace-facet", Method: http.MethodPut, Path: "/api/v1/facets/{id}", Summary: "Replace a facet", Tags: []string{"Facets"}, Errors: []int{400, 404, 409, 412, 422, 428}}, func(ctx context.Context, input *replaceFacetInput) (*taxonomyOutput[Facet], error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "facet")
		if err != nil {
			return nil, err
		}

		value, err := service.ReplaceFacet(ctx, input.ID, expected, toFacet(input.Body))
		if err != nil {
			return nil, mapOrganizationError(err, "facet")
		}

		return &taxonomyOutput[Facet]{ETag: revisionETag(value.Revision), Body: fromFacet(value)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "delete-facet", Method: http.MethodDelete, Path: "/api/v1/facets/{id}", Summary: "Delete a facet", Tags: []string{"Facets"}, DefaultStatus: 204, Errors: []int{400, 404, 409, 412, 428}}, func(ctx context.Context, input *preconditionedTaxonomyInput) (*noContentOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "facet")
		if err != nil {
			return nil, err
		}

		if err := service.DeleteFacet(ctx, input.ID, expected); err != nil {
			return nil, mapOrganizationError(err, "facet")
		}

		return &noContentOutput{}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "merge-facet", Method: http.MethodPost, Path: "/api/v1/facets/{id}/merge", Summary: "Merge a facet", Tags: []string{"Facets"}, DefaultStatus: 204, Errors: []int{400, 404, 409, 412, 422, 428}}, func(ctx context.Context, input *mergeTaxonomyInput) (*noContentOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "facet")
		if err != nil {
			return nil, err
		}

		if err := service.MergeFacet(ctx, input.ID, expected, input.Body.TargetID); err != nil {
			return nil, mapOrganizationError(err, "facet")
		}

		return &noContentOutput{}, nil
	})
}

func registerOrganization(api huma.API, service OrganizationService, recipes RecipeService) {
	registerTaxonomy(api, service)
	registerCookings(api, service)
	registerRecipeOrganization(api, service)
	registerBooks(api, service)
	registerSavedViews(api, service, recipes)
}

func registerRecipeOrganization(api huma.API, service OrganizationService) {
	huma.Register(api, huma.Operation{OperationID: "get-recipe-organization", Method: http.MethodGet, Path: "/api/v1/recipes/{id}/organization", Summary: "Get recipe organization", Tags: []string{"Organization"}, Errors: []int{404, 409}}, func(ctx context.Context, input *organizationPathInput) (*organizationOutput, error) {
		value, err := service.GetOrganization(ctx, input.ID)
		if err != nil {
			return nil, mapOrganizationError(err, "organization")
		}

		return &organizationOutput{ETag: revisionETag(value.Revision), Body: fromOrganization(value)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "replace-recipe-organization", Method: http.MethodPut, Path: "/api/v1/recipes/{id}/organization", Summary: "Replace recipe organization", Description: "Replaces tags, categories, personal state, and provenance, and applies any named book membership changes in the same transaction.", Tags: []string{"Organization"}, Errors: []int{400, 404, 409, 412, 422, 428}}, func(ctx context.Context, input *replaceOrganizationInput) (*organizationOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "organization")
		if err != nil {
			return nil, err
		}

		value, err := service.ReplaceOrganization(ctx, input.ID, expected, toOrganization(input.ID, input.Body), toBookMembership(input.Body.BookMembership))
		if err != nil {
			return nil, mapOrganizationError(err, "organization")
		}

		return &organizationOutput{ETag: revisionETag(value.Revision), Body: fromOrganization(value)}, nil
	})
}

func registerBooks(api huma.API, service OrganizationService) {
	registerBookCollection(api, service)
	registerBookLifecycle(api, service)
	registerBookHistory(api, service)
}

func registerBookCollection(api huma.API, service OrganizationService) {
	huma.Register(api, huma.Operation{OperationID: "create-book", Method: http.MethodPost, Path: "/api/v1/books", Summary: "Create a book", Tags: []string{"Books"}, DefaultStatus: 201, Errors: []int{404, 409, 422}}, func(ctx context.Context, input *createBookInput) (*bookOutput, error) {
		value, err := retryCreate(ctx, api, "create-book", input, func(work context.Context) (library.Book, error) { return service.CreateBook(work, toBook(input.Body)) }, service.GetBook)
		if err != nil {
			return nil, mapOrganizationError(err, "book")
		}

		return &bookOutput{Location: "/api/v1/books/" + value.ID, ETag: revisionETag(value.Revision), Body: fromBook(value)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "list-books", Method: http.MethodGet, Path: "/api/v1/books", Summary: "List books", Tags: []string{"Books"}}, func(ctx context.Context, input *listBooksInput) (*booksOutput, error) {
		values, total, err := service.ListBooks(ctx, library.BookListOptions{Limit: input.Limit, Offset: input.Offset, EntryID: library.EntryID(input.EntryID), State: library.State(input.State)})
		if err != nil {
			return nil, mapOrganizationError(err, "book")
		}

		output := &booksOutput{}
		output.Body.Items = make([]BookSummary, len(values))

		output.Body.Total, output.Body.Limit, output.Body.Offset = total, input.Limit, input.Offset
		for i, value := range values {
			output.Body.Items[i] = BookSummary{
				ID: value.ID, Kind: string(value.Kind), Title: value.Title,
				CoverAssetID: value.CoverAssetID, Author: value.Author,
				EntryCount: value.EntryCount, ContainsEntry: value.ContainsEntry,
				Revision: value.Revision, UpdatedAt: value.UpdatedAt,
				DeletedAt: value.DeletedAt,
			}
		}

		return output, nil
	})
}

func registerBookLifecycle(api huma.API, service OrganizationService) {
	huma.Register(api, huma.Operation{OperationID: "get-book", Method: http.MethodGet, Path: "/api/v1/books/{id}", Summary: "Get a book", Tags: []string{"Books"}, Errors: []int{404}}, func(ctx context.Context, input *bookPathInput) (*bookOutput, error) {
		value, err := service.GetBook(ctx, input.ID)
		if err != nil {
			return nil, mapOrganizationError(err, "book")
		}

		return &bookOutput{ETag: revisionETag(value.Revision), Body: fromBook(value)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-trashed-book", Method: http.MethodGet, Path: "/api/v1/trash/books/{id}", Summary: "Get a trashed book", Tags: []string{"Books"}, Errors: []int{404}}, func(ctx context.Context, input *bookPathInput) (*bookOutput, error) {
		value, err := service.GetTrashedBook(ctx, input.ID)
		if err != nil {
			return nil, mapOrganizationError(err, "book")
		}

		return &bookOutput{ETag: revisionETag(value.Revision), Body: fromBook(value)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "list-book-sourced-recipes", Method: http.MethodGet, Path: "/api/v1/books/{id}/sourced-recipes", Summary: "List sourced recipes", Tags: []string{"Books"}, Errors: []int{404}}, func(ctx context.Context, input *bookPathInput) (*bookSourceRecipesOutput, error) {
		values, err := service.ListBookSourceRecipes(ctx, input.ID)
		if err != nil {
			return nil, mapOrganizationError(err, "book")
		}

		output := &bookSourceRecipesOutput{}

		output.Body.Items = make([]BookSourceRecipe, len(values))
		for index, value := range values {
			output.Body.Items[index] = fromBookSourceRecipe(value)
		}

		return output, nil
	})
	huma.Register(api, huma.Operation{OperationID: "replace-book", Method: http.MethodPut, Path: "/api/v1/books/{id}", Summary: "Replace a book", Tags: []string{"Books"}, Errors: []int{400, 404, 409, 412, 422, 428}}, func(ctx context.Context, input *replaceBookInput) (*bookOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "book")
		if err != nil {
			return nil, err
		}

		value, err := service.ReplaceBook(ctx, input.ID, expected, toBook(input.Body))
		if err != nil {
			return nil, mapOrganizationError(err, "book")
		}

		return &bookOutput{ETag: revisionETag(value.Revision), Body: fromBook(value)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "trash-book", Method: http.MethodDelete, Path: "/api/v1/books/{id}", Summary: "Move a book to trash", Tags: []string{"Books"}, DefaultStatus: 204, Errors: []int{400, 404, 409, 412, 428}}, func(ctx context.Context, input *preconditionedBookInput) (*noContentOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "book")
		if err != nil {
			return nil, err
		}

		if err := service.DeleteBook(ctx, input.ID, expected); err != nil {
			return nil, mapOrganizationError(err, "book")
		}

		return &noContentOutput{}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "restore-book", Method: http.MethodPost, Path: "/api/v1/books/{id}/restore", Summary: "Restore a book from trash", Tags: []string{"Books"}, Errors: []int{400, 404, 409, 412, 422, 428}}, func(ctx context.Context, input *preconditionedBookInput) (*bookOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "book")
		if err != nil {
			return nil, err
		}

		value, err := service.RestoreBook(ctx, input.ID, expected)
		if err != nil {
			return nil, mapOrganizationError(err, "book")
		}

		return &bookOutput{ETag: revisionETag(value.Revision), Body: fromBook(value)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "delete-trashed-book", Method: http.MethodDelete, Path: "/api/v1/trash/books/{id}", Summary: "Delete a trashed book", Description: "Permanently deletes the book and its retained revisions. Cover assets remain until reference-aware garbage collection proves that no retained object uses them.", Tags: []string{"Books"}, DefaultStatus: 204, Errors: []int{400, 404, 409, 412, 428}}, func(ctx context.Context, input *preconditionedBookInput) (*noContentOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "book")
		if err != nil {
			return nil, err
		}

		if err := service.PurgeBook(ctx, input.ID, expected); err != nil {
			return nil, mapOrganizationError(err, "book")
		}

		return &noContentOutput{}, nil
	})
}

func registerBookHistory(api huma.API, service OrganizationService) {
	huma.Register(api, huma.Operation{OperationID: "list-book-revisions", Method: http.MethodGet, Path: "/api/v1/books/{id}/revisions", Summary: "List book revisions", Tags: []string{"Books"}, Errors: []int{404}}, func(ctx context.Context, input *listBookRevisionsInput) (*bookRevisionsOutput, error) {
		values, err := service.ListBookRevisions(ctx, input.ID, library.RevisionListOptions{Limit: input.Limit, Offset: input.Offset})
		if err != nil {
			return nil, mapOrganizationError(err, "book")
		}

		output := &bookRevisionsOutput{}

		output.Body.Items = make([]BookRevisionSummary, len(values))
		for index, value := range values {
			output.Body.Items[index] = fromBookRevisionSummary(value)
		}

		return output, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-book-revision", Method: http.MethodGet, Path: "/api/v1/books/{id}/revisions/{revision}", Summary: "Get a book revision", Tags: []string{"Books"}, Errors: []int{404}}, func(ctx context.Context, input *bookRevisionPathInput) (*bookRevisionOutput, error) {
		value, err := service.GetBookRevision(ctx, input.ID, input.Revision)
		if err != nil {
			return nil, mapOrganizationError(err, "book")
		}

		return &bookRevisionOutput{Body: fromBookRevision(value)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "restore-book-revision", Method: http.MethodPost, Path: "/api/v1/books/{id}/revisions/{revision}/restore", Summary: "Restore a book revision", Tags: []string{"Books"}, Errors: []int{400, 404, 409, 412, 422, 428}}, func(ctx context.Context, input *restoreBookRevisionInput) (*bookOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "book")
		if err != nil {
			return nil, err
		}

		value, err := service.RestoreBookRevision(ctx, input.ID, input.Revision, expected)
		if err != nil {
			return nil, mapOrganizationError(err, "book")
		}

		return &bookOutput{ETag: revisionETag(value.Revision), Body: fromBook(value)}, nil
	})
}

func registerSavedViews(api huma.API, service OrganizationService, recipes RecipeService) {
	huma.Register(api, huma.Operation{OperationID: "create-saved-view", Method: http.MethodPost, Path: "/api/v1/saved-views", Summary: "Create a saved view", Tags: []string{"Saved views"}, DefaultStatus: 201, Errors: []int{409, 422}}, func(ctx context.Context, input *createSavedViewInput) (*savedViewOutput, error) {
		value, err := retryCreate(ctx, api, "create-saved-view", input, func(work context.Context) (library.SavedView, error) {
			return service.CreateSavedView(work, toSavedView(input.Body))
		}, service.GetSavedView)
		if err != nil {
			return nil, mapOrganizationError(err, "saved view")
		}

		return &savedViewOutput{Location: "/api/v1/saved-views/" + value.ID, ETag: revisionETag(value.Revision), Body: fromSavedView(value)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "list-saved-views", Method: http.MethodGet, Path: "/api/v1/saved-views", Summary: "List saved views", Tags: []string{"Saved views"}}, func(ctx context.Context, _ *struct{}) (*savedViewsOutput, error) {
		values, err := service.ListSavedViews(ctx)
		if err != nil {
			return nil, mapOrganizationError(err, "saved view")
		}

		output := &savedViewsOutput{}

		output.Body.Items = make([]SavedView, len(values))
		for i, value := range values {
			output.Body.Items[i] = fromSavedView(value)
		}

		return output, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-saved-view", Method: http.MethodGet, Path: "/api/v1/saved-views/{id}", Summary: "Get a saved view", Tags: []string{"Saved views"}, Errors: []int{404}}, func(ctx context.Context, input *savedViewPathInput) (*savedViewOutput, error) {
		value, err := service.GetSavedView(ctx, input.ID)
		if err != nil {
			return nil, mapOrganizationError(err, "saved view")
		}

		return &savedViewOutput{ETag: revisionETag(value.Revision), Body: fromSavedView(value)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "list-saved-view-recipes", Method: http.MethodGet, Path: "/api/v1/saved-views/{id}/recipes", Summary: "List saved view recipes", Tags: []string{"Saved views"}, Errors: []int{404, 422}}, func(ctx context.Context, input *savedViewRecipesInput) (*listRecipesOutput, error) {
		view, err := service.GetSavedView(ctx, input.ID)
		if err != nil {
			return nil, mapOrganizationError(err, "saved view")
		}

		return readRecipePage(ctx, recipes, groupTranslations(library.ListOptions{
			Limit: input.Limit, Offset: input.Offset, Query: view.Query,
			State: library.StateActive, Sort: view.Sort,
		}, input.GroupTranslations, input.Languages))
	})
	huma.Register(api, huma.Operation{OperationID: "replace-saved-view", Method: http.MethodPut, Path: "/api/v1/saved-views/{id}", Summary: "Replace a saved view", Tags: []string{"Saved views"}, Errors: []int{400, 404, 412, 422, 428}}, func(ctx context.Context, input *replaceSavedViewInput) (*savedViewOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "saved view")
		if err != nil {
			return nil, err
		}

		value, err := service.ReplaceSavedView(ctx, input.ID, expected, toSavedView(input.Body))
		if err != nil {
			return nil, mapOrganizationError(err, "saved view")
		}

		return &savedViewOutput{ETag: revisionETag(value.Revision), Body: fromSavedView(value)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "delete-saved-view", Method: http.MethodDelete, Path: "/api/v1/saved-views/{id}", Summary: "Delete a saved view", Tags: []string{"Saved views"}, DefaultStatus: 204, Errors: []int{400, 404, 412, 428}}, func(ctx context.Context, input *preconditionedSavedViewInput) (*noContentOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "saved view")
		if err != nil {
			return nil, err
		}

		if err := service.DeleteSavedView(ctx, input.ID, expected); err != nil {
			return nil, mapOrganizationError(err, "saved view")
		}

		return &noContentOutput{}, nil
	})
}

// registerCookings exposes the record a recipe's counts are derived from. The
// collection carries no revision ETag because it is not an aggregate anyone
// replaces: a cooking is added, corrected, or forgotten one at a time.
func registerCookings(api huma.API, service OrganizationService) {
	huma.Register(api, huma.Operation{OperationID: "list-recipe-cookings", Method: http.MethodGet, Path: "/api/v1/recipes/{id}/cookings", Summary: "List cookings", Description: "Returns the remembered cookings, most recent first, with the count and most recent date derived from them.", Tags: []string{"Cooking history"}, Errors: []int{404}}, func(ctx context.Context, input *listCookingsInput) (*cookingHistoryOutput, error) {
		history, err := service.ListCookings(ctx, input.ID, library.CookingListOptions{Limit: input.Limit, Offset: input.Offset})
		if err != nil {
			return nil, mapOrganizationError(err, "recipe")
		}

		return &cookingHistoryOutput{Body: fromCookingHistory(history)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "create-recipe-cooking", Method: http.MethodPost, Path: "/api/v1/recipes/{id}/cookings", Summary: "Record a cooking", Tags: []string{"Cooking history"}, DefaultStatus: 201, Errors: []int{404, 409, 422}}, func(ctx context.Context, input *recordCookingInput) (*cookingOutput, error) {
		record, err := retryCreate(ctx, api, "create-recipe-cooking", input, func(work context.Context) (library.CookingRecord, error) {
			return service.RecordCooking(work, input.ID, toCooking(input.Body))
		}, func(work context.Context, id string) (library.CookingRecord, error) {
			configured, ok := api.(*retryAPI)
			if !ok {
				return library.CookingRecord{}, library.ErrNotFound
			}

			if reader := configured.cooking; reader != nil {
				return reader.GetCooking(work, input.ID, id)
			}

			return library.CookingRecord{}, library.ErrNotFound
		})
		if err != nil {
			return nil, mapCookingError(err, "recipe")
		}

		return &cookingOutput{Location: "/api/v1/recipes/" + input.ID + "/cookings/" + record.ID, Body: fromCooking(record)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "replace-recipe-cooking", Method: http.MethodPut, Path: "/api/v1/recipes/{id}/cookings/{cooking_id}", Summary: "Replace a cooking", Tags: []string{"Cooking history"}, Errors: []int{404, 422}}, func(ctx context.Context, input *replaceCookingInput) (*cookingOutput, error) {
		record, err := service.ReplaceCooking(ctx, input.ID, input.CookingID, toCooking(input.Body))
		if err != nil {
			return nil, mapCookingError(err, "cooking")
		}

		return &cookingOutput{Location: "/api/v1/recipes/" + input.ID + "/cookings/" + record.ID, Body: fromCooking(record)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "delete-recipe-cooking", Method: http.MethodDelete, Path: "/api/v1/recipes/{id}/cookings/{cooking_id}", Summary: "Delete a cooking", Tags: []string{"Cooking history"}, DefaultStatus: 204, Errors: []int{404}}, func(ctx context.Context, input *cookingPathInput) (*noContentOutput, error) {
		if err := service.DeleteCooking(ctx, input.ID, input.CookingID); err != nil {
			return nil, mapOrganizationError(err, "cooking")
		}

		return &noContentOutput{}, nil
	})
}

// mapCookingError keeps a rejected cooking from reporting itself as a failure
// to organize a recipe, which is a different thing entirely.
func mapCookingError(err error, entity string) error {
	if _, ok := errors.AsType[huma.StatusError](err); ok {
		return err
	}

	if exhausted := storageProblem(err); exhausted != nil {
		return exhausted
	}

	if errors.Is(err, library.ErrInvalidOrganization) {
		return problem(422, "cooking-invalid", "A cooking needs a real calendar date, and a batch size greater than zero")
	}

	return mapOrganizationError(err, entity)
}

func parseRevisionPrecondition(value, noun string) (uint64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, problem(428, "precondition-required", "If-Match is required for changes to an existing "+noun)
	}

	if strings.HasPrefix(value, "W/") || len(value) < 3 || value[0] != '"' || value[len(value)-1] != '"' {
		return 0, problem(400, "invalid-if-match", "If-Match must contain one strong "+noun+" revision ETag")
	}

	revision, err := strconv.ParseUint(value[1:len(value)-1], 10, 64)
	if err != nil || revision == 0 {
		return 0, problem(400, "invalid-if-match", "If-Match must contain one strong "+noun+" revision ETag")
	}

	return revision, nil
}

func mapOrganizationError(err error, entity string) error {
	if _, ok := errors.AsType[huma.StatusError](err); ok {
		return err
	}

	if exhausted := storageProblem(err); exhausted != nil {
		return exhausted
	}

	if denied := accessProblem(err); denied != nil {
		return denied
	}

	switch {
	case errors.Is(err, library.ErrInvalidOrganization):
		return problem(422, "organization-invalid", "Those tags, books, or collections could not be applied as given")
	case errors.Is(err, library.ErrBookIdentityInUse):
		return problem(409, "book-identity-conflict", "A section or entry ID in this book is already used by another book. Use new IDs.")
	case errors.Is(err, library.ErrUnknownMember):
		return problem(404, "recipe-not-found", "A book entry names a recipe this library does not hold")
	case errors.Is(err, library.ErrNotFound):
		return problem(404, strings.ReplaceAll(entity, " ", "-")+"-not-found", "The requested "+entity+" does not exist")
	case errors.Is(err, library.ErrInvalidState):
		return problem(409, "organization-state-conflict", "The recipe is not active")
	case errors.Is(err, library.ErrConflict):
		return problem(409, strings.ReplaceAll(entity, " ", "-")+"-conflict", "The "+entity+" conflicts with retained library data")
	case errors.Is(err, library.ErrPreconditionFailed):
		if p, ok := errors.AsType[*library.PreconditionError](err); ok {
			return problem(412, strings.ReplaceAll(entity, " ", "-")+"-revision-conflict", fmt.Sprintf("The %s changed since it was loaded. Expected revision %d, current revision is %d", entity, p.Expected, p.Current))
		}

		return problem(412, "revision-conflict", "The resource changed since it was loaded")
	default:
		return problem(500, "internal-error", "The request could not be completed")
	}
}

func toOrganization(recipeID string, input OrganizationInput) library.Organization {
	r := library.Organization{EntryID: library.EntryID(recipeID), FacetIDs: append([]string(nil), input.FacetIDs...), TagIDs: append([]string(nil), input.TagIDs...), State: library.UserState{Favorite: input.UserState.Favorite, Rating: input.UserState.Rating, PersonalNote: input.UserState.PersonalNote, PreferredYield: toRational(input.UserState.PreferredYield), PreferredScale: toRational(input.UserState.PreferredScale), PreferredYieldAssertion: input.UserState.PreferredYieldAssertion}}
	for _, v := range input.Sources {
		r.Sources = append(r.Sources, library.RecipeSource{ID: v.ID, Kind: library.SourceKind(v.Kind), URL: v.URL, PublicationBookID: v.PublicationBookID, PageStart: v.PageStart, PageEnd: v.PageEnd, OriginalTitle: v.OriginalTitle, Attribution: v.Attribution, AccessedAt: v.AccessedAt})
	}

	return r
}

func toBookMembership(input []BookMembershipInput) []library.BookMembershipChange {
	if len(input) == 0 {
		return nil
	}

	result := make([]library.BookMembershipChange, len(input))
	for index, change := range input {
		result[index] = library.BookMembershipChange{BookID: change.BookID, Revision: change.Revision, Member: change.Member}
	}

	return result
}

func fromOrganization(v library.Organization) RecipeOrganization {
	r := RecipeOrganization{LibraryEntryID: string(v.EntryID), Revision: v.Revision, FacetIDs: append([]string(nil), v.FacetIDs...), TagIDs: append([]string(nil), v.TagIDs...), UserState: UserState{Favorite: v.State.Favorite, Rating: v.State.Rating, PersonalNote: v.State.PersonalNote, PreferredYield: fromRational(v.State.PreferredYield), PreferredScale: fromRational(v.State.PreferredScale), PreferredYieldAssertion: v.State.PreferredYieldAssertion}, UpdatedAt: v.UpdatedAt, Sources: []RecipeSource{}}
	for _, x := range v.Sources {
		r.Sources = append(r.Sources, RecipeSource{ID: x.ID, Kind: string(x.Kind), URL: x.URL, PublicationBookID: x.PublicationBookID, PageStart: x.PageStart, PageEnd: x.PageEnd, OriginalTitle: x.OriginalTitle, Attribution: x.Attribution, AccessedAt: x.AccessedAt})
	}

	return r
}

func toRational(value *string) *recipe.Rational {
	if value == nil {
		return nil
	}

	converted := recipe.Rational(*value)

	return &converted
}

func fromRational(value *recipe.Rational) *string {
	if value == nil {
		return nil
	}

	converted := value.String()

	return &converted
}

func toCooking(v CookingInput) library.CookingRecord {
	return library.CookingRecord{CookedOn: library.Date(v.CookedOn), CookedTime: v.CookedTime, Note: v.Note, Scale: toRational(v.Scale), SessionID: v.SessionID}
}

func fromCooking(v library.CookingRecord) CookingRecord {
	return CookingRecord{ID: v.ID, SessionID: v.SessionID, CookedOn: v.CookedOn.String(), CookedTime: v.CookedTime, Note: v.Note, Scale: fromRational(v.Scale), CreatedAt: v.CreatedAt}
}

func fromCookingHistory(v library.CookingHistory) CookingHistory {
	result := CookingHistory{LibraryEntryID: string(v.EntryID), TimesCooked: v.TimesCooked, ImportedBaselines: v.ImportedBaselines, Items: []CookingRecord{}}
	if v.LastCooked != nil {
		value := v.LastCooked.String()
		result.LastCooked = &value
	}

	for _, record := range v.Records {
		result.Items = append(result.Items, fromCooking(record))
	}

	return result
}

func toTag(v TagInput) library.Tag {
	return library.Tag{Name: v.Name, Color: v.Color}
}

func fromTag(v library.Tag) Tag {
	return Tag{ID: v.ID, Name: v.Name, Color: v.Color, Revision: v.Revision, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

func toFacet(v FacetInput) library.Facet {
	return library.Facet{Kind: library.FacetKind(v.Kind), Value: v.Value}
}

func fromFacet(v library.Facet) Facet {
	return Facet{ID: v.ID, Kind: string(v.Kind), Value: v.Value, Revision: v.Revision, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

func toBook(v BookInput) library.Book {
	r := library.Book{Kind: library.BookKind(v.Kind), Title: v.Title, Subtitle: v.Subtitle, Description: v.Description, CoverAssetID: v.CoverAssetID, Author: v.Author, Editor: v.Editor, Edition: v.Edition, ISBN: v.ISBN, Publisher: v.Publisher, PublicationDate: v.PublicationDate}
	for _, x := range v.Sections {
		r.Sections = append(r.Sections, library.BookSection{ID: x.ID, Title: x.Title, Position: x.Position})
	}

	for _, x := range v.Entries {
		r.Entries = append(r.Entries, library.BookEntry{ID: x.ID, SectionID: x.SectionID, EntryID: library.EntryID(x.LibraryEntryID), Position: x.Position, Note: x.Note})
	}

	return r
}

func fromBook(v library.Book) Book {
	r := Book{ID: v.ID, Kind: string(v.Kind), Title: v.Title, Subtitle: v.Subtitle, Description: v.Description, CoverAssetID: v.CoverAssetID, Author: v.Author, Editor: v.Editor, Edition: v.Edition, ISBN: v.ISBN, Publisher: v.Publisher, PublicationDate: v.PublicationDate, Revision: v.Revision, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, DeletedAt: v.DeletedAt, Sections: []BookSection{}, Entries: []BookEntry{}}
	for _, x := range v.Sections {
		r.Sections = append(r.Sections, BookSection{ID: x.ID, Title: x.Title, Position: x.Position})
	}

	for _, x := range v.Entries {
		r.Entries = append(r.Entries, BookEntry{ID: x.ID, SectionID: x.SectionID, LibraryEntryID: string(x.EntryID), Position: x.Position, Note: x.Note, RecipeState: string(x.State)})
	}

	return r
}

func fromBookRevision(v library.BookRevision) BookRevision {
	return BookRevision{Book: fromBook(v.Book), State: string(v.State), Cause: string(v.Cause), SourceRevision: v.SourceRevision, CreatedAt: v.CreatedAt, ActorID: v.ActorID, ActorName: v.ActorName}
}

func fromBookRevisionSummary(v library.BookRevisionSummary) BookRevisionSummary {
	return BookRevisionSummary{BookID: v.BookID, Revision: v.Revision, State: string(v.State), Cause: string(v.Cause), SourceRevision: v.SourceRevision, CreatedAt: v.CreatedAt, ActorID: v.ActorID, ActorName: v.ActorName}
}

func fromBookSourceRecipe(v library.BookSourceRecipe) BookSourceRecipe {
	return BookSourceRecipe{LibraryEntryID: string(v.EntryID), Title: v.Title, PageStart: v.PageStart, PageEnd: v.PageEnd, OriginalTitle: v.OriginalTitle}
}

func toSavedView(v SavedViewInput) library.SavedView {
	return library.SavedView{Title: v.Title, Query: v.Query, Sort: library.ListSort(v.Sort)}
}

func fromSavedView(v library.SavedView) SavedView {
	return SavedView{ID: v.ID, Title: v.Title, Query: v.Query, Sort: string(v.Sort), Revision: v.Revision, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
