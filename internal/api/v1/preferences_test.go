package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/savorum/savorum/internal/configuration"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/preferences"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

func TestCurrentUserPreferencesAPIIsolationAndConcurrency(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "preferences.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	local, err := store.LocalPrincipal(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	other, err := store.CreateLocalUser(t.Context(), local.UserID, "preference-other", "Other reader", "a-long-test-passphrase")
	if err != nil {
		t.Fatal(err)
	}

	config, err := configuration.NewService(store, configuration.InstanceOverrides{})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	registerPreferences(humago.New(mux, huma.DefaultConfig("test", "1")), preferences.NewService(store, config))

	perform := func(p identity.Principal, method, body string) *httptest.ResponseRecorder {
		t.Helper()

		ctx := t.Context()
		if p.UserID != "" {
			ctx = identity.WithPrincipal(ctx, p)
		}

		request := httptest.NewRequestWithContext(ctx, method, "/api/v1/me/preferences", bytes.NewBufferString(body))
		request.Header.Set("Accept-Language", "fr-CA, en;q=0.5")

		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}

		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)

		return response
	}
	if response := perform(identity.Principal{}, http.MethodGet, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d %s", response.Code, response.Body.String())
	}

	response := perform(local, http.MethodPut, `{"revision":0,"overrides":{"measurement":"metric","ui_locale":"fr-CA","content_languages":["de","en"],"timezone":"America/Denver"}}`)
	if response.Code != http.StatusOK {
		t.Fatalf("save: %d %s", response.Code, response.Body.String())
	}

	var saved UserPreferences
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}

	if saved.Effective.UILocale != "fr" || saved.Overrides.UILocale != "fr-CA" {
		t.Fatalf("catalog fallback rewrote explicit choice: %#v", saved)
	}

	if saved.Revision != 1 || saved.Version != 1 || saved.Effective.Measurement != "metric" || saved.Effective.ContentLanguages[0] != "de" {
		t.Fatalf("resolved wire: %#v", saved)
	}

	if response := perform(local, http.MethodPut, `{"revision":0,"overrides":{}}`); response.Code != http.StatusConflict {
		t.Fatalf("stale write: %d %s", response.Code, response.Body.String())
	}

	if response := perform(local, http.MethodPut, `{"revision":1,"overrides":{"timezone":"Mars/Here"}}`); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid timezone: %d %s", response.Code, response.Body.String())
	}

	if response := perform(local, http.MethodPut, `{"revision":1,"overrides":{"measurement":"us"},"user_id":"someone-else"}`); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("accepted another user ID: %d %s", response.Code, response.Body.String())
	}

	response = perform(other, http.MethodGet, "")

	saved = UserPreferences{}
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}

	if saved.Revision != 0 || saved.Overrides.UILocale != "" || saved.Overrides.Measurement != "" || saved.Effective.Measurement != "original" {
		t.Fatalf("preferences leaked between users: %#v", saved)
	}

	response = perform(local, http.MethodPut, `{"revision":1,"overrides":{}}`)
	if response.Code != http.StatusOK {
		t.Fatalf("clear override: %d %s", response.Code, response.Body.String())
	}
}
