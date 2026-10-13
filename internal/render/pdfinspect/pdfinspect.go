// Package pdfinspect reads back the PDFs Savorum generates: page box sizes and
// selectable text. It reads only the shape of document Savorum itself produces.
package pdfinspect

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Page is one page of a generated document.
type Page struct {
	// Width and Height are the media box in PDF points, which is the size the
	// page will physically print at.
	Width  float64
	Height float64
	// Text is the readable content of the page, in drawing order.
	Text string
}

var (
	objectPattern = regexp.MustCompile(`(?m)^(\d+)\s+0\s+obj\b`)
	rootPattern   = regexp.MustCompile(`/Root\s+(\d+)\s+0\s+R`)
	countPattern  = regexp.MustCompile(`/Count\s+(\d+)`)
)

// PageCount reports how many pages a generated document has without decoding
// any of them.
func PageCount(pdf []byte) int {
	document, err := parse(pdf)
	if err != nil {
		return 0
	}

	pages, err := document.pageObjects()
	if err != nil {
		return 0
	}

	return len(pages)
}

// Pages reports every page with its exact size and readable text.
func Pages(pdf []byte) ([]Page, error) {
	document, err := parse(pdf)
	if err != nil {
		return nil, err
	}

	objects, err := document.pageObjects()
	if err != nil {
		return nil, err
	}

	pages := make([]Page, 0, len(objects))
	for _, number := range objects {
		body := document.objects[number]
		width, height := mediaBox(document, number)

		text, err := document.pageText(body)
		if err != nil {
			return nil, fmt.Errorf("read page %d text: %w", number, err)
		}

		pages = append(pages, Page{Width: width, Height: height, Text: text})
	}

	return pages, nil
}

// Text is every page's readable content joined, which is what a test asks when
// it wants to know that nothing was dropped from a recipe.
func Text(pdf []byte) (string, error) {
	pages, err := Pages(pdf)
	if err != nil {
		return "", err
	}

	parts := make([]string, 0, len(pages))
	for _, page := range pages {
		parts = append(parts, page.Text)
	}

	return strings.Join(parts, "\n"), nil
}

// TextSizes reports the type sizes a document actually draws with, in points.
func TextSizes(pdf []byte) ([]float64, error) {
	parsed, err := parse(pdf)
	if err != nil {
		return nil, err
	}

	numbers, err := parsed.pageObjects()
	if err != nil {
		return nil, err
	}

	var sizes []float64

	for _, number := range numbers {
		body, ok := parsed.objects[number]
		if !ok {
			continue
		}

		for _, contents := range references(body, "Contents") {
			stream, err := parsed.streamOf(contents)
			if err != nil {
				return nil, err
			}

			sizes = append(sizes, drawnTextSizes(stream)...)
		}
	}

	return sizes, nil
}

type document struct {
	source  []byte
	objects map[int][]byte
	root    int
}

func parse(pdf []byte) (*document, error) {
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		return nil, errors.New("not a PDF document")
	}

	matches := objectPattern.FindAllSubmatchIndex(pdf, -1)
	if len(matches) == 0 {
		return nil, errors.New("PDF contains no objects")
	}

	parsed := &document{source: pdf, objects: make(map[int][]byte, len(matches))}
	for index, match := range matches {
		number, err := strconv.Atoi(string(pdf[match[2]:match[3]]))
		if err != nil {
			continue
		}

		start := match[1]

		end := len(pdf)
		if index+1 < len(matches) {
			end = matches[index+1][0]
		}

		body := pdf[start:end]
		if stop := bytes.LastIndex(body, []byte("endobj")); stop >= 0 {
			body = body[:stop]
		}

		parsed.objects[number] = body
	}

	if root := rootPattern.FindSubmatch(pdf); root != nil {
		parsed.root, _ = strconv.Atoi(string(root[1]))
	}

	return parsed, nil
}

// pageObjects walks the page tree in reading order. Savorum's renderer emits a
// flat tree, but a nested one is followed rather than assumed away.
func (d *document) pageObjects() ([]int, error) {
	start := d.root
	if start != 0 {
		if pages := reference(d.objects[start], "Pages"); pages != 0 {
			start = pages
		}
	}

	if start == 0 || !isPageTreeNode(d.objects[start]) {
		start = d.findPageTreeRoot()
	}

	if start == 0 {
		return nil, errors.New("PDF has no page tree")
	}

	var (
		pages []int
		walk  func(number int, depth int)
	)

	walk = func(number, depth int) {
		if depth > 32 {
			return
		}

		body := d.objects[number]
		if isPage(body) {
			pages = append(pages, number)
			return
		}

		for _, child := range references(body, "Kids") {
			walk(child, depth+1)
		}
	}
	walk(start, 0)

	if len(pages) == 0 {
		return nil, errors.New("PDF page tree holds no pages")
	}

	return pages, nil
}

func (d *document) findPageTreeRoot() int {
	best, bestCount := 0, -1

	for number, body := range d.objects {
		if !isPageTreeNode(body) {
			continue
		}

		count := 0
		if match := countPattern.FindSubmatch(body); match != nil {
			count, _ = strconv.Atoi(string(match[1]))
		}

		if count > bestCount {
			best, bestCount = number, count
		}
	}

	return best
}

// pagePattern matches a page object without matching a page tree node. It
// deliberately never appends to the object body: the bodies are slices of the
// caller's document, and growing one would write into the bytes that follow it.
var pagePattern = regexp.MustCompile(`/Type\s*/Page(?:[^s]|\z)`)

func isPage(body []byte) bool {
	return pagePattern.Match(body)
}

func isPageTreeNode(body []byte) bool {
	return bytes.Contains(body, []byte("/Type /Pages")) || bytes.Contains(body, []byte("/Type/Pages"))
}

var mediaBoxPattern = regexp.MustCompile(`/MediaBox\s*\[\s*([-0-9.]+)\s+([-0-9.]+)\s+([-0-9.]+)\s+([-0-9.]+)\s*\]`)

// mediaBox reads the page's own box, falling back to the box its parent states
// the way a reader would.
func mediaBox(d *document, number int) (float64, float64) {
	for depth := 0; depth < 32 && number != 0; depth++ {
		body := d.objects[number]
		if match := mediaBoxPattern.FindSubmatch(body); match != nil {
			left := parseFloat(match[1])
			bottom := parseFloat(match[2])
			right := parseFloat(match[3])
			top := parseFloat(match[4])

			return right - left, top - bottom
		}

		number = reference(body, "Parent")
	}

	return 0, 0
}

func parseFloat(value []byte) float64 {
	parsed, _ := strconv.ParseFloat(string(value), 64)
	return parsed
}

func reference(body []byte, key string) int {
	match := regexp.MustCompile(`/` + key + `\s+(\d+)\s+0\s+R`).FindSubmatch(body)
	if match == nil {
		return 0
	}

	number, _ := strconv.Atoi(string(match[1]))

	return number
}

func references(body []byte, key string) []int {
	match := regexp.MustCompile(`/` + key + `\s*\[([^\]]*)\]`).FindSubmatch(body)
	if match == nil {
		if single := reference(body, key); single != 0 {
			return []int{single}
		}

		return nil
	}

	found := regexp.MustCompile(`(\d+)\s+0\s+R`).FindAllSubmatch(match[1], -1)

	numbers := make([]int, 0, len(found))
	for _, entry := range found {
		number, err := strconv.Atoi(string(entry[1]))
		if err == nil {
			numbers = append(numbers, number)
		}
	}

	return numbers
}

// streamOf returns an object's decoded stream payload.
func (d *document) streamOf(number int) ([]byte, error) {
	body := d.objects[number]

	before, after, ok := bytes.Cut(body, []byte("stream"))
	if !ok {
		return nil, nil
	}

	payload := after
	payload = bytes.TrimPrefix(payload, []byte("\r"))

	payload = bytes.TrimPrefix(payload, []byte("\n"))
	if stop := bytes.LastIndex(payload, []byte("endstream")); stop >= 0 {
		payload = payload[:stop]
	}

	if !bytes.Contains(before, []byte("/FlateDecode")) {
		return payload, nil
	}

	reader, err := zlib.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("open compressed stream: %w", err)
	}
	defer reader.Close()

	decoded, err := io.ReadAll(io.LimitReader(reader, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("read compressed stream: %w", err)
	}

	return decoded, nil
}

// dictionaryValue extracts a nested dictionary written inline under a key,
// balancing the angle brackets so a dictionary containing others is returned
// whole rather than cut at the first close.
func dictionaryValue(body []byte, key string) []byte {
	marker := regexp.MustCompile(`/` + key + `\s*<<`).FindIndex(body)
	if marker == nil {
		return nil
	}

	start := marker[1] - 2
	depth := 0

	for index := start; index+1 < len(body); index++ {
		switch {
		case body[index] == '<' && body[index+1] == '<':
			depth++
			index++
		case body[index] == '>' && body[index+1] == '>':
			depth--

			index++
			if depth == 0 {
				return body[start : index+1]
			}
		}
	}

	return nil
}

// TextRun is one run of text as the page actually draws it: what it says, the
// size it is set in, and where its baseline sits. Y grows upwards, as PDF
// coordinates do, so a run further down the page has a smaller Y.
//
// Tests use these to measure typesetting on the page rather than the numbers
// fed to the typesetter.
type TextRun struct {
	Text string
	Size float64
	X, Y float64
}

// TextRuns reports every run of text on a page, in the order it is drawn.
func TextRuns(pdf []byte, page int) ([]TextRun, error) {
	parsed, err := parse(pdf)
	if err != nil {
		return nil, err
	}

	numbers, err := parsed.pageObjects()
	if err != nil {
		return nil, err
	}

	if page < 0 || page >= len(numbers) {
		return nil, fmt.Errorf("page %d is outside the document", page+1)
	}

	body := parsed.objects[numbers[page]]
	fonts := parsed.pageFonts(body)

	var runs []TextRun

	for _, contents := range references(body, "Contents") {
		stream, err := parsed.streamOf(contents)
		if err != nil {
			return nil, err
		}

		runs = append(runs, drawnTextRuns(stream, fonts)...)
	}

	return runs, nil
}
