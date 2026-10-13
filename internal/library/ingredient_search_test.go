package library

import (
	"errors"
	"slices"
	"testing"
)

func TestIngredientQualifiersPreserveValuesAndCombineWithAuthors(t *testing.T) {
	t.Parallel()

	criteria, err := ParseSearchQuery(`soup ingredient:"chicken broth" INGREDIENT:"A \"B\" \\ C" author:"Julia Child" tag:t1`)
	if err != nil {
		t.Fatal(err)
	}

	if criteria.Text != "soup" || !slices.Equal(criteria.Ingredients, []string{"chicken broth", `A "B" \ C`}) ||
		!slices.Equal(criteria.Authors, []string{"Julia Child"}) || !slices.Equal(criteria.Tags, []string{"t1"}) {
		t.Fatalf("criteria = %+v", criteria)
	}

	for _, query := range []string{`ingredient:`, `ingredient:" "`, `ingredient:"chicken`} {
		if _, err := ParseSearchQuery(query); !errors.Is(err, ErrInvalidSearch) {
			t.Errorf("query %q error = %v", query, err)
		}
	}

	literal, err := ParseSearchQuery(`"ingredient:chicken"`)
	if err != nil || len(literal.Ingredients) != 0 || literal.Text != "ingredient:chicken" {
		t.Fatalf("literal query = %+v, %v", literal, err)
	}
}
