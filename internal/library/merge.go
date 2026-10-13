package library

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/recipe"
)

// MergeConflict locates different values in the two recipes being compared.
type MergeConflict struct {
	Path  string `json:"path"`
	Left  any    `json:"left,omitempty"`
	Right any    `json:"right,omitempty"`
}

// RecipeMergePreview pairs the compared entries with a suggested document and unresolved differences.
type RecipeMergePreview struct {
	Left      Entry           `json:"left"`
	Right     Entry           `json:"right"`
	Suggested recipe.Document `json:"suggested"`
	Conflicts []MergeConflict `json:"conflicts"`
}

// RecipeMergeRequest identifies the survivor, duplicate, reviewed revisions, and merged document.
type RecipeMergeRequest struct {
	Survivor                  EntryID         `json:"survivor"`
	Duplicate                 EntryID         `json:"duplicate"`
	ExpectedSurvivorRevision  uint64          `json:"expected_survivor_revision"`
	ExpectedDuplicateRevision uint64          `json:"expected_duplicate_revision"`
	Recipe                    recipe.Document `json:"recipe"`
}

// MergeMutation carries the validated changes for an atomic duplicate merge.
type MergeMutation struct {
	Recipe                    recipe.Document
	Survivor                  EntryID
	Duplicate                 EntryID
	ExpectedSurvivorRevision  uint64
	ExpectedDuplicateRevision uint64
	Timestamp                 time.Time
}

// MergeRepository commits a duplicate merge against both expected entry revisions.
type MergeRepository interface {
	MergeEntries(context.Context, MergeMutation) (Entry, error)
}

// CompareRecipes proposes a merged document and identifies conflicting values without writing.
func (s *Service) CompareRecipes(ctx context.Context, leftID, rightID EntryID) (RecipeMergePreview, error) {
	left, err := s.repository.Get(ctx, leftID, StateActive)
	if err != nil {
		return RecipeMergePreview{}, err
	}

	right, err := s.repository.Get(ctx, rightID, StateActive)
	if err != nil {
		return RecipeMergePreview{}, err
	}

	if err := validateDocumentEncoding(left.Recipe, right.Recipe); err != nil {
		return RecipeMergePreview{}, err
	}

	suggested, conflicts := twoWayMerge(left.Recipe, right.Recipe)

	return RecipeMergePreview{Left: left, Right: right, Suggested: suggested, Conflicts: conflicts}, nil
}

// MergeRecipes validates a reviewed merge and commits it to the surviving entry.
func (s *Service) MergeRecipes(ctx context.Context, request RecipeMergeRequest) (Entry, error) {
	if request.Survivor == "" || request.Duplicate == "" || request.Survivor == request.Duplicate {
		return Entry{}, fmt.Errorf("%w: two distinct recipes are required", ErrInvalidOrganization)
	}

	left, err := s.repository.Get(ctx, request.Survivor, StateActive)
	if err != nil {
		return Entry{}, err
	}

	right, err := s.repository.Get(ctx, request.Duplicate, StateActive)
	if err != nil {
		return Entry{}, err
	}

	if err := validateDocumentEncoding(left.Recipe, right.Recipe, request.Recipe); err != nil {
		return Entry{}, err
	}

	if request.ExpectedSurvivorRevision == 0 {
		request.ExpectedSurvivorRevision = left.Revision
	}

	if request.ExpectedDuplicateRevision == 0 {
		request.ExpectedDuplicateRevision = right.Revision
	}

	if left.Revision != request.ExpectedSurvivorRevision || right.Revision != request.ExpectedDuplicateRevision {
		if left.Revision != request.ExpectedSurvivorRevision {
			return Entry{}, &PreconditionError{EntryID: left.ID, Expected: request.ExpectedSurvivorRevision, Current: left.Revision}
		}

		return Entry{}, &PreconditionError{EntryID: right.ID, Expected: request.ExpectedDuplicateRevision, Current: right.Revision}
	}

	if request.Recipe.ID != left.Recipe.ID {
		return Entry{}, ErrDocumentIdentityChange
	}

	_, conflicts := twoWayMerge(left.Recipe, right.Recipe)
	for _, conflict := range conflicts {
		value := semanticFields(request.Recipe)[conflict.Path]
		if semanticAnyEqual(value, conflict.Left) || semanticAnyEqual(value, conflict.Right) {
			continue
		}

		return Entry{}, fmt.Errorf("%w: merge conflict %s requires an explicit left or right choice", ErrConflict, conflict.Path)
	}

	if err := s.validate(request.Recipe, s.units); err != nil {
		return Entry{}, err
	}

	if merger, ok := s.repository.(MergeRepository); ok {
		return merger.MergeEntries(ctx, MergeMutation{Recipe: request.Recipe, Survivor: request.Survivor, Duplicate: request.Duplicate, ExpectedSurvivorRevision: request.ExpectedSurvivorRevision, ExpectedDuplicateRevision: request.ExpectedDuplicateRevision, Timestamp: s.now().UTC()})
	}

	return Entry{}, fmt.Errorf("%w: repository does not support atomic recipe merge", ErrConflict)
}

func twoWayMerge(left, right recipe.Document) (recipe.Document, []MergeConflict) {
	merged := left
	lf, rf := semanticFields(left), semanticFields(right)
	keys := make([]string, 0, len(lf)+len(rf))
	seen := map[string]bool{}

	for k := range lf {
		if k != "id" && k != "schema_version" {
			seen[k] = true
			keys = append(keys, k)
		}
	}

	for k := range rf {
		if !seen[k] && k != "id" && k != "schema_version" {
			keys = append(keys, k)
		}
	}

	sort.Strings(keys)

	var conflicts []MergeConflict

	for _, k := range keys {
		l, r := lf[k], rf[k]
		if semanticAnyEqual(l, r) {
			continue
		}

		if isEmptyValue(l) && !isEmptyValue(r) {
			// Preserve the source side's original order; canonicalization is
			// only for comparison.
			_ = setDocumentFieldValue(&merged, k, rawDocumentFields(right)[k])

			continue
		}

		if !isEmptyValue(l) && isEmptyValue(r) {
			continue
		}

		conflicts = append(conflicts, MergeConflict{Path: k, Left: l, Right: r})
	}

	return merged, conflicts
}

func isEmptyValue(value any) bool {
	if value == nil {
		return true
	}

	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	}

	return false
}
