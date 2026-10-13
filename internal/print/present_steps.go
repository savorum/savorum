package print

import (
	"strconv"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/recipe"
)

func (p presenter) presentSteps(document recipe.Document) []PrintStepGroup {
	groups := make([]PrintStepGroup, 0, len(document.InstructionSections))
	phases := stepPhases(document)

	for _, section := range document.InstructionSections {
		group := PrintStepGroup{Title: strings.TrimSpace(section.Title)}
		for index, step := range section.Steps {
			group.Steps = append(group.Steps, p.presentStep(step, index+1, phases[step.ID]))
		}

		groups = append(groups, group)
	}

	return groups
}

func stepPhases(document recipe.Document) map[string][]recipe.TimingAssertion {
	phases := make(map[string][]recipe.TimingAssertion)
	if document.Times == nil {
		return phases
	}

	byID := make(map[string]recipe.TimingAssertion, len(document.Times.Assertions))
	for _, assertion := range document.Times.Assertions {
		byID[assertion.ID] = assertion
	}

	for _, section := range document.InstructionSections {
		for _, step := range section.Steps {
			for _, ref := range step.TimingRefs {
				if assertion, found := byID[ref]; found {
					phases[step.ID] = append(phases[step.ID], assertion)
				}
			}
		}
	}

	return phases
}

func (p presenter) presentStep(step recipe.Step, number int, phases []recipe.TimingAssertion) PrintStep {
	var supplemental []string

	for _, phase := range phases {
		if p.microwaveOutputWatts > 0 && phase.Microwave != nil {
			estimate := recipe.EstimateMicrowaveTime(phase, p.microwaveOutputWatts)
			if estimate.Applicable {
				words := durationExpressionWords(estimate.Estimated, "", p.locale)
				if !estimate.AuthoredChoice && catalogLocale(p.locale) == "en" {
					words = strings.TrimPrefix(words, printMessage("about", p.locale))
					words = "About " + words
				}

				supplemental = append(supplemental, strings.NewReplacer(
					"{estimated}", words, "{target}", p.localizeNumber(strconv.Itoa(estimate.TargetWatts)),
					"{original}", durationExpressionWords(estimate.Original, phase.Note, p.locale),
					"{source}", p.localizeNumber(strconv.Itoa(estimate.SourceWatts)),
				).Replace(p.label("microwave_estimate")))
			}
		}

		if !phaseAddsInformation(step, phase) {
			continue
		}

		label := phaseLabel(phase, p.locale)
		if duration := phaseDurationWords(phase, p.locale); duration != "" {
			label += " " + duration
		}

		supplemental = append(supplemental, label)
	}

	return PrintStep{
		Number: number, Title: visibleStepTitle(step.Title, step.Text),
		Text:   strings.TrimSpace(step.Text),
		Phases: supplemental, NumberText: p.localizeNumber(strconv.Itoa(number)),
	}
}

func visibleStepTitle(title, text string) string {
	title = strings.TrimSpace(title)

	text = strings.TrimSpace(text)
	if title == "" || strings.EqualFold(title, text) || strings.HasPrefix(strings.ToLower(text), strings.ToLower(title)) {
		return ""
	}

	return title
}

// phaseAddsInformation reports whether a phase says something the instruction
// does not. A phase whose span falls inside what the prose states for the same
// kind of work ("Bake 6 to 8 minutes" vs. eight minutes) is already stated.
func phaseAddsInformation(step recipe.Step, phase recipe.TimingAssertion) bool {
	if phase.Provenance == recipe.TimingDerived {
		return false
	}

	for _, derived := range recipe.ExtractTimings(step) {
		if derived.Kind != phase.Kind && phase.Provenance != "" {
			continue
		}

		if timingSpanContains(derived, phase) {
			return false
		}
	}

	return !textStatesRange(step.Text, phase)
}

// digitZeroes holds the first code point of each decimal digit run, so figures
// in any of these scripts can be read.
var digitZeroes = []rune{
	0x30, 0x660, 0x6f0, 0x7c0, 0x966, 0x9e6, 0xa66, 0xae6, 0xb66, 0xbe6, 0xc66, 0xce6, 0xd66, 0xe50,
	0xed0, 0xf20, 0x1040, 0xff10,
}

// figuresIn returns the whole numbers the text writes in decimal digits.
func figuresIn(text string) map[int]bool {
	figures := map[int]bool{}
	current, inNumber := 0, false

	flush := func() {
		if inNumber {
			figures[current] = true
		}

		current, inNumber = 0, false
	}

	for _, char := range text {
		value := -1

		for _, zero := range digitZeroes {
			if char >= zero && char < zero+10 {
				value = int(char - zero)

				break
			}
		}

		if value < 0 || current > 1_000_000 {
			flush()

			continue
		}

		current, inNumber = current*10+value, true
	}

	flush()

	return figures
}

// textStatesRange reports whether the instruction already gives both ends of a
// range in figures, for wording the timing extractor does not read.
func textStatesRange(text string, phase recipe.TimingAssertion) bool {
	low, lowOK := recipe.ParseDuration(spanStart(phase.Duration))

	high, highOK := recipe.ParseDuration(phase.Duration.Max)
	if !lowOK || !highOK {
		return false
	}

	figures := figuresIn(text)
	stated := func(duration time.Duration) bool {
		minutes := int(duration / time.Minute)

		return figures[minutes] || (minutes%60 == 0 && figures[minutes/60])
	}

	return stated(low) && stated(high)
}

// timingSpanContains reports whether everything the phase claims lies within
// the span the instruction already gives.
func timingSpanContains(outer, inner recipe.TimingAssertion) bool {
	low, ok := recipe.ParseDuration(spanStart(outer.Duration))
	if !ok {
		return false
	}

	high := low
	if value, ok := recipe.ParseDuration(outer.Duration.Max); ok && value > high {
		high = value
	}

	within := func(value string) bool {
		duration, ok := recipe.ParseDuration(value)
		return ok && duration >= low && duration <= high
	}
	if !within(spanStart(inner.Duration)) {
		return false
	}

	return inner.Duration.Max == "" || within(inner.Duration.Max)
}

// referencedAssertions collects the assertions some instruction points at.
func referencedAssertions(document recipe.Document) map[string]bool {
	referenced := make(map[string]bool)

	for _, section := range document.InstructionSections {
		for _, step := range section.Steps {
			for _, ref := range step.TimingRefs {
				referenced[ref] = true
			}
		}
	}

	return referenced
}
