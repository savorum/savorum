package v1

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAPIContractIsOpenAPI31AndVersioned(t *testing.T) {
	t.Parallel()
	handler, _, definition := testAPI(t)

	response := request(t, handler, http.MethodGet, "/openapi.json", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("OpenAPI status = %d; body = %s", response.Code, response.Body.String())
	}

	var document struct {
		OpenAPI string                     `json:"openapi"`
		Paths   map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}

	if document.OpenAPI != "3.1.0" {
		t.Fatalf("OpenAPI version = %q", document.OpenAPI)
	}

	for _, route := range []string{
		"/api/v1/configuration",
		"/api/v1/health",
		"/api/v1/recipes",
		"/api/v1/recipes/{id}",
		"/api/v1/recipes/{id}/restore",
		"/api/v1/recipes/{id}/revisions",
		"/api/v1/recipes/{id}/revisions/{revision}",
		"/api/v1/recipes/{id}/revisions/{revision}/restore",
		"/api/v1/trash/recipes/{id}",
		"/api/v1/recipes/{id}/organization",
		"/api/v1/trash/empty",
		"/api/v1/books",
		"/api/v1/books/{id}",
		"/api/v1/trash/books/{id}",
		"/api/v1/books/{id}/restore",
		"/api/v1/books/{id}/revisions",
		"/api/v1/books/{id}/revisions/{revision}",
		"/api/v1/books/{id}/revisions/{revision}/restore",
		"/api/v1/saved-views",
		"/api/v1/saved-views/{id}",
		"/api/v1/saved-views/{id}/recipes",
	} {
		if _, exists := document.Paths[route]; !exists {
			t.Errorf("OpenAPI is missing %s", route)
		}
	}

	for _, obsolete := range []string{
		"/api/v1/book-trash/{id}",
		"/api/v1/recipes/{id}/purge",
		"/api/v1/books/{id}/purge",
	} {
		if _, exists := document.Paths[obsolete]; exists {
			t.Errorf("OpenAPI still exposes obsolete lifecycle route %s", obsolete)
		}
	}

	contract := strings.ToLower(response.Body.String())
	for _, obsolete := range []string{"purge", "delete for good", "deleted for good"} {
		if strings.Contains(contract, obsolete) {
			t.Errorf("OpenAPI still exposes obsolete lifecycle term %q", obsolete)
		}
	}

	if definition.OpenAPI().Info.Version != Version {
		t.Fatalf("API info version = %q", definition.OpenAPI().Info.Version)
	}
}

func TestConfigurationExposesOnlySafeInstanceValues(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	response := request(t, handler, http.MethodGet, "/api/v1/configuration", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("configuration status = %d; body = %s", response.Code, response.Body.String())
	}

	var body InstanceConfiguration
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	if body.DisplayName != "Savorum" || body.DefaultLocale != "en-US" || body.DefaultMeasurement != "original" {
		t.Fatalf("configuration = %#v", body)
	}

	encoded := strings.ToLower(response.Body.String())
	for _, forbidden := range []string{"database", "asset_path", "listen", "trusted_proxy", "secret", "password"} {
		if strings.Contains(encoded, forbidden) {
			t.Errorf("configuration response exposed %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestHealthReflectsDatabaseAvailability(t *testing.T) {
	t.Parallel()
	handler, store, _ := testAPI(t)

	response := request(t, handler, http.MethodGet, "/api/v1/health", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("healthy status = %d", response.Code)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	response = request(t, handler, http.MethodGet, "/api/v1/health", nil)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unhealthy status = %d; body = %s", response.Code, response.Body.String())
	}
}
