package importer

import (
	"context"
	"slices"
	"testing"
)

type availableOCR struct{}

func (availableOCR) Extract(context.Context, string, []byte) (string, error) { return "", nil }

func TestDocumentRuntimeAvailability(t *testing.T) {
	for _, tc := range []struct {
		name       string
		tools      []string
		extensions []string
	}{
		{"none", nil, nil},
		{"no shell", []string{"pdfinfo", "pdftotext", "tesseract"}, nil},
		{"pdf", []string{"/bin/sh", "pdfinfo", "pdftotext"}, []string{".pdf"}},
		{"images", []string{"/bin/sh", "tesseract"}, []string{".png", ".jpg", ".jpeg"}},
		{"all", []string{"/bin/sh", "pdfinfo", "pdftotext", "pdftoppm", "tesseract"}, []string{".pdf", ".png", ".jpg", ".jpeg"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			format, ok := (documentAdapter{}).formatForTools(func(tool string) bool { return slices.Contains(tc.tools, tool) })
			if ok != (len(tc.extensions) > 0) || !slices.Equal(format.Extensions, tc.extensions) {
				t.Fatal(format, ok)
			}
		})
	}

	registry := NewRegistry(plainTextAdapter{}, documentAdapter{OCR: availableOCR{}})
	if len(registry.AvailableFormats()) != 2 || len(registry.Formats()) != 2 {
		t.Fatal("injected provider hidden")
	}

	t.Setenv("PATH", t.TempDir())

	unavailable := NewRegistry(plainTextAdapter{}, documentAdapter{})
	if len(unavailable.AvailableFormats()) != 1 || len(unavailable.Formats()) != 2 {
		t.Fatal("registration or unrelated format changed")
	}
}
