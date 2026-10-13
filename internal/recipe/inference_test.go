package recipe_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
)

func TestExtractTimingsMatchesSharedContractCorpus(t *testing.T) {
	t.Parallel()

	encoded, err := os.ReadFile("../../testdata/timing_inference.json")
	if err != nil {
		t.Fatal(err)
	}

	var corpus []struct {
		Text     string                   `json:"text"`
		Expected []recipe.TimingAssertion `json:"expected"`
	}
	if err := json.Unmarshal(encoded, &corpus); err != nil {
		t.Fatal(err)
	}

	if len(corpus) < 200 {
		t.Fatalf("timing contract corpus has %d cases, want at least 200", len(corpus))
	}

	for index, fixture := range corpus {
		found := recipe.ExtractTimings(recipe.Step{ID: "fixture", Text: fixture.Text})
		if len(found) == 0 && len(fixture.Expected) == 0 {
			continue
		}

		if !reflect.DeepEqual(found, fixture.Expected) {
			t.Errorf("case %d %q\nfound: %#v\n want: %#v", index, fixture.Text, found, fixture.Expected)
		}
	}
}

func TestExtractTimingsBuildsAnOrderedSemanticSequence(t *testing.T) {
	step := recipe.Step{ID: "knead", Text: "Knead the dough for 5 minutes, then cover and let rest for 30 minutes."}

	got := recipe.ExtractTimings(step)
	if len(got) != 2 {
		t.Fatalf("timings = %#v", got)
	}
	// Kneading has no kind in the closed vocabulary, so none is set.
	if got[0].Kind != recipe.TimingKind("") || got[0].Duration.Value != "PT5M" || got[0].Duration.SourceText != "for 5 minutes" {
		t.Errorf("unclassified timing = %#v", got[0])
	}

	if got[1].Kind != recipe.TimingRest || got[1].Duration.Value != "PT30M" || got[1].Provenance != recipe.TimingDerived {
		t.Errorf("rest timing = %#v", got[1])
	}
}

func TestExtractTimingsPreservesRangesAndRejectsCadence(t *testing.T) {
	rangeStep := recipe.Step{ID: "bake", Text: "Bake until golden, about 20–25 minutes."}
	got := recipe.ExtractTimings(rangeStep)
	// Baking is cooking, and the verb's own wording survives in the label.
	if len(got) != 1 || got[0].Kind != recipe.TimingCook || got[0].Label != "Bake" {
		t.Fatalf("range timing kind = %#v", got)
	}

	if got[0].Duration.Min != "PT20M" || got[0].Duration.Max != "PT25M" || !got[0].Duration.Approximate {
		t.Fatalf("range timing duration = %#v", got[0].Duration)
	}

	if got[0].Duration.Value != "" {
		t.Errorf("a range also carried an exact value: %#v", got[0].Duration)
	}

	if got := recipe.ExtractTimings(recipe.Step{ID: "stir", Text: "Stir every 5 minutes."}); len(got) != 0 {
		t.Fatalf("cadence became a duration: %#v", got)
	}
}

func TestReconcileTimingsUpdatesDerivedAndPreservesSupplemental(t *testing.T) {
	document := recipe.Document{
		Times:               &recipe.Times{Assertions: []recipe.TimingAssertion{{ID: "authored", Kind: recipe.TimingRest, Duration: recipe.DurationExpression{Value: "PT20M"}, Provenance: recipe.TimingSupplemental}}},
		InstructionSections: []recipe.InstructionSection{{Steps: []recipe.Step{{ID: "step", Text: "Knead for 5 minutes.", TimingRefs: []string{"authored"}}}}},
	}

	first := recipe.ReconcileTimings(document)
	if len(first.Times.Assertions) != 2 {
		t.Fatalf("first assertions = %#v", first.Times.Assertions)
	}

	first.InstructionSections[0].Steps[0].Text = "Knead for 8 minutes."

	second := recipe.ReconcileTimings(first)
	if len(second.Times.Assertions) != 2 || second.Times.Assertions[0].Duration.Value != "PT20M" || second.Times.Assertions[1].Duration.Value != "PT8M" {
		t.Fatalf("updated assertions = %#v", second.Times.Assertions)
	}
	// The step keeps a reference to the authored assertion and gains one to
	// the rebuilt derived assertion, rather than losing either.
	refs := second.InstructionSections[0].Steps[0].TimingRefs
	if len(refs) != 2 || refs[0] != "authored" {
		t.Fatalf("step timing refs = %#v", refs)
	}
}

// A Document shares its section slices and Times, so ReconcileTimings must not
// write through them into the caller's copy.
func TestReconcileTimingsLeavesItsArgumentAlone(t *testing.T) {
	t.Parallel()

	document := recipe.Document{
		Times:               &recipe.Times{},
		InstructionSections: []recipe.InstructionSection{{ID: "m1", Steps: []recipe.Step{{ID: "m1-1", Text: "Bake for 20 minutes."}}}},
	}

	_ = recipe.ReconcileTimings(document)

	if refs := document.InstructionSections[0].Steps[0].TimingRefs; len(refs) != 0 {
		t.Fatalf("the argument gained timing references %#v", refs)
	}

	if assertions := document.Times.Assertions; len(assertions) != 0 {
		t.Fatalf("the argument gained timing assertions %#v", assertions)
	}

	again := recipe.ReconcileTimings(document)
	if refs := again.InstructionSections[0].Steps[0].TimingRefs; len(refs) != 1 {
		t.Fatalf("reconciling the same document twice produced references %#v", refs)
	}
}

// A repeated timing reference is withdrawn.
func TestReconcileTimingsWithdrawsARepeatedReference(t *testing.T) {
	t.Parallel()

	document := recipe.Document{
		Times:               &recipe.Times{Assertions: []recipe.TimingAssertion{{ID: "authored", Kind: recipe.TimingRest, Duration: recipe.DurationExpression{Value: "PT20M"}, Provenance: recipe.TimingSupplemental}}},
		InstructionSections: []recipe.InstructionSection{{ID: "m1", Steps: []recipe.Step{{ID: "m1-1", Text: "Rest the dough.", TimingRefs: []string{"authored", "authored"}}}}},
	}

	reconciled := recipe.ReconcileTimings(document)
	if refs := reconciled.InstructionSections[0].Steps[0].TimingRefs; len(refs) != 1 || refs[0] != "authored" {
		t.Fatalf("step timing refs = %#v", refs)
	}
}

func TestExtractTimingsDoesNotDuplicateOneSharedPhrase(t *testing.T) {
	got := recipe.ExtractTimings(recipe.Step{ID: "roux", Text: "Cook and stir the flour for about 1 minute."})
	if len(got) != 1 {
		t.Fatalf("shared phrase produced %#v", got)
	}
}

func TestExtractTimingsReadsADurationStatedWithoutAConnector(t *testing.T) {
	t.Parallel()
	// "Bake 6 to 8 minutes" needs no connecting "for".
	found := recipe.ExtractTimings(recipe.Step{
		ID:   "s",
		Text: "Bake 6 to 8 minutes or until lightly browned and cheese is melted.",
	})
	if len(found) != 1 {
		t.Fatalf("extracted %d timings, want 1: %#v", len(found), found)
	}

	if found[0].Kind != recipe.TimingCook || found[0].Label != "Bake" {
		t.Errorf("kind = %q label = %q, want cook labelled Bake", found[0].Kind, found[0].Label)
	}

	if found[0].Duration.Min != "PT6M" || found[0].Duration.Max != "PT8M" {
		t.Errorf("span = %q–%q, want PT6M–PT8M", found[0].Duration.Min, found[0].Duration.Max)
	}

	if found[0].Duration.Approximate {
		t.Error("a stated range is not an approximation")
	}
}

func TestExtractTimingsIgnoresANumberNoActionClaims(t *testing.T) {
	t.Parallel()
	// The connector is optional, not the verb. A quantity, an oven setting, or
	// a pan size is not a duration however close it sits to a word.
	for _, text := range []string{
		"Divide the dough into 16 balls.",
		"Preheat the oven to 450°F.",
		"Slide avocado, and halve tomatoes.",
	} {
		if found := recipe.ExtractTimings(recipe.Step{ID: "s", Text: text}); len(found) != 0 {
			t.Errorf("%q extracted %#v, want nothing", text, found)
		}
	}
}
