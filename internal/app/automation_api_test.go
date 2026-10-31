package app_test

import (
	"net/http"
	"testing"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

func TestExportJobsProduceADownloadableArtifact(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	entry := h.recipe()
	queued := decode[map[string]any](t, h.send(http.MethodPost, "/api/v1/export-jobs", map[string]any{"format": "sref", "recipes": []string{entry}}, http.StatusAccepted))
	path := "/api/v1/jobs/" + text(t, queued, "id")

	finished := h.waitFor(path)
	if finished["state"] != "completed" {
		t.Fatalf("export job = %v", finished)
	}

	h.send(http.MethodGet, path+"/items", nil, http.StatusOK)
	h.send(http.MethodGet, text(t, finished, "download_url"), nil, http.StatusOK)
}

const recipeText = `Tomato Soup

Ingredients
- 2 cups tomatoes
- 1 cup water

Instructions
1. Simmer the tomatoes in the water.
2. Blend until smooth.
`

func TestImportJobsAreReviewedCommittedAndCancelled(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	queue := func(key string) string {
		job := decode[map[string]any](t, h.send(http.MethodPost, "/api/v1/import-jobs", map[string]any{"kind": "text", "text": recipeText, "policy": "preview-only"}, http.StatusAccepted, "Idempotency-Key", key))

		return "/api/v1/jobs/" + text(t, job, "id")
	}

	reviewed := queue("review")
	if state := h.waitFor(reviewed)["state"]; state != "awaiting-review" {
		t.Fatalf("import job state = %v", state)
	}

	h.send(http.MethodGet, reviewed+"/preview", nil, http.StatusOK)

	current := h.send(http.MethodGet, reviewed, nil, http.StatusOK)
	h.send(http.MethodPost, reviewed+"/commit", map[string]any{"policy": "allow-warnings"}, http.StatusOK, "If-Match", current.Header().Get("ETag"))

	cancelled := queue("cancel")
	h.waitFor(cancelled)
	current = h.send(http.MethodGet, cancelled, nil, http.StatusOK)
	h.send(http.MethodPost, cancelled+"/cancel", nil, http.StatusOK, "If-Match", current.Header().Get("ETag"))
}

func TestAnInterruptedExportJobIsResumed(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	entry := h.recipe()
	queued := decode[map[string]any](t, h.send(http.MethodPost, "/api/v1/export-jobs", map[string]any{"format": "sref", "recipes": []string{entry}}, http.StatusAccepted))
	path := "/api/v1/jobs/" + text(t, queued, "id")
	h.waitFor(path)

	account := decode[struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}](t, h.send(http.MethodGet, "/api/v1/account", nil, http.StatusOK))

	// A process that stopped mid-job leaves it interrupted; the state is
	// recorded directly because a test cannot stop the process.
	ctx := access.WithLibrary(identity.WithPrincipal(t.Context(), identity.Principal{UserID: account.User.ID, InstanceAdmin: true}), "default")

	record, err := h.store.AutomationJob(ctx, text(t, queued, "id"))
	if err != nil {
		t.Fatal(err)
	}

	record.Job.State = "interrupted"

	if err := h.store.SaveAutomationJob(ctx, &record); err != nil {
		t.Fatal(err)
	}

	current := h.send(http.MethodGet, path, nil, http.StatusOK)
	h.send(http.MethodPost, path+"/resume", nil, http.StatusOK, "If-Match", current.Header().Get("ETag"))
}
