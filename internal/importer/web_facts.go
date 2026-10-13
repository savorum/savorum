package importer

import (
	"math/big"
	"regexp"
	"strings"

	"github.com/savorum/savorum/internal/importer/webrecipe"
	"github.com/savorum/savorum/internal/recipe"
)

func webYields(values []webrecipe.Yield) []recipe.Yield {
	var result []recipe.Yield

	for _, value := range values {
		if text := strings.TrimSpace(value.Text); text != "" {
			result = append(result, recipe.Yield{Text: text})
		}
	}

	return result
}

var nutrientByProperty = map[string]recipe.NutrientKind{
	"calories":              recipe.NutrientEnergy,
	"fatContent":            recipe.NutrientFat,
	"saturatedFatContent":   recipe.NutrientSaturatedFat,
	"transFatContent":       recipe.NutrientTransFat,
	"unsaturatedFatContent": recipe.NutrientUnsaturatedFat,
	"cholesterolContent":    recipe.NutrientCholesterol,
	"carbohydrateContent":   recipe.NutrientCarbohydrate,
	"sugarContent":          recipe.NutrientSugars,
	"fiberContent":          recipe.NutrientFiber,
	"proteinContent":        recipe.NutrientProtein,
	"sodiumContent":         recipe.NutrientSodium,
}

var nutrientValuePattern = regexp.MustCompile(`(?i)^(about|approx\.?|approximately|around|~)?\s*(\d+(?:[.,]\d+)?)(?:\s*(?:-|–|to)\s*(\d+(?:[.,]\d+)?))?\s*(kcal|calories|cals?|kj|g|grams?|mg|milligrams?|mcg|µg|micrograms?)?\.?$`)

// Without a servingSize the basis stays unspecified, even beside a serving count.
func webNutrition(value *webrecipe.Nutrition) []recipe.NutritionStatement {
	if value == nil {
		return nil
	}

	statement := recipe.NutritionStatement{Basis: recipe.NutritionBasis{Kind: recipe.NutritionBasisUnspecified}}
	if value.ServingSize != "" {
		statement.Basis = recipe.NutritionBasis{Kind: recipe.NutritionBasisServing, Text: value.ServingSize}
	}

	for _, property := range value.Values {
		kind, known := nutrientByProperty[property.Property]
		if !known {
			continue
		}

		statement.Nutrients = append(statement.Nutrients, webNutrient(kind, property.Text))
	}

	if len(statement.Nutrients) == 0 {
		return nil
	}

	return []recipe.NutritionStatement{statement}
}

func webNutrient(kind recipe.NutrientKind, text string) recipe.Nutrient {
	opaque := recipe.Nutrient{Nutrient: kind, Text: text}

	match := nutrientValuePattern.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil {
		return opaque
	}

	unit := nutrientUnit(match[4])
	energy := kind == recipe.NutrientEnergy

	if unit != "" && energy != (unit == recipe.NutrientUnitKilocalorie || unit == recipe.NutrientUnitKilojoule) {
		return opaque
	}

	first, ok := decimalRational(match[2])
	if !ok {
		return opaque
	}

	amount := recipe.Amount{Approximate: match[1] != ""}
	if match[3] != "" {
		second, valid := decimalRational(match[3])
		if !valid {
			return opaque
		}

		amount.Min, amount.Max = &first, &second
	} else {
		amount.Value = &first
	}

	nutrient := recipe.Nutrient{Nutrient: kind, Amount: &amount, Unit: unit}
	if canonical := strings.TrimSpace(match[2] + " " + string(unit)); !strings.EqualFold(canonical, strings.TrimSpace(text)) {
		nutrient.SourceText = text
	}

	return nutrient
}

func nutrientUnit(word string) recipe.NutrientUnit {
	switch strings.ToLower(word) {
	case "kcal", "calories", "cal", "cals":
		return recipe.NutrientUnitKilocalorie
	case "kj":
		return recipe.NutrientUnitKilojoule
	case "g", "gram", "grams":
		return recipe.NutrientUnitGram
	case "mg", "milligram", "milligrams":
		return recipe.NutrientUnitMilligram
	case "mcg", "µg", "microgram", "micrograms":
		return recipe.NutrientUnitMicrogram
	default:
		return ""
	}
}

func decimalRational(text string) (recipe.Rational, bool) {
	value, ok := new(big.Rat).SetString(strings.ReplaceAll(text, ",", "."))
	if !ok {
		return "", false
	}

	parsed, err := recipe.ParseRational(value.RatString())

	return parsed, err == nil
}

var dietBySchemaName = map[string]struct {
	diet recipe.Diet
	text string
}{
	"DiabeticDiet":   {recipe.DietDiabetic, "Diabetic"},
	"GlutenFreeDiet": {recipe.DietGlutenFree, "Gluten-free"},
	"HalalDiet":      {recipe.DietHalal, "Halal"},
	"HinduDiet":      {recipe.DietHindu, "Hindu"},
	"KosherDiet":     {recipe.DietKosher, "Kosher"},
	"LowCalorieDiet": {recipe.DietLowCalorie, "Low calorie"},
	"LowFatDiet":     {recipe.DietLowFat, "Low fat"},
	"LowLactoseDiet": {recipe.DietLowLactose, "Low lactose"},
	"LowSaltDiet":    {recipe.DietLowSalt, "Low salt"},
	"VeganDiet":      {recipe.DietVegan, "Vegan"},
	"VegetarianDiet": {recipe.DietVegetarian, "Vegetarian"},
}

func webDietaryClaims(names []string) []recipe.DietaryClaim {
	var result []recipe.DietaryClaim

	for _, name := range names {
		claim := recipe.DietaryClaim{Text: name, Suitability: recipe.SuitabilitySuitable}
		if known, ok := dietBySchemaName[name]; ok {
			claim.Text, claim.Diet = known.text, known.diet
		}

		result = append(result, claim)
	}

	return result
}

func withImageProvenance(asset RemoteAsset, provenance webrecipe.ImageProvenance) RemoteAsset {
	asset.Creator, asset.Credit, asset.Rights = provenance.Creator, provenance.Credit, provenance.Rights
	if provenance.License != "" || provenance.LicenseURL != "" {
		asset.License = &recipe.AssetLicense{Name: provenance.License, URL: provenance.LicenseURL}
	}

	return asset
}
