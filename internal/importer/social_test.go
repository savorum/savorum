package importer

import (
	"context"
	"html"
	"strconv"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/importer/webrecipe"
)

func TestPublicSocialCaptionImport(t *testing.T) {
	for _, host := range []string{"www.tiktok.com", "www.instagram.com", "www.youtube.com", "www.reddit.com", "www.facebook.com", "www.pinterest.com"} {
		t.Run(host, func(t *testing.T) {
			caption := "Ingredients\n1 cup rice\n2 cups water\nInstructions\n1. Simmer until tender.\nCredit: Home cook"
			page := `<html><meta property="og:title" content="Rice supper"><meta property="og:description" content="` + html.EscapeString(caption) + `"></html>`
			address := "https://" + host + "/recipe/123"

			batch, err := DecodeWebRecipeContext(context.Background(), []byte(page), "text/html", address)
			if err != nil {
				t.Fatal(err)
			}

			if len(batch.Recipes) != 1 {
				t.Fatalf("recipes = %d", len(batch.Recipes))
			}

			item := batch.Recipes[0]
			if item.Document.Title != "Rice supper" || item.Document.Source.URL != address || item.SourceKey != address {
				t.Fatalf("identity lost: %#v", item)
			}

			if len(item.Document.IngredientSections[0].Ingredients) != 2 || item.Document.InstructionSections[0].Steps[0].Text != "Simmer until tender." {
				t.Fatalf("recipe contents = %#v", item.Document)
			}

			sourceText, ok := item.ImporterMetadata["source_text"].(string)
			if !ok || !strings.Contains(sourceText, caption) {
				t.Fatal("caption lost")
			}

			if item.Diagnostics[len(item.Diagnostics)-1].Code != "social-caption" {
				t.Fatal("missing extraction notice")
			}
		})
	}
}

func TestSocialVideoDescriptionAndStructuredRecipePriority(t *testing.T) {
	page := `<script type="application/ld+json">{"@type":"VideoObject","name":"Rice", "description":"Ingredients\n1 cup rice\nInstructions\nBoil the rice."}</script>`

	batch, err := DecodeWebRecipeContext(context.Background(), []byte(page), "text/html", "https://www.youtube.com/watch?v=abc")
	if err != nil || len(batch.Recipes) != 1 {
		t.Fatalf("video caption: %v %v", batch, err)
	}

	page += `<script type="application/ld+json">{"@type":"Recipe","name":"Authored recipe", "recipeIngredient":["2 cups rice"], "recipeInstructions":["Steam."]}</script>`

	batch, err = DecodeWebRecipeContext(context.Background(), []byte(page), "text/html", "https://www.youtube.com/watch?v=abc")
	if err != nil || batch.Recipes[0].Document.Title != "Authored recipe" {
		t.Fatalf("structured recipe priority: %v %v", batch, err)
	}
}

func TestSocialCaptionDoesNotCreateRecipesFromMarketingOrLoginPages(t *testing.T) {
	for _, caption := range []string{"Log in to see photos and videos", "Watch my delicious dinner!", "Ingredients\n1 cup rice", "Instructions\nSubscribe to my channel"} {
		page := `<meta property="og:description" content="` + html.EscapeString(caption) + `">`

		_, err := DecodeWebRecipeContext(context.Background(), []byte(page), "text/html", "https://www.instagram.com/reel/abc/")
		if webrecipe.CodeOf(err) != webrecipe.NoRecipeStructuredData {
			t.Fatalf("caption %q: %v", caption, err)
		}
	}
}

func TestSocialImportCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := DecodeWebRecipeContext(ctx, []byte(`<meta property="og:description" content="Ingredients">`), "text/html", "https://youtu.be/abc")
	if err == nil {
		t.Fatal("cancelled import succeeded")
	}
}

func TestSocialPostFollowsRecipeLinkAndPreservesProvenance(t *testing.T) {
	page := webrecipe.Load([]byte(`<meta property="og:description" content="Full recipe: https://example.com/rice">`), "text/html", "https://pin.it/rice")

	var requested []string

	fetch := func(_ context.Context, address string) (*webrecipe.Page, error) {
		requested = append(requested, address)
		return webrecipe.Load([]byte(`{"@type":"Recipe","name":"Rice","recipeIngredient":["1 cup rice"],"recipeInstructions":["Boil."]}`), "application/ld+json", address), nil
	}

	batch, err := fetchSharedRecipe(context.Background(), page, fetch)
	if err != nil || len(requested) != 1 || requested[0] != "https://example.com/rice" {
		t.Fatalf("linked import: %v %v", requested, err)
	}

	item := batch.Recipes[0]
	if item.Document.Source.URL != requested[0] || item.Document.Notes[0] != "Shared from https://pin.it/rice" {
		t.Fatalf("provenance lost: %#v", item.Document)
	}
}

func TestSocialPostDoesNotRecursivelyFollowMorePosts(t *testing.T) {
	page := webrecipe.Load([]byte(`<meta property="og:description" content="https://example.com/one https://example.com/two https://example.com/three https://example.com/four">`), "text/html", "https://www.reddit.com/r/recipes/post")
	calls := 0
	fetch := func(_ context.Context, _ string) (*webrecipe.Page, error) {
		calls++
		return webrecipe.Load([]byte(`<meta property="og:description" content="https://example.com/more">`), "text/html", "https://www.pinterest.com/pin/another"), nil
	}

	_, err := fetchSharedRecipe(context.Background(), page, fetch)
	if err == nil || calls != 3 {
		t.Fatalf("unbounded or empty import: %d %v", calls, err)
	}
}

func TestSocialDraftKeepsAuthoredTextWithoutGuessingStructure(t *testing.T) {
	for _, caption := range []string{
		"4 lemons. Cook for 3 minutes. 2 cups cream.",
		"Recette :\n250 g de farine\nMélangez.",
	} {
		body := `<script type="application/ld+json">{"@type":"VideoObject","name":"Caption","description":` + strconv.Quote(caption) + `}</script>`

		batch, err := DecodeWebRecipeContext(context.Background(), []byte(body), "text/html", "https://example.com/video/recipe123")
		if err != nil {
			t.Fatal(err)
		}

		item := batch.Recipes[0]
		if len(item.Document.IngredientSections) != 0 || len(item.Document.InstructionSections) != 0 || item.Document.Notes[0] != caption || item.ImporterMetadata["source_text"] != caption {
			t.Fatalf("caption structure guessed or lost: %#v", item)
		}

		if item.Diagnostics[len(item.Diagnostics)-1].Code != "social-caption-draft" {
			t.Fatal("draft not flagged")
		}
	}
}

func TestIdentifiedPostLocaleInterpretsMeasurements(t *testing.T) {
	for _, test := range []struct{ country, cup, tablespoon string }{
		{"US", "volume.cup.us.customary", "volume.tablespoon.us.customary"},
		{"AU", "volume.cup.au", "volume.tablespoon.au"},
	} {
		t.Run(test.country, func(t *testing.T) {
			body := `<script type="application/json">{"id":"recipe123","textLanguage":"en","locationCreated":"` + test.country + `","contents":[{"desc":"Ingredients"},{"desc":"2 cups cream"},{"desc":"1 tbsp sugar"},{"desc":"Instructions"},{"desc":"Mix."}]}</script>`

			batch, err := DecodeWebRecipeContext(context.Background(), []byte(body), "text/html", "https://example.com/video/recipe123")
			if err != nil {
				t.Fatal(err)
			}

			doc := batch.Recipes[0].Document

			lines := doc.IngredientSections[0].Ingredients
			if doc.SourceLocale != "en-"+test.country || lines[0].Quantity == nil || lines[1].Quantity == nil {
				t.Fatalf("locale/quantity lost: %#v", doc)
			}

			if lines[0].Quantity.Unit != test.cup || lines[1].Quantity.Unit != test.tablespoon {
				t.Fatalf("wrong regional units: %+v %+v", lines[0].Quantity, lines[1].Quantity)
			}
		})
	}
}

func TestLinkedRecipeWinsOverIncompleteCaptionDraft(t *testing.T) {
	page := webrecipe.Load([]byte(`<script type="application/ld+json">{"@type":"VideoObject","description":"Ingredients\n1 cup rice\nhttps://example.com/rice"}</script>`), "text/html", "https://example.com/video/recipe123")

	batch, err := fetchSharedRecipe(context.Background(), page, func(_ context.Context, address string) (*webrecipe.Page, error) {
		return webrecipe.Load([]byte(`{"@type":"Recipe","name":"Linked rice","recipeIngredient":["1 cup rice"],"recipeInstructions":["Boil."]}`), "application/ld+json", address), nil
	})
	if err != nil || batch.Recipes[0].Document.Title != "Linked rice" {
		t.Fatalf("linked priority: %v %v", batch, err)
	}
}

func TestCaptionWithoutRegionOffersMeasurementReview(t *testing.T) {
	page := `<script type="application/ld+json">{"@type":"VideoObject","description":"Ingredients\n1 cup rice\n1 tbsp oil\nInstructions\nCook."}</script>`

	batch, err := DecodeWebRecipeContext(context.Background(), []byte(page), "text/html", "https://example.com/video/recipe123")
	if err != nil {
		t.Fatal(err)
	}

	for _, d := range batch.Recipes[0].Diagnostics {
		if d.Code == "ingredient-review-required" {
			if len(d.Targets) != 2 || d.Targets[0].Resolution == nil || d.Targets[1].Resolution == nil {
				t.Fatalf("review choices missing: %+v", d)
			}

			return
		}
	}

	t.Fatal("unresolved measurements silently accepted")
}

func TestCaptionDraftCarriesThumbnailThroughImagePipeline(t *testing.T) {
	body := `<meta property="og:image" content="https://example.com/poster.jpg"><script type="application/ld+json">{"@type":"VideoObject","description":"A caption without section headings"}</script>`

	batch, err := DecodeWebRecipeContext(context.Background(), []byte(body), "text/html", "https://example.com/video/recipe123")
	if err != nil {
		t.Fatal(err)
	}

	assets := batch.Recipes[0].Assets
	if len(assets) != 1 || !assets[0].RecipeImage || assets[0].URL != "https://example.com/poster.jpg" {
		t.Fatalf("draft poster missing: %+v", assets)
	}
}

func TestLinkedRecipeImageTakesPriorityOverVideoThumbnail(t *testing.T) {
	page := webrecipe.Load([]byte(`<meta property="og:image" content="https://example.com/poster.jpg"><meta property="og:description" content="https://example.com/rice">`), "text/html", "https://example.com/video/recipe123")

	batch, err := fetchSharedRecipe(context.Background(), page, func(_ context.Context, address string) (*webrecipe.Page, error) {
		return webrecipe.Load([]byte(`{"@type":"Recipe","name":"Rice","recipeIngredient":["1 cup rice"],"recipeInstructions":["Cook."],"image":"https://example.com/recipe.jpg"}`), "application/ld+json", address), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	assets := batch.Recipes[0].Assets
	if len(assets) != 1 || assets[0].URL != "https://example.com/recipe.jpg" {
		t.Fatalf("wrong image priority: %+v", assets)
	}
}

func TestSocialDraftRecognizesExplicitLocalizedHeadings(t *testing.T) {
	for _, caption := range []string{
		"Ingrédients :\n250 g de farine\nÉtapes :\n1. Mélangez.",
		"Ingredientes\n1 taza de agua\nPreparación\n1. Mezcla.", //nolint:misspell // Spanish source text.
	} {
		body := `<script type="application/ld+json">{"@type":"VideoObject","name":"Caption","description":` + strconv.Quote(caption) + `}</script>`

		batch, err := DecodeWebRecipeContext(t.Context(), []byte(body), "text/html", "https://example.com/video/recipe123")
		if err != nil {
			t.Fatal(err)
		}

		item := batch.Recipes[0]
		if len(item.Document.IngredientSections) != 1 || len(item.Document.IngredientSections[0].Ingredients) != 1 || len(item.Document.InstructionSections) != 1 || len(item.Document.InstructionSections[0].Steps) != 1 || item.ImporterMetadata["source_text"] != caption {
			t.Fatalf("localized caption changed: %+v", item)
		}
	}
}
