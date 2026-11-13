package recipetext

import (
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Fact kinds returned by Fact.
const (
	FactYield = "yield"
	FactPrep  = "prep"
	FactCook  = "cook"
	FactTotal = "total"
)

type factLabel struct {
	kind, label string
	runes       int
}

var (
	factLabels    = buildFactLabels()
	durationWords = buildDurationWords()
	durationPart  = buildDurationPart()
	durationGap   = regexp.MustCompile(`^[\s,;+&]*`)
)

func buildFactLabels() []factLabel {
	var labels []factLabel

	for _, entry := range vocabularies {
		for kind, words := range entry.Facts {
			for _, word := range words {
				labels = append(labels, factLabel{kind: kind, label: word, runes: utf8.RuneCountInString(word)})
			}
		}
	}

	sort.SliceStable(labels, func(i, j int) bool { return labels[i].runes > labels[j].runes })

	return labels
}

var servingLine = buildServingLine()

func buildServingLine() *regexp.Regexp {
	var words []string

	for _, entry := range vocabularies {
		for _, word := range entry.Facts[FactYield] {
			if !strings.Contains(word, " ") {
				words = append(words, word)
			}
		}
	}

	return regexp.MustCompile(`(?i)^(?:\pL{1,5}\s+)?\pN+(?:\s*[-–]\s*\pN+)?\s*` + alternatives(words) + `$`)
}

// ServingLine recognizes a standalone serving count, such as "6 people".
func ServingLine(line string) bool {
	return servingLine.MatchString(strings.TrimSpace(line))
}

// Fact returns the kind and value of a labeled line such as "Prep time: 20 min".
// Labels need a colon, except a yield label followed by a number ("Serves 4").
func Fact(line string) (kind, value string) {
	text := strings.TrimSpace(line)

	for _, candidate := range factLabels {
		if utf8.RuneCountInString(text) <= candidate.runes {
			continue
		}

		cut := len(string([]rune(text)[:candidate.runes]))
		if !strings.EqualFold(text[:cut], candidate.label) {
			continue
		}

		rest := text[cut:]
		if next, _ := utf8.DecodeRuneInString(rest); unicode.IsLetter(next) || unicode.IsDigit(next) {
			continue
		}

		rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
		if after, found := strings.CutPrefix(rest, ":"); found {
			rest = after
		} else if after, found := strings.CutPrefix(rest, "："); found {
			rest = after
		} else if first, _ := utf8.DecodeRuneInString(rest); candidate.kind != FactYield || !unicode.IsDigit(first) {
			continue
		}

		if value := strings.TrimSpace(rest); value != "" {
			return candidate.kind, value
		}
	}

	return "", ""
}

func buildDurationWords() map[string]*big.Rat {
	seconds := map[string]int64{"second": 1, "minute": 60, "hour": 60 * 60, "day": 24 * 60 * 60}
	words := make(map[string]*big.Rat)
	ambiguous := make(map[string]bool)

	for _, entry := range vocabularies {
		for unit, size := range seconds {
			for _, word := range entry.Units[unit] {
				word = strings.ToLower(word)
				if previous, found := words[word]; found && previous.Cmp(big.NewRat(size, 1)) != 0 {
					ambiguous[word] = true
				}

				words[word] = big.NewRat(size, 1)
			}
		}
	}

	for word := range ambiguous {
		delete(words, word)
	}

	return words
}

func buildDurationPart() *regexp.Regexp {
	units := make([]string, 0, len(durationWords))
	for word := range durationWords {
		units = append(units, word)
	}

	return regexp.MustCompile(`(?i)^([\p{Nd}]+(?:[.,٫][\p{Nd}]+)?)\s*` + alternatives(units))
}

// Duration parses amounts and time units from supported recipe languages.
func Duration(text string) (time.Duration, bool) {
	rest := strings.TrimSpace(text)
	total := new(big.Rat)
	parts := 0

	for rest != "" {
		match := durationPart.FindStringSubmatchIndex(rest)
		if match == nil {
			return 0, false
		}

		if next, _ := utf8.DecodeRuneInString(rest[match[1]:]); unicode.IsLetter(next) {
			return 0, false
		}

		amount, ok := new(big.Rat).SetString(NumericText(rest[match[2]:match[3]]))
		if !ok {
			return 0, false
		}

		unit := durationWords[strings.ToLower(strings.TrimSpace(rest[match[3]:match[1]]))]
		total.Add(total, amount.Mul(amount, unit))

		parts++
		rest = strings.TrimSpace(durationGap.ReplaceAllString(rest[match[1]:], ""))
	}

	limit := new(big.Rat).SetInt64(int64(time.Duration(1<<63-1) / time.Second))
	if parts == 0 || !total.IsInt() || total.Sign() <= 0 || total.Cmp(limit) > 0 {
		return 0, false
	}

	return time.Duration(total.Num().Int64()) * time.Second, true
}
