package webrecipe

import (
	"context"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Post contains only text and links a public social page actually publishes.
// It never represents inferred video contents or authenticated account data.
type Post struct {
	Title          string
	Texts          []string
	Links          []string
	Images         []string
	Language       string
	SourceLocale   string
	LocaleEvidence string
	AuthoredText   string
}

// ExtractPostContext reads public captions with the same parser budgets as
// structured recipes. Links are returned as untrusted input for Fetch.
func ExtractPostContext(ctx context.Context, page *Page) (*Post, error) {
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

	post := &Post{}

	for _, meta := range elementsByTag(parsed.root, "meta") {
		key := strings.ToLower(attribute(meta, "property"))
		if key == "" {
			key = strings.ToLower(attribute(meta, "name"))
		}

		value := strings.TrimSpace(attribute(meta, "content"))

		switch key {
		case "og:title":
			post.Title = value
		case "og:description", "twitter:description", "description":
			post.addText(value)
		case "og:see_also":
			post.addLink(value)
		}
	}

	for _, block := range parsed.blocks {
		walkObjects(block.value, func(object map[string]any, _ string, _ []string) { post.readObject(object, page.URL) })
	}

	if err := post.readPageData(ctx, parsed); err != nil {
		return nil, err
	}

	if len(post.Images) == 0 {
		post.Images = parsed.pageImages()
	}
	// Open Graph descriptions are often shortened. Prefer the complete
	// authored caption, including its links, when the page publishes it.
	slices.SortStableFunc(post.Texts, func(a, b string) int {
		if paragraphs := strings.Count(b, "\n") - strings.Count(a, "\n"); paragraphs != 0 {
			return paragraphs
		}

		return len(b) - len(a)
	})

	for _, text := range post.Texts {
		for _, link := range postLinkPattern.FindAllString(text, -1) {
			post.addLink(strings.TrimRight(link, ").,;!?]"))
		}
	}

	if err := parsed.check(); err != nil {
		return nil, err
	}

	if len(post.Texts) == 0 && len(post.Links) == 0 {
		return nil, nil
	}

	return post, nil
}

func postObject(object map[string]any) bool {
	for _, kind := range typeNames(object["@type"]) {
		switch kind {
		case "VideoObject", "SocialMediaPosting", "DiscussionForumPosting":
			return true
		}
	}

	return false
}

func (p *Post) addText(value string) {
	if value == "" || len(value) > 256<<10 || len(p.Texts) >= 8 {
		return
	}

	if slices.Contains(p.Texts, value) {
		return
	}

	p.Texts = append(p.Texts, value)
}

func (p *Post) addLink(value string) {
	if len(p.Links) >= 3 {
		return
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return
	}

	if slices.Contains(p.Links, value) {
		return
	}

	p.Links = append(p.Links, value)
}

var postLinkPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

func (p *Post) readObject(object map[string]any, address string) {
	if !postObject(object) {
		return
	}
	// A graph can also describe a recommended video. A stated URL must
	// identify the requested post before its caption is considered.
	if objectURL, ok := object["url"].(string); ok && objectURL != "" && objectURL != address {
		return
	}

	if title, ok := object["name"].(string); ok && p.Title == "" {
		p.Title = title
	}

	if language, ok := object["inLanguage"].(string); ok {
		p.readLocale(language, "")
	}

	for _, key := range []string{"image", "thumbnailUrl"} {
		for _, address := range normalizeImages(object[key]) {
			p.addImage(address)
		}
	}

	for _, key := range []string{"articleBody", "text", "description"} {
		if value, ok := object[key].(string); ok {
			p.addText(value)

			if p.AuthoredText == "" && len(value) <= 256<<10 {
				p.AuthoredText = value
			}
		}
	}
}

func (p *Post) addImage(value string) {
	if len(p.Images) >= 3 || slices.Contains(p.Images, value) {
		return
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return
	}

	p.Images = append(p.Images, value)
}
