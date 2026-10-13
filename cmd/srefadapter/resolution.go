package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/savorum/savorum/internal/units"
)

// resolutionCorpus is SREF's unit-resolution corpus: identified occurrences,
// the source evidence available, and the decisions a person supplied.
//
// Unlike document test suites, comparing unit IDs verifies that the
// implementation made the specific resolution decision recorded in the corpus.
type resolutionCorpus struct {
	Profiles []struct {
		ID    string            `json:"id"`
		Units map[string]string `json:"units"`
	} `json:"profiles"`
	Cases []resolutionCase `json:"cases"`
}

type resolutionCase struct {
	ID           string `json:"id"`
	SourceLocale string `json:"source_locale"`
	Occurrences  []struct {
		ID           string `json:"id"`
		Ingredient   string `json:"ingredient"`
		Form         string `json:"form"`
		ExplicitUnit string `json:"explicit_unit"`
	} `json:"occurrences"`
	Decisions []struct {
		Kind       string `json:"kind"`
		Occurrence string `json:"occurrence"`
		Ingredient string `json:"ingredient"`
		Unit       string `json:"unit"`
		Profile    string `json:"profile"`
	} `json:"decisions"`
}

// resolutionAnswer has the shape the corpus states its expectations in.
type resolutionAnswer struct {
	Outcome  string             `json:"outcome"`
	Units    map[string]*string `json:"units,omitempty"`
	Conflict string             `json:"conflict,omitempty"`
}

// resolve answers every vector in the corpus through the catalog Savorum
// actually normalizes with.
//
// The corpus supplies each convention as a mapping to exact identities
// (section 9.5), which is built against this catalog rather than matched to
// one of Savorum's published conventions.
func resolve(path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // path is the conformance fixture the runner requested
	if err != nil {
		return fmt.Errorf("read resolution corpus: %w", err)
	}

	var corpus resolutionCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		return fmt.Errorf("parse resolution corpus: %w", err)
	}

	catalog := units.DefaultCatalog()

	profiles := map[string]units.ConventionProfile{}

	for _, published := range corpus.Profiles {
		profile, err := catalog.NewConventionProfile(units.Convention(published.ID), published.ID, published.Units)
		if err != nil {
			return emit(map[string]any{
				"outcome": "unsupported",
				"detail":  fmt.Sprintf("convention %q: %v", published.ID, err),
			})
		}

		profiles[published.ID] = profile
	}

	answers := map[string]resolutionAnswer{}

	for _, vector := range corpus.Cases {
		answer, supported := answerResolution(catalog, profiles, vector)
		if !supported {
			return emit(map[string]any{
				"outcome": "unsupported",
				"detail":  "a vector names a convention the corpus did not publish",
			})
		}

		answers[vector.ID] = answer
	}

	return emit(map[string]any{"outcome": "accepted", "resolutions": answers})
}

func answerResolution(
	catalog units.Catalog, profiles map[string]units.ConventionProfile, vector resolutionCase,
) (resolutionAnswer, bool) {
	interpretations := map[string]string{}
	conventions := map[string]string{}

	for _, decision := range vector.Decisions {
		switch decision.Kind {
		case "occurrence-interpretation":
			// Contradictory instructions for one occurrence are a conflict to
			// report, never a race the last answer wins.
			if held, taken := interpretations[decision.Occurrence]; taken && held != decision.Unit {
				return resolutionAnswer{Outcome: "conflict", Conflict: "occurrence"}, true
			}

			interpretations[decision.Occurrence] = decision.Unit
		default:
			if held, taken := conventions[decision.Ingredient]; taken && held != decision.Profile {
				return resolutionAnswer{Outcome: "conflict", Conflict: "convention"}, true
			}

			conventions[decision.Ingredient] = decision.Profile
		}
	}

	resolved := map[string]*string{}

	for _, occurrence := range vector.Occurrences {
		decisions := units.Decisions{Interpretation: interpretations[occurrence.ID]}

		if named, chosen := conventions[occurrence.Ingredient]; chosen {
			profile, held := profiles[named]
			if !held {
				return resolutionAnswer{}, false
			}

			decisions.Convention = &profile
		}

		unitID, resolution := catalog.ResolveOccurrence(
			units.Occurrence{Form: occurrence.Form, ExplicitUnitID: occurrence.ExplicitUnit},
			vector.SourceLocale, decisions,
		)
		if resolution != units.AliasResolved {
			resolved[occurrence.ID] = nil
			continue
		}

		identity := unitID
		resolved[occurrence.ID] = &identity
	}

	return resolutionAnswer{Outcome: "resolved", Units: resolved}, true
}
