package library

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/savorum/savorum/internal/recipe"
)

// ErrImportTargetChanged reports that an entry changed after its import preview was reviewed.
var ErrImportTargetChanged = errors.New("recipe reviewed for import has changed")

// MaximumImportRecipes bounds one import so a single request cannot be an
// unbounded amount of work.
const MaximumImportRecipes = 500

// ErrDuplicateDocument reports an import of a portable recipe identity already held here.
var ErrDuplicateDocument = errors.New("this library already holds that recipe document")

// DuplicatePolicy decides what an import does when a recipe it carries is
// already in this library.
type DuplicatePolicy string

const (
	// DuplicateReject is the default because it is the only answer that cannot
	// lose anything: the import stops and says which recipe it already had.
	DuplicateReject DuplicatePolicy = "reject"
	// DuplicateSkip keeps what the library already holds.
	DuplicateSkip DuplicatePolicy = "skip"
	// DuplicateReplace makes the imported recipe the current one. The entry's
	// history is retained, so the version it replaced remains readable and
	// restorable.
	DuplicateReplace DuplicatePolicy = "replace"
)

func (p DuplicatePolicy) valid() bool {
	return p == DuplicateReject || p == DuplicateSkip || p == DuplicateReplace
}

// ImportOutcome records whether one imported document created, replaced, or retained an entry.
type ImportOutcome string

// Outcomes recorded for individual recipes in an import.
const (
	ImportCreated  ImportOutcome = "created"
	ImportReplaced ImportOutcome = "replaced"
	ImportSkipped  ImportOutcome = "skipped"
)

// ImportResolutionAction records a reviewer's decision about a possible duplicate.
type ImportResolutionAction string

// Decisions a reviewer can apply to an import candidate.
const (
	ImportAsSeparate ImportResolutionAction = "separate"
	ImportUpdate     ImportResolutionAction = "update"
	ImportKeep       ImportResolutionAction = "skip"
)

// ImportResolution binds a duplicate decision to the exact target entry that was reviewed.
type ImportResolution struct {
	Action         ImportResolutionAction
	TargetEntryID  EntryID
	TargetRevision uint64
	TargetRecipeID recipe.RecipeID
	MatchEvidence  ImportMatchEvidence
}

// ImportResult reports what became of one recipe in an import, including what
// had to be left behind.
type ImportResult struct {
	ImportEventID string
	RecipeID      recipe.RecipeID
	EntryID       EntryID
	Revision      uint64
	Outcome       ImportOutcome
	Title         string
	// DroppedAssets counts photographs the document named that this library
	// holds no bytes for. They are removed rather than imported as references
	// to nothing.
	DroppedAssets int
	// DeferredAssets counts photographs this import has not fetched yet. The
	// recipe keeps their addresses; this is not a failure.
	DeferredAssets int
	// Warnings are what the import survived rather than what stopped it, for
	// the reviewer to judge.
	Warnings []string
}

// DuplicateError names the recipe an import stopped on and where it already
// lives, so the answer to "which one?" does not require a second search.
type DuplicateError struct {
	RecipeID recipe.RecipeID
	EntryID  EntryID
	Title    string
}

// DuplicateErrorPhrase identifies a DuplicateError in stored error text.
const DuplicateErrorPhrase = " is already in this library as entry "

func (e *DuplicateError) Error() string {
	return fmt.Sprintf("recipe document %q"+DuplicateErrorPhrase+"%q", e.RecipeID, e.EntryID)
}

func (*DuplicateError) Unwrap() error { return ErrDuplicateDocument }

// PreflightImportIdentities resolves duplicate policy before an exchange layer
// stages package assets. The returned decisions are true for documents that
// will be created or replaced and false for documents the policy will skip.
// ImportRecipes repeats these checks inside its full plan, and the repository
// rechecks revisions while committing.
func (s *Service) PreflightImportIdentities(ctx context.Context, documents []recipe.Document, policy DuplicatePolicy) ([]bool, error) {
	if policy == "" {
		policy = DuplicateReject
	}

	if !policy.valid() {
		return nil, fmt.Errorf("%w: duplicate policy %q is not supported", ErrInvalidOrganization, policy)
	}

	decisions := make([]bool, len(documents))

	seen := make(map[recipe.RecipeID]struct{}, len(documents))
	for index, document := range documents {
		if _, repeated := seen[document.ID]; repeated {
			return nil, fmt.Errorf("%w: recipe document %q appears twice in one import", ErrInvalidOrganization, document.ID)
		}

		seen[document.ID] = struct{}{}

		entryID, _, state, err := s.repository.FindByDocumentID(ctx, document.ID)
		switch {
		case errors.Is(err, ErrNotFound):
			decisions[index] = true
			continue
		case err != nil:
			return nil, fmt.Errorf("look for an existing copy of %q: %w", document.ID, err)
		case policy == DuplicateReject:
			duplicate, lookupErr := s.duplicateError(ctx, document.ID, entryID, state)
			if lookupErr != nil {
				return nil, lookupErr
			}

			return nil, duplicate
		case policy == DuplicateSkip:
			continue
		case policy == DuplicateReplace && state == StateTrash:
			return nil, fmt.Errorf("%w: the copy of %q already here is in the trash, so restore it before replacing it", ErrInvalidState, document.ID)
		default:
			decisions[index] = true
		}
	}

	return decisions, nil
}

// ImportRecipes brings portable recipe documents into this library. Every
// document is validated before anything is written, so an import either
// describes a set of recipes this library can hold or changes nothing.
func (s *Service) ImportRecipes(ctx context.Context, documents []recipe.Document, policy DuplicatePolicy) ([]ImportResult, error) {
	return s.importRecipes(ctx, documents, policy, nil)
}

// ImportRecipesWithSources is the atomic import path for application-owned
// originals. The repository writes the source rows in the same transaction as
// each recipe revision.
func (s *Service) ImportRecipesWithSources(ctx context.Context, documents []recipe.Document, policy DuplicatePolicy, sources [][]SourceAttachment) ([]ImportResult, error) {
	return s.importRecipes(ctx, documents, policy, sources)
}

func (s *Service) importRecipes(ctx context.Context, documents []recipe.Document, policy DuplicatePolicy, sources [][]SourceAttachment) ([]ImportResult, error) {
	if sources != nil && len(sources) != len(documents) {
		return nil, fmt.Errorf("%w: source attachment groups must match imported recipes", ErrInvalidOrganization)
	}

	if policy == "" {
		policy = DuplicateReject
	}

	if !policy.valid() {
		return nil, fmt.Errorf("%w: duplicate policy %q is not supported", ErrInvalidOrganization, policy)
	}

	if len(documents) == 0 {
		return nil, fmt.Errorf("%w: an import needs at least one recipe", ErrInvalidOrganization)
	}

	if len(documents) > MaximumImportRecipes {
		return nil, fmt.Errorf("%w: an import carries at most %d recipes", ErrInvalidOrganization, MaximumImportRecipes)
	}

	if _, err := s.PreflightImportIdentities(ctx, documents, policy); err != nil {
		return nil, err
	}

	prepared, dropped, err := s.prepareImportedRecipes(ctx, documents)
	if err != nil {
		return nil, err
	}

	results, mutations, mutationIndexes, err := s.planImportedRecipes(ctx, prepared, dropped, policy, sources)
	if err != nil {
		return nil, err
	}

	for i := range mutations {
		mutations[i].History = importContextAt(ctx, mutationIndexes[i])
	}

	changed, err := s.repository.ImportMany(ctx, mutations)
	if err != nil {
		return nil, fmt.Errorf("commit recipe import: %w", err)
	}

	if len(changed) != len(mutationIndexes) {
		return nil, fmt.Errorf("commit recipe import: repository returned %d entries for %d changes", len(changed), len(mutationIndexes))
	}

	for index, entry := range changed {
		result := &results[mutationIndexes[index]]
		result.ImportEventID = ImportEventID(entry.ID, entry.Revision)
		result.EntryID, result.Revision = entry.ID, entry.Revision
	}

	return results, nil
}

func (s *Service) prepareImportedRecipes(ctx context.Context, documents []recipe.Document) ([]recipe.Document, []int, error) {
	prepared := make([]recipe.Document, len(documents))

	dropped := make([]int, len(documents))
	for index, document := range documents {
		stripped, count, err := s.withHeldAssets(ctx, recipe.ReconcileTimings(document))
		if err != nil {
			return nil, nil, err
		}

		if err := s.validate(stripped, s.units); err != nil {
			return nil, nil, err
		}

		prepared[index], dropped[index] = stripped, count
	}

	return prepared, dropped, nil
}

func (s *Service) planImportedRecipes(ctx context.Context, documents []recipe.Document, dropped []int, policy DuplicatePolicy, sources [][]SourceAttachment) ([]ImportResult, []ImportMutation, []int, error) {
	results := make([]ImportResult, len(documents))
	mutations := make([]ImportMutation, 0, len(documents))

	indexes := make([]int, 0, len(documents))
	for index, document := range documents {
		result, mutation, err := s.planImportedRecipe(ctx, document, dropped[index], policy, sourceAttachmentsAt(sources, index))
		if err != nil {
			return nil, nil, nil, err
		}

		results[index] = result

		if mutation != nil {
			mutations = append(mutations, *mutation)
			indexes = append(indexes, index)
		}
	}

	return results, mutations, indexes, nil
}

func sourceAttachmentsAt(sources [][]SourceAttachment, index int) []SourceAttachment {
	if index >= len(sources) {
		return nil
	}

	return append([]SourceAttachment(nil), sources[index]...)
}

func (s *Service) planImportedRecipe(ctx context.Context, document recipe.Document, dropped int, policy DuplicatePolicy, sources []SourceAttachment) (ImportResult, *ImportMutation, error) {
	result := ImportResult{RecipeID: document.ID, Title: document.Title, DroppedAssets: dropped}

	entryID, revision, state, err := s.repository.FindByDocumentID(ctx, document.ID)
	switch {
	case errors.Is(err, ErrNotFound):
		entryID, err = s.newID()
		if err != nil {
			return result, nil, fmt.Errorf("generate library ID: %w", err)
		}

		result.Outcome = ImportCreated

		return result, newImportMutation(entryID, 0, document, sources, s.now().UTC()), nil
	case err != nil:
		return result, nil, fmt.Errorf("look for an existing copy of %q: %w", document.ID, err)
	case policy == DuplicateReject:
		duplicate, lookupErr := s.duplicateError(ctx, document.ID, entryID, state)
		return result, nil, errors.Join(duplicate, lookupErr)
	case policy == DuplicateSkip:
		existing, getErr := s.repository.Get(ctx, entryID, state)
		if getErr != nil {
			return result, nil, fmt.Errorf("read the existing copy of %q: %w", document.ID, getErr)
		}

		result.EntryID, result.Revision, result.Outcome = entryID, revision, ImportSkipped
		result.Title, result.DroppedAssets = existing.Recipe.Title, 0

		return result, nil, nil
	case state == StateTrash:
		return result, nil, fmt.Errorf("%w: the copy of %q already here is in the trash, so restore it before replacing it", ErrInvalidState, document.ID)
	default:
		result.Outcome = ImportReplaced
		return result, newImportMutation(entryID, revision, document, sources, s.now().UTC()), nil
	}
}

func newImportMutation(id EntryID, revision uint64, document recipe.Document, sources []SourceAttachment, timestamp time.Time) *ImportMutation {
	snapshot := document
	return &ImportMutation{ID: id, ExpectedRevision: revision, Recipe: document, SourceSnapshot: &snapshot, Timestamp: timestamp, SourceAttachments: sources}
}

// ReconcileImportRecipes applies explicit preview decisions atomically. An
// update adopts the existing portable identity and revises that entry; local
// organization and cooking records remain attached because neither is part of
// the recipe-head mutation. A separate copy receives a fresh portable identity
// only when the incoming one is already held.
func (s *Service) ReconcileImportRecipes(ctx context.Context, documents []recipe.Document, resolutions []ImportResolution) ([]ImportResult, error) {
	return s.reconcileImportRecipes(ctx, documents, resolutions, nil)
}

// ReconcileImportRecipesWithSources is the atomic reconciliation path for
// retained originals.
func (s *Service) ReconcileImportRecipesWithSources(ctx context.Context, documents []recipe.Document, resolutions []ImportResolution, sources [][]SourceAttachment) ([]ImportResult, error) {
	return s.reconcileImportRecipes(ctx, documents, resolutions, sources)
}

func (s *Service) reconcileImportRecipes(ctx context.Context, documents []recipe.Document, resolutions []ImportResolution, sources [][]SourceAttachment) ([]ImportResult, error) {
	if len(documents) == 0 || len(documents) != len(resolutions) || len(documents) > MaximumImportRecipes {
		return nil, fmt.Errorf("%w: reconciliation needs one decision for each of 1 to %d recipes", ErrInvalidOrganization, MaximumImportRecipes)
	}

	if sources != nil && len(sources) != len(documents) {
		return nil, fmt.Errorf("%w: source attachment groups must match reconciled recipes", ErrInvalidOrganization)
	}

	plans, mutations, mutationIndexes, err := s.planReconciledRecipes(ctx, documents, resolutions, sources)
	if err != nil {
		return nil, err
	}

	for i := range mutations {
		mutations[i].History = importContextAt(ctx, mutationIndexes[i])
	}

	changed, err := s.repository.ImportMany(ctx, mutations)
	if err != nil {
		var precondition *PreconditionError
		if errors.Is(err, ErrConflict) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalidState) ||
			errors.Is(err, ErrDocumentIdentityChange) || errors.As(err, &precondition) {
			return nil, staleImportTarget(err)
		}

		return nil, fmt.Errorf("commit reconciled recipe import: %w", err)
	}

	if len(changed) != len(mutationIndexes) {
		return nil, fmt.Errorf("commit reconciled recipe import: repository returned %d entries for %d changes", len(changed), len(mutationIndexes))
	}

	results := make([]ImportResult, len(plans))
	for index := range plans {
		results[index] = plans[index].result
	}

	for index, entry := range changed {
		result := &results[mutationIndexes[index]]
		result.ImportEventID = ImportEventID(entry.ID, entry.Revision)
		result.EntryID, result.Revision, result.RecipeID = entry.ID, entry.Revision, entry.Recipe.ID
	}

	return results, nil
}

type reconciledImportPlan struct {
	document recipe.Document
	mutation *ImportMutation
	result   ImportResult
}

func (s *Service) planReconciledRecipes(ctx context.Context, documents []recipe.Document, resolutions []ImportResolution, sources [][]SourceAttachment) ([]reconciledImportPlan, []ImportMutation, []int, error) {
	plans := make([]reconciledImportPlan, len(documents))
	mutations := make([]ImportMutation, 0, len(documents))

	indexes := make([]int, 0, len(documents))
	for index, incoming := range documents {
		plan, err := s.planReconciledRecipe(ctx, incoming, resolutions[index], sourceAttachmentsAt(sources, index))
		if err != nil {
			return nil, nil, nil, err
		}

		plans[index] = plan
		if plan.mutation != nil {
			mutations = append(mutations, *plan.mutation)
			indexes = append(indexes, index)
		}
	}

	return plans, mutations, indexes, nil
}

func (s *Service) planReconciledRecipe(ctx context.Context, incoming recipe.Document, resolution ImportResolution, sources []SourceAttachment) (reconciledImportPlan, error) {
	var (
		plan reconciledImportPlan
		err  error
	)

	switch resolution.Action {
	case ImportKeep:
		return s.planKeptImport(ctx, incoming, resolution)
	case ImportUpdate:
		plan, err = s.planUpdatedImport(ctx, incoming, resolution, sources)
	case ImportAsSeparate:
		plan, err = s.planSeparateImport(ctx, incoming, sources)
	default:
		return plan, fmt.Errorf("%w: reconciliation action %q is not supported", ErrInvalidOrganization, resolution.Action)
	}

	if err != nil {
		return plan, err
	}

	stripped, dropped, err := s.withHeldAssets(ctx, recipe.ReconcileTimings(plan.document))
	if err != nil {
		return plan, err
	}

	if err := s.validate(stripped, s.units); err != nil {
		return plan, err
	}

	plan.document, plan.result.DroppedAssets = stripped, dropped
	plan.mutation.Recipe = stripped

	return plan, nil
}

func (s *Service) planKeptImport(ctx context.Context, incoming recipe.Document, resolution ImportResolution) (reconciledImportPlan, error) {
	entry, err := s.repository.Get(ctx, resolution.TargetEntryID, StateActive)
	if err != nil {
		return reconciledImportPlan{}, staleImportTarget(err)
	}

	if err := validateImportResolution(incoming, resolution, entry); err != nil {
		return reconciledImportPlan{}, err
	}

	result := ImportResult{RecipeID: entry.Recipe.ID, EntryID: entry.ID, Revision: entry.Revision, Outcome: ImportSkipped, Title: entry.Recipe.Title}

	return reconciledImportPlan{document: incoming, result: result}, nil
}

func (s *Service) planUpdatedImport(ctx context.Context, incoming recipe.Document, resolution ImportResolution, sources []SourceAttachment) (reconciledImportPlan, error) {
	entry, err := s.repository.Get(ctx, resolution.TargetEntryID, StateActive)
	if err != nil {
		return reconciledImportPlan{}, staleImportTarget(err)
	}

	if err := validateImportResolution(incoming, resolution, entry); err != nil {
		return reconciledImportPlan{}, err
	}

	document, snapshot, expected := incoming, incoming, entry.Recipe
	document.ID = entry.Recipe.ID
	mutation := &ImportMutation{ID: entry.ID, ExpectedRevision: resolution.TargetRevision, ExpectedRecipe: &expected, Recipe: document, SourceSnapshot: &snapshot, Timestamp: s.now().UTC(), SourceAttachments: sources}
	result := ImportResult{RecipeID: document.ID, Title: incoming.Title, Outcome: ImportReplaced}

	return reconciledImportPlan{document: document, mutation: mutation, result: result}, nil
}

func (s *Service) planSeparateImport(ctx context.Context, incoming recipe.Document, sources []SourceAttachment) (reconciledImportPlan, error) {
	document := incoming
	if _, _, _, err := s.repository.FindByDocumentID(ctx, document.ID); err == nil {
		copyID, generateErr := s.newID()
		if generateErr != nil {
			return reconciledImportPlan{}, fmt.Errorf("generate separate recipe identity: %w", generateErr)
		}

		document.ID = recipe.RecipeID(string(copyID) + "-copy")
	} else if !errors.Is(err, ErrNotFound) {
		return reconciledImportPlan{}, fmt.Errorf("check separate recipe identity: %w", err)
	}

	entryID, err := s.newID()
	if err != nil {
		return reconciledImportPlan{}, fmt.Errorf("generate separate library ID: %w", err)
	}

	snapshot := incoming
	mutation := &ImportMutation{ID: entryID, Recipe: document, SourceSnapshot: &snapshot, Timestamp: s.now().UTC(), SourceAttachments: sources}
	result := ImportResult{RecipeID: document.ID, Title: incoming.Title, Outcome: ImportCreated}

	return reconciledImportPlan{document: document, mutation: mutation, result: result}, nil
}

func validateImportResolution(incoming recipe.Document, resolution ImportResolution, entry Entry) error {
	if entry.ID != resolution.TargetEntryID || entry.Revision != resolution.TargetRevision ||
		entry.Recipe.ID != resolution.TargetRecipeID ||
		!importEvidenceStillMatches(incoming, entry.Recipe, resolution.MatchEvidence) {
		return staleImportTarget(nil)
	}

	return nil
}

func staleImportTarget(cause error) error {
	if cause == nil {
		return fmt.Errorf("%w: %w; refresh the preview", ErrImportTargetChanged, ErrConflict)
	}

	return fmt.Errorf("%w: %w; refresh the preview: %w", ErrImportTargetChanged, ErrConflict, cause)
}

func importEvidenceStillMatches(incoming, existing recipe.Document, evidence ImportMatchEvidence) bool {
	if evidence == ImportMatchSimilarity {
		score, corroborated := importSimilarity(incoming, existing)
		return corroborated && score >= 0.58
	}

	return exactImportMatch(incoming, existing, evidence)
}

func (s *Service) duplicateError(ctx context.Context, recipeID recipe.RecipeID, entryID EntryID, state State) (*DuplicateError, error) {
	existing, err := s.repository.Get(ctx, entryID, state)
	if err != nil {
		return nil, fmt.Errorf("read the existing copy of %q: %w", recipeID, err)
	}

	return &DuplicateError{RecipeID: recipeID, EntryID: entryID, Title: existing.Recipe.Title}, nil
}

// withHeldAssets removes photographs the document names that this library holds
// no bytes for, along with every reference to them.
func (s *Service) withHeldAssets(ctx context.Context, document recipe.Document) (recipe.Document, int, error) {
	if len(document.Assets) == 0 {
		return document, 0, nil
	}

	hashes := make([]string, 0, len(document.Assets))
	for _, asset := range document.Assets {
		hashes = append(hashes, asset.SHA256)
	}

	present, err := s.repository.RegisteredAssets(ctx, hashes)
	if err != nil {
		return recipe.Document{}, 0, err
	}

	kept := make([]recipe.Asset, 0, len(document.Assets))
	missing := make(map[string]struct{})

	for _, asset := range document.Assets {
		if present[asset.SHA256] {
			kept = append(kept, asset)
			continue
		}

		missing[asset.ID] = struct{}{}
	}

	if len(missing) == 0 {
		return document, 0, nil
	}

	document.Assets = kept

	recipeImages := make([]string, 0, len(document.ImageRefs))
	for _, reference := range document.ImageRefs {
		if _, gone := missing[reference]; !gone {
			recipeImages = append(recipeImages, reference)
		}
	}

	if len(recipeImages) == 0 {
		recipeImages = nil
	}

	document.ImageRefs = recipeImages
	if _, gone := missing[document.PrimaryImage]; gone {
		document.PrimaryImage = ""
	}

	sections := make([]recipe.InstructionSection, len(document.InstructionSections))
	copy(sections, document.InstructionSections)

	for sectionIndex := range sections {
		steps := make([]recipe.Step, len(sections[sectionIndex].Steps))
		copy(steps, sections[sectionIndex].Steps)

		for stepIndex := range steps {
			references := make([]string, 0, len(steps[stepIndex].ImageRefs))
			for _, reference := range steps[stepIndex].ImageRefs {
				if _, gone := missing[reference]; !gone {
					references = append(references, reference)
				}
			}

			if len(references) == 0 {
				references = nil
			}

			steps[stepIndex].ImageRefs = references
		}

		sections[sectionIndex].Steps = steps
	}

	document.InstructionSections = sections

	return document, len(missing), nil
}
