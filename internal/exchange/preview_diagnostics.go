package exchange

import (
	"errors"
	"strings"

	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func previewDiagnostic(diagnostic importer.Diagnostic) ImportDiagnostic {
	severity := ImportSeverity(diagnostic.Severity)

	area := diagnostic.Area
	if area == "" {
		area = "content"
	}

	switch diagnostic.Code {
	case "multiple-recipes", "microdata-fallback", "recipe-card-only":
		area = "source"

		if severity == "" {
			severity = SeverityInfo
		}
	case "ingredients-enriched", "instructions-enriched", "ingredient-headings-recovered":
		area = "normalization"

		if severity == "" {
			severity = SeverityInfo
		}
	}

	if severity == "" {
		severity = SeverityWarning
	}

	return ImportDiagnostic{
		Code: diagnostic.Code, Class: diagnostic.EffectiveClass(), Severity: severity,
		Area: area, Message: diagnostic.Message, Count: diagnostic.Occurrences(), Targets: diagnostic.Targets,
	}
}

func diagnosticMessages(diagnostics []importer.Diagnostic) []string {
	messages := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		messages = append(messages, diagnostic.Message)
	}

	return messages
}

// hasWarning reports whether anything about this candidate is still the
// reviewer's to decide. Info is not: it says what the import did, not what
// somebody has to do about it.
func hasWarning(diagnostics []ImportDiagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Class == importer.DiagnosticActionable {
			return true
		}
	}

	return false
}

// withPendingAssets returns the document as the commit would see it, with a
// stand-in for every photograph the import has yet to attach.
//
// A candidate under review may refer to photographs fetched only at commit, so
// validation asks whether it could be stored if they all arrived. The
// stand-ins never leave this function.
func withPendingAssets(document recipe.Document, item ImportItem) recipe.Document {
	// Match identifiers and paths as write validation does: asset paths are
	// unique without regard to case.
	present := make(map[string]struct{}, 2*len(document.Assets))
	for _, asset := range document.Assets {
		present[asset.ID] = struct{}{}
		present[strings.ToLower(asset.Path)] = struct{}{}
	}

	pending := append([]recipe.Asset(nil), document.Assets...)

	add := func(id, mediaType string, role recipe.AssetRole) {
		if id == "" {
			return
		}

		path := "assets/" + id
		if _, already := present[id]; already {
			return
		}

		if _, already := present[strings.ToLower(path)]; already {
			return
		}

		present[id] = struct{}{}
		present[strings.ToLower(path)] = struct{}{}
		pending = append(pending, recipe.Asset{
			ID: id, Path: path, MediaType: mediaType, Role: role,
			SHA256: strings.Repeat("0", 64), Size: 1,
		})
	}

	for _, remote := range item.RemoteAssets {
		mediaType := "application/octet-stream"
		if remote.RecipeImage || remote.Role == recipe.AssetRoleImage || remote.Role == "" {
			mediaType = "image/jpeg"
		}

		add(remote.LogicalID, mediaType, remote.Role)
	}

	for _, embedded := range item.EmbeddedAssets {
		add(importer.EmbeddedAssetID(embedded), embedded.MediaType, recipe.AssetRoleImage)
	}

	if len(pending) == len(document.Assets) {
		return document
	}

	document.Assets = pending

	return document
}

// unwritableDiagnostics reports, at review time, a candidate the library would
// refuse at commit time.
//
// It applies the write path's validation so the row is kept out of the
// selection. It is a backstop: an adapter producing an unwritable document is
// a bug in that adapter.
func unwritableDiagnostics(document recipe.Document, item ImportItem) []ImportDiagnostic {
	err := recipe.ValidateForWrite(withPendingAssets(document, item), units.DefaultCatalog())
	if err == nil {
		return nil
	}

	var validation *recipe.ValidationError
	if !errors.As(err, &validation) {
		return []ImportDiagnostic{{
			Code: "candidate-invalid", Class: importer.DiagnosticBlocking, Severity: SeverityError,
			Area: "content", Count: 1, Message: err.Error(),
		}}
	}

	diagnostics := make([]ImportDiagnostic, 0, len(validation.Violations))
	for _, violation := range validation.Violations {
		diagnostics = append(diagnostics, ImportDiagnostic{
			Code: "candidate-invalid", Class: importer.DiagnosticBlocking, Severity: SeverityError,
			Area: "content", Count: 1,
			Message: "This recipe cannot be stored as it stands: " + violation.Message + " (" + violation.Path + ").",
			Targets: []importer.DiagnosticTarget{{Field: violation.Path, Evidence: violation.Message}},
		})
	}

	return diagnostics
}

func hasError(diagnostics []ImportDiagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == SeverityError {
			return true
		}
	}

	return false
}
