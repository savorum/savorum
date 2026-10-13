package v1

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/sref/bundle"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestExportReturnsTheRecipeWithoutLibraryState(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	plain := testfixture.Document()

	plain.Assets, plain.ImageRefs, plain.PrimaryImage = nil, nil, ""
	for sectionIndex := range plain.InstructionSections {
		for stepIndex := range plain.InstructionSections[sectionIndex].Steps {
			plain.InstructionSections[sectionIndex].Steps[stepIndex].ImageRefs = nil
		}
	}

	created := createRecipe(t, handler, plain)
	publication := createBook(t, handler, map[string]any{
		"kind": "publication", "title": "Local publication record", "sections": []any{}, "entries": []any{},
	})
	page := uint64(42)
	organizationPath := "/api/v1/recipes/" + created.ID + "/organization"

	body := organizationJSON(t, map[string]any{
		"user_state": map[string]any{"favorite": true, "rating": 5},
		"sources": []RecipeSource{{
			Kind: "publication", PublicationBookID: publication.ID, PageStart: &page,
			OriginalTitle: "Local edition title", Attribution: "Local catalog note",
		}},
	})
	if applied := requestWithIfMatch(t, handler, http.MethodPut, organizationPath, body, `"1"`); applied.Code != http.StatusOK {
		t.Fatalf("organize status=%d", applied.Code)
	}

	response := request(t, handler, http.MethodGet, "/api/v1/recipes/"+created.ID+"/export/sref", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", response.Code, response.Body.String())
	}

	if got := response.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("export content type = %q", got)
	}

	disposition := response.Header().Get("Content-Disposition")
	if !strings.HasPrefix(disposition, "attachment;") || !strings.Contains(disposition, ".sref") {
		t.Fatalf("export is not offered as a file: %q", disposition)
	}

	opened, err := sref.ReadPackage(response.Body.Bytes(), sref.DefaultLimits())
	if err != nil {
		t.Fatalf("Savorum cannot read what it exported: %v", err)
	}

	// The portable recipe is what travels. How this library organizes it, and
	// the local identity it gave the entry, are not part of the recipe.
	payload := recipeJSONOf(t, response.Body.Bytes())
	for _, leaked := range []string{
		"favorite", "rating", "times_cooked", "tag_ids", "schema_version", created.ID,
		"publication_book_id", publication.ID, "Local edition title", "Local catalog note",
	} {
		if strings.Contains(payload, leaked) {
			t.Fatalf("export leaked library state %q", leaked)
		}
	}

	if !strings.Contains(payload, `"sref"`) {
		t.Fatalf("export is not an SREF document: %s", payload)
	}

	if opened.Recipe.ID != plain.ID {
		t.Fatal("export changed the recipe's portable identity")
	}

	if !reflect.DeepEqual(opened.Recipe.Source, plain.Source) {
		t.Fatalf("portable source changed: have %#v want %#v", opened.Recipe.Source, plain.Source)
	}
}

func TestCookbookManagerImportsExportIndividuallyAndAsASelection(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	preview := stageImportPreview(t, handler, map[string]string{
		"Imported Soup_101.yml":  "name: Imported Soup\ningredients:\n  - 2 tsp salt\ndirections:\n  - Simmer.\n",
		"Imported Bread_102.yml": "name: Imported Bread\ningredients:\n  - 200g flour\ndirections:\n  - Bake.\nvideo: https://videos.example/bread\n",
	})
	if len(preview.Items) != 2 {
		t.Fatalf("preview items = %d, want 2", len(preview.Items))
	}

	itemIDs := []string{preview.Items[0].ID, preview.Items[1].ID}

	commitBody, err := json.Marshal(ImportPreviewCommitInput{ItemIDs: itemIDs, OnDuplicate: "skip"})
	if err != nil {
		t.Fatal(err)
	}

	committed := request(t, handler, http.MethodPost, "/api/v1/recipes/import-previews/"+preview.ID+"/commit", commitBody)
	if committed.Code != http.StatusOK {
		t.Fatalf("commit status=%d body=%s", committed.Code, committed.Body.String())
	}

	var result ImportPreviewCommitResponse
	if err := json.Unmarshal(committed.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}

	if len(result.Items) != 2 {
		t.Fatalf("committed items = %#v", result.Items)
	}

	entryIDs := []string{result.Items[0].LibraryEntryID, result.Items[1].LibraryEntryID}

	single := request(t, handler, http.MethodGet, "/api/v1/recipes/"+entryIDs[0]+"/export/sref", nil)
	if single.Code != http.StatusOK {
		t.Fatalf("single export status=%d body=%s", single.Code, single.Body.String())
	}

	opened, err := sref.ReadPackage(single.Body.Bytes(), sref.DefaultLimits())
	if err != nil {
		t.Fatalf("read single export: %v", err)
	}

	if _, leaked := opened.Recipe.Extensions["x-cookbookmanager"]; leaked {
		t.Fatalf("single export leaked importer-private metadata: %#v", opened.Recipe.Extensions)
	}

	selectionBody, err := json.Marshal(ExportBundleInput{Recipes: entryIDs})
	if err != nil {
		t.Fatal(err)
	}

	selection := request(t, handler, http.MethodPost, "/api/v1/exports/sref-bundle", selectionBody)
	if selection.Code != http.StatusOK {
		t.Fatalf("selection export status=%d body=%s", selection.Code, selection.Body.String())
	}

	openedBundle, err := bundle.Read(selection.Body.Bytes(), bundle.DefaultLimits())
	if err != nil {
		t.Fatalf("read selection bundle: %v", err)
	}

	if len(openedBundle.Members) != 2 {
		t.Fatalf("bundle members = %d, want 2", len(openedBundle.Members))
	}

	for _, member := range openedBundle.Members {
		packaged, err := sref.ReadPackage(member.Bytes, sref.DefaultLimits())
		if err != nil {
			t.Fatalf("read bundle member %q: %v", member.RecipeID, err)
		}

		if _, leaked := packaged.Recipe.Extensions["x-cookbookmanager"]; leaked {
			t.Fatalf("bundle member %q leaked importer-private metadata", member.RecipeID)
		}
	}
}

func TestBundleExportRejectsRawSelectionShapeBeforeStreaming(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	post := func(recipes []string) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]any{"recipes": recipes})
		if err != nil {
			t.Fatal(err)
		}

		return request(t, handler, http.MethodPost, "/api/v1/exports/sref-bundle", body)
	}

	repeated := make([]string, export.MaximumSelectionRecipes+1)
	for index := range repeated {
		repeated[index] = "missing"
	}

	if response := post(repeated); response.Code < 400 || response.Code >= 500 || response.Header().Get("Content-Type") == "application/zip" {
		t.Fatalf("251 repeated status=%d type=%q body=%s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}

	unique := make([]string, export.MaximumSelectionRecipes+1)
	for index := range unique {
		unique[index] = fmt.Sprintf("missing-%d", index)
	}

	if response := post(unique); response.Code < 400 || response.Code >= 500 {
		t.Fatalf("251 unique status=%d body=%s", response.Code, response.Body.String())
	}

	for name, ids := range map[string][]string{"blank": {"entry", "  "}, "long": {strings.Repeat("x", export.MaximumEntryIDLength+1)}} {
		if response := post(ids); response.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d body=%s", name, response.Code, response.Body.String())
		}
	}
}

func TestExportCarriesEveryPhotographTheRecipeReferences(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	image := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("savorum-package-image")...)
	digestBytes := sha256.Sum256(image)
	digest := hex.EncodeToString(digestBytes[:])
	uploadRequest := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/assets", bytes.NewReader(image))
	uploadRequest.Header.Set("Content-Type", "application/octet-stream")
	uploadRequest.Header.Set("X-Asset-Media-Type", "image/png")
	uploadRequest.Header.Set("X-Asset-SHA256", digest)
	uploadRequest.Header.Set("X-Asset-Size", strconv.Itoa(len(image)))

	upload := httptest.NewRecorder()
	handler.ServeHTTP(upload, uploadRequest)

	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", upload.Code, upload.Body.String())
	}

	document := testfixture.Document()
	document.Assets = document.Assets[:1]
	document.Assets[0].Path = "assets/finished-pies.png"
	document.Assets[0].MediaType = "image/png"
	document.Assets[0].SHA256 = digest
	document.Assets[0].Size = int64(len(image))
	document.PrimaryImage = document.Assets[0].ID

	document.ImageRefs = []string{document.Assets[0].ID}
	for sectionIndex := range document.InstructionSections {
		for stepIndex := range document.InstructionSections[sectionIndex].Steps {
			document.InstructionSections[sectionIndex].Steps[stepIndex].ImageRefs = nil
		}
	}

	created := createRecipe(t, handler, document)

	packaged := request(t, handler, http.MethodGet, "/api/v1/recipes/"+created.ID+"/export/sref", nil)
	if packaged.Code != http.StatusOK {
		t.Fatalf("package export status=%d body=%s", packaged.Code, packaged.Body.String())
	}

	opened, err := sref.ReadPackage(packaged.Body.Bytes(), sref.DefaultLimits())
	if err != nil {
		t.Fatalf("Savorum cannot read the package it wrote: %v", err)
	}

	if len(opened.Assets) != len(created.Recipe.Assets) {
		t.Fatalf("the package carries %d photographs, want %d", len(opened.Assets), len(created.Recipe.Assets))
	}

	if !bytes.Equal(opened.Assets[0].Bytes, image) {
		t.Fatal("the package carries different bytes than the library holds")
	}
}

// recipeJSONOf reads the recipe document out of a package, which is where an
// assertion about what did and did not travel has to look.
