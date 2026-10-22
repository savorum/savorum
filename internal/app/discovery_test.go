package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/app"
	"github.com/savorum/savorum/internal/configuration"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
)

func TestAStrangerCanFindTheGuide(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	settings, err := configuration.NewService(store, configuration.InstanceOverrides{})
	if err != nil {
		t.Fatal(err)
	}

	application, err := app.New(store, units.DefaultCatalog(), app.Options{Configuration: settings, AuthEnabled: true})
	if err != nil {
		t.Fatal(err)
	}

	get := func(target string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		request.Host = "recipes.example.com"
		response := httptest.NewRecorder()
		application.Handler.ServeHTTP(response, request)

		return response
	}

	denied := get("/api/v1/recipes")
	if denied.Code != http.StatusUnauthorized || !strings.Contains(denied.Header().Get("Link"), `<http://recipes.example.com/llms.txt>; rel="service-doc"`) ||
		!strings.HasPrefix(denied.Header().Get("WWW-Authenticate"), "Bearer") || !strings.Contains(denied.Body.String(), "API tokens") {
		t.Fatalf("401 = %d %v %s", denied.Code, denied.Header(), denied.Body.String())
	}

	if guide := get("/llms.txt"); guide.Code != http.StatusOK || !strings.Contains(guide.Body.String(), "http://recipes.example.com/account/tokens") {
		t.Fatalf("guide = %d %s", guide.Code, guide.Body.String())
	}

	var capabilities struct {
		Links struct {
			Guide  string `json:"guide"`
			Tokens string `json:"tokens"`
		} `json:"links"`
	}
	if err := json.Unmarshal(get("/api/v1/capabilities").Body.Bytes(), &capabilities); err != nil {
		t.Fatal(err)
	}

	if capabilities.Links.Guide != "http://recipes.example.com/llms.txt" || capabilities.Links.Tokens != "http://recipes.example.com/account/tokens" {
		t.Fatalf("capabilities links = %#v", capabilities.Links)
	}

	var spec struct {
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(get("/openapi.json").Body.Bytes(), &spec); err != nil || len(spec.Servers) != 1 || spec.Servers[0].URL != "http://recipes.example.com" {
		t.Fatalf("spec servers = %#v %v", spec.Servers, err)
	}
}
