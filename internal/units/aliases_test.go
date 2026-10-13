package units_test

import (
	"testing"

	"github.com/savorum/savorum/internal/units"
)

func TestAliasLookupUsesTheMostSpecificSourceLocale(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()

	for _, test := range []struct {
		name, form, locale, unit string
	}{
		{name: "US cup", form: "cup", locale: "en-US", unit: "volume.cup.us.customary"},
		{name: "Australian cup", form: "cup", locale: "en-AU", unit: "volume.cup.au"},
		{name: "imperial pint", form: "pint", locale: "en-GB", unit: "volume.pint.imperial"},
		{name: "qualified metric teaspoon", form: "metric teaspoon", locale: "en-CA", unit: "volume.teaspoon.metric.culinary"},
		{name: "language fallback", form: "grams", locale: "en-US", unit: "mass.gram"},
		{name: "extension fallback", form: "cup", locale: "en-US-u-nu-latn", unit: "volume.cup.us.customary"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			unit, resolution := catalog.ResolveAlias(test.form, test.locale)
			if unit != test.unit || resolution != units.AliasResolved {
				t.Fatalf("ResolveAlias(%q, %q) = %q, %v", test.form, test.locale, unit, resolution)
			}
		})
	}
}

func TestAliasLookupNeverSearchesSiblingOrUnscopedLocales(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()
	for _, locale := range []string{"", "en", "en-CA", "fr-CA"} {
		if unit, resolution := catalog.ResolveAlias("cup", locale); unit != "" || resolution != units.AliasUnknown {
			t.Errorf("cup under %q = %q, %v", locale, unit, resolution)
		}
	}
}

// A bare teaspoon needs a region exactly as a bare cup does.
func TestABareTeaspoonIsAsRegionBoundAsABareCup(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()

	for _, form := range []string{"tsp", "tsp.", "teaspoon", "teaspoons"} {
		for _, locale := range []string{"", "en", "en-CA"} {
			if unit, resolution := catalog.ResolveAlias(form, locale); unit != "" || resolution != units.AliasUnknown {
				t.Errorf("%q under %q = %q, %v", form, locale, unit, resolution)
			}
		}

		if unit, _ := catalog.ResolveAlias(form, "en-US"); unit != "volume.teaspoon.us.customary" {
			t.Errorf("%q under en-US = %q", form, unit)
		}

		if unit, _ := catalog.ResolveAlias(form, "en-AU"); unit != "volume.teaspoon.au" {
			t.Errorf("%q under en-AU = %q", form, unit)
		}

		// The form still names an interpretation a person can be offered. An
		// alias that stops resolving must not become an unknown word the
		// review workflow has no question for.
		descriptor, found := catalog.DescribeAlias(form)
		if !found || len(descriptor.CandidateUnitIDs) < 2 {
			t.Errorf("%q describes %v, found=%v", form, descriptor.CandidateUnitIDs, found)
		}
	}
}

func TestExplicitAliasesOutrankLocaleAndCasePoliciesRemainExact(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()
	if unit, resolution := catalog.ResolveExplicitAlias("imperial pint"); unit != "volume.pint.imperial" || resolution != units.AliasResolved {
		t.Fatalf("explicit imperial pint = %q, %v", unit, resolution)
	}

	if unit, resolution := catalog.ResolveAlias("T", "en-US"); unit != "volume.tablespoon.us.customary" || resolution != units.AliasResolved {
		t.Fatalf("T = %q, %v", unit, resolution)
	}

	if unit, resolution := catalog.ResolveAlias("t", "en-US"); unit != "volume.teaspoon.us.customary" || resolution != units.AliasResolved {
		t.Fatalf("t = %q, %v", unit, resolution)
	}

	if unit, resolution := catalog.ResolveAlias("ML", "en"); unit != "volume.milliliter" || resolution != units.AliasResolved {
		t.Fatalf("case-insensitive ml = %q, %v", unit, resolution)
	}
}

func TestAliasDescriptorsControlBulkResolution(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()

	descriptor, found := catalog.DescribeAlias("Tbsp")
	if !found || descriptor.NormalizedForm != "tbsp" || descriptor.CaseSensitive {
		t.Fatalf("descriptor = %#v", descriptor)
	}

	if got, resolution := catalog.ResolveAliasForConvention("Tbsp", units.ConventionAU); resolution != units.AliasResolved || got != "volume.tablespoon.au" {
		t.Fatalf("Australian resolution = %q, %v", got, resolution)
	}

	if catalog.ResolveAliasAs("Tbsp", "mass.gram") {
		t.Fatal("an unrelated unit was accepted")
	}

	exact, found := catalog.DescribeAlias("T")
	if !found || !exact.CaseSensitive || exact.NormalizedForm != "T" {
		t.Fatalf("exact descriptor = %#v", exact)
	}

	if lower, found := catalog.DescribeAlias("t"); !found || !lower.CaseSensitive || lower.NormalizedForm != "t" || lower.NormalizedForm == exact.NormalizedForm {
		t.Fatalf("lower descriptor = %#v", lower)
	}
}
