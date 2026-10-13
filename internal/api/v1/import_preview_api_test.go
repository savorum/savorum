package v1

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestRejectedBatchImportChangesNothing(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	existing := testfixture.Document()
	createRecipe(t, handler, existing)

	newRecipe := testfixture.Document()
	newRecipe.ID = "new-before-duplicate"
	newRecipe.Title = "New before duplicate"

	response := importRecipes(t, handler, map[string]any{
		"recipes": []any{srefDocument(t, newRecipe), srefDocument(t, existing)},
	})
	if response.Code != http.StatusConflict {
		t.Fatalf("batch import status=%d body=%s", response.Code, response.Body.String())
	}

	listed := listSummaries(t, handler, "")
	if len(listed) != 1 || listed[0].RecipeID != string(existing.ID) {
		t.Fatalf("rejected batch changed the library: %#v", listed)
	}
}

func TestImportPreviewStagesReviewAndCommitsOnlySelectedRecipes(t *testing.T) {
	handler, _, _ := testAPI(t)

	var body bytes.Buffer

	writer := multipart.NewWriter(&body)

	good, err := writer.CreateFormFile("files", "soup.jsonld")
	if err != nil {
		t.Fatal(err)
	}

	_, _ = io.WriteString(good, `{"@type":"Recipe","name":"Preview soup","recipeIngredient":["water"],"recipeInstructions":["Simmer."]}`)

	bad, err := writer.CreateFormFile("files", "broken.html")
	if err != nil {
		t.Fatal(err)
	}

	_, _ = io.WriteString(bad, "not a recipe")

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	previewRequest := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/recipes/import-preview", &body)
	previewRequest.Header.Set("Content-Type", writer.FormDataContentType())

	previewResponse := httptest.NewRecorder()
	handler.ServeHTTP(previewResponse, previewRequest)

	if previewResponse.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", previewResponse.Code, previewResponse.Body.String())
	}

	var preview ImportPreviewResponse
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}

	if preview.ID == "" || len(preview.Items) != 2 || !preview.Items[0].CanCommit || preview.Items[1].CanCommit {
		t.Fatalf("preview = %#v", preview)
	}

	if preview.Items[0].Diagnostics == nil {
		t.Fatal("a clean preview must encode diagnostics as an empty collection")
	}

	if listed := listSummaries(t, handler, ""); len(listed) != 0 {
		t.Fatalf("preview changed the library: %#v", listed)
	}

	commitBody, err := json.Marshal(ImportPreviewCommitInput{
		ItemIDs: []string{preview.Items[0].ID}, OnDuplicate: "skip",
	})
	if err != nil {
		t.Fatal(err)
	}

	commit := request(t, handler, http.MethodPost, "/api/v1/recipes/import-previews/"+preview.ID+"/commit", commitBody)
	if commit.Code != http.StatusOK {
		t.Fatalf("commit status=%d body=%s", commit.Code, commit.Body.String())
	}

	var committed ImportPreviewCommitResponse
	if err := json.Unmarshal(commit.Body.Bytes(), &committed); err != nil {
		t.Fatal(err)
	}

	if len(committed.Items) != 1 || committed.Items[0].Title != "Preview soup" ||
		committed.Remaining == nil || len(committed.Remaining.Items) != 1 {
		t.Fatalf("committed = %#v", committed)
	}

	if listed := listSummaries(t, handler, ""); len(listed) != 1 || listed[0].Title != "Preview soup" {
		t.Fatalf("selected commit = %#v", listed)
	}

	if cancelled := request(t, handler, http.MethodDelete, "/api/v1/recipes/import-previews/"+preview.ID, nil); cancelled.Code != http.StatusNoContent {
		t.Fatalf("cancel status=%d body=%s", cancelled.Code, cancelled.Body.String())
	}
}

func TestImportPreviewMeasurementResolutionPlanIsPreviewedAndApplied(t *testing.T) {
	handler := importJobAPI(t, nil)

	preview := stageImportPreview(t, handler, map[string]string{
		"review.yml": "name: Regional cake\ningredients:\n  - 1 Tbsp olive oil\n  - 1 cup flour\ndirections:\n  - Mix.\n",
	})
	if preview.PreviewRevision != 1 || len(preview.ResolutionGroups) != 2 || len(preview.ResolutionProfiles) == 0 {
		t.Fatalf("preview = %#v", preview)
	}

	group := preview.ResolutionGroups[0]

	detail := request(t, handler, http.MethodGet,
		"/api/v1/recipes/import-previews/"+preview.ID+"/measurement-resolution-groups/"+group.ID, nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}

	var decodedDetail MeasurementResolutionGroupDetail
	if err := json.Unmarshal(detail.Body.Bytes(), &decodedDetail); err != nil || len(decodedDetail.Occurrences) != 1 {
		t.Fatalf("detail = %#v, err = %v", decodedDetail, err)
	}

	planBody, err := json.Marshal(MeasurementResolutionPlanInput{
		ResolutionGroupIDs:      []string{group.ID},
		Interpretation:          MeasurementResolutionInterpretationInput{Kind: "canonical-unit", UnitID: group.Choices[0].UnitID},
		ExpectedPreviewRevision: preview.PreviewRevision,
	})
	if err != nil {
		t.Fatal(err)
	}

	planned := request(t, handler, http.MethodPost,
		"/api/v1/recipes/import-previews/"+preview.ID+"/measurement-resolution-plans", planBody)
	if planned.Code != http.StatusOK {
		t.Fatalf("plan status=%d body=%s", planned.Code, planned.Body.String())
	}

	var plan MeasurementResolutionPlanResponse
	if err := json.Unmarshal(planned.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}

	if plan.PlanID == "" || plan.AffectedOccurrences != 1 || len(plan.Occurrences) != 1 || plan.ImpactDigest == "" {
		t.Fatalf("plan = %#v", plan)
	}

	applyBody, err := json.Marshal(MeasurementResolutionPlanApplyInput{ExpectedPreviewRevision: preview.PreviewRevision})
	if err != nil {
		t.Fatal(err)
	}

	applied := request(t, handler, http.MethodPost,
		"/api/v1/recipes/import-previews/"+preview.ID+"/measurement-resolution-plans/"+plan.PlanID+"/apply", applyBody)
	if applied.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", applied.Code, applied.Body.String())
	}

	if err := json.Unmarshal(applied.Body.Bytes(), &plan); err != nil || plan.Preview == nil || plan.Preview.PreviewRevision != 2 {
		t.Fatalf("applied plan = %#v, err = %v", plan, err)
	}

	stale := request(t, handler, http.MethodPost,
		"/api/v1/recipes/import-previews/"+preview.ID+"/measurement-resolution-plans/"+plan.PlanID+"/apply", applyBody)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale status=%d body=%s", stale.Code, stale.Body.String())
	}

	startBody, err := json.Marshal(ImportJobStartInput{ItemIDs: []string{plan.Preview.Items[0].ID}})
	if err != nil {
		t.Fatal(err)
	}

	started := request(t, handler, http.MethodPost,
		"/api/v1/recipes/import-previews/"+preview.ID+"/jobs", startBody)
	if started.Code != http.StatusAccepted {
		t.Fatalf("start job status=%d body=%s", started.Code, started.Body.String())
	}

	var job ImportJobStartResponse
	if err := json.Unmarshal(started.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}

	if len(job.Job.ResolutionDecisions) != 1 || job.Job.ResolutionDecisions[0].ImpactDigest != plan.ImpactDigest ||
		len(job.Job.ResolutionDecisions[0].AffectedOccurrenceIDs) != 1 {
		t.Fatalf("resolution decisions = %#v", job.Job.ResolutionDecisions)
	}

	finished := awaitJob(t, handler, job.Job.ID)
	if finished.State != "completed" || len(finished.ResolutionDecisions) != 1 {
		t.Fatalf("finished resolution job = %#v", finished)
	}

	entry := request(t, handler, http.MethodGet, "/api/v1/recipes/"+readJobItems(t, handler, job.Job.ID, "")[0].LibraryEntryID, nil)
	if entry.Code != http.StatusOK || strings.Contains(entry.Body.String(), `"source_locale"`) {
		t.Fatalf("resolved recipe locale status=%d body=%s", entry.Code, entry.Body.String())
	}
}

func TestCrossSiteMultipartImportCannotMutateTheLibrary(t *testing.T) {
	t.Parallel()
	raw, _, _ := testAPI(t)
	handler := httpsecurity.NewHandler(raw, httpsecurity.Options{})
	makeRequest := func(origin string) *httptest.ResponseRecorder {
		t.Helper()

		var body bytes.Buffer

		writer := multipart.NewWriter(&body)

		file, err := writer.CreateFormFile("files", "soup.jsonld")
		if err != nil {
			t.Fatal(err)
		}

		_, _ = io.WriteString(file, `{"@type":"Recipe","name":"Browser soup","recipeIngredient":["water"],"recipeInstructions":["Simmer."]}`)

		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}

		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/recipes/import-external", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		request.Header.Set("Origin", origin)

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		return response
	}

	if rejected := makeRequest("https://attacker.example"); rejected.Code != http.StatusForbidden {
		t.Fatalf("cross-site import status=%d body=%s", rejected.Code, rejected.Body.String())
	}

	if listed := listSummaries(t, handler, ""); len(listed) != 0 {
		t.Fatalf("cross-site multipart changed the library: %#v", listed)
	}

	if accepted := makeRequest("http://example.com"); accepted.Code != http.StatusOK {
		t.Fatalf("same-origin import status=%d body=%s", accepted.Code, accepted.Body.String())
	}

	if listed := listSummaries(t, handler, ""); len(listed) != 1 || listed[0].Title != "Browser soup" {
		t.Fatalf("same-origin multipart import = %#v", listed)
	}
}

func TestImportPreviewReconcilesSourceMatchesWithoutLosingEntryIdentity(t *testing.T) {
	handler, _, _ := testAPI(t)
	existing := testfixture.Document()
	existing.ID, existing.Title = "original-portable-id", "Original soup"
	existing.Source = &recipe.Source{Application: "Kitchen Export", SourceID: "soup-42", URL: "https://example.test/soup"}
	created := createRecipe(t, handler, existing)
	tag := createTag(t, handler, "Imported favorite", "")
	book := createBook(t, handler, map[string]any{
		"kind": "personal", "title": "Keep this relationship", "sections": []any{}, "entries": []any{},
	})

	organization := organizationJSON(t, map[string]any{
		"tag_ids": []string{tag.ID}, "user_state": map[string]any{"favorite": true},
		"book_membership": []map[string]any{{"book_id": book.ID, "revision": 1, "member": true}},
	})
	if response := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/recipes/"+created.ID+"/organization", organization, `"1"`); response.Code != http.StatusOK {
		t.Fatalf("organize source recipe status=%d body=%s", response.Code, response.Body.String())
	}

	recordCooking(t, handler, created.ID, map[string]any{"cooked_on": "2026-08-20", "note": "Keep this history."})

	previewDocument := func(document recipe.Document) ImportPreviewResponse {
		t.Helper()

		encoded, err := sref.Encode(document)
		if err != nil {
			t.Fatal(err)
		}

		var body bytes.Buffer

		writer := multipart.NewWriter(&body)

		file, err := writer.CreateFormFile("files", "soup.json")
		if err != nil {
			t.Fatal(err)
		}

		_, _ = file.Write(encoded)

		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}

		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/recipes/import-preview", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("preview status=%d body=%s", response.Code, response.Body.String())
		}

		var preview ImportPreviewResponse
		if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
			t.Fatal(err)
		}

		return preview
	}
	commit := func(preview ImportPreviewResponse, action, target string) ImportPreviewCommitResponse {
		t.Helper()

		body, err := json.Marshal(ImportPreviewCommitInput{Resolutions: []ImportPreviewResolutionInput{{
			ItemID: preview.Items[0].ID, Action: action, TargetEntryID: target,
		}}})
		if err != nil {
			t.Fatal(err)
		}

		response := request(t, handler, http.MethodPost, "/api/v1/recipes/import-previews/"+preview.ID+"/commit", body)
		if response.Code != http.StatusOK {
			t.Fatalf("commit status=%d body=%s", response.Code, response.Body.String())
		}

		var result ImportPreviewCommitResponse
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}

		return result
	}

	incoming := existing
	incoming.ID, incoming.Title = "incoming-source-id", "Updated soup"

	preview := previewDocument(incoming)
	if len(preview.Items) != 1 || preview.Items[0].Analysis.Status != "definite-match" ||
		len(preview.Items[0].Analysis.Matches) != 1 || preview.Items[0].Analysis.Matches[0].Evidence != "source-identity" ||
		preview.Items[0].Analysis.Matches[0].EntryID != created.ID || preview.Items[0].Analysis.Matches[0].Revision != 1 {
		t.Fatalf("source analysis = %#v", preview.Items)
	}

	edited := existing
	edited.Title = "Edited while preview was open"

	editedBody, err := json.Marshal(fromDomain(edited))
	if err != nil {
		t.Fatal(err)
	}

	if response := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/recipes/"+created.ID, editedBody, `"1"`); response.Code != http.StatusOK {
		t.Fatalf("concurrent edit status=%d body=%s", response.Code, response.Body.String())
	}

	staleBody, err := json.Marshal(ImportPreviewCommitInput{Resolutions: []ImportPreviewResolutionInput{{
		ItemID: preview.Items[0].ID, Action: "update", TargetEntryID: created.ID,
	}}})
	if err != nil {
		t.Fatal(err)
	}

	stale := request(t, handler, http.MethodPost, "/api/v1/recipes/import-previews/"+preview.ID+"/commit", staleBody)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), "import-preview-stale") {
		t.Fatalf("stale commit status=%d body=%s", stale.Code, stale.Body.String())
	}

	unchanged := request(t, handler, http.MethodGet, "/api/v1/recipes/"+created.ID, nil)
	if unchanged.Code != http.StatusOK || !strings.Contains(unchanged.Body.String(), `"title":"Edited while preview was open"`) {
		t.Fatalf("stale commit changed target status=%d body=%s", unchanged.Code, unchanged.Body.String())
	}

	preview = previewDocument(incoming)
	if preview.Items[0].Analysis.Matches[0].Revision != 2 {
		t.Fatalf("refreshed preview match = %#v", preview.Items[0].Analysis.Matches[0])
	}

	updatedResult := commit(preview, "update", created.ID)
	if len(updatedResult.Items) != 1 || updatedResult.Items[0].Outcome != "replaced" ||
		updatedResult.Items[0].LibraryEntryID != created.ID || updatedResult.Items[0].RecipeID != string(existing.ID) || updatedResult.Items[0].Revision != 3 {
		t.Fatalf("updated result = %#v", updatedResult.Items)
	}

	updated := request(t, handler, http.MethodGet, "/api/v1/recipes/"+created.ID, nil)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"title":"Updated soup"`) {
		t.Fatalf("updated entry status=%d body=%s", updated.Code, updated.Body.String())
	}

	history := request(t, handler, http.MethodGet, "/api/v1/recipes/"+created.ID+"/revisions", nil)
	if history.Code != http.StatusOK || !strings.Contains(history.Body.String(), `"cause":"import"`) || !strings.Contains(history.Body.String(), `"revision":3`) {
		t.Fatalf("import history status=%d body=%s", history.Code, history.Body.String())
	}

	keptOrganization := request(t, handler, http.MethodGet, "/api/v1/recipes/"+created.ID+"/organization", nil)
	if keptOrganization.Code != http.StatusOK || !strings.Contains(keptOrganization.Body.String(), `"`+tag.ID+`"`) || !strings.Contains(keptOrganization.Body.String(), `"favorite":true`) {
		t.Fatalf("updated import lost organization: %s", keptOrganization.Body.String())
	}

	keptBook := readBook(t, handler, book.ID)
	if len(keptBook.Entries) != 1 || keptBook.Entries[0].LibraryEntryID != created.ID {
		t.Fatalf("updated import lost book membership: %#v", keptBook)
	}

	if cooking := readCookings(t, handler, created.ID); cooking.TimesCooked != 1 || len(cooking.Items) != 1 {
		t.Fatalf("updated import lost cooking history: %#v", cooking)
	}

	separateDocument := existing
	separateDocument.Title = "Separate soup"
	preview = previewDocument(separateDocument)

	separateResult := commit(preview, "separate", "")
	if len(separateResult.Items) != 1 || separateResult.Items[0].Outcome != "created" ||
		separateResult.Items[0].LibraryEntryID == created.ID || separateResult.Items[0].RecipeID == string(existing.ID) {
		t.Fatalf("separate result = %#v", separateResult.Items)
	}
}

func TestStagedImportPreviewIsReadBackAndDisappearsWhenCancelled(t *testing.T) {
	handler := importJobAPI(t, nil)

	preview := stageImportPreview(t, handler, map[string]string{
		"book.yml": "name: Kept back\ningredients:\n  - 1 cup flour\ndirections:\n  - Mix.\n",
	})

	// A review is finished in passes, so it can be read back rather than
	// uploaded again.
	reopened := request(t, handler, http.MethodGet, "/api/v1/recipes/import-previews/"+preview.ID, nil)
	if reopened.Code != http.StatusOK {
		t.Fatalf("reopen status=%d body=%s", reopened.Code, reopened.Body.String())
	}

	var decoded ImportPreviewResponse
	if err := json.Unmarshal(reopened.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.ID != preview.ID || len(decoded.Items) != 1 || len(decoded.ResolutionDecisions) != 1 {
		t.Fatalf("reopened = %#v", decoded)
	}

	if decoded.ResolutionDecisions[0].MeasureLabel != "cup" ||
		len(decoded.ResolutionDecisions[0].GroupIDs) != 1 ||
		decoded.ResolutionDecisions[0].Choices[0].Magnitude == "" {
		t.Fatalf("decision = %#v", decoded.ResolutionDecisions[0])
	}

	if cancelled := request(t, handler, http.MethodDelete, "/api/v1/recipes/import-previews/"+preview.ID, nil); cancelled.Code != http.StatusNoContent {
		t.Fatalf("cancel status=%d body=%s", cancelled.Code, cancelled.Body.String())
	}

	if gone := request(t, handler, http.MethodGet, "/api/v1/recipes/import-previews/"+preview.ID, nil); gone.Code != http.StatusNotFound {
		t.Fatalf("discarded preview status=%d body=%s", gone.Code, gone.Body.String())
	}
}
