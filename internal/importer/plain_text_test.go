package importer_test

import (
	"context"
	"testing"

	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func TestPlainTextRecipePreservesSectionsNotesAndURLs(t *testing.T) {
	input := importer.Input{Name: "old-recipe.txt", Data: []byte("Aunt May's stew\n\nIngredients\nSauce:\n2 grams flour\n1 tsp salt\n\nInstructions\n1. Mix the flour.\n2) Bake until done.\n\nNote from Dad: use a hot pan\nhttps://example.test/stew\n")}

	batch, err := importer.DefaultRegistry().Import(context.Background(), input, importer.Options{})
	if err != nil {
		t.Fatal(err)
	}

	if len(batch.Recipes) != 1 {
		t.Fatalf("recipes=%d", len(batch.Recipes))
	}

	doc := batch.Recipes[0].Document
	if doc.Title != "Aunt May's stew" || len(doc.IngredientSections) != 1 || doc.IngredientSections[0].Title != "Sauce" || len(doc.IngredientSections[0].Ingredients) != 2 || doc.IngredientSections[0].Ingredients[0].Quantity == nil || doc.IngredientSections[0].Ingredients[0].Quantity.Amount.Value.String() != "2" || len(doc.InstructionSections) != 1 || len(doc.InstructionSections[0].Steps) != 2 || doc.InstructionSections[0].Steps[0].Text != "Mix the flour." || doc.InstructionSections[0].Steps[1].Text != "Bake until done." {
		t.Fatalf("document=%#v", doc)
	}

	if doc.Source == nil || doc.Source.URL != "https://example.test/stew" || len(doc.Notes) != 2 || doc.Notes[0] != "Note from Dad: use a hot pan" || doc.Notes[1] != "https://example.test/stew" || len(batch.Recipes[0].Diagnostics) == 0 {
		t.Fatalf("source=%#v notes=%#v diagnostics=%#v", doc.Source, doc.Notes, batch.Recipes[0].Diagnostics)
	}
}

func TestPlainTextWithoutHeadingsUsesConservativeHeuristics(t *testing.T) {
	batch, err := importer.DefaultRegistry().Import(context.Background(), importer.Input{Name: "pasted.txt", Data: []byte("Quick eggs\n\n2 eggs\n1 tbsp butter\n\nWhisk well and serve.")}, importer.Options{})
	if err != nil {
		t.Fatal(err)
	}

	doc := batch.Recipes[0].Document
	if len(doc.IngredientSections) != 1 || len(doc.IngredientSections[0].Ingredients) != 2 || len(doc.Notes) != 1 || len(doc.InstructionSections) != 0 {
		t.Fatalf("document=%#v", doc)
	}
}

func TestPlainTextMethodHeadingsKeepOrderWithoutEmptySections(t *testing.T) {
	batch, err := importer.DefaultRegistry().Import(context.Background(), importer.Input{Name: "recipe.txt", Data: []byte("Cake\nIngredients\n2 eggs\nInstructions\nBatter:\nMix eggs.\nBake:\nBake until set.")}, importer.Options{})
	if err != nil {
		t.Fatal(err)
	}

	sections := batch.Recipes[0].Document.InstructionSections
	if len(sections) != 2 || sections[0].Title != "Batter" || sections[1].Title != "Bake" || len(sections[0].Steps) != 1 || len(sections[1].Steps) != 1 {
		t.Fatalf("sections=%+v", sections)
	}
}

func TestPlainTextStateTransitionsPreserveContent(t *testing.T) {
	input := importer.Input{
		Name: "transitions.txt",
		Data: []byte("Layered recipe\nPreamble for review\nIngredients\nSauce:\n1 cup water\nInstructions\nPrep:\n1. Stir well\nNote: keep warm\nIngredients\nFinish:\n1 tsp salt\nInstructions\nServe:\n2. Serve now\nhttps://example.test/layered"),
	}

	batch, err := importer.DefaultRegistry().Import(context.Background(), input, importer.Options{})
	if err != nil {
		t.Fatal(err)
	}

	doc := batch.Recipes[0].Document
	if doc.Title != "Layered recipe" {
		t.Fatalf("title lost: %q", doc.Title)
	}

	assertPlainTextSections(t, doc, []string{"Sauce", "Finish"}, []string{"Prep", "Serve"})

	if len(doc.Notes) != 3 || doc.Notes[0] != "Preamble for review" || doc.Notes[1] != "Note: keep warm" || doc.Notes[2] != "https://example.test/layered" {
		t.Fatalf("notes lost across transitions: %#v", doc.Notes)
	}

	if doc.Source == nil || doc.Source.URL != "https://example.test/layered" {
		t.Fatalf("source URL lost: %#v", doc.Source)
	}
}

func assertPlainTextSections(t *testing.T, doc recipe.Document, ingredientTitles, instructionTitles []string) {
	t.Helper()

	if len(doc.IngredientSections) != 2 || len(doc.InstructionSections) != 2 {
		t.Fatalf("expected two populated sections of each kind: ingredients=%#v instructions=%#v", doc.IngredientSections, doc.InstructionSections)
	}

	for i, source := range []string{"1 cup water", "1 tsp salt"} {
		section := doc.IngredientSections[i]
		if section.Title != ingredientTitles[i] || len(section.Ingredients) != 1 || section.Ingredients[0].SourceText != source {
			t.Fatalf("ingredient section %d lost title or ordering: %#v", i, section)
		}
	}

	for i, text := range []string{"Stir well", "Serve now"} {
		section := doc.InstructionSections[i]
		if section.Title != instructionTitles[i] || len(section.Steps) != 1 || section.Steps[0].Text != text {
			t.Fatalf("instruction section %d lost title or ordering: %#v", i, section)
		}
	}

	if err := recipe.ValidateForWrite(doc, units.DefaultCatalog()); err != nil {
		t.Fatalf("plain-text document must pass write validation: %v", err)
	}
}

func TestPlainTextRepeatedHeadingsCreateOnlyPopulatedSections(t *testing.T) {
	for _, tc := range []struct {
		name              string
		text              string
		ingredientTitles  []string
		instructionTitles []string
	}{
		{
			name:              "repeated modes followed by subtitles",
			text:              "Layered recipe\nIngredients\nSauce:\n1 cup water\nInstructions\nPrep:\nStir well\nIngredients\nFinish:\n1 tsp salt\nInstructions\nServe:\nServe now",
			ingredientTitles:  []string{"Sauce", "Finish"},
			instructionTitles: []string{"Prep", "Serve"},
		},
		{
			name:              "consecutive and trailing mode headings",
			text:              "Layered recipe\nIngredients\nIngredients\nSauce:\n1 cup water\nInstructions\nInstructions\nPrep:\nStir well\nIngredients\nIngredients\nFinish:\n1 tsp salt\nInstructions\nDirections\nServe:\nServe now\nIngredients\nInstructions",
			ingredientTitles:  []string{"Sauce", "Finish"},
			instructionTitles: []string{"Prep", "Serve"},
		},
		{
			name:              "untitled sections do not inherit a previous title",
			text:              "Layered recipe\nIngredients\nSauce:\n1 cup water\nInstructions\nPrep:\nStir well\nIngredients\n1 tsp salt\nInstructions\nServe now",
			ingredientTitles:  []string{"Sauce", ""},
			instructionTitles: []string{"Prep", ""},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			batch, err := importer.DefaultRegistry().Import(context.Background(), importer.Input{Name: "recipe.txt", Data: []byte(tc.text)}, importer.Options{})
			if err != nil {
				t.Fatal(err)
			}

			assertPlainTextSections(t, batch.Recipes[0].Document, tc.ingredientTitles, tc.instructionTitles)
		})
	}
}

func TestPlainTextRefusesInvalidText(t *testing.T) {
	for _, data := range [][]byte{[]byte("Recipe\x00binary"), {0xff, 0xfe, 0x41}} {
		if _, err := importer.DefaultRegistry().Import(context.Background(), importer.Input{Name: "recipe.txt", Data: data}, importer.Options{}); err == nil {
			t.Fatal("invalid text accepted")
		}
	}
}

func TestPlainTextRecognizesSourceLanguageHeadings(t *testing.T) {
	for _, example := range []struct{ text, ingredient, instruction string }{
		{"Kuchen\nZutaten\n2 g Mehl\nZubereitung\n20 Minuten backen.", "2 g Mehl", "20 Minuten backen."},
		{"パン\n材料\n2 g 小麦粉\n作り方\n20分焼く。", "2 g 小麦粉", "20分焼く。"},
		{"Soupe\nIngrédients\n2 g sel\nPréparation\nCuire 20 minutes.", "2 g sel", "Cuire 20 minutes."},
	} {
		batch, err := importer.DefaultRegistry().Import(t.Context(), importer.Input{Name: "recipe.txt", Data: []byte(example.text)}, importer.Options{})
		if err != nil {
			t.Fatal(err)
		}

		doc := batch.Recipes[0].Document
		if len(doc.IngredientSections) != 1 || len(doc.IngredientSections[0].Ingredients) != 1 || len(doc.InstructionSections) != 1 || len(doc.InstructionSections[0].Steps) != 1 {
			t.Fatalf("source not recognized: %+v", doc)
		}

		if doc.IngredientSections[0].Ingredients[0].SourceText != example.ingredient || doc.InstructionSections[0].Steps[0].Text != example.instruction || doc.Language != "" {
			t.Fatalf("source changed: %+v", doc)
		}
	}
}

func TestPlainTextKeepsAStepThatCitesALink(t *testing.T) {
	step := "Prepare starter following https://example.test/starter then rest overnight"

	batch, err := importer.DefaultRegistry().Import(context.Background(), importer.Input{Name: "bread.txt", Data: []byte("Bread\nIngredients\n500 g flour\nInstructions\n" + step + "\nhttps://example.test/bread\n")}, importer.Options{})
	if err != nil {
		t.Fatal(err)
	}

	doc := batch.Recipes[0].Document
	if len(doc.InstructionSections) != 1 || len(doc.InstructionSections[0].Steps) != 1 || doc.InstructionSections[0].Steps[0].Text != step {
		t.Fatalf("instructions=%+v", doc.InstructionSections)
	}

	if len(doc.Notes) != 1 || doc.Notes[0] != "https://example.test/bread" || doc.Source == nil || doc.Source.URL != "https://example.test/bread" {
		t.Fatalf("notes=%#v source=%#v", doc.Notes, doc.Source)
	}
}
