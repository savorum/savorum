// Package testfixture builds the recipes tests share. Fixtures live here rather
// than beside each suite so that a document exercising a particular shape is
// written once and every test that needs it sees the same one.
package testfixture

import (
	"fmt"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
)

// PrintCorpus is the set of deliberately awkward recipes the print subsystem
// is judged against: very short and very long recipes, an overlong title, a
// step larger than a card, and non-ASCII text.
func PrintCorpus() []recipe.Document {
	return []recipe.Document{
		printShort(),
		printWeeknight(),
		printLong(),
		printManyGroups(),
		printLongInstructions(),
		printLongTitle(),
		printWithoutImage(),
		printWithNotesAndSource(),
		printNonUSMeasures(),
		printUnicode(),
		printRightToLeft(),
		printEmoji(),
	}
}

func printEmoji() recipe.Document {
	document := printShort()
	document.ID = "print-emoji"
	document.Title = "🍞 Toast by 👩🏽‍🍳"
	document.Description = "A recipe with emoji and everyday symbols."
	document.Notes = []string{"☑ Prepare the bread. ☐ Set the table. 🌶️ Add chilli if desired."}

	return document
}

func simple(value, unit string) *recipe.Quantity {
	return &recipe.Quantity{
		Kind:   recipe.QuantitySimple,
		Amount: &recipe.Amount{Value: Rational(value)},
		Unit:   unit,
	}
}

func ingredient(id, name, value, unit string) recipe.Ingredient {
	line := recipe.Ingredient{ID: id, Name: name}
	if value != "" {
		line.Quantity = simple(value, unit)
	}

	return line
}

func steps(id, title string, texts ...string) recipe.InstructionSection {
	section := recipe.InstructionSection{ID: id, Title: title}
	for index, text := range texts {
		section.Steps = append(section.Steps, recipe.Step{
			ID:   fmt.Sprintf("%s-%d", id, index+1),
			Text: text,
		})
	}

	return section
}

func base(id, title string) recipe.Document {
	return recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            recipe.RecipeID(id),
		Title:         title,
		Language:      "en-US",
	}
}

func printShort() recipe.Document {
	document := base("print-short", "Buttered toast")
	document.Yields = []recipe.Yield{{Quantity: simple("2", "count.serving"), Label: "servings"}}
	document.Times = &recipe.Times{Prep: iso("PT2M"), Cook: iso("PT3M")}
	document.IngredientSections = []recipe.IngredientSection{{
		ID: "all",
		Ingredients: []recipe.Ingredient{
			ingredient("bread", "thick-cut bread", "2", "count.item"),
			ingredient("butter", "salted butter", "1", "volume.tablespoon.us.customary"),
		},
	}}
	document.InstructionSections = []recipe.InstructionSection{
		steps("method", "", "Toast the bread until deep golden.", "Butter it while it is still hot."),
	}

	return document
}

func printWeeknight() recipe.Document {
	document := base("print-weeknight", "Chicken tikka masala")
	document.Description = "A weeknight version that leans on a hot pan and a long enough simmer."
	document.Yields = []recipe.Yield{{Quantity: simple("4", "count.serving"), Label: "servings"}}
	document.Times = &recipe.Times{
		Prep: iso("PT25M"), Cook: iso("PT35M"),
		Assertions: []recipe.TimingAssertion{{ID: "timing-1", Kind: recipe.TimingMarinate, Duration: recipe.DurationExpression{Value: "PT2H"}}},
	}
	document.IngredientSections = []recipe.IngredientSection{
		{
			ID: "marinade", Title: "Marinade",
			Ingredients: []recipe.Ingredient{
				ingredient("yoghurt", "whole-milk yoghurt", "1/2", "volume.cup.us.customary"),
				ingredient("garam", "garam masala", "2", "volume.teaspoon.us.customary"),
				ingredient("ginger", "grated ginger", "1", "volume.tablespoon.us.customary"),
				ingredient("chicken", "boneless chicken thighs", "2", "mass.pound.avoirdupois"),
			},
		},
		{
			ID: "sauce", Title: "Sauce",
			Ingredients: []recipe.Ingredient{
				ingredient("butter", "unsalted butter", "3", "volume.tablespoon.us.customary"),
				ingredient("onion", "yellow onion, finely chopped", "1", "count.item"),
				ingredient("tomato", "crushed tomatoes", "28", "mass.ounce.avoirdupois"),
				ingredient("cream", "heavy cream", "3/4", "volume.cup.us.customary"),
				{ID: "salt", Name: "salt", Note: "to taste"},
			},
		},
	}
	document.InstructionSections = []recipe.InstructionSection{
		steps("prep", "Marinate",
			"Whisk the yoghurt, garam masala, and ginger together with a generous pinch of salt.",
			"Fold the chicken through the marinade and refrigerate for at least two hours.",
		),
		steps("cook", "Cook",
			"Sear the chicken in a hot, dry pan until it takes colour on both sides, then set it aside.",
			"Melt the butter in the same pan and cook the onion until it is soft and sweet.",
			"Add the tomatoes and simmer until the sauce darkens and thickens.",
			"Return the chicken with any resting juices, pour in the cream, and simmer until cooked through.",
		),
	}
	document.Notes = []string{"The marinade can sit overnight; the yoghurt keeps the chicken tender."}

	return document
}

func printLong() recipe.Document {
	document := base("print-long", "Country sourdough")
	document.Description = "A four-day loaf built on a stiff levain and a long cold retard."
	document.Yields = []recipe.Yield{{Quantity: simple("2", "count.item"), Label: "loaves"}}
	document.Times = &recipe.Times{
		Prep: iso("PT1H"), Cook: iso("PT50M"),
		Assertions: []recipe.TimingAssertion{
			{ID: "timing-1", Kind: recipe.TimingFerment, Duration: recipe.DurationExpression{Value: "PT12H"}, Label: "Build the levain"},
			{ID: "timing-2", Kind: recipe.TimingProof, Duration: recipe.DurationExpression{Value: "PT5H"}},
			{ID: "timing-3", Kind: recipe.TimingChill, Duration: recipe.DurationExpression{Value: "P1D"}},
		},
	}
	document.IngredientSections = []recipe.IngredientSection{
		{
			ID: "levain", Title: "Levain",
			Ingredients: []recipe.Ingredient{
				ingredient("starter", "mature sourdough starter", "40", "mass.gram"),
				ingredient("levain-flour", "strong white bread flour", "120", "mass.gram"),
				ingredient("levain-water", "water at 26°C", "120", "volume.milliliter"),
			},
		},
		{
			ID: "dough", Title: "Final dough",
			Ingredients: []recipe.Ingredient{
				ingredient("flour", "strong white bread flour", "800", "mass.gram"),
				ingredient("whole", "stoneground wholemeal flour", "200", "mass.gram"),
				ingredient("water", "water at 30°C", "740", "volume.milliliter"),
				ingredient("salt", "fine sea salt", "22", "mass.gram"),
				ingredient("levain", "levain from above", "240", "mass.gram"),
			},
		},
	}
	document.InstructionSections = []recipe.InstructionSection{
		steps("levain", "Levain",
			"Mix the starter, flour, and water until no dry flour remains, and leave it covered until it has doubled and smells sweetly sour.",
		),
		steps("mix", "Mix and fold",
			"Combine the flours with 700 ml of the water and rest the mixture for one hour so the flour hydrates fully.",
			"Add the levain and squeeze it through the dough until it disappears.",
			"Dissolve the salt in the remaining water, work it through, and begin the first fold.",
			"Fold the dough every forty-five minutes for four hours, handling it more gently as it strengthens.",
		),
		steps("shape", "Shape and retard",
			"Turn the dough out, pre-shape it into two loose rounds, and rest them uncovered for thirty minutes.",
			"Shape each round tightly, seam side up in a floured banneton.",
			"Refrigerate overnight, or for up to a full day for a more pronounced sourness.",
		),
		steps("bake", "Bake",
			"Heat a covered pot at 250°C for a full hour before baking.",
			"Score each loaf decisively and bake covered for twenty minutes, then uncovered until the crust is deeply coloured.",
			"Cool completely on a rack before cutting, or the crumb will still be setting.",
		),
	}
	document.Notes = []string{
		"A cooler kitchen needs a longer bulk; watch the dough rather than the clock.",
		"The loaf keeps for three days cut side down on a board.",
	}

	return document
}

func printManyGroups() recipe.Document {
	document := base("print-many-groups", "Layered celebration cake")
	document.Yields = []recipe.Yield{{Quantity: simple("12", "count.serving"), Label: "servings"}}

	groups := []string{"Sponge", "Syrup", "Curd", "Buttercream", "Praline", "To finish"}
	for index, title := range groups {
		section := recipe.IngredientSection{ID: fmt.Sprintf("g%d", index), Title: title}
		for row := 1; row <= 5; row++ {
			section.Ingredients = append(section.Ingredients, ingredient(
				fmt.Sprintf("g%d-%d", index, row),
				fmt.Sprintf("%s ingredient %d", strings.ToLower(title), row),
				fmt.Sprint(row*25), "mass.gram",
			))
		}

		document.IngredientSections = append(document.IngredientSections, section)
	}

	for index, title := range groups {
		document.InstructionSections = append(document.InstructionSections, steps(
			fmt.Sprintf("s%d", index), title,
			fmt.Sprintf("Prepare the %s, working it until it is smooth and even.", strings.ToLower(title)),
			fmt.Sprintf("Set the %s aside at room temperature until you need it.", strings.ToLower(title)),
		))
	}

	return document
}

func printLongInstructions() recipe.Document {
	document := base("print-long-instructions", "Cassoulet")
	document.Yields = []recipe.Yield{{Quantity: simple("8", "count.serving"), Label: "servings"}}
	document.IngredientSections = []recipe.IngredientSection{{
		ID: "all",
		Ingredients: []recipe.Ingredient{
			ingredient("beans", "dried Tarbais beans", "1", "mass.pound.avoirdupois"),
			ingredient("duck", "duck legs confit", "4", "count.item"),
			ingredient("sausage", "coarse pork sausage", "1", "mass.pound.avoirdupois"),
		},
	}}
	// One step deliberately larger than a whole card, so pagination has to
	// divide it rather than shrink the type or clip it.
	enormous := strings.Repeat(
		"Keep the surface barely trembling and break the crust back into the beans each time it forms, "+
			"because the whole point of the dish is the repeated crust rather than any single one of them. ", 12)
	document.InstructionSections = []recipe.InstructionSection{
		steps("method", "",
			"Soak the beans overnight in cold water with a split onion.",
			strings.TrimSpace(enormous),
			"Rest the finished dish for twenty minutes before serving it at the table.",
		),
	}

	return document
}

func printLongTitle() recipe.Document {
	document := base(
		"print-long-title",
		"Slow-roasted shoulder of lamb with preserved lemon, green olives, and a great deal of garlic",
	)
	document.Yields = []recipe.Yield{{Quantity: simple("6", "count.serving"), Label: "servings"}}
	document.IngredientSections = []recipe.IngredientSection{{
		ID: "all",
		Ingredients: []recipe.Ingredient{
			ingredient("lamb", "bone-in lamb shoulder", "5", "mass.pound.avoirdupois"),
			ingredient("garlic", "garlic, separated into cloves", "2", "count.bunch"),
			ingredient("lemon", "preserved lemon, rinsed and chopped", "1", "count.item"),
			ingredient("olives", "green olives, pitted", "1", "volume.cup.us.customary"),
		},
	}}
	document.InstructionSections = []recipe.InstructionSection{
		steps("method", "",
			"Score the fat, season heavily, and roast uncovered until the surface is dark.",
			"Add the aromatics and a splash of water, cover, and cook low until the meat gives way.",
		),
	}

	return document
}

func printWithoutImage() recipe.Document {
	document := base("print-no-image", "Vinaigrette")
	document.Yields = []recipe.Yield{{Quantity: simple("1", "volume.cup.us.customary")}}
	document.IngredientSections = []recipe.IngredientSection{{
		ID: "all",
		Ingredients: []recipe.Ingredient{
			ingredient("vinegar", "sherry vinegar", "3", "volume.tablespoon.us.customary"),
			ingredient("oil", "olive oil", "1/2", "volume.cup.us.customary"),
			ingredient("mustard", "Dijon mustard", "1", "volume.teaspoon.us.customary"),
		},
	}}
	document.InstructionSections = []recipe.InstructionSection{
		steps("method", "", "Whisk the vinegar and mustard, then stream in the oil."),
	}

	return document
}

func printWithNotesAndSource() recipe.Document {
	document := base("print-notes-source", "Grandmother's apple cake")
	document.Description = "Written on a card in her hand, with the oven temperature crossed out twice."
	document.Authors = []recipe.Author{{Name: "Ada Whitfield", Kind: recipe.AuthorPerson, URL: "https://example.com/ada-whitfield"}}
	document.Source = &recipe.Source{Name: "Family notebook", URL: "https://example.com/apple-cake"}
	document.Yields = []recipe.Yield{{Quantity: simple("10", "count.serving"), Label: "servings"}}
	document.IngredientSections = []recipe.IngredientSection{{
		ID: "all",
		Ingredients: []recipe.Ingredient{
			ingredient("apples", "tart apples, peeled and sliced", "6", "count.item"),
			ingredient("flour", "all-purpose flour", "2", "volume.cup.us.customary"),
			ingredient("sugar", "granulated sugar", "1", "volume.cup.us.customary"),
			ingredient("eggs", "eggs", "3", "count.item"),
		},
	}}
	document.InstructionSections = []recipe.InstructionSection{
		steps("method", "",
			"Toss the apples with a little of the sugar and set them aside.",
			"Beat the eggs with the rest of the sugar until pale, then fold in the flour.",
			"Layer the batter and apples and bake until a skewer comes out clean.",
		),
	}
	document.Notes = []string{
		"The crossed-out temperature was 375°F; the card settles on 350°F.",
		"It is better on the second day.",
	}

	return document
}

func printNonUSMeasures() recipe.Document {
	document := base("print-metric", "Lemon drizzle loaf")
	document.Language = "en-GB"
	document.Yields = []recipe.Yield{{Quantity: simple("1", "count.item"), Label: "loaf"}}
	document.IngredientSections = []recipe.IngredientSection{{
		ID: "all",
		Ingredients: []recipe.Ingredient{
			ingredient("butter", "unsalted butter, softened", "225", "mass.gram"),
			ingredient("sugar", "caster sugar", "225", "mass.gram"),
			ingredient("flour", "self-raising flour", "225", "mass.gram"),
			ingredient("milk", "whole milk", "60", "volume.milliliter"),
			ingredient("eggs", "eggs", "4", "count.item"),
		},
	}}
	document.InstructionSections = []recipe.InstructionSection{
		steps("method", "",
			"Cream the butter and sugar until pale.",
			"Beat in the eggs one at a time, then fold in the flour and milk.",
			"Bake until risen and golden, then spoon over the drizzle while warm.",
		),
	}
	document.InstructionSections[0].Steps[2].Temperatures = []recipe.Temperature{
		{Amount: recipe.Amount{Value: Rational("180")}, Unit: "temperature.celsius"},
	}

	return document
}

func printUnicode() recipe.Document {
	document := base("print-unicode", "Crème brûlée à la vanille")
	document.Language = "fr-FR"
	document.Description = "Une crème très simple, cuite au bain-marie, brûlée juste avant de servir."
	document.Yields = []recipe.Yield{{Quantity: simple("6", "count.serving"), Label: "ramequins"}}
	document.IngredientSections = []recipe.IngredientSection{{
		ID: "all",
		Ingredients: []recipe.Ingredient{
			ingredient("cream", "crème fleurette entière", "500", "volume.milliliter"),
			ingredient("yolks", "jaunes d'œufs", "6", "count.item"),
			ingredient("sugar", "sucre en poudre", "90", "mass.gram"),
			ingredient("vanilla", "gousse de vanille, fendue et grattée", "1", "count.item"),
		},
	}}
	document.InstructionSections = []recipe.InstructionSection{
		steps("method", "",
			"Faites infuser la vanille dans la crème chaude pendant vingt minutes.",
			"Blanchissez les jaunes avec le sucre, puis versez la crème dessus en fouettant.",
			"Cuisez au bain-marie à 95 °C jusqu'à ce que la crème tremble encore légèrement.",
			"Saupoudrez de sucre et brûlez la surface au chalumeau avant de servir.",
		),
	}

	return document
}

// iso builds an exact duration expression for a fixture.
func iso(value string) *recipe.DurationExpression {
	return &recipe.DurationExpression{Value: value}
}

// printRightToLeft is an Arabic recipe with Latin unit abbreviations and a
// Latin product name inside its text.
func printRightToLeft() recipe.Document {
	document := base("print-right-to-left", "شاي بالنعناع")
	document.Language = "ar"
	document.Yields = []recipe.Yield{{Quantity: simple("4", "count.serving")}}
	document.Times = &recipe.Times{Prep: iso("PT5M"), Cook: iso("PT5M")}
	document.IngredientSections = []recipe.IngredientSection{{
		ID: "tea",
		Ingredients: []recipe.Ingredient{
			ingredient("green-tea", "شاي أخضر صيني (Gunpowder)", "10", "mass.gram"),
			ingredient("mint", "نعناع طازج", "1", "count.bunch"),
			ingredient("sugar", "سكر", "40", "mass.gram"),
			ingredient("water", "ماء مغلي", "750", "volume.milliliter"),
		},
	}}
	document.InstructionSections = []recipe.InstructionSection{
		steps("method", "",
			"اشطف الشاي بقليل من الماء المغلي وتخلّص من الماء.",
			"أضف النعناع والسكر وباقي الماء، واتركه 4–5 دقائق.",
			"اسكبه من علوٍّ في الكؤوس.",
		),
	}

	return document
}
