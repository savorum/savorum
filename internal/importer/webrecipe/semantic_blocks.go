package webrecipe

import (
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

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

func withinNode(node, ancestor *html.Node) bool {
	for current := node; current != nil; current = current.Parent {
		if current == ancestor {
			return true
		}
	}

	return false
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
