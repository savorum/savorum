package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/testfixture"
)

func TestRecipeReferencesSeparateActiveAndTrash(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	create := func(id, title string) string {
		document := fromDomain(testfixture.Document())
		document.ID = id
		document.Title = title

		body, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}

		created := request(t, handler, http.MethodPost, "/api/v1/recipes", body)
		if created.Code != http.StatusCreated {
			t.Fatalf("create %q status = %d; body = %s", title, created.Code, created.Body.String())
		}

		return strings.TrimPrefix(created.Header().Get("Location"), "/api/v1/recipes/")
	}

	firstID := create("reference-pie", "Zebra pie")
	secondID := create("reference-soup", "Alpha soup")

	if trashed := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/recipes/"+firstID, nil, `"1"`); trashed.Code != http.StatusOK {
		t.Fatalf("trash status = %d; body = %s", trashed.Code, trashed.Body.String())
	}

	read := func(target string) []map[string]any {
		response := request(t, handler, http.MethodGet, target, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("references %s status = %d; body = %s", target, response.Code, response.Body.String())
		}

		var body struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}

		return body.Items
	}

	active := read("/api/v1/recipe-references")
	if len(active) != 1 || len(active[0]) != 3 || active[0]["id"] != secondID || active[0]["title"] != "Alpha soup" || active[0]["revision"] != float64(1) {
		t.Fatalf("active references = %#v", active)
	}

	trash := read("/api/v1/recipe-references?state=trash")
	if len(trash) != 1 || len(trash[0]) != 3 || trash[0]["id"] != firstID || trash[0]["title"] != "Zebra pie" || trash[0]["revision"] != float64(2) {
		t.Fatalf("trash references = %#v", trash)
	}
}

func TestRecipeLifecycleRejectsStaleWritesAndRetainsHistory(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	original := fromDomain(testfixture.Document())

	body, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	created := request(t, handler, http.MethodPost, "/api/v1/recipes", body)
	if created.Code != http.StatusCreated || created.Header().Get("ETag") != `"1"` {
		t.Fatalf("create status = %d, ETag = %q; body = %s", created.Code, created.Header().Get("ETag"), created.Body.String())
	}

	var entry LibraryEntry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}

	location := created.Header().Get("Location")

	updatedDocument := original
	updatedDocument.Title = "Updated pumpkin stew"

	updatedBody, err := json.Marshal(updatedDocument)
	if err != nil {
		t.Fatal(err)
	}

	missingPrecondition := request(t, handler, http.MethodPut, location, updatedBody)
	if missingPrecondition.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing precondition status = %d; body = %s", missingPrecondition.Code, missingPrecondition.Body.String())
	}

	updated := requestWithIfMatch(t, handler, http.MethodPut, location, updatedBody, `"1"`)
	if updated.Code != http.StatusOK || updated.Header().Get("ETag") != `"2"` {
		t.Fatalf("update status = %d, ETag = %q; body = %s", updated.Code, updated.Header().Get("ETag"), updated.Body.String())
	}

	identityChange := updatedDocument
	identityChange.ID = "independent-copy"

	identityBody, err := json.Marshal(identityChange)
	if err != nil {
		t.Fatal(err)
	}

	rejectedIdentity := requestWithIfMatch(t, handler, http.MethodPut, location, identityBody, `"2"`)
	if rejectedIdentity.Code != http.StatusConflict || !strings.Contains(rejectedIdentity.Body.String(), "recipe-document-identity-change") {
		t.Fatalf("identity-changing update status = %d; body = %s", rejectedIdentity.Code, rejectedIdentity.Body.String())
	}

	stale := requestWithIfMatch(t, handler, http.MethodPut, location, body, `"1"`)
	if stale.Code != http.StatusPreconditionFailed || !strings.Contains(stale.Body.String(), "current revision is 2") {
		t.Fatalf("stale update status = %d; body = %s", stale.Code, stale.Body.String())
	}

	historyPath := location + "/revisions"

	history := request(t, handler, http.MethodGet, historyPath, nil)
	if history.Code != http.StatusOK {
		t.Fatalf("history status = %d; body = %s", history.Code, history.Body.String())
	}

	var page struct {
		Items []RecipeRevisionSummary `json:"items"`
	}
	if err := json.Unmarshal(history.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}

	if len(page.Items) != 2 || page.Items[0].Revision != 2 || strings.Contains(history.Body.String(), `"recipe"`) {
		t.Fatalf("history list is not metadata-only: %s", history.Body.String())
	}

	historical := request(t, handler, http.MethodGet, historyPath+"/1", nil)

	var historicalRevision RecipeRevision
	if err := json.Unmarshal(historical.Body.Bytes(), &historicalRevision); err != nil {
		t.Fatal(err)
	}

	if historicalRevision.Recipe.Title != original.Title {
		t.Fatalf("historical detail did not retain immutable snapshot: %s", historical.Body.String())
	}

	trashed := requestWithIfMatch(t, handler, http.MethodDelete, location, nil, `"2"`)
	if trashed.Code != http.StatusOK || trashed.Header().Get("ETag") != `"3"` {
		t.Fatalf("trash status = %d, ETag = %q; body = %s", trashed.Code, trashed.Header().Get("ETag"), trashed.Body.String())
	}

	if active := request(t, handler, http.MethodGet, location, nil); active.Code != http.StatusNotFound {
		t.Fatalf("trashed recipe remained in active reads: %d", active.Code)
	}

	trashLocation := "/api/v1/trash/recipes/" + entry.ID
	if trash := request(t, handler, http.MethodGet, trashLocation, nil); trash.Code != http.StatusOK || trash.Header().Get("ETag") != `"3"` {
		t.Fatalf("trash read status = %d, ETag = %q; body = %s", trash.Code, trash.Header().Get("ETag"), trash.Body.String())
	}

	restored := requestWithIfMatch(t, handler, http.MethodPost, location+"/restore", nil, `"3"`)
	if restored.Code != http.StatusOK || restored.Header().Get("ETag") != `"4"` {
		t.Fatalf("restore status = %d, ETag = %q; body = %s", restored.Code, restored.Header().Get("ETag"), restored.Body.String())
	}

	restoredHistory := requestWithIfMatch(t, handler, http.MethodPost, historyPath+"/1/restore", nil, `"4"`)
	if restoredHistory.Code != http.StatusOK || restoredHistory.Header().Get("ETag") != `"5"` {
		t.Fatalf("history restore status = %d, ETag = %q; body = %s", restoredHistory.Code, restoredHistory.Header().Get("ETag"), restoredHistory.Body.String())
	}

	if err := json.Unmarshal(restoredHistory.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}

	if entry.Recipe.Title != original.Title {
		t.Fatalf("history restore did not restore revision 1: %s", restoredHistory.Body.String())
	}

	history = request(t, handler, http.MethodGet, historyPath, nil)
	if err := json.Unmarshal(history.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}

	if len(page.Items) != 5 || page.Items[0].Cause != "restore" || page.Items[0].SourceRevision == nil || *page.Items[0].SourceRevision != 1 {
		t.Fatalf("restore provenance is incomplete: %s", history.Body.String())
	}

	trashed = requestWithIfMatch(t, handler, http.MethodDelete, location, nil, `"5"`)
	if trashed.Code != http.StatusOK || trashed.Header().Get("ETag") != `"6"` {
		t.Fatalf("second trash status = %d; body = %s", trashed.Code, trashed.Body.String())
	}

	purged := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/trash/recipes/"+entry.ID, nil, `"6"`)
	if purged.Code != http.StatusNoContent {
		t.Fatalf("purge status = %d; body = %s", purged.Code, purged.Body.String())
	}

	if response := request(t, handler, http.MethodGet, historyPath, nil); response.Code != http.StatusNotFound {
		t.Fatalf("purged history status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestIngredientWarningsFollowSuccessfulRecipeRevisions(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	document := fromDomain(testfixture.Document())

	body, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	created := request(t, handler, http.MethodPost, "/api/v1/recipes", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}

	var entry LibraryEntry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}

	location := created.Header().Get("Location")

	updated := document
	for sectionIndex := range updated.IngredientSections {
		for ingredientIndex := range updated.IngredientSections[sectionIndex].Ingredients {
			ingredient := &updated.IngredientSections[sectionIndex].Ingredients[ingredientIndex]
			if ingredient.ID == "salt" {
				ingredient.Name = "fine sea salt"
			}
		}
	}

	updatedBody, err := json.Marshal(updated)
	if err != nil {
		t.Fatal(err)
	}

	saved := requestWithIfMatch(t, handler, http.MethodPut, location, updatedBody, `"1"`)
	if saved.Code != http.StatusOK || strings.Contains(saved.Body.String(), "The source does not state a measurable amount.") ||
		!strings.Contains(saved.Body.String(), `"review":"corrected"`) {
		t.Fatalf("corrected ingredient status=%d body=%s", saved.Code, saved.Body.String())
	}

	historical := request(t, handler, http.MethodGet, location+"/revisions/1", nil)
	if historical.Code != http.StatusOK || !strings.Contains(historical.Body.String(), "The source does not state a measurable amount.") {
		t.Fatalf("historical warning was changed: status=%d body=%s", historical.Code, historical.Body.String())
	}

	stale := requestWithIfMatch(t, handler, http.MethodPut, location, body, `"1"`)
	if stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale correction status=%d body=%s", stale.Code, stale.Body.String())
	}

	current := request(t, handler, http.MethodGet, location, nil)
	if strings.Contains(current.Body.String(), "The source does not state a measurable amount.") {
		t.Fatalf("stale write restored warning: %s", current.Body.String())
	}

	restored := requestWithIfMatch(t, handler, http.MethodPost, location+"/revisions/1/restore", nil, `"2"`)
	if restored.Code != http.StatusOK || !strings.Contains(restored.Body.String(), "The source does not state a measurable amount.") {
		t.Fatalf("restored revision diagnostic status=%d body=%s", restored.Code, restored.Body.String())
	}
}

func TestEmptyTrashPurgesOnlyAnAtomicReviewedRevisionSet(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	entries := make([]LibraryEntry, 0, 2)

	for index := range 2 {
		document := fromDomain(testfixture.Document())
		document.ID = fmt.Sprintf("trash-document-%d", index+1)
		document.Title = fmt.Sprintf("Trash recipe %d", index+1)

		body, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}

		created := request(t, handler, http.MethodPost, "/api/v1/recipes", body)
		if created.Code != http.StatusCreated {
			t.Fatalf("create status = %d; body = %s", created.Code, created.Body.String())
		}

		var entry LibraryEntry
		if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
			t.Fatal(err)
		}

		location := created.Header().Get("Location")

		trashed := requestWithIfMatch(t, handler, http.MethodDelete, location, nil, `"1"`)
		if trashed.Code != http.StatusOK {
			t.Fatalf("trash status = %d; body = %s", trashed.Code, trashed.Body.String())
		}

		entries = append(entries, entry)
	}

	staleBody, err := json.Marshal(map[string]any{"recipes": []map[string]any{
		{"id": entries[0].ID, "revision": 2},
		{"id": entries[1].ID, "revision": 1},
	}})
	if err != nil {
		t.Fatal(err)
	}

	stale := request(t, handler, http.MethodPost, "/api/v1/trash/empty", staleBody)
	if stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale empty trash status = %d; body = %s", stale.Code, stale.Body.String())
	}

	duplicate := `{"recipes":[{"id":"` + entries[0].ID + `","revision":2},{"id":"` + entries[0].ID + `","revision":2}]}`
	for _, body := range []string{`{"recipes":[],"books":[]}`, duplicate} {
		invalid := request(t, handler, http.MethodPost, "/api/v1/trash/empty", []byte(body))
		if invalid.Code != http.StatusUnprocessableEntity || !strings.Contains(invalid.Body.String(), "recipe-request-invalid") {
			t.Fatalf("empty trash %s: status = %d; body = %s", body, invalid.Code, invalid.Body.String())
		}
	}

	merged := request(t, handler, http.MethodPost, "/api/v1/recipes/merge", []byte(`{"survivor":"`+entries[0].ID+`","duplicate":"`+entries[0].ID+`","expected_survivor_revision":1,"expected_duplicate_revision":1,"recipe":`+string(mustJSON(t, fromDomain(testfixture.Document())))+`}`))
	if merged.Code != http.StatusUnprocessableEntity || !strings.Contains(merged.Body.String(), "recipe-request-invalid") {
		t.Fatalf("merge with itself: status = %d; body = %s", merged.Code, merged.Body.String())
	}

	for _, entry := range entries {
		response := request(t, handler, http.MethodGet, "/api/v1/trash/recipes/"+entry.ID, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("atomic empty trash removed %s: status = %d", entry.ID, response.Code)
		}
	}

	exactBody, err := json.Marshal(map[string]any{"recipes": []map[string]any{
		{"id": entries[0].ID, "revision": 2},
		{"id": entries[1].ID, "revision": 2},
	}})
	if err != nil {
		t.Fatal(err)
	}

	purged := request(t, handler, http.MethodPost, "/api/v1/trash/empty", exactBody)
	if purged.Code != http.StatusNoContent {
		t.Fatalf("empty trash status = %d; body = %s", purged.Code, purged.Body.String())
	}

	for _, entry := range entries {
		response := request(t, handler, http.MethodGet, "/api/v1/trash/recipes/"+entry.ID, nil)
		if response.Code != http.StatusNotFound {
			t.Fatalf("empty trash retained %s: status = %d", entry.ID, response.Code)
		}
	}
}
