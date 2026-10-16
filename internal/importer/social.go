package importer

import (
	"context"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/savorum/savorum/internal/importer/webrecipe"
)

var forumPostPath = regexp.MustCompile(`^/r/[A-Za-z0-9_]+/comments/[a-z0-9]+(?:/[^/]+)?/?$`)

func postFeedURL(address string) string {
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || !forumPostPath.MatchString(u.Path) || strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/.rss") {
		return ""
	}

	switch strings.ToLower(u.Host) {
	case "reddit.com", "www.reddit.com", "old.reddit.com", "np.reddit.com", "m.reddit.com":
		// Reddit exposes post bodies as Atom at the permalink's .rss endpoint.
		u.Scheme, u.Host = "https", "www.reddit.com"
		u.Path = strings.TrimRight(u.Path, "/") + "/.rss"
		u.RawPath, u.RawQuery, u.Fragment = "", "", ""

		return u.String()
	}

	return ""
}

func decodeWebPage(ctx context.Context, page *webrecipe.Page) (ImportBatch, *webrecipe.Post, error) {
	extracted, err := webrecipe.ExtractContext(ctx, page)
	if err == nil {
		return batchFor(extracted), nil, nil
	}

	if !slices.Contains([]webrecipe.FailureCode{webrecipe.NoRecipeStructuredData, webrecipe.RecipeIncomplete, webrecipe.MalformedStructuredData}, webrecipe.CodeOf(err)) {
		return ImportBatch{}, nil, err
	}

	post, postErr := webrecipe.ExtractPostContext(ctx, page)
	if postErr != nil {
		return ImportBatch{}, nil, postErr
	}

	if post == nil {
		return ImportBatch{}, nil, err
	}

	for _, text := range post.Texts {
		batch, parseErr := captionBatch(ctx, page.URL, post, text)
		if parseErr == nil && captionComplete(batch) {
			return batch, post, nil
		}
	}

	return ImportBatch{}, post, err
}

func captionComplete(batch ImportBatch) bool {
	if len(batch.Recipes) != 1 {
		return false
	}

	document := batch.Recipes[0].Document

	return len(document.IngredientSections) > 0 && len(document.InstructionSections) > 0
}

func captionBatch(ctx context.Context, address string, post *webrecipe.Post, text string) (ImportBatch, error) {
	if err := ctx.Err(); err != nil {
		return ImportBatch{}, err
	}

	if post.URL != "" {
		address = post.URL
	}

	title := strings.TrimSpace(post.Title)
	if title == "" {
		title = "Shared recipe"
	}

	batch, err := (plainTextAdapter{}).Import(ctx, Input{Name: "Social recipe.txt", URL: address, Data: []byte(title)}, Options{})
	if err != nil {
		return ImportBatch{}, err
	}

	item := &batch.Recipes[0]
	item.Document.Title = strings.TrimSpace(post.Title)
	parser := plainTextParser{item: item, mode: plainModeNotes, requireHeadings: true}
	parser.parse(strings.Split(normalizePlainText([]byte(text)), "\n"), 0)

	if !captionComplete(batch) {
		inferRecipeSections(item, text, post.SourceLocale)
	}

	if item.Document.Title == "" {
		item.Document.Title = title
	}

	item.Document.Language = post.Language
	item.Document.SourceLocale = post.SourceLocale
	batch.Source = ImportSource{Format: webRecipeAdapter{}.Format()}
	item.Assets = webAssets(&webrecipe.Recipe{Images: post.Images}, item.Document.Title)

	item.ImporterMetadata["source_text"] = text
	if post.LocaleEvidence != "" {
		item.ImporterMetadata["source_locale_evidence"] = post.LocaleEvidence
	}

	item.Diagnostics = append(item.Diagnostics, ReviewIngredientMeasurements(&item.Document)...)
	if address != "" {
		item.SourceKey = address
	}

	item.Diagnostics = append(item.Diagnostics, Diagnostic{
		Code: "social-caption", Class: DiagnosticInformational, Severity: SeverityInfo,
		Message: "Imported from the public caption. Video and audio were not read.",
	})

	return batch, nil
}

func captionDraft(ctx context.Context, address string, post *webrecipe.Post, originalErr error) (ImportBatch, error) {
	if post == nil || strings.TrimSpace(post.AuthoredText) == "" {
		return ImportBatch{}, originalErr
	}

	batch, err := captionBatch(ctx, address, post, post.AuthoredText)
	if err != nil {
		return ImportBatch{}, err
	}

	item := &batch.Recipes[0]
	// An incomplete section model can misclassify a heading or numbered step
	// as an ingredient, especially across languages. Keep a text-only draft.
	item.Document.Notes = []string{post.AuthoredText}
	item.Document.IngredientSections = nil
	item.Document.InstructionSections = nil
	item.Diagnostics = slices.DeleteFunc(item.Diagnostics, func(d Diagnostic) bool {
		return d.Code == "ingredient-review-required" || d.Code == "plain-text-uncertain"
	})
	item.Diagnostics = append(item.Diagnostics, Diagnostic{
		Code: "social-caption-draft", Class: DiagnosticActionable, Severity: SeverityWarning,
		Area: "source", Count: 2,
		Message: "Add ingredients and steps to finish this draft.",
	})

	return batch, nil
}

// fetchSharedRecipe follows at most three explicit links from a public caption.
// Every request retains the normal outbound safety policy. Linked structured
// recipes take priority over inferred captions and incomplete drafts.
func fetchSharedRecipe(ctx context.Context, page *webrecipe.Page, fetch func(context.Context, string) (*webrecipe.Page, error)) (ImportBatch, error) {
	batch, post, originalErr := decodeWebPage(ctx, page)
	if originalErr == nil && post == nil && !captionComplete(batch) {
		post, _ = webrecipe.ExtractPostContext(ctx, page)
	}

	if post != nil && post.URL != "" {
		page = webrecipe.Load(page.Body, page.ContentType, post.URL)
	}

	inferred := len(batch.Recipes) == 1 && slices.ContainsFunc(batch.Recipes[0].Diagnostics, func(d Diagnostic) bool { return d.Code == "text-sections-inferred" })
	if post == nil || (originalErr == nil && !inferred && captionComplete(batch)) {
		return batch, originalErr
	}

	if originalErr != nil && post.Feed != "" {
		if next := fetchAdvertisedPost(ctx, page.URL, post.Feed, fetch); next != nil {
			post = next

			if parsed, err := captionBatch(ctx, page.URL, post, post.AuthoredText); err == nil && captionComplete(parsed) {
				batch, originalErr = parsed, nil
			}
		}
	}

	for _, address := range post.Links {
		if address == page.URL {
			continue
		}

		if ctx.Err() != nil {
			return ImportBatch{}, ctx.Err()
		}

		linked, err := fetch(ctx, address)
		if err != nil {
			continue
		}

		extracted, err := webrecipe.ExtractContext(ctx, linked)
		if err != nil || !extracted.Usable() {
			continue
		}

		batch = batchFor(extracted)

		item := &batch.Recipes[0]
		if len(item.Assets) == 0 {
			item.Assets = webAssets(&webrecipe.Recipe{Images: post.Images}, item.Document.Title)
		}

		item.Document.Notes = append(item.Document.Notes, "Shared from "+page.URL)
		item.Diagnostics = append(item.Diagnostics, Diagnostic{
			Code: "social-linked-recipe", Class: DiagnosticInformational, Severity: SeverityInfo,
			Message: "Imported from the recipe linked in the post.",
		})
		item.ImporterMetadata = map[string]any{"shared_from": page.URL, "source_text": strings.Join(post.Texts, "\n\n")}

		return batch, nil
	}

	if ctx.Err() != nil {
		return ImportBatch{}, ctx.Err()
	}

	if originalErr == nil {
		return batch, nil
	}

	return captionDraft(ctx, page.URL, post, originalErr)
}

func fetchAdvertisedPost(ctx context.Context, address, feedURL string, fetch func(context.Context, string) (*webrecipe.Page, error)) *webrecipe.Post {
	feed, err := fetch(ctx, feedURL)
	if err != nil || feed.ContentType != "application/atom+xml" {
		return nil
	}

	post, err := webrecipe.ExtractPostContext(ctx, feed)
	if err != nil || post == nil || post.URL != address {
		return nil
	}

	return post
}
