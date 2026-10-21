package library

import (
	"context"
	"fmt"

	"github.com/savorum/savorum/internal/recipe"
)

// PendingPhotographs names a recipe whose import deferred photographs.
type PendingPhotographs struct {
	LibraryID string
	EntryID   EntryID
}

// PendingPhotographRepository lists recipes with deferred photographs across
// every library.
type PendingPhotographRepository interface {
	ListPendingPhotographs(ctx context.Context, after EntryID, limit int) ([]PendingPhotographs, error)
}

// PendingPhotographs lists up to limit recipes with deferred photographs after
// the given entry ID.
func (s *Service) PendingPhotographs(ctx context.Context, after EntryID, limit int) ([]PendingPhotographs, error) {
	repository, ok := s.repository.(PendingPhotographRepository)
	if !ok || limit <= 0 {
		return nil, nil
	}

	pending, err := repository.ListPendingPhotographs(ctx, after, limit)
	if err != nil {
		return nil, fmt.Errorf("list recipes with pending photographs: %w", err)
	}

	return pending, nil
}

// CompletePendingPhotographs stores photographs an import deferred, recorded as
// part of the import. Unlike Update, it writes extensions as given.
func (s *Service) CompletePendingPhotographs(ctx context.Context, id EntryID, expectedRevision uint64, document recipe.Document) (Entry, error) {
	entry, err := s.getForTransition(ctx, id, StateActive, expectedRevision)
	if err != nil {
		return Entry{}, err
	}

	if document.ID != entry.Recipe.ID {
		return Entry{}, ErrDocumentIdentityChange
	}

	if err := s.validate(document, s.units); err != nil {
		return Entry{}, err
	}

	return s.revise(ctx, RevisionCommand{
		ID:               id,
		ExpectedRevision: expectedRevision,
		Recipe:           document,
		CurrentState:     StateActive,
		TargetState:      StateActive,
		Metadata:         RevisionMetadata{Cause: RevisionCauseImport},
	})
}
