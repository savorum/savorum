package exchange

import (
	"runtime/debug"
	"strings"

	"github.com/savorum/savorum/internal/buildinfo"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
)

func historyForImport(batch importer.ImportBatch, imported importer.ImportedRecipe) library.ImportContext {
	result := library.ImportContext{
		Importer: batch.Source.Format.ID, ImporterVersion: importerImplementationVersion(),
		InputKind: batch.Source.InputKind, DisplayName: batch.Source.InputName, SourceID: imported.SourceKey,
	}
	if imported.Document.Source != nil {
		result.SourceURL = imported.Document.Source.URL
	}

	for _, diagnostics := range [][]importer.Diagnostic{batch.Diagnostics, imported.Diagnostics} {
		for _, d := range diagnostics {
			result.Diagnostics = append(result.Diagnostics, library.ImportDiagnostic{Code: d.Code, Class: string(d.EffectiveClass()), Area: d.Area, Message: d.Message, Count: d.Occurrences()})
		}
	}

	return library.SanitizeImportContext(result)
}

// importerImplementationVersion names the Savorum adapter implementation that
// interpreted the source. The source format's own version is a compatibility
// fact, not an implementation build identity.
func importerImplementationVersion() string {
	version := buildinfo.CurrentVersion()
	revision := strings.TrimSpace(buildinfo.Revision)
	dirty := false

	if revision == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" {
					revision = strings.TrimSpace(setting.Value)
				}

				if setting.Key == "vcs.modified" && setting.Value == "true" {
					dirty = true
				}
			}
		}
	}

	if revision != "" {
		if dirty {
			return version + "+" + revision + "+dirty"
		}

		return version + "+" + revision
	}

	if version == "development" {
		return "development-unknown"
	}

	return version
}
