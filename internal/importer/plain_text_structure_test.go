package importer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/importer/webrecipe"
)

func TestCaptionPreservesAuthoredTitle(t *testing.T) {
	for _, title := range []string{"Shared recipe", "Untitled recipe", "夕食"} {
		batch, err := captionBatch(t.Context(), "https://example.com/post", &webrecipe.Post{Title: title}, "Sauce:\n2 tbsp oil\n1 tbsp vinegar\n\nMix.")
		if err != nil {
			t.Fatal(err)
		}

		if doc := batch.Recipes[0].Document; doc.Title != title || len(doc.IngredientSections) != 1 || doc.IngredientSections[0].Title != "Sauce" {
			t.Fatalf("authored title or ingredient group changed: %+v", doc)
		}
	}
}

func TestCaptionSectionBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, caption              string
		ingredients, steps, groups int
	}{
		{"compact bullets and groups", "Brown sugar topping:\n-2 tbsp sugar\n-1 tbsp butter\n\nBatter\n-2 eggs\n-100g flour\n\nFinish:\n-1 tsp salt\n\nInstructions:\n1. Mix.\n2. Bake.", 5, 2, 3},
		{"method between sections", "Ingredients:\n2 eggs\n1 tsp oil\n\nSauce\n2 tbsp honey\n1 tbsp water\n\nMix until dissolved.\n\nMethod:\n- Cook the eggs.\n- Serve.\n\nDone!\n#breakfast", 4, 3, 2},
		{"qualified headings", "Ingredients:\n2 carrots\n1 onion\nDrizzle of oil\nSalt and pepper\n\nInstructions for vegetables:\n1. Chop.\n2. Cook.\n\nInstructions for serving:\n1. Plate.", 4, 3, 1},
		{"wrapped numbered step", "Ingredients:\n2 eggs\n1 tsp oil\nInstructions:\n1. Heat the oil,\nthen add the eggs.\n2. Serve.", 2, 2, 1},
		{"numbered subheading", "Ingredients:\n1 egg\nInstructions:\n1. Batter:\nMix.\n2. Cook.", 1, 2, 1},
		{"multibyte subheading", "材料:\n生地に混ぜる野菜ときのこと調味料の下準備:\n100g 小麦粉\n200ml 牛乳\n作り方:\n混ぜる。", 2, 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"@type": "VideoObject", "name": "Dinner", "description": test.caption})
			if err != nil {
				t.Fatal(err)
			}

			batch, err := DecodeWebRecipe(body, "application/ld+json", "https://example.com/posts/recipe123")
			if err != nil {
				t.Fatal(err)
			}

			doc := batch.Recipes[0].Document

			i, s := 0, 0
			for _, group := range doc.IngredientSections {
				i += len(group.Ingredients)
			}

			for _, group := range doc.InstructionSections {
				s += len(group.Steps)
			}

			if i != test.ingredients || s != test.steps || len(doc.IngredientSections) != test.groups {
				t.Fatalf("sections: %+v", doc)
			}

			if test.name == "compact bullets and groups" && doc.IngredientSections[0].Title != "Brown sugar topping" {
				t.Fatalf("first group lost: %+v", doc)
			}

			if test.name == "wrapped numbered step" && doc.InstructionSections[0].Steps[0].Text != "Heat the oil, then add the eggs." {
				t.Fatalf("wrapped step lost: %+v", doc)
			}

			if batch.Recipes[0].ImporterMetadata["source_text"] != test.caption {
				t.Fatal("source text changed")
			}
		})
	}
}

func TestSocialCaptionInfersIngredientsAndSteps(t *testing.T) {
	for _, test := range []struct {
		name, caption, instruction string
		ingredients                int
	}{
		{"bullets", "A weeknight supper.\n\nBean supper:\n– 2 cans beans\n– 1 onion\n– 1.5 tbsp oil\n– Salt to taste\n\nBrown the onion, then stir in the beans and simmer for 10 minutes.\n\nFind more recipes in my cookbook.\n#dinner", "Brown the onion, then stir in the beans and simmer for 10 minutes.", 4},
		{"heading without method heading", "Ingredients:\n500 g flour\n370 ml water\n10 g yeast\n1 tsp salt\n\nCover and bake for 30 minutes.", "Cover and bake for 30 minutes.", 4},
		{"timed method", "500 g chicken\n1 tsp salt\n\nOven bake for 20-23 minutes at 200C.", "Oven bake for 20-23 minutes at 200C.", 2},
		{"French", "250 g de farine\n100 g de beurre\n\nMélangez les ingrédients et cuisez pendant 20 minutes.", "Mélangez les ingrédients et cuisez pendant 20 minutes.", 2},
		{"Spanish", "250 g de harina\n100 g de mantequilla\n\nMezcla los ingredients y hornea durante 20 minutos.", "Mezcla los ingredients y hornea durante 20 minutos.", 2},
		{"German", "250 g Mehl\n100 g Butter\n\nVermischen und backen.", "Vermischen und backen.", 2},
		{"Japanese", "250g 小麦粉\n100g バター\n\nよく混ぜて焼きます。", "よく混ぜて焼きます。", 2},
		{"Chinese cooked ingredient", "100g 烤鸡\n200g 米饭\n\n混合后加入调味料。", "混合后加入调味料。", 2},
		{"Arabic numbered steps", "٢٥٠ غرام دقيق\n١٠٠ غرام زبدة\n\n١. اخلطي المكونات.", "اخلطي المكونات.", 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := json.Marshal(map[string]any{"@type": "VideoObject", "name": "Supper", "description": test.caption})
			if err != nil {
				t.Fatal(err)
			}

			batch, err := DecodeWebRecipeContext(t.Context(), data, "application/ld+json", "https://example.com/video/recipe123")
			if err != nil || len(batch.Recipes) != 1 {
				t.Fatalf("import: %+v %v", batch, err)
			}

			item := batch.Recipes[0]

			doc := item.Document
			if len(doc.IngredientSections) != 1 || len(doc.IngredientSections[0].Ingredients) != test.ingredients || len(doc.InstructionSections) != 1 || len(doc.InstructionSections[0].Steps) != 1 || doc.InstructionSections[0].Steps[0].Text != test.instruction {
				t.Fatalf("sections: %+v %+v; lines=%v action=%v", doc.IngredientSections, doc.InstructionSections, textIngredientLines(strings.Split(test.caption, "\n"), ""), recipeTextAction(test.instruction))
			}

			if item.ImporterMetadata["source_text"] != test.caption {
				t.Fatal("original caption lost")
			}

			found := false

			for _, d := range item.Diagnostics {
				if d.Code == "text-sections-inferred" {
					found = true
				}
			}

			if !found {
				t.Fatal("missing inference review")
			}
		})
	}
}

func TestInferredIngredientGroups(t *testing.T) {
	text := "Chicken marinade\n\n- 500 g chicken\n- 1 tsp salt\n\nOven bake for 20 minutes.\n\nRice\n- 200 g rice\n- 1 tbsp oil\n\nSauce\n- 100 g yogurt\n- 20 g mustard"
	item := ImportedRecipe{}
	inferRecipeSections(&item, text, "")

	groups := item.Document.IngredientSections
	if len(groups) != 3 || groups[0].Title != "Chicken marinade" || groups[1].Title != "Rice" || groups[2].Title != "Sauce" || len(item.Document.InstructionSections) != 1 {
		t.Fatalf("grouped text: %+v", item.Document)
	}

	for _, group := range groups {
		if len(group.Ingredients) != 2 {
			t.Fatalf("group contents: %+v", group)
		}
	}
}

func TestCaptionDoesNotTurnPromotionsIntoSteps(t *testing.T) {
	for _, caption := range []string{
		"2 cups rice\n1 cup water\n\nFind the instructions in my cookbook.",
		"2 cups rice\n1 cup water\n\nSubscribe for more recipes!",
		"Cook with me!\n\n2 cups rice\n1 cup water",
		"4 lemons. Cook for 3 minutes. 2 cups cream.",
	} {
		data, err := json.Marshal(map[string]any{"@type": "VideoObject", "name": "Supper", "description": caption})
		if err != nil {
			t.Fatal(err)
		}

		batch, err := DecodeWebRecipeContext(t.Context(), data, "application/ld+json", "https://example.com/video/recipe123")
		if err != nil {
			t.Fatal(err)
		}

		if len(batch.Recipes[0].Document.InstructionSections) != 0 {
			t.Fatalf("invented method for %q", caption)
		}
	}
}

func TestCaptionKeepsUnmeasuredIngredientAndPromotionalNotes(t *testing.T) {
	caption := "Dinner:\n- 2 eggs\n- 1 tbsp oil\n- Salt to taste\n\nMix and cook.\n\nMy cookbook has more ideas."

	data, err := json.Marshal(map[string]any{"@type": "VideoObject", "description": caption})
	if err != nil {
		t.Fatal(err)
	}

	batch, err := DecodeWebRecipeContext(t.Context(), data, "application/ld+json", "https://example.com/video/recipe123")
	if err != nil {
		t.Fatal(err)
	}

	doc := batch.Recipes[0].Document
	if doc.Title != "Dinner" || len(doc.IngredientSections[0].Ingredients) != 3 || !strings.Contains(strings.Join(doc.Notes, "\n"), "cookbook") {
		t.Fatalf("caption split: %+v", doc)
	}
}
