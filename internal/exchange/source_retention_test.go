package exchange_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
)

// sourceDocumentAdapter stands in for the bounded PDF/image adapter. Keeping
// this fixture at the document format boundary ensures the retention tests do
// not accidentally bless JSON or other structured imports for source storage.
type sourceDocumentAdapter struct{ document recipe.Document }

func (sourceDocumentAdapter) Format() importer.Format {
	return importer.Format{ID: "document", Name: "PDF or image recipe", Version: "1", Extensions: []string{".pdf", ".png", ".jpg", ".jpeg"}}
}

func (sourceDocumentAdapter) Probe(_ context.Context, input importer.Input) importer.ProbeResult {
	for _, extension := range []string{".pdf", ".png", ".jpg", ".jpeg"} {
		if strings.HasSuffix(strings.ToLower(input.Name), extension) {
			return importer.ProbeResult{Confidence: importer.ConfidenceExact, Reason: "document fixture"}
		}
	}

	return importer.ProbeResult{}
}

func (a sourceDocumentAdapter) Import(context.Context, importer.Input, importer.Options) (importer.ImportBatch, error) {
	return importer.ImportBatch{
		Source: importer.ImportSource{Format: a.Format()},
		Recipes: []importer.ImportedRecipe{{
			Document: a.document,
			ImporterMetadata: map[string]any{
				"source_text": "Scanned card\n\nIngredients\n1 cup flour\n\nInstructions\nMix.",
			},
		}},
	}, nil
}

func sourceDocumentService(recipes *importLibrary, blobs exchange.Assets, sources *sourceRecorder) *exchange.Service {
	return exchange.NewService(&atomicSourceRecorder{importLibrary: recipes, sources: sources}, blobs,
		exchange.WithImporterRegistry(importer.NewRegistry(sourceDocumentAdapter{document: func() recipe.Document {
			document := testfixture.Document()
			document.ID, document.Title = "source-document", "Source cake"

			return document
		}()})),
		exchange.WithSourceAttachments(sources),
	)
}

type sourceRecorder struct{ values []library.SourceAttachment }

// atomicSourceRecorder models the repository contract used by production:
// source metadata arrives with the mutation and is committed in that same
// transaction. It deliberately has no post-commit AttachSource fallback.
type atomicSourceRecorder struct {
	*importLibrary
	sources *sourceRecorder
}

func (r *atomicSourceRecorder) ImportRecipesWithSources(ctx context.Context, documents []recipe.Document, policy library.DuplicatePolicy, sources [][]library.SourceAttachment) ([]library.ImportResult, error) {
	base := r.importLibrary

	results, err := base.ImportRecipes(ctx, documents, policy)
	if err != nil {
		return nil, err
	}

	for index, attachments := range sources {
		if index >= len(results) || results[index].Outcome == library.ImportSkipped {
			continue
		}

		for _, attachment := range attachments {
			attachment.EntryID = results[index].EntryID
			attachment.Revision = results[index].Revision
			r.sources.values = append(r.sources.values, attachment)
		}
	}

	return results, nil
}

func (r *atomicSourceRecorder) ReconcileImportRecipesWithSources(ctx context.Context, documents []recipe.Document, _ []library.ImportResolution, sources [][]library.SourceAttachment) ([]library.ImportResult, error) {
	base := r.importLibrary

	results, err := base.ImportRecipes(ctx, documents, library.DuplicateSkip)
	if err != nil {
		return nil, err
	}

	for index, attachments := range sources {
		if index >= len(results) || results[index].Outcome == library.ImportSkipped {
			continue
		}

		for _, attachment := range attachments {
			attachment.EntryID = results[index].EntryID
			attachment.Revision = results[index].Revision
			r.sources.values = append(r.sources.values, attachment)
		}
	}

	return results, nil
}

func (r *sourceRecorder) AttachSource(_ context.Context, value library.SourceAttachment) error {
	r.values = append(r.values, value)
	return nil
}

type sourceAssets struct{ values []assets.Blob }

type failingSourceAssets struct{ sourceAssets }

func (*failingSourceAssets) Ingest(context.Context, io.Reader, assets.IngestOptions) (assets.Blob, error) {
	return assets.Blob{}, errors.New("source asset store unavailable")
}

func (*sourceAssets) Open(context.Context, assets.ID) (assets.Blob, io.ReadCloser, error) {
	return assets.Blob{}, nil, assets.ErrNotFound
}

func (a *sourceAssets) Ingest(_ context.Context, source io.Reader, options assets.IngestOptions) (assets.Blob, error) {
	data, err := io.ReadAll(source)
	if err != nil {
		return assets.Blob{}, fmt.Errorf("read retained test asset: %w", err)
	}

	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	blob := assets.Blob{ID: assets.ID("sha256:" + hash), ContentHash: hash, MediaType: options.MediaType, Size: int64(len(data))}
	a.values = append(a.values, blob)

	return blob, nil
}

func TestPreviewCommitSourceIngestFailureDoesNotCommitRecipe(t *testing.T) {
	sources := &sourceRecorder{}
	recipes := &importLibrary{}
	service := sourceDocumentService(recipes, &failingSourceAssets{}, sources)

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{Filename: "failed.pdf", Data: []byte("%PDF-1.7 source")}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.CommitPreviewWithOptions(context.Background(), preview.ID, []string{preview.Items[0].ID}, library.DuplicateSkip, exchange.PreviewCommitOptions{RetainSourceItemIDs: []string{preview.Items[0].ID}})
	if err == nil || !strings.Contains(err.Error(), "source asset store unavailable") {
		t.Fatalf("source ingest error = %v", err)
	}

	if len(recipes.imported) != 0 || len(sources.values) != 0 {
		t.Fatalf("failed source retention committed recipe or attachment: recipes=%#v sources=%#v", recipes.imported, sources.values)
	}
}

func TestPreviewCommitCanRetainUploadedOriginalSeparately(t *testing.T) {
	sources := &sourceRecorder{}
	blobs := &sourceAssets{}
	recipes := &importLibrary{}
	service := sourceDocumentService(recipes, blobs, sources)
	original := []byte("%PDF-1.7 bounded document fixture")

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{Filename: "cookbook.pdf", Data: original}})
	if err != nil {
		t.Fatal(err)
	}

	if len(preview.Items) != 1 || !preview.Items[0].Source.CanRetain {
		t.Fatalf("preview source retention = %#v", preview.Items)
	}

	result, err := service.CommitPreviewWithOptions(context.Background(), preview.ID, []string{preview.Items[0].ID}, library.DuplicateSkip, exchange.PreviewCommitOptions{RetainSourceItemIDs: []string{preview.Items[0].ID}})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Items) != 1 || len(sources.values) != 1 || len(blobs.values) != 1 {
		t.Fatalf("result=%#v sources=%#v blobs=%#v", result, sources.values, blobs.values)
	}

	attachment := sources.values[0]
	if attachment.EntryID != result.Items[0].EntryID || attachment.Revision != result.Items[0].Revision || attachment.Filename != "cookbook.pdf" || attachment.MediaType != "application/pdf" || attachment.Size != int64(len(original)) {
		t.Fatalf("attachment=%#v result=%#v", attachment, result.Items[0])
	}
}

func TestPreviewCommitDoesNotRetainOriginalByDefault(t *testing.T) {
	sources := &sourceRecorder{}
	service := sourceDocumentService(&importLibrary{}, &sourceAssets{}, sources)

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{Filename: "card.jpg", Data: []byte("image fixture")}})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.CommitPreview(context.Background(), preview.ID, []string{preview.Items[0].ID}, library.DuplicateSkip); err != nil {
		t.Fatal(err)
	}

	if len(sources.values) != 0 {
		t.Fatalf("default commit retained source: %#v", sources.values)
	}
}

func TestEditPreviewTextReparsesDocumentCandidateBeforeCommit(t *testing.T) {
	document := testfixture.Document()
	document.ID, document.Title = "scanned-card", "Scanned card"
	document.Language = "en"
	document.SourceLocale = "en-AU"
	document.Source = &recipe.Source{URL: "https://example.com/video/cake"}
	service := exchange.NewService(&importLibrary{}, &sourceAssets{}, exchange.WithImporterRegistry(importer.NewRegistry(sourceDocumentAdapter{document: document})))
	original := []byte("image fixture")

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{Filename: "card.jpg", Data: original}})
	if err != nil {
		t.Fatal(err)
	}

	if strings.TrimSpace(preview.Items[0].Source.ExtractedText) == "" {
		t.Fatalf("document source did not expose extracted text: %#v", preview.Items[0].Source)
	}
	// The replacement uses the shared plain-text importer, preserving the
	// original image bytes for the optional attachment.
	updated, err := service.EditPreviewText(context.Background(), preview.ID, preview.Items[0].ID, "Corrected cake\n\nIngredients\n2 cups flour\n\nInstructions\nFold gently.")
	if err != nil {
		t.Fatal(err)
	}

	if len(updated.Items) != 1 || updated.Items[0].Recipe.Title != "Corrected cake" || updated.Items[0].Source.Filename != "card.jpg" || !updated.Items[0].Source.CanRetain {
		t.Fatalf("updated preview = %#v", updated.Items)
	}

	edited := updated.Items[0].Recipe
	if edited.Source == nil || edited.Source.URL != document.Source.URL || edited.SourceLocale != "en-AU" || edited.Language != "en" || edited.IngredientSections[0].Ingredients[0].Quantity.Unit != "volume.cup.au" {
		t.Fatalf("edited text lost source or regional measurements: %#v", edited)
	}
}

func TestPlannedSourceRetentionSurvivesJobPayload(t *testing.T) {
	sources := &sourceRecorder{}
	blobs := &sourceAssets{}
	document := testfixture.Document()
	document.ID, document.Title = "planned-document", "Planned"
	service := exchange.NewService(&atomicSourceRecorder{importLibrary: &importLibrary{}, sources: sources}, blobs, exchange.WithImporterRegistry(importer.NewRegistry(sourceDocumentAdapter{document: document})), exchange.WithSourceAttachments(sources))
	data := []byte("image fixture")

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{Filename: "planned.png", Data: data}})
	if err != nil {
		t.Fatal(err)
	}

	plan, err := service.PlanPreviewImportWithOptions(context.Background(), preview.ID, []exchange.PreviewResolution{{ItemID: preview.Items[0].ID}}, false, exchange.PreviewCommitOptions{RetainSourceItemIDs: []string{preview.Items[0].ID}})
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Items) != 1 || !plan.Items[0].RetainSource || !plan.Items[0].Resumable() {
		t.Fatalf("planned item=%#v", plan.Items)
	}

	restored, err := exchange.RestorePlanned(plan.Items[0].Payload())
	if err != nil {
		t.Fatal(err)
	}

	if !restored.RetainSource || !strings.EqualFold(restored.Source.Filename, "planned.png") {
		t.Fatalf("restored item=%#v", restored)
	}
}
