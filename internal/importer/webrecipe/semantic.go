package webrecipe

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/savorum/savorum/internal/recipetext"
)

const (
	maximumSemanticRoots  = 32
	maximumSemanticBlocks = 10_000
	maximumSemanticText   = 256 << 10
	maximumSemanticEntry  = 64 << 10
)

var (
	semanticAmount = regexp.MustCompile(`^[\p{N}¼½¾⅓⅔⅛⅜⅝⅞]+(?:[\s.,/–−-]|\p{L})`)
	semanticNumber = regexp.MustCompile(`^\s*\d+[.)]\s+`)
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
	blocks             []semanticBlock
	groups             []IngredientGroup
	sections           []InstructionSection
	ingredientMarkers  int
	instructionMarkers int
	amounts            int
	procedures         int
	uncertain          bool
}

// semanticRecipe assigns fields only when DOM section boundaries and repeated
// entry evidence agree within one article. Metadata never supplies that evidence.
func (d *document) semanticRecipe() (*Recipe, error) {
	roots, err := d.semanticRoots()
	if err != nil {
		return nil, controlledExtractionError(err)
	}

	var candidates []semanticCandidate

	for _, root := range roots {
		blocks, err := d.semanticBlocks(root)
		if err != nil {
			return nil, controlledExtractionError(err)
		}

		candidate := semanticCandidate{blocks: blocks}
		if err := candidate.read(d); err != nil {
			return nil, controlledExtractionError(err)
		}

		if candidate.recipeLike() {
			candidates = append(candidates, candidate)
		}
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	chosen := candidates[0]
	result := d.semanticMetadata(chosen.title)
	result.ExtractionMethod = "semantic-html"

	if len(candidates) == 1 && chosen.strong() {
		result.IngredientGroups = chosen.groups
		result.Sections = chosen.sections
		result.warn("semantic-html", "Ingredients and instructions were read from this page's article. Review the recipe before saving.")
	} else {
		text, err := d.semanticReviewText(candidates)
		if err != nil {
			return nil, controlledExtractionError(err)
		}

		result.ReviewText = text
		result.Notes = []string{text}
		result.warn("semantic-html-review", "This page's recipe sections are incomplete or ambiguous. Edit the extracted text to choose ingredients and steps before saving.")
	}

	return result, nil
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

	return c.title != "" && !c.uncertain && c.ingredientMarkers == 1 && c.instructionMarkers == 1 &&
		ingredients >= 3 && c.amounts >= 3 && c.amounts*2 >= ingredients && steps >= 2 && c.procedures >= 2 && c.procedures*2 >= steps
}

// Leaf article regions prevent ingredients in one recipe from borrowing the
// method or title of another. A main region is used when it has no articles.
func (d *document) semanticRoots() ([]*html.Node, error) {
	var roots []*html.Node

	var walk func(*html.Node) (bool, error)

	walk = func(node *html.Node) (bool, error) {
		if node == nil || semanticExcluded(node) {
			return false, nil
		}

		if err := d.budget.spend(1); err != nil {
			return false, err
		}

		hasRegion := false

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			found, err := walk(child)
			if err != nil {
				return false, err
			}

			hasRegion = hasRegion || found
		}

		region := node.Type == html.ElementNode && (node.Data == "article" || node.Data == "main" ||
			strings.EqualFold(attribute(node, "role"), "main") || strings.EqualFold(attribute(node, "role"), "article"))
		if region && !hasRegion {
			roots = append(roots, node)
			if len(roots) > maximumSemanticRoots {
				return false, errExtractionLimited
			}
		}

		return region || hasRegion, nil
	}

	_, err := walk(d.root)

	return roots, err
}

func semanticExcluded(node *html.Node) bool {
	if node.Type != html.ElementNode {
		return false
	}

	switch node.Data {
	case "nav", "footer", "aside", "script", "style", "template", "noscript", "form", "button":
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

func headingLevel(node *html.Node) int {
	if node.Type == html.ElementNode && len(node.Data) == 2 && node.Data[0] == 'h' && node.Data[1] >= '1' && node.Data[1] <= '6' {
		return int(node.Data[1] - '0')
	}

	return 0
}

func (d *document) semanticBlocks(root *html.Node) ([]semanticBlock, error) {
	var blocks []semanticBlock

	var walk func(*html.Node) error

	walk = func(node *html.Node) error {
		if semanticExcluded(node) {
			return nil
		}

		if err := d.budget.spend(1); err != nil {
			return err
		}

		level := headingLevel(node)
		if node.Type == html.ElementNode && (level != 0 || node.Data == "p" || node.Data == "ul" || node.Data == "ol" || node.Data == "hr") {
			text := ""

			if node.Data != "ul" && node.Data != "ol" {
				var err error

				text, err = d.semanticText(node, false)
				if err != nil {
					return err
				}
			}

			blocks = append(blocks, semanticBlock{node: node, text: text, level: level})
		} else {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if err := walk(child); err != nil {
					return err
				}
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

type semanticReading struct {
	mode        string
	group       string
	section     string
	marker      *html.Node
	markerLevel int
}

func (c *semanticCandidate) read(d *document) error {
	reading := semanticReading{}
	for index, block := range c.blocks {
		if reading.boundary(c, block) || reading.mode == "" {
			continue
		}

		if semanticGroupLabel(block, c.blocks, index) {
			title := strings.TrimSuffix(collapseSpace(block.text), ":")
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

func (r *semanticReading) boundary(c *semanticCandidate, block semanticBlock) bool {
	if block.end {
		if withinNode(r.marker, block.node) {
			r.mode = ""
		}

		return true
	}

	kind, _ := recipetext.Heading(collapseSpace(block.text))
	if kind != "" {
		r.mode, r.marker, r.markerLevel = kind, block.node, block.level
		r.group, r.section = "", ""

		if kind == "ingredients" {
			c.ingredientMarkers++
		} else {
			c.instructionMarkers++
		}

		return true
	}

	c.readTitle(block)

	if block.node.Data == "hr" || (block.level > 0 && r.markerLevel > 0 && block.level <= r.markerLevel) {
		r.mode = ""
		return true
	}

	return false
}

func (c *semanticCandidate) readTitle(block semanticBlock) {
	if block.level == 0 || c.ingredientMarkers+c.instructionMarkers != 0 || (c.title != "" && block.level > c.titleLevel) {
		return
	}

	if c.title != "" && block.level == c.titleLevel {
		c.uncertain = true
	}

	c.title = collapseSpace(block.text)
	c.titleLevel = block.level
}

func (r *semanticReading) content(c *semanticCandidate, d *document, block semanticBlock) error {
	switch block.node.Data {
	case "ul", "ol":
		if r.mode == "ingredients" {
			return c.readIngredients(d, block.node, r.group)
		}

		return c.readSteps(d, block.node, r.section)
	case "p":
		return c.readParagraph(d, block.node, r.mode, r.group, r.section)
	default:
		r.mode = ""
		return nil
	}
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

	if block.level > 0 || strings.HasSuffix(text, ":") {
		return true
	}

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

		if text != "" {
			group.Ingredients = append(group.Ingredients, Ingredient{Text: text})
			if semanticAmount.MatchString(text) {
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
	return utf8.RuneCountInString(text) >= 12 && len(strings.Fields(text)) >= 3 && !semanticAmount.MatchString(text)
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

func (c *semanticCandidate) readParagraph(d *document, node *html.Node, mode, group, section string) error {
	text, err := d.semanticText(node, false)
	if err != nil {
		return err
	}

	if strings.TrimSpace(text) == "" {
		return nil
	}

	switch {
	case mode == "ingredients":
		lines := strings.Split(text, "\n")
		ingredients := IngredientGroup{Title: group}

		for _, line := range lines {
			if line = strings.TrimSpace(line); line != "" {
				ingredients.Ingredients = append(ingredients.Ingredients, Ingredient{Text: line})
				if semanticAmount.MatchString(line) && !semanticLinkedOnly(node) {
					c.amounts++
				} else {
					c.uncertain = true
				}
			}
		}

		c.groups = append(c.groups, ingredients)
	case semanticNumber.MatchString(text):
		text = semanticNumber.ReplaceAllString(text, "")

		c.sections = append(c.sections, InstructionSection{Title: section, Steps: []Step{{Text: text}}})
		if semanticProcedure(text) && !semanticLinkedOnly(node) {
			c.procedures++
		}
	default:
		c.uncertain = true
	}

	return nil
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
