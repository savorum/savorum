package app_test

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/app"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/configuration"
	"github.com/savorum/savorum/internal/contracttest"
	"github.com/savorum/savorum/internal/importer/webrecipe"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
)

// Every documented operation is called once on the assembled application with
// a minimal request, and each response must match the contract. Operations
// that do not resolve to a registered route fail because the router's own
// not-found body is not a documented response.
func TestEveryOperationAnswersWithinTheContract(t *testing.T) {
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	blobs, err := files.NewAssetStore(filepath.Join(t.TempDir(), "assets"), files.DefaultMaximumAssetSize)
	if err != nil {
		t.Fatal(err)
	}

	assetService, err := assets.NewService(store, blobs)
	if err != nil {
		t.Fatal(err)
	}

	settings, err := configuration.NewService(store, configuration.InstanceOverrides{})
	if err != nil {
		t.Fatal(err)
	}

	application, err := app.New(store, units.DefaultCatalog(), app.Options{
		Assets:              assetService,
		Configuration:       settings,
		WebhookDirectory:    t.TempDir(),
		AutomationDirectory: t.TempDir(),
		StoragePath:         t.TempDir(),
		PageFetcher: func(_ context.Context, address string) (*webrecipe.Page, error) {
			return &webrecipe.Page{URL: address, ContentType: "text/html", Body: []byte(recipePage)}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	closeServices(t, application)

	sweep(t, func(operation contracttest.Operation) bool {
		return !strings.HasPrefix(operation.Path, "/api/v1/auth/")
	}, "", func(request *http.Request) (int, http.Header, []byte) {
		response := httptest.NewRecorder()
		application.Handler.ServeHTTP(response, request)

		return response.Code, response.Header(), response.Body.Bytes()
	})
}

// With authentication and a provider configured, the provider routes exist and
// every other operation answers an anonymous caller.
func TestEveryOperationAnswersAnonymousCallersWithinTheContract(t *testing.T) {
	fixture := newNativeFixture(t)

	client := fixture.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	sweep(t, func(contracttest.Operation) bool { return true }, fixture.server.URL, func(request *http.Request) (int, http.Header, []byte) {
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}

		defer response.Body.Close()

		body, _ := io.ReadAll(response.Body)

		return response.StatusCode, response.Header, body
	})
}

func sweep(t *testing.T, include func(contracttest.Operation) bool, origin string, serve func(*http.Request) (int, http.Header, []byte)) {
	t.Helper()

	contract, err := contracttest.Load()
	if err != nil {
		t.Fatal(err)
	}

	operations := contract.Operations()
	slices.SortFunc(operations, func(a, b contracttest.Operation) int {
		return cmp.Or(strings.Compare(a.Path, b.Path), strings.Compare(a.Method, b.Method))
	})

	for _, operation := range operations {
		details := contract.Details(operation)
		if streams(details) || !include(operation) {
			continue
		}

		t.Run(operation.Method+" "+operation.Path, func(t *testing.T) {
			target, body, headers := minimalRequest(t, contract, operation, details)

			request, err := http.NewRequestWithContext(t.Context(), operation.Method, origin+target, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}

			if origin == "" {
				request.Host = "example.com"
			}

			request.Header.Set("Origin", cmp.Or(origin, "http://example.com"))

			if body != "" {
				request.Header.Set("Content-Type", "application/json")
			}

			for name, value := range headers {
				request.Header.Set(name, value)
			}

			code, header, payload := serve(request)
			if err := contract.Validate(operation, code, header.Get("Content-Type"), payload); err != nil {
				t.Errorf("%s -> %d: %v\n%.300s", target, code, err, payload)
			}
		})
	}
}

func streams(details map[string]any) bool {
	responses, _ := details["responses"].(map[string]any)
	for _, response := range responses {
		content, _ := response.(map[string]any)["content"].(map[string]any)
		if _, ok := content["text/event-stream"]; ok {
			return true
		}
	}

	return false
}

func minimalRequest(t *testing.T, contract *contracttest.Contract, operation contracttest.Operation, details map[string]any) (target, body string, headers map[string]string) {
	t.Helper()

	target = operation.Path
	headers = map[string]string{}

	var query []string

	parameters, _ := details["parameters"].([]any)
	for _, entry := range parameters {
		parameter, _ := entry.(map[string]any)
		name, _ := parameter["name"].(string)
		value := contract.Example(parameter["schema"])
		required, _ := parameter["required"].(bool)

		switch parameter["in"] {
		case "path":
			if name == "provider" {
				value = "work"
			}

			target = strings.ReplaceAll(target, "{"+name+"}", toText(value))
		case "query":
			if required {
				query = append(query, name+"="+toText(value))
			}
		case "header":
			if required && !slices.Contains([]string{"Origin", "X-CSRF-Token"}, name) {
				headers[name] = toText(value)
			}
		}
	}

	if len(query) > 0 {
		target += "?" + strings.Join(query, "&")
	}

	if request, ok := details["requestBody"].(map[string]any); ok {
		content, _ := request["content"].(map[string]any)
		for media, entry := range content {
			if !strings.HasSuffix(media, "json") {
				continue
			}

			declared, _ := entry.(map[string]any)

			encoded, err := json.Marshal(contract.Example(declared["schema"]))
			if err != nil {
				t.Fatal(err)
			}

			return target, string(encoded), headers
		}
	}

	return target, "", headers
}

func toText(value any) string {
	if text, ok := value.(string); ok {
		return text
	}

	return fmt.Sprint(value)
}
