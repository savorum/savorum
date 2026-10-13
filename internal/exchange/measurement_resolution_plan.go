package exchange

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

type storedResolutionPlan struct {
	request       MeasurementResolutionPlanRequest
	plan          MeasurementResolutionPlan
	fingerprints  map[string]string
	selectedUnits map[string]string
	snapshots     map[string]candidateResolutionSnapshot
	// retainedBefore is what the session held for each ingredient this plan
	// retained an answer for, so reverting the plan withdraws the answer too.
	retainedBefore  map[string]*pendingIngredientDecisions
	applied         bool
	appliedRevision uint64
}

func buildMeasurementResolutionPlan(previewID string, session *previewSession, request MeasurementResolutionPlanRequest) (MeasurementResolutionPlan, map[string]string, map[string]string, error) {
	selected, unitsByOccurrence, fingerprints, unchanged, err := selectResolutionOccurrences(previewID, session, request)
	if err != nil {
		return MeasurementResolutionPlan{}, nil, nil, err
	}

	plan := MeasurementResolutionPlan{PreviewRevision: request.ExpectedRevision, CurrentActionableOccurrences: actionableOccurrenceCount(session), UnchangedOccurrences: unchanged}

	working, affected, selected, unchanged, err := applyResolutionOccurrences(session, request, selected, unitsByOccurrence, fingerprints, &plan)
	if err != nil {
		return MeasurementResolutionPlan{}, nil, nil, err
	}

	plan.Occurrences = append([]MeasurementResolutionOccurrence(nil), selected...)

	plan.UnchangedOccurrences = unchanged

	catalog := units.DefaultCatalog()
	for itemID, document := range working {
		if err := recipe.ValidateForWrite(document, catalog); err != nil {
			plan.ValidationFailures = append(plan.ValidationFailures, session.candidates[itemID].view.Recipe.Title+": "+err.Error())
		}
	}

	plan.AffectedOccurrences = len(selected)
	plan.AffectedIngredients = len(resolutionScopes(selected))
	plan.AffectedRecipes = len(affected)
	plan.ResultingActionableOccurrences = plan.CurrentActionableOccurrences - len(selected)
	plan.RemainingActionableOccurrences = plan.ResultingActionableOccurrences
	plan.ImpactDigest = resolutionImpactDigest(selected, unitsByOccurrence, request.Interpretation.Kind)

	return plan, unitsByOccurrence, fingerprints, nil
}

func validateResolutionRequest(request MeasurementResolutionPlanRequest) error {
	if request.ExpectedRevision == 0 {
		return fmt.Errorf("%w: expected_preview_revision is required", ErrResolutionPlanInvalid)
	}

	switch request.Interpretation.Kind {
	case ResolutionCanonicalUnit, ResolutionConvention, ResolutionKeepWritten:
		return nil
	default:
		return fmt.Errorf("%w: choose a controlled interpretation", ErrResolutionPlanInvalid)
	}
}

func selectResolutionOccurrences(previewID string, session *previewSession, request MeasurementResolutionPlanRequest) ([]MeasurementResolutionOccurrence, map[string]string, map[string]string, int, error) {
	if err := validateResolutionRequest(request); err != nil {
		return nil, nil, nil, 0, err
	}

	groups := stringSet(request.GroupIDs)
	if request.Interpretation.Kind != ResolutionConvention && len(groups) == 0 {
		return nil, nil, nil, 0, fmt.Errorf("%w: choose at least one resolution group", ErrResolutionPlanInvalid)
	}

	excludedOccurrences := stringSet(request.ExcludedOccurrenceIDs)
	excludedItems := stringSet(request.ExcludedItemIDs)
	// Ingredient scopes any exclusion touches, which a convention action must
	// then leave alone entirely.
	excludedScopes := map[string]bool{}
	catalog := units.DefaultCatalog()
	unitsByOccurrence := map[string]string{}
	fingerprints := map[string]string{}

	var selected []MeasurementResolutionOccurrence

	unchanged := 0

	for _, occurrence := range resolutionOccurrences(previewID, session) {
		if len(groups) > 0 && !groups[occurrence.GroupID] {
			continue
		}

		unitID, applicable, err := selectResolutionUnit(occurrence, request, len(groups), catalog)
		if err != nil {
			return nil, nil, nil, 0, err
		}

		if !applicable {
			continue
		}

		if excludedOccurrences[occurrence.ID] || excludedItems[occurrence.ItemID] {
			unchanged++
			excludedScopes[pendingKey(occurrence.ItemID, occurrence.IngredientID)] = true

			continue
		}

		selected = append(selected, occurrence)
		unitsByOccurrence[occurrence.ID] = unitID
		fingerprints[occurrence.ID] = occurrence.Fingerprint
	}

	// Conventions affect whole authored ingredients, so excluding any occurrence
	// excludes its ingredient. Occurrence interpretations keep per-measure scope.
	if request.Interpretation.Kind == ResolutionConvention && len(excludedScopes) != 0 {
		kept := make([]MeasurementResolutionOccurrence, 0, len(selected))

		for _, occurrence := range selected {
			if excludedScopes[pendingKey(occurrence.ItemID, occurrence.IngredientID)] {
				unchanged++

				delete(unitsByOccurrence, occurrence.ID)
				delete(fingerprints, occurrence.ID)

				continue
			}

			kept = append(kept, occurrence)
		}

		selected = kept
	}

	if len(selected) == 0 {
		if request.Interpretation.Kind == ResolutionConvention && len(excludedScopes) != 0 {
			return nil, nil, nil, 0, fmt.Errorf(
				"%w: a measuring convention is chosen for a whole ingredient, "+
					"and every ingredient in this selection has a measurement excluded",
				ErrResolutionPlanInvalid)
		}

		return nil, nil, nil, 0, fmt.Errorf("%w: no occurrences remain after exclusions", ErrResolutionPlanInvalid)
	}

	return selected, unitsByOccurrence, fingerprints, unchanged, nil
}

func selectResolutionUnit(occurrence MeasurementResolutionOccurrence, request MeasurementResolutionPlanRequest, groupCount int, catalog units.Catalog) (string, bool, error) {
	switch request.Interpretation.Kind {
	case ResolutionCanonicalUnit:
		// Every occurrence the request names has to admit the identity.
		if !slices.Contains(occurrence.CandidateUnitIDs, request.Interpretation.UnitID) {
			return "", false, fmt.Errorf("%w: the unit is not permitted for this alias", ErrResolutionPlanInvalid)
		}

		return request.Interpretation.UnitID, true, nil
	case ResolutionConvention:
		// A convention answers through the identities it selects, not through
		// the word a reader was shown. A measure whose family the convention
		// does not cover is left alone rather than handed a neighbouring
		// convention's answer (SREF section 9.5).
		profile, known := catalog.ConventionProfileFor(units.Convention(request.Interpretation.Convention))
		if !known {
			return "", false, fmt.Errorf("%w: unknown measuring convention", ErrResolutionPlanInvalid)
		}

		resolved, found := profile.Select(occurrence.CandidateUnitIDs)
		if !found {
			if groupCount > 0 {
				return "", false, fmt.Errorf("%w: the convention cannot resolve every selected alias", ErrResolutionPlanInvalid)
			}

			return "", false, nil
		}

		return resolved, true, nil
	default:
		return "", true, nil
	}
}

// applyResolutionOccurrences applies the plan one ingredient at a time.
//
// One ingredient is one decision scope (SREF section 9.5), so every occurrence
// of it that this plan selects is submitted together; a compound amount only
// resolves once all its measures are answered.
func applyResolutionOccurrences(session *previewSession, request MeasurementResolutionPlanRequest, selected []MeasurementResolutionOccurrence, unitsByOccurrence, fingerprints map[string]string, plan *MeasurementResolutionPlan) (map[string]recipe.Document, map[string]bool, []MeasurementResolutionOccurrence, int, error) {
	working := map[string]recipe.Document{}
	affected := map[string]bool{}
	resolved := make([]MeasurementResolutionOccurrence, 0, len(selected))

	var pending, unresolvable []MeasurementResolutionOccurrence

	unchanged := plan.UnchangedOccurrences

	for _, scope := range resolutionScopes(selected) {
		document, found := working[scope.itemID]
		if !found {
			var err error

			document, err = cloneDocument(session.candidates[scope.itemID].view.Recipe)
			if err != nil {
				return nil, nil, nil, 0, fmt.Errorf("prepare measurement resolution: %w", err)
			}
		}

		outcome, conflict := applyResolutionScope(&document, scope, request, unitsByOccurrence,
			session.pendingDecisions[pendingKey(scope.itemID, scope.ingredientID)])

		if outcome == resolutionPending {
			pending = append(pending, scope.occurrences...)

			continue
		}

		if outcome != resolutionApplied {
			// A contradiction refuses the plan. Anything else is a measure no
			// answer can settle; it keeps its authored wording and is reported.
			if outcome == resolutionConflict {
				plan.ValidationFailures = append(plan.ValidationFailures, scope.occurrences[0].RecipeTitle+": "+conflict)
				unchanged += len(scope.occurrences)
			} else {
				unresolvable = append(unresolvable, scope.occurrences...)
			}

			for _, occurrence := range scope.occurrences {
				delete(unitsByOccurrence, occurrence.ID)
				delete(fingerprints, occurrence.ID)
			}

			continue
		}

		resolved = append(resolved, scope.occurrences...)
		working[scope.itemID] = document
		affected[scope.itemID] = true
		appendResolutionExample(plan, scope.occurrences[0], request.Interpretation.Kind, unitsByOccurrence[scope.occurrences[0].ID])
	}

	plan.PendingOccurrences = append([]MeasurementResolutionOccurrence(nil), pending...)
	plan.UnresolvableOccurrences = append([]MeasurementResolutionOccurrence(nil), unresolvable...)

	return working, affected, resolved, unchanged, nil
}

// resolutionScope is every occurrence one plan selected inside one authored
// ingredient statement, which is the unit a decision is submitted in.
type resolutionScope struct {
	itemID       string
	ingredientID string
	// declaredLocale is what the source document stated, empty when it stated
	// none, unlike the reading locale.
	declaredLocale string
	occurrences    []MeasurementResolutionOccurrence
}

func resolutionScopes(selected []MeasurementResolutionOccurrence) []resolutionScope {
	var (
		order  []string
		scopes = map[string]*resolutionScope{}
	)

	for _, occurrence := range selected {
		key := occurrence.ItemID + "\x00" + occurrence.IngredientID

		scope, found := scopes[key]
		if !found {
			scope = &resolutionScope{
				itemID: occurrence.ItemID, ingredientID: occurrence.IngredientID,
				declaredLocale: occurrence.DeclaredLocale,
			}
			scopes[key] = scope
			order = append(order, key)
		}

		scope.occurrences = append(scope.occurrences, occurrence)
	}

	result := make([]resolutionScope, 0, len(order))
	for _, key := range order {
		result = append(result, *scopes[key])
	}

	return result
}

type resolutionApplication int

const (
	resolutionApplied resolutionApplication = iota
	resolutionIngredientChanged
	resolutionUnresolvable
	// resolutionPending is a valid answer the ingredient is not yet complete
	// enough to store. It is not a failure and must not be discarded.
	resolutionPending
	// resolutionConflict is two answers that contradict each other for one
	// ingredient. It is reported, never settled quietly.
	resolutionConflict
)

// applyResolutionScope submits one ingredient's decisions and reads the result.
// The returned detail is non-empty only for a conflict, which is the outcome a
// person has to read in their own terms.
func applyResolutionScope(document *recipe.Document, scope resolutionScope, request MeasurementResolutionPlanRequest, unitsByOccurrence map[string]string, pending *pendingIngredientDecisions) (resolutionApplication, string) {
	ingredient := findIngredient(document, scope.ingredientID)
	if ingredient == nil || ingredient.SourceText != scope.occurrences[0].SourceText {
		return resolutionIngredientChanged, ""
	}

	if request.Interpretation.Kind == ResolutionKeepWritten {
		normalization := ingredient.Normalization
		if normalization == nil {
			normalization = &recipe.Normalization{Method: recipe.NormalizationUnresolved}
		}

		normalization.Review = recipe.ReviewConfirmed
		ingredient.Normalization = normalization

		return resolutionApplied, ""
	}

	context, err := resolutionContext(request, scope, unitsByOccurrence, pending)
	if err != nil {
		return resolutionConflict, err.Error()
	}

	parsed := ingredientparser.ParseWithContext(ingredient.SourceText, ingredient.ID,
		importer.ReadingLocale(*document), context)
	if parsed.Ingredient.ID != ingredient.ID || parsed.Ingredient.SourceText != ingredient.SourceText {
		return resolutionIngredientChanged, ""
	}

	if len(parsed.Ambiguities) != 0 || parsed.Ingredient.Quantity == nil {
		if len(parsed.Ambiguities) == 0 {
			// Nothing is left to ask and the line still has no quantity, so
			// the amount itself is beyond this parser.
			return resolutionUnresolvable, ""
		}

		// Measures remain unanswered, and a quantity expression is stored
		// whole or not at all, so the answers are retained but not written.
		return resolutionPending, ""
	}

	*ingredient = parsed.Ingredient

	return resolutionApplied, ""
}

// resolutionContext turns a reviewed plan into the decisions SREF section 9.5
// defines. A canonical-unit action is an occurrence interpretation and reaches
// only the occurrences it names; a convention action is an ingredient
// convention and reaches every applicable measure of the ingredient.
func resolutionContext(request MeasurementResolutionPlanRequest, scope resolutionScope, unitsByOccurrence map[string]string, pending *pendingIngredientDecisions) (ingredientparser.ResolutionContext, error) {
	context := ingredientparser.ResolutionContext{
		Interpretations: map[string]string{},
		DeclaredLocale:  scope.declaredLocale,
	}

	// Answers already given against the current reading of this ingredient.
	if pending.current(scope.occurrences[0].SourceText) {
		maps.Copy(context.Interpretations, pending.interpretations)
		context.Convention = pending.convention
	}

	if request.Interpretation.Kind == ResolutionConvention {
		profile, known := units.DefaultCatalog().ConventionProfileFor(units.Convention(request.Interpretation.Convention))
		if !known {
			return ingredientparser.ResolutionContext{}, errors.New("that measuring convention is not one this build offers")
		}

		if context.Convention != nil && context.Convention.Convention != profile.Convention {
			// Two conventions for one ingredient scope is a conflicting
			// resolution context, and settling it by recency would discard a
			// reviewed decision without saying so.
			return ingredientparser.ResolutionContext{},
				errors.New("this ingredient was given two different measuring conventions")
		}

		context.Convention = &profile

		return context, nil
	}

	for _, occurrence := range scope.occurrences {
		unitID := unitsByOccurrence[occurrence.ID]
		if unitID == "" {
			continue
		}

		if held, taken := context.Interpretations[occurrence.OccurrenceKey]; taken && held != unitID {
			// Two answers for one measure is a conflicting resolution context.
			// Retrying the locale lookup instead would quietly discard both.
			return ingredientparser.ResolutionContext{}, errors.New("this measurement was given two different answers")
		}

		context.Interpretations[occurrence.OccurrenceKey] = unitID
	}

	return context, nil
}

func appendResolutionExample(plan *MeasurementResolutionPlan, occurrence MeasurementResolutionOccurrence, kind, unitID string) {
	if len(plan.Examples) >= 5 {
		return
	}

	after := "kept as written (reviewed)"
	if kind != ResolutionKeepWritten {
		after = "interpreted as " + resolutionUnitLabel(units.DefaultCatalog(), unitID)
	}

	plan.Examples = append(plan.Examples, MeasurementResolutionExample{RecipeTitle: occurrence.RecipeTitle, SourceText: occurrence.SourceText, Before: "kept as written", After: after})
}

func resolutionImpactDigest(selected []MeasurementResolutionOccurrence, unitsByOccurrence map[string]string, kind string) string {
	parts := make([]string, 0, len(selected))
	for _, occurrence := range selected {
		parts = append(parts, occurrence.ID+"|"+occurrence.Fingerprint+"|"+unitsByOccurrence[occurrence.ID]+"|"+kind)
	}

	sort.Strings(parts)

	return resolutionHash(parts...)
}
