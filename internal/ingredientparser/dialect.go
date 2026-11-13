package ingredientparser

import (
	"cmp"
	_ "embed"
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

type dialect struct {
	approximate   *regexp.Regexp
	connective    *regexp.Regexp
	optional      *regexp.Regexp
	amount        *regexp.Regexp
	modifier      *regexp.Regexp
	modifierAfter *regexp.Regexp
	modifiers     []string
	orWords       []string
	plusWords     []string

	nameFirst       bool
	unspaced        bool
	prefixUnit      *regexp.Regexp
	prefixUnitTail  *regexp.Regexp
	approximateTail *regexp.Regexp
	opaque          []string
	impliedOne      []string
	numberWords     map[string]string
	mixedNumber     *regexp.Regexp
	articles        []string
	multipliers     map[string]string
	vague           []string
	vagueAfter      []string
	distributive    []string
	bounds          []string
	between         []string
	minusWords      []string
	fractionOf      []string
	// Ingredient-part phrases mapped to preparation notes.
	parts       map[string]string
	sizes       []string
	descriptors []string
	// Unregistered count descriptors retained as notes.
	portionWords   []string
	liquids        []string
	unmeasuredNote *regexp.Regexp
	unmeasuredRun  *regexp.Regexp
	// Ingredient names whose first word also names a unit.
	compounds []string
	// Unit interpretations selected by ingredient context.
	senses []sense
}

type sense struct {
	words []string
	unit  string
	name  *regexp.Regexp
}

type senseSpec struct {
	Words []string `json:"words"`
	Unit  string   `json:"unit"`
	Name  string   `json:"name"`
}

type dialectSpec struct {
	Locale              string            `json:"locale"`
	Approximate         []string          `json:"approximate,omitempty"`
	ApproximateAttached []string          `json:"approximate_attached,omitempty"`
	Connective          string            `json:"connective,omitempty"`
	RangeWords          []string          `json:"range_words,omitempty"`
	OrWords             []string          `json:"or_words,omitempty"`
	PlusWords           []string          `json:"plus_words,omitempty"`
	Optional            []string          `json:"optional,omitempty"`
	Modifiers           []string          `json:"modifiers,omitempty"`
	ModifiersAfter      []string          `json:"modifiers_after,omitempty"`
	NumberWords         map[string]string `json:"number_words,omitempty"`
	MixedJoiner         string            `json:"mixed_joiner,omitempty"`
	Articles            []string          `json:"articles,omitempty"`
	Multipliers         map[string]string `json:"multipliers,omitempty"`
	Vague               []string          `json:"vague,omitempty"`
	VagueAfter          []string          `json:"vague_after,omitempty"`
	Distributive        []string          `json:"distributive,omitempty"`
	Bounds              []string          `json:"bounds,omitempty"`
	Between             []string          `json:"between,omitempty"`
	MinusWords          []string          `json:"minus_words,omitempty"`
	FractionOf          []string          `json:"fraction_of,omitempty"`
	Parts               map[string]string `json:"parts,omitempty"`
	Sizes               []string          `json:"sizes,omitempty"`
	Descriptors         []string          `json:"descriptors,omitempty"`
	PortionWords        []string          `json:"portion_words,omitempty"`
	Liquids             []string          `json:"liquids,omitempty"`
	UnmeasuredNotes     []string          `json:"unmeasured_notes,omitempty"`
	NameFirst           bool              `json:"name_first,omitempty"`
	Unspaced            bool              `json:"unspaced,omitempty"`
	PrefixUnits         []string          `json:"prefix_units,omitempty"`
	Opaque              []string          `json:"opaque,omitempty"`
	ImpliedOne          []string          `json:"implied_one,omitempty"`
	Compounds           []string          `json:"compounds,omitempty"`
	Senses              []senseSpec       `json:"senses,omitempty"`
}

var (
	dialects   = map[string]*dialect{}
	dialectsMu sync.Mutex
)

func dialectFor(locale string) *dialect {
	base := cmp.Or(grammarLanguage(locale), "en")

	dialectsMu.Lock()
	defer dialectsMu.Unlock()

	if built, found := dialects[base]; found {
		return built
	}

	spec, known := dialectSpecs[base]
	if !known {
		base, spec = "en", dialectSpecs["en"]
	}

	built := compileDialect(spec)
	dialects[base] = built

	return built
}

func alternation(words []string) string {
	sorted := append([]string(nil), words...)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })

	quoted := make([]string, len(sorted))
	for index, word := range sorted {
		quoted[index] = regexp.QuoteMeta(word)
	}

	return strings.Join(quoted, "|")
}

func compileDialect(spec dialectSpec) *dialect {
	built := &dialect{
		orWords:   lowerAll(spec.OrWords),
		plusWords: append([]string{"plus"}, lowerAll(spec.PlusWords)...),
		nameFirst: spec.NameFirst,
		unspaced:  spec.Unspaced,
		opaque:    lowerAll(spec.Opaque),

		impliedOne:   lowerAll(spec.ImpliedOne),
		compounds:    lowerAll(spec.Compounds),
		numberWords:  spec.NumberWords,
		articles:     lowerAll(spec.Articles),
		modifiers:    lowerAll(spec.Modifiers),
		multipliers:  spec.Multipliers,
		vague:        lowerAll(spec.Vague),
		vagueAfter:   lowerAll(spec.VagueAfter),
		distributive: lowerAll(spec.Distributive),
		bounds:       lowerAll(spec.Bounds),
		between:      lowerAll(spec.Between),
		minusWords:   lowerAll(spec.MinusWords),
		fractionOf:   lowerAll(spec.FractionOf),
		parts:        spec.Parts,
		sizes:        lowerAll(spec.Sizes),
		descriptors:  lowerAll(spec.Descriptors),
		portionWords: lowerAll(spec.PortionWords),
		liquids:      lowerAll(spec.Liquids),
	}

	if len(spec.UnmeasuredNotes) > 0 {
		built.unmeasuredNote = regexp.MustCompile(`(?i)^(?:` + alternation(spec.UnmeasuredNotes) + `)` + wordEnd)
		built.unmeasuredRun = regexp.MustCompile(`(?i)^(?:` + alternation(spec.UnmeasuredNotes) + `)(?:\s+(?:` + alternation(spec.UnmeasuredNotes) + `))*$`)
	}

	for _, contextual := range spec.Senses {
		built.senses = append(built.senses, sense{
			words: lowerAll(contextual.Words), unit: contextual.Unit,
			name: regexp.MustCompile(`(?i)^(?:` + contextual.Name + `)`),
		})
	}

	approximate := `~\s+`
	if len(spec.Approximate) > 0 {
		approximate = `(?:` + alternation(spec.Approximate) + `|~)\s+`
	}

	if len(spec.ApproximateAttached) > 0 {
		approximate = `(?:` + alternation(spec.ApproximateAttached) + `)\s*|` + approximate
		built.approximateTail = regexp.MustCompile(`(?i)\s*(?:` + alternation(spec.ApproximateAttached) + `)\s*$`)
	}

	built.approximate = regexp.MustCompile(`(?i)^(?:` + approximate + `)`)

	if spec.MixedJoiner != "" {
		built.mixedNumber = regexp.MustCompile(`(?i)(\pN)\s*` + regexp.QuoteMeta(spec.MixedJoiner) + `\s*(\pN+/\pN+)`)
	}

	if len(spec.Modifiers) > 0 {
		built.modifier = regexp.MustCompile(`(?i)^(` + alternation(spec.Modifiers) + `)\s+`)
	}

	if len(spec.ModifiersAfter) > 0 {
		built.modifierAfter = regexp.MustCompile(`(?i)^(` + alternation(spec.ModifiersAfter) + `)` + wordEnd)
	}

	if spec.Connective != "" {
		built.connective = regexp.MustCompile(`(?i)^(?:` + spec.Connective + `)`)
	}

	optional := "optional"
	if len(spec.Optional) > 0 {
		optional = alternation(append([]string{"optional"}, spec.Optional...))
	}

	built.optional = regexp.MustCompile(`(?i)^\s*(?:` + optional + `)\s*[:\-–—]\s*` +
		`|\s*[\(\[（［]\s*(?:` + optional + `)\s*[\)\]）］]` +
		`|\s*[,;，、；\-–—]\s*(?:` + optional + `)\s*$` +
		`|\s+(?:` + optional + `)\s*$`)

	separators := `\s*(?:-|–|—|〜|～|~|to)\s*`
	if len(spec.RangeWords) > 0 {
		separators += `|\s+(?:` + alternation(spec.RangeWords) + `)\s+`
	}

	built.amount = regexp.MustCompile(`^(` + amountPattern + `)(?:(?:` + separators + `)(` + amountPattern + `))?(?:\s+|$)(.*)$`)

	if len(spec.PrefixUnits) > 0 {
		units := alternation(spec.PrefixUnits)
		amounts := `(?:` + amountPattern + `)(?:(?:` + separators + `)(?:` + amountPattern + `))?`
		built.prefixUnit = regexp.MustCompile(`^(` + units + `)\s*(` + amounts + `)(?:\s+(.*))?$`)
		built.prefixUnitTail = regexp.MustCompile(`^(.*?)\s*(` + units + `)\s*(` + amounts + `)$`)
	}

	return built
}

func lowerAll(values []string) []string {
	lowered := make([]string, len(values))
	for index, value := range values {
		lowered[index] = strings.ToLower(value)
	}

	return lowered
}

// cutWord removes a leading word and the space after it, ignoring case.
func cutWord(value, word string) (string, bool) {
	if len(value) <= len(word) || value[len(word)] != ' ' || !strings.EqualFold(value[:len(word)], word) {
		return "", false
	}

	return strings.TrimSpace(value[len(word)+1:]), true
}

func (d *dialect) cutOr(value string) (string, bool) {
	for _, word := range d.orWords {
		if rest, ok := cutWord(value, word); ok {
			return rest, true
		}
	}

	return "", false
}

func (d *dialect) cutPlus(value string) (string, bool) {
	for _, word := range d.plusWords {
		if rest, ok := cutWord(value, word); ok {
			return rest, true
		}
	}

	return "", false
}

func (d *dialect) isOpaqueMeasure(value string) bool {
	lowered := strings.ToLower(strings.TrimSpace(value))

	for _, word := range d.opaque {
		rest, found := strings.CutPrefix(lowered, word)
		if !found {
			continue
		}

		runes := []rune(rest)
		if len(runes) == 0 || !unicode.IsLetter(runes[0]) || isUnspacedScript([]rune(word)[len([]rune(word))-1]) {
			return true
		}
	}

	return false
}

func (d *dialect) describesOpaqueMeasure(line string) bool {
	lowered := strings.ToLower(line)

	for _, word := range d.opaque {
		if utf8.RuneCountInString(word) > 1 && strings.Contains(lowered, word) {
			return true
		}
	}

	return false
}

// isUnspacedScript reports a script written without spaces between words.
func isUnspacedScript(character rune) bool {
	return unicode.In(character, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Thai)
}

// digitZeroes holds the first code point of each decimal digit run.
var digitZeroes = []rune{
	0x30, 0x660, 0x6f0, 0x7c0, 0x966, 0x9e6, 0xa66, 0xae6, 0xb66, 0xbe6, 0xc66, 0xce6, 0xd66, 0xe50,
	0xed0, 0xf20, 0x1040, 0xff10,
}

var asciiForms = strings.NewReplacer(
	"٫", ".", "٬", "", "،", ",", "／", "/", "．", ".", "，", ",", "－", "-", "＋", "+", "～", "〜",
	"、", ",", "（", "(", "）", ")",
)

func asciiDigits(value string) string {
	var out strings.Builder

	for _, character := range asciiForms.Replace(value) {
		converted := false

		if character > unicode.MaxASCII {
			for _, zero := range digitZeroes {
				if character >= zero && character < zero+10 {
					out.WriteRune('0' + character - zero)

					converted = true

					break
				}
			}
		}

		if !converted {
			out.WriteRune(character)
		}
	}

	return out.String()
}

var (
	digitThenLetter = regexp.MustCompile(`(\d)(\p{L})`)
	letterThenDigit = regexp.MustCompile(`(\p{L})(\d)`)
)

// Reorder quantities as amount, unit, then ingredient; report any removed approximation marker.
func (d *dialect) reorder(line string, read reading) (string, bool) {
	if !d.nameFirst && len(d.impliedOne) != 0 && !d.amount.MatchString(line) {
		if first, rest, found := strings.Cut(line, " "); found && strings.TrimSpace(rest) != "" && d.impliesOne(first) {
			return "1 " + line, false
		}
	}

	if !d.amount.MatchString(line) {
		if rewritten, approximate, found := d.reorderSeparated(line, read); found {
			return rewritten, approximate
		}

		if leadingMeasure(line, read) {
			if match := trailingBareAmount.FindStringSubmatch(line); match != nil {
				return match[2] + " " + match[1], false
			}

			return "1 " + line, false
		}
	}

	if d.prefixUnit != nil {
		if match := d.prefixUnit.FindStringSubmatch(line); match != nil && strings.TrimSpace(match[3]) != "" {
			return match[2] + " " + match[1] + " " + strings.TrimSpace(match[3]), false
		}
	}

	if d.amount.MatchString(line) {
		return line, false
	}

	body, note := splitTrailingNote(line)

	rewritten, approximate, found := d.reorderBody(body, read)
	if !found {
		// Retry the full line when the comma separates the ingredient from its quantity.
		if rewritten, approximate, found = d.reorderBody(line, read); found && note != "" {
			return rewritten, approximate
		}

		return line, false
	}

	if note != "" {
		rewritten += ", " + note
	}

	return rewritten, approximate
}

var trailingParenthesis = regexp.MustCompile(`^(.*?)\s*\(([^()]*)\)$`)

func splitTrailingNote(line string) (string, string) {
	body, note := splitNote(line)

	if match := trailingParenthesis.FindStringSubmatch(body); match != nil && strings.TrimSpace(match[1]) != "" {
		body = strings.TrimSpace(match[1])
		note = strings.TrimSpace(strings.Join([]string{match[2], note}, ", "))
		note = strings.Trim(note, ", ")
	}

	return body, note
}

// reorderBody moves the quantity at the end of a line to its front.
func (d *dialect) reorderBody(line string, read reading) (string, bool, bool) {
	if d.prefixUnitTail != nil {
		if match := d.prefixUnitTail.FindStringSubmatch(line); match != nil {
			if head, approximate := d.splitApproximate(trimLeader(match[1])); head != "" {
				return match[3] + " " + match[2] + " " + head, approximate, true
			}
		}
	}

	spaced := letterThenDigit.ReplaceAllString(digitThenLetter.ReplaceAllString(line, "$1 $2"), "$1 $2")
	tokens := strings.Fields(spaced)

	probe := read
	probe.walk = nil
	probe.discovering = false

	for take := 1; take <= 3 && take < len(tokens); take++ {
		tail := strings.Join(tokens[len(tokens)-take:], " ")

		// Keep mixed-number components together when splitting the quantity.
		head, approximate := d.splitApproximate(trimLeader(strings.Join(tokens[:len(tokens)-take], " ")))
		if head == "" || beginsAmount(tokens[len(tokens)-take-1]) {
			continue
		}

		rewritten := tail + " " + head
		if d.impliesOne(tail) {
			rewritten = "1 " + rewritten
		}

		if !d.amount.MatchString(rewritten) {
			continue
		}

		// Reject splits that leave quantity text in the ingredient name.
		parsed := parseStatedAs(rewritten, "probe", rewritten, probe)
		if parsed.Quantity != nil && parsed.Name != head && strings.HasSuffix(parsed.Name, head) {
			continue
		}

		// Require a unit to avoid reordering names such as "Vitamin B 12".
		if !d.nameFirst && !d.endsInUnit(tail, read) {
			continue
		}

		if parsed.Quantity != nil || parsed.Normalization != nil && parsed.Normalization.Method == recipe.NormalizationUnresolved {
			return rewritten, approximate, true
		}
	}

	return line, false, false
}

var trailingBareAmount = regexp.MustCompile(`^(.*\S)\s+(\pN+(?:[.,/]\pN+)?)$`)

var separator = regexp.MustCompile(`\s+[-–—]+\s*|\s*[-–—]+\s+|\s*[:：]\s*|\s*(?:[・‥…]{2,}|…|\.{3,})\s*`)

func (d *dialect) reorderSeparated(line string, read reading) (string, bool, bool) {
	at := separator.FindStringIndex(line)
	if at == nil {
		return "", false, false
	}

	head := strings.TrimSpace(line[:at[0]])

	tail, approximate := d.takeApproximate(strings.TrimSpace(line[at[1]:]))
	if head == "" || strings.ContainsAny(head, "0123456789") || !beginsAmount(tail) {
		return "", false, false
	}

	probe := read
	probe.walk, probe.discovering = nil, false

	rewritten := normalizeAmounts(tail) + " " + head
	parsed := parseStatedAs(rewritten, "probe", rewritten, probe)

	if parsed.Quantity == nil || parsed.Name == "" || !strings.Contains(head, parsed.Name) {
		return "", false, false
	}

	return rewritten, approximate, true
}

func leadingMeasure(line string, read reading) bool {
	for _, split := range unitSplits(line, read) {
		// Require a word boundary to avoid interpreting prefixes of ingredient names as units.
		if next, _ := utf8.DecodeRuneInString(line[len(split.form):]); !unicode.IsSpace(next) {
			continue
		}

		descriptor, found := catalog.DescribeAlias(split.form)
		if !found || beginsAmount(split.rest) {
			continue
		}

		if _, resolution := catalog.ResolveOccurrence(units.Occurrence{Form: descriptor.AuthoredForm}, read.locale, units.Decisions{}); resolution == units.AliasUnknown {
			continue
		}

		for _, id := range descriptor.CandidateUnitIDs {
			if !impliesOneMeasure(id) {
				return false
			}
		}

		return true
	}

	return false
}

func impliesOneMeasure(id string) bool {
	switch id {
	case "count.pinch", "count.handful", "count.dash", "count.drop", "count.splash", "count.bunch", "count.sprig":
		return true
	}

	return strings.HasPrefix(id, "volume.")
}

func (d *dialect) endsInUnit(tail string, read reading) bool {
	match := d.amount.FindStringSubmatch(tail)
	if match == nil {
		return false
	}

	descriptor, found := catalog.DescribeAlias(strings.TrimSpace(match[len(match)-1]))
	if !found {
		return false
	}

	_, resolution := catalog.ResolveOccurrence(units.Occurrence{Form: descriptor.AuthoredForm}, read.locale, units.Decisions{})

	return resolution != units.AliasUnknown
}

func trimLeader(name string) string {
	return strings.TrimRightFunc(name, func(character rune) bool {
		return unicode.IsSpace(character) || strings.ContainsRune("…‥・.．:：-–—,，、", character)
	})
}

func (d *dialect) namesCompound(value string) bool {
	lowered := strings.ToLower(strings.TrimSpace(value))

	for _, name := range d.compounds {
		if rest, found := strings.CutPrefix(lowered, name); found && (rest == "" || rest[0] == ' ' || rest[0] == ',') {
			return true
		}
	}

	return false
}

func (d *dialect) senseOf(value string) (string, string, bool) {
	for _, contextual := range d.senses {
		for _, word := range contextual.words {
			if len(value) < len(word) || !strings.EqualFold(value[:len(word)], word) {
				continue
			}

			after := value[len(word):]

			last, _ := utf8.DecodeLastRuneInString(word)
			if after != "" && !isUnspacedScript(last) && after[0] != ' ' {
				continue
			}

			name := strings.TrimSpace(after)
			if d.connective != nil {
				name = strings.TrimSpace(d.connective.ReplaceAllString(name, ""))
			}

			if contextual.name.MatchString(name) {
				return contextual.unit, strings.TrimSpace(after), true
			}
		}
	}

	return "", "", false
}

func (d *dialect) takeApproximate(value string) (string, bool) {
	prefix := d.approximate.FindString(value)
	if prefix == "" {
		return value, false
	}

	return strings.TrimSpace(strings.TrimPrefix(value, prefix)), true
}

// RE2's \b is ASCII-only; use Unicode word boundaries.
const wordEnd = `(?:$|[^\pL\pN])`

func (d *dialect) takeModifierAfter(value string) (string, string) {
	if d.modifierAfter == nil {
		return "", value
	}

	match := d.modifierAfter.FindStringSubmatch(value)
	if match == nil {
		return "", value
	}

	return match[1], strings.TrimLeft(strings.TrimSpace(value[len(match[1]):]), ", ")
}

func (d *dialect) takeModifier(value string) (string, string) {
	if d.modifier == nil {
		return "", value
	}

	match := d.modifier.FindStringSubmatch(value)
	if match == nil {
		return "", value
	}

	return match[1], strings.TrimSpace(value[len(match[0]):])
}

func (d *dialect) spellNumber(line string, read reading) string {
	first, rest, found := strings.Cut(line, " ")
	if !found {
		return line
	}

	lowered := strings.ToLower(first)

	if word, after, cut := strings.Cut(rest, " "); cut && slices.Contains(d.articles, lowered) {
		if value, known := d.numberWords[strings.ToLower(word)]; known {
			return value + " " + after
		}
	}

	if value, known := d.numberWords[lowered]; known {
		if article, after, cut := strings.Cut(rest, " "); cut && slices.Contains(d.articles, strings.ToLower(article)) {
			rest = after
		}

		return value + " " + rest
	}

	if slices.Contains(d.articles, lowered) && d.measureFollows(rest, read) {
		if moved, found := d.moveModifier(rest); found {
			return moved
		}

		return "1 " + rest
	}

	if moved, found := d.moveModifier(line); found {
		return moved
	}

	return line
}

func (d *dialect) moveModifier(line string) (string, bool) {
	word, rest, found := cutAnyWord(line, d.modifiers)
	if !found || !beginsAmount(rest) {
		return line, false
	}

	match := d.amount.FindStringSubmatch(rest)
	if len(match) != 4 || match[3] == "" {
		return line, false
	}

	amount := strings.TrimSpace(rest[:len(rest)-len(match[3])])

	return amount + " " + word + " " + strings.TrimSpace(match[3]), true
}

func (d *dialect) splitUnmeasured(line string) (string, string) {
	if d.unmeasuredNote == nil {
		return line, ""
	}

	if name, note := splitNote(line); name != "" && note != "" && d.unmeasuredNote.MatchString(note) {
		return name, note
	}

	for index, character := range line {
		if index == 0 || !unicode.IsSpace(character) {
			continue
		}

		if note := strings.TrimSpace(line[index:]); d.unmeasuredNote.MatchString(note) &&
			(d.unmeasuredNote.FindString(note) == note || d.unmeasuredRun.MatchString(note)) {
			return strings.TrimSpace(line[:index]), note
		}
	}

	return line, ""
}

func (d *dialect) impliesOne(word string) bool {
	return slices.Contains(d.impliedOne, strings.ToLower(word))
}

func (d *dialect) splitApproximate(head string) (string, bool) {
	if d.approximateTail != nil && d.approximateTail.MatchString(head) {
		return strings.TrimSpace(d.approximateTail.ReplaceAllString(head, "")), true
	}

	return strings.TrimSpace(head), false
}

//go:embed dialects.json
var dialectsJSON []byte

var (
	dialectSpecs                     = readDialects()
	dialectLanguages, dialectMatcher = dialectTags()
)

func grammarLanguage(locale string) string {
	tag := language.Make(strings.TrimSpace(locale))
	if base, _ := tag.Base(); base.String() != "und" {
		if _, known := dialectSpecs[base.String()]; known {
			return base.String()
		}
	}

	// An English fallback match does not establish the source language.
	if _, index, confidence := dialectMatcher.Match(tag); index > 0 && confidence >= language.High {
		return dialectLanguages[index]
	}

	return ""
}

func dialectTags() ([]string, language.Matcher) {
	languages := slices.Sorted(maps.Keys(dialectSpecs))
	languages = append([]string{"en"}, slices.DeleteFunc(languages, func(code string) bool { return code == "en" })...)

	tags := make([]language.Tag, len(languages))
	for index, code := range languages {
		tags[index] = language.Make(code)
	}

	return languages, language.NewMatcher(tags)
}

func readDialects() map[string]dialectSpec {
	var specs []dialectSpec
	if err := json.Unmarshal(dialectsJSON, &specs); err != nil {
		panic(err)
	}

	byLocale := make(map[string]dialectSpec, len(specs))
	for _, spec := range specs {
		byLocale[spec.Locale] = spec
	}

	return byLocale
}

// ReadingLanguage infers the ingredient language when the declaration is English or absent.
func ReadingLanguage(lines []string, stated string) string {
	if base, _ := language.Make(stated).Base(); stated != "" && base.String() != AliasLanguage {
		return stated
	}

	statedScore := measuredLines(lines, cmp.Or(stated, AliasLanguage))
	best, bestScore := stated, statedScore

	for _, candidate := range dialectLanguages {
		if score := measuredLines(lines, candidate); score > bestScore {
			best, bestScore = candidate, score
		}
	}

	if bestScore >= 3 && bestScore >= 2*statedScore && best != stated {
		return best
	}

	return stated
}

func measuredLines(lines []string, locale string) int {
	count := 0

	for _, line := range lines {
		parsed := ParseDetailed(line, "probe", locale)
		quantity := parsed.Ingredient.Quantity

		known := slices.ContainsFunc(parsed.Ambiguities, func(ambiguity Ambiguity) bool { return len(ambiguity.CandidateUnitIDs) > 1 })
		if known || quantity != nil && quantity.Kind == recipe.QuantitySimple && quantity.Unit != "" && quantity.Unit != "count.item" {
			count++
		}
	}

	return count
}
