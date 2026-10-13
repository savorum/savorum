package ingredientparser

import (
	"strings"
	"testing"
)

func FuzzIngredientLine(f *testing.F) {
	for _, line := range []string{
		"2 cups flour, plus 1 tbsp for dusting",
		"1½ kg potatoes, peeled",
		"⅓ cup water or 80 ml milk",
		"999999999999999999999999999999999999 cups flour",
		strings.Repeat("1/", 256) + " flour",
		"olive oil, to taste, optional",
	} {
		f.Add(line)
	}

	f.Fuzz(func(t *testing.T, line string) {
		if len(line) > 16<<10 {
			return
		}

		parsed := ParseDetailed(line, "ingredient", "en-US").Ingredient
		if parsed.ID != "ingredient" || parsed.SourceText != strings.TrimSpace(line) {
			t.Fatalf("ingredient identity or source wording changed")
		}
	})
}

func TestHugeRationalAndSeparatorInputKeepsSource(t *testing.T) {
	for _, line := range []string{
		strings.Repeat("9", 4096) + "/" + strings.Repeat("9", 4096) + " cups flour",
		strings.Repeat("1/", 8192) + " flour",
	} {
		parsed := ParseDetailed(line, "ingredient", "en-US").Ingredient
		if parsed.SourceText != line {
			t.Fatal("hostile quantity changed source text")
		}
	}
}
