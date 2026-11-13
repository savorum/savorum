package units

import (
	"slices"

	"github.com/savorum/savorum/internal/recipe"
)

// UnitChoices lists editor units for a measuring style. Counts excludes serving units.
type UnitChoices struct {
	Volume []string
	Mass   []string
	Counts []string
}

var styleChoices = map[MeasurementSystem]UnitChoices{
	MeasureMetric: {
		Volume: []string{"volume.teaspoon.metric.culinary", "volume.tablespoon.metric.culinary", "volume.cup.metric.culinary", "volume.milliliter", "volume.liter"},
		Mass:   []string{"mass.gram", "mass.kilogram", "mass.ounce.avoirdupois", "mass.pound.avoirdupois"},
	},
	MeasureUS: {
		Volume: []string{"volume.teaspoon.us.customary", "volume.tablespoon.us.customary", "volume.cup.us.customary", "volume.milliliter", "volume.liter"},
		Mass:   []string{"mass.ounce.avoirdupois", "mass.pound.avoirdupois", "mass.gram", "mass.kilogram"},
	},
	MeasureAU: {
		Volume: []string{"volume.teaspoon.au", "volume.tablespoon.au", "volume.cup.au", "volume.milliliter", "volume.liter"},
		Mass:   []string{"mass.gram", "mass.kilogram", "mass.ounce.avoirdupois", "mass.pound.avoirdupois"},
	},
}

// ChoicesFor reports the measures an editor offers for a style.
func (c Catalog) ChoicesFor(system MeasurementSystem) (UnitChoices, bool) {
	choices, found := styleChoices[system]
	if !found {
		return UnitChoices{}, false
	}

	for _, entry := range c.Entries() {
		if entry.Unit.Kind != recipe.UnitPhysical && entry.Unit.ID != "count.serving" {
			choices.Counts = append(choices.Counts, entry.Unit.ID)
		}
	}

	choices.Volume, choices.Mass = slices.Clone(choices.Volume), slices.Clone(choices.Mass)

	return choices, true
}
