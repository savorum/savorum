package v1

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/automation"
	"github.com/savorum/savorum/internal/idempotency"
	"github.com/savorum/savorum/internal/importjob"
)

// JobResource gives bulk, transfer and existing import jobs a common lifecycle.
type JobResource struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	State       string `json:"state"`
	Revision    uint64 `json:"revision"`
	CreatedAt   string `json:"created_at"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	Finished    int    `json:"finished"`
	Total       int    `json:"total"`
	Code        string `json:"code,omitempty"`
	ImportJobID string `json:"import_job_id,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
}

// BulkItemResult carries independent status and structured details for one bulk or import item.
type BulkItemResult struct {
	Import          *ImportJobItemResource `json:"import,omitempty"`
	BookRevision    uint64                 `json:"book_revision,omitempty"`
	ID              string                 `json:"id"`
	Status          string                 `json:"status" enum:"succeeded,failed,pending,running,skipped,cancelled"`
	Code            string                 `json:"code,omitempty"`
	Revision        uint64                 `json:"revision,omitempty"`
	CurrentRevision uint64                 `json:"current_revision,omitempty"`
	Recipe          *LibraryEntry          `json:"recipe,omitempty"`
}

type bulkOutput struct {
	Status   int
	Location string `header:"Location"`
	Body     struct {
		Policy string           `json:"policy" enum:"partial-success"`
		Items  []BulkItemResult `json:"items,omitempty"`
		Job    *JobResource     `json:"job,omitempty"`
	}
}

type jobOutput struct {
	Location string `header:"Location"`
	ETag     string `header:"ETag"`
	Body     JobResource
}

type (
	jobPath struct {
		ID string `path:"id"`
	}
	jobItemsInput struct {
		ID     string `path:"id"`
		Offset int    `query:"offset" default:"0" minimum:"0"`
		Limit  int    `query:"limit" default:"100" minimum:"1" maximum:"200"`
	}
)

func jobResource(job automation.Job) JobResource {
	value := JobResource{ID: job.ID, Kind: job.Kind, State: job.State, Revision: job.Revision, CreatedAt: job.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), ExpiresAt: job.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"), Finished: job.Finished, Total: job.Total, Code: job.Code, ImportJobID: job.ImportJobID}
	if job.DownloadHandle != "" && job.State == "completed" {
		value.DownloadURL = "/api/v1/jobs/" + job.ID + "/downloads/" + job.DownloadHandle
	}

	return value
}

func bulkResult(result automation.Result) BulkItemResult {
	value := BulkItemResult{BookRevision: result.BookRevision, ID: result.ID, Status: result.Status, Code: result.Code, Revision: result.Revision, CurrentRevision: result.CurrentRevision}
	if result.Entry != nil {
		entry := fromLibrary(*result.Entry)
		value.Recipe = &entry
	}

	return value
}

func automationError(err error) error {
	if conflict, ok := errors.AsType[*automation.RevisionConflict](err); ok {
		value := problem(412, "job-revision-conflict", "Reload this job before resuming it")
		value.Params = map[string]any{"expected_revision": conflict.Expected, "current_revision": conflict.Current}

		return value
	}

	if denied := accessProblem(err); denied != nil {
		return denied
	}

	switch {
	case errors.Is(err, idempotency.ErrConflict):
		return problem(409, "idempotency-conflict", "Use the same key only for the same request")
	case errors.Is(err, idempotency.ErrInvalid), errors.Is(err, automation.ErrInvalid):
		return problem(422, "automation-invalid", "Check the input type, policy, IDs, and resource revisions")
	case errors.Is(err, automation.ErrNotFound):
		return problem(404, "job-not-found", "This job is unavailable or its 24-hour retention period ended")
	case errors.Is(err, automation.ErrState):
		return problem(409, "job-state-conflict", "Reload this job before changing it")
	case errors.Is(err, idempotency.ErrCapacity), errors.Is(err, automation.ErrCapacity):
		return problem(429, "automation-capacity", "The retained job or request limit was reached; retry after older work expires")
	default:
		return mapError(err)
	}
}

func registerAutomation(api huma.API, recipes RecipeService, options Options) {
	for _, action := range []string{"read", "tags", "book", "trash", "restore"} {
		registerBulk(api, recipes, options, action)
	}

	registerGetJob(api, options)
	registerListJobResults(api, options)
	registerResumeJob(api, options)
	registerGetJobImportPreview(api, options)
	registerCommitAutomationImport(api, options)
	registerCancelJob(api, options)
	registerDownloadJobArtifact(api, options)
	registerTransferCreation(api, options)
}

func registerGetJob(api huma.API, options Options) {
	jobs := options.Automation

	huma.Register(api, huma.Operation{OperationID: "get-job", Method: http.MethodGet, Path: "/api/v1/jobs/{id}", Summary: "Read a bulk or transfer job", Description: "Creator-scoped and authorized at every read. Transfer jobs and temporary artifacts expire after 24 hours. Existing import job IDs use the same lifecycle projection here, for reads, cancel, and resume; /import-jobs keeps its detailed compatible representation. An import job has no revision: its revision is 0 and the response carries no ETag.", Tags: []string{"Jobs"}, Errors: []int{403, 404}}, func(ctx context.Context, input *jobPath) (*jobOutput, error) {
		record, err := jobs.Get(ctx, input.ID)
		if errors.Is(err, automation.ErrNotFound) && options.ImportJobs != nil {
			job, importErr := options.ImportJobs.Get(ctx, input.ID)
			if importErr != nil {
				return nil, mapImportJobError(importErr)
			}

			return &jobOutput{Body: importJobProjection(job)}, nil
		}

		if err != nil {
			return nil, automationError(err)
		}

		return &jobOutput{ETag: revisionETag(record.Job.Revision), Body: jobResource(record.Job)}, nil
	})
}

// importJobProjection presents an import job in the common job lifecycle.
func importJobProjection(job importjob.Job) JobResource {
	progress := job.Progress

	return JobResource{
		ID: job.ID, Kind: "import", State: string(job.State), CreatedAt: job.CreatedAt.Format(time.RFC3339),
		Total: job.TotalItems, Finished: progress.Imported + progress.Skipped + progress.Failed + progress.Cancelled,
	}
}

func registerListJobResults(api huma.API, options Options) {
	jobs := options.Automation

	huma.Register(api, huma.Operation{OperationID: "list-job-results", Method: http.MethodGet, Path: "/api/v1/jobs/{id}/items", Summary: "Read job item results", Description: "A common per-item lifecycle. Import-specific fidelity and source details are carried in import. Bulk results appear as items finish. Use next_offset for the next page; pages stop near 16 MiB of stored snapshots (one larger recipe is still returned). interrupted uncertain mutations explicitly report outcome-unknown and are never silently repeated.", Tags: []string{"Jobs"}}, func(ctx context.Context, input *jobItemsInput) (*jobResultsOutput, error) {
		output := &jobResultsOutput{}
		output.Body.Items = []BulkItemResult{}
		record, err := jobs.Get(ctx, input.ID)

		var importID string

		switch {
		case errors.Is(err, automation.ErrNotFound):
			importID = input.ID
		case err != nil:
			return nil, automationError(err)
		default:
			importID = record.Job.ImportJobID
		}

		if importID != "" && options.ImportJobs != nil {
			items, total, err := options.ImportJobs.Items(ctx, importID, importjob.ItemListOptions{Limit: input.Limit, Offset: input.Offset})
			if err != nil {
				return nil, mapImportJobError(err)
			}

			output.Body.Total = total
			output.Body.NextOffset = input.Offset + len(items)

			for _, item := range items {
				detail := importJobItemResource(item)

				status := string(item.State)
				if status == "imported" {
					status = "succeeded"
				}

				code := ""
				if status == "failed" {
					code = "import-item-failed"
				}

				output.Body.Items = append(output.Body.Items, BulkItemResult{ID: item.SourceItemID, Status: status, Code: code, Revision: item.Revision, Import: &detail})
			}

			return output, nil
		}

		if err != nil {
			return nil, automationError(err)
		}

		output.Body.Total = record.Job.Total

		results, err := jobs.Results(ctx, input.ID, input.Offset, input.Limit)
		if err != nil {
			return nil, automationError(err)
		}

		for _, result := range results {
			output.Body.Items = append(output.Body.Items, bulkResult(result))
		}

		output.Body.NextOffset = input.Offset + len(results)

		return output, nil
	})
}

func registerResumeJob(api huma.API, options Options) {
	jobs := options.Automation

	huma.Register(api, huma.Operation{OperationID: "resume-job", Method: http.MethodPost, Path: "/api/v1/jobs/{id}/resume", Summary: "Resume interrupted work", Description: "Send the job revision in If-Match. An import job has no revision and needs none. Completed item results remain; an in-flight mutation whose result could not be checkpointed reports outcome-unknown and is not repeated. Refetch that resource before deciding a new action. Original per-item resource revisions continue to apply.", Tags: []string{"Jobs"}}, func(ctx context.Context, input *struct {
		ID      string `path:"id"`
		IfMatch string `header:"If-Match"`
	},
	) (*jobOutput, error) {
		_, err := jobs.Get(ctx, input.ID)
		if errors.Is(err, automation.ErrNotFound) && options.ImportJobs != nil {
			job, err := options.ImportJobs.Resume(ctx, input.ID)
			if err != nil {
				return nil, mapImportJobError(err)
			}

			return &jobOutput{Body: importJobProjection(job)}, nil
		}

		if err != nil {
			return nil, automationError(err)
		}

		revision, err := parseRevisionPrecondition(input.IfMatch, "job")
		if err != nil {
			return nil, err
		}

		record, err := jobs.Resume(ctx, input.ID, revision)
		if err != nil {
			return nil, automationError(err)
		}

		return &jobOutput{ETag: revisionETag(record.Job.Revision), Body: jobResource(record.Job)}, nil
	})
}

func registerGetJobImportPreview(api huma.API, options Options) {
	jobs := options.Automation

	huma.Register(api, huma.Operation{OperationID: "get-job-import-preview", Method: http.MethodGet, Path: "/api/v1/jobs/{id}/preview", Summary: "Read a frozen import preview", Tags: []string{"Jobs"}, Errors: []int{403, 404, 409}}, func(ctx context.Context, input *jobPath) (*struct{ Body ImportPreviewResponse }, error) {
		record, err := jobs.Get(ctx, input.ID)
		if err != nil {
			return nil, automationError(err)
		}

		if record.Preview == nil {
			return nil, automationError(automation.ErrState)
		}

		return &struct{ Body ImportPreviewResponse }{importPreviewResponse(*record.Preview)}, nil
	})
}

func registerCommitAutomationImport(api huma.API, options Options) {
	jobs := options.Automation

	huma.Register(api, huma.Operation{OperationID: "commit-automation-import", Method: http.MethodPost, Path: "/api/v1/jobs/{id}/commit", Summary: "Commit a frozen import", Description: "require-no-warnings refuses warning/error diagnostics; allow-warnings still refuses errors. Repeating commit returns the original import job. Duplicate policy is reject or skip; updates to existing recipes require the normal reviewed reconciliation flow.", Tags: []string{"Jobs"}, Errors: []int{403, 404, 409, 422}}, func(ctx context.Context, input *struct {
		ID   string `path:"id"`
		Body struct {
			Policy string `json:"policy" enum:"require-no-warnings,allow-warnings"`
		}
	},
	) (*jobOutput, error) {
		record, err := jobs.Commit(ctx, input.ID, input.Body.Policy)
		if err != nil {
			return nil, automationError(err)
		}

		return &jobOutput{Body: jobResource(record.Job)}, nil
	})
}

func registerCancelJob(api huma.API, options Options) {
	jobs := options.Automation

	huma.Register(api, huma.Operation{OperationID: "cancel-job", Method: http.MethodPost, Path: "/api/v1/jobs/{id}/cancel", Summary: "Cancel unfinished work", Description: "Already committed item results remain. A mutation in progress may finish before cancellation is observed.", Tags: []string{"Jobs"}, Errors: []int{403, 404, 409}}, func(ctx context.Context, input *jobPath) (*jobOutput, error) {
		record, err := jobs.Cancel(ctx, input.ID)
		if errors.Is(err, automation.ErrNotFound) && options.ImportJobs != nil {
			job, importErr := options.ImportJobs.Cancel(ctx, input.ID)
			if importErr != nil {
				return nil, mapImportJobError(importErr)
			}

			return &jobOutput{Body: importJobProjection(job)}, nil
		}

		if err != nil {
			return nil, automationError(err)
		}

		return &jobOutput{Body: jobResource(record.Job)}, nil
	})
}

func registerDownloadJobArtifact(api huma.API, options Options) {
	jobs := options.Automation

	huma.Register(api, huma.Operation{OperationID: "get-job-artifact", Method: http.MethodGet, Path: "/api/v1/jobs/{id}/downloads/{handle}", Summary: "Download an export artifact", Description: "Opaque handle, creator and current library authorization required. No unauthenticated/static download URL. Expires 24 hours after job creation.", Tags: []string{"Jobs"}, Errors: []int{403, 404}, Responses: map[string]*huma.Response{"200": {Description: "Authenticated SREF package or bundle", Content: map[string]*huma.MediaType{"application/zip": {Schema: &huma.Schema{Type: "string", Format: "binary"}}}}}}, func(ctx context.Context, input *struct {
		ID     string `path:"id"`
		Handle string `path:"handle"`
	},
	) (*huma.StreamResponse, error) {
		job, reader, err := jobs.Download(ctx, input.ID, input.Handle)
		if err != nil {
			return nil, automationError(err)
		}

		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			defer reader.Close()

			hctx.SetHeader("Content-Type", job.MediaType)
			hctx.SetHeader("Content-Disposition", "attachment; filename="+strconv.Quote(job.Filename))
			hctx.SetHeader("Cache-Control", "no-store")
			_, _ = io.Copy(hctx.BodyWriter(), reader)
		}}, nil
	})
}

func registerBulk(api huma.API, recipes RecipeService, options Options, action string) {
	service, ok := recipes.(automation.Library)
	if !ok {
		return
	}

	huma.Register(api, huma.Operation{OperationID: "apply-bulk-" + action + "-recipes", Method: http.MethodPost, Path: "/api/v1/bulk/recipes/" + action, Summary: "Bulk " + action + " recipes", Description: "Partial success: each item uses normal resource authorization and its own revision. Tags/book use the organization's revision; trash/restore use the recipe revision. Book membership also requires the initial book revision, advanced only by this batch's successful items. Work above the advertised synchronous limit, or with an Idempotency-Key, returns 202 with a persistent job. No arbitrary operation names are accepted.", Tags: []string{"Bulk"}, Errors: []int{403, 409, 422, 429}}, func(ctx context.Context, input *struct {
		IdempotencyKey string `header:"Idempotency-Key" maxLength:"200"`
		Body           struct {
			Items        []automation.Target `json:"items" minItems:"1" maxItems:"10000"`
			AddTags      []string            `json:"add_tags,omitempty" maxItems:"100"`
			RemoveTags   []string            `json:"remove_tags,omitempty" maxItems:"100"`
			BookID       string              `json:"book_id,omitempty"`
			BookRevision uint64              `json:"book_revision,omitempty"`
			Member       bool                `json:"member,omitempty"`
		}
	},
	) (*bulkOutput, error) {
		body := input.Body

		request := automation.BulkRequest{Action: action, Items: body.Items, AddTags: body.AddTags, RemoveTags: body.RemoveTags, BookID: body.BookID, BookRevision: body.BookRevision, Member: body.Member}
		if err := automation.ValidateBulk(request); err != nil {
			return nil, automationError(err)
		}

		output := &bulkOutput{}
		output.Body.Policy = "partial-success"

		if len(request.Items) > options.BulkLimit || input.IdempotencyKey != "" {
			data, err := json.Marshal(request)
			if err != nil {
				return nil, mapError(err)
			}

			work, id, release, err := options.Idempotency.Begin(ctx, "bulk-"+action, input.IdempotencyKey, data)
			if err != nil {
				return nil, automationError(err)
			}
			defer release()

			var record automation.Record
			if id != "" {
				record, err = options.Automation.Get(work, id)
			} else {
				record, err = options.Automation.StartBulk(work, request)
			}

			if err != nil {
				return nil, automationError(err)
			}

			job := jobResource(record.Job)
			output.Status = 202
			output.Location = "/api/v1/jobs/" + job.ID
			output.Body.Job = &job

			return output, nil
		}

		for _, item := range request.Items {
			result := automation.BulkItem(ctx, service, request, item)

			output.Body.Items = append(output.Body.Items, bulkResult(result))
			if action == "book" && result.Status == "succeeded" {
				request.BookRevision = result.BookRevision
			}
		}

		output.Status = 200

		return output, nil
	})
	operation := api.OpenAPI().Paths["/api/v1/bulk/recipes/"+action].Post
	queued := *operation.Responses["200"]
	queued.Description = "Persistent job accepted; poll the returned Location"
	operation.Responses["202"] = &queued
}

func registerTransferCreation(api huma.API, options Options) {
	huma.Register(api, huma.Operation{OperationID: "create-export-job", Method: http.MethodPost, Path: "/api/v1/export-jobs", Summary: "Queue an export", DefaultStatus: 202, Description: "Uses the ordinary export service, with bounded background compression and an authenticated 24-hour artifact. Idempotency-Key retains the same job for 24 hours; reusing a key with different input returns 409.", Tags: []string{"Jobs"}, Errors: []int{403, 409, 422, 429}}, func(ctx context.Context, input *struct {
		IdempotencyKey string `header:"Idempotency-Key" maxLength:"200"`
		Body           automation.ExportInput
	},
	) (*jobOutput, error) {
		data, err := json.Marshal(input.Body)
		if err != nil {
			return nil, mapError(err)
		}

		work, id, release, err := options.Idempotency.Begin(ctx, "export-job", input.IdempotencyKey, data)
		if err != nil {
			return nil, automationError(err)
		}
		defer release()

		var record automation.Record
		if id != "" {
			record, err = options.Automation.Get(work, id)
		} else {
			record, err = options.Automation.StartExport(work, input.Body)
		}

		if err != nil {
			return nil, automationError(err)
		}

		return &jobOutput{Location: "/api/v1/jobs/" + record.Job.ID, Body: jobResource(record.Job)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "create-import-job", Method: http.MethodPost, Path: "/api/v1/import-jobs", Summary: "Queue an import preview", DefaultStatus: 202, Description: "kind=url uses the same SSRF-protected URL importer as the UI. kind=file accepts base64 file_content; kind=text accepts text. filename is a display basename, never a local path. Default policy is preview-only; automatic commit requires require-no-warnings or allow-warnings. Read /jobs/{id}/preview for structured fidelity diagnostics and /jobs/{id} for state. Idempotency-Key is retained atomically for 24 hours.", Tags: []string{"Jobs"}, MaxBodyBytes: 90 << 20, Errors: []int{403, 409, 413, 422, 429}}, func(ctx context.Context, input *struct {
		IdempotencyKey string `header:"Idempotency-Key" maxLength:"200"`
		Body           struct {
			Kind        string `json:"kind" enum:"url,file,text"`
			URL         string `json:"url,omitempty" maxLength:"8192"`
			Filename    string `json:"filename,omitempty" maxLength:"255"`
			FileContent []byte `json:"file_content,omitempty"`
			Text        string `json:"text,omitempty" maxLength:"1000000"`
			Policy      string `json:"policy,omitempty" default:"preview-only" enum:"preview-only,require-no-warnings,allow-warnings"`
			OnDuplicate string `json:"on_duplicate,omitempty" default:"reject" enum:"reject,skip"`
		}
	},
	) (*jobOutput, error) {
		body := input.Body

		content := body.FileContent
		if body.Kind == "text" {
			if len(content) > 0 {
				return nil, automationError(automation.ErrInvalid)
			}

			content = []byte(body.Text)
			if body.Filename == "" {
				body.Filename = "recipe.txt"
			}
		} else if body.Text != "" {
			return nil, automationError(automation.ErrInvalid)
		}

		request := automation.ImportInput{Kind: body.Kind, URL: body.URL, Filename: body.Filename, Data: content, Policy: body.Policy, OnDuplicate: body.OnDuplicate}

		data, err := json.Marshal(request)
		if err != nil {
			return nil, mapError(err)
		}

		work, id, release, err := options.Idempotency.Begin(ctx, "import-job", input.IdempotencyKey, data)
		if err != nil {
			return nil, automationError(err)
		}
		defer release()

		var record automation.Record
		if id != "" {
			record, err = options.Automation.Get(work, id)
		} else {
			record, err = options.Automation.StartImport(work, request)
		}

		if err != nil {
			return nil, automationError(err)
		}

		return &jobOutput{Location: "/api/v1/jobs/" + record.Job.ID, Body: jobResource(record.Job)}, nil
	})
}

type jobResultsOutput struct {
	Body struct {
		NextOffset int              `json:"next_offset"`
		Items      []BulkItemResult `json:"items"`
		Total      int              `json:"total"`
	}
}
