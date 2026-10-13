package print

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
)

// presentTimings writes the recipe's clock the way a cook plans with it: how
// much work it is, and how long it takes before it can be eaten. Named phases
// follow, because "marinate 8 hours" is planning information a summary hides.
func (p presenter) presentTimings(document recipe.Document) []PrintTiming {
	summary := recipe.SummarizeTimes(document)

	var timings []PrintTiming

	active := summaryWords(summary.Active, p.locale)

	elapsed := summaryWords(summary.Elapsed, p.locale)
	switch {
	case active != "" && elapsed != "" && active != elapsed:
		timings = append(timings, PrintTiming{Label: p.label("active"), Value: active})
		timings = append(timings, PrintTiming{Label: p.label("total"), Value: elapsed})
	case elapsed != "":
		timings = append(timings, PrintTiming{Label: p.label("total"), Value: elapsed})
	case active != "":
		timings = append(timings, PrintTiming{Label: p.label("active"), Value: active})
	default:
		if prep := summaryWords(summary.Prep, p.locale); prep != "" {
			timings = append(timings, PrintTiming{Label: p.label("phase_prep"), Value: prep})
		}

		if cook := summaryWords(summary.Cook, p.locale); cook != "" {
			timings = append(timings, PrintTiming{Label: p.label("phase_cook"), Value: cook})
		}
	}

	if document.Times == nil {
		return timings
	}

	referenced := referencedAssertions(document)
	for _, phase := range document.Times.Assertions {
		// An assertion a step points at is printed with that instruction, not
		// again in the recipe's own clock. One timing fact, shown once.
		if referenced[phase.ID] {
			continue
		}

		value := phaseDurationWords(phase, p.locale)
		if value == "" {
			continue
		}

		timings = append(timings, PrintTiming{Label: phaseLabel(phase, p.locale), Value: value})
	}
	// A contradicted total is quoted, never printed as the time taken.
	if written := summaryWords(summary.WrittenTotal, p.locale); written != "" {
		timings = append(timings, PrintTiming{Label: p.label("written_total"), Value: written})
	}

	return timings
}

func phaseDurationWords(phase recipe.TimingAssertion, locales ...string) string {
	return durationExpressionWords(phase.Duration, phase.Note, locales...)
}

// durationExpressionWords sets one duration for a reader. Opaque wording is
// printed as the recipe wrote it: "overnight" is the whole of what the source
// said, and there is nothing to convert.
func durationExpressionWords(expression recipe.DurationExpression, note string, locales ...string) string {
	if expression.IsOpaque() {
		return expression.Text
	}

	minimum := durationWords(expression.Value, locales...)
	if expression.IsRange() {
		minimum = durationWords(expression.Min, locales...)
	}

	maximum := durationWords(expression.Max, locales...)

	if minimum == "" {
		return ""
	}

	value := minimum
	if maximum != "" && maximum != minimum {
		value += "–" + maximum
	}

	if expression.Approximate {
		value = printMessage("about", locales...) + value
	}

	if strings.TrimSpace(note) == "at least" {
		value = printMessage("at_least", locales...) + value
	}

	return value
}

// durationPattern is the whole duration vocabulary Savorum accepts, which is
// narrower than ISO 8601 allows: whole units only.
var durationPattern = regexp.MustCompile(`^P(?:([0-9]+)Y)?(?:([0-9]+)M)?(?:([0-9]+)W)?(?:([0-9]+)D)?(?:T(?:([0-9]+)H)?(?:([0-9]+)M)?(?:([0-9]+)S)?)?$`)

var durationNames = []string{"year", "month", "week", "day", "hour", "minute", "second"}

// durationWords writes a stored duration the way it is read aloud. A value the
// model would not have accepted is printed as it stands rather than discarded.
func durationWords(value string, locales ...string) string {
	locale := "en"
	if len(locales) > 0 {
		locale = locales[0]
	}

	p := presenter{locale: locale}

	if value == "" {
		return ""
	}

	matches := durationPattern.FindStringSubmatch(value)
	if matches == nil {
		return value
	}

	amounts := make([]uint64, len(durationNames))
	for index := range durationNames {
		if field := matches[index+1]; field != "" {
			amount, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return value
			}

			amounts[index] = amount
		}
	}
	// Seconds and minutes past the next unit are carried into it, so 375
	// minutes reads as 6 hours 15 minutes.
	amounts[5] += amounts[6] / 60
	amounts[6] %= 60
	amounts[4] += amounts[5] / 60
	amounts[5] %= 60

	parts := make([]string, 0, len(durationNames))
	for index, name := range durationNames {
		amount := amounts[index]
		if amount == 0 {
			continue
		}

		key := "duration_" + name + "_many"
		if amount == 1 {
			key = "duration_" + name + "_one"
		}

		parts = append(parts, p.localizeNumber(strconv.FormatUint(amount, 10))+" "+printMessage(key, locales...))
	}

	return strings.Join(parts, " ")
}

func phaseLabel(phase recipe.TimingAssertion, locales ...string) string {
	if label := strings.TrimSpace(phase.Label); label != "" {
		return label
	}

	if name := printMessage("phase_"+string(phase.Kind), locales...); name != "" {
		return name
	}

	return printMessage("phase_other", locales...)
}

// spanStart is the earliest length a duration expression states.
func spanStart(expression recipe.DurationExpression) string {
	if expression.Min != "" {
		return expression.Min
	}

	return expression.Value
}

// summaryWords sets a summary duration for a reader, keeping a range or an
// opaque wait as what the recipe said rather than reducing it to a number.
func summaryWords(expression *recipe.DurationExpression, locales ...string) string {
	if expression == nil {
		return ""
	}

	return durationExpressionWords(*expression, "", locales...)
}
