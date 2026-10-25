package library

import (
	"context"
	"errors"
	"time"

	"github.com/savorum/savorum/internal/recipe"
)

var (
	// ErrNotFound reports that the requested library entry does not exist.
	ErrNotFound = errors.New("library entry not found")
	// ErrConflict reports an attempt to create an entry that already exists.
	ErrConflict = errors.New("library entry already exists")
	// ErrInvalidState reports an operation on an entry in the wrong lifecycle state.
	ErrInvalidState = errors.New("library entry is not in the required state")
	// ErrPreconditionFailed reports a failed library entry revision precondition.
	ErrPreconditionFailed = errors.New("library entry revision precondition failed")
	// ErrDocumentIdentityChange reports an attempt to replace a recipe's portable identity.
	ErrDocumentIdentityChange = errors.New("recipe document identity is immutable within a library entry")
	// ErrInvalidOrganization reports invalid library organization data.
	ErrInvalidOrganization = errors.New("library organization data is invalid")
	// ErrUnknownMember reports a book entry naming a recipe this library does not hold.
	ErrUnknownMember = errors.New("a book entry names a recipe this library does not hold")
	// ErrBookIdentityInUse reports a book section or entry ID another book already holds.
	ErrBookIdentityInUse = errors.New("a book section or entry ID is already used by another book")
	// ErrInvalidSearch reports an invalid library search query.
	ErrInvalidSearch = errors.New("library search query is invalid")
)

// State distinguishes active library entries from entries retained in trash.
type State string

// EntryID identifies one aggregate in this Savorum library. It is generated
// locally, never forms part of a portable recipe document, and must not be
// confused with recipe.RecipeID.
type EntryID string

// Lifecycle states retained by the library.
const (
	StateActive State = "active"
	StateTrash  State = "trash"
)

// RevisionCause records which operation produced an immutable snapshot.
type RevisionCause string

// Causes recorded in recipe and book revision history.
//
// Retained history is immutable and its vocabulary is fixed by the schema, not
// by this list: a recipe revision accepts api, manual_edit, import, restore and
// trash, and a book revision accepts api, manual_edit, restore, trash and
// recipe_lifecycle. A new operation records the closest cause the schema already
// admits and names itself in import history or the audit log, which is why a
// merge records import and reanalysis records manual_edit. A cause outside those
// sets is refused by a CHECK constraint when the revision is written, so adding
// one here does not make it storable.
const (
	RevisionCauseAPI             RevisionCause = "api"
	RevisionCauseManualEdit      RevisionCause = "manual_edit"
	RevisionCauseImport          RevisionCause = "import"
	RevisionCauseRestore         RevisionCause = "restore"
	RevisionCauseTrash           RevisionCause = "trash"
	RevisionCauseRecipeLifecycle RevisionCause = "recipe_lifecycle"
	RevisionCauseMerge           RevisionCause = "merge"
)

// PreconditionError reports the expected and current revisions of a stale write.
type PreconditionError struct {
	EntryID  EntryID
	Expected uint64
	Current  uint64
}

func (*PreconditionError) Error() string {
	return "library entry revision does not match the required revision"
}

func (*PreconditionError) Unwrap() error {
	return ErrPreconditionFailed
}

// Entry is the local aggregate root around a portable recipe document. Its ID
// belongs to this library and is deliberately distinct from Recipe.ID.
type Entry struct {
	ID        EntryID         `json:"id"`
	Recipe    recipe.Document `json:"recipe"`
	Revision  uint64          `json:"revision"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	DeletedAt *time.Time      `json:"deleted_at,omitempty"`
}

// Summary is the intentionally small read model used by library and search
// views. It must remain independent from the complete portable recipe
// document so collection reads never decode document_json.
type Summary struct {
	ID           EntryID
	RecipeID     recipe.RecipeID
	Title        string
	Language     string
	Description  string
	Authors      []string
	ActiveTime   string
	TotalTime    string
	Revision     uint64
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
	PrimaryImage *AssetReference
	Tags         []Tag
	// Books is curated membership. SourceBooks is the separate provenance
	// relationship: publication books a recipe was taken from.
	Books       []BookSummary
	SourceBooks []BookSummary
	Favorite    bool
	// TimesCooked and LastCooked are counted from this reader's cooking
	// records. They are reported here so a library view can say "last made
	// three months ago" without reading every record.
	TimesCooked uint64
	LastCooked  *Date
	// Translations lists the other renditions of a grouped result.
	Translations []SummaryRendition
}

// SummaryRendition is another rendition of a grouped search result. Matched
// reports whether it also matched a search, and is false when there was none.
type SummaryRendition struct {
	ID       EntryID
	Title    string
	Language string
	Matched  bool
}

// AssetReference describes an image in a library summary without loading its bytes.
type AssetReference struct {
	ID           string
	LocalAssetID string
	MediaType    string
	Alt          string
	Caption      string
	Available    bool
}

// BookSummary identifies a collection or source publication in a recipe summary.
type BookSummary struct {
	ID    string
	Kind  BookKind
	Title string
}

// RecipeReference is the narrow identity a picker needs to name a recipe
// without loading its summary or document.
type RecipeReference struct {
	ID       EntryID
	Title    string
	Revision uint64
}

// Revision contains an immutable recipe snapshot and its lifecycle metadata.
type Revision struct {
	ActorID        string
	ActorName      string
	EntryID        EntryID
	Number         uint64
	Recipe         recipe.Document
	State          State
	Cause          RevisionCause
	SourceRevision *uint64
	CreatedAt      time.Time
}

// RevisionSummary deliberately excludes the immutable recipe snapshot. The
// full document is available from GetRevision only.
//
// Changes names the recipe areas this revision altered, against the revision
// before it. It is empty where nothing in the document changed, such as a
// move to trash, and nil where there is no earlier revision to compare
// against.
type RevisionSummary struct {
	ActorID        string
	ActorName      string
	EntryID        EntryID
	Number         uint64
	State          State
	Cause          RevisionCause
	SourceRevision *uint64
	CreatedAt      time.Time
	Changes        []string
}

// RevisionMetadata records a change's cause and any historical revision it restores.
type RevisionMetadata struct {
	Cause          RevisionCause
	SourceRevision *uint64
}

// RevisionCommand requests a document or lifecycle change against an expected revision.
type RevisionCommand struct {
	ID               EntryID
	ExpectedRevision uint64
	Recipe           recipe.Document
	CurrentState     State
	TargetState      State
	Metadata         RevisionMetadata
	Timestamp        time.Time
}

// ImportMutation is one create or replacement inside an atomic recipe import.
// ExpectedRevision is zero for a create and the current revision for a
// replacement. Organization and history remain attached to replacements.
type ImportMutation struct {
	History          ImportContext
	ID               EntryID
	ExpectedRevision uint64
	// ExpectedRecipe binds a reconciled replacement to the exact document the
	// person reviewed. Repositories compare it inside the same transaction as
	// ExpectedRevision so purge/recreate cannot reuse a revision number.
	ExpectedRecipe *recipe.Document
	Recipe         recipe.Document
	// SourceSnapshot is comparison state from the source import. It is kept in
	// import history and is never used as an ordinary user revision.
	SourceSnapshot *recipe.Document
	Timestamp      time.Time
	// SourceAttachments are inserted with the recipe revision in the same
	// repository transaction. EntryID and Revision are assigned by the
	// repository; callers provide only the already-ingested asset metadata.
	SourceAttachments []SourceAttachment
}

// ListOptions selects a page of library summaries by state, search, and ordering.
type ListOptions struct {
	EntryIDs []EntryID
	Limit    int
	Offset   int
	Query    string
	State    State
	Sort     ListSort
	Search   SearchCriteria
	// GroupTranslations presents each translation set among active results
	// once, as the matching rendition PreferredLanguages chooses.
	GroupTranslations  bool
	PreferredLanguages []string
}

// ListSort selects the ordering of library search results.
type ListSort string

// Supported orderings of library summaries.
const (
	ListSortRelevance    ListSort = "relevance"
	ListSortUpdatedDesc  ListSort = "updated-desc"
	ListSortTitleAsc     ListSort = "title-asc"
	ListSortRatingDesc   ListSort = "rating-desc"
	ListSortTotalTimeAsc ListSort = "total-time-asc"
	ListSortCookedDesc   ListSort = "cooked-desc"
)

// SearchCriteria combines text, taxonomy, provenance, and user-state filters.
type SearchCriteria struct {
	Text string
	// TextAlternatives holds bounded title/author term expansions resolved by
	// persistence. A non-nil map records an attempted expansion.
	TextAlternatives map[string][]string
	Tags             []string
	Facets           map[FacetKind][]string
	Books            []string
	Sources          []string
	Authors          []string
	Ingredients      []string
	Favorite         *bool
	MinRating        *uint8
	MadeNever        bool
	ReviewRequired   *bool
	MissingContent   string
	// Imports narrows to the recipes the named imports saved.
	Imports []string
	// StatedDiets and StatedFreeFrom match only unconditional whole-recipe
	// claims the source made; the absence of a contrary claim never matches.
	StatedDiets    []string
	StatedFreeFrom []string
}

// RevisionListOptions bounds a page of immutable history summaries.
type RevisionListOptions struct {
	Limit  int
	Offset int
}

// PurgeTarget identifies the exact entry revision authorized for permanent removal.
type PurgeTarget struct {
	ID       EntryID
	Revision uint64
}

// BookPurgeTarget names one trashed book and the revision the caller reviewed,
// so emptying the trash is as explicit for books as it is for recipes.
type BookPurgeTarget struct {
	ID       string
	Revision uint64
}

// BookPurgeError names, by title, the book a mixed purge batch failed on.
type BookPurgeError struct {
	BookID string
	Title  string
	Err    error
}

// Name is what to call the book when explaining the failure to a reader.
func (e *BookPurgeError) Name() string {
	if e.Title != "" {
		return e.Title
	}

	return e.BookID
}

func (e *BookPurgeError) Error() string {
	return "purge book " + e.BookID + ": " + e.Err.Error()
}

func (e *BookPurgeError) Unwrap() error {
	return e.Err
}

// Repository persists recipe aggregates and their immutable revision history.
type Repository interface {
	Create(ctx context.Context, entry Entry, metadata RevisionMetadata) error
	Get(ctx context.Context, id EntryID, state State) (Entry, error)
	// FindByDocumentID reports the entry already carrying a portable recipe
	// document, which is how an import discovers that it is looking at a recipe
	// this library already has.
	FindByDocumentID(ctx context.Context, documentID recipe.RecipeID) (EntryID, uint64, State, error)
	// RegisteredAssets reports which of the named content hashes this library
	// actually holds bytes for.
	RegisteredAssets(ctx context.Context, hashes []string) (map[string]bool, error)
	ImportMany(ctx context.Context, mutations []ImportMutation) ([]Entry, error)
	List(ctx context.Context, options ListOptions) ([]Summary, error)
	Count(ctx context.Context, options ListOptions) (int, error)
	ListPage(ctx context.Context, options ListOptions) ([]Summary, int, error)
	ListReferences(ctx context.Context, state State) ([]RecipeReference, error)
	Revise(ctx context.Context, command RevisionCommand) (Entry, error)
	GetRevision(ctx context.Context, id EntryID, revision uint64) (Revision, error)
	ListRevisions(ctx context.Context, id EntryID, options RevisionListOptions) ([]RevisionSummary, error)
	Purge(ctx context.Context, id EntryID, expectedRevision uint64, orphanedAt time.Time) error
	PurgeMany(ctx context.Context, recipes []PurgeTarget, books []BookPurgeTarget, orphanedAt time.Time) error
}

// RevisionDocumentRepository reads the documents behind a page of revisions so
// history can say what each change touched. Listing history does not need the
// documents, which is why this is separate from Repository: a store that
// cannot supply them returns history without change summaries rather than
// failing.
type RevisionDocumentRepository interface {
	ListRevisionDocuments(ctx context.Context, id EntryID, options RevisionListOptions) ([]Revision, error)
}

// OrganizationRepository persists application-owned organization separately
// from portable recipe documents and their immutable revision history.
type OrganizationRepository interface {
	GetOrganization(ctx context.Context, recipeID, userID string) (Organization, error)
	ReplaceOrganization(ctx context.Context, recipeID, userID string, expectedRevision uint64, replacement Organization, membership []BookMembershipChange, timestamp time.Time) (Organization, error)
	CreateTag(ctx context.Context, tag Tag) error
	GetTag(ctx context.Context, id string) (Tag, error)
	ListTags(ctx context.Context) ([]Tag, error)
	ReplaceTag(ctx context.Context, expectedRevision uint64, tag Tag) (Tag, error)
	DeleteTag(ctx context.Context, id string, expectedRevision uint64, timestamp time.Time) error
	MergeTag(ctx context.Context, id string, expectedRevision uint64, targetID string, timestamp time.Time) error
	CreateFacet(ctx context.Context, facet Facet) error
	GetFacet(ctx context.Context, id string) (Facet, error)
	ListFacets(ctx context.Context, kind FacetKind) ([]Facet, error)
	ReplaceFacet(ctx context.Context, expectedRevision uint64, facet Facet) (Facet, error)
	DeleteFacet(ctx context.Context, id string, expectedRevision uint64, timestamp time.Time) error
	MergeFacet(ctx context.Context, id string, expectedRevision uint64, targetID string, timestamp time.Time) error
	CreateBook(ctx context.Context, book Book) error
	GetBook(ctx context.Context, id string, state State) (Book, error)
	ListBooks(ctx context.Context, options BookListOptions) ([]BookListItem, error)
	CountBooks(ctx context.Context, state State) (int, error)
	ListBookSourceRecipes(ctx context.Context, id string) ([]BookSourceRecipe, error)
	ReplaceBook(ctx context.Context, expectedRevision uint64, book Book) (Book, error)
	ReviseBookState(ctx context.Context, id string, expectedRevision uint64, currentState, targetState State, metadata RevisionMetadata, timestamp time.Time) (Book, error)
	RestoreBookRevision(ctx context.Context, id string, sourceRevision, expectedRevision uint64, metadata RevisionMetadata, timestamp time.Time) (Book, error)
	GetBookRevision(ctx context.Context, id string, revision uint64) (BookRevision, error)
	ListBookRevisions(ctx context.Context, id string, options RevisionListOptions) ([]BookRevisionSummary, error)
	PurgeBook(ctx context.Context, id string, expectedRevision uint64, orphanedAt time.Time) error
	CreateSavedView(ctx context.Context, view SavedView) error
	GetSavedView(ctx context.Context, id string) (SavedView, error)
	ListSavedViews(ctx context.Context) ([]SavedView, error)
	ReplaceSavedView(ctx context.Context, expectedRevision uint64, view SavedView) (SavedView, error)
	DeleteSavedView(ctx context.Context, id string, expectedRevision uint64) error
}

// CookingRepository persists a recipe's actual use. Its record count is added
// to any provenance-bearing imported baseline; the most recent date is always
// read from actual records rather than an imported aggregate.
type CookingRepository interface {
	ListCookings(ctx context.Context, recipeID, userID string, options CookingListOptions) (CookingHistory, error)
	RecordCooking(ctx context.Context, recipeID, userID string, record CookingRecord) (CookingRecord, error)
	ReplaceCooking(ctx context.Context, recipeID, userID string, record CookingRecord) (CookingRecord, error)
	DeleteCooking(ctx context.Context, recipeID, userID, cookingID string) error
}

// ImportMetadataRepository persists library facts supplied by another system.
// It is intentionally separate from portable recipe persistence.
type ImportMetadataRepository interface {
	SetImportedCookCountBaseline(ctx context.Context, recipeID, userID string, baseline ImportedCookCountBaseline) error
}

// Persistence is the complete storage contract required by the library
// service. All canonical v1 workflows include recipe and organization data.
type Persistence interface {
	Repository
	OrganizationRepository
	CookingRepository
}
