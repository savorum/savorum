// Package corpuscheck reports which preservation constructs a recipe document
// carries.
//
// The pinned-slice coverage gate and the repin review report share it, so they
// cannot disagree.
package corpuscheck

import (
	"sort"

	"github.com/savorum/savorum/internal/recipe"
)

// Constructs are the preservation properties this corpus slice is here to
// exercise. They are deliberately coarse: presence of a value or relationship,
// not whether that value is faithful to its source.
//
// This is not a source-fidelity check; the corpus's own regressions own that.
const (
	Language                    = "language"
	SourceLocale                = "source-locale"
	Yield                       = "yield"
	Source                      = "source"
	Notes                       = "notes"
	IngredientSections          = "ingredient-sections"
	MultipleIngredientSections  = "multiple-ingredient-sections"
	InstructionSections         = "instruction-sections"
	MultipleInstructionSections = "multiple-instruction-sections"
	Times                       = "times"
	TimingAssertions            = "timing-assertions"
	NormalizedTimingDurations   = "normalized-timing-durations"
	OpaqueTimingDurations       = "opaque-timing-durations"
	StepTemperatures            = "step-temperatures"
	StepTimingRefs              = "step-timing-refs"
	StepIngredientUses          = "step-ingredient-uses"
	StepIngredientUseQuantities = "step-ingredient-use-quantities"
	IngredientQuantities        = "ingredient-quantities"
	// SourceRecipeRegions marks a target whose page holds more than one
	// recipe, so the case represents one region of it. A repin that adds or
	// removes it is a source-boundary change rather than a version bump, and
	// the reviewer has to look at which region the case now stands for.
	SourceRecipeRegions = "source-recipe-regions"
)

// sourceRecipeRegionsMember is the extension the corpus writes when a pinned
// page carries several separately executable recipes.
const sourceRecipeRegionsMember = "x-org.savorum.source-recipe-regions"

// Of reports the constructs a document carries. Presence is by value: an empty
// collection is not the construct.
func Of(document recipe.Document) map[string]bool {
	found := map[string]bool{}

	set := func(name string, present bool) {
		if present {
			found[name] = true
		}
	}

	set(Language, document.Language != "")
	set(SourceLocale, document.SourceLocale != "")
	set(Yield, len(document.Yields) > 0)
	set(Source, document.Source != nil && document.Source.Name != "")
	set(Notes, len(document.Notes) > 0)
	set(IngredientSections, len(document.IngredientSections) > 0)
	set(MultipleIngredientSections, len(document.IngredientSections) > 1)
	set(InstructionSections, len(document.InstructionSections) > 0)
	set(MultipleInstructionSections, len(document.InstructionSections) > 1)
	set(Times, document.Times != nil)

	if document.Times != nil {
		set(TimingAssertions, len(document.Times.Assertions) > 0)

		for _, assertion := range document.Times.Assertions {
			// A duration a reader can compute with, not only display.
			set(NormalizedTimingDurations, assertion.Duration.Value != "" ||
				assertion.Duration.Min != "" || assertion.Duration.Max != "")
			set(OpaqueTimingDurations, assertion.Duration.Text != "")
		}
	}

	for _, section := range document.InstructionSections {
		for _, step := range section.Steps {
			set(StepTemperatures, len(step.Temperatures) > 0)
			set(StepTimingRefs, len(step.TimingRefs) > 0)
			set(StepIngredientUses, len(step.IngredientUses) > 0)

			for _, use := range step.IngredientUses {
				set(StepIngredientUseQuantities, use.Quantity != nil)
			}
		}
	}

	for _, section := range document.IngredientSections {
		for _, ingredient := range section.Ingredients {
			set(IngredientQuantities, ingredient.Quantity != nil)
		}
	}

	_, regions := document.Extensions[sourceRecipeRegionsMember]
	set(SourceRecipeRegions, regions)

	return found
}

// Names lists a construct set in a stable order.
func Names(constructs map[string]bool) []string {
	names := make([]string, 0, len(constructs))
	for name := range constructs {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// Change is what happened to one case between two corpus revisions.
type Change struct {
	CaseID string
	// Status is `added`, `removed`, or `changed`. A case present in both with
	// the same constructs is not reported: a repin review wants what moved.
	Status string
	Gained []string
	Lost   []string
}

// Compare reports the construct-level difference between two sets of targets,
// keyed by case ID.
//
// This is a presence inventory, not a semantic diff: a lost construct is a
// prompt to look, not a description of what changed.
func Compare(before, after map[string]recipe.Document) []Change {
	seen := map[string]bool{}
	for id := range before {
		seen[id] = true
	}

	for id := range after {
		seen[id] = true
	}

	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}

	sort.Strings(ids)

	var changes []Change

	for _, id := range ids {
		old, hadOld := before[id]
		current, hasNew := after[id]

		switch {
		case !hadOld:
			changes = append(changes, Change{CaseID: id, Status: "added", Gained: Names(Of(current))})
		case !hasNew:
			changes = append(changes, Change{CaseID: id, Status: "removed", Lost: Names(Of(old))})
		default:
			gained, lost := difference(Of(old), Of(current))
			if len(gained) != 0 || len(lost) != 0 {
				changes = append(changes, Change{CaseID: id, Status: "changed", Gained: gained, Lost: lost})
			}
		}
	}

	return changes
}

func difference(before, after map[string]bool) (gained, lost []string) {
	for name := range after {
		if !before[name] {
			gained = append(gained, name)
		}
	}

	for name := range before {
		if !after[name] {
			lost = append(lost, name)
		}
	}

	sort.Strings(gained)
	sort.Strings(lost)

	return gained, lost
}
