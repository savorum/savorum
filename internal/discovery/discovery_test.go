package discovery_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/savorum/savorum/internal/discovery"
)

func serve(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	request.Host = "recipes.example.com"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	return response
}

func handler(publicURL string) http.Handler {
	spec := &huma.OpenAPI{OpenAPI: "3.1.0", Info: &huma.Info{Title: "Savorum API", Version: "1"}, Servers: []*huma.Server{{URL: "http://127.0.0.1:8080"}}}

	return discovery.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(discovery.Origin(r.Context())))
	}), discovery.Options{Spec: func() *huma.OpenAPI { return spec }, PublicURL: func(context.Context) string { return publicURL }})
}

func TestEveryResponseLinksToTheGuide(t *testing.T) {
	t.Parallel()

	response := serve(t, handler(""), "/api/v1/recipes")
	if response.Code != http.StatusUnauthorized || response.Body.String() != "http://recipes.example.com" {
		t.Fatalf("passthrough = %d %q", response.Code, response.Body.String())
	}

	link := response.Header().Get("Link")
	for _, want := range []string{
		`<http://recipes.example.com/openapi.json>; rel="service-desc"`,
		`<http://recipes.example.com/llms.txt>; rel="service-doc"`,
		`<http://recipes.example.com/.well-known/api-catalog>; rel="api-catalog"`,
	} {
		if !strings.Contains(link, want) {
			t.Fatalf("Link %q lacks %q", link, want)
		}
	}
}

func TestGuideIsWrittenForTheConfiguredOrigin(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/llms.txt", "/AGENTS.md"} {
		response := serve(t, handler("https://cook.example.org/"), path)
		body := response.Body.String()

		if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/markdown") ||
			!strings.HasPrefix(body, "# Savorum\n") || !strings.Contains(body, "https://cook.example.org/account/tokens") || strings.Contains(body, "{{") {
			t.Fatalf("%s = %d %s", path, response.Code, body)
		}
	}
}

func TestAssistantGuideOffersConnectionSetupSeparately(t *testing.T) {
	t.Parallel()

	full := discovery.Guide("https://cook.example.org")
	assistant := discovery.AssistantGuide("https://cook.example.org")
	connection := discovery.ConnectionGuide("https://cook.example.org")

	if !strings.HasPrefix(connection, "## Connecting without a key\n") || !strings.Contains(connection, "/oauth/device") || strings.Contains(connection, "\n## MCP tools") {
		t.Fatalf("connection section = %q", connection)
	}

	if strings.Contains(assistant, "/oauth/device") || !strings.Contains(assistant, "\n## MCP tools\n") || len(assistant)+len(connection) != len(full) {
		t.Fatalf("assistant guide kept connection setup or lost content")
	}
}

func TestSpecNamesTheRequestOrigin(t *testing.T) {
	t.Parallel()

	response := serve(t, handler(""), "/openapi.json")

	var document struct {
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
		ExternalDocs struct {
			URL string `json:"url"`
		} `json:"externalDocs"` //nolint:tagliatelle // OpenAPI field name
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}

	if len(document.Servers) != 1 || document.Servers[0].URL != "http://recipes.example.com" || document.ExternalDocs.URL != "http://recipes.example.com/llms.txt" {
		t.Fatalf("spec = %s", response.Body.String())
	}
}

func TestAPICatalog(t *testing.T) {
	t.Parallel()

	response := serve(t, handler(""), "/.well-known/api-catalog")

	var catalog struct {
		Linkset []map[string]json.RawMessage `json:"linkset"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/linkset+json") || len(catalog.Linkset) != 1 ||
		!strings.Contains(string(catalog.Linkset[0]["service-desc"]), "http://recipes.example.com/openapi.json") {
		t.Fatalf("catalog = %s", response.Body.String())
	}
}
