package export

import (
	"context"
	"fmt"
	"strings"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
)

// RecipeSnapshot is one recipe frozen at an exact revision. Every snapshot in
// an artifact is resolved before any is serialized, so concurrent edits cannot
// mix versions. The revision itself is not exported.
type RecipeSnapshot struct {
	EntryID  library.EntryID
	Revision uint64
	Recipe   recipe.Document
}

// Library is the reading half of the library an export needs.
type Library interface {
	Get(ctx context.Context, id library.EntryID) (library.Entry, error)
	GetTrashed(ctx context.Context, id library.EntryID) (library.Entry, error)
	GetRevision(ctx context.Context, id library.EntryID, revision uint64) (library.Revision, error)
	GetBook(ctx context.Context, id string) (library.Book, error)
}

// Source is where an export's recipes come from. Resolving a source is the
// moment revisions are frozen, and the order it returns is the order the
// artifact carries.
type Source interface {
	// Name suggests what the artifact should be called. It is a convenience
	// for the person receiving the file; the identity inside it is
	// authoritative.
	Name() string
	Resolve(ctx context.Context) ([]RecipeSnapshot, error)
	// Collection reports whether the source is a set of recipes rather than
	// one; a selection of one recipe is still a collection.
	Collection() bool
}

type recipeSource struct {
	library Library
	id      library.EntryID
	name    string
}

// NewRecipeSource exports the current version of one recipe, including one in
// the trash.
func NewRecipeSource(reader Library, id library.EntryID) Source {
	return &recipeSource{library: reader, id: id}
}

func (s *recipeSource) Name() string   { return s.name }
func (*recipeSource) Collection() bool { return false }

func (s *recipeSource) Resolve(ctx context.Context) ([]RecipeSnapshot, error) {
	entry, err := s.library.Get(ctx, s.id)
	if err != nil {
		trashed, trashErr := s.library.GetTrashed(ctx, s.id)
		if trashErr != nil {
			return nil, err
		}

		entry = trashed
	}

	s.name = entry.Recipe.Title

	return []RecipeSnapshot{{EntryID: entry.ID, Revision: entry.Revision, Recipe: entry.Recipe}}, nil
}

type revisionSource struct {
	library  Library
	id       library.EntryID
	revision uint64
	name     string
}

// NewRevisionSource exports a retained earlier version exactly as it read
// then, without restoring it.
func NewRevisionSource(reader Library, id library.EntryID, revision uint64) Source {
	return &revisionSource{library: reader, id: id, revision: revision}
}

func (s *revisionSource) Name() string   { return s.name }
func (*revisionSource) Collection() bool { return false }

func (s *revisionSource) Resolve(ctx context.Context) ([]RecipeSnapshot, error) {
	retained, err := s.library.GetRevision(ctx, s.id, s.revision)
	if err != nil {
		return nil, err
	}

	s.name = retained.Recipe.Title

	return []RecipeSnapshot{{
		EntryID: retained.EntryID, Revision: retained.Number, Recipe: retained.Recipe,
	}}, nil
}

type selectionSource struct {
	maximum  int
	library  Library
	entryIDs []library.EntryID
	name     string
}

// NewSelectionSource exports an ordered selection, preserving the order the
// reader selected in, resolved to complete stored recipes.
func NewSelectionSource(reader Library, ids []library.EntryID) Source {
	return &selectionSource{library: reader, entryIDs: ids, name: "recipes", maximum: MaximumSelectionRecipes}
}

// NewBoundedSelectionSource lets asynchronous export choose an explicit bounded
// selection size while synchronous callers retain their existing limit.
func NewBoundedSelectionSource(reader Library, ids []library.EntryID, maximum int) Source {
	if maximum < 1 || maximum > 10000 {
		maximum = MaximumSelectionRecipes
	}

	return &selectionSource{library: reader, entryIDs: ids, name: "recipes", maximum: maximum}
}

func (s *selectionSource) Name() string   { return s.name }
func (*selectionSource) Collection() bool { return true }

func (s *selectionSource) Resolve(ctx context.Context) ([]RecipeSnapshot, error) {
	if len(s.entryIDs) == 0 {
		return nil, ErrNoRecipes
	}
	// Validate the raw selection before deriving any slice or map capacity from
	// its length. This source is a public policy boundary too: CLI and future
	// callers must be safe even when HTTP validation is bypassed.
	if len(s.entryIDs) > s.maximum {
		return nil, fmt.Errorf("%w: a bundle accepts at most %d recipe entries", ErrTooLarge, s.maximum)
	}

	for _, id := range s.entryIDs {
		trimmed := strings.TrimSpace(string(id))
		if trimmed == "" || len(trimmed) > MaximumEntryIDLength {
			return nil, fmt.Errorf("%w: entry identifiers contain between 1 and %d characters", ErrInvalidSelection, MaximumEntryIDLength)
		}
	}

	snapshots := make([]RecipeSnapshot, 0, len(s.entryIDs))

	seen := make(map[library.EntryID]bool, len(s.entryIDs))
	for _, id := range s.entryIDs {
		id = library.EntryID(strings.TrimSpace(string(id)))
		if seen[id] {
			continue
		}

		seen[id] = true

		entry, err := s.library.Get(ctx, id)
		if err != nil {
			return nil, err
		}

		snapshots = append(snapshots, RecipeSnapshot{
			EntryID: entry.ID, Revision: entry.Revision, Recipe: entry.Recipe,
		})
	}

	return snapshots, nil
}

type bookSource struct {
	library Library
	bookID  string
	name    string
}

// NewBookSource exports the recipes a book holds, in the book's own order. The
// book itself stays in this library, since SREF has no collection model.
func NewBookSource(reader Library, bookID string) Source {
	return &bookSource{library: reader, bookID: bookID}
}

func (s *bookSource) Name() string   { return s.name }
func (*bookSource) Collection() bool { return true }

func (s *bookSource) Resolve(ctx context.Context) ([]RecipeSnapshot, error) {
	book, err := s.library.GetBook(ctx, s.bookID)
	if err != nil {
		return nil, err
	}

	s.name = strings.TrimSpace(book.Title)

	ordered := library.BookOrder(book)
	if len(ordered) == 0 {
		return nil, ErrNoRecipes
	}

	snapshots := make([]RecipeSnapshot, 0, len(ordered))
	for _, id := range ordered {
		entry, err := s.library.Get(ctx, id)
		if err != nil {
			return nil, err
		}

		snapshots = append(snapshots, RecipeSnapshot{
			EntryID: entry.ID, Revision: entry.Revision, Recipe: entry.Recipe,
		})
	}

	return snapshots, nil
}
