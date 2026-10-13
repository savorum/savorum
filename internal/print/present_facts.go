package print

import (
	"math/big"
	"slices"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
)

func (p presenter) presentYields(document recipe.Document, preferred *int) (*PrintYield, []PrintYield) {
	shown, index := document.DisplayYield(preferred)
	if shown == nil {
		return nil, nil
	}

	var others []PrintYield

	for position := range document.Yields {
		if position == index {
			continue
		}

		if printed := p.presentYield(&document.Yields[position]); printed != nil {
			others = append(others, *printed)
		}
	}

	return p.presentYield(shown), others
}

// Only a whole-recipe statement follows the batch; a serving stays a serving.
func (p presenter) presentNutrition(document recipe.Document) []PrintNutrition {
	result := make([]PrintNutrition, 0, len(document.Nutrition))

	for _, statement := range document.Nutrition {
		follows := statement.Basis.Kind == recipe.NutritionBasisRecipe

		factor := big.NewRat(1, 1)
		if follows {
			factor = p.scale
		}

		printed := PrintNutrition{Basis: p.nutritionBasis(statement.Basis), Note: strings.TrimSpace(statement.Note)}
		if p.scaled() {
			if follows {
				printed.Basis = p.label("nutrition_basis_batch")
			} else {
				printed.AsWritten = true
			}
		}

		for _, nutrient := range statement.Nutrients {
			printed.Values = append(printed.Values, PrintNutrient{
				Label: nutrientLabel(nutrient, p.locale),
				Value: p.nutrientValue(nutrient, factor),
			})
		}

		result = append(result, printed)
	}

	return result
}

func (p presenter) nutritionBasis(basis recipe.NutritionBasis) string {
	text := strings.TrimSpace(basis.Text)
	if text == "" && basis.Quantity != nil {
		text = newPresenter(p.catalog, p.system, nil, p.locale).presentQuantity(*basis.Quantity).Text
	}

	switch basis.Kind {
	case recipe.NutritionBasisRecipe:
		return p.label("nutrition_basis_recipe")
	case recipe.NutritionBasisServing:
		if text != "" {
			return p.label("nutrition_basis_serving_of") + text
		}

		return p.label("nutrition_basis_serving")
	case recipe.NutritionBasisQuantity:
		if text != "" {
			if strings.TrimSpace(basis.Text) == "" {
				return p.label("nutrition_basis_per") + text
			}

			return text
		}
	case recipe.NutritionBasisUnspecified:
	}

	if text != "" {
		return text
	}

	return p.label("nutrition_basis_unspecified")
}

func (p presenter) nutrientValue(nutrient recipe.Nutrient, factor *big.Rat) string {
	if nutrient.Amount == nil {
		return strings.TrimSpace(nutrient.Text)
	}

	amount := *nutrient.Amount
	scaled := func(value *recipe.Rational) string {
		rat, err := value.Rat()
		if err != nil {
			return string(*value)
		}

		number := new(big.Rat).Mul(rat, factor)

		precision, exact := number.FloatPrec()
		if exact && precision <= 128 {
			return p.localizeNumber(number.FloatString(precision))
		}

		return p.localizeNumber(number.RatString())
	}

	var text string

	switch {
	case amount.Value != nil:
		text = scaled(amount.Value)
	case amount.Min != nil && amount.Max != nil:
		text = scaled(amount.Min) + "–" + scaled(amount.Max)
	}

	if nutrient.Unit != "" {
		text += " " + string(nutrient.Unit)
	}

	if amount.Approximate {
		text = p.label("about") + text
	}

	return text
}

func nutrientLabel(nutrient recipe.Nutrient, locale string) string {
	if label := strings.TrimSpace(nutrient.Label); label != "" {
		return label
	}

	return printMessage("nutrient_"+string(nutrient.Nutrient), locale)
}

func (p presenter) presentClaims(document recipe.Document) []string {
	var lines []string

	for _, claim := range document.DietaryClaims {
		lines = append(lines, claimLine(claim.Text, claim.Condition, p.variantTitle(document, claim.VariantRef)))
	}

	for _, declaration := range document.AllergenDeclarations {
		line := claimLine(declaration.Text, declaration.Condition, p.variantTitle(document, declaration.VariantRef))
		if !containsLine(lines, line) {
			lines = append(lines, line)
		}
	}

	return lines
}

func claimLine(text, condition, variant string) string {
	line := strings.TrimSpace(text)
	if condition = strings.TrimSpace(condition); condition != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(condition)) {
		line += " (" + condition + ")"
	}

	if variant != "" {
		line += " — " + variant
	}

	return line
}

func (p presenter) variantTitle(document recipe.Document, id string) string {
	if id == "" {
		return ""
	}

	for index, variant := range document.Variants {
		if variant.ID == id {
			if title := strings.TrimSpace(variant.Title); title != "" {
				return title
			}

			return p.label("variant_number") + p.number(big.NewRat(int64(index+1), 1))
		}
	}

	return ""
}

func containsLine(lines []string, line string) bool {
	return slices.Contains(lines, line)
}

func imageCredit(asset recipe.Asset) string {
	if credit := strings.TrimSpace(asset.Credit); credit != "" {
		return credit
	}

	parts := make([]string, 0, 3)
	if creator := strings.TrimSpace(asset.Creator); creator != "" {
		parts = append(parts, creator)
	}

	if asset.License != nil {
		if name := strings.TrimSpace(asset.License.Name); name != "" {
			parts = append(parts, name)
		} else if asset.License.URL != "" {
			parts = append(parts, asset.License.URL)
		}
	}

	if rights := strings.TrimSpace(asset.Rights); rights != "" && len(parts) == 0 {
		parts = append(parts, rights)
	}

	return strings.Join(parts, " · ")
}
