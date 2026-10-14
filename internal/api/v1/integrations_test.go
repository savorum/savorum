package v1

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/automation"
	"github.com/savorum/savorum/internal/event"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/idempotency"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/importjob"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
	"github.com/savorum/savorum/internal/webhook"
)

func integrationAPI(t *testing.T) (http.Handler, huma.API, *automation.Service, context.Context) {
	t.Helper()
	_, store, _ := testAPI(t)

	principal, err := store.LocalPrincipal(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	ctx := access.WithLibrary(identity.WithPrincipal(t.Context(), principal), "default")

	recipes, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	exchanger := exchange.NewService(recipes, nil)

	imports, err := importjob.NewService(store, exchanger)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(imports.Close)

	exporter, err := export.NewService(recipes, nil)
	if err != nil {
		t.Fatal(err)
	}

	receipts := idempotency.New(store)

	jobs, err := automation.New(store, recipes, exchanger, imports, exporter, receipts, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = jobs.Close() })

	hooks, err := webhook.New(store, t.TempDir(), webhook.DestinationPolicy{}, 10)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = hooks.Close() })

	raw, definition, err := New(recipes, store, nil, Options{Pantry: store, Events: store, Automation: jobs, Idempotency: receipts, ImportJobs: imports, Exchange: exchanger, Webhooks: hooks})
	if err != nil {
		t.Fatal(err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Anonymous") == "true" {
			raw.ServeHTTP(w, r)
			return
		}

		scoped := access.WithLibrary(identity.WithPrincipal(r.Context(), principal), "default")
		raw.ServeHTTP(w, r.WithContext(scoped))
	})

	return handler, definition, jobs, ctx
}

func TestIntegrationHTTPContractsAndCapabilityPrivacy(t *testing.T) {
	handler, definition, _, _ := integrationAPI(t)

	schemas := definition.OpenAPI().Components.Schemas.Map()
	if schemas["Capabilities"].AdditionalProperties != true || schemas["CapabilityAdapter"].AdditionalProperties != true {
		t.Fatal("discovery responses must tolerate additive fields")
	}

	if schemas["PantryEntryInput"].AdditionalProperties != false {
		t.Fatal("response compatibility must not weaken input validation")
	}

	public := httptest.NewRecorder()
	anonymous := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/capabilities", nil)
	anonymous.Header.Set("X-Test-Anonymous", "true")
	handler.ServeHTTP(public, anonymous)

	var minimal Capabilities
	if err := json.Unmarshal(public.Body.Bytes(), &minimal); err != nil {
		t.Fatal(err)
	}

	if public.Code != 200 || minimal.APIMajor != 1 || len(minimal.Features) > 0 || minimal.Limits != nil {
		t.Fatalf("bootstrap leaked operational metadata: %s", public.Body.String())
	}

	authorized := request(t, handler, http.MethodGet, "/api/v1/capabilities", nil)

	var complete Capabilities
	if err := json.Unmarshal(authorized.Body.Bytes(), &complete); err != nil {
		t.Fatal(err)
	}

	if len(complete.Importers) == 0 || len(complete.Exporters) == 0 || complete.Limits == nil || len(complete.EventTypes) == 0 {
		t.Fatalf("missing discovery: %s", authorized.Body.String())
	}

	body := []byte(`{"name":"Flour","amount":"half a bag"}`)

	missing := request(t, handler, http.MethodPut, "/api/v1/pantry/flour", body)
	if missing.Code != 428 {
		t.Fatalf("missing pantry precondition: %d %s", missing.Code, missing.Body.String())
	}

	create := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/v1/pantry/flour", bytes.NewReader(body))
	create.Header.Set("Content-Type", "application/json")
	create.Header.Set("If-None-Match", "*")

	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)

	if created.Code != 200 || created.Header().Get("ETag") != `"1"` {
		t.Fatalf("pantry creation: %d %s", created.Code, created.Body.String())
	}

	stale := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/pantry/flour", body, `"9"`)
	if stale.Code != 412 || !strings.Contains(stale.Body.String(), "pantry-revision-conflict") || !strings.Contains(stale.Body.String(), "current_revision") {
		t.Fatalf("structured conflict: %s", stale.Body.String())
	}

	invalid := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/pantry/flour", []byte(`{"name":""}`), `"1"`)
	if invalid.Code != 422 || !strings.Contains(invalid.Body.String(), "request-invalid") || !strings.Contains(invalid.Body.String(), "violations") {
		t.Fatalf("framework validation contract: %d %s", invalid.Code, invalid.Body.String())
	}

	bulk := request(t, handler, http.MethodPost, "/api/v1/bulk/recipes/trash", []byte(`{"items":[{"id":"missing","revision":1},{"id":"no-revision","revision":0}]}`))
	if bulk.Code != 200 || !strings.Contains(bulk.Body.String(), "not-found") || !strings.Contains(bulk.Body.String(), "precondition-required") {
		t.Fatalf("bulk partial success: %d %s", bulk.Code, bulk.Body.String())
	}
}

func TestAutomationImportUsesSharedPreviewAndExplicitCommit(t *testing.T) {
	handler, _, jobs, ctx := integrationAPI(t)

	document := testfixture.Document()
	document.Assets = nil
	document.ImageRefs = nil

	document.PrimaryImage = ""
	for i := range document.InstructionSections {
		for j := range document.InstructionSections[i].Steps {
			document.InstructionSections[i].Steps[j].ImageRefs = nil
		}
	}

	data, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(map[string]any{"kind": "file", "filename": "recipe.sref.json", "file_content": data, "policy": "preview-only", "on_duplicate": "reject"})
	if err != nil {
		t.Fatal(err)
	}

	response := request(t, handler, http.MethodPost, "/api/v1/import-jobs", body)
	if response.Code != 202 {
		t.Fatalf("import creation: %d %s", response.Code, response.Body.String())
	}

	var job JobResource
	if err = json.Unmarshal(response.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}

	if err = jobs.ProcessPending(ctx); err != nil {
		t.Fatal(err)
	}

	status := request(t, handler, http.MethodGet, "/api/v1/jobs/"+job.ID, nil)
	if !strings.Contains(status.Body.String(), "awaiting-review") {
		t.Fatalf("preview committed without consent: %s", status.Body.String())
	}

	preview := request(t, handler, http.MethodGet, "/api/v1/jobs/"+job.ID+"/preview", nil)
	if preview.Code != 200 || !strings.Contains(preview.Body.String(), testfixture.Document().Title) {
		t.Fatalf("shared preview: %d %s", preview.Code, preview.Body.String())
	}

	first := request(t, handler, http.MethodPost, "/api/v1/jobs/"+job.ID+"/commit", []byte(`{"policy":"allow-warnings"}`))
	second := request(t, handler, http.MethodPost, "/api/v1/jobs/"+job.ID+"/commit", []byte(`{"policy":"allow-warnings"}`))

	var one, two JobResource
	if err = json.Unmarshal(first.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}

	if err = json.Unmarshal(second.Body.Bytes(), &two); err != nil {
		t.Fatal(err)
	}

	if first.Code != 200 || second.Code != 200 || one.ImportJobID == "" || one.ImportJobID != two.ImportJobID {
		t.Fatalf("commit replay: %s / %s", first.Body.String(), second.Body.String())
	}
}

type streamReader struct{ calls atomic.Int32 }

func (r *streamReader) ReadEvents(_ context.Context, after string, _ int) (event.Page, error) {
	if r.calls.Add(1) > 1 {
		return event.Page{}, access.ErrForbidden
	}

	return event.Page{Cursor: "evt_1", Events: []event.Envelope{{ID: "evt_1", Type: "recipe.updated", Version: 1}}, Resync: after == ""}, nil
}

func TestSSEFramingResumeAndAuthorizationLoss(t *testing.T) {
	reader := &streamReader{}
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("stream", "1"))
	registerEvents(api, reader, nil)

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Last-Event-ID", "evt_previous")

	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("SSE headers: %+v", response.Header)
	}

	scanner := bufio.NewScanner(response.Body)

	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err = scanner.Err(); err != nil {
		t.Fatal(err)
	}

	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "id: evt_1\nevent: change\ndata:") || strings.Contains(text, "event: resync") || reader.calls.Load() != 2 {
		t.Fatalf("stream replay/revocation: %q calls=%d", text, reader.calls.Load())
	}
}

func TestCancellingAFinishedJobKeepsItsOutcome(t *testing.T) {
	handler, _, jobs, ctx := integrationAPI(t)

	queue := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/bulk/recipes/trash", strings.NewReader(`{"items":[{"id":"missing","revision":1}]}`))
	queue.Header.Set("Content-Type", "application/json")
	queue.Header.Set("Idempotency-Key", "finished-job")

	queued := httptest.NewRecorder()
	handler.ServeHTTP(queued, queue)

	if queued.Code != http.StatusAccepted {
		t.Fatalf("queue bulk job: %d %s", queued.Code, queued.Body.String())
	}

	var accepted struct {
		Job JobResource `json:"job"`
	}
	if err := json.Unmarshal(queued.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}

	job := accepted.Job

	if err := jobs.ProcessPending(ctx); err != nil {
		t.Fatal(err)
	}

	var finished JobResource
	if err := json.Unmarshal(request(t, handler, http.MethodGet, "/api/v1/jobs/"+job.ID, nil).Body.Bytes(), &finished); err != nil {
		t.Fatal(err)
	}

	if finished.State != "completed-with-errors" {
		t.Fatalf("finished job state = %q", finished.State)
	}

	cancelled := request(t, handler, http.MethodPost, "/api/v1/jobs/"+job.ID+"/cancel", nil)

	var after JobResource
	if err := json.Unmarshal(cancelled.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}

	if cancelled.Code != http.StatusOK || after.State != finished.State || after.Revision != finished.Revision {
		t.Fatalf("cancel rewrote a finished job: %d %s", cancelled.Code, cancelled.Body.String())
	}
}
