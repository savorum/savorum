// Package ingredientparser conservatively extracts the common structured
// amount at the start of an ingredient line. Import adapters share it so each
// source application does not invent a different interpretation of cups,
// fractions, and ranges.
package ingredientparser

import (
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

	"golang.org/x/text/unicode/norm"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

// amountPattern is a whole number, a fraction, a mixed number, or a decimal.
// A decimal comma takes one or two digits, so "1,500" is never read as 1.5.
const amountPattern = `(?:\d+\s+)?(?:\d+/\d+)|\d+(?:\.\d+|,\d{1,2})?`

var (
	attachedUnit = regexp.MustCompile(`^(` + amountPattern + `)(\p{L})`)
	// A hyphen joining an amount to a word is the compound-adjective spelling
	// of the same measure: "8-oz" is "8 oz". Only a letter after the hyphen is
	// treated this way, so a range keeps its separator and "2-3" stays a range.
	hyphenatedUnit = regexp.MustCompile(`^(` + amountPattern + `)-(\p{L})`)
)

const unresolvedWarning = "Savorum could not safely identify every amount and unit in this ingredient. The original line was preserved for review."

// Ambiguity kinds explain which ingredient interpretation requires review.
const (
	// ParserVersion participates in resolution fingerprints. Bump it whenever
	// parsing changes, so a dry-run plan previewed under the old behavior is
	// stale rather than applied under a scope nobody reviewed.
	ParserVersion      = "6"
	AmbiguityUnitAlias = "unit_alias"
)

// Ambiguity is a parser-issued, controlled resolution descriptor. Only
// descriptors returned here are eligible for import-wide review actions.
//
// ResolutionKey groups like measures across an import; OccurrenceKey addresses
// one measure in one ingredient, because an ingredient can say `cup` twice and
// SREF section 9.5 requires an answer to reach only the occurrence it names.
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

// Parse returns an ingredient with a structured quantity when the line begins
// with an unambiguous amount. Uncertain prose remains untouched in SourceText.
//
// Authored optionality (`60 g bell pepper, diced, optional`) is lifted into the
// typed member first; the whole line is still kept as SourceText.
func Parse(line, id, sourceLocale string) recipe.Ingredient {
	return ParseDetailed(line, id, sourceLocale).Ingredient
}

// AliasLanguage is the language the registry's unit aliases are written in,
// and so the reading language for a source that states no locale.
const AliasLanguage = "en"

func readingLocale(sourceLocale string) string {
	if strings.TrimSpace(sourceLocale) == "" {
		return AliasLanguage
	}

	return sourceLocale
}

// ParseDetailed preserves Parse's conservative result while additionally
// publishing every ambiguity the parser knows how to resolve safely.
func ParseDetailed(line, id, sourceLocale string) ParseResult {
	return ParseWithContext(line, id, sourceLocale, ResolutionContext{})
}

// ResolutionContext carries the decisions a person made about this ingredient.
// It is a normalization input, not a property of the recipe: nothing here is
// written to the document, and reading a stored recipe never reconstructs one.
//
// The two kinds are kept apart because SREF section 9.5 keeps them apart.
// Interpretations settle the occurrences they address and nothing else.
// Convention settles the unqualified, convention-dependent measures of this
// whole ingredient, including every term of a sum and every branch of an
// alternative. Neither is inferred from the other: choosing a US customary cup
// for one measure is not a statement about the rest of the ingredient.
type ResolutionContext struct {
	// Interpretations maps an ambiguity's OccurrenceKey to the identity a
	// person selected for that occurrence.
	Interpretations map[string]string
	// Convention, when set, is the measuring convention a person chose for this
	// ingredient, already expanded to the identities it selects.
	Convention *units.ConventionProfile
	// DeclaredLocale is the locale the source document stated, empty when it
	// stated none. It is not the reading locale, so provenance never attributes
	// the AliasLanguage fallback to the source.
	DeclaredLocale string
}

func (c ResolutionContext) empty() bool {
	return len(c.Interpretations) == 0 && c.Convention == nil
}

// ParseWithContext reparses the unchanged source under explicit decisions.
//
// An interpretation naming a unit the authored form does not permit is refused
// rather than applied, and a convention that covers none of the ingredient's
// measures simply resolves nothing: an unanswerable decision leaves the line
// exactly as it was, with its ambiguities still available for review.
func ParseWithContext(line, id, sourceLocale string, context ResolutionContext) ParseResult {
	// The line is read in canonical composition; the ingredient keeps the text as
	// it was written.
	written := strings.TrimSpace(line)
	source := norm.NFC.String(written)
	stated, optional := splitAuthoredOptional(listMarker.ReplaceAllString(source, ""), sourceLocale)

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

	// The decision resolved what the source alone could not. That is an
	// inference a person reviewed and corrected, and saying so is the only
	// thing the exported document records about it.
	parsed.Normalization = &recipe.Normalization{
		Method: recipe.NormalizationInferred, Review: recipe.ReviewCorrected,
		Warnings: []string{interpretationProvenance(line, id, sourceLocale, context)},
	}

	return ParseResult{Ingredient: parsed}
}

// outstanding is the measures still to be answered under a supplied context,
// so a review workflow sees an answered measure drop out rather than reappear.
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

		// The locale is withheld: a measure the locale alone would settle is
		// not one this context answered, and it is not outstanding either —
		// the discovery pass would not have reported it.
		if _, resolution := catalog.ResolveOccurrence(
			units.Occurrence{Form: descriptor.AuthoredForm}, "", decisions,
		); resolution == units.AliasResolved {
			continue
		}

		remaining = append(remaining, ambiguity)
	}

	return remaining
}

// interpretationProvenance says what the source supplied and what the
// interpretation was chosen for.
//
// SREF section 9.5 forbids stating that a source omitted regional evidence
// when it supplied a locale, and requires a decision that departs from a stated
// locale to be exposed as a difference rather than as agreement.
func interpretationProvenance(line, id, sourceLocale string, context ResolutionContext) string {
	scope := "It was chosen for this measurement."
	if context.Convention != nil {
		scope = "It was chosen for every applicable measure of this ingredient."
	}

	detail := "The source did not identify a regional measurement convention."

	if declared := strings.TrimSpace(context.DeclaredLocale); declared != "" {
		// What the locale alone would have made of the line, so a decision
		// that departs from it is reported as departing from it rather than as
		// agreement with the source.
		alone := ParseWithContext(line, id, sourceLocale, ResolutionContext{DeclaredLocale: declared})
		if alone.Ingredient.Quantity != nil {
			detail = fmt.Sprintf("The source states the locale %s, which reads it differently.", declared)
		} else {
			detail = fmt.Sprintf("The source states the locale %s, which does not identify it.", declared)
		}
	}

	return recipe.ImportReviewUnitPrefix + " " + detail + " " + scope
}

// splitAuthoredOptional takes an explicit statement that the ingredient may be
// left out, and nothing else, from the line.
//
// `to taste`, `for serving`, and `preferably fresh` are not optionality, and
// the word boundary keeps `optionally` (an adverb about method) out. The words
// that state it are the reading language's.
func splitAuthoredOptional(line, locale string) (string, bool) {
	authoredOptional := dialectFor(locale).optional
	if !authoredOptional.MatchString(line) {
		return line, false
	}

	stated := authoredOptional.ReplaceAllString(line, "")

	stated = strings.TrimSpace(strings.Trim(strings.TrimSpace(stated), ",;，、；-–—"))
	if stated == "" {
		// The line said nothing else, so there is no ingredient to attach
		// optionality to.
		return line, false
	}

	return stated, true
}

// listMarker is a bullet in front of an ingredient line. Whitespace must follow
// it, so a leading minus or dash joined to a number is left alone.
var listMarker = regexp.MustCompile(`^[-*•·–—]\s+`)

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

	working := original

	working, isApproximate := grammar.takeApproximate(working)

	working, approximatedBefore := grammar.reorder(normalizeAmounts(working), read)
	isApproximate = isApproximate || approximatedBefore

	match := grammar.amount.FindStringSubmatch(working)
	if len(match) != 4 {
		if beginsAmount(working) {
			return unresolved(fallback)
		}

		return fallback
	}

	minimum, ok := rational(match[1])
	if !ok {
		return fallback
	}

	remainder := strings.TrimSpace(match[3])
	if remainder == "" {
		return fallback
	}

	amount := recipe.Amount{Approximate: isApproximate}
	if match[2] == "" {
		amount.Value = &minimum
	} else {
		maximum, valid := rational(match[2])
		if !valid {
			return fallback
		}

		amount.Min, amount.Max = &minimum, &maximum
	}

	unitID, name, recognizedUnit, ambiguous := readLeadingUnit(remainder, grammar, read)
	if ambiguous {
		return unresolved(fallback)
	}

	// A measure whose size differs between countries, such as a cup, has no
	// alias. It is not a count of an ingredient called "cup of milk" either.
	if !recognizedUnit && grammar.isOpaqueMeasure(remainder) {
		return unresolved(fallback)
	}

	// A number with no unit, followed by a measurement, is one of two things,
	// and reading it as a count of an ingredient called "170-180 g light brown
	// sugar" is neither of them.
	if !recognizedUnit {
		if line, decided := readUnitless(id, source, fallback, amount, remainder, read); decided {
			return line
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

	name, note := nameAfterMeasure(name, read)
	if name == "" {
		return fallback
	}

	// A registry cup is the cup of that country's kitchens. A line that says it
	// means a paper cup or a rice-cooker cup does not state that cup.
	if describesAnotherCup(unitID, original, grammar) {
		return unresolved(fallback)
	}

	return recipe.Ingredient{
		ID: id, Name: name, Note: note, SourceText: source,
		Quantity:      quantity,
		Normalization: &recipe.Normalization{Method: recipe.NormalizationExtracted},
	}
}

// withAlternative adds the source's own equivalent for a measure, when it states
// one. It reports false when the line names an alternative it cannot read, or
// one that contradicts the size of the measure.
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

// readLeadingUnit reads the measure that starts value: a word the ingredient
// after it gives a sense to, or a registry alias. Without one the line counts
// items.
func readLeadingUnit(value string, grammar *dialect, read reading) (string, string, bool, bool) {
	if unit, rest, found := grammar.senseOf(value); found {
		return unit, rest, true, false
	}

	unit, rest, resolution := leadingUnitAs(value, read)
	if resolution == units.AliasResolved {
		return unit, rest, true, false
	}

	return itemUnit, value, false, resolution == units.AliasAmbiguous
}

// describesAnotherCup reports a registry cup that the line itself describes as
// a different measure.
func describesAnotherCup(unitID, line string, grammar *dialect) bool {
	return strings.HasPrefix(unitID, "volume.cup.") && grammar.describesOpaqueMeasure(line)
}

func appendAdditiveTerms(terms []recipe.SimpleQuantity, name string, read reading) ([]recipe.SimpleQuantity, string, bool) {
	for {
		term, remainder, found := additive(name, read)
		if !found {
			return terms, name, !beginsAdditive(name, read)
		}

		terms = append(terms, term)
		name = remainder
	}
}

func normalizeAmounts(value string) string {
	working := strings.Join(strings.Fields(unicodeFractions.Replace(asciiDigits(value))), " ")
	working = hyphenatedUnit.ReplaceAllString(working, "$1 $2")

	return attachedUnit.ReplaceAllString(working, "$1 $2")
}

// beginsAmount reports whether value starts with a digit in any script, so an
// amount the grammar cannot read is flagged rather than taken for prose.
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

// readUnitless decides a line whose leading number has no unit, unless it is a
// plain count of items.
func readUnitless(
	id, source string, fallback recipe.Ingredient, amount recipe.Amount, remainder string, read reading,
) (recipe.Ingredient, bool) {
	if line, packaged := readParenthesizedPackage(id, source, fallback, amount, remainder, read); packaged {
		return line, true
	}

	if line, counted := readCountedMeasure(id, source, unresolved(fallback), amount, remainder, read); counted {
		return line, true
	}

	// Without unit aliases in the reading language, the words after a bare
	// number may be a measure the registry cannot name, as in "2 cucharadas de
	// aceite", so the line is not taken for a count of items.
	if !catalog.HasAliasesFor(readingLocale(read.locale)) {
		return unresolved(fallback), true
	}

	return recipe.Ingredient{}, false
}

// containers name a thing a shop sells rather than a way of measuring, which
// is what makes "2 400 g cans" a count of packages and not two of anything.
var containers = map[string]bool{
	"container.can": true, "container.tin": true, "container.packet": true,
}

// itemUnit is the quantity unit for a count with no container named. A source
// that says how many and how big each one is has stated a count of items.
const itemUnit = "count.item"

// readCountedMeasure interprets a bare leading number that is followed by a
// real measurement. There are three readings:
//
//   - "2 400 g cans chopped tomatoes" counts packages. The count and the
//     container are the ingredient's quantity, and the measurement is the size
//     of the package, which is what `package_size` is for.
//   - "1 170-180 g light brown sugar" counts nothing. Exporters emit a leading
//     1 from a quantity column that has no value, and one of a 170-180 g
//     measure is that measure, so the stray number is dropped and the
//     measurement read instead. Nothing is lost, because 1 × x is x.
//   - "2 8 oz chicken breasts" counts items of a stated size. No container is
//     named, so the count is of items and the measurement is the size of each,
//     exactly as for a named container.
//
// A leading number other than one is never absorbed into the measurement it
// precedes; see isOne.
//
// The returned flag says the line is a counted measure and has been decided,
// whichever of the three it turned out to be.
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

	if container, tail, resolution := leadingUnitAs(rest, read); resolution == units.AliasResolved && strings.TrimSpace(tail) != "" {
		if containers[container] {
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

	if !isOne(count) {
		// Only a whole number counts items. "1/2 3/4 cup hot sauce" is a
		// mangled line, and half of an item is not what it is trying to say.
		if !isWholeCount(count) {
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

// readParenthesizedPackage recognizes the common exported form
// "2 (14.5 oz) cans tomatoes". The count remains the ingredient quantity and
// the parenthesized measure becomes the package size instead of being stranded
// in the ingredient name. A source can omit a named container, as in
// "1 (4 pound) pork shoulder roast"; the item count is still explicit.
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
	if container, rest, resolution := leadingUnitAs(tail, read); resolution == units.AliasResolved && containers[container] {
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

// isOne reports whether an amount is exactly one, which is the only count a
// measurement can absorb without losing anything.
func isOne(amount recipe.Amount) bool {
	return amount.Value != nil && *amount.Value == "1" && amount.Min == nil && amount.Max == nil
}

// isWholeCount reports whether an amount can be a number of items. Amounts are
// canonical rationals, so a whole number carries no denominator.
func isWholeCount(amount recipe.Amount) bool {
	if amount.Value == nil || amount.Min != nil || amount.Max != nil {
		return false
	}

	return !strings.ContainsAny(string(*amount.Value), "/.")
}

// alternative recognizes a second fully specified amount at the start of the
// remaining line, with a slash, an "or", or no separator at all. It does not
// interpret slashes inside ingredient names because a following amount and
// known unit are both required.
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

// parenthesizedEquivalent reads "(240 ml)" after a measure as the source's own
// equivalent for it. Any other parenthesis is left to the ingredient.
func parenthesizedEquivalent(value string, read reading) (recipe.SimpleQuantity, string, bool) {
	end := strings.Index(value, ")")
	if end < 0 {
		return recipe.SimpleQuantity{}, "", false
	}

	// A measure is followed by what it measures; a placeholder stands in for it.
	const placeholder = "\x00"

	term, rest, found := followingMeasure(strings.TrimSpace(value[1:end])+" "+placeholder, read)
	if !found || rest != placeholder {
		return recipe.SimpleQuantity{}, "", false
	}

	return term, strings.TrimSpace(value[end+1:]), true
}

// statesAnotherMeasure reports a spoon or cup whose own stated equivalent in
// the same dimension differs from the registry's size by more than a tenth.
// The source then uses a measure of its own, and the registry's is not it.
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

	return strings.HasPrefix(working, "/") || orWord || beginsAmount(working)
}

// additive recognizes one explicit term following "+" or "plus". Requiring
// the connector prevents a number at the start of an ingredient name from
// becoming an invented sum.
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

// reading is how one ingredient's measures are to be interpreted: the locale
// the source declared, the interpretations a person supplied for individual
// occurrences, and the measuring convention they chose for the ingredient.
//
// trace, when set, records every alias occurrence the walk examines. Discovery
// needs the whole quantity expression, and an ordinary parse stops at the first
// measure it cannot resolve, so the two are the same walk under different
// instructions rather than two grammars that have to agree.
type reading struct {
	locale          string
	interpretations map[string]string
	convention      *units.ConventionProfile
	// walk records the alias occurrences examined so far. It is what makes an
	// occurrence addressable: the ordinal of a measure among the like measures
	// of its own ingredient.
	walk *aliasWalk
	// discovering tolerates an ambiguous measure so the walk reaches the rest
	// of a compound amount. Ordinary parsing stops at the first measure it
	// cannot resolve, which is what preserves the authored line for review,
	// and would otherwise only ever report the leading ambiguity.
	discovering bool
}

// aliasWalk collects the alias occurrences of one quantity expression, in the
// order the source states them.
type aliasWalk struct {
	occurrences []aliasOccurrence
}

type aliasOccurrence struct {
	descriptor units.AliasDescriptor
	ambiguous  bool
}

type unitSplit struct{ form, rest string }

// maxUnspacedUnit is the longest unit, in characters, read from the front of
// a word in a language that does not space words.
const maxUnspacedUnit = 6

func unitSplits(value string, read reading) []unitSplit {
	var splits []unitSplit

	// A language written without spaces puts a unit against the word after it,
	// so a unit may end at any character. These are listed first, and the
	// caller tries the last entry first, so a reading a space marks wins.
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

// leadingUnitAs applies SREF section 9.5 precedence to the leading measure of
// value: explicit source meaning, then an interpretation addressing this
// occurrence, then the ingredient's convention, then the locale lookup of
// section 9.2. A decision never overrides a qualifier the source wrote, and a
// convention that does not cover the measure's family leaves it to the locale
// rather than borrowing a neighbouring convention.
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

		// Discovery keeps walking so the rest of a compound amount is seen.
		// Ordinary parsing stops here, which is what preserves the whole
		// authored line for review.
		if read.discovering {
			return descriptor.CandidateUnitIDs[0], v.rest, units.AliasResolved
		}

		return "", v.rest, units.AliasAmbiguous
	}

	return "", "", units.AliasUnknown
}

// decisions are the resolution inputs that apply to this occurrence.
//
// Discovery supplies none: it reports what the source alone says, so a decision
// already made does not hide the occurrence it settled.
func (r reading) decisions(descriptor units.AliasDescriptor) units.Decisions {
	if r.discovering {
		return units.Decisions{}
	}

	return units.Decisions{
		Interpretation: r.interpretations[occurrenceKey(descriptor, r.occurrenceIndex(descriptor))],
		Convention:     r.convention,
	}
}

// occurrenceIndex counts how many occurrences of this same measure the walk has
// already passed, so the second `cup` of one ingredient is addressed separately
// from the first.
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

// aliasAmbiguities reports every measure of one ingredient that the source
// evidence alone cannot identify, including the later terms of a compound
// amount. It is the ordinary parse walked in discovery mode, so there is no
// second grammar to keep in step.
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

		// The cursor advances past every alias the walk consumed, ambiguous or
		// not, so a repeated form is located where it actually occurs rather
		// than at the first spelling of it in the line.
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

// aliasOffset locates an authored form at or after cursor, honoring the alias
// set's own case policy.
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

// occurrenceKey addresses one measure inside one ingredient. The group key
// alone cannot: an ingredient may say `cup` twice, and an answer given for one
// of them must not settle the other.
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

// nameAfterMeasure takes the text following a measure as the ingredient name,
// dropping a connective that only joined the two: "8 oz of chicken" is chicken.
func nameAfterMeasure(value string, read reading) (string, string) {
	name, note := splitNote(value)

	trimmed := name
	if connective := dialectFor(read.locale).connective; connective != nil {
		trimmed = strings.TrimSpace(connective.ReplaceAllString(name, ""))
	}

	if trimmed == "" {
		return name, note
	}

	return trimmed, note
}

// splitNote separates an ingredient from its note at the first comma outside
// brackets, so "kosher salt (half for table salt, 50% extra)" stays whole.
func splitNote(value string) (string, string) {
	depth := 0

	for index, character := range value {
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
