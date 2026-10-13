package library

import "testing"

func TestSanitizeImportContextRemovesCredentialBearingText(t *testing.T) {
	t.Parallel()

	got := SanitizeImportContext(ImportContext{ //nolint:gosec // the credentials are the fixture: this asserts they are stripped
		DisplayName: "https://example.test/recipe.json?token=abc",
		SourceURL:   "https://alice:password@example.test/recipe?signature=abc#private",
		Diagnostics: []ImportDiagnostic{{Message: "Authorization: Bearer abc; token=abc"}},
	})
	if got.DisplayName != "example.test" || got.SourceURL != "https://example.test" {
		t.Fatalf("safe import identity = %#v", got)
	}

	if got.Diagnostics[0].Message != "Authorization=[redacted]" {
		t.Fatalf("safe diagnostic = %q", got.Diagnostics[0].Message)
	}
}
