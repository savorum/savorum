package webrecipe

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/savorum/savorum/internal/recipetext"
	"golang.org/x/net/html"
)

func (d *document) semanticMethod() []InstructionSection {
	roots, err := d.semanticRoots()
	if err != nil || len(roots) == 0 {
		return nil
	}

	regions := make(map[*html.Node]bool, len(roots))
	for _, root := range roots {
		regions[root] = true
	}

	var found [][]InstructionSection

	for _, root := range roots {
		blocks, err := d.semanticBlocks(root, regions)
		if err != nil {
			return nil
		}

		candidate := semanticCandidate{blocks: blocks}
		if candidate.read(d) != nil || candidate.instructionMarkers == 0 {
			continue
		}

		steps := 0
		for _, section := range candidate.sections {
			steps += len(section.Steps)
		}

		if steps >= 2 && candidate.procedures*2 >= steps {
			found = append(found, candidate.sections)
		}
	}

	if len(found) != 1 {
		return nil
	}

	return found[0]
}

var semanticDuration = regexp.MustCompile(`^\d+(?:[.,]\d+)?\s*(\p{L}+)\.?$`)

var semanticTimeWords = func() map[string]bool {
	words := map[string]bool{}

	for _, vocabulary := range recipetext.NumberVocabularies() {
		for _, word := range vocabulary.Times {
			words[strings.ToLower(word)] = true
		}
	}

	return words
}()

func semanticDurationNext(following []semanticBlock) bool {
	for _, block := range following {
		if block.end || block.node.Data == "hr" {
			continue
		}

		return semanticDurationText(semanticPlain(block.text))
	}

	return false
}

func semanticDurationText(text string) bool {
	match := semanticDuration.FindStringSubmatch(text)

	return match != nil && semanticTimeWords[strings.ToLower(match[1])]
}

func semanticStepTitle(text string, following []semanticBlock) bool {
	plain := strings.TrimSuffix(semanticPlain(text), ":")
	if last, _ := utf8.DecodeLastRuneInString(plain); plain == "" || utf8.RuneCountInString(plain) > 60 || strings.ContainsRune(".!?。！", last) {
		return false
	}

	if !semanticNumber.MatchString(plain) && (len(strings.Fields(plain)) > 4 || semanticAmount.MatchString(plain) ||
		utf8.RuneCountInString(plain) > 24) {
		return false
	}

	for _, block := range following {
		if block.end || block.node.Data == "hr" || block.node.Data == "figcaption" {
			continue
		}

		next := semanticPlain(block.text)

		return block.level == 0 && next != "" && !semanticNumber.MatchString(next) && semanticProcedure(next)
	}

	return false
}

func semanticStepNext(following []semanticBlock) bool {
	for _, block := range following {
		if block.end || block.node.Data == "hr" || semanticPlain(block.text) == "" {
			continue
		}

		return block.level == 0 && !semanticMostlyLinked(block.node) &&
			(semanticNumber.MatchString(block.text) || semanticProcedure(semanticPlain(block.text)))
	}

	return false
}

func semanticLeadIn(block semanticBlock) bool {
	plain := semanticPlain(block.text)

	return block.level == 0 && block.node.Data != "ol" && block.node.Data != "ul" &&
		(plain == "" || strings.HasSuffix(plain, ":") || semanticMostlyLinked(block.node))
}

func semanticNumberedParagraphNext(following []semanticBlock) bool {
	for _, block := range following {
		if block.end || block.node.Data == "hr" || semanticPlain(block.text) == "" {
			continue
		}

		return block.level == 0 && semanticNumber.MatchString(block.text)
	}

	return false
}

func semanticNumberedNext(following []semanticBlock) bool {
	leads := 0

	for _, block := range following {
		switch {
		case block.end || block.node.Data == "hr":
			continue
		case semanticLeadIn(block) && leads < 3:
			leads++
			continue
		case block.node.Data == "ol":
			return true
		default:
			return semanticNumber.MatchString(block.text) || block.level > 0 && semanticStepLabel.MatchString(semanticPlain(block.text))
		}
	}

	return false
}

func (c *semanticCandidate) readSteps(d *document, list *html.Node, title string) error {
	section := InstructionSection{Title: title}

	for row := list.FirstChild; row != nil; row = row.NextSibling {
		if !c.semanticEntry(row) {
			continue
		}

		text, err := d.semanticText(row, false)
		if err != nil {
			return err
		}

		if text == "" {
			continue
		}

		section.Steps = append(section.Steps, Step{Text: text})
		if semanticProcedure(text) {
			c.procedures++
		}
	}

	if len(section.Steps) > 0 {
		c.sections = append(c.sections, section)
		c.listSteps = c.listSteps || title == ""
	}

	return nil
}

func semanticProcedure(text string) bool {
	return utf8.RuneCountInString(text) >= 12 && (len(strings.Fields(text)) >= 3 || recipetext.Unspaced(text)) &&
		!semanticAmount.MatchString(text)
}

func semanticProse(line string) bool {
	if semanticLong(line) {
		return true
	}

	words := strings.Fields(line)
	if len(words) == 0 || semanticStatesAmount(line) || strings.ContainsRune(semanticBullets, []rune(line)[0]) {
		return false
	}

	last := words[len(words)-1]

	return strings.HasSuffix(line, "!") || strings.HasSuffix(line, "?") ||
		len(words) > 6 && strings.HasSuffix(last, ".") && !strings.Contains(strings.TrimSuffix(last, "."), ".")
}

func semanticLong(line string) bool {
	return utf8.RuneCountInString(line) > 100 || len(strings.Fields(line)) > 16
}

func semanticProseParagraph(block semanticBlock) bool {
	if semanticMostlyLinked(block.node) {
		return true
	}

	for line := range strings.SplitSeq(block.text, "\n") {
		if line = strings.TrimSpace(semanticPlain(line)); line != "" && !semanticProse(line) {
			return false
		}
	}

	return true
}
