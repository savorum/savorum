package print

import (
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func TestAdjustedPrintNamesItsBasisAndLeavesStandingTimeAlone(t *testing.T) {
	presenter := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, nil, "en")
	presenter.microwaveOutputWatts = 1250
	step := recipe.Step{ID: "heat", Text: "Microwave for 2 minutes, then stand for 2 minutes."}
	phases := []recipe.TimingAssertion{
		{ID: "heat-time", Kind: recipe.TimingCook, Duration: recipe.DurationExpression{Value: "PT2M"}, Microwave: &recipe.MicrowaveConditions{RatedOutputWatts: 1000, Power: &recipe.MicrowavePower{Percent: "100"}, SourceText: "1000 W"}},
		{ID: "stand-time", Kind: recipe.TimingRest, Duration: recipe.DurationExpression{Value: "PT2M"}},
	}
	printed := presenter.presentStep(step, 1, phases)

	joined := strings.Join(printed.Phases, " | ")
	if !strings.Contains(joined, "About 1 min 36 sec for 1250 W microwave") || !strings.Contains(joined, "recipe: 2 min at 1000 W") {
		t.Fatalf("adjusted print = %q", joined)
	}

	if strings.Contains(joined, "1 min 36 sec Rest") {
		t.Fatalf("standing time was adjusted: %q", joined)
	}
}
