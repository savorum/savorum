package recipe

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/recipetext"
)

var (
	durationAmountPattern = `(?:\d+\s+\d+\s*/\s*\d+|\d+\s*/\s*\d+|\d+(?:[.,]\d+)|\d*\s*[¼½¾⅐⅑⅒⅓⅔⅕⅖⅗⅘⅙⅚⅛⅜⅝⅞]|\d+)`
	// The connector is optional ("Bake 6 to 8 minutes"); the action verb is
	// not, so a bare number is never read as a duration.
	timedActionPattern          = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_À-ÖØ-öø-ÿ])(knead|whisk|stir|beat|mix|fold|shape|roll|chop|slice|saute|sauté|sear|fry|bake|roast|grill|broil|simmer|boil|cook|microwave|rest|rise|proof|prove|marinate|brine|soak|chill|refrigerate|freeze|cool|ferment|cure|drain)(?:$|[^A-Za-z0-9_À-ÖØ-öø-ÿ])[^.!?;\n]{0,80}?(?:\b(for|about|approximately|around|at least)\s+)?(` + durationAmountPattern + `)\s*(?:(?:-|–|—|to)\s*(` + durationAmountPattern + `)\s*)?(seconds?|secs?|minutes?|mins?|hours?|hrs?|days?)\b`)
	afterPattern                = regexp.MustCompile(`(?i)\bafter\s+(about\s+|approximately\s+|around\s+)?(` + durationAmountPattern + `)\s*(?:(?:-|–|—|to)\s*(` + durationAmountPattern + `)\s*)?(seconds?|secs?|minutes?|mins?|hours?|hrs?|days?)\b`)
	microwaveAlternativePattern = regexp.MustCompile(`(?i)([0-9][0-9,.]*)\s*(k?W|watts?)\s*:\s*(` + durationAmountPattern + `)\s*(seconds?|secs?|minutes?|mins?|hours?|hrs?)`)
	microwaveRatedPattern       = regexp.MustCompile(`(?i)([0-9][0-9,.]*)\s*(k?W|watts?)\s+(?:microwave|oven)`)
	microwaveSelectedPattern    = regexp.MustCompile(`(?i)(?:at|on)\s+([0-9][0-9,.]*)\s*(k?W|watts?)\b`)
	microwavePercentPattern     = regexp.MustCompile(`(?i)([0-9]{1,3})\s*%\s*(?:power)?`)
)

var vulgarDurationFractions = map[rune]*big.Rat{
	'¼': big.NewRat(1, 4), '½': big.NewRat(1, 2), '¾': big.NewRat(3, 4),
	'⅐': big.NewRat(1, 7), '⅑': big.NewRat(1, 9), '⅒': big.NewRat(1, 10),
	'⅓': big.NewRat(1, 3), '⅔': big.NewRat(2, 3),
	'⅕': big.NewRat(1, 5), '⅖': big.NewRat(2, 5), '⅗': big.NewRat(3, 5), '⅘': big.NewRat(4, 5),
	'⅙': big.NewRat(1, 6), '⅚': big.NewRat(5, 6),
	'⅛': big.NewRat(1, 8), '⅜': big.NewRat(3, 8), '⅝': big.NewRat(5, 8), '⅞': big.NewRat(7, 8),
}

// ExtractTimings conservatively recognizes timing whose meaning is explicit
// in one instruction. Cadences such as “stir every 5 minutes” are deliberately
// left as prose.
func ExtractTimings(step Step) []TimingAssertion {
	text := strings.TrimSpace(step.Text)
	if text == "" {
		return nil
	}

	if alternative := extractMicrowaveAlternatives(text); alternative != nil {
		return []TimingAssertion{*alternative}
	}

	result := extractActionTimings(text)

	result = extractElapsedTimings(text, result)
	for _, found := range recipetext.Timings(text) {
		timing := TimingAssertion{
			Kind: TimingKind(found.Kind), Provenance: TimingDerived,
			Duration: DurationExpression{Value: durationForAmount(found.Amount, found.Unit), SourceText: found.Source},
		}
		if timing.Duration.Value == "" {
			continue
		}

		if found.Maximum != "" {
			timing.Duration.Min, timing.Duration.Max = timing.Duration.Value, durationForAmount(found.Maximum, found.Unit)
			if timing.Duration.Max == "" {
				continue
			}

			timing.Duration.Value = ""
		}

		if !sameExtractedTiming(result, timing) {
			result = append(result, timing)
		}
	}

	return result
}

func extractActionTimings(text string) []TimingAssertion {
	var result []TimingAssertion

	for _, match := range timedActionPattern.FindAllStringSubmatchIndex(text, -1) {
		whole := text[match[0]:match[1]]
		if cadenceNear(text, match[0], match[1]) {
			continue
		}

		action := text[match[2]:match[3]]

		qualifier := ""
		if match[4] >= 0 {
			qualifier = strings.ToLower(text[match[4]:match[5]])
		}

		minimum := text[match[6]:match[7]]

		maximum := ""
		if match[8] >= 0 {
			maximum = text[match[8]:match[9]]
		}

		unit := text[match[10]:match[11]]
		kind, label := timingKindForAction(action)

		phase := TimingAssertion{
			Kind:  kind,
			Label: label,
			Duration: DurationExpression{
				Value:       durationForAmount(minimum, unit),
				Approximate: qualifier == "about" || qualifier == "approximately" || qualifier == "around",
				SourceText:  sourceExcerpt(whole),
			},
			Provenance: TimingDerived,
		}
		if strings.EqualFold(action, "microwave") {
			phase.Microwave = microwaveConditionsFromText(microwaveClause(text, match[0], match[1]))
		}
		// Attention is authored semantics, so a derived timing leaves it unset.
		if maximum != "" {
			phase.Duration.Min = phase.Duration.Value
			phase.Duration.Value = ""
			phase.Duration.Max = durationForAmount(maximum, unit)
		}

		if qualifier == "at least" {
			phase.Note = "at least"
		}

		if !phase.Duration.IsZero() && !sameExtractedTiming(result, phase) {
			result = append(result, phase)
		}
	}

	return result
}

func extractElapsedTimings(text string, result []TimingAssertion) []TimingAssertion {
	// “After 15 minutes” is an elapsed interval, but only when it was not
	// already part of a more specific action match.
	for _, match := range afterPattern.FindAllStringSubmatchIndex(text, -1) {
		whole := text[match[0]:match[1]]
		if cadenceNear(text, match[0], match[1]) || overlapsSource(result, whole) {
			continue
		}

		minimum := text[match[4]:match[5]]

		maximum := ""
		if match[6] >= 0 {
			maximum = text[match[6]:match[7]]
		}

		unit := text[match[8]:match[9]]

		phase := TimingAssertion{
			Kind:       TimingOther,
			Label:      "Wait",
			Duration:   DurationExpression{Value: durationForAmount(minimum, unit), SourceText: whole},
			Provenance: TimingDerived,
		}
		if match[2] >= 0 {
			phase.Duration.Approximate = true
		}

		if maximum != "" {
			phase.Duration.Min = phase.Duration.Value
			phase.Duration.Value = ""
			phase.Duration.Max = durationForAmount(maximum, unit)
		}

		if !phase.Duration.IsZero() {
			result = append(result, phase)
		}
	}

	return result
}

func sameExtractedTiming(entries []TimingAssertion, candidate TimingAssertion) bool {
	for _, entry := range entries {
		if entry.Duration.Value == candidate.Duration.Value && entry.Duration.Min == candidate.Duration.Min && entry.Duration.Max == candidate.Duration.Max && strings.EqualFold(strings.TrimSpace(entry.Duration.SourceText), strings.TrimSpace(candidate.Duration.SourceText)) {
			return true
		}
	}

	return false
}

func sourceExcerpt(value string) string {
	lower := strings.ToLower(value)
	for _, marker := range []string{"for ", "about ", "approximately ", "around ", "at least "} {
		if index := strings.LastIndex(lower, marker); index >= 0 {
			return strings.TrimSpace(value[index:])
		}
	}

	return strings.TrimSpace(value)
}

func cadenceNear(text string, start, end int) bool {
	left := max(start-16, 0)
	return strings.Contains(strings.ToLower(text[left:end]), "every ")
}

func overlapsSource(entries []TimingAssertion, source string) bool {
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Duration.SourceText), strings.ToLower(source)) || strings.Contains(strings.ToLower(source), strings.ToLower(entry.Duration.SourceText)) {
			return true
		}
	}

	return false
}

// timingKindForAction maps a recipe's verb onto SREF's closed kind vocabulary
// and returns the label that keeps the verb's own wording. Baking and
// simmering are cooking; the distinction the verb makes survives in the label
// rather than in a kind SREF does not define.
func timingKindForAction(action string) (TimingKind, string) {
	switch strings.ToLower(action) {
	case "bake", "roast", "grill", "broil":
		return TimingCook, "Bake"
	case "simmer", "boil":
		return TimingCook, "Simmer"
	case "cook", "saute", "sauté", "sear", "fry":
		return TimingCook, ""
	case "microwave":
		return TimingCook, "Microwave"
	case "rest":
		return TimingRest, ""
	case "rise", "proof", "prove":
		return TimingProof, ""
	case "marinate":
		return TimingMarinate, ""
	case "brine":
		return TimingBrine, ""
	case "soak":
		return TimingSoak, ""
	case "chill", "refrigerate":
		return TimingChill, ""
	case "freeze":
		return TimingFreeze, ""
	case "cool":
		return TimingCool, ""
	case "ferment":
		return TimingFerment, ""
	case "cure":
		return TimingCure, ""
	case "drain":
		return TimingDrain, ""
	default:
		// Unclassified verbs state a duration without a kind, which SREF permits
		// rather than defaulting to "other".
		return "", ""
	}
}

func extractMicrowaveAlternatives(text string) *TimingAssertion {
	if !strings.Contains(strings.ToLower(text), "microwave") {
		return nil
	}

	matches := microwaveAlternativePattern.FindAllStringSubmatch(text, -1)
	if len(matches) < 2 {
		return nil
	}

	choices := make([]MicrowaveChoice, 0, len(matches))
	for _, match := range matches {
		watts, ok := parseWatts(match[1], match[2])

		duration := durationForAmount(match[3], match[4])
		if !ok || duration == "" {
			return nil
		}

		choices = append(choices, MicrowaveChoice{
			Duration: DurationExpression{Value: duration, SourceText: match[0]},
			Power:    &MicrowavePower{Watts: watts}, SourceText: match[0],
		})
	}

	return &TimingAssertion{Kind: TimingCook, Label: "Microwave", Duration: DurationExpression{Text: text}, Microwave: &MicrowaveConditions{SourceText: text, Choices: choices}, Provenance: TimingDerived}
}

func microwaveConditionsFromText(text string) *MicrowaveConditions {
	conditions := &MicrowaveConditions{SourceText: strings.TrimSpace(text)}
	if match := microwaveRatedPattern.FindStringSubmatch(text); match != nil {
		conditions.RatedOutputWatts, _ = parseWatts(match[1], match[2])
	}

	if match := microwaveSelectedPattern.FindStringSubmatch(text); match != nil {
		if watts, ok := parseWatts(match[1], match[2]); ok {
			conditions.Power = &MicrowavePower{Watts: watts}
		}
	}

	if match := microwavePercentPattern.FindStringSubmatch(text); match != nil {
		if percent, err := strconv.Atoi(match[1]); err == nil && percent > 0 && percent <= 100 {
			conditions.Power = &MicrowavePower{Percent: strconv.Itoa(percent)}
		}
	} else {
		lower := strings.ToLower(text)
		switch {
		case strings.Contains(lower, "half power"):
			conditions.Power = &MicrowavePower{Percent: "50"}
		case strings.Contains(lower, "on high") || strings.Contains(lower, "high power"):
			conditions.Power = &MicrowavePower{Percent: "100"}
		case strings.Contains(lower, "medium"):
			conditions.Power = &MicrowavePower{Text: "medium"}
		case regexp.MustCompile(`\bp[0-9]+\b`).MatchString(lower):
			conditions.Power = &MicrowavePower{Text: regexp.MustCompile(`\bp[0-9]+\b`).FindString(lower)}
		}

		if strings.Contains(lower, "defrost") {
			conditions.ModeText = "defrost"
		}
	}

	return conditions
}

func microwaveClause(text string, start, end int) string {
	left := start
	for left > 0 && !strings.ContainsRune(";,.\n", rune(text[left-1])) {
		left--
	}

	right := end
	for right < len(text) && !strings.ContainsRune(";,.\n", rune(text[right])) {
		right++
	}

	return strings.TrimSpace(text[left:right])
}

func parseWatts(amount, unit string) (int, bool) {
	normalized := strings.ReplaceAll(amount, ",", "")

	value, err := strconv.ParseFloat(normalized, 64)
	if err != nil || value <= 0 {
		return 0, false
	}

	if strings.EqualFold(unit, "kw") {
		value *= 1000
	}

	if value > 1_000_000 {
		return 0, false
	}

	return int(math.Round(value)), true
}

func durationForAmount(value, unit string) string {
	amount, ok := parseDurationAmount(value)
	if !ok || amount.Sign() <= 0 {
		return ""
	}

	var seconds int64

	switch strings.ToLower(unit) {
	case "second", "seconds", "sec", "secs":
		seconds = 1
	case "minute", "minutes", "min", "mins":
		seconds = 60
	case "hour", "hours", "hr", "hrs":
		seconds = 60 * 60
	case "day", "days":
		seconds = 24 * 60 * 60
	default:
		return ""
	}

	scaled := new(big.Rat).Mul(amount, big.NewRat(seconds, 1))
	whole, remainder := new(big.Int), new(big.Int)
	whole.QuoRem(scaled.Num(), scaled.Denom(), remainder)
	// Stored durations have whole-second precision. Round subsecond prose to
	// the nearest second instead of silently shortening it.
	if new(big.Int).Lsh(remainder, 1).Cmp(scaled.Denom()) >= 0 {
		whole.Add(whole, big.NewInt(1))
	}

	if !whole.IsInt64() || whole.Sign() <= 0 {
		return ""
	}

	const maxDurationSeconds = int64(^uint64(0)>>1) / int64(time.Second)
	if whole.Cmp(big.NewInt(maxDurationSeconds)) > 0 {
		return ""
	}

	return FormatDuration(time.Duration(whole.Int64()) * time.Second)
}

func parseDurationAmount(value string) (*big.Rat, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", "."))
	if value == "" {
		return nil, false
	}

	runes := []rune(value)
	if fraction, found := vulgarDurationFractions[runes[len(runes)-1]]; found {
		wholeText := strings.TrimSpace(string(runes[:len(runes)-1]))

		whole := new(big.Rat)
		if wholeText != "" {
			if _, ok := whole.SetString(wholeText); !ok {
				return nil, false
			}
		}

		return new(big.Rat).Add(whole, fraction), true
	}

	fields := strings.Fields(value)
	if len(fields) == 2 {
		whole, fractional := new(big.Rat), new(big.Rat)
		if _, ok := whole.SetString(fields[0]); !ok {
			return nil, false
		}

		if _, ok := fractional.SetString(strings.ReplaceAll(fields[1], " ", "")); !ok {
			return nil, false
		}

		return new(big.Rat).Add(whole, fractional), true
	}

	amount := new(big.Rat)
	parsed, ok := amount.SetString(strings.ReplaceAll(value, " ", ""))

	return parsed, ok
}

// ReconcileTimings re-evaluates derived entries from the current prose while
// preserving independently authored timing.
//
// Derived assertions are rebuilt rather than edited, so every reference to one
// is withdrawn from its step before extraction runs again. Authored assertions
// and their references are left alone.
//
// The argument is left untouched: Document shares its section slices and
// Times with the caller, so both are copied first.
func ReconcileTimings(document Document) Document {
	document.InstructionSections = copiedInstructionSections(document.InstructionSections)
	globalMicrowaveWatts, globalMicrowaveSource := recipeWideMicrowaveBasis(document)

	times := Times{}
	if document.Times != nil {
		times = *document.Times
	}

	document.Times = &times

	kept := make([]TimingAssertion, 0, len(document.Times.Assertions))
	derivedIDs := make(map[string]bool)
	used := make(map[string]bool)

	for _, assertion := range document.Times.Assertions {
		if assertion.Provenance == TimingDerived {
			derivedIDs[assertion.ID] = true
			continue
		}

		if assertion.Provenance == "" {
			assertion.Provenance = TimingSupplemental
		}

		used[assertion.ID] = true
		kept = append(kept, assertion)
	}

	next := 1
	allocate := func() string {
		for {
			candidate := fmt.Sprintf("timing-%d", next)
			next++

			if !used[candidate] {
				used[candidate] = true
				return candidate
			}
		}
	}

	for sectionIndex := range document.InstructionSections {
		for stepIndex := range document.InstructionSections[sectionIndex].Steps {
			step := &document.InstructionSections[sectionIndex].Steps[stepIndex]
			kept = reconcileStepTimings(step, document, kept, derivedIDs, used, allocate, globalMicrowaveWatts, globalMicrowaveSource)
		}
	}

	document.Times.Assertions = kept
	if document.Times.Prep == nil && document.Times.Cook == nil && document.Times.Additional == nil && document.Times.Total == nil && len(kept) == 0 {
		document.Times = nil
	}

	return document
}

func reconcileStepTimings(step *Step, document Document, kept []TimingAssertion, derivedIDs, used map[string]bool, allocate func() string, globalMicrowaveWatts int, globalMicrowaveSource string) []TimingAssertion {
	step.TimingRefs = withoutRefs(step.TimingRefs, derivedIDs)
	for _, reference := range step.TimingRefs {
		used[reference] = true
	}

	for _, extracted := range ExtractTimings(*step) {
		if extracted.Microwave != nil && extracted.Microwave.RatedOutputWatts == 0 && globalMicrowaveWatts > 0 {
			extracted.Microwave.RatedOutputWatts = globalMicrowaveWatts
			extracted.Microwave.SourceText = globalMicrowaveSource + " · " + extracted.Microwave.SourceText
		}

		if timingOverridden(kept, extracted, step.ID, document) {
			continue
		}

		extracted.ID = allocate()
		kept = append(kept, extracted)
		step.TimingRefs = append(step.TimingRefs, extracted.ID)
	}

	return kept
}

func recipeWideMicrowaveBasis(document Document) (int, string) {
	candidates := append([]string(nil), document.Notes...)
	for _, equipment := range document.Equipment {
		if strings.Contains(strings.ToLower(equipment.Name+" "+equipment.Note), "microwave") {
			candidates = append(candidates, strings.TrimSpace(equipment.Name+" "+equipment.Note))
		}
	}

	for _, candidate := range candidates {
		lower := strings.ToLower(candidate)
		if !strings.Contains(lower, "microwave") {
			continue
		}

		if match := microwaveRatedPattern.FindStringSubmatch(candidate); match != nil {
			if watts, ok := parseWatts(match[1], match[2]); ok {
				return watts, candidate
			}
		}
	}

	return 0, ""
}

// copiedInstructionSections detaches the steps this function rewrites from the
// arrays the caller still holds. Only the parts ReconcileTimings writes are
// copied; everything else in a step is shared, because nothing here changes it.
func copiedInstructionSections(sections []InstructionSection) []InstructionSection {
	if len(sections) == 0 {
		return sections
	}

	copied := append([]InstructionSection(nil), sections...)
	for sectionIndex := range copied {
		copied[sectionIndex].Steps = append([]Step(nil), copied[sectionIndex].Steps...)
		for stepIndex := range copied[sectionIndex].Steps {
			step := &copied[sectionIndex].Steps[stepIndex]
			step.TimingRefs = append([]string(nil), step.TimingRefs...)
		}
	}

	return copied
}

// withoutRefs drops references naming removed assertions and repeated
// references, which the library refuses.
func withoutRefs(refs []string, removed map[string]bool) []string {
	if len(refs) == 0 {
		return refs
	}

	seen := make(map[string]bool, len(refs))

	remaining := make([]string, 0, len(refs))
	for _, ref := range refs {
		if removed[ref] || seen[ref] {
			continue
		}

		seen[ref] = true

		remaining = append(remaining, ref)
	}

	if len(remaining) == 0 {
		return nil
	}

	return remaining
}

// timingOverridden reports whether an authored assertion the step already
// refers to says what the extraction just found. Only assertions this step
// points at are compared: the same five minutes stated by two different steps
// is two events, and collapsing them would lose one.
func timingOverridden(entries []TimingAssertion, derived TimingAssertion, stepID string, document Document) bool {
	referenced := stepAssertionRefs(document, stepID)
	for _, entry := range entries {
		if !referenced[entry.ID] {
			continue
		}

		if entry.Duration.SourceText != "" && strings.EqualFold(strings.TrimSpace(entry.Duration.SourceText), strings.TrimSpace(derived.Duration.SourceText)) {
			return true
		}

		if entry.Kind == derived.Kind && entry.Duration.Value == derived.Duration.Value && entry.Duration.Min == derived.Duration.Min && entry.Duration.Max == derived.Duration.Max {
			return true
		}
	}

	return false
}

// stepAssertionRefs collects the assertion IDs one step points at.
func stepAssertionRefs(document Document, stepID string) map[string]bool {
	refs := make(map[string]bool)

	for _, section := range document.InstructionSections {
		for _, step := range section.Steps {
			if step.ID != stepID {
				continue
			}

			for _, ref := range step.TimingRefs {
				refs[ref] = true
			}
		}
	}

	return refs
}
