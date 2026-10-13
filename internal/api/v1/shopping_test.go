package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/shopping"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestShoppingRejectsQueuedWorkFromAnotherAccount(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/shopping/lists/groceries/changes", strings.NewReader(`{"id":"add","kind":"add","item_id":"milk","base_revision":0,"name":"Milk"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Savorum-Principal", "another-account")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusForbidden {
		t.Fatalf("wrong-account replay: %d %s", response.Code, response.Body.String())
	}
}

func TestShoppingPlanReviewAndRetry(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	created := request(t, handler, http.MethodPost, "/api/v1/meals", []byte(`{"name":"Picnic","dishes":[{"display_name":"Salad","plain_items":[{"name":"Mixed leaves","quantity":"1 bag"}]}]}`))
	if created.Code != 201 {
		t.Fatal(created.Body.String())
	}

	var planned Meal
	if err := json.Unmarshal(created.Body.Bytes(), &planned); err != nil {
		t.Fatal(err)
	}

	input := shopping.ShoppingReviewInput{MealIDs: []string{planned.ID}}

	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}

	preview := request(t, handler, http.MethodPost, "/api/v1/shopping/lists/groceries/review", body)
	if preview.Code != 200 {
		t.Fatal(preview.Body.String())
	}

	var review shopping.ShoppingReview
	if err := json.Unmarshal(preview.Body.Bytes(), &review); err != nil {
		t.Fatal(err)
	}

	if len(review.Requirements) != 1 || review.Requirements[0].Choices[0].Quantity != "1 bag" {
		t.Fatalf("review: %#v", review)
	}

	twice := request(t, handler, http.MethodPost, "/api/v1/shopping/lists/groceries/review", []byte(`{"meal_ids":["`+planned.ID+`","`+planned.ID+`"]}`))
	if twice.Code != http.StatusUnprocessableEntity || !strings.Contains(twice.Body.String(), "shopping-invalid") {
		t.Fatalf("meal named twice: %d %s", twice.Code, twice.Body.String())
	}

	selection := review.Requirements[0]
	commit := shopping.ShoppingApplyInput{ID: "review-one", Input: input, Token: review.Token, Selections: []shopping.ShoppingSelection{{ID: selection.ID, Selected: true, ChoiceID: selection.ChoiceID}}}

	body, err = json.Marshal(commit)
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		applied := request(t, handler, http.MethodPost, "/api/v1/shopping/lists/groceries/apply", body)
		if applied.Code != 200 {
			t.Fatal(applied.Body.String())
		}

		var list shopping.ShoppingList
		if err := json.Unmarshal(applied.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}

		if list.Revision != 1 || len(list.Items) != 1 || list.Items[0].Sources[0].MealID != planned.ID {
			t.Fatalf("applied: %#v", list)
		}
	}
}

func TestShoppingNamedListsKeepChangesSeparate(t *testing.T) {
	t.Parallel()

	handler, _, _ := testAPI(t)
	for range 2 {
		created := request(t, handler, http.MethodPost, "/api/v1/shopping/lists", []byte(`{"id":"picnic","name":"Picnic"}`))
		if created.Code != http.StatusCreated || created.Header().Get("Location") != "/api/v1/shopping/lists/picnic" {
			t.Fatalf("create: %d %s", created.Code, created.Body.String())
		}
	}

	changed := request(t, handler, http.MethodPost, "/api/v1/shopping/lists/picnic/changes", []byte(`{"id":"bread-add","kind":"add","item_id":"bread","base_revision":0,"name":"Bread"}`))
	if changed.Code != 200 {
		t.Fatalf("edit: %d %s", changed.Code, changed.Body.String())
	}

	for _, target := range []struct {
		id    string
		count int
	}{{"groceries", 0}, {"picnic", 1}} {
		response := request(t, handler, http.MethodGet, "/api/v1/shopping/lists/"+target.id, nil)

		var list shopping.ShoppingList
		if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}

		if response.Code != 200 || len(list.Items) != target.count {
			t.Fatalf("%s: %d %s", target.id, response.Code, response.Body.String())
		}
	}

	missing := request(t, handler, http.MethodGet, "/api/v1/shopping/lists/missing", nil)
	if missing.Code != 404 {
		t.Fatalf("unknown list: %d", missing.Code)
	}
}

func TestShoppingReviewPinsPlannedRecipeRevision(t *testing.T) {
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

	plannedResponse := request(t, handler, http.MethodPost, "/api/v1/meals", []byte(`{"dishes":[{"entry_id":"`+recipeID+`","display_name":"Pie"}]}`))
	if plannedResponse.Code != http.StatusCreated {
		t.Fatalf("plan: %d %s", plannedResponse.Code, plannedResponse.Body.String())
	}

	var planned Meal
	if err := json.Unmarshal(plannedResponse.Body.Bytes(), &planned); err != nil {
		t.Fatal(err)
	}

	apply := func(operationID string, input shopping.ShoppingReviewInput) {
		t.Helper()

		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}

		preview := request(t, handler, http.MethodPost, "/api/v1/shopping/lists/groceries/review", body)
		if preview.Code != http.StatusOK {
			t.Fatalf("review: %d %s", preview.Code, preview.Body.String())
		}

		var review shopping.ShoppingReview
		if err := json.Unmarshal(preview.Body.Bytes(), &review); err != nil {
			t.Fatal(err)
		}

		commit := shopping.ShoppingApplyInput{ID: operationID, Input: input, Token: review.Token}
		for _, requirement := range review.Requirements {
			commit.Selections = append(commit.Selections, shopping.ShoppingSelection{ID: requirement.ID, Selected: requirement.Selected, ChoiceID: requirement.Choices[0].ID})
		}

		body, err = json.Marshal(commit)
		if err != nil {
			t.Fatal(err)
		}

		for range 2 {
			if applied := request(t, handler, http.MethodPost, "/api/v1/shopping/lists/groceries/apply", body); applied.Code != http.StatusOK {
				t.Fatalf("apply: %d %s", applied.Code, applied.Body.String())
			}
		}
	}

	stored := func() Meal {
		t.Helper()

		var value Meal
		if err := json.Unmarshal(request(t, handler, http.MethodGet, "/api/v1/meals/"+planned.ID, nil).Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}

		return value
	}

	apply("first", shopping.ShoppingReviewInput{MealIDs: []string{planned.ID}})

	if pinned := stored(); pinned.Revision != 2 || pinned.Dishes[0].RecipeRevision == nil || *pinned.Dishes[0].RecipeRevision != 1 {
		t.Fatalf("after first review: %#v", pinned)
	}

	document.Title = "Revised pie"

	recipeBody, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	if edited := requestWithIfMatch(t, handler, http.MethodPut, "/api/v1/recipes/"+recipeID, recipeBody, `"1"`); edited.Code != http.StatusOK {
		t.Fatalf("edit recipe: %d %s", edited.Code, edited.Body.String())
	}

	if unchanged := stored(); unchanged.Revision != 2 || *unchanged.Dishes[0].RecipeRevision != 1 {
		t.Fatalf("after recipe edit: %#v", unchanged)
	}

	apply("refresh", shopping.ShoppingReviewInput{MealIDs: []string{planned.ID}, RefreshRecipes: true})

	if refreshed := stored(); refreshed.Revision != 3 || *refreshed.Dishes[0].RecipeRevision != 2 {
		t.Fatalf("after refresh: %#v", refreshed)
	}
}

func TestShoppingReviewNamesAMissingRecipeOrRevision(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	recipeBody, err := json.Marshal(fromDomain(testfixture.Document()))
	if err != nil {
		t.Fatal(err)
	}

	created := request(t, handler, http.MethodPost, "/api/v1/recipes", recipeBody)
	if created.Code != http.StatusCreated {
		t.Fatalf("create recipe: %d %s", created.Code, created.Body.String())
	}

	recipeID := strings.TrimPrefix(created.Header().Get("Location"), "/api/v1/recipes/")

	for _, portion := range []string{
		`{"id":"p1","recipe_id":"` + recipeID + `","revision":999}`,
		`{"id":"p2","recipe_id":"r-nope"}`,
	} {
		review := request(t, handler, http.MethodPost, "/api/v1/shopping/lists/groceries/review", []byte(`{"portions":[`+portion+`]}`))
		if review.Code != http.StatusNotFound || !strings.Contains(review.Body.String(), `"code":"recipe-not-found"`) {
			t.Fatalf("review %s: %d %s", portion, review.Code, review.Body.String())
		}
	}

	if pinned := request(t, handler, http.MethodPost, "/api/v1/meals", []byte(`{"dishes":[{"entry_id":"`+recipeID+`","recipe_revision":999,"display_name":"Pie"}]}`)); pinned.Code != http.StatusUnprocessableEntity || !strings.Contains(pinned.Body.String(), "meal-invalid") {
		t.Fatalf("meal pinned to a missing revision: %d %s", pinned.Code, pinned.Body.String())
	}
}
