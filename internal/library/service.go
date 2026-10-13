package library

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/recipe"
)

// Bounds for collection reads, search text, and revision history pages.
const (
	DefaultListLimit    = 50
	MaximumListLimit    = 200
	MaximumSearchLength = 200
	DefaultHistoryLimit = 50
	MaximumHistoryLimit = 200
)

type (
	// Validator checks whether a recipe is writable with the supplied unit catalog.
	Validator func(recipe.Document, recipe.UnitCatalog) error
	// IDGenerator supplies a new local library-entry identity.
	IDGenerator func() (EntryID, error)
	// Clock supplies timestamps for library changes.
	Clock func() time.Time
)

// Service validates recipe mutations and coordinates their persistence and history.
type Service struct {
	repository Persistence
	units      recipe.UnitCatalog
	validate   Validator
	newID      IDGenerator
	now        Clock
}

// Option customizes a library service at construction time.
type Option func(*Service)

// WithIDGenerator supplies the generator used for new local identities.
func WithIDGenerator(generator IDGenerator) Option {
	return func(service *Service) {
		service.newID = generator
	}
}

// WithClock supplies the clock used for library-change timestamps.
func WithClock(clock Clock) Option {
	return func(service *Service) {
		service.now = clock
	}
}

// NewService constructs a validated library service over persistence and a unit catalog.
func NewService(repository Persistence, units recipe.UnitCatalog, options ...Option) (*Service, error) {
	if repository == nil {
		return nil, errors.New("library repository is required")
	}

	if units == nil {
		return nil, errors.New("unit catalog is required")
	}

	service := &Service{
		repository: repository,
		units:      units,
		validate:   recipe.ValidateForWrite,
		newID:      randomID,
		now:        time.Now,
	}
	for _, option := range options {
		option(service)
	}

	if service.newID == nil {
		return nil, errors.New("ID generator is required")
	}

	if service.now == nil {
		return nil, errors.New("clock is required")
	}

	return service, nil
}

// Create validates a recipe and stores its first revision under a new local identity.
func (s *Service) Create(ctx context.Context, document recipe.Document) (Entry, error) {
	document = recipe.ReconcileTimings(document)
	if err := s.validate(document, s.units); err != nil {
		return Entry{}, err
	}

	id, err := s.newID()
	if err != nil {
		return Entry{}, fmt.Errorf("generate library ID: %w", err)
	}

	timestamp := s.now().UTC()

	entry := Entry{
		ID:        id,
		Recipe:    document,
		Revision:  1,
		CreatedAt: timestamp,
		UpdatedAt: timestamp,
	}
	if err := s.repository.Create(ctx, entry, RevisionMetadata{Cause: RevisionCauseAPI}); err != nil {
		return Entry{}, fmt.Errorf("create library entry: %w", err)
	}

	return entry, nil
}

// Get returns an active library entry and its current recipe.
func (s *Service) Get(ctx context.Context, id EntryID) (Entry, error) {
	if strings.TrimSpace(string(id)) == "" {
		return Entry{}, ErrNotFound
	}

	entry, err := s.repository.Get(ctx, id, StateActive)
	if err != nil {
		return Entry{}, fmt.Errorf("get library entry: %w", err)
	}

	return entry, nil
}

// List returns a bounded page of matching summaries.
func (s *Service) List(ctx context.Context, options ListOptions) ([]Summary, error) {
	options, err := normalizeListOptions(options)
	if err != nil {
		return nil, err
	}

	entries, err := s.repository.List(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("list library entries: %w", err)
	}

	return entries, nil
}

// ListPage returns a bounded page of matching summaries together with the
// exact total match count.
func (s *Service) ListPage(ctx context.Context, options ListOptions) ([]Summary, int, error) {
	options, err := normalizeListOptions(options)
	if err != nil {
		return nil, 0, err
	}

	entries, total, err := s.repository.ListPage(ctx, options)
	if err != nil {
		return nil, 0, fmt.Errorf("list library page: %w", err)
	}

	return entries, total, nil
}

// ListReferences names every recipe in one lifecycle state for pickers that
// do not need summaries.
func (s *Service) ListReferences(ctx context.Context, state State) ([]RecipeReference, error) {
	if state == "" {
		state = StateActive
	}

	if state != StateActive && state != StateTrash {
		return nil, errors.New("library state must be active or trash")
	}

	references, err := s.repository.ListReferences(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("list recipe references: %w", err)
	}

	return references, nil
}

func normalizeListOptions(options ListOptions) (ListOptions, error) {
	if options.Limit == 0 {
		options.Limit = DefaultListLimit
	}

	if options.Limit < 1 || options.Limit > MaximumListLimit {
		return options, fmt.Errorf("limit must be between 1 and %d", MaximumListLimit)
	}

	if options.Offset < 0 {
		return options, errors.New("offset cannot be negative")
	}

	if options.State == "" {
		options.State = StateActive
	}

	if options.State != StateActive && options.State != StateTrash {
		return options, errors.New("library state must be active or trash")
	}

	var err error

	options.Sort, err = normalizeListSort(options.Sort)
	if err != nil {
		return options, err
	}

	options.Query, err = normalizeSearchQuery(options.Query)
	if err != nil {
		return options, err
	}

	options.Search, err = ParseSearchQuery(options.Query)
	if err != nil {
		return options, err
	}

	return options, nil
}

func normalizeListSort(value ListSort) (ListSort, error) {
	value = ListSort(strings.TrimSpace(string(value)))
	if value == "" {
		return ListSortRelevance, nil
	}

	switch value {
	case ListSortRelevance, ListSortUpdatedDesc, ListSortTitleAsc, ListSortRatingDesc:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported recipe list sort %q: %w", value, ErrInvalidSearch)
	}
}

// Count returns the number of entries matching a search and lifecycle state.
func (s *Service) Count(ctx context.Context, options ListOptions) (int, error) {
	normalized, err := normalizeSearchQuery(options.Query)
	if err != nil {
		return 0, err
	}

	options.Query = normalized

	options.Search, err = ParseSearchQuery(options.Query)
	if err != nil {
		return 0, err
	}

	if options.State == "" {
		options.State = StateActive
	}

	if options.State != StateActive && options.State != StateTrash {
		return 0, errors.New("library state must be active or trash")
	}

	total, err := s.repository.Count(ctx, options)
	if err != nil {
		return 0, fmt.Errorf("count library entries: %w", err)
	}

	return total, nil
}

// Update validates a replacement recipe and advances the expected entry revision.
func (s *Service) Update(ctx context.Context, id EntryID, expectedRevision uint64, document recipe.Document) (Entry, error) {
	if strings.TrimSpace(string(id)) == "" {
		return Entry{}, ErrNotFound
	}

	if expectedRevision == 0 {
		return Entry{}, errors.New("expected revision is required")
	}

	entry, err := s.repository.Get(ctx, EntryID(strings.TrimSpace(string(id))), StateActive)
	if err != nil {
		return Entry{}, fmt.Errorf("get library entry for update: %w", err)
	}

	if entry.Revision != expectedRevision {
		return Entry{}, &PreconditionError{EntryID: entry.ID, Expected: expectedRevision, Current: entry.Revision}
	}

	if document.ID != entry.Recipe.ID {
		return Entry{}, ErrDocumentIdentityChange
	}
	// Opaque imported data is not an editable API surface. Older clients may
	// omit these sidecars entirely, and newer clients must not be able to
	// mutate semantics this build does not understand, so ordinary edits always
	// carry forward the stored values.
	if len(entry.Recipe.Extensions) > 0 {
		document.Extensions = entry.Recipe.Extensions
	}

	if len(entry.Recipe.SREFOpaque) > 0 {
		document.SREFOpaque = entry.Recipe.SREFOpaque
	}

	document = recipe.ReconcileIngredientDiagnostics(entry.Recipe, document)

	document = recipe.ReconcileTimings(document)
	if err := s.validate(document, s.units); err != nil {
		return Entry{}, err
	}

	return s.revise(ctx, RevisionCommand{
		ID:               entry.ID,
		ExpectedRevision: expectedRevision,
		Recipe:           document,
		CurrentState:     StateActive,
		TargetState:      StateActive,
		Metadata:         RevisionMetadata{Cause: RevisionCauseManualEdit},
	})
}

// ReanalyzeMicrowave runs the same timing reconciliation used by create,
// edit, and import against a revision the caller has seen. A no-op returns the
// current entry without creating history.
func (s *Service) ReanalyzeMicrowave(ctx context.Context, id EntryID, expectedRevision uint64) (Entry, error) {
	entry, err := s.getForTransition(ctx, id, StateActive, expectedRevision)
	if err != nil {
		return Entry{}, err
	}

	document := recipe.ReconcileTimings(entry.Recipe)
	if reflect.DeepEqual(document, entry.Recipe) {
		return entry, nil
	}

	if err := s.validate(document, s.units); err != nil {
		return Entry{}, err
	}

	// Recorded as a manual edit; see the RevisionCause constants.
	return s.revise(ctx, RevisionCommand{ID: id, ExpectedRevision: expectedRevision, Recipe: document, CurrentState: StateActive, TargetState: StateActive, Metadata: RevisionMetadata{Cause: RevisionCauseManualEdit}})
}

// Trash moves an active entry to recoverable trash at its expected revision.
func (s *Service) Trash(ctx context.Context, id EntryID, expectedRevision uint64) (Entry, error) {
	entry, err := s.getForTransition(ctx, id, StateActive, expectedRevision)
	if err != nil {
		return Entry{}, fmt.Errorf("get library entry for trash: %w", err)
	}

	return s.revise(ctx, RevisionCommand{
		ID:               id,
		ExpectedRevision: expectedRevision,
		Recipe:           entry.Recipe,
		CurrentState:     StateActive,
		TargetState:      StateTrash,
		Metadata:         RevisionMetadata{Cause: RevisionCauseTrash},
	})
}

// GetTrashed returns an entry retained in trash.
func (s *Service) GetTrashed(ctx context.Context, id EntryID) (Entry, error) {
	if strings.TrimSpace(string(id)) == "" {
		return Entry{}, ErrNotFound
	}

	entry, err := s.repository.Get(ctx, id, StateTrash)
	if err != nil {
		return Entry{}, fmt.Errorf("get trashed library entry: %w", err)
	}

	return entry, nil
}

// Restore validates a trashed recipe and returns it to the active library.
func (s *Service) Restore(ctx context.Context, id EntryID, expectedRevision uint64) (Entry, error) {
	entry, err := s.getForTransition(ctx, id, StateTrash, expectedRevision)
	if err != nil {
		return Entry{}, fmt.Errorf("get library entry for restore: %w", err)
	}

	if err := s.validate(entry.Recipe, s.units); err != nil {
		return Entry{}, err
	}

	return s.revise(ctx, RevisionCommand{
		ID:               id,
		ExpectedRevision: expectedRevision,
		Recipe:           entry.Recipe,
		CurrentState:     StateTrash,
		TargetState:      StateActive,
		Metadata: RevisionMetadata{
			Cause:          RevisionCauseRestore,
			SourceRevision: new(entry.Revision),
		},
	})
}

func (s *Service) getForTransition(ctx context.Context, id EntryID, expectedState State, expectedRevision uint64) (Entry, error) {
	entry, err := s.repository.Get(ctx, id, expectedState)
	if err == nil {
		return entry, nil
	}

	if !errors.Is(err, ErrNotFound) {
		return Entry{}, err
	}

	otherState := StateActive
	if expectedState == StateActive {
		otherState = StateTrash
	}

	entry, otherErr := s.repository.Get(ctx, id, otherState)
	if errors.Is(otherErr, ErrNotFound) {
		return Entry{}, ErrNotFound
	}

	if otherErr != nil {
		return Entry{}, otherErr
	}

	if entry.Revision != expectedRevision {
		return Entry{}, &PreconditionError{EntryID: id, Expected: expectedRevision, Current: entry.Revision}
	}

	return Entry{}, ErrInvalidState
}

// RestoreRevision validates a historical snapshot and makes it a new active revision.
func (s *Service) RestoreRevision(ctx context.Context, id EntryID, sourceRevision, expectedRevision uint64) (Entry, error) {
	revision, err := s.GetRevision(ctx, id, sourceRevision)
	if err != nil {
		return Entry{}, err
	}

	if err := s.validate(revision.Recipe, s.units); err != nil {
		return Entry{}, err
	}

	currentState := StateActive

	currentEntry, err := s.repository.Get(ctx, id, StateActive)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return Entry{}, fmt.Errorf("get current recipe state for revision restore: %w", err)
		}

		currentEntry, err = s.repository.Get(ctx, id, StateTrash)
		if err != nil {
			return Entry{}, fmt.Errorf("get trashed recipe state for revision restore: %w", err)
		}

		currentState = StateTrash
	}

	if currentEntry.Revision != expectedRevision {
		return Entry{}, &PreconditionError{EntryID: id, Expected: expectedRevision, Current: currentEntry.Revision}
	}

	if revision.Recipe.ID != currentEntry.Recipe.ID {
		return Entry{}, ErrDocumentIdentityChange
	}

	return s.revise(ctx, RevisionCommand{
		ID:               id,
		ExpectedRevision: expectedRevision,
		Recipe:           revision.Recipe,
		CurrentState:     currentState,
		TargetState:      StateActive,
		Metadata: RevisionMetadata{
			Cause:          RevisionCauseRestore,
			SourceRevision: new(sourceRevision),
		},
	})
}

// GetRevision returns one immutable recipe snapshot by revision number.
func (s *Service) GetRevision(ctx context.Context, id EntryID, number uint64) (Revision, error) {
	if strings.TrimSpace(string(id)) == "" || number == 0 {
		return Revision{}, ErrNotFound
	}

	revision, err := s.repository.GetRevision(ctx, id, number)
	if err != nil {
		return Revision{}, fmt.Errorf("get recipe revision: %w", err)
	}

	return revision, nil
}

// ListRevisions returns a bounded page of an entry's history summaries.
func (s *Service) ListRevisions(ctx context.Context, id EntryID, options RevisionListOptions) ([]RevisionSummary, error) {
	if strings.TrimSpace(string(id)) == "" {
		return nil, ErrNotFound
	}

	if options.Limit == 0 {
		options.Limit = DefaultHistoryLimit
	}

	if options.Limit < 1 || options.Limit > MaximumHistoryLimit {
		return nil, fmt.Errorf("history limit must be between 1 and %d", MaximumHistoryLimit)
	}

	if options.Offset < 0 {
		return nil, errors.New("history offset cannot be negative")
	}

	revisions, err := s.repository.ListRevisions(ctx, id, options)
	if err != nil {
		return nil, fmt.Errorf("list recipe revisions: %w", err)
	}

	if err := s.describeRevisionChanges(ctx, id, options, revisions); err != nil {
		return nil, err
	}

	return revisions, nil
}

// Purge permanently removes an entry after checking its expected revision.
func (s *Service) Purge(ctx context.Context, id EntryID, expectedRevision uint64) error {
	if strings.TrimSpace(string(id)) == "" {
		return ErrNotFound
	}

	if expectedRevision == 0 {
		return errors.New("expected recipe revision is required")
	}

	if err := s.repository.Purge(ctx, id, expectedRevision, s.now().UTC()); err != nil {
		return fmt.Errorf("purge library entry: %w", err)
	}

	return nil
}

// EmptyTrash permanently removes reviewed trash in one operation. Recipes and
// books are named the same way, with the exact revision each was reviewed at,
// and the whole batch fails if any of them no longer matches.
func (s *Service) EmptyTrash(ctx context.Context, recipes []PurgeTarget, books []BookPurgeTarget) error {
	if len(recipes) == 0 && len(books) == 0 {
		return fmt.Errorf("%w: at least one trash entry is required", ErrInvalidOrganization)
	}

	seen := make(map[EntryID]struct{}, len(recipes))
	for _, target := range recipes {
		if strings.TrimSpace(string(target.ID)) == "" {
			return ErrNotFound
		}

		if target.Revision == 0 {
			return fmt.Errorf("%w: every trash entry requires a revision", ErrInvalidOrganization)
		}

		if _, duplicate := seen[target.ID]; duplicate {
			return fmt.Errorf("%w: trash entry %q is duplicated", ErrInvalidOrganization, target.ID)
		}

		seen[target.ID] = struct{}{}
	}

	seenBooks := make(map[string]struct{}, len(books))
	for _, target := range books {
		if strings.TrimSpace(target.ID) == "" {
			return ErrNotFound
		}

		if target.Revision == 0 {
			return fmt.Errorf("%w: every trash entry requires a revision", ErrInvalidOrganization)
		}

		if _, duplicate := seenBooks[target.ID]; duplicate {
			return fmt.Errorf("%w: trash book %q is duplicated", ErrInvalidOrganization, target.ID)
		}

		seenBooks[target.ID] = struct{}{}
	}

	if err := s.repository.PurgeMany(ctx, recipes, books, s.now().UTC()); err != nil {
		return fmt.Errorf("empty trash: %w", err)
	}

	return nil
}

func (s *Service) revise(ctx context.Context, command RevisionCommand) (Entry, error) {
	if strings.TrimSpace(string(command.ID)) == "" {
		return Entry{}, ErrNotFound
	}

	if command.ExpectedRevision == 0 {
		return Entry{}, errors.New("expected revision is required")
	}

	command.Timestamp = s.now().UTC()

	entry, err := s.repository.Revise(ctx, command)
	if err != nil {
		return Entry{}, fmt.Errorf("revise library entry: %w", err)
	}

	return entry, nil
}

func normalizeSearchQuery(query string) (string, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) > MaximumSearchLength {
		return "", fmt.Errorf("search query cannot exceed %d characters", MaximumSearchLength)
	}

	return query, nil
}

func randomID() (EntryID, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("read ID entropy: %w", err)
	}

	return EntryID("r" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(entropy[:]))), nil
}
