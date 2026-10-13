package ingredientparser_test

import (
	"reflect"
	"testing"

	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func TestParsePublishesAndAppliesControlledUnitAliasAmbiguity(t *testing.T) {
	t.Parallel()

	result := ingredientparser.ParseDetailed("1 ½ Tbsp olive oil", "i1", "en")
	if result.Ingredient.Quantity != nil || len(result.Ambiguities) != 1 {
		t.Fatalf("result = %#v", result)
	}

	ambiguity := result.Ambiguities[0]
	if ambiguity.Kind != ingredientparser.AmbiguityUnitAlias || ambiguity.AuthoredForm != "Tbsp" ||
		ambiguity.SourceStart != 5 || ambiguity.SourceEnd != 9 ||
		!reflect.DeepEqual(ambiguity.CandidateUnitIDs, []string{"volume.tablespoon.au", "volume.tablespoon.us.customary"}) {
		t.Fatalf("ambiguity = %#v", ambiguity)
	}

	resolved := ingredientparser.ParseWithContext("1 ½ Tbsp olive oil", "i1", "en",
		interpreting(ambiguity, "volume.tablespoon.au"))
	if resolved.Ingredient.Quantity == nil || resolved.Ingredient.Quantity.Unit != "volume.tablespoon.au" ||
		resolved.Ingredient.SourceText != "1 ½ Tbsp olive oil" {
		t.Fatalf("resolved ingredient = %#v", resolved.Ingredient)
	}

	normalization := resolved.Ingredient.Normalization
	if normalization == nil || normalization.Method != "inferred" || normalization.Review != "corrected" || len(normalization.Warnings) != 1 {
		t.Fatalf("normalization = %#v", normalization)
	}
}

func TestParseResolutionRejectsAUnitOutsideTheAliasCandidates(t *testing.T) {
	t.Parallel()

	result := ingredientparser.ParseDetailed("1 Tbsp oil", "i1", "en")

	resolved := ingredientparser.ParseWithContext("1 Tbsp oil", "i1", "en",
		interpreting(result.Ambiguities[0], "mass.gram"))
	if resolved.Ingredient.Quantity != nil || len(resolved.Ambiguities) != 1 {
		t.Fatalf("arbitrary resolution was accepted: %#v", resolved)
	}
}

func TestParseCommonImportedIngredientSpecifications(t *testing.T) {
	tests := []struct {
		line, value, min, max, unit, name, note string
		approximate                             bool
	}{
		{"1 ½ cups flour", "3/2", "", "", "volume.cup.us.customary", "flour", "", false},
		{"¼ – 1/2 tsp red pepper flakes", "", "1/4", "1/2", "volume.teaspoon.us.customary", "red pepper flakes", "", false},
		{"about 50 g butter, softened", "50", "", "", "mass.gram", "butter", "softened", true},
		{"2 cloves garlic, minced", "2", "", "", "count.clove", "garlic", "minced", false},
		{"3 large eggs", "3", "", "", "count.item", "large eggs", "", false},
		{"2 cans tomatoes", "2", "", "", "container.can", "tomatoes", "", false},
		// CookBook Manager writes a leading 1 from an empty quantity column.
		{"1 170-180 g light brown sugar", "", "170", "180", "mass.gram", "light brown sugar", "", false},
		{"1 110-115 g white sugar", "", "110", "115", "mass.gram", "white sugar", "", false},
		{"1 500 g strong white flour", "500", "", "", "mass.gram", "strong white flour", "", false},
	}
	for _, test := range tests {
		t.Run(test.line, func(t *testing.T) {
			got := ingredientparser.Parse(test.line, "i1", "en-US")
			if got.Quantity == nil || got.Quantity.Amount == nil {
				t.Fatalf("quantity = %#v", got)
			}

			amount := got.Quantity.Amount

			value, lower, upper := "", "", ""
			if amount.Value != nil {
				value = amount.Value.String()
			}

			if amount.Min != nil {
				lower = amount.Min.String()
			}

			if amount.Max != nil {
				upper = amount.Max.String()
			}

			if value != test.value || lower != test.min || upper != test.max || got.Quantity.Unit != test.unit || got.Name != test.name || got.Note != test.note || amount.Approximate != test.approximate {
				t.Fatalf("parsed = %#v", got)
			}
		})
	}
}

func TestParseLeavesUncertainProseAlone(t *testing.T) {
	got := ingredientparser.Parse("Salt to taste", "i1", "en-US")
	if got.Quantity != nil || got.Name != "Salt to taste" || got.SourceText != "Salt to taste" {
		t.Fatalf("parsed = %#v", got)
	}
}

func TestParseMarksAnUnresolvedAmountForReview(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		"2 cups all-purpose flour",
		"2.5 cups/320 grams all-purpose flour",
		"1/2 3/4 cup hot sauce",
	} {
		got := ingredientparser.Parse(line, "i1", "en")
		if got.Quantity != nil || got.Name != line || got.SourceText != line {
			t.Errorf("%q parsed = %#v, want the exact line left unresolved", line, got)
			continue
		}

		if got.Normalization == nil || got.Normalization.Method != "unresolved" || len(got.Normalization.Warnings) == 0 {
			t.Errorf("%q normalization = %#v, want a visible unresolved warning", line, got.Normalization)
		}
	}
}

func TestParseUsesLanguageLevelAliasesWithoutInventingARegion(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		line, value, unit string
	}{
		{line: "67g granulated sugar", value: "67", unit: "mass.gram"},
		{line: "1 1/2 metric teaspoons vanilla", value: "3/2", unit: "volume.teaspoon.metric.culinary"},
		{line: "2 pounds chicken", value: "2", unit: "mass.pound.avoirdupois"},
	} {
		got := ingredientparser.Parse(test.line, "i1", "en")
		if got.Quantity == nil || got.Quantity.Amount == nil || got.Quantity.Amount.Value == nil ||
			got.Quantity.Amount.Value.String() != test.value || got.Quantity.Unit != test.unit {
			t.Errorf("%q parsed = %#v", test.line, got)
		}
	}
}

func TestParseResolvesAliasesFromSourceLocale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, locale, line, unit string
	}{
		{name: "US exact customary cup", locale: "en-US", line: "1 cup flour", unit: "volume.cup.us.customary"},
		{name: "Australian tablespoon", locale: "en-AU", line: "1 tbsp syrup", unit: "volume.tablespoon.au"},
		{name: "British imperial pint", locale: "en-GB", line: "1 pint milk", unit: "volume.pint.imperial"},
		{name: "qualified culinary teaspoon", locale: "en-CA", line: "1 metric teaspoon cumin", unit: "volume.teaspoon.metric.culinary"},
		{name: "extension fallback", locale: "en-US-u-nu-latn", line: "1 cup flour", unit: "volume.cup.us.customary"},
		{name: "explicit qualifier outranks locale", locale: "en-US", line: "1 imperial pint milk", unit: "volume.pint.imperial"},
		{name: "explicit legal measure", locale: "en-GB", line: "1 legal tablespoon molasses", unit: "volume.tablespoon.us.legal"},
		{name: "explicit metric cup without locale", line: "1 metric cup milk", unit: "volume.cup.metric.culinary"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := ingredientparser.Parse(test.line, "i1", test.locale)
			if got.Quantity == nil || got.Quantity.Unit != test.unit {
				t.Fatalf("parsed = %#v, want %s", got, test.unit)
			}
		})
	}
}

func TestParseLeavesAliasesOpaqueWithoutDefensibleLocaleEvidence(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line string }{
		{locale: "en", line: "1 cup flour"},
		{locale: "en-CA", line: "1 cup flour"},
		{locale: "", line: "1 tbsp oil"},
	} {
		got := ingredientparser.Parse(test.line, "i1", test.locale)
		if got.Quantity != nil || got.SourceText != test.line {
			t.Errorf("%s under %q parsed = %#v, want opaque source text", test.line, test.locale, got)
		}
	}
}

func TestParseHonorsCaseSensitiveSpoonSymbols(t *testing.T) {
	t.Parallel()

	tablespoon := ingredientparser.Parse("1 T molasses", "i1", "en-US")
	teaspoon := ingredientparser.Parse("1 t salt", "i2", "en-US")

	if tablespoon.Quantity == nil || tablespoon.Quantity.Unit != "volume.tablespoon.us.customary" {
		t.Fatalf("tablespoon = %#v", tablespoon)
	}

	if teaspoon.Quantity == nil || teaspoon.Quantity.Unit != "volume.teaspoon.us.customary" {
		t.Fatalf("teaspoon = %#v", teaspoon)
	}
}

func TestParseMetricAndImperialAlternatives(t *testing.T) {
	for _, line := range []string{
		"40 – 50 g / 1.5 – 1.75 oz dried egg noodles",
		"2 pounds 1 kg boneless chicken breasts",
	} {
		got := ingredientparser.Parse(line, "i1", "en-US")
		if got.Quantity == nil || got.Quantity.Kind != "alternatives" || len(got.Quantity.Options) != 2 {
			t.Fatalf("%q parsed = %#v", line, got)
		}
	}
}

func TestParseCompoundQuantityAlternatives(t *testing.T) {
	for _, line := range []string{
		"2 tbsp + 2 tsp or 40 mL oil",
		"2 tbsp plus 2 tsp / 40 mL oil",
	} {
		got := ingredientparser.Parse(line, "i1", "en-US")
		if got.Name != "oil" || got.Quantity == nil || got.Quantity.Kind != "alternatives" || len(got.Quantity.Options) != 2 {
			t.Fatalf("%q parsed = %#v", line, got)
		}

		compound := got.Quantity.Options[0]
		if compound.Kind != "sum" || len(compound.Terms) != 2 || compound.Terms[0].Unit != "volume.tablespoon.us.customary" || compound.Terms[1].Unit != "volume.teaspoon.us.customary" {
			t.Fatalf("%q compound option = %#v", line, compound)
		}

		simple := got.Quantity.Options[1]
		if simple.Kind != "simple" || simple.Amount == nil || simple.Amount.Value == nil || *simple.Amount.Value != "40" || simple.Unit != "volume.milliliter" {
			t.Fatalf("%q simple option = %#v", line, simple)
		}
	}
}

func TestParseReadsACountOfPackagesAsAPackageSize(t *testing.T) {
	t.Parallel()
	// "2 400 g cans" is two of something a shop sells, not two of a measure.
	// The count and the container are the quantity; the measurement is how big
	// each package is.
	got := ingredientparser.Parse("2 400 g cans chopped tomatoes", "i1", "en-US")
	if got.Name != "chopped tomatoes" {
		t.Errorf("name = %q, want the ingredient without its packaging", got.Name)
	}

	if got.Quantity == nil || got.Quantity.Unit != "container.can" {
		t.Fatalf("quantity = %#v, want a count of cans", got.Quantity)
	}

	if got.Quantity.Amount == nil || got.Quantity.Amount.Value == nil || *got.Quantity.Amount.Value != "2" {
		t.Errorf("count = %#v, want 2", got.Quantity.Amount)
	}

	if got.PackageSize == nil || got.PackageSize.Unit != "mass.gram" {
		t.Fatalf("package size = %#v, want 400 g", got.PackageSize)
	}

	if got.PackageSize.Amount.Value == nil || *got.PackageSize.Amount.Value != "400" {
		t.Errorf("package size = %#v, want 400 g", got.PackageSize.Amount)
	}
}

func TestParseReadsAParenthesizedPackageSize(t *testing.T) {
	t.Parallel()

	got := ingredientparser.Parse("2 (14.5 oz) cans diced tomatoes, drained", "i1", "en")
	if got.Name != "diced tomatoes" || got.Note != "drained" {
		t.Errorf("ingredient = %#v", got)
	}

	if got.Quantity == nil || got.Quantity.Unit != "container.can" || got.Quantity.Amount == nil ||
		got.Quantity.Amount.Value == nil || *got.Quantity.Amount.Value != "2" {
		t.Fatalf("quantity = %#v, want 2 cans", got.Quantity)
	}

	if got.PackageSize == nil || got.PackageSize.Unit != "mass.ounce.avoirdupois" ||
		got.PackageSize.Amount.Value == nil || *got.PackageSize.Amount.Value != "29/2" {
		t.Fatalf("package size = %#v, want 14.5 oz", got.PackageSize)
	}
}

// A count followed by a measurement and a name is items of a stated size, as
// "3 (400 g) chocolate" is; the measurement must never stay in the name.
func TestParseReadsACountedMeasureAsItemsOfAStatedSize(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		line, name, count, size, unit string
	}{
		{"3 400 g chocolate", "chocolate", "3", "400", "mass.gram"},
		{"2 8 oz chicken breasts", "chicken breasts", "2", "8", "mass.ounce.avoirdupois"},
		{"2 8-oz chicken breasts", "chicken breasts", "2", "8", "mass.ounce.avoirdupois"},
		{"4 6 oz salmon fillets", "salmon fillets", "4", "6", "mass.ounce.avoirdupois"},
	} {
		got := ingredientparser.Parse(test.line, "i1", "en-US")
		if got.Name != test.name {
			t.Errorf("%q name = %q, want %q", test.line, got.Name, test.name)
		}

		if got.SourceText != test.line {
			t.Errorf("%q source text = %q, want the line preserved exactly", test.line, got.SourceText)
		}

		if got.Quantity == nil || got.Quantity.Unit != "count.item" ||
			got.Quantity.Amount == nil || got.Quantity.Amount.Value == nil ||
			string(*got.Quantity.Amount.Value) != test.count {
			t.Fatalf("%q quantity = %#v, want %s items", test.line, got.Quantity, test.count)
		}

		if got.PackageSize == nil || got.PackageSize.Unit != test.unit ||
			got.PackageSize.Amount.Value == nil ||
			string(*got.PackageSize.Amount.Value) != test.size {
			t.Errorf("%q package size = %#v, want %s %s", test.line, got.PackageSize, test.size, test.unit)
		}
	}
}

// The count of a thing is a whole number. A fractional leading amount in front
// of a measure is a mangled line, not half an item, and stays as written.
func TestParseLeavesAFractionalCountedMeasureAlone(t *testing.T) {
	t.Parallel()

	got := ingredientparser.Parse("1/2 3/4 cup hot sauce", "i1", "en-US")
	if got.Quantity != nil {
		t.Errorf("quantity = %#v, want the line left unparsed", got.Quantity)
	}

	if got.SourceText != "1/2 3/4 cup hot sauce" {
		t.Errorf("source text = %q, want the line preserved exactly", got.SourceText)
	}
}

// The word joining a measure to what it measures belongs to neither.
func TestParseDoesNotLeaveAConnectiveInTheIngredientName(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ line, name string }{
		{"8 oz of chicken", "chicken"},
		{"2 cups of all-purpose flour", "all-purpose flour"},
		{"1 can of tomatoes", "tomatoes"},
		{"2 8 oz of chicken breasts", "chicken breasts"},
		{"3 sprigs of thyme", "thyme"},
		// A word that merely starts with those letters is not the connective,
		// and a line with no measure in front of it is not touched at all.
		{"1 cup offal", "offal"},
		{"a pinch of salt", "a pinch of salt"},
	} {
		if got := ingredientparser.Parse(test.line, "i1", "en-US"); got.Name != test.name {
			t.Errorf("%q name = %q, want %q", test.line, got.Name, test.name)
		}
	}
}

// Trimming the name does not reach into a note, which answers a different
// question and keeps the words the source wrote.
func TestParseKeepsAConnectiveInsideANote(t *testing.T) {
	t.Parallel()

	got := ingredientparser.Parse("2 tbsp of olive oil, of good quality", "i1", "en-US")
	if got.Name != "olive oil" || got.Note != "of good quality" {
		t.Errorf("parsed = %#v, want the note left as written", got)
	}
}

// A hyphen joining an amount to a unit is the compound-adjective spelling of
// the same measure. A hyphen between two numbers is still a range.
func TestParseReadsAHyphenatedMeasureAndKeepsRangesIntact(t *testing.T) {
	t.Parallel()

	got := ingredientparser.Parse("1 14-ounce can coconut milk", "i1", "en-US")
	if got.Name != "coconut milk" || got.Quantity == nil || got.Quantity.Unit != "container.can" {
		t.Fatalf("parsed = %#v, want one can of coconut milk", got)
	}

	if got.PackageSize == nil || got.PackageSize.Unit != "mass.ounce.avoirdupois" {
		t.Errorf("package size = %#v, want 14 oz", got.PackageSize)
	}

	ginger := ingredientparser.Parse("3-inch piece of ginger", "i1", "en-US")
	if ginger.Name != "piece of ginger" || ginger.Quantity == nil ||
		ginger.Quantity.Unit != "length.inch.international" {
		t.Errorf("parsed = %#v, want a 3 inch piece of ginger", ginger)
	}

	stock := ingredientparser.Parse("2-3 cups stock", "i1", "en-US")
	if stock.Quantity == nil || stock.Quantity.Amount == nil ||
		stock.Quantity.Amount.Min == nil || stock.Quantity.Amount.Max == nil {
		t.Errorf("parsed = %#v, want a 2 to 3 cup range", stock)
	}
}

// Authored optionality is a typed member, not prose, and neighbouring phrases
// such as "to taste" must not be swept in.
func TestAuthoredOptionalityIsLiftedOutOfTheLineWithoutLosingIt(t *testing.T) {
	for _, testCase := range []struct {
		line     string
		optional bool
		name     string
	}{
		{"60 g bell pepper, diced, optional", true, "bell pepper"},
		{"½ teaspoon baking powder, optional", true, "baking powder"},
		{"1 tablespoon chopped parsley (optional)", true, "chopped parsley"},
		{"Optional: 2 tablespoons cream", true, "cream"},
		{"2 tablespoons cream - optional", true, "cream"},
		// Not optionality. Each of these says something else, and reading any
		// of them as a sanctioned omission asserts what the source did not.
		{"1 teaspoon salt, or to taste", false, "salt"},
		{"lemon wedges, for serving", false, "lemon wedges"},
		{"2 tablespoons parsley, preferably fresh", false, "parsley"},
		{"1 onion, optionally sliced thin", false, "onion"},
	} {
		parsed := ingredientparser.Parse(testCase.line, "i1", "en-US")
		if parsed.Optional != testCase.optional {
			t.Errorf("%q optional = %v, want %v", testCase.line, parsed.Optional, testCase.optional)
		}

		if parsed.SourceText != testCase.line {
			t.Errorf("%q lost its source text: %q", testCase.line, parsed.SourceText)
		}

		if testCase.optional && parsed.Name != testCase.name {
			t.Errorf("%q name = %q, want %q", testCase.line, parsed.Name, testCase.name)
		}
	}
}

// A line that says nothing but "optional" has no ingredient to attach it to.
func TestABareOptionalMarkerIsNotAnOptionalIngredient(t *testing.T) {
	parsed := ingredientparser.Parse("optional", "i1", "en-US")
	if parsed.Optional || parsed.Name != "optional" {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestNoteSplitsAtACommaOutsideBrackets(t *testing.T) {
	// A comma inside a bracket belongs to the aside it is part of. Cutting
	// there leaves an ingredient named after half a parenthesis.
	result := ingredientparser.ParseDetailed(
		"1 1/2 tsp cooking salt / kosher salt (half for table salt, 50% extra for flakes)", "i1", "en-US")

	if result.Ingredient.Name != "cooking salt / kosher salt (half for table salt, 50% extra for flakes)" {
		t.Fatalf("name = %q", result.Ingredient.Name)
	}

	if result.Ingredient.Note != "" {
		t.Fatalf("note = %q", result.Ingredient.Note)
	}

	separated := ingredientparser.ParseDetailed("2 apples (peeled), thinly sliced", "i2", "en-US")
	if separated.Ingredient.Name != "apples (peeled)" || separated.Ingredient.Note != "thinly sliced" {
		t.Fatalf("ingredient = %#v", separated.Ingredient)
	}
}

// interpreting addresses one measure, which is what an occurrence
// interpretation is: it names the occurrence, never the alias.
func interpreting(ambiguity ingredientparser.Ambiguity, unitID string) ingredientparser.ResolutionContext {
	return ingredientparser.ResolutionContext{
		Interpretations: map[string]string{ambiguity.OccurrenceKey: unitID},
	}
}

func usingConvention(convention units.Convention) ingredientparser.ResolutionContext {
	profile, found := units.DefaultCatalog().ConventionProfileFor(convention)
	if !found {
		panic("no profile for " + string(convention))
	}

	return ingredientparser.ResolutionContext{Convention: &profile}
}

func TestEveryMeasureOfACompoundAmountIsOfferedForReview(t *testing.T) {
	t.Parallel()

	// Every measure of a compound amount is reported, not just the first.
	result := ingredientparser.ParseDetailed("1 tablespoon plus 2 teaspoons neutral oil", "i1", "en")
	if len(result.Ambiguities) != 2 {
		t.Fatalf("ambiguities = %#v", result.Ambiguities)
	}

	if result.Ambiguities[0].AuthoredForm != "tablespoon" || result.Ambiguities[1].AuthoredForm != "teaspoons" {
		t.Fatalf("ambiguities are out of source order: %#v", result.Ambiguities)
	}

	if result.Ambiguities[0].SourceStart >= result.Ambiguities[1].SourceStart {
		t.Fatalf("source offsets are out of order: %#v", result.Ambiguities)
	}
}

func TestOneRepeatedAliasIsAddressedTwice(t *testing.T) {
	t.Parallel()

	// An alias string cannot address an occurrence: one ingredient can say
	// `cup` twice, and an answer given for one must not settle the other.
	result := ingredientparser.ParseDetailed("1/2 cup plus 1/4 cup whole milk", "i1", "en")
	if len(result.Ambiguities) != 2 {
		t.Fatalf("ambiguities = %#v", result.Ambiguities)
	}

	first, second := result.Ambiguities[0], result.Ambiguities[1]
	if first.ResolutionKey != second.ResolutionKey {
		t.Fatalf("two spellings of one measure grouped apart: %q and %q", first.ResolutionKey, second.ResolutionKey)
	}

	if first.OccurrenceKey == second.OccurrenceKey || first.SourceStart == second.SourceStart {
		t.Fatalf("two occurrences share an address: %#v", result.Ambiguities)
	}
}

func TestAnIngredientConventionSettlesEveryTermOfACompoundAmount(t *testing.T) {
	t.Parallel()

	// SREF section 9.5: an ingredient convention reaches every applicable
	// measure of the ingredient's quantity expression, including a measure the
	// person was never asked about.
	cases := []struct {
		name  string
		line  string
		units []string
	}{
		{
			name:  "a sum of a cup and a tablespoon",
			line:  "¼ cup plus 1 tablespoon olive oil",
			units: []string{"volume.cup.us.customary", "volume.tablespoon.us.customary"},
		},
		{
			// The teaspoon must follow the convention, not the language-level
			// metric spoon.
			name:  "a sum whose second measure is a teaspoon",
			line:  "1 tablespoon plus 2 teaspoons neutral oil",
			units: []string{"volume.tablespoon.us.customary", "volume.teaspoon.us.customary"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolved := ingredientparser.ParseWithContext(testCase.line, "i1", "en", usingConvention(units.ConventionUS))

			quantity := resolved.Ingredient.Quantity
			if quantity == nil || quantity.Kind != recipe.QuantitySum || len(quantity.Terms) != len(testCase.units) {
				t.Fatalf("quantity = %#v", quantity)
			}

			for index, term := range quantity.Terms {
				if term.Unit != testCase.units[index] {
					t.Fatalf("term %d = %q, want %q", index, term.Unit, testCase.units[index])
				}
			}
		})
	}
}

func TestAnOccurrenceInterpretationReachesOnlyItsOwnMeasure(t *testing.T) {
	t.Parallel()

	// Answering the tablespoon says nothing about the teaspoons. The
	// ingredient stays unresolved and keeps offering the measure nobody has
	// answered, rather than acquiring a convention the person never named.
	first := ingredientparser.ParseDetailed("1 tablespoon plus 2 teaspoons neutral oil", "i1", "en")

	resolved := ingredientparser.ParseWithContext("1 tablespoon plus 2 teaspoons neutral oil", "i1", "en",
		interpreting(first.Ambiguities[0], "volume.tablespoon.us.customary"))

	if resolved.Ingredient.Quantity != nil {
		t.Fatalf("an unanswered measure was resolved anyway: %#v", resolved.Ingredient.Quantity)
	}

	// One question left, not two.
	if len(resolved.Ambiguities) != 1 || resolved.Ambiguities[0].AuthoredForm != "teaspoons" {
		t.Fatalf("the outstanding measures = %#v", resolved.Ambiguities)
	}
}

func TestAnExplicitQualifierSurvivesAnIngredientConvention(t *testing.T) {
	t.Parallel()

	// A decision never overrides meaning the source itself stated.
	resolved := ingredientparser.ParseWithContext(
		"1 tablespoon plus 2 metric teaspoons neutral oil", "i1", "en", usingConvention(units.ConventionUS))

	quantity := resolved.Ingredient.Quantity
	if quantity == nil || len(quantity.Terms) != 2 {
		t.Fatalf("quantity = %#v", quantity)
	}

	if quantity.Terms[0].Unit != "volume.tablespoon.us.customary" ||
		quantity.Terms[1].Unit != "volume.teaspoon.metric.culinary" {
		t.Fatalf("terms = %#v", quantity.Terms)
	}
}

func TestAConventionThatCoversNoFamilyResolvesNothing(t *testing.T) {
	t.Parallel()

	// The British imperial convention answers fluid volumes and no household
	// spoon, so a bare tablespoon stays unresolved instead of borrowing another
	// convention's identity.
	resolved := ingredientparser.ParseWithContext("2 tablespoons oil", "i1", "en", usingConvention(units.ConventionImperial))
	if resolved.Ingredient.Quantity != nil {
		t.Fatalf("a convention borrowed an answer it does not hold: %#v", resolved.Ingredient.Quantity)
	}
}

func TestAMetricConventionResolvesABareTablespoonToTheFifteenMillilitreSpoon(t *testing.T) {
	t.Parallel()

	resolved := ingredientparser.ParseWithContext("2 tablespoons oil", "i1", "en", usingConvention(units.ConventionMetric))

	quantity := resolved.Ingredient.Quantity
	if quantity == nil || quantity.Unit != "volume.tablespoon.metric.culinary" {
		t.Fatalf("quantity = %#v", quantity)
	}

	// The Australian spoon is reached only by asking for it.
	australian := ingredientparser.ParseWithContext("2 tablespoons oil", "i1", "en", usingConvention(units.ConventionAU))
	if australian.Ingredient.Quantity == nil || australian.Ingredient.Quantity.Unit != "volume.tablespoon.au" {
		t.Fatalf("Australian quantity = %#v", australian.Ingredient.Quantity)
	}
}

func TestAnAnswerDoesNotReachAnIngredientWithNothingToAnswer(t *testing.T) {
	t.Parallel()

	// The measure is already unambiguous, so the decision changes nothing
	// about it: a convention settles what the source leaves open, and never
	// re-decides what the source already said.
	parsed := ingredientparser.ParseWithContext("2 grams salt", "i1", "en", usingConvention(units.ConventionUS))
	if parsed.Ingredient.Quantity == nil || parsed.Ingredient.Quantity.Unit != "mass.gram" {
		t.Fatalf("quantity = %#v", parsed.Ingredient.Quantity)
	}
}

// A list marker is not an optional marker.
func TestABulletedIngredientIsNotOptional(t *testing.T) {
	for _, line := range []string{"- 2 carrots", "• 2 carrots", "– 2 carrots"} {
		parsed := ingredientparser.Parse(line, "i1", "en-US")
		if parsed.Optional || parsed.Name != "carrots" || parsed.Quantity == nil || parsed.SourceText != line {
			t.Errorf("%q parsed = %#v", line, parsed)
		}
	}
}

// Without unit aliases in the reading language, a bare number is not read as a
// count, because the words after it may be a measure. Swahili has none.
func TestABareNumberIsNotACountWithoutAliasesInTheReadingLanguage(t *testing.T) {
	for _, line := range []string{"2 vijiko vya mafuta", "2 mayai"} {
		parsed := ingredientparser.Parse(line, "i1", "sw")
		if parsed.Quantity != nil || parsed.Name != line ||
			parsed.Normalization == nil || parsed.Normalization.Method != "unresolved" {
			t.Errorf("%q parsed = %#v, want it left unresolved", line, parsed)
		}
	}
}

// Digits of another script are read as the numbers they write.
func TestAmountsInOtherScriptsAreReadAsNumbers(t *testing.T) {
	parsed := ingredientparser.Parse("٢٠٠ g طحين", "i1", "ar")
	if parsed.Quantity == nil || parsed.Quantity.Amount.Value.String() != "200" || parsed.Name != "طحين" {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestParseReadsADecimalCommaButNotAThousandsSeparator(t *testing.T) {
	parsed := ingredientparser.Parse("1,5 kg flour", "i1", "en")
	if parsed.Quantity == nil || parsed.Quantity.Amount.Value.String() != "3/2" ||
		parsed.Quantity.Unit != "mass.kilogram" || parsed.Name != "flour" {
		t.Fatalf("parsed = %#v", parsed)
	}

	thousands := ingredientparser.Parse("1,500 g flour", "i1", "en")
	if thousands.Quantity != nil || thousands.Normalization == nil || thousands.Normalization.Method != "unresolved" {
		t.Fatalf("parsed = %#v, want it left unresolved", thousands)
	}
}

// A qualified English name identifies its unit from any English locale, and a
// convention chosen for the ingredient does not override it.
func TestQualifiedEnglishSpoonAndCupNamesSurviveLocaleAndConvention(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ line, want string }{
		{"1 Australian cup flour", "volume.cup.au"},
		{"1 Australian tablespoon oil", "volume.tablespoon.au"},
		{"1 Australian teaspoon salt", "volume.teaspoon.au"},
		{"1 metric cup flour", "volume.cup.metric.culinary"},
		{"1 metric tablespoon oil", "volume.tablespoon.metric.culinary"},
		{"1 metric teaspoon salt", "volume.teaspoon.metric.culinary"},
	} {
		for _, locale := range []string{"", "en", "en-US", "en-AU", "en-GB"} {
			for _, context := range []struct {
				name    string
				context ingredientparser.ResolutionContext
			}{
				{"no convention", ingredientparser.ResolutionContext{}},
				{"US", usingConvention(units.ConventionUS)},
				{"Australian", usingConvention(units.ConventionAU)},
				{"Metric", usingConvention(units.ConventionMetric)},
			} {
				resolved := ingredientparser.ParseWithContext(test.line, "i1", locale, context.context)

				quantity := resolved.Ingredient.Quantity
				if quantity == nil || quantity.Unit != test.want {
					t.Errorf("%q under %q with %s = %#v, want %s", test.line, locale, context.name, quantity, test.want)
				}
			}
		}
	}
}
