package webrecipe

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/recipetext"
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

// Nested article regions keep their content out of the enclosing candidate.
func (d *document) semanticRoots() ([]*html.Node, error) {
	var (
		roots []*html.Node
		walk  func(*html.Node, bool) error
	)

	walk = func(node *html.Node, insideRegion bool) error {
		if node == nil || semanticExcluded(node) {
			return nil
		}

		if err := d.budget.spend(1); err != nil {
			return err
		}

		region := semanticRegion(node, insideRegion)
		if region {
			roots = append(roots, node)
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child, insideRegion || region); err != nil {
				return err
			}
		}

		return nil
	}
	err := walk(d.root, false)

	return roots, err
}

func semanticRegion(node *html.Node, insideRegion bool) bool {
	if node.Type != html.ElementNode {
		return false
	}

	if semanticEntryRegion(node) || node.Data == "main" || strings.EqualFold(attribute(node, "role"), "main") {
		return true
	}

	return !insideRegion && slices.Contains(strings.Fields(attribute(node, "itemprop")), "articleBody")
}

func semanticEntryRegion(node *html.Node) bool {
	return node.Type == html.ElementNode && (node.Data == "article" || strings.EqualFold(attribute(node, "role"), "article") ||
		slices.Contains(strings.Fields(attribute(node, "class")), "hentry") || slices.Contains(strings.Fields(attribute(node, "class")), "h-entry"))
}

func semanticExcluded(node *html.Node) bool {
	if node.Type != html.ElementNode {
		return false
	}

	switch node.Data {
	case "nav", "footer", "aside", "script", "style", "template", "noscript", "form", "button":
		return true
	}

	if slices.Contains(strings.Fields(attribute(node, "itemprop")), "comment") || semanticChoice(node) {
		return true
	}

	switch strings.ToLower(attribute(node, "role")) {
	case "navigation", "contentinfo", "complementary", "dialog":
		return true
	}

	for _, attr := range node.Attr {
		if attr.Key == "hidden" || attr.Key == "inert" || (attr.Key == "aria-hidden" && strings.EqualFold(attr.Val, "true")) {
			return true
		}
	}

	style := strings.ToLower(strings.ReplaceAll(attribute(node, "style"), " ", ""))

	return strings.Contains(style, "display:none") || strings.Contains(style, "visibility:hidden")
}

func semanticChoice(node *html.Node) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "input" && strings.EqualFold(attribute(child, "type"), "radio") {
			return true
		}
	}

	return false
}

func headingLevel(node *html.Node) int {
	if node.Type == html.ElementNode && len(node.Data) == 2 && node.Data[0] == 'h' && node.Data[1] >= '1' && node.Data[1] <= '6' {
		return int(node.Data[1] - '0')
	}

	return 0
}

func (d *document) semanticBlocks(root *html.Node, regions map[*html.Node]bool) ([]semanticBlock, error) {
	var blocks []semanticBlock

	var walk func(*html.Node) error

	walk = func(node *html.Node) error {
		if semanticExcluded(node) || node != root && regions[node] {
			return nil
		}

		if err := d.budget.spend(1); err != nil {
			return err
		}

		entries, atomic, err := d.semanticNodeBlocks(node)
		if err != nil {
			return err
		}

		if atomic {
			blocks = append(blocks, entries...)
		} else {
			if err := d.semanticChildren(node, walk, &blocks); err != nil {
				return err
			}

			if node.Type == html.ElementNode && node.Data == "section" {
				blocks = append(blocks, semanticBlock{node: node, end: true})
			}
		}

		if len(blocks) > maximumSemanticBlocks {
			return errExtractionLimited
		}

		return nil
	}

	err := walk(root)

	return blocks, err
}

func (d *document) semanticChildren(node *html.Node, walk func(*html.Node) error, blocks *[]semanticBlock) error {
	writer := semanticTextWriter{document: d, root: node}
	flush := func() {
		*blocks = append(*blocks, semanticLines(node, writer.value())...)
		writer.text.Reset()
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if semanticExcluded(child) {
			continue
		}

		if semanticInlineNode(child) {
			if err := writer.walk(child, ""); err != nil {
				return err
			}
		} else {
			flush()

			if err := walk(child); err != nil {
				return err
			}
		}

		if len(*blocks) > maximumSemanticBlocks {
			return errExtractionLimited
		}
	}

	flush()

	return nil
}

func semanticInlineNode(node *html.Node) bool {
	if node.Type == html.TextNode || node.Type == html.CommentNode {
		return true
	}

	if node.Type != html.ElementNode {
		return false
	}

	switch node.Data {
	case "a", "abbr", "b", "bdi", "bdo", "br", "cite", "code", "data", "del", "dfn", "em", "i", "img", "ins", "kbd", "mark", "q", "rp", "rt", "ruby", "s", "samp", "small", "span", "strong", "sub", "sup", "time", "u", "var", "wbr":
		return true
	default:
		return false
	}
}

func semanticLines(node *html.Node, text string) []semanticBlock {
	var blocks []semanticBlock

	for line := range strings.SplitSeq(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			blocks = append(blocks, semanticBlock{node: node, text: line})
		}
	}

	return blocks
}

func (d *document) semanticNodeBlocks(node *html.Node) ([]semanticBlock, bool, error) {
	level := headingLevel(node)

	inline := node.Data == "p" || node.Data == "li"
	if node.Type != html.ElementNode || level == 0 && !inline && node.Data != "ul" && node.Data != "ol" && node.Data != "hr" && node.Data != "table" && node.Data != "dl" {
		return nil, false, nil
	}

	text := ""

	if node.Data != "ul" && node.Data != "ol" {
		var err error

		text, err = d.semanticText(node, false)
		if err != nil {
			return nil, false, err
		}
	}

	if !inline {
		return []semanticBlock{{node: node, text: text, level: level}}, true, nil
	}

	return semanticLines(node, text), true, nil
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
		case r.mode == "ingredients" && c.readTableIngredients(block.node, r.group):
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

func (c *semanticCandidate) namesGroup(text string) bool {
	title := strings.TrimSuffix(semanticPlain(text), ":")

	return slices.ContainsFunc(c.groups, func(group IngredientGroup) bool {
		return group.Title != "" && strings.EqualFold(strings.TrimSuffix(group.Title, ":"), title)
	})
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

func withinNode(node, ancestor *html.Node) bool {
	for current := node; current != nil; current = current.Parent {
		if current == ancestor {
			return true
		}
	}

	return false
}

func semanticGroupLabel(block semanticBlock, blocks []semanticBlock, index int) bool {
	text := collapseSpace(block.text)
	if text == "" || len(text) > 100 || semanticAmount.MatchString(text) || semanticNumber.MatchString(text) {
		return false
	}

	if index+1 >= len(blocks) || (blocks[index+1].node.Data != "ul" && blocks[index+1].node.Data != "ol") {
		return false
	}

	return block.level > 0 || strings.HasSuffix(text, ":") || semanticEmphasized(block)
}

func semanticEmphasized(block semanticBlock) bool {
	text := collapseSpace(block.text)

	for _, strong := range append(elementsByTag(block.node, "strong"), elementsByTag(block.node, "b")...) {
		if collapseSpace(textContent(strong)) == text {
			return true
		}
	}

	return false
}

func (c *semanticCandidate) readIngredients(d *document, list *html.Node, title string) error {
	group := IngredientGroup{Title: title}

	for row := list.FirstChild; row != nil; row = row.NextSibling {
		if !c.semanticEntry(row) {
			continue
		}

		text, err := d.semanticText(row, true)
		if err != nil {
			return err
		}

		nested, err := d.semanticNestedLists(row)
		if err != nil {
			return err
		}

		if len(nested) > 0 {
			if len(group.Ingredients) > 0 {
				c.groups = append(c.groups, group)
				group = IngredientGroup{Title: title}
			}

			nestedTitle := text
			if title != "" && text != "" {
				nestedTitle = title + " · " + text
			}

			if err := c.readNestedIngredients(d, nested, nestedTitle); err != nil {
				return err
			}

			continue
		}

		if text != "" && !recipetext.ServingLine(semanticPlain(text)) {
			group.Ingredients = append(group.Ingredients, Ingredient{Text: semanticPlain(text)})
			if c.statesAmount(text) {
				c.amounts++
			}
		}
	}

	if len(group.Ingredients) > 0 {
		c.groups = append(c.groups, group)
	}

	return nil
}

func (d *document) semanticNestedLists(root *html.Node) ([]*html.Node, error) {
	var (
		lists []*html.Node
		walk  func(*html.Node) error
	)

	walk = func(node *html.Node) error {
		if semanticExcluded(node) {
			return nil
		}

		if err := d.budget.spend(1); err != nil {
			return err
		}

		if node.Data == "ul" || node.Data == "ol" {
			lists = append(lists, node)
			return nil
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child); err != nil {
				return err
			}
		}

		return nil
	}

	err := walk(root)

	return lists, err
}

func (c *semanticCandidate) readNestedIngredients(d *document, lists []*html.Node, title string) error {
	if title == "" || semanticAmount.MatchString(title) {
		c.uncertain = true
	}

	for _, list := range lists {
		if err := c.readIngredients(d, list, title); err != nil {
			return err
		}
	}

	return nil
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

func semanticProcedure(text string) bool {
	return utf8.RuneCountInString(text) >= 12 && (len(strings.Fields(text)) >= 3 || recipetext.Unspaced(text)) &&
		!semanticAmount.MatchString(text)
}

func semanticMostlyLinked(node *html.Node) bool {
	linked := 0
	for _, anchor := range elementsByTag(node, "a") {
		linked += utf8.RuneCountInString(collapseSpace(textContent(anchor)))
	}

	total := utf8.RuneCountInString(collapseSpace(textContent(node)))

	return total > 0 && linked*2 >= total
}

func semanticLinkedOnly(node *html.Node) bool {
	text := collapseSpace(textContent(node))
	for _, anchor := range elementsByTag(node, "a") {
		if collapseSpace(textContent(anchor)) == text {
			return true
		}
	}

	return false
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

// Allow columns for an ingredient name, amount, and unit or note.
const maximumIngredientColumns = 3

func (c *semanticCandidate) readTableIngredients(table *html.Node, title string) bool {
	group := IngredientGroup{Title: title}
	read := false

	for _, row := range ingredientTableRows(table) {
		if len(row) > maximumIngredientColumns {
			return false
		}

		line := strings.Join(slices.DeleteFunc(row, func(cell string) bool { return cell == "" }), " ")

		switch {
		case line == "":
		case len(row) == 1 && !c.statesAmount(line):
			if len(group.Ingredients) > 0 {
				c.groups = append(c.groups, group)
			}

			group = IngredientGroup{Title: strings.TrimSuffix(line, ":")}
		default:
			group.Ingredients = append(group.Ingredients, Ingredient{Text: line})
			read = true

			if c.statesAmount(line) {
				c.amounts++
			}
		}
	}

	if len(group.Ingredients) > 0 {
		c.groups = append(c.groups, group)
	}

	return read
}

func ingredientTableRows(table *html.Node) [][]string {
	var rows [][]string

	if table.Data == "dl" {
		for _, term := range elementsByTag(table, "dt") {
			row := []string{collapseSpace(textContent(term))}

			for next := term.NextSibling; next != nil && (next.Type != html.ElementNode || next.Data != "dt"); next = next.NextSibling {
				if next.Type == html.ElementNode && next.Data == "dd" {
					row = append(row, collapseSpace(textContent(next)))
				}
			}

			rows = append(rows, row)
		}
	} else {
		for _, tableRow := range elementsByTag(table, "tr") {
			var row []string

			for cell := tableRow.FirstChild; cell != nil; cell = cell.NextSibling {
				if cell.Type == html.ElementNode && (cell.Data == "td" || cell.Data == "th") {
					row = append(row, collapseSpace(textContent(cell)))
				}
			}

			rows = append(rows, row)
		}
	}

	return rows
}

func (c *semanticCandidate) readParagraphIngredients(text string, node *html.Node, group string) bool {
	ingredients := IngredientGroup{Title: group}
	ended := false

	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if recipetext.OtherHeading(semanticPlain(line)) {
			ended = true
			break
		}

		if recipetext.ServingLine(semanticPlain(line)) {
			continue
		}

		if label, found := strings.CutSuffix(line, ":"); found && !semanticAmount.MatchString(line) && len(strings.Fields(label)) <= 5 {
			if len(ingredients.Ingredients) > 0 {
				c.addParagraphGroup(ingredients)
			}

			ingredients = IngredientGroup{Title: semanticPlain(label)}

			continue
		}

		ingredients.Ingredients = append(ingredients.Ingredients, Ingredient{Text: line})

		amount := c.statesAmount(line)

		switch {
		case semanticLinkedOnly(node) || semanticProse(line) && (!amount || semanticLong(line)):
			c.uncertain = true
		case amount:
			c.amounts++
		}
	}

	if len(ingredients.Ingredients) > 0 {
		c.addParagraphGroup(ingredients)
	}

	return ended
}

func (c *semanticCandidate) addParagraphGroup(group IngredientGroup) {
	if last := len(c.groups) - 1; last >= 0 && c.groups[last].Title == group.Title {
		c.groups[last].Ingredients = append(c.groups[last].Ingredients, group.Ingredients...)
		return
	}

	c.groups = append(c.groups, group)
}

const semanticBullets = "-–—•*·・●▪"

var semanticAddress = regexp.MustCompile(`(?:^|\s)\(https?://[^\s()]*\)`)

func semanticPlain(text string) string {
	return collapseSpace(semanticAddress.ReplaceAllString(text, ""))
}

func (c *semanticCandidate) statesAmount(line string) bool {
	if semanticStatesAmount(line) {
		return true
	}

	parsed := ingredientparser.ParseDetailed(semanticPlain(line), "", c.locale)

	return len(parsed.Ambiguities) > 0 || parsed.Ingredient.Quantity != nil && parsed.Ingredient.Quantity.Kind != recipe.QuantityOpaque
}

func semanticStatesAmount(line string) bool {
	line = strings.TrimLeft(line, semanticBullets+" \t")

	return semanticAmount.MatchString(line) || semanticTrailingAmount.MatchString(line)
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

func (d *document) semanticReviewText(candidates []semanticCandidate) (string, error) {
	var text strings.Builder

	for _, candidate := range candidates {
		for _, block := range candidate.blocks {
			if block.end {
				continue
			}

			value := block.text
			if block.node.Data == "ul" || block.node.Data == "ol" {
				var err error

				value, err = d.semanticText(block.node, false)
				if err != nil {
					return "", err
				}
			}

			if value != "" {
				text.WriteString(value)
				text.WriteString("\n\n")
			}

			if text.Len() > maximumSemanticText {
				return "", errExtractionLimited
			}
		}
	}

	return strings.TrimSpace(text.String()), nil
}
