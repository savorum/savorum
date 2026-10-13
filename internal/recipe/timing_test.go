package recipe_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestParseDuration(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		value string
		want  time.Duration
		ok    bool
	}{
		{value: "PT20M", want: 20 * time.Minute, ok: true},
		{value: "PT8H", want: 8 * time.Hour, ok: true},
		{value: "P1DT2H30M", want: 26*time.Hour + 30*time.Minute, ok: true},
		{value: "P1W", want: 7 * 24 * time.Hour, ok: true},
		{value: "PT90S", want: 90 * time.Second, ok: true},
		{value: "", ok: false},
		{value: "P", ok: false},
		// A month has no fixed length without a calendar position, so a total
		// containing one would be a guess rather than an answer.
		{value: "P1M", ok: false},
		{value: "P1Y", ok: false},
		{value: "PT0.5H", ok: false},
		{value: "45 minutes", ok: false},
		{value: "P20000W", ok: false},
		{value: "P15000WT2562047H", ok: false},
	} {
		got, ok := recipe.ParseDuration(testCase.value)
		if ok != testCase.ok || got != testCase.want {
			t.Errorf("ParseDuration(%q) = %v, %t; want %v, %t", testCase.value, got, ok, testCase.want, testCase.ok)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		value time.Duration
		want  string
	}{
		{value: 20 * time.Minute, want: "PT20M"},
		{value: 9*time.Hour + 15*time.Minute, want: "PT9H15M"},
		{value: 26*time.Hour + 30*time.Minute, want: "P1DT2H30M"},
		{value: 48 * time.Hour, want: "P2D"},
		{value: 0, want: ""},
		{value: -time.Minute, want: ""},
	} {
		if got := recipe.FormatDuration(testCase.value); got != testCase.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", testCase.value, got, testCase.want)
		}
	}
}

func TestSummarizeTimesSeparatesWorkFromWaiting(t *testing.T) {
	t.Parallel()

	document := recipe.Document{Times: &recipe.Times{Total: iso("PT9H15M"), Assertions: []recipe.TimingAssertion{
		{ID: "timing-1", Kind: recipe.TimingMarinate, Duration: recipe.DurationExpression{Value: "PT8H"}},
		{ID: "timing-2", Kind: recipe.TimingPrep, Duration: recipe.DurationExpression{Value: "PT20M"}},
		{ID: "timing-3", Kind: recipe.TimingCook, Duration: recipe.DurationExpression{Value: "PT45M"}},
		{ID: "timing-4", Kind: recipe.TimingRest, Duration: recipe.DurationExpression{Value: "PT10M"}},
	}}}
	got := recipe.SummarizeTimes(document)
	// Cooking does not count as work by default.
	want := recipe.TimeSummary{Prep: iso("PT20M"), Cook: iso("PT45M"), Active: iso("PT20M"), Elapsed: iso("PT9H15M")}
	if !reflect.DeepEqual(summaryValues(got), summaryValues(want)) {
		t.Fatalf("summary = %v, want %v", summaryValues(got), summaryValues(want))
	}
}

func TestSummarizeTimesPrefersAnAuthoredTotalOverAddedPhases(t *testing.T) {
	t.Parallel()
	// Phases can overlap. A recipe that says how long it really takes is
	// believed rather than corrected by addition.
	document := recipe.Document{
		Times: &recipe.Times{Total: iso("PT8H"), Assertions: []recipe.TimingAssertion{
			{ID: "timing-5", Kind: recipe.TimingProof, Duration: recipe.DurationExpression{Value: "PT8H"}},
			{ID: "timing-6", Kind: recipe.TimingPrep, Duration: recipe.DurationExpression{Value: "PT15M"}},
		}},
	}
	if got := recipe.SummarizeTimes(document); summaryValues(got)["elapsed"] != "PT8H" || summaryValues(got)["active"] != "PT15M" {
		t.Fatalf("summary = %#v", got)
	}
}

func TestSummarizeTimesHonorsAnAttentionOverride(t *testing.T) {
	t.Parallel()
	// Stated attention outranks the per-kind default in both directions:
	// chilling is waiting unless the recipe says otherwise, and cooking is
	// work unless the recipe says otherwise.
	document := recipe.Document{Times: &recipe.Times{Total: iso("PT2H30M"), Assertions: []recipe.TimingAssertion{
		{ID: "chill", Kind: recipe.TimingChill, Duration: recipe.DurationExpression{Value: "PT30M"}, Attention: recipe.AttentionActive},
		{ID: "cook", Kind: recipe.TimingCook, Duration: recipe.DurationExpression{Value: "PT2H"}, Attention: recipe.AttentionPassive},
	}}}
	if got := recipe.SummarizeTimes(document); summaryValues(got)["active"] != "PT30M" || summaryValues(got)["elapsed"] != "PT2H30M" {
		t.Fatalf("summary = %#v", got)
	}
}

func TestSummarizeTimesFallsBackToTheCoarseSummary(t *testing.T) {
	t.Parallel()

	document := recipe.Document{Times: &recipe.Times{Prep: iso("PT45M"), Cook: iso("PT30M"), Additional: iso("PT1H")}}
	got := recipe.SummarizeTimes(document)

	want := recipe.TimeSummary{Prep: iso("PT45M"), Cook: iso("PT30M"), Active: iso("PT1H15M"), Elapsed: iso("PT2H15M")}
	if !reflect.DeepEqual(summaryValues(got), summaryValues(want)) {
		t.Fatalf("summary = %v, want %v", summaryValues(got), summaryValues(want))
	}
}

func TestSummarizeTimesReportsNothingItCannotAdd(t *testing.T) {
	t.Parallel()

	document := recipe.Document{Times: &recipe.Times{Assertions: []recipe.TimingAssertion{
		{ID: "timing-7", Kind: recipe.TimingCure, Duration: recipe.DurationExpression{Value: "P1M"}},
		{ID: "timing-8", Kind: recipe.TimingPrep, Duration: recipe.DurationExpression{Value: "PT20M"}},
	}}}

	got := recipe.SummarizeTimes(document)
	if summaryValues(got)["elapsed"] != "" {
		t.Fatalf("elapsed = %q, want no answer", summaryValues(got)["elapsed"])
	}

	if summaryValues(got)["active"] != "PT20M" || summaryValues(got)["prep"] != "PT20M" {
		t.Fatalf("summary = %#v", got)
	}
}

func TestPhasesAloneDoNotClaimACompleteElapsedTime(t *testing.T) {
	t.Parallel()

	document := recipe.Document{Times: &recipe.Times{Assertions: []recipe.TimingAssertion{
		{ID: "timing-9", Kind: recipe.TimingChill, Duration: recipe.DurationExpression{Value: "PT2H"}},
	}}}
	if got := recipe.SummarizeTimes(document); summaryValues(got)["elapsed"] != "" {
		t.Fatalf("partial phase list claimed elapsed time %q", summaryValues(got)["elapsed"])
	}
}

func TestPartialPhasesDoNotEraseCoarseWorkingTime(t *testing.T) {
	t.Parallel()

	document := recipe.Document{Times: &recipe.Times{
		Prep: iso("PT20M"), Cook: iso("PT30M"), Total: iso("PT8H50M"),
		Assertions: []recipe.TimingAssertion{{ID: "timing-10", Kind: recipe.TimingMarinate, Duration: recipe.DurationExpression{Value: "PT8H"}}},
	}}

	got := recipe.SummarizeTimes(document)
	if summaryValues(got)["active"] != "PT50M" || summaryValues(got)["elapsed"] != "PT8H50M" {
		t.Fatalf("partial phase summary = %#v", got)
	}
}

func TestValidateAcceptsBothWaysOfStatingTheClock(t *testing.T) {
	t.Parallel()
	// SREF permits the coarse summary and named phases together, as in the
	// specification's roast-chicken example.
	document := documentWithoutTimings(t)

	document.Times = &recipe.Times{Prep: iso("PT25M"), Cook: iso("PT1H"), Additional: iso("PT8H15M"), Total: iso("PT9H40M"), Assertions: []recipe.TimingAssertion{
		{ID: "timing-11", Kind: recipe.TimingPrep, Duration: recipe.DurationExpression{Value: "PT25M"}},
		{ID: "timing-12", Kind: recipe.TimingBrine, Label: "Dry-brine", Duration: recipe.DurationExpression{Value: "PT8H"}},
		{ID: "timing-13", Kind: recipe.TimingCook, Duration: recipe.DurationExpression{Value: "PT1H"}},
		{ID: "timing-14", Kind: recipe.TimingRest, Duration: recipe.DurationExpression{Value: "PT15M"}},
	}}
	if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); err != nil {
		t.Fatalf("both statements of the clock should be valid: %v", err)
	}
	// Where they overlap the phases are the finer statement, so the summary
	// follows them rather than the coarse remainder.
	summary := recipe.SummarizeTimes(document)
	if summaryValues(summary)["active"] != "PT1H25M" || summaryValues(summary)["elapsed"] != "PT9H40M" {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestValidateAcceptsPhasesBesideAnAuthoredTotal(t *testing.T) {
	t.Parallel()
	document := documentWithoutTimings(t)

	document.Times = &recipe.Times{Total: iso("PT9H15M"), Assertions: []recipe.TimingAssertion{
		{ID: "timing-15", Kind: recipe.TimingMarinate, Duration: recipe.DurationExpression{Value: "PT8H"}},
		{ID: "timing-16", Kind: recipe.TimingCook, Duration: recipe.DurationExpression{Value: "PT45M"}},
	}}
	if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); err != nil {
		t.Fatalf("phases beside a total should be valid: %v", err)
	}
}

func TestValidateRequiresATimedPhaseToSaySomething(t *testing.T) {
	t.Parallel()
	document := documentWithoutTimings(t)
	document.Times = &recipe.Times{Assertions: []recipe.TimingAssertion{
		{ID: "timing-17", Kind: recipe.TimingOther, Duration: recipe.DurationExpression{Value: "PT1H"}},
		{ID: "timing-18", Kind: recipe.TimingRest},
		{Kind: "sitting about", Duration: recipe.DurationExpression{Value: "PT1H"}},
	}}

	violations := violationCodes(t, recipe.ValidateForWrite(document, units.DefaultCatalog()))
	for _, code := range []string{"timing_assertion.label_required", "duration.required", "timing_assertion.kind"} {
		if !violations[code] {
			t.Errorf("missing violation %q in %#v", code, violations)
		}
	}
}

func violationCodes(t *testing.T, err error) map[string]bool {
	t.Helper()

	codes := map[string]bool{}
	if err == nil {
		return codes
	}

	var validation *recipe.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %v, want a validation error", err)
	}

	for _, violation := range validation.Violations {
		codes[violation.Code] = true
	}

	return codes
}

// TestAcceptedDurationsAreExactlyTheUsableOnes keeps write validation from
// admitting a duration the application cannot then summarize or display.
func TestAcceptedDurationsAreExactlyTheUsableOnes(t *testing.T) {
	t.Parallel()

	accepted := []string{"PT20M", "PT8H", "P1DT2H30M", "P1W", "P2W3D", "PT90S", "P3M", "P1Y"}
	rejected := []string{
		"P",          // says nothing
		"PT",         // says nothing
		"PT0.5H",     // a fraction of an hour is expressible in minutes
		"P1,5D",      // the comma form of the same problem
		"P1YT",       // a time part with no time in it
		"45 minutes", // not a duration at all
		"PT1H30",     // an amount with no unit
	}

	for _, value := range accepted {
		document := testfixture.Document()

		document.Times = &recipe.Times{Assertions: []recipe.TimingAssertion{{ID: "cure", Kind: recipe.TimingCure, Duration: recipe.DurationExpression{Value: value}}}}
		if violations := violationCodes(t, recipe.ValidateForWrite(document, units.DefaultCatalog())); violations["duration.invalid"] {
			t.Errorf("duration %q should be accepted", value)
		}
	}

	for _, value := range rejected {
		document := testfixture.Document()

		document.Times = &recipe.Times{Assertions: []recipe.TimingAssertion{{ID: "cure", Kind: recipe.TimingCure, Duration: recipe.DurationExpression{Value: value}}}}
		if violations := violationCodes(t, recipe.ValidateForWrite(document, units.DefaultCatalog())); !violations["duration.invalid"] {
			t.Errorf("duration %q should be rejected", value)
		}
	}

	// Everything accepted either sums exactly or is deliberately unsummable,
	// and nothing accepted is silently treated as zero.
	for _, value := range accepted {
		_, summable := recipe.ParseDuration(value)

		calendarRelative := strings.ContainsAny(strings.TrimPrefix(value, "P"), "Y") ||
			strings.Contains(strings.SplitN(value, "T", 2)[0], "M")
		if summable == calendarRelative {
			t.Errorf("duration %q: summable=%t but calendar-relative=%t", value, summable, calendarRelative)
		}
	}
}

func TestSummarizeTimesSetsAsideATotalTheRecipeDisproves(t *testing.T) {
	t.Parallel()
	// A stated forty-minute total above a ninety-minute prove is preserved but
	// never reported as the elapsed time.
	document := recipe.Document{Times: &recipe.Times{Total: iso("PT40M"), Assertions: []recipe.TimingAssertion{
		{ID: "timing-19", Kind: recipe.TimingProof, Duration: recipe.DurationExpression{Value: "PT1H30M"}},
		{ID: "timing-20", Kind: recipe.TimingCook, Duration: recipe.DurationExpression{Value: "PT22M"}},
		// Hands-on work is stated as attention rather than a kind.
		{ID: "timing-21", Duration: recipe.DurationExpression{Value: "PT15M"}, Attention: recipe.AttentionActive},
	}}}

	got := recipe.SummarizeTimes(document)
	if summaryValues(got)["elapsed"] != "" {
		t.Fatalf("elapsed = %q, want no answer rather than a disproved one", summaryValues(got)["elapsed"])
	}

	if summaryValues(got)["written_total"] != "PT40M" {
		t.Fatalf("written total = %q, want the authored claim preserved", summaryValues(got)["written_total"])
	}

	if summaryValues(got)["active"] != "PT15M" {
		t.Fatalf("active = %q, want the working time the recipe does establish", summaryValues(got)["active"])
	}
}

func TestSummarizeTimesSetsAsideATotalBelowItsOwnCoarseClock(t *testing.T) {
	t.Parallel()

	document := recipe.Document{Times: &recipe.Times{Prep: iso("PT20M"), Cook: iso("PT30M"), Total: iso("PT40M")}}

	got := recipe.SummarizeTimes(document)
	if summaryValues(got)["elapsed"] != "PT50M" {
		t.Fatalf("elapsed = %q, want the coarse clock the total contradicts", summaryValues(got)["elapsed"])
	}

	if summaryValues(got)["written_total"] != "PT40M" {
		t.Fatalf("written total = %q, want the authored claim preserved", summaryValues(got)["written_total"])
	}
}

func TestTotalIsContradictedIgnoresOverlapAndCalendarDurations(t *testing.T) {
	t.Parallel()
	// Phases may overlap, so a total below their sum proves nothing, and a
	// month has no fixed length to compare against.
	overlapping := &recipe.Times{Total: iso("PT8H"), Assertions: []recipe.TimingAssertion{
		{ID: "timing-22", Kind: recipe.TimingProof, Duration: recipe.DurationExpression{Value: "PT8H"}},
		{ID: "timing-23", Kind: recipe.TimingPrep, Duration: recipe.DurationExpression{Value: "PT15M"}},
	}}
	if recipe.TotalIsContradicted(overlapping) {
		t.Error("overlapping phases must not disprove a stated total")
	}

	calendar := &recipe.Times{Total: iso("PT1H"), Assertions: []recipe.TimingAssertion{
		{ID: "timing-24", Kind: recipe.TimingCure, Duration: recipe.DurationExpression{Value: "P1M"}},
	}}
	if recipe.TotalIsContradicted(calendar) {
		t.Error("a calendar duration has no fixed length to compare")
	}
}

func TestSummarizeTimesWillNotAddUpAnElapsedTimeItsPhasesDisprove(t *testing.T) {
	t.Parallel()
	// With no authored total, prep plus cook (40 min) is disproved by a
	// ninety-minute prove and not reported.
	document := recipe.Document{Times: &recipe.Times{
		Prep: iso("PT15M"), Cook: iso("PT25M"),
		Assertions: []recipe.TimingAssertion{
			{ID: "timing-25", Kind: recipe.TimingProof, Duration: recipe.DurationExpression{Value: "PT1H30M"}, Provenance: recipe.TimingDerived},
			{ID: "timing-26", Kind: recipe.TimingCook, Label: "Bake", Attention: recipe.AttentionPassive, Duration: recipe.DurationExpression{Min: "PT22M", Max: "PT26M"}, Provenance: recipe.TimingDerived},
		},
	}}

	got := recipe.SummarizeTimes(document)
	if summaryValues(got)["elapsed"] != "" {
		t.Fatalf("elapsed = %q, want no claim rather than one the phases disprove", summaryValues(got)["elapsed"])
	}

	if summaryValues(got)["written_total"] != "" {
		t.Fatalf("written total = %q, want nothing: Savorum added this up rather than reading it", summaryValues(got)["written_total"])
	}
	// The working time is still true, and is still the useful thing to say.
	if summaryValues(got)["active"] != "PT40M" {
		t.Fatalf("active = %q, want the work the recipe does establish", summaryValues(got)["active"])
	}
}

func TestSummarizeTimesKeepsAnElapsedTimeItsPhasesFitInside(t *testing.T) {
	t.Parallel()

	document := recipe.Document{Times: &recipe.Times{
		Prep: iso("PT15M"), Cook: iso("PT45M"), Additional: iso("PT2H"),
		Assertions: []recipe.TimingAssertion{{ID: "timing-27", Kind: recipe.TimingRest, Duration: recipe.DurationExpression{Value: "PT10M"}}},
	}}
	if got := recipe.SummarizeTimes(document); summaryValues(got)["elapsed"] != "PT3H" {
		t.Fatalf("elapsed = %q, want the coarse clock every phase fits inside", summaryValues(got)["elapsed"])
	}
}

// iso builds an exact duration expression, which is what a test means when it
// writes a bare ISO string.
func iso(value string) *recipe.DurationExpression {
	return &recipe.DurationExpression{Value: value}
}

// summaryValues renders a summary as plain strings so a test can compare it
// without reaching through pointers on every field.
func summaryValues(summary recipe.TimeSummary) map[string]string {
	value := func(expression *recipe.DurationExpression) string {
		if expression == nil {
			return ""
		}

		if expression.IsOpaque() {
			return expression.Text
		}

		if expression.IsRange() {
			return expression.Min + "–" + expression.Max
		}

		return expression.Value
	}

	return map[string]string{
		"prep": value(summary.Prep), "cook": value(summary.Cook),
		"active": value(summary.Active), "elapsed": value(summary.Elapsed),
		"written_total": value(summary.WrittenTotal),
	}
}

// documentWithoutTimings is a valid recipe with its clock removed, including
// the step references that pointed into it.
func documentWithoutTimings(t *testing.T) recipe.Document {
	t.Helper()

	document := testfixture.Document()

	document.Times = nil
	for sectionIndex := range document.InstructionSections {
		for stepIndex := range document.InstructionSections[sectionIndex].Steps {
			document.InstructionSections[sectionIndex].Steps[stepIndex].TimingRefs = nil
		}
	}

	return document
}
