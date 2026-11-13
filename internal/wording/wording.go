// Package wording identifies recipe amounts affected by scaling.
package wording

import (
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/recipetext"
	"github.com/savorum/savorum/internal/units"
)

// FixedNumberWords contains language-specific markers for dimensions, times, and references.
type FixedNumberWords struct {
	Locale  string   `json:"locale"`
	Span    string   `json:"span"`
	Compact bool     `json:"compact"`
	After   []string `json:"after"`
	Before  []string `json:"before"`
}

var symbols = []string{"%", "°", "″", `"`, "”"}

// BuildFixedNumberWords combines time, dimension, temperature, and reference vocabulary by language.
func BuildFixedNumberWords(catalog units.Catalog) []FixedNumberWords {
	measures := map[string][]string{}

	for _, set := range catalog.AliasSets() {
		unit, _ := catalog.Lookup(set.UnitID)
		if unit.Dimension != recipe.DimensionLength && unit.Dimension != recipe.DimensionTemperature {
			continue
		}

		base, _ := language.MustParse(set.Language).Base()
		measures[base.String()] = append(measures[base.String()], set.Forms...)
	}

	vocabularies := recipetext.NumberVocabularies()
	result := make([]FixedNumberWords, 0, len(vocabularies))

	for _, entry := range vocabularies {
		result = append(result, FixedNumberWords{
			Locale: entry.Locale, Span: entry.Span, Compact: entry.Compact,
			After:  sortedUnique(slices.Concat(symbols, measures[entry.Locale], entry.Times)),
			Before: sortedUnique(entry.References),
		})
	}

	return result
}

func sortedUnique(words []string) []string {
	lowered := make([]string, 0, len(words))
	for _, word := range words {
		lowered = append(lowered, strings.ToLower(word))
	}

	slices.Sort(lowered)

	return slices.Compact(lowered)
}

type fixedNumberReader struct {
	words   FixedNumberWords
	numbers *regexp.Regexp
}

var (
	fixedNumberReaders     map[string]*fixedNumberReader
	fixedNumberReadersOnce sync.Once
)

const number = `\pN+(?:[.,]\pN+)?(?:\s+\pN+/\pN+|/\pN+)?`

func readerFor(locale string) *fixedNumberReader {
	fixedNumberReadersOnce.Do(func() {
		fixedNumberReaders = map[string]*fixedNumberReader{}

		for _, words := range BuildFixedNumberWords(units.DefaultCatalog()) {
			span := `-|–|—`
			if words.Span != "" {
				span += `|` + regexp.QuoteMeta(words.Span)
			}

			fixedNumberReaders[words.Locale] = &fixedNumberReader{
				words:   words,
				numbers: regexp.MustCompile(number + `(?:\s*(?:` + span + `)\s*` + number + `)?`),
			}
		}
	})

	base := "en"

	if tag, err := language.Parse(locale); err == nil {
		if primary, _ := tag.Base(); primary.String() != "und" {
			base = primary.String()
		}
	}

	if reader, found := fixedNumberReaders[base]; found {
		return reader
	}

	return fixedNumberReaders["en"]
}

var ratio = regexp.MustCompile(`^\pN{2,}/\pN{2,}$`)

// StatesAmount reports whether text contains a quantity affected by scaling.
// Dimensions, times, percentages, ratios, and note references are excluded.
func StatesAmount(text, locale string) bool {
	reader := readerFor(locale)
	fixed := false

	// Read dimensions right to left so "9 x 13-inch" applies the unit to both numbers.
	for _, match := range slices.Backward(reader.numbers.FindAllStringIndex(text, -1)) {
		start, end := match[0], match[1]
		rest := strings.TrimLeft(text[end:], " -‐")

		if !dimensionContinues(rest) {
			fixed = ratio.MatchString(text[start:end]) || reader.follows(rest) || reader.precedes(text[:start])
		}

		if !fixed {
			return true
		}
	}

	return false
}

func dimensionContinues(rest string) bool {
	after, found := strings.CutPrefix(rest, "x")
	if !found {
		after, found = strings.CutPrefix(rest, "×")
	}

	first, _ := utf8.DecodeRuneInString(strings.TrimSpace(after))

	return found && unicode.IsNumber(first)
}

func (r *fixedNumberReader) follows(rest string) bool {
	lowered := strings.ToLower(rest)
	for _, word := range r.words.After {
		if tail, found := strings.CutPrefix(lowered, word); found && (r.words.Compact || endsWord(word, tail)) {
			return true
		}
	}

	return false
}

func (r *fixedNumberReader) precedes(head string) bool {
	head = strings.ToLower(strings.TrimRightFunc(head, unicode.IsSpace))
	for _, word := range r.words.Before {
		if strings.HasSuffix(head, word) && (r.words.Compact || !endsInLetter(strings.TrimSuffix(head, word))) {
			return true
		}
	}

	return false
}

func endsWord(word, tail string) bool {
	last, _ := utf8.DecodeLastRuneInString(word)
	next, _ := utf8.DecodeRuneInString(tail)

	return tail == "" || !unicode.IsLetter(last) || !unicode.IsLetter(next) && !unicode.IsNumber(next)
}

func endsInLetter(text string) bool {
	last, _ := utf8.DecodeLastRuneInString(text)

	return unicode.IsLetter(last)
}
