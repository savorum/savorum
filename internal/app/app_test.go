package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/app"
	"github.com/savorum/savorum/internal/contracttest"
	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
)

// These assignments intentionally fail to compile if the application store
// stops requiring organization persistence or SQLite stops implementing the
// complete application persistence contract.
var (
	_ library.OrganizationRepository = app.Store(nil)
	_ app.Store                      = (*sqlite.Store)(nil)
)

func TestApplicationSeparatesAPIAndWebFallbacks(t *testing.T) {
	t.Parallel()

	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	application, err := app.New(store, units.DefaultCatalog(), app.Options{})
	if err != nil {
		t.Fatal(err)
	}

	apiRequest := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/not-a-route", nil)
	apiResponse := httptest.NewRecorder()
	contracttest.Handler(t, application.Handler).ServeHTTP(apiResponse, apiRequest)

	if apiResponse.Code != http.StatusNotFound {
		t.Fatalf("API miss status = %d", apiResponse.Code)
	}

	if strings.Contains(apiResponse.Body.String(), "Recipes · Savorum</title>") {
		t.Fatal("API miss fell through to the web shell")
	}

	webRequest := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/recipes/example", nil)
	webResponse := httptest.NewRecorder()
	contracttest.Handler(t, application.Handler).ServeHTTP(webResponse, webRequest)

	if webResponse.Code != http.StatusOK || !strings.Contains(webResponse.Body.String(), ">Recipes · Savorum</title>") {
		t.Fatalf("web route did not serve shell: status=%d body=%s", webResponse.Code, webResponse.Body.String())
	}
}

func TestApplicationSecurityPolicyCoversAPIAndWebResponses(t *testing.T) {
	t.Parallel()

	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	application, err := app.New(store, units.DefaultCatalog(), app.Options{})
	if err != nil {
		t.Fatal(err)
	}

	handler := httpsecurity.NewHandler(contracttest.Handler(t, application.Handler), httpsecurity.Options{})

	for _, target := range []string{"/", "/api/v1/health", "/api/v1/not-a-route"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))

		for name, expected := range map[string]string{
			"Content-Security-Policy": "default-src 'self'",
			"Referrer-Policy":         "no-referrer",
			"X-Content-Type-Options":  "nosniff",
			"X-Frame-Options":         "DENY",
		} {
			actual := response.Header().Get(name)
			if !strings.Contains(actual, expected) {
				t.Errorf("GET %s: %s = %q, want to contain %q", target, name, actual, expected)
			}
		}
	}
}

func TestTheConfiguredPrintRecipeLimitIsReportedAndEnforced(t *testing.T) {
	t.Parallel()

	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	application, err := app.New(store, units.DefaultCatalog(), app.Options{PrintRecipeLimit: 3})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = application.Renderer.Close() })

	capabilities := httptest.NewRecorder()
	contracttest.Handler(t, application.Handler).ServeHTTP(capabilities, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/capabilities", nil))

	if !strings.Contains(capabilities.Body.String(), `"print_recipes":3`) {
		t.Fatalf("capabilities do not report the print limit: %s", capabilities.Body.String())
	}

	for _, test := range []struct {
		recipes string
		status  int
	}{
		{`["a","b","c","d"]`, http.StatusRequestEntityTooLarge},
		{`["a","b","c"]`, http.StatusNotFound},
	} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/print/documents", strings.NewReader(`{"recipes":`+test.recipes+`}`))
		request.Header.Set("Content-Type", "application/json")

		response := httptest.NewRecorder()
		contracttest.Handler(t, application.Handler).ServeHTTP(response, request)

		if response.Code != test.status {
			t.Errorf("printing %s: status %d, want %d: %s", test.recipes, response.Code, test.status, response.Body.String())
		}
	}
}
