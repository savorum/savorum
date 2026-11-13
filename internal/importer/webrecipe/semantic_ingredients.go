package webrecipe

import (
	"slices"
	"strings"

	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/recipetext"
	"golang.org/x/net/html"
)

func (c *semanticCandidate) namesGroup(text string) bool {
	title := strings.TrimSuffix(semanticPlain(text), ":")

	return slices.ContainsFunc(c.groups, func(group IngredientGroup) bool {
		return group.Title != "" && strings.EqualFold(strings.TrimSuffix(group.Title, ":"), title)
	})
}

func semanticGroupLabel(block semanticBlock, blocks []semanticBlock, index int) bool {
	text := collapseSpace(block.text)
	if text == "" || len(text) > 100 || semanticAmount.MatchString(text) || semanticNumber.MatchString(text) {
		return false
	}

	if index+1 >= len(blocks) || (blocks[index+1].node.Data != "ul" && blocks[index+1].node.Data != "ol") {
		return false
	}

	between := index > 0 && (blocks[index-1].node.Data == "ul" || blocks[index-1].node.Data == "ol") &&
		len(strings.Fields(text)) <= 4 && !strings.ContainsAny(text, ".!?")

	return block.level > 0 || strings.HasSuffix(text, ":") || between || semanticEmphasized(block)
}

func semanticEmphasized(block semanticBlock) bool {
	text := collapseSpace(block.text)

	for _, strong := range append(elementsByTag(block.node, "strong"), elementsByTag(block.node, "b")...) {
		if collapseSpace(textContent(strong)) == text {
			return true
		}
	}

	return false
}

func (c *semanticCandidate) readIngredients(d *document, list *html.Node, title string) error {
	group := IngredientGroup{Title: title}

	for row := list.FirstChild; row != nil; row = row.NextSibling {
		if !c.semanticEntry(row) {
			continue
		}

		text, err := d.semanticText(row, true)
		if err != nil {
			return err
		}

		nested, err := d.semanticNestedLists(row)
		if err != nil {
			return err
		}

		if len(nested) > 0 {
			if len(group.Ingredients) > 0 {
				c.groups = append(c.groups, group)
				group = IngredientGroup{Title: title}
			}

			nestedTitle := text
			if title != "" && text != "" {
				nestedTitle = title + " · " + text
			}

			if err := c.readNestedIngredients(d, nested, nestedTitle); err != nil {
				return err
			}

			continue
		}

		if text != "" && !recipetext.ServingLine(semanticPlain(text)) {
			group.Ingredients = append(group.Ingredients, Ingredient{Text: semanticPlain(text)})
			if c.statesAmount(text) {
				c.amounts++
			}
		}
	}

	if len(group.Ingredients) > 0 {
		c.groups = append(c.groups, group)
	}

	return nil
}

func (d *document) semanticNestedLists(root *html.Node) ([]*html.Node, error) {
	var (
		lists []*html.Node
		walk  func(*html.Node) error
	)

	walk = func(node *html.Node) error {
		if semanticExcluded(node) {
			return nil
		}

		if err := d.budget.spend(1); err != nil {
			return err
		}

		if node.Data == "ul" || node.Data == "ol" {
			lists = append(lists, node)
			return nil
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child); err != nil {
				return err
			}
		}

		return nil
	}

	err := walk(root)

	return lists, err
}

func (c *semanticCandidate) readNestedIngredients(d *document, lists []*html.Node, title string) error {
	if title == "" || semanticAmount.MatchString(title) {
		c.uncertain = true
	}

	for _, list := range lists {
		if err := c.readIngredients(d, list, title); err != nil {
			return err
		}
	}

	return nil
}

// Allow columns for an ingredient name, amount, and unit or note.
const maximumIngredientColumns = 3

func (c *semanticCandidate) readTableIngredients(d *document, table *html.Node, title string) bool {
	group := IngredientGroup{Title: title}
	read := false

	for _, row := range d.ingredientTableRows(table) {
		if len(row) > maximumIngredientColumns {
			return false
		}

		line := strings.Join(slices.DeleteFunc(row, func(cell string) bool { return cell == "" }), " ")

		switch {
		case line == "":
		case len(row) == 1 && !c.statesAmount(line):
			if len(group.Ingredients) > 0 {
				c.groups = append(c.groups, group)
			}

			group = IngredientGroup{Title: strings.TrimSuffix(line, ":")}
		default:
			group.Ingredients = append(group.Ingredients, Ingredient{Text: line})
			read = true

			if c.statesAmount(line) {
				c.amounts++
			}
		}
	}

	if len(group.Ingredients) > 0 {
		c.groups = append(c.groups, group)
	}

	return read
}

func (d *document) ingredientTableRows(table *html.Node) [][]string {
	var rows [][]string

	text := func(node *html.Node) string {
		value, err := d.semanticText(node, false)
		if err != nil {
			return ""
		}

		return collapseSpace(semanticPlain(value))
	}

	if table.Data == "dl" {
		for _, term := range elementsByTag(table, "dt") {
			row := []string{text(term)}

			for next := term.NextSibling; next != nil && (next.Type != html.ElementNode || next.Data != "dt"); next = next.NextSibling {
				if next.Type == html.ElementNode && next.Data == "dd" {
					row = append(row, text(next))
				}
			}

			rows = append(rows, row)
		}
	} else {
		for _, tableRow := range elementsByTag(table, "tr") {
			var row []string

			for cell := tableRow.FirstChild; cell != nil; cell = cell.NextSibling {
				if cell.Type == html.ElementNode && (cell.Data == "td" || cell.Data == "th") {
					row = append(row, text(cell))
				}
			}

			rows = append(rows, row)
		}
	}

	return rows
}

func (c *semanticCandidate) readParagraphIngredients(text string, node *html.Node, group string) bool {
	ingredients := IngredientGroup{Title: group}
	ended := false

	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if recipetext.OtherHeading(semanticPlain(line)) {
			ended = true
			break
		}

		if recipetext.ServingLine(semanticPlain(line)) {
			continue
		}

		if label, found := strings.CutSuffix(line, ":"); found && !semanticAmount.MatchString(line) && len(strings.Fields(label)) <= 5 {
			if len(ingredients.Ingredients) > 0 {
				c.addParagraphGroup(ingredients)
			}

			ingredients = IngredientGroup{Title: semanticPlain(label)}

			continue
		}

		ingredients.Ingredients = append(ingredients.Ingredients, Ingredient{Text: line})

		amount := c.statesAmount(line)

		switch {
		case semanticLinkedOnly(node) || semanticProse(line) && (!amount || semanticLong(line)):
			c.uncertain = true
		case amount:
			c.amounts++
		}
	}

	if len(ingredients.Ingredients) > 0 {
		c.addParagraphGroup(ingredients)
	}

	return ended
}

func (c *semanticCandidate) addParagraphGroup(group IngredientGroup) {
	if last := len(c.groups) - 1; last >= 0 && c.groups[last].Title == group.Title {
		c.groups[last].Ingredients = append(c.groups[last].Ingredients, group.Ingredients...)
		return
	}

	c.groups = append(c.groups, group)
}

func (c *semanticCandidate) statesAmount(line string) bool {
	if semanticStatesAmount(line) {
		return true
	}

	parsed := ingredientparser.ParseDetailed(semanticPlain(line), "", c.locale)

	return len(parsed.Ambiguities) > 0 || parsed.Ingredient.Quantity != nil && parsed.Ingredient.Quantity.Kind != recipe.QuantityOpaque
}

func semanticStatesAmount(line string) bool {
	line = strings.TrimLeft(line, semanticBullets+" \t")

	return semanticAmount.MatchString(line) || semanticTrailingAmount.MatchString(line)
}
