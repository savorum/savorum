package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
)

// PlannedImport is one recipe's import, detached from the expiring review
// session so a background job can own it. The live half (decoded documents,
// photographs already fetched) is fast; the payload is the durable half,
// enough to redo the item after a restart.
type PlannedImport struct {
	ItemID string
	Title  string
	Source ImportPreviewSource
	// ReviewRequired is carried over from the preview. An unfetched photograph
	// does not require review.
	ReviewRequired bool
	Resolution     *library.ImportResolution
	RetainSource   bool

	item             ImportItem
	metadata         importer.LibraryMetadata
	diagnostics      []importer.Diagnostic
	importerMetadata map[string]any
	payload          []byte
}

// Payload is the item's resumable work, or nil when this item cannot be redone
// without the original upload. A job stores it as written; nothing else reads
// it.
func (planned PlannedImport) Payload() []byte { return planned.payload }

// Resumable reports whether a later process could run this item from its
// payload alone.
func (planned PlannedImport) Resumable() bool { return len(planned.payload) > 0 }

// Diagnostics are what the source adapter had to say about this recipe. They
// travel with the planned item so the job's record of it is as complete as the
// preview's was.
func (planned PlannedImport) Diagnostics() []importer.Diagnostic {
	return append([]importer.Diagnostic(nil), planned.diagnostics...)
}

// MeasurementResolutionRecords decodes an independent copy of the item's stored
// resolution decisions. Missing or malformed metadata yields nil.
func (planned PlannedImport) MeasurementResolutionRecords() []MeasurementResolutionRecord {
	return measurementResolutionRecords(planned.importerMetadata)
}

// ImportPlan is a whole selection, ready to be given to a job.
type ImportPlan struct {
	// Importer names the formats the selection came from, for the reader.
	Importer string
	Source   string
	Items    []PlannedImport
	// Remaining is the preview as it stands with the planned recipes taken out
	// of it, so a reviewer can keep working through the rest of a large queue
	// while the first selection imports.
	Remaining *ImportPreview
}

// plannedPayload is Savorum's private record of an item's work. The recipe is
// stored as SREF, the encoding kept readable across versions.
type plannedPayload struct {
	History          library.ImportContext    `json:"history"`
	ItemID           string                   `json:"item_id"`
	Title            string                   `json:"title"`
	ReviewRequired   bool                     `json:"review_required,omitempty"`
	Source           ImportPreviewSource      `json:"source"`
	Document         json.RawMessage          `json:"document"`
	RemoteAssets     []importer.RemoteAsset   `json:"remote_assets,omitempty"`
	Metadata         importer.LibraryMetadata `json:"library_metadata"`
	Diagnostics      []importer.Diagnostic    `json:"diagnostics,omitempty"`
	ImporterMetadata map[string]any           `json:"importer_metadata,omitempty"`
	Resolution       *plannedResolution       `json:"resolution,omitempty"`
	SourceBytes      []byte                   `json:"source_bytes,omitempty"`
	SourceFilename   string                   `json:"source_filename,omitempty"`
	SourceMediaType  string                   `json:"source_media_type,omitempty"`
}

type plannedResolution struct {
	Action         library.ImportResolutionAction `json:"action"`
	TargetEntryID  library.EntryID                `json:"target_entry_id,omitempty"`
	TargetRevision uint64                         `json:"target_revision,omitempty"`
	TargetRecipeID recipe.RecipeID                `json:"target_recipe_id,omitempty"`
	MatchEvidence  library.ImportMatchEvidence    `json:"match_evidence,omitempty"`
}

// PlanPreviewImport takes the selected recipes out of a staged preview and
// returns them as durable work. They leave the session immediately so they
// cannot be imported twice.
func (s *Service) PlanPreviewImport(ctx context.Context, previewID string, selections []PreviewResolution, resolved bool) (ImportPlan, error) {
	return s.PlanPreviewImportWithOptions(ctx, previewID, selections, resolved, PreviewCommitOptions{})
}

// PlanPreviewImportWithOptions claims selected candidates and returns a durable, executable import plan.
func (s *Service) PlanPreviewImportWithOptions(ctx context.Context, previewID string, selections []PreviewResolution, resolved bool, options PreviewCommitOptions) (ImportPlan, error) {
	claim, err := s.claimPreviewSelections(ctx, previewID, selections, resolved)
	if err != nil {
		return ImportPlan{}, err
	}

	if len(options.RetainSourceItemIDs) > 0 {
		if _, ok := s.library.(atomicSourceLibrary); !ok {
			s.releasePreviewClaim(previewID, claim.session)
			return ImportPlan{}, fmt.Errorf("retain import source: library does not support atomic source attachments")
		}
	}

	plan, formats, filenames, staged, err := buildImportPlan(claim, options, resolved)
	if err != nil {
		s.releasePreviewClaim(previewID, claim.session)
		return ImportPlan{}, err
	}
	// Every planned item shares one staged map, as a synchronous commit does,
	// so photographs fetched for the preview are not fetched again.
	for index := range plan.Items {
		plan.Items[index].item.stagedRemote = staged
	}

	plan.Importer = joinNames(formats)

	plan.Source = joinNames(filenames)
	if plan.Source == "" {
		plan.Source = plan.Importer
	}

	remaining := s.retirePlannedCandidates(previewID, claim)
	plan.Remaining = remaining

	return plan, nil
}

func buildImportPlan(claim claimedSelection, options PreviewCommitOptions, resolved bool) (ImportPlan, map[string]bool, map[string]bool, map[string]stagedRemoteAsset, error) {
	requestedSources := map[string]bool{}

	for _, rawItemID := range options.RetainSourceItemIDs {
		itemID := strings.TrimSpace(rawItemID)
		if itemID == "" || rawItemID != itemID || requestedSources[itemID] {
			return ImportPlan{}, nil, nil, nil, fmt.Errorf("retain import source: item selection is invalid")
		}

		requestedSources[itemID] = true
	}

	plan := ImportPlan{Items: make([]PlannedImport, 0, len(claim.selected))}
	formats, filenames := map[string]bool{}, map[string]bool{}
	staged := map[string]stagedRemoteAsset{}

	for index, candidate := range claim.selected {
		for _, remote := range candidate.item.RemoteAssets {
			identity, canonicalErr := canonicalRemoteURL(remote.URL)
			if canonicalErr == nil {
				if asset, ok := claim.session.stagedRemote[identity]; ok {
					staged[identity] = asset
				}
			}
		}

		planned, err := buildPlannedImport(candidate, options.RetainSourceItemIDs)
		if err != nil {
			return ImportPlan{}, nil, nil, nil, err
		}

		if resolved {
			resolution := claim.resolutions[index]
			planned.Resolution = &resolution
		}

		planned.payload = encodePlannedPayload(planned)
		plan.Items = append(plan.Items, planned)

		delete(requestedSources, candidate.view.ID)

		if name := candidate.view.Source.FormatName; name != "" {
			formats[name] = true
		}

		if name := candidate.view.Source.Filename; name != "" {
			filenames[name] = true
		}
	}

	if len(requestedSources) != 0 {
		return ImportPlan{}, nil, nil, nil, fmt.Errorf("retain import source: item is not selected")
	}

	return plan, formats, filenames, staged, nil
}

func buildPlannedImport(candidate *previewCandidate, retainedIDs []string) (PlannedImport, error) {
	metadata, err := cloneMetadata(candidate.importerMetadata)
	if err != nil {
		return PlannedImport{}, fmt.Errorf("copy importer metadata for plan: %w", err)
	}

	planned := PlannedImport{
		ItemID: candidate.view.ID, Title: candidate.view.Recipe.Title, Source: candidate.view.Source,
		ReviewRequired: hasWarning(candidate.view.Diagnostics),
		item:           candidate.item, metadata: candidate.metadata, diagnostics: candidate.diagnostics, importerMetadata: metadata,
	}
	if !slices.Contains(retainedIDs, candidate.view.ID) {
		return planned, nil
	}

	if len(candidate.item.SourceBytes) == 0 || candidate.item.SourceFilename == "" {
		return PlannedImport{}, fmt.Errorf("retain import source %q: no uploaded source bytes", candidate.view.ID)
	}

	planned.RetainSource = true
	planned.item.RetainSource = true
	planned.item.SourceBytes = append([]byte(nil), candidate.item.SourceBytes...)

	return planned, nil
}

// retirePlannedCandidates removes the claimed candidates and releases the
// session, returning what is left for review.
func (s *Service) retirePlannedCandidates(previewID string, claim claimedSelection) *ImportPreview {
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	current, present := s.previews[previewID]
	if !present || current != claim.session {
		return nil
	}

	for id := range claim.seen {
		delete(current.candidates, id)
	}

	current.busy = false
	if current.expired {
		s.removePreviewLocked(previewID, current)
		s.previewStats.Expired++

		return nil
	}

	previousBytes := current.retainedBytes
	current.retainedBytes = retainedSessionBytes(current)
	s.previewStats.RetainedBytes += current.retainedBytes - previousBytes

	remaining := previewFromSession(previewID, current)
	if len(remaining.Items) == 0 {
		s.removePreviewLocked(previewID, current)
		s.previewStats.Completed++

		return nil
	}

	return &remaining
}

// ImportPlanned commits one batch of planned recipes through the same
// validation, duplicate, asset and atomic-commit boundary as any other import.
// staging bounds only the outbound photograph work, so a cancelled job stops
// reaching out at once while the commit it is inside finishes safely.
func (s *Service) ImportPlanned(ctx, staging context.Context, planned []PlannedImport, policy library.DuplicatePolicy) ([]library.ImportResult, error) {
	if len(planned) == 0 {
		return nil, fmt.Errorf("an import batch carries at least one recipe")
	}

	items := make([]ImportItem, 0, len(planned))
	resolutions := make([]library.ImportResolution, 0, len(planned))

	resolved := planned[0].Resolution != nil
	for _, entry := range planned {
		if (entry.Resolution != nil) != resolved {
			return nil, fmt.Errorf("an import batch is either reconciled or policy-driven, not both")
		}

		items = append(items, entry.item)
		if entry.Resolution != nil {
			resolutions = append(resolutions, *entry.Resolution)
		}
	}

	if !resolved {
		resolutions = nil
	} else {
		policy = library.DuplicateSkip
	}

	results, err := s.importItems(ctx, staging, items, policy, resolutions)
	if err != nil {
		return nil, err
	}

	metadata := map[recipe.RecipeID]library.ImportedLibraryMetadata{}
	warnings := map[recipe.RecipeID][]string{}

	for index, imported := range results {
		metadata[imported.RecipeID] = toLibraryMetadata(planned[index].metadata)
		warnings[imported.RecipeID] = diagnosticMessages(planned[index].diagnostics)
	}

	results, err = s.finishExternalImport(ctx, results, metadata, warnings)
	if err != nil {
		return nil, err
	}

	return results, nil
}

// RestorePlanned rebuilds an item from what was written down for it. A restored
// item never carries a reconciliation choice: an interrupted batch may already
// have committed, and "import another copy" would then silently duplicate it.
func RestorePlanned(payload []byte) (PlannedImport, error) {
	var stored plannedPayload
	if err := json.Unmarshal(payload, &stored); err != nil {
		return PlannedImport{}, fmt.Errorf("read stored import item: %w", err)
	}

	document, err := sref.Decode(stored.Document)
	if err != nil {
		return PlannedImport{}, fmt.Errorf("read stored import recipe: %w", err)
	}

	return PlannedImport{
		ItemID: stored.ItemID, Title: stored.Title, Source: stored.Source, ReviewRequired: stored.ReviewRequired,
		RetainSource: len(stored.SourceBytes) > 0,
		item:         ImportItem{Document: &document, RemoteAssets: stored.RemoteAssets, History: stored.History, SourceBytes: stored.SourceBytes, SourceFilename: stored.SourceFilename, SourceMediaType: stored.SourceMediaType, RetainSource: len(stored.SourceBytes) > 0},
		metadata:     stored.Metadata, diagnostics: stored.Diagnostics, importerMetadata: stored.ImporterMetadata, payload: payload,
	}, nil
}

// encodePlannedPayload returns nil for items that cannot be persisted for resumption.
// Packages with ephemeral assets and unencodable documents are omitted so they are
// not resumed in an incomplete state.
func encodePlannedPayload(planned PlannedImport) []byte {
	if planned.item.Package || planned.item.Opened != nil || len(planned.item.EmbeddedAssets) > 0 {
		return nil
	}

	if planned.item.Document == nil {
		return nil
	}

	document, err := sref.Encode(*planned.item.Document)
	if err != nil {
		return nil
	}

	stored := plannedPayload{
		History: planned.item.History,
		ItemID:  planned.ItemID, Title: planned.Title, Source: planned.Source,
		ReviewRequired: planned.ReviewRequired,
		Document:       document, RemoteAssets: planned.item.RemoteAssets,
		Metadata: planned.metadata, Diagnostics: planned.diagnostics, ImporterMetadata: planned.importerMetadata,
	}
	if planned.RetainSource && len(planned.item.SourceBytes) > 0 {
		stored.SourceBytes = append([]byte(nil), planned.item.SourceBytes...)
		stored.SourceFilename = planned.item.SourceFilename
		stored.SourceMediaType = planned.item.SourceMediaType
	}

	if planned.Resolution != nil {
		stored.Resolution = &plannedResolution{
			Action: planned.Resolution.Action, TargetEntryID: planned.Resolution.TargetEntryID,
			TargetRevision: planned.Resolution.TargetRevision, TargetRecipeID: planned.Resolution.TargetRecipeID,
			MatchEvidence: planned.Resolution.MatchEvidence,
		}
	}

	encoded, err := json.Marshal(stored)
	if err != nil {
		return nil
	}

	return encoded
}

func joinNames(names map[string]bool) string {
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}

	sort.Strings(ordered)

	switch len(ordered) {
	case 0:
		return ""
	case 1:
		return ordered[0]
	case 2:
		return ordered[0] + " and " + ordered[1]
	default:
		return fmt.Sprintf("%s and %d others", ordered[0], len(ordered)-1)
	}
}
