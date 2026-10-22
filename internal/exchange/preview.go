package exchange

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/sref/archivepolicy"
	"github.com/savorum/savorum/internal/units"
)

var (
	// ErrPreviewNotFound reports import preview not found.
	ErrPreviewNotFound = errors.New("import preview not found")
	// ErrPreviewState reports import preview cannot be committed.
	ErrPreviewState = errors.New("import preview cannot be committed")
	// ErrPreviewImagePending says the photograph has not been retrieved yet
	// and the import will fetch it. It is not an error about the recipe, and
	// nothing has been decided about the picture.
	ErrPreviewImagePending = errors.New("import preview photograph has not been retrieved yet")
	// ErrPreviewImageUnavailable says the address itself did not yield a
	// photograph. The recipe keeps the address either way.
	ErrPreviewImageUnavailable = errors.New("import preview photograph could not be retrieved")
)

// ImportSeverity is deliberately small and stable so the same diagnostics can
// later be persisted as import history without translating an adapter's private
// vocabulary.
type ImportSeverity string

// SeverityInfo and related constants enumerate supported ImportSeverity values.
const (
	SeverityInfo    ImportSeverity = "info"
	SeverityWarning ImportSeverity = "warning"
	SeverityError   ImportSeverity = "error"
)

// ImportDiagnostic says what was interpreted, retained imperfectly, or could
// not be brought in. Area is a presentation-friendly group rather than a path
// into Savorum's storage schema.
type ImportDiagnostic struct {
	Code     string                      `json:"code"`
	Class    importer.DiagnosticClass    `json:"class"`
	Severity ImportSeverity              `json:"severity"`
	Area     string                      `json:"area"`
	Message  string                      `json:"message"`
	Count    int                         `json:"count"`
	Targets  []importer.DiagnosticTarget `json:"targets"`
}

// ImportPreviewSource identifies the uploaded or remote input that produced a preview item.
type ImportPreviewSource struct {
	FormatID    string `json:"format_id"`
	FormatName  string `json:"format_name"`
	Filename    string `json:"filename"`
	URL         string `json:"url"`
	Attribution string `json:"attribution"`
	// CanRetain is true for an uploaded file whose original bytes remain in
	// this preview until it is committed. Retention is always an explicit
	// choice at commit time.
	CanRetain bool `json:"can_retain"`
	// ExtractedText is the editable text used to interpret document uploads.
	// It is omitted for sources that already carry structured recipe data.
	ExtractedText string `json:"extracted_text"`
}

// ImportAssetSummary counts photographs by state. Available is on hand;
// Deferred will be fetched at import; Failed did not answer and will be
// retried at import; Unavailable cannot yield a photograph. Every one keeps
// its address on the recipe.
type ImportAssetSummary struct {
	Discovered  int `json:"discovered"`
	Available   int `json:"available"`
	Deferred    int `json:"deferred"`
	Failed      int `json:"failed"`
	Unavailable int `json:"unavailable"`
}

// PreviewAsset is a photograph retained for the lifetime of an import preview.
// The byte slice is copied before it leaves the preview store.
type PreviewAsset struct {
	Bytes     []byte
	MediaType string
}

// ImportPreviewItem is the common intermediate result for SREF, JSON-LD, and
// every source adapter. Recipe is the normalized candidate; diagnostics retain
// the decisions and losses that must remain visible beside it.
type ImportPreviewItem struct {
	ID          string                 `json:"id"`
	Recipe      recipe.Document        `json:"recipe"`
	Source      ImportPreviewSource    `json:"source"`
	Analysis    library.ImportAnalysis `json:"analysis"`
	Diagnostics []ImportDiagnostic     `json:"diagnostics"`
	Assets      ImportAssetSummary     `json:"assets"`
	AlreadyHere bool                   `json:"already_here"`
	CanCommit   bool                   `json:"can_commit"`
}

// ImportPreview is a revisioned, expiring set of decoded candidates awaiting selection.
type ImportPreview struct {
	ID        string              `json:"id"`
	ExpiresAt time.Time           `json:"expires_at"`
	Revision  uint64              `json:"revision"`
	Items     []ImportPreviewItem `json:"items"`
	// ResolutionDecisions are the questions a person is actually asked: one per
	// measure per source. ResolutionGroups remain the alias spellings each one
	// covers, which is what a plan is scoped by.
	ResolutionDecisions []MeasurementResolutionDecision `json:"resolution_decisions"`
	ResolutionGroups    []MeasurementResolutionGroup    `json:"resolution_groups"`
	ResolutionProfiles  []MeasurementConventionProfile  `json:"resolution_profiles"`
}

// UploadedImport carries one named input payload and its optional media type.
type UploadedImport struct {
	Filename string
	Data     []byte
}

// PreviewCommitOptions controls application-owned behavior that is not part
// of the portable recipe. Source files are retained only for explicitly
// selected preview items.
type PreviewCommitOptions struct {
	RetainSourceItemIDs []string
	// AcceptPlan admits work before retiring the selection; errors preserve it.
	AcceptPlan func(ImportPlan) error
}

// ImportCommitResult reports committed library mutations and the assets retained with them.
type ImportCommitResult struct {
	Items     []library.ImportResult
	Remaining *ImportPreview
}

// PreviewResolution chooses how one duplicate candidate should be committed.
type PreviewResolution struct {
	ItemID        string
	Action        library.ImportResolutionAction
	TargetEntryID library.EntryID
}

type previewCandidate struct {
	view             ImportPreviewItem
	item             ImportItem
	metadata         importer.LibraryMetadata
	importerMetadata map[string]any
	diagnostics      []importer.Diagnostic
	retainedBytes    int64
}

type previewSession struct {
	// imageOutcomes remembers a settled answer about one photograph so a row
	// that is scrolled past twice does not reach out twice. A deferred
	// photograph is deliberately not settled.
	imageOutcomes   map[string]remoteStagingOutcome
	imageFetches    int
	expiresAt       time.Time
	busy            bool
	expired         bool
	retainedBytes   int64
	timer           *time.Timer
	order           []string
	candidates      map[string]*previewCandidate
	revision        uint64
	resolutionPlans map[string]*storedResolutionPlan
	// pendingDecisions holds answers for ingredients that need more than one
	// before their quantity can be stored, keyed by item and ingredient. They
	// are review state, never part of a recipe.
	pendingDecisions map[string]*pendingIngredientDecisions
	stagedRemote     map[string]stagedRemoteAsset
	scope            string
}

// pendingIngredientDecisions is what has been answered about one authored
// ingredient statement, and what those answers were given against.
//
// The source text and parser and registry versions are carried so editing the
// line, or a build that reads it differently, invalidates the answers.
type pendingIngredientDecisions struct {
	interpretations map[string]string
	convention      *units.ConventionProfile
	sourceText      string
	parserVersion   string
	registryVersion string
}

// current reports whether these answers were given against the reading in
// front of us now.
func (pending *pendingIngredientDecisions) current(sourceText string) bool {
	return pending != nil && pending.sourceText == sourceText &&
		pending.parserVersion == ingredientparser.ParserVersion &&
		pending.registryVersion == units.RegistryVersion
}

func pendingKey(itemID, ingredientID string) string {
	return itemID + "\x00" + ingredientID
}

type decodedCandidate struct {
	document         recipe.Document
	item             ImportItem
	source           ImportPreviewSource
	metadata         importer.LibraryMetadata
	importerMetadata map[string]any
	diagnostics      []importer.Diagnostic
	batchDiagnostics []importer.Diagnostic
	sourceBytes      []byte
	sourceFilename   string
	sourceMediaType  string
}

// PreviewFiles decodes every selected file without writing recipes or assets.
// A bad file becomes an error item rather than preventing clean files in the
// same selection from being reviewed and committed.
func (s *Service) PreviewFiles(ctx context.Context, uploads []UploadedImport) (ImportPreview, error) {
	if len(uploads) == 0 {
		return ImportPreview{}, fmt.Errorf("%w: choose at least one import file", ErrPreviewState)
	}

	var inputBytes int64
	for _, upload := range uploads {
		if int64(len(upload.Data)) > math.MaxInt64-inputBytes {
			return ImportPreview{}, ErrPreviewTooLarge
		}

		inputBytes += int64(len(upload.Data))
	}

	reservation, err := s.beginPreview(inputBytes)
	if err != nil {
		return ImportPreview{}, err
	}
	defer reservation.release()

	options := importer.Options{WorkBudget: archivepolicy.NewBudget(sref.DefaultLimits().WorkBytes)}

	var decoded []decodedCandidate

	for _, upload := range uploads {
		items, err := s.decodeUpload(ctx, upload, options)
		if err != nil {
			if errors.Is(err, archivepolicy.ErrLimit) {
				return ImportPreview{}, ErrPreviewTooLarge
			}

			if ctxErr := ctx.Err(); ctxErr != nil {
				return ImportPreview{}, fmt.Errorf("decode uploaded imports: %w", ctxErr)
			}

			decoded = append(decoded, decodedCandidate{
				source: ImportPreviewSource{Filename: filepath.Base(upload.Filename)},
				diagnostics: []importer.Diagnostic{{
					Code: "file-unreadable", Message: s.decodeFailureMessage(err),
					Severity: importer.SeverityError, Area: "source",
				}},
			})

			continue
		}

		decoded = append(decoded, items...)
	}

	if len(decoded) > library.MaximumImportRecipes {
		return ImportPreview{}, fmt.Errorf("%w: an import carries at most %d recipes", ErrPreviewState, library.MaximumImportRecipes)
	}

	return s.createPreview(ctx, decoded, reservation)
}

// PreviewURL uses the same staged result as uploaded JSON-LD and SREF. Fetch
// failures still fail the request because there is no candidate to review.
func (s *Service) PreviewURL(ctx context.Context, rawURL string) (ImportPreview, error) {
	reservation, err := s.beginPreview(0)
	if err != nil {
		return ImportPreview{}, err
	}
	defer reservation.release()

	batch, err := s.registry.Import(ctx, importer.Input{URL: rawURL}, importer.Options{})
	if err != nil {
		return ImportPreview{}, err
	}

	return s.createPreview(ctx, candidatesFromBatch(batch, ""), reservation)
}

// EditPreviewText reparses corrected text through the same plain-text adapter
// used by pasted imports. The uploaded original remains attached to the
// candidate, so editing interpretation never changes what can be retained.
func (s *Service) EditPreviewText(ctx context.Context, previewID, itemID, text string) (ImportPreview, error) {
	if len([]byte(text)) > 8<<20 || strings.TrimSpace(text) == "" {
		return ImportPreview{}, fmt.Errorf("%w: enter some recipe text within the 8 MiB limit", ErrPreviewState)
	}

	s.previewsMu.Lock()

	session := s.previews[previewID]
	if session == nil || session.scope != s.previewScope(ctx) {
		s.previewsMu.Unlock()
		return ImportPreview{}, ErrPreviewNotFound
	}

	if session.busy {
		s.previewsMu.Unlock()
		return ImportPreview{}, fmt.Errorf("%w: this preview is already being changed", ErrPreviewState)
	}

	candidate := session.candidates[itemID]
	if candidate == nil || strings.TrimSpace(candidate.view.Source.ExtractedText) == "" {
		s.previewsMu.Unlock()
		return ImportPreview{}, fmt.Errorf("%w: this preview item has no editable extracted text", ErrPreviewState)
	}

	session.busy = true
	old := *candidate
	s.previewsMu.Unlock()

	// Text corrections always use the application's shared plain-text parser.
	// A caller may provide a restricted registry for the original source (for
	// example, a document-only preview), but that must not make the editable
	// text path depend on that source adapter also exposing .txt.
	batch, err := importer.DefaultRegistry().Import(ctx, importer.Input{Name: "edited-recipe.txt", Data: []byte(text)}, importer.Options{})
	if err != nil || len(batch.Recipes) != 1 {
		s.releasePreviewClaim(previewID, session)

		if err == nil {
			err = fmt.Errorf("recipe text did not produce one recipe")
		}

		return ImportPreview{}, err
	}

	imported := batch.Recipes[0]

	document := imported.Document
	if old.view.Recipe.Source != nil {
		document.Source = old.view.Recipe.Source
	}

	document.Language = old.view.Recipe.Language
	document.SourceLocale = old.view.Recipe.SourceLocale
	retainSemanticArticleMetadata(old, &imported, &document)
	imported.Diagnostics = append(imported.Diagnostics, importer.ReviewIngredientMeasurements(&document)...)
	item := old.item
	item.Document = &document
	item.SourceBytes = append([]byte(nil), old.item.SourceBytes...)
	item.SourceFilename = old.item.SourceFilename
	item.SourceMediaType = old.item.SourceMediaType
	view := old.view
	view.Recipe = document
	view.Source.ExtractedText = text

	view.Diagnostics = nil
	for _, diagnostic := range imported.Diagnostics {
		view.Diagnostics = append(view.Diagnostics, previewDiagnostic(diagnostic))
	}

	view.Diagnostics = append(view.Diagnostics, ImportDiagnostic{Code: "extracted_text_edited", Class: importer.DiagnosticInformational, Severity: SeverityInfo, Area: "source", Count: 1, Message: "The extracted text was edited before import."})
	view.AlreadyHere = false

	view.Diagnostics = append(view.Diagnostics, unwritableDiagnostics(document, item)...)

	view.CanCommit = document.ID != "" && !hasError(view.Diagnostics)
	if analyzer, ok := s.library.(interface {
		AnalyzeImportCandidates(context.Context, []recipe.Document) ([]library.ImportAnalysis, error)
	}); ok {
		analyses, analyzeErr := analyzer.AnalyzeImportCandidates(ctx, []recipe.Document{document})
		if analyzeErr != nil {
			s.releasePreviewClaim(previewID, session)
			return ImportPreview{}, analyzeErr
		}

		if len(analyses) == 1 {
			view.Analysis = analyses[0]
		}
	}

	view.Source.CanRetain = len(item.SourceBytes) > 0
	updated := &previewCandidate{view: view, item: item, metadata: imported.LibraryMetadata, importerMetadata: imported.ImporterMetadata, diagnostics: imported.Diagnostics}
	updated.retainedBytes = retainedCandidateBytes(updated)

	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	if s.previews[previewID] != session || session.expired {
		if s.previews[previewID] == session {
			session.busy = false
		}

		return ImportPreview{}, ErrPreviewNotFound
	}

	growth := updated.retainedBytes - candidate.retainedBytes
	if growth > 0 && (exceedsInt64(session.retainedBytes, growth, s.previewLimits.RetainedBytesPerSession) || exceedsInt64(s.previewStats.RetainedBytes+s.pendingPreviewBytes, growth, s.previewLimits.RetainedBytes)) {
		session.busy = false
		s.previewStats.Rejected++

		return ImportPreview{}, ErrPreviewTooLarge
	}

	session.candidates[itemID] = updated
	session.revision++
	session.resolutionPlans = map[string]*storedResolutionPlan{}
	previousBytes := session.retainedBytes
	session.retainedBytes = retainedSessionBytes(session)
	s.previewStats.RetainedBytes += session.retainedBytes - previousBytes
	session.busy = false

	return previewFromSession(previewID, session), nil
}

func retainSemanticArticleMetadata(previous previewCandidate, imported *importer.ImportedRecipe, document *recipe.Document) {
	if previous.importerMetadata["extraction_method"] != "semantic-html" {
		return
	}

	document.Authors = previous.view.Recipe.Authors
	document.Description = previous.view.Recipe.Description
	document.Published = previous.view.Recipe.Published

	document.Modified = previous.view.Recipe.Modified
	for key, value := range previous.importerMetadata {
		if key != "source_text" {
			imported.ImporterMetadata[key] = value
		}
	}
}

// Preview returns a staged preview the caller still owns.
//
// The session outlives the page that made it, so a person can return to a
// partly imported review. Expired, foreign, and unknown previews all return
// ErrPreviewNotFound.
func (s *Service) Preview(ctx context.Context, previewID string) (ImportPreview, error) {
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	session := s.previews[previewID]
	if session == nil || session.expired || session.scope != s.previewScope(ctx) {
		return ImportPreview{}, ErrPreviewNotFound
	}

	return previewFromSession(previewID, session), nil
}

// CancelPreview releases all retained state and capacity for an unfinished preview.
func (s *Service) CancelPreview(previewID string) {
	s.previewsMu.Lock()
	if session := s.previews[previewID]; session != nil {
		if session.busy {
			session.expired = true
		} else {
			s.removePreviewLocked(previewID, session)
		}

		s.previewStats.Canceled++
	}
	s.previewsMu.Unlock()
}

func previewFromSession(id string, session *previewSession) ImportPreview {
	preview := ImportPreview{ID: id, ExpiresAt: session.expiresAt, Revision: session.revision}
	for _, itemID := range session.order {
		if candidate := session.candidates[itemID]; candidate != nil {
			preview.Items = append(preview.Items, candidate.view)
		}
	}

	preview.ResolutionGroups = resolutionGroups(id, session)
	preview.ResolutionDecisions = resolutionDecisions(id, session)
	preview.ResolutionProfiles = measurementConventionProfiles()

	return preview
}

func randomPreviewID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("create import preview identifier: %w", err)
	}

	return hex.EncodeToString(data), nil
}

// CancelPreviewScoped releases a preview only for its initiating authorization scope.
func (s *Service) CancelPreviewScoped(ctx context.Context, previewID string) error {
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	session := s.previews[previewID]
	if session == nil || session.scope != s.previewScope(ctx) {
		return ErrPreviewNotFound
	}

	if session.busy {
		session.expired = true
	} else {
		s.removePreviewLocked(previewID, session)
	}

	s.previewStats.Canceled++

	return nil
}
