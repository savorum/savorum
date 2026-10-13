package recipe_test

import (
	"reflect"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
)

func TestMicrowaveEstimateScalesExactAndRangeWithoutChangingSource(t *testing.T) {
	exact := recipe.TimingAssertion{Duration: recipe.DurationExpression{Value: "PT2M", SourceText: "2 minutes"}, Microwave: &recipe.MicrowaveConditions{RatedOutputWatts: 1000, Power: &recipe.MicrowavePower{Percent: "100"}, SourceText: "high in a 1000 W microwave"}}
	original := exact

	got := recipe.EstimateMicrowaveTime(exact, 1250)
	if !got.Applicable || got.Estimated.Value != "PT1M36S" || !got.Estimated.Approximate || got.PowerPercent != 100 {
		t.Fatalf("estimate = %#v", got)
	}

	if !reflect.DeepEqual(exact, original) {
		t.Fatal("estimation changed authored timing")
	}

	ranged := exact
	ranged.Duration = recipe.DurationExpression{Min: "PT2M", Max: "PT3M"}

	got = recipe.EstimateMicrowaveTime(ranged, 1250)
	if got.Estimated.Min != "PT1M36S" || got.Estimated.Max != "PT2M24S" {
		t.Fatalf("range = %#v", got)
	}
}

func TestMicrowaveEstimatePrefersAuthoredChoiceAndWithholdsUnsafeCases(t *testing.T) {
	assertion := recipe.TimingAssertion{Duration: recipe.DurationExpression{Text: "600 W: 4 minutes; 500 W: 5 minutes"}, Microwave: &recipe.MicrowaveConditions{SourceText: "options", Choices: []recipe.MicrowaveChoice{{Duration: recipe.DurationExpression{Value: "PT4M"}, Power: &recipe.MicrowavePower{Watts: 600}, SourceText: "600 W: 4 minutes"}, {Duration: recipe.DurationExpression{Value: "PT5M"}, Power: &recipe.MicrowavePower{Watts: 500}, SourceText: "500 W: 5 minutes"}}}}

	got := recipe.EstimateMicrowaveTime(assertion, 500)
	if !got.Applicable || !got.AuthoredChoice || got.Estimated.Value != "PT5M" {
		t.Fatalf("authored option = %#v", got)
	}

	assertion.Microwave.Restrictions = []string{"Use only 500 W or 600 W"}
	if got := recipe.EstimateMicrowaveTime(assertion, 900); got.Applicable || got.Reason != "source_restriction" {
		t.Fatalf("restriction = %#v", got)
	}
}

func TestExtractMicrowaveTimingKeepsPowerRoleAndAlternatives(t *testing.T) {
	got := recipe.ExtractTimings(recipe.Step{Text: "Microwave on high in a 1000 W microwave for 2 minutes."})
	if len(got) != 1 || got[0].Microwave == nil || got[0].Microwave.RatedOutputWatts != 1000 || got[0].Microwave.Power.Percent != "100" {
		t.Fatalf("rated output = %#v", got)
	}

	selected := recipe.ExtractTimings(recipe.Step{Text: "Microwave at 600 W for 4 minutes."})
	if len(selected) != 1 || selected[0].Microwave.Power.Watts != 600 || selected[0].Microwave.RatedOutputWatts != 0 {
		t.Fatalf("selected power = %#v", selected)
	}

	alternatives := recipe.ExtractTimings(recipe.Step{Text: "Microwave: 700 W: 4 minutes; 900 W: 3 minutes."})
	if len(alternatives) != 1 || alternatives[0].Duration.Text == "" || len(alternatives[0].Microwave.Choices) != 2 {
		t.Fatalf("alternatives = %#v", alternatives)
	}
}
