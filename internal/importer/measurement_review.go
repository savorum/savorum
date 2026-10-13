package importer

import (
	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/recipe"
)

// ReviewIngredientMeasurements reinterprets authored lines under the document's
// source locale and exposes unresolved measurements to the import review UI.
func ReviewIngredientMeasurements(document *recipe.Document) []Diagnostic {
	for section := range document.IngredientSections {
		for index, ingredient := range document.IngredientSections[section].Ingredients {
			document.IngredientSections[section].Ingredients[index] = ingredientparser.Parse(ingredient.SourceText, ingredient.ID, ReadingLocale(*document))
		}
	}

	return ingredientMeasurementDiagnostics(*document)
}

func ingredientMeasurementDiagnostics(document recipe.Document) []Diagnostic {
	targets := unresolvedIngredientTargets(document)
	if len(targets) == 0 {
		return nil
	}

	return []Diagnostic{{
		Code: "ingredient-review-required", Class: DiagnosticActionable, Severity: SeverityWarning,
		Area: "ingredients", Count: len(targets), Targets: targets,
		Message: "Some ingredient measurements need review. Choose their meaning or keep the original wording.",
	}}
}
