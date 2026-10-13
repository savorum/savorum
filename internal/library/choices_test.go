package library_test

import (
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestSearchProjectionIncludesEveryAuthoredAlternative(t *testing.T) {
	document := testfixture.Document()
	document.IngredientSections[0].Ingredients = []recipe.Ingredient{{ID: "choice", Relation: "or", Alternatives: []recipe.Ingredient{{ID: "butter", Name: "butter"}, {ID: "margarine", Name: "margarine", Note: "softened"}}}}

	projection, err := library.BuildProjection(document)
	if err != nil {
		t.Fatal(err)
	}

	for _, word := range []string{"butter", "margarine", "softened"} {
		if !strings.Contains(projection.SearchText, word) {
			t.Fatalf("search missing %q", word)
		}
	}
}
