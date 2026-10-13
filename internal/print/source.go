package print

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
)

// RecipeSnapshot is one recipe frozen at a specific revision; every snapshot
// in a document is resolved before any is typeset.
type RecipeSnapshot struct {
	EntryID  library.EntryID
	Revision uint64
	Recipe   recipe.Document
	// PreferredScale is the batch this reader asked Savorum to open the recipe
	// at, resolved against the revision being printed. It is empty when they
	// have not asked for one.
	PreferredScale recipe.Rational
	PreferredYield *int
}

// PrintSource is where a print job's recipes come from. Resolving a source is
// the moment revisions are frozen, and the order it returns is the order the
// document is composed in.
type PrintSource interface {
	// Title names the artifact the source produces.
	Title() string
	// Resolve fails without reading recipes when the source holds more than
	// maximum.
	Resolve(ctx context.Context, maximum int) ([]RecipeSnapshot, error)
}

// SourceIdentity is the resolved source state that can affect a document
// independently of its recipe snapshots.
type SourceIdentity struct {
	Kind     string `json:"kind"`
	ID       string `json:"id,omitempty"`
	Revision uint64 `json:"revision,omitempty"`
	Title    string `json:"title"`
}

type identifiedPrintSource interface {
	Identity() SourceIdentity
}

// RecipeLibrary is the reading half of the library the print service needs.
type RecipeLibrary interface {
	Get(ctx context.Context, id library.EntryID) (library.Entry, error)
	GetOrganization(ctx context.Context, recipeID string) (library.Organization, error)
}

// BookLibrary reads the curated book a print job may be built from.
type BookLibrary interface {
	GetBook(ctx context.Context, id string) (library.Book, error)
}

type recipeSelectionSource struct {
	library  RecipeLibrary
	entryIDs []library.EntryID
	title    string
}

// NewRecipeSource prints exactly one recipe.
func NewRecipeSource(reader RecipeLibrary, id library.EntryID) PrintSource {
	return &recipeSelectionSource{library: reader, entryIDs: []library.EntryID{id}}
}

// NewSelectionSource prints an ordered selection, preserving the order the
// reader selected in rather than reordering it behind their back.
func NewSelectionSource(reader RecipeLibrary, ids []library.EntryID) PrintSource {
	return &recipeSelectionSource{library: reader, entryIDs: ids, title: "Recipes"}
}

func (s *recipeSelectionSource) Title() string { return s.title }
func (s *recipeSelectionSource) Identity() SourceIdentity {
	return SourceIdentity{Kind: "selection", Title: s.title}
}

func (s *recipeSelectionSource) Resolve(ctx context.Context, maximum int) ([]RecipeSnapshot, error) {
	if len(s.entryIDs) == 0 {
		return nil, ErrNoRecipes
	}

	if len(s.entryIDs) > maximum {
		return nil, collectionTooLarge(maximum)
	}

	snapshots := make([]RecipeSnapshot, 0, len(s.entryIDs))

	seen := make(map[library.EntryID]bool, len(s.entryIDs))
	for _, id := range s.entryIDs {
		if seen[id] {
			continue
		}

		seen[id] = true

		snapshot, err := resolveSnapshot(ctx, s.library, id)
		if err != nil {
			return nil, err
		}

		snapshots = append(snapshots, snapshot)
	}

	if len(snapshots) == 1 && s.title == "" {
		s.title = snapshots[0].Recipe.Title
	}

	return snapshots, nil
}

type bookSource struct {
	library  RecipeLibrary
	books    BookLibrary
	bookID   string
	title    string
	revision uint64
}

// NewBookSource prints a curated book through the same pipeline a single
// recipe uses, preserving the book's own section and entry ordering.
func NewBookSource(reader RecipeLibrary, books BookLibrary, bookID string) PrintSource {
	return &bookSource{library: reader, books: books, bookID: bookID}
}

func (s *bookSource) Title() string { return s.title }
func (s *bookSource) Identity() SourceIdentity {
	return SourceIdentity{Kind: "book", ID: s.bookID, Revision: s.revision, Title: s.title}
}

func (s *bookSource) Resolve(ctx context.Context, maximum int) ([]RecipeSnapshot, error) {
	book, err := s.books.GetBook(ctx, s.bookID)
	if err != nil {
		return nil, err
	}

	s.title = strings.TrimSpace(book.Title)
	s.revision = book.Revision

	ordered := library.BookOrder(book)
	if len(ordered) == 0 {
		return nil, ErrNoRecipes
	}

	if len(ordered) > maximum {
		return nil, collectionTooLarge(maximum)
	}

	snapshots := make([]RecipeSnapshot, 0, len(ordered))
	for _, id := range ordered {
		snapshot, err := resolveSnapshot(ctx, s.library, id)
		if err != nil {
			return nil, err
		}

		snapshots = append(snapshots, snapshot)
	}

	return snapshots, nil
}

func resolveSnapshot(ctx context.Context, reader RecipeLibrary, id library.EntryID) (RecipeSnapshot, error) {
	entry, err := reader.Get(ctx, id)
	if err != nil {
		return RecipeSnapshot{}, err
	}

	snapshot := RecipeSnapshot{EntryID: entry.ID, Revision: entry.Revision, Recipe: entry.Recipe}

	organization, err := reader.GetOrganization(ctx, string(id))
	if err == nil {
		snapshot.PreferredScale = preferredScaleOf(entry.Recipe, organization)
		snapshot.PreferredYield = organization.State.PreferredYieldAssertion
	}

	return snapshot, nil
}

// preferredScaleOf turns the yield a reader remembered into the factor that
// produces it from the revision being printed.
func preferredScaleOf(document recipe.Document, organization library.Organization) recipe.Rational {
	state := organization.State
	if state.PreferredYield != nil {
		wanted, err := state.PreferredYield.Rat()

		written := writtenYield(document, state.PreferredYieldAssertion)
		if err == nil && written != nil && wanted.Sign() > 0 && written.Sign() > 0 {
			factor := new(big.Rat).Quo(wanted, written)
			if canonical, err := recipe.ParseRational(factor.RatString()); err == nil {
				return canonical
			}
		}
	}

	if state.PreferredScale != nil {
		return *state.PreferredScale
	}

	return ""
}

// writtenYield reports the amount a recipe says it makes, when that can be
// read as a single exact number. A range or a description cannot say what
// doubling it would mean.
func writtenYield(document recipe.Document, preferred *int) *big.Rat {
	shown, _ := document.DisplayYield(preferred)
	if shown == nil || shown.Quantity == nil {
		return nil
	}

	quantity := shown.Quantity
	if quantity.Kind != recipe.QuantitySimple || quantity.Amount == nil || quantity.Amount.Value == nil {
		return nil
	}

	value, err := quantity.Amount.Value.Rat()
	if err != nil || value.Sign() <= 0 {
		return nil
	}

	return value
}

func collectionTooLarge(maximum int) error {
	return fmt.Errorf("%w: a printed collection holds at most %d recipes", ErrTooLarge, maximum)
}
