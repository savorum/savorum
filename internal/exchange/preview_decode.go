package exchange

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/imaging"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/importer/webrecipe"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/textutil"
)

// decodeFailureMessage says what went wrong with a chosen file, in the terms of
// the person who chose it, without the Go error chain's prefixes and codes.
func (s *Service) decodeFailureMessage(err error) string {
	var failure *webrecipe.Failure
	if errors.As(err, &failure) && strings.TrimSpace(failure.Message) != "" {
		return textutil.Capitalize(strings.TrimSpace(failure.Message))
	}

	if errors.Is(err, importer.ErrUnsupportedFormat) {
		// Naming what Savorum does read is the part a person can act on.
		if formats := s.readableFormats(); formats != "" {
			return "Savorum could not tell what kind of recipe file this is. It reads " + formats + "."
		}

		return "Savorum could not tell what kind of recipe file this is."
	}

	return textutil.Capitalize(strings.TrimSpace(err.Error()))
}

func (s *Service) readableFormats() string {
	names := make([]string, 0, len(s.registry.Formats()))

	seen := map[string]bool{}
	for _, format := range s.registry.Formats() {
		if format.Name != "" && !seen[format.Name] {
			seen[format.Name] = true
			names = append(names, format.Name)
		}
	}

	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

func (s *Service) decodeUpload(ctx context.Context, upload UploadedImport, options importer.Options) ([]decodedCandidate, error) {
	filename := filepath.Base(upload.Filename)

	batch, err := s.decodeSource(ctx, importer.Input{Name: filename, Data: upload.Data}, options)
	if err != nil {
		return nil, err
	}

	items := candidatesFromBatch(batch, filename)
	for i := range items {
		if batch.Source.Format.ID == "document" {
			items[i].sourceBytes = append([]byte(nil), upload.Data...)
			items[i].sourceFilename = filename
			items[i].sourceMediaType = sourceMediaType(filename, upload.Data)
		}
	}

	return items, nil
}

func candidatesFromBatch(batch importer.ImportBatch, filename string) []decodedCandidate {
	result := make([]decodedCandidate, 0, len(batch.Recipes))
	for _, imported := range batch.Recipes {
		document := imported.Document

		source := ImportPreviewSource{
			FormatID: batch.Source.Format.ID, FormatName: batch.Source.Format.Name,
			Filename: filename,
		}
		if document.Source != nil {
			source.URL = document.Source.URL
			source.Attribution = document.Source.Attribution
		}

		if extracted, ok := imported.ImporterMetadata["source_text"].(string); ok && strings.TrimSpace(extracted) != "" {
			source.ExtractedText = extracted
		}

		result = append(result, decodedCandidate{
			document: document,
			item: ImportItem{
				Document: &document, RemoteAssets: imported.Assets,
				History:        historyForImport(batch, imported),
				EmbeddedAssets: imported.EmbeddedAssets, PortableAssetReferences: imported.PortableAssetReferences,
			},
			source:           source,
			metadata:         imported.LibraryMetadata,
			importerMetadata: imported.ImporterMetadata,
			diagnostics:      imported.Diagnostics,
			batchDiagnostics: batch.Diagnostics,
		})
	}

	return result
}

func sourceMediaType(filename string, data []byte) string {
	if mediaType := mime.TypeByExtension(filepath.Ext(filename)); mediaType != "" {
		return mediaType
	}

	if mediaType := strings.TrimSpace(imaging.Detect(data)); mediaType != "" && mediaType != "application/octet-stream" {
		return mediaType
	}

	return "application/octet-stream"
}

func (s *Service) createPreview(ctx context.Context, decoded []decodedCandidate, reservation *previewReservation) (ImportPreview, error) {
	if len(decoded) == 0 {
		return ImportPreview{}, fmt.Errorf("%w: no recipes were found", ErrPreviewState)
	}

	previewID, err := randomPreviewID()
	if err != nil {
		return ImportPreview{}, err
	}

	now := time.Now().UTC()
	expiresAt := now.Add(s.previewLimits.Lifetime)
	session := &previewSession{expiresAt: expiresAt, candidates: map[string]*previewCandidate{}, stagedRemote: map[string]stagedRemoteAsset{}, scope: s.previewScope(ctx), revision: 1, resolutionPlans: map[string]*storedResolutionPlan{}, pendingDecisions: map[string]*pendingIngredientDecisions{}}
	preview := ImportPreview{ID: previewID, ExpiresAt: expiresAt, Revision: 1}

	analyses := make([]library.ImportAnalysis, len(decoded))
	for index := range analyses {
		analyses[index] = library.ImportAnalysis{Status: library.ImportDefinitelyNew, Matches: []library.ImportMatch{}}
	}

	if analyzer, ok := s.library.(interface {
		AnalyzeImportCandidates(context.Context, []recipe.Document) ([]library.ImportAnalysis, error)
	}); ok {
		documents := make([]recipe.Document, len(decoded))
		for index := range decoded {
			documents[index] = decoded[index].document
		}

		analyzed, err := analyzer.AnalyzeImportCandidates(ctx, documents)
		if err != nil {
			return ImportPreview{}, fmt.Errorf("analyze import candidates: %w", err)
		}

		if len(analyzed) != len(decoded) {
			return ImportPreview{}, fmt.Errorf("analyze import candidates: got %d results for %d candidates", len(analyzed), len(decoded))
		}

		analyses = analyzed
	}

	seenRecipes := map[recipe.RecipeID]bool{}

	for index, decoded := range decoded {
		id := fmt.Sprintf("item-%d", index+1)

		decoded.item.SourceBytes = append([]byte(nil), decoded.sourceBytes...)
		decoded.item.SourceFilename = decoded.sourceFilename
		decoded.item.SourceMediaType = decoded.sourceMediaType
		view := ImportPreviewItem{ID: id, Recipe: decoded.document, Source: decoded.source, Analysis: analyses[index]}

		view.Source.CanRetain = len(decoded.sourceBytes) > 0 && decoded.source.FormatID == "document"
		for _, diagnostic := range append(append([]importer.Diagnostic(nil), decoded.batchDiagnostics...), decoded.diagnostics...) {
			view.Diagnostics = append(view.Diagnostics, previewDiagnostic(diagnostic))
		}

		switch view.Analysis.Status {
		case library.ImportDefiniteMatch:
			view.Diagnostics = append(view.Diagnostics, ImportDiagnostic{
				Code: "existing-source-match", Class: importer.DiagnosticActionable, Severity: SeverityWarning, Area: "duplicate", Count: 1,
				Message: "This source is already represented in the library. Choose whether to update it, keep the existing copy, or import another copy.",
			})
		case library.ImportLikelyDuplicate:
			view.Diagnostics = append(view.Diagnostics, ImportDiagnostic{
				Code: "likely-duplicate", Class: importer.DiagnosticActionable, Severity: SeverityWarning, Area: "duplicate", Count: 1,
				Message: "This recipe resembles one already in the library. Similarity is only a suggestion; keeping both is the safe default.",
			})
		case library.ImportAmbiguous:
			view.Diagnostics = append(view.Diagnostics, ImportDiagnostic{
				Code: "ambiguous-identity", Class: importer.DiagnosticActionable, Severity: SeverityWarning, Area: "duplicate", Count: 1,
				Message: "More than one library recipe carries the same source evidence, so Savorum cannot choose one to update.",
			})
		case library.ImportDefinitelyNew:
			// Nothing resembles this recipe, so there is nothing to say about it.
		}

		if seenRecipes[decoded.document.ID] {
			view.Diagnostics = append(view.Diagnostics, ImportDiagnostic{
				Code: "duplicate-candidate", Class: importer.DiagnosticBlocking, Severity: SeverityError, Area: "duplicate", Count: 1,
				Message: "Another selected file contains this same portable recipe. Keep one copy and leave this duplicate out.",
			})
		} else if decoded.document.ID != "" {
			seenRecipes[decoded.document.ID] = true

			decisions, preflightErr := s.library.PreflightImportIdentities(ctx, []recipe.Document{decoded.document}, library.DuplicateSkip)
			if preflightErr != nil {
				view.Diagnostics = append(view.Diagnostics, ImportDiagnostic{
					Code: "candidate-invalid", Class: importer.DiagnosticBlocking, Severity: SeverityError, Area: "content", Message: preflightErr.Error(), Count: 1,
				})
			} else if len(decisions) == 1 && !decisions[0] {
				view.AlreadyHere = true
				view.Diagnostics = append(view.Diagnostics, ImportDiagnostic{
					Code: "already-in-library", Class: importer.DiagnosticInformational, Severity: SeverityInfo, Area: "duplicate", Count: 1,
					Message: "This exact portable recipe is already in the library and will be left alone by default.",
				})
			}
		}

		view.Diagnostics = append(view.Diagnostics, unwritableDiagnostics(decoded.document, decoded.item)...)

		if !hasError(view.Diagnostics) {
			s.inspectAssets(decoded, &view)
		}

		view.CanCommit = decoded.document.ID != "" && !hasError(view.Diagnostics)
		// Collapse as the durable import does, so both report the same findings.
		view.Diagnostics = aggregatedPreviewDiagnostics(view.Diagnostics)
		candidate := &previewCandidate{
			view: view, item: decoded.item, metadata: decoded.metadata, importerMetadata: decoded.importerMetadata,
		}
		candidate.diagnostics = durablePreviewDiagnostics(view.Diagnostics)
		candidate.retainedBytes = retainedCandidateBytes(candidate)

		session.order = append(session.order, id)
		session.candidates[id] = candidate

		preview.Items = append(preview.Items, view)
	}

	session.retainedBytes = retainedSessionBytes(session)
	if err := reservation.admit(previewID, session); err != nil {
		return ImportPreview{}, err
	}

	preview.ResolutionGroups = resolutionGroups(previewID, session)
	preview.ResolutionDecisions = resolutionDecisions(previewID, session)
	preview.ResolutionProfiles = measurementConventionProfiles()

	return preview, nil
}

// aggregatedPreviewDiagnostics collapses a candidate's findings by the sentence
// each one reads as, keeping the severity the preview assigned.
func aggregatedPreviewDiagnostics(values []ImportDiagnostic) []ImportDiagnostic {
	// Keyed exactly as the collapse is, so a merged entry keeps the severity
	// its occurrences carried rather than one picked out of a different group.
	key := func(code string, class importer.DiagnosticClass, message string) string {
		return code + "\x00" + string(class) + "\x00" + message
	}

	severities := make(map[string]ImportSeverity, len(values))
	for _, value := range values {
		severities[key(value.Code, value.Class, value.Message)] = value.Severity
	}

	collapsed := importer.AggregateDiagnostics(durablePreviewDiagnostics(values))

	result := make([]ImportDiagnostic, 0, len(collapsed))
	for _, value := range collapsed {
		result = append(result, ImportDiagnostic{
			Code: value.Code, Class: value.Class, Severity: severities[key(value.Code, value.Class, value.Message)],
			Area: value.Area, Message: value.Message, Count: value.Count, Targets: value.Targets,
		})
	}

	return result
}

func durablePreviewDiagnostics(values []ImportDiagnostic) []importer.Diagnostic {
	diagnostics := make([]importer.Diagnostic, 0, len(values))
	for _, value := range values {
		diagnostics = append(diagnostics, importer.Diagnostic{
			Code: value.Code, Class: value.Class, Severity: importer.Severity(value.Severity),
			Area: value.Area, Message: value.Message, Count: value.Count, Targets: value.Targets,
		})
	}

	return diagnostics
}

// inspectAssets records what photographs a candidate names. It fetches
// nothing; see PreviewImage.
func (*Service) inspectAssets(decoded decodedCandidate, view *ImportPreviewItem) {
	if decoded.item.Package {
		view.Assets.Discovered = len(decoded.document.Assets)
		view.Assets.Available = len(decoded.document.Assets)

		return
	}

	if decoded.item.PortableAssetReferences {
		view.Assets.Discovered = len(decoded.document.Assets)
		view.Assets.Available = len(decoded.item.EmbeddedAssets)

		view.Assets.Unavailable = max(0, view.Assets.Discovered-view.Assets.Available)
		if view.Assets.Unavailable > 0 {
			view.Diagnostics = append(view.Diagnostics, ImportDiagnostic{
				Code: "asset-bytes-missing", Class: importer.DiagnosticActionable, Severity: SeverityWarning, Area: "asset", Count: view.Assets.Unavailable,
				Message: "This SREF document names photographs but does not carry their bytes. Import a .sref package to bring them with the recipe.",
			})
		}

		return
	}

	view.Assets.Discovered = len(decoded.item.RemoteAssets)
	for _, remote := range decoded.item.RemoteAssets {
		if _, err := canonicalRemoteURL(remote.URL); err != nil {
			view.Assets.Unavailable++
			continue
		}

		view.Assets.Deferred++
	}

	if view.Assets.Unavailable > 0 {
		view.Diagnostics = append(view.Diagnostics, ImportDiagnostic{
			Code: "asset-address-unusable", Class: importer.DiagnosticActionable, Severity: SeverityWarning, Area: "asset", Count: view.Assets.Unavailable,
			Message: plural(view.Assets.Unavailable,
				"One photograph is named by an address Savorum cannot fetch. The recipe keeps the address; the picture will not be there.",
				fmt.Sprintf("%d photographs are named by addresses Savorum cannot fetch. The recipe keeps them; the pictures will not be there.", view.Assets.Unavailable)),
		})
	}
}
