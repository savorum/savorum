package semanticdiff_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/semanticdiff"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestAnAbsentOptionalArrayAndAnEmptyOneSayTheSameThing(t *testing.T) {
	t.Parallel()

	absent := testfixture.Document()
	absent.Keywords = nil
	absent.Notes = nil
	empty := testfixture.Document()
	empty.Keywords = []string{}

	empty.Notes = []string{}
	if differences := semanticdiff.Diff(absent, empty); len(differences) != 0 {
		t.Fatalf("an omitted optional array was treated as a change:\n%s", semanticdiff.Report(differences))
	}
}

func TestStorageBookkeepingIsNotRecipeChange(t *testing.T) {
	t.Parallel()

	before := testfixture.Document()
	after := testfixture.Document()

	after.SchemaVersion = before.SchemaVersion + 1
	for index := range after.Times.Assertions {
		after.Times.Assertions[index].Provenance = ""
	}

	if differences := semanticdiff.Diff(before, after); len(differences) != 0 {
		t.Fatalf("Savorum's own bookkeeping was reported as recipe change:\n%s", semanticdiff.Report(differences))
	}
}

func TestReorderingIsReportedAgainstOrderRatherThanEveryItem(t *testing.T) {
	t.Parallel()

	before := testfixture.Document()
	after := testfixture.Document()

	ingredients := after.IngredientSections[0].Ingredients
	if len(ingredients) < 2 {
		t.Fatalf("the fixture no longer has two ingredients to reorder")
	}

	ingredients[0], ingredients[1] = ingredients[1], ingredients[0]

	differences := semanticdiff.Diff(before, after)
	if len(differences) != 1 {
		t.Fatalf("a reordering should be one difference, got:\n%s", semanticdiff.Report(differences))
	}

	if !strings.HasSuffix(differences[0].Path, ".ingredients.order") {
		t.Fatalf("a reordering was reported against %q rather than the order", differences[0].Path)
	}
}

func TestAnIngredientKeepsItsIdentityWhenItsNeighboursMove(t *testing.T) {
	t.Parallel()

	before := testfixture.Document()
	after := testfixture.Document()
	ingredients := after.IngredientSections[0].Ingredients
	ingredients[0], ingredients[1] = ingredients[1], ingredients[0]
	// Renaming the moved ingredient adds exactly one difference.
	renamed := ingredients[0].ID
	ingredients[0].Note = "chilled"

	differences := semanticdiff.Diff(before, after)
	if len(differences) != 2 {
		t.Fatalf("expected the order and one note, got:\n%s", semanticdiff.Report(differences))
	}

	wanted := fmt.Sprintf("ingredients[%s].note", renamed)
	if !strings.HasSuffix(differences[1].Path, wanted) {
		t.Fatalf("the changed ingredient was addressed as %q rather than by its identity %q", differences[1].Path, wanted)
	}
}

func TestAnExactAmountThatBecameADecimalIsALoss(t *testing.T) {
	t.Parallel()

	before := testfixture.Document()
	before.IngredientSections[0].Ingredients[0].Quantity = &recipe.Quantity{
		Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: testfixture.Rational("1/3")}, Unit: "volume.cup.us",
	}
	after := testfixture.Document()
	// What a float round trip does to a third of a cup.
	degraded := recipe.Rational("0.3333333333333333")
	after.IngredientSections[0].Ingredients[0].Quantity = &recipe.Quantity{
		Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: &degraded}, Unit: "volume.cup.us",
	}

	differences := semanticdiff.Diff(before, after)
	if len(differences) != 1 || differences[0].Before != "1/3" {
		t.Fatalf("a float-degraded amount was not reported as a loss:\n%s", semanticdiff.Report(differences))
	}
}

func TestARangeIsNotTheSameAsItsLowerBound(t *testing.T) {
	t.Parallel()

	before := testfixture.Document()
	before.IngredientSections[0].Ingredients[0].Quantity = &recipe.Quantity{
		Kind:   recipe.QuantitySimple,
		Amount: &recipe.Amount{Min: testfixture.Rational("2"), Max: testfixture.Rational("3"), Approximate: true},
		Unit:   "count.item",
	}
	after := testfixture.Document()

	after.IngredientSections[0].Ingredients[0].Quantity = &recipe.Quantity{
		Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: testfixture.Rational("2")}, Unit: "count.item",
	}
	if semanticdiff.Equal(before, after) {
		t.Fatal("a range collapsed to a single value compared equal")
	}
}

func TestOpaqueIngredientTextIsCompared(t *testing.T) {
	t.Parallel()

	before := testfixture.Document()
	before.IngredientSections[0].Ingredients[0].Quantity = &recipe.Quantity{
		Kind: recipe.QuantityOpaque, Text: "a good handful",
	}
	after := testfixture.Document()
	after.IngredientSections[0].Ingredients[0].Quantity = &recipe.Quantity{
		Kind: recipe.QuantityOpaque, Text: "a handful",
	}

	differences := semanticdiff.Diff(before, after)
	if len(differences) != 1 || !strings.HasSuffix(differences[0].Path, ".quantity.text") {
		t.Fatalf("opaque quantity wording was not compared:\n%s", semanticdiff.Report(differences))
	}
}

func TestOpaqueTemperatureTextIsCompared(t *testing.T) {
	t.Parallel()

	before := testfixture.Document()
	before.InstructionSections[0].Steps[0].Temperatures = []recipe.Temperature{{Text: "moderate oven"}}
	after := testfixture.Document()
	after.InstructionSections[0].Steps[0].Temperatures = []recipe.Temperature{{Text: "quick oven"}}

	differences := semanticdiff.Diff(before, after)
	if len(differences) != 1 || !strings.HasSuffix(differences[0].Path, ".temperatures[0].text") {
		t.Fatalf("opaque temperature wording was not compared:\n%s", semanticdiff.Report(differences))
	}
}

func TestPreservedNumbersAreComparedByTheirLiteralRatherThanThroughAFloat(t *testing.T) {
	t.Parallel()

	const large = "184467440737095516160"

	before := testfixture.Document()
	before.Extensions = map[string]any{"x-example": map[string]any{"sequence": json.Number(large)}}
	after := testfixture.Document()

	var degraded any
	if err := json.Unmarshal([]byte(large), &degraded); err != nil {
		t.Fatal(err)
	}

	after.Extensions = map[string]any{"x-example": map[string]any{"sequence": degraded}}

	differences := semanticdiff.Diff(before, after)
	if len(differences) != 1 {
		t.Fatalf("an integer beyond float64 was not reported as changed:\n%s", semanticdiff.Report(differences))
	}

	if differences[0].Before != "number:"+large {
		t.Fatalf("the preserved literal was not kept: %#v", differences[0])
	}
}

func TestAPreservedNumberIsNotAPreservedString(t *testing.T) {
	t.Parallel()

	before := testfixture.Document()
	before.Extensions = map[string]any{"x-example": map[string]any{"servings": json.Number("4")}}
	after := testfixture.Document()

	after.Extensions = map[string]any{"x-example": map[string]any{"servings": "4"}}
	if semanticdiff.Equal(before, after) {
		t.Fatal("a preserved number that became a string compared equal")
	}
}

func TestPreservedObjectMemberOrderIsNotSemanticButArrayOrderIs(t *testing.T) {
	t.Parallel()

	first := testfixture.Document()
	first.SREFOpaque = map[string]any{"document/publication": map[string]any{
		"status": "draft", "reviewers": []any{"ana", "ben"},
	}}
	// The same object, built with its members named in the other order.
	second := testfixture.Document()

	second.SREFOpaque = map[string]any{"document/publication": map[string]any{
		"reviewers": []any{"ana", "ben"}, "status": "draft",
	}}
	if !semanticdiff.Equal(first, second) {
		t.Fatalf("member order was treated as meaning:\n%s", semanticdiff.Report(semanticdiff.Diff(first, second)))
	}

	reordered := testfixture.Document()

	reordered.SREFOpaque = map[string]any{"document/publication": map[string]any{
		"status": "draft", "reviewers": []any{"ben", "ana"},
	}}
	if semanticdiff.Equal(first, reordered) {
		t.Fatal("a reordered preserved array compared equal")
	}
}

func TestALostExtensionIsReported(t *testing.T) {
	t.Parallel()

	before := testfixture.Document()
	before.Extensions = map[string]any{"x-vendor": map[string]any{"kept": true}}
	after := testfixture.Document()

	differences := semanticdiff.Diff(before, after)
	if len(differences) != 1 || differences[0].After != "" {
		t.Fatalf("a discarded extension was not reported:\n%s", semanticdiff.Report(differences))
	}
}

func TestAnAssetIsComparedByEveryFactTheRecipeRecords(t *testing.T) {
	t.Parallel()

	before := testfixture.Document()
	before.Assets = []recipe.Asset{{
		ID: "hero", Path: "assets/hero.jpg", MediaType: "image/jpeg",
		SHA256: strings.Repeat("a", 64), Size: 1024, Role: recipe.AssetRoleImage,
		Alt: "A hand pie", Caption: "Straight from the oven",
	}}
	after := testfixture.Document()
	after.Assets = []recipe.Asset{{
		ID: "hero", Path: "assets/hero.jpg", MediaType: "image/jpeg",
		SHA256: strings.Repeat("a", 64), Size: 1024, Role: recipe.AssetRoleImage,
		Alt: "A hand pie",
	}}

	differences := semanticdiff.Diff(before, after)
	if len(differences) != 1 || !strings.HasSuffix(differences[0].Path, ".caption") {
		t.Fatalf("a dropped caption was not reported:\n%s", semanticdiff.Report(differences))
	}
}

func TestReportNamesThePathsRatherThanDumpingTwoDocuments(t *testing.T) {
	t.Parallel()

	before := testfixture.Document()
	after := testfixture.Document()
	after.Title = "Something else"

	report := semanticdiff.Report(semanticdiff.Diff(before, after))
	if !strings.HasPrefix(report, "title: ") || strings.Contains(report, "\n") {
		t.Fatalf("unhelpful report: %q", report)
	}
}

// excludedFields are the members of the recipe model the projection
// deliberately ignores, with the reason each one is not part of what a recipe
// means. Anything not listed here has to be projected.
var excludedFields = map[string]string{
	"Document.SchemaVersion":     "the storage codec's discriminator; SREF carries its own independent version",
	"TimingAssertion.Provenance": "Savorum's record of how it came to hold a timing, deliberately absent from SREF",
}

func TestProjectionCoversEveryFieldOfTheRecipeModel(t *testing.T) {
	t.Parallel()
	// Walk the model so a new recipe.Document field cannot go unprojected.
	projected := map[string]bool{}
	for _, name := range projectedFields() {
		projected[name] = true
	}

	for _, name := range modelFields(reflect.TypeFor[recipe.Document]()) {
		if _, excluded := excludedFields[name]; excluded {
			continue
		}

		if !projected[name] {
			t.Errorf("%s is in the recipe model but not in the semantic projection; project it or record why it is excluded", name)
		}
	}

	for name := range excludedFields {
		if projected[name] {
			t.Errorf("%s is listed as excluded but is projected", name)
		}
	}
}

func modelFields(target reflect.Type) []string {
	seen := map[string]bool{}

	var (
		names []string
		walk  func(reflect.Type)
	)

	walk = func(current reflect.Type) {
		for current.Kind() == reflect.Pointer || current.Kind() == reflect.Slice {
			current = current.Elem()
		}

		if current.Kind() != reflect.Struct || seen[current.Name()] {
			return
		}

		seen[current.Name()] = true
		for field := range current.Fields() {
			names = append(names, current.Name()+"."+field.Name)
			walk(field.Type)
		}
	}
	walk(target)
	sort.Strings(names)

	return names
}

// projectedFields lists what Project reads. It is written out rather than
// derived so that adding a model field forces a deliberate decision here.
func projectedFields() []string {
	return []string{
		"AlternativeOption.Amount", "AlternativeOption.Kind", "AlternativeOption.Terms", "AlternativeOption.Unit",
		"Amount.Approximate", "Amount.Max", "Amount.Min", "Amount.Value",
		"Asset.Alt", "Asset.Caption", "Asset.Creator", "Asset.Credit", "Asset.Rights", "Asset.License", "Asset.SourceURL",
		"AssetLicense.Name", "AssetLicense.URL", "Asset.ID", "Asset.MediaType", "Asset.Path", "Asset.Role", "Asset.SHA256", "Asset.Size",
		"Author.Kind", "Author.KindLabel", "Author.Name", "Author.URL",
		"Document.Assets", "Document.Authors", "Document.Categories", "Document.CookingMethods", "Document.Courses",
		"Document.Cuisines", "Document.Description", "Document.Extensions", "Document.ID", "Document.ImageRefs",
		"Document.Equipment", "Document.IngredientSections", "Document.InstructionSections", "Document.Keywords", "Document.Language",
		"Document.Modified", "Document.Notes", "Document.PrimaryImage", "Document.Published", "Document.SREFOpaque",
		"Document.Source", "Document.SourceLocale", "Document.Times", "Document.Title", "Document.Translations", "Document.Variants", "Document.Yields",
		"Document.Nutrition", "Document.DietaryClaims", "Document.AllergenDeclarations",
		"DurationExpression.Approximate", "DurationExpression.Max", "DurationExpression.Min",
		"DurationExpression.SourceText", "DurationExpression.Text", "DurationExpression.Value",
		"Equipment.Name", "Equipment.Note", "Equipment.Quantity",
		"Ingredient.ID", "Ingredient.Name", "Ingredient.Normalization", "Ingredient.Note", "Ingredient.Optional",
		"Ingredient.Alternatives", "Ingredient.Relation", "Ingredient.PackageSize", "Ingredient.Quantity", "Ingredient.SourceText", "Ingredient.Temperature", "Ingredient.Recipe",
		"RecipeReference.Title", "RecipeReference.RecipeID", "RecipeReference.URL",
		"IngredientSection.ID", "IngredientSection.Ingredients", "IngredientSection.Title",
		"IngredientUse.IngredientRef", "IngredientUse.Quantity",
		"InstructionSection.ID", "InstructionSection.Steps", "InstructionSection.Title",
		"MicrowaveChoice.Duration", "MicrowaveChoice.Power", "MicrowaveChoice.RatedOutputWatts", "MicrowaveChoice.SourceText",
		"MicrowaveConditions.Choices", "MicrowaveConditions.ModeText", "MicrowaveConditions.Power", "MicrowaveConditions.RatedOutputWatts", "MicrowaveConditions.Restrictions", "MicrowaveConditions.SourceText",
		"MicrowavePower.Percent", "MicrowavePower.Text", "MicrowavePower.Watts",
		"Normalization.Method", "Normalization.Review", "Normalization.Warnings",
		"Quantity.Amount", "Quantity.Kind", "Quantity.Options", "Quantity.Terms", "Quantity.Text", "Quantity.Unit",
		"SimpleQuantity.Amount", "SimpleQuantity.Kind", "SimpleQuantity.Unit",
		"Source.Application", "Source.Attribution", "Source.Name", "Source.RetrievedAt", "Source.SourceID", "Source.URL",
		"Step.ID", "Step.ImageRefs", "Step.IngredientUses", "Step.Temperatures", "Step.Text", "Step.TimingRefs", "Step.Title",
		"Temperature.Amount", "Temperature.Note", "Temperature.Purpose", "Temperature.SourceText", "Temperature.Text", "Temperature.Unit",
		"TemperaturePurpose.Kind", "TemperaturePurpose.Label",
		"Translation.Language", "Translation.RecipeID", "Translation.Relation", "Translation.Title", "Translation.URL",
		"Times.Additional", "Times.Assertions", "Times.Cook", "Times.Prep", "Times.Total",
		"TimingAssertion.Attention", "TimingAssertion.Duration", "TimingAssertion.ID", "TimingAssertion.Kind",
		"TimingAssertion.Label", "TimingAssertion.Microwave", "TimingAssertion.Note",
		"Variant.ID", "Variant.Text", "Variant.Title",
		"NutritionStatement.Basis", "NutritionStatement.Nutrients", "NutritionStatement.Note",
		"NutritionBasis.Kind", "NutritionBasis.Text", "NutritionBasis.Quantity",
		"Nutrient.Nutrient", "Nutrient.Label", "Nutrient.Amount", "Nutrient.Unit", "Nutrient.SourceText", "Nutrient.Text",
		"DietaryClaim.Text", "DietaryClaim.Suitability", "DietaryClaim.Diet", "DietaryClaim.Condition", "DietaryClaim.VariantRef",
		"AllergenDeclaration.Text", "AllergenDeclaration.Substance", "AllergenDeclaration.Presence", "AllergenDeclaration.Condition", "AllergenDeclaration.VariantRef",
		"Yield.Label", "Yield.Quantity", "Yield.Text",
	}
}

func TestLostIgnoresWhatWasAddedAndReportsWhatWentMissing(t *testing.T) {
	t.Parallel()

	before := testfixture.Document()
	enriched := testfixture.Document()
	enriched.Times.Assertions = append(enriched.Times.Assertions, recipe.TimingAssertion{
		ID: "timing-1", Kind: recipe.TimingRest,
		Duration: recipe.DurationExpression{Value: "PT10M", SourceText: "rest for 10 minutes"},
	})

	enriched.Keywords = append(enriched.Keywords, "make-ahead")
	if lost := semanticdiff.Lost(before, enriched); len(lost) != 0 {
		t.Fatalf("an addition was reported as a loss:\n%s", semanticdiff.Report(lost))
	}

	if len(semanticdiff.Diff(before, enriched)) == 0 {
		t.Fatal("Diff should still report the addition; only Lost overlooks it")
	}
}

func TestLostReportsAnIngredientThatWentMissing(t *testing.T) {
	t.Parallel()

	before := testfixture.Document()
	after := testfixture.Document()

	ingredients := after.IngredientSections[0].Ingredients
	if len(ingredients) < 2 {
		t.Fatalf("the fixture no longer has two ingredients to thin out")
	}

	after.IngredientSections[0].Ingredients = ingredients[1:]
	if len(semanticdiff.Lost(before, after)) == 0 {
		t.Fatal("a removed ingredient was not reported as a loss")
	}
}

func TestLostReportsAReorderingEvenThoughNothingWentMissing(t *testing.T) {
	t.Parallel()

	before := testfixture.Document()
	after := testfixture.Document()
	ingredients := after.IngredientSections[0].Ingredients
	ingredients[0], ingredients[1] = ingredients[1], ingredients[0]

	lost := semanticdiff.Lost(before, after)
	if len(lost) != 1 || !strings.HasSuffix(lost[0].Path, ".ingredients.order") {
		t.Fatalf("a reordering was not reported as a loss of authored order:\n%s", semanticdiff.Report(lost))
	}
}

func TestLostReportsAListThatGotShorter(t *testing.T) {
	t.Parallel()

	before := testfixture.Document()
	after := testfixture.Document()

	after.Notes = nil
	if len(semanticdiff.Lost(before, after)) == 0 {
		t.Fatal("discarded notes were not reported as a loss")
	}
}
