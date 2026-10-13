package corpuscheck_test

import (
	"slices"
	"testing"

	"github.com/savorum/savorum/internal/corpuscheck"
	"github.com/savorum/savorum/internal/recipe"
)

// Normalized durations and retained wording are separate constructs, since a
// target can keep every assertion while losing its normalized durations.
func TestNormalizedAndOpaqueDurationsAreDistinctConstructs(t *testing.T) {
	t.Parallel()

	normalized := recipe.Document{Times: &recipe.Times{Assertions: []recipe.TimingAssertion{
		{ID: "timing-001", Duration: recipe.DurationExpression{Value: "PT1H30M", SourceText: "one and one-half hours"}},
	}}}
	opaque := recipe.Document{Times: &recipe.Times{Assertions: []recipe.TimingAssertion{
		{ID: "timing-001", Duration: recipe.DurationExpression{Text: "one and one-half hours"}},
	}}}

	for _, document := range []recipe.Document{normalized, opaque} {
		if !corpuscheck.Of(document)[corpuscheck.TimingAssertions] {
			t.Fatal("a timing assertion went unseen")
		}
	}

	if !corpuscheck.Of(normalized)[corpuscheck.NormalizedTimingDurations] ||
		corpuscheck.Of(normalized)[corpuscheck.OpaqueTimingDurations] {
		t.Fatalf("normalized duration constructs = %v", corpuscheck.Names(corpuscheck.Of(normalized)))
	}

	if !corpuscheck.Of(opaque)[corpuscheck.OpaqueTimingDurations] ||
		corpuscheck.Of(opaque)[corpuscheck.NormalizedTimingDurations] {
		t.Fatalf("opaque duration constructs = %v", corpuscheck.Names(corpuscheck.Of(opaque)))
	}

	// Which one a repin moved between is what the reviewer needs to see.
	changes := corpuscheck.Compare(
		map[string]recipe.Document{"rc-000043": opaque},
		map[string]recipe.Document{"rc-000043": normalized},
	)
	if len(changes) != 1 ||
		!slices.Contains(changes[0].Gained, corpuscheck.NormalizedTimingDurations) ||
		!slices.Contains(changes[0].Lost, corpuscheck.OpaqueTimingDurations) {
		t.Fatalf("repin comparison = %#v", changes)
	}
}

// A case that starts representing one region of a multi-recipe page is a
// source-boundary change the report must show.
func TestASourceBoundaryChangeIsVisibleInTheComparison(t *testing.T) {
	t.Parallel()

	whole := recipe.Document{Notes: []string{"source"}}
	region := recipe.Document{Notes: []string{"source"}, Extensions: map[string]any{
		"x-org.savorum.source-recipe-regions": map[string]any{"represented": "sahne-mousse"},
	}}

	if corpuscheck.Of(whole)[corpuscheck.SourceRecipeRegions] {
		t.Fatal("a single-recipe page reported a recipe-region boundary")
	}

	if !corpuscheck.Of(region)[corpuscheck.SourceRecipeRegions] {
		t.Fatal("a multi-recipe page reported no recipe-region boundary")
	}

	changes := corpuscheck.Compare(
		map[string]recipe.Document{"rc-000051": whole},
		map[string]recipe.Document{"rc-000051": region},
	)
	if len(changes) != 1 || !slices.Contains(changes[0].Gained, corpuscheck.SourceRecipeRegions) {
		t.Fatalf("boundary comparison = %#v", changes)
	}
}
