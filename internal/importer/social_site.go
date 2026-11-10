package importer

import (
	"context"
	"math"
	"net/url"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/savorum/savorum/internal/importer/webrecipe"
)

const (
	maximumCreatorSites    = 2
	maximumSitemapRequests = 6
)

func creatorRecipe(ctx context.Context, page *webrecipe.Page, post *webrecipe.Post, fetch func(context.Context, string) (*webrecipe.Page, error)) (ImportBatch, bool) {
	queries := creatorQueries(post)
	if len(queries) == 0 {
		return ImportBatch{}, false
	}

	for _, site := range creatorSites(ctx, post, fetch) {
		address := matchingSitemapPage(ctx, site, queries, fetch)
		if address == "" {
			continue
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
			Code: "social-creator-recipe", Class: DiagnosticActionable, Severity: SeverityWarning,
			Area: "source", Count: 1,
			Message: "Imported a recipe with a matching name from the creator's website. Check that it is the recipe in the post.",
		})
		item.ImporterMetadata = map[string]any{"shared_from": page.URL, "source_text": post.AuthoredText}

		return batch, true
	}

	return ImportBatch{}, false
}

func creatorQueries(post *webrecipe.Post) [][]string {
	var queries [][]string

	for _, text := range []string{post.Title, post.Subject, addressSlug(post.URL)} {
		if words := nameWords(text); len(words) >= 2 && !slices.ContainsFunc(queries, func(query []string) bool { return slices.Equal(query, words) }) {
			queries = append(queries, words)
		}
	}

	return queries
}

func creatorSites(ctx context.Context, post *webrecipe.Post, fetch func(context.Context, string) (*webrecipe.Page, error)) []string {
	var sites []string

	add := func(link string) {
		root := siteRoot(link)
		if root != "" && !sameSite(root, post.URL) && !slices.ContainsFunc(sites, func(site string) bool { return sameSite(site, root) }) && len(sites) < maximumCreatorSites {
			sites = append(sites, root)
		}
	}

	for _, link := range slices.Concat(post.Links, linksIn(post.Comments)) {
		if parsed, err := url.Parse(link); err == nil && strings.Trim(parsed.Path, "/") == "" {
			add(link)
		}
	}

	if post.Profile == "" || len(sites) == maximumCreatorSites {
		return sites
	}

	page, err := fetch(ctx, post.Profile)
	if err != nil {
		return sites
	}

	names := slices.Concat(accountNames(post.Profile), accountNames(page.URL))

	links, _ := webrecipe.ProfileLinks(ctx, page)
	for _, link := range links {
		parsed, err := url.Parse(link)
		if err != nil {
			continue
		}

		host := compactName(parsed.Hostname())

		if slices.ContainsFunc(names, func(name string) bool { return strings.Contains(host, name) }) {
			add(link)
		}
	}

	return sites
}

func linksIn(texts []string) []string {
	var links []string

	for _, text := range texts {
		for field := range strings.FieldsSeq(text) {
			if strings.HasPrefix(field, "http://") || strings.HasPrefix(field, "https://") {
				links = append(links, strings.TrimRight(field, ").,;!?]"))
			}
		}
	}

	return links
}

func siteRoot(link string) string {
	parsed, err := url.Parse(link)
	if err != nil || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" {
		return ""
	}

	return parsed.Scheme + "://" + parsed.Host + "/"
}

func sameSite(a, b string) bool {
	first, err := url.Parse(a)
	if err != nil {
		return false
	}

	second, err := url.Parse(b)
	if err != nil {
		return false
	}

	host := func(u *url.URL) string { return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") }

	return host(first) != "" && host(first) == host(second)
}

func accountNames(address string) []string {
	parsed, err := url.Parse(address)
	if err != nil {
		return nil
	}

	var names []string

	for segment := range strings.SplitSeq(parsed.Path, "/") {
		if name := compactName(segment); utf8.RuneCountInString(name) >= 4 && strings.ContainsFunc(name, unicode.IsLetter) {
			names = append(names, name)
		}
	}

	return names
}

func compactName(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}

		return -1
	}, text)
}

func matchingSitemapPage(ctx context.Context, site string, queries [][]string, fetch func(context.Context, string) (*webrecipe.Page, error)) string {
	pending := sitemapsFromRobots(ctx, site, fetch)
	if len(pending) == 0 {
		pending = []string{site + "sitemap.xml"}
	}

	var pages []string

	for requests := 0; len(pending) > 0 && requests < maximumSitemapRequests && ctx.Err() == nil; requests++ {
		address := pending[0]
		pending = pending[1:]

		page, err := fetch(ctx, address)
		if err != nil || !sameSite(page.URL, site) {
			continue
		}

		sitemap, err := webrecipe.ReadSitemap(ctx, page)
		if err != nil {
			continue
		}

		pending = append(pending, sitemap.Sitemaps...)

		for _, candidate := range sitemap.Pages {
			if sameSite(candidate, site) {
				pages = append(pages, candidate)
			}
		}
	}

	return bestSlugMatch(pages, queries)
}

func sitemapsFromRobots(ctx context.Context, site string, fetch func(context.Context, string) (*webrecipe.Page, error)) []string {
	page, err := fetch(ctx, site+"robots.txt")
	if err != nil || page.ContentType != "text/plain" || !sameSite(page.URL, site) {
		return nil
	}

	var sitemaps []string

	for line := range strings.SplitSeq(string(page.Body), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.EqualFold(key, "sitemap") && sameSite(strings.TrimSpace(value), site) && len(sitemaps) < 4 {
			sitemaps = append(sitemaps, strings.TrimSpace(value))
		}
	}

	return sitemaps
}

func addressSlug(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}

	slug, most := "", 1

	for segment := range strings.SplitSeq(parsed.Path, "/") {
		segment = strings.TrimSuffix(segment, path.Ext(segment))
		if count := len(nameWords(segment)); count > most {
			slug, most = segment, count
		}
	}

	return slug
}

func nameWords(text string) []string {
	var words []string

	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		if strings.ContainsFunc(word, unicode.IsLetter) && !slices.Contains(words, word) {
			words = append(words, word)
		}
	}

	return words
}

// Weight words by length and inverse frequency to discount site-wide boilerplate.
// Require overlap in both directions to avoid partial matches to other dishes.
func bestSlugMatch(pages []string, queries [][]string) string {
	const (
		minimumMatchedWords  = 2
		minimumSlugCoverage  = 0.8
		minimumQueryCoverage = 0.4
	)

	slugs := make([][]string, len(pages))
	wordFrequency := map[string]int{}

	for i, page := range pages {
		slugs[i] = nameWords(addressSlug(page))
		for _, word := range slugs[i] {
			wordFrequency[word]++
		}
	}

	weight := func(word string) float64 {
		if wordFrequency[word] == 0 {
			return 0
		}

		return float64(utf8.RuneCountInString(word)) * math.Log1p(float64(len(pages))/float64(wordFrequency[word]))
	}

	weightedOverlap := func(words, others []string) (float64, int) {
		matched, total, count := 0.0, 0.0, 0

		for _, word := range words {
			total += weight(word)

			if slices.ContainsFunc(others, func(other string) bool { return similarWord(word, other) }) {
				matched += weight(word)
				count++
			}
		}

		if total == 0 {
			return 0, 0
		}

		return matched / total, count
	}

	best, bestScore := "", 0.0

	for i, slug := range slugs {
		for _, query := range queries {
			slugCoverage, matched := weightedOverlap(slug, query)
			queryCoverage, _ := weightedOverlap(query, slug)
			score := slugCoverage + queryCoverage

			if matched >= minimumMatchedWords && slugCoverage >= minimumSlugCoverage && queryCoverage >= minimumQueryCoverage && score > bestScore {
				best, bestScore = pages[i], score
			}
		}
	}

	return best
}

func similarWord(a, b string) bool {
	if a == b {
		return true
	}

	first, second := []rune(a), []rune(b)

	shared := 0
	for shared < len(first) && shared < len(second) && first[shared] == second[shared] {
		shared++
	}

	return shared >= 4 && shared >= min(len(first), len(second))-1
}
