package app_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

const recipePage = `<html><head><script type="application/ld+json">
{"@context":"https://schema.org","@type":"Recipe","name":"Tomato Soup",
 "recipeIngredient":["2 cups tomatoes","1 cup water"],
 "recipeInstructions":[{"@type":"HowToStep","text":"Simmer the tomatoes in the water."},{"@type":"HowToStep","text":"Blend until smooth."}]}
</script></head><body></body></html>`

type previewItems struct {
	ID    string `json:"id"`
	Items []struct {
		ID string `json:"id"`
	} `json:"items"`
}

func TestImportPreviewsAreReadCommittedQueuedAndDiscarded(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	stage := func(name string) previewItems {
		return decode[previewItems](t, h.upload(name+".html", []byte(recipePage)))
	}

	committed := stage("committed")
	path := "/api/v1/recipes/import-previews/" + committed.ID

	h.send(http.MethodGet, path, nil, http.StatusOK)

	h.send(http.MethodPost, path+"/commit", map[string]any{"item_ids": []string{committed.Items[0].ID}}, http.StatusOK)

	queued := stage("queued")
	started := decode[struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}](t, h.send(http.MethodPost, "/api/v1/recipes/import-previews/"+queued.ID+"/jobs", map[string]any{"item_ids": []string{queued.Items[0].ID}}, http.StatusAccepted))

	h.send(http.MethodGet, "/api/v1/import-jobs/"+started.Job.ID, nil, http.StatusOK)
	h.send(http.MethodGet, "/api/v1/import-jobs/"+started.Job.ID+"/items", nil, http.StatusOK)

	discarded := stage("discarded")
	h.send(http.MethodDelete, "/api/v1/recipes/import-previews/"+discarded.ID, nil, http.StatusNoContent)
}

func TestMeasurementPlansAreProposedAppliedAndReverted(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	review := "name: Regional cake\ningredients:\n  - 1 Tbsp olive oil\n  - 1 cup flour\ndirections:\n  - Mix.\n"
	preview := decode[struct {
		ID       string `json:"id"`
		Revision uint64 `json:"preview_revision"`
		Groups   []struct {
			ID      string `json:"id"`
			Choices []struct {
				UnitID string `json:"unit_id"`
			} `json:"choices"`
		} `json:"resolution_groups"`
	}](t, h.upload("review.yml", []byte(review)))

	path := "/api/v1/recipes/import-previews/" + preview.ID

	h.send(http.MethodGet, path+"/measurement-resolution-groups/"+preview.Groups[0].ID, nil, http.StatusOK)

	plan := decode[struct {
		PlanID string `json:"plan_id"`
	}](t, h.send(http.MethodPost, path+"/measurement-resolution-plans", map[string]any{
		"resolution_group_ids":      []string{preview.Groups[0].ID},
		"interpretation":            map[string]any{"kind": "canonical-unit", "unit_id": preview.Groups[0].Choices[0].UnitID},
		"expected_preview_revision": preview.Revision,
	}, http.StatusOK))

	h.send(http.MethodPost, path+"/measurement-resolution-plans/"+plan.PlanID+"/apply", map[string]any{"expected_preview_revision": preview.Revision}, http.StatusOK)

	current := decode[struct {
		Revision uint64 `json:"preview_revision"`
	}](t, h.send(http.MethodGet, path, nil, http.StatusOK))

	h.send(http.MethodDelete, path+"/measurement-resolution-plans/"+plan.PlanID, map[string]any{"expected_preview_revision": current.Revision}, http.StatusOK)
}

// A job finishes quickly, so a cancel can arrive after the last batch; the
// scenario repeats until a cancel lands on a running job.
func TestALargeImportIsCancelledAndResumed(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for attempt := range 5 {
		files := map[string][]byte{}

		for index := range 300 {
			title := fmt.Sprintf("Soup %d number %d", attempt, index)
			files[fmt.Sprintf("soup-%d.txt", index)] = []byte(title + "\n\nIngredients\n- 1 litre water\n- " + title + " stock\n\nInstructions\n1. Simmer.\n")
		}

		preview := decode[struct {
			ID    string `json:"id"`
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}](t, h.uploadAll(files, http.StatusOK))

		ids := make([]string, 0, len(preview.Items))
		for _, item := range preview.Items {
			ids = append(ids, item.ID)
		}

		started := decode[struct {
			Job struct {
				ID string `json:"id"`
			} `json:"job"`
		}](t, h.send(http.MethodPost, "/api/v1/recipes/import-previews/"+preview.ID+"/jobs", map[string]any{"item_ids": ids}, http.StatusAccepted))

		path := "/api/v1/import-jobs/" + started.Job.ID

		if h.attempt(http.MethodPost, path+"/cancel", nil, "").Code != http.StatusOK {
			continue
		}

		for range 400 {
			if state := decode[struct {
				State string `json:"state"`
			}](t, h.send(http.MethodGet, path, nil, http.StatusOK)).State; state == "cancelled" || state == "completed" {
				break
			}

			time.Sleep(50 * time.Millisecond)
		}

		if h.attempt(http.MethodPost, path+"/resume", nil, "").Code == http.StatusOK {
			return
		}
	}

	t.Fatal("no import job was cancelled while running and then resumed")
}
