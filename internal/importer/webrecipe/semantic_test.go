package webrecipe

import (
	"strings"
	"testing"
)

// These invented structural examples are available under CC0 1.0 Universal.
const semanticFixture = `<html lang="en"><head><link rel="canonical" href="https://example.test/broth">
<script type="application/ld+json">{"@type":"Article","headline":"Pantry broth",
"url":"https://example.test/broth","description":"An invented broth.","author":"Avery Cook",
"datePublished":"2025-02-03","image":{"@type":"ImageObject","url":"/broth.jpg","creditText":"Avery Cook"},
"recipeYield":"99 portions","totalTime":"PT99H","nutrition":{"calories":"999"}}</script></head>
<body><nav><p>Ingredients</p><ul><li>20 unrelated links</li></ul></nav><main><article>
<header><h1>Pantry broth</h1></header><p>A story mentions four hours and ten servings.</p>
<div><p><span><strong>Ingredients:</strong></span></p></div>
<p><strong>Base</strong></p><ul><li>2–3 carrots</li><li>1 1/2 cups water</li><li>Salt to taste</li></ul>
<p><strong>Finish</strong></p><ul><li>2 tsp oil</li></ul>
<div><p><strong>Instructions:</strong></p></div>
<ol><li><p>Combine the base in a pan.</p><p>Keep all of this continuation.</p>
<ul><li>Use a small pan.</li><li>Keep the lid close.</li></ul></li>
<li><p>Stir in the oil and serve with <a href="/bread?token=private#recipe">bread</a>.</p></li></ol>
<hr><p>Related recipes:</p><ul><li><a href="/other">Other soup</a></li></ul>
</article></main><footer><p>Instructions</p><ol><li>Browse our other recipes.</li></ol></footer></body></html>`

func TestSemanticArticleKeepsDOMGroupsAndStepOwnership(t *testing.T) {
	result := mustExtract(t, semanticFixture, "https://example.test/broth")
	if result.ExtractionMethod != "semantic-html" || result.MetadataSource != "article-json-ld" || result.Name != "Pantry broth" {
		t.Fatalf("reading provenance = %+v", result)
	}

	if len(result.IngredientGroups) != 2 || result.IngredientGroups[0].Title != "Base" || result.IngredientGroups[1].Title != "Finish" || len(allIngredients(result.IngredientGroups)) != 4 {
		t.Fatalf("groups = %+v", result.IngredientGroups)
	}

	steps := allSteps(result.Sections)
	if len(steps) != 2 || !strings.Contains(steps[0].Text, "\n\nKeep all of this continuation.") || !strings.Contains(steps[0].Text, "- Use a small pan.") {
		t.Fatalf("step boundaries = %+v", steps)
	}

	if !strings.Contains(steps[1].Text, "bread (https://example.test/bread)") || strings.Contains(result.ReviewText, "private") {
		t.Fatalf("step link = %q", steps[1].Text)
	}

	if result.Authors[0].Name != "Avery Cook" || result.Published != "2025-02-03" || result.Description != "An invented broth." || result.Images[0] != "https://example.test/broth.jpg" {
		t.Fatalf("metadata = %+v", result)
	}

	if len(result.Yields) != 0 || result.Nutrition != nil || result.Times.Total != "" || result.ImageProvenance[result.Images[0]].Credit != "Avery Cook" {
		t.Fatalf("unsupported metadata or missing image provenance = %+v", result)
	}
}

func TestSemanticArticleDoesNotReplaceStructuredRecipes(t *testing.T) {
	for _, structured := range []string{
		`<script type="application/ld+json">{"@type":"Recipe","name":"Structured winner","recipeIngredient":["water"],"recipeInstructions":["Stir."]}</script>`,
		`<div itemscope itemtype="https://schema.org/Recipe"><span itemprop="name">Structured winner</span><span itemprop="recipeIngredient">water</span><span itemprop="recipeInstructions">Stir.</span></div>`,
	} {
		body := strings.Replace(semanticFixture, "</article>", structured+"</article>", 1)

		result := mustExtract(t, body, "https://example.test/broth")
		if result.Name != "Structured winner" || result.ExtractionMethod != "" || len(allIngredients(result.IngredientGroups)) != 1 {
			t.Fatalf("structured precedence = %+v", result)
		}
	}
}

func TestSemanticArticleWeakAndCompetingCandidatesStayDrafts(t *testing.T) {
	fixtures := map[string]string{
		"missing method":      `<main><article><h1>Draft soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li><li>1 cup water</li><li>3 tsp oil</li></ul></article></main>`,
		"few ingredients":     `<main><article><h1>Draft soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li></ul><h2>Instructions</h2><ol><li>Mix the carrots well.</li><li>Serve the carrots warm.</li></ol></article></main>`,
		"metadata title only": strings.Replace(semanticFixture, `<h1>Pantry broth</h1>`, "", 1),
		"competing articles":  semanticFixture + semanticFixture,
		"split regions":       `<main><article><h1>One soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li><li>1 cup water</li><li>3 tsp oil</li></ul></article><article><h1>Another soup</h1><h2>Instructions</h2><ol><li>Mix the base well.</li><li>Serve the base warm.</li></ol></article></main>`,
		"repeated pairs":      strings.Replace(semanticFixture, "<hr>", `<h2>Ingredients</h2><ul><li>3 onions</li></ul><h2>Instructions</h2><ol><li>Mix another dish well.</li></ol><hr>`, 1),
		"unnumbered prose":    strings.Replace(semanticFixture, "<ol>", "<p>Consider the story of this dish.</p><ol>", 1),
		"plain ingredients":   `<main><article><h1>Unquantified soup</h1><h2>Ingredients</h2><ul><li>Water</li><li>Carrots</li><li>Salt</li></ul><h2>Instructions</h2><ol><li>Combine all ingredients well.</li><li>Serve the soup warm.</li></ol></article></main>`,
	}

	for name, body := range fixtures {
		t.Run(name, func(t *testing.T) {
			result := mustExtract(t, body, "https://example.test/broth")
			if result.ReviewText == "" || len(result.IngredientGroups)+len(result.Sections) != 0 || !hasWarning(result, "semantic-html-review") {
				t.Fatalf("uncertain recipe was assigned fields = %+v", result)
			}
		})
	}
}

func TestSemanticArticleExcludesNavigationHiddenAndRelatedContent(t *testing.T) {
	content := `<h1>Invented soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li><li>1 cup water</li><li>3 tsp oil</li></ul><h2>Instructions</h2><ol><li>Mix the base well.</li><li>Serve the base warm.</li></ol>`
	for _, container := range []string{"nav", "aside", "footer", `div hidden`, `div aria-hidden="true"`, `div style="display: none"`} {
		body := "<main><" + container + "><article>" + content + "</article></" + strings.Fields(container)[0] + "></main>"
		if got := extractFailure(t, body, "https://example.test/soup"); got != NoRecipeStructuredData {
			t.Fatalf("excluded %s: %s", container, got)
		}
	}

	body := `<main><article>` + content + `<h2>Related recipes</h2><ol><li>Read about another soup.</li><li>Visit our other recipes.</li></ol></article></main>`
	if result := mustExtract(t, body, "https://example.test/soup"); len(allSteps(result.Sections)) != 2 {
		t.Fatalf("related content included = %+v", result)
	}
}

func TestSemanticArticleNestedIngredientLabelsAndLocalizedMarkers(t *testing.T) {
	body := `<main><article><h1>Invented soup</h1><section><h2>Zutaten</h2><h3>Flüssigkeit</h3><ul><li>Basis<ul><li>2 Karotten</li><li>1 Liter Wasser</li><li>3 g Salz</li></ul></li></ul></section>
<section><h2>Zubereitung</h2><ol><li>Alle Zutaten gut vermischen.</li><li>Die Suppe warm servieren.</li></ol></section></article></main>`

	result := mustExtract(t, body, "https://example.test/soup")
	if len(result.IngredientGroups) != 1 || result.IngredientGroups[0].Title != "Flüssigkeit · Basis" || len(allSteps(result.Sections)) != 2 || result.Language != "" {
		t.Fatalf("nested/localized recipe = %+v", result)
	}
}

func TestSemanticArticleDoesNotBorrowMethodOutsideSection(t *testing.T) {
	body := `<main><article><h1>Invented soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li><li>1 cup water</li><li>3 tsp oil</li></ul><section><p>Instructions</p></section><ol><li>Read another story first.</li><li>Visit another recipe next.</li></ol></article></main>`

	result := mustExtract(t, body, "https://example.test/soup")
	if len(result.Sections) != 0 || !hasWarning(result, "semantic-html-review") {
		t.Fatalf("method borrowed across section = %+v", result)
	}
}

func TestSemanticArticleBoundsCandidates(t *testing.T) {
	body := "<main>" + strings.Repeat("<article><h1>Soup</h1><h2>Ingredients</h2><p>2 carrots</p></article>", maximumSemanticCandidates+1) + "</main>"
	if got := extractFailure(t, body, "https://example.test/soup"); got != ExtractionLimited {
		t.Fatalf("candidate budget = %s", got)
	}
}

func TestSemanticArticleTitleIntroducesSectionsAfterCompetingHeadings(t *testing.T) {
	body := `<main><article><h1>To make ahead</h1><p>Assemble the day before.</p><h1>Tips</h1><p>Shred your own cheese.</p>
<h2>Baked macaroni</h2><h3>Ingredients</h3><ul><li>1 lb pasta</li><li>2 cups milk</li><li>8 oz cheddar</li></ul>
<h3>Instructions</h3><ol><li>Boil the pasta until tender.</li><li>Bake the pasta with milk and cheddar.</li></ol></article></main>`

	result := mustExtract(t, body, "https://example.test/macaroni")
	if result.Name != "Baked macaroni" || len(allIngredients(result.IngredientGroups)) != 3 || len(allSteps(result.Sections)) != 2 {
		t.Fatalf("recipe section title = %+v", result)
	}

	competing := strings.Replace(body, "<h2>Baked macaroni</h2>", "", 1)
	if result := mustExtract(t, competing, "https://example.test/macaroni"); !hasWarning(result, "semantic-html-review") {
		t.Fatalf("competing titles without a section heading = %+v", result)
	}
}

func TestSemanticArticleReadsQualifiedHeadingsAndParagraphs(t *testing.T) {
	body := `<main><article><h1>Walnut rolls</h1>
<p>Ingredients for the dough:</p><p>2 cups oil<br>1 kg flour<br>1 tsp baking powder</p>
<p>Ingredients for the filling:</p><p>200 g walnuts<br>2 tbsp sugar</p>
<p>Method:</p><p>Mix the oil, flour, and baking powder into a soft dough.</p><p>Fill each piece of dough and bake for 25 minutes.</p>
<p>Notes</p><p>Keep the rolls in a tin for a week.</p>
<p><a href="/rolls">Rolls</a> <a href="/baking">Baking</a></p></article></main>`

	result := mustExtract(t, body, "https://example.test/rolls")
	if result.ReviewText != "" || len(result.IngredientGroups) != 2 ||
		result.IngredientGroups[0].Title != "Ingredients for the dough" || result.IngredientGroups[1].Title != "Ingredients for the filling" {
		t.Fatalf("groups = %+v, review %q", result.IngredientGroups, result.ReviewText)
	}

	if steps := allSteps(result.Sections); len(steps) != 2 || !strings.HasPrefix(steps[1].Text, "Fill each piece") {
		t.Fatalf("steps = %+v", steps)
	}
}

func TestSemanticArticleReadsTrailingAmountsAndTitledSteps(t *testing.T) {
	body := `<main><article><h1>なすの田楽</h1>
<h2>なす田楽の材料（２人分）</h2><ul><li>なす … 2本</li><li>砂糖 … 大さじ3</li><li>みそ … 大さじ2</li></ul>
<h2>なす田楽の作り方</h2>
<h3>なすの切り方</h3><p>なすはヘタを切り落とし、３㎝幅に切ります。</p><ul><li><img src="/cut.jpg"></li></ul>
<h3>みそだれの作り方</h3><p>鍋に砂糖とみそを入れて、弱めの中火にかけます。</p>
<h2>【補足】</h2><ul><li>フライパンで焼いてもできます。</li></ul></article></main>`

	result := mustExtract(t, body, "https://example.test/dengaku")
	if result.ReviewText != "" || len(allIngredients(result.IngredientGroups)) != 3 {
		t.Fatalf("ingredients = %+v, review %q", result.IngredientGroups, result.ReviewText)
	}

	if len(result.Sections) != 2 || result.Sections[0].Title != "なすの切り方" || result.Sections[1].Title != "みそだれの作り方" || len(allSteps(result.Sections)) != 2 {
		t.Fatalf("sections = %+v", result.Sections)
	}
}

func TestSemanticArticleCompletesARecipeThatNamesItself(t *testing.T) {
	body := `<script type="application/ld+json">{"@type":"Recipe","name":"Rocky road","image":"https://example.test/rocky.jpg","aggregateRating":{"ratingValue":"5"}}</script>` +
		`<main><article><h1>Rocky road</h1><h2>Ingredients</h2><ul><li>115 g butter</li><li>200 g sugar</li><li>2 eggs</li></ul>` +
		`<h2>Instructions</h2><ol><li>Melt the butter with the sugar.</li><li>Beat in the eggs and bake for 22 minutes.</li></ol></article></main>`

	result := mustExtract(t, body, "https://example.test/rocky")
	if result.Name != "Rocky road" || len(allIngredients(result.IngredientGroups)) != 3 || len(allSteps(result.Sections)) != 2 ||
		result.ExtractionMethod != "semantic-html" || len(result.Images) != 1 {
		t.Fatalf("completed recipe = %+v", result)
	}
}

func TestSemanticArticleReadsAMethodItDoesNotLabel(t *testing.T) {
	body := `<main><article><h1>Layer cake</h1>
<p><u>Ingredients for the sponge:</u></p><ul><li>4 eggs</li><li>120 g sugar</li><li>80 g flour</li></ul>
<p>Whisk the eggs with the sugar until pale and thick.</p><p>Fold in the flour and bake for 30 minutes.</p>
<p><u>Ingredients for the cream:</u></p><ul><li>250 g mascarpone</li><li>2 tbsp sugar</li></ul>
<p>Beat the mascarpone with the sugar until smooth.</p></article></main>`

	result := mustExtract(t, body, "https://example.test/cake")
	if result.ReviewText != "" || len(allIngredients(result.IngredientGroups)) != 5 || len(allSteps(result.Sections)) != 3 {
		t.Fatalf("groups %+v, sections %+v, review %q", result.IngredientGroups, result.Sections, result.ReviewText)
	}
}

func TestSemanticArticleReadsAnUnfamiliarMethodHeading(t *testing.T) {
	body := `<main><article><h1>Invented soup</h1><h2>Ingredients</h2><ul><li>2 carrots</li><li>1 litre water</li><li>3 g salt</li></ul>` +
		`<h2>Let's cook</h2><ol><li>Mix everything in the pan.</li><li>Cook the soup and serve warm.</li></ol>` +
		`<h2>Serving ideas</h2><p>Serve with bread.</p></article></main>`

	result := mustExtract(t, body, "https://example.test/soup")
	if result.ReviewText != "" || len(allSteps(result.Sections)) != 2 {
		t.Fatalf("sections %+v, review %q", result.Sections, result.ReviewText)
	}
}

func TestSemanticArticleTreatsParagraphsBeforeALabelledMethodAsNotes(t *testing.T) {
	body := `<main><article><h1>Pickled celery</h1><h2>Ingredients</h2><ul><li>2 celery stalks</li><li>3 tbsp vinegar</li><li>1 tsp salt</li></ul>` +
		`<p>Use the freshest celery you can find for the best crunch.</p>` +
		`<h2>Pickling instructions</h2><ol><li>Slice the celery thinly.</li><li>Mix with the vinegar and salt, then chill.</li></ol></article></main>`

	result := mustExtract(t, body, "https://example.test/celery")
	if steps := allSteps(result.Sections); result.ReviewText != "" || len(steps) != 2 || !strings.HasPrefix(steps[0].Text, "Slice") {
		t.Fatalf("steps %+v, review %q", steps, result.ReviewText)
	}
}

func TestSemanticArticleDoesNotNameARecipeAfterALabel(t *testing.T) {
	body := `<h1>Easy flatbread</h1><main><article><h3>Nutrition per piece:</h3><p>120 kcal</p>` +
		`<h2>Ingredients</h2><ul><li>500 g flour</li><li>10 g yeast</li><li>300 ml water</li></ul>` +
		`<h2>Instructions</h2><ol><li>Mix everything into a soft dough.</li><li>Bake the flatbread for 20 minutes.</li></ol></article></main>`

	if result := mustExtract(t, body, "https://example.test/flatbread"); result.Name != "Easy flatbread" || result.ReviewText != "" {
		t.Fatalf("name %q, review %q", result.Name, result.ReviewText)
	}
}

func TestSemanticArticleReadsASecondIngredientLabelAsAGroup(t *testing.T) {
	body := `<div class="post hentry"><h3>Garlic mushrooms</h3><div>材料：<ul><li>鮮冬菇 300克</li><li>蒜頭 4粒</li><li>葱 2支</li></ul>` +
		`調味料：<ul><li>生抽 1湯匙</li><li>糖 1茶匙</li></ul>做法：<ol><li>冬菇洗淨，切片備用，蒜頭切碎。</li><li>燒熱油，爆香蒜頭，加入冬菇炒熟即成。</li></ol></div></div>`

	result := mustExtract(t, body, "https://example.test/mushrooms")
	if result.ReviewText != "" || len(result.IngredientGroups) != 2 || result.IngredientGroups[1].Title != "調味料：" && result.IngredientGroups[1].Title != "調味料" {
		t.Fatalf("groups %+v, review %q", result.IngredientGroups, result.ReviewText)
	}
}

func TestSemanticArticleReadsStepsUnderTheirOwnHeadings(t *testing.T) {
	body := `<main><article><h1>Matcha crepe cake</h1><h3>Nguyên liệu</h3><ul><li>240g bột mì</li><li>90g đường trắng</li><li>500ml sữa tươi</li></ul>` +
		`<h3>Làm bánh</h3><h4>Bước 1 : Làm phần bột</h4><p>Cho bột mì và đường vào tô, khuấy đều.</p><ul><li>Thêm sữa tươi từ từ và khuấy đều tay.</li></ul>` +
		`<h4>Bước 2 : Tráng bánh</h4><p>Tráng từng lớp bánh mỏng trên chảo chống dính.</p></article></main>`

	result := mustExtract(t, body, "https://example.test/crepe")
	if result.ReviewText != "" || len(allSteps(result.Sections)) != 3 {
		t.Fatalf("sections %+v, review %q", result.Sections, result.ReviewText)
	}
}

func TestSemanticArticleCompletesAMethodForARecordWithIngredientsOnly(t *testing.T) {
	body := `<script type="application/ld+json">{"@type":"Recipe","name":"Barley salad","ingredients":["200 g barley","1 cucumber","2 tbsp oil"]}</script>` +
		`<main><article><h1>A summer story</h1><p>We ate this salad on the porch.</p>` +
		`<h2>Instructions</h2><ol><li>Boil the barley for 25 minutes and drain it.</li><li>Dice the cucumber and toss everything with the oil.</li></ol></article></main>`

	result := mustExtract(t, body, "https://example.test/barley")
	if len(allIngredients(result.IngredientGroups)) != 3 || len(allSteps(result.Sections)) != 2 || !hasWarning(result, "instructions-enriched") {
		t.Fatalf("recipe = %+v", result)
	}
}

func TestSemanticArticleCompletesARecordWhoseTitleIsShorter(t *testing.T) {
	body := `<script type="application/ld+json">{"@type":"Recipe","name":"Plum crumble","recipeIngredient":["500 g plums","100 g flour","80 g butter"]}</script>` +
		`<main><article><h1>Plum crumble with oats</h1><h2>Ingredients</h2><ul><li>500 g plums</li><li>100 g flour</li><li>80 g butter</li></ul>` +
		`<h2>Instructions</h2><ol><li>Halve the plums and lay them in a dish.</li><li>Rub the flour with the butter, scatter it over and bake for 35 minutes.</li></ol></article></main>`

	result := mustExtract(t, body, "https://example.test/crumble")
	if result.Name != "Plum crumble" || len(allSteps(result.Sections)) != 2 {
		t.Fatalf("recipe = %+v", result)
	}
}

func TestSemanticArticleReadsBulletedParagraphIngredients(t *testing.T) {
	body := `<main><article><h1>Green pesto</h1><h2>Ingredients</h2><p>- 50 g basil<br>- 30 g pine nuts<br>- 100 ml olive oil</p>` +
		`<h2>Instructions</h2><ol><li>Toast the pine nuts in a dry pan.</li><li>Blend everything with the oil until smooth.</li></ol></article></main>`

	result := mustExtract(t, body, "https://example.test/pesto")
	if result.ReviewText != "" || len(allIngredients(result.IngredientGroups)) != 3 {
		t.Fatalf("groups %+v, review %q", result.IngredientGroups, result.ReviewText)
	}
}

func TestSemanticArticleReadsAShortNumberedLineAsAStepTitle(t *testing.T) {
	body := `<main><article><h1>Fried rice</h1><h2>Ingredients</h2><ul><li>300 g rice</li><li>2 eggs</li><li>2 tbsp soy sauce</li></ul>` +
		`<h2>Instructions</h2><p>1. Prepare</p><p>Cook the rice a day ahead and chill it overnight.</p>` +
		`<p>2. Fry</p><p>Scramble the eggs, add the rice and fry with the soy sauce.</p></article></main>`

	result := mustExtract(t, body, "https://example.test/rice")
	if result.ReviewText != "" || len(result.Sections) != 2 || result.Sections[0].Title != "Prepare" {
		t.Fatalf("sections %+v, review %q", result.Sections, result.ReviewText)
	}
}

func TestSemanticArticleDoesNotReadADurationLabelAsASection(t *testing.T) {
	body := `<main><article><h1>Lentil soup</h1><p>Preparation</p><p>15 minutes</p>` +
		`<h2>Ingredients</h2><ul><li>200 g lentils</li><li>1 onion</li><li>1 litre stock</li></ul>` +
		`<h2>Preparation</h2><ol><li>Chop the onion and soften it in a pot.</li><li>Add the lentils and stock and simmer for 30 minutes.</li></ol></article></main>`

	result := mustExtract(t, body, "https://example.test/lentils")
	if result.ReviewText != "" || len(allSteps(result.Sections)) != 2 {
		t.Fatalf("sections %+v, review %q", result.Sections, result.ReviewText)
	}
}

func TestSemanticArticleReadsStepNumbersInSeveralStyles(t *testing.T) {
	body := `<main><article><h1>Bean stew</h1><h2>Ingredients</h2><ul><li>400 g beans</li><li>1 onion</li><li>500 ml stock</li></ul>` +
		`<h2>Instructions</h2><p>1</p><p>①Soften the onion in a large pot.</p><p>(https://example.test/step.jpg)</p>` +
		`<p>2- Add the beans and the stock.</p><p>3.- Simmer for 40 minutes and season.</p></article></main>`

	result := mustExtract(t, body, "https://example.test/beans")
	if steps := allSteps(result.Sections); result.ReviewText != "" || len(steps) != 3 || !strings.HasPrefix(steps[0].Text, "Soften") {
		t.Fatalf("steps %+v, review %q", steps, result.ReviewText)
	}
}

func TestSemanticArticleReadsUnnumberedStepTitles(t *testing.T) {
	body := `<main><article><h1>Pumpkin pie</h1><h2>Ingredients</h2><ul><li>500 g pumpkin</li><li>200 g sugar</li><li>3 eggs</li></ul>` +
		`<h2>Instructions</h2><p>Filling</p><p>Purée the pumpkin and whisk it with the sugar and eggs.</p>` +
		`<p>Baking</p><p>Pour the filling into the crust and bake for 50 minutes.</p></article></main>`

	result := mustExtract(t, body, "https://example.test/pie")
	if result.ReviewText != "" || len(result.Sections) != 2 || result.Sections[1].Title != "Baking" {
		t.Fatalf("sections %+v, review %q", result.Sections, result.ReviewText)
	}
}

func TestSemanticArticleEndsIngredientLinesAtProse(t *testing.T) {
	body := `<main><article><h1>Chia pudding</h1><p>Ingredients:</p><p>400 ml milk<br>4 tbsp chia seeds<br>2 tbsp honey<br>Olive oil q.b.</p>` +
		`<p>Stir the chia seeds into the milk with the honey, then chill it overnight.</p>` +
		`<p>Serve the pudding cold with fresh fruit on top.</p><p>Enjoy!</p></article></main>`

	result := mustExtract(t, body, "https://example.test/chia")
	if result.ReviewText != "" || len(allIngredients(result.IngredientGroups)) != 4 || len(allSteps(result.Sections)) != 2 {
		t.Fatalf("groups %+v, sections %+v, review %q", result.IngredientGroups, result.Sections, result.ReviewText)
	}
}

func TestSemanticArticleKeepsAnUnnumberedParagraphBetweenNumberedSteps(t *testing.T) {
	body := `<main><article><h1>Strawberry crêpes</h1><p>Preparation 15 minutes</p><h2>Ingredients</h2><ul><li>1 cup flour</li><li>2 eggs</li><li>6 strawberries</li></ul>` +
		`<h2>Instructions</h2><p>1. Whisk the flour with the eggs into a batter.</p><p>2. Slice the strawberries.</p>` +
		`<p>Simmer them with a little sugar until soft.</p><p>3. Fry thin crêpes and fill them with the strawberries.</p>` +
		`<p>Thanks for reading this blog.</p></article></main>`

	result := mustExtract(t, body, "https://example.test/crepes")
	if steps := allSteps(result.Sections); result.ReviewText != "" || len(steps) != 4 {
		t.Fatalf("steps %+v, review %q", steps, result.ReviewText)
	}
}

func TestSemanticArticleSkipsLeadInsAndRelatedLinksInTheMethod(t *testing.T) {
	body := `<main><article><h1>Raspberry sorbet</h1><h2>Ingredients</h2><ul><li>500 g raspberries</li><li>150 g sugar</li><li>200 ml water</li></ul>` +
		`<h2>Raspberry sorbet method:</h2><p>Watch the video here:</p><p><a href="/subscribe">Subscribe to my channel</a></p>` +
		`<p>1. Boil the water and sugar for 2 minutes.</p><p>2. Purée the raspberries and strain them.</p>` +
		`<p><a href="/other">Don't miss: chocolate sorbet</a></p><p>3. Churn everything in an ice cream maker.</p>` +
		`<dl><dt>Tags</dt><dd>sorbet</dd></dl></article></main>`

	result := mustExtract(t, body, "https://example.test/sorbet")
	if steps := allSteps(result.Sections); result.ReviewText != "" || len(steps) != 3 || !strings.HasPrefix(steps[0].Text, "Boil") {
		t.Fatalf("steps %+v, review %q", steps, result.ReviewText)
	}
}

func TestSemanticArticleTitlesAMethodPartAfterItsIngredientGroup(t *testing.T) {
	body := `<main><article><h1>Ginger smash</h1><h2>Ingredients</h2><ul><li>4 cl ginger beer</li><li>2 cl lime juice</li></ul>` +
		`<p><strong>Rosemary sugar</strong></p><ul><li>2 tbsp sugar</li><li>1 sprig rosemary</li></ul>` +
		`<h2>Instructions</h2><p>Fill the glass with ice and pour in the ginger beer and lime.</p>` +
		`<p>Rosemary sugar</p><p>Chop the rosemary finely and mix it with the sugar.</p></article></main>`

	result := mustExtract(t, body, "https://example.test/smash")
	if result.ReviewText != "" || len(result.Sections) != 2 || result.Sections[1].Title != "Rosemary sugar" {
		t.Fatalf("sections %+v, review %q", result.Sections, result.ReviewText)
	}
}

func TestSemanticArticleDiscountsALabelThatIntroducesNothing(t *testing.T) {
	body := `<main><h1>Strawberry slice</h1><h2>Preparation difficulty</h2><h3>Easy</h3><h2>Price</h2><h3>Moderate</h3>` +
		`<p>Ingredients:</p><ul><li>2 packets custard powder</li><li>1 litre milk</li><li>200 g butter</li></ul>` +
		`<p>Preparation:</p><p>Cook the custard in the milk and let it cool before beating in the butter.</p>` +
		`<p>Split the sponge in two and fill it with the cream.</p></main>`

	result := mustExtract(t, body, "https://example.test/slice")
	if result.ReviewText != "" || len(allSteps(result.Sections)) != 2 {
		t.Fatalf("sections %+v, review %q", result.Sections, result.ReviewText)
	}
}

func TestSemanticArticleReadsIngredientsSetOutInATable(t *testing.T) {
	body := `<main><article><h1>Pancakes</h1><h2>Ingredients</h2><table><tr><th colspan="2">Batter</th></tr>` +
		`<tr><td>Flour</td><td>250 g</td></tr><tr><td>Milk</td><td>500 ml</td></tr><tr><td>Eggs</td><td>3</td></tr></table>` +
		`<h2>Instructions</h2><ol><li>Whisk everything into a smooth batter.</li><li>Fry thin pancakes in a hot pan.</li></ol></article></main>`

	result := mustExtract(t, body, "https://example.test/pancakes")
	if result.ReviewText != "" || len(result.IngredientGroups) != 1 || result.IngredientGroups[0].Title != "Batter" ||
		len(allIngredients(result.IngredientGroups)) != 3 {
		t.Fatalf("groups %+v, review %q", result.IngredientGroups, result.ReviewText)
	}
}

func TestSemanticArticleLeavesAWideTableForReview(t *testing.T) {
	body := `<main><article><h1>Omelette</h1><h2>Ingredients</h2><table><tr><td>1</td><td>Eggs</td><td>60</td><td>16</td></tr>` +
		`<tr><td>2</td><td>Prawns</td><td>40</td><td>11</td></tr><tr><td>3</td><td>Spring onion</td><td>20</td><td>5</td></tr></table>` +
		`<h2>Instructions</h2><ol><li>Beat the eggs with the prawns.</li><li>Fry the omelette until golden.</li></ol></article></main>`

	if result := mustExtract(t, body, "https://example.test/omelette"); result.ReviewText == "" {
		t.Fatalf("a data sheet was read as ingredients: %+v", result.IngredientGroups)
	}
}

func TestSemanticArticleTitlesAListAfterAShortLineBetweenLists(t *testing.T) {
	body := `<main><article><h1>Fig pinsa</h1><h3>Ingredients</h3><ul><li>1 pinsa base</li><li>100 g gorgonzola</li><li>40 g walnuts</li></ul>` +
		`<div>After baking</div><ul><li>50 g ham</li><li>3 figs</li></ul>` +
		`<h3>Instructions</h3><ol><li>Bake the base with the cheese and walnuts.</li><li>Top with the ham and figs.</li></ol></article></main>`

	result := mustExtract(t, body, "https://example.test/pinsa")
	if len(result.IngredientGroups) != 2 || result.IngredientGroups[1].Title != "After baking" || len(allIngredients(result.IngredientGroups)) != 5 {
		t.Fatalf("groups %+v, review %q", result.IngredientGroups, result.ReviewText)
	}
}
