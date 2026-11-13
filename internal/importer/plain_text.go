package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/recipetext"
)

// plainTextAdapter is deliberately conservative: it extracts obvious recipe
// sections and keeps every other meaningful line as a note for review.
type plainTextAdapter struct{}

const (
	maxPlainTextLines     = 10_000
	maxPlainTextLineBytes = 64 << 10
)

func (plainTextAdapter) Format() Format {
	return Format{InputKinds: []string{"file", "text"}, MediaTypes: []string{"text/plain"}, ID: "plain-text", Name: "Plain text recipe", Version: "1", Extensions: []string{".txt"}, Capabilities: Capabilities{StructuredIngredients: true, StructuredInstructions: true}}
}

func (plainTextAdapter) Probe(_ context.Context, input Input) ProbeResult {
	if bytes.HasPrefix(bytes.TrimSpace(input.Data), []byte("PK\x03\x04")) || bytes.HasPrefix(bytes.TrimSpace(input.Data), []byte("%PDF-")) {
		return ProbeResult{Confidence: ConfidenceNone}
	}

	name := strings.ToLower(input.Name)
	if strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".text") {
		return ProbeResult{Confidence: ConfidenceExtension, Reason: "plain-text extension"}
	}

	text := normalizePlainText(plainTextBytes(input.Data))
	if len(input.Data) > 8<<20 || !isLikelyPlainText(text) || isMarkup(text) {
		return ProbeResult{Confidence: ConfidenceNone}
	}

	for line := range strings.SplitSeq(text, "\n") {
		section, _ := recipetext.Heading(line)
		if section != "" {
			return ProbeResult{Confidence: ConfidenceHeadings, Reason: "recipe section headings"}
		}
	}

	return ProbeResult{Confidence: ConfidenceNone}
}

func (plainTextAdapter) Import(_ context.Context, input Input, _ Options) (ImportBatch, error) {
	data := plainTextBytes(input.Data)
	if len(input.Data) > 8<<20 || !isLikelyPlainText(string(data)) {
		return ImportBatch{}, fmt.Errorf("recipe text must be valid UTF-8 text under 8 MiB")
	}

	text := normalizePlainText(data)
	if strings.TrimSpace(text) == "" {
		return ImportBatch{}, fmt.Errorf("plain-text recipe is empty")
	}

	lineCount := 0
	for line := range strings.SplitSeq(text, "\n") {
		lineCount++
		if lineCount > maxPlainTextLines || len(line) > maxPlainTextLineBytes {
			return ImportBatch{}, fmt.Errorf("plain-text recipe exceeds line limits")
		}
	}

	lines := strings.Split(text, "\n")

	title, start := plainTitle(lines)

	hash := sha256.Sum256([]byte(text))
	doc := recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            recipe.RecipeID(fmt.Sprintf("plain-text.%x", hash[:8])),
		Title:         title,
		Source:        plainTextSource(input.URL, lines),
	}
	item := ImportedRecipe{Document: doc, SourceKey: fmt.Sprintf("sha256:%x", hash[:]), ImporterMetadata: map[string]any{"source_text": text}}
	parser := plainTextParser{item: &item, mode: plainModeNotes}
	parser.parse(lines, start)

	if len(item.Document.IngredientSections) == 0 || len(item.Document.InstructionSections) == 0 {
		inferRecipeSections(&item, strings.Join(lines[start:], "\n"), "")
	}

	if item.Document.Title == "" {
		item.Document.Title = "Untitled recipe"
	}

	return ImportBatch{Source: ImportSource{Format: plainTextAdapter{}.Format(), InputName: input.Name, InputKind: "file"}, Recipes: []ImportedRecipe{item}}, nil
}

type plainTextMode uint8

const (
	plainModeNotes plainTextMode = iota
	plainModeIngredients
	plainModeInstructions
)

type plainTextParser struct {
	requireHeadings    bool
	readingLocale      string
	item               *ImportedRecipe
	mode               plainTextMode
	ingredientSection  int
	instructionSection int
	ingredientTitle    string
	instructionTitle   string
}

func plainTextSource(inputURL string, lines []string) *recipe.Source {
	if parsed, err := url.Parse(inputURL); err == nil && parsed.IsAbs() {
		return &recipe.Source{URL: inputURL}
	}

	// A line that is only a link names the source more reliably than a link
	// cited inside a step.
	for _, line := range lines {
		if isOnlyURL(strings.TrimSpace(line)) {
			return &recipe.Source{URL: embeddedURL(line)}
		}
	}

	for _, line := range lines {
		if embedded := embeddedURL(line); embedded != "" {
			return &recipe.Source{URL: embedded}
		}
	}

	return nil
}

type stepRun struct {
	numbered bool
	bulleted bool
	opened   bool
}

func (p *plainTextParser) parse(lines []string, start int) {
	ingredients := textIngredientLines(lines, p.readingLocale)

	var run stepRun

	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			run.numbered = false
			continue
		}

		// A step that cites a link stays in the cooking sequence. Elsewhere a
		// line with a link is source information and is kept as a note.
		if embeddedURL(line) != "" && (p.mode != plainModeInstructions || isOnlyURL(line)) {
			p.item.Document.Notes = append(p.item.Document.Notes, line)

			continue
		}

		if videoChapter(lines, i) {
			p.item.Document.Notes = append(p.item.Document.Notes, line)

			continue
		}

		if p.readHeading(line) {
			run.numbered = false
			run.bulleted = false

			continue
		}

		clean := recipeTextLine(line)
		if p.mode == plainModeIngredients && textIngredientHeading(lines, ingredients, i) {
			p.readIngredient(strings.TrimSuffix(clean, ":")+":", i)
			continue
		}

		if p.mode == plainModeIngredients && i > start && strings.TrimSpace(lines[i-1]) == "" && recipeTextAction(clean) && !recipeTextMeasured(clean, p.readingLocale) {
			p.mode = plainModeInstructions
			p.instructionSection = len(p.item.Document.InstructionSections) + 1
			p.markInferred()
		}

		if p.requireHeadings && p.mode == plainModeIngredients && p.listEnded(lines, i) {
			p.mode = plainModeNotes
		}

		if p.mode == plainModeInstructions && run.bulleted && i > start && strings.TrimSpace(lines[i-1]) == "" && !textStep.MatchString(clean) && !textBullet.MatchString(line) {
			p.item.Document.Notes = append(p.item.Document.Notes, line)
			continue
		}

		switch p.mode {
		case plainModeIngredients:
			p.readIngredientLine(lines, i)
		case plainModeInstructions:
			p.readStepLine(line, clean, i, &run)
		case plainModeNotes:
			if !p.readFact(line) {
				p.readUnclassified(line, i)
			}
		}
	}
}

func (p *plainTextParser) readStepLine(line, clean string, lineIndex int, run *stepRun) {
	if bareStepNumber.MatchString(clean) {
		run.numbered = false
		run.opened = true

		return
	}

	if run.numbered && !run.opened && !textStep.MatchString(clean) && !textBullet.MatchString(line) && !isPlainNote(clean) && !strings.Contains(clean, ":") {
		steps := p.item.Document.InstructionSections[p.instructionSection-1].Steps
		steps[len(steps)-1].Text += " " + clean

		return
	}

	p.readInstruction(clean, lineIndex)
	run.numbered = (run.opened || textStep.MatchString(clean)) && !looksPlainSubheading(clean)
	run.opened = false
	run.bulleted = textBullet.MatchString(line)
}

func (p *plainTextParser) readHeading(line string) bool {
	line = plainHeadingText(line)
	section, locale := recipetext.Heading(line)
	title := ""

	if section == "" && looksPlainSubheading(line) {
		section, locale = headingWithin(strings.Fields(strings.TrimSuffix(line, ":")))
		if section != "" {
			title = strings.TrimSuffix(line, ":")
		}
	}

	if locale != "" {
		p.readingLocale = locale
	}

	switch section {
	case "ingredients":
		p.mode = plainModeIngredients
		// Headings select the next slot; only content creates it. A repeated
		// mode heading followed by a subtitle still names just one new section.
		p.ingredientSection = len(p.item.Document.IngredientSections) + 1
		p.ingredientTitle = title

		return true
	case "instructions":
		p.mode = plainModeInstructions
		p.instructionSection = len(p.item.Document.InstructionSections) + 1
		p.instructionTitle = title

		return true
	default:
		return false
	}
}

func (p *plainTextParser) listEnded(lines []string, index int) bool {
	if index == 0 || strings.TrimSpace(lines[index-1]) != "" || textIngredientCandidate(lines[index], p.readingLocale) {
		return false
	}

	for next := index + 1; next < len(lines); next++ {
		if strings.TrimSpace(lines[next]) != "" {
			return !textIngredientCandidate(lines[next], p.readingLocale)
		}
	}

	return true
}

func headingWithin(words []string) (section, locale string) {
	for start := range words {
		for end := start + 1; end <= len(words) && end-start < len(words); end++ {
			if section, locale = recipetext.Heading(strings.Join(words[start:end], " ")); section != "" {
				return section, locale
			}
		}
	}

	return "", ""
}

func (p *plainTextParser) readIngredient(line string, lineIndex int) {
	if looksPlainSubheading(line) {
		p.ingredientSection = len(p.item.Document.IngredientSections) + 1

		p.ingredientTitle = strings.TrimSuffix(line, ":")

		return
	}

	id := fmt.Sprintf("i%d", lineIndex+1)

	ingredient := ingredientparser.Parse(line, id, p.readingLocale)
	if len(ingredient.Name) == 0 && ingredient.Quantity == nil {
		p.item.Document.Notes = append(p.item.Document.Notes, line)
		p.item.Diagnostics = append(p.item.Diagnostics, plainUncertain(id, "ingredients", line))

		return
	}

	for len(p.item.Document.IngredientSections) < p.ingredientSection {
		p.item.Document.IngredientSections = append(p.item.Document.IngredientSections, recipe.IngredientSection{
			ID:    fmt.Sprintf("ingredients-%d", len(p.item.Document.IngredientSections)+1),
			Title: p.ingredientTitle,
		})
		p.ingredientTitle = ""
	}

	p.item.Document.IngredientSections[p.ingredientSection-1].Ingredients = append(
		p.item.Document.IngredientSections[p.ingredientSection-1].Ingredients,
		ingredient,
	)
}

func (p *plainTextParser) readIngredientLine(lines []string, index int) {
	line := recipeTextLine(lines[index])
	if index > 0 && strings.TrimSpace(lines[index-1]) != "" && !textBullet.MatchString(strings.TrimSpace(lines[index])) && p.joinIngredient(line) {
		return
	}

	p.readIngredient(line, index)
}

func (p *plainTextParser) joinIngredient(line string) bool {
	if p.ingredientSection < 1 || p.ingredientSection > len(p.item.Document.IngredientSections) || recipeTextMeasured(line, p.readingLocale) || recipeTextAction(line) {
		return false
	}

	items := p.item.Document.IngredientSections[p.ingredientSection-1].Ingredients
	if len(items) == 0 {
		return false
	}

	previous := &items[len(items)-1]

	locale := p.readingLocale
	if locale == "" {
		locale = ingredientparser.AliasLanguage
	}

	amountOnly := previous.Quantity != nil && previous.Name == "" || ingredientparser.MeasureOnly(previous.SourceText, locale)
	if amountOnly || strings.HasSuffix(previous.SourceText, ",") || strings.HasSuffix(previous.SourceText, "，") || strings.HasSuffix(previous.SourceText, "،") {
		*previous = ingredientparser.Parse(previous.SourceText+" "+line, previous.ID, p.readingLocale)
		return true
	}

	return false
}

func (p *plainTextParser) readInstruction(line string, lineIndex int) {
	if isPlainNote(line) {
		p.item.Document.Notes = append(p.item.Document.Notes, line)
		p.item.Diagnostics = append(p.item.Diagnostics, plainUncertain(fmt.Sprintf("line-%d", lineIndex+1), "notes", line))

		return
	}

	if looksPlainSubheading(line) {
		p.instructionSection = len(p.item.Document.InstructionSections) + 1

		p.instructionTitle = strings.TrimSuffix(line, ":")

		return
	}

	for len(p.item.Document.InstructionSections) < p.instructionSection {
		p.item.Document.InstructionSections = append(p.item.Document.InstructionSections, recipe.InstructionSection{
			ID:    fmt.Sprintf("instructions-%d", len(p.item.Document.InstructionSections)+1),
			Title: p.instructionTitle,
		})
		p.instructionTitle = ""
	}

	section := &p.item.Document.InstructionSections[p.instructionSection-1]
	section.Steps = append(section.Steps, recipe.Step{ID: fmt.Sprintf("step-%d", lineIndex+1), Text: stripNumber(line)})
}

func (p *plainTextParser) readFact(line string) bool {
	kind, value := recipetext.Fact(recipeTextLine(line))
	if kind == "" {
		return false
	}

	document := &p.item.Document
	if kind == recipetext.FactYield {
		if len(document.Yields) > 0 {
			return false
		}

		document.Yields = []recipe.Yield{{Text: value}}

		return true
	}

	if document.Times == nil {
		document.Times = &recipe.Times{}
	}

	field := map[string]**recipe.DurationExpression{
		recipetext.FactPrep:  &document.Times.Prep,
		recipetext.FactCook:  &document.Times.Cook,
		recipetext.FactTotal: &document.Times.Total,
	}[kind]
	if *field != nil {
		return false
	}

	*field = &recipe.DurationExpression{Text: value}
	if length, ok := recipetext.Duration(value); ok {
		*field = &recipe.DurationExpression{Value: recipe.FormatDuration(length), SourceText: value}
	}

	return true
}

func (p *plainTextParser) readUnclassified(line string, lineIndex int) {
	if p.requireHeadings {
		p.item.Document.Notes = append(p.item.Document.Notes, line)
		return
	}

	id := fmt.Sprintf("i%d", lineIndex+1)

	candidate := ingredientparser.Parse(line, id, p.readingLocale)
	if candidate.Quantity != nil || looksIngredientLine(line) {
		if len(p.item.Document.IngredientSections) == 0 {
			p.item.Document.IngredientSections = append(p.item.Document.IngredientSections, recipe.IngredientSection{ID: "ingredients-1"})
		}

		p.item.Document.IngredientSections[0].Ingredients = append(p.item.Document.IngredientSections[0].Ingredients, candidate)

		return
	}

	p.item.Document.Notes = append(p.item.Document.Notes, line)
	p.item.Diagnostics = append(p.item.Diagnostics, plainUncertain(fmt.Sprintf("line-%d", lineIndex+1), "content", line))
}

func plainTextBytes(data []byte) []byte {
	var order binary.ByteOrder

	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		order = binary.LittleEndian
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		order = binary.BigEndian
	default:
		return data
	}

	body := data[2:]
	if len(body)%2 != 0 {
		return data
	}

	units := make([]uint16, len(body)/2)
	for index := range units {
		units[index] = order.Uint16(body[2*index:])
	}

	return []byte(string(utf16.Decode(units)))
}

func normalizePlainText(data []byte) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.TrimPrefix(string(data), "\ufeff"), "\r\n", "\n"), "\r", "\n")
}

func plainTitle(lines []string) (string, int) {
	for i, line := range lines {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}

		s = plainHeadingText(s)

		section, _ := recipetext.Heading(s)
		if section != "" || textIngredientLines(lines[i:], "")[0] {
			return "", i
		}

		return s, i + 1
	}

	return "", len(lines)
}

var (
	plainMarkdownHeading = regexp.MustCompile(`^#{1,6}\s+`)
	plainStepNumber      = regexp.MustCompile(`^(?:\d+|[ivxlcdm]+)[.)-]\s*`)
	plainChapter         = regexp.MustCompile(`^[(\[]?(?:\d{1,2}:)?\d{1,2}:\d{2}[)\]]?\s*[-–—:]?\s+\S`)
)

// Require adjacent timestamps to distinguish video chapters from timed steps.
func videoChapter(lines []string, index int) bool {
	if !plainChapter.MatchString(strings.TrimSpace(lines[index])) {
		return false
	}

	for _, neighbour := range []int{index - 1, index + 1} {
		if neighbour >= 0 && neighbour < len(lines) && plainChapter.MatchString(strings.TrimSpace(lines[neighbour])) {
			return true
		}
	}

	return false
}

func plainHeadingText(s string) string {
	return strings.TrimSpace(strings.Trim(plainMarkdownHeading.ReplaceAllString(s, ""), "*_"))
}

func stripNumber(s string) string {
	s = textStep.ReplaceAllString(strings.TrimSpace(s), "")
	return plainStepNumber.ReplaceAllString(s, "")
}

func looksPlainSubheading(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasSuffix(s, ":") && utf8.RuneCountInString(s) < 60
}

var plainIngredientPattern = regexp.MustCompile(`^\s*(?:\d+(?:[./]\d+)?|[¼½¾⅓⅔⅛⅜⅝⅞])\s+\S+`)

func looksIngredientLine(s string) bool {
	return plainIngredientPattern.MatchString(recipetext.NumericText(s))
}

func isPlainNote(s string) bool {
	l := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(l, "#") || strings.HasPrefix(l, "note:") || strings.HasPrefix(l, "note ") || strings.HasPrefix(l, "tip:") || strings.HasPrefix(l, "source:")
}

func isMarkup(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))

	return strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html") || strings.Contains(lower, "<body")
}

func isLikelyPlainText(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, '\x00') }

func embeddedURL(s string) string {
	for field := range strings.FieldsSeq(s) {
		if strings.HasPrefix(field, "https://") || strings.HasPrefix(field, "http://") {
			return strings.Trim(field, "<>(),")
		}
	}

	return ""
}

func isOnlyURL(s string) bool {
	return len(strings.Fields(s)) == 1 && embeddedURL(s) != ""
}

func plainUncertain(id, area, line string) Diagnostic {
	return Diagnostic{Code: "plain-text-uncertain", Class: DiagnosticActionable, Severity: SeverityWarning, Area: area, Message: "Some text was kept in notes. Check its placement.", Targets: []DiagnosticTarget{{ObjectID: id, Field: area, Evidence: line, Preserved: line, Action: "Review classification before import."}}}
}
