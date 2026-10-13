package v1

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestAssetCapacityRefusalsAreControlledErrors(t *testing.T) {
	for _, testCase := range []struct {
		err    error
		status int
		kind   string
	}{
		{assets.ErrBusy, http.StatusTooManyRequests, "asset-upload-busy"},
		{assets.ErrCapacity, http.StatusInsufficientStorage, "asset-capacity-exhausted"},
		{diskspace.ErrCapacity, http.StatusInsufficientStorage, "asset-capacity-exhausted"},
	} {
		status, kind, _ := assetHTTPError(testCase.err)
		if status != testCase.status || kind != testCase.kind {
			t.Errorf("error %v = (%d,%q)", testCase.err, status, kind)
		}
	}
}

func TestAssetIngestionServingAndSummaryImage(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	image := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("savorum-test-image")...)
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

	assetID := "sha256:" + digest

	var uploaded assetBlob
	if err := json.Unmarshal(upload.Body.Bytes(), &uploaded); err != nil || uploaded.ID != assetID {
		t.Fatalf("upload body=%s err=%v", upload.Body.String(), err)
	}

	if location := upload.Header().Get("Location"); location != "" {
		t.Fatalf("upload location=%q names no routable resource", location)
	}

	document := testfixture.Document()
	document.Assets[0].Path = "assets/finished-pies.png"
	document.Assets[0].MediaType = "image/png"
	document.Assets[0].SHA256 = digest
	document.Assets[0].Size = int64(len(image))
	document.InstructionSections[1].Steps[1].ImageRefs = []string{document.Assets[0].ID}

	encoded, err := json.Marshal(fromDomain(document))
	if err != nil {
		t.Fatal(err)
	}

	created := request(t, handler, http.MethodPost, "/api/v1/recipes", encoded)
	if created.Code != http.StatusCreated {
		t.Fatalf("create with asset status=%d body=%s", created.Code, created.Body.String())
	}

	var entry LibraryEntry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}

	deliveryURL := "/api/v1/recipes/" + entry.ID + "/assets/" + assetID

	listed := request(t, handler, http.MethodGet, "/api/v1/recipes?limit=10", nil)
	if !strings.Contains(listed.Body.String(), `"url":"`+deliveryURL+`"`) || strings.Contains(listed.Body.String(), `"recipe"`) {
		t.Fatalf("summary image missing or full recipe leaked: %s", listed.Body.String())
	}

	served := request(t, handler, http.MethodGet, deliveryURL, nil)
	if served.Code != http.StatusOK || !bytes.Equal(served.Body.Bytes(), image) {
		t.Fatalf("served asset status=%d body=%x", served.Code, served.Body.Bytes())
	}

	if served.Header().Get("Cache-Control") != "private, no-store, max-age=0" || served.Header().Get("ETag") != `"`+assetID+`"` {
		t.Fatalf("private headers cache=%q etag=%q", served.Header().Get("Cache-Control"), served.Header().Get("ETag"))
	}

	if served.Header().Get("X-Content-Type-Options") != "nosniff" || served.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("asset security headers nosniff=%q csp=%q", served.Header().Get("X-Content-Type-Options"), served.Header().Get("Content-Security-Policy"))
	}

	proxy := newTestAuthenticatedSharedCache(handler)
	authenticatedRequest := httptest.NewRequestWithContext(t.Context(), http.MethodGet, deliveryURL, nil)
	authenticatedRequest.Header.Set("Authorization", "Bearer active-session")

	authenticated := httptest.NewRecorder()
	proxy.ServeHTTP(authenticated, authenticatedRequest)

	if authenticated.Code != http.StatusOK {
		t.Fatalf("authenticated proxy asset status=%d body=%s", authenticated.Code, authenticated.Body.String())
	}

	loggedOut := httptest.NewRecorder()
	proxy.ServeHTTP(loggedOut, httptest.NewRequestWithContext(t.Context(), http.MethodGet, deliveryURL, nil))

	if loggedOut.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out request was replayed from shared cache status=%d body=%s", loggedOut.Code, loggedOut.Body.String())
	}

	byDigest := request(t, handler, http.MethodGet, "/api/v1/assets/"+assetID, nil)
	if byDigest.Code != http.StatusNotFound {
		t.Fatalf("digest-only delivery status=%d body=%s", byDigest.Code, byDigest.Body.String())
	}

	otherDocument := testfixture.Document()
	otherDocument.ID = recipe.RecipeID("r_asset_scope_other")
	otherDocument.Assets = nil
	otherDocument.PrimaryImage = ""

	otherDocument.ImageRefs = nil
	for sectionIndex := range otherDocument.InstructionSections {
		for stepIndex := range otherDocument.InstructionSections[sectionIndex].Steps {
			otherDocument.InstructionSections[sectionIndex].Steps[stepIndex].ImageRefs = nil
		}
	}

	otherBody, err := json.Marshal(fromDomain(otherDocument))
	if err != nil {
		t.Fatal(err)
	}

	otherCreated := request(t, handler, http.MethodPost, "/api/v1/recipes", otherBody)
	if otherCreated.Code != http.StatusCreated {
		t.Fatalf("create other recipe status=%d body=%s", otherCreated.Code, otherCreated.Body.String())
	}

	var otherEntry LibraryEntry
	if err := json.Unmarshal(otherCreated.Body.Bytes(), &otherEntry); err != nil {
		t.Fatal(err)
	}

	wrongReference := request(t, handler, http.MethodGet, "/api/v1/recipes/"+otherEntry.ID+"/assets/"+assetID, nil)
	if wrongReference.Code != http.StatusNotFound {
		t.Fatalf("different recipe reference status=%d body=%s", wrongReference.Code, wrongReference.Body.String())
	}

	assetReference := document.Assets[0]
	document.Assets = nil
	document.PrimaryImage = ""

	document.ImageRefs = nil
	for sectionIndex := range document.InstructionSections {
		for stepIndex := range document.InstructionSections[sectionIndex].Steps {
			document.InstructionSections[sectionIndex].Steps[stepIndex].ImageRefs = nil
		}
	}

	withoutAsset, err := json.Marshal(fromDomain(document))
	if err != nil {
		t.Fatal(err)
	}

	updated := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/recipes/"+entry.ID, withoutAsset, `"1"`)
	if updated.Code != http.StatusOK {
		t.Fatalf("remove asset reference status=%d body=%s", updated.Code, updated.Body.String())
	}

	revoked := request(t, handler, http.MethodGet, deliveryURL, nil)
	if revoked.Code != http.StatusNotFound {
		t.Fatalf("removed reference remained accessible status=%d body=%s", revoked.Code, revoked.Body.String())
	}

	document.Assets = []recipe.Asset{assetReference}

	readdedBody, err := json.Marshal(fromDomain(document))
	if err != nil {
		t.Fatal(err)
	}

	readded := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/recipes/"+entry.ID, readdedBody, `"2"`)
	if readded.Code != http.StatusOK {
		t.Fatalf("restore asset reference status=%d body=%s", readded.Code, readded.Body.String())
	}

	trashed := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/recipes/"+entry.ID, nil, `"3"`)
	if trashed.Code != http.StatusOK {
		t.Fatalf("trash referenced recipe status=%d body=%s", trashed.Code, trashed.Body.String())
	}

	if afterTrash := request(t, handler, http.MethodGet, deliveryURL, nil); afterTrash.Code != http.StatusNotFound {
		t.Fatalf("trashed recipe authorized asset status=%d body=%s", afterTrash.Code, afterTrash.Body.String())
	}

	purged := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/trash/recipes/"+entry.ID, nil, `"4"`)
	if purged.Code != http.StatusNoContent {
		t.Fatalf("purge referenced recipe status=%d body=%s", purged.Code, purged.Body.String())
	}

	if afterPurge := request(t, handler, http.MethodGet, deliveryURL, nil); afterPurge.Code != http.StatusNotFound {
		t.Fatalf("purged recipe authorized asset status=%d body=%s", afterPurge.Code, afterPurge.Body.String())
	}

	badRequest := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/assets", bytes.NewReader(image))
	badRequest.Header.Set("Content-Type", "application/octet-stream")
	badRequest.Header.Set("X-Asset-Media-Type", "image/jpeg")

	badUpload := httptest.NewRecorder()
	handler.ServeHTTP(badUpload, badRequest)

	if badUpload.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mismatched media type status=%d body=%s", badUpload.Code, badUpload.Body.String())
	}
}

func TestActiveAssetContentIsDownloadOnly(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	content := []byte("<!doctype html><script>top.location='https://attacker.invalid'</script>")
	uploadRequest := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/assets", bytes.NewReader(content))
	uploadRequest.Header.Set("Content-Type", "application/octet-stream")
	uploadRequest.Header.Set("X-Asset-Media-Type", "text/html")

	upload := httptest.NewRecorder()
	handler.ServeHTTP(upload, uploadRequest)

	if upload.Code != http.StatusCreated {
		t.Fatalf("upload active asset status=%d body=%s", upload.Code, upload.Body.String())
	}

	var stored assetBlob
	if err := json.Unmarshal(upload.Body.Bytes(), &stored); err != nil {
		t.Fatal(err)
	}

	document := testfixture.Document()
	document.ID = recipe.RecipeID("r_active_download")
	document.Assets = []recipe.Asset{{
		ID: "unsafe", Path: "assets/content.html", MediaType: "text/html",
		SHA256: stored.SHA256, Size: stored.Size, Role: recipe.AssetRoleOther,
	}}
	document.PrimaryImage = ""

	document.ImageRefs = nil
	for sectionIndex := range document.InstructionSections {
		for stepIndex := range document.InstructionSections[sectionIndex].Steps {
			document.InstructionSections[sectionIndex].Steps[stepIndex].ImageRefs = nil
		}
	}

	body, err := json.Marshal(fromDomain(document))
	if err != nil {
		t.Fatal(err)
	}

	created := request(t, handler, http.MethodPost, "/api/v1/recipes", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create active reference status=%d body=%s", created.Code, created.Body.String())
	}

	var entry LibraryEntry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}

	download := request(t, handler, http.MethodGet, "/api/v1/recipes/"+entry.ID+"/assets/"+stored.ID, nil)
	if download.Code != http.StatusOK || download.Header().Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(download.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("active download status=%d type=%q disposition=%q", download.Code, download.Header().Get("Content-Type"), download.Header().Get("Content-Disposition"))
	}
}

func TestBookCoverDeliveryRequiresTheActiveReferencingBook(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	image := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("book-cover")...)
	uploadRequest := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/assets", bytes.NewReader(image))
	uploadRequest.Header.Set("Content-Type", "application/octet-stream")
	uploadRequest.Header.Set("X-Asset-Media-Type", "image/png")

	upload := httptest.NewRecorder()
	handler.ServeHTTP(upload, uploadRequest)

	if upload.Code != http.StatusCreated {
		t.Fatalf("upload cover status=%d body=%s", upload.Code, upload.Body.String())
	}

	var stored assetBlob
	if err := json.Unmarshal(upload.Body.Bytes(), &stored); err != nil {
		t.Fatal(err)
	}

	book := createBook(t, handler, map[string]any{
		"kind": "personal", "title": "Covered", "cover_asset_id": stored.ID,
		"sections": []any{}, "entries": []any{},
	})
	other := createBook(t, handler, map[string]any{
		"kind": "personal", "title": "Other", "sections": []any{}, "entries": []any{},
	})

	deliveryURL := "/api/v1/books/" + book.ID + "/assets/" + stored.ID
	if response := request(t, handler, http.MethodGet, deliveryURL, nil); response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), image) {
		t.Fatalf("active book cover status=%d body=%x", response.Code, response.Body.Bytes())
	}

	if response := request(t, handler, http.MethodGet, "/api/v1/books/"+other.ID+"/assets/"+stored.ID, nil); response.Code != http.StatusNotFound {
		t.Fatalf("unrelated book authorized cover status=%d body=%s", response.Code, response.Body.String())
	}

	if response := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/books/"+book.ID, nil, `"1"`); response.Code != http.StatusNoContent {
		t.Fatalf("trash book status=%d body=%s", response.Code, response.Body.String())
	}

	if response := request(t, handler, http.MethodGet, deliveryURL, nil); response.Code != http.StatusNotFound {
		t.Fatalf("trashed book authorized cover status=%d body=%s", response.Code, response.Body.String())
	}
}
