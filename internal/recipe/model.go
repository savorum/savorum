package recipe

import (
	"encoding/json"
	"fmt"
)

// CurrentDocumentSchemaVersion is the stored recipe document version this build
// reads and writes.
const CurrentDocumentSchemaVersion = 1

// RecipeID identifies one logical portable recipe document across libraries,
// exports, imports, and synchronization. It is not a library entry ID.
type RecipeID string

// Document is the complete portable recipe content stored, imported, and exported by Savorum.
type Document struct {
	SchemaVersion  int                  `json:"schema_version"`
	ID             RecipeID             `json:"id"`
	Title          string               `json:"title"`
	Description    string               `json:"description,omitempty"`
	Language       string               `json:"language,omitempty"`
	SourceLocale   string               `json:"source_locale,omitempty"`
	Authors        []Author             `json:"authors,omitempty"`
	Source         *Source              `json:"source,omitempty"`
	Yields         []Yield              `json:"yields,omitempty"`
	Nutrition      []NutritionStatement `json:"nutrition,omitempty"`
	Times          *Times               `json:"times,omitempty"`
	Categories     []string             `json:"categories,omitempty"`
	Courses        []string             `json:"courses,omitempty"`
	Keywords       []string             `json:"keywords,omitempty"`
	Cuisines       []string             `json:"cuisines,omitempty"`
	CookingMethods []string             `json:"cooking_methods,omitempty"`
	Published      string               `json:"published,omitempty"`
	Modified       string               `json:"modified,omitempty"`
	Notes          []string             `json:"notes,omitempty"`
	// Variants are authored modifications of this recipe, in the order the
	// source gave them. They are preserved content, not a machine-applicable
	// delta: a reader must recognize one as a variant and show what it says,
	// and is never required to work out what applying it would mean.
	Variants             []Variant             `json:"variants,omitempty"`
	Equipment            []Equipment           `json:"equipment,omitempty"`
	DietaryClaims        []DietaryClaim        `json:"dietary_claims,omitempty"`
	AllergenDeclarations []AllergenDeclaration `json:"allergen_declarations,omitempty"`
	// Translations declare other renditions of this recipe in other
	// languages. They share no content with this document.
	Translations        []Translation        `json:"translations,omitempty"`
	IngredientSections  []IngredientSection  `json:"ingredient_sections"`
	InstructionSections []InstructionSection `json:"instruction_sections"`
	Assets              []Asset              `json:"assets,omitempty"`
	ImageRefs           []string             `json:"image_refs,omitempty"`
	PrimaryImage        string               `json:"primary_image,omitempty"`
	// Extensions preserves SREF extension members this build does not
	// interpret, keyed by the member name they arrived under: the standard
	// `extensions` object under that name, every other key an `x-` member.
	Extensions map[string]any `json:"extensions_raw,omitempty"`
	// SREFOpaque carries standardized members introduced by a compatible newer
	// SREF release. They are addressed by stable recipe-local identity and are
	// deliberately separate from vendor extensions because they belong to the
	// format even though this build cannot interpret them yet.
	SREFOpaque map[string]any `json:"sref_opaque_raw,omitempty"`
}

// Author is an ordered recipe-level credit distinct from application users,
// library owners, or asset credits. Name alone is required; Kind, KindLabel,
// and URL are preserved when provided by the source.
type Author struct {
	Name      string     `json:"name"`
	Kind      AuthorKind `json:"kind,omitempty"`
	KindLabel string     `json:"kind_label,omitempty"`
	URL       string     `json:"url,omitempty"`
}

// AuthorKind classifies a recipe credit without tying it to a Savorum account.
type AuthorKind string

// AuthorPerson and related constants enumerate supported AuthorKind values.
const (
	AuthorPerson       AuthorKind = "person"
	AuthorOrganization AuthorKind = "organization"
	AuthorOther        AuthorKind = "other"
)

// AuthorNames projects structured credits for concise reader, print, search,
// and comparison views without discarding the structured document itself.
func AuthorNames(authors []Author) []string {
	if authors == nil {
		return nil
	}

	names := make([]string, len(authors))
	for index, author := range authors {
		names[index] = author.Name
	}

	return names
}

// Source is portable recipe provenance. It travels through SREF independently
// of the Savorum library record, its publication relationships, and import
// history.
type Source struct {
	Name        string `json:"name,omitempty"`
	URL         string `json:"url,omitempty"`
	Application string `json:"application,omitempty"`
	SourceID    string `json:"source_id,omitempty"`
	RetrievedAt string `json:"retrieved_at,omitempty"`
	Attribution string `json:"attribution,omitempty"`
}

// Yield is one authored statement of what the recipe makes.
type Yield struct {
	Text     string    `json:"text,omitempty"`
	Quantity *Quantity `json:"quantity,omitempty"`
	Label    string    `json:"label,omitempty"`
}

// NutritionStatement is authored nutrition on one basis; Savorum never calculates it.
type NutritionStatement struct {
	Basis     NutritionBasis `json:"basis"`
	Nutrients []Nutrient     `json:"nutrients"`
	Note      string         `json:"note,omitempty"`
}

// NutritionBasis says what a statement's values describe.
type NutritionBasis struct {
	Kind     NutritionBasisKind `json:"kind"`
	Text     string             `json:"text,omitempty"`
	Quantity *Quantity          `json:"quantity,omitempty"`
}

// NutritionBasisKind enumerates SREF nutrition bases.
type NutritionBasisKind string

// NutritionBasisRecipe and related constants enumerate NutritionBasisKind values.
const (
	NutritionBasisRecipe      NutritionBasisKind = "recipe"
	NutritionBasisServing     NutritionBasisKind = "serving"
	NutritionBasisQuantity    NutritionBasisKind = "quantity"
	NutritionBasisUnspecified NutritionBasisKind = "unspecified"
)

// Nutrient is one authored value; an unmentioned nutrient is absent, never zero.
type Nutrient struct {
	Nutrient   NutrientKind `json:"nutrient"`
	Label      string       `json:"label,omitempty"`
	Amount     *Amount      `json:"amount,omitempty"`
	Unit       NutrientUnit `json:"unit,omitempty"`
	SourceText string       `json:"source_text,omitempty"`
	Text       string       `json:"text,omitempty"`
}

// NutrientKind enumerates SREF nutrients.
type NutrientKind string

// NutrientEnergy and related constants enumerate NutrientKind values.
const (
	NutrientEnergy         NutrientKind = "energy"
	NutrientFat            NutrientKind = "fat"
	NutrientSaturatedFat   NutrientKind = "saturated_fat"
	NutrientTransFat       NutrientKind = "trans_fat"
	NutrientUnsaturatedFat NutrientKind = "unsaturated_fat"
	NutrientCholesterol    NutrientKind = "cholesterol"
	NutrientCarbohydrate   NutrientKind = "carbohydrate"
	NutrientSugars         NutrientKind = "sugars"
	NutrientFiber          NutrientKind = "fiber"
	NutrientProtein        NutrientKind = "protein"
	NutrientSodium         NutrientKind = "sodium"
	NutrientSalt           NutrientKind = "salt"
	NutrientOther          NutrientKind = "other"
)

// NutrientKinds lists every nutrient in SREF order.
var NutrientKinds = []NutrientKind{
	NutrientEnergy, NutrientFat, NutrientSaturatedFat, NutrientTransFat, NutrientUnsaturatedFat,
	NutrientCholesterol, NutrientCarbohydrate, NutrientSugars, NutrientFiber, NutrientProtein,
	NutrientSodium, NutrientSalt, NutrientOther,
}

// NutrientUnit enumerates SREF nutrient units.
type NutrientUnit string

// NutrientUnitKilocalorie and related constants enumerate NutrientUnit values.
const (
	NutrientUnitKilocalorie NutrientUnit = "kcal"
	NutrientUnitKilojoule   NutrientUnit = "kJ"
	NutrientUnitGram        NutrientUnit = "g"
	NutrientUnitMilligram   NutrientUnit = "mg"
	NutrientUnitMicrogram   NutrientUnit = "mcg"
)

// DietaryClaim is an authored dietary suitability statement.
type DietaryClaim struct {
	Text        string      `json:"text"`
	Suitability Suitability `json:"suitability"`
	Diet        Diet        `json:"diet,omitempty"`
	Condition   string      `json:"condition,omitempty"`
	VariantRef  string      `json:"variant_ref,omitempty"`
}

// Suitability is whether a dietary claim affirms or denies suitability.
type Suitability string

// SuitabilitySuitable and related constants enumerate Suitability values.
const (
	SuitabilitySuitable   Suitability = "suitable"
	SuitabilityUnsuitable Suitability = "unsuitable"
)

// Diet is SREF's normalized dietary subject.
type Diet string

// DietDiabetic and related constants enumerate Diet values.
const (
	DietDiabetic   Diet = "diabetic"
	DietGlutenFree Diet = "gluten_free"
	DietHalal      Diet = "halal"
	DietHindu      Diet = "hindu"
	DietKosher     Diet = "kosher"
	DietLowCalorie Diet = "low_calorie"
	DietLowFat     Diet = "low_fat"
	DietLowLactose Diet = "low_lactose"
	DietLowSalt    Diet = "low_salt"
	DietVegan      Diet = "vegan"
	DietVegetarian Diet = "vegetarian"
)

// Diets lists every normalized diet.
var Diets = []Diet{
	DietDiabetic, DietGlutenFree, DietHalal, DietHindu, DietKosher, DietLowCalorie,
	DietLowFat, DietLowLactose, DietLowSalt, DietVegan, DietVegetarian,
}

// AllergenDeclaration is an authored statement about one substance.
type AllergenDeclaration struct {
	Text       string           `json:"text"`
	Substance  string           `json:"substance"`
	Presence   AllergenPresence `json:"presence"`
	Condition  string           `json:"condition,omitempty"`
	VariantRef string           `json:"variant_ref,omitempty"`
}

// AllergenPresence keeps presence, precaution, and absence distinct.
type AllergenPresence string

// AllergenContains and related constants enumerate AllergenPresence values.
const (
	AllergenContains   AllergenPresence = "contains"
	AllergenMayContain AllergenPresence = "may_contain"
	AllergenFreeFrom   AllergenPresence = "free_from"
)

// RecipeReference identifies another recipe without carrying its content.
type RecipeReference struct {
	Title    string   `json:"title,omitempty"`
	RecipeID RecipeID `json:"recipe_id,omitempty"`
	URL      string   `json:"url,omitempty"`
}

// DurationExpression is one authored length of time. It has exactly one shape:
// an exact Value, a Min/Max range, or opaque Text such as "overnight" that has
// no numeric reading at all. Approximate qualifies a value or a range and is
// meaningless on opaque text. SourceText keeps the authored wording of a value
// that was normalized.
//
// Summary times and timing assertions use the same expression, because a
// recipe is as entitled to say "additional: overnight" as it is to say it
// about one phase.
type DurationExpression struct {
	Value       string `json:"value,omitempty"`
	Min         string `json:"min,omitempty"`
	Max         string `json:"max,omitempty"`
	Approximate bool   `json:"approximate,omitempty"`
	Text        string `json:"text,omitempty"`
	SourceText  string `json:"source_text,omitempty"`
}

// IsOpaque reports whether the expression carries only unparsed wording.
func (d DurationExpression) IsOpaque() bool { return d.Text != "" }

// IsRange reports whether the expression is bounded rather than exact.
func (d DurationExpression) IsRange() bool { return d.Min != "" || d.Max != "" }

// IsZero reports whether the expression says nothing at all.
func (d DurationExpression) IsZero() bool {
	return d.Value == "" && d.Min == "" && d.Max == "" && d.Text == ""
}

// Times is the recipe's authored clock. The summary expressions preserve values
// a source stated, while Assertions preserves any named workflow detail. They
// remain independent because assertions may overlap or be partial, and a
// summary is never derived by adding them up.
type Times struct {
	Prep       *DurationExpression `json:"prep,omitempty"`
	Cook       *DurationExpression `json:"cook,omitempty"`
	Additional *DurationExpression `json:"additional,omitempty"`
	Total      *DurationExpression `json:"total,omitempty"`
	Assertions []TimingAssertion   `json:"assertions,omitempty"`
}

// TimingKind names what one timing assertion is for. The set is SREF's closed
// vocabulary, so an exported recipe needs no private extension to carry it. An
// absent kind is valid and means the source supported a duration but not a
// defensible classification; TimingOther is itself a classification and is not
// a substitute for saying nothing.
type TimingKind string

// TimingPrep and related constants enumerate supported TimingKind values.
const (
	TimingPrep     TimingKind = "prep"
	TimingSoak     TimingKind = "soak"
	TimingBrine    TimingKind = "brine"
	TimingMarinate TimingKind = "marinate"
	TimingCure     TimingKind = "cure"
	TimingProof    TimingKind = "proof"
	TimingFerment  TimingKind = "ferment"
	TimingChill    TimingKind = "chill"
	TimingFreeze   TimingKind = "freeze"
	TimingDrain    TimingKind = "drain"
	TimingCook     TimingKind = "cook"
	TimingRest     TimingKind = "rest"
	TimingCool     TimingKind = "cool"
	TimingOther    TimingKind = "other"
)

// Attention is authored recipe semantics: whether the cook is working during
// this time. It is not a display default and not a timer policy. An empty
// value means the recipe did not say, and Savorum MUST NOT write one merely
// because its own presentation treats a kind one way or the other.
type Attention string

// AttentionActive and related constants enumerate supported Attention values.
const (
	AttentionActive  Attention = "active"
	AttentionPassive Attention = "passive"
)

// TimingProvenance records how Savorum came to hold a timing, so a timing
// derived from wording the instruction already shows is not restated back at
// the cook. It is Savorum's own metadata and deliberately absent from SREF.
type TimingProvenance string

// TimingDerived and related constants enumerate supported TimingProvenance values.
const (
	TimingDerived      TimingProvenance = "derived"
	TimingSupplemental TimingProvenance = "supplemental"
)

// TimingAssertion is one authored timing event, stored exactly once however
// many places show it. ID is recipe-local and stable; steps point at it
// through Step.TimingRefs rather than the assertion listing its steps, so one
// event referenced from several steps stays one event.
type TimingAssertion struct {
	ID   string     `json:"id"`
	Kind TimingKind `json:"kind,omitempty"`
	// Label names a TimingOther assertion and may retitle a named one.
	Label      string               `json:"label,omitempty"`
	Duration   DurationExpression   `json:"duration"`
	Attention  Attention            `json:"attention,omitempty"`
	Note       string               `json:"note,omitempty"`
	Microwave  *MicrowaveConditions `json:"microwave,omitempty"`
	Provenance TimingProvenance     `json:"provenance,omitempty"`
}

// MicrowaveConditions preserves the cooking conditions the recipe associates
// with one timing event. It is authored recipe content, never the reader's
// appliance preference or a calculated duration.
type MicrowaveConditions struct {
	RatedOutputWatts int               `json:"rated_output_watts,omitempty"`
	Power            *MicrowavePower   `json:"power,omitempty"`
	ModeText         string            `json:"mode_text,omitempty"`
	SourceText       string            `json:"source_text"`
	Restrictions     []string          `json:"restrictions,omitempty"`
	Choices          []MicrowaveChoice `json:"choices,omitempty"`
}

// MicrowavePower keeps selected watts, a relative percentage, and opaque
// authored wording distinct. Exactly one member is present.
type MicrowavePower struct {
	Watts   int    `json:"watts,omitempty"`
	Percent string `json:"percent,omitempty"`
	Text    string `json:"text,omitempty"`
}

// MicrowaveChoice is one authored power/time alternative for the same event.
type MicrowaveChoice struct {
	Duration         DurationExpression `json:"duration"`
	RatedOutputWatts int                `json:"rated_output_watts,omitempty"`
	Power            *MicrowavePower    `json:"power,omitempty"`
	SourceText       string             `json:"source_text"`
}

// IngredientSection is an ordered group of ingredients under an optional heading.
type IngredientSection struct {
	ID          string       `json:"id"`
	Title       string       `json:"title,omitempty"`
	Ingredients []Ingredient `json:"ingredients"`
}

// Ingredient preserves an authored ingredient line alongside its structured interpretation.
type Ingredient struct {
	// Alternatives is a shallow authored choice; branch IDs share the ingredient namespace.
	Alternatives []Ingredient `json:"alternatives,omitempty"`
	Relation     string       `json:"relation,omitempty"`
	ID           string       `json:"id"`
	Name         string       `json:"name,omitempty"`
	Quantity     *Quantity    `json:"quantity,omitempty"`
	// Optional says the author sanctioned leaving this occurrence out. It
	// belongs to the occurrence, never propagated to another naming the same
	// thing, and does not cover "to taste", "for serving", or "preferably".
	Optional      bool             `json:"optional,omitempty"`
	Note          string           `json:"note,omitempty"`
	SourceText    string           `json:"source_text,omitempty"`
	Normalization *Normalization   `json:"normalization,omitempty"`
	PackageSize   *SimpleQuantity  `json:"package_size,omitempty"`
	Temperature   *Temperature     `json:"temperature,omitempty"`
	Recipe        *RecipeReference `json:"recipe,omitempty"`
}

// Variant is one authored alternate realization of the recipe, such as "for a
// richer version, add 100 ml cream with the stock".
//
// Text is what the author wrote and is required; Title is the source's own
// name for it when there was one. Savorum keeps both and interprets neither.
// A variant does not nest and never restructures the recipe: content a source
// presents as a modification of this recipe is a variant however many fields
// it would touch, and content presented as a separately structured recipe is a
// recipe whatever the prose calls it.
type Variant struct {
	ID    string `json:"id,omitempty"`
	Title string `json:"title,omitempty"`
	Text  string `json:"text"`
}

// Equipment is one authored recipe-level requirement. Name and Note preserve
// what the recipe says; the optional quantity uses the ordinary SREF quantity
// model but does not scale automatically with recipe yield.
type Equipment struct {
	Name     string    `json:"name"`
	Quantity *Quantity `json:"quantity,omitempty"`
	Note     string    `json:"note,omitempty"`
}

// Translation is one declared relationship between this recipe and another
// rendition of it. Title, RecipeID, and URL identify the counterpart; at least
// one is present. RecipeID names a portable recipe document, never a library
// entry.
type Translation struct {
	Relation TranslationRelation `json:"relation"`
	Language string              `json:"language"`
	Title    string              `json:"title,omitempty"`
	RecipeID RecipeID            `json:"recipe_id,omitempty"`
	URL      string              `json:"url,omitempty"`
}

// TranslationRelation says how a counterpart relates to the declaring recipe.
type TranslationRelation string

// TranslationOriginal and related constants enumerate TranslationRelation values.
const (
	// TranslationOriginal: this recipe was translated from the counterpart.
	TranslationOriginal TranslationRelation = "original"
	// TranslationOf: the counterpart was translated from this recipe.
	TranslationOf TranslationRelation = "translation"
	// TranslationAlternate: the same recipe in another language, with no
	// stated direction.
	TranslationAlternate TranslationRelation = "alternate"
)

// Normalization records how an ingredient interpretation was produced and reviewed.
type Normalization struct {
	Method   NormalizationMethod `json:"method"`
	Review   ReviewState         `json:"review,omitempty"`
	Warnings []string            `json:"warnings,omitempty"`
}

// NormalizationMethod identifies the source of a structured ingredient interpretation.
type NormalizationMethod string

// NormalizationAuthored and related constants enumerate supported NormalizationMethod values.
const (
	NormalizationAuthored   NormalizationMethod = "authored"
	NormalizationExtracted  NormalizationMethod = "extracted"
	NormalizationInferred   NormalizationMethod = "inferred"
	NormalizationUnresolved NormalizationMethod = "unresolved"
)

// ReviewState records whether a person has confirmed an inferred interpretation.
type ReviewState string

// ReviewConfirmed and related constants enumerate supported ReviewState values.
const (
	ReviewConfirmed ReviewState = "confirmed"
	ReviewCorrected ReviewState = "corrected"
)

// QuantityKind selects the single, compound, or alternative shape carried by Quantity.
type QuantityKind string

// QuantitySimple and related constants enumerate supported QuantityKind values.
const (
	QuantitySimple       QuantityKind = "simple"
	QuantitySum          QuantityKind = "sum"
	QuantityAlternatives QuantityKind = "alternatives"
	QuantityOpaque       QuantityKind = "opaque"
)

// Quantity represents one exact amount, a sum of amounts, or a set of alternatives.
type Quantity struct {
	Kind    QuantityKind        `json:"kind"`
	Amount  *Amount             `json:"amount,omitempty"`
	Unit    string              `json:"unit,omitempty"`
	Terms   []SimpleQuantity    `json:"terms,omitempty"`
	Options []AlternativeOption `json:"options,omitempty"`
	Text    string              `json:"text,omitempty"`
}

// SimpleQuantity pairs a rational amount with one unit identifier.
type SimpleQuantity struct {
	Kind   QuantityKind `json:"kind"`
	Amount Amount       `json:"amount"`
	Unit   string       `json:"unit,omitempty"`
}

// AlternativeOption is deliberately shallower than Quantity. An authored
// alternative may be one simple quantity or one sum of simple quantities, but
// cannot contain another alternative or an opaque expression.
type AlternativeOption struct {
	Kind   QuantityKind     `json:"kind"`
	Amount *Amount          `json:"amount,omitempty"`
	Unit   string           `json:"unit,omitempty"`
	Terms  []SimpleQuantity `json:"terms,omitempty"`
}

// Amount holds an exact rational value or inclusive rational bounds.
type Amount struct {
	Value       *Rational `json:"value,omitempty"`
	Min         *Rational `json:"min,omitempty"`
	Max         *Rational `json:"max,omitempty"`
	Approximate bool      `json:"approximate,omitempty"`
}

// Temperature preserves either a structured amount and unit or opaque authored text.
type Temperature struct {
	Amount Amount `json:"amount,omitzero"`
	Unit   string `json:"unit,omitempty"`
	// Text is an opaque authored temperature expression. It is mutually
	// exclusive with Amount and Unit, which prevents a derived numeric reading
	// from being presented as source-authored data.
	Text       string              `json:"text,omitempty"`
	Purpose    *TemperaturePurpose `json:"purpose,omitempty"`
	Note       string              `json:"note,omitempty"`
	SourceText string              `json:"source_text,omitempty"`
}

// MarshalJSON writes exactly one temperature value form. Amount is a value
// struct in the domain so numeric temperatures remain convenient to construct;
// a pointer in the interchange shape keeps an opaque temperature from gaining
// an empty numeric object on the wire.
func (value Temperature) MarshalJSON() ([]byte, error) {
	type temperatureJSON struct {
		Amount     *Amount             `json:"amount,omitempty"`
		Unit       string              `json:"unit,omitempty"`
		Text       string              `json:"text,omitempty"`
		Purpose    *TemperaturePurpose `json:"purpose,omitempty"`
		Note       string              `json:"note,omitempty"`
		SourceText string              `json:"source_text,omitempty"`
	}

	encoded := temperatureJSON{
		Unit:       value.Unit,
		Text:       value.Text,
		Purpose:    value.Purpose,
		Note:       value.Note,
		SourceText: value.SourceText,
	}
	if value.Text == "" {
		encoded.Amount = &value.Amount
	}

	marshaled, err := json.Marshal(encoded)
	if err != nil {
		return nil, fmt.Errorf("encode temperature: %w", err)
	}

	return marshaled, nil
}

// TemperaturePurpose identifies what should reach a temperature, with an optional custom label.
type TemperaturePurpose struct {
	Kind  TemperaturePurposeKind `json:"kind"`
	Label string                 `json:"label,omitempty"`
}

// TemperaturePurposeKind classifies the target of a cooking temperature.
type TemperaturePurposeKind string

// TemperaturePurposeOven and related constants enumerate supported TemperaturePurposeKind values.
const (
	TemperaturePurposeOven     TemperaturePurposeKind = "oven"
	TemperaturePurposeInternal TemperaturePurposeKind = "internal"
	TemperaturePurposeOil      TemperaturePurposeKind = "oil"
	TemperaturePurposeWater    TemperaturePurposeKind = "water"
	TemperaturePurposeSurface  TemperaturePurposeKind = "surface"
	TemperaturePurposeOther    TemperaturePurposeKind = "other"
)

// InstructionSection is an ordered group of recipe steps under an optional heading.
type InstructionSection struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	Steps []Step `json:"steps"`
}

// IngredientUse associates an ingredient with one instruction step. Quantity
// is an independent authored assertion; nil means only that the ingredient
// participates and does not imply the ingredient's entire declared amount.
type IngredientUse struct {
	IngredientRef string    `json:"ingredient_ref"`
	Quantity      *Quantity `json:"quantity,omitempty"`
}

// Step contains one authored instruction and its structured timing and temperature references.
type Step struct {
	ID             string          `json:"id"`
	Title          string          `json:"title,omitempty"`
	Text           string          `json:"text,omitempty"`
	IngredientUses []IngredientUse `json:"ingredient_uses,omitempty"`
	ImageRefs      []string        `json:"image_refs,omitempty"`
	// TimingRefs points at canonical timing assertions in Times.Assertions.
	// The step refers to the event; the event is not duplicated here.
	TimingRefs   []string      `json:"timing_refs,omitempty"`
	Temperatures []Temperature `json:"temperatures,omitempty"`
}

// Asset identifies media attached to a recipe by logical ID and content hash.
type Asset struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	MediaType string    `json:"media_type"`
	SHA256    string    `json:"sha256"`
	Size      int64     `json:"size"`
	Role      AssetRole `json:"role,omitempty"`
	Alt       string    `json:"alt,omitempty"`
	Caption   string    `json:"caption,omitempty"`
	// An absent License means the license is unknown.
	Creator   string        `json:"creator,omitempty"`
	Credit    string        `json:"credit,omitempty"`
	Rights    string        `json:"rights,omitempty"`
	License   *AssetLicense `json:"license,omitempty"`
	SourceURL string        `json:"source_url,omitempty"`
}

// AssetLicense is the license a source applies to one asset.
type AssetLicense struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

// HasProvenance reports whether the asset carries any provenance.
func (a Asset) HasProvenance() bool {
	return a.Creator != "" || a.Credit != "" || a.Rights != "" || a.License != nil || a.SourceURL != ""
}

// AssetRole identifies how attached media should be presented with a recipe.
type AssetRole string

// AssetRoleImage and related constants enumerate supported AssetRole values.
const (
	AssetRoleImage  AssetRole = "image"
	AssetRoleSource AssetRole = "source"
	AssetRoleOther  AssetRole = "other"
)
