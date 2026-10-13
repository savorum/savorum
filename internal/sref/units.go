package sref

import (
	"fmt"

	"github.com/savorum/savorum/internal/recipe"
)

// validateKnownUnitIDs enforces the exact-registry rule. Unknown syntactically
// valid IDs are safe only when the document retains a newer compatible
// registry version that may define them.
func validateKnownUnitIDs(document recipe.Document, catalog recipe.UnitCatalog) error {
	for index, value := range document.Yields {
		if err := validateQuantityUnitIDs(fmt.Sprintf("yields[%d].quantity", index), value.Quantity, catalog); err != nil {
			return err
		}
	}

	for index, statement := range document.Nutrition {
		if err := validateQuantityUnitIDs(fmt.Sprintf("nutrition[%d].basis.quantity", index), statement.Basis.Quantity, catalog); err != nil {
			return err
		}
	}

	for index, equipment := range document.Equipment {
		if err := validateQuantityUnitIDs(fmt.Sprintf("equipment[%d].quantity", index), equipment.Quantity, catalog); err != nil {
			return err
		}
	}

	for sectionIndex, section := range document.IngredientSections {
		for ingredientIndex, ingredient := range section.Ingredients {
			path := fmt.Sprintf("ingredient_sections[%d].ingredients[%d]", sectionIndex, ingredientIndex)
			if err := validateIngredientUnitIDs(path, ingredient, catalog); err != nil {
				return err
			}
		}
	}

	for sectionIndex, section := range document.InstructionSections {
		for stepIndex, step := range section.Steps {
			for temperatureIndex, temperature := range step.Temperatures {
				path := fmt.Sprintf("instruction_sections[%d].steps[%d].temperatures[%d].unit", sectionIndex, stepIndex, temperatureIndex)
				if err := validateUnitID(path, temperature.Unit, catalog); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func validateUnitID(path, id string, catalog recipe.UnitCatalog) error {
	if id == "" {
		return nil
	}

	if _, known := catalog.Lookup(id); !known {
		return shapeFailure("unknown-unit", "%s names unknown unit %q in registry %s", path, id, UnitRegistryVersion)
	}

	return nil
}

func validateQuantityUnitIDs(path string, quantity *recipe.Quantity, catalog recipe.UnitCatalog) error {
	if quantity == nil {
		return nil
	}

	if err := validateUnitID(path+".unit", quantity.Unit, catalog); err != nil {
		return err
	}

	for index, term := range quantity.Terms {
		if err := validateUnitID(fmt.Sprintf("%s.terms[%d].unit", path, index), term.Unit, catalog); err != nil {
			return err
		}
	}

	for index, option := range quantity.Options {
		optionPath := fmt.Sprintf("%s.options[%d]", path, index)
		if err := validateUnitID(optionPath+".unit", option.Unit, catalog); err != nil {
			return err
		}

		for termIndex, term := range option.Terms {
			termPath := fmt.Sprintf("%s.terms[%d].unit", optionPath, termIndex)
			if err := validateUnitID(termPath, term.Unit, catalog); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateIngredientUnitIDs(path string, ingredient recipe.Ingredient, catalog recipe.UnitCatalog) error {
	if err := validateQuantityUnitIDs(path+".quantity", ingredient.Quantity, catalog); err != nil {
		return err
	}

	if ingredient.PackageSize != nil {
		if err := validateUnitID(path+".package_size.unit", ingredient.PackageSize.Unit, catalog); err != nil {
			return err
		}
	}

	if ingredient.Temperature != nil {
		if err := validateUnitID(path+".temperature.unit", ingredient.Temperature.Unit, catalog); err != nil {
			return err
		}
	}

	for index, branch := range ingredient.Alternatives {
		if err := validateIngredientUnitIDs(fmt.Sprintf("%s.alternatives[%d]", path, index), branch, catalog); err != nil {
			return err
		}
	}

	return nil
}
