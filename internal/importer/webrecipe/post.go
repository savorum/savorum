package webrecipe

import (
	"cmp"
	"context"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/savorum/savorum/internal/recipetext"
	"golang.org/x/net/html"
)

// Post contains only text and links a public social page actually publishes.
// It never represents inferred video contents or authenticated account data.
type Post struct {
	URL            string
	Feed           string
	Title          string
	Texts          []string
	Links          []string
	Images         []string
	Language       string
	SourceLocale   string
	LocaleEvidence string
	AuthoredText   string
	Comments       []string
	Discussion     string
	Subject        string
	Profile        string
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

	if page.ContentType == "application/atom+xml" {
		return extractFeedPost(ctx, page)
	}

	parsed, err := parse(ctx, page)
	if err != nil {
		return nil, err
	}

	post := &Post{Feed: alternateFeed(parsed)}

	var canonical, kind, description string

	for _, meta := range elementsByTag(parsed.root, "meta") {
		key := strings.ToLower(attribute(meta, "property"))
		if key == "" {
			key = strings.ToLower(attribute(meta, "name"))
		}

		value := strings.TrimSpace(attribute(meta, "content"))

		switch key {
		case "og:type":
			kind = value
		case "og:url":
			canonical = value
		case "og:title":
			post.Title = value
		case "og:description":
			description = value
			post.addText(value)
		case "twitter:description", "description":
			post.addText(value)
		case "og:see_also":
			post.addLink(value)
		}
	}

	post.readSocialMetadata(page.URL, canonical, kind, description)

	authors := microdataAuthors(parsed)
	if len(authors) > 0 {
		post.Profile = resolvedLink(page.URL, authors[0])
	}

	for _, block := range parsed.blocks {
		walkObjects(block.value, func(object map[string]any, _ string, _ []string) { post.readObject(object, page.URL, authors) })
	}

	if err := post.readPageData(ctx, parsed, canonical); err != nil {
		return nil, err
	}

	if len(post.Images) == 0 {
		post.Images = parsed.pageImages()
	}
	// Open Graph descriptions are often shortened. Prefer the complete
	// authored caption, including its links, when the page publishes it.
	slices.SortStableFunc(post.Texts, func(a, b string) int {
		if a == b {
			return 0
		}

		if a == post.AuthoredText {
			return -1
		}

		if b == post.AuthoredText {
			return 1
		}

		if paragraphs := strings.Count(b, "\n") - strings.Count(a, "\n"); paragraphs != 0 {
			return paragraphs
		}

		return len(b) - len(a)
	})

	post.prioritizeInstructionLinks()

	for _, text := range slices.Concat(post.Texts, post.Comments) {
		for _, link := range postLinks(text) {
			post.addLink(link)
		}
	}

	if err := parsed.check(); err != nil {
		return nil, err
	}

	if post.Profile == "" {
		post.Profile = accountPage(cmp.Or(post.URL, canonical, page.URL))
	}

	if len(post.Texts) == 0 && len(post.Links) == 0 && len(post.Comments) == 0 && post.Feed == "" {
		return nil, nil
	}

	return post, nil
}

func (p *Post) prioritizeInstructionLinks() {
	links := p.Links

	p.Links = nil
	for _, text := range p.Texts {
		for line := range strings.SplitSeq(text, "\n") {
			link := postLinkPattern.FindStringIndex(line)
			if link == nil {
				continue
			}

			label := line[:link[0]]
			for _, word := range instructionLinkLabels(label) {
				if section, _ := recipetext.Heading(word); section == "instructions" {
					p.addLink(cleanPostLink(line[link[0]:link[1]]))
					break
				}
			}
		}
	}

	for _, link := range links {
		p.addLink(link)
	}
}

func instructionLinkLabels(label string) []string {
	phrases := strings.FieldsFunc(label, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsSpace(r) })
	return append(phrases, strings.FieldsFunc(label, func(r rune) bool { return !unicode.IsLetter(r) })...)
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

func postLinks(text string) []string {
	links := postLinkPattern.FindAllString(text, -1)
	for i, link := range links {
		links[i] = cleanPostLink(link)
	}

	return links
}

func cleanPostLink(link string) string {
	link, _, _ = strings.Cut(link, "](")
	return strings.TrimRight(link, ").,;!?]")
}

func (p *Post) readObject(object map[string]any, address string, pageAuthors []string) {
	if !postObject(object) {
		return
	}
	// A graph can also describe a recommended video. A stated URL must
	// identify the requested post before its caption is considered.
	if objectURL, ok := object["url"].(string); ok && objectURL != "" && objectURL != address {
		return
	}

	p.readSharedContent(object["sharedContent"])

	authors := append(authorURLs(object["author"], object["creator"]), pageAuthors...)
	if p.Profile == "" && len(authors) > 0 {
		p.Profile = resolvedLink(address, authors[0])
	}

	p.readAuthorComments(object["comment"], authors)

	if title, ok := object["name"].(string); ok && p.Title == "" {
		p.Title = title
	}

	if language, ok := object["inLanguage"].(string); ok {
		p.readLocale(language, "")
	}

	for _, key := range []string{"image", "thumbnailUrl"} {
		for _, image := range normalizeImages(object[key], address) {
			p.addImage(image)
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

func postFromHTML(ctx context.Context, page *Page) (*Post, error) {
	d, err := parse(ctx, page)
	if err != nil {
		return nil, err
	}

	if d.root == nil {
		return nil, failuref(NoRecipeStructuredData, nil, "the post body could not be read")
	}

	root := fragmentRoot(d.root)

	text, err := d.semanticText(root, false)
	if err != nil {
		return nil, controlledExtractionError(err)
	}

	post := &Post{AuthoredText: text}
	post.addText(text)

	base, err := url.Parse(page.URL)
	if err != nil {
		return nil, failuref(FetchRejected, nil, "that does not look like a web address")
	}

	for _, anchor := range elementsByTag(root, "a") {
		post.addLink(resolvedLink(page.URL, attribute(anchor, "href")))
	}

	for _, anchor := range elementsByTag(d.root, "a") {
		target, err := url.Parse(attribute(anchor, "href"))
		if err == nil && base.ResolveReference(target).Hostname() != base.Hostname() {
			post.addLink(base.ResolveReference(target).String())
		}
	}

	return post, nil
}

func (p *Post) readSharedContent(value any) {
	values, ok := value.([]any)
	if !ok {
		values = []any{value}
	}

	for _, value := range values[:min(64, len(values))] {
		if object, ok := value.(map[string]any); ok {
			if address, ok := object["url"].(string); ok {
				p.addLink(address)
			}
		}
	}
}

func fragmentRoot(root *html.Node) *html.Node {
	for _, body := range elementsByTag(root, "body") {
		var container *html.Node

		for child := body.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.ElementNode || semanticExcluded(child) {
				continue
			}

			switch child.Data {
			case "div", "article", "main", "section":
				if container != nil {
					return root
				}

				container = child
			case "p", "ul", "ol", "table", "h1", "h2", "h3":
				return root
			}
		}

		if container != nil {
			return container
		}
	}

	return root
}

func (p *Post) readAuthorComments(value any, authors []string) {
	comments, ok := value.([]any)
	if !ok || len(authors) == 0 {
		return
	}

	for _, value := range comments[:min(64, len(comments))] {
		comment, ok := value.(map[string]any)
		if !ok {
			continue
		}

		text, _ := comment["text"].(string)

		text = strings.TrimSpace(text)
		if text == "" || len(text) > 256<<10 || len(p.Comments) >= 8 || slices.Contains(p.Comments, text) {
			continue
		}

		if slices.ContainsFunc(authorURLs(comment["author"]), func(author string) bool {
			return slices.ContainsFunc(authors, func(known string) bool { return profileKey(known) == profileKey(author) })
		}) {
			p.Comments = append(p.Comments, text)
		}
	}
}

func authorURLs(values ...any) []string {
	var urls []string

	for _, value := range values {
		items, ok := value.([]any)
		if !ok {
			items = []any{value}
		}

		for _, item := range items[:min(16, len(items))] {
			if author, ok := item.(map[string]any); ok {
				if address, ok := author["url"].(string); ok {
					if profileKey(address) != "" {
						urls = append(urls, address)
					}
				}
			}
		}
	}

	return urls
}

func microdataAuthors(d *document) []string {
	if d.root == nil {
		return nil
	}

	var urls []string

	for _, node := range slices.Concat(elementsByTag(d.root, "span"), elementsByTag(d.root, "div")) {
		if !strings.EqualFold(attribute(node, "itemprop"), "author") || len(urls) >= 4 {
			continue
		}

		for _, link := range elementsByTag(node, "link") {
			if strings.EqualFold(attribute(link, "itemprop"), "url") {
				if address := attribute(link, "href"); profileKey(address) != "" {
					urls = append(urls, address)
				}
			}
		}
	}

	return urls
}

func profileKey(address string) string {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || parsed.Hostname() == "" {
		return ""
	}

	return strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.") + strings.ToLower(strings.TrimRight(parsed.Path, "/"))
}
