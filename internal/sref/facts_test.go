package sref_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
)

const factsDocument = `{
  "sref": {"version": "0.4.0", "unit_registry": "0.2.0"},
  "id": "facts", "title": "Facts",
  "yields": [{"text": "24 cookies"}, {"text": "about 600 g"}],
  "nutrition": [{"basis": {"kind": "serving", "text": "2 cookies"}, "nutrients": [{"nutrient": "energy", "amount": {"value": "310"}, "unit": "kcal"}, {"nutrient": "fiber", "text": "trace"}]}],
  "variants": [{"id": "oat", "text": "Use oat flour."}],
  "dietary_claims": [{"text": "Gluten-free with oat flour", "suitability": "suitable", "diet": "gluten_free", "condition": "with oat flour", "variant_ref": "oat"}],
  "allergen_declarations": [{"text": "May contain nuts", "substance": "nuts", "presence": "may_contain"}],
  "ingredient_sections": [{"id": "s", "ingredients": [{"id": "cream", "name": "pastry cream", "recipe": {"title": "Pastry Cream", "url": "https://example.com/cream"}}]}],
  "assets": [{"id": "photo", "path": "assets/photo.jpg", "media_type": "image/jpeg", "sha256": "` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `", "size": 1, "credit": "Photo: Jane Smith", "license": {"name": "CC BY 4.0"}}]
}`

func TestAuthoredFactsSurviveReadingAndWriting(t *testing.T) {
	t.Parallel()

	document, err := sref.Decode([]byte(factsDocument))
	if err != nil {
		t.Fatal(err)
	}

	if len(document.Yields) != 2 || document.Nutrition[0].Basis.Kind != recipe.NutritionBasisServing ||
		document.DietaryClaims[0].VariantRef != "oat" || document.AllergenDeclarations[0].Presence != recipe.AllergenMayContain ||
		document.IngredientSections[0].Ingredients[0].Recipe.Title != "Pastry Cream" || document.Assets[0].License.Name != "CC BY 4.0" {
		t.Fatalf("read %#v", document)
	}

	written, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	again, err := sref.Decode(written)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(document, again) {
		t.Fatalf("round trip changed the recipe:\n%s", written)
	}
}

func TestTheSingularYieldBelongsToEarlierVersions(t *testing.T) {
	t.Parallel()

	earlier := `{"sref": {"version": "0.2.0", "unit_registry": "0.2.0"}, "id": "r", "title": "R", "yield": {"text": "4 servings"}, "instruction_sections": [{"id": "m", "steps": [{"id": "s", "text": "Mix."}]}]}`

	document, err := sref.Decode([]byte(earlier))
	if err != nil {
		t.Fatal(err)
	}

	if len(document.Yields) != 1 || document.Yields[0].Text != "4 servings" {
		t.Fatalf("yields = %#v", document.Yields)
	}

	current := strings.Replace(earlier, `"0.2.0", "unit_registry"`, `"0.4.0", "unit_registry"`, 1)
	if _, err := sref.Decode([]byte(current)); sref.RequirementID(err) != "singular-yield-member" {
		t.Fatalf("0.4 singular yield: %v (%s)", err, sref.RequirementID(err))
	}
}
