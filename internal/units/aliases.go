// Package units is the measurement catalog: the units Savorum recognizes, the
// text people actually write for them, and the conversions between them. The
// server validates against this catalog and the frontend converts through it,
// so a quantity cannot mean one thing in the browser and another on disk.
package units

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

// RegistryVersion identifies the SREF unit vocabulary implemented by this
// catalog. Resolution plans bind themselves to it so a preview made against
// one vocabulary cannot be replayed after the aliases have changed.
const RegistryVersion = "0.2.0"

// AliasSet is one SREF parser vocabulary under one BCP 47 language or locale.
// Aliases are source evidence, not display labels; changing a viewer's locale
// never calls this resolver and therefore cannot reinterpret stored semantics.
type AliasSet struct {
	UnitID        string
	Language      string
	Forms         []string
	CaseSensitive bool
}

// AliasResolution distinguishes text that is not a registry alias from text
// that is an alias but cannot safely identify one unit under the supplied
// source locale. Importers use the distinction to keep ambiguous measures
// opaque instead of treating them as a count.
type AliasResolution uint8

// Alias resolution states distinguish unknown, unique, and ambiguous forms.
const (
	AliasUnknown AliasResolution = iota
	AliasResolved
	AliasAmbiguous
)

// aliasRef locates one registered form: its alias set, and its place in it.
type aliasRef struct{ set, form int }

// AliasDescriptor is the controlled ambiguity contract exposed to import
// review. CandidateUnitIDs contains only identities the registry associates
// with the authored form; callers must not widen it with arbitrary unit IDs.
type AliasDescriptor struct {
	AuthoredForm     string
	NormalizedForm   string
	CaseSensitive    bool
	CandidateUnitIDs []string
}

func registerAliasSets(c *Catalog, sets []AliasSet) error {
	for _, set := range sets {
		if _, found := c.units[set.UnitID]; !found {
			return fmt.Errorf("alias set names unknown unit %q", set.UnitID)
		}

		if _, err := language.Parse(set.Language); err != nil {
			return fmt.Errorf("unit %q has invalid alias language %q", set.UnitID, set.Language)
		}

		if len(set.Forms) == 0 {
			return fmt.Errorf("unit %q has an empty alias set", set.UnitID)
		}

		seen := make(map[string]bool, len(set.Forms))
		for _, form := range set.Forms {
			if form == "" {
				return fmt.Errorf("unit %q has an empty alias", set.UnitID)
			}

			key := matchKey(form, set.CaseSensitive)

			if seen[key] {
				return fmt.Errorf("unit %q repeats alias %q", set.UnitID, form)
			}

			seen[key] = true
		}

		copied := set
		copied.Forms = append([]string(nil), set.Forms...)
		c.aliasSets = append(c.aliasSets, copied)

		if c.aliasExact == nil {
			c.aliasExact, c.aliasFolded = map[string][]aliasRef{}, map[string][]aliasRef{}
		}

		index := c.aliasExact
		if !copied.CaseSensitive {
			index = c.aliasFolded
		}

		for position, form := range copied.Forms {
			key := matchKey(form, copied.CaseSensitive)
			index[key] = append(index[key], aliasRef{set: len(c.aliasSets) - 1, form: position})
		}
	}

	return nil
}

// AliasSets reports the registry vocabulary in declaration order. It is
// published for parity tests and returned defensively so callers cannot alter
// the catalog.
func (c Catalog) AliasSets() []AliasSet {
	result := make([]AliasSet, len(c.aliasSets))
	for index, set := range c.aliasSets {
		result[index] = set
		result[index].Forms = append([]string(nil), set.Forms...)
	}

	return result
}

// ResolveAlias applies SREF's RFC 4647-style source-locale lookup. The first
// locale level with any matching forms supplies the complete candidate set;
// sibling locales and an unscoped default are never searched.
func (c Catalog) ResolveAlias(form, sourceLocale string) (string, AliasResolution) {
	for _, locale := range localeLookupChain(sourceLocale) {
		candidates := c.aliasCandidates(form, func(set AliasSet, _ string) bool {
			return asciiEqualFold(set.Language, locale)
		})
		if len(candidates) != 0 {
			return resolvedCandidate(candidates)
		}
	}

	return "", AliasUnknown
}

// HasAliasesFor reports whether any alias set applies to sourceLocale under the
// lookup of ResolveAlias.
func (c Catalog) HasAliasesFor(sourceLocale string) bool {
	for _, locale := range localeLookupChain(sourceLocale) {
		for _, set := range c.aliasSets {
			if asciiEqualFold(set.Language, locale) {
				return true
			}
		}
	}

	return false
}

// ResolveExplicitAlias recognizes qualified forms whose wording itself names
// a region, standard, or metric magnitude. SREF permits this evidence before
// locale lookup, so "imperial pint" remains imperial even in an en-US source.
func (c Catalog) ResolveExplicitAlias(form string) (string, AliasResolution) {
	candidates := c.aliasCandidates(form, func(_ AliasSet, registered string) bool {
		return explicitAliasForm(registered)
	})
	if len(candidates) == 0 {
		return "", AliasUnknown
	}

	return resolvedCandidate(candidates)
}

// RecognizesAlias reports whether form occurs anywhere in the registry. It
// does not resolve the form. A parser uses it to distinguish ordinary prose
// from a unit alias that lacks enough locale evidence and must remain opaque.
func (c Catalog) RecognizesAlias(form string) bool {
	return len(c.aliasCandidates(form, func(AliasSet, string) bool { return true })) != 0
}

// DescribeAlias reports the registry-backed interpretations of an authored
// form. A form matched by a case-sensitive alias keeps its exact spelling in
// the normalized key; ordinary aliases use case folding.
func (c Catalog) DescribeAlias(form string) (AliasDescriptor, bool) {
	candidates := c.aliasCandidates(form, func(AliasSet, string) bool { return true })
	if len(candidates) == 0 {
		return AliasDescriptor{}, false
	}

	caseSensitive := len(c.aliasExact[matchKey(form, true)]) != 0
	normalized := matchKey(form, caseSensitive)

	return AliasDescriptor{
		AuthoredForm: form, NormalizedForm: normalized, CaseSensitive: caseSensitive,
		CandidateUnitIDs: candidates,
	}, true
}

// ResolveAliasAs accepts only a unit already named by DescribeAlias.
func (c Catalog) ResolveAliasAs(form, unitID string) bool {
	descriptor, found := c.DescribeAlias(form)
	if !found {
		return false
	}

	return slices.Contains(descriptor.CandidateUnitIDs, unitID)
}

// ResolveAliasForConvention returns the one controlled interpretation of form
// belonging to convention. More than one match remains ambiguous.
func (c Catalog) ResolveAliasForConvention(form string, convention Convention) (string, AliasResolution) {
	descriptor, found := c.DescribeAlias(form)
	if !found {
		return "", AliasUnknown
	}

	var matches []string

	for _, candidate := range descriptor.CandidateUnitIDs {
		if current, ok := c.ConventionOf(candidate); ok && current == convention {
			matches = append(matches, candidate)
		}
	}

	if len(matches) == 0 {
		return "", AliasUnknown
	}

	return resolvedCandidate(matches)
}

func (c Catalog) aliasCandidates(form string, include func(AliasSet, string) bool) []string {
	seen := map[string]bool{}

	for _, matches := range [][]aliasRef{c.aliasExact[matchKey(form, true)], c.aliasFolded[matchKey(form, false)]} {
		for _, ref := range matches {
			set := c.aliasSets[ref.set]
			if include(set, set.Forms[ref.form]) {
				seen[set.UnitID] = true
			}
		}
	}

	result := make([]string, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}

	sort.Strings(result)

	return result
}

func resolvedCandidate(candidates []string) (string, AliasResolution) {
	if len(candidates) == 1 {
		return candidates[0], AliasResolved
	}

	return "", AliasAmbiguous
}

// matchKey is the text alias comparison sees. Both sides are put in canonical
// composition, so a composed and a decomposed spelling of one form match; a
// case-insensitive set additionally compares under Unicode case folding.
// Compatibility mappings (fullwidth forms, ligatures, ℃), accent stripping, and
// script conversion are never applied, so a form that is not registered as
// written does not match.
func matchKey(value string, caseSensitive bool) string {
	value = norm.NFC.String(value)
	if caseSensitive {
		return value
	}

	return cases.Fold().String(value)
}

func asciiEqualFold(left, right string) bool {
	return asciiLower(left) == asciiLower(right)
}

func asciiLower(value string) string {
	bytes := []byte(value)
	for index, value := range bytes {
		if value >= 'A' && value <= 'Z' {
			bytes[index] += 'a' - 'A'
		}
	}

	return string(bytes)
}

func localeLookupChain(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	if _, err := language.Parse(value); err != nil {
		return nil
	}

	parts := strings.Split(value, "-")

	result := make([]string, 0, len(parts))
	for len(parts) > 0 {
		result = append(result, strings.Join(parts, "-"))

		parts = parts[:len(parts)-1]
		if len(parts) > 1 && len(parts[len(parts)-1]) == 1 {
			parts = parts[:len(parts)-1]
		}
	}

	return result
}

func explicitAliasForm(value string) bool {
	lower := asciiLower(value)
	for _, prefix := range []string{"us ", "american ", "australian ", "imperial ", "metric ", "legal "} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}

	fields := strings.Fields(lower)
	if len(fields) < 3 || fields[1] != "ml" {
		return false
	}

	for _, value := range fields[0] {
		if value < '0' || value > '9' {
			return false
		}
	}

	return fields[0] != ""
}

func aliases(unitID, locale string, forms ...string) AliasSet {
	return AliasSet{UnitID: unitID, Language: locale, Forms: forms}
}

func exactAliases(unitID, locale string, forms ...string) AliasSet {
	return AliasSet{UnitID: unitID, Language: locale, Forms: forms, CaseSensitive: true}
}
