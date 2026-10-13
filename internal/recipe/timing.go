package recipe

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// summableDurationPattern accepts the ISO 8601 durations whose length is a
// fixed number of seconds. Years and months depend on a calendar position a
// recipe does not have, so a recipe using them keeps the value but is not
// summed into a total that would be a guess.
var summableDurationPattern = regexp.MustCompile(`^P(?:([0-9]+)W)?(?:([0-9]+)D)?(?:T(?:([0-9]+)H)?(?:([0-9]+)M)?(?:([0-9]+)S)?)?$`)

// TimingKinds lists the named kinds in the order a recipe usually passes
// through them. Presentation and editing both read this order.
var TimingKinds = []TimingKind{
	TimingPrep, TimingSoak, TimingBrine, TimingMarinate, TimingCure,
	TimingProof, TimingFerment, TimingChill, TimingFreeze, TimingDrain,
	TimingCook, TimingRest, TimingCool, TimingOther,
}

// IsTimingKind reports whether the value is one of the named kinds. An empty
// kind is not one of them: it is the valid absence of a classification, which
// callers check for separately rather than conflating with a bad value.
func IsTimingKind(kind TimingKind) bool {
	return slices.Contains(TimingKinds, kind)
}

// activeUnderSavorumPolicy is Savorum's presentation default, not SREF recipe
// semantics. SREF leaves an unstated attention unknown, and Savorum must not
// write its guess back into a document.
//
// Only preparation defaults to work: a `cook` assertion covers both a sauté
// the cook stands over and an oven they walk away from, so no default is right
// for it. Attention always wins. The coarse authored `cook` summary is still
// treated as work, whereas an assertion's kind may have been inferred.
func activeUnderSavorumPolicy(kind TimingKind) bool {
	return kind == TimingPrep
}

func (t TimingAssertion) activeUnderSavorumPolicy() bool {
	switch t.Attention {
	case AttentionActive:
		return true
	case AttentionPassive:
		return false
	}

	return activeUnderSavorumPolicy(t.Kind)
}

// ParseDuration reads an ISO 8601 duration whose length is exact. It reports
// false for an unparsable value, for calendar-relative years and months, and
// for a length beyond the range of time.Duration.
func ParseDuration(value string) (time.Duration, bool) {
	if value == "" || value == "P" {
		return 0, false
	}

	matches := summableDurationPattern.FindStringSubmatch(value)
	if matches == nil {
		return 0, false
	}

	units := []time.Duration{7 * 24 * time.Hour, 24 * time.Hour, time.Hour, time.Minute, time.Second}

	var (
		total   time.Duration
		present bool
	)

	for index, unit := range units {
		field := matches[index+1]
		if field == "" {
			continue
		}

		amount, err := strconv.ParseInt(field, 10, 64)
		if err != nil || amount > int64(math.MaxInt64-total)/int64(unit) {
			return 0, false
		}

		present = true
		total += time.Duration(amount) * unit
	}

	if !present {
		return 0, false
	}

	return total, true
}

// FormatDuration writes a duration in the canonical ISO 8601 form the document
// model stores. A zero or negative duration has no representation.
func FormatDuration(value time.Duration) string {
	if value <= 0 {
		return ""
	}

	days := int64(value / (24 * time.Hour))
	remainder := value % (24 * time.Hour)
	hours := int64(remainder / time.Hour)
	minutes := int64(remainder % time.Hour / time.Minute)
	seconds := int64(remainder % time.Minute / time.Second)

	result := "P"
	if days > 0 {
		result += fmt.Sprintf("%dD", days)
	}

	if hours == 0 && minutes == 0 && seconds == 0 {
		return result
	}

	result += "T"
	if hours > 0 {
		result += fmt.Sprintf("%dH", hours)
	}

	if minutes > 0 {
		result += fmt.Sprintf("%dM", minutes)
	}

	if seconds > 0 {
		result += fmt.Sprintf("%dS", seconds)
	}

	return result
}

// TimeSummary answers the two questions a cook asks about a recipe's clock:
// how much work it is, and how long it takes before it can be eaten. Every
// field is a derived ISO 8601 duration, and an empty field means the recipe
// does not say. Active and Elapsed differ exactly when a recipe waits.
type TimeSummary struct {
	Prep    *DurationExpression
	Cook    *DurationExpression
	Active  *DurationExpression
	Elapsed *DurationExpression
	// WrittenTotal is an authored total the recipe itself contradicts. It is
	// preserved exactly as written and reported separately from Elapsed, so a
	// headline never states a total the recipe disproves.
	WrittenTotal *DurationExpression
}

// FixedDuration returns the single fixed length an expression states. A range
// states no single length, opaque text states none at all, and a calendar
// duration has no fixed length to state, so each answers false.
func FixedDuration(expression *DurationExpression) (time.Duration, bool) {
	if expression == nil || expression.IsOpaque() || expression.IsRange() {
		return 0, false
	}

	return ParseDuration(expression.Value)
}

// exact builds the expression for a length Savorum calculated rather than read.
func exact(value time.Duration) *DurationExpression {
	return &DurationExpression{Value: FormatDuration(value)}
}

// TotalIsContradicted reports whether an authored total is demonstrably wrong
// rather than merely smaller than its assertions added up. They may overlap and
// need not be exhaustive, so a total below their sum proves nothing. A total
// below one single phase, or below the coarse clock it is meant to contain,
// proves the total cannot be the time the recipe takes. Calendar durations
// have no fixed length and are never compared.
func TotalIsContradicted(times *Times) bool {
	if times == nil {
		return false
	}

	total, ok := FixedDuration(times.Total)
	if !ok {
		return false
	}

	if coarse, ok := sumDurations(times.Prep, times.Cook, times.Additional); ok {
		if coarse > total {
			return true
		}
	}

	return assertionExceeds(times.Assertions, total, true)
}

// assertionExceeds reports whether one single timing assertion is longer than
// a claimed elapsed time, which disproves the claim whether a source wrote it
// or Savorum added it up.
func assertionExceeds(assertions []TimingAssertion, claim time.Duration, known bool) bool {
	if !known {
		return false
	}

	for _, assertion := range assertions {
		if duration, ok := FixedDuration(&assertion.Duration); ok && duration > claim {
			return true
		}
	}

	return false
}

// SummarizeTimes applies Savorum's presentation policy to the authored clock.
// Authored summary fields remain authoritative at their own level; assertions fill
// missing summaries and supply the active-time projection. SREF itself defines
// neither this precedence nor an active-time calculation.
func SummarizeTimes(document Document) TimeSummary {
	if document.Times == nil {
		return TimeSummary{}
	}

	times := document.Times
	elapsed, writtenTotal := elapsedTime(times)

	summary := TimeSummary{Prep: times.Prep, Cook: times.Cook, Elapsed: elapsed, WrittenTotal: writtenTotal}
	if len(times.Assertions) == 0 {
		return summarizeCoarseTimes(times, elapsed, writtenTotal)
	}

	projected := summarizeAssertions(times.Assertions)
	if summary.Prep == nil {
		summary.Prep = projected.Prep
	}

	if summary.Cook == nil {
		summary.Cook = projected.Cook
	}
	// A phase list may be partial. Use a phase's finer statement for prep or
	// cook when that kind is present, but retain a coarse prep/cook contribution
	// when the assertions say nothing about it. This is Savorum's display policy;
	// it does not manufacture an authored active value.
	activeValues := make([]*DurationExpression, 0, len(times.Assertions)+2)
	hasPrep, hasCook := false, false

	for index, assertion := range times.Assertions {
		if !assertion.activeUnderSavorumPolicy() {
			continue
		}

		if assertion.Provenance == TimingDerived && (times.Prep != nil || times.Cook != nil) {
			// A timing Savorum read out of the prose does not displace a
			// summary the recipe stated for itself.
			continue
		}
		// An assertion supersedes the coarse value for its kind only when it
		// contributes one; otherwise an unattended bake would erase the
		// authored cooking time.
		switch assertion.Kind {
		case TimingPrep:
			hasPrep = true
		case TimingCook:
			hasCook = true
		case TimingSoak, TimingBrine, TimingMarinate, TimingCure, TimingProof,
			TimingFerment, TimingChill, TimingFreeze, TimingDrain, TimingRest,
			TimingCool, TimingOther:
			// Only prep and cook have a coarse field to supersede; every other
			// kind contributes to the totals without replacing an authored one.
		}

		activeValues = append(activeValues, &times.Assertions[index].Duration)
	}

	if !hasPrep {
		activeValues = append(activeValues, times.Prep)
	}

	if !hasCook {
		activeValues = append(activeValues, times.Cook)
	}

	if active, ok := sumDurations(activeValues...); ok {
		summary.Active = exact(active)
	}

	return summary
}

// elapsedTime decides what a recipe may be said to take, and what it may not.
// The claim is the authored total when there is one and the coarse clock added
// up when there is not, and either is withdrawn once the recipe's own phases
// disprove it. Only an authored claim comes back as a written total. Nothing
// is estimated in its place, because phases may be partial or overlap.
func elapsedTime(times *Times) (elapsed, writtenTotal *DurationExpression) {
	claim := times.Total
	if claim != nil && TotalIsContradicted(times) {
		writtenTotal, claim = claim, nil
	}

	if claim == nil {
		if summed, ok := sumDurations(times.Prep, times.Cook, times.Additional); ok {
			claim = exact(summed)
		}
	}

	if fixed, ok := FixedDuration(claim); ok && assertionExceeds(times.Assertions, fixed, true) {
		claim = nil
	}

	return claim, writtenTotal
}

func summarizeAssertions(assertions []TimingAssertion) TimeSummary {
	var prep, cook, active time.Duration

	prepKnown, cookKnown, activeKnown := true, true, true

	var sawPrep, sawCook, sawActive bool

	for index := range assertions {
		assertion := assertions[index]

		duration, ok := FixedDuration(&assertion.Duration)
		if !ok {
			// A range or an opaque wait is a real length Savorum cannot add.
			// The projection it belongs to is withheld rather than reported
			// short, because an understated clock reads as a complete one.
			if assertion.activeUnderSavorumPolicy() {
				activeKnown = false
			}

			switch assertion.Kind {
			case TimingPrep:
				prepKnown = false
			case TimingCook:
				cookKnown = false
			case TimingSoak, TimingBrine, TimingMarinate, TimingCure, TimingProof,
				TimingFerment, TimingChill, TimingFreeze, TimingDrain, TimingRest,
				TimingCool, TimingOther:
				// Neither coarse total is stated by this kind, so neither is
				// made unreliable by not knowing its duration.
			}

			continue
		}

		if assertion.activeUnderSavorumPolicy() {
			active += duration
			sawActive = true
		}

		switch assertion.Kind {
		case TimingPrep:
			prep += duration
			sawPrep = true
		case TimingCook:
			cook += duration
			sawCook = true
		case TimingSoak, TimingBrine, TimingMarinate, TimingCure, TimingProof,
			TimingFerment, TimingChill, TimingFreeze, TimingDrain, TimingRest,
			TimingCool, TimingOther:
			// Counted in the active and total figures above, but not reported
			// as prep or cook, which are the only two kinds a recipe states
			// coarsely.
		}
	}

	summary := TimeSummary{}
	if sawPrep && prepKnown {
		summary.Prep = exact(prep)
	}

	if sawCook && cookKnown {
		summary.Cook = exact(cook)
	}

	if sawActive && activeKnown {
		summary.Active = exact(active)
	}

	return summary
}

func summarizeCoarseTimes(times *Times, total, writtenTotal *DurationExpression) TimeSummary {
	if times == nil {
		return TimeSummary{}
	}

	summary := TimeSummary{Prep: times.Prep, Cook: times.Cook, Elapsed: total, WrittenTotal: writtenTotal}
	// Preparing and cooking are the parts of a coarse summary that need the
	// cook. Additional time is the unnamed remainder, which is waiting.
	if active, ok := sumDurations(times.Prep, times.Cook); ok {
		summary.Active = exact(active)
	}

	return summary
}

// sumDurations totals the values that are present. It reports false when none
// is present or when one of them cannot be added exactly.
func sumDurations(values ...*DurationExpression) (time.Duration, bool) {
	var (
		total   time.Duration
		present bool
	)

	for _, value := range values {
		if value == nil || value.IsZero() {
			continue
		}

		duration, ok := FixedDuration(value)
		if !ok {
			return 0, false
		}

		present = true
		total += duration
	}

	if !present {
		return 0, false
	}

	return total, true
}
