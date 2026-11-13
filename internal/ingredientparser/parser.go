// Package ingredientparser extracts structured quantities while preserving authored ingredient text.
package ingredientparser

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

// amountPattern is a whole number, a fraction, a mixed number, or a decimal.
// A decimal comma takes one or two digits, so "1,500" is never read as 1.5.
const amountPattern = `(?:\d+\s+)?(?:\d+/\d+)|\d+(?:\.\d+|,\d{1,2})?`

var (
	attachedUnit   = regexp.MustCompile(`^(` + amountPattern + `)(\p{L})`)
	hyphenatedUnit = regexp.MustCompile(`^(` + amountPattern + `)-(\p{L})`)
)

const unresolvedWarning = "Savorum could not safely identify every amount and unit in this ingredient. The original line was preserved for review."

// Ambiguity kinds explain which ingredient interpretation requires review.
const (
	// ParserVersion invalidates resolution plans; bump it when parsing behavior changes.
	ParserVersion      = "9"
	AmbiguityUnitAlias = "unit_alias"
)

// Ambiguity identifies an unresolved measurement eligible for import review.
type Ambiguity struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	AuthoredForm     string   `json:"authored_form"`
	SourceStart      int      `json:"source_start"`
	SourceEnd        int      `json:"source_end"`
	CandidateUnitIDs []string `json:"candidate_unit_ids"`
	ResolutionKey    string   `json:"resolution_key"`
	OccurrenceKey    string   `json:"occurrence_key"`
}

// ParseResult contains the parsed ingredient and any ambiguities found.
type ParseResult struct {
	Ingredient  recipe.Ingredient
	Ambiguities []Ambiguity
}

var unicodeFractions = strings.NewReplacer(
	"½", " 1/2", "⅓", " 1/3", "⅔", " 2/3", "¼", " 1/4", "¾", " 3/4",
	"⅛", " 1/8", "⅜", " 3/8", "⅝", " 5/8", "⅞", " 7/8",
)

var catalog = units.DefaultCatalog()

// Parse extracts unambiguous quantities and optionality, preserving the original line in SourceText.
func Parse(line, id, sourceLocale string) recipe.Ingredient {
	return ParseDetailed(line, id, sourceLocale).Ingredient
}

// AliasLanguage is the fallback language for sources without a declared locale.
const AliasLanguage = "en"

func readingLocale(sourceLocale string) string {
	if tag, err := language.Parse(strings.TrimSpace(sourceLocale)); err != nil || tag.IsRoot() {
		return AliasLanguage
	}

	return sourceLocale
}

// ParseDetailed returns the parsed ingredient and its resolvable ambiguities.
func ParseDetailed(line, id, sourceLocale string) ParseResult {
	return ParseWithContext(line, id, sourceLocale, ResolutionContext{})
}

// ResolutionContext holds occurrence-specific decisions and an ingredient-wide convention.
type ResolutionContext struct {
	Interpretations map[string]string
	Convention      *units.ConventionProfile
	DeclaredLocale  string
}

func (c ResolutionContext) empty() bool {
	return len(c.Interpretations) == 0 && c.Convention == nil
}

// ParseWithContext reparses the source using explicit measurement decisions.
// Unsupported decisions leave the ingredient unresolved and available for review.
func ParseWithContext(line, id, sourceLocale string, context ResolutionContext) ParseResult {
	sourceLocale = readingLocale(sourceLocale)
	written := strings.TrimSpace(line)
	source := norm.NFC.String(written)
	stated, optional := splitAuthoredOptional(tidyLine(source), sourceLocale)

	discovered := aliasAmbiguities(stated, id, source, sourceLocale)

	read := reading{locale: sourceLocale, convention: context.Convention}
	if len(context.Interpretations) != 0 {
		permitted := make(map[string]string, len(context.Interpretations))

		for _, ambiguity := range discovered {
			unitID := context.Interpretations[ambiguity.OccurrenceKey]
			if unitID != "" && catalog.ResolveAliasAs(ambiguity.AuthoredForm, unitID) {
				permitted[ambiguity.OccurrenceKey] = unitID
			}
		}

		read.interpretations = permitted
	}

	parsed := parseStatedAs(stated, id, source, read)
	parsed.SourceText = written
	parsed.Optional = optional

	unresolved := parsed.Normalization != nil && parsed.Normalization.Method == recipe.NormalizationUnresolved
	if unresolved || context.empty() {
		result := ParseResult{Ingredient: parsed}
		if unresolved {
			result.Ambiguities = outstanding(discovered, read)
		}

		return result
	}

	parsed.Normalization = &recipe.Normalization{
		Method: recipe.NormalizationInferred, Review: recipe.ReviewCorrected,
		Warnings: []string{interpretationProvenance(line, id, sourceLocale, context)},
	}

	return ParseResult{Ingredient: parsed}
}

func outstanding(discovered []Ambiguity, read reading) []Ambiguity {
	if read.interpretations == nil && read.convention == nil {
		return discovered
	}

	remaining := make([]Ambiguity, 0, len(discovered))

	for _, ambiguity := range discovered {
		descriptor, found := catalog.DescribeAlias(ambiguity.AuthoredForm)
		if !found {
			remaining = append(remaining, ambiguity)
			continue
		}

		decisions := units.Decisions{
			Interpretation: read.interpretations[ambiguity.OccurrenceKey],
			Convention:     read.convention,
		}

		// Resolve with the supplied decisions alone; locale-resolved measures are absent from discovery.
		if _, resolution := catalog.ResolveOccurrence(
			units.Occurrence{Form: descriptor.AuthoredForm}, "", decisions,
		); resolution == units.AliasResolved {
			continue
		}

		remaining = append(remaining, ambiguity)
	}

	return remaining
}

func interpretationProvenance(line, id, sourceLocale string, context ResolutionContext) string {
	scope := "It was chosen for this measurement."
	if context.Convention != nil {
		scope = "It was chosen for every applicable measure of this ingredient."
	}

	detail := "The source did not identify a regional measurement convention."

	if declared := strings.TrimSpace(context.DeclaredLocale); declared != "" {
		alone := ParseWithContext(line, id, sourceLocale, ResolutionContext{DeclaredLocale: declared})
		if alone.Ingredient.Quantity != nil {
			detail = fmt.Sprintf("The source states the locale %s, which reads it differently.", declared)
		} else {
			detail = fmt.Sprintf("The source states the locale %s, which does not identify it.", declared)
		}
	}

	return recipe.ImportReviewUnitPrefix + " " + detail + " " + scope
}

// WithoutOptional removes an optional-ingredient marker from text.
func WithoutOptional(text, locale string) string {
	stated := dialectFor(locale).optional.ReplaceAllString(text, "")

	return strings.TrimSpace(strings.Trim(strings.TrimSpace(stated), ",;，、；-–—"))
}

// Extract explicit optionality markers, preserving qualifiers such as "to taste" and "for serving".
func splitAuthoredOptional(line, locale string) (string, bool) {
	if !dialectFor(locale).optional.MatchString(line) {
		return line, false
	}

	stated := WithoutOptional(line, locale)
	if stated == "" {
		return line, false
	}

	return stated, true
}

// Require whitespace after list dashes to preserve negative quantities.
var listMarker = regexp.MustCompile(`^(?:[-*•·–—]\s+|[・●▪◦■◆►▶✓✔]\s*)`)

var bidiMarks = strings.NewReplacer("\u200e", "", "\u200f", "", "\u202a", "", "\u202b", "", "\u202c", "",
	"\u202d", "", "\u202e", "", "\u2066", "", "\u2067", "", "\u2068", "", "\u2069", "")

var dashedNumber = regexp.MustCompile(`^[-–—](\pN)`)

func tidyLine(line string) string {
	line = dashedNumber.ReplaceAllString(strings.TrimSpace(bidiMarks.Replace(line)), "$1")
	line = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(listMarker.ReplaceAllString(line, "")), ";,；，"))

	if trimmed, found := strings.CutSuffix(line, "."); found {
		word := trimmed[strings.LastIndexAny(trimmed, " ,")+1:]
		if utf8.RuneCountInString(word) >= 4 && !strings.Contains(word, ".") && unicode.IsLetter([]rune(word)[len([]rune(word))-1]) {
			return trimmed
		}
	}

	return line
}

type lineReading int

const (
	lineMeasured lineReading = iota
	lineUnmeasured
	lineUnresolved
	lineAsWritten
)

func readLeadingAmount(original string, grammar *dialect, read reading) (recipe.Amount, string, string, lineReading) {
	working, isApproximate := grammar.takeApproximate(original)

	working, partNote, limited := grammar.rewriteLine(working)
	if limited {
		return recipe.Amount{}, "", "", lineUnresolved
	}

	working = normalizeAmounts(working)
	if grammar.mixedNumber != nil {
		working = grammar.mixedNumber.ReplaceAllString(working, "$1 $2")
	}

	working, approximatedBefore := grammar.reorder(working, read)
	isApproximate = isApproximate || approximatedBefore

	if !grammar.amount.MatchString(working) {
		working = grammar.spellNumber(working, read)
	}

	match := grammar.amount.FindStringSubmatch(working)
	if len(match) != 4 {
		if _, share := splitNote(original); beginsAmount(working) || percentageOnly.MatchString(share) {
			return recipe.Amount{}, "", "", lineUnresolved
		}

		return recipe.Amount{}, "", "", lineUnmeasured
	}

	minimum, ok := rational(match[1])
	remainder := strings.TrimSpace(match[3])

	if !ok || remainder == "" {
		return recipe.Amount{}, "", "", lineAsWritten
	}

	amount := recipe.Amount{Approximate: isApproximate, Value: &minimum}

	if match[2] != "" {
		maximum, valid := rational(match[2])
		if !valid {
			return recipe.Amount{}, "", "", lineAsWritten
		}

		amount.Value, amount.Min, amount.Max = nil, &minimum, &maximum
	}

	amount, remainder = grammar.multiply(amount, cutMultiplier(remainder))

	remainder = grammar.cutConnectiveBeforeUnit(remainder, read)
	if !isWholeCount(amount) {
		remainder = grammar.cutFractionOf(remainder)
	}

	return amount, remainder, partNote, lineMeasured
}

func (d *dialect) cutConnectiveBeforeUnit(remainder string, read reading) string {
	if d.connective == nil {
		return remainder
	}

	after := strings.TrimSpace(d.connective.ReplaceAllString(remainder, ""))
	if after == remainder || after == "" {
		return remainder
	}

	probe := read
	probe.walk, probe.discovering = nil, false

	if _, _, resolution := leadingUnitAs(after, probe); resolution != units.AliasUnknown {
		return after
	}

	return remainder
}

func parseStatedAs(line, id, source string, read reading) recipe.Ingredient {
	if read.walk == nil {
		read.walk = &aliasWalk{}
	}

	original := strings.TrimSpace(line)
	grammar := dialectFor(read.locale)

	fallback := recipe.Ingredient{ID: id, Name: original, SourceText: source}
	if original == "" {
		return fallback
	}

	if line, vague := readVague(original, fallback, grammar, read); vague {
		return line
	}

	amount, remainder, partNote, outcome := readLeadingAmount(original, grammar, read)
	switch outcome {
	case lineMeasured:
	case lineUnresolved:
		return unresolved(fallback)
	case lineUnmeasured:
		fallback.Name, fallback.Note = grammar.splitUnmeasured(original)

		return fallback
	case lineAsWritten:
		return fallback
	}

	modifier, unitID, name, recognizedUnit, ambiguous := readMeasure(remainder, grammar, read)
	// Preserve separate ingredients joined by a plus sign.
	if ambiguous || recognizedUnit && grammar.subtracts(name) || secondMeasure.MatchString(name) {
		return unresolved(fallback)
	}

	if !recognizedUnit {
		if line, decided := readWithoutUnit(fallback, amount, remainder, partNote, grammar, read); decided {
			return line
		}
	}

	var packageSize *recipe.SimpleQuantity
	if recognizedUnit {
		amount, unitID, name, packageSize = sizedMeasure(amount, unitID, name, read)

		if word, rest, found := cutAnyWord(name, grammar.distributive); found && word != "" {
			name, modifier = rest, joinNotes([]string{modifier, word})
		}
	}

	first := recipe.SimpleQuantity{Kind: recipe.QuantitySimple, Amount: amount, Unit: unitID}
	primaryTerms := []recipe.SimpleQuantity{first}

	if recognizedUnit {
		var valid bool

		primaryTerms, name, valid = appendAdditiveTerms(primaryTerms, name, read)
		if !valid {
			return unresolved(fallback)
		}
	}

	primary := optionFromTerms(primaryTerms)
	quantity := quantityFromOption(primary)

	if recognizedUnit {
		var readable bool

		if quantity, name, readable = withAlternative(quantity, primary, name, read); !readable {
			return unresolved(fallback)
		}
	}

	var aside []string
	if recognizedUnit {
		if quantity, name, aside = withAside(quantity, primary, name, read); statesAnotherMeasure(quantity, read) {
			return unresolved(fallback)
		}
	}

	name, note := nameAfterMeasure(name, read)
	if name == "" {
		return fallback
	}

	note = joinNotes(append(append([]string{partNote, modifier}, aside...), note))

	// Ounces are ambiguous between mass and volume for liquids.
	if unitID == "mass.ounce.avoirdupois" && grammar.liquid(name) {
		return unresolved(fallback)
	}

	if describesAnotherCup(unitID, original, grammar) {
		return unresolved(fallback)
	}

	return finishCount(recipe.Ingredient{
		ID: id, Name: name, Note: note, SourceText: source,
		Quantity:      quantity,
		PackageSize:   packageSize,
		Normalization: &recipe.Normalization{Method: recipe.NormalizationExtracted},
	}, nil, read)
}

func withAlternative(
	quantity *recipe.Quantity, primary recipe.AlternativeOption, name string, read reading,
) (*recipe.Quantity, string, bool) {
	next, rest, found := alternative(name, read)
	if !found {
		return quantity, name, !beginsAlternative(name, read)
	}

	terms, remainder, valid := appendAdditiveTerms([]recipe.SimpleQuantity{next}, rest, read)
	if !valid {
		return quantity, name, false
	}

	stated := &recipe.Quantity{
		Kind:    recipe.QuantityAlternatives,
		Options: []recipe.AlternativeOption{primary, optionFromTerms(terms)},
	}

	return stated, remainder, !statesAnotherMeasure(stated, read)
}

func readMeasure(value string, grammar *dialect, read reading) (modifier, unitID, rest string, recognized, ambiguous bool) {
	if word, measured := grammar.takeModifier(value); word != "" {
		if unitID, rest, recognized, ambiguous = readLeadingUnit(measured, grammar, read); recognized || ambiguous {
			return word, unitID, rest, recognized, ambiguous
		}
	}

	if word, measured, sized := grammar.cutSize(value); sized {
		unit, after, found, _ := readLeadingUnit(measured, grammar, read)
		if found && (containers(unit) || !strings.Contains(word, "-")) {
			return word, unit, after, true, false
		}
	}

	unitID, rest, recognized, ambiguous = readLeadingUnit(value, grammar, read)
	if recognized {
		modifier, rest = grammar.takeModifierAfter(rest)
	}

	return modifier, unitID, rest, recognized, ambiguous
}

func joinNotes(parts []string) string {
	return strings.Join(slices.DeleteFunc(parts, func(part string) bool { return part == "" }), "; ")
}

func withAside(
	quantity *recipe.Quantity, primary recipe.AlternativeOption, name string, read reading,
) (*recipe.Quantity, string, []string) {
	if quantity.Kind == recipe.QuantityAlternatives {
		return quantity, name, nil
	}

	// Leave trailing count sizes for finishCount: "1 wheel Brie (8 oz)".
	trailingIsSize := primary.Kind == recipe.QuantitySimple && counts(primary.Unit)

	inner, rest, leading := strings.Cut(strings.TrimPrefix(name, "("), ")")
	if !strings.HasPrefix(name, "(") || !leading || !beginsAmount(inner) {
		if trailingIsSize {
			return quantity, name, nil
		}

		body, note := splitNote(name)

		open := strings.LastIndex(body, "(")
		if open <= 0 || !strings.HasSuffix(body, ")") {
			return quantity, name, nil
		}

		measures, words := readAside(body[open+1:len(body)-1], read)
		if len(measures) == 0 || len(words) > 0 {
			return quantity, name, nil
		}

		rest := strings.TrimSpace(body[:open])
		if note != "" {
			rest += ", " + note
		}

		return withOptions(primary, measures), rest, nil
	}

	measures, words := readAside(inner, read)
	if len(measures) == 0 {
		return quantity, strings.TrimSpace(rest), []string{strings.TrimSpace(inner)}
	}

	return withOptions(primary, measures), strings.TrimSpace(rest), words
}

func withOptions(primary recipe.AlternativeOption, measures []recipe.SimpleQuantity) *recipe.Quantity {
	options := []recipe.AlternativeOption{primary}
	for _, measure := range measures {
		options = append(options, optionFromTerms([]recipe.SimpleQuantity{measure}))
	}

	return &recipe.Quantity{Kind: recipe.QuantityAlternatives, Options: options}
}

func readAside(inner string, read reading) ([]recipe.SimpleQuantity, []string) {
	var measures []recipe.SimpleQuantity

	var words []string

	for _, part := range asideParts(inner) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if term, rest, found := followingMeasure(part+" "+placeholder, read); found && rest == placeholder {
			measures = append(measures, term)
		} else {
			words = append(words, part)
		}
	}

	return measures, words
}

// Split equivalent measures at slashes, preserving fractional amounts.
func asideParts(inner string) []string {
	var parts []string

	start := 0

	for index, character := range inner {
		if character != '/' {
			continue
		}

		before, _ := utf8.DecodeLastRuneInString(inner[:index])
		after, _ := utf8.DecodeRuneInString(inner[index+1:])

		if !unicode.IsDigit(before) || !unicode.IsDigit(after) {
			parts = append(parts, inner[start:index])
			start = index + 1
		}
	}

	return append(parts, inner[start:])
}

func readLeadingUnit(value string, grammar *dialect, read reading) (string, string, bool, bool) {
	if unit, rest, found := grammar.senseOf(value); found {
		return unit, rest, true, false
	}

	unit, rest, resolution := leadingUnitAs(value, read)
	if resolution == units.AliasResolved && !namesIngredient[unit] {
		return unit, rest, true, false
	}

	return itemUnit, value, false, resolution == units.AliasAmbiguous
}

func describesAnotherCup(unitID, line string, grammar *dialect) bool {
	return strings.HasPrefix(unitID, "volume.cup.") && grammar.describesOpaqueMeasure(line)
}

func appendAdditiveTerms(terms []recipe.SimpleQuantity, name string, read reading) ([]recipe.SimpleQuantity, string, bool) {
	for {
		term, remainder, found := additive(name, read)
		if !found {
			term, remainder, found = adjacentTerm(terms[len(terms)-1], name, read)
		}

		if !found {
			return terms, name, !beginsAdditive(name, read)
		}

		terms = append(terms, term)
		name = remainder
	}
}

var bareDecimal = regexp.MustCompile(`(^|\s)\.(\d)`)

func normalizeAmounts(value string) string {
	working := strings.Join(strings.Fields(unicodeFractions.Replace(asciiDigits(value))), " ")
	working = bareDecimal.ReplaceAllString(working, "${1}0.$2")
	working = hyphenatedUnit.ReplaceAllString(working, "$1 $2")

	return attachedUnit.ReplaceAllString(working, "$1 $2")
}

func cutMultiplier(value string) string {
	for _, sign := range []string{"x", "X", "×"} {
		if after, found := strings.CutPrefix(value, sign); found {
			if after = strings.TrimSpace(after); beginsAmount(after) {
				return after
			}
		}
	}

	return value
}

func beginsAmount(value string) bool {
	first, _ := utf8.DecodeRuneInString(value)

	return unicode.IsDigit(first)
}

func unresolved(ingredient recipe.Ingredient) recipe.Ingredient {
	ingredient.Normalization = &recipe.Normalization{
		Method:   recipe.NormalizationUnresolved,
		Warnings: []string{unresolvedWarning},
	}

	return ingredient
}

func readUnitless(
	id, source string, fallback recipe.Ingredient, amount recipe.Amount, remainder string, read reading,
) (recipe.Ingredient, bool) {
	if line, packaged := readParenthesizedPackage(id, source, fallback, amount, remainder, read); packaged {
		return line, true
	}

	if line, counted := readCountedMeasure(id, source, unresolved(fallback), amount, remainder, read); counted {
		return line, true
	}

	// Without language-specific aliases, a bare number may precede an unrecognized unit.
	if !catalog.HasAliasesFor(readingLocale(read.locale)) && !catalog.HasAliasesFor(cmp.Or(grammarLanguage(read.locale), read.locale)) {
		return unresolved(fallback), true
	}

	return recipe.Ingredient{}, false
}

func containers(unitID string) bool {
	unit, found := catalog.Lookup(unitID)

	return found && unit.Kind == recipe.UnitContainer
}

const itemUnit = "count.item"

// Absorb a leading one only when no container or item-size qualifier is stated.
// The flag reports a handled line, including unresolved input.
func readCountedMeasure(
	id, source string, fallback recipe.Ingredient, count recipe.Amount, remainder string, read reading,
) (recipe.Ingredient, bool) {
	inner, rest, found := alternative(remainder, read)
	if !found {
		if beginsAmount(strings.TrimSpace(remainder)) {
			return fallback, true
		}

		return recipe.Ingredient{}, false
	}

	if second, after, stated := alternative(rest, read); stated && !beginsAmount(strings.TrimSpace(rest)) {
		if isOne(count) {
			name, note := nameAfterMeasure(after, read)
			if name == "" {
				return fallback, true
			}

			return recipe.Ingredient{
				ID: id, Name: name, Note: note, SourceText: source,
				Quantity: &recipe.Quantity{Kind: recipe.QuantityAlternatives, Options: []recipe.AlternativeOption{
					{Kind: recipe.QuantitySimple, Amount: &inner.Amount, Unit: inner.Unit},
					{Kind: recipe.QuantitySimple, Amount: &second.Amount, Unit: second.Unit},
				}},
				Normalization: &recipe.Normalization{Method: recipe.NormalizationExtracted},
			}, true
		}

		if !sameSize(inner, second) {
			return fallback, true
		}

		rest = after
	}

	if container, tail, resolution := leadingUnitAs(rest, read); resolution == units.AliasResolved && strings.TrimSpace(tail) != "" {
		if counts(container) && container != itemUnit {
			name, note := nameAfterMeasure(strings.TrimSpace(tail), read)
			if name == "" {
				return fallback, true
			}

			return recipe.Ingredient{
				ID: id, Name: name, Note: note, SourceText: source,
				Quantity:      &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &count, Unit: container},
				PackageSize:   &inner,
				Normalization: &recipe.Normalization{Method: recipe.NormalizationExtracted},
			}, true
		}
	}

	name, note := nameAfterMeasure(rest, read)
	if name == "" {
		return fallback, true
	}

	// Lengths and explicit count words indicate item size, even when the count is one.
	sized := isLength(inner.Unit) || dialectFor(read.locale).beginsCountWord(rest, read)

	if !isOne(count) || sized {
		// Fractional item counts require an explicit size qualifier.
		if !isWholeCount(count) && !sized {
			return fallback, true
		}

		return recipe.Ingredient{
			ID: id, Name: name, Note: note, SourceText: source,
			Quantity:      &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &count, Unit: itemUnit},
			PackageSize:   &inner,
			Normalization: &recipe.Normalization{Method: recipe.NormalizationExtracted},
		}, true
	}

	return recipe.Ingredient{
		ID: id, Name: name, Note: note, SourceText: source,
		Quantity:      &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &inner.Amount, Unit: inner.Unit},
		Normalization: &recipe.Normalization{Method: recipe.NormalizationExtracted},
	}, true
}

func readParenthesizedPackage(
	id, source string, fallback recipe.Ingredient, count recipe.Amount, remainder string, read reading,
) (recipe.Ingredient, bool) {
	remainder = strings.TrimSpace(remainder)
	if !strings.HasPrefix(remainder, "(") {
		return recipe.Ingredient{}, false
	}

	closing := strings.Index(remainder, ")")
	if closing <= 1 {
		return unresolved(fallback), true
	}

	inside := strings.TrimSpace(remainder[1:closing])

	tail := strings.TrimSpace(remainder[closing+1:])
	if tail == "" {
		return unresolved(fallback), true
	}

	packageSize, marker, found := followingMeasure(inside+" ingredient", read)
	if !found || marker != "ingredient" {
		return unresolved(fallback), true
	}

	unitID, name := itemUnit, tail
	if container, rest, resolution := leadingUnitAs(tail, read); resolution == units.AliasResolved && containers(container) {
		unitID, name = container, rest
	}

	name, note := nameAfterMeasure(name, read)
	if name == "" {
		return unresolved(fallback), true
	}

	return recipe.Ingredient{
		ID: id, Name: name, Note: note, SourceText: source,
		Quantity:      &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &count, Unit: unitID},
		PackageSize:   &packageSize,
		Normalization: &recipe.Normalization{Method: recipe.NormalizationExtracted},
	}, true
}

func sameSize(first, second recipe.SimpleQuantity) bool {
	if first.Amount.Value == nil || second.Amount.Value == nil {
		return false
	}

	converted, err := catalog.Convert(*second.Amount.Value, second.Unit, first.Unit)
	if err != nil {
		return false
	}

	size, ok := new(big.Rat).SetString(string(*first.Amount.Value))
	if !ok || size.Sign() <= 0 {
		return false
	}

	difference := new(big.Rat).Sub(size, converted.Rat())
	difference.Abs(difference).Mul(difference, big.NewRat(10, 1))

	return difference.Cmp(size) <= 0
}

func isOne(amount recipe.Amount) bool {
	return amount.Value != nil && *amount.Value == "1" && amount.Min == nil && amount.Max == nil
}

func isWholeCount(amount recipe.Amount) bool {
	if amount.Value == nil || amount.Min != nil || amount.Max != nil {
		return false
	}

	return !strings.ContainsAny(string(*amount.Value), "/.")
}

// Require a complete amount and registered unit to avoid splitting slashes in ingredient names.
func alternative(value string, read reading) (recipe.SimpleQuantity, string, bool) {
	working := strings.TrimSpace(value)
	if strings.HasPrefix(working, "(") {
		return parenthesizedEquivalent(working, read)
	}

	if after, ok := strings.CutPrefix(working, "/"); ok {
		working = strings.TrimSpace(after)
	} else if after, ok := dialectFor(read.locale).cutOr(working); ok {
		working = after
	} else if !beginsAmount(working) {
		return recipe.SimpleQuantity{}, "", false
	}

	return followingMeasure(working, read)
}

// Sentinel ingredient name used to require a complete parenthesized measure.
const placeholder = "\x00"

func parenthesizedEquivalent(value string, read reading) (recipe.SimpleQuantity, string, bool) {
	end := strings.Index(value, ")")
	if end < 0 {
		return recipe.SimpleQuantity{}, "", false
	}

	term, rest, found := followingMeasure(strings.TrimSpace(value[1:end])+" "+placeholder, read)
	if !found || rest != placeholder {
		return recipe.SimpleQuantity{}, "", false
	}

	return term, strings.TrimSpace(value[end+1:]), true
}

// An equivalent differing by more than 10% indicates a source-specific measure.
func statesAnotherMeasure(quantity *recipe.Quantity, read reading) bool {
	if quantity.Kind != recipe.QuantityAlternatives || len(quantity.Options) != 2 ||
		read.convention != nil || len(read.interpretations) != 0 {
		return false
	}

	sizes := make([]*big.Rat, 0, len(quantity.Options))

	for _, option := range quantity.Options {
		if option.Kind != recipe.QuantitySimple || option.Amount == nil || option.Amount.Value == nil {
			return false
		}

		household := strings.HasPrefix(option.Unit, "volume.cup.") ||
			strings.HasPrefix(option.Unit, "volume.tablespoon.") || strings.HasPrefix(option.Unit, "volume.teaspoon.")
		if !household && option.Unit != "volume.milliliter" {
			return false
		}

		converted, err := catalog.Convert(*option.Amount.Value, option.Unit, "volume.milliliter")
		if err != nil {
			return false
		}

		sizes = append(sizes, converted.Rat())
	}

	difference := new(big.Rat).Sub(sizes[0], sizes[1])
	difference.Abs(difference).Mul(difference, big.NewRat(10, 1))

	return difference.Cmp(sizes[0]) > 0 && difference.Cmp(sizes[1]) > 0
}

func beginsAlternative(value string, read reading) bool {
	working := strings.TrimSpace(value)
	_, orWord := dialectFor(read.locale).cutOr(working)

	return strings.HasPrefix(working, "/") || orWord || beginsAmount(working) && !percentage.MatchString(working)
}

var percentage = regexp.MustCompile(`^\d+(?:[.,]\d+)?\s*%`)

func additive(value string, read reading) (recipe.SimpleQuantity, string, bool) {
	working := strings.TrimSpace(value)
	switch {
	case strings.HasPrefix(working, "+"):
		working = strings.TrimSpace(working[1:])
	default:
		after, ok := dialectFor(read.locale).cutPlus(working)
		if !ok {
			return recipe.SimpleQuantity{}, "", false
		}

		working = after
	}

	return followingMeasure(working, read)
}

func adjacentTerm(last recipe.SimpleQuantity, value string, read reading) (recipe.SimpleQuantity, string, bool) {
	if !beginsAmount(strings.TrimSpace(value)) {
		return recipe.SimpleQuantity{}, "", false
	}

	term, rest, found := followingMeasure(value, read)
	if !found {
		return recipe.SimpleQuantity{}, "", false
	}

	converted, err := catalog.Convert("1", term.Unit, last.Unit)
	if err != nil || converted.Rat().Cmp(big.NewRat(1, 1)) >= 0 {
		return recipe.SimpleQuantity{}, "", false
	}

	return term, rest, true
}

func readWithoutUnit(
	fallback recipe.Ingredient, amount recipe.Amount, remainder, partNote string, grammar *dialect, read reading,
) (recipe.Ingredient, bool) {
	if _, _, vague := cutAnyWord(remainder, grammar.vague); vague {
		return unresolved(fallback), true
	}

	if beginsAdditive(remainder, read) || namesUnitAlone(remainder, read) || grammar.isOpaqueMeasure(grammar.skipQualifiers(remainder)) ||
		grammar.qualifiedPortion(remainder, read) {
		return unresolved(fallback), true
	}

	line, decided := readUnitless(fallback.ID, fallback.SourceText, fallback, amount, remainder, read)
	if !decided {
		return recipe.Ingredient{}, false
	}

	return finishCount(line, []string{partNote}, read), true
}

func readVague(original string, fallback recipe.Ingredient, grammar *dialect, read reading) (recipe.Ingredient, bool) {
	amount, rest, vague := grammar.vagueAmount(original, read)
	if !vague {
		return recipe.Ingredient{}, false
	}

	name, note := nameAfterMeasure(rest, read)
	if name == "" {
		return fallback, true
	}

	fallback.Name, fallback.Note = name, note
	fallback.Quantity = &recipe.Quantity{Kind: recipe.QuantityOpaque, Text: amount}
	fallback.Normalization = &recipe.Normalization{Method: recipe.NormalizationExtracted}

	return fallback, true
}

func sizedMeasure(
	amount recipe.Amount, unitID, name string, read reading,
) (recipe.Amount, string, string, *recipe.SimpleQuantity) {
	if counts(unitID) {
		size, rest := leadingSize(name, read)

		return amount, unitID, rest, size
	}

	container, rest, resolution := leadingUnitAs(name, read)
	if resolution != units.AliasResolved || !containers(container) {
		return amount, unitID, name, nil
	}

	size := recipe.SimpleQuantity{Kind: recipe.QuantitySimple, Amount: amount, Unit: unitID}
	one := recipe.Rational("1")

	return recipe.Amount{Value: &one}, container, rest, &size
}

// MeasureOnly reports a standalone measure that may continue on the next text line.
func MeasureOnly(line, locale string) bool {
	match := dialectFor(locale).amount.FindStringSubmatch(normalizeAmounts(strings.TrimSpace(line)))

	return len(match) == 4 && namesUnitAlone(match[3], reading{locale: locale, walk: &aliasWalk{}})
}

func namesUnitAlone(value string, read reading) bool {
	unit, rest, resolution := leadingUnitAs(strings.TrimSpace(value)+" "+placeholder, read)

	return resolution == units.AliasResolved && rest == placeholder && !namesIngredient[unit]
}

var secondMeasure = regexp.MustCompile(`\S\s*\+\s*\pN`)

func isLength(unitID string) bool {
	unit, found := catalog.Lookup(unitID)

	return found && unit.Dimension == recipe.DimensionLength
}

func beginsAdditive(value string, read reading) bool {
	working := strings.TrimSpace(value)
	_, plusWord := dialectFor(read.locale).cutPlus(working)

	return strings.HasPrefix(working, "+") || plusWord
}

func followingMeasure(working string, read reading) (recipe.SimpleQuantity, string, bool) {
	working = normalizeAmounts(working)

	match := dialectFor(read.locale).amount.FindStringSubmatch(working)
	if len(match) != 4 {
		return recipe.SimpleQuantity{}, "", false
	}

	minimum, ok := rational(match[1])
	if !ok {
		return recipe.SimpleQuantity{}, "", false
	}

	amount := recipe.Amount{}
	if match[2] == "" {
		amount.Value = &minimum
	} else {
		maximum, valid := rational(match[2])
		if !valid {
			return recipe.SimpleQuantity{}, "", false
		}

		amount.Min, amount.Max = &minimum, &maximum
	}

	unitID, rest, resolution := leadingUnitAs(strings.TrimSpace(match[3]), read)
	if resolution != units.AliasResolved || strings.TrimSpace(rest) == "" {
		return recipe.SimpleQuantity{}, "", false
	}

	return recipe.SimpleQuantity{Kind: recipe.QuantitySimple, Amount: amount, Unit: unitID}, strings.TrimSpace(rest), true
}

func optionFromTerms(terms []recipe.SimpleQuantity) recipe.AlternativeOption {
	if len(terms) == 1 {
		return recipe.AlternativeOption{Kind: recipe.QuantitySimple, Amount: &terms[0].Amount, Unit: terms[0].Unit}
	}

	return recipe.AlternativeOption{Kind: recipe.QuantitySum, Terms: terms}
}

func quantityFromOption(option recipe.AlternativeOption) *recipe.Quantity {
	if option.Kind == recipe.QuantitySimple {
		return &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: option.Amount, Unit: option.Unit}
	}

	return &recipe.Quantity{Kind: recipe.QuantitySum, Terms: option.Terms}
}

type reading struct {
	locale          string
	interpretations map[string]string
	convention      *units.ConventionProfile
	walk            *aliasWalk
	// Continue past ambiguous measures during discovery to find every occurrence.
	discovering bool
}

type aliasWalk struct {
	occurrences []aliasOccurrence
}

type aliasOccurrence struct {
	descriptor units.AliasDescriptor
	ambiguous  bool
}

type unitSplit struct{ form, rest string }

const maxUnspacedUnit = 6

func unitSplits(value string, read reading) []unitSplit {
	var splits []unitSplit

	// Try character boundaries for unspaced scripts, preserving preference for whitespace boundaries.
	if dialectFor(read.locale).unspaced {
		count := 0

		for index := range value {
			if count > 0 && count <= maxUnspacedUnit {
				if rest := strings.TrimSpace(value[index:]); rest != "" {
					splits = append(splits, unitSplit{form: value[:index], rest: rest})
				}
			}

			count++
		}
	}

	for index, r := range value {
		if unicode.IsSpace(r) || r == '/' {
			form, rest := strings.TrimSpace(value[:index]), strings.TrimSpace(value[index:])
			if form != "" && rest != "" {
				splits = append(splits, unitSplit{form: form, rest: rest})
			}
		}
	}

	return splits
}

// Resolve by source qualifier, occurrence decision, ingredient convention, then locale.
// A convention applies only to the unit families it defines.
func leadingUnitAs(value string, read reading) (string, string, units.AliasResolution) {
	if dialectFor(read.locale).namesCompound(value) {
		return "", "", units.AliasUnknown
	}

	splits := unitSplits(value, read)
	for _, v := range slices.Backward(splits) {
		if _, known := catalog.Lookup(v.form); known {
			return v.form, v.rest, units.AliasResolved
		}

		if id, resolution := catalog.ResolveExplicitAlias(v.form); resolution != units.AliasUnknown {
			return id, v.rest, resolution
		}
	}

	for _, v := range slices.Backward(splits) {
		descriptor, found := catalog.DescribeAlias(v.form)
		if !found {
			continue
		}

		unitID, resolution := catalog.ResolveOccurrence(
			units.Occurrence{Form: descriptor.AuthoredForm}, read.locale, read.decisions(descriptor))
		if resolution == units.AliasResolved {
			read.observe(descriptor, false)

			return unitID, v.rest, resolution
		}

		read.observe(descriptor, true)

		if read.discovering {
			return descriptor.CandidateUnitIDs[0], v.rest, units.AliasResolved
		}

		return "", v.rest, units.AliasAmbiguous
	}

	return "", "", units.AliasUnknown
}

// Discovery ignores review decisions to retain all source ambiguities.
func (r reading) decisions(descriptor units.AliasDescriptor) units.Decisions {
	if r.discovering {
		return units.Decisions{}
	}

	return units.Decisions{
		Interpretation: r.interpretations[occurrenceKey(descriptor, r.occurrenceIndex(descriptor))],
		Convention:     r.convention,
	}
}

func (r reading) occurrenceIndex(descriptor units.AliasDescriptor) int {
	if r.walk == nil {
		return 0
	}

	index := 0

	for _, seen := range r.walk.occurrences {
		if seen.descriptor.NormalizedForm == descriptor.NormalizedForm {
			index++
		}
	}

	return index
}

func (r reading) observe(descriptor units.AliasDescriptor, ambiguous bool) {
	if r.walk != nil {
		r.walk.occurrences = append(r.walk.occurrences,
			aliasOccurrence{descriptor: descriptor, ambiguous: ambiguous})
	}
}

func aliasAmbiguities(line, ingredientID, source, sourceLocale string) []Ambiguity {
	walk := &aliasWalk{}
	parseStatedAs(line, ingredientID, source, reading{
		locale: sourceLocale, walk: walk, discovering: true,
	})

	var (
		found  []Ambiguity
		counts = map[string]int{}
		cursor = 0
	)

	for _, occurrence := range walk.occurrences {
		descriptor := occurrence.descriptor
		index := counts[descriptor.NormalizedForm]
		counts[descriptor.NormalizedForm]++

		// Advance past consumed aliases to locate repeated forms independently.
		start := aliasOffset(source, descriptor, cursor)
		if start >= 0 {
			cursor = start + len(descriptor.AuthoredForm)
		}

		if !occurrence.ambiguous || len(descriptor.CandidateUnitIDs) == 0 || start < 0 {
			continue
		}

		key := ambiguityResolutionKey(descriptor)
		occurrenceIdentity := occurrenceKey(descriptor, index)
		digest := sha256.Sum256([]byte(ingredientID + "|" + occurrenceIdentity + "|" + strings.TrimSpace(source)))

		found = append(found, Ambiguity{
			ID: "ambiguity-" + hex.EncodeToString(digest[:8]), Kind: AmbiguityUnitAlias,
			AuthoredForm: descriptor.AuthoredForm, SourceStart: start, SourceEnd: start + len(descriptor.AuthoredForm),
			CandidateUnitIDs: append([]string(nil), descriptor.CandidateUnitIDs...),
			ResolutionKey:    key, OccurrenceKey: occurrenceIdentity,
		})
	}

	return found
}

func aliasOffset(source string, descriptor units.AliasDescriptor, cursor int) int {
	if cursor > len(source) {
		return -1
	}

	haystack, needle := source[cursor:], descriptor.AuthoredForm
	if !descriptor.CaseSensitive {
		haystack, needle = strings.ToLower(haystack), strings.ToLower(needle)
	}

	offset := strings.Index(haystack, needle)
	if offset < 0 {
		return -1
	}

	return cursor + offset
}

func occurrenceKey(descriptor units.AliasDescriptor, index int) string {
	return ambiguityResolutionKey(descriptor) + "|occurrence=" + strconv.Itoa(index)
}

func ambiguityResolutionKey(descriptor units.AliasDescriptor) string {
	candidates := append([]string(nil), descriptor.CandidateUnitIDs...)
	sort.Strings(candidates)

	caseMode := "folded"
	if descriptor.CaseSensitive {
		caseMode = "exact"
	}

	return strings.Join([]string{
		AmbiguityUnitAlias, descriptor.NormalizedForm, caseMode, strings.Join(candidates, ","),
		"parser=" + ParserVersion, "units=" + units.RegistryVersion,
	}, "|")
}

func rational(value string) (recipe.Rational, bool) {
	parts := strings.Fields(strings.Replace(value, ",", ".", 1))

	result := new(big.Rat)
	if len(parts) == 2 {
		whole, ok := new(big.Rat).SetString(parts[0])
		if !ok {
			return "", false
		}

		fraction, ok := new(big.Rat).SetString(parts[1])
		if !ok {
			return "", false
		}

		result.Add(whole, fraction)
	} else {
		parsed, ok := result.SetString(parts[0])
		if !ok {
			return "", false
		}

		result = parsed
	}

	if result.Sign() < 0 {
		return "", false
	}

	text := result.RatString()
	parsed, err := recipe.ParseRational(text)

	return parsed, err == nil
}

func nameAfterMeasure(value string, read reading) (string, string) {
	name, note := splitNote(value)
	if note == "" {
		name, note = dialectFor(read.locale).splitUnmeasured(name)
	}

	trimmed := name
	if connective := dialectFor(read.locale).connective; connective != nil {
		trimmed = strings.TrimSpace(connective.ReplaceAllString(name, ""))
	}

	if trimmed == "" {
		return name, note
	}

	return trimmed, note
}

func splitNote(value string) (string, string) {
	depth := 0

	for index, character := range value {
		if character == ',' && index > 0 && index+1 < len(value) && isASCIIDigit(value[index-1]) && isASCIIDigit(value[index+1]) {
			continue
		}

		switch character {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				return strings.TrimSpace(value[:index]), strings.TrimSpace(value[index+1:])
			}
		}
	}

	return strings.TrimSpace(value), ""
}

func isASCIIDigit(character byte) bool {
	return character >= '0' && character <= '9'
}
