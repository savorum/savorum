package importer

// Bounded local extraction for recipe PDFs and images. Providers are process
// local and replaceable; no network or cloud service is part of this path.
import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/imaging"
)

const (
	documentMaxBytes = 32 << 20
	maxDocumentPages = 8
	// maxDocumentTextBytes bounds the recipe text extraction may produce.
	maxDocumentTextBytes = 8 << 20
	// maxDocumentReportBytes bounds the geometry report a tool writes, which
	// carries a coordinate for every word and so is far larger than the text.
	maxDocumentReportBytes = 64 << 20
)

// documentTool names a local command extraction depends on, and how a host
// gets it. A host without one is told what to install rather than handed the
// exec package's report that a PATH lookup failed.
type documentTool struct {
	command string
	product string
	install string
	purpose string
}

var documentTools = []documentTool{
	{
		command: "pdfinfo", product: "Poppler",
		install: "brew install poppler, or apt install poppler-utils",
		purpose: "check the page count of a PDF",
	},
	{
		command: "pdftotext", product: "Poppler",
		install: "brew install poppler, or apt install poppler-utils",
		purpose: "read the text in a PDF",
	},
	{
		command: "pdftoppm", product: "Poppler",
		install: "brew install poppler, or apt install poppler-utils",
		purpose: "turn the pages of a scanned PDF into images",
	},
	{
		command: "tesseract", product: "Tesseract OCR",
		install: "brew install tesseract, or apt install tesseract-ocr",
		purpose: "recognise the text in an image",
	},
}

// requireDocumentTools reports the first named command this host does not
// have, as a sentence naming what to install. Extraction is deliberately
// local, with no service to fall back on.
func requireDocumentTools(commands ...string) error {
	for _, command := range commands {
		if _, err := exec.LookPath(command); err == nil {
			continue
		}

		for _, tool := range documentTools {
			if tool.command != command {
				continue
			}

			return fmt.Errorf(
				"%s needs the %s command from %s to %s, and it is not installed on this Savorum host: install it with %s, then import the file again",
				"local extraction", tool.command, tool.product, tool.purpose, tool.install,
			)
		}

		return fmt.Errorf("local extraction needs the %s command, and it is not installed on this Savorum host", command)
	}

	return nil
}

// OCRProvider extracts text from a local document path.
type OCRProvider interface {
	Extract(context.Context, string, []byte) (string, error)
}

type cappedBuffer struct {
	b   bytes.Buffer
	max int
}

func (w *cappedBuffer) Write(p []byte) (int, error) {
	if len(p) > w.max-w.b.Len() {
		return 0, errDocumentOutputLimit
	}

	written, err := w.b.Write(p)
	if err != nil {
		return written, fmt.Errorf("buffer extracted text: %w", err)
	}

	return written, nil
}

type localOCR struct{}

func (localOCR) Extract(ctx context.Context, name string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if len(data) > documentMaxBytes {
		return "", fmt.Errorf("document exceeds limit")
	}

	release, err := acquireDocumentWork(ctx)
	if err != nil {
		return "", err
	}
	defer release()

	dir, err := os.MkdirTemp("", "savorum-extract-")
	if err != nil {
		return "", fmt.Errorf("create document extraction workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	isPDF := strings.HasSuffix(strings.ToLower(name), ".pdf") || bytes.HasPrefix(data, []byte("%PDF-"))
	if !isPDF {
		if data, name, err = prepareDocumentImage(data, name); err != nil {
			return "", err
		}
	}

	in := filepath.Join(dir, "input"+filepath.Ext(name))
	if err := os.WriteFile(in, data, 0o600); err != nil {
		return "", fmt.Errorf("stage document for extraction: %w", err)
	}

	// Text PDFs need only Poppler. The OCR pair is checked for blank pages.
	required := "tesseract"
	if isPDF {
		required = "pdftotext"
	}

	if err := requireDocumentTools(required); err != nil {
		return "", err
	}

	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	if !isPDF {
		return extractImageText(cctx, in)
	}

	pages, err := extractPDFPageText(cctx, in)
	if err != nil {
		return "", err
	}

	if len(pages) > maxDocumentPages {
		return "", fmt.Errorf("PDF exceeds eight page limit")
	}

	if hasBlankPage(pages) {
		if pages, err = replaceBlankPDFPages(cctx, dir, in, pages); err != nil {
			return "", err
		}
	}

	if err := cctx.Err(); err != nil {
		return "", err
	}

	return joinDocumentPages(pages)
}

// documentImageLimits bounds an image accepted for text recognition.
var documentImageLimits = imaging.Limits{Width: 32768, Height: 32768, Pixels: 40_000_000}

// maxDocumentImagePixels is the largest image Tesseract is given. Its memory
// grows with the pixel count: a 12-megapixel phone photo (4032 by 3024) stays
// well inside the tool's address-space limit, while a 24-megapixel one exceeds it.
const maxDocumentImagePixels = 12_600_000

func validateDocumentImage(data []byte) error {
	if _, _, err := imaging.Measure(data, documentImageLimits); err != nil {
		return fmt.Errorf("unsupported or invalid image: %w", err)
	}

	return nil
}

// prepareDocumentImage scales an image larger than maxDocumentImagePixels down
// to that size, as a grayscale PNG with its orientation applied, so recognition
// fits its memory limit. Smaller images are returned unchanged.
func prepareDocumentImage(data []byte, name string) ([]byte, string, error) {
	config, _, err := imaging.Measure(data, documentImageLimits)
	if err != nil {
		return nil, "", fmt.Errorf("unsupported or invalid image: %w", err)
	}

	if config.Width*config.Height <= maxDocumentImagePixels {
		return data, name, nil
	}

	decoded, err := imaging.Decode(data, documentImageLimits)
	if err != nil {
		return nil, "", fmt.Errorf("unsupported or invalid image: %w", err)
	}

	width, height := decoded.Size()
	scale := math.Sqrt(float64(maxDocumentImagePixels) / float64(width*height))
	reduced := imaging.Reduce(decoded, image.Rect(0, 0, width, height), max(1, int(float64(width)*scale)), max(1, int(float64(height)*scale)))

	gray := image.NewGray(reduced.Bounds())
	draw.Draw(gray, gray.Bounds(), reduced, reduced.Bounds().Min, draw.Src)

	var out bytes.Buffer

	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&out, gray); err != nil {
		return nil, "", fmt.Errorf("scale image for text recognition: %w", err)
	}

	// Hand the decoded pixels back to the system before Tesseract starts, so
	// the two never hold their peak memory at once on a small host.
	debug.FreeOSMemory()

	return out.Bytes(), "scaled.png", nil
}

// joinDocumentPages writes the pages out under the text budget, refusing
// rather than truncating a document that exceeds it.
func joinDocumentPages(pages []string) (string, error) {
	out := cappedBuffer{max: maxDocumentTextBytes}

	for index, page := range pages {
		if index > 0 {
			if _, err := out.Write([]byte("\n")); err != nil {
				return "", err
			}
		}

		if _, err := out.Write([]byte(page)); err != nil {
			return "", err
		}
	}

	return out.b.String(), nil
}

// extractImageText recognises a raster image. Tesseract is asked for word
// boxes so the page can be rebuilt in reading order.
func extractImageText(ctx context.Context, inputPath string) (string, error) {
	out := cappedBuffer{max: maxDocumentReportBytes}
	if err := runDocumentTool(ctx, &out, "tesseract", inputPath, "stdout", "--dpi", "200", "tsv"); err != nil {
		return "", err
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	return documentPageText(tesseractLayoutWords(out.b.Bytes())), nil
}

// extractPDFPageText reads each page of a PDF in reading order, from Poppler's
// word geometry rather than its -layout transcript, which interleaves columns.
func extractPDFPageText(ctx context.Context, inputPath string) ([]string, error) {
	count, err := countPDFPages(ctx, inputPath)
	if err != nil {
		return nil, err
	}

	out := cappedBuffer{max: maxDocumentReportBytes}
	if err := runDocumentTool(ctx, &out, "pdftotext", "-f", "1", "-l", strconv.Itoa(count), "-bbox-layout", inputPath, "-"); err != nil {
		return nil, err
	}

	pages, err := pdfLayoutPages(out.b.Bytes())
	if err != nil {
		return nil, err
	}

	if len(pages) != count {
		return nil, fmt.Errorf("PDF page geometry does not match page count")
	}

	texts := make([]string, 0, len(pages))
	for _, words := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		texts = append(texts, documentPageText(words))
	}

	return texts, nil
}

func countPDFPages(ctx context.Context, inputPath string) (int, error) {
	if err := requireDocumentTools("pdfinfo"); err != nil {
		return 0, err
	}

	out := cappedBuffer{max: 64 << 10}
	if err := runDocumentTool(ctx, &out, "pdfinfo", inputPath); err != nil {
		return 0, err
	}

	count := 0

	for line := range strings.SplitSeq(out.b.String(), "\n") {
		if !strings.HasPrefix(line, "Pages:") {
			continue
		}

		if count != 0 {
			return 0, fmt.Errorf("invalid PDF page count")
		}

		value, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Pages:")))
		if err != nil || value < 1 {
			return 0, fmt.Errorf("invalid PDF page count")
		}

		if value > maxDocumentPages {
			return 0, fmt.Errorf("PDF exceeds eight page limit")
		}

		count = value
	}

	if count == 0 {
		return 0, fmt.Errorf("missing PDF page count")
	}

	return count, nil
}

func hasBlankPage(pages []string) bool {
	for _, page := range pages {
		if strings.TrimSpace(page) == "" {
			return true
		}
	}

	return false
}

// replaceBlankPDFPages recognises the pages that carry no text layer, leaving
// the pages that do exactly as Poppler read them.
func replaceBlankPDFPages(ctx context.Context, dir, inputPath string, textPages []string) ([]string, error) {
	if err := requireDocumentTools("pdftoppm", "tesseract"); err != nil {
		return nil, err
	}

	result := append([]string(nil), textPages...)
	for index, text := range textPages {
		if strings.TrimSpace(text) != "" {
			continue
		}

		page := strconv.Itoa(index + 1)

		out := cappedBuffer{max: 8 << 20}
		if err := runDocumentTool(ctx, &out, "pdftoppm", "-f", page, "-l", page, "-singlefile", "-png", "-scale-to", "2048", inputPath); err != nil {
			return nil, fmt.Errorf("render scanned PDF page %s: %w", page, err)
		}

		if err := validateDocumentImage(out.b.Bytes()); err != nil {
			return nil, fmt.Errorf("rendered PDF page %s: %w", page, err)
		}

		imagePath := filepath.Join(dir, "page-"+page+".png")
		if err := os.WriteFile(imagePath, out.b.Bytes(), 0o600); err != nil {
			return nil, fmt.Errorf("stage rendered PDF page %s: %w", page, err)
		}

		text, err := recognizePDFPage(ctx, imagePath, index)
		if err != nil {
			return nil, err
		}

		result[index] = text
	}

	return result, nil
}

func recognizePDFPage(ctx context.Context, page string, index int) (string, error) {
	out := cappedBuffer{max: maxDocumentReportBytes}
	if err := runDocumentTool(ctx, &out, "tesseract", page, "stdout", "--dpi", "150", "tsv"); err != nil {
		return "", fmt.Errorf("scanned PDF page %d: %w", index+1, err)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	return documentPageText(tesseractLayoutWords(out.b.Bytes())), nil
}

type documentAdapter struct{ OCR OCRProvider }

func (documentAdapter) Format() Format {
	return Format{InputKinds: []string{"file"}, MediaTypes: []string{"application/pdf", "image/png", "image/jpeg"}, ID: "document", Name: "PDF or image", Version: "1", Extensions: []string{".pdf", ".png", ".jpg", ".jpeg"}, Capabilities: Capabilities{Images: true, SourceIdentity: true}}
}

func (documentAdapter) Probe(_ context.Context, in Input) ProbeResult {
	n := strings.ToLower(in.Name)
	if bytes.HasPrefix(in.Data, []byte("%PDF-")) {
		return ProbeResult{Confidence: ConfidenceContent, Reason: "PDF signature"}
	}

	for _, x := range []string{".png", ".jpg", ".jpeg"} {
		if strings.HasSuffix(n, x) {
			return ProbeResult{Confidence: ConfidenceExtension, Reason: "image extension"}
		}
	}

	return ProbeResult{}
}

func (a documentAdapter) Import(ctx context.Context, in Input, _ Options) (ImportBatch, error) {
	p := a.OCR
	if p == nil {
		p = localOCR{}
	}

	text, e := p.Extract(ctx, in.Name, in.Data)
	if e != nil {
		return ImportBatch{}, e
	}

	b, e := plainTextAdapter{}.Import(ctx, Input{Name: in.Name, Data: []byte(text)}, Options{})
	if e != nil {
		return b, e
	}

	b.Source.Format = a.Format()

	b.Source.InputName = in.Name
	for i := range b.Recipes {
		b.Recipes[i].Diagnostics = append(b.Recipes[i].Diagnostics, Diagnostic{Code: "extracted_text", Class: DiagnosticInformational, Severity: SeverityInfo, Message: "Recipe text was extracted locally; review OCR and parsing results."})
	}

	return b, nil
}
