package importer

import (
	"strings"
	"testing"
)

// place lays a line of words out at a height and a starting position, in the
// units an extraction tool reports. Twelve is a body size, and the widths are
// close enough to a proportional face for the gaps between columns to be the
// only large ones on the page.
func place(top, left float64, words ...string) []layoutWord {
	placed := make([]layoutWord, 0, len(words))

	for _, word := range words {
		width := 6 * float64(len([]rune(word)))
		placed = append(placed, layoutWord{
			minX: left, minY: top, maxX: left + width, maxY: top + 12, text: word,
		})

		left += width + 4
	}

	return placed
}

func TestTwoColumnPageIsReadOneColumnAtATime(t *testing.T) {
	t.Parallel()

	// A headline across the page, then ingredients beside method.
	var words []layoutWord

	words = append(
		words,
		place(20, 50, "Soft", "Bakery", "Chocolate", "Chip", "Cookies", "for", "the", "Weekend")...,
	)
	words = append(words, place(60, 50, "Ingredients")...)
	words = append(words, place(60, 320, "Method")...)
	words = append(words, place(80, 50, "300g", "flour")...)
	words = append(words, place(82, 320, "Whisk", "the", "dry", "ingredients.")...)
	words = append(words, place(100, 50, "15g", "cornstarch")...)
	words = append(words, place(101, 320, "Beat", "the", "butter", "and", "sugar.")...)
	words = append(words, place(120, 50, "1/2", "tsp", "baking", "soda")...)
	words = append(words, place(122, 320, "Fold", "the", "two", "together.")...)

	want := strings.Join([]string{
		"Soft Bakery Chocolate Chip Cookies for the Weekend",
		"Ingredients",
		"300g flour",
		"15g cornstarch",
		"1/2 tsp baking soda",
		"Method",
		"Whisk the dry ingredients.",
		"Beat the butter and sugar.",
		"Fold the two together.",
	}, "\n")

	if got := documentPageText(words); got != want {
		t.Fatalf("page read as:\n%s\n\nwant:\n%s", got, want)
	}
}

func TestSingleColumnPageKeepsItsOrderAndItsBreaks(t *testing.T) {
	t.Parallel()

	// The last line sits two steps below the one before it, which is the break
	// a page puts between a list and what follows it. The plain text
	// interpreter reads that break, so it has to survive.
	var words []layoutWord
	for index, line := range []string{
		"Cinnamon toast",
		"2 apples, sliced thinly",
		"1 tablespoon maple syrup",
	} {
		words = append(words, place(20+float64(index)*20, 50, strings.Fields(line)...)...)
	}

	words = append(words, place(100, 50, strings.Fields("Toast the bread until golden.")...)...)

	want := "Cinnamon toast\n2 apples, sliced thinly\n1 tablespoon maple syrup\n\nToast the bread until golden."
	if got := documentPageText(words); got != want {
		t.Fatalf("page read as:\n%s\n\nwant:\n%s", got, want)
	}
}

func TestHeadingsOverTwoColumnsAreTwoLines(t *testing.T) {
	t.Parallel()

	// Set at the same height, and so one line if lines were only a matter of
	// height. A gap this wide is a column boundary, not a word space.
	lines := groupLayoutLines(append(place(60, 50, "Ingredients"), place(60, 320, "Method")...))
	if len(lines) != 2 || lines[0].text != "Ingredients" || lines[1].text != "Method" {
		t.Fatalf("lines = %#v", lines)
	}
}

func TestTesseractReportKeepsWordsItBelieves(t *testing.T) {
	t.Parallel()

	report := strings.Join([]string{
		"level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext",
		"4\t1\t1\t1\t1\t0\t0\t0\t100\t20\t-1\t",
		"5\t1\t1\t1\t1\t1\t50\t20\t30\t12\t96\t300g",
		"5\t1\t1\t1\t1\t2\t90\t20\t28\t12\t94\tflour",
		// Speckle Tesseract itself does not believe, which is worth less than
		// the line it would otherwise join.
		"5\t1\t1\t1\t1\t3\t130\t20\t4\t12\t3\t~n",
		"5\t1\t1\t1\t1\t4\t140\t20\t4\t12\t0\t",
	}, "\n")

	words := tesseractLayoutWords([]byte(report))
	if len(words) != 2 || words[0].text != "300g" || words[1].maxX != 118 {
		t.Fatalf("words = %#v", words)
	}
}

func TestPDFGeometryIsReadPageByPage(t *testing.T) {
	t.Parallel()

	report := `<html><body><doc>
<page width="612.0" height="792.0"><flow><block>
<line><word xMin="50.0" yMin="20.0" xMax="80.0" yMax="32.0">Toast</word></line>
</block></flow></page>
<page width="612.0" height="792.0"><flow><block>
<line><word xMin="50.0" yMin="20.0" xMax="90.0" yMax="32.0">Apples</word></line>
</block></flow></page>
</doc></body></html>`

	pages, err := pdfLayoutPages([]byte(report))
	if err != nil {
		t.Fatal(err)
	}

	if len(pages) != 2 || documentPageText(pages[0]) != "Toast" || documentPageText(pages[1]) != "Apples" {
		t.Fatalf("pages = %#v", pages)
	}
}
