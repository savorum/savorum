package app_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/app"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
)

func TestCalendarFeedsPublishRotateAndRevoke(t *testing.T) {
	t.Parallel()

	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	application, err := app.New(store, units.DefaultCatalog(), app.Options{})
	if err != nil {
		t.Fatal(err)
	}

	call := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()

		var reader bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&reader).Encode(body); err != nil {
				t.Fatal(err)
			}
		}

		request := httptest.NewRequestWithContext(t.Context(), method, path, &reader)
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}

		recorder := httptest.NewRecorder()
		application.Handler.ServeHTTP(recorder, request)

		return recorder
	}

	date := time.Now().UTC().AddDate(0, 0, 2).Format(time.DateOnly)
	if response := call(http.MethodPost, "/api/v1/meals", map[string]any{
		"date": date, "meal_type": "dinner", "dishes": []map[string]any{{"display_name": "Feed soup"}},
	}); response.Code != http.StatusCreated {
		t.Fatalf("create meal: %d %s", response.Code, response.Body)
	}

	created := call(http.MethodPost, "/api/v1/calendar-feeds", map[string]any{"name": "Kitchen"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create feed: %d %s", created.Code, created.Body)
	}

	var secret struct {
		Feed struct {
			ID string `json:"id"`
		} `json:"feed"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &secret); err != nil {
		t.Fatal(err)
	}

	feed := call(http.MethodGet, secret.Path, nil)
	if feed.Code != http.StatusOK || !strings.HasPrefix(feed.Header().Get("Content-Type"), "text/calendar") ||
		!strings.Contains(feed.Body.String(), "SUMMARY:Dinner: Feed soup") || !strings.Contains(feed.Body.String(), "X-WR-CALNAME:Kitchen") {
		t.Fatalf("feed: %d %q %s", feed.Code, feed.Header().Get("Content-Type"), feed.Body)
	}

	if listed := call(http.MethodGet, "/api/v1/calendar-feeds", nil); strings.Contains(listed.Body.String(), "savorum_cal_") {
		t.Fatalf("listing exposed a feed secret: %s", listed.Body)
	}

	rotated := call(http.MethodPost, "/api/v1/calendar-feeds/"+secret.Feed.ID+"/rotate", nil)

	var next struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(rotated.Body.Bytes(), &next); err != nil || rotated.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", rotated.Code, rotated.Body)
	}

	if old := call(http.MethodGet, secret.Path, nil); old.Code != http.StatusNotFound {
		t.Fatalf("rotated URL still reads: %d", old.Code)
	}

	if current := call(http.MethodGet, next.Path, nil); current.Code != http.StatusOK {
		t.Fatalf("new URL: %d", current.Code)
	}

	if deleted := call(http.MethodDelete, "/api/v1/calendar-feeds/"+secret.Feed.ID, nil); deleted.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", deleted.Code, deleted.Body)
	}

	if revoked := call(http.MethodGet, next.Path, nil); revoked.Code != http.StatusNotFound {
		t.Fatalf("revoked URL still reads: %d", revoked.Code)
	}

	for _, path := range []string{"/calendar/nonsense.ics", "/calendar/" + strings.TrimSuffix(strings.TrimPrefix(next.Path, "/calendar/"), ".ics")} {
		if response := call(http.MethodGet, path, nil); response.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", path, response.Code)
		}
	}

	export := call(http.MethodGet, "/api/v1/meals/calendar?from="+date+"&to="+date, nil)
	if export.Code != http.StatusOK || !strings.Contains(export.Header().Get("Content-Disposition"), "attachment") || !strings.Contains(export.Body.String(), "Feed soup") {
		t.Fatalf("export: %d %v %s", export.Code, export.Header(), export.Body)
	}
}
