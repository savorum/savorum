package importer

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/recipetext"
)

var (
	textBullet = regexp.MustCompile(`^(?:[-*•·–—▪◦‣⁃]\s*)?(?:\[[ xX]\]|[☐☑□])\s*|^[-*•·–—▪◦‣⁃]\s*`)
	textStep   = regexp.MustCompile(`^(?:[\p{N}]+[.)]\s+|[\p{N}]+[、．）]\s*|[（(][\p{N}]+[）)]\s*)`)
	textAmount = regexp.MustCompile(`^[\p{N}¼½¾⅓⅔⅛⅜⅝⅞]`)
)

func recipeTextLine(line string) string {
	return textBullet.ReplaceAllString(strings.TrimSpace(line), "")
}

func recipeTextAction(line string) bool {
	line = textStep.ReplaceAllString(recipeTextLine(line), "")
	return recipetext.InstructionStart(line) || len(recipe.ExtractTimings(recipe.Step{Text: line})) > 0
}

func recipeTextMeasured(line, locale string) bool {
	line = recipeTextLine(line)
	if textStep.MatchString(line) || !textAmount.MatchString(line) {
		return false
	}

	if len(recipetext.Timings(line)) > 0 {
		return false
	}

	parsed := ingredientparser.Parse(line, "", locale)

	return parsed.Quantity != nil || parsed.Normalization != nil || looksIngredientLine(line)
}

// Infer sections from ingredient lists followed by cooking prose. Isolated
// quantities and unrelated paragraphs remain notes.
func inferRecipeSections(item *ImportedRecipe, text, locale string) {
	lines := strings.Split(normalizePlainText([]byte(text)), "\n")
	if len(lines) > 4096 {
		return
	}

	ingredients := textIngredientLines(lines, locale)
	candidate := *item
	candidate.Document.IngredientSections = nil
	candidate.Document.InstructionSections = nil
	candidate.Document.Notes = nil
	candidate.Diagnostics = slices.DeleteFunc(slices.Clone(item.Diagnostics), func(d Diagnostic) bool {
		return d.Code == "plain-text-uncertain"
	})
	parser := plainTextParser{item: &candidate, readingLocale: locale}
	explicit, stepParagraph := plainModeNotes, false

	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			stepParagraph = false
			continue
		}

		if parser.readHeading(line) {
			explicit = parser.mode
			stepParagraph = false

			continue
		}

		clean := recipeTextLine(line)
		if textIngredientHeading(lines, ingredients, i) {
			parser.readInferredHeading(clean, i)

			explicit = plainModeIngredients

			continue
		}

		if ingredients[i] || (explicit == plainModeIngredients && textIngredientCandidate(line, parser.readingLocale)) {
			if parser.mode != plainModeIngredients || parser.ingredientSection == 0 {
				parser.mode = plainModeIngredients
				parser.ingredientSection = len(candidate.Document.IngredientSections) + 1
			}

			parser.readIngredientLine(lines, i)

			stepParagraph = false

			continue
		}

		hasIngredients := len(candidate.Document.IngredientSections) > 0

		step := explicit == plainModeInstructions || (hasIngredients && (recipeTextAction(clean) || textStep.MatchString(clean))) || stepParagraph
		if step && !strings.HasPrefix(clean, "#") && !isOnlyURL(clean) {
			if parser.mode != plainModeInstructions || parser.instructionSection == 0 {
				parser.mode = plainModeInstructions
				parser.instructionSection = len(candidate.Document.InstructionSections) + 1
			}

			parser.readInstruction(textStep.ReplaceAllString(clean, ""), i)

			stepParagraph = true

			continue
		}

		stepParagraph = false

		candidate.Document.Notes = append(candidate.Document.Notes, line)
	}

	if len(candidate.Document.IngredientSections) == 0 || len(candidate.Document.InstructionSections) == 0 {
		return
	}

	parser.markInferred()

	*item = candidate
}

func (p *plainTextParser) markInferred() {
	if !slices.ContainsFunc(p.item.Diagnostics, func(d Diagnostic) bool { return d.Code == "text-sections-inferred" }) {
		p.item.Diagnostics = append(p.item.Diagnostics, Diagnostic{Code: "text-sections-inferred", Class: DiagnosticActionable, Severity: SeverityWarning, Area: "source", Message: "Check the ingredients and steps inferred from the text."})
	}
}

func (p *plainTextParser) readInferredHeading(line string, index int) {
	if len(p.item.Document.IngredientSections) == 0 && looksPlainSubheading(line) && p.item.Document.Title == "" {
		p.item.Document.Title = strings.TrimSuffix(line, ":")
		return
	}

	p.readIngredient(strings.TrimSuffix(line, ":")+":", index)
	p.mode = plainModeIngredients
}

func textIngredientHeading(lines []string, ingredients []bool, index int) bool {
	line := strings.TrimSpace(lines[index])
	if ingredients[index] || utf8.RuneCountInString(line) > 80 || strings.ContainsAny(line, ".!?。！？;#/") || (recipeTextAction(line) && !strings.HasSuffix(line, ":")) || (index > 0 && strings.TrimSpace(lines[index-1]) != "") {
		return false
	}

	for next := index + 1; next < len(lines); next++ {
		if strings.TrimSpace(lines[next]) != "" {
			return ingredients[next] || (strings.HasSuffix(line, ":") && slices.Contains(ingredients[:index], true) && recipeTextMeasured(lines[next], ""))
		}
	}

	return false
}

func textIngredientLines(lines []string, locale string) []bool {
	result := make([]bool, len(lines))
	for start := 0; start < len(lines); start++ {
		if !recipeTextMeasured(lines[start], locale) {
			continue
		}

		end, measured := start, 0
		for end < len(lines) {
			line := recipeTextLine(lines[end])

			heading, _ := recipetext.Heading(line)

			measuredLine := recipeTextMeasured(line, locale)
			if line == "" || heading != "" || (recipeTextAction(line) && !measuredLine) || textStep.MatchString(line) || embeddedURL(line) != "" {
				break
			}

			if measuredLine {
				measured++
			} else if !textBullet.MatchString(strings.TrimSpace(lines[end])) && (utf8.RuneCountInString(line) > 80 || strings.ContainsAny(line, ".!?。！？:#")) {
				break
			}

			end++
		}

		if measured >= 2 {
			for i := start; i < end; i++ {
				result[i] = true
			}
		}

		start = max(start, end-1)
	}

	return result
}

func textIngredientCandidate(line, locale string) bool {
	return recipeTextMeasured(line, locale) || (!recipeTextAction(line) && textBullet.MatchString(line))
}
