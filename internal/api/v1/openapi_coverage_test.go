package v1

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Routes registered directly on the mux must be described by
// `documentDirectImports`. This walks the mux's own route list.
func TestEveryDirectlyRegisteredRouteIsDescribed(t *testing.T) {
	t.Parallel()

	handler, _, _ := testAPI(t)

	registered := handler.Routes()
	if len(registered) == 0 {
		t.Fatal("no direct mux registrations found; this test has stopped testing anything")
	}

	document := map[string]any{}

	contract, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(contract, &document); err != nil {
		t.Fatal(err)
	}

	paths, _ := document["paths"].(map[string]any)

	for _, route := range registered {
		method, path := strings.ToLower(route.Method), route.Path
		t.Run(route.Method+" "+path, func(t *testing.T) {
			operations, described := paths[path].(map[string]any)
			if !described {
				t.Fatalf("%s is served and api/openapi.json does not mention it; describe it in documentDirectImports", path)
			}

			if _, found := operations[method]; !found {
				t.Fatalf("%s %s is served and api/openapi.json describes the path without that method", route.Method, path)
			}
		})
	}
}
