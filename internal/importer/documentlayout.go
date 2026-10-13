package importer

// Reading order for a page set in columns. Text taken line by line across
// ingredients beside a method interleaves them irrecoverably, so both
// extraction paths ask their tool for word geometry and rebuild the page here:
// words into lines, lines into columns, columns in reading order.
import (
	"encoding/xml"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// A column is only believed when both sides hold enough lines to be a column
// rather than a ragged edge, and when few enough lines cross the gutter for it
// to be a gutter rather than a coincidence.
const (
	minColumnLines    = 3
	maxCrossingShare  = 0.25
	minGutterShare    = 0.02
	columnSearchStart = 0.15
	columnSearchEnd   = 0.85
	maxColumnDepth    = 3
	// A horizontal gap wider than this many word heights is a column boundary
	// rather than a word space, which is what keeps a heading over one column
	// from being glued to the heading of the next.
	columnGapHeights = 2.5
	// How far the middles of one line's words may spread, as a fraction of the
	// page's type size.
	lineSpread = 0.7
	// A step of this many times a column's own leading is the break a page set
	// between two paragraphs rather than the space between two lines of one.
	paragraphLeading = 1.6
)

// layoutWord is one word and where it sits on the page, in whatever units the
// extraction tool reports: points for a PDF, pixels for a raster image. Only
// ratios matter here, so the two are handled identically.
type layoutWord struct {
	minX, minY, maxX, maxY float64
	text                   string
}

// layoutLine is a run of words a reader takes in as one line. It keeps the
// words it was built from so a line can be dealt back out to the column it
// turns out to belong to.
type layoutLine struct {
	minX, minY, maxX, maxY float64
	text                   string
	words                  []layoutWord
}

// documentPageText rebuilds one page's reading order from its words.
func documentPageText(words []layoutWord) string {
	lines := pageLines(words, 0)
	if len(lines) == 0 {
		return ""
	}

	return strings.Join(readingOrder(lines, 0), "\n")
}

// pageLines reads the page's lines, one column at a time. Grouping words into
// lines across a whole page set in columns measures a line of one column
// against a line of the other, and a heading in the second column sitting a
// little above the first column's line is enough to break that line in two. So
// the columns are found from a provisional grouping, the words are dealt back
// out to them, and each column is grouped on its own. Words that span the
// channel belong to neither column and keep their own line.
func pageLines(words []layoutWord, depth int) []layoutLine {
	lines := groupLayoutLines(words)
	if depth >= maxColumnDepth || len(lines) < minColumnLines*2 {
		return lines
	}

	gutter, found := findGutter(lines)
	if !found {
		return lines
	}

	var left, right, spanning []layoutWord

	// A line is dealt out whole. Splitting on the words alone would put the
	// last word of a headline set across the page into the second column.
	for _, line := range lines {
		switch {
		case line.maxX <= gutter:
			left = append(left, line.words...)
		case line.minX >= gutter:
			right = append(right, line.words...)
		default:
			spanning = append(spanning, line.words...)
		}
	}

	regrouped := append(pageLines(left, depth+1), pageLines(right, depth+1)...)
	regrouped = append(regrouped, groupLayoutLines(spanning)...)

	sort.SliceStable(regrouped, func(left, right int) bool {
		if regrouped[left].minY != regrouped[right].minY {
			return regrouped[left].minY < regrouped[right].minY
		}

		return regrouped[left].minX < regrouped[right].minX
	})

	return regrouped
}

// groupLayoutLines gathers words into lines. Words are ordered by their
// vertical middle and gathered while the whole run's middles stay inside a
// fraction of a line, which is stable against the descenders and mixed type
// sizes that make a word's own box a poor guide to which line it is on. The
// bound is on the run's spread rather than its distance from a first or
// average word: on a page set in columns the words of two columns arrive
// interleaved, and either of those drifts with them until it cuts a line in
// half. Line leading is comfortably wider than this, so two stacked lines
// still separate.
func groupLayoutLines(words []layoutWord) []layoutLine {
	if len(words) == 0 {
		return nil
	}

	ordered := append([]layoutWord(nil), words...)
	sort.SliceStable(ordered, func(left, right int) bool {
		if middle(ordered[left]) != middle(ordered[right]) {
			return middle(ordered[left]) < middle(ordered[right])
		}

		return ordered[left].minX < ordered[right].minX
	})

	var (
		bands        [][]layoutWord
		band         []layoutWord
		lowest       float64
		highest      float64
		lineTogether = lineSpread * medianHeight(ordered)
	)

	for _, word := range ordered {
		center := middle(word)
		if len(band) > 0 && math.Max(highest, center)-math.Min(lowest, center) > lineTogether {
			bands = append(bands, band)
			band = nil
		}

		if len(band) == 0 {
			lowest, highest = center, center
		}

		lowest, highest = math.Min(lowest, center), math.Max(highest, center)

		band = append(band, word)
	}

	if len(band) > 0 {
		bands = append(bands, band)
	}

	gap := columnGap(ordered)

	var lines []layoutLine
	for _, member := range bands {
		lines = append(lines, splitBandAtColumnGaps(member, gap)...)
	}

	sort.SliceStable(lines, func(left, right int) bool {
		if lines[left].minY != lines[right].minY {
			return lines[left].minY < lines[right].minY
		}

		return lines[left].minX < lines[right].minX
	})

	return lines
}

func middle(word layoutWord) float64 {
	return (word.minY + word.maxY) / 2
}

// medianHeight is the page's own type size, which is what makes the same
// thresholds work for a PDF measured in points and a screenshot in pixels.
func medianHeight(words []layoutWord) float64 {
	heights := make([]float64, 0, len(words))

	for _, word := range words {
		if height := word.maxY - word.minY; height > 0 {
			heights = append(heights, height)
		}
	}

	if len(heights) == 0 {
		return 0
	}

	sort.Float64s(heights)

	return heights[len(heights)/2]
}

// columnGap is the horizontal distance that separates columns rather than
// words, measured against the page's own type size so a PDF in points and a
// screenshot in pixels are judged the same way.
func columnGap(words []layoutWord) float64 {
	height := medianHeight(words)
	if height <= 0 {
		return math.Inf(1)
	}

	return columnGapHeights * height
}

// splitBandAtColumnGaps divides one horizontal band wherever the words leave a
// column-sized hole. Two headings at the same height over two columns are two
// lines, not one line reading "Ingredients Method".
func splitBandAtColumnGaps(band []layoutWord, gap float64) []layoutLine {
	sort.SliceStable(band, func(left, right int) bool { return band[left].minX < band[right].minX })

	var (
		lines   []layoutLine
		current []layoutWord
	)

	for _, word := range band {
		if len(current) > 0 && word.minX-current[len(current)-1].maxX > gap {
			lines = append(lines, makeLayoutLine(current))
			current = nil
		}

		current = append(current, word)
	}

	if len(current) > 0 {
		lines = append(lines, makeLayoutLine(current))
	}

	return lines
}

func makeLayoutLine(words []layoutWord) layoutLine {
	line := layoutLine{
		minX: words[0].minX, minY: words[0].minY,
		maxX: words[0].maxX, maxY: words[0].maxY,
	}

	parts := make([]string, 0, len(words))

	for _, word := range words {
		line.minX = math.Min(line.minX, word.minX)
		line.minY = math.Min(line.minY, word.minY)
		line.maxX = math.Max(line.maxX, word.maxX)
		line.maxY = math.Max(line.maxY, word.maxY)
		parts = append(parts, word.text)
	}

	line.text = strings.Join(parts, " ")
	line.words = words

	return line
}

// readingOrder writes lines out the way a reader takes them: a line spanning
// the page ends the columns above it and stands on its own, and everything
// between two such lines is read one column at a time.
func readingOrder(lines []layoutLine, depth int) []string {
	if depth >= maxColumnDepth || len(lines) < minColumnLines*2 {
		return lineTexts(lines)
	}

	gutter, found := findGutter(lines)
	if !found {
		return lineTexts(lines)
	}

	var (
		out  []string
		band []layoutLine
	)

	for _, line := range lines {
		if line.minX < gutter && line.maxX > gutter {
			out = append(out, readColumns(band, gutter, depth)...)
			band = nil

			out = append(out, line.text)

			continue
		}

		band = append(band, line)
	}

	return append(out, readColumns(band, gutter, depth)...)
}

// readColumns reads the lines on one side of a gutter and then the other. Both
// sides are strictly smaller than what came in, which is what ends the
// recursion for a page with more than two columns.
func readColumns(lines []layoutLine, gutter float64, depth int) []string {
	var left, right []layoutLine

	for _, line := range lines {
		if line.maxX <= gutter {
			left = append(left, line)
		} else {
			right = append(right, line)
		}
	}

	return append(readingOrder(left, depth+1), readingOrder(right, depth+1)...)
}

// findGutter looks for the vertical channel a page was set around. A candidate
// is judged by how few lines cross it and how evenly it divides them; a page
// that was always one column has no candidate that leaves lines on both sides.
func findGutter(lines []layoutLine) (float64, bool) {
	minX, maxX := lines[0].minX, lines[0].maxX
	for _, line := range lines[1:] {
		minX = math.Min(minX, line.minX)
		maxX = math.Max(maxX, line.maxX)
	}

	width := maxX - minX
	if width <= 0 {
		return 0, false
	}

	var (
		best      float64
		bestScore = math.Inf(1)
		found     bool
	)

	for _, line := range lines {
		candidate := line.maxX
		if candidate < minX+columnSearchStart*width || candidate > minX+columnSearchEnd*width {
			continue
		}

		score, ok := scoreGutter(lines, candidate, width)
		if !ok || score >= bestScore {
			continue
		}

		best, bestScore, found = candidate, score, true
	}

	return best, found
}

// scoreGutter reports how good a candidate channel is, lower being better, or
// false when it is not a channel at all.
func scoreGutter(lines []layoutLine, candidate, width float64) (float64, bool) {
	var (
		left, right, crossing int
		leftEdge              = math.Inf(-1)
		rightEdge             = math.Inf(1)
	)

	for _, line := range lines {
		switch {
		case line.maxX <= candidate:
			left++
			leftEdge = math.Max(leftEdge, line.maxX)
		case line.minX >= candidate:
			right++
			rightEdge = math.Min(rightEdge, line.minX)
		default:
			crossing++
		}
	}

	if left < minColumnLines || right < minColumnLines {
		return 0, false
	}

	if float64(crossing) > maxCrossingShare*float64(len(lines)) {
		return 0, false
	}

	if rightEdge-leftEdge < minGutterShare*width {
		return 0, false
	}

	// Crossing lines are what a wrong guess produces, so they dominate; an even
	// division separates the tie between channels that cross nothing.
	balance := math.Abs(float64(left-right)) / float64(len(lines))

	return float64(crossing) + balance, true
}

// lineTexts writes a column out, keeping the breaks the page set between its
// paragraphs. A step further from the line above it than this column's own
// leading is the break a page puts between a heading and the list under it,
// and the plain text interpreter reads those breaks. The measure is the
// column's own leading rather than a fixed distance, because a recipe card and
// a printed page are set at different sizes.
func lineTexts(lines []layoutLine) []string {
	leading := medianLeading(lines)
	texts := make([]string, 0, len(lines))

	for index, line := range lines {
		if index > 0 && leading > 0 && line.minY-lines[index-1].minY > paragraphLeading*leading {
			texts = append(texts, "")
		}

		texts = append(texts, line.text)
	}

	return texts
}

func medianLeading(lines []layoutLine) float64 {
	steps := make([]float64, 0, len(lines))

	for index := 1; index < len(lines); index++ {
		if step := lines[index].minY - lines[index-1].minY; step > 0 {
			steps = append(steps, step)
		}
	}

	if len(steps) == 0 {
		return 0
	}

	sort.Float64s(steps)

	return steps[len(steps)/2]
}

// pdfBBoxDocument is the shape of `pdftotext -bbox-layout`. Poppler's own
// flows and blocks are deliberately ignored: they describe where the text was
// found, and the question here is what order a cook reads it in.
type pdfBBoxDocument struct {
	Pages []struct {
		Flows []struct {
			Blocks []struct {
				Lines []struct {
					Words []struct {
						MinX float64 `xml:"xMin,attr"`
						MinY float64 `xml:"yMin,attr"`
						MaxX float64 `xml:"xMax,attr"`
						MaxY float64 `xml:"yMax,attr"`
						Text string  `xml:",chardata"`
					} `xml:"word"`
				} `xml:"line"`
			} `xml:"block"`
		} `xml:"flow"`
	} `xml:"body>doc>page"`
}

// pdfLayoutPages reads the words of each page out of a bounding-box report.
func pdfLayoutPages(data []byte) ([][]layoutWord, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose
	decoder.Entity = xml.HTMLEntity

	var document pdfBBoxDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("read PDF page geometry: %w", err)
	}

	pages := make([][]layoutWord, 0, len(document.Pages))

	for _, page := range document.Pages {
		var words []layoutWord

		for _, flow := range page.Flows {
			for _, block := range flow.Blocks {
				for _, line := range block.Lines {
					for _, word := range line.Words {
						text := strings.TrimSpace(word.Text)
						if text == "" {
							continue
						}

						words = append(words, layoutWord{
							minX: word.MinX, minY: word.MinY,
							maxX: word.MaxX, maxY: word.MaxY,
							text: text,
						})
					}
				}
			}
		}

		pages = append(pages, words)
	}

	return pages, nil
}

// minimumWordConfidence drops what Tesseract itself does not believe it read.
// Speckle and rule lines come back as one-character words with no confidence,
// and they are worth less than the line breaks they destroy.
const minimumWordConfidence = 30

// tesseractLayoutWords reads the words out of Tesseract's TSV report. Asking
// for word boxes rather than laid-out text is what restores the space between
// an amount and its unit, which the plain output runs together.
func tesseractLayoutWords(data []byte) []layoutWord {
	var words []layoutWord

	for index, row := range strings.Split(string(data), "\n") {
		fields := strings.Split(strings.TrimRight(row, "\r"), "\t")
		if index == 0 || len(fields) < 12 || fields[0] != "5" {
			continue
		}

		text := strings.TrimSpace(strings.Join(fields[11:], "\t"))
		if text == "" {
			continue
		}

		confidence, err := strconv.ParseFloat(fields[10], 64)
		if err != nil || confidence < minimumWordConfidence {
			continue
		}

		numbers, err := parseWordBox(fields[6:10])
		if err != nil {
			continue
		}

		words = append(words, layoutWord{
			minX: numbers[0], minY: numbers[1],
			maxX: numbers[0] + numbers[2], maxY: numbers[1] + numbers[3],
			text: text,
		})
	}

	return words
}

func parseWordBox(fields []string) ([4]float64, error) {
	var numbers [4]float64

	for index, field := range fields {
		value, err := strconv.ParseFloat(field, 64)
		if err != nil {
			return numbers, fmt.Errorf("read word geometry %q: %w", field, err)
		}

		numbers[index] = value
	}

	return numbers, nil
}
