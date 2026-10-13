package library

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/recipe"
)

// Bounds for classifications, provenance, and book membership in one mutation.
const (
	MaximumFacetValues  = 100
	MaximumTags         = 100
	MaximumSources      = 100
	MaximumBookSections = 100
	MaximumBookEntries  = 1000

	MaximumBookMembershipChanges = 100
)

// GetOrganization returns a recipe's local classifications and user state.
func (s *Service) GetOrganization(ctx context.Context, recipeID string) (Organization, error) {
	repository := s.repository

	organization, err := repository.GetOrganization(ctx, strings.TrimSpace(recipeID), principalUserID(ctx))
	if err != nil {
		return Organization{}, fmt.Errorf("get recipe organization: %w", err)
	}

	return organization, nil
}

// ReplaceOrganization writes recipe organization and any book-membership
// changes as one operation. Either every named aggregate advances or none of
// them does, so a partly saved curation cannot be observed.
func (s *Service) ReplaceOrganization(ctx context.Context, recipeID string, expectedRevision uint64, replacement Organization, membership []BookMembershipChange) (Organization, error) {
	repository := s.repository

	if expectedRevision == 0 {
		return Organization{}, errors.New("expected organization revision is required")
	}

	replacement.EntryID = EntryID(strings.TrimSpace(recipeID))
	if err := validateOrganization(&replacement, s.newTypedID); err != nil {
		return Organization{}, err
	}

	membership, err := s.prepareBookMembership(membership)
	if err != nil {
		return Organization{}, err
	}

	result, err := repository.ReplaceOrganization(ctx, string(replacement.EntryID), principalUserID(ctx), expectedRevision, replacement, membership, s.now().UTC())
	if err != nil {
		return Organization{}, fmt.Errorf("replace recipe organization: %w", err)
	}

	return result, nil
}

func (s *Service) prepareBookMembership(changes []BookMembershipChange) ([]BookMembershipChange, error) {
	if len(changes) == 0 {
		return nil, nil
	}

	if len(changes) > MaximumBookMembershipChanges {
		return nil, invalidOrganization("too many book membership changes")
	}

	seen := make(map[string]struct{}, len(changes))

	prepared := make([]BookMembershipChange, 0, len(changes))
	for _, change := range changes {
		change.BookID = strings.TrimSpace(change.BookID)
		if change.BookID == "" {
			return nil, invalidOrganization("book membership changes require a book")
		}

		if change.Revision == 0 {
			return nil, invalidOrganization("book membership changes require the book revision they expect")
		}

		if _, duplicate := seen[change.BookID]; duplicate {
			return nil, invalidOrganization("book " + change.BookID + " is named twice in one membership change")
		}

		seen[change.BookID] = struct{}{}
		if change.Member {
			id, err := s.newTypedID("e")
			if err != nil {
				return nil, err
			}

			change.EntryID = id
		}

		prepared = append(prepared, change)
	}

	return prepared, nil
}

// CreateBook validates collection metadata and assigns its first revision and identity.
func (s *Service) CreateBook(ctx context.Context, book Book) (Book, error) {
	repository := s.repository

	id, err := s.newTypedID("b")
	if err != nil {
		return Book{}, fmt.Errorf("generate book ID: %w", err)
	}

	book.ID = id
	if err := validateBook(&book, s.newTypedID); err != nil {
		return Book{}, err
	}

	now := s.now().UTC()
	book.Revision = 1
	book.CreatedAt = now

	book.UpdatedAt = now
	if err := repository.CreateBook(ctx, book); err != nil {
		return Book{}, fmt.Errorf("create book: %w", err)
	}

	created, err := repository.GetBook(ctx, book.ID, StateActive)
	if err != nil {
		return Book{}, fmt.Errorf("read created book: %w", err)
	}

	return created, nil
}

// GetBook returns an active book with its ordered contents.
func (s *Service) GetBook(ctx context.Context, id string) (Book, error) {
	repository := s.repository

	book, err := repository.GetBook(ctx, strings.TrimSpace(id), StateActive)
	if err != nil {
		return Book{}, fmt.Errorf("get book: %w", err)
	}

	return book, nil
}

// GetTrashedBook returns a book retained in trash.
func (s *Service) GetTrashedBook(ctx context.Context, id string) (Book, error) {
	repository := s.repository

	book, err := repository.GetBook(ctx, strings.TrimSpace(id), StateTrash)
	if err != nil {
		return Book{}, fmt.Errorf("get trashed book: %w", err)
	}

	return book, nil
}

// ListBooks returns a bounded page of book summaries and the total count in that state.
func (s *Service) ListBooks(ctx context.Context, options BookListOptions) ([]BookListItem, int, error) {
	repository := s.repository

	if options.Limit <= 0 || options.Limit > 200 || options.Offset < 0 {
		return nil, 0, invalidOrganization("book list pagination is invalid")
	}

	if options.State == "" {
		options.State = StateActive
	}

	if options.State != StateActive && options.State != StateTrash {
		return nil, 0, invalidOrganization("book state must be active or trash")
	}

	books, err := repository.ListBooks(ctx, options)
	if err != nil {
		return nil, 0, fmt.Errorf("list books: %w", err)
	}

	total, err := repository.CountBooks(ctx, options.State)
	if err != nil {
		return nil, 0, fmt.Errorf("count books: %w", err)
	}

	return books, total, nil
}

// ListBookSourceRecipes returns recipes whose provenance names this publication.
func (s *Service) ListBookSourceRecipes(ctx context.Context, id string) ([]BookSourceRecipe, error) {
	repository := s.repository

	recipes, err := repository.ListBookSourceRecipes(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, fmt.Errorf("list book source recipes: %w", err)
	}

	return recipes, nil
}

// ReplaceBook validates new contents and advances the expected book revision.
func (s *Service) ReplaceBook(ctx context.Context, id string, expectedRevision uint64, book Book) (Book, error) {
	repository := s.repository
	book.ID = strings.TrimSpace(id)

	if expectedRevision == 0 {
		return Book{}, errors.New("expected book revision is required")
	}

	if err := validateBook(&book, s.newTypedID); err != nil {
		return Book{}, err
	}

	book.UpdatedAt = s.now().UTC()

	result, err := repository.ReplaceBook(ctx, expectedRevision, book)
	if err != nil {
		return Book{}, fmt.Errorf("replace book: %w", err)
	}

	return result, nil
}

// DeleteBook moves an active book to recoverable trash at its expected revision.
func (s *Service) DeleteBook(ctx context.Context, id string, expectedRevision uint64) error {
	repository := s.repository

	if expectedRevision == 0 {
		return errors.New("expected book revision is required")
	}

	if _, err := repository.ReviseBookState(ctx, strings.TrimSpace(id), expectedRevision, StateActive, StateTrash, RevisionMetadata{Cause: RevisionCauseTrash}, s.now().UTC()); err != nil {
		return fmt.Errorf("move book to trash: %w", err)
	}

	return nil
}

// RestoreBook returns a trashed book to the active library at its expected revision.
func (s *Service) RestoreBook(ctx context.Context, id string, expectedRevision uint64) (Book, error) {
	if expectedRevision == 0 {
		return Book{}, errors.New("expected book revision is required")
	}

	repository := s.repository

	return repository.ReviseBookState(ctx, strings.TrimSpace(id), expectedRevision, StateTrash, StateActive, RevisionMetadata{Cause: RevisionCauseRestore, SourceRevision: new(expectedRevision)}, s.now().UTC())
}

// GetBookRevision returns one immutable book snapshot.
func (s *Service) GetBookRevision(ctx context.Context, id string, revision uint64) (BookRevision, error) {
	if strings.TrimSpace(id) == "" || revision == 0 {
		return BookRevision{}, ErrNotFound
	}

	repository := s.repository

	result, err := repository.GetBookRevision(ctx, strings.TrimSpace(id), revision)
	if err != nil {
		return BookRevision{}, fmt.Errorf("get book revision: %w", err)
	}

	return result, nil
}

// ListBookRevisions returns a bounded page of book history summaries.
func (s *Service) ListBookRevisions(ctx context.Context, id string, options RevisionListOptions) ([]BookRevisionSummary, error) {
	if options.Limit <= 0 || options.Limit > 200 || options.Offset < 0 {
		return nil, invalidOrganization("book revision pagination is invalid")
	}

	repository := s.repository

	return repository.ListBookRevisions(ctx, strings.TrimSpace(id), options)
}

// RestoreBookRevision restores a historical snapshot as a new book revision.
func (s *Service) RestoreBookRevision(ctx context.Context, id string, sourceRevision, expectedRevision uint64) (Book, error) {
	if sourceRevision == 0 || expectedRevision == 0 {
		return Book{}, errors.New("source and expected book revisions are required")
	}

	repository := s.repository

	revision, err := repository.GetBookRevision(ctx, strings.TrimSpace(id), sourceRevision)
	if err != nil {
		return Book{}, fmt.Errorf("get book revision for restore: %w", err)
	}

	book := revision.Book
	if err := validateBook(&book, s.newTypedID); err != nil {
		return Book{}, err
	}

	return repository.RestoreBookRevision(ctx, strings.TrimSpace(id), sourceRevision, expectedRevision, RevisionMetadata{Cause: RevisionCauseRestore, SourceRevision: new(sourceRevision)}, s.now().UTC())
}

// PurgeBook permanently removes a book after checking its expected revision.
func (s *Service) PurgeBook(ctx context.Context, id string, expectedRevision uint64) error {
	if expectedRevision == 0 {
		return errors.New("expected book revision is required")
	}

	repository := s.repository
	if err := repository.PurgeBook(ctx, strings.TrimSpace(id), expectedRevision, s.now().UTC()); err != nil {
		return fmt.Errorf("purge book: %w", err)
	}

	return nil
}

// CreateSavedView validates and stores a reusable search under a new identity.
func (s *Service) CreateSavedView(ctx context.Context, view SavedView) (SavedView, error) {
	repository := s.repository

	id, err := s.newTypedID("v")
	if err != nil {
		return SavedView{}, fmt.Errorf("generate saved view ID: %w", err)
	}

	view.ID = id
	if err := validateSavedView(view); err != nil {
		return SavedView{}, err
	}

	view.Sort, _ = normalizeListSort(view.Sort)
	now := s.now().UTC()
	view.Revision = 1
	view.CreatedAt = now

	view.UpdatedAt = now
	if err := repository.CreateSavedView(ctx, view); err != nil {
		return SavedView{}, fmt.Errorf("create saved view: %w", err)
	}

	return view, nil
}

// GetSavedView returns a stored search and ordering by identity.
func (s *Service) GetSavedView(ctx context.Context, id string) (SavedView, error) {
	repository := s.repository

	view, err := repository.GetSavedView(ctx, strings.TrimSpace(id))
	if err != nil {
		return SavedView{}, fmt.Errorf("get saved view: %w", err)
	}

	return view, nil
}

// ListSavedViews returns the library's reusable searches.
func (s *Service) ListSavedViews(ctx context.Context) ([]SavedView, error) {
	repository := s.repository

	views, err := repository.ListSavedViews(ctx)
	if err != nil {
		return nil, fmt.Errorf("list saved views: %w", err)
	}

	return views, nil
}

// ReplaceSavedView validates a stored search change against its expected revision.
func (s *Service) ReplaceSavedView(ctx context.Context, id string, expectedRevision uint64, view SavedView) (SavedView, error) {
	repository := s.repository
	view.ID = strings.TrimSpace(id)

	if expectedRevision == 0 {
		return SavedView{}, errors.New("expected saved view revision is required")
	}

	if err := validateSavedView(view); err != nil {
		return SavedView{}, err
	}

	view.Sort, _ = normalizeListSort(view.Sort)
	view.UpdatedAt = s.now().UTC()

	result, err := repository.ReplaceSavedView(ctx, expectedRevision, view)
	if err != nil {
		return SavedView{}, fmt.Errorf("replace saved view: %w", err)
	}

	return result, nil
}

// DeleteSavedView removes a stored search after checking its expected revision.
func (s *Service) DeleteSavedView(ctx context.Context, id string, expectedRevision uint64) error {
	repository := s.repository

	if expectedRevision == 0 {
		return errors.New("expected saved view revision is required")
	}

	if err := repository.DeleteSavedView(ctx, strings.TrimSpace(id), expectedRevision); err != nil {
		return fmt.Errorf("delete saved view: %w", err)
	}

	return nil
}

func validateOrganization(value *Organization, newID func(string) (string, error)) error {
	if value.EntryID == "" {
		return invalidOrganization("recipe ID is required")
	}

	if len(value.FacetIDs) > MaximumFacetValues || len(value.TagIDs) > MaximumTags || len(value.Sources) > MaximumSources {
		return invalidOrganization("organization exceeds an item limit")
	}

	var valid bool

	value.FacetIDs, valid = normalizeUniqueIDs(value.FacetIDs)
	if !valid {
		return invalidOrganization("facet IDs must be non-empty and unique")
	}

	value.TagIDs, valid = normalizeUniqueIDs(value.TagIDs)
	if !valid {
		return invalidOrganization("tag IDs must be non-empty and unique")
	}

	if value.State.Rating != nil && (*value.State.Rating < 1 || *value.State.Rating > 5) {
		return invalidOrganization("rating must be between 1 and 5")
	}
	// A remembered batch is either an amount of the recipe's own yield or, when
	// that yield cannot be read as a number, a plain factor. Holding both would
	// be two answers to one question.
	if value.State.PreferredYield != nil && value.State.PreferredScale != nil {
		return invalidOrganization("a remembered batch is either a yield or a scale, not both")
	}

	if err := validatePositiveRational(value.State.PreferredYield, "remembered yield"); err != nil {
		return err
	}

	if err := validatePositiveRational(value.State.PreferredScale, "remembered batch size"); err != nil {
		return err
	}

	if value.State.PreferredYieldAssertion != nil && (*value.State.PreferredYieldAssertion < 0 || *value.State.PreferredYieldAssertion >= 100) {
		return invalidOrganization("preferred yield must name one of the recipe's yields")
	}

	if len([]rune(value.State.PersonalNote)) > 1000000 {
		return invalidOrganization("personal note is too long")
	}

	if err := validateOrganizationSources(value.Sources, newID); err != nil {
		return err
	}

	return nil
}

func validateOrganizationSources(sources []RecipeSource, newID func(string) (string, error)) error {
	for index := range sources {
		source := &sources[index]
		if source.Kind != SourceURL && source.Kind != SourcePublication && source.Kind != SourceOther {
			return invalidOrganization("source kind must be url, publication, or other")
		}

		if source.Kind == SourceURL {
			parsed, err := url.ParseRequestURI(source.URL)
			if err != nil || parsed.Scheme == "" || parsed.Host == "" {
				return invalidOrganization("URL sources require an absolute URL")
			}
		}

		if source.Kind == SourcePublication && strings.TrimSpace(source.PublicationBookID) == "" {
			return invalidOrganization("publication sources require a publication book")
		}

		if source.Kind != SourcePublication && source.PublicationBookID != "" {
			return invalidOrganization("only publication sources may reference a publication book")
		}

		if source.PageStart != nil && source.PageEnd != nil && *source.PageEnd < *source.PageStart {
			return invalidOrganization("source page end cannot precede page start")
		}

		if source.ID == "" {
			id, err := newID("s")
			if err != nil {
				return err
			}

			source.ID = id
		}
	}

	return nil
}

// validatePositiveRational rejects a stored factor that is absent from the
// canonical form or that would scale a recipe to nothing.
func validatePositiveRational(value *recipe.Rational, noun string) error {
	if value == nil {
		return nil
	}

	parsed, err := recipe.ParseRational(value.String())
	if err != nil {
		return invalidOrganization(noun + " is not an exact amount")
	}

	rat, err := parsed.Rat()
	if err != nil || rat.Sign() <= 0 {
		return invalidOrganization(noun + " must be greater than zero")
	}

	return nil
}

func normalizeUniqueIDs(values []string) ([]string, bool) {
	result := make([]string, 0, len(values))

	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, false
		}

		if _, exists := seen[value]; exists {
			return nil, false
		}

		seen[value] = struct{}{}
		result = append(result, value)
	}

	return result, true
}

func validateBook(book *Book, newID func(string) (string, error)) error {
	book.Title = strings.TrimSpace(book.Title)
	if book.ID == "" || book.Title == "" || len([]rune(book.Title)) > 1000 {
		return invalidOrganization("book ID and title are required")
	}

	if book.Kind != BookPersonal && book.Kind != BookPublication {
		return invalidOrganization("book kind must be personal or publication")
	}

	if len(book.Sections) > MaximumBookSections || len(book.Entries) > MaximumBookEntries {
		return invalidOrganization("book exceeds an item limit")
	}

	sections, err := validateBookSections(book.Sections, newID)
	if err != nil {
		return err
	}

	return validateBookEntries(book.Entries, sections, newID)
}

func validateBookSections(sections []BookSection, newID func(string) (string, error)) (map[string]struct{}, error) {
	ids := map[string]struct{}{}
	positions := map[int]struct{}{}

	for index := range sections {
		section := &sections[index]

		section.Title = strings.TrimSpace(section.Title)
		if section.Title == "" {
			return nil, invalidOrganization("book section title is required")
		}

		if section.ID == "" {
			id, err := newID("k")
			if err != nil {
				return nil, err
			}

			section.ID = id
		}

		if _, exists := ids[section.ID]; exists {
			return nil, invalidOrganization("book section IDs must be unique")
		}

		if _, exists := positions[section.Position]; exists || section.Position < 0 {
			return nil, invalidOrganization("book section positions must be unique non-negative values")
		}

		ids[section.ID] = struct{}{}
		positions[section.Position] = struct{}{}
	}

	return ids, nil
}

func validateBookEntries(entries []BookEntry, sections map[string]struct{}, newID func(string) (string, error)) error {
	positions := map[string]map[int]struct{}{}
	entryIDs := map[string]struct{}{}
	recipes := map[EntryID]struct{}{}

	for index := range entries {
		entry := &entries[index]
		if entry.ID == "" {
			id, err := newID("e")
			if err != nil {
				return err
			}

			entry.ID = id
		}

		if _, exists := entryIDs[entry.ID]; exists {
			return invalidOrganization("book entry IDs must be unique")
		}

		if strings.TrimSpace(string(entry.EntryID)) == "" {
			return invalidOrganization("book entries require a recipe")
		}

		if _, exists := recipes[entry.EntryID]; exists {
			return invalidOrganization("a recipe may appear only once in a book")
		}

		if entry.SectionID != "" {
			if _, exists := sections[entry.SectionID]; !exists {
				return invalidOrganization("book entry references an unknown section")
			}
		}

		if entry.Position < 0 {
			return invalidOrganization("book entry positions must be non-negative")
		}

		if positions[entry.SectionID] == nil {
			positions[entry.SectionID] = map[int]struct{}{}
		}

		if _, exists := positions[entry.SectionID][entry.Position]; exists {
			return invalidOrganization("book entry positions must be unique within a section")
		}

		entryIDs[entry.ID] = struct{}{}
		recipes[entry.EntryID] = struct{}{}
		positions[entry.SectionID][entry.Position] = struct{}{}
	}

	return nil
}

func validateSavedView(view SavedView) error {
	if strings.TrimSpace(view.ID) == "" || strings.TrimSpace(view.Title) == "" {
		return invalidOrganization("saved view ID and title are required")
	}

	if len([]rune(view.Title)) > 1000 || len([]rune(view.Query)) > MaximumSearchLength || len([]rune(view.Sort)) > 100 {
		return invalidOrganization("saved view field exceeds its length limit")
	}

	if _, err := ParseSearchQuery(view.Query); err != nil {
		return invalidOrganization("saved view query is invalid: " + err.Error())
	}

	if _, err := normalizeListSort(view.Sort); err != nil {
		return invalidOrganization("saved view sort is not supported")
	}

	return nil
}

func invalidOrganization(message string) error {
	return fmt.Errorf("%s: %w", message, ErrInvalidOrganization)
}

func validColor(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}

	for _, r := range value[1:] {
		if !isHexDigit(r) {
			return false
		}
	}

	return true
}

func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func (*Service) newTypedID(prefix string) (string, error) {
	id, err := randomID()
	if err != nil {
		return "", err
	}

	return prefix + strings.TrimPrefix(string(id), "r"), nil
}

var _ = time.Time{}
