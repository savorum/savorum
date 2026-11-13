package ingredientparser

import (
	"math/big"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func cutLeadingWord(value, word string) (string, bool) {
	if len(value) < len(word) || !strings.EqualFold(value[:len(word)], word) {
		return value, false
	}

	rest := value[len(word):]
	if rest != "" {
		next, _ := utf8.DecodeRuneInString(rest)
		if unicode.IsLetter(next) || unicode.IsNumber(next) {
			return value, false
		}
	}

	return strings.TrimLeft(rest, " ,"), true
}

func cutAnyWord(value string, words []string) (string, string, bool) {
	for _, word := range words {
		if rest, found := cutLeadingWord(value, word); found {
			return value[:len(word)], rest, true
		}
	}

	return "", value, false
}

var hyphenQualifier = regexp.MustCompile(`^(\pL+(?:-\pL+)+)\s+`)

var sizeCode = regexp.MustCompile(`(?i)^no\.?\s*\d+\s+`)

func (d *dialect) rewriteLine(line string) (string, string, bool) {
	if word, rest, found := cutAnyWord(line, d.bounds); found && word != "" && beginsAmount(rest) {
		return line, "", true
	}

	if len(d.between) == 2 {
		if rest, found := cutLeadingWord(line, d.between[0]); found && beginsAmount(rest) {
			if low, high, cut := strings.Cut(rest, " "+d.between[1]+" "); cut && beginsAmount(high) {
				return low + "-" + high, "", false
			}
		}
	}

	for part, note := range d.parts {
		index := indexWord(line, part+" of ")
		if index < 0 {
			continue
		}

		rest := strings.TrimSpace(line[index+len(part)+len(" of "):])
		if !beginsAmount(rest) {
			continue
		}

		before := strings.TrimSpace(line[:index])
		if before != "" {
			note = before + " " + part
		}

		return rest, note, false
	}

	return line, "", false
}

func indexWord(line, phrase string) int {
	index := strings.Index(strings.ToLower(line), phrase)
	if index > 0 {
		previous, _ := utf8.DecodeLastRuneInString(line[:index])
		if unicode.IsLetter(previous) {
			return -1
		}
	}

	return index
}

func (d *dialect) measureFollows(value string, read reading) bool {
	value = d.skipQualifiers(value)
	if _, _, resolution := leadingUnitAs(value, read); resolution == units.AliasResolved {
		return true
	}

	if d.isOpaqueMeasure(value) || beginsAmount(value) {
		return true
	}

	for multiplier := range d.multipliers {
		if _, found := cutLeadingWord(value, multiplier); found {
			return true
		}
	}

	_, _, counted := cutAnyWord(value, d.portionWords)

	return counted
}

func (d *dialect) skipQualifiers(value string) string {
	for {
		if _, rest, found := cutAnyWord(value, append(slices.Clone(d.sizes), d.modifiers...)); found {
			value = rest

			continue
		}

		if match := hyphenQualifier.FindStringSubmatch(value); match != nil {
			value = value[len(match[0]):]

			continue
		}

		if match := sizeCode.FindString(value); match != "" {
			value = value[len(match):]

			continue
		}

		return value
	}
}

func (d *dialect) qualifiedPortion(value string, read reading) bool {
	match := hyphenQualifier.FindStringSubmatch(value)
	if match == nil {
		return false
	}

	rest := value[len(match[0]):]
	if _, _, portion := cutAnyWord(rest, d.portionWords); portion {
		return true
	}

	unit, _, resolution := leadingUnitAs(rest, read)

	return resolution == units.AliasResolved && strings.HasPrefix(unit, "count.")
}

func (d *dialect) multiply(amount recipe.Amount, remainder string) (recipe.Amount, string) {
	for word, value := range d.multipliers {
		rest, found := cutLeadingWord(remainder, word)
		if !found {
			continue
		}

		factor, ok := new(big.Rat).SetString(value)
		if !ok {
			continue
		}

		if after, of := cutLeadingWord(rest, "of"); of {
			rest = after
		}

		scaled := func(value *recipe.Rational) *recipe.Rational {
			if value == nil {
				return nil
			}

			rat, err := value.Rat()
			if err != nil {
				return value
			}

			result := recipe.Rational(new(big.Rat).Mul(rat, factor).RatString())

			return &result
		}

		amount.Value, amount.Min, amount.Max = scaled(amount.Value), scaled(amount.Min), scaled(amount.Max)

		return amount, rest
	}

	return amount, remainder
}

func (d *dialect) cutFractionOf(remainder string) string {
	if _, rest, found := cutAnyWord(remainder, d.fractionOf); found {
		return rest
	}

	return remainder
}

func (d *dialect) countWords(name string) (string, []string) {
	var notes []string

	counted := d.portionWords

	for !d.namesCompound(name) {
		qualifier, rest, listed := cutAnyWord(name, d.sizes)
		sized := listed

		if !sized {
			if match := hyphenQualifier.FindStringSubmatch(name); match != nil {
				qualifier, rest, sized = match[1], name[len(match[0]):], true
			}
		}

		if sized {
			if word, after, found := cutAnyWord(rest, counted); found {
				return after, append(notes, qualifier+" "+word)
			}

			if listed {
				notes, name = append(notes, qualifier), rest

				continue
			}
		}

		if word, after, found := cutAnyWord(name, d.descriptors); found {
			notes, name = append(notes, word), after

			continue
		}

		if word, after, found := cutAnyWord(name, counted); found && after != "" {
			return after, append(notes, word)
		}

		if rest, size, found := d.trailingSizeWord(name); found {
			return rest, append(notes, size)
		}

		return name, notes
	}

	return name, notes
}

func (d *dialect) beginsCountWord(value string, read reading) bool {
	value = strings.TrimSpace(value)
	if _, _, found := cutAnyWord(value, d.portionWords); found {
		return true
	}

	unit, _, resolution := leadingUnitAs(value, read)

	return resolution == units.AliasResolved && counts(unit)
}

func (d *dialect) liquid(name string) bool {
	words := strings.Fields(strings.ToLower(name))

	return len(words) > 0 && slices.Contains(d.liquids, words[len(words)-1])
}

func (d *dialect) subtracts(name string) bool {
	_, rest, found := cutAnyWord(name, d.minusWords)

	return found && beginsAmount(rest)
}

var percentageOnly = regexp.MustCompile(`^\pN+(?:[.,]\pN+)?\s*%$`)

func finishCount(ingredient recipe.Ingredient, notes []string, read reading) recipe.Ingredient {
	quantity := ingredient.Quantity
	if quantity == nil || quantity.Kind != recipe.QuantitySimple || !counts(quantity.Unit) {
		return ingredient
	}

	grammar := dialectFor(read.locale)

	if _, rest, found := cutAnyWord(ingredient.Name, grammar.descriptors); found && rest == "" && ingredient.Note != "" {
		ingredient.Name, ingredient.Note = ingredient.Name+", "+ingredient.Note, ""
	}

	name, counted := grammar.countWords(ingredient.Name)
	if name == "" {
		return ingredient
	}

	ingredient.Name = name
	note := ingredient.Note

	if ingredient.PackageSize == nil {
		if size, rest, remaining, found := trailingSize(ingredient.Name, note, read); found {
			ingredient.PackageSize = &size
			ingredient.Name, note = rest, remaining
		}
	}

	ingredient.Note = joinNotes(append(append(notes, counted...), note))

	if quantity.Unit == itemUnit {
		if unit, word, rest, ok := namesCountUnit(ingredient.Name, read); ok {
			quantity.Unit, ingredient.Name = unit, word
			ingredient.Note = joinNotes([]string{ingredient.Note, rest})
		}
	}

	return ingredient
}

func trailingSize(name, note string, read reading) (recipe.SimpleQuantity, string, string, bool) {
	grammar := dialectFor(read.locale)

	measure := func(text string) (recipe.SimpleQuantity, bool) {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), "each"))
		approximate := false

		if rest, found := grammar.takeApproximate(text); found {
			text, approximate = rest, true
		}

		size, rest, found := followingMeasure(normalizeAmounts(text)+" "+placeholder, read)
		if !found || rest != placeholder {
			return recipe.SimpleQuantity{}, false
		}

		size.Amount.Approximate = size.Amount.Approximate || approximate

		return size, true
	}

	if size, ok := measure(note); ok && note != "" {
		return size, name, "", true
	}

	if open := strings.LastIndex(name, "("); open > 0 && strings.HasSuffix(name, ")") {
		if size, ok := measure(name[open+1 : len(name)-1]); ok {
			return size, strings.TrimSpace(name[:open]), note, true
		}
	}

	return recipe.SimpleQuantity{}, name, note, false
}

func leadingSize(name string, read reading) (*recipe.SimpleQuantity, string) {
	inner, rest, closed := strings.Cut(strings.TrimPrefix(name, "("), ")")
	if !strings.HasPrefix(name, "(") || !closed {
		return nil, name
	}

	inner = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(inner), "each"))

	size, after, found := followingMeasure(normalizeAmounts(inner)+" "+placeholder, read)
	if !found || after != placeholder || strings.TrimSpace(rest) == "" {
		return nil, name
	}

	return &size, strings.TrimSpace(rest)
}

func (d *dialect) vagueAmount(line string, read reading) (string, string, bool) {
	if word, rest, found := cutAnyWord(line, d.vague); found && word != "" && !beginsAmount(rest) {
		measured := strings.TrimSpace(rest)
		if _, tail, resolution := leadingUnitAs(measured, read); resolution == units.AliasResolved {
			measured = tail
		} else if tail, opaque := d.cutOpaque(measured); opaque {
			measured = tail
		}

		return strings.TrimSpace(strings.TrimSuffix(line, measured)), strings.TrimSpace(measured), true
	}

	lowered := strings.ToLower(line)
	for _, word := range d.vagueAfter {
		if name, found := strings.CutSuffix(lowered, word); found && strings.TrimSpace(name) != "" {
			return strings.TrimSpace(line[len(name):]), trimLeader(line[:len(name)]), true
		}
	}

	return "", line, false
}

func (d *dialect) cutOpaque(value string) (string, bool) {
	for _, word := range d.opaque {
		if rest, found := cutLeadingWord(value, word); found {
			return rest, true
		}
	}

	return value, false
}

func (d *dialect) trailingSizeWord(name string) (string, string, bool) {
	index := strings.LastIndexFunc(name, unicode.IsSpace)
	if index <= 0 {
		return name, "", false
	}

	word := name[index+1:]
	if !slices.Contains(d.sizes, strings.ToLower(word)) {
		return name, "", false
	}

	return strings.TrimSpace(name[:index]), word, true
}

func counts(unitID string) bool {
	unit, found := catalog.Lookup(unitID)

	return found && unit.Kind != recipe.UnitPhysical
}

func (d *dialect) cutSize(value string) (string, string, bool) {
	if word, rest, found := cutAnyWord(value, d.sizes); found {
		return word, rest, true
	}

	if match := hyphenQualifier.FindStringSubmatch(value); match != nil {
		return match[1], value[len(match[0]):], true
	}

	return "", value, false
}

var namesIngredient = map[string]bool{"count.egg": true}

func namesCountUnit(name string, read reading) (string, string, string, bool) {
	unit, rest, resolution := leadingUnitAs(name+" "+placeholder, read)
	if resolution != units.AliasResolved || !namesIngredient[unit] {
		return "", "", "", false
	}

	rest = strings.TrimSpace(strings.TrimSuffix(rest, placeholder))
	word := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(name, rest), ","))

	return unit, word, rest, true
}
