package main

import "testing"

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
		}
	}
}
