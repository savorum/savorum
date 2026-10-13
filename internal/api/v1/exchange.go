package v1

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/importer/webrecipe"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/textutil"
)

// ImportInput carries SREF recipe documents, validated against SREF rather
// than restated in this API's schema. on_duplicate says what to do when a
// document's identity is already in the library.
type ImportInput struct {
	Recipes     []json.RawMessage `json:"recipes" minItems:"1" maxItems:"500" doc:"SREF recipe documents, each carrying its own sref version member"`
	OnDuplicate string            `json:"on_duplicate,omitempty" enum:"reject,skip,replace" doc:"Defaults to reject, which changes nothing and reports what was already here"`
}

// streamExternalRecipeImport is format-neutral. Source adapters are selected
// by the importer registry while batching, validation, duplicate policy, and
// result reporting stay identical for every brand and file type.
func streamExternalRecipeImport(service ExchangeService) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(response, request.Body, httpsecurity.ExternalImportBodyBytes)
		// The reader above caps the body before anything parses it, so the
		// parse cannot be asked for more memory than that limit.
		if err := request.ParseMultipartForm(httpsecurity.ExternalImportBodyBytes); err != nil { //nolint:gosec // bounded by MaxBytesReader above
			writeAssetHTTPError(response, http.StatusBadRequest, "recipe-import-invalid", "Choose a supported recipe export.")
			return
		}
		defer func() { _ = request.MultipartForm.RemoveAll() }()

		var inputs []importer.Input

		for _, headers := range request.MultipartForm.File {
			for _, header := range headers {
				opened, err := header.Open()
				if err != nil {
					writeAssetHTTPError(response, http.StatusUnprocessableEntity, "recipe-import-invalid", "One selected export could not be opened.")
					return
				}

				body, readErr := io.ReadAll(io.LimitReader(opened, 64<<20))
				_ = opened.Close()

				if readErr != nil {
					writeAssetHTTPError(response, http.StatusUnprocessableEntity, "recipe-import-invalid", "One selected export could not be read.")
					return
				}

				inputs = append(inputs, importer.Input{Name: header.Filename, Data: body})
			}
		}

		if len(inputs) == 0 {
			writeAssetHTTPError(response, http.StatusBadRequest, "recipe-import-empty", "Choose at least one supported recipe export.")
			return
		}

		selectedFormats, err := service.ProbeSources(request.Context(), inputs)
		if err != nil {
			status := http.StatusUnprocessableEntity
			if errors.Is(err, importer.ErrUnsupportedFormat) {
				status = http.StatusUnsupportedMediaType
			}

			writeAssetHTTPError(response, status, "recipe-import-format", err.Error())

			return
		}

		policy := library.DuplicatePolicy(request.URL.Query().Get("on_duplicate"))
		if policy == "" {
			policy = library.DuplicateSkip
		}

		results, err := service.ImportSources(request.Context(), inputs, importer.Options{}, policy)
		if err != nil {
			status, kind, detail := importHTTPError(err)
			writeAssetHTTPError(response, status, kind, detail)

			return
		}

		response.Header().Set("Content-Type", "application/json")

		formatNames := uniqueFormatNames(selectedFormats)
		_ = json.NewEncoder(response).Encode(map[string]any{"items": importResults(results).Body.Items, "formats": formatNames})
	}
}

func uniqueFormatNames(formats []importer.Format) []string {
	names := make([]string, 0, len(formats))
	seen := make(map[string]bool, len(formats))

	for _, format := range formats {
		if !seen[format.Name] {
			seen[format.Name] = true
			names = append(names, format.Name)
		}
	}

	sort.Strings(names)

	return names
}

// streamImportPreview accepts every file format the shared exchange service
// understands, including SREF documents, packages, bundles, saved recipe pages,
// and third-party exports. It stages a review session and writes nothing.
func streamImportPreview(service ExchangeService) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(response, request.Body, httpsecurity.ExternalImportBodyBytes)
		// The reader above caps the body before anything parses it, so the
		// parse cannot be asked for more memory than that limit.
		if err := request.ParseMultipartForm(httpsecurity.ExternalImportBodyBytes); err != nil { //nolint:gosec // bounded by MaxBytesReader above
			writeAssetHTTPError(response, http.StatusBadRequest, "recipe-import-invalid", "Choose one or more supported recipe files.")
			return
		}
		defer func() { _ = request.MultipartForm.RemoveAll() }()

		var uploads []exchange.UploadedImport

		for _, headers := range request.MultipartForm.File {
			for _, header := range headers {
				opened, err := header.Open()
				if err != nil {
					writeAssetHTTPError(response, http.StatusUnprocessableEntity, "recipe-import-invalid", "One selected file could not be opened.")
					return
				}

				body, readErr := io.ReadAll(io.LimitReader(opened, httpsecurity.ExternalImportBodyBytes+1))
				_ = opened.Close()

				if readErr != nil || int64(len(body)) > httpsecurity.ExternalImportBodyBytes {
					writeAssetHTTPError(response, http.StatusUnprocessableEntity, "recipe-import-invalid", "One selected file could not be read safely.")
					return
				}

				uploads = append(uploads, exchange.UploadedImport{Filename: header.Filename, Data: body})
			}
		}

		preview, err := service.PreviewFiles(request.Context(), uploads)
		if err != nil {
			status, kind, detail := importHTTPError(err)
			writeAssetHTTPError(response, status, kind, detail)

			return
		}

		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(importPreviewResponse(preview))
	}
}

// ImportPreviewCommitInput supplies selections for committing an import preview.
type ImportPreviewCommitInput struct {
	ItemIDs             []string                       `json:"item_ids,omitempty"`
	Resolutions         []ImportPreviewResolutionInput `json:"resolutions,omitempty"`
	OnDuplicate         string                         `json:"on_duplicate,omitempty"`
	RetainSourceItemIDs []string                       `json:"retain_source_item_ids,omitempty" maxItems:"500" doc:"Uploaded preview item IDs whose original files should be retained with the imported revision"`
}

// ImportPreviewResolutionInput supplies a resolution selected during import review.
type ImportPreviewResolutionInput struct {
	ItemID        string `json:"item_id"`
	Action        string `json:"action" enum:"separate,update,skip"`
	TargetEntryID string `json:"target_entry_id,omitempty"`
}

func streamImportPreviewCommit(service ExchangeService) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(response, request.Body, 1<<20)

		var input ImportPreviewCommitInput

		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(&input); err != nil {
			writeAssetHTTPError(response, http.StatusBadRequest, "import-preview-invalid", "Choose the recipes to import.")
			return
		}

		policy := library.DuplicatePolicy(input.OnDuplicate)
		if policy == "" {
			policy = library.DuplicateSkip
		}

		var (
			committed exchange.ImportCommitResult
			err       error
		)

		if len(input.Resolutions) > 0 {
			resolutions := make([]exchange.PreviewResolution, len(input.Resolutions))
			for index, resolution := range input.Resolutions {
				resolutions[index] = exchange.PreviewResolution{
					ItemID: resolution.ItemID, Action: library.ImportResolutionAction(resolution.Action),
					TargetEntryID: library.EntryID(resolution.TargetEntryID),
				}
			}

			if retained, ok := service.(interface {
				ReconcilePreviewWithOptions(context.Context, string, []exchange.PreviewResolution, exchange.PreviewCommitOptions) (exchange.ImportCommitResult, error)
			}); ok {
				committed, err = retained.ReconcilePreviewWithOptions(request.Context(), request.PathValue("preview_id"), resolutions, exchange.PreviewCommitOptions{RetainSourceItemIDs: input.RetainSourceItemIDs})
			} else {
				committed, err = service.ReconcilePreview(request.Context(), request.PathValue("preview_id"), resolutions)
			}
		} else {
			if retained, ok := service.(interface {
				CommitPreviewWithOptions(context.Context, string, []string, library.DuplicatePolicy, exchange.PreviewCommitOptions) (exchange.ImportCommitResult, error)
			}); ok {
				committed, err = retained.CommitPreviewWithOptions(request.Context(), request.PathValue("preview_id"), input.ItemIDs, policy, exchange.PreviewCommitOptions{RetainSourceItemIDs: input.RetainSourceItemIDs})
			} else {
				committed, err = service.CommitPreview(request.Context(), request.PathValue("preview_id"), input.ItemIDs, policy)
			}
		}

		if err != nil {
			status, kind, detail := importHTTPError(err)
			writeAssetHTTPError(response, status, kind, detail)

			return
		}

		result := ImportPreviewCommitResponse{Items: importResults(committed.Items).Body.Items}
		if committed.Remaining != nil {
			remaining := importPreviewResponse(*committed.Remaining)
			result.Remaining = &remaining
		}

		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(result)
	}
}

func streamImportPreviewCancel(service ExchangeService) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if err := service.CancelPreviewScoped(request.Context(), request.PathValue("preview_id")); err != nil {
			status, kind, detail := importHTTPError(err)
			writeAssetHTTPError(response, status, kind, detail)

			return
		}

		response.WriteHeader(http.StatusNoContent)
	}
}

// streamStagedImportPreview returns a review a person left and came back to.
func streamStagedImportPreview(service ExchangeService) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		preview, err := service.Preview(request.Context(), request.PathValue("preview_id"))
		if err != nil {
			status, kind, detail := importHTTPError(err)
			writeAssetHTTPError(response, status, kind, detail)

			return
		}

		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(importPreviewResponse(preview))
	}
}

func streamImportPreviewImage(service ExchangeService) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		asset, err := service.PreviewImage(request.Context(), request.PathValue("preview_id"), request.PathValue("item_id"))
		if err != nil {
			status, kind, detail := importHTTPError(err)
			writeAssetHTTPError(response, status, kind, detail)

			return
		}

		mediaType := strings.TrimSpace(asset.MediaType)
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}

		response.Header().Set("Content-Type", mediaType)
		response.Header().Set("Cache-Control", "private, no-store")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(asset.Bytes)
	}
}

type importPreviewTextInput struct {
	Text string `json:"text"`
}

func streamImportPreviewText(service ExchangeService) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		editable, ok := service.(interface {
			EditPreviewText(context.Context, string, string, string) (exchange.ImportPreview, error)
		})
		if !ok {
			writeAssetHTTPError(response, http.StatusNotImplemented, "import-preview-edit-unavailable", "Extracted text editing is unavailable.")
			return
		}

		request.Body = http.MaxBytesReader(response, request.Body, 8<<20)

		var input importPreviewTextInput

		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(&input); err != nil {
			writeAssetHTTPError(response, http.StatusBadRequest, "import-preview-invalid", "Enter the corrected recipe text.")
			return
		}

		preview, err := editable.EditPreviewText(request.Context(), request.PathValue("preview_id"), request.PathValue("item_id"), input.Text)
		if err != nil {
			status, kind, detail := importHTTPError(err)
			writeAssetHTTPError(response, status, kind, detail)

			return
		}

		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(importPreviewResponse(preview))
	}
}

func streamMeasurementResolutionGroup(service ExchangeService) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		detail, err := service.MeasurementResolutionGroup(request.Context(), request.PathValue("preview_id"), request.PathValue("group_id"))
		if err != nil {
			status, kind, message := importHTTPError(err)
			writeAssetHTTPError(response, status, kind, message)

			return
		}

		result := MeasurementResolutionGroupDetail{
			Group:       measurementResolutionGroup(detail.Group),
			Occurrences: make([]MeasurementResolutionOccurrence, 0, len(detail.Occurrences)),
		}
		for _, occurrence := range detail.Occurrences {
			result.Occurrences = append(result.Occurrences, measurementResolutionOccurrence(occurrence))
		}

		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(result)
	}
}

func streamMeasurementResolutionPlan(service ExchangeService) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(response, request.Body, 1<<20)

		var input MeasurementResolutionPlanInput

		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(&input); err != nil {
			writeAssetHTTPError(response, http.StatusBadRequest, "measurement-resolution-invalid", "Choose a valid measurement interpretation.")
			return
		}

		plan, err := service.CreateMeasurementResolutionPlan(request.Context(), request.PathValue("preview_id"), exchange.MeasurementResolutionPlanRequest{
			GroupIDs: input.ResolutionGroupIDs,
			Interpretation: exchange.MeasurementResolutionInterpretation{
				Kind: input.Interpretation.Kind, UnitID: input.Interpretation.UnitID, Convention: input.Interpretation.Convention,
			},
			ExcludedOccurrenceIDs: input.ExcludedOccurrenceIDs, ExcludedItemIDs: input.ExcludedItemIDs,
			ExpectedRevision: input.ExpectedPreviewRevision,
		})
		if err != nil {
			status, kind, message := importHTTPError(err)
			writeAssetHTTPError(response, status, kind, message)

			return
		}

		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(measurementResolutionPlanResponse(plan))
	}
}

func streamMeasurementResolutionPlanApply(service ExchangeService) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(response, request.Body, 1<<20)

		var input MeasurementResolutionPlanApplyInput

		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(&input); err != nil {
			writeAssetHTTPError(response, http.StatusBadRequest, "measurement-resolution-invalid", "The preview revision is required.")
			return
		}

		plan, err := service.ApplyMeasurementResolutionPlan(request.Context(), request.PathValue("preview_id"), request.PathValue("plan_id"), input.ExpectedPreviewRevision)
		if err != nil {
			status, kind, message := importHTTPError(err)
			writeAssetHTTPError(response, status, kind, message)

			return
		}

		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(measurementResolutionPlanResponse(plan))
	}
}

func streamMeasurementResolutionPlanRevert(service ExchangeService) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(response, request.Body, 1<<20)

		var input MeasurementResolutionPlanApplyInput

		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(&input); err != nil {
			writeAssetHTTPError(response, http.StatusBadRequest, "measurement-resolution-invalid", "The preview revision is required.")
			return
		}

		preview, err := service.RevertMeasurementResolutionPlan(request.Context(), request.PathValue("preview_id"), request.PathValue("plan_id"), input.ExpectedPreviewRevision)
		if err != nil {
			status, kind, message := importHTTPError(err)
			writeAssetHTTPError(response, status, kind, message)

			return
		}

		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(importPreviewResponse(preview))
	}
}

// ImportedRecipe describes a recipe created by an import.
type ImportedRecipe struct {
	ImportEventID  string   `json:"import_event_id,omitempty"`
	LibraryEntryID string   `json:"library_entry_id"`
	RecipeID       string   `json:"recipe_id"`
	Title          string   `json:"title"`
	Revision       uint64   `json:"revision" minimum:"1"`
	Outcome        string   `json:"outcome" enum:"created,replaced,skipped"`
	DroppedAssets  int      `json:"dropped_assets" minimum:"0" doc:"Images the recipe named that could not be retrieved and were left out"`
	DeferredAssets int      `json:"deferred_assets" minimum:"0" doc:"Images not fetched yet whose addresses the recipe retains"`
	Warnings       []string `json:"warnings,omitempty" doc:"What the import had to work around, in the reader's words"`
}

// ImportPreviewDiagnostic is a diagnostic displayed during import review.
type ImportPreviewDiagnostic struct {
	Code     string                   `json:"code"`
	Class    string                   `json:"class" enum:"blocking,actionable,informational"`
	Severity string                   `json:"severity" enum:"info,warning,error"`
	Area     string                   `json:"area"`
	Message  string                   `json:"message"`
	Count    int                      `json:"count" minimum:"1"`
	Targets  []ImportDiagnosticTarget `json:"targets,omitempty"`
}

// ImportDiagnosticTarget identifies the recipe element named by a diagnostic.
type ImportDiagnosticTarget struct {
	ObjectID   string                      `json:"object_id,omitempty"`
	Field      string                      `json:"field,omitempty"`
	Evidence   string                      `json:"evidence,omitempty"`
	Preserved  string                      `json:"preserved,omitempty"`
	Action     string                      `json:"action,omitempty"`
	Resolution *ImportResolutionDescriptor `json:"resolution,omitempty"`
}

// ImportResolutionDescriptor describes an import choice requiring resolution.
type ImportResolutionDescriptor struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	AuthoredForm     string   `json:"authored_form"`
	SourceStart      int      `json:"source_start" minimum:"0"`
	SourceEnd        int      `json:"source_end" minimum:"0"`
	CandidateUnitIDs []string `json:"candidate_unit_ids"`
	ResolutionKey    string   `json:"resolution_key"`
}

// ImportPreviewSource describes the source submitted for an import preview.
type ImportPreviewSource struct {
	FormatID      string `json:"format_id"`
	FormatName    string `json:"format_name"`
	Filename      string `json:"filename,omitempty"`
	URL           string `json:"url,omitempty"`
	Attribution   string `json:"attribution,omitempty"`
	CanRetain     bool   `json:"can_retain" doc:"The uploaded original can be retained explicitly when this preview item is committed"`
	ExtractedText string `json:"extracted_text,omitempty" doc:"Text extracted from a document upload, editable before commit"`
}

// ImportPreviewAssets counts photographs that are available, deferred until
// import, failed but retried at import, or unavailable. Each keeps its address
// on the recipe.
type ImportPreviewAssets struct {
	Discovered  int    `json:"discovered" minimum:"0"`
	Available   int    `json:"available" minimum:"0"`
	Deferred    int    `json:"deferred" minimum:"0"`
	Failed      int    `json:"failed" minimum:"0"`
	Unavailable int    `json:"unavailable" minimum:"0"`
	PreviewURL  string `json:"preview_url,omitempty" doc:"Retrieves this recipe's leading photograph on demand; answers 404 with a problem type saying whether it is pending or unavailable"`
}

// ImportPreviewMatch describes a possible existing-recipe match.
type ImportPreviewMatch struct {
	EntryID         string   `json:"entry_id"`
	Revision        uint64   `json:"revision" minimum:"1"`
	RecipeID        string   `json:"recipe_id"`
	Title           string   `json:"title"`
	Authors         []string `json:"authors"`
	Yield           string   `json:"yield,omitempty"`
	IngredientCount int      `json:"ingredient_count" minimum:"0"`
	Evidence        string   `json:"evidence" enum:"source-identity,canonical-url,portable-id,similarity"`
	Score           float64  `json:"score" minimum:"0" maximum:"1"`
}

// ImportPreviewAnalysis groups analysis produced while reviewing an import.
type ImportPreviewAnalysis struct {
	Status  string               `json:"status" enum:"new,definite-match,likely-duplicate,ambiguous"`
	Matches []ImportPreviewMatch `json:"matches"`
}

// ImportPreviewSection describes one section in an import preview.
type ImportPreviewSection struct {
	Title string   `json:"title,omitempty"`
	Items []string `json:"items"`
}

// ImportPreviewRecipe is intentionally reader-facing. It exposes the recipe a
// person needs to recognize and assess without dumping Savorum's storage
// schema into the review UI.
type ImportPreviewRecipe struct {
	Title           string                 `json:"title"`
	Description     string                 `json:"description,omitempty"`
	Yield           string                 `json:"yield,omitempty"`
	IngredientCount int                    `json:"ingredient_count" minimum:"0"`
	StepCount       int                    `json:"step_count" minimum:"0"`
	Ingredients     []ImportPreviewSection `json:"ingredients,omitempty"`
	Instructions    []ImportPreviewSection `json:"instructions,omitempty"`
}

// ImportPreviewItem describes one candidate recipe in an import preview.
type ImportPreviewItem struct {
	ID          string                    `json:"id"`
	Recipe      ImportPreviewRecipe       `json:"recipe"`
	Source      ImportPreviewSource       `json:"source"`
	Analysis    ImportPreviewAnalysis     `json:"analysis"`
	Diagnostics []ImportPreviewDiagnostic `json:"diagnostics"`
	Assets      ImportPreviewAssets       `json:"assets"`
	AlreadyHere bool                      `json:"already_here"`
	CanCommit   bool                      `json:"can_commit"`
}

// ImportPreviewResponse is the API response for an import preview.
type ImportPreviewResponse struct {
	ID                  string                          `json:"id"`
	ExpiresAt           string                          `json:"expires_at" format:"date-time"`
	PreviewRevision     uint64                          `json:"preview_revision" minimum:"1"`
	Items               []ImportPreviewItem             `json:"items"`
	ResolutionDecisions []MeasurementResolutionDecision `json:"resolution_decisions"`
	ResolutionGroups    []MeasurementResolutionGroup    `json:"resolution_groups"`
	ResolutionProfiles  []MeasurementResolutionProfile  `json:"resolution_profiles"`
}

// MeasurementResolutionProfile describes a measurement convention in an import.
type MeasurementResolutionProfile struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Selects is the exact identities this convention chooses, and Unanswered
	// the measure families it does not answer for.
	Selects             []string `json:"selects"`
	Unanswered          []string `json:"unanswered"`
	UnitRegistryVersion string   `json:"unit_registry_version"`
}

// MeasurementResolutionChoice is one selectable interpretation of a measurement.
type MeasurementResolutionChoice struct {
	UnitID     string `json:"unit_id"`
	Label      string `json:"label"`
	Convention string `json:"convention"`
	// ConventionLabel names only the measuring convention, for a choice shown
	// beside a measure that has already been named.
	ConventionLabel string `json:"convention_label,omitempty"`
	// Magnitude is how much this identity holds, read out of the registry.
	Magnitude string `json:"magnitude,omitempty"`
}

// MeasurementResolutionSubgroup groups related measurement-resolution choices.
type MeasurementResolutionSubgroup struct {
	SourceText  string `json:"source_text"`
	Occurrences int    `json:"occurrences" minimum:"1"`
	Recipes     int    `json:"recipes" minimum:"1"`
}

// MeasurementResolutionExample provides an authored example for a resolution.
type MeasurementResolutionExample struct {
	RecipeTitle string `json:"recipe_title"`
	SourceText  string `json:"source_text"`
	Before      string `json:"before"`
	After       string `json:"after,omitempty"`
}

// MeasurementResolutionGroup contains related unresolved measurements.
type MeasurementResolutionGroup struct {
	ID               string                          `json:"id"`
	Kind             string                          `json:"kind" enum:"unit_alias"`
	AuthoredForm     string                          `json:"authored_form"`
	Occurrences      int                             `json:"occurrences" minimum:"1"`
	Ingredients      int                             `json:"ingredients" minimum:"0"`
	Recipes          int                             `json:"recipes" minimum:"1"`
	SourceLabel      string                          `json:"source_label,omitempty"`
	CandidateUnitIDs []string                        `json:"candidate_unit_ids"`
	Choices          []MeasurementResolutionChoice   `json:"choices"`
	Subgroups        []MeasurementResolutionSubgroup `json:"subgroups"`
	Examples         []MeasurementResolutionExample  `json:"examples"`
}

// MeasurementResolutionDecision is one question about one measure within one
// source, covering every spelling of it the source used.
type MeasurementResolutionDecision struct {
	ID           string `json:"id"`
	SourceLabel  string `json:"source_label,omitempty"`
	MeasureLabel string `json:"measure_label"`
	// AuthoredForms are the spellings the source used, commonest first.
	AuthoredForms    []string                       `json:"authored_forms"`
	GroupIDs         []string                       `json:"group_ids"`
	Occurrences      int                            `json:"occurrences" minimum:"1"`
	Ingredients      int                            `json:"ingredients" minimum:"0"`
	Recipes          int                            `json:"recipes" minimum:"1"`
	CandidateUnitIDs []string                       `json:"candidate_unit_ids"`
	Choices          []MeasurementResolutionChoice  `json:"choices"`
	Examples         []MeasurementResolutionExample `json:"examples"`
}

// MeasurementResolutionOccurrence identifies one unresolved measurement occurrence.
type MeasurementResolutionOccurrence struct {
	ID               string   `json:"id"`
	GroupID          string   `json:"group_id"`
	ItemID           string   `json:"item_id"`
	RecipeID         string   `json:"recipe_id"`
	RecipeTitle      string   `json:"recipe_title"`
	IngredientID     string   `json:"ingredient_id"`
	SourceText       string   `json:"source_text"`
	AuthoredForm     string   `json:"authored_form"`
	CandidateUnitIDs []string `json:"candidate_unit_ids"`
	ResolutionKey    string   `json:"resolution_key"`
	Fingerprint      string   `json:"fingerprint"`
}

// MeasurementResolutionGroupDetail provides the choices for a resolution group.
type MeasurementResolutionGroupDetail struct {
	Group       MeasurementResolutionGroup        `json:"group"`
	Occurrences []MeasurementResolutionOccurrence `json:"occurrences"`
}

// MeasurementResolutionInterpretationInput supplies a chosen unit interpretation.
type MeasurementResolutionInterpretationInput struct {
	Kind       string `json:"kind" enum:"canonical-unit,convention,keep-written"`
	UnitID     string `json:"unit_id,omitempty"`
	Convention string `json:"convention,omitempty" enum:"us,au,imperial,metric"`
}

// MeasurementResolutionPlanInput supplies choices for a resolution plan.
type MeasurementResolutionPlanInput struct {
	ResolutionGroupIDs      []string                                 `json:"resolution_group_ids,omitempty"`
	Interpretation          MeasurementResolutionInterpretationInput `json:"interpretation"`
	ExcludedOccurrenceIDs   []string                                 `json:"excluded_occurrence_ids,omitempty"`
	ExcludedItemIDs         []string                                 `json:"excluded_item_ids,omitempty"`
	ExpectedPreviewRevision uint64                                   `json:"expected_preview_revision" minimum:"1"`
}

// MeasurementResolutionPlanApplyInput identifies a plan revision to apply.
type MeasurementResolutionPlanApplyInput struct {
	ExpectedPreviewRevision uint64 `json:"expected_preview_revision" minimum:"1"`
}

// MeasurementResolutionPlanResponse is the API representation of a resolution plan.
type MeasurementResolutionPlanResponse struct {
	PlanID                         string   `json:"plan_id"`
	PreviewRevision                uint64   `json:"preview_revision" minimum:"1"`
	ImpactDigest                   string   `json:"impact_digest"`
	AffectedOccurrences            int      `json:"affected_occurrences" minimum:"0"`
	AffectedIngredients            int      `json:"affected_ingredients" minimum:"0"`
	AffectedRecipes                int      `json:"affected_recipes" minimum:"0"`
	RemainingActionableOccurrences int      `json:"remaining_actionable_occurrences" minimum:"0"`
	UnchangedOccurrences           int      `json:"unchanged_occurrences" minimum:"0"`
	CurrentActionableOccurrences   int      `json:"current_actionable_occurrences" minimum:"0"`
	ResultingActionableOccurrences int      `json:"resulting_actionable_occurrences" minimum:"0"`
	ValidationFailures             []string `json:"validation_failures"`
	// UnresolvableOccurrences keep their authored wording because no answer can
	// settle them. They are reported rather than refusing the plan.
	UnresolvableOccurrences []MeasurementResolutionOccurrence `json:"unresolvable_occurrences"`
	// PendingOccurrences are answered measures inside an ingredient that is not
	// yet complete enough to store. The answers are kept and stop being asked
	// about; the ingredient waits for the rest of its measures.
	PendingOccurrences []MeasurementResolutionOccurrence `json:"pending_occurrences"`
	Examples           []MeasurementResolutionExample    `json:"examples"`
	Occurrences        []MeasurementResolutionOccurrence `json:"occurrences"`
	Preview            *ImportPreviewResponse            `json:"preview,omitempty"`
}

// ImportPreviewCommitResponse is the API response for a committed preview.
type ImportPreviewCommitResponse struct {
	Items     []ImportedRecipe       `json:"items"`
	Remaining *ImportPreviewResponse `json:"remaining,omitempty"`
}

type (
	importInput  struct{ Body ImportInput }
	importOutput struct {
		Body struct {
			Items []ImportedRecipe `json:"items"`
		}
	}
)

// ExchangeService brings recipes in as SREF. It needs the asset store as well
// as the library, because a package carries the bytes of the photographs its
// recipe declares. Export is a separate service with a separate contract; see
// export.go.
type ExchangeService interface {
	ProbeSources(ctx context.Context, inputs []importer.Input) ([]importer.Format, error)
	ImportSources(ctx context.Context, inputs []importer.Input, options importer.Options, policy library.DuplicatePolicy) ([]library.ImportResult, error)
	ImportDocuments(ctx context.Context, documents [][]byte, policy library.DuplicatePolicy) ([]library.ImportResult, error)
	ImportPackage(ctx context.Context, data []byte, policy library.DuplicatePolicy) ([]library.ImportResult, error)
	ImportURL(ctx context.Context, rawURL string, policy library.DuplicatePolicy) ([]library.ImportResult, error)
	PreviewFiles(ctx context.Context, uploads []exchange.UploadedImport) (exchange.ImportPreview, error)
	PreviewURL(ctx context.Context, rawURL string) (exchange.ImportPreview, error)
	PreviewImage(ctx context.Context, previewID, itemID string) (exchange.PreviewAsset, error)
	Preview(ctx context.Context, previewID string) (exchange.ImportPreview, error)
	MeasurementResolutionGroup(ctx context.Context, previewID, groupID string) (exchange.MeasurementResolutionGroupDetail, error)
	CreateMeasurementResolutionPlan(ctx context.Context, previewID string, request exchange.MeasurementResolutionPlanRequest) (exchange.MeasurementResolutionPlan, error)
	ApplyMeasurementResolutionPlan(ctx context.Context, previewID, planID string, expectedRevision uint64) (exchange.MeasurementResolutionPlan, error)
	RevertMeasurementResolutionPlan(ctx context.Context, previewID, planID string, expectedRevision uint64) (exchange.ImportPreview, error)
	CommitPreview(ctx context.Context, previewID string, itemIDs []string, policy library.DuplicatePolicy) (exchange.ImportCommitResult, error)
	PlanPreviewImport(ctx context.Context, previewID string, selections []exchange.PreviewResolution, resolved bool) (exchange.ImportPlan, error)
	ReconcilePreview(ctx context.Context, previewID string, selections []exchange.PreviewResolution) (exchange.ImportCommitResult, error)
	CancelPreviewScoped(ctx context.Context, previewID string) error
}

func registerExchange(api huma.API, exchanger ExchangeService) {
	huma.Register(api, huma.Operation{
		OperationID: "preview-recipe-url", Method: http.MethodPost, Path: "/api/v1/recipes/import-url/preview",
		Summary:     "Preview a web page",
		Description: "Fetches and interprets a public recipe page without changing the library. The response uses the same candidate and diagnostic model as uploaded JSON-LD and SREF files.",
		Tags:        []string{"Import"}, Errors: []int{413, 422, 502, 503, 504},
	}, func(ctx context.Context, input *importURLInput) (*importPreviewOutput, error) {
		preview, err := exchanger.PreviewURL(ctx, input.Body.URL)
		if err != nil {
			return nil, mapImportError(err)
		}

		return &importPreviewOutput{Body: importPreviewResponse(preview)}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "import-recipe-url", Method: http.MethodPost, Path: "/api/v1/recipes/import-url",
		Summary: "Import a web page",
		Description: "Fetches a public recipe page under Savorum's outbound policy and extracts its recipe. " +
			"Structured data is read tolerantly: a page with one unreadable block and one good one still imports. " +
			"A page that refused to answer, a page with no recipe on it, and a page whose recipe has no ingredients " +
			"or method are reported as three different problems, because they are.",
		Tags: []string{"Import"}, Errors: []int{409, 422, 502, 504},
	}, func(ctx context.Context, input *importURLInput) (*importOutput, error) {
		results, err := exchanger.ImportURL(ctx, input.Body.URL, library.DuplicatePolicy(input.Body.OnDuplicate))
		if err != nil {
			return nil, mapImportError(err)
		}

		return importResults(results), nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "import-recipes", Method: http.MethodPost, Path: "/api/v1/recipes/import",
		Summary:     "Import SREF documents",
		Description: "Reads SREF recipe documents. Every recipe is validated before any is written. A recipe already in this library is reported rather than duplicated unless on_duplicate says otherwise. A recipe with photographs travels as a `.sref` package and is uploaded to `/api/v1/recipes/import-package`.",
		Tags:        []string{"Import"}, DefaultStatus: 200, Errors: []int{409, 422},
	}, func(ctx context.Context, input *importInput) (*importOutput, error) {
		documents := make([][]byte, 0, len(input.Body.Recipes))
		for _, document := range input.Body.Recipes {
			documents = append(documents, append([]byte(nil), document...))
		}

		results, err := exchanger.ImportDocuments(ctx, documents, library.DuplicatePolicy(input.Body.OnDuplicate))
		if err != nil {
			return nil, mapImportError(err)
		}

		return importResults(results), nil
	})
}

// ImportURLInput supplies a URL to import.
type ImportURLInput struct {
	URL         string `json:"url" format:"uri"`
	OnDuplicate string `json:"on_duplicate,omitempty" enum:"reject,skip,replace"`
}
type (
	importURLInput      struct{ Body ImportURLInput }
	importPreviewOutput struct{ Body ImportPreviewResponse }
)

func importResults(results []library.ImportResult) *importOutput {
	output := &importOutput{}

	output.Body.Items = make([]ImportedRecipe, 0, len(results))
	for _, result := range results {
		output.Body.Items = append(output.Body.Items, ImportedRecipe{
			LibraryEntryID: string(result.EntryID), RecipeID: string(result.RecipeID),
			ImportEventID: result.ImportEventID, Title: result.Title, Revision: result.Revision,
			Outcome: string(result.Outcome), DroppedAssets: result.DroppedAssets, DeferredAssets: result.DeferredAssets,
			Warnings: result.Warnings,
		})
	}

	return output
}

func importPreviewResponse(preview exchange.ImportPreview) ImportPreviewResponse {
	response := ImportPreviewResponse{
		ID: preview.ID, ExpiresAt: preview.ExpiresAt.UTC().Format(time.RFC3339), PreviewRevision: preview.Revision,
		Items:               make([]ImportPreviewItem, 0, len(preview.Items)),
		ResolutionProfiles:  make([]MeasurementResolutionProfile, 0, len(preview.ResolutionProfiles)),
		ResolutionGroups:    make([]MeasurementResolutionGroup, 0, len(preview.ResolutionGroups)),
		ResolutionDecisions: make([]MeasurementResolutionDecision, 0, len(preview.ResolutionDecisions)),
	}
	for _, profile := range preview.ResolutionProfiles {
		response.ResolutionProfiles = append(response.ResolutionProfiles, MeasurementResolutionProfile{
			ID: profile.ID, Label: profile.Label, Selects: profile.Selects,
			Unanswered: profile.Unanswered, UnitRegistryVersion: profile.UnitRegistryVersion,
		})
	}

	for _, group := range preview.ResolutionGroups {
		response.ResolutionGroups = append(response.ResolutionGroups, measurementResolutionGroup(group))
	}

	for _, decision := range preview.ResolutionDecisions {
		response.ResolutionDecisions = append(response.ResolutionDecisions, measurementResolutionDecision(decision))
	}

	for _, item := range preview.Items {
		mapped := ImportPreviewItem{
			ID:          item.ID,
			Recipe:      previewRecipe(item.Recipe, item.Source.Filename),
			Diagnostics: make([]ImportPreviewDiagnostic, 0, len(item.Diagnostics)),
			Source: ImportPreviewSource{
				FormatID: item.Source.FormatID, FormatName: item.Source.FormatName,
				Filename: item.Source.Filename, URL: item.Source.URL, Attribution: item.Source.Attribution, CanRetain: item.Source.CanRetain,
				ExtractedText: item.Source.ExtractedText,
			},
			Analysis: ImportPreviewAnalysis{
				Status:  string(item.Analysis.Status),
				Matches: make([]ImportPreviewMatch, 0, len(item.Analysis.Matches)),
			},
			Assets: ImportPreviewAssets{
				Discovered: item.Assets.Discovered, Available: item.Assets.Available, Deferred: item.Assets.Deferred,
				Failed: item.Assets.Failed, Unavailable: item.Assets.Unavailable,
			},
			AlreadyHere: item.AlreadyHere, CanCommit: item.CanCommit,
		}
		// Retrieval is on demand, so offer the image whenever one may exist.
		if item.Assets.Discovered > item.Assets.Unavailable {
			mapped.Assets.PreviewURL = "/api/v1/recipes/import-previews/" + preview.ID + "/items/" + item.ID + "/image"
		}

		for _, match := range item.Analysis.Matches {
			mapped.Analysis.Matches = append(mapped.Analysis.Matches, ImportPreviewMatch{
				EntryID: string(match.EntryID), Revision: match.Revision, RecipeID: string(match.RecipeID), Title: match.Title,
				Authors: append([]string(nil), match.Authors...), Yield: match.Yield,
				IngredientCount: match.IngredientCount, Evidence: string(match.Evidence), Score: match.Score,
			})
		}

		for _, diagnostic := range item.Diagnostics {
			targets := make([]ImportDiagnosticTarget, 0, len(diagnostic.Targets))
			for _, target := range diagnostic.Targets {
				mappedTarget := ImportDiagnosticTarget{
					ObjectID: target.ObjectID, Field: target.Field, Evidence: target.Evidence,
					Preserved: target.Preserved, Action: target.Action,
				}
				if target.Resolution != nil {
					mappedTarget.Resolution = &ImportResolutionDescriptor{
						ID: target.Resolution.ID, Kind: target.Resolution.Kind, AuthoredForm: target.Resolution.AuthoredForm,
						SourceStart: target.Resolution.SourceStart, SourceEnd: target.Resolution.SourceEnd,
						CandidateUnitIDs: append([]string(nil), target.Resolution.CandidateUnitIDs...), ResolutionKey: target.Resolution.ResolutionKey,
					}
				}

				targets = append(targets, mappedTarget)
			}

			mapped.Diagnostics = append(mapped.Diagnostics, ImportPreviewDiagnostic{
				Code: diagnostic.Code, Class: string(diagnostic.Class), Severity: string(diagnostic.Severity),
				Area: diagnostic.Area, Message: diagnostic.Message, Count: diagnostic.Count, Targets: targets,
			})
		}

		response.Items = append(response.Items, mapped)
	}

	return response
}

func measurementResolutionGroup(group exchange.MeasurementResolutionGroup) MeasurementResolutionGroup {
	result := MeasurementResolutionGroup{
		ID: group.ID, Kind: group.Kind, AuthoredForm: group.AuthoredForm, Occurrences: group.Occurrences,
		Ingredients: group.Ingredients, Recipes: group.Recipes, SourceLabel: group.SourceLabel,
		CandidateUnitIDs: append(make([]string, 0, len(group.CandidateUnitIDs)), group.CandidateUnitIDs...),
		Choices:          make([]MeasurementResolutionChoice, 0, len(group.Choices)),
		Subgroups:        make([]MeasurementResolutionSubgroup, 0, len(group.Subgroups)),
		Examples:         make([]MeasurementResolutionExample, 0, len(group.Examples)),
	}
	for _, choice := range group.Choices {
		result.Choices = append(result.Choices, measurementResolutionChoice(choice))
	}

	for _, subgroup := range group.Subgroups {
		result.Subgroups = append(result.Subgroups, MeasurementResolutionSubgroup{SourceText: subgroup.SourceText, Occurrences: subgroup.Occurrences, Recipes: subgroup.Recipes})
	}

	for _, example := range group.Examples {
		result.Examples = append(result.Examples, measurementResolutionExample(example))
	}

	return result
}

func measurementResolutionChoice(choice exchange.MeasurementResolutionChoice) MeasurementResolutionChoice {
	return MeasurementResolutionChoice{
		UnitID: choice.UnitID, Label: choice.Label, Convention: choice.Convention,
		ConventionLabel: choice.ConventionLabel, Magnitude: choice.Magnitude,
	}
}

func measurementResolutionDecision(decision exchange.MeasurementResolutionDecision) MeasurementResolutionDecision {
	result := MeasurementResolutionDecision{
		ID: decision.ID, SourceLabel: decision.SourceLabel, MeasureLabel: decision.MeasureLabel,
		AuthoredForms:    append(make([]string, 0, len(decision.AuthoredForms)), decision.AuthoredForms...),
		GroupIDs:         append(make([]string, 0, len(decision.GroupIDs)), decision.GroupIDs...),
		Occurrences:      decision.Occurrences,
		Ingredients:      decision.Ingredients,
		Recipes:          decision.Recipes,
		CandidateUnitIDs: append(make([]string, 0, len(decision.CandidateUnitIDs)), decision.CandidateUnitIDs...),
		Choices:          make([]MeasurementResolutionChoice, 0, len(decision.Choices)),
		Examples:         make([]MeasurementResolutionExample, 0, len(decision.Examples)),
	}
	for _, choice := range decision.Choices {
		result.Choices = append(result.Choices, measurementResolutionChoice(choice))
	}

	for _, example := range decision.Examples {
		result.Examples = append(result.Examples, measurementResolutionExample(example))
	}

	return result
}

func measurementResolutionExample(example exchange.MeasurementResolutionExample) MeasurementResolutionExample {
	return MeasurementResolutionExample{RecipeTitle: example.RecipeTitle, SourceText: example.SourceText, Before: example.Before, After: example.After}
}

func measurementResolutionOccurrence(occurrence exchange.MeasurementResolutionOccurrence) MeasurementResolutionOccurrence {
	return MeasurementResolutionOccurrence{
		ID: occurrence.ID, GroupID: occurrence.GroupID, ItemID: occurrence.ItemID, RecipeID: occurrence.RecipeID,
		RecipeTitle: occurrence.RecipeTitle, IngredientID: occurrence.IngredientID, SourceText: occurrence.SourceText,
		AuthoredForm:     occurrence.AuthoredForm,
		CandidateUnitIDs: append(make([]string, 0, len(occurrence.CandidateUnitIDs)), occurrence.CandidateUnitIDs...),
		ResolutionKey:    occurrence.ResolutionKey, Fingerprint: occurrence.Fingerprint,
	}
}

func measurementResolutionPlanResponse(plan exchange.MeasurementResolutionPlan) MeasurementResolutionPlanResponse {
	result := MeasurementResolutionPlanResponse{
		PlanID: plan.ID, PreviewRevision: plan.PreviewRevision, ImpactDigest: plan.ImpactDigest,
		AffectedOccurrences: plan.AffectedOccurrences, AffectedIngredients: plan.AffectedIngredients,
		AffectedRecipes:                plan.AffectedRecipes,
		RemainingActionableOccurrences: plan.RemainingActionableOccurrences, UnchangedOccurrences: plan.UnchangedOccurrences,
		CurrentActionableOccurrences: plan.CurrentActionableOccurrences, ResultingActionableOccurrences: plan.ResultingActionableOccurrences,
		ValidationFailures:      append(make([]string, 0, len(plan.ValidationFailures)), plan.ValidationFailures...),
		Examples:                make([]MeasurementResolutionExample, 0, len(plan.Examples)),
		Occurrences:             make([]MeasurementResolutionOccurrence, 0, len(plan.Occurrences)),
		UnresolvableOccurrences: make([]MeasurementResolutionOccurrence, 0, len(plan.UnresolvableOccurrences)),
		PendingOccurrences:      make([]MeasurementResolutionOccurrence, 0, len(plan.PendingOccurrences)),
	}
	for _, example := range plan.Examples {
		result.Examples = append(result.Examples, measurementResolutionExample(example))
	}

	for _, occurrence := range plan.Occurrences {
		result.Occurrences = append(result.Occurrences, measurementResolutionOccurrence(occurrence))
	}

	for _, occurrence := range plan.UnresolvableOccurrences {
		result.UnresolvableOccurrences = append(result.UnresolvableOccurrences, measurementResolutionOccurrence(occurrence))
	}

	for _, occurrence := range plan.PendingOccurrences {
		result.PendingOccurrences = append(result.PendingOccurrences, measurementResolutionOccurrence(occurrence))
	}

	if plan.Preview != nil {
		preview := importPreviewResponse(*plan.Preview)
		result.Preview = &preview
	}

	return result
}

func previewRecipe(document recipe.Document, fallback string) ImportPreviewRecipe {
	result := ImportPreviewRecipe{Title: document.Title, Description: document.Description}
	if result.Title == "" {
		result.Title = fallback
	}

	if first := document.FirstYield(); first != nil {
		result.Yield = first.Text
	}

	for _, section := range document.IngredientSections {
		mapped := ImportPreviewSection{Title: section.Title}
		for _, ingredient := range section.Ingredients {
			text := strings.TrimSpace(ingredient.SourceText)
			if text == "" {
				text = strings.TrimSpace(strings.Join([]string{ingredient.Name, ingredient.Note}, " "))
			}

			if text != "" {
				mapped.Items = append(mapped.Items, text)
			}
		}

		if len(mapped.Items) > 0 {
			result.Ingredients = append(result.Ingredients, mapped)
			result.IngredientCount += len(mapped.Items)
		}
	}

	for _, section := range document.InstructionSections {
		mapped := ImportPreviewSection{Title: section.Title}
		for _, step := range section.Steps {
			text := strings.TrimSpace(step.Text)
			if text == "" {
				text = strings.TrimSpace(step.Title)
			}

			if text != "" {
				mapped.Items = append(mapped.Items, text)
			}
		}

		if len(mapped.Items) > 0 {
			result.Instructions = append(result.Instructions, mapped)
			result.StepCount += len(mapped.Items)
		}
	}

	return result
}

// streamPackageImport accepts an uploaded `.sref` package. It is a raw handler
// because the body is an archive rather than JSON.
func streamPackageImport(exchanger ExchangeService) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		contentType := strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0])
		if contentType != "application/zip" && contentType != "application/octet-stream" {
			writeAssetHTTPError(response, http.StatusUnsupportedMediaType, "package-content-type",
				"A package upload uses Content-Type: application/zip")

			return
		}

		policy := library.DuplicatePolicy(request.URL.Query().Get("on_duplicate"))
		request.Body = http.MaxBytesReader(response, request.Body, httpsecurity.PackageImportBodyBytes)

		data, err := io.ReadAll(request.Body)
		if err != nil {
			writeAssetHTTPError(response, http.StatusRequestEntityTooLarge, "package-too-large",
				"The package exceeds the configured size limit")

			return
		}

		results, err := exchanger.ImportPackage(request.Context(), data, policy)
		if err != nil {
			status, kind, detail := importHTTPError(err)
			writeAssetHTTPError(response, status, kind, detail)

			return
		}

		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(importResults(results).Body)
	}
}

type importErrorFormatter[T any] func(status int, kind, detail string, params map[string]any) T

// importHTTPError maps a failure for the raw package-upload handler, which
// writes its own problem document because its body is an archive rather than
// JSON.
func importHTTPError(err error) (int, string, string) {
	type tuple struct {
		status int
		kind   string
		detail string
	}

	result := mapImportErrorAs(err, func(status int, kind, detail string, _ map[string]any) tuple {
		return tuple{status: status, kind: kind, detail: detail}
	})

	return result.status, result.kind, result.detail
}

func mapImportErrorAs[T any](err error, format importErrorFormatter[T]) T {
	if exhausted := storageProblem(err); exhausted != nil {
		return format(exhausted.Status, exhausted.Code, exhausted.Detail, nil)
	}

	if denied := accessProblem(err); denied != nil {
		return format(denied.Status, denied.Code, denied.Detail, nil)
	}

	if failure, ok := errors.AsType[*webrecipe.Failure](err); ok {
		if problemKind, known := webImportProblems[failure.Code]; known {
			return format(problemKind.status, problemKind.kind, textutil.Capitalize(failure.Message), nil)
		}

		message := textutil.Capitalize(failure.Message)

		return format(http.StatusUnprocessableEntity, "import-invalid", message, map[string]any{"diagnostic": message})
	}

	var duplicate *library.DuplicateError
	switch {
	case errors.As(err, &duplicate):
		return format(http.StatusConflict, "recipe-already-here",
			"This library already holds “"+duplicate.Title+"”. Choose whether to keep what is here, replace it, or leave it out.", nil)
	case errors.Is(err, library.ErrImportTargetChanged):
		return format(http.StatusConflict, "import-preview-stale", "A recipe changed after this preview. Refresh the import and review the current recipe before choosing again.", nil)
	case errors.Is(err, exchange.ErrResolutionPlanStale):
		return format(http.StatusConflict, "measurement-resolution-stale", "This import preview changed after the measurement plan was generated. Review a fresh plan before applying it.", nil)
	case errors.Is(err, exchange.ErrResolutionPlanInvalid):
		return format(http.StatusUnprocessableEntity, "measurement-resolution-invalid", err.Error(), nil)
	case errors.Is(err, sref.ErrUnsafePackage):
		return format(http.StatusUnprocessableEntity, "package-unsafe", "The package could not be read safely: "+err.Error(), nil)
	case errors.Is(err, sref.ErrUnsupportedVersion):
		return format(http.StatusUnprocessableEntity, "sref-version-unsupported", err.Error(), nil)
	case errors.Is(err, sref.ErrInvalidDocument):
		return format(http.StatusUnprocessableEntity, "sref-invalid", "The SREF recipe document is invalid.", nil)
	case errors.Is(err, library.ErrNotFound):
		return format(http.StatusNotFound, "recipe-not-found", "The requested recipe does not exist", nil)
	case errors.Is(err, library.ErrInvalidState):
		return format(http.StatusConflict, "import-state-conflict", "A recipe this import would replace is in the trash. Restore it first.", nil)
	case errors.Is(err, library.ErrInvalidOrganization):
		return format(http.StatusUnprocessableEntity, "import-invalid", "The import could not be read as a set of recipes this library can hold", nil)
	case errors.Is(err, exchange.ErrPreviewImagePending):
		return format(http.StatusNotFound, "import-preview-image-pending",
			"This photograph has not been retrieved yet. Savorum fetches it when the recipe is imported.", nil)
	case errors.Is(err, exchange.ErrPreviewImageUnavailable):
		return format(http.StatusNotFound, "import-preview-image-unavailable",
			"This photograph could not be retrieved. The recipe keeps its address so it can be tried again.", nil)
	case errors.Is(err, exchange.ErrPreviewNotFound):
		return format(http.StatusNotFound, "import-preview-expired", "This import preview has expired. Choose the source again.", nil)
	case errors.Is(err, exchange.ErrPreviewTooLarge):
		return format(http.StatusRequestEntityTooLarge, "import-preview-too-large", "That is more than Savorum can review at once. Choose a smaller source.", nil)
	case errors.Is(err, exchange.ErrPreviewBusy):
		return format(http.StatusServiceUnavailable, "import-preview-busy", "Savorum is already reviewing as many imports as it can hold. Finish or close one, then try again.", nil)
	case errors.Is(err, exchange.ErrPreviewState):
		return format(http.StatusUnprocessableEntity, "import-preview-invalid", err.Error(), nil)
	default:
		return format(http.StatusUnprocessableEntity, "import-invalid", err.Error(), nil)
	}
}

// webImportProblems gives every extraction failure its own status and wording,
// because each has a different remedy.
var webImportProblems = map[webrecipe.FailureCode]struct {
	status int
	kind   string
}{
	webrecipe.FetchRejected:           {422, "import-url-rejected"},
	webrecipe.FetchBlocked:            {502, "import-url-blocked"},
	webrecipe.FetchTimeout:            {504, "import-url-timeout"},
	webrecipe.FetchTooLarge:           {422, "import-url-too-large"},
	webrecipe.UnsupportedContentType:  {422, "import-url-unsupported-type"},
	webrecipe.NoRecipeStructuredData:  {422, "import-no-recipe-found"},
	webrecipe.MalformedStructuredData: {422, "import-recipe-unreadable"},
	webrecipe.ExtractionLimited:       {422, "import-recipe-too-complex"},
	webrecipe.RecipeIncomplete:        {422, "import-recipe-incomplete"},
	webrecipe.NormalizationFailed:     {422, "import-recipe-unreadable"},
}

func mapImportError(err error) error {
	return mapImportErrorAs(err, func(status int, kind, detail string, params map[string]any) error {
		result := problem(status, kind, detail)
		if params != nil {
			result.Params = params
		}

		return result
	})
}
