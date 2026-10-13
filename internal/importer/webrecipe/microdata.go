package webrecipe

import (
	"strings"

	"golang.org/x/net/html"
)

// microdataRecipe reads Schema.org microdata into the same generic map shape
// JSON-LD produces, so that every normalization rule above applies to it
// unchanged. Microdata is a fallback: when a page carries both, the JSON-LD is
// the representation its author maintains and the microdata is usually older.
func (d *document) microdataRecipe() (map[string]any, bool, error) {
	if d.root == nil {
		return nil, false, nil
	}

	var (
		found   *html.Node
		walkErr error
	)

	forEachElement(d.root, func(node *html.Node) {
		if found != nil || walkErr != nil {
			return
		}

		if walkErr = d.budget.spend(1); walkErr != nil || attribute(node, "itemscope") == "" && !hasItemScope(node) {
			return
		}

		if isRecipeItemType(attribute(node, "itemtype")) {
			found = node
		}
	})

	if walkErr != nil {
		return nil, false, controlledExtractionError(walkErr)
	}

	if found == nil {
		return nil, false, nil
	}

	node, err := d.microdataObject(found, 1)
	if err != nil {
		return nil, false, controlledExtractionError(err)
	}

	if firstString(node["name"]) == "" {
		if heading := d.pageHeading(); heading != "" {
			node["name"] = heading
		}
	}

	return node, true, nil
}

func hasItemScope(node *html.Node) bool {
	for _, item := range node.Attr {
		if strings.EqualFold(item.Key, "itemscope") {
			return true
		}
	}

	return false
}

func isRecipeItemType(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasSuffix(value, "schema.org/recipe") || strings.HasSuffix(value, "/recipe")
}

// microdataObject collects the itemprops belonging to one item scope. A nested
// scope becomes a nested object rather than being flattened into its parent,
// which is what keeps a HowToSection's steps attached to their section.
func (d *document) microdataObject(scope *html.Node, scopeDepth int) (map[string]any, error) {
	if scopeDepth > maximumJSONNesting {
		return nil, errExtractionLimited
	}

	result := map[string]any{}
	if itemType := attribute(scope, "itemtype"); itemType != "" {
		result["@type"] = lastPathSegment(itemType)
	}

	collector := microdataCollector{document: d, result: result, scopeDepth: scopeDepth}
	if err := collector.walk(scope); err != nil {
		return nil, err
	}

	return result, nil
}

type microdataCollector struct {
	document   *document
	result     map[string]any
	scopeDepth int
	text       strings.Builder
}

func (c *microdataCollector) walk(node *html.Node) error {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := c.document.budget.spend(1); err != nil {
			return err
		}

		if child.Type == html.TextNode {
			c.text.WriteString(child.Data)

			continue
		}

		if child.Type != html.ElementNode || child.Data == "style" || child.Data == "script" {
			continue
		}

		if err := c.readElement(child); err != nil {
			return err
		}
	}

	return nil
}

func (c *microdataCollector) readElement(node *html.Node) error {
	property := strings.TrimSpace(attribute(node, "itemprop"))
	if hasItemScope(node) && property != "" {
		value, err := c.document.microdataObject(node, c.scopeDepth+1)
		if err != nil {
			return err
		}

		addMicrodataProperty(c.result, property, value)

		return nil
	}

	start := c.text.Len()
	if err := c.walk(node); err != nil {
		return err
	}

	appendBlockSpace(&c.text, node)

	if property == "" {
		return nil
	}

	value, found := microdataMachineValue(node)
	if !found {
		if err := c.document.budget.spend(c.text.Len() - start); err != nil {
			return err
		}

		value = collapseSpace(c.text.String()[start:])
	}

	if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
		return nil
	}

	addMicrodataProperty(c.result, property, value)

	return nil
}

func appendBlockSpace(builder *strings.Builder, node *html.Node) {
	switch node.Data {
	case "p", "div", "li", "br", "tr", "section", "h1", "h2", "h3", "h4", "h5", "h6":
		builder.WriteByte(' ')
	}
}

// addMicrodataProperty accumulates repeated properties into an array, because
// recipeIngredient appearing forty times is a list and not forty overwrites.
func addMicrodataProperty(target map[string]any, property string, value any) {
	existing, present := target[property]
	if !present {
		if isRepeatedMicrodataProperty(property) {
			target[property] = []any{value}
			return
		}

		target[property] = value

		return
	}

	if list, ok := existing.([]any); ok {
		target[property] = append(list, value)
		return
	}

	target[property] = []any{existing, value}
}

func isRepeatedMicrodataProperty(property string) bool {
	switch property {
	case "recipeIngredient", "ingredients", "recipeInstructions", "image", "author",
		"itemListElement", "supply", "tool", "recipeCuisine", "recipeCategory":
		return true
	}

	return false
}

// microdataMachineValue reads a property's explicit machine-readable value.
// The caller indexes visible subtree text once and uses that as the fallback.
func microdataMachineValue(node *html.Node) (any, bool) {
	for _, name := range []string{"content", "datetime", "value"} {
		if value := strings.TrimSpace(attribute(node, name)); value != "" {
			return value, true
		}
	}

	switch node.Data {
	case "a", "link", "area":
		if value := strings.TrimSpace(attribute(node, "href")); value != "" {
			return value, true
		}
	case "img", "audio", "video", "source", "embed", "iframe":
		if value := strings.TrimSpace(attribute(node, "src")); value != "" {
			return value, true
		}
	case "meta":
		return strings.TrimSpace(attribute(node, "content")), true
	case "time":
		if value := strings.TrimSpace(attribute(node, "datetime")); value != "" {
			return value, true
		}
	}

	return nil, false
}

func lastPathSegment(value string) string {
	value = strings.TrimSpace(strings.TrimSuffix(value, "/"))
	if index := strings.LastIndexAny(value, "/#"); index >= 0 && index < len(value)-1 {
		return value[index+1:]
	}

	return value
}
