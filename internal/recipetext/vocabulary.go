// Package recipetext recognizes explicit source wording independently of interface translations.
package recipetext

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

//go:embed vocabulary.json
var vocabularyJSON []byte

type vocabulary struct {
	Locale       string              `json:"locale"`
	Ingredients  []string            `json:"ingredients"`
	Instructions []string            `json:"instructions"`
	Actions      map[string][]string `json:"actions"`
	Units        map[string][]string `json:"units"`
	Cadence      []string            `json:"cadence"`
	Span         string              `json:"span"`
	Compact      bool                `json:"compact"`
}

var vocabularies = readVocabulary()

func readVocabulary() []vocabulary {
	var result []vocabulary
	if err := json.Unmarshal(vocabularyJSON, &result); err != nil {
		panic(err)
	}

	return result
}

// Heading identifies a source section and its reading vocabulary, without assigning a recipe language.
func Heading(text string) (section, locale string) {
	text = strings.ToLower(strings.TrimFunc(text, func(character rune) bool {
		return unicode.IsSpace(character) || strings.ContainsRune(":：-", character)
	}))

	var matches []string

	for _, entry := range vocabularies {
		if slices.Contains(entry.Ingredients, text) {
			section = "ingredients"

			matches = append(matches, entry.Locale)
		}

		if slices.Contains(entry.Instructions, text) {
			section = "instructions"

			matches = append(matches, entry.Locale)
		}
	}

	if len(matches) == 1 {
		locale = matches[0]
	}

	return section, locale
}

// Timing keeps the original duration wording beside its recognized numeric fields.
type Timing struct {
	Kind, Amount, Maximum, Unit, Source string
}

type timingPattern struct {
	kind     string
	unit     string
	action   *regexp.Regexp
	cadence  *regexp.Regexp
	duration *regexp.Regexp
}

var (
	sentenceBoundary = regexp.MustCompile(`[!?;\n。！？；]|\.(?:\s|$)`)
	patterns         = buildPatterns()
)

func alternatives(words []string) string {
	quoted := make([]string, len(words))
	for index, word := range words {
		quoted[index] = regexp.QuoteMeta(word)
	}

	sort.Slice(quoted, func(i, j int) bool { return len(quoted[i]) > len(quoted[j]) })

	return "(?:" + strings.Join(quoted, "|") + ")"
}

func buildPatterns() []timingPattern {
	var result []timingPattern

	amount := `([\p{Nd}]+(?:[.,٫][\p{Nd}]+)?)`

	for _, entry := range vocabularies {
		if entry.Locale == "en" {
			continue
		}

		before, after := `(?:^|[^\p{L}\p{N}_])`, `(?:$|[^\p{L}\p{N}_])`
		if entry.Compact {
			before, after = "", ""
		}

		cadence := regexp.MustCompile(`(?i)` + before + alternatives(entry.Cadence) + after)
		for _, kind := range []string{"cook", "rest"} {
			action := regexp.MustCompile(`(?i)` + before + alternatives(entry.Actions[kind]) + after)
			for _, unit := range []string{"second", "minute", "hour", "day"} {
				span := `(?:-|–|—|` + regexp.QuoteMeta(entry.Span) + `)`
				duration := regexp.MustCompile(`(?i)(?:^|[^\p{N}.,٫٬/'’+−\-])(` + amount + `\s*(?:` + span + `\s*` + amount + `\s*)?` + alternatives(entry.Units[unit]) + `)` + after)
				result = append(result, timingPattern{kind, unit, action, cadence, duration})
			}
		}
	}

	return result
}

// Timings recognizes explicit localized cooking and resting durations. Cadences stay as prose.
func Timings(text string) []Timing {
	var result []Timing
	for _, clause := range sentenceBoundary.Split(text, -1) {
		result = append(result, clauseTimings(clause)...)
	}

	return result
}

func clauseTimings(text string) []Timing {
	var found []Timing

	seen := make(map[[2]int]bool)
	kinds := make(map[string]bool)

	for _, pattern := range patterns {
		if pattern.action.MatchString(text) {
			kinds[pattern.kind] = true
		}
	}

	if len(kinds) != 1 {
		return nil
	}

	for _, pattern := range patterns {
		action := pattern.action.FindStringIndex(text)
		if action == nil || pattern.cadence.MatchString(text) {
			continue
		}

		for _, match := range pattern.duration.FindAllStringSubmatchIndex(text, -1) {
			start, end := match[2], match[3]

			prefix := strings.TrimSpace(text[:start])

			previous, _ := utf8.DecodeLastRuneInString(prefix)
			if unicode.IsDigit(previous) || strings.ContainsRune("/+-−'’.,٫٬", previous) {
				continue
			}

			gap := ""
			if end < action[0] {
				gap = text[end:action[0]]
			} else if start > action[1] {
				gap = text[action[1]:start]
			}

			if utf8.RuneCountInString(gap) > 80 {
				continue
			}

			key := [2]int{start, end}
			if seen[key] {
				continue
			}

			seen[key] = true

			maximum := ""
			if match[6] >= 0 {
				maximum = NumericText(text[match[6]:match[7]])
			}

			found = append(found, Timing{
				Kind: pattern.kind, Amount: NumericText(text[match[4]:match[5]]), Maximum: maximum,
				Unit: pattern.unit, Source: strings.TrimSpace(text[start:end]),
			})
		}
	}

	if len(found) > 1 {
		return nil
	}

	return found
}

// NumericText normalizes decimal digits without changing surrounding authored text.
func NumericText(text string) string {
	return strings.Map(func(character rune) rune {
		if character == '٫' || character == ',' {
			return '.'
		}

		for _, span := range unicode.Nd.R16 {
			if character >= rune(span.Lo) && character <= rune(span.Hi) && (character-rune(span.Lo))%rune(span.Stride) == 0 {
				return '0' + ((character-rune(span.Lo))/rune(span.Stride))%10
			}
		}

		for _, span := range unicode.Nd.R32 {
			value, low, high, stride := int64(character), int64(span.Lo), int64(span.Hi), int64(span.Stride)
			if value >= low && value <= high && (value-low)%stride == 0 {
				return '0' + rune(((value-low)/stride)%10)
			}
		}

		return character
	}, text)
}

var instructionPattern = buildInstructionPattern()

func buildInstructionPattern() *regexp.Regexp {
	var patterns []string

	for _, entry := range vocabularies {
		words := append(slices.Clone(entry.Actions["cook"]), entry.Actions["rest"]...)
		words = append(words, entry.Actions["prepare"]...)

		before, after := `^`, `(?:$|[^\p{L}\p{N}_])`
		if entry.Compact {
			before, after = "", ""
		}

		patterns = append(patterns, before+alternatives(words)+after)
	}

	return regexp.MustCompile(`(?i)` + strings.Join(patterns, "|"))
}

// InstructionStart recognizes cooking actions in the source vocabularies.
func InstructionStart(text string) bool {
	return instructionPattern.MatchString(text)
}
