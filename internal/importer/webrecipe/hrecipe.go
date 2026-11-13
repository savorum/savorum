package webrecipe

import (
	"slices"
	"strings"

	"golang.org/x/net/html"
)

func (d *document) hRecipe() (map[string]any, bool) {
	if d.root == nil {
		return nil, false
	}

	var root *html.Node

	forEachElement(d.root, func(node *html.Node) {
		if root == nil && hasClass(node, "h-recipe") {
			root = node
		}
	})

	if root == nil {
		return nil, false
	}

	recipe := map[string]any{"@type": "Recipe"}

	var ingredients []any

	forEachElement(root, func(node *html.Node) {
		text := collapseSpace(textContent(node))

		switch {
		case hasClass(node, "p-name") && recipe["name"] == nil && text != "":
			recipe["name"] = text
		case hasClass(node, "p-ingredient") && text != "":
			ingredients = append(ingredients, text)
		case hasClass(node, "e-instructions") && recipe["recipeInstructions"] == nil:
			recipe["recipeInstructions"] = hRecipeSteps(node)
		case hasClass(node, "p-yield") && text != "":
			recipe["recipeYield"] = text
		case hasClass(node, "p-summary") && text != "":
			recipe["description"] = text
		case hasClass(node, "dt-duration"):
			recipe["totalTime"] = cmpOr(attribute(node, "datetime"), text)
		case hasClass(node, "u-photo"):
			recipe["image"] = cmpOr(attribute(node, "src"), attribute(node, "href"), text)
		}
	})

	if len(ingredients) > 0 {
		recipe["recipeIngredient"] = ingredients
	}

	if recipe["name"] == nil {
		if heading := d.pageHeading(); heading != "" {
			recipe["name"] = heading
		}
	}

	return recipe, recipe["name"] != nil && (len(ingredients) > 0 || recipe["recipeInstructions"] != nil)
}

func hRecipeSteps(node *html.Node) any {
	for _, tag := range []string{"li", "p"} {
		var steps []any

		for _, element := range elementsByTag(node, tag) {
			if text := collapseSpace(textContent(element)); text != "" {
				steps = append(steps, text)
			}
		}

		if len(steps) > 0 {
			return steps
		}
	}

	return collapseSpace(textContent(node))
}

func hasClass(node *html.Node, name string) bool {
	return slices.Contains(strings.Fields(attribute(node, "class")), name)
}

func cmpOr(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}

	return ""
}
