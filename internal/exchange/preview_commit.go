package exchange

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
)

// CommitPreview imports only the selected candidates. Unselected candidates
// remain staged so a bulk import can commit clean items and keep problem items
// available for a later decision.
func (s *Service) CommitPreview(ctx context.Context, previewID string, itemIDs []string, policy library.DuplicatePolicy) (result ImportCommitResult, err error) {
	return s.CommitPreviewWithOptions(ctx, previewID, itemIDs, policy, PreviewCommitOptions{})
}

// CommitPreviewWithOptions commits selected candidates and, when requested,
// stores their uploaded original files alongside the resulting recipe
// revision. The original is ingested through the normal verified asset store;
// the attachment relation is what keeps it distinct from recipe images.
func (s *Service) CommitPreviewWithOptions(ctx context.Context, previewID string, itemIDs []string, policy library.DuplicatePolicy, options PreviewCommitOptions) (result ImportCommitResult, err error) {
	selections := make([]PreviewResolution, len(itemIDs))
	for index, itemID := range itemIDs {
		selections[index] = PreviewResolution{ItemID: itemID}
	}

	return s.commitPreviewWithOptions(ctx, previewID, selections, policy, false, options)
}

// ReconcilePreview commits selected duplicate resolutions from a current preview revision.
func (s *Service) ReconcilePreview(ctx context.Context, previewID string, selections []PreviewResolution) (result ImportCommitResult, err error) {
	return s.commitPreviewWithOptions(ctx, previewID, selections, library.DuplicateSkip, true, PreviewCommitOptions{})
}

// ReconcilePreviewWithOptions commits selected resolutions with source-retention options.
func (s *Service) ReconcilePreviewWithOptions(ctx context.Context, previewID string, selections []PreviewResolution, options PreviewCommitOptions) (result ImportCommitResult, err error) {
	return s.commitPreviewWithOptions(ctx, previewID, selections, library.DuplicateSkip, true, options)
}

// claimedSelection is the outcome of validating what a caller asked to import.
// The session it names is left busy, so exactly one commit or plan can own a
// selection at a time; releasing it is the caller's obligation.
type claimedSelection struct {
	session     *previewSession
	selected    []*previewCandidate
	resolutions []library.ImportResolution
	seen        map[string]bool
}

// claimPreviewSelections validates a selection against the staged preview and
// takes the session, for both synchronous commits and background jobs.
func (s *Service) claimPreviewSelections(ctx context.Context, previewID string, selections []PreviewResolution, resolved bool) (claimedSelection, error) {
	var claim claimedSelection
	if len(selections) == 0 {
		return claim, fmt.Errorf("%w: choose at least one recipe", ErrPreviewState)
	}

	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	session, ok := s.previews[previewID]
	if !ok || session.scope != s.previewScope(ctx) {
		return claim, ErrPreviewNotFound
	}

	if session.busy {
		return claim, fmt.Errorf("%w: this preview is already being committed", ErrPreviewState)
	}

	session.busy = true
	selected := make([]*previewCandidate, 0, len(selections))
	resolutions := make([]library.ImportResolution, 0, len(selections))
	seen := map[string]bool{}
	updatedTargets := map[library.EntryID]bool{}
	// reject releases the session and returns a zero claim with the error.
	reject := func(format string, arguments ...any) (claimedSelection, error) {
		session.busy = false
		return claimedSelection{}, fmt.Errorf(format, arguments...)
	}

	for _, selection := range selections {
		id := selection.ItemID

		candidate, found := session.candidates[id]
		if !found || seen[id] || !candidate.view.CanCommit {
			return reject("%w: candidate %q is not available to import", ErrPreviewState, id)
		}

		if resolved {
			resolution := library.ImportResolution{Action: selection.Action, TargetEntryID: selection.TargetEntryID}
			switch selection.Action {
			case library.ImportAsSeparate:
			case library.ImportKeep:
				match := candidateMatch(candidate, selection.TargetEntryID)
				if match == nil {
					return reject("%w: candidate %q does not match target %q", ErrPreviewState, id, selection.TargetEntryID)
				}

				resolution.TargetRevision, resolution.TargetRecipeID, resolution.MatchEvidence = match.Revision, match.RecipeID, match.Evidence
			case library.ImportUpdate:
				if candidate.view.Analysis.Status != library.ImportDefiniteMatch || len(candidate.view.Analysis.Matches) != 1 ||
					candidate.view.Analysis.Matches[0].EntryID != selection.TargetEntryID {
					return reject("%w: candidate %q has no definite match to update", ErrPreviewState, id)
				}

				match := candidate.view.Analysis.Matches[0]
				resolution.TargetRevision, resolution.TargetRecipeID, resolution.MatchEvidence = match.Revision, match.RecipeID, match.Evidence

				if updatedTargets[selection.TargetEntryID] {
					return reject("%w: target %q cannot be updated by more than one candidate", ErrPreviewState, selection.TargetEntryID)
				}

				updatedTargets[selection.TargetEntryID] = true
			default:
				return reject("%w: candidate %q needs a reconciliation choice", ErrPreviewState, id)
			}

			resolutions = append(resolutions, resolution)
		}

		seen[id] = true

		selected = append(selected, candidate)
	}

	return claimedSelection{session: session, selected: selected, resolutions: resolutions, seen: seen}, nil
}

// releasePreviewClaim gives an unfinished session back. A session that expired
// while it was busy is removed here rather than at the timer, because the timer
// could not take it away from work in progress.
func (s *Service) releasePreviewClaim(previewID string, session *previewSession) {
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	current, present := s.previews[previewID]
	if !present || current != session {
		return
	}

	if current.expired {
		s.removePreviewLocked(previewID, current)
		s.previewStats.Expired++

		return
	}

	current.busy = false
}

func (s *Service) commitPreviewWithOptions(ctx context.Context, previewID string, selections []PreviewResolution, policy library.DuplicatePolicy, resolved bool, options PreviewCommitOptions) (result ImportCommitResult, err error) {
	claim, err := s.claimPreviewSelections(ctx, previewID, selections, resolved)
	if err != nil {
		return result, err
	}

	session, selected, resolutions, seen := claim.session, claim.selected, claim.resolutions, claim.seen

	defer func() {
		if err != nil {
			s.releasePreviewClaim(previewID, session)
		}
	}()

	if err := s.validateRetainedSources(selected, options.RetainSourceItemIDs); err != nil {
		return result, err
	}

	items := make([]ImportItem, 0, len(selected))
	for _, candidate := range selected {
		item := candidate.item
		item.RetainSource = slices.Contains(options.RetainSourceItemIDs, candidate.view.ID)
		item.stagedRemote = session.stagedRemote
		items = append(items, item)
	}

	var results []library.ImportResult
	if resolved {
		results, err = s.ImportResolved(ctx, items, resolutions)
	} else {
		results, err = s.Import(ctx, items, policy)
	}

	if err != nil {
		return result, err
	}

	metadata := map[recipe.RecipeID]library.ImportedLibraryMetadata{}
	warnings := map[recipe.RecipeID][]string{}

	for index, imported := range results {
		metadata[imported.RecipeID] = toLibraryMetadata(selected[index].metadata)
		warnings[imported.RecipeID] = diagnosticMessages(selected[index].diagnostics)
	}

	results, err = s.finishExternalImport(ctx, results, metadata, warnings)
	if err != nil {
		return result, err
	}

	result.Items = results

	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	current, present := s.previews[previewID]
	if !present || current != session {
		return result, nil
	}

	for id := range seen {
		delete(current.candidates, id)
	}

	current.busy = false
	if current.expired {
		s.removePreviewLocked(previewID, current)
		s.previewStats.Expired++

		return result, nil
	}

	previousBytes := current.retainedBytes
	current.retainedBytes = retainedSessionBytes(current)
	s.previewStats.RetainedBytes += current.retainedBytes - previousBytes

	remaining := previewFromSession(previewID, current)
	if len(remaining.Items) == 0 {
		s.removePreviewLocked(previewID, current)
		s.previewStats.Completed++
	} else {
		result.Remaining = &remaining
	}

	return result, nil
}

func (s *Service) validateRetainedSources(selected []*previewCandidate, requested []string) error {
	if len(requested) == 0 {
		return nil
	}

	if _, ok := s.library.(atomicSourceLibrary); !ok {
		return fmt.Errorf("retain import source: library does not support atomic source attachments")
	}

	selectedByID := make(map[string]*previewCandidate, len(selected))
	for _, candidate := range selected {
		selectedByID[candidate.view.ID] = candidate
	}

	seen := make(map[string]struct{}, len(requested))
	for _, rawID := range requested {
		itemID := strings.TrimSpace(rawID)

		candidate, present := selectedByID[itemID]
		if itemID == "" || rawID != itemID || !present {
			return fmt.Errorf("retain import source: item %q is not selected", itemID)
		}

		if _, duplicate := seen[itemID]; duplicate {
			return fmt.Errorf("retain import source: item %q was selected twice", itemID)
		}

		seen[itemID] = struct{}{}
		if len(candidate.item.SourceBytes) == 0 || candidate.item.SourceFilename == "" {
			return fmt.Errorf("retain import source %q: no uploaded source bytes", itemID)
		}
	}

	return nil
}

func candidateMatch(candidate *previewCandidate, entryID library.EntryID) *library.ImportMatch {
	for index := range candidate.view.Analysis.Matches {
		match := &candidate.view.Analysis.Matches[index]
		if match.EntryID == entryID {
			return match
		}
	}

	return nil
}
