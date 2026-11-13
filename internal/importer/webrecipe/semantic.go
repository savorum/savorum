package webrecipe

import (
	"regexp"
	"slices"
	"strings"

	"github.com/savorum/savorum/internal/recipetext"
	"golang.org/x/net/html"
)

const (
	maximumSemanticCandidates = 32
	maximumSemanticBlocks     = 10_000
	maximumSemanticText       = 256 << 10
	maximumSemanticEntry      = 64 << 10
)

var (
	semanticAmount         = regexp.MustCompile(`^[\p{N}¼½¾⅓⅔⅛⅜⅝⅞]+(?:[\s.,/–−-]|\p{L})`)
	semanticTrailingAmount = regexp.MustCompile(`[\p{N}¼½¾⅓⅔⅛⅜⅝⅞][\p{N}.,/～〜~–−-]*\s*[\p{L}\p{M}]{0,12}\.?\)?$`)
	// Exclude decimal amounts and quantity ranges from step numbering.
	semanticNumber     = regexp.MustCompile(`^\s*(?:\d+\s*(?:[.)]-?|-)|[①-⑳]\s*)(?:\s+|(\p{L}))`)
	semanticStepNumber = regexp.MustCompile(`^\d{1,2}$`)
	semanticStepLabel  = regexp.MustCompile(`^(?:\p{L}{1,10}\s*)?(?:\d{1,2}\s*[.:)：、]|[①-⑳])`)
)

type semanticBlock struct {
	node  *html.Node
	text  string
	level int
	end   bool
}

type semanticCandidate struct {
	title              string
	titleLevel         int
	headings           []semanticBlock
	blocks             []semanticBlock
	groups             []IngredientGroup
	sections           []InstructionSection
	ingredientMarkers  int
	instructionMarkers int
	amounts            int
	procedures         int
	paragraphSteps     bool
	listSteps          bool
	implicitMethod     bool
	uncertain          bool
	locale             string
}

// semanticRecipe assigns fields only when DOM section boundaries and repeated
// entry evidence agree within one article. Metadata never supplies that evidence.
func (d *document) semanticRecipe() (*Recipe, error) {
	roots, err := d.semanticRoots()
	if err != nil {
		return nil, controlledExtractionError(err)
	}

	if len(roots) == 0 {
		chosen, err := d.headingBoundedRecipe()
		if err != nil {
			return nil, controlledExtractionError(err)
		}

		if chosen == nil {
			return nil, nil
		}

		return d.semanticReading(chosen), nil
	}

	var candidates []semanticCandidate

	regions := make(map[*html.Node]bool, len(roots))
	for _, root := range roots {
		regions[root] = true
	}

	for _, root := range roots {
		blocks, err := d.semanticBlocks(root, regions)
		if err != nil {
			return nil, controlledExtractionError(err)
		}

		candidate := semanticCandidate{blocks: blocks}
		if err := candidate.read(d); err != nil {
			return nil, controlledExtractionError(err)
		}

		if candidate.title == "" {
			candidate.title = semanticPlain(d.pageHeading())
		}

		if candidate.recipeLike() {
			candidates = append(candidates, candidate)
			if len(candidates) > maximumSemanticCandidates {
				return nil, controlledExtractionError(errExtractionLimited)
			}
		}
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	if len(candidates) == 1 && candidates[0].strong() {
		return d.semanticReading(&candidates[0]), nil
	}

	result := d.semanticMetadata(candidates[0].title)
	result.ExtractionMethod = "semantic-html"

	text, err := d.semanticReviewText(candidates)
	if err != nil {
		return nil, controlledExtractionError(err)
	}

	result.ReviewText = text
	result.Notes = []string{text}
	result.warn("semantic-html-review", "This page's recipe sections are incomplete or ambiguous. Edit the extracted text to choose ingredients and steps before saving.")

	return result, nil
}

func (d *document) semanticReading(chosen *semanticCandidate) *Recipe {
	result := d.semanticMetadata(chosen.title)
	result.ExtractionMethod = "semantic-html"
	result.IngredientGroups = chosen.groups
	result.Sections = chosen.sections
	result.warn("semantic-html", "Ingredients and instructions were read from this page's article. Review the recipe before saving.")

	return result
}

func (c *semanticCandidate) recipeLike() bool {
	return (c.ingredientMarkers > 0 || c.instructionMarkers > 0) &&
		(c.amounts > 0 || c.procedures > 0 || (c.ingredientMarkers > 0 && c.instructionMarkers > 0 && len(c.groups)+len(c.sections) > 0))
}

func (c *semanticCandidate) strong() bool {
	ingredients, steps := 0, 0
	for _, group := range c.groups {
		ingredients += len(group.Ingredients)
	}

	for _, section := range c.sections {
		steps += len(section.Steps)
	}

	return c.title != "" && !c.uncertain && (!c.paragraphSteps || !c.listSteps) && c.ingredientMarkers == 1 &&
		(c.instructionMarkers == 1 || c.instructionMarkers == 0 && c.implicitMethod) &&
		ingredients >= 3 && c.amounts >= 3 && c.amounts*2 >= ingredients && steps >= 2 && c.procedures >= 2 && c.procedures*2 >= steps
}

type semanticReading struct {
	mode        string
	group       string
	section     string
	marker      *html.Node
	markerLevel int
	markerText  string
	// Count only headings followed by section content.
	labelled     map[string]int
	groupStart   int
	sectionStart int
	numbered     bool
	listed       bool
	implicit     bool
	implicitFrom semanticProgress
	following    []semanticBlock
}

type semanticProgress struct {
	sections       int
	procedures     int
	paragraphSteps bool
}

func (r *semanticReading) filled(c *semanticCandidate) bool {
	return len(c.groups) > r.groupStart || len(c.sections) > r.sectionStart
}

func (r *semanticReading) part(c *semanticCandidate, title string) {
	if r.mode == "ingredients" {
		for index := r.groupStart; index < len(c.groups); index++ {
			if c.groups[index].Title == "" {
				c.groups[index].Title = r.markerText
			}
		}

		r.group = title
	} else {
		for index := r.sectionStart; index < len(c.sections); index++ {
			if c.sections[index].Title == "" {
				c.sections[index].Title = r.markerText
			}
		}

		r.section = title
	}
}

func (c *semanticCandidate) read(d *document) error {
	c.locale = d.pageLanguage()
	reading := semanticReading{}

	for index, block := range c.blocks {
		reading.following = c.blocks[index+1:]
		if reading.boundary(c, block, reading.following) || reading.mode == "" {
			continue
		}

		if semanticGroupLabel(block, c.blocks, index) {
			title := strings.TrimSuffix(semanticPlain(block.text), ":")
			if reading.mode == "ingredients" {
				reading.group = title
			} else {
				reading.section = title
			}

			continue
		}

		if err := reading.content(c, d, block); err != nil {
			return err
		}
	}

	return nil
}

func (r *semanticReading) boundary(c *semanticCandidate, block semanticBlock, following []semanticBlock) bool {
	if block.end {
		if withinNode(r.marker, block.node) {
			r.mode = ""
		}

		return true
	}

	text := semanticPlain(block.text)

	if r.mode != "" && recipetext.OtherHeading(text) {
		r.mode = ""
		return true
	}

	kind, qualified := blockSection(text, block.level, following)
	if kind != "" && kind == r.mode && !r.implicit && (block.level == 0 || block.level >= r.markerLevel) &&
		(block.node != r.marker || strings.TrimSuffix(text, ":") != r.markerText) {
		r.part(c, strings.TrimSuffix(text, ":"))
		return true
	}

	if qualified && r.implicit && kind == "ingredients" {
		r.mode, r.implicit, r.listed, r.numbered = "ingredients", false, false, false
		r.part(c, strings.TrimSuffix(text, ":"))

		return true
	}

	if kind == "" && block.level > 0 && r.mode == "ingredients" && r.filled(c) && c.instructionMarkers == 0 &&
		(r.markerLevel == 0 || block.level <= r.markerLevel) && semanticNumberedNext(following) {
		kind = "instructions"
	}

	// Prefer labeled instructions over inferred steps.
	if kind == "instructions" && c.implicitMethod {
		c.sections, c.procedures, c.paragraphSteps = c.sections[:r.implicitFrom.sections], r.implicitFrom.procedures, r.implicitFrom.paragraphSteps
		c.implicitMethod = false
	}

	if kind != "" {
		r.begin(c, block, kind, text)
		return true
	}

	c.readTitle(block)

	if block.node.Data == "hr" {
		if r.filled(c) {
			r.mode = ""
		}

		return true
	}

	if block.level > 0 && (r.markerLevel == 0 || block.level <= r.markerLevel) {
		r.mode = ""
		return true
	}

	return false
}

func blockSection(text string, level int, following []semanticBlock) (kind string, qualified bool) {
	kind, _, qualified = semanticSection(text, level)
	if kind != "" && semanticDurationNext(following) {
		return "", false
	}

	return kind, qualified
}

func (r *semanticReading) begin(c *semanticCandidate, block semanticBlock, kind, text string) {
	if c.ingredientMarkers+c.instructionMarkers == 0 {
		c.resolveTitle(block.level)
	}

	read := len(c.sections)
	if kind == "ingredients" {
		read = len(c.groups)
	}

	if start, seen := r.labelled[kind]; seen && start == read {
		c.unmark(kind)
	}

	if r.labelled == nil {
		r.labelled = map[string]int{}
	}

	r.labelled[kind] = read

	r.mode, r.marker, r.markerLevel, r.markerText = kind, block.node, block.level, strings.TrimSuffix(text, ":")
	r.group, r.section = "", ""
	r.groupStart, r.sectionStart, r.numbered, r.listed, r.implicit = len(c.groups), len(c.sections), false, false, false

	if kind == "ingredients" {
		c.ingredientMarkers++
	} else {
		c.instructionMarkers++
	}
}

func (c *semanticCandidate) unmark(kind string) {
	if kind == "ingredients" {
		c.ingredientMarkers--
	} else {
		c.instructionMarkers--
	}
}

func (c *semanticCandidate) readTitle(block semanticBlock) {
	if block.level == 0 || c.ingredientMarkers+c.instructionMarkers != 0 || strings.HasSuffix(semanticPlain(block.text), ":") {
		return
	}

	c.headings = append(c.headings, block)
	if c.title != "" && block.level > c.titleLevel {
		return
	}

	if c.title != "" && block.level == c.titleLevel && c.title != semanticPlain(block.text) {
		c.uncertain = true
	}

	c.title = semanticPlain(block.text)
	c.titleLevel = block.level
}

func (c *semanticCandidate) resolveTitle(markerLevel int) {
	if !c.uncertain {
		return
	}

	for _, heading := range slices.Backward(c.headings) {
		if markerLevel != 0 && heading.level >= markerLevel {
			continue
		}

		if heading.level > c.titleLevel {
			c.title, c.titleLevel, c.uncertain = semanticPlain(heading.text), heading.level, false
		}

		return
	}
}

func (r *semanticReading) content(c *semanticCandidate, d *document, block semanticBlock) error {
	switch block.node.Data {
	case "ul", "ol":
		if semanticMostlyLinked(block.node) && r.filled(c) {
			r.mode = ""
			return nil
		}

		read := len(c.groups) + len(c.sections)

		var err error
		if r.mode == "ingredients" {
			err = c.readIngredients(d, block.node, r.group)
		} else {
			err = c.readSteps(d, block.node, r.section)
		}

		r.listed = r.listed || len(c.groups)+len(c.sections) > read

		return err
	case "table", "dl":
		switch {
		case r.filled(c):
			r.mode = ""
		case r.mode == "ingredients" && c.readTableIngredients(d, block.node, r.group):
			r.listed = true
		default:
			c.uncertain = true
		}

		return nil
	case "p", "div", "section":
		return r.paragraphBlock(c, block)
	default:
		if block.text != "" && block.level == 0 {
			return r.paragraphBlock(c, block)
		}

		if block.level > r.markerLevel && r.markerLevel > 0 {
			if r.mode == "ingredients" {
				r.group = semanticPlain(block.text)
			} else {
				r.section = semanticPlain(block.text)
			}

			r.listed, r.numbered = false, false

			return nil
		}

		r.mode = ""

		return nil
	}
}

func (r *semanticReading) paragraphBlock(c *semanticCandidate, block semanticBlock) error {
	if plain := semanticPlain(block.text); plain == "" || semanticStepNumber.MatchString(plain) || semanticDurationText(plain) {
		return nil
	}

	if r.mode == "instructions" && semanticStepTitle(block.text, r.following) &&
		(semanticNumber.MatchString(block.text) || !r.numbered && (!r.filled(c) || c.sections[len(c.sections)-1].Title != "" ||
			semanticEmphasized(block) || c.namesGroup(block.text))) {
		r.section = strings.TrimSuffix(semanticPlain(semanticNumber.ReplaceAllString(block.text, "$1")), ":")
		r.listed, r.numbered = false, false

		return nil
	}

	if r.mode == "instructions" && semanticLeadIn(block) && (!r.filled(c) || semanticMostlyLinked(block.node) && semanticStepNext(r.following)) {
		return nil
	}

	prose := r.mode == "ingredients" && !r.listed && r.filled(c) && semanticProseParagraph(block)

	if r.mode == "ingredients" && (r.listed || prose) && r.filled(c) && c.instructionMarkers == 0 &&
		semanticProcedure(semanticPlain(block.text)) && !semanticMostlyLinked(block.node) {
		if !c.implicitMethod {
			r.implicitFrom = semanticProgress{sections: len(c.sections), procedures: c.procedures, paragraphSteps: c.paragraphSteps}
		}

		r.mode, r.implicit, r.listed, r.numbered, r.sectionStart = "instructions", true, false, false, len(c.sections)
		c.implicitMethod = true

		return r.paragraph(c, block)
	}

	if prose || r.listed && r.filled(c) || r.mode == "instructions" && r.filled(c) && !semanticNumber.MatchString(block.text) &&
		(r.numbered && !semanticNumberedParagraphNext(r.following) || !semanticProcedure(semanticPlain(block.text)) ||
			semanticMostlyLinked(block.node)) {
		r.mode = ""
		return nil
	}

	return r.paragraph(c, block)
}

func (r *semanticReading) paragraph(c *semanticCandidate, block semanticBlock) error {
	if r.mode == "instructions" && semanticNumber.MatchString(block.text) {
		r.numbered = true
	}

	if c.readParagraph(block.text, block.node, r.mode, r.group, r.section) {
		r.mode = ""
	}

	return nil
}

func (c *semanticCandidate) semanticEntry(node *html.Node) bool {
	if node.Type != html.ElementNode || node.Data != "li" || semanticExcluded(node) {
		return false
	}

	if semanticLinkedOnly(node) {
		c.uncertain = true
		return false
	}

	return true
}

func (c *semanticCandidate) readParagraph(text string, node *html.Node, mode, group, section string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}

	switch {
	case mode == "ingredients":
		return c.readParagraphIngredients(text, node, group)
	case semanticNumber.MatchString(text) || semanticProcedure(semanticPlain(text)):
		if !semanticNumber.MatchString(text) && section == "" {
			c.paragraphSteps = true
		}

		text = semanticNumber.ReplaceAllString(text, "$1")

		c.sections = append(c.sections, InstructionSection{Title: section, Steps: []Step{{Text: text}}})
		if semanticProcedure(semanticPlain(text)) && !semanticLinkedOnly(node) {
			c.procedures++
		}
	default:
		c.uncertain = true
	}

	return false
}

const semanticBullets = "-–—•*·・●▪"

var semanticAddress = regexp.MustCompile(`(?:^|\s)\(https?://[^\s()]*\)`)

func semanticPlain(text string) string {
	return collapseSpace(semanticAddress.ReplaceAllString(text, ""))
}
