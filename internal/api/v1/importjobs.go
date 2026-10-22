package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/savorum/savorum/internal/idempotency"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/importjob"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/textutil"
)

// ImportJobService is the durable half of importing: a bulk import outlives
// the request that started it.
type ImportJobService interface {
	Start(ctx context.Context, request importjob.StartRequest) (importjob.Job, error)
	Get(ctx context.Context, id string) (importjob.Job, error)
	List(ctx context.Context, options importjob.ListOptions) ([]importjob.Job, int, error)
	Items(ctx context.Context, jobID string, options importjob.ItemListOptions) ([]importjob.Item, int, error)
	Cancel(ctx context.Context, id string) (importjob.Job, error)
	Resume(ctx context.Context, id string) (importjob.Job, error)
}

// ImportJobProgress is the aggregate a person watches. Finished counts every
// recipe that has stopped moving, whatever it stopped as.
type ImportJobProgress struct {
	Total                     int                          `json:"total" minimum:"0"`
	Pending                   int                          `json:"pending" minimum:"0"`
	Running                   int                          `json:"running" minimum:"0"`
	Imported                  int                          `json:"imported" minimum:"0"`
	Skipped                   int                          `json:"skipped" minimum:"0"`
	Failed                    int                          `json:"failed" minimum:"0"`
	Cancelled                 int                          `json:"cancelled" minimum:"0"`
	ReviewRequired            int                          `json:"review_required" minimum:"0"`
	Resumable                 int                          `json:"resumable" minimum:"0" doc:"Unfinished recipes Savorum still holds enough of to run again"`
	InformationalPreservation int                          `json:"informational_preservation" minimum:"0"`
	WithoutDiagnostics        int                          `json:"without_diagnostics" minimum:"0"`
	DiagnosticOccurrences     int                          `json:"diagnostic_occurrences" minimum:"0"`
	Diagnostics               []ImportJobDiagnosticSummary `json:"diagnostics"`
	Finished                  int                          `json:"finished" minimum:"0"`
	CurrentBatch              int                          `json:"current_batch" minimum:"0" doc:"The one-based batch being processed, zero before work starts"`
	TotalBatches              int                          `json:"total_batches" minimum:"1"`
}

// ImportJobResource is the API representation of an import job.
type ImportJobResource struct {
	ID       string `json:"id"`
	Importer string `json:"importer" doc:"The source formats this import came from, in the reader's words"`
	Source   string `json:"source" doc:"What was chosen: a filename, several, or the format when there was no file"`
	// LeadTitle is the job's first recipe by position, absent until items are
	// recorded.
	LeadTitle string `json:"lead_title,omitempty" doc:"The title of this job's first recipe, for naming the import by what it brought"`
	// interrupted means the process stopped mid-import; it can be resumed.
	State                   string                     `json:"state" enum:"pending,running,completed,completed-with-errors,cancelled,failed,interrupted"`
	OnDuplicate             string                     `json:"on_duplicate" enum:"reject,skip,replace"`
	TotalItems              int                        `json:"total_items" minimum:"0"`
	Error                   string                     `json:"error,omitempty"`
	CreatedAt               string                     `json:"created_at" format:"date-time"`
	StartedAt               string                     `json:"started_at,omitempty" format:"date-time"`
	FinishedAt              string                     `json:"finished_at,omitempty" format:"date-time"`
	CancellationRequestedAt string                     `json:"cancellation_requested_at,omitempty" format:"date-time"`
	Progress                ImportJobProgress          `json:"progress"`
	ResolutionDecisions     []ImportResolutionDecision `json:"resolution_decisions"`
}

// ImportResolutionDecision records a resolution selected for an import item.
type ImportResolutionDecision struct {
	ResolutionKind     string `json:"resolution_kind"`
	InterpretationKind string `json:"interpretation_kind"`
	// DecisionScope is what this record establishes about the scope a person
	// authorized: an occurrence, an ingredient, a review of the wording as it
	// stands, or `unknown` for a record written before the scope was recorded.
	DecisionScope            string   `json:"decision_scope"`
	ScopeIngredientIDs       []string `json:"scope_ingredient_ids,omitempty"`
	ScopeOccurrenceKeys      []string `json:"scope_occurrence_keys,omitempty"`
	ConventionUnitIDs        []string `json:"convention_unit_ids,omitempty"`
	AffectedIngredientCount  int      `json:"affected_ingredient_count" minimum:"0"`
	UnitID                   string   `json:"unit_id,omitempty"`
	Convention               string   `json:"convention,omitempty"`
	ParserVersion            string   `json:"parser_version"`
	UnitRegistryVersion      string   `json:"unit_registry_version"`
	AffectedOccurrenceIDs    []string `json:"affected_occurrence_ids"`
	ExcludedOccurrenceIDs    []string `json:"excluded_occurrence_ids,omitempty"`
	ExcludedItemIDs          []string `json:"excluded_item_ids,omitempty"`
	AffectedRecipeCount      int      `json:"affected_recipe_count" minimum:"0"`
	OriginalActionableCount  int      `json:"original_actionable_count" minimum:"0"`
	ResultingActionableCount int      `json:"resulting_actionable_count" minimum:"0"`
	ImpactDigest             string   `json:"impact_digest"`
}

// ImportJobItemResource is one recipe's row. review_required is independent
// of state.
type ImportJobItemResource struct {
	ImportEventID  string                `json:"import_event_id,omitempty"`
	Position       int                   `json:"position" minimum:"0"`
	Title          string                `json:"title"`
	Filename       string                `json:"filename,omitempty"`
	SourceURL      string                `json:"source_url,omitempty"`
	State          string                `json:"state" enum:"pending,running,imported,skipped,failed,cancelled"`
	ReviewRequired bool                  `json:"review_required"`
	Outcome        string                `json:"outcome,omitempty" enum:"created,replaced,skipped"`
	LibraryEntryID string                `json:"library_entry_id,omitempty"`
	RecipeID       string                `json:"recipe_id,omitempty"`
	Revision       uint64                `json:"revision,omitempty" minimum:"1"`
	DroppedAssets  int                   `json:"dropped_assets" minimum:"0" doc:"Photographs this recipe names that could not be retrieved and were left out"`
	DeferredAssets int                   `json:"deferred_assets" minimum:"0" doc:"Photographs not fetched yet whose addresses the recipe retains"`
	Diagnostics    []ImportJobDiagnostic `json:"diagnostics,omitempty"`
	Error          string                `json:"error,omitempty"`
	StartedAt      string                `json:"started_at,omitempty" format:"date-time"`
	FinishedAt     string                `json:"finished_at,omitempty" format:"date-time"`
}

// ImportJobStartInput supplies the inputs for starting an import job.
type ImportJobStartInput struct {
	ItemIDs             []string                       `json:"item_ids,omitempty" maxItems:"500"`
	Resolutions         []ImportPreviewResolutionInput `json:"resolutions,omitempty" maxItems:"500"`
	OnDuplicate         string                         `json:"on_duplicate,omitempty" enum:"reject,skip,replace"`
	RetainSourceItemIDs []string                       `json:"retain_source_item_ids,omitempty" maxItems:"500" doc:"Uploaded preview item IDs whose original files should be retained"`
}

// ImportJobStartResponse names the job to watch and whatever is left of the
// review queue.
type ImportJobStartResponse struct {
	Job       ImportJobResource      `json:"job"`
	Remaining *ImportPreviewResponse `json:"remaining,omitempty"`
}

type startImportJobInput struct {
	IdempotencyKey string `header:"Idempotency-Key" maxLength:"200"`
	PreviewID      string `path:"preview_id"`
	Body           ImportJobStartInput
}

type startImportJobOutput struct {
	Location string `header:"Location"`
	Body     ImportJobStartResponse
}

type importJobOutput struct{ Body ImportJobResource }

type listImportJobsInput struct {
	Limit  int    `query:"limit" default:"25" minimum:"1" maximum:"100"`
	Offset int    `query:"offset" default:"0" minimum:"0"`
	State  string `query:"state,omitempty" enum:"pending,running,completed,completed-with-errors,cancelled,failed,interrupted"`
}

type listImportJobsOutput struct {
	Body struct {
		Items  []ImportJobResource `json:"items"`
		Total  int                 `json:"total" minimum:"0"`
		Limit  int                 `json:"limit"`
		Offset int                 `json:"offset"`
	}
}

type listImportJobItemsInput struct {
	JobID  string `path:"job_id"`
	Limit  int    `query:"limit" default:"50" minimum:"1" maximum:"200"`
	Offset int    `query:"offset" default:"0" minimum:"0"`
	Filter string `query:"filter,omitempty" enum:"imported,skipped,review-required,informational,failed,pending,cancelled" doc:"Review-required and informational describe consequences rather than storage states"`
}

// ImportJobDiagnostic is a diagnostic produced while processing an import job.
type ImportJobDiagnostic struct {
	Code    string                   `json:"code"`
	Class   string                   `json:"class" enum:"blocking,actionable,informational"`
	Area    string                   `json:"area"`
	Message string                   `json:"message"`
	Count   int                      `json:"count" minimum:"1"`
	Targets []ImportDiagnosticTarget `json:"targets,omitempty"`
}

// ImportJobDiagnosticSummary groups job diagnostics for presentation.
type ImportJobDiagnosticSummary struct {
	Code        string `json:"code"`
	Class       string `json:"class" enum:"blocking,actionable,informational"`
	Area        string `json:"area"`
	Message     string `json:"message"`
	Occurrences int    `json:"occurrences" minimum:"1"`
	Recipes     int    `json:"recipes" minimum:"1"`
}

type listImportJobItemsOutput struct {
	Body struct {
		Items  []ImportJobItemResource `json:"items"`
		Total  int                     `json:"total" minimum:"0"`
		Limit  int                     `json:"limit"`
		Offset int                     `json:"offset"`
	}
}

type importJobIDInput struct {
	JobID string `path:"job_id"`
}

// registerImportJobs puts jobs at their own resource rather than under
// /recipes. A job is not a recipe, and Go's router cannot tell
// /recipes/import-jobs/{id} apart from /recipes/{id}/revisions.
func registerImportJobs(api huma.API, exchanger ExchangeService, jobs ImportJobService, receipts *idempotency.Coordinator) {
	registerImportJobStart(api, exchanger, jobs, receipts)

	huma.Register(api, huma.Operation{
		OperationID: "list-import-jobs", Method: http.MethodGet, Path: "/api/v1/import-jobs",
		Summary:     "List import jobs",
		Description: "Newest first. Progress is counted from the recipes themselves on every read, so it cannot drift from what actually happened.",
		Tags:        []string{"Import jobs"},
	}, func(ctx context.Context, input *listImportJobsInput) (*listImportJobsOutput, error) {
		found, total, err := jobs.List(ctx, importjob.ListOptions{
			Limit: input.Limit, Offset: input.Offset, State: importjob.State(input.State),
		})
		if err != nil {
			return nil, mapImportJobError(err)
		}

		output := &listImportJobsOutput{}

		output.Body.Items = make([]ImportJobResource, 0, len(found))
		for _, job := range found {
			output.Body.Items = append(output.Body.Items, importJobResource(job))
		}

		output.Body.Total, output.Body.Limit, output.Body.Offset = total, input.Limit, input.Offset

		return output, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-import-job", Method: http.MethodGet, Path: "/api/v1/import-jobs/{job_id}",
		Summary: "Get an import job", Tags: []string{"Import jobs"}, Errors: []int{404},
	}, func(ctx context.Context, input *importJobIDInput) (*importJobOutput, error) {
		job, err := jobs.Get(ctx, input.JobID)
		if err != nil {
			return nil, mapImportJobError(err)
		}

		return &importJobOutput{Body: importJobResource(job)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-import-job-items", Method: http.MethodGet,
		Path:    "/api/v1/import-jobs/{job_id}/items",
		Summary: "List import job items",
		Description: "One row per recipe the import was asked to bring in, in the order they were selected. " +
			"Filtering is by what a reader is looking for rather than by storage state.",
		Tags: []string{"Import jobs"}, Errors: []int{404, 422},
	}, func(ctx context.Context, input *listImportJobItemsInput) (*listImportJobItemsOutput, error) {
		items, total, err := jobs.Items(ctx, input.JobID, importjob.ItemListOptions{
			Limit: input.Limit, Offset: input.Offset, Filter: importjob.ItemFilter(input.Filter),
		})
		if err != nil {
			return nil, mapImportJobError(err)
		}

		output := &listImportJobItemsOutput{}

		output.Body.Items = make([]ImportJobItemResource, 0, len(items))
		for _, item := range items {
			output.Body.Items = append(output.Body.Items, importJobItemResource(item))
		}

		output.Body.Total, output.Body.Limit, output.Body.Offset = total, input.Limit, input.Offset

		return output, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cancel-import-job", Method: http.MethodPost,
		Path:    "/api/v1/import-jobs/{job_id}/cancel",
		Summary: "Cancel an import job",
		Description: "Cancellation is cooperative and is not a rollback. Nothing further is scheduled, the batch " +
			"already being written finishes safely, and every recipe already committed stays in the library.",
		Tags: []string{"Import jobs"}, Errors: []int{404, 409},
	}, func(ctx context.Context, input *importJobIDInput) (*importJobOutput, error) {
		job, err := jobs.Cancel(ctx, input.JobID)
		if err != nil {
			return nil, mapImportJobError(err)
		}

		return &importJobOutput{Body: importJobResource(job)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "resume-import-job", Method: http.MethodPost,
		Path:    "/api/v1/import-jobs/{job_id}/resume",
		Summary: "Resume an import job",
		Description: "Runs the recipes an interrupted, cancelled, or partly failed import never finished, from what " +
			"was written down for each of them. A recipe whose bytes only ever existed inside the uploaded file is " +
			"reported as needing that file again rather than retried against nothing.",
		Tags: []string{"Import jobs"}, Errors: []int{404, 409, 422},
	}, func(ctx context.Context, input *importJobIDInput) (*importJobOutput, error) {
		job, err := jobs.Resume(ctx, input.JobID)
		if err != nil {
			return nil, mapImportJobError(err)
		}

		return &importJobOutput{Body: importJobResource(job)}, nil
	})
}

func importJobSelections(input ImportJobStartInput) ([]exchange.PreviewResolution, bool, error) {
	if len(input.Resolutions) > 0 {
		selections := make([]exchange.PreviewResolution, len(input.Resolutions))
		for index, resolution := range input.Resolutions {
			selections[index] = exchange.PreviewResolution{
				ItemID: resolution.ItemID, Action: library.ImportResolutionAction(resolution.Action),
				TargetEntryID: library.EntryID(resolution.TargetEntryID),
			}
		}

		return selections, true, nil
	}

	if len(input.ItemIDs) == 0 {
		return nil, false, problem(http.StatusUnprocessableEntity, "import-preview-invalid", "Choose at least one recipe to import.")
	}

	selections := make([]exchange.PreviewResolution, len(input.ItemIDs))
	for index, itemID := range input.ItemIDs {
		selections[index] = exchange.PreviewResolution{ItemID: itemID}
	}

	return selections, false, nil
}

func importJobResource(job importjob.Job) ImportJobResource {
	diagnostics := make([]ImportJobDiagnosticSummary, 0, len(job.Progress.Diagnostics))
	for _, diagnostic := range job.Progress.Diagnostics {
		diagnostics = append(diagnostics, ImportJobDiagnosticSummary{
			Code: diagnostic.Code, Class: string(diagnostic.Class), Area: diagnostic.Area,
			Message: diagnostic.Message, Occurrences: diagnostic.Occurrences, Recipes: diagnostic.Recipes,
		})
	}

	decisions := make([]ImportResolutionDecision, 0, len(job.ResolutionDecisions))
	for _, decision := range job.ResolutionDecisions {
		decisions = append(decisions, ImportResolutionDecision{
			ResolutionKind: decision.ResolutionKind, InterpretationKind: decision.InterpretationKind,
			DecisionScope: decision.DecisionScope(), ScopeIngredientIDs: decision.ScopeIngredientIDs,
			ScopeOccurrenceKeys: decision.ScopeOccurrenceKeys, ConventionUnitIDs: decision.ConventionUnitIDs,
			AffectedIngredientCount: decision.AffectedIngredientCount,
			UnitID:                  decision.UnitID, Convention: decision.Convention, ParserVersion: decision.ParserVersion,
			UnitRegistryVersion: decision.UnitRegistryVersion, AffectedOccurrenceIDs: decision.AffectedOccurrenceIDs,
			ExcludedOccurrenceIDs: decision.ExcludedOccurrenceIDs, ExcludedItemIDs: decision.ExcludedItemIDs,
			AffectedRecipeCount: decision.AffectedRecipeCount, OriginalActionableCount: decision.OriginalActionableCount,
			ResultingActionableCount: decision.ResultingActionableCount, ImpactDigest: decision.ImpactDigest,
		})
	}

	return ImportJobResource{
		ID: job.ID, Importer: job.Importer, Source: job.Source, LeadTitle: job.LeadTitle, State: string(job.State),
		OnDuplicate: string(job.DuplicatePolicy), TotalItems: job.TotalItems, Error: job.Error,
		CreatedAt: job.CreatedAt.UTC().Format(time.RFC3339),
		StartedAt: optionalInstant(job.StartedAt), FinishedAt: optionalInstant(job.FinishedAt),
		CancellationRequestedAt: optionalInstant(job.CancellationRequestedAt),
		ResolutionDecisions:     decisions,
		Progress: ImportJobProgress{
			Total: job.Progress.Total, Pending: job.Progress.Pending, Running: job.Progress.Running,
			Imported: job.Progress.Imported, Skipped: job.Progress.Skipped, Failed: job.Progress.Failed,
			Cancelled: job.Progress.Cancelled, ReviewRequired: job.Progress.ReviewRequired,
			Resumable:                 job.Progress.Resumable,
			InformationalPreservation: job.Progress.InformationalPreservation,
			WithoutDiagnostics:        job.Progress.WithoutDiagnostics,
			DiagnosticOccurrences:     job.Progress.DiagnosticOccurrences,
			Diagnostics:               diagnostics,
			Finished:                  job.Progress.Finished(),
			CurrentBatch:              currentImportBatch(job), TotalBatches: importBatchCount(job),
		},
	}
}

func importBatchCount(job importjob.Job) int {
	if job.Progress.Total <= 0 {
		return 1
	}

	batchSize := max(job.BatchSize, 1)

	return (job.Progress.Total + batchSize - 1) / batchSize
}

func currentImportBatch(job importjob.Job) int {
	if job.State == importjob.StatePending || job.Progress.Total <= 0 {
		return 0
	}

	completedOrRunning := job.Progress.Finished() + job.Progress.Running
	if completedOrRunning <= 0 {
		return 1
	}

	batchSize := max(job.BatchSize, 1)
	batch := (completedOrRunning + batchSize - 1) / batchSize

	return min(batch, importBatchCount(job))
}

func importJobItemResource(item importjob.Item) ImportJobItemResource {
	diagnostics := make([]ImportJobDiagnostic, 0, len(item.Diagnostics))
	for _, diagnostic := range item.Diagnostics {
		targets := make([]ImportDiagnosticTarget, 0, len(diagnostic.Targets))
		for _, target := range diagnostic.Targets {
			targets = append(targets, ImportDiagnosticTarget{
				ObjectID: target.ObjectID, Field: target.Field, Evidence: target.Evidence,
				Preserved: target.Preserved, Action: target.Action,
			})
		}

		diagnostics = append(diagnostics, ImportJobDiagnostic{
			Code: diagnostic.Code, Class: string(diagnostic.EffectiveClass()), Area: diagnostic.Area,
			Message: diagnostic.Message, Count: diagnostic.Occurrences(), Targets: targets,
		})
	}

	return ImportJobItemResource{
		ImportEventID: item.ImportEventID, Position: item.Position, Title: item.Title, Filename: item.Filename, SourceURL: item.SourceURL,
		State: string(item.State), ReviewRequired: item.ReviewRequired, Outcome: string(item.Outcome),
		LibraryEntryID: string(item.EntryID), RecipeID: item.RecipeID, Revision: item.Revision,
		DroppedAssets: item.DroppedAssets, DeferredAssets: item.DeferredAssets,
		Diagnostics: diagnostics, Error: item.Error,
		StartedAt: optionalInstant(item.StartedAt), FinishedAt: optionalInstant(item.FinishedAt),
	}
}

func optionalInstant(value *time.Time) string {
	if value == nil {
		return ""
	}

	return value.UTC().Format(time.RFC3339)
}

func mapImportJobError(err error) error {
	if exhausted := storageProblem(err); exhausted != nil {
		return exhausted
	}

	if denied := accessProblem(err); denied != nil {
		return denied
	}

	switch {
	case errors.Is(err, importjob.ErrTooLarge):
		return problem(http.StatusRequestEntityTooLarge, "import-too-large", "Choose fewer recipes for this import; the selection exceeds the available memory allowance.")
	case errors.Is(err, importjob.ErrCapacity):
		return problem(http.StatusServiceUnavailable, "import-busy", "Other imports are using the available capacity. Try again shortly.")
	case errors.Is(err, importjob.ErrNotFound):
		return problem(http.StatusNotFound, "import-job-not-found", "That import does not exist.")
	case errors.Is(err, importjob.ErrNotResumable):
		return problem(http.StatusUnprocessableEntity, "import-job-not-resumable",
			"Savorum did not keep a copy of the recipes this import did not finish. Each unfinished recipe now says what it needs to be imported again.")
	case errors.Is(err, importjob.ErrJobState):
		return problem(http.StatusConflict, "import-job-state", textutil.Capitalize(unwrapJobState(err)))
	default:
		return mapImportError(err)
	}
}

// unwrapJobState keeps the specific sentence and drops the sentinel's prefix,
// which says nothing a reader did not already know from the status.
func unwrapJobState(err error) string {
	message := err.Error()
	if prefix := importjob.ErrJobState.Error() + ": "; len(message) > len(prefix) && message[:len(prefix)] == prefix {
		return message[len(prefix):]
	}

	return message
}

func registerImportJobStart(api huma.API, exchanger ExchangeService, jobs ImportJobService, receipts *idempotency.Coordinator) {
	huma.Register(api, huma.Operation{
		OperationID: "start-import-job", Method: http.MethodPost,
		Path:          "/api/v1/recipes/import-previews/{preview_id}/jobs",
		Summary:       "Start an import job",
		DefaultStatus: http.StatusAccepted,
		Description: "Takes the selected recipes out of the review session and imports them as durable work. " +
			"The response is a job to watch rather than a result to wait for: closing the page, losing the " +
			"connection, or restarting Savorum does not abandon the import. Recipes are committed in batches, " +
			"so partial success is preserved and inspectable recipe by recipe.",
		Tags: []string{"Import jobs"}, Errors: []int{404, 409, 413, 422, 503},
	}, func(ctx context.Context, input *startImportJobInput) (*startImportJobOutput, error) {
		if receipts != nil && input.IdempotencyKey != "" {
			data, err := json.Marshal(input.Body)
			if err != nil {
				return nil, mapError(err)
			}

			work, id, release, err := receipts.Begin(ctx, "preview-import:"+input.PreviewID, input.IdempotencyKey, data)
			if err != nil {
				return nil, automationError(err)
			}
			defer release()

			ctx = work
			if id != "" {
				job, err := jobs.Get(ctx, id)
				if err != nil {
					return nil, mapImportJobError(err)
				}

				return &startImportJobOutput{Location: "/api/v1/import-jobs/" + job.ID, Body: ImportJobStartResponse{Job: importJobResource(job)}}, nil
			}
		}

		selections, resolved, err := importJobSelections(input.Body)
		if err != nil {
			return nil, err
		}

		policy := library.DuplicatePolicy(input.Body.OnDuplicate)
		if policy == "" {
			policy = library.DuplicateSkip
		}

		var job importjob.Job

		start := func(plan exchange.ImportPlan) error {
			var err error

			job, err = jobs.Start(ctx, importjob.StartRequest{Importer: plan.Importer, Source: plan.Source, Policy: policy, Items: plan.Items})

			return err
		}

		var plan exchange.ImportPlan
		if planner, ok := exchanger.(interface {
			PlanPreviewImportWithOptions(context.Context, string, []exchange.PreviewResolution, bool, exchange.PreviewCommitOptions) (exchange.ImportPlan, error)
		}); ok {
			plan, err = planner.PlanPreviewImportWithOptions(ctx, input.PreviewID, selections, resolved, exchange.PreviewCommitOptions{RetainSourceItemIDs: input.Body.RetainSourceItemIDs, AcceptPlan: start})
		} else {
			plan, err = exchanger.PlanPreviewImport(ctx, input.PreviewID, selections, resolved)
			if err == nil {
				err = start(plan)
			}
		}

		if err != nil {
			return nil, mapImportJobError(err)
		}

		output := &startImportJobOutput{
			Location: "/api/v1/import-jobs/" + job.ID,
			Body:     ImportJobStartResponse{Job: importJobResource(job)},
		}

		if plan.Remaining != nil {
			remaining := importPreviewResponse(*plan.Remaining)
			output.Body.Remaining = &remaining
		}

		return output, nil
	})
}
