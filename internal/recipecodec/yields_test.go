package recipecodec

import (
	"strings"
	"testing"
)

func TestAStoredSingularYieldReadsAsOneYield(t *testing.T) {
	t.Parallel()

	stored := `{"schema_version":1,"id":"r","title":"R","yield":{"text":"4 servings"},"ingredient_sections":[],"instruction_sections":[]}`

	document, err := Decode([]byte(stored), CurrentVersion)
	if err != nil {
		t.Fatal(err)
	}

	if len(document.Yields) != 1 || document.Yields[0].Text != "4 servings" {
		t.Fatalf("yields = %#v", document.Yields)
	}

	encoded, _, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(encoded), `"yield":`) || !strings.Contains(string(encoded), `"yields":[{"text":"4 servings"}]`) {
		t.Fatalf("rewritten document = %s", encoded)
	}
}
