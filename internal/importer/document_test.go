package importer

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageHeaderCannotClaimUnboundedDimensions(t *testing.T) {
	data, err := os.ReadFile("testdata/document-scan.png")
	if err != nil {
		t.Fatal(err)
	}

	for _, width := range []uint32{0, 1<<31 - 1} {
		mutated := append([]byte(nil), data...)
		binary.BigEndian.PutUint32(mutated[16:20], width)
		binary.BigEndian.PutUint32(mutated[29:33], crc32.ChecksumIEEE(mutated[12:29]))

		if err := validateDocumentImage(mutated); err == nil {
			t.Fatalf("accepted image width %d", width)
		}
	}
}

func TestLocalOCRRejectsOversizedPDFBeforeParsing(t *testing.T) {
	data := make([]byte, documentMaxBytes+1)
	copy(data, "%PDF-1.7\n1 0 obj\n")

	if _, err := (localOCR{}).Extract(t.Context(), "large.pdf", data); err == nil {
		t.Fatal("accepted a PDF beyond the input byte limit")
	}
}

func toolPath(t *testing.T, files map[string]string) string {
	t.Helper()

	d := t.TempDir()

	if _, ok := files["pdfinfo"]; !ok {
		pages := 1
		if report, exists := files["pdftotext"]; exists {
			pages = strings.Count(report, "<page ")
		}

		files["pdfinfo"] = fmt.Sprintf("printf 'Pages: %d\\n'", pages)
	}

	for n, s := range files {
		p := filepath.Join(d, n)
		if e := os.WriteFile(p, []byte("#!/bin/sh\n"+s), 0o700); e != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
			t.Fatal(e)
		}
	}

	return d
}

func withTools(t *testing.T, d string) {
	t.Helper()
	t.Setenv("PATH", d+":"+os.Getenv("PATH"))
}

// bboxTool writes the geometry report Poppler produces, so a test can state
// what a PDF holds and where without needing Poppler installed.
func bboxTool(pages ...[]string) string {
	return "printf '" + shellFormat(bboxReport(pages)) + "'"
}

// tsvTool writes the word report Tesseract produces, for the same reason.
func tsvTool(lines ...string) string {
	return "printf '" + shellFormat(tesseractReport(lines)) + "'"
}

// shellFormat escapes a report for a `printf` format string inside single
// quotes, which composes where a here-document cannot.
func shellFormat(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "%", "%%", "'", "'\\''", "\n", "\\n", "\t", "\\t")

	return replacer.Replace(value)
}

func bboxReport(pages [][]string) string {
	var out strings.Builder

	out.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n<html><body><doc>\n")

	for _, lines := range pages {
		out.WriteString(`<page width="612.0" height="792.0">` + "\n<flow><block>\n")

		for index, line := range lines {
			top := 50.0 + 20.0*float64(index)
			fmt.Fprintf(&out, `<line yMin="%.1f" yMax="%.1f">`, top, top+12)

			left := 50.0

			for word := range strings.FieldsSeq(line) {
				width := 6.0 * float64(len([]rune(word)))
				fmt.Fprintf(&out, `<word xMin="%.1f" yMin="%.1f" xMax="%.1f" yMax="%.1f">%s</word>`,
					left, top, left+width, top+12, xmlText(word))

				left += width + 4
			}

			out.WriteString("</line>\n")
		}

		out.WriteString("</block></flow>\n</page>\n")
	}

	out.WriteString("</doc></body></html>\n")

	return out.String()
}

func xmlText(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(value)
}

func tesseractReport(lines []string) string {
	var out strings.Builder

	out.WriteString("level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n")

	for index, line := range lines {
		top := 50 + 20*index

		left := 50

		for position, word := range strings.Fields(line) {
			width := 6 * len([]rune(word))
			fmt.Fprintf(&out, "5\t1\t1\t1\t%d\t%d\t%d\t%d\t%d\t12\t96\t%s\n",
				index+1, position+1, left, top, width, word)

			left += width + 4
		}
	}

	return out.String()
}

func TestLocalOCRTextPDFSkipsOCR(t *testing.T) {
	d := toolPath(t, map[string]string{
		"pdftotext": bboxTool([]string{"Ingredients:", "1 cup flour", "Instructions:", "Mix"}),
		"tesseract": "echo OCR >&2; exit 9", "pdftoppm": "exit 9",
	})
	withTools(t, d)

	got, e := (localOCR{}).Extract(context.Background(), "a.pdf", []byte("%PDF-1.7"))
	if e != nil || !strings.Contains(got, "1 cup flour") {
		t.Fatalf("text=%q err=%v", got, e)
	}
}

func TestLocalOCRScannedPDFRendersPagesInOrder(t *testing.T) {
	d := toolPath(t, map[string]string{
		"pdftotext": bboxTool(nil, nil),
		"pdftoppm":  renderPageTool(t),
		"tesseract": "case \"$1\" in *1.png) " + tsvTool("page-one") + ";; *) " + tsvTool("page-two") + ";; esac",
	})
	withTools(t, d)

	got, e := (localOCR{}).Extract(context.Background(), "a.pdf", []byte("%PDF-1.7"))
	if e != nil || !strings.Contains(got, "page-one") || !strings.Contains(got, "page-two") {
		t.Fatalf("text=%q err=%v", got, e)
	}
}

func TestLocalOCRRejectsLargeImage(t *testing.T) {
	_, e := (localOCR{}).Extract(context.Background(), "a.png", []byte("not image"))
	if e == nil {
		t.Fatal("invalid image accepted")
	}
}

type fakeOCR struct {
	calls []string
	text  string
	err   error
}

func (f *fakeOCR) Extract(_ context.Context, name string, _ []byte) (string, error) {
	f.calls = append(f.calls, name)
	return f.text, f.err
}

func TestDocumentUsesReplaceableExtractorAndCommonParser(t *testing.T) {
	f := &fakeOCR{text: "Toast\n\nIngredients:\n1 cup flour\n\nInstructions:\nMix."}

	b, e := (documentAdapter{OCR: f}).Import(context.Background(), Input{Name: "scan.jpg", Data: []byte("image")}, Options{})
	if e != nil {
		t.Fatal(e)
	}

	if len(f.calls) != 1 || len(b.Recipes) != 1 || len(b.Recipes[0].Document.IngredientSections) != 1 || len(b.Recipes[0].Document.InstructionSections) != 1 {
		t.Fatalf("calls=%v batch=%#v", f.calls, b)
	}

	if len(b.Recipes[0].Diagnostics) != 1 {
		t.Fatal("missing extraction review diagnostic")
	}
}

func TestDocumentPropagatesExtractorFailure(t *testing.T) {
	want := errors.New("OCR unavailable")

	_, e := (documentAdapter{OCR: &fakeOCR{err: want}}).Import(context.Background(), Input{Name: "scan.png", Data: []byte("x")}, Options{})
	if !errors.Is(e, want) {
		t.Fatalf("error=%v", e)
	}
}

func TestLocalOCRTextPDFPageLimit(t *testing.T) {
	for _, count := range []int{8, 9} {
		t.Run(string(rune('0'+count)), func(t *testing.T) { //nolint:gosec // count is 8 or 9, from the loop above
			pages := make([][]string, count)
			for index := range pages {
				pages[index] = []string{"Recipe page"}
			}

			withTools(t, toolPath(t, map[string]string{
				"pdftotext": bboxTool(pages...), "tesseract": "exit 9", "pdftoppm": "exit 9",
			}))

			_, err := (localOCR{}).Extract(context.Background(), "a.pdf", []byte("%PDF-1.7"))
			if (err != nil) != (count > 8) {
				t.Fatalf("%d pages error=%v", count, err)
			}
		})
	}
}

func TestLocalOCRMixedPDFKeepsTextAndExtractsOnlyBlankPages(t *testing.T) {
	withTools(t, toolPath(t, map[string]string{
		"pdftotext": bboxTool([]string{"Recipe title", "Ingredients", "2 eggs"}, nil),
		"pdftoppm":  renderPageTool(t),
		"tesseract": "case \"$1\" in *1.png) exit 9;; *) " + tsvTool("Instructions", "Beat eggs.") + ";; esac",
	}))

	text, err := (localOCR{}).Extract(context.Background(), "mixed.pdf", []byte("%PDF-1.7"))
	if err != nil || !strings.Contains(text, "2 eggs") || !strings.Contains(text, "Beat eggs.") {
		t.Fatalf("text=%q error=%v", text, err)
	}
}

func TestLocalOCRHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := (localOCR{}).Extract(ctx, "a.pdf", []byte("%PDF-1.7"))
	if err == nil {
		t.Fatal("cancelled extraction succeeded")
	}
}

func TestLocalOCRNamesTheToolAHostIsMissing(t *testing.T) {
	// With no tools on PATH, the message names the command, its product, and
	// how to install it.
	cases := []struct {
		name, file, want string
		data             []byte
	}{
		{name: "a PDF names pdftotext", file: "a.pdf", want: "pdftotext", data: []byte("%PDF-1.7")},
		{name: "an image names tesseract", file: "document-scan.png", want: "tesseract"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			data := testCase.data
			if data == nil {
				data = readDocumentFixture(t, testCase.file)
			}

			t.Setenv("PATH", t.TempDir())

			_, err := (localOCR{}).Extract(context.Background(), testCase.file, data)
			if err == nil {
				t.Fatal("extraction succeeded without its tools")
			}

			for _, want := range []string{testCase.want, "not installed on this Savorum host", "brew install"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}
