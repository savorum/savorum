package units_test

import (
	"errors"
	"math/big"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func TestEveryCatalogUnitCanBeReadAloud(t *testing.T) {
	t.Parallel()

	for _, entry := range units.DefaultCatalog().Entries() {
		if _, found := units.LookupLabel(entry.Unit.ID); !found {
			t.Errorf("unit %q has no label, so a reader would see the registry identity", entry.Unit.ID)
		}
	}
}

func TestEveryLadderRungNamesARegisteredUnit(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()

	for system, byDimension := range units.Ladders() {
		for dimension, rungs := range byDimension {
			for _, rung := range rungs {
				unit, found := catalog.Lookup(rung.Unit)
				if !found {
					t.Fatalf("%s %s ladder names unregistered unit %q", system, dimension, rung.Unit)
				}

				if unit.Dimension != dimension {
					t.Errorf("%s ladder for %s rounds into %q, which measures %s", system, dimension, rung.Unit, unit.Dimension)
				}
			}
		}
	}
}

func TestPresentationRoundsToAMeasureAKitchenOwns(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()

	cases := []struct {
		name        string
		amount      string
		unit        string
		system      units.MeasurementSystem
		wantAmount  string
		wantUnit    string
		approximate bool
		wantParts   []string
	}{
		{
			name:   "as written leaves the recipe alone",
			amount: "1/3", unit: "volume.cup.us.customary", system: units.MeasureAsWritten,
			wantAmount: "1/3", wantUnit: "volume.cup.us.customary",
		},
		{
			name:   "a third of a US cup becomes a spoonable metric amount",
			amount: "1/3", unit: "volume.cup.us.customary", system: units.MeasureMetric,
			wantAmount: "80", wantUnit: "volume.milliliter", approximate: true,
		},
		{
			name:   "a recipe already in the reader's measures is not converted",
			amount: "200", unit: "mass.gram", system: units.MeasureMetric,
			wantAmount: "200", wantUnit: "mass.gram",
		},
		{
			name:   "an exact metric weight stays exact in US measures",
			amount: "4",
			unit:   "mass.ounce.avoirdupois", system: units.MeasureUS,
			wantAmount: "4", wantUnit: "mass.ounce.avoirdupois",
		},
		{
			name:   "an Australian cup is a first-class reading preference",
			amount: "250", unit: "volume.milliliter", system: units.MeasureAU,
			wantAmount: "1", wantUnit: "volume.cup.au",
		},
		{
			name:   "an oven temperature rounds to a dial a cook can set",
			amount: "180", unit: "temperature.celsius", system: units.MeasureUS,
			wantAmount: "355", wantUnit: "temperature.fahrenheit", approximate: true,
		},
		{
			name:   "a count has no physical definition and is never converted",
			amount: "3", unit: "count.egg", system: units.MeasureMetric,
			wantAmount: "3", wantUnit: "count.egg",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			shown, err := catalog.Present(recipe.Rational(testCase.amount), testCase.unit, testCase.system)
			if err != nil {
				t.Fatalf("present amount: %v", err)
			}

			want, _ := new(big.Rat).SetString(testCase.wantAmount)
			if shown.Amount.Cmp(want) != 0 {
				t.Errorf("amount = %s, want %s", shown.Amount.RatString(), testCase.wantAmount)
			}

			if shown.Unit != testCase.wantUnit {
				t.Errorf("unit = %q, want %q", shown.Unit, testCase.wantUnit)
			}

			if shown.Approximate != testCase.approximate {
				t.Errorf("approximate = %t, want %t", shown.Approximate, testCase.approximate)
			}
		})
	}
}

func TestTinyAmountsKeepTheirMeasureRatherThanRoundingAway(t *testing.T) {
	t.Parallel()
	// An eighth of a teaspoon has no whole quarter-teaspoon to round to. A
	// measure of zero would delete the ingredient's amount, so the unrounded
	// value survives instead.
	shown, err := units.DefaultCatalog().Present("1/64", "volume.teaspoon.metric.culinary", units.MeasureUS)
	if err != nil {
		t.Fatalf("present amount: %v", err)
	}

	if shown.Amount.Sign() == 0 {
		t.Fatal("a measurable amount was rounded away to nothing")
	}
}

func TestFriendlyVolumePresentation(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()

	cases := []struct {
		name        string
		amount      string
		wantAmount  string
		wantUnit    string
		approximate bool
		wantParts   []string
	}{
		{name: "fifteen sixteenths teaspoon becomes one teaspoon", amount: "15/16", wantAmount: "1", wantUnit: "volume.teaspoon.us.customary", approximate: true},
		{name: "five sixteenths teaspoon becomes one third teaspoon", amount: "5/16", wantAmount: "1/3", wantUnit: "volume.teaspoon.us.customary", approximate: true},
		{name: "five eighths teaspoon remains exact", amount: "5/8", wantAmount: "5/8", wantUnit: "volume.teaspoon.us.customary"},
		{name: "three teaspoons becomes one tablespoon", amount: "3", wantAmount: "1", wantUnit: "volume.tablespoon.us.customary"},
		{name: "four teaspoons becomes two device operations", amount: "4", wantAmount: "4", wantUnit: "volume.teaspoon.us.customary", wantParts: []string{"1 volume.tablespoon.us.customary", "1 volume.teaspoon.us.customary"}},
		{name: "nine and a half teaspoons becomes an exact two-device plan", amount: "19/2", wantAmount: "19/2", wantUnit: "volume.teaspoon.us.customary", wantParts: []string{"3 volume.tablespoon.us.customary", "1/2 volume.teaspoon.us.customary"}},
		{name: "four and three quarter teaspoons becomes one and a half tablespoons", amount: "19/4", wantAmount: "3/2", wantUnit: "volume.tablespoon.us.customary", approximate: true},
		{name: "two and three eighths teaspoons becomes two and a half teaspoons", amount: "19/8", wantAmount: "5/2", wantUnit: "volume.teaspoon.us.customary", approximate: true},
		{name: "a scaled one and five eighths teaspoons uses common spoons", amount: "13/8", wantAmount: "3/2", wantUnit: "volume.teaspoon.us.customary", approximate: true},
		{name: "twelve teaspoons becomes one quarter cup", amount: "12", wantAmount: "1/4", wantUnit: "volume.cup.us.customary"},
		// Eighteen teaspoons (3/8 cup) resolves to 1/4 cup plus 2 tablespoons
		// because standard measuring cups do not include 1/8 cup.
		{
			name: "eighteen teaspoons is counted out rather than read as three eighths of a cup", amount: "18",
			wantAmount: "18", wantUnit: "volume.teaspoon.us.customary",
			wantParts: []string{"1/4 volume.cup.us.customary", "2 volume.tablespoon.us.customary"},
		},
		{
			name: "thirty teaspoons uses the graduated cup a kitchen has", amount: "30",
			wantAmount: "30", wantUnit: "volume.teaspoon.us.customary",
			wantParts: []string{"1/2 volume.cup.us.customary", "2 volume.tablespoon.us.customary"},
		},
		{
			name: "twenty seven teaspoons is half a cup and a tablespoon", amount: "27",
			wantAmount: "27", wantUnit: "volume.teaspoon.us.customary",
			wantParts: []string{"1/2 volume.cup.us.customary", "1 volume.tablespoon.us.customary"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			shown, err := catalog.PresentNormalized(recipe.Rational(testCase.amount), "volume.teaspoon.us.customary", units.MeasureAsWritten)
			if err != nil {
				t.Fatalf("present amount: %v", err)
			}

			if got := shown.Amount.RatString(); got != testCase.wantAmount {
				t.Errorf("amount = %s, want %s", got, testCase.wantAmount)
			}

			if shown.Unit != testCase.wantUnit {
				t.Errorf("unit = %q, want %q", shown.Unit, testCase.wantUnit)
			}

			if shown.Approximate != testCase.approximate {
				t.Errorf("approximate = %t, want %t", shown.Approximate, testCase.approximate)
			}

			var parts []string
			for _, part := range shown.Parts {
				parts = append(parts, part.Amount.RatString()+" "+part.Unit)
			}

			if !sameStringList(parts, testCase.wantParts) {
				t.Errorf("parts = %q, want %q", parts, testCase.wantParts)
			}
		})
	}

	shown, err := catalog.PresentNormalized("19/8", "volume.tablespoon.us.customary", units.MeasureAsWritten)
	if err != nil {
		t.Fatalf("present mixed tablespoon amount: %v", err)
	}

	if got := shown.Amount.RatString(); got != "57/8" || shown.Unit != "volume.teaspoon.us.customary" || !shown.Approximate || len(shown.Parts) != 2 || shown.Parts[0].Amount.RatString() != "2" || shown.Parts[1].Amount.RatString() != "1" {
		t.Errorf("19/8 tbsp = %#v, want 2 tbsp + 1 tsp (approximate)", shown)
	}

	authored, err := catalog.Present("13/8", "volume.teaspoon.us.customary", units.MeasureAsWritten)
	if err != nil || authored.Amount.RatString() != "13/8" || len(authored.Parts) != 0 {
		t.Errorf("authored 13/8 tsp was not preserved: %#v, %v", authored, err)
	}

	australian, err := catalog.PresentNormalized("9", "volume.teaspoon.au", units.MeasureAsWritten)
	if err != nil || len(australian.Parts) != 2 || australian.Parts[0].Amount.RatString() != "2" || australian.Parts[0].Unit != "volume.tablespoon.au" || australian.Parts[1].Amount.RatString() != "1" {
		t.Errorf("Australian plan = %#v, %v; want 2 tbsp + 1 tsp", australian, err)
	}
}

func TestUSPreferenceKeepsAuthoredUSCupInsteadOfExpandingItIntoSpoons(t *testing.T) {
	shown, err := units.DefaultCatalog().Present("3", "volume.cup.us.culinary", units.MeasureUS)
	if err != nil {
		t.Fatal(err)
	}

	if shown.Amount.RatString() != "3" || shown.Unit != "volume.cup.us.culinary" || len(shown.Parts) != 0 || shown.Approximate {
		t.Fatalf("3 US cups = %#v, want the authored 3 cups", shown)
	}
}

func TestUSLegalMeasuresStayTheirOwnIdentityWhenNormalizedAsWritten(t *testing.T) {
	t.Parallel()

	for _, unit := range []string{"volume.cup.us.legal", "volume.tablespoon.us.legal", "volume.teaspoon.us.legal"} {
		shown, err := units.DefaultCatalog().PresentNormalized("2", unit, units.MeasureAsWritten)
		if err != nil {
			t.Fatal(err)
		}

		if shown.Amount.RatString() != "2" || shown.Unit != unit || len(shown.Parts) != 0 || shown.Approximate {
			t.Errorf("2 %s = %#v, want the exact authored identity", unit, shown)
		}
	}
}

func TestMetricKeepsSpoonScaleVolumesInSpoons(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()

	cases := []struct {
		amount string
		want   string
	}{
		{amount: "5/4", want: "5/4"},
		{amount: "15/16", want: "1"},
		{amount: "5/8", want: "5/8"},
		{amount: "5/16", want: "1/3"},
	}
	for _, testCase := range cases {
		shown, err := catalog.PresentNormalized(recipe.Rational(testCase.amount), "volume.teaspoon.us.customary", units.MeasureMetric)
		if err != nil {
			t.Fatalf("present %s: %v", testCase.amount, err)
		}

		if got := shown.Amount.RatString(); got != testCase.want || shown.Unit != "volume.teaspoon.metric.culinary" {
			t.Errorf("%s tsp = %s %s, want %s metric tsp", testCase.amount, got, shown.Unit, testCase.want)
		}
	}
}

func TestAustralianPresentationDoesNotTradeAccuracyForRegionalFractions(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()

	cases := []struct {
		amount string
		want   string
	}{
		{amount: "5/4", want: "5/4"},
		{amount: "15/16", want: "1"},
		{amount: "5/8", want: "5/8"},
		{amount: "5/16", want: "1/3"},
	}
	for _, testCase := range cases {
		shown, err := catalog.PresentNormalized(recipe.Rational(testCase.amount), "volume.teaspoon.us.customary", units.MeasureAU)
		if err != nil {
			t.Fatalf("present %s: %v", testCase.amount, err)
		}

		if got := shown.Amount.RatString(); got != testCase.want || shown.Unit != "volume.teaspoon.au" {
			t.Errorf("%s US tsp = %s %s, want %s AU tsp", testCase.amount, got, shown.Unit, testCase.want)
		}
	}
}

func TestMeasurementPlansPreferAccuracyWhenOperationsMatch(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()

	exact, err := catalog.PresentNormalized("25/4", "volume.teaspoon.us.customary", units.MeasureUS)
	if err != nil || exact.Approximate || len(exact.Parts) != 2 || exact.Parts[0].Amount.RatString() != "2" || exact.Parts[1].Amount.RatString() != "1/4" {
		t.Errorf("6¼ tsp = %#v, %v; want exact 2 tbsp + ¼ tsp", exact, err)
	}

	simpler, err := catalog.PresentNormalized("25/8", "volume.teaspoon.us.customary", units.MeasureUS)
	if err != nil || simpler.Unit != "volume.tablespoon.us.customary" || simpler.Amount.RatString() != "1" || !simpler.Approximate {
		t.Errorf("3⅛ tsp = %#v, %v; want approximate 1 tbsp", simpler, err)
	}

	australian, err := catalog.PresentNormalized("3", "volume.tablespoon.us.customary", units.MeasureAU)
	if err != nil || len(australian.Parts) != 2 || australian.Parts[0].Amount.RatString() != "2" || australian.Parts[0].Unit != "volume.tablespoon.au" || australian.Parts[1].Amount.RatString() != "1" || australian.Parts[1].Unit != "volume.teaspoon.au" {
		t.Errorf("3 US tbsp = %#v, %v; want 2 AU tbsp + 1 AU tsp", australian, err)
	}
}

func sameStringList(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}

	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}

	return true
}

func TestConventionIsReadFromTheRegistryRatherThanTheLabel(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()
	if got := catalog.MeasurementConventionOf([]string{"volume.cup.au", "volume.tablespoon.au"}); got != units.ConventionAU {
		t.Errorf("convention = %q, want %q", got, units.ConventionAU)
	}

	if got := catalog.MeasurementConventionOf([]string{"volume.cup.au", "volume.cup.us.customary"}); got != units.ConventionNone {
		t.Errorf("mixed conventions reported %q, want none", got)
	}
}

func TestNormalizationRefusesAnUnknownUnit(t *testing.T) {
	t.Parallel()

	shown, err := units.DefaultCatalog().PresentNormalized("2", "future.length.international", units.MeasureMetric)
	if !errors.Is(err, units.ErrUnknownUnit) {
		t.Fatalf("normalization error = %v", err)
	}

	if shown.Unit != "future.length.international" || shown.Amount.Cmp(big.NewRat(2, 1)) != 0 {
		t.Fatalf("unknown unit fallback = %#v", shown)
	}
}
