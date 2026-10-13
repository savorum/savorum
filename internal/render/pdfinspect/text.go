package pdfinspect

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
)

// pageText reconstructs what a reader would be able to select on one page.
// Text is drawn as glyph identifiers, so each font's ToUnicode map is read
// back to recover the characters.
func (d *document) pageText(pageBody []byte) (string, error) {
	fonts := d.pageFonts(pageBody)

	var out strings.Builder

	for _, number := range references(pageBody, "Contents") {
		stream, err := d.streamOf(number)
		if err != nil {
			return "", err
		}

		writeContentText(&out, stream, fonts)
	}

	return normalizeSpaces(out.String()), nil
}

// pageFonts maps the resource names a content stream uses to the character
// mapping of the font behind each one.
func (d *document) pageFonts(pageBody []byte) map[string]map[uint32]string {
	fonts := make(map[string]map[uint32]string)

	resources := dictionaryValue(pageBody, "Resources")
	if resources == nil {
		if number := reference(pageBody, "Resources"); number != 0 {
			resources = d.objects[number]
		}
	}

	if resources == nil {
		return fonts
	}

	fontDictionary := dictionaryValue(resources, "Font")
	if fontDictionary == nil {
		if number := reference(resources, "Font"); number != 0 {
			fontDictionary = d.objects[number]
		}
	}

	if fontDictionary == nil {
		return fonts
	}

	for _, entry := range regexp.MustCompile(`/([^\s/<>\[\]]+)\s+(\d+)\s+0\s+R`).FindAllSubmatch(fontDictionary, -1) {
		number, err := strconv.Atoi(string(entry[2]))
		if err != nil {
			continue
		}

		fonts[string(entry[1])] = d.characterMap(number)
	}

	return fonts
}

// characterMap resolves a font's ToUnicode mapping, following a composite
// font down to the descendant that actually carries it.
func (d *document) characterMap(number int) map[uint32]string {
	for depth := 0; depth < 4 && number != 0; depth++ {
		body := d.objects[number]
		if unicodeStream := reference(body, "ToUnicode"); unicodeStream != 0 {
			stream, err := d.streamOf(unicodeStream)
			if err == nil {
				return parseCMap(stream)
			}
		}

		descendants := references(body, "DescendantFonts")
		if len(descendants) == 0 {
			return nil
		}

		number = descendants[0]
	}

	return nil
}

var (
	bfcharPattern  = regexp.MustCompile(`(?s)beginbfchar(.*?)endbfchar`)
	bfrangePattern = regexp.MustCompile(`(?s)beginbfrange(.*?)endbfrange`)
	hexPattern     = regexp.MustCompile(`<([0-9A-Fa-f]*)>`)
	pairPattern    = regexp.MustCompile(`<([0-9A-Fa-f]+)>\s*<([0-9A-Fa-f]+)>`)
	triplePattern  = regexp.MustCompile(`<([0-9A-Fa-f]+)>\s*<([0-9A-Fa-f]+)>\s*<([0-9A-Fa-f]+)>`)
)

func parseCMap(stream []byte) map[uint32]string {
	mapping := make(map[uint32]string)

	for _, block := range bfcharPattern.FindAllSubmatch(stream, -1) {
		for _, pair := range pairPattern.FindAllSubmatch(block[1], -1) {
			mapping[hexValue(pair[1])] = hexText(pair[2])
		}
	}

	for _, block := range bfrangePattern.FindAllSubmatch(stream, -1) {
		for _, triple := range triplePattern.FindAllSubmatch(block[1], -1) {
			low, high := hexValue(triple[1]), hexValue(triple[2])
			base := hexValue(triple[3])

			if high < low || high-low > 0xffff || uint64(base)+uint64(high-low) > unicode.MaxRune {
				continue
			}

			for code := low; code <= high; code++ {
				mapping[code] = string(rune(base + (code - low))) //nolint:gosec // the unicode.MaxRune test above bounds the destination
			}
		}
	}

	return mapping
}

func hexValue(digits []byte) uint32 {
	value, _ := strconv.ParseUint(string(digits), 16, 32)
	return uint32(value)
}

// hexText reads a ToUnicode destination, which is UTF-16BE and may name a
// character outside the basic plane through a surrogate pair.
func hexText(digits []byte) string {
	units := make([]uint16, 0, len(digits)/4)
	for index := 0; index+4 <= len(digits); index += 4 {
		value, err := strconv.ParseUint(string(digits[index:index+4]), 16, 16)
		if err != nil {
			return ""
		}

		units = append(units, uint16(value))
	}

	return string(utf16.Decode(units))
}

// writeContentText walks a content stream, following the font each show
// operator draws with. Only the operators Savorum's renderer emits are
// understood; anything else is skipped rather than guessed at.
func writeContentText(out *strings.Builder, stream []byte, fonts map[string]map[uint32]string) {
	var (
		current map[uint32]string
		text    []rune
	)

	show := func(value string) { text = append(text, []rune(value)...) }

	tokens := tokenize(stream)
	for index, token := range tokens {
		// Only operators drive the text machine. Every other token kind is an
		// operand, read through the index when the operator that owns it runs.
		if token.kind != tokenOperator {
			continue
		}

		switch token.text {
		case "Tf":
			if index >= 2 && tokens[index-2].kind == tokenName {
				current = fonts[tokens[index-2].text]
			}
		case "Tj", "'", "\"":
			if index >= 1 && tokens[index-1].kind == tokenString {
				show(decodeShown(tokens[index-1], current))
			}
		case "TJ":
			var array strings.Builder
			writeArrayText(&array, tokens, index, current)
			show(array.String())
		case "Td", "TD", "T*", "ET":
			// Undo the hyphen the typesetter drew as its own run at a line
			// break.
			if trimmed, undone := undoLineBreakHyphen(text); undone {
				text = trimmed
				continue
			}

			show(" ")
		}
	}

	out.WriteString(string(text))
}

func writeArrayText(out *strings.Builder, tokens []token, operatorIndex int, mapping map[uint32]string) {
	start := -1

	for cursor := operatorIndex - 1; cursor >= 0; cursor-- {
		if tokens[cursor].kind == tokenArrayStart {
			start = cursor
			break
		}
	}

	if start < 0 {
		return
	}

	for cursor := start + 1; cursor < operatorIndex; cursor++ {
		if tokens[cursor].kind == tokenString {
			out.WriteString(decodeShown(tokens[cursor], mapping))
		}
	}
}

func decodeShown(shown token, mapping map[uint32]string) string {
	if mapping == nil {
		if shown.hex {
			return ""
		}

		return shown.text
	}

	source := []byte(shown.text)

	var out strings.Builder
	// Savorum's renderer embeds composite fonts with two-byte identifiers.
	for index := 0; index+1 < len(source); index += 2 {
		code := uint32(source[index])<<8 | uint32(source[index+1])
		if text, found := mapping[code]; found {
			out.WriteString(text)
		}
	}

	return out.String()
}

// drawnTextRuns walks a content stream and reports each run of text with the
// size and baseline it is drawn at. Savorum's renderer emits one run per
// `q … cm … BT … ET … Q` group and places it with an absolute `cm`, so the
// position is read from that operator rather than by carrying a matrix stack.
func drawnTextRuns(stream []byte, fonts map[string]map[uint32]string) []TextRun {
	var (
		runs       []TextRun
		mapping    map[uint32]string
		pending    []float64
		size, x, y float64
		drawing    bool
		text       strings.Builder
	)

	tokens := tokenize(stream)
	numbers := make([]float64, 0, 6)

	for index, token := range tokens {
		if value, ok := numericOperand(token); ok {
			numbers = append(numbers, value)

			continue
		}

		if token.kind != tokenOperator {
			continue
		}

		switch token.text {
		case "cm":
			// The translation is the last two operands of the matrix.
			if len(numbers) >= 6 {
				pending = []float64{numbers[len(numbers)-2], numbers[len(numbers)-1]}
			}
		case "BT":
			drawing = true

			text.Reset()

			if len(pending) == 2 {
				x, y = pending[0], pending[1]
			}
		case "Tf":
			if index >= 2 && tokens[index-2].kind == tokenName {
				mapping = fonts[tokens[index-2].text]
			}

			if len(numbers) >= 1 {
				size = numbers[len(numbers)-1]
			}
		case "Tj", "'", "\"":
			if drawing && index >= 1 && tokens[index-1].kind == tokenString {
				text.WriteString(decodeShown(tokens[index-1], mapping))
			}
		case "TJ":
			if drawing {
				writeArrayText(&text, tokens, index, mapping)
			}
		case "ET":
			runs = appendCompletedTextRun(runs, drawing, text.String(), size, x, y)
			drawing = false
		}

		numbers = numbers[:0]
	}

	return runs
}

func numericOperand(value token) (float64, bool) {
	if value.kind != tokenOther {
		return 0, false
	}

	number, err := strconv.ParseFloat(value.text, 64)

	return number, err == nil
}

func appendCompletedTextRun(runs []TextRun, drawing bool, text string, size, x, y float64) []TextRun {
	if !drawing || strings.TrimSpace(text) == "" {
		return runs
	}

	return append(runs, TextRun{Text: normalizeSpaces(text), Size: size, X: x, Y: y})
}

// drawnTextSizes collects the size operand of every Tf operator in a content
// stream, which is the size the text after it is drawn at.
func drawnTextSizes(stream []byte) []float64 {
	var sizes []float64

	tokens := tokenize(stream)
	for index, token := range tokens {
		if token.kind != tokenOperator || token.text != "Tf" {
			continue
		}

		if index < 1 || tokens[index-1].kind != tokenOther {
			continue
		}

		size, err := strconv.ParseFloat(tokens[index-1].text, 64)
		if err == nil && size > 0 {
			sizes = append(sizes, size)
		}
	}

	return sizes
}

type tokenKind int

const (
	tokenOther tokenKind = iota
	tokenOperator
	tokenString
	tokenName
	tokenArrayStart
	tokenArrayEnd
)

type token struct {
	kind tokenKind
	text string
	hex  bool
}

// tokenize reads the small part of the content-stream grammar Savorum's own
// output uses: names, literal and hexadecimal strings, arrays, and operators.
func tokenize(stream []byte) []token {
	var tokens []token

	for index := 0; index < len(stream); {
		character := stream[index]
		switch {
		case character == '%':
			for index < len(stream) && stream[index] != '\n' {
				index++
			}
		case isWhitespace(character):
			index++
		case character == '/':
			start := index + 1

			index++
			for index < len(stream) && !isDelimiter(stream[index]) && !isWhitespace(stream[index]) {
				index++
			}

			tokens = append(tokens, token{kind: tokenName, text: string(stream[start:index])})
		case character == '(':
			text, next := readLiteralString(stream, index)
			tokens = append(tokens, token{kind: tokenString, text: text})
			index = next
		case character == '<' && index+1 < len(stream) && stream[index+1] != '<':
			end := bytes.IndexByte(stream[index:], '>')
			if end < 0 {
				index = len(stream)
				break
			}

			digits := hexPattern.FindSubmatch(stream[index : index+end+1])
			if digits != nil {
				tokens = append(tokens, token{kind: tokenString, text: string(decodeHexString(digits[1])), hex: true})
			}

			index += end + 1
		case character == '[':
			tokens = append(tokens, token{kind: tokenArrayStart})
			index++
		case character == ']':
			tokens = append(tokens, token{kind: tokenArrayEnd})
			index++
		case character == '<' || character == '>':
			index += 2
		default:
			start := index
			for index < len(stream) && !isDelimiter(stream[index]) && !isWhitespace(stream[index]) {
				index++
			}

			if index == start {
				index++
				continue
			}

			word := string(stream[start:index])
			if isOperator(word) {
				tokens = append(tokens, token{kind: tokenOperator, text: word})
			} else {
				tokens = append(tokens, token{kind: tokenOther, text: word})
			}
		}
	}

	return tokens
}

func decodeHexString(digits []byte) []byte {
	if len(digits)%2 == 1 {
		digits = append(digits, '0')
	}

	out := make([]byte, 0, len(digits)/2)
	for index := 0; index+2 <= len(digits); index += 2 {
		value, err := strconv.ParseUint(string(digits[index:index+2]), 16, 8)
		if err != nil {
			return out
		}

		out = append(out, byte(value))
	}

	return out
}

func readLiteralString(stream []byte, start int) (string, int) {
	var out []byte

	depth := 0

	index := start
	for index < len(stream) {
		character := stream[index]
		switch character {
		case '\\':
			var escaped []byte

			escaped, index = readLiteralEscape(stream, index)
			out = append(out, escaped...)
		case '(':
			depth++
			if depth > 1 {
				out = append(out, character)
			}

			index++
		case ')':
			depth--
			if depth == 0 {
				return string(out), index + 1
			}

			out = append(out, character)
			index++
		default:
			out = append(out, character)
			index++
		}
	}

	return string(out), index
}

// readLiteralEscape decodes one PDF literal-string escape. A show operator
// draws two-byte glyph identifiers, so most bytes arrive through this path.
func readLiteralEscape(stream []byte, index int) ([]byte, int) {
	if index+1 >= len(stream) {
		return nil, index + 1
	}

	next := stream[index+1]
	switch {
	case next >= '0' && next <= '7':
		value := 0
		digits := 0

		for digits < 3 && index+1+digits < len(stream) {
			digit := stream[index+1+digits]
			if digit < '0' || digit > '7' {
				break
			}

			value = value*8 + int(digit-'0')
			digits++
		}

		return []byte{byte(value)}, index + 1 + digits
	case next == 'n':
		return []byte{'\n'}, index + 2
	case next == 'r':
		return []byte{'\r'}, index + 2
	case next == 't':
		return []byte{'\t'}, index + 2
	case next == 'b':
		return []byte{'\b'}, index + 2
	case next == 'f':
		return []byte{'\f'}, index + 2
	case next == '\n':
		return nil, index + 2
	case next == '\r':
		index += 2
		if index < len(stream) && stream[index] == '\n' {
			index++
		}

		return nil, index
	default:
		return []byte{next}, index + 2
	}
}

func isWhitespace(character byte) bool {
	switch character {
	case 0, '\t', '\n', '\f', '\r', ' ':
		return true
	default:
		return false
	}
}

func isDelimiter(character byte) bool {
	switch character {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	default:
		return false
	}
}

var operators = map[string]bool{
	"Tj": true, "TJ": true, "Tf": true, "Td": true, "TD": true,
	"T*": true, "ET": true, "BT": true, "'": true, "\"": true,
	// cm carries the position each text run is placed at.
	"cm": true,
	// Images are drawn with Do inside saved graphics states.
	"q": true, "Q": true, "Do": true,
}

func isOperator(word string) bool { return operators[word] }

var spacePattern = regexp.MustCompile(`[ \t\r\n]+`)

func normalizeSpaces(value string) string {
	return strings.TrimSpace(spacePattern.ReplaceAllString(value, " "))
}

// undoLineBreakHyphen removes a hyphen the typesetter added at a line break,
// along with the spacing this reader inserted around it. It fires only where a
// letter, the hyphen, and the next line meet, which is what a broken word
// looks like and what a dash between words does not.
func undoLineBreakHyphen(text []rune) ([]rune, bool) {
	end := len(text)
	for end > 0 && text[end-1] == ' ' {
		end--
	}

	if end == 0 || text[end-1] != '-' {
		return text, false
	}

	before := end - 1
	for before > 0 && text[before-1] == ' ' {
		before--
	}

	if before == 0 || !unicode.IsLetter(text[before-1]) {
		return text, false
	}

	return text[:before], true
}
