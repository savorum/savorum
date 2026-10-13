package units

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// ConventionProfile is a measuring convention expanded to the exact unit
// identities it selects, family by family, against one registry version.
//
// SREF section 9.5 requires an ingredient-wide answer to expand to canonical
// identities before it resolves anything. RegistryVersion is carried so a
// decision made against one vocabulary cannot be replayed under another.
type ConventionProfile struct {
	Convention      Convention
	Label           string
	RegistryVersion string
	// units maps a unit family — the measure a convention answers for, such as
	// volume.cup — to the one identity this convention selects for it. A family
	// the map omits is a measure this convention does not answer.
	units map[string]string
}

// UnitFor reports the identity this convention selects for a unit family.
func (p ConventionProfile) UnitFor(family string) (string, bool) {
	unitID, found := p.units[family]

	return unitID, found
}

// Families lists the measures this convention answers for, in a stable order.
func (p ConventionProfile) Families() []string {
	families := make([]string, 0, len(p.units))
	for family := range p.units {
		families = append(families, family)
	}

	sort.Strings(families)

	return families
}

// Selections lists the identities this convention selects, in family order.
func (p ConventionProfile) Selections() []string {
	families := p.Families()

	selections := make([]string, 0, len(families))
	for _, family := range families {
		selections = append(selections, p.units[family])
	}

	return selections
}

// Select applies the convention to a measure, given the interpretations its
// authored form permits.
//
// It answers by family rather than by alias membership: a metric convention
// means the 250 mL cup, which no alias associates with the bare word. A family
// the convention does not cover stays unanswered, and a form whose
// interpretations span more than one family is left alone.
func (p ConventionProfile) Select(candidates []string) (string, bool) {
	var families []string

	for _, candidate := range candidates {
		if family := unitFamily(candidate); !slices.Contains(families, family) {
			families = append(families, family)
		}
	}

	if len(families) != 1 {
		return "", false
	}

	selected, found := p.units[families[0]]

	return selected, found
}

// unitFamily is the measure a unit is one interpretation of. It is the first
// two identifier segments: volume.cup.us.customary, volume.cup.au, and
// volume.cup.metric.culinary are three answers to one question, and mass.gram
// is its own.
func unitFamily(unitID string) string {
	parts := strings.Split(unitID, ".")
	if len(parts) < 2 {
		return unitID
	}

	return parts[0] + "." + parts[1]
}

// NewConventionProfile builds a profile from an explicit family-to-identity
// mapping, checked against this catalog.
//
// Conformance vectors supply their own mapping this way, and resolve through
// the same code path as a published convention.
func (c Catalog) NewConventionProfile(convention Convention, label string, mapping map[string]string) (ConventionProfile, error) {
	built := ConventionProfile{
		Convention: convention, Label: label, RegistryVersion: RegistryVersion,
		units: make(map[string]string, len(mapping)),
	}

	for family, unitID := range mapping {
		if _, known := c.units[unitID]; !known {
			return ConventionProfile{}, fmt.Errorf("convention selects unknown unit %q", unitID)
		}

		if unitFamily(unitID) != family {
			return ConventionProfile{}, fmt.Errorf("%q is not an interpretation of %q", unitID, family)
		}

		built.units[family] = unitID
	}

	if len(built.units) == 0 {
		return ConventionProfile{}, errors.New("a convention selects at least one identity")
	}

	return built, nil
}

// ConventionProfileFor returns the expanded profile for a convention.
func (c Catalog) ConventionProfileFor(convention Convention) (ConventionProfile, bool) {
	for _, profile := range c.profiles {
		if profile.Convention == convention {
			return profile, true
		}
	}

	return ConventionProfile{}, false
}

// ConventionProfiles reports every convention a person can choose, in
// declaration order.
func (c Catalog) ConventionProfiles() []ConventionProfile {
	return append([]ConventionProfile(nil), c.profiles...)
}

func registerConventionProfiles(c *Catalog, profiles []ConventionProfile) error {
	for _, profile := range profiles {
		if profile.RegistryVersion != RegistryVersion {
			return fmt.Errorf("convention %q names registry %q", profile.Convention, profile.RegistryVersion)
		}

		for family, unitID := range profile.units {
			if _, found := c.units[unitID]; !found {
				return fmt.Errorf("convention %q selects unknown unit %q", profile.Convention, unitID)
			}

			if unitFamily(unitID) != family {
				return fmt.Errorf("convention %q maps %q to %q, which is not one of its interpretations", profile.Convention, family, unitID)
			}

			if held, found := c.ConventionOf(unitID); !found || held != profile.Convention {
				return fmt.Errorf("convention %q selects %q, which belongs to %q", profile.Convention, unitID, held)
			}
		}

		c.profiles = append(c.profiles, profile)
	}

	return nil
}

func profile(convention Convention, label string, units map[string]string) ConventionProfile {
	return ConventionProfile{
		Convention: convention, Label: label, RegistryVersion: RegistryVersion, units: units,
	}
}

// defaultConventionProfiles is what each measuring convention actually selects.
//
// Metric answers the 15 mL tablespoon; the Australian 20 mL tablespoon is
// reached only through the Australian convention or an explicit qualifier.
func defaultConventionProfiles() []ConventionProfile {
	return []ConventionProfile{
		profile(ConventionUS, "US customary", map[string]string{
			"volume.cup":        "volume.cup.us.customary",
			"volume.tablespoon": "volume.tablespoon.us.customary",
			"volume.teaspoon":   "volume.teaspoon.us.customary",
			"volume.fluidounce": "volume.fluidounce.us",
			"volume.pint":       "volume.pint.us.liquid",
			"volume.quart":      "volume.quart.us.liquid",
			"volume.gallon":     "volume.gallon.us.liquid",
		}),
		profile(ConventionAU, "Australian", map[string]string{
			"volume.cup":        "volume.cup.au",
			"volume.tablespoon": "volume.tablespoon.au",
			"volume.teaspoon":   "volume.teaspoon.au",
		}),
		profile(ConventionImperial, "British imperial", map[string]string{
			"volume.fluidounce": "volume.fluidounce.imperial",
			"volume.pint":       "volume.pint.imperial",
			"volume.quart":      "volume.quart.imperial",
			"volume.gallon":     "volume.gallon.imperial",
		}),
		profile(ConventionMetric, "Metric culinary", map[string]string{
			"volume.cup":        "volume.cup.metric.culinary",
			"volume.tablespoon": "volume.tablespoon.metric.culinary",
			"volume.teaspoon":   "volume.teaspoon.metric.culinary",
		}),
	}
}

// Occurrence is one unit occurrence undergoing normalization: the form the
// source wrote, and the meaning the source itself gave it, where it gave one.
type Occurrence struct {
	// Form is the authored spelling.
	Form string
	// ExplicitUnitID is the identity the source states through a qualifier, a
	// stated equivalence, or a canonical ID. A decision never overrides it.
	ExplicitUnitID string
}

// Decisions are SREF section 9.5's resolution inputs as they apply to one
// occurrence: the interpretation supplied for this occurrence, and the
// convention chosen for the ingredient it belongs to.
type Decisions struct {
	Interpretation string
	Convention     *ConventionProfile
}

// ContestedFamily reports whether a measure family has more than one
// registered identity carrying a measuring convention — whether, in other
// words, the word is one somebody could have to choose between readings of.
//
// `volume.cup` is contested, because three identities answer to `cup` and only
// evidence separates them. `mass.gram` is not: it is metric, and nothing else
// is called a gram.
func (c Catalog) ContestedFamily(family string) bool {
	answers := 0

	for id, convention := range c.conventions {
		if convention == ConventionNone {
			continue
		}

		if unitFamily(id) == family {
			answers++
		}
	}

	return answers > 1
}

// contestedOccurrence reports the contested family an authored form belongs to,
// if it belongs to exactly one. A form whose readings span several families is
// not one measure and is left to ordinary lookup.
func (c Catalog) contestedOccurrence(descriptor AliasDescriptor) (string, bool) {
	var families []string

	for _, candidate := range descriptor.CandidateUnitIDs {
		if family := unitFamily(candidate); !slices.Contains(families, family) {
			families = append(families, family)
		}
	}

	if len(families) != 1 || !c.ContestedFamily(families[0]) {
		return "", false
	}

	return families[0], true
}

// ResolveOccurrence applies section 9.5's precedence to one occurrence:
// explicit source meaning, then the occurrence's own interpretation, then the
// ingredient's convention, then the source-locale lookup of section 9.2.
//
// Inside an explicit convention scope, a contested family the convention does
// not cover stays unresolved rather than falling back to the source locale,
// which would mix conventions within one ingredient. Uncontested units such as
// grams keep resolving from their own evidence.
func (c Catalog) ResolveOccurrence(occurrence Occurrence, sourceLocale string, decisions Decisions) (string, AliasResolution) {
	if occurrence.ExplicitUnitID != "" {
		if _, known := c.units[occurrence.ExplicitUnitID]; known {
			return occurrence.ExplicitUnitID, AliasResolved
		}

		return "", AliasUnknown
	}

	descriptor, recognized := c.DescribeAlias(occurrence.Form)
	if !recognized {
		return "", AliasUnknown
	}

	if decisions.Interpretation != "" && slices.Contains(descriptor.CandidateUnitIDs, decisions.Interpretation) {
		return decisions.Interpretation, AliasResolved
	}

	if decisions.Convention != nil {
		if unitID, found := decisions.Convention.Select(descriptor.CandidateUnitIDs); found {
			return unitID, AliasResolved
		}

		if _, contested := c.contestedOccurrence(descriptor); contested {
			return "", AliasAmbiguous
		}
	}

	if unitID, resolution := c.ResolveAlias(occurrence.Form, sourceLocale); resolution != AliasUnknown {
		return unitID, resolution
	}

	return "", AliasAmbiguous
}
