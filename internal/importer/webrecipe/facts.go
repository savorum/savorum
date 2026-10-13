package webrecipe

import (
	"strings"
)

// Nutrition is one Schema.org NutritionInformation block as the page wrote it.
type Nutrition struct {
	ServingSize string
	Values      []NutrientValue
}

// NutrientValue is one property of a NutritionInformation block.
type NutrientValue struct {
	Property string
	Text     string
}

// ImageProvenance is what an ImageObject states about the file itself.
type ImageProvenance struct {
	Creator    string
	Credit     string
	Rights     string
	License    string
	LicenseURL string
}

var nutritionProperties = []string{
	"calories", "fatContent", "saturatedFatContent", "transFatContent", "unsaturatedFatContent",
	"cholesterolContent", "carbohydrateContent", "sugarContent", "fiberContent", "proteinContent",
	"sodiumContent",
}

func normalizeNutrition(value any) *Nutrition {
	node, ok := value.(map[string]any)
	if !ok {
		if list, isList := value.([]any); isList && len(list) > 0 {
			node, ok = list[0].(map[string]any)
		}

		if !ok {
			return nil
		}
	}

	result := Nutrition{ServingSize: collapseSpace(firstString(node["servingSize"]))}
	for _, property := range nutritionProperties {
		if text := collapseSpace(firstString(node[property])); text != "" {
			result.Values = append(result.Values, NutrientValue{Property: property, Text: text})
		}
	}

	if len(result.Values) == 0 {
		return nil
	}

	return &result
}

func normalizeDiets(value any) []string {
	var result []string

	seen := map[string]bool{}

	for _, item := range flatStrings(value) {
		for part := range strings.SplitSeq(item, ",") {
			part = strings.TrimSpace(part)

			name := part[strings.LastIndexAny(part, "/#:")+1:]
			if name != "" && !seen[name] {
				seen[name] = true
				result = append(result, name)
			}
		}
	}

	return result
}

func normalizeImageProvenance(value any) map[string]ImageProvenance {
	result := map[string]ImageProvenance{}

	var visit func(any)

	visit = func(value any) {
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				visit(item)
			}
		case map[string]any:
			address := firstString(typed["url"])
			if address == "" {
				address = firstString(typed["contentUrl"])
			}

			provenance := ImageProvenance{
				Creator:    collapseSpace(personName(typed["creator"])),
				Credit:     collapseSpace(firstString(typed["creditText"])),
				Rights:     collapseSpace(firstString(typed["copyrightNotice"])),
				LicenseURL: strings.TrimSpace(firstString(typed["license"])),
			}
			if !looksLikeAddress(provenance.LicenseURL) {
				provenance.License, provenance.LicenseURL = provenance.LicenseURL, ""
			}

			if address != "" && provenance != (ImageProvenance{}) {
				result[strings.TrimSpace(address)] = provenance
			}
		}
	}
	visit(value)

	return result
}

func personName(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		return firstString(typed["name"])
	case []any:
		if len(typed) > 0 {
			return personName(typed[0])
		}
	}

	return firstString(value)
}

// normalizeYields drops a member another member already states, as the "12"
// in ["12", "12 rolls"].
func normalizeYields(value any) []Yield {
	list, ok := value.([]any)
	if !ok {
		if single := normalizeYield(value); single.Text != "" {
			return []Yield{single}
		}

		return nil
	}

	var candidates []Yield

	for _, item := range list {
		if single := normalizeYield(item); single.Text != "" {
			candidates = append(candidates, single)
		}
	}

	var result []Yield

	for index, candidate := range candidates {
		redundant := false

		for other, kept := range candidates {
			if other == index {
				continue
			}

			longer := len(kept.Text) > len(candidate.Text) || (len(kept.Text) == len(candidate.Text) && other < index)
			if longer && (kept.Text == candidate.Text || strings.HasPrefix(kept.Text, candidate.Text+" ")) {
				redundant = true
				break
			}
		}

		if !redundant {
			result = append(result, candidate)
		}
	}

	return result
}
