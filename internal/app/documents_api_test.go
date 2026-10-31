package app_test

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// requireDocumentTools skips a test on a host without the local PDF tools.
func requireDocumentTools(t *testing.T) {
	t.Helper()

	for _, tool := range []string{"pdfinfo", "pdftotext", "pdftoppm", "tesseract"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("SAVORUM_DOCUMENT_TOOLS_REQUIRED") == "1" {
				t.Fatalf("%s is not installed", tool)
			}

			t.Skipf("%s is not installed", tool)
		}
	}
}

// textPDF builds a one-page PDF whose text layer holds the given lines.
func textPDF(lines []string) []byte {
	var content bytes.Buffer

	content.WriteString("BT /F1 12 Tf 14 TL 72 720 Td\n")

	for _, line := range lines {
		escaped := strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`).Replace(line)
		fmt.Fprintf(&content, "(%s) Tj T*\n", escaped)
	}

	content.WriteString("ET\n")

	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", content.Len(), content.String()),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}

	var document bytes.Buffer

	document.WriteString("%PDF-1.4\n")

	offsets := make([]int, len(objects))

	for index, object := range objects {
		offsets[index] = document.Len()

		fmt.Fprintf(&document, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}

	table := document.Len()

	fmt.Fprintf(&document, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)

	for _, offset := range offsets {
		fmt.Fprintf(&document, "%010d 00000 n \n", offset)
	}

	fmt.Fprintf(&document, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, table)

	return document.Bytes()
}

func TestAPDFRecipeIsPreviewedEditedRetainedAndItsSourceRead(t *testing.T) {
	requireDocumentTools(t)

	h := newHarness(t)

	pdf := textPDF([]string{"Tomato Soup", "", "Ingredients", "- 2 cups tomatoes", "- 1 pint water", "", "Instructions", "1. Simmer the tomatoes in the water.", "2. Blend until smooth."})
	preview := decode[previewItems](t, h.upload("soup.pdf", pdf))
	path := "/api/v1/recipes/import-previews/" + preview.ID
	item := preview.Items[0].ID

	h.send(http.MethodPost, path+"/items/"+item+"/text", map[string]any{"text": recipeText}, http.StatusOK)

	committed := decode[struct {
		Items []struct {
			EntryID string `json:"library_entry_id"`
		} `json:"items"`
	}](t, h.send(http.MethodPost, path+"/commit", map[string]any{"item_ids": []string{item}, "retain_source_item_ids": []string{item}}, http.StatusOK))

	recipePath := "/api/v1/recipes/" + committed.Items[0].EntryID

	sources := h.send(http.MethodGet, recipePath+"/sources", nil, http.StatusOK)
	source := decode[struct {
		Items []struct {
			AssetID string `json:"asset_id"`
		} `json:"items"`
	}](t, sources).Items[0].AssetID

	h.send(http.MethodGet, recipePath+"/sources/"+source, nil, http.StatusOK)
	h.send(http.MethodGet, recipePath+"/revisions/1/sources", nil, http.StatusOK)
	h.send(http.MethodGet, recipePath+"/revisions/1/sources/"+source, nil, http.StatusOK)
}
