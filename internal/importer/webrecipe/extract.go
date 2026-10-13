package webrecipe

import "context"

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
		if node, ok, err := parsed.microdataRecipe(); err != nil {
			return nil, err
		} else if ok {
			found = []candidate{{node: node}}
			usedMicrodata = true
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
	return &Page{URL: documentURL, ContentType: contentType, Body: data}
}
