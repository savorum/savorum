package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/testfixture"
)

func TestMealPlanningAPIEndToEnd(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	create := request(t, handler, http.MethodPost, "/api/v1/meals", []byte(`{
		"date":"2026-09-21",
		"meal_type":"dinner",
		"notes":"Marinate the night before",
		"dishes":[
			{"display_name":"Lemon chicken","scale":"2"},
			{"display_name":"Bagged salad","plain_items":[{"name":"Mixed salad","quantity":"1 bag"}]}
		]
	}`))
	if create.Code != http.StatusCreated || create.Header().Get("ETag") == "" {
		t.Fatalf("create status=%d ETag=%q body=%s", create.Code, create.Header().Get("ETag"), create.Body.String())
	}

	var planned Meal
	if err := json.Unmarshal(create.Body.Bytes(), &planned); err != nil {
		t.Fatal(err)
	}

	if planned.Revision != 1 {
		t.Fatalf("created revision = %d", planned.Revision)
	}

	if planned.DisplayTitle != "Lemon chicken, Bagged salad" || len(planned.Dishes) != 2 || planned.Dishes[0].ID == "" {
		t.Fatalf("created meal = %#v", planned)
	}

	week := request(t, handler, http.MethodGet, "/api/v1/meals?from=2026-09-20&to=2026-09-26&include_unscheduled=true", nil)
	if week.Code != http.StatusOK || !strings.Contains(week.Body.String(), planned.ID) {
		t.Fatalf("week status=%d body=%s", week.Code, week.Body.String())
	}

	updateBody := []byte(`{
		"date":"2026-09-23",
		"meal_type":"dinner",
		"dishes":[{"id":"` + planned.Dishes[0].ID + `","display_name":"Lemon chicken","scale":"3"}]
	}`)

	updated := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/meals/"+planned.ID, updateBody, `"1"`)
	if updated.Code != http.StatusOK || updated.Header().Get("ETag") == create.Header().Get("ETag") || !strings.Contains(updated.Body.String(), `"revision":2`) {
		t.Fatalf("update status=%d ETag=%q body=%s", updated.Code, updated.Header().Get("ETag"), updated.Body.String())
	}

	if stale := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/meals/"+planned.ID, updateBody, `"1"`); stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale update status=%d body=%s", stale.Code, stale.Body.String())
	}

	copyResponse := request(t, handler, http.MethodPost, "/api/v1/meals/"+planned.ID+"/copy", []byte(`{"date":null,"meal_type":""}`))
	if copyResponse.Code != http.StatusCreated || copyResponse.Header().Get("Location") == create.Header().Get("Location") {
		t.Fatalf("copy status=%d location=%q body=%s", copyResponse.Code, copyResponse.Header().Get("Location"), copyResponse.Body.String())
	}

	if stale := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/meals/"+planned.ID, nil, `"1"`); stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale delete status=%d body=%s", stale.Code, stale.Body.String())
	}

	if deleted := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/meals/"+planned.ID, nil, `"2"`); deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

func TestMealETagFollowsRecipeStatusWithoutAdvancingRevision(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	document := fromDomain(testfixture.Document())

	body, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	created := request(t, handler, http.MethodPost, "/api/v1/recipes", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create recipe status=%d body=%s", created.Code, created.Body.String())
	}

	recipeID := strings.TrimPrefix(created.Header().Get("Location"), "/api/v1/recipes/")

	planned := request(t, handler, http.MethodPost, "/api/v1/meals", []byte(`{"dishes":[{"entry_id":"`+recipeID+`","display_name":"Pie"}]}`))
	if planned.Code != http.StatusCreated {
		t.Fatalf("plan status=%d body=%s", planned.Code, planned.Body.String())
	}

	location := planned.Header().Get("Location")
	before := request(t, handler, http.MethodGet, location, nil)

	if trashed := requestWithIfMatch(t, handler, http.MethodDelete, "/api/v1/recipes/"+recipeID, nil, `"1"`); trashed.Code != http.StatusOK {
		t.Fatalf("trash status=%d body=%s", trashed.Code, trashed.Body.String())
	}

	after := request(t, handler, http.MethodGet, location, nil)
	if !strings.Contains(after.Body.String(), `"recipe_status":"trashed"`) || !strings.Contains(after.Body.String(), `"revision":1`) {
		t.Fatalf("meal after trash = %s", after.Body.String())
	}

	if after.Header().Get("ETag") == before.Header().Get("ETag") {
		t.Fatalf("ETag %q did not change when the representation did", after.Header().Get("ETag"))
	}
}

func TestSavedMealCreatesIndependentOccurrence(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	created := request(t, handler, http.MethodPost, "/api/v1/saved-meals", []byte(`{
		"name":"Pancake breakfast",
		"dishes":[{"display_name":"Pancakes","scale":"2"},{"display_name":"Breakfast potatoes"}]
	}`))
	if created.Code != http.StatusCreated {
		t.Fatalf("create saved meal status=%d body=%s", created.Code, created.Body.String())
	}

	var saved SavedMeal
	if err := json.Unmarshal(created.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}

	planned := request(t, handler, http.MethodPost, "/api/v1/saved-meals/"+saved.ID+"/plan", []byte(`{"date":"2026-09-27","meal_type":"breakfast"}`))
	if planned.Code != http.StatusCreated || !strings.Contains(planned.Body.String(), `"Pancake breakfast"`) {
		t.Fatalf("plan saved meal status=%d body=%s", planned.Code, planned.Body.String())
	}
}

func TestMealRejectsImpossibleCalendarDate(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	response := request(t, handler, http.MethodPost, "/api/v1/meals", []byte(`{
		"date":"2026-02-30",
		"dishes":[{"display_name":"Soup"}]
	}`))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid date status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestMealListIncludesUndatedMealsOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	for _, body := range []string{
		`{"name":"Dated","date":"2026-09-21","dishes":[{"display_name":"Soup"}]}`,
		`{"name":"Undated","dishes":[{"display_name":"Bread"}]}`,
	} {
		if created := request(t, handler, http.MethodPost, "/api/v1/meals", []byte(body)); created.Code != http.StatusCreated {
			t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
		}
	}

	for query, want := range map[string]string{
		"":                               "Dated",
		"?include_unscheduled=false":     "Dated",
		"?include_unscheduled=true":      "Dated,Undated",
		"?from=2026-09-20&to=2026-09-26": "Dated",
		"?from=2026-09-20&to=2026-09-26&include_unscheduled=true": "Dated,Undated",
	} {
		response := request(t, handler, http.MethodGet, "/api/v1/meals"+query, nil)

		var page struct {
			Items []Meal `json:"items"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}

		names := []string{}
		for _, item := range page.Items {
			names = append(names, item.Name)
		}

		slices.Sort(names)

		if got := strings.Join(names, ","); response.Code != http.StatusOK || got != want {
			t.Fatalf("GET /api/v1/meals%s = %q, want %q", query, got, want)
		}
	}
}

func TestMealIdentityAndRecipeProblemsAreRequestErrors(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	created := request(t, handler, http.MethodPost, "/api/v1/meals", []byte(`{"dishes":[{"id":"soup","display_name":"Soup"}]}`))
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}

	var planned Meal
	if err := json.Unmarshal(created.Body.Bytes(), &planned); err != nil {
		t.Fatal(err)
	}

	saved := request(t, handler, http.MethodPost, "/api/v1/saved-meals", []byte(`{"name":"Brunch","dishes":[{"display_name":"Eggs"}]}`))
	if saved.Code != http.StatusCreated {
		t.Fatalf("create saved meal status=%d body=%s", saved.Code, saved.Body.String())
	}

	var brunch SavedMeal
	if err := json.Unmarshal(saved.Body.Bytes(), &brunch); err != nil {
		t.Fatal(err)
	}

	other := request(t, handler, http.MethodPost, "/api/v1/meals", []byte(`{"dishes":[{"display_name":"Salad"}]}`))
	if other.Code != http.StatusCreated {
		t.Fatalf("create other status=%d body=%s", other.Code, other.Body.String())
	}

	otherPath := strings.TrimSpace(other.Header().Get("Location"))
	mealPath, savedPath := "/api/v1/meals/"+planned.ID, "/api/v1/saved-meals/"+brunch.ID

	for _, tc := range []struct {
		name, method, path, body, code string
		status                         int
	}{
		{"duplicate dish", http.MethodPut, mealPath, `{"dishes":[{"id":"a","display_name":"A"},{"id":"a","display_name":"B"}]}`, "meal-invalid", 422},
		{"duplicate plain item", http.MethodPut, mealPath, `{"dishes":[{"display_name":"A","plain_items":[{"id":"x","name":"Milk"},{"id":"x","name":"Eggs"}]}]}`, "meal-invalid", 422},
		{"recipe dish with shopping items", http.MethodPost, "/api/v1/meals", `{"dishes":[{"entry_id":"r-does-not-exist","display_name":"Pie","plain_items":[{"name":"Cream"}]}]}`, "meal-invalid", 422},
		{"saved recipe dish with shopping items", http.MethodPost, "/api/v1/saved-meals", `{"name":"Pie","dishes":[{"entry_id":"r-does-not-exist","display_name":"Pie","plain_items":[{"name":"Cream"}]}]}`, "meal-invalid", 422},
		{"dish held by another meal", http.MethodPut, otherPath, `{"dishes":[{"id":"` + planned.Dishes[0].ID + `","display_name":"Soup"}]}`, "meal-invalid", 422},
		{"missing recipe", http.MethodPost, "/api/v1/meals", `{"dishes":[{"entry_id":"r-does-not-exist","display_name":"Ghost"}]}`, "recipe-not-found", 404},
		{"saved duplicate dish", http.MethodPut, savedPath, `{"name":"Brunch","dishes":[{"id":"a","display_name":"A"},{"id":"a","display_name":"B"}]}`, "meal-invalid", 422},
		{"saved missing recipe", http.MethodPost, "/api/v1/saved-meals", `{"name":"Ghost","dishes":[{"entry_id":"r-does-not-exist","display_name":"Ghost"}]}`, "recipe-not-found", 404},
	} {
		var response *httptest.ResponseRecorder
		if tc.method == http.MethodPut {
			response = requestWithIfMatch(t, handler, tc.method, tc.path, []byte(tc.body), `"1"`)
		} else {
			response = request(t, handler, tc.method, tc.path, []byte(tc.body))
		}

		if response.Code != tc.status || !strings.Contains(response.Body.String(), tc.code) {
			t.Fatalf("%s: status=%d body=%s", tc.name, response.Code, response.Body.String())
		}
	}
}

func TestSavedMealRefusesAShoppingItemItCouldNotPlan(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	blank := request(t, handler, http.MethodPost, "/api/v1/saved-meals", []byte(`{"name":"Picnic","dishes":[{"display_name":"Salad","plain_items":[{"name":"   "}]}]}`))
	if blank.Code != http.StatusUnprocessableEntity || !strings.Contains(blank.Body.String(), "meal-invalid") {
		t.Fatalf("blank shopping item: %d %s", blank.Code, blank.Body.String())
	}

	saved := request(t, handler, http.MethodPost, "/api/v1/saved-meals", []byte(`{"name":"Picnic","dishes":[{"display_name":"Salad","plain_items":[{"name":"  Leaves  "}]}]}`))
	if saved.Code != http.StatusCreated {
		t.Fatalf("save: %d %s", saved.Code, saved.Body.String())
	}

	var picnic SavedMeal
	if err := json.Unmarshal(saved.Body.Bytes(), &picnic); err != nil {
		t.Fatal(err)
	}

	if planned := request(t, handler, http.MethodPost, "/api/v1/saved-meals/"+picnic.ID+"/plan", []byte(`{}`)); planned.Code != http.StatusCreated {
		t.Fatalf("plan saved meal: %d %s", planned.Code, planned.Body.String())
	}
}
