package main

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/savorum/savorum/internal/codegen"
	"github.com/savorum/savorum/internal/units"
)

// The native client vendors this shared unit catalog.
const nativeCatalogPath = "internal/units/catalog.generated.json"

type nativeUnit struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Dimension  string `json:"dimension,omitempty"`
	Convention string `json:"convention,omitempty"`
	Base       string `json:"base,omitempty"`
	Factor     string `json:"factor,omitempty"`
	Offset     string `json:"offset,omitempty"`
	Exact      bool   `json:"exact"`
	One        string `json:"one"`
	Many       string `json:"many"`
	Tight      bool   `json:"tight,omitempty"`
}

type nativeRung struct {
	Unit    string `json:"unit"`
	Ceiling string `json:"ceiling,omitempty"`
	Step    string `json:"step"`
}

type nativeVolumePolicy struct {
	Fractions             [][]string `json:"fractions"`
	CompoundFractions     [][]string `json:"compound_fractions"`
	RemainderSteps        []string   `json:"remainder_steps"`
	MaxRemainder          string     `json:"max_remainder"`
	MaxRelativeError      string     `json:"max_relative_error"`
	MaxTeaspoonError      string     `json:"max_teaspoon_error"`
	RelativeErrorWeight   string     `json:"relative_error_weight"`
	MinimumSimplicityGain string     `json:"minimum_simplicity_gain"`
}

type nativeChoices struct {
	Volume []string `json:"volume"`
	Mass   []string `json:"mass"`
	Counts []string `json:"counts"`
}

type nativeCatalog struct {
	Units              []nativeUnit                       `json:"units"`
	Ladders            map[string]map[string][]nativeRung `json:"ladders"`
	VolumePresentation nativeVolumePolicy                 `json:"volume_presentation"`
	Choices            map[string]nativeChoices           `json:"choices"`
}

func writeNativeCatalog(entries []units.Entry, check bool) error {
	catalog := nativeCatalog{Ladders: map[string]map[string][]nativeRung{}}

	for _, entry := range entries {
		label, _ := units.LookupLabel(entry.Unit.ID)
		catalog.Units = append(catalog.Units, nativeUnit{
			ID: entry.Unit.ID, Kind: string(entry.Unit.Kind), Dimension: string(entry.Unit.Dimension),
			Convention: string(entry.Convention), Base: entry.BaseUnit, Factor: entry.Multiplier,
			Offset: entry.Offset, Exact: entry.Exact, One: label.One, Many: label.Many, Tight: label.Tight,
		})
	}

	for system, dimensions := range units.Ladders() {
		catalog.Ladders[string(system)] = map[string][]nativeRung{}

		for dimension, rungs := range dimensions {
			for _, rung := range rungs {
				catalog.Ladders[string(system)][string(dimension)] = append(
					catalog.Ladders[string(system)][string(dimension)],
					nativeRung{Unit: rung.Unit, Ceiling: rung.Ceiling, Step: rung.Step},
				)
			}
		}
	}

	catalog.Choices = map[string]nativeChoices{}

	for _, system := range []units.MeasurementSystem{units.MeasureMetric, units.MeasureUS, units.MeasureAU} {
		choices, _ := units.DefaultCatalog().ChoicesFor(system)
		catalog.Choices[string(system)] = nativeChoices{Volume: choices.Volume, Mass: choices.Mass, Counts: choices.Counts}
	}

	policy := units.VolumePresentation()
	catalog.VolumePresentation = nativeVolumePolicy{
		Fractions: policy.Fractions, CompoundFractions: policy.CompoundFractions,
		RemainderSteps: policy.RemainderSteps, MaxRemainder: policy.MaxRemainder,
		MaxRelativeError: policy.MaxRelativeError, MaxTeaspoonError: policy.MaxTeaspoonError,
		RelativeErrorWeight: policy.RelativeErrorWeight, MinimumSimplicityGain: policy.MinimumSimplicityGain,
	}

	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return fmt.Errorf("encode native unit catalog: %w", err)
	}

	data = append(data, '\n')
	if check {
		return codegen.CheckArtifact(nativeCatalogPath, data, command)
	}

	return codegen.WriteArtifact(nativeCatalogPath, data)
}

// Native unit labels use String Catalog plural substitutions.
const labelCatalogPath = "internal/units/UnitLabels.xcstrings"

type catalogString struct {
	State string `json:"state"`
	Value string `json:"value"`
}

type catalogVariant struct {
	StringUnit catalogString `json:"stringUnit"`
}

type catalogSubstitution struct {
	ArgNum          int                                  `json:"argNum"`
	FormatSpecifier string                               `json:"formatSpecifier"`
	Variations      map[string]map[string]catalogVariant `json:"variations"`
}

type catalogLocalization struct {
	StringUnit    catalogString                  `json:"stringUnit"`
	Substitutions map[string]catalogSubstitution `json:"substitutions"`
}

type catalogEntry struct {
	ExtractionState string                         `json:"extractionState"`
	Localizations   map[string]catalogLocalization `json:"localizations"`
}

func writeLabelCatalog(entries []units.Entry, check bool) error {
	locales := units.LocalizedLabelCatalogs()
	labels := map[string]catalogEntry{}

	for _, entry := range entries {
		id := entry.Unit.ID
		label, _ := units.LookupLabel(id)
		english := map[string]string{"one": label.One, "other": label.Many}
		localizations := map[string]catalogLocalization{"en": pluralLocalization(english)}

		for locale, catalog := range locales {
			forms := english
			if localized, found := catalog[id]; found {
				forms = localized.Forms
			}

			localizations[locale] = pluralLocalization(categoryForms(forms, locale))
		}

		labels[id+" %lf"] = catalogEntry{ExtractionState: "manual", Localizations: localizations}
	}

	data, err := json.MarshalIndent(map[string]any{"sourceLanguage": "en", "strings": labels, "version": "1.0"}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode unit label catalog: %w", err)
	}

	data = append(data, '\n')
	if check {
		return codegen.CheckArtifact(labelCatalogPath, data, command)
	}

	return codegen.WriteArtifact(labelCatalogPath, data)
}

func categoryForms(forms map[string]string, locale string) map[string]string {
	result := map[string]string{"other": forms["other"]}

	for numerator := int64(0); numerator <= 400; numerator++ {
		category := units.PluralCategory(big.NewRat(numerator, 2), locale)
		if form, found := forms[category]; found {
			result[category] = form
		}
	}

	return result
}

func pluralLocalization(forms map[string]string) catalogLocalization {
	plural := map[string]catalogVariant{}
	for category, value := range forms {
		plural[category] = catalogVariant{StringUnit: catalogString{State: "translated", Value: value}}
	}

	return catalogLocalization{
		StringUnit: catalogString{State: "translated", Value: "%#@label@"},
		Substitutions: map[string]catalogSubstitution{"label": {
			ArgNum: 1, FormatSpecifier: "lf", Variations: map[string]map[string]catalogVariant{"plural": plural},
		}},
	}
}
