package ingredientparser

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"

	"github.com/savorum/savorum/internal/recipe"
)

// dialect is what a reading language changes about the grammar of an
// ingredient line: the words that mark an approximate or optional amount, join
// a measure to what it measures, or separate a range or an alternative, and the
// order in which the language writes a quantity.
//
// The units themselves are not here. They come from the registry's alias sets
// for the same language.
type dialect struct {
	approximate *regexp.Regexp
	connective  *regexp.Regexp
	optional    *regexp.Regexp
	amount      *regexp.Regexp
	orWords     []string
	plusWords   []string

	// nameFirst languages list the ingredient before its quantity: 小麦粉 200g.
	nameFirst bool
	// unspaced languages write a unit against the word after it: 2個卵.
	unspaced bool
	// prefixUnit matches a unit written before its number: 大さじ2.
	prefixUnit *regexp.Regexp
	// prefixUnitTail matches that at the end of a line: サラダ油 大さじ2.
	prefixUnitTail *regexp.Regexp
	// approximateTail matches an approximation word against the end of what
	// precedes a quantity: じゃがいも 約3個.
	approximateTail *regexp.Regexp
	// opaque lists measures the registry cannot name because their size
	// differs between countries. A line that uses one stays as written.
	opaque []string
	// impliedOne lists measure words that contain the number one: ひとつまみ is
	// "one pinch". A line that leads or, for a nameFirst language, ends with one
	// and states no other amount is read as an amount of one.
	impliedOne []string
	// compounds lists fixed names that begin with a word that is also a unit: in
	// "bouquet garni" the first word is part of the ingredient's name.
	compounds []string
	// senses read a broad word as a unit only when the ingredient after it
	// selects that sense: "gousse d'ail" is a clove, "gousse de vanille" is not.
	senses []sense
}

// sense is one contextual reading of a word the registry gives no alias.
type sense struct {
	words []string
	unit  string
	// name matches the start of the ingredient, after any connective.
	name *regexp.Regexp
}

type senseSpec struct {
	words []string
	unit  string
	name  string
}

type dialectSpec struct {
	approximate []string
	// approximateAttached are approximation words written against the number.
	approximateAttached []string
	connective          string
	rangeWords          []string
	orWords             []string
	optional            []string
	nameFirst           bool
	unspaced            bool
	prefixUnits         []string
	opaque              []string
	impliedOne          []string
	compounds           []string
	senses              []senseSpec
}

var (
	dialects   = map[string]*dialect{}
	dialectsMu sync.Mutex
)

// dialectFor returns the grammar for the primary language of a locale. A
// locale that names no language, or one without a table, reads as English.
func dialectFor(locale string) *dialect {
	base := "en"

	if strings.TrimSpace(locale) != "" {
		if tag, err := language.Parse(locale); err == nil {
			if primary, _ := tag.Base(); primary.String() != "und" {
				base = primary.String()
			}
		}
	}

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
		orWords:   lowerAll(spec.orWords),
		plusWords: []string{"plus"},
		nameFirst: spec.nameFirst,
		unspaced:  spec.unspaced,
		opaque:    lowerAll(spec.opaque),

		impliedOne: lowerAll(spec.impliedOne),
		compounds:  lowerAll(spec.compounds),
	}

	for _, contextual := range spec.senses {
		built.senses = append(built.senses, sense{
			words: lowerAll(contextual.words), unit: contextual.unit,
			name: regexp.MustCompile(`(?i)^(?:` + contextual.name + `)`),
		})
	}

	approximate := `~\s+`
	if len(spec.approximate) > 0 {
		approximate = `(?:` + alternation(spec.approximate) + `|~)\s+`
	}

	if len(spec.approximateAttached) > 0 {
		approximate = `(?:` + alternation(spec.approximateAttached) + `)\s*|` + approximate
		built.approximateTail = regexp.MustCompile(`(?i)\s*(?:` + alternation(spec.approximateAttached) + `)\s*$`)
	}

	built.approximate = regexp.MustCompile(`(?i)^(?:` + approximate + `)`)

	if spec.connective != "" {
		built.connective = regexp.MustCompile(`(?i)^(?:` + spec.connective + `)`)
	}

	optional := "optional"
	if len(spec.optional) > 0 {
		optional = alternation(append([]string{"optional"}, spec.optional...))
	}

	built.optional = regexp.MustCompile(`(?i)^\s*(?:` + optional + `)\s*[:\-–—]\s*` +
		`|\s*[\(\[（［]\s*(?:` + optional + `)\s*[\)\]）］]` +
		`|\s*[,;，、；\-–—]\s*(?:` + optional + `)\s*$` +
		`|\s+(?:` + optional + `)\s*$`)

	separators := `\s*(?:-|–|—|〜|～|~|to)\s*`
	if len(spec.rangeWords) > 0 {
		separators += `|\s+(?:` + alternation(spec.rangeWords) + `)\s+`
	}

	built.amount = regexp.MustCompile(`^(` + amountPattern + `)(?:(?:` + separators + `)(` + amountPattern + `))?(?:\s+|$)(.*)$`)

	if len(spec.prefixUnits) > 0 {
		units := alternation(spec.prefixUnits)
		built.prefixUnit = regexp.MustCompile(`^(` + units + `)\s*(` + amountPattern + `)(?:\s+(.*))?$`)
		built.prefixUnitTail = regexp.MustCompile(`^(.*?)\s*(` + units + `)\s*(` + amountPattern + `)$`)
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

// isOpaqueMeasure reports whether value begins with a measure word whose size
// differs between countries, such as a cup.
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

// describesOpaqueMeasure reports whether a line names, in words of more than one
// character, a measure whose size differs between countries. A line that reads
// "1컵" beside "종이컵" is describing a paper cup, not the registry's cup.
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

// asciiDigits writes the decimal digits of any script, and the punctuation
// that sits beside them, in ASCII so one grammar reads them all.
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

// reorder writes a line in the order the grammar reads: the amount, its unit,
// then the ingredient. Japanese writes a unit before its number (大さじ2), and
// Japanese, Korean, Chinese and Thai list the ingredient before its quantity
// (小麦粉 200g). Everything else is returned as it came.
//
// The second result reports an approximation word that stood before the number
// and was taken out of the ingredient.
func (d *dialect) reorder(line string, read reading) (string, bool) {
	if !d.nameFirst && len(d.impliedOne) != 0 && !d.amount.MatchString(line) {
		if first, rest, found := strings.Cut(line, " "); found && strings.TrimSpace(rest) != "" && d.impliesOne(first) {
			return "1 " + line, false
		}
	}

	if d.prefixUnit != nil {
		if match := d.prefixUnit.FindStringSubmatch(line); match != nil && strings.TrimSpace(match[3]) != "" {
			return match[2] + " " + match[1] + " " + strings.TrimSpace(match[3]), false
		}
	}

	if !d.nameFirst || d.amount.MatchString(line) {
		return line, false
	}

	// A note follows the quantity: 小麦粉 200g(ふるう). It keeps its place after
	// the ingredient once the amount has moved to the front.
	body, note := splitTrailingNote(line)

	rewritten, approximate, found := d.reorderBody(body, read)
	if !found {
		return line, false
	}

	if note != "" {
		rewritten += ", " + note
	}

	return rewritten, approximate
}

var trailingParenthesis = regexp.MustCompile(`^(.*?)\s*\(([^()]*)\)$`)

// splitTrailingNote separates a note in parentheses, or after a comma, from
// the end of a line whose ingredient comes first.
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
			if head, approximate := d.splitApproximate(match[1]); head != "" {
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

		head, approximate := d.splitApproximate(strings.Join(tokens[:len(tokens)-take], " "))
		if head == "" {
			continue
		}

		rewritten := tail + " " + head
		if d.impliesOne(tail) {
			rewritten = "1 " + rewritten
		}

		if !d.amount.MatchString(rewritten) {
			continue
		}

		// A line that names a measure the registry cannot is still a
		// quantity; it is read as one and left unresolved.
		parsed := parseStatedAs(rewritten, "probe", rewritten, probe)
		if parsed.Quantity != nil || (parsed.Normalization != nil && parsed.Normalization.Method == recipe.NormalizationUnresolved) {
			return rewritten, approximate, true
		}
	}

	return line, false, false
}

// namesCompound reports a value that begins with a fixed name, so its first
// word is not read as a unit.
func (d *dialect) namesCompound(value string) bool {
	lowered := strings.ToLower(strings.TrimSpace(value))

	for _, name := range d.compounds {
		if rest, found := strings.CutPrefix(lowered, name); found && (rest == "" || rest[0] == ' ' || rest[0] == ',') {
			return true
		}
	}

	return false
}

// senseOf reads a leading word as a unit when the ingredient after it selects
// that sense. It returns the unit and the text after the word.
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

// takeApproximate removes a leading approximation word and reports whether there
// was one.
func (d *dialect) takeApproximate(value string) (string, bool) {
	prefix := d.approximate.FindString(value)
	if prefix == "" {
		return value, false
	}

	return strings.TrimSpace(strings.TrimPrefix(value, prefix)), true
}

// impliesOne reports a measure word that states the number one itself.
func (d *dialect) impliesOne(word string) bool {
	return slices.Contains(d.impliedOne, strings.ToLower(word))
}

// splitApproximate takes an approximation word off the end of the text that
// precedes a quantity.
func (d *dialect) splitApproximate(head string) (string, bool) {
	if d.approximateTail != nil && d.approximateTail.MatchString(head) {
		return strings.TrimSpace(d.approximateTail.ReplaceAllString(head, "")), true
	}

	return strings.TrimSpace(head), false
}

// dialectSpecs is written in the words a recipe uses. The keys are primary
// language subtags, so `pt` serves pt-BR and pt-PT.
var dialectSpecs = map[string]dialectSpec{
	"en": {
		approximate: []string{"about", "around", "approximately", "approx.", "approx"},
		connective:  `of\s+`,
		orWords:     []string{"or"},
	},
	"de": {
		approximate: []string{"ca.", "circa", "etwa", "ungefähr", "ungefaehr"},
		rangeWords:  []string{"bis"},
		orWords:     []string{"oder"},
		optional:    []string{"fakultativ"},
		opaque:      []string{"tasse", "tassen", "becher", "glas", "gläser", "pfund", "pfd.", "msp.", "messerspitze", "messerspitzen", "schuss", "spritzer", "schluck", "kaffeelöffel", "dessertlöffel"},
	},
	"fr": {
		approximate: []string{"environ", "env.", "à peu près"},
		connective:  `(?:de la|de l['’]|de|du|des)\s+|d['’]|de l['’]`,
		rangeWords:  []string{"à"},
		orWords:     []string{"ou"},
		compounds:   []string{"bouquet garni"},
		senses:      []senseSpec{{[]string{"gousse", "gousses"}, "count.clove", `ails?\b`}},
		optional:    []string{"facultatif", "facultative", "optionnel", "optionnelle", "en option"},
		opaque:      []string{"tasse", "tasses", "verre", "verres", "bol", "bols", "livre", "livres", "noix", "pointe", "cuillère à dessert", "cuillères à dessert", "trait", "filet"},
	},
	"es": {
		approximate: []string{"aprox.", "aprox", "aproximadamente", "unos", "unas"},
		connective:  `(?:de la|de los|de las|del|de)\s+`,
		rangeWords:  []string{"a"},
		orWords:     []string{"o"},
		optional:    []string{"opcional", "opcionales"},
		opaque:      []string{"taza", "tazas", "vaso", "vasos", "libra", "libras", "chorrito", "chorro", "cucharada de postre", "cucharadas de postre"},
	},
	"it": {
		approximate: []string{"circa", "ca."},
		connective:  `(?:del|della|dei|delle|di)\s+|d['’]`,
		rangeWords:  []string{"a"},
		orWords:     []string{"o"},
		optional:    []string{"facoltativo", "facoltativa", "opzionale"},
		senses: []senseSpec{
			{[]string{"spicchio", "spicchi"}, "count.clove", `aglio\b`},
			{
				[]string{"scatola", "scatole"},
				"container.can",
				`(?:pomodor|pelat|passata|polpa|tonno|fagiol|cec[ei]\b|mais\b|pisell|lentic|sgombr|sardin|acciug|oliv|funghi|ananas|pesche)`,
			},
		},
		opaque: []string{"tazza", "tazze", "tazzina", "tazzine", "bicchiere", "bicchieri", "libbra", "libbre", "noce", "noci"},
	},
	"pt": {
		approximate: []string{"cerca de", "aprox.", "aprox"},
		connective:  `(?:de|do|da|dos|das)\s+`,
		rangeWords:  []string{"a", "até"},
		orWords:     []string{"ou"},
		optional:    []string{"opcional", "facultativo"},
		opaque:      []string{"xícara", "xícaras", "chávena", "chávenas", "copo", "copos", "libra", "libras", "colher de sobremesa", "colheres de sobremesa", "colher de café", "colheres de café", "cc"},
	},
	"nl": {
		approximate: []string{"ongeveer", "circa", "ca."},
		rangeWords:  []string{"tot"},
		orWords:     []string{"of"},
		optional:    []string{"optioneel", "facultatief"},
		opaque:      []string{"kop", "kopje", "kopjes", "glas", "glazen", "ons", "pond", "koffielepel", "dessertlepel"},
	},
	"sv": {
		approximate: []string{"ca", "ca.", "cirka"},
		rangeWords:  []string{"till"},
		orWords:     []string{"eller"},
		senses:      []senseSpec{{[]string{"klyfta", "klyftor"}, "count.clove", `vitlök`}},
		optional:    []string{"valfri", "valfritt", "valfria", "eventuellt"},
		opaque:      []string{"kopp", "koppar", "mugg", "muggar", "glas", "kryddmått", "krm", "skålpund", "kaffesked"},
	},
	"da": {
		approximate: []string{"ca.", "ca", "cirka"},
		rangeWords:  []string{"til"},
		orWords:     []string{"eller"},
		optional:    []string{"valgfri", "valgfrit", "evt.", "evt"},
		opaque:      []string{"kop", "kopper", "krus", "glas", "pund", "dessertske", "kaffeske"},
	},
	"nb": {
		approximate: []string{"ca.", "ca", "cirka"},
		rangeWords:  []string{"til"},
		orWords:     []string{"eller"},
		optional:    []string{"valgfritt", "valgfri", "eventuelt"},
		opaque:      []string{"kopp", "kopper", "krus", "glass", "pund", "dessertskje", "kaffeskje"},
	},
	"fi": {
		approximate: []string{"noin", "n."},
		orWords:     []string{"tai"},
		optional:    []string{"valinnainen", "halutessasi"},
		opaque:      []string{"kuppi", "kupillinen", "kuppia", "lasi", "lasillinen", "lasia", "mitta", "naula"},
	},
	"pl": {
		approximate: []string{"ok.", "około"},
		rangeWords:  []string{"do"},
		orWords:     []string{"lub"},
		optional:    []string{"opcjonalnie", "opcjonalny", "opcjonalne"},
		opaque:      []string{"szklanka", "szklanki", "szklanek", "filiżanka", "filiżanki", "kubek", "kubki", "funt", "funty", "łyżka deserowa"},
	},
	"cs": {
		approximate: []string{"cca", "cca.", "asi", "přibližně"},
		rangeWords:  []string{"až", "do"},
		orWords:     []string{"nebo"},
		optional:    []string{"volitelně", "volitelné"},
		opaque:      []string{"hrnek", "hrnky", "hrnků", "sklenice", "sklenička", "libra", "dezertní lžíce"},
	},
	"hu": {
		approximate: []string{"kb.", "kb", "körülbelül"},
		orWords:     []string{"vagy"},
		optional:    []string{"opcionális", "elhagyható"},
		opaque:      []string{"bögre", "bögrényi", "csésze", "pohár", "font", "desszertkanál", "kávéskanál"},
	},
	"ro": {
		approximate: []string{"aprox.", "aprox", "aproximativ", "circa"},
		connective:  `de\s+`,
		rangeWords:  []string{"până la"},
		orWords:     []string{"sau"},
		optional:    []string{"opțional", "opțională"},
		opaque:      []string{"ceașcă", "ceşcuță", "cană", "căni", "pahar", "pahare", "livră"},
	},
	"tr": {
		approximate: []string{"yaklaşık"},
		orWords:     []string{"veya", "ya da"},
		optional:    []string{"isteğe bağlı", "opsiyonel"},
		opaque:      []string{"su bardağı", "çay bardağı", "fincan", "bardak", "tatlı kaşığı", "çay kaşığı", "ç.k.", "çk", "yemek kaşığı", "y.k.", "yk", "kahve fincanı"},
	},
	"ru": {
		approximate: []string{"около", "примерно"},
		rangeWords:  []string{"до"},
		orWords:     []string{"или"},
		optional:    []string{"по желанию", "необязательно"},
		opaque:      []string{"стакан", "стакана", "стаканов", "чашка", "чашки", "чашек", "кружка", "кружки", "фунт", "фунта", "десертная ложка", "десертные ложки"},
	},
	"uk": {
		approximate: []string{"близько", "приблизно"},
		rangeWords:  []string{"до"},
		orWords:     []string{"або"},
		optional:    []string{"за бажанням", "необов'язково"},
		opaque:      []string{"склянка", "склянки", "склянок", "чашка", "чашки", "чашок", "кухоль", "фунт", "десертна ложка"},
	},
	"el": {
		approximate: []string{"περίπου"},
		rangeWords:  []string{"έως"},
		orWords:     []string{"ή"},
		optional:    []string{"προαιρετικά", "προαιρετικό"},
		opaque:      []string{"φλιτζάνι", "φλιτζάνια", "ποτήρι", "ποτήρια", "κουταλιά του γλυκού"},
	},
	"ar": {
		approximate: []string{"حوالي", "تقريباً", "تقريبا"},
		rangeWords:  []string{"إلى", "الى"},
		orWords:     []string{"أو", "او"},
		optional:    []string{"اختياري", "اختيارية"},
		opaque:      []string{"كوب", "أكواب", "فنجان", "فناجين", "كأس", "كاس"},
	},
	"he": {
		approximate: []string{"בערך", "כ-"},
		rangeWords:  []string{"עד"},
		orWords:     []string{"או"},
		optional:    []string{"אופציונלי", "לא חובה"},
		opaque:      []string{"כוס", "כוסות", "ספל", "ספלים"},
	},
	"th": {
		approximateAttached: []string{"ประมาณ"},
		rangeWords:          []string{"ถึง"},
		orWords:             []string{"หรือ"},
		optional:            []string{"ไม่บังคับ", "เป็นทางเลือก"},
		nameFirst:           true,
		unspaced:            true,
		opaque:              []string{"ถ้วย", "แก้ว", "ถ้วยตวง"},
	},
	"vi": {
		approximate: []string{"khoảng"},
		rangeWords:  []string{"đến"},
		orWords:     []string{"hoặc"},
		optional:    []string{"tùy chọn", "tuỳ chọn"},
		opaque:      []string{"chén", "ly", "cốc", "bát", "tách"},
	},
	"id": {
		approximate: []string{"sekitar", "kira-kira"},
		rangeWords:  []string{"sampai"},
		orWords:     []string{"atau"},
		optional:    []string{"opsional"},
		opaque:      []string{"cangkir", "gelas", "mangkuk"},
		impliedOne:  []string{"sejumput", "secubit", "segenggam"},
	},
	"ja": {
		approximateAttached: []string{"約", "およそ"},
		orWords:             []string{"または", "又は"},
		optional:            []string{"お好みで", "お好み", "任意"},
		nameFirst:           true,
		unspaced:            true,
		prefixUnits:         []string{"大さじ", "小さじ", "大匙", "小匙", "計量カップ", "カップ"},
		opaque:              []string{"合", "杯", "升", "紙コップ", "炊飯器のカップ", "炊飯器付属のカップ"},
		impliedOne:          []string{"ひとつまみ", "ひとつかみ"},
	},
	"ko": {
		approximateAttached: []string{"약"},
		orWords:             []string{"또는"},
		optional:            []string{"선택"},
		senses:              []senseSpec{{[]string{"쪽"}, "count.clove", `마늘`}},
		nameFirst:           true,
		unspaced:            true,
		opaque:              []string{"되", "국자", "종이컵", "밥숟가락"},
	},
	"zh": {
		approximateAttached: []string{"大约", "大約", "约", "約"},
		rangeWords:          []string{"至", "到"},
		orWords:             []string{"或"},
		optional:            []string{"可选", "可選", "任选"},
		senses:              []senseSpec{{[]string{"瓣"}, "count.clove", `大?蒜`}},
		nameFirst:           true,
		unspaced:            true,
		opaque:              []string{"杯", "碗", "勺"},
	},
}
