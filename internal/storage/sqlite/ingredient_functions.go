package sqlite

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type ingredientSearchDocument struct {
	IngredientSections []struct {
		Ingredients []ingredientSearchOccurrence `json:"ingredients"`
	} `json:"ingredient_sections"`
}

type ingredientSearchOccurrence struct {
	Name         string                       `json:"name"`
	SourceText   string                       `json:"source_text"`
	Alternatives []ingredientSearchOccurrence `json:"alternatives"`
}

// ingredientSearchNames indexes leaf names, with original-line fallback only
// when the leaf has no name. Choice wording, notes, and headings are excluded.
func ingredientSearchNames(value string) (string, error) {
	var document ingredientSearchDocument
	if err := json.Unmarshal([]byte(value), &document); err != nil {
		return "", fmt.Errorf("decode ingredient search content: %w", err)
	}

	names := []string{}

	var appendOccurrence func(ingredientSearchOccurrence)

	appendOccurrence = func(ingredient ingredientSearchOccurrence) {
		if len(ingredient.Alternatives) > 0 {
			for _, alternative := range ingredient.Alternatives {
				appendOccurrence(alternative)
			}

			return
		}

		name := strings.TrimSpace(ingredient.Name)
		if name == "" {
			name = strings.TrimSpace(ingredient.SourceText)
		}

		if name != "" {
			names = append(names, name)
		}
	}

	for _, section := range document.IngredientSections {
		for _, ingredient := range section.Ingredients {
			appendOccurrence(ingredient)
		}
	}

	slices.Sort(names)

	encoded, err := json.Marshal(slices.Compact(names))
	if err != nil {
		return "", fmt.Errorf("encode ingredient search content: %w", err)
	}

	return string(encoded), nil
}
