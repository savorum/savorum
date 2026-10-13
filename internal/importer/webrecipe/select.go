package webrecipe

import (
	"net/url"
	"sort"
	"strings"
)

// candidate is one Recipe object found on a page, together with what is known
// about where it sat. Position is kept so that ties break deterministically in
// document order rather than by map iteration.
type candidate struct {
	node       map[string]any
	position   int
	mainEntity bool
	score      int
	reason     string
}

// candidates finds every Recipe object on the page by type alone; whether one
// is worth importing is judged after selection.
func (d *document) candidates() []candidate {
	var found []candidate

	seen := map[string]bool{}

	for _, item := range d.blocks {
		if item.broken {
			continue
		}

		walkObjects(item.value, func(object map[string]any, parentKey string, parentTypes []string) {
			if !hasType(object["@type"], "Recipe") {
				return
			}
			// The same node reached twice, through @graph and again through a
			// reference, is one candidate rather than two.
			key := identityKey(object)
			if key != "" && seen[key] {
				return
			}

			seen[key] = true

			found = append(found, candidate{
				node:       object,
				position:   len(found),
				mainEntity: isMainEntity(parentKey, parentTypes),
			})
		})
	}

	return found
}

// isMainEntity reports the strongest statement a page can make about which
// recipe it is about: that the Recipe is the main entity of the page carrying
// it. A recommendation, a carousel item, or a related-recipe card never is.
func isMainEntity(parentKey string, parentTypes []string) bool {
	if !strings.EqualFold(parentKey, "mainEntity") && !strings.EqualFold(parentKey, "mainEntityOfPage") {
		return false
	}

	if len(parentTypes) == 0 {
		return true
	}

	for _, name := range parentTypes {
		switch strings.ToLower(name) {
		case "webpage", "itempage", "article", "website", "collectionpage":
			return true
		}
	}

	return false
}

func identityKey(object map[string]any) string {
	if id := firstString(object["@id"]); id != "" {
		return "id:" + id
	}

	if name := firstString(object["name"]); name != "" {
		return "name:" + strings.ToLower(name) + "|url:" + firstString(object["url"])
	}

	return ""
}

// selectCandidate chooses which Recipe on the page is the one that was asked
// for. It never merges two Recipe objects into one.
func (d *document) selectCandidate(found []candidate) (candidate, bool) {
	if len(found) == 0 {
		return candidate{}, false
	}

	pageURL := normalizeURL(d.page.URL)
	canonical := normalizeURL(d.canonicalURL())
	heading := normalizeTitle(d.pageHeading())

	for index := range found {
		scoreCandidate(&found[index], pageURL, canonical, heading)
	}

	ranked := make([]candidate, len(found))
	copy(ranked, found)
	sort.SliceStable(ranked, func(first, second int) bool {
		if ranked[first].score != ranked[second].score {
			return ranked[first].score > ranked[second].score
		}

		return ranked[first].position < ranked[second].position
	})

	winner := ranked[0]
	if len(ranked) > 1 {
		d.warnAboutRunnerUp(winner, ranked[1])
	}

	return winner, true
}

func scoreCandidate(item *candidate, pageURL, canonical, heading string) {
	var reasons []string

	if item.mainEntity {
		item.score += 100

		reasons = append(reasons, "the page names it as its main entity")
	}

	if candidateURLMatches(item.node["url"], pageURL, canonical) {
		item.score += 60

		reasons = append(reasons, "its address is this page")
	}

	if candidateURLMatches(item.node["@id"], pageURL, canonical) {
		item.score += 40

		reasons = append(reasons, "its identifier is this page")
	}

	if candidateTitleMatches(item.node["name"], heading) {
		item.score += 30

		reasons = append(reasons, "its name is the page heading")
	}

	// Content is worth less than provenance. A complete related recipe is
	// still the wrong recipe when the page says which one it is about.
	if countMembers(item.node["recipeIngredient"]) > 0 {
		item.score += 20
	}

	if countMembers(item.node["recipeInstructions"]) > 0 {
		item.score += 20
	}

	item.reason = strings.Join(reasons, ", ")
}

func candidateURLMatches(value any, pageURL, canonical string) bool {
	address := normalizeURL(firstString(value))

	return address != "" && (address == pageURL || canonical != "" && address == canonical)
}

func candidateTitleMatches(value any, heading string) bool {
	name := normalizeTitle(firstString(value))

	return heading != "" && name != "" && (name == heading || strings.Contains(heading, name))
}

func (d *document) warnAboutRunnerUp(winner, runnerUp candidate) {
	if !usableNode(runnerUp.node) {
		return
	}

	name := firstString(winner.node["name"])
	// An indistinguishable tie is chosen and reported, not chosen silently.
	if runnerUp.score >= winner.score && usableNode(winner.node) {
		d.warn("multiple-recipes", "This page carries more than one recipe and does not say which it is about. Savorum imported “"+name+"”.")

		return
	}

	d.warn("multiple-recipes", "This page carries more than one recipe. Savorum imported “"+name+"” because "+winner.reason+".")
}

func usableNode(node map[string]any) bool {
	return firstString(node["name"]) != "" &&
		(countMembers(node["recipeIngredient"]) > 0 || countMembers(node["recipeInstructions"]) > 0)
}

func countMembers(value any) int {
	switch typed := value.(type) {
	case nil:
		return 0
	case string:
		if strings.TrimSpace(typed) == "" {
			return 0
		}

		return 1
	case []any:
		total := 0
		for _, item := range typed {
			total += countMembers(item)
		}

		return total
	case map[string]any:
		return 1
	}

	return 0
}

// canonicalURL reads <link rel="canonical">, which is how a page states its own
// address when the address that was requested is not it.
func (d *document) canonicalURL() string {
	for _, node := range elementsByTag(d.root, "link") {
		if strings.EqualFold(strings.TrimSpace(attribute(node, "rel")), "canonical") {
			return attribute(node, "href")
		}
	}

	return ""
}

// pageLanguage reads the language the document itself declares, for a recipe
// node that omits inLanguage.
func (d *document) pageLanguage() string {
	for _, node := range elementsByTag(d.root, "html") {
		if value := strings.TrimSpace(attribute(node, "lang")); value != "" {
			return value
		}
	}

	for _, node := range elementsByTag(d.root, "meta") {
		name := strings.TrimSpace(attribute(node, "http-equiv"))
		if strings.EqualFold(name, "content-language") {
			if value := strings.TrimSpace(attribute(node, "content")); value != "" {
				return value
			}
		}
	}

	return ""
}

// pageHeading prefers the first h1, falling back to <title>. It is used only
// to recognize a candidate, never to become recipe content.
func (d *document) pageHeading() string {
	if headings := elementsByTag(d.root, "h1"); len(headings) > 0 {
		if value := textContent(headings[0]); value != "" {
			return value
		}
	}

	if titles := elementsByTag(d.root, "title"); len(titles) > 0 {
		return textContent(titles[0])
	}

	return ""
}

// normalizeURL reduces an address to what makes two of them the same page:
// scheme and a trailing slash are not differences a reader would recognize.
func normalizeURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return strings.ToLower(value)
	}

	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))

	path := strings.TrimSuffix(parsed.Path, "/")
	if host == "" {
		return strings.ToLower(path)
	}

	return host + strings.ToLower(path)
}

func normalizeTitle(value string) string {
	return strings.ToLower(collapseSpace(value))
}
