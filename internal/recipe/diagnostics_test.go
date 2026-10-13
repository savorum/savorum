package recipe_test

import (
	"testing"

	"github.com/savorum/savorum/internal/recipe"
)

func unresolvedIngredient(id string) recipe.Ingredient {
	return recipe.Ingredient{
		ID: id, Name: "2 c flour", SourceText: "2 c flour",
		Normalization: &recipe.Normalization{
			Method:   recipe.NormalizationUnresolved,
			Warnings: []string{"The amount and measure were ambiguous."},
		},
	}
}

func documentWithIngredients(values ...recipe.Ingredient) recipe.Document {
	return recipe.Document{IngredientSections: []recipe.IngredientSection{{ID: "ingredients", Ingredients: values}}}
}

func TestIngredientDiagnosticFollowsStableIdentityAcrossReordering(t *testing.T) {
	uncertain := unresolvedIngredient("uncertain")
	clean := recipe.Ingredient{ID: "clean", Name: "salt"}
	current := documentWithIngredients(uncertain, clean)
	replacement := documentWithIngredients(clean, uncertain)

	got := recipe.ReconcileIngredientDiagnostics(current, replacement)
	if got.IngredientSections[0].Ingredients[1].Normalization == nil ||
		len(got.IngredientSections[0].Ingredients[1].Normalization.Warnings) != 1 {
		t.Fatalf("reordered diagnostic = %#v", got.IngredientSections[0].Ingredients)
	}
}

func TestDeletingAnIngredientDeletesItsDiagnostic(t *testing.T) {
	current := documentWithIngredients(unresolvedIngredient("uncertain"), recipe.Ingredient{ID: "clean", Name: "salt"})

	got := recipe.ReconcileIngredientDiagnostics(current, documentWithIngredients(recipe.Ingredient{ID: "clean", Name: "salt"}))
	if len(got.IngredientSections[0].Ingredients) != 1 || got.IngredientSections[0].Ingredients[0].Normalization != nil {
		t.Fatalf("ingredients after deletion = %#v", got.IngredientSections[0].Ingredients)
	}
}

func TestCorrectedIngredientClearsBroadWarning(t *testing.T) {
	current := documentWithIngredients(unresolvedIngredient("i1"))
	replacement := documentWithIngredients(unresolvedIngredient("i1"))
	replacement.IngredientSections[0].Ingredients[0].Quantity = &recipe.Quantity{
		Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: new(recipe.Rational("2"))}, Unit: "volume.cup.us.customary",
	}

	got := recipe.ReconcileIngredientDiagnostics(current, replacement)

	normalization := got.IngredientSections[0].Ingredients[0].Normalization
	if normalization == nil || normalization.Method != recipe.NormalizationAuthored || normalization.Review != recipe.ReviewCorrected || len(normalization.Warnings) != 0 {
		t.Fatalf("corrected normalization = %#v", normalization)
	}
}

func TestPartialIngredientCorrectionNarrowsWarning(t *testing.T) {
	current := documentWithIngredients(unresolvedIngredient("i1"))
	replacement := documentWithIngredients(unresolvedIngredient("i1"))
	replacement.IngredientSections[0].Ingredients[0].Quantity = &recipe.Quantity{
		Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: new(recipe.Rational("2"))},
	}

	got := recipe.ReconcileIngredientDiagnostics(current, replacement)

	normalization := got.IngredientSections[0].Ingredients[0].Normalization
	if normalization == nil || normalization.Method != recipe.NormalizationInferred || normalization.Review != recipe.ReviewCorrected ||
		len(normalization.Warnings) != 1 || normalization.Warnings[0] != recipe.MissingIngredientMeasureWarning {
		t.Fatalf("partial normalization = %#v", normalization)
	}
}

func TestAuthoredTextAcceptanceIsRespected(t *testing.T) {
	current := documentWithIngredients(unresolvedIngredient("i1"))
	replacement := documentWithIngredients(unresolvedIngredient("i1"))
	replacement.IngredientSections[0].Ingredients[0].Normalization = &recipe.Normalization{
		Method: recipe.NormalizationAuthored, Review: recipe.ReviewConfirmed,
	}

	got := recipe.ReconcileIngredientDiagnostics(current, replacement)
	if normalization := got.IngredientSections[0].Ingredients[0].Normalization; normalization.Method != recipe.NormalizationAuthored || len(normalization.Warnings) != 0 {
		t.Fatalf("accepted normalization = %#v", normalization)
	}
}

func TestPortableNormalizationProvenanceIsNotAlwaysActionable(t *testing.T) {
	cases := []struct {
		name          string
		normalization *recipe.Normalization
		want          bool
	}{
		{name: "unreviewed", normalization: &recipe.Normalization{Method: recipe.NormalizationUnresolved, Warnings: []string{"ambiguous"}}, want: true},
		{name: "confirmed source", normalization: &recipe.Normalization{Method: recipe.NormalizationUnresolved, Review: recipe.ReviewConfirmed, Warnings: []string{"ambiguous"}}},
		{name: "import choice", normalization: &recipe.Normalization{Method: recipe.NormalizationInferred, Review: recipe.ReviewCorrected, Warnings: []string{recipe.ImportReviewUnitPrefix + " The source did not identify a regional measurement convention. It was chosen for this measurement."}}},
		{name: "partial edit", normalization: &recipe.Normalization{Method: recipe.NormalizationInferred, Review: recipe.ReviewCorrected, Warnings: []string{recipe.MissingIngredientMeasureWarning}}, want: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := recipe.NormalizationNeedsReview(test.normalization); got != test.want {
				t.Fatalf("NormalizationNeedsReview() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestBranchDiagnosticsFollowTheOriginalIngredientAfterAddingAChoice(t *testing.T) {
	current := documentWithIngredients(unresolvedIngredient("flour"))
	branch := unresolvedIngredient("flour")
	branch.Name = "flour"
	branch.Quantity = &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: new(recipe.Rational("2"))}, Unit: "volume.cup.us.customary"}
	replacement := documentWithIngredients(recipe.Ingredient{ID: "choice", Relation: "or", Alternatives: []recipe.Ingredient{branch, {ID: "other", Name: "other flour"}}})

	got := recipe.ReconcileIngredientDiagnostics(current, replacement)
	if got.IngredientSections[0].Ingredients[0].Alternatives[0].Normalization.Method != recipe.NormalizationAuthored {
		t.Fatal("corrected branch retained a stale unresolved warning")
	}
}
