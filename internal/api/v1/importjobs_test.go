package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/automation"
	"github.com/savorum/savorum/internal/configuration"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/idempotency"
	"github.com/savorum/savorum/internal/importjob"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/print"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
)

// importJobAPI composes the API the way the application does, with one exchange
// service shared between the review routes and the job.
func importJobAPI(t *testing.T, fetcher exchange.RemoteAssetFetcher) http.Handler {
	t.Helper()

	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	service, err := library.NewService(store, units.DefaultCatalog(),
		library.WithClock(func() time.Time { return time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC) }))
	if err != nil {
		t.Fatal(err)
	}

	blobStore, err := files.NewAssetStore(filepath.Join(t.TempDir(), "assets"), files.DefaultMaximumAssetSize)
	if err != nil {
		t.Fatal(err)
	}

	assetService, err := assets.NewService(store, blobStore)
	if err != nil {
		t.Fatal(err)
	}

	configurationService, err := configuration.NewService(store, configuration.InstanceOverrides{})
	if err != nil {
		t.Fatal(err)
	}

	printer, err := print.NewService(sharedRenderer(), units.DefaultCatalog(), assetService, service, service)
	if err != nil {
		t.Fatal(err)
	}

	options := []exchange.Option{}
	if fetcher != nil {
		options = append(options, exchange.WithRemoteAssetFetcher(fetcher))
	}

	exchanger := exchange.NewService(service, assetService, options...)

	jobs, err := importjob.NewService(store, exchanger, importjob.WithLimits(importjob.Limits{BatchSize: 2}))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(jobs.Close)

	exporter, err := export.NewService(service, assetService)
	if err != nil {
		t.Fatal(err)
	}

	receipts := idempotency.New(store)

	automationJobs, err := automation.New(store, service, exchanger, jobs, exporter, receipts, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = automationJobs.Close() })

	handler, _, err := New(service, store, printer, Options{
		Assets: assetService, Configuration: configurationService, Exchange: exchanger, ImportJobs: jobs,
		Automation: automationJobs, Idempotency: receipts,
	})
	if err != nil {
		t.Fatal(err)
	}

	return handler
}

func stageImportPreview(t *testing.T, handler http.Handler, files map[string]string) ImportPreviewResponse {
	t.Helper()

	var body bytes.Buffer

	writer := multipart.NewWriter(&body)
	for name, contents := range files {
		part, err := writer.CreateFormFile("files", name)
		if err != nil {
			t.Fatal(err)
		}

		_, _ = io.WriteString(part, contents)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	previewRequest := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/recipes/import-preview", &body)
	previewRequest.Header.Set("Content-Type", writer.FormDataContentType())

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, previewRequest)

	if response.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", response.Code, response.Body.String())
	}

	var preview ImportPreviewResponse
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}

	return preview
}

func awaitJob(t *testing.T, handler http.Handler, id string) ImportJobResource {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)

	for {
		response := request(t, handler, http.MethodGet, "/api/v1/import-jobs/"+id, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("read job status=%d body=%s", response.Code, response.Body.String())
		}

		var job ImportJobResource
		if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
			t.Fatal(err)
		}

		switch job.State {
		case "completed", "completed-with-errors", "cancelled", "failed", "interrupted":
			return job
		}

		if time.Now().After(deadline) {
			t.Fatalf("job %s never finished: %#v", id, job)
		}

		time.Sleep(5 * time.Millisecond)
	}
}

func TestImportJobRunsASelectionAndReportsEveryRecipe(t *testing.T) {
	handler := importJobAPI(t, nil)
	preview := stageImportPreview(t, handler, map[string]string{
		"soup.jsonld":   `{"@type":"Recipe","name":"Job soup","recipeIngredient":["water"],"recipeInstructions":["Simmer."]}`,
		"toast.jsonld":  `{"@type":"Recipe","name":"Job toast","recipeIngredient":["bread"],"recipeInstructions":["Toast."]}`,
		"broken.html":   "not a recipe",
		"noodle.jsonld": `{"@type":"Recipe","name":"Job noodles","recipeIngredient":["noodles"],"recipeInstructions":["Boil."]}`,
	})

	selected := make([]string, 0, len(preview.Items))
	for _, item := range preview.Items {
		if item.CanCommit {
			selected = append(selected, item.ID)
		}
	}

	if len(selected) != 3 {
		t.Fatalf("selectable items = %d", len(selected))
	}

	body, err := json.Marshal(ImportJobStartInput{ItemIDs: selected, OnDuplicate: "skip"})
	if err != nil {
		t.Fatal(err)
	}

	started := request(t, handler, http.MethodPost, "/api/v1/recipes/import-previews/"+preview.ID+"/jobs", body)
	// The request that starts an import is answered before it finishes.
	if started.Code != http.StatusAccepted {
		t.Fatalf("start status=%d body=%s", started.Code, started.Body.String())
	}

	var start ImportJobStartResponse
	if err := json.Unmarshal(started.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}

	if start.Job.ID == "" || start.Job.TotalItems != 3 {
		t.Fatalf("started job = %#v", start.Job)
	}

	if location := started.Header().Get("Location"); location != "/api/v1/import-jobs/"+start.Job.ID {
		t.Fatalf("Location = %q", location)
	}
	// The unusable file stays in the review queue: a reviewer can keep working
	// through what is left while the selection imports.
	if start.Remaining == nil || len(start.Remaining.Items) != 1 || start.Remaining.Items[0].CanCommit {
		t.Fatalf("remaining preview = %#v", start.Remaining)
	}

	job := awaitJob(t, handler, start.Job.ID)
	if job.State != "completed" || job.Progress.Imported != 3 || job.Progress.Finished != 3 {
		t.Fatalf("finished job = %#v", job)
	}

	items := readJobItems(t, handler, job.ID, "")
	if len(items) != 3 {
		t.Fatalf("job items = %#v", items)
	}

	for _, item := range items {
		if item.State != "imported" || item.LibraryEntryID == "" || item.Revision == 0 || item.ImportEventID == "" {
			t.Fatalf("job item = %#v", item)
		}

		history := request(t, handler, http.MethodGet, "/api/v1/recipes/"+item.LibraryEntryID+"/import-events", nil)
		if history.Code != http.StatusOK {
			t.Fatalf("history status=%d body=%s", history.Code, history.Body.String())
		}

		var body struct {
			Items []library.ImportEvent `json:"items"`
		}
		if err := json.Unmarshal(history.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}

		if len(body.Items) != 1 || body.Items[0].ID != item.ImportEventID || body.Items[0].JobID != job.ID || body.Items[0].Revision != item.Revision {
			t.Fatalf("event history for %#v = %#v", item, body.Items)
		}

		if body.Items[0].RecipeID == "" || body.Items[0].BaselineRevision != item.Revision {
			t.Fatalf("event lost resulting baseline metadata: %#v", body.Items[0])
		}

		if body.Items[0].ImporterVersion == "" || body.Items[0].ImporterVersion == "1" {
			t.Fatalf("event recorded the source format version instead of implementation identity: %#v", body.Items[0])
		}

		recipe := request(t, handler, http.MethodGet, "/api/v1/recipes/"+item.LibraryEntryID, nil)
		if recipe.Code != http.StatusOK || strings.Contains(recipe.Body.String(), "import_event") {
			t.Fatalf("portable recipe representation leaked local history: status=%d body=%s", recipe.Code, recipe.Body.String())
		}
	}

	if listed := listSummaries(t, handler, ""); len(listed) != 3 {
		t.Fatalf("library holds %d recipes", len(listed))
	}
	// The same recipes turn up in a listing of jobs, with their progress.
	list := request(t, handler, http.MethodGet, "/api/v1/import-jobs", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}

	var listing struct {
		Items []ImportJobResource `json:"items"`
		Total int                 `json:"total"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}

	if listing.Total != 1 || len(listing.Items) != 1 || listing.Items[0].Progress.Imported != 3 {
		t.Fatalf("job listing = %#v", listing)
	}
}

func TestImportJobProgressNamesItsPersistedBatch(t *testing.T) {
	job := importjob.Job{
		State: importjob.StateRunning, BatchSize: 25,
		Progress: importjob.Progress{Total: 60, Imported: 25, Running: 25, Pending: 10},
	}

	resource := importJobResource(job)
	if resource.Progress.CurrentBatch != 2 || resource.Progress.TotalBatches != 3 {
		t.Fatalf("batch progress = %#v", resource.Progress)
	}

	job.State = importjob.StatePending

	job.Progress = importjob.Progress{Total: 60, Pending: 60}
	if current := importJobResource(job).Progress.CurrentBatch; current != 0 {
		t.Fatalf("pending current batch = %d, want 0", current)
	}

	job.State = importjob.StateCompleted

	job.Progress = importjob.Progress{Total: 60, Imported: 60}
	if current := importJobResource(job).Progress.CurrentBatch; current != 3 {
		t.Fatalf("completed current batch = %d, want 3", current)
	}
}

func TestImportJobItemsFilterByWhatAReaderIsLookingFor(t *testing.T) {
	handler := importJobAPI(t, nil)
	soup := `{"@type":"Recipe","name":"Filter soup","recipeIngredient":["water"],"recipeInstructions":["Simmer."]}`
	preview := stageImportPreview(t, handler, map[string]string{"soup.jsonld": soup})

	body, err := json.Marshal(ImportJobStartInput{ItemIDs: []string{preview.Items[0].ID}})
	if err != nil {
		t.Fatal(err)
	}

	first := request(t, handler, http.MethodPost, "/api/v1/recipes/import-previews/"+preview.ID+"/jobs", body)

	var start ImportJobStartResponse
	if err := json.Unmarshal(first.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}

	awaitJob(t, handler, start.Job.ID)

	// The same recipe again. Meeting a portable recipe a second time is
	// reported rather than duplicated, and "skipped" is its own filter.
	repeat := stageImportPreview(t, handler, map[string]string{"soup.jsonld": soup})

	body, err = json.Marshal(ImportJobStartInput{ItemIDs: []string{repeat.Items[0].ID}, OnDuplicate: "skip"})
	if err != nil {
		t.Fatal(err)
	}

	second := request(t, handler, http.MethodPost, "/api/v1/recipes/import-previews/"+repeat.ID+"/jobs", body)

	var again ImportJobStartResponse
	if err := json.Unmarshal(second.Body.Bytes(), &again); err != nil {
		t.Fatal(err)
	}

	job := awaitJob(t, handler, again.Job.ID)
	if job.Progress.Skipped != 1 {
		t.Fatalf("second job progress = %#v", job.Progress)
	}

	if skipped := readJobItems(t, handler, job.ID, "skipped"); len(skipped) != 1 || skipped[0].Outcome != "skipped" {
		t.Fatalf("skipped items = %#v", skipped)
	}

	if imported := readJobItems(t, handler, job.ID, "imported"); len(imported) != 0 {
		t.Fatalf("imported items = %#v", imported)
	}

	if listed := listSummaries(t, handler, ""); len(listed) != 1 {
		t.Fatalf("library holds %d recipes, want 1", len(listed))
	}
}

func TestFailedImportItemNamesARecognizedCause(t *testing.T) {
	handler := importJobAPI(t, nil)
	soup := `{"@type":"Recipe","name":"Duplicate soup","recipeIngredient":["water"],"recipeInstructions":["Simmer."]}`

	for attempt, policy := range []string{"", "reject"} {
		preview := stageImportPreview(t, handler, map[string]string{"soup.jsonld": soup})

		body, err := json.Marshal(ImportJobStartInput{ItemIDs: []string{preview.Items[0].ID}, OnDuplicate: policy})
		if err != nil {
			t.Fatal(err)
		}

		response := request(t, handler, http.MethodPost, "/api/v1/recipes/import-previews/"+preview.ID+"/jobs", body)

		var start ImportJobStartResponse
		if err := json.Unmarshal(response.Body.Bytes(), &start); err != nil {
			t.Fatal(err)
		}

		job := awaitJob(t, handler, start.Job.ID)

		if attempt == 0 {
			continue
		}

		failed := readJobItems(t, handler, job.ID, "failed")
		if len(failed) != 1 || failed[0].ErrorCode != "already-in-library" || failed[0].Error == "" {
			t.Fatalf("failed items = %#v", failed)
		}
	}
}

func TestCancellingAFinishedImportSaysSoRatherThanPretending(t *testing.T) {
	handler := importJobAPI(t, nil)
	preview := stageImportPreview(t, handler, map[string]string{
		"soup.jsonld": `{"@type":"Recipe","name":"Cancel soup","recipeIngredient":["water"],"recipeInstructions":["Simmer."]}`,
	})

	body, err := json.Marshal(ImportJobStartInput{ItemIDs: []string{preview.Items[0].ID}})
	if err != nil {
		t.Fatal(err)
	}

	response := request(t, handler, http.MethodPost, "/api/v1/recipes/import-previews/"+preview.ID+"/jobs", body)

	var start ImportJobStartResponse
	if err := json.Unmarshal(response.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}

	awaitJob(t, handler, start.Job.ID)

	cancelled := request(t, handler, http.MethodPost, "/api/v1/import-jobs/"+start.Job.ID+"/cancel", nil)
	if cancelled.Code != http.StatusConflict || !strings.Contains(cancelled.Body.String(), "import-job-state") {
		t.Fatalf("cancel finished job status=%d body=%s", cancelled.Code, cancelled.Body.String())
	}

	resumed := request(t, handler, http.MethodPost, "/api/v1/import-jobs/"+start.Job.ID+"/resume", nil)
	if resumed.Code != http.StatusConflict {
		t.Fatalf("resume finished job status=%d body=%s", resumed.Code, resumed.Body.String())
	}

	for _, action := range []string{"cancel", "resume"} {
		common := request(t, handler, http.MethodPost, "/api/v1/jobs/"+start.Job.ID+"/"+action, nil)
		if common.Code != http.StatusConflict || !strings.Contains(common.Body.String(), "import-job-state") {
			t.Fatalf("%s through the common job route status=%d body=%s", action, common.Code, common.Body.String())
		}
	}

	missing := request(t, handler, http.MethodGet, "/api/v1/import-jobs/does-not-exist", nil)
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "import-job-not-found") {
		t.Fatalf("unknown job status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestStartingAnImportWithNoSelectionIsRefused(t *testing.T) {
	handler := importJobAPI(t, nil)
	preview := stageImportPreview(t, handler, map[string]string{
		"soup.jsonld": `{"@type":"Recipe","name":"Empty","recipeIngredient":["water"],"recipeInstructions":["Simmer."]}`,
	})

	body, err := json.Marshal(ImportJobStartInput{})
	if err != nil {
		t.Fatal(err)
	}

	response := request(t, handler, http.MethodPost, "/api/v1/recipes/import-previews/"+preview.ID+"/jobs", body)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty selection status=%d body=%s", response.Code, response.Body.String())
	}

	expired := request(t, handler, http.MethodPost, "/api/v1/recipes/import-previews/gone/jobs",
		mustJSON(t, ImportJobStartInput{ItemIDs: []string{"item-1"}}))
	if expired.Code != http.StatusNotFound || !strings.Contains(expired.Body.String(), "import-preview-expired") {
		t.Fatalf("expired preview status=%d body=%s", expired.Code, expired.Body.String())
	}
}

// A photograph nobody has looked at yet and one that cannot be had are
// different answers, and the endpoint has to say which.
func TestPreviewImageDistinguishesPendingFromUnavailable(t *testing.T) {
	fetcher := &refusingFetcher{}
	handler := importJobAPI(t, fetcher)
	preview := stageImportPreview(t, handler, map[string]string{
		"photo.jsonld": `{"@type":"Recipe","name":"Photo cake","image":"https://images.example/cake.jpg","recipeIngredient":["flour"],"recipeInstructions":["Bake."]}`,
	})
	item := preview.Items[0]
	// The address is offered whether or not anything has been fetched.
	if item.Assets.PreviewURL == "" || item.Assets.Deferred != 1 || item.Assets.Available != 0 {
		t.Fatalf("preview assets = %#v", item.Assets)
	}

	if fetcher.calls != 0 {
		t.Fatalf("staging a review fetched %d photographs", fetcher.calls)
	}

	response := request(t, handler, http.MethodGet, item.Assets.PreviewURL, nil)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "import-preview-image-unavailable") {
		t.Fatalf("unavailable image status=%d body=%s", response.Code, response.Body.String())
	}

	if fetcher.calls != 1 {
		t.Fatalf("looking at the row fetched %d times", fetcher.calls)
	}
	// The recipe is importable regardless. A photograph is not a reason to
	// refuse the text somebody asked for.
	if !item.CanCommit {
		t.Fatalf("a photograph made a recipe unimportable: %#v", item)
	}
}

func readJobItems(t *testing.T, handler http.Handler, jobID, filter string) []ImportJobItemResource {
	t.Helper()

	target := "/api/v1/import-jobs/" + jobID + "/items"
	if filter != "" {
		target += "?filter=" + filter
	}

	response := request(t, handler, http.MethodGet, target, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("items status=%d body=%s", response.Code, response.Body.String())
	}

	var listing struct {
		Items []ImportJobItemResource `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}

	return listing.Items
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	return encoded
}

type refusingFetcher struct{ calls int }

func (fetcher *refusingFetcher) Fetch(context.Context, string) (exchange.FetchedAsset, error) {
	fetcher.calls++
	return exchange.FetchedAsset{}, fmt.Errorf("remote server returned 404 Not Found")
}
