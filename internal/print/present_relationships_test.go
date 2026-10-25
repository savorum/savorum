package print

import (
	"math/big"
	"slices"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func TestPresentStepsCarriesPhaseRelationships(t *testing.T) {
	document := recipe.Document{
		IngredientSections: []recipe.IngredientSection{{
			Ingredients: []recipe.Ingredient{{ID: "flour", Name: "flour"}, {ID: "water", Name: "water"}},
		}},
		InstructionSections: []recipe.InstructionSection{{
			Steps: []recipe.Step{{ID: "mix", Text: "Mix the dough.", IngredientUses: []recipe.IngredientUse{{IngredientRef: "flour"}, {IngredientRef: "water"}}, TimingRefs: []string{"rest"}}},
		}},
		Times: &recipe.Times{Assertions: []recipe.TimingAssertion{{
			ID: "rest", Kind: recipe.TimingRest, Duration: recipe.DurationExpression{Value: "PT30M"},
		}}},
	}

	steps := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(1, 1)).presentSteps(document)
	if got, want := steps[0].Steps[0].Phases, []string{"Rest 30 min"}; !sameStrings(got, want) {
		t.Errorf("phases = %q, want %q", got, want)
	}
}

func TestPresentStepsShowsDistinctTitlesAndSuppressesRedundantOnes(t *testing.T) {
	t.Parallel()

	document := recipe.Document{InstructionSections: []recipe.InstructionSection{{Steps: []recipe.Step{
		{ID: "one", Title: "Make the roux", Text: "Melt the butter and stir in flour."},
		{ID: "two", Title: "Bake", Text: "Bake until golden."},
	}}}}

	steps := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(1, 1)).presentSteps(document)[0].Steps
	if steps[0].Title != "Make the roux" || steps[1].Title != "" {
		t.Fatalf("presented titles = %q, %q", steps[0].Title, steps[1].Title)
	}
}

func TestPrintKeepsCourseAndCuisineInTheQuietMetadataLine(t *testing.T) {
	t.Parallel()

	document := recipe.Document{
		Categories:     []string{"Cake"},
		Courses:        []string{"Dessert", "Afternoon tea"},
		Cuisines:       []string{"British"},
		CookingMethods: []string{"Baking"},
		Keywords:       []string{"layered"},
	}
	got := presentClassifications(document)

	want := []string{"Course Dessert, Afternoon tea", "Cuisine British"}
	if !sameStrings(got, want) {
		t.Fatalf("print classifications = %q, want %q", got, want)
	}
}

func TestIngredientUseQuantityUsesTheOrdinaryScalingRules(t *testing.T) {
	t.Parallel()

	value := recipe.Rational("3/2")
	use := recipe.IngredientUse{
		IngredientRef: "sugar",
		Quantity: &recipe.Quantity{
			Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: &value}, Unit: "volume.cup.us.customary",
		},
	}

	shown := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(2, 1)).presentQuantity(*use.Quantity)
	if shown.Text != "3 cups" {
		t.Fatalf("scaled ingredient use = %q", shown.Text)
	}
}

func TestPresentStepsSuppressesTextLinkedTimingButPrintsSupplementalTiming(t *testing.T) {
	document := recipe.Document{
		InstructionSections: []recipe.InstructionSection{{Steps: []recipe.Step{{ID: "knead", Text: "Knead for 5 minutes, then cover the bowl.", TimingRefs: []string{"t1", "t2"}}}}},
		Times: &recipe.Times{Assertions: []recipe.TimingAssertion{
			{ID: "t1", Duration: recipe.DurationExpression{Value: "PT5M", SourceText: "for 5 minutes"}, Provenance: recipe.TimingDerived},
			{ID: "t2", Kind: recipe.TimingRest, Duration: recipe.DurationExpression{Value: "PT30M"}, Provenance: recipe.TimingSupplemental},
		}},
	}
	steps := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(1, 1)).presentSteps(document)

	printed := steps[0].Steps[0]
	if got, want := printed.Phases, []string{"Rest 30 min"}; !sameStrings(got, want) {
		t.Fatalf("phases = %q, want %q", got, want)
	}
}

func TestPresentIngredientsUsesFriendlyScaledVolumes(t *testing.T) {
	value := recipe.Rational("1/2")
	document := recipe.Document{IngredientSections: []recipe.IngredientSection{{
		Ingredients: []recipe.Ingredient{{
			ID:   "oil",
			Name: "neutral oil",
			Quantity: &recipe.Quantity{
				Kind:   recipe.QuantitySimple,
				Amount: &recipe.Amount{Value: &value},
				Unit:   "volume.teaspoon.us.customary",
			},
		}},
	}}}

	printed := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(15, 8)).presentIngredients(document)

	amount := printed[0].Ingredients[0].Amount
	if amount.Text != "1 tsp" {
		t.Errorf("amount = %q, want %q", amount.Text, "1 tsp")
	}

	if !amount.Approximate {
		t.Error("rounded amount is not marked approximate")
	}
}

func TestPresentEquipmentDoesNotScaleWithRecipeBatch(t *testing.T) {
	t.Parallel()

	document := recipe.Document{Equipment: []recipe.Equipment{{
		Name: "baking sheets", Note: "lined", Quantity: &recipe.Quantity{
			Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: rationalPointer("2")},
		},
	}}}

	printed := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(3, 1)).presentEquipment(document)
	if len(printed) != 1 || printed[0].Amount.Text != "2" || printed[0].Text != "baking sheets, lined" {
		t.Fatalf("printed equipment = %#v", printed)
	}
}

func TestPresentIngredientsPrefersEditoriallySimpleScaledVolumes(t *testing.T) {
	value := recipe.Rational("1/2")
	document := recipe.Document{IngredientSections: []recipe.IngredientSection{{
		Ingredients: []recipe.Ingredient{{
			ID:   "oil",
			Name: "neutral oil",
			Quantity: &recipe.Quantity{
				Kind:   recipe.QuantitySimple,
				Amount: &recipe.Amount{Value: &value},
				Unit:   "volume.teaspoon.us.customary",
			},
		}},
	}}}

	printed := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(19, 1)).presentIngredients(document)

	amount := printed[0].Ingredients[0].Amount
	if amount.Text != "3 tbsp + ½ tsp" {
		t.Errorf("amount = %q, want %q", amount.Text, "3 tbsp + ½ tsp")
	}

	if amount.Approximate {
		t.Error("exact measurement plan is marked approximate")
	}
}

func TestCompoundAlternativesScaleAndChooseTheRequestedSystem(t *testing.T) {
	value := func(text string) *recipe.Rational {
		parsed := recipe.Rational(text)
		return &parsed
	}
	document := recipe.Document{IngredientSections: []recipe.IngredientSection{{
		Ingredients: []recipe.Ingredient{{
			ID: "oil", Name: "oil",
			Quantity: &recipe.Quantity{Kind: recipe.QuantityAlternatives, Options: []recipe.AlternativeOption{
				{Kind: recipe.QuantitySum, Terms: []recipe.SimpleQuantity{
					{Kind: recipe.QuantitySimple, Amount: recipe.Amount{Value: value("2")}, Unit: "volume.tablespoon.us.customary"},
					{Kind: recipe.QuantitySimple, Amount: recipe.Amount{Value: value("2")}, Unit: "volume.teaspoon.us.customary"},
				}},
				{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: value("40")}, Unit: "volume.milliliter"},
			}},
		}},
	}}}

	asWritten := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(2, 1)).presentIngredients(document)[0].Ingredients[0].Amount.Text
	if !strings.Contains(asWritten, " or ") || !strings.Contains(asWritten, "80ml") {
		t.Fatalf("scaled as-written alternative = %q", asWritten)
	}

	metric := newPresenter(units.DefaultCatalog(), units.MeasureMetric, big.NewRat(1, 1)).presentIngredients(document)[0].Ingredients[0].Amount.Text
	if metric != "40ml" {
		t.Errorf("metric alternative = %q, want 40ml", metric)
	}

	us := newPresenter(units.DefaultCatalog(), units.MeasureUS, big.NewRat(1, 1)).presentIngredients(document)[0].Ingredients[0].Amount.Text
	if us != "2 tbsp plus 2 tsp" {
		t.Errorf("US alternative = %q, want authored compound option", us)
	}
}

func sameStrings(got, want []string) bool {
	return slices.Equal(got, want)
}

func rationalPointer(value string) *recipe.Rational {
	parsed := recipe.Rational(value)
	return &parsed
}

func TestAuthoredIdentityChoicesScaleEveryBranch(t *testing.T) {
	choice := recipe.Ingredient{ID: "fat", Relation: "preferred", Optional: true, Note: "If butter is unavailable", Alternatives: []recipe.Ingredient{
		{ID: "butter", Name: "butter", Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: printChoiceValue("1")}, Unit: "volume.cup.us.customary"}},
		{ID: "oil", Name: "oil", Note: "neutral", Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: printChoiceValue("3/4")}, Unit: "volume.cup.us.customary"}},
	}}

	printed := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(2, 1)).presentIngredient(choice)
	for _, expected := range []string{"2 cups butter", "1½ cups oil", "neutral", "If butter is unavailable", "optional"} {
		if !strings.Contains(printed.Text, expected) {
			t.Fatalf("print %q lacks %q", printed.Text, expected)
		}
	}

	if printed.Choice == nil || len(printed.Choice.Branches) != 2 || !printed.Choice.Preferred || printed.Choice.Branches[0].Amount.Text != "2 cups" || printed.Choice.Branches[1].Amount.Text != "1½ cups" {
		t.Fatalf("branch layout = %#v", printed.Choice)
	}

	if *choice.Alternatives[1].Quantity.Amount.Value != "3/4" {
		t.Fatal("scaling mutated the authored amount")
	}

	choice.Note = ""

	printed = newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(1, 1)).presentIngredient(choice)
	if printed.Choice.Note != "(optional)" {
		t.Fatalf("standalone choice note = %q", printed.Choice.Note)
	}
}

func printChoiceValue(value string) *recipe.Rational {
	parsed := recipe.Rational(value)
	return &parsed
}

func TestSharedAlternativeAmountOccupiesAmountLane(t *testing.T) {
	for _, test := range []struct{ relation, amount, text string }{
		{"or", "2", "lemon or lime"},
		{"and_or", "2", "lemon and/or lime"},
		{"preferred", "2", "lemon or lime"},
	} {
		t.Run(test.relation, func(t *testing.T) {
			choice := recipe.Ingredient{Relation: test.relation, Alternatives: []recipe.Ingredient{
				{Name: "lemon", Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: rationalPointer("1")}, Unit: "count.item"}},
				{Name: "lime", Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: rationalPointer("1")}, Unit: "count.item"}},
			}}

			got := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(2, 1)).presentIngredient(choice)
			if got.Choice == nil || !got.Choice.SharedAmount {
				t.Fatalf("shared amount layout lost: %#v", got.Choice)
			}

			if got.Amount.Text != test.amount || got.Text != test.text {
				t.Fatalf("got amount %q and text %q; want %q and %q", got.Amount.Text, got.Text, test.amount, test.text)
			}
		})
	}
}

func TestAlternativeAmountsKeepTheirBranchMeaning(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*recipe.Ingredient)
	}{
		{"unequal", func(i *recipe.Ingredient) { i.Quantity.Amount.Value = rationalPointer("2") }},
		{"unit", func(i *recipe.Ingredient) { i.Quantity.Unit = "mass.gram" }},
		{"approximate", func(i *recipe.Ingredient) { i.Quantity.Amount.Approximate = true }},
		{"range", func(i *recipe.Ingredient) {
			i.Quantity.Amount = &recipe.Amount{Min: rationalPointer("1"), Max: rationalPointer("2")}
		}},
		{"opaque", func(i *recipe.Ingredient) {
			i.Quantity = &recipe.Quantity{Kind: recipe.QuantityOpaque, Text: "a little"}
		}},
		{"package", func(i *recipe.Ingredient) {
			i.PackageSize = &recipe.SimpleQuantity{Kind: recipe.QuantitySimple, Amount: recipe.Amount{Value: rationalPointer("400")}, Unit: "mass.gram"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			choice := recipe.Ingredient{Relation: "or", Alternatives: []recipe.Ingredient{
				{Name: "lemon", Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: rationalPointer("1")}, Unit: "count.item"}},
				{Name: "lime", Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: rationalPointer("1")}, Unit: "count.item"}},
			}}
			test.change(&choice.Alternatives[1])

			p := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(1, 1))
			got := p.presentIngredient(choice)

			if got.Choice == nil || len(got.Choice.Branches) != 2 || got.Choice.Joiner != "or" {
				t.Fatalf("separate branch layout lost: %#v", got.Choice)
			}

			second := p.presentIngredient(choice.Alternatives[1])
			if !got.Amount.Empty() || !strings.Contains(got.Text, second.Amount.Text+" "+second.Text) || !strings.Contains(got.Text, "1 lemon") {
				t.Fatalf("branch meaning lost: %#v", got)
			}
		})
	}
}

func TestScaledYieldStatesWhatTheBatchMakes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		text  string
		scale *big.Rat
		want  string
	}{
		{name: "a counted yield is recalculated", text: "24 rolls", scale: big.NewRat(2, 1), want: "48 rolls"},
		{name: "a range keeps both ends", text: "4 - 5 people", scale: big.NewRat(2, 1), want: "8 - 10 people"},
		{name: "a written range keeps its wording", text: "4 to 5 people", scale: big.NewRat(3, 1), want: "12 to 15 people"},
		{name: "halving reads as a cook writes it", text: "makes 3 loaves", scale: big.NewRat(1, 2), want: "makes 1½ loaves"},
		// Two numbers that are not a range could be a tin size and a count, and
		// multiplying the wrong one is worse than not multiplying at all.
		{name: "unrelated numbers keep the batch factor", text: "two 9 inch tins, 16 slices", scale: big.NewRat(2, 1), want: "two 9 inch tins, 16 slices × 2"},
		// The 9 describes the tin rather than counting what comes out of it.
		{name: "a number describing the vessel keeps the batch factor", text: "Makes one 9-inch tart", scale: big.NewRat(2, 1), want: "Makes one 9-inch tart × 2"},
		{name: "wording with no number keeps the batch factor", text: "a big bowl", scale: big.NewRat(2, 1), want: "a big bowl × 2"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			document := recipe.Document{Yields: []recipe.Yield{{Text: testCase.text}}}
			printed := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, testCase.scale).presentYield(document.FirstYield())

			if printed == nil || printed.Text != testCase.want {
				t.Fatalf("yield = %#v, want %q", printed, testCase.want)
			}
		})
	}
}

func TestPrintedDurationsCarryMinutesIntoHours(t *testing.T) {
	for value, want := range map[string]string{
		"PT375M": "6 hr 15 min",
		"PT90S":  "1 min 30 sec",
		"PT48H":  "48 hr",
		"P1DT2H": "1 day 2 hr",
	} {
		if got := durationWords(value); got != want {
			t.Errorf("durationWords(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestIngredientNoteInBracketsTakesNoComma(t *testing.T) {
	document := recipe.Document{IngredientSections: []recipe.IngredientSection{{
		Ingredients: []recipe.Ingredient{
			{ID: "legs", Name: "chicken drumsticks", Note: "(Note 1)"},
			{ID: "onion", Name: "onion", Note: "chopped"},
		},
	}}}

	printed := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(1, 1)).presentIngredients(document)
	if got, want := printed[0].Ingredients[0].Text, "chicken drumsticks (Note 1)"; got != want {
		t.Errorf("bracketed note = %q, want %q", got, want)
	}

	if got, want := printed[0].Ingredients[1].Text, "onion, chopped"; got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
}

func TestPresentStepsDoesNotRestateARangeTheProseGivesInFigures(t *testing.T) {
	t.Parallel()

	phase := recipe.TimingAssertion{
		ID: "cook", Kind: recipe.TimingCook, Provenance: recipe.TimingSupplemental,
		Duration: recipe.DurationExpression{Min: "PT15M", Max: "PT20M"},
	}
	for text, want := range map[string]bool{
		"يُطهى الخليط لمدة 15–20 دقيقة.": false,
		"يُطهى الخليط لمدة ١٥–٢٠ دقيقة.": false,
		"Cook until thick.":      true,
		"Cook about 20 minutes.": true,
	} {
		step := recipe.Step{ID: "s", Text: text, TimingRefs: []string{"cook"}}
		if got := phaseAddsInformation(step, phase); got != want {
			t.Errorf("%q: adds information %v, want %v", text, got, want)
		}
	}
}
