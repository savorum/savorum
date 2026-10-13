package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/savorum/savorum/internal/testfixture"
)

func TestCookingCountsAreCountedFromCookingRecords(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	created := createRecipe(t, handler, testfixture.Document())
	cookingsPath := "/api/v1/recipes/" + created.ID + "/cookings"

	if found := listSummaries(t, handler, "made:never"); len(found) != 1 {
		t.Fatalf("a recipe with no cookings should be one still to try, got %d", len(found))
	}

	first := recordCooking(t, handler, created.ID, map[string]any{"cooked_on": "2026-07-12"})
	recordCooking(t, handler, created.ID, map[string]any{"cooked_on": "2026-08-20", "note": "Use less salt next time.", "scale": "2"})

	history := readCookings(t, handler, created.ID)
	if history.TimesCooked != 2 {
		t.Fatalf("times cooked = %d, want 2", history.TimesCooked)
	}

	if history.LastCooked == nil || *history.LastCooked != "2026-08-20" {
		t.Fatalf("last cooked = %v, want 2026-08-20", history.LastCooked)
	}

	if len(history.Items) != 2 || history.Items[0].CookedOn != "2026-08-20" {
		t.Fatalf("cookings are not most recent first: %#v", history.Items)
	}

	if history.Items[0].Scale == nil || *history.Items[0].Scale != "2" {
		t.Fatalf("the batch actually made was not kept: %#v", history.Items[0])
	}

	summaries := listSummaries(t, handler, "")
	if len(summaries) != 1 || summaries[0].TimesCooked != 2 {
		t.Fatalf("recipe summary times cooked = %#v", summaries)
	}

	if summaries[0].LastCooked == nil || *summaries[0].LastCooked != "2026-08-20" {
		t.Fatalf("recipe summary last cooked = %v", summaries[0].LastCooked)
	}

	if found := listSummaries(t, handler, "made:never"); len(found) != 0 {
		t.Fatalf("a recipe cooked twice must not appear as never cooked, got %d", len(found))
	}

	// Curating a recipe cannot change how often it has been cooked.
	organizationPath := "/api/v1/recipes/" + created.ID + "/organization"

	body := organizationJSON(t, map[string]any{"user_state": map[string]any{"favorite": true}})
	if applied := requestWithIfMatch(t, handler, http.MethodPut, organizationPath, body, `"1"`); applied.Code != http.StatusOK {
		t.Fatalf("organize status=%d body=%s", applied.Code, applied.Body.String())
	}

	if after := readCookings(t, handler, created.ID); after.TimesCooked != 2 {
		t.Fatalf("times cooked after curation = %d, want 2", after.TimesCooked)
	}

	// Forgetting one occasion takes the count back down with it.
	if response := request(t, handler, http.MethodDelete, cookingsPath+"/"+first.ID, nil); response.Code != http.StatusNoContent {
		t.Fatalf("delete cooking status=%d body=%s", response.Code, response.Body.String())
	}

	after := readCookings(t, handler, created.ID)
	if after.TimesCooked != 1 || after.LastCooked == nil || *after.LastCooked != "2026-08-20" {
		t.Fatalf("history after forgetting one cooking = %#v", after)
	}
}

func TestCookingSessionRetriesAreIdempotent(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	created := createRecipe(t, handler, testfixture.Document())
	body := []byte(`{"cooked_on":"2026-09-04","scale":"2","session_id":"cook-session-1"}`)
	responses := make(chan *httptest.ResponseRecorder, 2)

	var group sync.WaitGroup
	for range 2 {
		group.Go(func() {
			responses <- request(t, handler, http.MethodPost, "/api/v1/recipes/"+created.ID+"/cookings", body)
		})
	}

	group.Wait()
	close(responses)

	var ids []string

	for response := range responses {
		if response.Code != http.StatusCreated {
			t.Fatalf("retry status=%d body=%s", response.Code, response.Body.String())
		}

		var record CookingRecord
		if err := json.Unmarshal(response.Body.Bytes(), &record); err != nil {
			t.Fatal(err)
		}

		ids = append(ids, record.ID)
	}

	if len(ids) != 2 || ids[0] != ids[1] {
		t.Fatalf("idempotent retry IDs = %#v", ids)
	}

	if history := readCookings(t, handler, created.ID); history.TimesCooked != 1 || len(history.Items) != 1 {
		t.Fatalf("idempotent retry history = %#v", history)
	}
}

func TestRecordingACookingDefaultsToToday(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	created := createRecipe(t, handler, testfixture.Document())
	record := recordCooking(t, handler, created.ID, map[string]any{})
	// testAPI holds the clock at 2026-08-26.
	if record.CookedOn != "2026-08-26" {
		t.Fatalf("cooked on = %q, want today", record.CookedOn)
	}
}

func TestRememberedBatchIsAYieldOrAScaleButNotBoth(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	created := createRecipe(t, handler, testfixture.Document())
	organizationPath := "/api/v1/recipes/" + created.ID + "/organization"

	both := organizationJSON(t, map[string]any{
		"user_state": map[string]any{"favorite": false, "preferred_yield": "6", "preferred_scale": "3/2"},
	})
	if response := requestWithIfMatch(t, handler, http.MethodPut, organizationPath, both, `"1"`); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("two answers to one question should be rejected: status=%d body=%s", response.Code, response.Body.String())
	}

	wanted := organizationJSON(t, map[string]any{
		"user_state": map[string]any{"favorite": false, "preferred_yield": "6"},
	})

	response := requestWithIfMatch(t, handler, http.MethodPut, organizationPath, wanted, `"1"`)
	if response.Code != http.StatusOK {
		t.Fatalf("remember a yield status=%d body=%s", response.Code, response.Body.String())
	}

	var organization RecipeOrganization
	if err := json.Unmarshal(response.Body.Bytes(), &organization); err != nil {
		t.Fatal(err)
	}

	if organization.UserState.PreferredYield == nil || *organization.UserState.PreferredYield != "6" {
		t.Fatalf("remembered yield = %v", organization.UserState.PreferredYield)
	}
}
