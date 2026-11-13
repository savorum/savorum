package webrecipe

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
)

type semanticTextWriter struct {
	document  *document
	text      strings.Builder
	root      *html.Node
	omitLists bool
}

func (d *document) semanticText(root *html.Node, omitLists bool) (string, error) {
	writer := semanticTextWriter{document: d, root: root, omitLists: omitLists}
	if err := writer.walk(root, ""); err != nil {
		return "", err
	}

	return writer.value(), nil
}

func (w *semanticTextWriter) value() string {
	var lines []string

	for line := range strings.SplitSeq(w.text.String(), "\n") {
		line = collapseSpace(line)
		if line == "" && (len(lines) == 0 || lines[len(lines)-1] == "") {
			continue
		}

		lines = append(lines, line)
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (w *semanticTextWriter) walk(node *html.Node, marker string) error {
	if semanticExcluded(node) || (w.omitLists && node != w.root && (node.Data == "ul" || node.Data == "ol")) {
		return nil
	}

	if err := w.document.budget.spend(1 + len(node.Data)); err != nil {
		return err
	}

	w.open(node, marker)

	if err := w.children(node); err != nil {
		return err
	}

	w.close(node)

	if w.text.Len() > maximumSemanticEntry {
		return errExtractionLimited
	}

	return nil
}

func (w *semanticTextWriter) open(node *html.Node, marker string) {
	if node.Type == html.TextNode {
		w.text.WriteString(node.Data)
	}

	if node.Type != html.ElementNode {
		return
	}

	if w.separatesTrailingPart(node) {
		w.text.WriteByte(' ')
	}

	switch node.Data {
	case "p":
		if node.Parent == nil || node.Parent.Data != "li" || node.PrevSibling != nil {
			w.text.WriteString("\n\n")
		}
	case "div", "section", "ul", "ol":
		w.text.WriteString("\n\n")
	case "br":
		w.text.WriteByte('\n')
	case "li":
		if node != w.root {
			w.text.WriteByte('\n')
			w.text.WriteString(marker)
		}
	}
}

// Preserve layout-separated amounts and names, such as "Beer<span>250 ml</span>".
func (w *semanticTextWriter) separatesTrailingPart(node *html.Node) bool {
	if !semanticInlineNode(node) || node.Data == "br" || node.Data == "wbr" || node.PrevSibling == nil {
		return false
	}

	if node.PrevSibling.Type != html.ElementNode || !semanticInlineNode(node.PrevSibling) {
		for next := node.NextSibling; next != nil; next = next.NextSibling {
			if next.Type != html.TextNode || strings.TrimSpace(next.Data) != "" {
				return false
			}
		}
	}

	written := w.text.String()
	last, _ := utf8.DecodeLastRuneInString(written)
	first, _ := utf8.DecodeRuneInString(strings.TrimLeftFunc(textContent(node), unicode.IsSpace))

	return written != "" && !unicode.IsSpace(last) && (unicode.IsLetter(first) || unicode.IsDigit(first))
}

func (w *semanticTextWriter) close(node *html.Node) {
	if node.Type != html.ElementNode {
		return
	}

	switch node.Data {
	case "a":
		if address := safeProvenanceURL(attribute(node, "href"), w.document.page.URL); address != "" && collapseSpace(textContent(node)) != "" {
			w.text.WriteString(" (" + address + ")")
		}
	case "p", "div", "section", "ul", "ol", "tr", "dt", "dd":
		w.text.WriteString("\n\n")
	case "li":
		w.text.WriteByte('\n')
	}
}

func (w *semanticTextWriter) children(node *html.Node) error {
	number, increment := 1, 1
	if node.Data == "ol" {
		number, increment = semanticListStart(node)
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		marker := "- "

		if child.Type == html.ElementNode && child.Data == "li" && node.Data == "ol" {
			if stated, err := strconv.Atoi(attribute(child, "value")); err == nil {
				number = stated
			}

			marker = strconv.Itoa(number) + ". "
			number += increment
		}

		if err := w.walk(child, marker); err != nil {
			return err
		}
	}

	return nil
}

func semanticListStart(node *html.Node) (number, increment int) {
	number, increment = 1, 1

	for _, attr := range node.Attr {
		if attr.Key == "reversed" {
			increment = -1
			number = 0

			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.ElementNode && child.Data == "li" {
					number++
				}
			}
		}
	}

	if stated, err := strconv.Atoi(attribute(node, "start")); err == nil {
		number = stated
	}

	return number, increment
}
