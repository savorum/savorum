package exchange

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

// measurementConventionProfiles is the menu of conventions a person can choose,
// read from the catalog so it always matches the resolver.
func measurementConventionProfiles() []MeasurementConventionProfile {
	catalog := units.DefaultCatalog()

	// Every measure family any offered convention answers for. A family one
	// convention covers and another does not is precisely what has to be shown.
	answered := map[string]bool{}

	for _, profile := range catalog.ConventionProfiles() {
		for _, family := range profile.Families() {
			answered[family] = true
		}
	}

	offered := catalog.ConventionProfiles()
	result := make([]MeasurementConventionProfile, 0, len(offered))

	for _, profile := range offered {
		entry := MeasurementConventionProfile{
			ID: string(profile.Convention), Label: profile.Label,
			Selects: profile.Selections(), UnitRegistryVersion: profile.RegistryVersion,
		}

		held := map[string]bool{}
		for _, family := range profile.Families() {
			held[family] = true
		}

		for family := range answered {
			if !held[family] {
				entry.Unanswered = append(entry.Unanswered, family)
			}
		}

		sort.Strings(entry.Unanswered)
		result = append(result, entry)
	}

	return result
}

func resolutionGroups(previewID string, session *previewSession) []MeasurementResolutionGroup {
	occurrences := resolutionOccurrences(previewID, session)

	byGroup := map[string][]MeasurementResolutionOccurrence{}
	for _, occurrence := range occurrences {
		byGroup[occurrence.GroupID] = append(byGroup[occurrence.GroupID], occurrence)
	}

	ids := make([]string, 0, len(byGroup))
	for id := range byGroup {
		ids = append(ids, id)
	}

	sort.Strings(ids)

	catalog := units.DefaultCatalog()

	groups := make([]MeasurementResolutionGroup, 0, len(ids))
	for _, id := range ids {
		found := byGroup[id]
		first := found[0]
		group := MeasurementResolutionGroup{
			ID: id, Kind: ingredientparser.AmbiguityUnitAlias, AuthoredForm: first.AuthoredForm,
			Occurrences: len(found), SourceLabel: resolutionSourceLabel(session, first.ItemID),
			CandidateUnitIDs: append([]string(nil), first.CandidateUnitIDs...),
		}
		recipes := map[string]bool{}
		ingredients := map[string]bool{}
		subgroups := map[string][]MeasurementResolutionOccurrence{}

		for _, occurrence := range found {
			recipes[occurrence.ItemID] = true
			ingredients[pendingKey(occurrence.ItemID, occurrence.IngredientID)] = true

			subgroups[occurrence.SourceText] = append(subgroups[occurrence.SourceText], occurrence)
			if len(group.Examples) < 3 {
				group.Examples = append(group.Examples, MeasurementResolutionExample{
					RecipeTitle: occurrence.RecipeTitle, SourceText: occurrence.SourceText, Before: "kept as written",
				})
			}
		}

		group.Recipes = len(recipes)
		group.Ingredients = len(ingredients)

		texts := make([]string, 0, len(subgroups))
		for text := range subgroups {
			texts = append(texts, text)
		}

		sort.Strings(texts)

		for _, text := range texts {
			recipeIDs := map[string]bool{}
			for _, occurrence := range subgroups[text] {
				recipeIDs[occurrence.ItemID] = true
			}

			group.Subgroups = append(group.Subgroups, MeasurementResolutionSubgroup{
				SourceText: text, Occurrences: len(subgroups[text]), Recipes: len(recipeIDs),
			})
		}

		candidateIDs := append([]string(nil), first.CandidateUnitIDs...)
		sort.SliceStable(candidateIDs, func(i, j int) bool {
			left, _ := catalog.ConventionOf(candidateIDs[i])
			right, _ := catalog.ConventionOf(candidateIDs[j])

			return left == units.ConventionUS && right != units.ConventionUS
		})

		for _, unitID := range candidateIDs {
			convention, _ := catalog.ConventionOf(unitID)
			group.Choices = append(group.Choices, MeasurementResolutionChoice{
				UnitID: unitID, Label: resolutionUnitLabel(catalog, unitID), Convention: string(convention),
				ConventionLabel: resolutionConventionLabel(catalog, unitID),
				Magnitude:       resolutionUnitMagnitude(catalog, unitID),
			})
		}

		groups = append(groups, group)
	}

	return groups
}

// resolutionSourceLabel names the upload or address an item arrived from.
func resolutionSourceLabel(session *previewSession, itemID string) string {
	candidate := session.candidates[itemID]
	if candidate == nil {
		return ""
	}

	switch source := candidate.view.Source; {
	case source.Filename != "":
		return source.Filename
	case source.URL != "":
		return source.URL
	default:
		return source.FormatName
	}
}

// resolutionUnitMagnitude states how much an identity holds, read out of the
// registry.
func resolutionUnitMagnitude(catalog units.Catalog, unitID string) string {
	unit, found := catalog.Lookup(unitID)
	if !found {
		return ""
	}

	var base, target string

	switch unit.Dimension {
	case recipe.DimensionVolume:
		base, target = "volume.milliliter", "ml"
	case recipe.DimensionMass:
		base, target = "mass.gram", "g"
	case recipe.DimensionLength, recipe.DimensionTemperature:
		// Nothing a competing identity disagrees about by size.
		return ""
	default:
		return ""
	}

	conversion, err := catalog.Convert("1", unitID, base)
	if err != nil {
		return ""
	}

	// One decimal place separates every candidate the registry offers.
	value, _ := conversion.Rat().Float64()
	rounded := strconv.FormatFloat(value, 'f', 1, 64)

	return strings.TrimSuffix(rounded, ".0") + " " + target
}

// resolutionDecisions collects the alias spellings of one source that ask the
// same question into the single question they are.
//
// Spellings share a decision when their candidate sets are equal, within one
// source.
func resolutionDecisions(previewID string, session *previewSession) []MeasurementResolutionDecision {
	groups := resolutionGroups(previewID, session)

	byGroup := make(map[string]MeasurementResolutionGroup, len(groups))
	for _, group := range groups {
		byGroup[group.ID] = group
	}

	var (
		order  []string
		byKey  = map[string]*MeasurementResolutionDecision{}
		counts = map[string]map[string]int{}
		// Distinct, since one recipe may write both `cup` and `cups`.
		recipes     = map[string]map[string]bool{}
		ingredients = map[string]map[string]bool{}
		keyOfGroup  = map[string]string{}
	)

	for _, group := range groups {
		candidates := append([]string(nil), group.CandidateUnitIDs...)
		sort.Strings(candidates)
		key := strings.Join(append([]string{group.SourceLabel}, candidates...), "\x00")
		keyOfGroup[group.ID] = key

		decision, found := byKey[key]
		if !found {
			decision = &MeasurementResolutionDecision{
				ID: resolutionHash("decision", previewID, key), SourceLabel: group.SourceLabel,
				CandidateUnitIDs: append([]string(nil), group.CandidateUnitIDs...),
				Choices:          append([]MeasurementResolutionChoice(nil), group.Choices...),
			}
			byKey[key] = decision
			counts[key] = map[string]int{}
			recipes[key] = map[string]bool{}
			ingredients[key] = map[string]bool{}
			order = append(order, key)
		}

		decision.GroupIDs = append(decision.GroupIDs, group.ID)
		decision.Occurrences += group.Occurrences
		// Folded: a group holds whichever capitalisation it met first. Case
		// cannot distinguish measures within one decision, because `T` and `t`
		// offer different candidates and so land in different decisions.
		counts[key][strings.ToLower(group.AuthoredForm)] += group.Occurrences
	}

	for _, occurrence := range resolutionOccurrences(previewID, session) {
		key, found := keyOfGroup[occurrence.GroupID]
		if !found {
			continue
		}

		recipes[key][occurrence.ItemID] = true
		ingredients[key][pendingKey(occurrence.ItemID, occurrence.IngredientID)] = true
	}

	result := make([]MeasurementResolutionDecision, 0, len(order))

	for _, key := range order {
		decision := byKey[key]
		decision.Recipes = len(recipes[key])
		decision.Ingredients = len(ingredients[key])
		decision.AuthoredForms = authoredFormsByFrequency(counts[key])
		// The fullest spelling the sources used, rather than registry wording.
		decision.MeasureLabel = longestAuthoredForm(decision.AuthoredForms)
		decision.Examples = decisionExamples(decision, byGroup)
		result = append(result, *decision)
	}

	// Most-asked first: the decision that settles the most measurements is the
	// one worth answering first.
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].Occurrences == result[right].Occurrences {
			return result[left].MeasureLabel < result[right].MeasureLabel
		}

		return result[left].Occurrences > result[right].Occurrences
	})

	return result
}

// authoredFormsByFrequency orders the spellings a source used, commonest
// first. The wording a person recognises is their own, not the registry's.
func authoredFormsByFrequency(counts map[string]int) []string {
	forms := make([]string, 0, len(counts))
	for form := range counts {
		forms = append(forms, form)
	}

	sort.SliceStable(forms, func(left, right int) bool {
		if counts[forms[left]] == counts[forms[right]] {
			return forms[left] < forms[right]
		}

		return counts[forms[left]] > counts[forms[right]]
	})

	return forms
}

func longestAuthoredForm(forms []string) string {
	longest := ""
	for _, form := range forms {
		if len(form) > len(longest) {
			longest = form
		}
	}

	return longest
}

// decisionExamples takes authored lines from each spelling in turn, so a
// decision covering `tsp` and `teaspoons` shows one of each rather than four
// of whichever sorted first.
func decisionExamples(decision *MeasurementResolutionDecision, byGroup map[string]MeasurementResolutionGroup) []MeasurementResolutionExample {
	var result []MeasurementResolutionExample

	for round := range 3 {
		for _, groupID := range decision.GroupIDs {
			examples := byGroup[groupID].Examples
			if round >= len(examples) || len(result) >= 4 {
				continue
			}

			result = append(result, examples[round])
		}
	}

	return result
}

func resolutionOccurrences(previewID string, session *previewSession) []MeasurementResolutionOccurrence {
	var result []MeasurementResolutionOccurrence

	for _, itemID := range session.order {
		candidate := session.candidates[itemID]
		if candidate == nil {
			continue
		}

		cohort := strings.Join([]string{candidate.view.Source.FormatID, candidate.view.Source.Filename}, "|")
		for _, diagnostic := range candidate.view.Diagnostics {
			if diagnostic.Class != importer.DiagnosticActionable {
				continue
			}

			for _, target := range diagnostic.Targets {
				resolution := target.Resolution
				if resolution == nil || resolution.Kind != ingredientparser.AmbiguityUnitAlias {
					continue
				}

				// A measure a retained answer already settles is not a
				// question, even while its ingredient is unresolved.
				if settledByPending(session, itemID, target, resolution) {
					continue
				}

				groupID := resolutionHash("group", previewID, cohort, resolution.ResolutionKey)
				occurrenceID := resolutionHash("occurrence", previewID, itemID, resolution.ID)
				result = append(result, MeasurementResolutionOccurrence{
					ID: occurrenceID, GroupID: groupID, ItemID: itemID, RecipeID: string(candidate.view.Recipe.ID),
					RecipeTitle: candidate.view.Recipe.Title, IngredientID: target.ObjectID, SourceText: target.Evidence,
					AuthoredForm: resolution.AuthoredForm, CandidateUnitIDs: append([]string(nil), resolution.CandidateUnitIDs...),
					ResolutionKey: resolution.ResolutionKey, OccurrenceKey: resolution.OccurrenceKey,
					DeclaredLocale: candidate.view.Recipe.SourceLocale,
					Fingerprint:    ambiguityFingerprint(itemID, target),
				})
			}
		}
	}

	sort.Slice(result, func(left, right int) bool {
		if result[left].GroupID == result[right].GroupID {
			return result[left].ID < result[right].ID
		}

		return result[left].GroupID < result[right].GroupID
	})

	return result
}

// settledByPending reports whether an answer already given about this
// ingredient settles this measure.
func settledByPending(
	session *previewSession, itemID string,
	target importer.DiagnosticTarget, resolution *importer.ResolutionDescriptor,
) bool {
	pending := session.pendingDecisions[pendingKey(itemID, target.ObjectID)]
	if !pending.current(target.Evidence) {
		return false
	}

	catalog := units.DefaultCatalog()

	descriptor, found := catalog.DescribeAlias(resolution.AuthoredForm)
	if !found {
		return false
	}

	// The locale is withheld: this asks only what the retained answers settle.
	_, settled := catalog.ResolveOccurrence(
		units.Occurrence{Form: descriptor.AuthoredForm}, "",
		units.Decisions{
			Interpretation: pending.interpretations[resolution.OccurrenceKey],
			Convention:     pending.convention,
		},
	)

	return settled == units.AliasResolved
}

func ambiguityFingerprint(itemID string, target importer.DiagnosticTarget) string {
	resolution := target.Resolution
	if resolution == nil {
		return ""
	}

	return resolutionHash("fingerprint", itemID, target.ObjectID, target.Evidence, resolution.ResolutionKey,
		strings.Join(resolution.CandidateUnitIDs, ","))
}

func resolutionHash(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:12])
}

// resolutionUnitLabel names one candidate identity the way a person reads it:
// the measuring convention it belongs to, and the measure it is.
//
// Both halves come out of the registry, so the labels cannot drift from it.
func resolutionUnitLabel(catalog units.Catalog, unitID string) string {
	name := unitID

	if label, found := units.LookupLabel(unitID); found && label.One != "" {
		name = label.One
	}

	convention, known := catalog.ConventionOf(unitID)
	if !known {
		return name
	}

	profile, held := catalog.ConventionProfileFor(convention)
	if !held || profile.Label == "" {
		return name
	}

	return profile.Label + " " + name
}

// resolutionConventionLabel names only the convention, for a choice presented
// beside a measure the card has already named.
func resolutionConventionLabel(catalog units.Catalog, unitID string) string {
	convention, known := catalog.ConventionOf(unitID)
	if !known {
		return ""
	}

	profile, held := catalog.ConventionProfileFor(convention)
	if !held {
		return ""
	}

	return profile.Label
}

func actionableOccurrenceCount(session *previewSession) int {
	total := 0

	for _, candidate := range session.candidates {
		for _, diagnostic := range candidate.view.Diagnostics {
			if diagnostic.Class != importer.DiagnosticActionable {
				continue
			}

			switch {
			case len(diagnostic.Targets) > 0:
				total += len(diagnostic.Targets)
			case diagnostic.Count > 0:
				total += diagnostic.Count
			default:
				total++
			}
		}
	}

	return total
}
