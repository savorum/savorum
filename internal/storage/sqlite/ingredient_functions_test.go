package sqlite

import "testing"

func TestIngredientSearchNamesPreferLeafNamesAndExcludeChoiceWording(t *testing.T) {
	t.Parallel()

	input := `{"ingredient_sections":[{"title":"chicken heading","ingredients":[
		{"name":" lentils ","source_text":"chicken raw line","note":"chicken note","optional":true},
		{"name":"\t ","source_text":"  unparsed chicken line  ","note":"not a name"},
		{"source_text":"lamb choice wording","alternatives":[{"name":"tofu"},{"name":"chicken thighs"}]},
		{"name":"tofu"}
	]}],"instructions":[{"text":"chicken instruction"}]}`

	got, err := ingredientSearchNames(input)
	if err != nil || got != `["chicken thighs","lentils","tofu","unparsed chicken line"]` {
		t.Fatalf("ingredient search names = %s, %v", got, err)
	}
}
