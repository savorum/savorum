package exchange

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

// decisionKind translates the action a person took into the resolution input
// it supplies. Choosing one identity for one measure is an occurrence
// interpretation; choosing a convention is an ingredient convention; keeping
// the wording is neither, and records that a person reviewed it.
func decisionKind(interpretation string) string {
	switch interpretation {
	case ResolutionConvention:
		return DecisionIngredientConvention
	case ResolutionKeepWritten:
		return DecisionReviewedAsWritten
	default:
		return DecisionOccurrenceInterpretation
	}
}

// conventionSelections records what the convention expanded to, so the decision
// can be read back without re-expanding the word under a later registry.
func conventionSelections(interpretation MeasurementResolutionInterpretation) []string {
	if interpretation.Kind != ResolutionConvention {
		return nil
	}

	profile, found := units.DefaultCatalog().ConventionProfileFor(units.Convention(interpretation.Convention))
	if !found {
		return nil
	}

	return profile.Selections()
}

func measurementResolutionRecords(metadata map[string]any) []MeasurementResolutionRecord {
	values, found := metadata["measurement_resolution_plans"]
	if !found {
		return nil
	}

	encoded, err := json.Marshal(values)
	if err != nil {
		return nil
	}

	var records []MeasurementResolutionRecord
	if json.Unmarshal(encoded, &records) != nil {
		return nil
	}

	return records
}

func findIngredient(document *recipe.Document, id string) *recipe.Ingredient {
	for sectionIndex := range document.IngredientSections {
		for ingredientIndex := range document.IngredientSections[sectionIndex].Ingredients {
			ingredient := &document.IngredientSections[sectionIndex].Ingredients[ingredientIndex]
			if ingredient.ID == id {
				return ingredient
			}
		}
	}

	return nil
}

func cloneDocument(document recipe.Document) (recipe.Document, error) {
	encoded, err := json.Marshal(document)
	if err != nil {
		return recipe.Document{}, fmt.Errorf("encode recipe for deep copy: %w", err)
	}

	var result recipe.Document

	if err := json.Unmarshal(encoded, &result); err != nil {
		return recipe.Document{}, fmt.Errorf("decode recipe deep copy: %w", err)
	}

	return result, nil
}

func cloneDiagnostics(values []ImportDiagnostic) ([]ImportDiagnostic, error) {
	// A deep copy encodes and decodes with the same field names, so what those
	// names are does not matter here; ImportDiagnostic is a domain type that the
	// API layer maps into its own tagged model rather than serializing directly.
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode diagnostics for deep copy: %w", err)
	}

	var result []ImportDiagnostic

	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, fmt.Errorf("decode diagnostics deep copy: %w", err)
	}

	return result, nil
}

func cloneMetadata(value map[string]any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode importer metadata for deep copy: %w", err)
	}

	var result map[string]any

	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, fmt.Errorf("decode importer metadata deep copy: %w", err)
	}

	return result, nil
}

func sortedDocumentIDs(values map[string]recipe.Document) []string {
	result := make([]string, 0, len(values))
	for id := range values {
		result = append(result, id)
	}

	sort.Strings(result)

	return result
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}

	return result
}

func pluralResolution(count int, one, many string) string {
	if count == 1 {
		return one
	}

	return many
}
