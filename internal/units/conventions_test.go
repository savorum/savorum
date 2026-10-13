package units_test

import (
	"testing"

	"github.com/savorum/savorum/internal/units"
)

// A convention names a measuring system, so it answers for the measure rather
// than for the spelling. The registry associates no metric identity with a
// bare `cup`, because no source evidence could establish one; a person calling
// the recipe metric supplies exactly that evidence.
func TestAConventionAnswersForTheMeasureRatherThanTheSpelling(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()
	cup, found := catalog.DescribeAlias("cup")

	if !found {
		t.Fatal("cup is not a registry alias")
	}

	for _, test := range []struct {
		convention units.Convention
		unit       string
	}{
		{units.ConventionUS, "volume.cup.us.customary"},
		{units.ConventionAU, "volume.cup.au"},
		{units.ConventionMetric, "volume.cup.metric.culinary"},
	} {
		profile, held := catalog.ConventionProfileFor(test.convention)
		if !held {
			t.Fatalf("no profile for %s", test.convention)
		}

		if selected, ok := profile.Select(cup.CandidateUnitIDs); !ok || selected != test.unit {
			t.Errorf("%s cup = %q, %v", test.convention, selected, ok)
		}
	}
}

// Each household convention answers the tablespoon and the teaspoon with its
// own identity, and the metric one is the 15 mL spoon rather than the
// Australian 20 mL spoon.
func TestEachHouseholdConventionAnswersTheSpoons(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()
	tablespoon, _ := catalog.DescribeAlias("tablespoon")
	teaspoon, _ := catalog.DescribeAlias("teaspoon")

	for _, test := range []struct {
		convention units.Convention
		tablespoon string
		teaspoon   string
	}{
		{units.ConventionUS, "volume.tablespoon.us.customary", "volume.teaspoon.us.customary"},
		{units.ConventionAU, "volume.tablespoon.au", "volume.teaspoon.au"},
		{units.ConventionMetric, "volume.tablespoon.metric.culinary", "volume.teaspoon.metric.culinary"},
	} {
		profile, held := catalog.ConventionProfileFor(test.convention)
		if !held {
			t.Fatalf("no profile for %s", test.convention)
		}

		if selected, ok := profile.Select(tablespoon.CandidateUnitIDs); !ok || selected != test.tablespoon {
			t.Errorf("%s tablespoon = %q, %v", test.convention, selected, ok)
		}

		if selected, ok := profile.Select(teaspoon.CandidateUnitIDs); !ok || selected != test.teaspoon {
			t.Errorf("%s teaspoon = %q, %v", test.convention, selected, ok)
		}
	}
}

// The British imperial convention names fluid volumes and no household spoon or
// cup, so a decision for it leaves a bare tablespoon unresolved.
func TestAConventionWithNoAnswerForAFamilyGivesNone(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()

	profile, held := catalog.ConventionProfileFor(units.ConventionImperial)
	if !held {
		t.Fatal("no imperial profile")
	}

	tablespoon, _ := catalog.DescribeAlias("tablespoon")
	if selected, ok := profile.Select(tablespoon.CandidateUnitIDs); ok {
		t.Fatalf("an imperial decision selected %q for a tablespoon", selected)
	}

	// A measure the convention does not answer for is left to the source
	// evidence it already had, not handed another convention's answer.
	gram, _ := catalog.DescribeAlias("grams")
	if selected, ok := profile.Select(gram.CandidateUnitIDs); ok {
		t.Fatalf("a volume convention answered for a mass: %q", selected)
	}
}

// Every published profile names identities this catalog holds, in the families
// it claims to answer for, against the registry version it declares.
func TestEveryConventionProfileSelectsRegisteredIdentities(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()

	profiles := catalog.ConventionProfiles()
	if len(profiles) == 0 {
		t.Fatal("no convention profiles")
	}

	for _, profile := range profiles {
		if profile.RegistryVersion != units.RegistryVersion {
			t.Errorf("%s names registry %q", profile.Convention, profile.RegistryVersion)
		}

		for _, family := range profile.Families() {
			unitID, _ := profile.UnitFor(family)

			convention, known := catalog.ConventionOf(unitID)
			if !known || convention != profile.Convention {
				t.Errorf("%s selects %s, which belongs to %q", profile.Convention, unitID, convention)
			}
		}
	}
}

// The precedence of SREF section 9.5, exercised on one occurrence at a time.
func TestOccurrenceResolutionFollowsTheSpecifiedPrecedence(t *testing.T) {
	t.Parallel()

	catalog := units.DefaultCatalog()
	us, _ := catalog.ConventionProfileFor(units.ConventionUS)

	// Explicit source meaning outranks every decision.
	explicit := units.Occurrence{Form: "teaspoons", ExplicitUnitID: "volume.teaspoon.metric.culinary"}

	unitID, resolution := catalog.ResolveOccurrence(explicit, "en-AU", units.Decisions{Convention: &us})
	if unitID != "volume.teaspoon.metric.culinary" || resolution != units.AliasResolved {
		t.Errorf("explicit meaning = %q, %v", unitID, resolution)
	}

	// An interpretation outranks the ingredient's convention, which is how a
	// reviewed exception is expressed.
	exception := units.Decisions{Interpretation: "volume.teaspoon.au", Convention: &us}
	if unitID, _ = catalog.ResolveOccurrence(units.Occurrence{Form: "teaspoons"}, "", exception); unitID != "volume.teaspoon.au" {
		t.Errorf("occurrence exception = %q", unitID)
	}

	// A convention outranks the locale, including an exact-region match.
	if unitID, _ = catalog.ResolveOccurrence(units.Occurrence{Form: "cup"}, "en-AU", units.Decisions{Convention: &us}); unitID != "volume.cup.us.customary" {
		t.Errorf("convention over locale = %q", unitID)
	}

	// With no decision, the locale decides and a bare teaspoon under `en`
	// decides nothing.
	if _, resolution = catalog.ResolveOccurrence(units.Occurrence{Form: "tsp"}, "en", units.Decisions{}); resolution != units.AliasAmbiguous {
		t.Errorf("a bare teaspoon under en resolved to %v", resolution)
	}
}
