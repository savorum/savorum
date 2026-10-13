package units_test

import (
	"errors"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func TestDefaultCatalogKeepsRegionalUnitsDistinct(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()
	for _, id := range []string{"volume.cup.au", "volume.cup.us.customary", "volume.cup.us.culinary", "volume.cup.metric.culinary", "volume.cup.us.legal", "volume.teaspoon.metric.culinary", "volume.teaspoon.us.legal", "volume.tablespoon.us.legal", "volume.pint.imperial", "volume.pint.us.liquid"} {
		unit, found := catalog.Lookup(id)
		if !found {
			t.Errorf("catalog does not contain %s", id)
			continue
		}

		if unit.Kind != recipe.UnitPhysical || unit.Dimension != recipe.DimensionVolume {
			t.Errorf("%s has incorrect semantics: %#v", id, unit)
		}
	}

	for _, ambiguous := range []string{"volume.cup", "volume.cup.metric", "volume.teaspoon.metric", "volume.tablespoon.metric", "volume.pint"} {
		if _, found := catalog.Lookup(ambiguous); found {
			t.Fatalf("ambiguous %s must not be a registered standard unit", ambiguous)
		}
	}
}

func TestConversionsUseExactRegionalDefinitions(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()

	tests := []struct {
		name     string
		value    recipe.Rational
		from     string
		to       string
		expected string
		exact    bool
	}{
		{name: "Australian tablespoon to US culinary tablespoon", value: "1", from: "volume.tablespoon.au", to: "volume.tablespoon.us.culinary", expected: "4/3", exact: false},
		{name: "US customary cup to milliliters", value: "1", from: "volume.cup.us.customary", to: "volume.milliliter", expected: "473176473/2000000", exact: true},
		{name: "fahrenheit to celsius", value: "350", from: "temperature.fahrenheit", to: "temperature.celsius", expected: "530/3", exact: true},
		{name: "negative forty is shared", value: "-40", from: "temperature.fahrenheit", to: "temperature.celsius", expected: "-40", exact: true},
		{name: "international inch to millimeters", value: "3/2", from: "length.inch.international", to: "length.millimeter", expected: "381/10", exact: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			converted, err := catalog.Convert(test.value, test.from, test.to)
			if err != nil {
				t.Fatal(err)
			}

			if converted.String() != test.expected || converted.Exact() != test.exact {
				t.Fatalf("conversion = %s exact=%v, want %s exact=%v", converted.String(), converted.Exact(), test.expected, test.exact)
			}

			exposed := converted.Rat()
			exposed.SetInt64(0)

			if converted.String() != test.expected {
				t.Fatal("Conversion.Rat exposed mutable internal state")
			}
		})
	}
}

func TestConversionRejectsFalseSemantics(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()

	for _, test := range []struct {
		name  string
		value recipe.Rational
		from  string
		to    string
		err   error
	}{
		{name: "volume to mass", value: "1", from: "volume.cup.au", to: "mass.gram", err: units.ErrDimensionMismatch},
		{name: "count conversion", value: "2", from: "count.clove", to: "count.item", err: units.ErrNotPhysical},
		{name: "unknown unit", value: "1", from: "volume.cup", to: "volume.milliliter", err: units.ErrUnknownUnit},
		{name: "negative mass", value: "-1", from: "mass.gram", to: "mass.kilogram", err: units.ErrNegativeAmount},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := catalog.Convert(test.value, test.from, test.to)
			if !errors.Is(err, test.err) {
				t.Fatalf("expected %v, got %v", test.err, err)
			}
		})
	}
}

func TestCatalogRejectsInvalidDefinitions(t *testing.T) {
	t.Parallel()

	physical := recipe.Unit{ID: "mass.bad", Kind: recipe.UnitPhysical, Dimension: recipe.DimensionMass}
	for _, definitions := range [][]units.Definition{
		{{Unit: physical, Kind: units.DefinitionLinear, BaseUnit: "mass.missing", Multiplier: "1", Exact: true}},
		{{Unit: physical, Kind: units.DefinitionBase}, {Unit: physical, Kind: units.DefinitionBase}},
		{{Unit: physical, Kind: units.DefinitionLinear, BaseUnit: "mass.bad", Multiplier: "0", Exact: true}},
		{{Unit: recipe.Unit{ID: "count.bad", Kind: recipe.UnitCount}, Kind: units.DefinitionLinear, BaseUnit: "mass.bad", Multiplier: "1"}},
		{{Unit: recipe.Unit{ID: "bad_unit", Kind: recipe.UnitCount}, Kind: units.DefinitionNone}},
		{{Unit: recipe.Unit{ID: "mass.one", Kind: recipe.UnitPhysical, Dimension: recipe.DimensionMass}, Kind: units.DefinitionBase}, {Unit: recipe.Unit{ID: "mass.two", Kind: recipe.UnitPhysical, Dimension: recipe.DimensionMass}, Kind: units.DefinitionBase}},
	} {
		if _, err := units.NewCatalog(definitions); err == nil {
			t.Fatalf("invalid catalog unexpectedly succeeded: %#v", definitions)
		}
	}
}

func TestDefaultCatalogDistinguishesNonphysicalUnits(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()

	clove, found := catalog.Lookup("count.clove")
	if !found || clove.Kind != recipe.UnitCount || clove.Dimension != "" {
		t.Fatalf("count.clove has incorrect semantics: %#v", clove)
	}

	can, found := catalog.Lookup("container.can")
	if !found || can.Kind != recipe.UnitContainer || can.Dimension != "" {
		t.Fatalf("container.can has incorrect semantics: %#v", can)
	}
}

func TestRegistryEntriesDescribeEveryUnitOnce(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()
	entries := catalog.Entries()

	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if seen[entry.Unit.ID] {
			t.Fatalf("%s appears twice in the registry", entry.Unit.ID)
		}

		seen[entry.Unit.ID] = true
		if _, found := catalog.Lookup(entry.Unit.ID); !found {
			t.Errorf("%s is published but cannot be looked up", entry.Unit.ID)
		}

		if entry.Unit.Kind != recipe.UnitPhysical {
			if entry.BaseUnit != "" || entry.Multiplier != "" || entry.Offset != "" {
				t.Errorf("%s is nonphysical yet publishes a transformation", entry.Unit.ID)
			}

			continue
		}

		if entry.Convention == units.ConventionNone {
			t.Errorf("%s does not say which measuring convention it belongs to", entry.Unit.ID)
		}

		if entry.BaseUnit == "" || entry.Multiplier == "" || entry.Offset == "" {
			t.Errorf("%s does not publish a complete transformation: %#v", entry.Unit.ID, entry)
		}

		base, found := catalog.Lookup(entry.BaseUnit)
		if !found || base.Dimension != entry.Unit.Dimension {
			t.Errorf("%s converts through %q, which is not its dimensional base", entry.Unit.ID, entry.BaseUnit)
		}
	}

	if len(entries) == 0 {
		t.Fatal("the registry published no units")
	}
}

func TestRegistryPublishesCanonicalTransformations(t *testing.T) {
	t.Parallel()

	expected := map[string]units.Entry{
		"mass.gram":                 {Convention: units.ConventionMetric, BaseUnit: "mass.gram", Multiplier: "1", Offset: "0", Exact: true},
		"mass.ounce.avoirdupois":    {Convention: units.ConventionUS, BaseUnit: "mass.gram", Multiplier: "45359237/1600000", Offset: "0", Exact: true},
		"volume.cup.au":             {Convention: units.ConventionAU, BaseUnit: "volume.milliliter", Multiplier: "250", Offset: "0", Exact: true},
		"volume.cup.us.culinary":    {Convention: units.ConventionUS, BaseUnit: "volume.milliliter", Multiplier: "240", Offset: "0", Exact: false},
		"temperature.fahrenheit":    {Convention: units.ConventionUS, BaseUnit: "temperature.celsius", Multiplier: "5/9", Offset: "-160/9", Exact: true},
		"length.inch.international": {Convention: units.ConventionUS, BaseUnit: "length.millimeter", Multiplier: "127/5", Offset: "0", Exact: true},
	}
	for _, entry := range units.DefaultCatalog().Entries() {
		want, checked := expected[entry.Unit.ID]
		if !checked {
			continue
		}

		delete(expected, entry.Unit.ID)

		if entry.Convention != want.Convention || entry.BaseUnit != want.BaseUnit || entry.Multiplier != want.Multiplier || entry.Offset != want.Offset || entry.Exact != want.Exact {
			t.Errorf("%s publishes %#v, want %#v", entry.Unit.ID, entry, want)
		}
	}

	for id := range expected {
		t.Errorf("%s is no longer published by the registry", id)
	}
}

func TestCatalogRequiresAMeasuringConvention(t *testing.T) {
	t.Parallel()

	physical := recipe.Unit{ID: "mass.gram", Kind: recipe.UnitPhysical, Dimension: recipe.DimensionMass}
	if _, err := units.NewCatalog([]units.Definition{{Unit: physical, Kind: units.DefinitionBase}}); err == nil {
		t.Fatal("a physical unit without a convention was accepted")
	}

	if _, err := units.NewCatalog([]units.Definition{{Unit: physical, Convention: "cubits", Kind: units.DefinitionBase}}); err == nil {
		t.Fatal("an unregistered convention was accepted")
	}
}

// TestImperialVolumesAreTheirOwnConvention: an imperial pint and a US liquid
// pint are the same word and a fifth of a litre apart.
func TestImperialVolumesAreTheirOwnConvention(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()
	for _, id := range []string{
		"volume.fluidounce.imperial", "volume.pint.imperial",
		"volume.quart.imperial", "volume.gallon.imperial",
	} {
		if convention, found := conventionOf(catalog, id); !found {
			t.Errorf("catalog does not contain %s", id)
		} else if convention != units.ConventionImperial {
			t.Errorf("%s has convention %q, want imperial", id, convention)
		}
	}

	for _, id := range []string{"volume.pint.us.liquid", "volume.cup.us.legal", "volume.fluidounce.us"} {
		if convention, found := conventionOf(catalog, id); !found {
			t.Errorf("catalog does not contain %s", id)
		} else if convention != units.ConventionUS {
			t.Errorf("%s has convention %q, want us", id, convention)
		}
	}
}

// TestVolumeFamiliesConvertExactly pins the registry definitions the exchange
// format states, in both directions across the two systems.
func TestVolumeFamiliesConvertExactly(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()

	tests := []struct {
		name     string
		value    recipe.Rational
		from     string
		to       string
		expected string
	}{
		{name: "US gallon is 128 US fluid ounces", value: "1", from: "volume.gallon.us.liquid", to: "volume.fluidounce.us", expected: "128"},
		{name: "imperial gallon is 160 imperial fluid ounces", value: "1", from: "volume.gallon.imperial", to: "volume.fluidounce.imperial", expected: "160"},
		{name: "imperial pint to milliliters", value: "1", from: "volume.pint.imperial", to: "volume.milliliter", expected: "454609/800"},
		{name: "US liquid pint to milliliters", value: "1", from: "volume.pint.us.liquid", to: "volume.milliliter", expected: "473176473/1000000"},
		{name: "legal cup is exactly 240 mL", value: "1", from: "volume.cup.us.legal", to: "volume.milliliter", expected: "240"},
		{name: "metric culinary cup is exactly 250 mL", value: "1", from: "volume.cup.metric.culinary", to: "volume.milliliter", expected: "250"},
		{name: "deciliters to milliliters", value: "2", from: "volume.deciliter", to: "volume.milliliter", expected: "200"},
		{name: "centiliters to milliliters", value: "20", from: "volume.centiliter", to: "volume.milliliter", expected: "200"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			converted, err := catalog.Convert(test.value, test.from, test.to)
			if err != nil {
				t.Fatal(err)
			}

			if !converted.Exact() {
				t.Errorf("%s reported an inexact conversion", test.name)
			}

			if converted.String() != test.expected {
				t.Errorf("%s = %s, want %s", test.name, converted.String(), test.expected)
			}
		})
	}
}

// TestLegalCupIsNotACustomaryOrCulinaryCup: three cups an American recipe
// might mean, as three identities.
func TestLegalCupIsNotACustomaryOrCulinaryCup(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()
	millilitres := func(id string) string {
		value, err := catalog.Convert("1", id, "volume.milliliter")
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}

		return value.String()
	}
	exactness := func(id string) bool {
		value, err := catalog.Convert("1", id, "volume.milliliter")
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}

		return value.Exact()
	}
	legal := millilitres("volume.cup.us.legal")
	customary := millilitres("volume.cup.us.customary")
	culinary := millilitres("volume.cup.us.culinary")

	if legal == customary {
		t.Errorf("the legal and customary cups collapsed onto %s mL", legal)
	}

	if legal != "240" || customary != "473176473/2000000" {
		t.Errorf("legal=%s customary=%s, want 240 and 473176473/2000000", legal, customary)
	}
	// The culinary cup is the same magnitude as the legal one but is an
	// inexact kitchen rounding rather than a defined quantity, so they must
	// stay separate identities even though the numbers agree.
	if culinary != "240" {
		t.Errorf("culinary cup = %s mL, want 240", culinary)
	}

	if exactness("volume.cup.us.culinary") {
		t.Error("the culinary cup reported an exact definition")
	}

	if !exactness("volume.cup.us.legal") {
		t.Error("the legal cup reported an inexact definition")
	}
}

// conventionOf finds a unit's measuring convention through the published
// registry, which is the only place the catalog states it.
func conventionOf(catalog units.Catalog, id string) (units.Convention, bool) {
	for _, entry := range catalog.Entries() {
		if entry.Unit.ID == id {
			return entry.Convention, true
		}
	}

	return units.ConventionNone, false
}
