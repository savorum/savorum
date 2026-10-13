package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestOrganizationBooksProvenanceAndSavedViewsEndToEnd(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	document := fromDomain(testfixture.Document())

	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	createdResponse := request(t, handler, http.MethodPost, "/api/v1/recipes", encoded)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create recipe status = %d; body = %s", createdResponse.Code, createdResponse.Body.String())
	}

	var created LibraryEntry
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(createdResponse.Body.String(), `"organization":`) {
		t.Fatalf("recipe representation contains independently versioned organization: %s", createdResponse.Body.String())
	}

	recipeBody := createdResponse.Body.String()
	recipeETag := createdResponse.Header().Get("ETag")
	organizationPath := "/api/v1/recipes/" + created.ID + "/organization"

	initialOrganization := request(t, handler, http.MethodGet, organizationPath, nil)
	if initialOrganization.Code != http.StatusOK || initialOrganization.Header().Get("ETag") != `"1"` {
		t.Fatalf("initial organization status=%d ETag=%q body=%s", initialOrganization.Code, initialOrganization.Header().Get("ETag"), initialOrganization.Body.String())
	}

	mainFacet := createFacet(t, handler, "course", "Main")
	italianFacet := createFacet(t, handler, "cuisine", "Italian")
	bakingFacet := createFacet(t, handler, "method", "Baking")
	weeknightTag := createTag(t, handler, "Weeknight", "#53745b")
	needsTweakingTag := createTag(t, handler, "Needs tweaking", "")

	organizationBody, err := json.Marshal(map[string]any{
		"facet_ids":  []string{mainFacet.ID, italianFacet.ID, bakingFacet.ID},
		"tag_ids":    []string{weeknightTag.ID, needsTweakingTag.ID},
		"user_state": map[string]any{"favorite": true, "rating": 5, "personal_note": "Double the sauce."},
		"sources":    []any{},
	})
	if err != nil {
		t.Fatal(err)
	}

	organized := requestWithIfMatch(t, handler, http.MethodPut, organizationPath, organizationBody, `"1"`)
	if organized.Code != http.StatusOK || organized.Header().Get("ETag") != `"2"` {
		t.Fatalf("organize status = %d ETag=%q; body=%s", organized.Code, organized.Header().Get("ETag"), organized.Body.String())
	}

	var organization RecipeOrganization
	if err := json.Unmarshal(organized.Body.Bytes(), &organization); err != nil {
		t.Fatal(err)
	}

	if len(organization.FacetIDs) != 3 || len(organization.TagIDs) != 2 || !organization.UserState.Favorite {
		t.Fatalf("organization did not round trip: %s", organized.Body.String())
	}

	stale := requestWithIfMatch(t, handler, http.MethodPut, organizationPath, organizationBody, `"1"`)
	if stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale organization status=%d body=%s", stale.Code, stale.Body.String())
	}

	search := request(t, handler, http.MethodGet, "/api/v1/recipes?q=tag%3Aweeknight&limit=10", nil)
	if search.Code != http.StatusOK || !strings.Contains(search.Body.String(), created.ID) {
		t.Fatalf("tag search status=%d body=%s", search.Code, search.Body.String())
	}

	plainTagSearch := request(t, handler, http.MethodGet, "/api/v1/recipes?q=weeknight&limit=10", nil)
	if plainTagSearch.Code != http.StatusOK || !strings.Contains(plainTagSearch.Body.String(), created.ID) {
		t.Fatalf("plain tag search status=%d body=%s", plainTagSearch.Code, plainTagSearch.Body.String())
	}

	recipeRead := request(t, handler, http.MethodGet, "/api/v1/recipes/"+created.ID, nil)

	var current LibraryEntry
	if err := json.Unmarshal(recipeRead.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}

	if current.Revision != 1 || current.Recipe.Cuisines[0] != "Australian" || recipeRead.Header().Get("ETag") != recipeETag || recipeRead.Body.String() != recipeBody {
		t.Fatalf("organization changed recipe representation or revision: ETag=%q body=%s", recipeRead.Header().Get("ETag"), recipeRead.Body.String())
	}

	personalBody, err := json.Marshal(map[string]any{"kind": "personal", "title": "Weeknight Favorites", "sections": []map[string]any{{"id": "section-dinner", "title": "Dinner", "position": 0}}, "entries": []map[string]any{{"id": "entry-pies", "section_id": "section-dinner", "library_entry_id": created.ID, "position": 0, "note": "Make 2×."}}})
	if err != nil {
		t.Fatal(err)
	}

	personalResponse := request(t, handler, http.MethodPost, "/api/v1/books", personalBody)
	if personalResponse.Code != http.StatusCreated {
		t.Fatalf("create personal book status=%d body=%s", personalResponse.Code, personalResponse.Body.String())
	}

	var personal Book
	if err := json.Unmarshal(personalResponse.Body.Bytes(), &personal); err != nil {
		t.Fatal(err)
	}

	if len(personal.Entries) != 1 || personal.Entries[0].Note != "Make 2×." {
		t.Fatalf("book entry context missing: %s", personalResponse.Body.String())
	}

	publicationBody, err := json.Marshal(map[string]any{"kind": "publication", "title": "Test Kitchen Compendium", "author": "A. Cook", "isbn": "9780000000000", "publisher": "Example Press", "publication_date": "2020-01-01", "sections": []any{}, "entries": []any{}})
	if err != nil {
		t.Fatal(err)
	}

	publicationResponse := request(t, handler, http.MethodPost, "/api/v1/books", publicationBody)
	if publicationResponse.Code != http.StatusCreated {
		t.Fatalf("create publication status=%d body=%s", publicationResponse.Code, publicationResponse.Body.String())
	}

	var publication Book
	if err := json.Unmarshal(publicationResponse.Body.Bytes(), &publication); err != nil {
		t.Fatal(err)
	}

	publicationBodyBeforeSource := publicationResponse.Body.String()
	publicationETag := publicationResponse.Header().Get("ETag")
	page := uint64(312)
	organization.Sources = []RecipeSource{{Kind: "publication", PublicationBookID: publication.ID, PageStart: &page, OriginalTitle: "Vegetable Hand Pies", Attribution: "A. Cook"}}
	replacement := OrganizationInput{FacetIDs: organization.FacetIDs, TagIDs: organization.TagIDs, UserState: organization.UserState, Sources: organization.Sources}

	replacementJSON, err := json.Marshal(replacement)
	if err != nil {
		t.Fatal(err)
	}

	withSource := requestWithIfMatch(t, handler, http.MethodPut, organizationPath, replacementJSON, `"2"`)
	if withSource.Code != http.StatusOK {
		t.Fatalf("add publication source status=%d body=%s", withSource.Code, withSource.Body.String())
	}

	publicationRead := request(t, handler, http.MethodGet, "/api/v1/books/"+publication.ID, nil)
	if publicationRead.Code != http.StatusOK {
		t.Fatalf("get publication status=%d body=%s", publicationRead.Code, publicationRead.Body.String())
	}

	if err := json.Unmarshal(publicationRead.Body.Bytes(), &publication); err != nil {
		t.Fatal(err)
	}

	if publicationRead.Header().Get("ETag") != publicationETag || publicationRead.Body.String() != publicationBodyBeforeSource || strings.Contains(publicationRead.Body.String(), `"sourced_recipes"`) {
		t.Fatalf("recipe provenance changed book representation or revision: ETag=%q body=%s", publicationRead.Header().Get("ETag"), publicationRead.Body.String())
	}

	sourcesRead := request(t, handler, http.MethodGet, "/api/v1/books/"+publication.ID+"/sourced-recipes", nil)
	if sourcesRead.Code != http.StatusOK || sourcesRead.Header().Get("ETag") != "" {
		t.Fatalf("list book source recipes status=%d ETag=%q body=%s", sourcesRead.Code, sourcesRead.Header().Get("ETag"), sourcesRead.Body.String())
	}

	var sourced struct {
		Items []BookSourceRecipe `json:"items"`
	}
	if err := json.Unmarshal(sourcesRead.Body.Bytes(), &sourced); err != nil {
		t.Fatal(err)
	}

	if len(sourced.Items) != 1 || sourced.Items[0].LibraryEntryID != created.ID || sourced.Items[0].PageStart == nil || *sourced.Items[0].PageStart != 312 {
		t.Fatalf("publication provenance not exposed by its collection: %s", sourcesRead.Body.String())
	}

	viewBody, err := json.Marshal(map[string]any{"title": "Quick favorites", "query": "favorite:true course:main", "sort": "updated-desc"})
	if err != nil {
		t.Fatal(err)
	}

	viewResponse := request(t, handler, http.MethodPost, "/api/v1/saved-views", viewBody)
	if viewResponse.Code != http.StatusCreated {
		t.Fatalf("create saved view status=%d body=%s", viewResponse.Code, viewResponse.Body.String())
	}

	var view SavedView
	if err := json.Unmarshal(viewResponse.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}

	if view.Query != "favorite:true course:main" || view.Revision != 1 {
		t.Fatalf("saved view mismatch: %s", viewResponse.Body.String())
	}
}

func TestCanonicalTaxonomyLifecycleAndOrganizationAssignments(t *testing.T) {
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

	organizationPath := "/api/v1/recipes/" + created.ID + "/organization"

	sourceTag := createTag(t, handler, "Freezer Friendly", "#53745b")
	targetTag := createTag(t, handler, "Weeknight", "")

	duplicateTag, err := json.Marshal(TagInput{Name: " freezer friendly ", Color: "#ffffff"})
	if err != nil {
		t.Fatal(err)
	}

	if response := request(t, handler, http.MethodPost, "/api/v1/tags", duplicateTag); response.Code != http.StatusConflict {
		t.Fatalf("normalized duplicate tag status=%d body=%s", response.Code, response.Body.String())
	}

	italian := createFacet(t, handler, "cuisine", "Italian")
	tuscan := createFacet(t, handler, "cuisine", "Tuscan")

	unknownAssignment, err := json.Marshal(map[string]any{
		"facet_ids": []string{italian.ID}, "tag_ids": []string{"t-missing"},
		"user_state": map[string]any{"favorite": false}, "sources": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}

	if response := requestWithIfMatch(t, handler, http.MethodPut, organizationPath, unknownAssignment, `"1"`); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown taxonomy assignment status=%d body=%s", response.Code, response.Body.String())
	}

	embeddedDefinitions, err := json.Marshal(map[string]any{
		"facets":     []map[string]string{{"kind": "cuisine", "value": "Italian"}},
		"tags":       []map[string]string{{"name": "Weeknight"}},
		"user_state": map[string]any{"favorite": false}, "sources": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}

	if response := requestWithIfMatch(t, handler, http.MethodPut, organizationPath, embeddedDefinitions, `"1"`); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("embedded taxonomy definitions status=%d body=%s", response.Code, response.Body.String())
	}

	assignment, err := json.Marshal(map[string]any{
		"facet_ids": []string{italian.ID, tuscan.ID}, "tag_ids": []string{sourceTag.ID, targetTag.ID},
		"user_state": map[string]any{"favorite": false}, "sources": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}

	assigned := requestWithIfMatch(t, handler, http.MethodPut, organizationPath, assignment, `"1"`)
	if assigned.Code != http.StatusOK || assigned.Header().Get("ETag") != `"2"` {
		t.Fatalf("assign taxonomy status=%d ETag=%q body=%s", assigned.Code, assigned.Header().Get("ETag"), assigned.Body.String())
	}

	organizationBody := assigned.Body.String()

	rename, err := json.Marshal(TagInput{Name: "Zebra Freezer", Color: "#0000ff"})
	if err != nil {
		t.Fatal(err)
	}

	renamed := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/tags/"+sourceTag.ID, rename, `"1"`)
	if renamed.Code != http.StatusOK || renamed.Header().Get("ETag") != `"2"` {
		t.Fatalf("rename tag status=%d ETag=%q body=%s", renamed.Code, renamed.Header().Get("ETag"), renamed.Body.String())
	}

	if stale := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/tags/"+sourceTag.ID, rename, `"1"`); stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale tag replacement status=%d body=%s", stale.Code, stale.Body.String())
	}

	afterRename := request(t, handler, http.MethodGet, organizationPath, nil)
	if afterRename.Header().Get("ETag") != `"2"` || afterRename.Body.String() != organizationBody {
		t.Fatalf("tag definition changed organization representation: ETag=%q body=%s", afterRename.Header().Get("ETag"), afterRename.Body.String())
	}

	list := request(t, handler, http.MethodGet, "/api/v1/recipes?q=zebra&limit=10", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"name":"Zebra Freezer"`) || !strings.Contains(list.Body.String(), `"color":"#0000ff"`) {
		t.Fatalf("renamed tag missing from recipe projection: status=%d body=%s", list.Code, list.Body.String())
	}

	mergeTagBody, err := json.Marshal(map[string]string{"target_id": targetTag.ID})
	if err != nil {
		t.Fatal(err)
	}

	mergedTag := requestWithIfMatch(t, handler, http.MethodPost, "/api/v1/tags/"+sourceTag.ID+"/merge", mergeTagBody, `"2"`)
	if mergedTag.Code != http.StatusNoContent {
		t.Fatalf("merge tag status=%d body=%s", mergedTag.Code, mergedTag.Body.String())
	}

	if missing := request(t, handler, http.MethodGet, "/api/v1/tags/"+sourceTag.ID, nil); missing.Code != http.StatusNotFound {
		t.Fatalf("merged source tag status=%d body=%s", missing.Code, missing.Body.String())
	}

	afterTagMerge := readOrganization(t, handler, organizationPath, `"3"`)
	if len(afterTagMerge.TagIDs) != 1 || afterTagMerge.TagIDs[0] != targetTag.ID {
		t.Fatalf("merged tag assignments=%#v", afterTagMerge.TagIDs)
	}

	deletedTag := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/tags/"+targetTag.ID, nil, `"1"`)
	if deletedTag.Code != http.StatusNoContent {
		t.Fatalf("delete assigned tag status=%d body=%s", deletedTag.Code, deletedTag.Body.String())
	}

	afterTagDelete := readOrganization(t, handler, organizationPath, `"4"`)
	if len(afterTagDelete.TagIDs) != 0 {
		t.Fatalf("deleted tag still assigned=%#v", afterTagDelete.TagIDs)
	}

	mergeFacetBody, err := json.Marshal(map[string]string{"target_id": tuscan.ID})
	if err != nil {
		t.Fatal(err)
	}

	mergedFacet := requestWithIfMatch(t, handler, http.MethodPost, "/api/v1/facets/"+italian.ID+"/merge", mergeFacetBody, `"1"`)
	if mergedFacet.Code != http.StatusNoContent {
		t.Fatalf("merge facet status=%d body=%s", mergedFacet.Code, mergedFacet.Body.String())
	}

	afterFacetMerge := readOrganization(t, handler, organizationPath, `"5"`)
	if len(afterFacetMerge.FacetIDs) != 1 || afterFacetMerge.FacetIDs[0] != tuscan.ID {
		t.Fatalf("merged facet assignments=%#v", afterFacetMerge.FacetIDs)
	}

	filteredFacets := request(t, handler, http.MethodGet, "/api/v1/facets?kind=cuisine", nil)
	if filteredFacets.Code != http.StatusOK || !strings.Contains(filteredFacets.Body.String(), tuscan.ID) || strings.Contains(filteredFacets.Body.String(), italian.ID) {
		t.Fatalf("filtered facet catalog status=%d body=%s", filteredFacets.Code, filteredFacets.Body.String())
	}

	deletedFacet := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/facets/"+tuscan.ID, nil, `"1"`)
	if deletedFacet.Code != http.StatusNoContent {
		t.Fatalf("delete assigned facet status=%d body=%s", deletedFacet.Code, deletedFacet.Body.String())
	}

	afterFacetDelete := readOrganization(t, handler, organizationPath, `"6"`)
	if len(afterFacetDelete.FacetIDs) != 0 {
		t.Fatalf("deleted facet still assigned=%#v", afterFacetDelete.FacetIDs)
	}
}

func TestRecipeListSortsAndSavedViewExecution(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	type createdRecipe struct {
		ID    string
		Title string
	}

	created := make([]createdRecipe, 0, 3)

	for index, title := range []string{"Zulu", "Alpha", "Beta"} {
		document := testfixture.Document()
		document.ID = recipe.RecipeID(fmt.Sprintf("saved-sort-%d", index))
		document.Title = title

		body, err := json.Marshal(fromDomain(document))
		if err != nil {
			t.Fatal(err)
		}

		response := request(t, handler, http.MethodPost, "/api/v1/recipes", body)
		if response.Code != http.StatusCreated {
			t.Fatalf("create %s status=%d body=%s", title, response.Code, response.Body.String())
		}

		var entry LibraryEntry
		if err := json.Unmarshal(response.Body.Bytes(), &entry); err != nil {
			t.Fatal(err)
		}

		created = append(created, createdRecipe{ID: entry.ID, Title: title})
	}

	ratings := map[string]uint8{"Zulu": 5, "Beta": 2}
	for _, entry := range created {
		rating, rated := ratings[entry.Title]

		state := map[string]any{"favorite": false}
		if rated {
			state["rating"] = rating
		}

		body, err := json.Marshal(map[string]any{
			"facet_ids": []any{}, "tag_ids": []any{}, "user_state": state, "sources": []any{},
		})
		if err != nil {
			t.Fatal(err)
		}

		response := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/recipes/"+entry.ID+"/organization", body, `"1"`)
		if response.Code != http.StatusOK {
			t.Fatalf("rate %s status=%d body=%s", entry.Title, response.Code, response.Body.String())
		}
	}

	assertTitles := func(path string, want []string) {
		t.Helper()

		response := request(t, handler, http.MethodGet, path, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("list %s status=%d body=%s", path, response.Code, response.Body.String())
		}

		var page struct {
			Items []RecipeSummary `json:"items"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}

		got := make([]string, len(page.Items))
		for index := range page.Items {
			got[index] = page.Items[index].Title
		}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("list %s titles=%#v", path, got)
		}
	}
	assertTitles("/api/v1/recipes?limit=10&sort=title-asc", []string{"Alpha", "Beta", "Zulu"})
	assertTitles("/api/v1/recipes?limit=10&sort=rating-desc", []string{"Zulu", "Beta", "Alpha"})

	if invalid := request(t, handler, http.MethodGet, "/api/v1/recipes?sort=unsupported", nil); invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid list sort status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	for sort, want := range map[string][]string{
		"title-asc":   {"Alpha", "Beta", "Zulu"},
		"rating-desc": {"Zulu", "Beta", "Alpha"},
	} {
		body, err := json.Marshal(map[string]any{"title": sort, "query": "", "sort": sort})
		if err != nil {
			t.Fatal(err)
		}

		response := request(t, handler, http.MethodPost, "/api/v1/saved-views", body)
		if response.Code != http.StatusCreated {
			t.Fatalf("create %s view status=%d body=%s", sort, response.Code, response.Body.String())
		}

		var view SavedView
		if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}

		assertTitles("/api/v1/saved-views/"+view.ID+"/recipes?limit=10", want)
	}

	if missing := request(t, handler, http.MethodGet, "/api/v1/saved-views/vmissing/recipes", nil); missing.Code != http.StatusNotFound {
		t.Fatalf("missing saved view execution status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestSavedViewListReplaceAndDeleteLifecycle(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	create := func(title, query, sort string) SavedView {
		t.Helper()

		body, err := json.Marshal(map[string]any{"title": title, "query": query, "sort": sort})
		if err != nil {
			t.Fatal(err)
		}

		response := request(t, handler, http.MethodPost, "/api/v1/saved-views", body)
		if response.Code != http.StatusCreated || response.Header().Get("ETag") != `"1"` {
			t.Fatalf("create %q view status=%d ETag=%q body=%s", title, response.Code, response.Header().Get("ETag"), response.Body.String())
		}

		var view SavedView
		if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}

		if view.Title != title || view.Query != query || view.Sort != sort || view.Revision != 1 || view.ID == "" {
			t.Fatalf("created view = %s", response.Body.String())
		}

		return view
	}

	first := create("Alpha view", "favorite:true", "title-asc")
	second := create("Beta view", "course:dessert", "updated-desc")

	list := request(t, handler, http.MethodGet, "/api/v1/saved-views", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list saved views status=%d body=%s", list.Code, list.Body.String())
	}

	var views struct {
		Items []SavedView `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &views); err != nil {
		t.Fatal(err)
	}

	if len(views.Items) != 2 || views.Items[0].ID != first.ID || views.Items[1].ID != second.ID {
		t.Fatalf("saved view list = %s", list.Body.String())
	}

	changed, err := json.Marshal(map[string]any{"title": "Alpha renamed", "query": "rating:5", "sort": "rating-desc"})
	if err != nil {
		t.Fatal(err)
	}

	replaced := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/saved-views/"+first.ID, changed, `"1"`)
	if replaced.Code != http.StatusOK || replaced.Header().Get("ETag") != `"2"` {
		t.Fatalf("replace view status=%d ETag=%q body=%s", replaced.Code, replaced.Header().Get("ETag"), replaced.Body.String())
	}

	var updated SavedView
	if err := json.Unmarshal(replaced.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}

	if updated.Title != "Alpha renamed" || updated.Query != "rating:5" || updated.Sort != "rating-desc" || updated.Revision != 2 {
		t.Fatalf("replaced view = %s", replaced.Body.String())
	}

	if stale := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/saved-views/"+first.ID, changed, `"1"`); stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale replace status=%d body=%s", stale.Code, stale.Body.String())
	}

	reread := request(t, handler, http.MethodGet, "/api/v1/saved-views/"+first.ID, nil)
	if reread.Code != http.StatusOK {
		t.Fatalf("read replaced view status=%d body=%s", reread.Code, reread.Body.String())
	}

	var confirmed SavedView
	if err := json.Unmarshal(reread.Body.Bytes(), &confirmed); err != nil {
		t.Fatal(err)
	}

	if confirmed.Revision != 2 || confirmed.Title != "Alpha renamed" {
		t.Fatalf("stale replace mutated the view: %s", reread.Body.String())
	}

	if stale := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/saved-views/"+first.ID, nil, `"1"`); stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale delete status=%d body=%s", stale.Code, stale.Body.String())
	}

	if deleted := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/saved-views/"+first.ID, nil, `"2"`); deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}

	if gone := request(t, handler, http.MethodGet, "/api/v1/saved-views/"+first.ID, nil); gone.Code != http.StatusNotFound {
		t.Fatalf("deleted view status=%d body=%s", gone.Code, gone.Body.String())
	}

	list = request(t, handler, http.MethodGet, "/api/v1/saved-views", nil)
	if err := json.Unmarshal(list.Body.Bytes(), &views); err != nil {
		t.Fatal(err)
	}

	if len(views.Items) != 1 || views.Items[0].ID != second.ID {
		t.Fatalf("saved view list after delete = %s", list.Body.String())
	}
}

func TestSearchIdentifiesTaxonomyByStableIdentity(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	created := createRecipe(t, handler, testfixture.Document())
	organizationPath := "/api/v1/recipes/" + created.ID + "/organization"
	tag := createTag(t, handler, "Thai", "")

	body := organizationJSON(t, map[string]any{
		"tag_ids":    []string{tag.ID},
		"user_state": map[string]any{"favorite": false},
	})
	if applied := requestWithIfMatch(t, handler, http.MethodPut, organizationPath, body, `"1"`); applied.Code != http.StatusOK {
		t.Fatalf("organize status=%d body=%s", applied.Code, applied.Body.String())
	}

	if found := listSummaries(t, handler, "tag:"+tag.ID); len(found) != 1 {
		t.Fatalf("tag identifier should match, got %d", len(found))
	}

	renamed, err := json.Marshal(map[string]any{"name": "Thai food"})
	if err != nil {
		t.Fatal(err)
	}

	if response := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/tags/"+tag.ID, renamed, `"1"`); response.Code != http.StatusOK {
		t.Fatalf("rename tag status=%d body=%s", response.Code, response.Body.String())
	}

	if found := listSummaries(t, handler, "tag:"+tag.ID); len(found) != 1 {
		t.Fatalf("renaming a tag broke a search that named its identity, got %d", len(found))
	}

	if found := listSummaries(t, handler, "tag:Thai"); len(found) != 0 {
		t.Fatalf("the old display name should no longer match, got %d", len(found))
	}
}

func TestOrdinaryTextWithColonsIsNotAFilter(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	document := testfixture.Document()
	document.Title = "Sourdough at a 1:1 ratio"

	created := createRecipe(t, handler, document)
	for _, query := range []string{"ratio 1:1", `"note:sourdough"`, "sourdough"} {
		found := listSummaries(t, handler, query)
		if len(found) != 1 || found[0].ID != created.ID {
			t.Fatalf("query %q should be an ordinary search, got %d results", query, len(found))
		}
	}
}
