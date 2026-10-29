package v1

import (
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
)

// RecipeDocument is the API representation of a recipe document.
type RecipeDocument struct {
	SchemaVersion        int                   `json:"schema_version" minimum:"1" maximum:"1"`
	ID                   string                `json:"id" pattern:"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$" doc:"Portable recipe document identifier"`
	Title                string                `json:"title" minLength:"1" maxLength:"1000"`
	Description          string                `json:"description,omitempty" maxLength:"1000000"`
	Language             string                `json:"language,omitempty" maxLength:"255"`
	SourceLocale         string                `json:"source_locale,omitempty" maxLength:"255"`
	Authors              []Author              `json:"authors,omitempty" maxItems:"100"`
	Source               *Source               `json:"source,omitempty"`
	Yields               []Yield               `json:"yields,omitempty" maxItems:"100" doc:"Independent authored yield assertions in source order. None is primary; which one a reader scales by is their own state"`
	Nutrition            []NutritionStatement  `json:"nutrition,omitempty" maxItems:"100" doc:"Authored nutrition statements, each with its own basis. Savorum does not calculate nutrition"`
	DietaryClaims        []DietaryClaim        `json:"dietary_claims,omitempty" maxItems:"1000" doc:"Dietary suitability the source stated; never derived from ingredients"`
	AllergenDeclarations []AllergenDeclaration `json:"allergen_declarations,omitempty" maxItems:"1000" doc:"Allergen presence, precaution, and absence the source stated; an unmentioned substance is unknown"`
	Times                *Times                `json:"times,omitempty"`
	Categories           []string              `json:"categories,omitempty" maxItems:"1000" doc:"Ordered source-authored broad classifications"`
	Courses              []string              `json:"courses,omitempty" maxItems:"1000" doc:"Ordered source-authored meal-role classifications"`
	Keywords             []string              `json:"keywords,omitempty" maxItems:"1000"`
	Cuisines             []string              `json:"cuisines,omitempty" maxItems:"1000"`
	CookingMethods       []string              `json:"cooking_methods,omitempty" maxItems:"1000" doc:"Ordered source-authored cooking-method labels"`
	Published            string                `json:"published,omitempty" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}(?:[Tt][0-9]{2}:[0-9]{2}:[0-9]{2}(?:\\.[0-9]+)?(?:[Zz]|[+-][0-9]{2}:[0-9]{2}))?$" doc:"Authored recipe or source-work publication date or date-time"`
	Modified             string                `json:"modified,omitempty" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}(?:[Tt][0-9]{2}:[0-9]{2}:[0-9]{2}(?:\\.[0-9]+)?(?:[Zz]|[+-][0-9]{2}:[0-9]{2}))?$" doc:"Authored recipe or source-work modification date or date-time"`
	Notes                []string              `json:"notes,omitempty" maxItems:"1000"`
	Variants             []Variant             `json:"variants,omitempty" maxItems:"1000" doc:"Authored alternate realizations of this recipe, in source order. Preserved content, not a machine-applicable delta: identify one as a variant and show what it says rather than trying to apply it"`
	Equipment            []Equipment           `json:"equipment,omitempty" maxItems:"1000" doc:"Ordered equipment the recipe identifies as required or expected; recipe content, not kitchen inventory"`
	Translations         []Translation         `json:"translations,omitempty" maxItems:"1000" doc:"Declared relationships to other renditions of this recipe in other languages. Each rendition is its own recipe; a relationship shares no content"`
	IngredientSections   []IngredientSection   `json:"ingredient_sections" maxItems:"1000"`
	InstructionSections  []InstructionSection  `json:"instruction_sections" maxItems:"1000"`
	Assets               []Asset               `json:"assets,omitempty" maxItems:"10000"`
	ImageRefs            []string              `json:"image_refs,omitempty" maxItems:"10000"`
	PrimaryImage         string                `json:"primary_image,omitempty" pattern:"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$"`
	// Extensions holds SREF extension members Savorum does not interpret. They
	// are round-tripped so editing a recipe never discards what another
	// application put in it.
	Extensions map[string]any `json:"extensions_raw,omitempty"`
	// SREFOpaque holds future standardized SREF members. Clients must preserve
	// this opaque sidecar and must not interpret or edit its contents.
	SREFOpaque map[string]any `json:"sref_opaque_raw,omitempty" readOnly:"true"`
}

// Author identifies an author of a recipe.
type Author struct {
	Name      string `json:"name" minLength:"1" maxLength:"1000"`
	Kind      string `json:"kind,omitempty" enum:"person,organization,other" doc:"Absent when the source does not support a defensible classification"`
	KindLabel string `json:"kind_label,omitempty" maxLength:"1000" doc:"Source-authored classification used only with kind other"`
	URL       string `json:"url,omitempty" format:"uri" pattern:"^[A-Za-z][A-Za-z0-9+.-]*:" maxLength:"10000"`
}

// Source identifies where the recipe originated.
type Source struct {
	Name        string `json:"name,omitempty" maxLength:"1000" doc:"Portable source name credited by the recipe"`
	URL         string `json:"url,omitempty" format:"uri" maxLength:"10000" doc:"Portable canonical source URL"`
	Application string `json:"application,omitempty" maxLength:"1000" doc:"Portable source application or format identity; namespaces source_id"`
	SourceID    string `json:"source_id,omitempty" maxLength:"1000" doc:"Identifier within application; never a Savorum library or publication ID"`
	RetrievedAt string `json:"retrieved_at,omitempty" format:"date-time" doc:"Supplementary retrieval time; does not identify a source"`
	Attribution string `json:"attribution,omitempty" maxLength:"1000000" doc:"Portable source credit or attribution"`
}

// Yield describes the amount of food produced by the recipe.
type Yield struct {
	Text     string    `json:"text,omitempty" maxLength:"1000"`
	Quantity *Quantity `json:"quantity,omitempty"`
	Label    string    `json:"label,omitempty" maxLength:"1000"`
}

// DurationExpression carries an exact value, a range, or opaque authored text.
type DurationExpression struct {
	Value       string `json:"value,omitempty" pattern:"^P(?:[0-9]+Y)?(?:[0-9]+M)?(?:[0-9]+W)?(?:[0-9]+D)?(?:T(?:[0-9]+H)?(?:[0-9]+M)?(?:[0-9]+S)?)?$" maxLength:"100"`
	Min         string `json:"min,omitempty" pattern:"^P(?:[0-9]+Y)?(?:[0-9]+M)?(?:[0-9]+W)?(?:[0-9]+D)?(?:T(?:[0-9]+H)?(?:[0-9]+M)?(?:[0-9]+S)?)?$" maxLength:"100"`
	Max         string `json:"max,omitempty" pattern:"^P(?:[0-9]+Y)?(?:[0-9]+M)?(?:[0-9]+W)?(?:[0-9]+D)?(?:T(?:[0-9]+H)?(?:[0-9]+M)?(?:[0-9]+S)?)?$" maxLength:"100"`
	Approximate bool   `json:"approximate,omitempty"`
	Text        string `json:"text,omitempty" maxLength:"1000" doc:"Opaque wording such as overnight; excludes a value or a range"`
	SourceText  string `json:"source_text,omitempty" maxLength:"1000"`
}

// Times carries authored summary values and any authored phase breakdown.
type Times struct {
	Prep       *DurationExpression `json:"prep,omitempty"`
	Cook       *DurationExpression `json:"cook,omitempty"`
	Additional *DurationExpression `json:"additional,omitempty"`
	Total      *DurationExpression `json:"total,omitempty"`
	Assertions []TimingAssertion   `json:"assertions,omitempty" maxItems:"50"`
}

// TimingAssertion describes an authored recipe timing.
type TimingAssertion struct {
	ID       string             `json:"id" maxLength:"255"`
	Kind     string             `json:"kind,omitempty" enum:"prep,soak,brine,marinate,cure,proof,ferment,chill,freeze,drain,cook,rest,cool,other" doc:"Absent when the source supports a duration but no defensible classification"`
	Label    string             `json:"label,omitempty" maxLength:"100" doc:"Required when kind is other; may retitle a named one"`
	Duration DurationExpression `json:"duration"`
	// Attention is what the recipe says about whether the cook is working. It
	// is not a display default and does not mean a timer should be offered.
	Attention  string               `json:"attention,omitempty" enum:"active,passive" doc:"Authored semantics; absent means the recipe does not say"`
	Note       string               `json:"note,omitempty" maxLength:"1000"`
	Microwave  *MicrowaveConditions `json:"microwave,omitempty"`
	Provenance string               `json:"provenance,omitempty" enum:"derived,supplemental" doc:"How Savorum came to hold this timing; not part of the portable recipe"`
}

// MicrowaveConditions carries authored microwave operating conditions for one timing.
type MicrowaveConditions struct {
	RatedOutputWatts int               `json:"rated_output_watts,omitempty" minimum:"1" maximum:"1000000"`
	Power            *MicrowavePower   `json:"power,omitempty"`
	ModeText         string            `json:"mode_text,omitempty" maxLength:"1000"`
	SourceText       string            `json:"source_text" minLength:"1" maxLength:"1000"`
	Restrictions     []string          `json:"restrictions,omitempty" maxItems:"100"`
	Choices          []MicrowaveChoice `json:"choices,omitempty" maxItems:"100"`
}

// MicrowavePower distinguishes selected watts, percentages, and opaque levels.
type MicrowavePower struct {
	Watts   int    `json:"watts,omitempty" minimum:"1" maximum:"1000000"`
	Percent string `json:"percent,omitempty" pattern:"^(?:100|[1-9][0-9]?)$"`
	Text    string `json:"text,omitempty" maxLength:"1000"`
}

// MicrowaveChoice is one source-authored power and duration alternative.
type MicrowaveChoice struct {
	Duration         DurationExpression `json:"duration"`
	RatedOutputWatts int                `json:"rated_output_watts,omitempty" minimum:"1" maximum:"1000000"`
	Power            *MicrowavePower    `json:"power,omitempty"`
	SourceText       string             `json:"source_text" minLength:"1" maxLength:"1000"`
}

// TimeSummary answers how much work a recipe is and how long it takes before
// it can be eaten. Every value is derived from the recipe and is read-only.
type TimeSummary struct {
	Prep    *DurationExpression `json:"prep,omitempty"`
	Cook    *DurationExpression `json:"cook,omitempty"`
	Active  *DurationExpression `json:"active,omitempty" doc:"Time the recipe needs the cook"`
	Elapsed *DurationExpression `json:"elapsed,omitempty" doc:"Time from starting to being able to eat"`
	// WrittenTotal reports a total the recipe's own clock contradicts, so a
	// client can show what the recipe states without stating it as the truth.
	WrittenTotal *DurationExpression `json:"written_total,omitempty" doc:"An authored total the recipe itself contradicts, preserved but not presented as the elapsed time"`
}

// IngredientSection groups ingredients under an optional authored heading.
type IngredientSection struct {
	ID          string       `json:"id" pattern:"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$"`
	Title       string       `json:"title,omitempty" maxLength:"1000"`
	Ingredients []Ingredient `json:"ingredients" minItems:"1" maxItems:"10000"`
}

// Variant is one authored alternate realization of the recipe. A reader must
// present it as a variant rather than as a generic note, and is never required
// to work out what applying it would mean.
type Variant struct {
	ID    string `json:"id,omitempty" pattern:"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$" doc:"Recipe-local identifier that claims use to scope themselves to this variant"`
	Title string `json:"title,omitempty" maxLength:"1000"`
	Text  string `json:"text" minLength:"1" maxLength:"1000000"`
}

// Equipment describes equipment used by a recipe.
type Equipment struct {
	Name     string    `json:"name" minLength:"1" maxLength:"1000" doc:"Authored equipment description; not a normalized registry identity"`
	Quantity *Quantity `json:"quantity,omitempty" doc:"Authored equipment count or measure; does not automatically scale with recipe yield"`
	Note     string    `json:"note,omitempty" maxLength:"1000000"`
}

// Translation declares another rendition of a recipe. Title, recipe_id, and url
// identify the counterpart; at least one is present.
type Translation struct {
	Relation string `json:"relation" enum:"original,translation,alternate" doc:"original: this recipe was translated from the counterpart; translation: the counterpart was translated from this recipe; alternate: the same recipe in another language with no stated direction"`
	Language string `json:"language" minLength:"2" maxLength:"255" doc:"The counterpart's primary language as a BCP 47 tag"`
	Title    string `json:"title,omitempty" maxLength:"1000" doc:"The counterpart's title as authored"`
	RecipeID string `json:"recipe_id,omitempty" pattern:"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$" doc:"The counterpart's portable recipe document ID, not a library entry ID"`
	URL      string `json:"url,omitempty" format:"uri" maxLength:"10000" doc:"Where the counterpart is published"`
}

// Ingredient describes one ingredient occurrence or authored choice.
type Ingredient struct {
	Alternatives  []Ingredient      `json:"alternatives,omitempty" minItems:"2" maxItems:"100" doc:"Ordered complete authored branches; nested choices are refused"`
	Relation      string            `json:"relation,omitempty" enum:"or,and_or,preferred"`
	ID            string            `json:"id" pattern:"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$"`
	Name          string            `json:"name,omitempty" minLength:"1" maxLength:"1000"`
	Quantity      *Quantity         `json:"quantity,omitempty"`
	Optional      bool              `json:"optional,omitempty" doc:"The author sanctioned leaving this occurrence out. It belongs to the occurrence, never to the ingredient: the same thing may be optional in a garnish section and required by the method"`
	Note          string            `json:"note,omitempty" maxLength:"1000000"`
	SourceText    string            `json:"source_text,omitempty" maxLength:"1000000"`
	Normalization *Normalization    `json:"normalization,omitempty"`
	PackageSize   *SimpleQuantity   `json:"package_size,omitempty"`
	Temperature   *Temperature      `json:"temperature,omitempty"`
	Recipe        *RecipeDependency `json:"recipe,omitempty" doc:"The recipe that makes this ingredient, as the source identified it. Its content is not included"`
}

// Normalization records how an ingredient was normalized and any review state.
type Normalization struct {
	Method   string   `json:"method" enum:"authored,extracted,inferred,unresolved"`
	Review   string   `json:"review,omitempty" enum:"confirmed,corrected"`
	Warnings []string `json:"warnings,omitempty" maxItems:"100"`
}

// Quantity is a simple, summed, alternative, or opaque authored amount.
type Quantity struct {
	Kind    string              `json:"kind" enum:"simple,sum,alternatives,opaque"`
	Amount  *Amount             `json:"amount,omitempty"`
	Unit    string              `json:"unit,omitempty" pattern:"^[a-z][a-z0-9]*(\\.[a-z][a-z0-9]*)+$" maxLength:"255"`
	Terms   []SimpleQuantity    `json:"terms,omitempty" maxItems:"100"`
	Options []AlternativeOption `json:"options,omitempty" maxItems:"100"`
	Text    string              `json:"text,omitempty" maxLength:"1000"`
}

// SimpleQuantity is one amount and optional unit.
type SimpleQuantity struct {
	Kind   string `json:"kind" enum:"simple"`
	Amount Amount `json:"amount"`
	Unit   string `json:"unit,omitempty" pattern:"^[a-z][a-z0-9]*(\\.[a-z][a-z0-9]*)+$" maxLength:"255"`
}

// AlternativeOption describes one alternative ingredient option.
type AlternativeOption struct {
	Kind   string           `json:"kind" enum:"simple,sum"`
	Amount *Amount          `json:"amount,omitempty"`
	Unit   string           `json:"unit,omitempty" pattern:"^[a-z][a-z0-9]*(\\.[a-z][a-z0-9]*)+$" maxLength:"255"`
	Terms  []SimpleQuantity `json:"terms,omitempty" maxItems:"100"`
}

// Amount is an exact rational value or rational range.
type Amount struct {
	Value       *string `json:"value,omitempty" pattern:"^(0|[1-9][0-9]*|[1-9][0-9]*/[1-9][0-9]*)$" maxLength:"128"`
	Min         *string `json:"min,omitempty" pattern:"^(0|[1-9][0-9]*|[1-9][0-9]*/[1-9][0-9]*)$" maxLength:"128"`
	Max         *string `json:"max,omitempty" pattern:"^(0|[1-9][0-9]*|[1-9][0-9]*/[1-9][0-9]*)$" maxLength:"128"`
	Approximate bool    `json:"approximate,omitempty"`
}

// TemperatureAmount represents the numeric part of a temperature.
type TemperatureAmount struct {
	Value       *string `json:"value,omitempty" pattern:"^-?(0|[1-9][0-9]*|[1-9][0-9]*/[1-9][0-9]*)$" maxLength:"128"`
	Min         *string `json:"min,omitempty" pattern:"^-?(0|[1-9][0-9]*|[1-9][0-9]*/[1-9][0-9]*)$" maxLength:"128"`
	Max         *string `json:"max,omitempty" pattern:"^-?(0|[1-9][0-9]*|[1-9][0-9]*/[1-9][0-9]*)$" maxLength:"128"`
	Approximate bool    `json:"approximate,omitempty"`
}

// Temperature describes an authored temperature and its purpose.
type Temperature struct {
	Amount     TemperatureAmount   `json:"amount,omitzero"`
	Unit       string              `json:"unit,omitempty" pattern:"^[a-z][a-z0-9]*(\\.[a-z][a-z0-9]*)+$" maxLength:"255"`
	Text       string              `json:"text,omitempty" maxLength:"1000"`
	Purpose    *TemperaturePurpose `json:"purpose,omitempty"`
	Note       string              `json:"note,omitempty" maxLength:"1000"`
	SourceText string              `json:"source_text,omitempty" maxLength:"1000"`
}

// TemperaturePurpose describes why a temperature is specified.
type TemperaturePurpose struct {
	Kind  string `json:"kind" enum:"oven,internal,oil,water,surface,other"`
	Label string `json:"label,omitempty" maxLength:"1000"`
}

// InstructionSection groups recipe steps under an optional heading.
type InstructionSection struct {
	ID    string `json:"id" pattern:"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$"`
	Title string `json:"title,omitempty" maxLength:"1000"`
	Steps []Step `json:"steps" minItems:"1" maxItems:"10000"`
}

// IngredientUse describes an ingredient use in a recipe.
type IngredientUse struct {
	IngredientRef string    `json:"ingredient_ref" pattern:"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$"`
	Quantity      *Quantity `json:"quantity,omitempty"`
}

// Step is one authored recipe instruction.
type Step struct {
	ID             string          `json:"id" pattern:"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$"`
	Title          string          `json:"title,omitempty" maxLength:"1000" doc:"Optional authored heading for this instruction"`
	Text           string          `json:"text,omitempty" maxLength:"1000000" doc:"Instruction prose; a step requires a title or text"`
	IngredientUses []IngredientUse `json:"ingredient_uses,omitempty" maxItems:"10000"`
	ImageRefs      []string        `json:"image_refs,omitempty" maxItems:"10000"`
	TimingRefs     []string        `json:"timing_refs,omitempty" maxItems:"50" doc:"Canonical timing assertions this step is associated with"`
	Temperatures   []Temperature   `json:"temperatures,omitempty" maxItems:"100"`
}

// Asset describes a portable file referenced by a recipe.
type Asset struct {
	ID        string        `json:"id" pattern:"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$"`
	Path      string        `json:"path" pattern:"^assets/[A-Za-z0-9][A-Za-z0-9._/-]{0,510}$" maxLength:"512"`
	MediaType string        `json:"media_type" pattern:"^[A-Za-z0-9!#$&^_.+-]+/[A-Za-z0-9!#$&^_.+-]+$" maxLength:"255"`
	SHA256    string        `json:"sha256" pattern:"^[a-f0-9]{64}$"`
	Size      int64         `json:"size" minimum:"0"`
	Role      string        `json:"role,omitempty" enum:"image,source,other"`
	Alt       string        `json:"alt,omitempty" maxLength:"1000"`
	Caption   string        `json:"caption,omitempty" maxLength:"1000000"`
	Creator   string        `json:"creator,omitempty" maxLength:"1000" doc:"Who the source credits with creating the file"`
	Credit    string        `json:"credit,omitempty" maxLength:"1000000" doc:"Authored credit line"`
	Rights    string        `json:"rights,omitempty" maxLength:"1000000" doc:"Authored copyright or rights statement"`
	License   *AssetLicense `json:"license,omitempty" doc:"License the source applies to the file; absent means unknown"`
	SourceURL string        `json:"source_url,omitempty" format:"uri" maxLength:"10000" doc:"Page or file the asset came from"`
}

// LibraryEntry is the API representation of a stored recipe.
type LibraryEntry struct {
	ID     string         `json:"id" doc:"Identifier of this entry in the local Savorum library"`
	Recipe RecipeDocument `json:"recipe"`
	// TimeSummary applies Savorum's presentation policy to the authored coarse
	// clock and any named phases. It is reported so every client answers "how
	// much work" and "how long until we eat" the same way.
	TimeSummary TimeSummary `json:"time_summary"`
	Revision    uint64      `json:"revision" minimum:"1"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	DeletedAt   *time.Time  `json:"deleted_at,omitempty"`
}

// AssetReference identifies an asset referenced by an API resource.
type AssetReference struct {
	AssetID      string `json:"asset_id" pattern:"^sha256:[a-f0-9]{64}$"`
	LocalAssetID string `json:"local_asset_id"`
	MediaType    string `json:"media_type"`
	Alt          string `json:"alt,omitempty"`
	Caption      string `json:"caption,omitempty"`
	URL          string `json:"url,omitempty" doc:"Immutable asset URL"`
}

// LibraryBookSummary summarizes a book containing a recipe.
type LibraryBookSummary struct {
	ID    string `json:"id"`
	Kind  string `json:"kind" enum:"personal,publication"`
	Title string `json:"title"`
}

// TagSummary summarizes a tag assigned to a recipe.
type TagSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// FacetSummary identifies a library category assigned to a recipe.
type FacetSummary struct {
	ID    string `json:"id"`
	Kind  string `json:"kind" enum:"course,cuisine,method,dietary"`
	Value string `json:"value"`
}

// RecipeSummary provides a compact recipe representation.
type RecipeSummary struct {
	ID           string               `json:"id" doc:"Identifier of this entry in the local Savorum library"`
	RecipeID     string               `json:"recipe_id" doc:"Portable recipe document identifier"`
	Title        string               `json:"title"`
	Description  string               `json:"description,omitempty"`
	Authors      []string             `json:"authors"`
	ActiveTime   string               `json:"active_time,omitempty" doc:"Derived time this recipe needs the cook"`
	TotalTime    string               `json:"total_time,omitempty" doc:"Derived time from starting to being able to eat"`
	Revision     uint64               `json:"revision" minimum:"1"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
	DeletedAt    *time.Time           `json:"deleted_at,omitempty"`
	PrimaryImage *AssetReference      `json:"primary_image,omitempty"`
	Tags         []TagSummary         `json:"tags"`
	Facets       []FacetSummary       `json:"facets,omitempty" doc:"Library categories assigned to this recipe"`
	Books        []LibraryBookSummary `json:"books" doc:"Books this recipe was deliberately placed in"`
	SourceBooks  []LibraryBookSummary `json:"source_books" doc:"Publication books this recipe was taken from"`
	Favorite     bool                 `json:"favorite"`
	TimesCooked  uint64               `json:"times_cooked" minimum:"0" doc:"Imported baseline plus this reader's cooking records"`
	LastCooked   *string              `json:"last_cooked,omitempty" format:"date" doc:"Most recent cooking record"`
	Language     string               `json:"language,omitempty" doc:"The recipe's primary language as a BCP 47 tag"`
	Translations []SummaryRendition   `json:"translations,omitempty" doc:"Other renditions of a result grouped by translation; present only when grouping was requested"`
}

// SummaryRendition is another rendition of a grouped search result.
type SummaryRendition struct {
	ID       string `json:"id" doc:"Library entry identifier"`
	Title    string `json:"title"`
	Language string `json:"language" doc:"The rendition's primary language as a BCP 47 tag"`
	Matched  bool   `json:"matched" doc:"The rendition also matched the search; false when the request had no search"`
}

// RecipeReference names one library entry for pickers that do not need its summary.
type RecipeReference struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Revision uint64 `json:"revision" minimum:"1"`
}

// RecipeRevision represents a historical recipe revision.
type RecipeRevision struct {
	ActorID        string         `json:"actor_id,omitempty"`
	ActorName      string         `json:"actor_name,omitempty"`
	LibraryEntryID string         `json:"library_entry_id" doc:"Identifier of the owning entry in the local Savorum library"`
	Revision       uint64         `json:"revision" minimum:"1"`
	Recipe         RecipeDocument `json:"recipe"`
	State          string         `json:"state" enum:"active,trash"`
	Cause          string         `json:"cause" enum:"api,manual_edit,import,restore,trash"`
	SourceRevision *uint64        `json:"source_revision,omitempty" minimum:"1"`
	CreatedAt      time.Time      `json:"created_at"`
}

// RecipeRevisionSummary provides a compact historical revision representation.
type RecipeRevisionSummary struct {
	ActorID        string    `json:"actor_id,omitempty"`
	ActorName      string    `json:"actor_name,omitempty"`
	LibraryEntryID string    `json:"library_entry_id"`
	Revision       uint64    `json:"revision" minimum:"1"`
	State          string    `json:"state" enum:"active,trash"`
	Cause          string    `json:"cause" enum:"api,manual_edit,import,restore,trash"`
	SourceRevision *uint64   `json:"source_revision,omitempty" minimum:"1"`
	CreatedAt      time.Time `json:"created_at"`
	// Changes names the parts of the recipe this revision altered against the
	// one before it. It is absent where the document did not change and where
	// there is no earlier revision.
	Changes []string `json:"changes,omitempty" enum:"title,description,yield,times,ingredients,instructions,notes,variants,equipment,photos,source,labels,language,translations,other"`
}

func toDomain(document RecipeDocument) recipe.Document {
	return recipe.Document{
		SchemaVersion: document.SchemaVersion,
		ID:            recipe.RecipeID(document.ID), Title: document.Title, Description: document.Description,
		Language: document.Language, SourceLocale: document.SourceLocale,
		Authors: toDomainAuthors(document.Authors), Source: toDomainSource(document.Source),
		Yields: toDomainYields(document.Yields), Nutrition: toDomainNutrition(document.Nutrition), Times: toDomainTimes(document.Times),
		DietaryClaims: toDomainDietaryClaims(document.DietaryClaims), AllergenDeclarations: toDomainAllergenDeclarations(document.AllergenDeclarations),
		Categories: append([]string(nil), document.Categories...), Courses: append([]string(nil), document.Courses...),
		Keywords: append([]string(nil), document.Keywords...), Cuisines: append([]string(nil), document.Cuisines...),
		CookingMethods: append([]string(nil), document.CookingMethods...), Published: document.Published, Modified: document.Modified,
		Notes: append([]string(nil), document.Notes...), Variants: toDomainVariants(document.Variants), Equipment: toDomainEquipment(document.Equipment),
		Translations:        toDomainTranslations(document.Translations),
		IngredientSections:  toDomainIngredientSections(document.IngredientSections),
		InstructionSections: toDomainInstructionSections(document.InstructionSections),
		Assets:              toDomainAssets(document.Assets), ImageRefs: append([]string(nil), document.ImageRefs...), PrimaryImage: document.PrimaryImage,
		Extensions: document.Extensions,
	}
}

func fromDomain(document recipe.Document) RecipeDocument {
	return RecipeDocument{
		SchemaVersion: document.SchemaVersion,
		ID:            string(document.ID), Title: document.Title, Description: document.Description,
		Language: document.Language, SourceLocale: document.SourceLocale,
		Authors: fromDomainAuthors(document.Authors), Source: fromDomainSource(document.Source),
		Yields: fromDomainYields(document.Yields), Nutrition: fromDomainNutrition(document.Nutrition), Times: fromDomainTimes(document.Times),
		DietaryClaims: fromDomainDietaryClaims(document.DietaryClaims), AllergenDeclarations: fromDomainAllergenDeclarations(document.AllergenDeclarations),
		Categories: append([]string(nil), document.Categories...), Courses: append([]string(nil), document.Courses...),
		Keywords: append([]string(nil), document.Keywords...), Cuisines: append([]string(nil), document.Cuisines...),
		CookingMethods: append([]string(nil), document.CookingMethods...), Published: document.Published, Modified: document.Modified,
		Notes: append([]string(nil), document.Notes...), Variants: fromDomainVariants(document.Variants), Equipment: fromDomainEquipment(document.Equipment),
		Translations:        fromDomainTranslations(document.Translations),
		IngredientSections:  fromDomainIngredientSections(document.IngredientSections),
		InstructionSections: fromDomainInstructionSections(document.InstructionSections),
		Assets:              fromDomainAssets(document.Assets), ImageRefs: append([]string(nil), document.ImageRefs...), PrimaryImage: document.PrimaryImage,
		Extensions: document.Extensions, SREFOpaque: document.SREFOpaque,
	}
}

func toDomainAuthors(authors []Author) []recipe.Author {
	if authors == nil {
		return nil
	}

	result := make([]recipe.Author, len(authors))
	for index, author := range authors {
		result[index] = recipe.Author{Name: author.Name, Kind: recipe.AuthorKind(author.Kind), KindLabel: author.KindLabel, URL: author.URL}
	}

	return result
}

func fromDomainAuthors(authors []recipe.Author) []Author {
	if authors == nil {
		return nil
	}

	result := make([]Author, len(authors))
	for index, author := range authors {
		result[index] = Author{Name: author.Name, Kind: string(author.Kind), KindLabel: author.KindLabel, URL: author.URL}
	}

	return result
}

func fromLibrary(entry library.Entry) LibraryEntry {
	times := recipe.SummarizeTimes(entry.Recipe)

	return LibraryEntry{
		ID: string(entry.ID), Recipe: fromDomain(entry.Recipe),
		TimeSummary: TimeSummary{
			Prep: fromDomainDuration(times.Prep), Cook: fromDomainDuration(times.Cook), Active: fromDomainDuration(times.Active),
			Elapsed: fromDomainDuration(times.Elapsed), WrittenTotal: fromDomainDuration(times.WrittenTotal),
		},
		Revision:  entry.Revision,
		CreatedAt: entry.CreatedAt, UpdatedAt: entry.UpdatedAt,
		DeletedAt: entry.DeletedAt,
	}
}

func fromSummary(summary library.Summary) RecipeSummary {
	result := RecipeSummary{
		ID: string(summary.ID), RecipeID: string(summary.RecipeID), Title: summary.Title,
		Description: summary.Description, Authors: append([]string(nil), summary.Authors...),
		ActiveTime: summary.ActiveTime, TotalTime: summary.TotalTime,
		Revision: summary.Revision, CreatedAt: summary.CreatedAt,
		UpdatedAt: summary.UpdatedAt, DeletedAt: summary.DeletedAt,
		Tags: []TagSummary{}, Books: []LibraryBookSummary{}, SourceBooks: []LibraryBookSummary{},
		Favorite: summary.Favorite, TimesCooked: summary.TimesCooked, Language: summary.Language,
	}
	for _, rendition := range summary.Translations {
		result.Translations = append(result.Translations, SummaryRendition{
			ID: string(rendition.ID), Title: rendition.Title, Language: rendition.Language, Matched: rendition.Matched,
		})
	}

	if summary.LastCooked != nil {
		value := summary.LastCooked.String()
		result.LastCooked = &value
	}

	for _, tag := range summary.Tags {
		result.Tags = append(result.Tags, TagSummary{ID: tag.ID, Name: tag.Name, Color: tag.Color})
	}

	for _, facet := range summary.Facets {
		result.Facets = append(result.Facets, FacetSummary{ID: facet.ID, Kind: string(facet.Kind), Value: facet.Value})
	}

	for _, book := range summary.Books {
		result.Books = append(result.Books, LibraryBookSummary{ID: book.ID, Kind: string(book.Kind), Title: book.Title})
	}

	for _, book := range summary.SourceBooks {
		result.SourceBooks = append(result.SourceBooks, LibraryBookSummary{ID: book.ID, Kind: string(book.Kind), Title: book.Title})
	}

	if summary.PrimaryImage != nil {
		result.PrimaryImage = &AssetReference{
			AssetID: summary.PrimaryImage.ID, LocalAssetID: summary.PrimaryImage.LocalAssetID,
			MediaType: summary.PrimaryImage.MediaType, Alt: summary.PrimaryImage.Alt,
			Caption: summary.PrimaryImage.Caption,
		}
		if summary.PrimaryImage.Available {
			result.PrimaryImage.URL = "/api/v1/recipes/" + string(summary.ID) + "/assets/" + summary.PrimaryImage.ID
		}
	}

	return result
}

func fromRevision(revision library.Revision) RecipeRevision {
	return RecipeRevision{
		LibraryEntryID: string(revision.EntryID), Revision: revision.Number, Recipe: fromDomain(revision.Recipe),
		State: string(revision.State), Cause: string(revision.Cause),
		SourceRevision: revision.SourceRevision, CreatedAt: revision.CreatedAt, ActorID: revision.ActorID, ActorName: revision.ActorName,
	}
}

func fromRevisionSummary(revision library.RevisionSummary) RecipeRevisionSummary {
	return RecipeRevisionSummary{
		LibraryEntryID: string(revision.EntryID), Revision: revision.Number,
		State: string(revision.State), Cause: string(revision.Cause),
		SourceRevision: revision.SourceRevision, CreatedAt: revision.CreatedAt, ActorID: revision.ActorID, ActorName: revision.ActorName,
		Changes: revision.Changes,
	}
}

func toDomainSource(source *Source) *recipe.Source {
	if source == nil {
		return nil
	}

	return &recipe.Source{Name: source.Name, URL: source.URL, Application: source.Application, SourceID: source.SourceID, RetrievedAt: source.RetrievedAt, Attribution: source.Attribution}
}

func fromDomainSource(source *recipe.Source) *Source {
	if source == nil {
		return nil
	}

	return &Source{Name: source.Name, URL: source.URL, Application: source.Application, SourceID: source.SourceID, RetrievedAt: source.RetrievedAt, Attribution: source.Attribution}
}

func toDomainTimes(value *Times) *recipe.Times {
	if value == nil {
		return nil
	}

	return &recipe.Times{
		Prep:       toDomainDuration(value.Prep),
		Cook:       toDomainDuration(value.Cook),
		Additional: toDomainDuration(value.Additional),
		Total:      toDomainDuration(value.Total),
		Assertions: toDomainAssertions(value.Assertions),
	}
}

func fromDomainTimes(value *recipe.Times) *Times {
	if value == nil {
		return nil
	}

	return &Times{
		Prep:       fromDomainDuration(value.Prep),
		Cook:       fromDomainDuration(value.Cook),
		Additional: fromDomainDuration(value.Additional),
		Total:      fromDomainDuration(value.Total),
		Assertions: fromDomainAssertions(value.Assertions),
	}
}

func toDomainDuration(value *DurationExpression) *recipe.DurationExpression {
	if value == nil {
		return nil
	}

	expression := recipe.DurationExpression(*value)

	return &expression
}

func fromDomainDuration(value *recipe.DurationExpression) *DurationExpression {
	if value == nil {
		return nil
	}

	expression := DurationExpression(*value)

	return &expression
}

func toDomainAssertions(values []TimingAssertion) []recipe.TimingAssertion {
	if values == nil {
		return nil
	}

	result := make([]recipe.TimingAssertion, len(values))
	for index, value := range values {
		result[index] = recipe.TimingAssertion{
			ID: value.ID, Kind: recipe.TimingKind(value.Kind), Label: value.Label,
			Duration:  recipe.DurationExpression(value.Duration),
			Attention: recipe.Attention(value.Attention), Note: value.Note,
			Microwave:  toDomainMicrowave(value.Microwave),
			Provenance: recipe.TimingProvenance(value.Provenance),
		}
	}

	return result
}

func fromDomainAssertions(values []recipe.TimingAssertion) []TimingAssertion {
	if values == nil {
		return nil
	}

	result := make([]TimingAssertion, len(values))
	for index, value := range values {
		result[index] = TimingAssertion{
			ID: value.ID, Kind: string(value.Kind), Label: value.Label,
			Duration:  DurationExpression(value.Duration),
			Attention: string(value.Attention), Note: value.Note,
			Microwave:  fromDomainMicrowave(value.Microwave),
			Provenance: string(value.Provenance),
		}
	}

	return result
}

func toDomainMicrowave(value *MicrowaveConditions) *recipe.MicrowaveConditions {
	if value == nil {
		return nil
	}

	result := &recipe.MicrowaveConditions{RatedOutputWatts: value.RatedOutputWatts, Power: toDomainMicrowavePower(value.Power), ModeText: value.ModeText, SourceText: value.SourceText, Restrictions: append([]string(nil), value.Restrictions...)}
	for _, choice := range value.Choices {
		result.Choices = append(result.Choices, recipe.MicrowaveChoice{Duration: recipe.DurationExpression(choice.Duration), RatedOutputWatts: choice.RatedOutputWatts, Power: toDomainMicrowavePower(choice.Power), SourceText: choice.SourceText})
	}

	return result
}

func fromDomainMicrowave(value *recipe.MicrowaveConditions) *MicrowaveConditions {
	if value == nil {
		return nil
	}

	result := &MicrowaveConditions{RatedOutputWatts: value.RatedOutputWatts, Power: fromDomainMicrowavePower(value.Power), ModeText: value.ModeText, SourceText: value.SourceText, Restrictions: append([]string(nil), value.Restrictions...)}
	for _, choice := range value.Choices {
		result.Choices = append(result.Choices, MicrowaveChoice{Duration: DurationExpression(choice.Duration), RatedOutputWatts: choice.RatedOutputWatts, Power: fromDomainMicrowavePower(choice.Power), SourceText: choice.SourceText})
	}

	return result
}

func toDomainMicrowavePower(value *MicrowavePower) *recipe.MicrowavePower {
	if value == nil {
		return nil
	}

	return &recipe.MicrowavePower{Watts: value.Watts, Percent: value.Percent, Text: value.Text}
}

func fromDomainMicrowavePower(value *recipe.MicrowavePower) *MicrowavePower {
	if value == nil {
		return nil
	}

	return &MicrowavePower{Watts: value.Watts, Percent: value.Percent, Text: value.Text}
}

func toDomainIngredientSections(sections []IngredientSection) []recipe.IngredientSection {
	result := make([]recipe.IngredientSection, len(sections))
	for index, section := range sections {
		result[index] = recipe.IngredientSection{ID: section.ID, Title: section.Title, Ingredients: make([]recipe.Ingredient, len(section.Ingredients))}
		for ingredientIndex, ingredient := range section.Ingredients {
			result[index].Ingredients[ingredientIndex] = toDomainIngredient(ingredient)
		}
	}

	return result
}

func toDomainIngredient(ingredient Ingredient) recipe.Ingredient {
	return recipe.Ingredient{
		Alternatives: toDomainIngredients(ingredient.Alternatives), Relation: ingredient.Relation, ID: ingredient.ID, Name: ingredient.Name, Quantity: toDomainQuantity(ingredient.Quantity),
		Optional: ingredient.Optional,
		Note:     ingredient.Note, SourceText: ingredient.SourceText, Normalization: toDomainNormalization(ingredient.Normalization),
		PackageSize: toDomainSimpleQuantity(ingredient.PackageSize), Temperature: toDomainTemperature(ingredient.Temperature),
		Recipe: toDomainRecipeReference(ingredient.Recipe),
	}
}

func toDomainIngredients(values []Ingredient) []recipe.Ingredient {
	if values == nil {
		return nil
	}

	result := make([]recipe.Ingredient, len(values))
	for index, value := range values {
		result[index] = toDomainIngredient(value)
	}

	return result
}

func toDomainVariants(values []Variant) []recipe.Variant {
	if values == nil {
		return nil
	}

	result := make([]recipe.Variant, len(values))
	for index, value := range values {
		result[index] = recipe.Variant{ID: value.ID, Title: value.Title, Text: value.Text}
	}

	return result
}

func fromDomainVariants(values []recipe.Variant) []Variant {
	if values == nil {
		return nil
	}

	result := make([]Variant, len(values))
	for index, value := range values {
		result[index] = Variant{ID: value.ID, Title: value.Title, Text: value.Text}
	}

	return result
}

func toDomainTranslations(values []Translation) []recipe.Translation {
	if values == nil {
		return nil
	}

	result := make([]recipe.Translation, len(values))
	for index, value := range values {
		result[index] = recipe.Translation{
			Relation: recipe.TranslationRelation(value.Relation), Language: value.Language,
			Title: value.Title, RecipeID: recipe.RecipeID(value.RecipeID), URL: value.URL,
		}
	}

	return result
}

func fromDomainTranslations(values []recipe.Translation) []Translation {
	if values == nil {
		return nil
	}

	result := make([]Translation, len(values))
	for index, value := range values {
		result[index] = Translation{
			Relation: string(value.Relation), Language: value.Language,
			Title: value.Title, RecipeID: string(value.RecipeID), URL: value.URL,
		}
	}

	return result
}

func toDomainEquipment(values []Equipment) []recipe.Equipment {
	if values == nil {
		return nil
	}

	result := make([]recipe.Equipment, len(values))
	for index, value := range values {
		result[index] = recipe.Equipment{Name: value.Name, Quantity: toDomainQuantity(value.Quantity), Note: value.Note}
	}

	return result
}

func fromDomainEquipment(values []recipe.Equipment) []Equipment {
	if values == nil {
		return nil
	}

	result := make([]Equipment, len(values))
	for index, value := range values {
		result[index] = Equipment{Name: value.Name, Quantity: fromDomainQuantity(value.Quantity), Note: value.Note}
	}

	return result
}

func fromDomainIngredientSections(sections []recipe.IngredientSection) []IngredientSection {
	result := make([]IngredientSection, len(sections))
	for index, section := range sections {
		result[index] = IngredientSection{ID: section.ID, Title: section.Title, Ingredients: make([]Ingredient, len(section.Ingredients))}
		for ingredientIndex, ingredient := range section.Ingredients {
			result[index].Ingredients[ingredientIndex] = fromDomainIngredient(ingredient)
		}
	}

	return result
}

func fromDomainIngredient(ingredient recipe.Ingredient) Ingredient {
	return Ingredient{
		Alternatives: fromDomainIngredients(ingredient.Alternatives), Relation: ingredient.Relation, ID: ingredient.ID, Name: ingredient.Name, Quantity: fromDomainQuantity(ingredient.Quantity),
		Optional: ingredient.Optional,
		Note:     ingredient.Note, SourceText: ingredient.SourceText, Normalization: fromDomainNormalization(ingredient.Normalization),
		PackageSize: fromDomainSimpleQuantity(ingredient.PackageSize), Temperature: fromDomainTemperature(ingredient.Temperature),
		Recipe: fromDomainRecipeReference(ingredient.Recipe),
	}
}

func fromDomainIngredients(values []recipe.Ingredient) []Ingredient {
	if values == nil {
		return nil
	}

	result := make([]Ingredient, len(values))
	for index, value := range values {
		result[index] = fromDomainIngredient(value)
	}

	return result
}

func toDomainInstructionSections(sections []InstructionSection) []recipe.InstructionSection {
	result := make([]recipe.InstructionSection, len(sections))
	for index, section := range sections {
		result[index] = recipe.InstructionSection{ID: section.ID, Title: section.Title, Steps: make([]recipe.Step, len(section.Steps))}
		for stepIndex, step := range section.Steps {
			result[index].Steps[stepIndex] = recipe.Step{ID: step.ID, Title: step.Title, Text: step.Text, IngredientUses: toDomainIngredientUses(step.IngredientUses), ImageRefs: append([]string(nil), step.ImageRefs...), TimingRefs: append([]string(nil), step.TimingRefs...), Temperatures: toDomainTemperatures(step.Temperatures)}
		}
	}

	return result
}

func fromDomainInstructionSections(sections []recipe.InstructionSection) []InstructionSection {
	result := make([]InstructionSection, len(sections))
	for index, section := range sections {
		result[index] = InstructionSection{ID: section.ID, Title: section.Title, Steps: make([]Step, len(section.Steps))}
		for stepIndex, step := range section.Steps {
			result[index].Steps[stepIndex] = Step{ID: step.ID, Title: step.Title, Text: step.Text, IngredientUses: fromDomainIngredientUses(step.IngredientUses), ImageRefs: append([]string(nil), step.ImageRefs...), TimingRefs: append([]string(nil), step.TimingRefs...), Temperatures: fromDomainTemperatures(step.Temperatures)}
		}
	}

	return result
}

func toDomainIngredientUses(values []IngredientUse) []recipe.IngredientUse {
	if values == nil {
		return nil
	}

	result := make([]recipe.IngredientUse, len(values))
	for index, value := range values {
		result[index] = recipe.IngredientUse{IngredientRef: value.IngredientRef, Quantity: toDomainQuantity(value.Quantity)}
	}

	return result
}

func fromDomainIngredientUses(values []recipe.IngredientUse) []IngredientUse {
	if values == nil {
		return nil
	}

	result := make([]IngredientUse, len(values))
	for index, value := range values {
		result[index] = IngredientUse{IngredientRef: value.IngredientRef, Quantity: fromDomainQuantity(value.Quantity)}
	}

	return result
}

func toDomainQuantity(value *Quantity) *recipe.Quantity {
	if value == nil {
		return nil
	}

	result := &recipe.Quantity{Kind: recipe.QuantityKind(value.Kind), Amount: toDomainAmount(value.Amount), Unit: value.Unit, Text: value.Text}

	if len(value.Terms) > 0 {
		result.Terms = make([]recipe.SimpleQuantity, len(value.Terms))
	}

	for index := range value.Terms {
		result.Terms[index] = *toDomainSimpleQuantity(&value.Terms[index])
	}

	if len(value.Options) > 0 {
		result.Options = make([]recipe.AlternativeOption, len(value.Options))
	}

	for index := range value.Options {
		result.Options[index] = *toDomainAlternativeOption(&value.Options[index])
	}

	return result
}

func fromDomainQuantity(value *recipe.Quantity) *Quantity {
	if value == nil {
		return nil
	}

	result := &Quantity{Kind: string(value.Kind), Amount: fromDomainAmount(value.Amount), Unit: value.Unit, Text: value.Text}

	result.Terms = make([]SimpleQuantity, len(value.Terms))
	for index := range value.Terms {
		result.Terms[index] = *fromDomainSimpleQuantity(&value.Terms[index])
	}

	result.Options = make([]AlternativeOption, len(value.Options))
	for index := range value.Options {
		result.Options[index] = *fromDomainAlternativeOption(&value.Options[index])
	}

	return result
}

func toDomainAlternativeOption(value *AlternativeOption) *recipe.AlternativeOption {
	if value == nil {
		return nil
	}

	result := &recipe.AlternativeOption{Kind: recipe.QuantityKind(value.Kind), Amount: toDomainAmount(value.Amount), Unit: value.Unit}

	if len(value.Terms) > 0 {
		result.Terms = make([]recipe.SimpleQuantity, len(value.Terms))
	}

	for index := range value.Terms {
		result.Terms[index] = *toDomainSimpleQuantity(&value.Terms[index])
	}

	return result
}

func fromDomainAlternativeOption(value *recipe.AlternativeOption) *AlternativeOption {
	if value == nil {
		return nil
	}

	result := &AlternativeOption{Kind: string(value.Kind), Amount: fromDomainAmount(value.Amount), Unit: value.Unit}

	result.Terms = make([]SimpleQuantity, len(value.Terms))
	for index := range value.Terms {
		result.Terms[index] = *fromDomainSimpleQuantity(&value.Terms[index])
	}

	return result
}

func toDomainSimpleQuantity(value *SimpleQuantity) *recipe.SimpleQuantity {
	if value == nil {
		return nil
	}

	return &recipe.SimpleQuantity{Kind: recipe.QuantityKind(value.Kind), Amount: *toDomainAmount(&value.Amount), Unit: value.Unit}
}

func fromDomainSimpleQuantity(value *recipe.SimpleQuantity) *SimpleQuantity {
	if value == nil {
		return nil
	}

	return &SimpleQuantity{Kind: string(value.Kind), Amount: *fromDomainAmount(&value.Amount), Unit: value.Unit}
}

func toDomainAmount(value *Amount) *recipe.Amount {
	if value == nil {
		return nil
	}

	return &recipe.Amount{Value: toDomainRational(value.Value), Min: toDomainRational(value.Min), Max: toDomainRational(value.Max), Approximate: value.Approximate}
}

func fromDomainAmount(value *recipe.Amount) *Amount {
	if value == nil {
		return nil
	}

	return &Amount{Value: fromDomainRational(value.Value), Min: fromDomainRational(value.Min), Max: fromDomainRational(value.Max), Approximate: value.Approximate}
}

func toDomainRational(value *string) *recipe.Rational {
	if value == nil {
		return nil
	}

	converted := recipe.Rational(*value)

	return &converted
}

func fromDomainRational(value *recipe.Rational) *string {
	if value == nil {
		return nil
	}

	converted := value.String()

	return &converted
}

func toDomainNormalization(value *Normalization) *recipe.Normalization {
	if value == nil {
		return nil
	}

	return &recipe.Normalization{Method: recipe.NormalizationMethod(value.Method), Review: recipe.ReviewState(value.Review), Warnings: append([]string(nil), value.Warnings...)}
}

func fromDomainNormalization(value *recipe.Normalization) *Normalization {
	if value == nil {
		return nil
	}

	return &Normalization{Method: string(value.Method), Review: string(value.Review), Warnings: append([]string(nil), value.Warnings...)}
}

func toDomainTemperature(value *Temperature) *recipe.Temperature {
	if value == nil {
		return nil
	}

	var purpose *recipe.TemperaturePurpose
	if value.Purpose != nil {
		purpose = &recipe.TemperaturePurpose{Kind: recipe.TemperaturePurposeKind(value.Purpose.Kind), Label: value.Purpose.Label}
	}

	return &recipe.Temperature{Amount: toDomainTemperatureAmount(value.Amount), Unit: value.Unit, Text: value.Text, Purpose: purpose, Note: value.Note, SourceText: value.SourceText}
}

func fromDomainTemperature(value *recipe.Temperature) *Temperature {
	if value == nil {
		return nil
	}

	var purpose *TemperaturePurpose
	if value.Purpose != nil {
		purpose = &TemperaturePurpose{Kind: string(value.Purpose.Kind), Label: value.Purpose.Label}
	}

	return &Temperature{Amount: fromDomainTemperatureAmount(value.Amount), Unit: value.Unit, Text: value.Text, Purpose: purpose, Note: value.Note, SourceText: value.SourceText}
}

func toDomainTemperatureAmount(value TemperatureAmount) recipe.Amount {
	return recipe.Amount{Value: toDomainRational(value.Value), Min: toDomainRational(value.Min), Max: toDomainRational(value.Max), Approximate: value.Approximate}
}

func fromDomainTemperatureAmount(value recipe.Amount) TemperatureAmount {
	return TemperatureAmount{Value: fromDomainRational(value.Value), Min: fromDomainRational(value.Min), Max: fromDomainRational(value.Max), Approximate: value.Approximate}
}

func toDomainTemperatures(values []Temperature) []recipe.Temperature {
	result := make([]recipe.Temperature, len(values))
	for index := range values {
		result[index] = *toDomainTemperature(&values[index])
	}

	return result
}

func fromDomainTemperatures(values []recipe.Temperature) []Temperature {
	result := make([]Temperature, len(values))
	for index := range values {
		result[index] = *fromDomainTemperature(&values[index])
	}

	return result
}

func toDomainAssets(assets []Asset) []recipe.Asset {
	result := make([]recipe.Asset, len(assets))
	for index, asset := range assets {
		result[index] = recipe.Asset{
			ID: asset.ID, Path: asset.Path, MediaType: asset.MediaType, SHA256: asset.SHA256, Size: asset.Size, Role: recipe.AssetRole(asset.Role), Alt: asset.Alt, Caption: asset.Caption,
			Creator: asset.Creator, Credit: asset.Credit, Rights: asset.Rights, License: toDomainAssetLicense(asset.License), SourceURL: asset.SourceURL,
		}
	}

	return result
}

func fromDomainAssets(assets []recipe.Asset) []Asset {
	result := make([]Asset, len(assets))
	for index, asset := range assets {
		result[index] = Asset{
			ID: asset.ID, Path: asset.Path, MediaType: asset.MediaType, SHA256: asset.SHA256, Size: asset.Size, Role: string(asset.Role), Alt: asset.Alt, Caption: asset.Caption,
			Creator: asset.Creator, Credit: asset.Credit, Rights: asset.Rights, License: fromDomainAssetLicense(asset.License), SourceURL: asset.SourceURL,
		}
	}

	return result
}
