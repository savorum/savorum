// Package v1 defines the stored recipe document.
package v1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Document is the stored representation of a recipe.
type Document struct {
	SchemaVersion int      `json:"schema_version"`
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Description   string   `json:"description,omitempty"`
	Language      string   `json:"language,omitempty"`
	SourceLocale  string   `json:"source_locale,omitempty"`
	Authors       []Author `json:"authors,omitempty"`
	Source        *Source  `json:"source,omitempty"`
	// LegacyYield is read from older documents and never written.
	LegacyYield          *Yield                `json:"yield,omitempty"`
	Yields               []Yield               `json:"yields,omitempty"`
	Nutrition            []NutritionStatement  `json:"nutrition,omitempty"`
	DietaryClaims        []DietaryClaim        `json:"dietary_claims,omitempty"`
	AllergenDeclarations []AllergenDeclaration `json:"allergen_declarations,omitempty"`
	Times                *Times                `json:"times,omitempty"`
	Categories           []string              `json:"categories,omitempty"`
	Courses              []string              `json:"courses,omitempty"`
	Keywords             []string              `json:"keywords,omitempty"`
	Cuisines             []string              `json:"cuisines,omitempty"`
	CookingMethods       []string              `json:"cooking_methods,omitempty"`
	Published            string                `json:"published,omitempty"`
	Modified             string                `json:"modified,omitempty"`
	Notes                []string              `json:"notes,omitempty"`
	Variants             []Variant             `json:"variants,omitempty"`
	Equipment            []Equipment           `json:"equipment,omitempty"`
	Translations         []Translation         `json:"translations,omitempty"`
	IngredientSections   []IngredientSection   `json:"ingredient_sections"`
	InstructionSections  []InstructionSection  `json:"instruction_sections"`
	Assets               []Asset               `json:"assets,omitempty"`
	ImageRefs            []string              `json:"image_refs,omitempty"`
	PrimaryImage         string                `json:"primary_image,omitempty"`
	Extensions           map[string]any        `json:"extensions_raw,omitempty"`
	SREFOpaque           map[string]any        `json:"sref_opaque_raw,omitempty"`
}

// Author is one credited recipe author.
type Author struct {
	Name      string `json:"name"`
	Kind      string `json:"kind,omitempty"`
	KindLabel string `json:"kind_label,omitempty"`
	URL       string `json:"url,omitempty"`
}

// Source is the recipe's source attribution.
type Source struct {
	Name        string `json:"name,omitempty"`
	URL         string `json:"url,omitempty"`
	Application string `json:"application,omitempty"`
	SourceID    string `json:"source_id,omitempty"`
	RetrievedAt string `json:"retrieved_at,omitempty"`
	Attribution string `json:"attribution,omitempty"`
}

// Yield is the recipe yield.
type Yield struct {
	Text     string    `json:"text,omitempty"`
	Quantity *Quantity `json:"quantity,omitempty"`
	Label    string    `json:"label,omitempty"`
}

// DurationExpression is an exact duration, a range, or authored text.
type DurationExpression struct {
	Value       string `json:"value,omitempty"`
	Min         string `json:"min,omitempty"`
	Max         string `json:"max,omitempty"`
	Approximate bool   `json:"approximate,omitempty"`
	Text        string `json:"text,omitempty"`
	SourceText  string `json:"source_text,omitempty"`
}

// Times is the recipe's timing summary and authored timings.
type Times struct {
	Prep       *DurationExpression `json:"prep,omitempty"`
	Cook       *DurationExpression `json:"cook,omitempty"`
	Additional *DurationExpression `json:"additional,omitempty"`
	Total      *DurationExpression `json:"total,omitempty"`
	Assertions []TimingAssertion   `json:"assertions,omitempty"`
}

// TimingAssertion is one authored recipe timing.
type TimingAssertion struct {
	ID         string               `json:"id"`
	Kind       string               `json:"kind,omitempty"`
	Label      string               `json:"label,omitempty"`
	Duration   DurationExpression   `json:"duration"`
	Attention  string               `json:"attention,omitempty"`
	Note       string               `json:"note,omitempty"`
	Microwave  *MicrowaveConditions `json:"microwave,omitempty"`
	Provenance string               `json:"provenance,omitempty"`
}

// MicrowaveConditions are the authored microwave settings for a timing.
type MicrowaveConditions struct {
	RatedOutputWatts int               `json:"rated_output_watts,omitempty"`
	Power            *MicrowavePower   `json:"power,omitempty"`
	ModeText         string            `json:"mode_text,omitempty"`
	SourceText       string            `json:"source_text"`
	Restrictions     []string          `json:"restrictions,omitempty"`
	Choices          []MicrowaveChoice `json:"choices,omitempty"`
}

// MicrowavePower is a wattage, a percentage, or authored text.
type MicrowavePower struct {
	Watts   int    `json:"watts,omitempty"`
	Percent string `json:"percent,omitempty"`
	Text    string `json:"text,omitempty"`
}

// MicrowaveChoice is one authored power and time option.
type MicrowaveChoice struct {
	Duration         DurationExpression `json:"duration"`
	RatedOutputWatts int                `json:"rated_output_watts,omitempty"`
	Power            *MicrowavePower    `json:"power,omitempty"`
	SourceText       string             `json:"source_text"`
}

// NutritionStatement is authored nutrition on one basis.
type NutritionStatement struct {
	Basis     NutritionBasis `json:"basis"`
	Nutrients []Nutrient     `json:"nutrients"`
	Note      string         `json:"note,omitempty"`
}

// NutritionBasis says what nutrition values describe.
type NutritionBasis struct {
	Kind     string    `json:"kind"`
	Text     string    `json:"text,omitempty"`
	Quantity *Quantity `json:"quantity,omitempty"`
}

// Nutrient is one authored nutrition value.
type Nutrient struct {
	Nutrient   string  `json:"nutrient"`
	Label      string  `json:"label,omitempty"`
	Amount     *Amount `json:"amount,omitempty"`
	Unit       string  `json:"unit,omitempty"`
	SourceText string  `json:"source_text,omitempty"`
	Text       string  `json:"text,omitempty"`
}

// DietaryClaim is an authored dietary suitability statement.
type DietaryClaim struct {
	Text        string `json:"text"`
	Suitability string `json:"suitability"`
	Diet        string `json:"diet,omitempty"`
	Condition   string `json:"condition,omitempty"`
	VariantRef  string `json:"variant_ref,omitempty"`
}

// AllergenDeclaration is an authored allergen statement.
type AllergenDeclaration struct {
	Text       string `json:"text"`
	Substance  string `json:"substance"`
	Presence   string `json:"presence"`
	Condition  string `json:"condition,omitempty"`
	VariantRef string `json:"variant_ref,omitempty"`
}

// RecipeReference identifies another recipe.
type RecipeReference struct {
	Title    string `json:"title,omitempty"`
	RecipeID string `json:"recipe_id,omitempty"`
	URL      string `json:"url,omitempty"`
}

// AssetLicense is the license applied to one asset.
type AssetLicense struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

// Variant is one authored recipe variation.
type Variant struct {
	ID    string `json:"id,omitempty"`
	Title string `json:"title,omitempty"`
	Text  string `json:"text"`
}

// Equipment is one authored equipment requirement.
type Equipment struct {
	Name     string    `json:"name"`
	Quantity *Quantity `json:"quantity,omitempty"`
	Note     string    `json:"note,omitempty"`
}

// Translation is one declared translation relationship.
type Translation struct {
	Relation string `json:"relation"`
	Language string `json:"language"`
	Title    string `json:"title,omitempty"`
	RecipeID string `json:"recipe_id,omitempty"`
	URL      string `json:"url,omitempty"`
}

// IngredientSection is an ingredient group.
type IngredientSection struct {
	ID          string       `json:"id"`
	Title       string       `json:"title,omitempty"`
	Ingredients []Ingredient `json:"ingredients"`
}

// Ingredient is one ingredient occurrence, or a choice between occurrences.
type Ingredient struct {
	Alternatives  []Ingredient     `json:"alternatives,omitempty"`
	Relation      string           `json:"relation,omitempty"`
	ID            string           `json:"id"`
	Name          string           `json:"name,omitempty"`
	Quantity      *Quantity        `json:"quantity,omitempty"`
	Optional      bool             `json:"optional,omitempty"`
	Note          string           `json:"note,omitempty"`
	SourceText    string           `json:"source_text,omitempty"`
	Normalization *Normalization   `json:"normalization,omitempty"`
	PackageSize   *SimpleQuantity  `json:"package_size,omitempty"`
	Temperature   *Temperature     `json:"temperature,omitempty"`
	Recipe        *RecipeReference `json:"recipe,omitempty"`
}

// Normalization records ingredient parsing metadata.
type Normalization struct {
	Method   string   `json:"method"`
	Review   string   `json:"review,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// Quantity is an amount, a sum, alternatives, or authored text.
type Quantity struct {
	Kind    string              `json:"kind"`
	Amount  *Amount             `json:"amount,omitempty"`
	Unit    string              `json:"unit,omitempty"`
	Terms   []SimpleQuantity    `json:"terms,omitempty"`
	Options []AlternativeOption `json:"options,omitempty"`
	Text    string              `json:"text,omitempty"`
}

// SimpleQuantity is one amount and unit.
type SimpleQuantity struct {
	Kind   string `json:"kind"`
	Amount Amount `json:"amount"`
	Unit   string `json:"unit,omitempty"`
}

// AlternativeOption is one alternative amount.
type AlternativeOption struct {
	Kind   string           `json:"kind"`
	Amount *Amount          `json:"amount,omitempty"`
	Unit   string           `json:"unit,omitempty"`
	Terms  []SimpleQuantity `json:"terms,omitempty"`
}

// Amount is an exact value or range.
type Amount struct {
	Value       *Rational `json:"value,omitempty"`
	Min         *Rational `json:"min,omitempty"`
	Max         *Rational `json:"max,omitempty"`
	Approximate bool      `json:"approximate,omitempty"`
}

// Rational is an exact rational number.
type Rational string

// Temperature is an authored temperature, either an amount and unit or text.
type Temperature struct {
	Amount     Amount              `json:"amount,omitzero"`
	Unit       string              `json:"unit,omitempty"`
	Text       string              `json:"text,omitempty"`
	Purpose    *TemperaturePurpose `json:"purpose,omitempty"`
	Note       string              `json:"note,omitempty"`
	SourceText string              `json:"source_text,omitempty"`
}

// MarshalJSON writes the amount only when the temperature is not authored text.
func (value Temperature) MarshalJSON() ([]byte, error) {
	type temperatureJSON struct {
		Amount     *Amount             `json:"amount,omitempty"`
		Unit       string              `json:"unit,omitempty"`
		Text       string              `json:"text,omitempty"`
		Purpose    *TemperaturePurpose `json:"purpose,omitempty"`
		Note       string              `json:"note,omitempty"`
		SourceText string              `json:"source_text,omitempty"`
	}

	encoded := temperatureJSON{Unit: value.Unit, Text: value.Text, Purpose: value.Purpose, Note: value.Note, SourceText: value.SourceText}
	if value.Text == "" {
		encoded.Amount = &value.Amount
	}

	data, err := json.Marshal(encoded)
	if err != nil {
		return nil, fmt.Errorf("encode temperature: %w", err)
	}

	return data, nil
}

// TemperaturePurpose describes why a temperature is specified.
type TemperaturePurpose struct {
	Kind  string `json:"kind"`
	Label string `json:"label,omitempty"`
}

// InstructionSection is an instruction group.
type InstructionSection struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	Steps []Step `json:"steps"`
}

// IngredientUse refers to an ingredient from a step.
type IngredientUse struct {
	IngredientRef string    `json:"ingredient_ref"`
	Quantity      *Quantity `json:"quantity,omitempty"`
}

// Step is one recipe instruction.
type Step struct {
	ID             string          `json:"id"`
	Title          string          `json:"title,omitempty"`
	Text           string          `json:"text,omitempty"`
	IngredientUses []IngredientUse `json:"ingredient_uses,omitempty"`
	ImageRefs      []string        `json:"image_refs,omitempty"`
	TimingRefs     []string        `json:"timing_refs,omitempty"`
	Temperatures   []Temperature   `json:"temperatures,omitempty"`
}

// Asset is a portable recipe asset.
type Asset struct {
	ID        string        `json:"id"`
	Path      string        `json:"path"`
	MediaType string        `json:"media_type"`
	SHA256    string        `json:"sha256"`
	Size      int64         `json:"size"`
	Role      string        `json:"role,omitempty"`
	Alt       string        `json:"alt,omitempty"`
	Caption   string        `json:"caption,omitempty"`
	Creator   string        `json:"creator,omitempty"`
	Credit    string        `json:"credit,omitempty"`
	Rights    string        `json:"rights,omitempty"`
	License   *AssetLicense `json:"license,omitempty"`
	SourceURL string        `json:"source_url,omitempty"`
}

// Decode reads one stored recipe document. Unknown members are rejected.
func Decode(data []byte) (Document, error) {
	var document Document

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("decode recipe document: %w", err)
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return Document{}, errors.New("decode recipe document: trailing JSON value")
		}

		return Document{}, fmt.Errorf("decode recipe document trailer: %w", err)
	}

	return document, nil
}
