package importjob

import (
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
)

func TestResultDiagnosticsDoesNotRepeatAPlannedWarning(t *testing.T) {
	planned := []importer.Diagnostic{{
		Code: "ingredient-review-required", Class: importer.DiagnosticActionable,
		Severity: importer.SeverityWarning, Area: "ingredients",
		Message: "Review one ingredient.", Count: 1,
	}}

	diagnostics := resultDiagnostics(planned, library.ImportResult{
		Warnings: []string{"Review one ingredient.", "A separate detail was preserved."},
	})

	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v, want the planned warning and one new warning", diagnostics)
	}

	if diagnostics[0].Code != "ingredient-review-required" || diagnostics[1].Message != "A separate detail was preserved." {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

// A recipe imported from a link is not told to choose a file again.
func TestAnUnresumableRecipeIsToldWhatItActuallyNeeds(t *testing.T) {
	tests := []struct {
		name string
		item Item
		says string
	}{
		{name: "uploaded file", item: Item{Filename: "export.zip"}, says: "export.zip"},
		{name: "pasted link", item: Item{SourceURL: "https://example.test/pasta"}, says: "https://example.test/pasta"},
		{name: "neither", item: Item{}, says: "from its source"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reason := unresumableReason(test.item)
			if !strings.Contains(reason, test.says) {
				t.Fatalf("reason = %q, want it to name %q", reason, test.says)
			}

			if test.item.Filename == "" && strings.Contains(reason, "file") {
				t.Fatalf("reason names a file this recipe never had: %q", reason)
			}
		})
	}
}
