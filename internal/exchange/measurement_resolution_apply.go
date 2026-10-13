package exchange

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

type candidateResolutionSnapshot struct {
	document         recipe.Document
	diagnostics      []ImportDiagnostic
	analysis         library.ImportAnalysis
	importerMetadata map[string]any
}

func (s *Service) currentResolutionPlan(ctx context.Context, previewID, planID string, expectedRevision uint64) (*previewSession, *storedResolutionPlan, error) {
	session, ok := s.previews[previewID]
	if !ok || session.scope != s.previewScope(ctx) {
		return nil, nil, ErrPreviewNotFound
	}

	stored := session.resolutionPlans[planID]
	if stored == nil || stored.applied || expectedRevision != session.revision || stored.plan.PreviewRevision != session.revision {
		return nil, nil, ErrResolutionPlanStale
	}

	current := map[string]MeasurementResolutionOccurrence{}
	for _, occurrence := range resolutionOccurrences(previewID, session) {
		current[occurrence.ID] = occurrence
	}

	for id, fingerprint := range stored.fingerprints {
		if current[id].Fingerprint != fingerprint {
			return nil, nil, ErrResolutionPlanStale
		}
	}

	return session, stored, nil
}

func prepareResolutionApplication(session *previewSession, stored *storedResolutionPlan, plan MeasurementResolutionPlan, unitsByOccurrence map[string]string) (map[string]candidateResolutionSnapshot, map[string]recipe.Document, map[string]MeasurementResolutionOccurrence, map[string]map[string]any, map[string]*pendingIngredientDecisions, error) {
	snapshots := map[string]candidateResolutionSnapshot{}
	working := map[string]recipe.Document{}
	retained := map[string]*pendingIngredientDecisions{}

	selected := map[string]MeasurementResolutionOccurrence{}
	for _, occurrence := range plan.Occurrences {
		selected[occurrence.ID] = occurrence
	}

	// Applied in the same ingredient-sized scopes the preview was built from,
	// so what is written is what was reviewed.
	for _, scope := range resolutionScopes(plan.Occurrences) {
		if _, found := snapshots[scope.itemID]; !found {
			snapshot, document, err := cloneResolutionCandidate(session.candidates[scope.itemID])
			if err != nil {
				return nil, nil, nil, nil, nil, fmt.Errorf("snapshot measurement resolution candidate: %w", err)
			}

			snapshots[scope.itemID], working[scope.itemID] = snapshot, document
		}

		document := working[scope.itemID]

		outcome, _ := applyResolutionScope(&document, scope, stored.request, unitsByOccurrence,
			session.pendingDecisions[pendingKey(scope.itemID, scope.ingredientID)])
		if outcome != resolutionApplied {
			return nil, nil, nil, nil, nil, ErrResolutionPlanStale
		}

		working[scope.itemID] = document
	}

	// Answers for incomplete ingredients are retained, not stored.
	for _, scope := range resolutionScopes(plan.PendingOccurrences) {
		context, err := resolutionContext(stored.request, scope, unitsByOccurrence,
			session.pendingDecisions[pendingKey(scope.itemID, scope.ingredientID)])
		if err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("%w: %w", ErrResolutionPlanInvalid, err)
		}

		// The recipe is untouched, but its review notes are about to change,
		// so removing the decision has to be able to put them back.
		if _, found := snapshots[scope.itemID]; !found {
			snapshot, _, err := cloneResolutionCandidate(session.candidates[scope.itemID])
			if err != nil {
				return nil, nil, nil, nil, nil, fmt.Errorf("snapshot measurement resolution candidate: %w", err)
			}

			snapshots[scope.itemID] = snapshot
		}

		retained[pendingKey(scope.itemID, scope.ingredientID)] = &pendingIngredientDecisions{
			interpretations: context.Interpretations, convention: context.Convention,
			sourceText:      scope.occurrences[0].SourceText,
			parserVersion:   ingredientparser.ParserVersion,
			registryVersion: units.RegistryVersion,
		}
	}

	catalog := units.DefaultCatalog()

	metadata := make(map[string]map[string]any, len(working))
	for itemID, document := range working {
		if err := recipe.ValidateForWrite(document, catalog); err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("%w: %w", ErrResolutionPlanInvalid, err)
		}

		value, err := appendResolutionSummary(session.candidates[itemID].importerMetadata, stored.request, plan)
		if err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("record measurement resolution summary: %w", err)
		}

		metadata[itemID] = value
	}

	return snapshots, working, selected, metadata, retained, nil
}

func installResolutionCandidates(previewID string, session *previewSession, stored *storedResolutionPlan, working map[string]recipe.Document, touched map[string]candidateResolutionSnapshot, selected map[string]MeasurementResolutionOccurrence, metadata map[string]map[string]any) {
	for itemID, document := range working {
		candidate := session.candidates[itemID]
		candidate.view.Recipe = document
		candidate.item.Document = &candidate.view.Recipe
		candidate.importerMetadata = metadata[itemID]
	}

	// Every candidate this plan touched, including retained-only ones, so a
	// recipe's review flag matches what the review still asks.
	for itemID := range touched {
		candidate := session.candidates[itemID]
		candidate.view.Diagnostics = applyResolutionDiagnostics(previewID, itemID, session, candidate.view.Diagnostics, selected, stored.request.Interpretation.Kind)
		candidate.diagnostics = importer.AggregateDiagnostics(durablePreviewDiagnostics(candidate.view.Diagnostics))
		candidate.retainedBytes = retainedCandidateBytes(candidate)
	}
}

func (s *Service) analyzeResolvedCandidates(ctx context.Context, session *previewSession, working map[string]recipe.Document) error {
	analyzer, ok := s.library.(interface {
		AnalyzeImportCandidates(context.Context, []recipe.Document) ([]library.ImportAnalysis, error)
	})
	if !ok {
		return nil
	}

	ids := sortedDocumentIDs(working)

	documents := make([]recipe.Document, 0, len(ids))
	for _, id := range ids {
		documents = append(documents, working[id])
	}

	analyses, err := analyzer.AnalyzeImportCandidates(ctx, documents)
	if err != nil {
		return err
	}

	if len(analyses) != len(ids) {
		return ErrResolutionPlanInvalid
	}

	for index, id := range ids {
		session.candidates[id].view.Analysis = analyses[index]
	}

	return nil
}

func (s *Service) finishResolutionApplication(previewID, planID string, session *previewSession, stored *storedResolutionPlan, plan MeasurementResolutionPlan, snapshots map[string]candidateResolutionSnapshot) (MeasurementResolutionPlan, error) {
	previousPlans := make(map[string]*storedResolutionPlan, len(session.resolutionPlans))
	maps.Copy(previousPlans, session.resolutionPlans)

	stored.snapshots, stored.applied = snapshots, true
	session.revision++

	stored.appliedRevision = session.revision
	for id := range session.resolutionPlans {
		if id != planID {
			delete(session.resolutionPlans, id)
		}
	}

	previousBytes := session.retainedBytes

	session.retainedBytes = retainedSessionBytes(session)
	if session.retainedBytes > s.previewLimits.RetainedBytesPerSession || exceedsInt64(s.previewStats.RetainedBytes-previousBytes, session.retainedBytes, s.previewLimits.RetainedBytes) {
		restoreResolutionCandidates(session, snapshots)
		restorePendingDecisions(session, stored.retainedBefore)
		stored.snapshots, stored.retainedBefore, stored.applied, stored.appliedRevision = nil, nil, false, 0
		session.revision--
		session.resolutionPlans, session.retainedBytes = previousPlans, previousBytes

		return MeasurementResolutionPlan{}, ErrPreviewTooLarge
	}

	s.previewStats.RetainedBytes += session.retainedBytes - previousBytes
	plan.ID, plan.PreviewRevision = planID, session.revision
	preview := previewFromSession(previewID, session)
	plan.Preview = &preview
	stored.plan = plan
	stored.plan.Preview = nil

	return plan, nil
}

func applyResolutionDiagnostics(previewID, itemID string, session *previewSession, diagnostics []ImportDiagnostic, selected map[string]MeasurementResolutionOccurrence, kind string) []ImportDiagnostic {
	var (
		result    []ImportDiagnostic
		confirmed []importer.DiagnosticTarget
	)

	for _, diagnostic := range diagnostics {
		if diagnostic.Class != importer.DiagnosticActionable {
			result = append(result, diagnostic)
			continue
		}

		remaining := diagnostic.Targets[:0]
		for _, target := range diagnostic.Targets {
			if target.Resolution == nil {
				remaining = append(remaining, target)
				continue
			}

			id := resolutionHash("occurrence", previewID, itemID, target.Resolution.ID)
			if _, found := selected[id]; found {
				if kind == ResolutionKeepWritten {
					confirmed = append(confirmed, target)
				}

				continue
			}

			// A measure a retained answer already settles is not review work.
			if !settledByPending(session, itemID, target, target.Resolution) {
				remaining = append(remaining, target)
			}
		}

		if len(remaining) > 0 {
			diagnostic.Targets = append([]importer.DiagnosticTarget(nil), remaining...)

			diagnostic.Count = len(remaining)
			if diagnostic.Code == "ingredient-review-required" {
				diagnostic.Message = fmt.Sprintf("Review %d ambiguous ingredient %s.", len(remaining), pluralResolution(len(remaining), "amount", "amounts"))
			}

			result = append(result, diagnostic)
		}
	}

	if len(confirmed) > 0 {
		result = append(result, ImportDiagnostic{
			Code: "ingredient-review-confirmed", Class: importer.DiagnosticInformational, Severity: SeverityInfo,
			Area: "ingredients", Count: len(confirmed), Targets: confirmed,
			Message: fmt.Sprintf("%d ambiguous ingredient %s reviewed and kept as written.", len(confirmed), pluralResolution(len(confirmed), "was", "were")),
		})
	}

	return result
}

func appendResolutionSummary(metadata map[string]any, request MeasurementResolutionPlanRequest, plan MeasurementResolutionPlan) (map[string]any, error) {
	result, err := cloneMetadata(metadata)
	if err != nil {
		return nil, err
	}

	if result == nil {
		result = map[string]any{}
	}

	affected := make([]string, 0, len(plan.Occurrences))
	for _, occurrence := range plan.Occurrences {
		affected = append(affected, occurrence.ID)
	}

	sort.Strings(affected)

	scopes := resolutionScopes(plan.Occurrences)
	ingredientIDs := make([]string, 0, len(scopes))

	for _, scope := range scopes {
		ingredientIDs = append(ingredientIDs, scope.ingredientID)
	}

	sort.Strings(ingredientIDs)

	occurrenceKeys := make([]string, 0, len(plan.Occurrences))
	for _, occurrence := range plan.Occurrences {
		occurrenceKeys = append(occurrenceKeys, occurrence.OccurrenceKey)
	}

	sort.Strings(occurrenceKeys)
	occurrenceKeys = slices.Compact(occurrenceKeys)

	summary := MeasurementResolutionRecord{
		ResolutionKind: ingredientparser.AmbiguityUnitAlias, InterpretationKind: request.Interpretation.Kind,
		DecisionKind: decisionKind(request.Interpretation.Kind),
		UnitID:       request.Interpretation.UnitID, Convention: request.Interpretation.Convention,
		ConventionUnitIDs:  conventionSelections(request.Interpretation),
		ScopeIngredientIDs: ingredientIDs, ScopeOccurrenceKeys: occurrenceKeys,
		ParserVersion: ingredientparser.ParserVersion, UnitRegistryVersion: units.RegistryVersion,
		AffectedOccurrenceIDs: affected, ExcludedOccurrenceIDs: append([]string(nil), request.ExcludedOccurrenceIDs...),
		ExcludedItemIDs: append([]string(nil), request.ExcludedItemIDs...), AffectedRecipeCount: plan.AffectedRecipes,
		AffectedIngredientCount: plan.AffectedIngredients,
		OriginalActionableCount: plan.CurrentActionableOccurrences, ResultingActionableCount: plan.ResultingActionableOccurrences,
		ImpactDigest: plan.ImpactDigest,
	}
	values, _ := result["measurement_resolution_plans"].([]any)
	result["measurement_resolution_plans"] = append(values, summary)

	return result, nil
}

func cloneResolutionCandidate(candidate *previewCandidate) (candidateResolutionSnapshot, recipe.Document, error) {
	document, err := cloneDocument(candidate.view.Recipe)
	if err != nil {
		return candidateResolutionSnapshot{}, recipe.Document{}, err
	}

	working, err := cloneDocument(candidate.view.Recipe)
	if err != nil {
		return candidateResolutionSnapshot{}, recipe.Document{}, err
	}

	diagnostics, err := cloneDiagnostics(candidate.view.Diagnostics)
	if err != nil {
		return candidateResolutionSnapshot{}, recipe.Document{}, err
	}

	metadata, err := cloneMetadata(candidate.importerMetadata)
	if err != nil {
		return candidateResolutionSnapshot{}, recipe.Document{}, err
	}

	return candidateResolutionSnapshot{
		document: document, diagnostics: diagnostics,
		analysis: candidate.view.Analysis, importerMetadata: metadata,
	}, working, nil
}

func restoreCandidate(candidate *previewCandidate, snapshot candidateResolutionSnapshot) {
	// Each snapshot is an independent deep copy and is discarded immediately
	// after restoration, so transferring its values cannot alias live state.
	candidate.view.Recipe = snapshot.document
	candidate.item.Document = &candidate.view.Recipe
	candidate.view.Diagnostics = snapshot.diagnostics
	candidate.view.Analysis = snapshot.analysis
	candidate.importerMetadata = snapshot.importerMetadata
	candidate.diagnostics = importer.AggregateDiagnostics(durablePreviewDiagnostics(candidate.view.Diagnostics))
	candidate.retainedBytes = retainedCandidateBytes(candidate)
}

// restorePendingDecisions puts back what the session held about each ingredient
// before a plan retained an answer for it, removing the key outright where it
// held nothing.
func restorePendingDecisions(session *previewSession, before map[string]*pendingIngredientDecisions) {
	for key, previous := range before {
		if previous == nil {
			delete(session.pendingDecisions, key)
			continue
		}

		session.pendingDecisions[key] = previous
	}
}

func restoreResolutionCandidates(session *previewSession, snapshots map[string]candidateResolutionSnapshot) {
	for itemID, snapshot := range snapshots {
		restoreCandidate(session.candidates[itemID], snapshot)
	}
}
