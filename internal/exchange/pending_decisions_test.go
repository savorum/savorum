package exchange

import (
	"testing"

	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/units"
)

// TestRetainedAnswersAreHeldToTheReadingTheyWereGivenAgainst: a change to the
// line, the parser version, or the registry version invalidates a retained
// answer.
func TestRetainedAnswersAreHeldToTheReadingTheyWereGivenAgainst(t *testing.T) {
	t.Parallel()

	const line = "1 tablespoon plus 2 teaspoons neutral oil"

	answers := &pendingIngredientDecisions{
		interpretations: map[string]string{"key": "volume.tablespoon.us.customary"},
		sourceText:      line,
		parserVersion:   ingredientparser.ParserVersion,
		registryVersion: units.RegistryVersion,
	}

	if !answers.current(line) {
		t.Fatal("answers given against this very line were rejected")
	}

	if answers.current("1 tablespoon plus 3 teaspoons neutral oil") {
		t.Fatal("answers about one line were accepted for another")
	}

	stale := *answers
	stale.parserVersion = "0"

	if stale.current(line) {
		t.Fatal("answers reviewed under another parser were accepted")
	}

	stale = *answers
	stale.registryVersion = "0.0.1"

	if stale.current(line) {
		t.Fatal("answers reviewed against another registry were accepted")
	}

	// Nothing retained is not an answer, and must not read as one.
	var absent *pendingIngredientDecisions
	if absent.current(line) {
		t.Fatal("an absent record answered for an ingredient")
	}
}
