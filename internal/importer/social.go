package importer

import (
	"context"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/savorum/savorum/internal/importer/webrecipe"
	"github.com/savorum/savorum/internal/recipe"
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
		if parseErr == nil && captionUsable(batch) {
			return batch, post, nil
		}
	}

	if batch, ok := commentBatch(ctx, page.URL, post); ok {
		return batch, post, nil
	}

	return ImportBatch{}, post, err
}

func commentBatch(ctx context.Context, address string, post *webrecipe.Post) (ImportBatch, bool) {
	if len(post.Comments) == 0 {
		return ImportBatch{}, false
	}

	candidates := slices.Clone(post.Comments)
	if len(post.Comments) > 1 || post.AuthoredText != "" {
		candidates = append(candidates, strings.Join(append([]string{post.AuthoredText}, post.Comments...), "\n\n"))
	}

	for _, text := range candidates {
		batch, err := captionBatch(ctx, address, post, strings.TrimSpace(text))
		if err != nil || !captionUsable(batch) {
			continue
		}

		item := &batch.Recipes[0]
		for i := range item.Diagnostics {
			if item.Diagnostics[i].Code == "social-caption" {
				item.Diagnostics[i].Code = "social-comment"
				item.Diagnostics[i].Message = "Imported from the author's comments on the post. Video and audio were not read."
			}
		}

		return batch, true
	}

	return ImportBatch{}, false
}

func captionComplete(batch ImportBatch) bool {
	if len(batch.Recipes) != 1 {
		return false
	}

	document := batch.Recipes[0].Document

	return len(document.IngredientSections) > 0 && len(document.InstructionSections) > 0
}

func captionUsable(batch ImportBatch) bool {
	return captionComplete(batch) && listedIngredients(&batch.Recipes[0].Document) > 0
}

func listedIngredients(document *recipe.Document) int {
	ingredients, prose := 0, 0

	for _, section := range document.IngredientSections {
		for _, ingredient := range section.Ingredients {
			ingredients++

			if utf8.RuneCountInString(ingredient.SourceText) > 80 {
				prose++
			}
		}
	}

	if prose*2 >= ingredients {
		return 0
	}

	return ingredients
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
	if post == nil {
		return ImportBatch{}, originalErr
	}

	text := strings.TrimSpace(strings.Join(append([]string{post.AuthoredText}, post.Comments...), "\n\n"))
	if text == "" {
		return ImportBatch{}, originalErr
	}

	batch, err := captionBatch(ctx, address, post, text)
	if err != nil {
		return ImportBatch{}, err
	}

	item := &batch.Recipes[0]
	if len(item.Document.IngredientSections) > 0 && listedIngredients(&item.Document) == 0 {
		item.Document.IngredientSections, item.Document.InstructionSections = nil, nil
	}

	if len(item.Document.IngredientSections)+len(item.Document.InstructionSections) == 0 {
		inferIngredientList(item, text, post.SourceLocale)
	}

	if len(item.Document.IngredientSections)+len(item.Document.InstructionSections) == 0 {
		readNumberedSteps(item, text)
	}

	if captionPartial(&item.Document) {
		item.Diagnostics = append(item.Diagnostics, Diagnostic{
			Code: "social-caption-partial", Class: DiagnosticActionable, Severity: SeverityWarning,
			Area: "source", Count: 1,
			Message: "The post gives only part of this recipe. Add what is missing to finish it.",
		})

		return batch, nil
	}
	// An incomplete section model can misclassify a heading or numbered step
	// as an ingredient, especially across languages. Keep a text-only draft.
	item.Document.Notes = []string{text}
	item.Document.IngredientSections = nil
	item.Document.InstructionSections = nil
	item.Diagnostics = slices.DeleteFunc(item.Diagnostics, func(d Diagnostic) bool {
		return d.Code == "ingredient-review-required" || d.Code == "plain-text-uncertain" || d.Code == "text-sections-inferred"
	})
	item.Diagnostics = append(item.Diagnostics, Diagnostic{
		Code: "social-caption-draft", Class: DiagnosticActionable, Severity: SeverityWarning,
		Area: "source", Count: 2,
		Message: "Add ingredients and steps to finish this draft.",
	})

	return batch, nil
}

func readNumberedSteps(item *ImportedRecipe, text string) {
	lines := strings.Split(normalizePlainText([]byte(text)), "\n")
	first := slices.IndexFunc(lines, func(line string) bool { return textStep.MatchString(recipeTextLine(line)) })

	numbered := 0

	for _, line := range lines[max(first, 0):] {
		if textStep.MatchString(recipeTextLine(line)) {
			numbered++
		}
	}

	if first < 0 || numbered < 3 || !strings.HasPrefix(strings.TrimLeft(recipeTextLine(lines[first]), "(（"), "1") {
		return
	}

	item.Document.Notes = nil
	parser := plainTextParser{item: item, mode: plainModeNotes}
	parser.parse(lines[:first], 0)

	parser.mode, parser.instructionSection = plainModeInstructions, 1
	parser.parse(lines, first)
	parser.markInferred()
}

func captionPartial(document *recipe.Document) bool {
	if len(document.IngredientSections) > 0 {
		return listedIngredients(document) >= 2
	}

	steps := 0
	for _, section := range document.InstructionSections {
		steps += len(section.Steps)
	}

	return steps >= 2
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

	if post.Discussion != "" && (originalErr != nil || inferred) && readDiscussion(ctx, post, fetch) && originalErr != nil {
		if parsed, ok := commentBatch(ctx, page.URL, post); ok {
			batch, originalErr = parsed, nil
		}
	}

	if originalErr != nil && post.Feed != "" {
		if next := fetchAdvertisedPost(ctx, page.URL, post.Feed, fetch); next != nil {
			post = next

			if parsed, err := captionBatch(ctx, page.URL, post, post.AuthoredText); err == nil && captionUsable(parsed) {
				batch, originalErr = parsed, nil
			}
		}
	}

	if linked, ok := linkedRecipe(ctx, page, post, fetch); ok {
		return linked, nil
	}

	if ctx.Err() != nil {
		return ImportBatch{}, ctx.Err()
	}

	if originalErr == nil {
		return batch, nil
	}

	if found, ok := creatorRecipe(ctx, page, post, fetch); ok {
		return found, nil
	}

	if ctx.Err() != nil {
		return ImportBatch{}, ctx.Err()
	}

	return captionDraft(ctx, page.URL, post, originalErr)
}

func linkedRecipe(ctx context.Context, page *webrecipe.Page, post *webrecipe.Post, fetch func(context.Context, string) (*webrecipe.Page, error)) (ImportBatch, bool) {
	for _, address := range post.Links {
		if address == page.URL {
			continue
		}

		if ctx.Err() != nil {
			return ImportBatch{}, false
		}

		linked, err := fetch(ctx, address)
		if err != nil {
			continue
		}

		extracted, err := webrecipe.ExtractContext(ctx, linked)
		if err != nil || !extracted.Usable() {
			continue
		}

		batch := batchFor(extracted)

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

		return batch, true
	}

	return ImportBatch{}, false
}

func readDiscussion(ctx context.Context, post *webrecipe.Post, fetch func(context.Context, string) (*webrecipe.Page, error)) bool {
	discussion := fetchDiscussion(ctx, post.Discussion, fetch)
	if discussion == nil {
		return false
	}

	post.Comments = discussion.Comments
	for _, link := range discussion.Links {
		if !slices.Contains(post.Links, link) && len(post.Links) < 3 {
			post.Links = append(post.Links, link)
		}
	}

	return true
}

func fetchDiscussion(ctx context.Context, address string, fetch func(context.Context, string) (*webrecipe.Page, error)) *webrecipe.Post {
	page, err := fetch(ctx, address)
	if err != nil {
		return nil
	}

	post, err := webrecipe.ExtractPostContext(ctx, page)
	if err != nil || post == nil || len(post.Comments) == 0 {
		return nil
	}

	return post
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
