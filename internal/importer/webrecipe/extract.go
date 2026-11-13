package webrecipe

import (
	"context"
	"errors"
	"strings"
)

const maximumConcurrentExtractions = 2

var extractionSlots = make(chan struct{}, maximumConcurrentExtractions)

// Extract runs the whole pipeline against an already-loaded page.
//
// Enrichment runs before the recipe is judged usable, so a page whose
// structured data carries only a name and an image can still be imported when
// its recipe card holds the content.
func Extract(page *Page) (*Recipe, error) {
	return ExtractContext(context.Background(), page)
}

// ExtractContext runs extraction under cancellation, deterministic work
// budgets, and a small process-wide admission limit so hostile pages cannot
// monopolize every server worker concurrently.
func ExtractContext(ctx context.Context, page *Page) (*Recipe, error) {
	select {
	case extractionSlots <- struct{}{}:
		defer func() { <-extractionSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	parsed, err := parse(ctx, page)
	if err != nil {
		return nil, err
	}

	if err := parsed.check(); err != nil {
		return nil, err
	}

	found := parsed.candidates()
	if err := parsed.check(); err != nil {
		return nil, err
	}

	usedMicrodata := false

	if len(found) == 0 {
		var err error

		found, usedMicrodata, err = parsed.markupRecipe()
		if err != nil {
			return nil, err
		}
	}

	var result *Recipe

	switch {
	case len(found) > 0:
		chosen, _ := parsed.selectCandidate(found)

		normalized, err := parsed.normalize(chosen)
		if err != nil {
			return nil, failuref(NormalizationFailed, err, "this page's recipe could not be interpreted")
		}

		result = normalized
	case cardApplies(parsed):
		result = &Recipe{
			Name:     collapseSpace(parsed.pageHeading()),
			URL:      safeProvenanceURL(parsed.page.URL, parsed.page.URL),
			Warnings: append([]Warning(nil), parsed.warnings...),
		}
		result.warn("recipe-card-only", "This page publishes no structured recipe data. Savorum read its recipe card instead.")
	default:
		result, err = parsed.semanticRecipe()
		if err != nil {
			return nil, err
		}

		if result == nil {
			if parsed.allBroken() {
				return nil, failuref(MalformedStructuredData, nil,
					"this page's recipe data could not be read")
			}

			return nil, failuref(NoRecipeStructuredData, nil,
				"this page does not publish a recipe Savorum can read")
		}
	}

	if usedMicrodata {
		result.warn("microdata-fallback", "This page describes its recipe in an older way that carries less detail, so some of it may be missing.")
	}

	enrich(parsed, result)

	if err := parsed.completeFromArticle(result); err != nil {
		return nil, err
	}

	if len(result.Images) == 0 {
		result.Images = parsed.pageImages()
	}

	if err := parsed.check(); err != nil {
		return nil, err
	}

	if !result.Usable() && result.ReviewText == "" {
		return nil, failuref(RecipeIncomplete, nil,
			"this page names a recipe but does not publish its ingredients or method")
	}

	return result, nil
}

func (d *document) completeFromArticle(result *Recipe) error {
	if result.ReviewText != "" || result.ExtractionMethod == "semantic-html" || result.ingredientCount() > 0 && result.stepCount() > 0 {
		return nil
	}

	article, err := d.semanticRecipe()
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	if err == nil && article != nil && article.ReviewText == "" && sameTitle(result.Name, article.Name) {
		result.withArticle(article)
	} else if result.ingredientCount() > 0 && result.stepCount() == 0 {
		if sections := d.semanticMethod(); len(sections) > 0 {
			result.Sections = sections
			result.warn("instructions-enriched", "Instructions were read from the page's article.")
		}
	}

	return nil
}

func (r *Recipe) withArticle(article *Recipe) {
	switch {
	case r.ingredientCount() == 0 && r.stepCount() == 0:
		r.IngredientGroups, r.Sections, r.ExtractionMethod = article.IngredientGroups, article.Sections, article.ExtractionMethod
		r.Warnings = append(r.Warnings, article.Warnings...)
	case r.stepCount() == 0:
		r.Sections = article.Sections
		r.warn("instructions-enriched", "Instructions were read from the page's article.")
	default:
		r.IngredientGroups = article.IngredientGroups
		r.warn("ingredients-enriched", "Ingredients were read from the page's article.")
	}

	if len(r.Images) == 0 {
		r.Images, r.ImageProvenance = article.Images, article.ImageProvenance
	}
}

func (d *document) markupRecipe() ([]candidate, bool, error) {
	node, ok, err := d.microdataRecipe()

	switch {
	case err != nil:
		return nil, false, err
	case ok:
		return []candidate{{node: node}}, true, nil
	}

	if node, ok := d.hRecipe(); ok {
		return []candidate{{node: node}}, false, nil
	}

	return nil, false, nil
}

func sameTitle(first, second string) bool {
	first, second = strings.ToLower(collapseSpace(first)), strings.ToLower(collapseSpace(second))
	if first == "" || second == "" {
		return false
	}

	if strings.Contains(first, second) || strings.Contains(second, first) {
		return true
	}

	if strings.Contains(first, " ") && strings.Contains(second, " ") {
		return sharedStem(strings.Fields(first), strings.Fields(second), 2)
	}

	return sharedStem([]rune(first), []rune(second), 4)
}

func sharedStem[T comparable](a, b []T, minimum int) bool {
	shared := 0
	for shared < len(a) && shared < len(b) && a[shared] == b[shared] {
		shared++
	}

	return shared >= minimum && shared*5 >= min(len(a), len(b))*3
}

// FetchAndExtract is the URL entry point: the safe fetch and the extraction,
// with the fetched address carried into selection so that the recipe the page
// is about can be told from the recipes it merely mentions.
func FetchAndExtract(ctx context.Context, rawURL string) (*Recipe, error) {
	page, err := Fetch(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	return ExtractContext(ctx, page)
}

// Load prepares a page that did not come from a fetch, so an uploaded file
// goes through exactly the same pipeline as a URL.
func Load(data []byte, contentType, documentURL string) *Page {
	return &Page{
		URL: documentURL, Body: data, declaredContentType: contentType,
		ContentType: strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])),
	}
}
