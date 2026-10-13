package webrecipe

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// Enricher repairs individual fields of an extracted recipe from the rendered
// page, chiefly ingredient grouping and the split of a line into amount, unit,
// name, and note. It is not a scraper: it fills fields it is confident about
// and leaves the rest of the recipe alone.
type Enricher interface {
	// Name identifies the enricher in warnings.
	Name() string
	// Applies reports whether the page is one this enricher understands. It
	// runs before any work and should be cheap.
	Applies(page *document) bool
	// Enrich fills or replaces fields on the recipe in place.
	Enrich(page *document, target *Recipe)
}

// enrichers are tried in order and all applicable ones run. Recipe-card
// enrichers come first because their generated markup is the most reliable;
// host-specific enrichers belong at the end.
func defaultEnrichers() []Enricher {
	return []Enricher{
		recipeCardEnricher{spec: wpRecipeMaker},
		recipeCardEnricher{spec: tastyRecipes},
	}
}

func enrich(page *document, target *Recipe) {
	if page.root == nil {
		return
	}

	for _, enricher := range defaultEnrichers() {
		if !enricher.Applies(page) {
			continue
		}

		enricher.Enrich(page, target)
	}
}

// cardSpec describes one recipe-card plugin by the class names it renders.
type cardSpec struct {
	product string
	// container identifies the card itself.
	container string
	// ingredientGroup and ingredientGroupName recover the grouping that
	// Schema.org flattened away.
	ingredientGroup      string
	ingredientGroupName  string
	ingredient           string
	amount               string
	unit                 string
	name                 string
	note                 string
	instructionGroup     string
	instructionGroupName string
	instruction          string
	equipment            string
}

var wpRecipeMaker = cardSpec{
	product:              "WP Recipe Maker",
	container:            "wprm-recipe",
	ingredientGroup:      "wprm-recipe-ingredient-group",
	ingredientGroupName:  "wprm-recipe-group-name",
	ingredient:           "wprm-recipe-ingredient",
	amount:               "wprm-recipe-ingredient-amount",
	unit:                 "wprm-recipe-ingredient-unit",
	name:                 "wprm-recipe-ingredient-name",
	note:                 "wprm-recipe-ingredient-notes",
	instructionGroup:     "wprm-recipe-instruction-group",
	instructionGroupName: "wprm-recipe-group-name",
	instruction:          "wprm-recipe-instruction",
	equipment:            "wprm-recipe-equipment-name",
}

var tastyRecipes = cardSpec{
	product:             "Tasty Recipes",
	container:           "tasty-recipes",
	ingredientGroup:     "tasty-recipes-ingredients-body",
	ingredientGroupName: "tasty-recipes-ingredients-header",
	ingredient:          "tasty-recipe-ingredients",
	instructionGroup:    "tasty-recipes-instructions-body",
	instruction:         "tasty-recipes-instructions",
}

type recipeCardEnricher struct{ spec cardSpec }

func (e recipeCardEnricher) Name() string { return e.spec.product }

func (e recipeCardEnricher) Applies(page *document) bool {
	return firstElementWithClass(page.root, e.spec.container) != nil
}

// Enrich replaces the ingredient list only when the card offers something the
// structured data did not: real groups, or a separation of the line into its
// parts. An equal or poorer reading is left alone, because the structured data
// is what the site maintains deliberately.
func (e recipeCardEnricher) Enrich(page *document, target *Recipe) {
	card := firstElementWithClass(page.root, e.spec.container)
	if card == nil {
		return
	}

	if groups := e.ingredientGroups(card); improvesIngredients(groups, target.IngredientGroups) {
		target.IngredientGroups = groups
		target.warn("ingredients-enriched",
			"Ingredient groups were read from the page's %s card, which keeps groups the recipe data leaves out.", e.spec.product)
	}

	if sections := e.instructionSections(card); improvesInstructions(sections, target.Sections) {
		target.Sections = sections
		target.warn("instructions-enriched",
			"Instruction groups came from this page's %s card.", e.spec.product)
	}

	if len(target.Equipment) == 0 && e.spec.equipment != "" {
		for _, node := range elementsWithClass(card, e.spec.equipment) {
			if value := textContent(node); value != "" {
				target.Equipment = append(target.Equipment, value)
			}
		}
	}
}

func (e recipeCardEnricher) ingredientGroups(card *html.Node) []IngredientGroup {
	var groups []IngredientGroup

	containers := elementsWithClass(card, e.spec.ingredientGroup)
	if len(containers) == 0 {
		containers = []*html.Node{card}
	}

	for _, container := range containers {
		group := IngredientGroup{}

		if e.spec.ingredientGroupName != "" {
			if heading := firstElementWithClass(container, e.spec.ingredientGroupName); heading != nil {
				group.Title = textContent(heading)
			}
		}

		for _, row := range elementsWithClass(container, e.spec.ingredient) {
			ingredient := Ingredient{
				Amount: textContent(firstElementWithClass(row, e.spec.amount)),
				Unit:   textContent(firstElementWithClass(row, e.spec.unit)),
				Name:   textContent(firstElementWithClass(row, e.spec.name)),
				Note:   trimNoteOpening(textContent(firstElementWithClass(row, e.spec.note))),
			}
			// The whole row remains the source line. The parts are an
			// interpretation offered beside it, never a replacement for it.
			ingredient.Text = trimRowFurniture(textContent(row))
			if ingredient.Text == "" {
				continue
			}

			group.Ingredients = append(group.Ingredients, ingredient)
		}

		if len(group.Ingredients) > 0 {
			groups = append(groups, group)
		}
	}

	return groups
}

// rowFurniture is the checkbox glyph a recipe card puts in front of an
// ingredient; left in place it stops the amount from being read.
var rowFurniture = regexp.MustCompile(`^[\p{Zs}\p{Cf}▢☐☑☒✓✔✅□■◻◼•·▪‣∙*+\-–—]+`)

func trimRowFurniture(value string) string {
	return strings.TrimSpace(rowFurniture.ReplaceAllString(value, ""))
}

// noteOpening is the punctuation a card leaves at the front of a note because
// its markup appends the note to the name: the note element itself begins
// ", finely diced".
var noteOpening = regexp.MustCompile(`^[\p{Zs},;:.\-–—]+`)

func trimNoteOpening(value string) string {
	return strings.TrimSpace(noteOpening.ReplaceAllString(value, ""))
}

func (e recipeCardEnricher) instructionSections(card *html.Node) []InstructionSection {
	var sections []InstructionSection

	containers := elementsWithClass(card, e.spec.instructionGroup)
	if len(containers) == 0 {
		return nil
	}

	for _, container := range containers {
		section := InstructionSection{}

		if e.spec.instructionGroupName != "" {
			if heading := firstElementWithClass(container, e.spec.instructionGroupName); heading != nil {
				section.Title = textContent(heading)
			}
		}

		for _, row := range elementsWithClass(container, e.spec.instruction) {
			if text := textContent(row); text != "" {
				section.Steps = append(section.Steps, Step{Text: text})
			}
		}

		if len(section.Steps) > 0 {
			sections = append(sections, section)
		}
	}

	return sections
}

// improvesIngredients reports whether a card's reading says more than the
// structured data: new groups, or lines split into parts, without losing lines.
func improvesIngredients(candidate, existing []IngredientGroup) bool {
	if countIngredients(candidate) == 0 {
		return false
	}

	if countIngredients(candidate) < countIngredients(existing) {
		return false
	}

	if len(existing) == 0 {
		return true
	}

	return namedGroups(candidate) > namedGroups(existing) || structuredLines(candidate) > structuredLines(existing)
}

func improvesInstructions(candidate, existing []InstructionSection) bool {
	if len(candidate) == 0 {
		return false
	}

	candidateSteps, existingSteps := countSteps(candidate), countSteps(existing)
	if candidateSteps < existingSteps {
		return false
	}

	return namedSections(candidate) > namedSections(existing)
}

func countIngredients(groups []IngredientGroup) int {
	total := 0
	for _, group := range groups {
		total += len(group.Ingredients)
	}

	return total
}

func namedGroups(groups []IngredientGroup) int {
	total := 0

	for _, group := range groups {
		if strings.TrimSpace(group.Title) != "" {
			total++
		}
	}

	return total
}

func structuredLines(groups []IngredientGroup) int {
	total := 0

	for _, group := range groups {
		for _, ingredient := range group.Ingredients {
			if ingredient.Amount != "" || ingredient.Unit != "" || ingredient.Name != "" {
				total++
			}
		}
	}

	return total
}

func countSteps(sections []InstructionSection) int {
	total := 0
	for _, section := range sections {
		total += len(section.Steps) + countSteps(section.Sections)
	}

	return total
}

func namedSections(sections []InstructionSection) int {
	total := 0

	for _, section := range sections {
		if strings.TrimSpace(section.Title) != "" {
			total++
		}

		total += namedSections(section.Sections)
	}

	return total
}

// elementsWithClass matches one class name exactly, so that a lookup for
// wprm-recipe-ingredient does not also return wprm-recipe-ingredient-name.
func elementsWithClass(root *html.Node, class string) []*html.Node {
	if class == "" || root == nil {
		return nil
	}

	needle := " " + strings.ToLower(class) + " "

	var result []*html.Node

	forEachElement(root, func(node *html.Node) {
		if strings.Contains(classList(node), needle) {
			result = append(result, node)
		}
	})

	return result
}

func firstElementWithClass(root *html.Node, class string) *html.Node {
	found := elementsWithClass(root, class)
	if len(found) == 0 {
		return nil
	}

	return found[0]
}

// cardApplies reports whether any recipe-card enricher recognizes the page, so
// a page with a card and no structured data can still be imported.
func cardApplies(page *document) bool {
	for _, enricher := range defaultEnrichers() {
		if enricher.Applies(page) {
			return true
		}
	}

	return false
}
