package print

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
)

func (p presenter) presentIngredients(document recipe.Document) []PrintIngredientGroup {
	groups := make([]PrintIngredientGroup, 0, len(document.IngredientSections))
	for _, section := range document.IngredientSections {
		group := PrintIngredientGroup{Title: strings.TrimSpace(section.Title)}
		for _, ingredient := range section.Ingredients {
			group.Ingredients = append(group.Ingredients, p.presentIngredient(ingredient))
		}

		groups = append(groups, group)
	}

	return groups
}

// Share only exact amounts in the same authored unit and package context.
// Ranges, approximations and compound quantities retain every branch amount.
func sharedAlternativeQuantity(ingredient recipe.Ingredient) *recipe.Quantity {
	if len(ingredient.Alternatives) < 2 {
		return nil
	}

	first := ingredient.Alternatives[0]
	if first.Quantity == nil {
		return nil
	}

	for _, branch := range ingredient.Alternatives[1:] {
		if branch.Quantity == nil || !exactAmountsMatch(first.Quantity.Kind, first.Quantity.Amount, first.Quantity.Unit, branch.Quantity.Kind, branch.Quantity.Amount, branch.Quantity.Unit) {
			return nil
		}

		if first.PackageSize == nil && branch.PackageSize == nil {
			continue
		}

		if first.PackageSize == nil || branch.PackageSize == nil || !exactAmountsMatch(first.PackageSize.Kind, &first.PackageSize.Amount, first.PackageSize.Unit, branch.PackageSize.Kind, &branch.PackageSize.Amount, branch.PackageSize.Unit) {
			return nil
		}
	}

	return first.Quantity
}

func exactAmountsMatch(leftKind recipe.QuantityKind, left *recipe.Amount, leftUnit string, rightKind recipe.QuantityKind, right *recipe.Amount, rightUnit string) bool {
	if leftKind != recipe.QuantitySimple || rightKind != recipe.QuantitySimple || leftUnit == "" || leftUnit != rightUnit || left == nil || right == nil {
		return false
	}

	if left.Value == nil || right.Value == nil || left.Approximate || right.Approximate || left.Min != nil || left.Max != nil || right.Min != nil || right.Max != nil {
		return false
	}

	leftValue, leftOK := new(big.Rat).SetString(string(*left.Value))
	rightValue, rightOK := new(big.Rat).SetString(string(*right.Value))

	return leftOK && rightOK && leftValue.Cmp(rightValue) == 0
}

func (p presenter) presentIngredientAlternatives(ingredient recipe.Ingredient) PrintIngredient {
	parts := make([]string, 0, len(ingredient.Alternatives))
	branches := make([]PrintIngredient, 0, len(ingredient.Alternatives))

	shared := sharedAlternativeQuantity(ingredient)

	var sharedAmount PrintAmount
	if shared != nil {
		sharedAmount = p.presentQuantity(*shared)
	}

	var keptAmount, keptWording bool

	for _, branch := range ingredient.Alternatives {
		shown := p.presentIngredient(branch)

		switch shown.AsWritten {
		case p.label("amount_as_written"):
			keptAmount = true
		case p.label("wording_as_written"):
			keptWording = true
		}

		amount := shown.Amount.Text
		if shared != nil {
			amount = ""
			shown.Amount = PrintAmount{}
		}

		text := strings.TrimSpace(amount + " " + shown.Text)
		if branch.Optional {
			text += p.label("optional")
			shown.Text += p.label("optional")
		}

		parts = append(parts, text)
		shown.AsWritten = ""
		branches = append(branches, shown)
	}

	joiner := p.label("or")
	if ingredient.Relation == "and_or" {
		joiner = p.label("and_or")
	}

	text := strings.Join(parts, joiner)

	if ingredient.Note != "" {
		text += " — " + ingredient.Note
	}

	if ingredient.Optional {
		text += p.label("optional")
	}

	// One choice between branches is one row, so it carries one answer about
	// the batch. A shared amount answers for all of them; otherwise a branch
	// that kept its own amount makes the whole row a retained one.
	row := PrintIngredient{Amount: sharedAmount, Text: text}

	choice := &PrintIngredientChoice{
		Preferred:    ingredient.Relation == "preferred",
		SharedAmount: shared != nil,
		Joiner:       strings.Trim(joiner, " \t\n\r;,，；、،؛:："),
		Branches:     branches,
		Note:         ingredient.Note,
	}
	if ingredient.Optional {
		choice.Note = strings.TrimSpace(choice.Note + p.label("optional"))
	}

	row.Choice = choice

	if p.scaled() {
		if shared != nil {
			keptAmount = !p.amountFollowsBatch(shared)
		}

		switch {
		case keptAmount:
			row.AsWritten = p.label("amount_as_written")
		case keptWording || statesNumber(ingredient.Note):
			row.AsWritten = p.label("wording_as_written")
		}
	}

	return row
}

func (p presenter) presentIngredient(ingredient recipe.Ingredient) PrintIngredient {
	if len(ingredient.Alternatives) > 0 {
		return p.presentIngredientAlternatives(ingredient)
	}

	var amount PrintAmount
	if ingredient.Quantity != nil {
		amount = p.presentQuantity(*ingredient.Quantity)
	}
	// Retain unparsed measures as original source text.
	if amount.Empty() && strings.TrimSpace(ingredient.SourceText) != "" {
		source := strings.TrimSpace(ingredient.SourceText)

		row := PrintIngredient{Text: source}
		if p.scaled() && statesNumber(source) {
			row.AsWritten = p.label("amount_as_written")
		}

		return row
	}

	name := strings.TrimSpace(ingredient.Name)
	if ingredient.PackageSize != nil {
		// A package size describes the tin, not the batch, so it is never
		// multiplied by the batch factor.
		size := newPresenter(p.catalog, p.system, big.NewRat(1, 1), p.locale).presentSimple(*ingredient.PackageSize)
		if !size.Empty() {
			name = fmt.Sprintf("%s (%s)", name, size.Text)
		}
	}

	text := withNote(name, strings.TrimSpace(ingredient.Note))
	if ingredient.Recipe != nil && strings.TrimSpace(ingredient.Recipe.Title) != "" {
		text += " (" + p.label("see") + strings.TrimSpace(ingredient.Recipe.Title) + ")"
	}

	return PrintIngredient{Amount: amount, Text: text, AsWritten: p.retention(ingredient.Quantity, text)}
}

// withNote joins a note to its ingredient. A note that opens with a bracket
// reads as an aside, so it takes no comma.
func withNote(name, note string) string {
	switch {
	case note == "":
		return name
	case name == "":
		return note
	case strings.HasPrefix(note, "(") || strings.HasPrefix(note, "[") ||
		strings.HasPrefix(note, "（") || strings.HasPrefix(note, "［"):
		return name + " " + note
	default:
		return name + ", " + note
	}
}

// statesNumber reports whether wording contains digits that were not recalculated
// during scaling, indicating the value should be presented as written.
func statesNumber(text string) bool {
	return strings.ContainsAny(text, "0123456789")
}

// amountFollowsBatch reports whether a quantity reads differently at this batch
// than at the recipe's own. An amount that scales says something new at ×2;
// opaque wording and an amount no reader can parse say what they said at ×1.
func (p presenter) amountFollowsBatch(quantity *recipe.Quantity) bool {
	if quantity == nil {
		return false
	}

	shown := p.presentQuantity(*quantity)
	if shown.Empty() {
		return false
	}

	return shown.Text != newPresenter(p.catalog, p.system, big.NewRat(1, 1), p.locale).presentQuantity(*quantity).Text
}

// retention names what a row kept at a batch that changed the others: its
// amount, or numbers its wording states beside a recalculated amount. A row
// states an amount either through its quantity or by writing one into the words
// the reader sees; "salt and pepper" states none and keeps nothing.
func (p presenter) retention(quantity *recipe.Quantity, text string) string {
	if !p.scaled() {
		return ""
	}

	if !p.amountFollowsBatch(quantity) && (quantity != nil || statesNumber(text)) {
		return p.label("amount_as_written")
	}

	if statesNumber(text) {
		return p.label("wording_as_written")
	}

	return ""
}

func (p presenter) presentEquipment(document recipe.Document) []PrintIngredient {
	if document.Equipment == nil {
		return nil
	}

	items := make([]PrintIngredient, 0, len(document.Equipment))
	// Equipment quantity is never multiplied by the displayed recipe batch.
	unscaled := newPresenter(p.catalog, p.system, big.NewRat(1, 1), p.locale)

	for _, equipment := range document.Equipment {
		var amount PrintAmount
		if equipment.Quantity != nil {
			amount = unscaled.presentQuantity(*equipment.Quantity)
		}

		parts := []string{strings.TrimSpace(equipment.Name)}
		if note := strings.TrimSpace(equipment.Note); note != "" {
			parts = append(parts, note)
		}

		// A pan is not multiplied by the batch, so it is not a row that could
		// have followed one; marking it would invent a distinction.
		items = append(items, PrintIngredient{Amount: amount, Text: strings.Join(parts, ", ")})
	}

	return items
}
