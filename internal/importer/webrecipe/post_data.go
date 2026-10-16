package webrecipe

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/text/language"
)

// Pages often publish their full caption in JSON used to render the page,
// while metadata contains only a shortened teaser. Read literal data only;
// scripts are never executed and media URLs are never requested.
func (p *Post) readPageData(ctx context.Context, document *document) error {
	identifiers := postIdentifiers(document.page.URL)
	if len(identifiers) == 0 {
		return nil
	}

	for _, script := range elementsByTag(document.root, "script") {
		body := strings.TrimSpace(scriptData(script))
		if raw := document.scripts[script]; raw != nil {
			body = strings.TrimSpace(string(raw))
		}

		kind := strings.ToLower(strings.TrimSpace(strings.Split(attribute(script, "type"), ";")[0]))
		if kind != "application/json" {
			// Accept a literal assigned to a variable, not expressions, calls,
			// script-generated strings, or arbitrary JavaScript source.
			prefix := assignedJSONPattern.FindStringIndex(body)
			if prefix == nil {
				continue
			}

			body = body[prefix[1]-1:]
		}

		if err := visitIdentifiedJSONObjects(ctx, document.budget, body, identifiers, func(object map[string]any) {
			p.readIdentifiedCaption(object, identifiers)
		}); err != nil {
			return controlledExtractionError(err)
		}
	}

	return nil
}

// Only bounded objects containing a requested identifier are decoded. Large
// surrounding application state is scanned without building its object tree.
func visitIdentifiedJSONObjects(ctx context.Context, budget *workBudget, text string, identifiers []string, visit func(map[string]any)) error {
	type frame struct {
		start      int
		identified bool
	}

	var stack []frame

	candidates := 0

	for i := 0; i < len(text); i++ {
		if err := budget.spend(1); err != nil {
			return err
		}

		c := text[i]

		switch c {
		case '"':
			end, err := jsonStringEnd(budget, text, i+1)
			if err != nil {
				return err
			}

			if len(stack) > 0 && text[stack[len(stack)-1].start] == '{' && identifierLiteral(text[i:end], identifiers) {
				stack[len(stack)-1].identified = true
			}

			i = end - 1
		case '{', '[':
			if len(stack) == maximumJSONNesting {
				return errExtractionLimited
			}

			stack = append(stack, frame{start: i})
		case '}', ']':
			if len(stack) == 0 {
				continue
			}

			f := stack[len(stack)-1]

			stack = stack[:len(stack)-1]
			if (text[f.start] == '{') != (c == '}') {
				stack = stack[:0]
				continue
			}

			if !boundedIdentifiedObject(f.identified, i+1-f.start) {
				continue
			}

			candidates++
			if candidates > maximumJSONSpans {
				return errExtractionLimited
			}

			if err := visitJSONObject(ctx, budget, text[f.start:i+1], visit); err != nil {
				return err
			}
		}
	}

	return budget.spend(0)
}

func boundedIdentifiedObject(identified bool, size int) bool {
	return identified && size <= maximumHTMLTokenBytes
}

func identifierLiteral(literal string, identifiers []string) bool {
	for _, id := range identifiers {
		if literal == `"`+id+`"` {
			return true
		}

		if len(literal) <= 6*len(id)+2 && strings.Contains(literal, `\`) {
			var value string
			if json.Unmarshal([]byte(literal), &value) == nil && value == id {
				return true
			}
		}
	}

	return false
}

var assignedJSONPattern = regexp.MustCompile(`^(?:var|let|const)\s+[A-Za-z_$][A-Za-z0-9_$]*\s*=\s*[\[{]`)

func postIdentifiers(address string) []string {
	parsed, err := url.Parse(address)
	if err != nil {
		return nil
	}

	identifiers := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for _, values := range parsed.Query() {
		identifiers = append(identifiers, values...)
	}

	return slices.DeleteFunc(identifiers, func(value string) bool { return len(value) < 6 })
}

func (p *Post) readIdentifiedCaption(object map[string]any, identifiers []string) {
	matched := false

	for _, key := range []string{"id", "videoId", "identifier"} {
		if value, ok := object[key].(string); ok && slices.Contains(identifiers, value) {
			matched = true
		}
	}
	// Hydration data also contains recommendations, comments, and ads. Only
	// an object identifying the requested post may contribute caption text.
	if !matched {
		return
	}

	authored := p.readParagraphs(object["contents"])
	p.readPostImages(object)

	captionLanguage, _ := object["textLanguage"].(string)
	country, _ := object["locationCreated"].(string)
	p.readLocale(captionLanguage, country)

	for _, key := range []string{"shortDescription", "description", "desc", "articleBody"} {
		if value, ok := object[key].(string); ok {
			p.addText(value)

			if !authored && value != "" && len(value) <= 256<<10 {
				p.AuthoredText = value
				authored = true
			}
		}
	}

	if title, ok := object["title"].(string); ok && p.Title == "" {
		p.Title = title
	}
}

// Only metadata on the identified post supplies locale evidence. The site's
// UI language, visitor country, and video availability regions do not.
func (p *Post) readLocale(captionLanguage, country string) {
	if captionLanguage == "" {
		return
	}

	tag, err := language.Parse(captionLanguage)
	if err != nil || tag == language.Und {
		return
	}

	p.Language = tag.String()
	p.SourceLocale = tag.String()
	p.LocaleEvidence = "The post declares its language as " + tag.String() + "."

	_, _, statedRegion := tag.Raw()
	if statedRegion.String() != "ZZ" || len(country) != 2 {
		return
	}

	region, err := language.ParseRegion(country)
	if err != nil || !region.IsCountry() {
		return
	}

	localized, err := language.Compose(tag, region)
	if err != nil {
		return
	}

	p.SourceLocale = localized.String()
	p.LocaleEvidence = "The post declares caption language " + tag.String() + " and creation country " + region.String() + "."
}

func (p *Post) readParagraphs(value any) bool {
	contents, ok := value.([]any)
	if !ok || len(contents) == 0 || len(contents) > 4096 {
		return false
	}

	paragraphs := make([]string, 0, len(contents))
	size := 0

	for _, content := range contents {
		paragraph, ok := content.(map[string]any)
		if !ok {
			return false
		}

		text, ok := paragraph["desc"].(string)
		if !ok {
			return false
		}

		size += len(text) + 1
		if size > 256<<10 {
			return false
		}

		paragraphs = append(paragraphs, text)
	}

	p.AuthoredText = strings.Join(paragraphs, "\n")
	p.addText(p.AuthoredText)

	if p.Title == "" {
		title := []rune(strings.TrimSpace(paragraphs[0]))
		p.Title = string(title[:min(200, len(title))])
	}

	return true
}

// Only still thumbnails on the identified post are considered; playback and
// animated-cover addresses are not recipe images.
func (p *Post) readPostImages(object map[string]any) {
	for _, key := range []string{"originCover", "cover"} {
		if value, ok := object[key].(string); ok {
			before := len(p.Images)
			p.addImage(value)

			if len(p.Images) > before {
				break
			}
		}
	}

	if thumbnail, ok := object["thumbnail"].(map[string]any); ok {
		if variants, ok := thumbnail["thumbnails"].([]any); ok && len(variants) <= 64 {
			var (
				best string
				area float64
			)

			for _, variant := range variants {
				image, ok := variant.(map[string]any)
				if !ok {
					continue
				}

				address, _ := image["url"].(string)
				width, _ := image["width"].(float64)

				height, _ := image["height"].(float64)
				if address != "" && (best == "" || width*height > area) {
					best = address
					area = width * height
				}
			}

			p.addImage(best)
		}
	}
}

func jsonStringEnd(budget *workBudget, text string, start int) (int, error) {
	escaped := false

	for i := start; i < len(text); i++ {
		if err := budget.spend(1); err != nil {
			return 0, err
		}

		switch {
		case escaped:
			escaped = false
		case text[i] == '\\':
			escaped = true
		case text[i] == '"':
			return i + 1, nil
		}
	}

	return len(text), nil
}

func visitJSONObject(ctx context.Context, budget *workBudget, text string, visit func(map[string]any)) error {
	if err := budget.spend(len(text)); err != nil {
		return err
	}

	var object map[string]any
	if err := decodeJSON(ctx, text, &object); err == nil {
		visit(object)
	}

	return nil
}
