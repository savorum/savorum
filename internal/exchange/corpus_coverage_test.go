package exchange_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/savorum/savorum/internal/corpuscheck"
	"github.com/savorum/savorum/internal/recipe"
)

// requiredSliceConstructs is the preservation coverage the pinned corpus slice
// is here to provide, per case, as a set of construct names.
//
// A construct listed here and absent from the target is a failure, so coverage
// is only dropped by removing a line in a reviewed change.
// `normalized-timing-durations` is separate from `timing-assertions` because a
// target can keep every assertion while losing its normalized durations.
var requiredSliceConstructs = map[string][]string{
	"rc-000043": {
		"ingredient-sections", "instruction-sections", "notes", "source",
		"timing-assertions", "normalized-timing-durations", "step-timing-refs",
	},
	"rc-000049": {
		"ingredient-sections", "instruction-sections", "language", "notes", "source",
		"step-temperatures", "timing-assertions", "normalized-timing-durations", "step-timing-refs",
	},
	"rc-000051": {
		"ingredient-sections", "instruction-sections", "language", "notes", "source",
		"step-temperatures", "timing-assertions", "normalized-timing-durations", "step-timing-refs",
	},
	"rc-000052": {
		"ingredient-sections", "instruction-sections", "language", "notes", "source", "yield",
	},
}

// sliceCasesWithoutTargets are slice fixtures that are not derived SREF
// targets, so there is no target file to read constructs out of. rc-000833 is
// project-authored saved Schema.org JSON-LD, and its constructs are asserted
// against the imported document by the slice's own case instead.
var sliceCasesWithoutTargets = map[string]bool{"rc-000833": true}

// TestThePinnedSliceStillCarriesItsRequiredConstructs fails a repin that
// quietly removes preservation coverage.
//
// The slice assertions cannot notice a target that stopped carrying something,
// so this reads the targets directly and holds them to a declared list.
func TestThePinnedSliceStillCarriesItsRequiredConstructs(t *testing.T) {
	t.Parallel()

	root := os.Getenv("RECIPE_CORPUS_ROOT")
	if root == "" {
		t.Skip("set RECIPE_CORPUS_ROOT to run the pinned recipe-corpus slice")
	}

	if !filepath.IsAbs(root) {
		root = filepath.Join("../..", root)
	}

	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}

	// The declaration is about the pinned revision. Checking it against some
	// other checkout would report coverage the pin does not have.
	if actual := corpusGitRevision(t, root); actual != recipeCorpusRevision {
		t.Fatalf("recipe-corpus revision = %s, want pinned %s", actual, recipeCorpusRevision)
	}

	for _, fixture := range pinnedCorpusSlice {
		if sliceCasesWithoutTargets[fixture.ID] {
			continue
		}

		required, declared := requiredSliceConstructs[fixture.ID]
		if !declared {
			t.Errorf("%s is in the slice and declares no required constructs", fixture.ID)
			continue
		}

		document := readSliceTarget(t, root, fixture)
		present := corpuscheck.Of(document)

		var missing []string

		for _, construct := range required {
			if !present[construct] {
				missing = append(missing, construct)
			}
		}

		if len(missing) != 0 {
			sort.Strings(missing)
			t.Errorf("%s no longer carries %v.\n"+
				"A repin that removes a construct needs an explicit coverage replacement, "+
				"or a reviewed change to this list saying the coverage was dropped on purpose.",
				fixture.ID, missing)
		}
	}
}

// TestTheSliceCoverageDeclarationIsHonest keeps the declaration from drifting
// into a list of things that happen to be true.
//
// A construct required of no case is coverage this slice does not have, and a
// name nothing can produce is a typo that would never fail.
func TestTheSliceCoverageDeclarationIsHonest(t *testing.T) {
	t.Parallel()

	known := corpuscheck.Of(everyConstructDocument())

	for id, required := range requiredSliceConstructs {
		for _, construct := range required {
			if !known[construct] {
				t.Errorf("%s requires %q, which nothing can report", id, construct)
			}
		}
	}

	declared := map[string]bool{}
	for _, fixture := range pinnedCorpusSlice {
		declared[fixture.ID] = true
	}

	for id := range requiredSliceConstructs {
		if !declared[id] {
			t.Errorf("%s declares required constructs and is not in the slice", id)
		}
	}

	// Every SREF target in the slice declares something, or the guard would
	// pass over it in silence.
	for _, fixture := range pinnedCorpusSlice {
		if sliceCasesWithoutTargets[fixture.ID] {
			continue
		}

		if len(requiredSliceConstructs[fixture.ID]) == 0 {
			t.Errorf("%s is a derived target with no declared coverage", fixture.ID)
		}
	}
}

func readSliceTarget(t *testing.T, root string, fixture corpusSliceCase) recipe.Document {
	t.Helper()

	path := filepath.Join(root, "evaluations", "sref", srefEvaluationRevision, "targets",
		fixture.ID+".recipe.json")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", fixture.ID, err)
	}

	var document recipe.Document
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("%s: %v", fixture.ID, err)
	}

	return document
}

// everyConstructDocument carries one of everything corpuscheck can report, so
// the declaration can be checked against names that exist.
func everyConstructDocument() recipe.Document {
	amount := recipe.Amount{Value: rationalOf("1")}

	return recipe.Document{
		Language: "en", SourceLocale: "en-GB",
		Yields: []recipe.Yield{{Text: "4 servings"}},
		Source: &recipe.Source{Name: "somewhere"},
		Notes:  []string{"a note"},
		Times: &recipe.Times{Assertions: []recipe.TimingAssertion{
			{ID: "t", Duration: recipe.DurationExpression{Value: "PT1H"}},
		}},
		IngredientSections: []recipe.IngredientSection{
			{ID: "a", Ingredients: []recipe.Ingredient{{
				ID: "i", Name: "salt",
				Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &amount, Unit: "mass.gram"},
			}}},
			{ID: "b", Ingredients: []recipe.Ingredient{{ID: "j", Name: "pepper"}}},
		},
		InstructionSections: []recipe.InstructionSection{
			{ID: "m", Steps: []recipe.Step{{
				ID: "s", Text: "Cook.", TimingRefs: []string{"t"},
				Temperatures: []recipe.Temperature{{Amount: amount, Unit: "temperature.celsius"}},
				IngredientUses: []recipe.IngredientUse{{
					IngredientRef: "i",
					Quantity:      &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &amount, Unit: "mass.gram"},
				}},
			}}},
			{ID: "n", Steps: []recipe.Step{{ID: "u", Text: "Serve."}}},
		},
	}
}

func rationalOf(value string) *recipe.Rational {
	parsed, err := recipe.ParseRational(value)
	if err != nil {
		panic(fmt.Sprintf("invalid rational %q: %v", value, err))
	}

	return &parsed
}
