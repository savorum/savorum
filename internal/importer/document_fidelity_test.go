package importer

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/imaging"
)

var documentFidelityFixtures = "testdata"

func skipWithoutDocumentTools(t *testing.T, tools ...string) {
	t.Helper()

	missing := make([]string, 0)

	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}

	if len(missing) == 0 {
		return
	}

	message := "document fidelity tools unavailable: " + strings.Join(missing, ", ")
	if os.Getenv("SAVORUM_DOCUMENT_TOOLS_REQUIRED") == "1" {
		t.Fatal(message)
	}

	t.Skip(message)
}

func readDocumentFixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(documentFidelityFixtures, name))
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func assertRecipeText(t *testing.T, extracted string) {
	t.Helper()
	// These lines are deliberately large, plain, and unambiguous in the
	// fixtures. Keeping the expected text exact catches regressions in the
	// command invocation, page rendering, and OCR language defaults.
	for _, line := range []string{
		"CINNAMON APPLE TOAST",
		"INGREDIENTS",
		"2 APPLES",
		"1 TABLESPOON MAPLE SYRUP",
		"INSTRUCTIONS",
		"SLICE THE APPLES.",
		"TOAST THE BREAD UNTIL GOLDEN.",
	} {
		if !strings.Contains(strings.ToUpper(extracted), line) {
			t.Fatalf("extracted text %q does not contain %q", extracted, line)
		}
	}
}

func TestLocalOCRTextPDFFidelity(t *testing.T) {
	skipWithoutDocumentTools(t, "pdfinfo", "pdftotext")

	text, err := (localOCR{}).Extract(context.Background(), "document-text.pdf", readDocumentFixture(t, "document-text.pdf"))
	if err != nil {
		t.Fatal(err)
	}

	assertRecipeText(t, text)
}

func TestLocalOCRImageFidelity(t *testing.T) {
	skipWithoutDocumentTools(t, "tesseract")

	text, err := (localOCR{}).Extract(context.Background(), "document-scan.png", readDocumentFixture(t, "document-scan.png"))
	if err != nil {
		t.Fatal(err)
	}

	assertRecipeText(t, text)
}

// A 24-megapixel phone photo would exceed Tesseract's memory limit unless it
// is scaled down first.
func TestLocalOCRImageFidelityLargePhoto(t *testing.T) {
	skipWithoutDocumentTools(t, "tesseract")

	decoded, err := imaging.Decode(readDocumentFixture(t, "document-scan.png"), imaging.Limits{})
	if err != nil {
		t.Fatal(err)
	}

	var photo bytes.Buffer
	if err := jpeg.Encode(&photo, imaging.Resample(decoded.Image, 4000, 6000), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}

	text, err := (localOCR{}).Extract(context.Background(), "photo.jpg", photo.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	assertRecipeText(t, text)
}

func TestPrepareDocumentImageScalesOnlyLargeImages(t *testing.T) {
	encode := func(width, height int) []byte {
		var out bytes.Buffer
		if err := jpeg.Encode(&out, image.NewGray(image.Rect(0, 0, width, height)), nil); err != nil {
			t.Fatal(err)
		}

		return out.Bytes()
	}

	small := encode(3024, 4032)
	if data, name, err := prepareDocumentImage(small, "small.jpg"); err != nil || name != "small.jpg" || !bytes.Equal(data, small) {
		t.Fatalf("changed a 12-megapixel image: %s, %v", name, err)
	}

	data, name, err := prepareDocumentImage(encode(4000, 6000), "large.jpg")
	if err != nil || name != "scaled.png" {
		t.Fatalf("did not scale a 24-megapixel image: %s, %v", name, err)
	}

	config, _, err := imaging.Measure(data, imaging.Limits{})
	if err != nil {
		t.Fatal(err)
	}

	if pixels := config.Width * config.Height; pixels > maxDocumentImagePixels || pixels < maxDocumentImagePixels*99/100 || abs(config.Height*2-config.Width*3) > 3 {
		t.Fatalf("scaled to %dx%d", config.Width, config.Height)
	}
}

func TestLocalOCRScannedPDFFidelity(t *testing.T) {
	skipWithoutDocumentTools(t, "pdfinfo", "pdftotext", "pdftoppm", "tesseract")

	text, err := (localOCR{}).Extract(context.Background(), "document-scanned.pdf", readDocumentFixture(t, "document-scanned.pdf"))
	if err != nil {
		t.Fatal(err)
	}

	assertRecipeText(t, text)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}

	return n
}
