package importer

import (
	"slices"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func TestPastedRecipeFormatting(t *testing.T) {
	for _, tc := range []struct {
		name, text, title  string
		ingredients, steps []string
	}{
		{"untitled", "2 eggs\n1 tbsp oil\n\nWhisk the eggs.\nCook.", "Untitled recipe", []string{"2 eggs", "1 tbsp oil"}, []string{"Whisk the eggs.", "Cook."}},
		{"untitled with seasoning", "2 eggs\nSalt to taste\n1 tbsp oil\n\nWhisk.\nCook.", "Untitled recipe", []string{"2 eggs", "Salt to taste", "1 tbsp oil"}, []string{"Whisk.", "Cook."}},
		{"numeric title", "3 bean salad\n\nIngredients\n2 cans beans\n1 tbsp oil\nInstructions\nMix.", "3 bean salad", []string{"2 cans beans", "1 tbsp oil"}, []string{"Mix."}},
		{"Markdown checklist", "# Crêpes\n\n**Ingrédients**\n- [ ] 250 g de farine\n- [x] 200 ml de lait\n\n## Préparation\n1. Mélangez.\n2. Faites cuire.", "Crêpes", []string{"250 g de farine", "200 ml de lait"}, []string{"Mélangez.", "Faites cuire."}},
		{"checkboxes", "Tortitas\nIngredientes\n☐ 250 g harina\n☑ 200 ml leche\nPreparación\nMezcla.\nCocina.", "Tortitas", []string{"250 g harina", "200 ml leche"}, []string{"Mezcla.", "Cocina."}}, //nolint:misspell // Spanish source text.
		{"Arabic numbering", "فطائر\nالمكونات\n٢٥٠ غرام دقيق\nالطريقة\n١. اخلطي المكونات.\n٢. اطهي في المقلاة.", "فطائر", []string{"٢٥٠ غرام دقيق"}, []string{"اخلطي المكونات.", "اطهي في المقلاة."}},
		{"fullwidth numbering", "パンケーキ\n材料\n250g 小麦粉\n作り方\n１． 材料を混ぜます。\n２）フライパンで焼きます。", "パンケーキ", []string{"250g 小麦粉"}, []string{"材料を混ぜます。", "フライパンで焼きます。"}},
		{"Chinese numbering", "煎饼\n食材\n250克 面粉\n做法\n1、混合食材。\n2、煎熟。", "煎饼", []string{"250克 面粉"}, []string{"混合食材。", "煎熟。"}},
		{"Chinese paragraphs", "煎饼\n食材\n250克 面粉\n做法\n混合食材。\n\n煎熟。", "煎饼", []string{"250克 面粉"}, []string{"混合食材。", "煎熟。"}},
		{"instruction section after bullets", "煎饼\n食材\n250克 面粉\n做法\n- 混合食材。\n\n做法\n\n煎熟。", "煎饼", []string{"250克 面粉"}, []string{"混合食材。", "煎熟。"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			batch, err := DefaultRegistry().Import(t.Context(), Input{Name: "pasted.txt", Data: []byte(tc.text)}, Options{})
			if err != nil {
				t.Fatal(err)
			}

			item := batch.Recipes[0]
			if item.Document.Title != tc.title {
				t.Fatalf("title: %q", item.Document.Title)
			}

			assertPastedLines(t, item, tc.text, tc.ingredients, tc.steps)
		})
	}
}

func TestExtractedIngredientLineBreaks(t *testing.T) {
	for _, tc := range []struct {
		name, text  string
		ingredients []string
	}{
		{"unit", "Pancakes\nIngredients\n250 g\nflour\n200 ml milk\nInstructions\nMix.", []string{"250 g flour", "200 ml milk"}},
		{"localized unit", "Блины\nИнгредиенты\n250 г\nмуки\n200 мл молока\nПриготовление\nСмешайте.", []string{"250 г муки", "200 мл молока"}},
		{"continuation", "Salad\nIngredients\n2 tbsp oil,\nplus extra to serve\n1 tomato\nSalt and pepper\nInstructions\nMix.", []string{"2 tbsp oil, plus extra to serve", "1 tomato", "Salt and pepper"}},
		{"counted ingredient", "Eggs\nIngredients\n1 egg\nSalt and pepper\nInstructions\nMix.", []string{"1 egg", "Salt and pepper"}},
		{"new quantity", "Salad\nIngredients\n2 tomatoes,\n1 onion\nInstructions\nMix.", []string{"2 tomatoes,", "1 onion"}},
		{"new bullet", "Salad\nIngredients\n2 tomatoes,\n- Salt and pepper\nInstructions\nMix.", []string{"2 tomatoes,", "Salt and pepper"}},
		{"new paragraph", "Salad\nIngredients\n2 tomatoes,\n\nSalt and pepper\nInstructions\nMix.", []string{"2 tomatoes,", "Salt and pepper"}},
		{"uncertain characters", "Bread\nIngredients\nl00 g flour\nI tsp salt\nO.5 cup water\nInstructions\nMix.", []string{"l00 g flour", "I tsp salt", "O.5 cup water"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{"photo.jpg", "scan.pdf"} {
				batch, err := (documentAdapter{OCR: &fakeOCR{text: tc.text}}).Import(t.Context(), Input{Name: name, Data: []byte("document")}, Options{})
				if err != nil {
					t.Fatal(err)
				}

				item := batch.Recipes[0]

				step := "Mix."
				if tc.name == "localized unit" {
					step = "Смешайте."
				}

				assertPastedLines(t, item, tc.text, tc.ingredients, []string{step})

				if tc.name == "uncertain characters" {
					for _, group := range item.Document.IngredientSections {
						for _, ingredient := range group.Ingredients {
							if ingredient.Quantity != nil {
								t.Fatalf("guessed quantity: %+v", ingredient)
							}
						}
					}
				}
			}
		})
	}
}

func assertPastedLines(t *testing.T, item ImportedRecipe, source string, ingredients, steps []string) {
	t.Helper()

	var gotIngredients, gotSteps []string

	for _, group := range item.Document.IngredientSections {
		for _, ingredient := range group.Ingredients {
			gotIngredients = append(gotIngredients, ingredient.SourceText)
		}
	}

	for _, group := range item.Document.InstructionSections {
		for _, step := range group.Steps {
			gotSteps = append(gotSteps, step.Text)
		}
	}

	if !slices.Equal(gotIngredients, ingredients) || !slices.Equal(gotSteps, steps) {
		t.Fatalf("ingredients=%q; steps=%q", gotIngredients, gotSteps)
	}

	if item.ImporterMetadata["source_text"] != source {
		t.Fatal("original text lost")
	}

	if err := recipe.ValidateForWrite(item.Document, units.DefaultCatalog()); err != nil {
		t.Fatal(err)
	}
}
