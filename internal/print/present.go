package print

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

// presenter resolves one recipe revision into the words a reader will see. It
// is the only place print scaling, conversion, rounding, and labelling happen.
type presenter struct {
	catalog              units.Catalog
	system               units.MeasurementSystem
	scale                *big.Rat
	locale               string
	microwaveOutputWatts int
}

func newPresenter(catalog units.Catalog, system units.MeasurementSystem, scale *big.Rat, locales ...string) presenter {
	if scale == nil {
		scale = big.NewRat(1, 1)
	}

	locale := "en"
	if len(locales) > 0 {
		locale = locales[0]
	}

	return presenter{catalog: catalog, system: system, scale: scale, locale: locale}
}

func (p presenter) scaled() bool {
	return p.scale.Cmp(big.NewRat(1, 1)) != 0
}

// present writes one recipe snapshot as the document a cook will read.
func (p presenter) present(snapshot RecipeSnapshot, content Content) PrintRecipe {
	document := snapshot.Recipe

	out := PrintRecipe{
		EntryID:  string(snapshot.EntryID),
		RecipeID: string(document.ID),
		Revision: snapshot.Revision,
		Title:    strings.TrimSpace(document.Title),
		Language: document.Language,
	}
	if out.Title == "" {
		out.Title = p.label("untitled_recipe")
	}

	out.Direction = recipe.LanguageDirection(document.Language)
	if out.Direction == "" {
		out.Direction = recipeTextDirection(document)
	}

	if content.Description {
		out.Description = strings.TrimSpace(document.Description)
	}

	out.Yield, out.OtherYields = p.presentYields(document, snapshot.PreferredYield)
	out.Timings = p.presentTimings(document)
	out.Classifications = presentClassifications(document, p.locale)
	out.IngredientGroups = p.presentIngredients(document)
	out.Equipment = p.presentEquipment(document)

	out.StepGroups = p.presentSteps(document)
	if content.Notes {
		for _, note := range document.Notes {
			if trimmed := strings.TrimSpace(note); trimmed != "" {
				out.Notes = append(out.Notes, trimmed)
			}
		}
	}

	if content.Nutrition {
		out.Nutrition = p.presentNutrition(document)
	}

	if content.Claims {
		out.Claims = p.presentClaims(document)
	}

	if content.Source {
		out.Attribution = presentAttribution(document)
	}
	// A recipe written in a regional set of measures says so once, rather than
	// qualifying every line, and only while it is being read as written.
	if p.system == units.MeasureAsWritten {
		switch p.catalog.MeasurementConventionOf(volumeUnitsOf(document)) {
		case units.ConventionUS:
			out.Convention = p.label("us_measures")
		case units.ConventionAU:
			out.Convention = p.label("au_measures")
		case units.ConventionMetric:
			out.Convention = p.label("metric_measures")
		case units.ConventionNone, units.ConventionImperial:
			// A mixed or imperial recipe has no single set to announce.
		}
	}

	if p.scaled() {
		out.Batch = p.number(p.scale) + p.label("batch")
		// Marked rows say which amounts stayed put; this says, once, that some did.
		if anyRetained(out.IngredientGroups) {
			out.KeptAsWritten = p.label("kept_as_written")
		}
	}

	return out
}

func anyRetained(groups []PrintIngredientGroup) bool {
	for _, group := range groups {
		for _, row := range group.Ingredients {
			if row.AsWritten != "" {
				return true
			}
		}
	}

	return false
}

func presentClassifications(document recipe.Document, locales ...string) []string {
	var result []string
	if values := nonemptyLabels(document.Courses); len(values) > 0 {
		result = append(result, printMessage("course", locales...)+strings.Join(values, ", "))
	}

	if values := nonemptyLabels(document.Cuisines); len(values) > 0 {
		result = append(result, printMessage("cuisine", locales...)+strings.Join(values, ", "))
	}

	return result
}

func nonemptyLabels(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}

	return result
}

func trimmedList(values []string) []string {
	var out []string

	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}

	return out
}

// volumeUnitsOf collects the measures a recipe's ingredients are written in,
// which is how the convention it was written in is recognised.
func volumeUnitsOf(document recipe.Document) []string {
	var out []string

	add := func(unit string) {
		if unit != "" {
			out = append(out, unit)
		}
	}

	for _, section := range document.IngredientSections {
		entries := append([]recipe.Ingredient(nil), section.Ingredients...)
		for _, ingredient := range section.Ingredients {
			entries = append(entries, ingredient.Alternatives...)
		}

		for _, ingredient := range entries {
			if ingredient.Quantity == nil {
				continue
			}

			add(ingredient.Quantity.Unit)

			for _, term := range ingredient.Quantity.Terms {
				add(term.Unit)
			}

			for _, option := range ingredient.Quantity.Options {
				add(option.Unit)

				for _, term := range option.Terms {
					add(term.Unit)
				}
			}
		}
	}

	return out
}

// parseScale reads a batch factor, which must be a positive exact rational.
func parseScale(value recipe.Rational) (*big.Rat, error) {
	if value == "" {
		return big.NewRat(1, 1), nil
	}

	parsed, err := value.Rat()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidOptions, err)
	}

	if parsed.Sign() <= 0 {
		return nil, fmt.Errorf("%w: a batch factor must be greater than zero", ErrInvalidOptions)
	}

	return parsed, nil
}

func recipeTextDirection(document recipe.Document) string {
	texts := []string{document.Title}
	for _, section := range document.InstructionSections {
		for _, step := range section.Steps {
			texts = append(texts, step.Text)
		}
	}

	if direction := recipe.TextDirection(texts...); direction != "" {
		return direction
	}

	return recipe.LeftToRight
}
