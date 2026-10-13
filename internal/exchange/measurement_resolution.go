package exchange

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
)

var (
	// ErrResolutionPlanInvalid reports measurement resolution plan is invalid.
	ErrResolutionPlanInvalid = errors.New("measurement resolution plan is invalid")
	// ErrResolutionPlanStale reports measurement resolution plan is stale.
	ErrResolutionPlanStale = errors.New("measurement resolution plan is stale")
)

// MeasurementConventionProfile describes a convention's choices and unanswered
// measure families in the named registry. A convention can resolve only part of
// a preview; its label alone does not describe that coverage.
type MeasurementConventionProfile struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Selects lists the exact identities this convention chooses, in family
	// order, against the registry named by UnitRegistryVersion.
	Selects []string `json:"selects"`
	// Unanswered lists the measure families this convention does not answer
	// for, so the limitation is stated rather than discovered.
	Unanswered          []string `json:"unanswered"`
	UnitRegistryVersion string   `json:"unit_registry_version"`
}

// MeasurementResolutionChoice is a selectable interpretation of an ambiguous measurement.
type MeasurementResolutionChoice struct {
	UnitID     string `json:"unit_id"`
	Label      string `json:"label"`
	Convention string `json:"convention"`
	// ConventionLabel names only the measuring convention, for a choice shown
	// beside a measure that has already been named.
	ConventionLabel string `json:"convention_label"`
	// Magnitude is how much this identity holds, from the registry, written the
	// way a cook would read it.
	Magnitude string `json:"magnitude"`
}

// MeasurementResolutionSubgroup groups related measurement choices.
type MeasurementResolutionSubgroup struct {
	SourceText  string `json:"source_text"`
	Occurrences int    `json:"occurrences"`
	Recipes     int    `json:"recipes"`
}

// MeasurementResolutionExample provides an authored measurement example.
type MeasurementResolutionExample struct {
	RecipeTitle string `json:"recipe_title"`
	SourceText  string `json:"source_text"`
	Before      string `json:"before"`
	After       string `json:"after"`
}

// MeasurementResolutionGroup contains related ambiguous measurements.
type MeasurementResolutionGroup struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	AuthoredForm string `json:"authored_form"`
	Occurrences  int    `json:"occurrences"`
	// Ingredients counts the authored ingredient statements these occurrences
	// belong to. An ingredient may carry several of them, so it is not the
	// occurrence count and cannot be derived from it.
	Ingredients int `json:"ingredients"`
	Recipes     int `json:"recipes"`
	// SourceLabel names the upload or address these occurrences came from. A
	// group never spans two sources, and a reader deciding for one of them is
	// entitled to see which.
	SourceLabel      string                          `json:"source_label"`
	CandidateUnitIDs []string                        `json:"candidate_unit_i_ds"`
	Choices          []MeasurementResolutionChoice   `json:"choices"`
	Subgroups        []MeasurementResolutionSubgroup `json:"subgroups"`
	Examples         []MeasurementResolutionExample  `json:"examples"`
}

// MeasurementResolutionDecision is one question about one measure within one
// source. It collects the alias groups of that source with identical
// candidates (`cup` and `cups`), so each measure is asked once. Sources stay
// apart: an answer for one upload is not evidence about another.
type MeasurementResolutionDecision struct {
	ID           string `json:"id"`
	SourceLabel  string `json:"source_label"`
	MeasureLabel string `json:"measure_label"`
	// AuthoredForms are the spellings the sources actually used, most frequent
	// first. The wording a person recognises is their own, not the registry's.
	AuthoredForms    []string                       `json:"authored_forms"`
	GroupIDs         []string                       `json:"group_i_ds"`
	Occurrences      int                            `json:"occurrences"`
	Ingredients      int                            `json:"ingredients"`
	Recipes          int                            `json:"recipes"`
	CandidateUnitIDs []string                       `json:"candidate_unit_i_ds"`
	Choices          []MeasurementResolutionChoice  `json:"choices"`
	Examples         []MeasurementResolutionExample `json:"examples"`
}

// MeasurementResolutionOccurrence identifies one ambiguous measurement occurrence.
type MeasurementResolutionOccurrence struct {
	ID               string
	GroupID          string
	ItemID           string
	RecipeID         string
	RecipeTitle      string
	IngredientID     string
	SourceText       string
	AuthoredForm     string
	CandidateUnitIDs []string
	ResolutionKey    string
	// OccurrenceKey addresses this measure inside its ingredient. ResolutionKey
	// groups like measures across the import and cannot address one: an
	// ingredient may say `cup` twice, and an answer for one of them must not
	// settle the other.
	OccurrenceKey string
	// DeclaredLocale is what the source document stated, empty when it stated
	// none. Provenance has to be able to tell those apart.
	DeclaredLocale string
	Fingerprint    string
}

// MeasurementResolutionGroupDetail pairs a resolution group with its affected recipe summaries.
type MeasurementResolutionGroupDetail struct {
	Group       MeasurementResolutionGroup
	Occurrences []MeasurementResolutionOccurrence
}

// MeasurementResolutionInterpretation selects a canonical unit, convention, or authored-text outcome.
type MeasurementResolutionInterpretation struct {
	Kind       string
	UnitID     string
	Convention string
}

// ResolutionCanonicalUnit and related constants identify supported ambiguity resolutions.
const (
	ResolutionCanonicalUnit = "canonical-unit"
	ResolutionConvention    = "convention"
	ResolutionKeepWritten   = "keep-written"
)

// MeasurementResolutionPlanRequest selects compatible groups and the interpretation applied to them.
type MeasurementResolutionPlanRequest struct {
	GroupIDs              []string
	Interpretation        MeasurementResolutionInterpretation
	ExcludedOccurrenceIDs []string
	ExcludedItemIDs       []string
	ExpectedRevision      uint64
}

// MeasurementResolutionPlan previews the exact occurrences, changes, and validation impact of a resolution.
type MeasurementResolutionPlan struct {
	ID                  string
	PreviewRevision     uint64
	ImpactDigest        string
	AffectedOccurrences int
	// PendingOccurrences are measures this plan answered in ingredients that
	// are not yet complete enough to store. The answers are retained in the
	// review session, so these are accepted rather than refused, and they stop
	// being asked about.
	PendingOccurrences []MeasurementResolutionOccurrence
	// UnresolvableOccurrences are measures this plan cannot settle: the
	// authored amount is beyond the parser, or the ingredient moved under the
	// plan. They keep their authored wording and do not refuse the plan.
	UnresolvableOccurrences []MeasurementResolutionOccurrence
	// AffectedIngredients counts authored ingredient statements, which may each
	// contain several affected measurement occurrences.
	AffectedIngredients            int
	AffectedRecipes                int
	RemainingActionableOccurrences int
	UnchangedOccurrences           int
	CurrentActionableOccurrences   int
	ResultingActionableOccurrences int
	ValidationFailures             []string
	Examples                       []MeasurementResolutionExample
	Occurrences                    []MeasurementResolutionOccurrence
	Preview                        *ImportPreview
}

// MeasurementResolutionRecord carries a staged plan's decisions into its import
// job as importer-local history, separate from recipe provenance.
type MeasurementResolutionRecord struct {
	ResolutionKind     string `json:"resolution_kind"`
	InterpretationKind string `json:"interpretation_kind"`
	// DecisionKind names which of SREF section 9.5's two decisions this was.
	// The interpretation kind is the action a person took in the interface;
	// this is what that action means to a resolver, and the two are recorded
	// separately so a later reader does not have to infer one from the other.
	DecisionKind string `json:"decision_kind"`
	UnitID       string `json:"unit_id,omitempty"`
	Convention   string `json:"convention,omitempty"`
	// ConventionUnitIDs is the mapping the convention expanded to, so what a
	// person authorized can be read back without depending on the registry
	// still expanding the same word the same way.
	ConventionUnitIDs []string `json:"convention_unit_ids,omitempty"`
	// ScopeIngredientIDs is the exact scope the decision was submitted for.
	// A convention names the ingredient statements it reached; an occurrence
	// interpretation names the ingredients its occurrences belong to.
	ScopeIngredientIDs       []string `json:"scope_ingredient_ids,omitempty"`
	ScopeOccurrenceKeys      []string `json:"scope_occurrence_keys,omitempty"`
	ParserVersion            string   `json:"parser_version"`
	UnitRegistryVersion      string   `json:"unit_registry_version"`
	AffectedOccurrenceIDs    []string `json:"affected_occurrence_ids"`
	ExcludedOccurrenceIDs    []string `json:"excluded_occurrence_ids,omitempty"`
	ExcludedItemIDs          []string `json:"excluded_item_ids,omitempty"`
	AffectedRecipeCount      int      `json:"affected_recipe_count"`
	AffectedIngredientCount  int      `json:"affected_ingredient_count"`
	OriginalActionableCount  int      `json:"original_actionable_count"`
	ResultingActionableCount int      `json:"resulting_actionable_count"`
	ImpactDigest             string   `json:"impact_digest"`
}

// Decision kinds are SREF section 9.5's two resolution inputs. They are what a
// decision means to a resolver, as distinct from the action a person took.
const (
	DecisionOccurrenceInterpretation = "occurrence-interpretation"
	DecisionIngredientConvention     = "ingredient-convention"
	DecisionReviewedAsWritten        = "reviewed-as-written"
	// DecisionScopeUnknown is what a record written before the scope was
	// recorded says about its scope, which is nothing.
	DecisionScopeUnknown = "unknown"
)

// DecisionScope reports what this record establishes about the scope a person
// authorized.
//
// A record predating the field says `unknown`. Absent members decode to zero
// values, and SREF section 9.5 forbids reconstructing a scope that cannot be
// established, so they must never be read as an ingredient-wide convention.
func (record MeasurementResolutionRecord) DecisionScope() string {
	switch record.DecisionKind {
	case DecisionOccurrenceInterpretation, DecisionIngredientConvention, DecisionReviewedAsWritten:
		return record.DecisionKind
	default:
		return DecisionScopeUnknown
	}
}

// Replayable reports whether this record establishes enough to reapply the
// decision to a fresh import of the same source.
//
// It is never true for a record whose scope is unknown, and SREF section 9.5
// requires more than this even when it is: a refreshed import is a new
// normalization, and reuse needs an explicitly scoped application policy rather
// than an inference from a stored record.
func (record MeasurementResolutionRecord) Replayable() bool {
	if record.DecisionScope() == DecisionScopeUnknown || len(record.ScopeIngredientIDs) == 0 {
		return false
	}

	if record.DecisionKind == DecisionIngredientConvention {
		return len(record.ConventionUnitIDs) != 0
	}

	return len(record.ScopeOccurrenceKeys) != 0
}

// MeasurementResolutionGroup lists the occurrences in a group within the caller's
// preview scope. Missing previews, inaccessible previews, and unknown groups all
// return ErrPreviewNotFound.
func (s *Service) MeasurementResolutionGroup(ctx context.Context, previewID, groupID string) (MeasurementResolutionGroupDetail, error) {
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	session, ok := s.previews[previewID]
	if !ok || session.scope != s.previewScope(ctx) {
		return MeasurementResolutionGroupDetail{}, ErrPreviewNotFound
	}

	for _, group := range resolutionGroups(previewID, session) {
		if group.ID == groupID {
			detail := MeasurementResolutionGroupDetail{Group: group}

			for _, occurrence := range resolutionOccurrences(previewID, session) {
				if occurrence.GroupID == groupID {
					detail.Occurrences = append(detail.Occurrences, occurrence)
				}
			}

			return detail, nil
		}
	}

	return MeasurementResolutionGroupDetail{}, ErrPreviewNotFound
}

// CreateMeasurementResolutionPlan retains a proposed resolution in the caller's
// preview session without applying it to candidates. ExpectedRevision must match
// the session revision or it returns ErrResolutionPlanStale. A plan exceeding
// retained-memory limits is not retained and returns ErrPreviewTooLarge.
// ValidationFailures describes candidate changes that would prevent application.
func (s *Service) CreateMeasurementResolutionPlan(ctx context.Context, previewID string, request MeasurementResolutionPlanRequest) (MeasurementResolutionPlan, error) {
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	session, ok := s.previews[previewID]
	if !ok || session.scope != s.previewScope(ctx) {
		return MeasurementResolutionPlan{}, ErrPreviewNotFound
	}

	if request.ExpectedRevision != session.revision {
		return MeasurementResolutionPlan{}, ErrResolutionPlanStale
	}

	plan, unitsByOccurrence, fingerprints, err := buildMeasurementResolutionPlan(previewID, session, request)
	if err != nil {
		return MeasurementResolutionPlan{}, err
	}

	randomID, err := randomPreviewID()
	if err != nil {
		return MeasurementResolutionPlan{}, err
	}

	plan.ID = "resolution-plan-" + randomID
	previousPlans := make(map[string]*storedResolutionPlan, len(session.resolutionPlans))
	maps.Copy(previousPlans, session.resolutionPlans)
	session.resolutionPlans[plan.ID] = &storedResolutionPlan{
		request: request, plan: plan, fingerprints: fingerprints, selectedUnits: unitsByOccurrence,
	}
	previousBytes := session.retainedBytes

	session.retainedBytes = retainedSessionBytes(session)
	if session.retainedBytes > s.previewLimits.RetainedBytesPerSession ||
		exceedsInt64(s.previewStats.RetainedBytes-previousBytes, session.retainedBytes, s.previewLimits.RetainedBytes) {
		session.resolutionPlans = previousPlans
		session.retainedBytes = previousBytes

		return MeasurementResolutionPlan{}, ErrPreviewTooLarge
	}

	s.previewStats.RetainedBytes += session.retainedBytes - previousBytes

	return plan, nil
}

// ApplyMeasurementResolutionPlan atomically applies a still-current plan to every affected candidate.
func (s *Service) ApplyMeasurementResolutionPlan(ctx context.Context, previewID, planID string, expectedRevision uint64) (MeasurementResolutionPlan, error) {
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	session, stored, err := s.currentResolutionPlan(ctx, previewID, planID, expectedRevision)
	if err != nil {
		return MeasurementResolutionPlan{}, err
	}

	plan, unitsByOccurrence, _, err := buildMeasurementResolutionPlan(previewID, session, stored.request)
	if err != nil {
		return MeasurementResolutionPlan{}, err
	}

	if len(plan.ValidationFailures) != 0 {
		return MeasurementResolutionPlan{}, fmt.Errorf("%w: %s", ErrResolutionPlanInvalid, strings.Join(plan.ValidationFailures, "; "))
	}

	// A plan that settles nothing is refused here rather than reported as a
	// validation failure: nothing is wrong with the recipes.
	if len(plan.Occurrences) == 0 && len(plan.PendingOccurrences) == 0 {
		return MeasurementResolutionPlan{}, fmt.Errorf("%w: this answer settles no measurement", ErrResolutionPlanInvalid)
	}

	if plan.ImpactDigest != stored.plan.ImpactDigest {
		return MeasurementResolutionPlan{}, ErrResolutionPlanStale
	}

	snapshots, working, selected, metadata, retained, err := prepareResolutionApplication(session, stored, plan, unitsByOccurrence)
	if err != nil {
		return MeasurementResolutionPlan{}, err
	}

	// Answers retained for incomplete ingredients are review state. Record them
	// before installing candidates, because the outstanding review is read from
	// them.
	stored.retainedBefore = make(map[string]*pendingIngredientDecisions, len(retained))
	for key := range retained {
		stored.retainedBefore[key] = session.pendingDecisions[key]
	}

	maps.Copy(session.pendingDecisions, retained)
	installResolutionCandidates(previewID, session, stored, working, snapshots, selected, metadata)

	if err := s.analyzeResolvedCandidates(ctx, session, working); err != nil {
		restoreResolutionCandidates(session, snapshots)
		restorePendingDecisions(session, stored.retainedBefore)
		stored.retainedBefore = nil

		return MeasurementResolutionPlan{}, err
	}

	return s.finishResolutionApplication(previewID, planID, session, stored, plan, snapshots)
}

// RevertMeasurementResolutionPlan restores the candidate snapshots captured by an applied plan.
func (s *Service) RevertMeasurementResolutionPlan(ctx context.Context, previewID, planID string, expectedRevision uint64) (ImportPreview, error) {
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	session, ok := s.previews[previewID]
	if !ok || session.scope != s.previewScope(ctx) {
		return ImportPreview{}, ErrPreviewNotFound
	}

	stored := session.resolutionPlans[planID]
	if stored == nil || !stored.applied || expectedRevision != session.revision || stored.appliedRevision != session.revision {
		return ImportPreview{}, ErrResolutionPlanStale
	}

	restoreResolutionCandidates(session, stored.snapshots)
	restorePendingDecisions(session, stored.retainedBefore)

	stored.applied = false
	stored.snapshots, stored.retainedBefore = nil, nil
	session.revision++
	previousBytes := session.retainedBytes
	session.retainedBytes = retainedSessionBytes(session)
	s.previewStats.RetainedBytes += session.retainedBytes - previousBytes

	return previewFromSession(previewID, session), nil
}
