package library

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/recipe"
)

// DefaultUserID identifies the local user until first-class accounts are available.
const DefaultUserID = "local"

// FacetKind selects a controlled category of recipe classification.
type FacetKind string

// Supported categories for classifying recipes.
const (
	FacetCourse  FacetKind = "course"
	FacetCuisine FacetKind = "cuisine"
	FacetMethod  FacetKind = "method"
	FacetDietary FacetKind = "dietary"
)

// Facet is one reusable value within a classification category.
type Facet struct {
	ID        string    `json:"id"`
	Kind      FacetKind `json:"kind"`
	Value     string    `json:"value"`
	Revision  uint64    `json:"revision"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Tag is a reusable library label with an optional display color.
type Tag struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color,omitempty"`
	Revision  uint64    `json:"revision"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UserState is what one person has decided about a recipe. Cooking counts and
// dates are derived from CookingRecord and reported by CookingHistory instead.
type UserState struct {
	Favorite     bool   `json:"favorite"`
	Rating       *uint8 `json:"rating,omitempty"`
	PersonalNote string `json:"personal_note,omitempty"`
	// PreferredYield is the amount this reader normally makes, expressed in the
	// recipe's own yield. Storing the wanted yield rather than a factor means a
	// correction to the recipe's yield still produces the amount they wanted.
	PreferredYield *recipe.Rational `json:"preferred_yield,omitempty"`
	// PreferredScale is the fallback for a recipe whose yield cannot be read as
	// a number. At most one of the two is set.
	PreferredScale          *recipe.Rational `json:"preferred_scale,omitempty"`
	PreferredYieldAssertion *int             `json:"preferred_yield_assertion,omitempty"`
}

// Date is a calendar date in YYYY-MM-DD form. A cooking is remembered by the
// day it happened rather than by a clock reading in some timezone.
type Date string

var datePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// ParseDate accepts a well-formed calendar date and rejects anything else,
// including a date-shaped string naming a day that does not exist.
func ParseDate(value string) (Date, error) {
	value = strings.TrimSpace(value)
	if !datePattern.MatchString(value) {
		return "", errors.New("date must be written as YYYY-MM-DD")
	}

	parsed, err := time.Parse(time.DateOnly, value)
	if err != nil || parsed.Format(time.DateOnly) != value {
		return "", errors.New("date does not name a real day")
	}

	return Date(value), nil
}

func (d Date) String() string { return string(d) }

// CookingRecord is one remembered occasion on which a recipe was cooked.
type CookingRecord struct {
	ID string `json:"id"`
	// SessionID makes finishing the same active cooking session idempotent.
	// It is absent for records created outside cooking mode.
	SessionID  *string `json:"session_id,omitempty"`
	CookedOn   Date    `json:"cooked_on"`
	CookedTime string  `json:"cooked_time,omitempty"`
	Note       string  `json:"note,omitempty"`
	// Scale is the batch actually made, as a factor of the recipe as written.
	// It is absent when the recipe was made as written.
	Scale     *recipe.Rational `json:"scale,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
}

// CookingHistory is the record of a recipe's use together with its derived
// count. TimesCooked is the imported aggregate baseline plus actual Savorum
// records; LastCooked is taken only from actual dated records.
type CookingHistory struct {
	EntryID     EntryID `json:"library_entry_id"`
	TimesCooked uint64  `json:"times_cooked"`
	// ImportedBaseline is an aggregate asserted by a source system. It is
	// deliberately separate from Records: Savorum never invents dated history.
	ImportedBaseline  uint64                      `json:"imported_baseline,omitempty"`
	ImportedBaselines []ImportedCookCountBaseline `json:"imported_baselines,omitempty"`
	LastCooked        *Date                       `json:"last_cooked,omitempty"`
	Records           []CookingRecord             `json:"records"`
}

// ImportedCookCountBaseline attributes a source system's aggregate cooking count.
type ImportedCookCountBaseline struct {
	Count     uint64 `json:"count"`
	Source    string `json:"source"`
	SourceKey string `json:"source_key"`
}

// ImportedLibraryMetadata carries optional user preferences and cooking counts from an import.
type ImportedLibraryMetadata struct {
	Favorite     *bool
	Rating       *uint8
	PersonalNote *string
	CookCount    *ImportedCookCountBaseline
}

// CookingListOptions bounds a page of remembered cooking occasions.
type CookingListOptions struct {
	Limit  int
	Offset int
}

// SourceKind distinguishes web, publication, and other recipe provenance.
type SourceKind string

// Supported provenance relationships for a library entry.
const (
	SourceURL         SourceKind = "url"
	SourcePublication SourceKind = "publication"
	SourceOther       SourceKind = "other"
)

// RecipeSource records where this Savorum library entry was obtained. These
// local navigation and publication relationships never become portable recipe
// source fields merely because the recipe is exported.
type RecipeSource struct {
	ID                string     `json:"id"`
	Kind              SourceKind `json:"kind"`
	URL               string     `json:"url,omitempty"`
	PublicationBookID string     `json:"publication_book_id,omitempty"`
	PageStart         *uint64    `json:"page_start,omitempty"`
	PageEnd           *uint64    `json:"page_end,omitempty"`
	OriginalTitle     string     `json:"original_title,omitempty"`
	Attribution       string     `json:"attribution,omitempty"`
	AccessedAt        *time.Time `json:"accessed_at,omitempty"`
}

// BookMembershipChange is one book-membership decision applied in the same
// transaction as a recipe-organization change. Each change names the book
// revision it expects, so curating a recipe never half-succeeds and never
// silently overwrites a book that changed in the meantime.
type BookMembershipChange struct {
	BookID   string `json:"book_id"`
	Revision uint64 `json:"revision"`
	Member   bool   `json:"member"`
	// EntryID is the book entry identifier used when membership is added. The
	// service generates it so identifier policy stays out of persistence.
	EntryID string `json:"-"`
}

// Organization holds a recipe's local classifications, user preferences, and provenance.
type Organization struct {
	EntryID   EntryID        `json:"library_entry_id"`
	Revision  uint64         `json:"revision"`
	FacetIDs  []string       `json:"facet_ids"`
	TagIDs    []string       `json:"tag_ids"`
	State     UserState      `json:"user_state"`
	Sources   []RecipeSource `json:"sources"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// BookKind distinguishes a curated collection from a source publication.
type BookKind string

// Supported roles of books in the library.
const (
	BookPersonal    BookKind = "personal"
	BookPublication BookKind = "publication"
)

// Book persists ordered membership. Preview identifiers are derived and
// excluded from serialized revisions.
type Book struct {
	ID              string        `json:"id"`
	Kind            BookKind      `json:"kind"`
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
	PreviewAssetID  string        `json:"-"`
	PreviewEntryID  string        `json:"-"`
}

// BookListItem is the bounded collection representation. Complete ordered
// sections and entries belong only to the individual book resource.
type BookListItem struct {
	ID             string
	Kind           BookKind
	Title          string
	CoverAssetID   string
	PreviewAssetID string
	PreviewEntryID string
	Author         string
	EntryCount     int
	ContainsEntry  bool
	Revision       uint64
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

// BookListOptions selects a page of active or trashed books and optional membership annotations.
type BookListOptions struct {
	Limit   int
	Offset  int
	EntryID EntryID
	State   State
}

// BookRevision contains an immutable book snapshot and the cause of its creation.
type BookRevision struct {
	ActorID        string
	ActorName      string
	Book           Book
	State          State
	Cause          RevisionCause
	SourceRevision *uint64
	CreatedAt      time.Time
}

// BookRevisionSummary identifies a historical book state without loading its contents.
type BookRevisionSummary struct {
	ActorID        string
	ActorName      string
	BookID         string
	Revision       uint64
	State          State
	Cause          RevisionCause
	SourceRevision *uint64
	CreatedAt      time.Time
}

// BookSection names and orders a group within a book.
type BookSection struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Position int    `json:"position"`
}

// BookEntry places a library recipe in a book, optionally within a section.
type BookEntry struct {
	ID        string  `json:"id"`
	SectionID string  `json:"section_id,omitempty"`
	EntryID   EntryID `json:"library_entry_id"`
	Position  int     `json:"position"`
	Note      string  `json:"note,omitempty"`
	State     State   `json:"recipe_state"`
}

// BookSourceRecipe is derived from recipe provenance. It is not manual book
// membership and therefore has no book position or entry note.
type BookSourceRecipe struct {
	EntryID       EntryID `json:"library_entry_id"`
	Title         string  `json:"title"`
	PageStart     *uint64 `json:"page_start,omitempty"`
	PageEnd       *uint64 `json:"page_end,omitempty"`
	OriginalTitle string  `json:"original_title,omitempty"`
}

// SavedView stores a reusable library search and its preferred ordering.
type SavedView struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Query     string    `json:"query"`
	Sort      ListSort  `json:"sort,omitempty"`
	Revision  uint64    `json:"revision"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BookOrder walks a book in its own order: sections in their declared
// positions, then entries within each. Membership retained through a recipe's
// trash is skipped, because a trashed recipe is not part of the book a reader
// would print today.
func BookOrder(book Book) []EntryID {
	positions := make(map[string]int, len(book.Sections))
	for _, section := range book.Sections {
		positions[section.ID] = section.Position
	}

	entries := make([]BookEntry, 0, len(book.Entries))
	for _, entry := range book.Entries {
		if entry.State == StateTrash {
			continue
		}

		entries = append(entries, entry)
	}

	sortBookEntries(entries, positions)

	ordered := make([]EntryID, 0, len(entries))
	for _, entry := range entries {
		ordered = append(ordered, entry.EntryID)
	}

	return ordered
}

func sortBookEntries(entries []BookEntry, sections map[string]int) {
	sectionRank := func(entry BookEntry) int {
		if entry.SectionID == "" {
			// Entries outside every section sort first, which is where a book
			// shows them.
			return -1
		}

		position, found := sections[entry.SectionID]
		if !found {
			return 1 << 30
		}

		return position
	}

	for index := 1; index < len(entries); index++ {
		current := entries[index]

		position := index - 1
		for position >= 0 {
			previous := entries[position]
			if sectionRank(previous) < sectionRank(current) ||
				(sectionRank(previous) == sectionRank(current) && previous.Position <= current.Position) {
				break
			}

			entries[position+1] = previous
			position--
		}

		entries[position+1] = current
	}
}
