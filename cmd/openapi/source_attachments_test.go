package main

import (
	"strings"
	"testing"
)

func TestRetainedSourceContractsArePublished(t *testing.T) {
	definition, err := schemaDefinition()
	if err != nil {
		t.Fatal(err)
	}

	for path, id := range map[string]string{
		"/api/v1/recipes/{recipe_id}/sources":                           "list-recipe-sources",
		"/api/v1/recipes/{recipe_id}/revisions/{revision}/sources":      "list-recipe-revision-sources",
		"/api/v1/recipes/{recipe_id}/sources/{id}":                      "get-recipe-source",
		"/api/v1/recipes/{recipe_id}/revisions/{revision}/sources/{id}": "get-recipe-revision-source",
	} {
		operation := definition.OpenAPI().Paths[path]
		if operation == nil || operation.Get == nil || operation.Get.OperationID != id {
			t.Errorf("retained source operation %q missing at %s", id, path)
			continue
		}

		if !strings.HasPrefix(id, "get-") {
			continue
		}

		response := operation.Get.Responses["200"]
		if response == nil || response.Content["*/*"] == nil {
			t.Errorf("retained source download %q has no binary success body", id)
			continue
		}

		schema := response.Content["*/*"].Schema
		if schema == nil || schema.Type != "string" || schema.Format != "binary" {
			t.Errorf("retained source download %q has an invalid binary success schema: %+v", id, schema)
		}
	}
}
