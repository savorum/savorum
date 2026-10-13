package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/idempotency"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/mealcalendar"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
	"github.com/savorum/savorum/internal/webhook"
)

func keyedRequest(t *testing.T, h http.Handler, path, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), "POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)

	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)

	return out
}

func TestContentCreationReceipts(t *testing.T) {
	t.Parallel()
	h, _, _ := testAPI(t)

	document, err := json.Marshal(fromDomain(testfixture.Document()))
	if err != nil {
		t.Fatal(err)
	}

	recipe := keyedRequest(t, h, "/api/v1/recipes", "recipe", string(document))
	if recipe.Code != 201 {
		t.Fatal(recipe.Body.String())
	}

	recipeID := strings.TrimPrefix(recipe.Header().Get("Location"), "/api/v1/recipes/")
	translated := fromDomain(testfixture.Document())
	translated.ID = "translated"
	translated.Language = "fr"

	translation, err := json.Marshal(translated)
	if err != nil {
		t.Fatal(err)
	}

	meal := keyedRequest(t, h, "/api/v1/meals", "source-meal", `{"dishes":[{"display_name":"Soup"}]}`)
	saved := keyedRequest(t, h, "/api/v1/saved-meals", "source-saved", `{"name":"Lunch","dishes":[{"display_name":"Soup"}]}`)
	mealID := strings.TrimPrefix(meal.Header().Get("Location"), "/api/v1/meals/")
	savedID := strings.TrimPrefix(saved.Header().Get("Location"), "/api/v1/saved-meals/")

	for _, tc := range []struct{ name, path, body string }{
		{"recipe", "/api/v1/recipes", string(document)},
		{"translation", "/api/v1/recipes/" + recipeID + "/translations/new", string(translation)},
		{"meal", "/api/v1/meals", `{"dishes":[{"display_name":"Soup"}]}`},
		{"meal-copy", "/api/v1/meals/" + mealID + "/copy", `{}`},
		{"saved-plan", "/api/v1/saved-meals/" + savedID + "/plan", `{}`},
		{"shopping-list", "/api/v1/shopping/lists", `{"id":"weekly","name":"Weekly"}`},
		{"saved", "/api/v1/saved-meals", `{"name":"Lunch","dishes":[{"display_name":"Soup"}]}`},
		{"book", "/api/v1/books", `{"kind":"personal","title":"Weeknight","sections":[],"entries":[]}`},
		{"tag", "/api/v1/tags", `{"name":"Lunch"}`},
		{"facet", "/api/v1/facets", `{"kind":"course","value":"Soup"}`},
		{"view", "/api/v1/saved-views", `{"title":"Lunch","query":"Soup","sort":"title-asc"}`},
		{"cooking", "/api/v1/recipes/" + recipeID + "/cookings", `{"cooked_on":"2026-10-11"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := keyedRequest(t, h, tc.path, tc.name, tc.body)
			if first.Code != 201 {
				t.Fatalf("first %d %s", first.Code, first.Body.String())
			}

			second := keyedRequest(t, h, tc.path, tc.name, tc.body)
			if second.Code != 201 || second.Header().Get("Location") != first.Header().Get("Location") {
				t.Fatalf("replay %d %s", second.Code, second.Body.String())
			}

			var a, b map[string]any

			_ = json.Unmarshal(first.Body.Bytes(), &a)

			_ = json.Unmarshal(second.Body.Bytes(), &b)
			if a["id"] != b["id"] {
				t.Fatalf("recreated %v %v", a, b)
			}
		})
	}
}

func TestMealConcurrentRetryAndDeletedResult(t *testing.T) {
	t.Parallel()
	h, _, _ := testAPI(t)
	body := `{"dishes":[{"display_name":"Soup"}]}`

	var wg sync.WaitGroup

	out := make([]*httptest.ResponseRecorder, 8)
	for i := range out {
		wg.Go(func() { out[i] = keyedRequest(t, h, "/api/v1/meals", "one", body) })
	}

	wg.Wait()

	for _, v := range out {
		if v.Code != 201 || v.Header().Get("Location") != out[0].Header().Get("Location") {
			t.Fatalf("concurrent replay %d %s", v.Code, v.Body.String())
		}
	}

	conflict := keyedRequest(t, h, "/api/v1/meals", "one", `{"dishes":[{"display_name":"Different"}]}`)
	if conflict.Code != 409 || !strings.Contains(conflict.Body.String(), "idempotency-conflict") {
		t.Fatal(conflict.Body.String())
	}

	deleted := requestWithIfMatch(t, h, "DELETE", out[0].Header().Get("Location"), nil, `"1"`)
	if deleted.Code != 204 {
		t.Fatal(deleted.Body.String())
	}

	replay := keyedRequest(t, h, "/api/v1/meals", "one", body)
	if replay.Code != 404 {
		t.Fatalf("deleted replay %d %s", replay.Code, replay.Body.String())
	}
}

func TestAtomicAdditionsAndBoundedSelections(t *testing.T) {
	t.Parallel()
	h, _, _ := testAPI(t)
	path := "/api/v1/shopping/lists/groceries/items"
	body := `{"items":[{"id":"milk","name":"Milk"},{"id":"eggs","name":"Eggs"}]}`

	first := keyedRequest(t, h, path, "batch", body)
	if first.Code != 200 {
		t.Fatalf("add %d %s", first.Code, first.Body.String())
	}

	_ = keyedRequest(t, h, path, "batch", body)

	failed := keyedRequest(t, h, path, "bad", `{"items":[{"id":"bread","name":"Bread"},{"id":"milk","name":"Milk"}]}`)
	if failed.Code != 409 {
		t.Fatal(failed.Body.String())
	}

	list := request(t, h, "GET", "/api/v1/shopping/lists/groceries", nil)
	if strings.Contains(list.Body.String(), "bread") || !strings.Contains(list.Body.String(), `"revision":1`) {
		t.Fatal(list.Body.String())
	}

	for range 12 {
		created := request(t, h, "POST", "/api/v1/books", []byte(`{"kind":"personal","title":"Same","sections":[],"entries":[]}`))
		if created.Code != 201 {
			t.Fatal(created.Body.String())
		}
	}

	page := request(t, h, "GET", "/api/v1/books/suggestions", nil)

	var values struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		HasMore bool `json:"has_more"`
	}

	_ = json.Unmarshal(page.Body.Bytes(), &values)
	if page.Code != 200 || len(values.Items) != 10 || !values.HasMore || strings.Contains(page.Body.String(), `"entries"`) {
		t.Fatal(page.Body.String())
	}

	lookup := request(t, h, "POST", "/api/v1/books/lookup", []byte(`{"ids":["missing","`+values.Items[1].ID+`","`+values.Items[0].ID+`","`+values.Items[1].ID+`"]}`))

	var found struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Unresolved []string `json:"unresolved_ids"`
	}

	_ = json.Unmarshal(lookup.Body.Bytes(), &found)
	if lookup.Code != 200 || len(found.Items) != 2 || found.Items[0].ID != values.Items[1].ID || len(found.Unresolved) != 1 {
		t.Fatal(lookup.Body.String())
	}
}

func TestWebhookCreationRetryOmitsSecret(t *testing.T) {
	h, _, _, _ := integrationAPI(t)

	for _, tc := range []struct{ path, event string }{
		{"/api/v1/webhooks", "recipe.created"},
		{"/api/v1/instance-webhooks", "user.disabled"},
	} {
		body := `{"name":"Receiver","url":"https://8.8.8.8/receive","event_types":["` + tc.event + `"],"enabled":false}`
		first := keyedRequest(t, h, tc.path, "receiver", body)
		replay := keyedRequest(t, h, tc.path, "receiver", body)

		var a, b webhook.Grant

		_ = json.Unmarshal(first.Body.Bytes(), &a)

		_ = json.Unmarshal(replay.Body.Bytes(), &b)
		if first.Code != 201 || replay.Code != 201 || a.Subscription.ID == "" || a.Subscription.ID != b.Subscription.ID || a.Secret == "" || b.Secret != "" {
			t.Fatalf("webhook replay: %s %s", first.Body.String(), replay.Body.String())
		}
	}
}

func TestCalendarCreationRetryOmitsCredential(t *testing.T) {
	_, store, _ := testAPI(t)

	principal, err := store.LocalPrincipal(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	recipes, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	calendar := mealcalendar.NewService(store, nil, func(context.Context) (string, error) { return "test", nil }, func(context.Context) string { return "" })

	raw, _, err := New(recipes, store, nil, Options{Idempotency: idempotency.New(store), MealCalendar: calendar})
	if err != nil {
		t.Fatal(err)
	}

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw.ServeHTTP(w, r.WithContext(access.WithLibrary(identity.WithPrincipal(r.Context(), principal), "default")))
	})
	first := keyedRequest(t, h, "/api/v1/calendar-feeds", "feed", `{"name":"Plan"}`)
	replay := keyedRequest(t, h, "/api/v1/calendar-feeds", "feed", `{"name":"Plan"}`)

	var a, b mealcalendar.CalendarFeedSecret

	_ = json.Unmarshal(first.Body.Bytes(), &a)

	_ = json.Unmarshal(replay.Body.Bytes(), &b)
	if first.Code != 201 || replay.Code != 201 || a.CalendarFeed.ID == "" || a.CalendarFeed.ID != b.CalendarFeed.ID || a.Path == "" || b.Path != "" {
		t.Fatalf("calendar replay: %s %s", first.Body.String(), replay.Body.String())
	}

	deleted := request(t, h, "DELETE", "/api/v1/calendar-feeds/"+a.CalendarFeed.ID, nil)
	if deleted.Code != 204 {
		t.Fatal(deleted.Body.String())
	}

	if result := keyedRequest(t, h, "/api/v1/calendar-feeds", "feed", `{"name":"Plan"}`); result.Code != 404 {
		t.Fatalf("deleted replay: %d %s", result.Code, result.Body.String())
	}
}

func TestCreationReplayRechecksAuthority(t *testing.T) {
	raw, store, _ := testAPI(t)

	owner, err := store.LocalPrincipal(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	ownerContext := access.WithLibrary(identity.WithPrincipal(t.Context(), owner), "default")

	editor, err := store.CreateLocalUser(t.Context(), owner.UserID, "retry-editor", "Editor", "retry editor password phrase")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.SetLibraryMember(ownerContext, "default", editor.UserID, access.Editor, false); err != nil {
		t.Fatal(err)
	}

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw.ServeHTTP(w, r.WithContext(access.WithLibrary(identity.WithPrincipal(r.Context(), editor), "default")))
	})
	body := `{"dishes":[{"display_name":"Soup"}]}`

	first := keyedRequest(t, h, "/api/v1/meals", "editor", body)
	if first.Code != 201 {
		t.Fatal(first.Body.String())
	}

	if err := store.SetLibraryMember(ownerContext, "default", editor.UserID, access.Viewer, false); err != nil {
		t.Fatal(err)
	}

	if replay := keyedRequest(t, h, "/api/v1/meals", "editor", body); replay.Code != 403 {
		t.Fatalf("revoked authority replay: %d %s", replay.Code, replay.Body.String())
	}
}
