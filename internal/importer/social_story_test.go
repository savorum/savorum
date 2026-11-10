package importer

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/importer/webrecipe"
)

// These invented social pages and sites are available under CC0 1.0 Universal.

func socialStoryPage(canonical, data string) []byte {
	return []byte(`<meta property="og:url" content="` + canonical + `"><script type="application/json">` + data + `</script>`)
}

func pageFetcher(t *testing.T, pages map[string]*webrecipe.Page) (func(context.Context, string) (*webrecipe.Page, error), *[]string) {
	t.Helper()

	var requested []string

	return func(_ context.Context, address string) (*webrecipe.Page, error) {
		requested = append(requested, address)
		if page, ok := pages[address]; ok {
			return page, nil
		}

		return nil, errors.New("not found")
	}, &requested
}

func diagnosticCodes(item ImportedRecipe) []string {
	var codes []string
	for _, diagnostic := range item.Diagnostics {
		codes = append(codes, diagnostic.Code)
	}

	return codes
}

func TestSocialPostUsesAuthorCommentThatHoldsTheRecipe(t *testing.T) {
	const address = "https://social.example/permalink.php?story_fbid=pfbid0abc&id=42"

	data := `{"story":{"post_id":"123456789","actors":[{"id":"42"}],"message":{"text":"Garlic soup\n\nIngredients:\n2 cups stock\n1 head garlic\n...."}},
"c1":{"__typename":"Comment","depth":0,"author":{"id":"7"},"body":{"text":"Ingredients:\n1 cup cream\nDirections:\nStir in cream."}},
"c2":{"__typename":"Comment","depth":0,"author":{"id":"42"},"body":{"text":"Garlic soup\n\nIngredients:\n2 cups stock\n1 head garlic\n\nDirections:\nSimmer the garlic in the stock.\nBlend until smooth."}}}`
	page := webrecipe.Load(socialStoryPage("https://social.example/cook/posts/garlic-soup/123456789/", data), "text/html", address)

	batch, err := fetchSharedRecipe(t.Context(), page, func(context.Context, string) (*webrecipe.Page, error) {
		t.Fatal("a complete author comment needs no request")
		return nil, nil
	})
	if err != nil || !captionComplete(batch) {
		t.Fatalf("comment recipe: %+v %v", batch, err)
	}

	item := batch.Recipes[0]

	source, _ := item.ImporterMetadata["source_text"].(string)
	if !slices.Contains(diagnosticCodes(item), "social-comment") || strings.Contains(source, "cream") {
		t.Fatalf("recipe came from another commenter: %v %q", diagnosticCodes(item), item.ImporterMetadata["source_text"])
	}

	if item.SourceKey != "https://social.example/cook/posts/garlic-soup/123456789/" {
		t.Fatalf("source key = %q", item.SourceKey)
	}
}

func TestSocialPostContinuesCaptionWithAuthorComment(t *testing.T) {
	data := `{"story":{"post_id":"123456789","actors":[{"id":"42"}],"message":{"text":"Garlic soup\n\nIngredients:\n2 cups stock\n1 head garlic"}},
"c":{"__typename":"Comment","depth":0,"author":{"id":"42"},"body":{"text":"Directions:\nSimmer the garlic in the stock.\nBlend until smooth."}}}`
	page := webrecipe.Load(socialStoryPage("https://social.example/cook/posts/garlic-soup/123456789/", data), "text/html", "https://social.example/cook/posts/garlic-soup/123456789/")

	batch, err := fetchSharedRecipe(t.Context(), page, func(context.Context, string) (*webrecipe.Page, error) { return nil, errors.New("offline") })
	if err != nil || !captionComplete(batch) || len(batch.Recipes[0].Document.IngredientSections[0].Ingredients) != 2 {
		t.Fatalf("split recipe: %+v %v", batch, err)
	}
}

func TestShortVideoReadsCommentsFromItsCanonicalPage(t *testing.T) {
	const (
		reel      = "https://social.example/reel/555555555"
		canonical = "https://social.example/cook/videos/garlic-soup/555555555/"
	)

	reelData := `{"video":{"id":"555555555","owner":{"id":"42"},"creation_story":{"message":{"text":"Garlic soup tonight! #dinner"}}}}`
	videoData := `{"data":{"id":"555555555","owner":{"id":"42"},"creation_story":{"message":{"text":"Garlic soup tonight! #dinner"}}},
"c":{"__typename":"Comment","depth":0,"author":{"id":"42"},"body":{"text":"Full recipe: https://cook.example/garlic-soup/"}}}`
	recipe := `{"@type":"Recipe","name":"Garlic Soup","recipeIngredient":["2 cups stock","1 head garlic"],"recipeInstructions":["Simmer.","Blend."]}`

	fetch, requested := pageFetcher(t, map[string]*webrecipe.Page{
		reel:                                webrecipe.Load(socialStoryPage(canonical, reelData), "text/html", reel),
		canonical:                           webrecipe.Load(socialStoryPage(reel+"/", videoData), "text/html", canonical),
		"https://cook.example/garlic-soup/": webrecipe.Load([]byte(recipe), "application/ld+json", "https://cook.example/garlic-soup/"),
	})

	batch, err := fetchWebRecipe(t.Context(), reel, fetch)
	if err != nil || !captionComplete(batch) || batch.Recipes[0].Document.Title != "Garlic Soup" {
		t.Fatalf("linked recipe from the canonical page: %+v %v", batch, err)
	}

	if !slices.Equal(*requested, []string{reel, canonical, "https://cook.example/garlic-soup/"}) {
		t.Fatalf("requests = %q", *requested)
	}
}

func TestSocialDraftFindsRecipeOnCreatorWebsite(t *testing.T) {
	const (
		post      = "https://www.facebook.com/reel/555555555"
		canonical = "https://www.facebook.com/GarlicHouse/videos/slow-cooker-garlic-soup/555555555/"
		profile   = "https://www.facebook.com/GarlicHouse/"
	)

	data := `{"video":{"id":"555555555","owner":{"id":"42"},"creation_story":{"message":{"text":"Cozy night in! #soup"}}},
"c":{"__typename":"Comment","depth":0,"author":{"id":"7"},"body":{"text":"Yum"}}}`
	profilePage := `<script type="application/json">{"a":{"external_url":"https:\/\/videos.example\/@garlichouse"},"b":{"external_url":"https:\/\/linktree.example\/garlichouse"},"c":{"external_url":"https:\/\/www.garlichouse.example\/"}}</script>`
	sitemap := `<urlset><url><loc>https://garlichouse.example/blog/slow-cooker-garlic-bread/</loc></url><url><loc>https://garlichouse.example/blog/slow-cooker-garlic-soup/</loc></url></urlset>`
	recipe := `{"@type":"Recipe","name":"Slow Cooker Garlic Soup","recipeIngredient":["2 cups stock","1 head garlic"],"recipeInstructions":["Simmer.","Blend."]}`

	fetch, requested := pageFetcher(t, map[string]*webrecipe.Page{
		post:    webrecipe.Load(socialStoryPage(canonical, data), "text/html", post),
		profile: webrecipe.Load([]byte(profilePage), "text/html", profile),
		"https://www.garlichouse.example/robots.txt":                {URL: "https://garlichouse.example/robots.txt", ContentType: "text/plain", Body: []byte("Sitemap: https://garlichouse.example/sitemap.xml\n")},
		"https://garlichouse.example/sitemap.xml":                   webrecipe.Load([]byte(sitemap), "application/xml", "https://garlichouse.example/sitemap.xml"),
		"https://garlichouse.example/blog/slow-cooker-garlic-soup/": webrecipe.Load([]byte(recipe), "application/ld+json", "https://garlichouse.example/blog/slow-cooker-garlic-soup/"),
	})

	batch, err := fetchWebRecipe(t.Context(), post, fetch)
	if err != nil || !captionComplete(batch) || batch.Recipes[0].Document.Title != "Slow Cooker Garlic Soup" {
		t.Fatalf("creator recipe: %+v %v", batch, err)
	}

	if item := batch.Recipes[0]; !slices.Contains(diagnosticCodes(item), "social-creator-recipe") {
		t.Fatalf("diagnostics = %v", diagnosticCodes(item))
	}

	for _, address := range *requested {
		if strings.Contains(address, "videos.example") || strings.Contains(address, "linktree.example") {
			t.Fatalf("searched a profile link that does not carry the account name: %q", *requested)
		}
	}
}

func TestBestSlugMatchRejectsOtherDishes(t *testing.T) {
	site := []string{
		"https://cook.example/blog/crockpot-garlic-parmesan-chicken-potatoes-recipe/",
		"https://cook.example/blog/crockpot-parmesan-garlic-chicken-spaghetti-recipe/",
		"https://cook.example/blog/crockpot-chicken-tacos-recipe/",
		"https://cook.example/blog/white-cheddar-mac-and-cheese-recipe/",
		"https://cook.example/blog/the-creamiest-baked-mac-and-cheese.html",
		"https://cook.example/blog/chicken-noodles-recipe/",
		"https://cook.example/blog/hawaiian-macaroni-salad-recipe/",
		"https://cook.example/blog/easy-beef-enchiladas-recipe/",
	}

	for query, want := range map[string]string{
		"crockpot parmesan garlic chicken potatoes":                                        site[0],
		"Creamy Baked Mac and Cheese for Thanksgiving Sides Series":                        site[4],
		"crockpot parmesan garlic chicken sandwiches":                                      "",
		"one pan chicken with buttered noodles perfect for busy crockpot enchiladas salad": "",
		"Crema de calabaza con jengibre":                                                   "",
	} {
		if got := bestSlugMatch(site, [][]string{nameWords(query)}); got != want {
			t.Errorf("%q matched %q, want %q", query, got, want)
		}
	}
}

func captionPage(caption string) *webrecipe.Page {
	body := `<script type="application/ld+json">{"@type":"VideoObject","name":"Dinner","description":` + strconv.Quote(caption) + `}</script>`
	return webrecipe.Load([]byte(body), "text/html", "https://video.example/watch?v=abc")
}

func TestOneSidedCaptionsImportAsPartialRecipes(t *testing.T) {
	offline := func(context.Context, string) (*webrecipe.Page, error) { return nil, errors.New("offline") }

	for name, tc := range map[string]struct {
		caption            string
		ingredients, steps int
	}{
		"labelled ingredients": {"Is it any good?\n\nTini's Mac and Cheese Ingredients:\n- 1 lb pasta (450g)\n- 500 g cheddar\n- Salt and pepper to taste\n\nThe original video:\nhttps://video.example/watch?v=other\n\n0:00 - What are we making?\n0:32 - A special pasta shape\n#macandcheese", 3, 0},
		"unlabelled list":      {"So good!\n\nCrispy Chicken\n- 800g chicken breast\n- garlic powder\n- 20g plain flour\n- 60ml water", 4, 0},
		"numbered method":      {"Marry Me Gnocchi\n\n1. Cook the bacon until crispy.\n2. Season the chicken and brown it.\n3. Add the cream and simmer.", 0, 3},
		"keycap method":        {"Ratatouille\n\nLes ingrédients pour cette recette :\n\n- 1️⃣Cuire 45min au four 1 oignon, des poivrons rouges, jaunes et oranges, 5 gousses d’ail et 2 tomates, puis mixer.\n- 2️⃣Couper finement en rondelles 1 aubergine, 1 courgette jaune et verte et 2 tomates bien mûres.\n- 3️⃣Mettre la sauce sur le plateau avec les légumes, l’huile d’olive et les herbes de Provence.", 0, 3},
	} {
		t.Run(name, func(t *testing.T) {
			batch, err := fetchSharedRecipe(t.Context(), captionPage(tc.caption), offline)
			if err != nil {
				t.Fatal(err)
			}

			item := batch.Recipes[0]
			ingredients, steps := 0, 0

			for _, section := range item.Document.IngredientSections {
				ingredients += len(section.Ingredients)
			}

			for _, section := range item.Document.InstructionSections {
				steps += len(section.Steps)
			}

			if ingredients != tc.ingredients || steps != tc.steps || !slices.Contains(diagnosticCodes(item), "social-caption-partial") {
				t.Fatalf("got %d ingredients, %d steps, %v", ingredients, steps, diagnosticCodes(item))
			}
		})
	}
}

func TestCaptionWithoutListsStaysTextDraft(t *testing.T) {
	batch, err := fetchSharedRecipe(t.Context(), captionPage("Tortilla No. 8.\nJust like my grandma used to make.\n\n#tortilla"), func(context.Context, string) (*webrecipe.Page, error) {
		return nil, errors.New("offline")
	})
	if err != nil || len(batch.Recipes[0].Document.IngredientSections)+len(batch.Recipes[0].Document.InstructionSections) != 0 || !slices.Contains(diagnosticCodes(batch.Recipes[0]), "social-caption-draft") {
		t.Fatalf("draft: %+v %v", batch, err)
	}
}
