package v1

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/testfixture"
)

func TestBookRevisionTracksMemberRecipeLifecycle(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	document, err := json.Marshal(fromDomain(testfixture.Document()))
	if err != nil {
		t.Fatal(err)
	}

	createdResponse := request(t, handler, http.MethodPost, "/api/v1/recipes", document)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create recipe status=%d body=%s", createdResponse.Code, createdResponse.Body.String())
	}

	var created LibraryEntry
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	bookInput := map[string]any{
		"kind": "personal", "title": "Lifecycle book", "sections": []any{},
		"entries": []map[string]any{{"id": "entry-lifecycle", "library_entry_id": created.ID, "position": 0}},
	}

	bookBody, err := json.Marshal(bookInput)
	if err != nil {
		t.Fatal(err)
	}

	createdBook := request(t, handler, http.MethodPost, "/api/v1/books", bookBody)
	if createdBook.Code != http.StatusCreated || createdBook.Header().Get("ETag") != `"1"` {
		t.Fatalf("create book status=%d ETag=%q body=%s", createdBook.Code, createdBook.Header().Get("ETag"), createdBook.Body.String())
	}

	var book Book
	if err := json.Unmarshal(createdBook.Body.Bytes(), &book); err != nil {
		t.Fatal(err)
	}

	if len(book.Entries) != 1 || book.Entries[0].RecipeState != "active" {
		t.Fatalf("active book member state missing: %s", createdBook.Body.String())
	}

	bookPath := "/api/v1/books/" + book.ID

	summaries := request(t, handler, http.MethodGet, "/api/v1/books?limit=1&offset=0&entry_id="+created.ID, nil)
	if summaries.Code != http.StatusOK {
		t.Fatalf("list book summaries status=%d body=%s", summaries.Code, summaries.Body.String())
	}

	var summaryPage struct {
		Items  []BookSummary `json:"items"`
		Total  int           `json:"total"`
		Limit  int           `json:"limit"`
		Offset int           `json:"offset"`
	}
	if err := json.Unmarshal(summaries.Body.Bytes(), &summaryPage); err != nil {
		t.Fatal(err)
	}

	if len(summaryPage.Items) != 1 || summaryPage.Total != 1 || summaryPage.Limit != 1 || summaryPage.Offset != 0 || summaryPage.Items[0].EntryCount != 1 || !summaryPage.Items[0].ContainsEntry {
		t.Fatalf("book summary page = %#v", summaryPage)
	}

	if strings.Contains(summaries.Body.String(), `"sections"`) || strings.Contains(summaries.Body.String(), `"entries"`) {
		t.Fatalf("book summary embedded aggregate data: %s", summaries.Body.String())
	}

	trashed := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/recipes/"+created.ID, nil, `"1"`)
	if trashed.Code != http.StatusOK || trashed.Header().Get("ETag") != `"2"` {
		t.Fatalf("trash recipe status=%d ETag=%q body=%s", trashed.Code, trashed.Header().Get("ETag"), trashed.Body.String())
	}

	trashedBook := request(t, handler, http.MethodGet, bookPath, nil)
	if trashedBook.Code != http.StatusOK || trashedBook.Header().Get("ETag") != `"2"` {
		t.Fatalf("book after trash status=%d ETag=%q body=%s", trashedBook.Code, trashedBook.Header().Get("ETag"), trashedBook.Body.String())
	}

	if err := json.Unmarshal(trashedBook.Body.Bytes(), &book); err != nil {
		t.Fatal(err)
	}

	if len(book.Entries) != 1 || book.Entries[0].RecipeState != "trash" {
		t.Fatalf("trashed member was not retained and marked: %s", trashedBook.Body.String())
	}

	if stale := requestWithIfMatch(t, handler, http.MethodPut, bookPath, bookBody, `"1"`); stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale book replacement status=%d body=%s", stale.Code, stale.Body.String())
	}

	if newBook := request(t, handler, http.MethodPost, "/api/v1/books", bookBody); newBook.Code != http.StatusConflict || !strings.Contains(newBook.Body.String(), "organization-state-conflict") {
		t.Fatalf("new membership for trashed recipe status=%d body=%s", newBook.Code, newBook.Body.String())
	}

	bookInput["title"] = "Edited while member is in trash"

	bookBody, err = json.Marshal(bookInput)
	if err != nil {
		t.Fatal(err)
	}

	editedBook := requestWithIfMatch(t, handler, http.MethodPut, bookPath, bookBody, `"2"`)
	if editedBook.Code != http.StatusOK || editedBook.Header().Get("ETag") != `"3"` {
		t.Fatalf("edit retained trash membership status=%d ETag=%q body=%s", editedBook.Code, editedBook.Header().Get("ETag"), editedBook.Body.String())
	}

	if err := json.Unmarshal(editedBook.Body.Bytes(), &book); err != nil {
		t.Fatal(err)
	}

	if len(book.Entries) != 1 || book.Entries[0].RecipeState != "trash" {
		t.Fatalf("book edit lost retained trash membership: %s", editedBook.Body.String())
	}

	restored := requestWithIfMatch(t, handler, http.MethodPost, "/api/v1/recipes/"+created.ID+"/restore", nil, `"2"`)
	if restored.Code != http.StatusOK || restored.Header().Get("ETag") != `"3"` {
		t.Fatalf("restore recipe status=%d ETag=%q body=%s", restored.Code, restored.Header().Get("ETag"), restored.Body.String())
	}

	restoredBook := request(t, handler, http.MethodGet, bookPath, nil)
	if restoredBook.Code != http.StatusOK || restoredBook.Header().Get("ETag") != `"4"` {
		t.Fatalf("book after restore status=%d ETag=%q body=%s", restoredBook.Code, restoredBook.Header().Get("ETag"), restoredBook.Body.String())
	}

	if err := json.Unmarshal(restoredBook.Body.Bytes(), &book); err != nil {
		t.Fatal(err)
	}

	if book.Entries[0].RecipeState != "active" {
		t.Fatalf("restored member state=%q", book.Entries[0].RecipeState)
	}

	trashedAgain := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/recipes/"+created.ID, nil, `"3"`)
	if trashedAgain.Code != http.StatusOK || trashedAgain.Header().Get("ETag") != `"4"` {
		t.Fatalf("second trash status=%d ETag=%q body=%s", trashedAgain.Code, trashedAgain.Header().Get("ETag"), trashedAgain.Body.String())
	}

	if response := request(t, handler, http.MethodGet, bookPath, nil); response.Header().Get("ETag") != `"5"` {
		t.Fatalf("book after second trash ETag=%q body=%s", response.Header().Get("ETag"), response.Body.String())
	}

	purged := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/trash/recipes/"+created.ID, nil, `"4"`)
	if purged.Code != http.StatusNoContent {
		t.Fatalf("purge recipe status=%d body=%s", purged.Code, purged.Body.String())
	}

	purgedBook := request(t, handler, http.MethodGet, bookPath, nil)
	if purgedBook.Code != http.StatusOK || purgedBook.Header().Get("ETag") != `"6"` {
		t.Fatalf("book after purge status=%d ETag=%q body=%s", purgedBook.Code, purgedBook.Header().Get("ETag"), purgedBook.Body.String())
	}

	if err := json.Unmarshal(purgedBook.Body.Bytes(), &book); err != nil {
		t.Fatal(err)
	}

	if len(book.Entries) != 0 {
		t.Fatalf("purged member remains in book: %s", purgedBook.Body.String())
	}
}

func TestBookTrashHistoryRestoreAndPurgeAPI(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	body, err := json.Marshal(map[string]any{
		"kind": "personal", "title": "Recoverable book", "sections": []any{}, "entries": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}

	created := request(t, handler, http.MethodPost, "/api/v1/books", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create book status=%d body=%s", created.Code, created.Body.String())
	}

	var book Book
	if err := json.Unmarshal(created.Body.Bytes(), &book); err != nil {
		t.Fatal(err)
	}

	bookPath := "/api/v1/books/" + book.ID

	trashed := requestWithIfMatch(t, handler, http.MethodDelete, bookPath, nil, `"1"`)
	if trashed.Code != http.StatusNoContent {
		t.Fatalf("trash book status=%d body=%s", trashed.Code, trashed.Body.String())
	}

	if active := request(t, handler, http.MethodGet, bookPath, nil); active.Code != http.StatusNotFound {
		t.Fatalf("trashed book active status=%d body=%s", active.Code, active.Body.String())
	}

	trashRead := request(t, handler, http.MethodGet, "/api/v1/trash/books/"+book.ID, nil)
	if trashRead.Code != http.StatusOK || trashRead.Header().Get("ETag") != `"2"` || !strings.Contains(trashRead.Body.String(), `"deleted_at"`) {
		t.Fatalf("read trashed book status=%d ETag=%q body=%s", trashRead.Code, trashRead.Header().Get("ETag"), trashRead.Body.String())
	}

	history := request(t, handler, http.MethodGet, bookPath+"/revisions?limit=10", nil)
	if history.Code != http.StatusOK || !strings.Contains(history.Body.String(), `"cause":"trash"`) || !strings.Contains(history.Body.String(), `"revision":1`) {
		t.Fatalf("book history status=%d body=%s", history.Code, history.Body.String())
	}

	restored := requestWithIfMatch(t, handler, http.MethodPost, bookPath+"/revisions/1/restore", nil, `"2"`)
	if restored.Code != http.StatusOK || restored.Header().Get("ETag") != `"3"` {
		t.Fatalf("restore book revision status=%d ETag=%q body=%s", restored.Code, restored.Header().Get("ETag"), restored.Body.String())
	}

	trashed = requestWithIfMatch(t, handler, http.MethodDelete, bookPath, nil, `"3"`)
	if trashed.Code != http.StatusNoContent {
		t.Fatalf("trash restored book status=%d body=%s", trashed.Code, trashed.Body.String())
	}

	purged := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/trash/books/"+book.ID, nil, `"4"`)
	if purged.Code != http.StatusNoContent {
		t.Fatalf("purge book status=%d body=%s", purged.Code, purged.Body.String())
	}

	if retained := request(t, handler, http.MethodGet, bookPath+"/revisions/1", nil); retained.Code != http.StatusNotFound {
		t.Fatalf("purged book retained history status=%d body=%s", retained.Code, retained.Body.String())
	}
}

func TestOrganizationAndBookMembershipAreOneAtomicWrite(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	created := createRecipe(t, handler, testfixture.Document())
	organizationPath := "/api/v1/recipes/" + created.ID + "/organization"

	personal := createBook(t, handler, map[string]any{
		"kind": "personal", "title": "Weeknight Favorites",
		"sections": []any{}, "entries": []any{},
	})
	other := createBook(t, handler, map[string]any{
		"kind": "personal", "title": "Sunday Cooking",
		"sections": []any{}, "entries": []any{},
	})

	// One save that names a book revision which is already stale must leave
	// every aggregate exactly as it was, including the organization itself.
	staleBody := organizationJSON(t, map[string]any{
		"user_state": map[string]any{"favorite": true},
		"book_membership": []map[string]any{
			{"book_id": personal.ID, "revision": 1, "member": true},
			{"book_id": other.ID, "revision": 99, "member": true},
		},
	})

	staleResponse := requestWithIfMatch(t, handler, http.MethodPut, organizationPath, staleBody, `"1"`)
	if staleResponse.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale book membership status=%d body=%s", staleResponse.Code, staleResponse.Body.String())
	}

	unchanged := request(t, handler, http.MethodGet, organizationPath, nil)
	if unchanged.Header().Get("ETag") != `"1"` {
		t.Fatalf("failed membership write still advanced organization: ETag=%q", unchanged.Header().Get("ETag"))
	}

	if book := readBook(t, handler, personal.ID); len(book.Entries) != 0 || book.Revision != 1 {
		t.Fatalf("failed membership write still changed a book: %#v", book)
	}

	goodBody := organizationJSON(t, map[string]any{
		"user_state": map[string]any{"favorite": true},
		"book_membership": []map[string]any{
			{"book_id": personal.ID, "revision": 1, "member": true},
			{"book_id": other.ID, "revision": 1, "member": false},
		},
	})

	applied := requestWithIfMatch(t, handler, http.MethodPut, organizationPath, goodBody, `"1"`)
	if applied.Code != http.StatusOK || applied.Header().Get("ETag") != `"2"` {
		t.Fatalf("membership write status=%d ETag=%q body=%s", applied.Code, applied.Header().Get("ETag"), applied.Body.String())
	}

	joined := readBook(t, handler, personal.ID)
	if len(joined.Entries) != 1 || joined.Entries[0].LibraryEntryID != created.ID || joined.Revision != 2 {
		t.Fatalf("membership was not applied: %#v", joined)
	}
	// A book whose membership already matched must not advance a revision.
	if untouched := readBook(t, handler, other.ID); untouched.Revision != 1 {
		t.Fatalf("unchanged membership advanced a book revision: %#v", untouched)
	}
}

func TestBookMembershipAndCookbookProvenanceStayDistinct(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	created := createRecipe(t, handler, testfixture.Document())
	organizationPath := "/api/v1/recipes/" + created.ID + "/organization"
	personal := createBook(t, handler, map[string]any{"kind": "personal", "title": "Weeknight Favorites", "sections": []any{}, "entries": []any{}})
	publication := createBook(t, handler, map[string]any{"kind": "publication", "title": "Test Kitchen Compendium", "sections": []any{}, "entries": []any{}})

	body := organizationJSON(t, map[string]any{
		"user_state":      map[string]any{"favorite": false},
		"sources":         []map[string]any{{"kind": "publication", "publication_book_id": publication.ID}},
		"book_membership": []map[string]any{{"book_id": personal.ID, "revision": 1, "member": true}},
	})
	if applied := requestWithIfMatch(t, handler, http.MethodPut, organizationPath, body, `"1"`); applied.Code != http.StatusOK {
		t.Fatalf("organize status=%d body=%s", applied.Code, applied.Body.String())
	}

	summaries := listSummaries(t, handler, "")
	if len(summaries) != 1 {
		t.Fatalf("expected one summary, got %d", len(summaries))
	}

	summary := summaries[0]
	if len(summary.Books) != 1 || summary.Books[0].ID != personal.ID {
		t.Fatalf("summary membership should name only the book the recipe was placed in: %#v", summary.Books)
	}

	if len(summary.SourceBooks) != 1 || summary.SourceBooks[0].ID != publication.ID {
		t.Fatalf("summary provenance should name only the book it was taken from: %#v", summary.SourceBooks)
	}

	if found := listSummaries(t, handler, "book:"+personal.ID); len(found) != 1 {
		t.Fatalf("book identifier should match curated membership, got %d", len(found))
	}

	if found := listSummaries(t, handler, "book:"+publication.ID); len(found) != 0 {
		t.Fatalf("book search must not match cookbook provenance, got %d", len(found))
	}

	if found := listSummaries(t, handler, "source:"+publication.ID); len(found) != 1 {
		t.Fatalf("source search should match cookbook provenance, got %d", len(found))
	}
}

func TestEmptyTrashNamesTheBookThatStoppedIt(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	created := createRecipe(t, handler, testfixture.Document())
	publication := createBook(t, handler, map[string]any{"kind": "publication", "title": "Test Kitchen Compendium", "sections": []any{}, "entries": []any{}})

	// An active recipe records the book as where it came from, so the book is
	// still cited and cannot be deleted permanently.
	body := organizationJSON(t, map[string]any{
		"sources": []map[string]any{{"kind": "publication", "publication_book_id": publication.ID}},
	})
	if applied := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/recipes/"+created.ID+"/organization", body, `"1"`); applied.Code != http.StatusOK {
		t.Fatalf("organize status=%d body=%s", applied.Code, applied.Body.String())
	}

	if response := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/books/"+publication.ID, nil, `"1"`); response.Code != http.StatusNoContent {
		t.Fatalf("trash book status=%d body=%s", response.Code, response.Body.String())
	}

	batch, err := json.Marshal(map[string]any{"books": []map[string]any{{"id": publication.ID, "revision": 2}}})
	if err != nil {
		t.Fatal(err)
	}

	response := request(t, handler, http.MethodPost, "/api/v1/trash/empty", batch)
	if response.Code != http.StatusConflict {
		t.Fatalf("cited book status=%d body=%s", response.Code, response.Body.String())
	}
	// The reader is told which book stopped the batch, by its title rather than
	// by an identifier, and is not told a recipe already exists.
	if !strings.Contains(response.Body.String(), "book-conflict") ||
		!strings.Contains(response.Body.String(), "Test Kitchen Compendium") ||
		strings.Contains(response.Body.String(), "recipe already exists") {
		t.Fatalf("a book conflict must name the book rather than a recipe: %s", response.Body.String())
	}
}

func TestEmptyTrashRemovesReviewedRecipesAndBooksTogether(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	created := createRecipe(t, handler, testfixture.Document())
	book := createBook(t, handler, map[string]any{"kind": "personal", "title": "Retired ideas", "sections": []any{}, "entries": []any{}})

	if response := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/recipes/"+created.ID, nil, `"1"`); response.Code != http.StatusOK {
		t.Fatalf("trash recipe status=%d body=%s", response.Code, response.Body.String())
	}

	if response := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/books/"+book.ID, nil, `"1"`); response.Code != http.StatusNoContent {
		t.Fatalf("trash book status=%d body=%s", response.Code, response.Body.String())
	}

	stale, err := json.Marshal(map[string]any{
		"recipes": []map[string]any{{"id": created.ID, "revision": 2}},
		"books":   []map[string]any{{"id": book.ID, "revision": 99}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if response := request(t, handler, http.MethodPost, "/api/v1/trash/empty", stale); response.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale mixed batch status=%d body=%s", response.Code, response.Body.String())
	}

	if response := request(t, handler, http.MethodGet, "/api/v1/trash/recipes/"+created.ID, nil); response.Code != http.StatusOK {
		t.Fatalf("failed batch removed a trashed recipe: status=%d", response.Code)
	}

	reviewed, err := json.Marshal(map[string]any{
		"recipes": []map[string]any{{"id": created.ID, "revision": 2}},
		"books":   []map[string]any{{"id": book.ID, "revision": 2}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if response := request(t, handler, http.MethodPost, "/api/v1/trash/empty", reviewed); response.Code != http.StatusNoContent {
		t.Fatalf("empty trash status=%d body=%s", response.Code, response.Body.String())
	}

	if response := request(t, handler, http.MethodGet, "/api/v1/trash/recipes/"+created.ID, nil); response.Code != http.StatusNotFound {
		t.Fatalf("trashed recipe survived: status=%d", response.Code)
	}

	if response := request(t, handler, http.MethodGet, "/api/v1/trash/books/"+book.ID, nil); response.Code != http.StatusNotFound {
		t.Fatalf("trashed book survived: status=%d", response.Code)
	}
}

func TestBookNamesAMissingMemberRecipe(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	body := []byte(`{"kind":"personal","title":"Ghosts","sections":[],"entries":[{"id":"entry","library_entry_id":"r-does-not-exist","position":0}]}`)
	if created := request(t, handler, http.MethodPost, "/api/v1/books", body); created.Code != http.StatusNotFound || !strings.Contains(created.Body.String(), `"code":"recipe-not-found"`) {
		t.Fatalf("book with a missing recipe status=%d body=%s", created.Code, created.Body.String())
	}
}

func TestBookIdentitiesHeldByAnotherBookAreConflicts(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	document := fromDomain(testfixture.Document())

	recipeBody, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	created := request(t, handler, http.MethodPost, "/api/v1/recipes", recipeBody)
	if created.Code != http.StatusCreated {
		t.Fatalf("create recipe: %d %s", created.Code, created.Body.String())
	}

	recipeID := strings.TrimPrefix(created.Header().Get("Location"), "/api/v1/recipes/")

	for _, body := range []string{
		`{"kind":"personal","title":"Shared section","sections":[{"id":"sec-shared","title":"Soups","position":0}],"entries":[]}`,
		`{"kind":"personal","title":"Shared entry","sections":[],"entries":[{"id":"be-shared","library_entry_id":"` + recipeID + `","position":0}]}`,
	} {
		if first := request(t, handler, http.MethodPost, "/api/v1/books", []byte(body)); first.Code != http.StatusCreated {
			t.Fatalf("first book: %d %s", first.Code, first.Body.String())
		}

		if second := request(t, handler, http.MethodPost, "/api/v1/books", []byte(body)); second.Code != http.StatusConflict || !strings.Contains(second.Body.String(), "book-identity-conflict") {
			t.Fatalf("second book: %d %s", second.Code, second.Body.String())
		}
	}
}
