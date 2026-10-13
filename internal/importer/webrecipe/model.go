// Package webrecipe extracts a recipe from a web page.
//
// Structured data on real recipe sites is polymorphic and frequently invalid,
// so this package is a tolerant pipeline rather than a decoder:
//
//	fetch → discover structured-data blocks → parse each independently →
//	discover Recipe candidates → select one → normalize → enrich → hand over
//
// Two rules hold throughout. A broken block is a warning, not a failure, while
// any usable interpretation remains. And nothing the page stated is discarded:
// the model is richer than a Savorum recipe, and only the importer's mapper
// decides what Savorum keeps.
package webrecipe

import (
	"errors"
	"fmt"
)

// FailureCode names why an extraction produced no recipe.
type FailureCode string

// Fetch limits bound remote recipe requests and their response bodies.
const (
	// FetchBlocked covers a refusal by the site: a bot wall, an authentication
	// requirement, or any other status that is not a recipe.
	FetchBlocked FailureCode = "FETCH_BLOCKED"
	FetchTimeout FailureCode = "FETCH_TIMEOUT"
	// FetchTooLarge is a response over the ceiling, detected while streaming.
	FetchTooLarge FailureCode = "FETCH_TOO_LARGE"
	// FetchRejected is a destination Savorum will not talk to at all: a private
	// address, a non-HTTP scheme, or a redirect into either.
	FetchRejected          FailureCode = "FETCH_REJECTED"
	UnsupportedContentType FailureCode = "UNSUPPORTED_CONTENT_TYPE"
	// NoRecipeStructuredData means the page parsed and held no Recipe at all.
	NoRecipeStructuredData FailureCode = "NO_RECIPE_STRUCTURED_DATA"
	// MalformedStructuredData means every candidate block was unreadable. A
	// page with one broken block and one good one does not reach this.
	MalformedStructuredData FailureCode = "MALFORMED_STRUCTURED_DATA"
	// ExtractionLimited means the page exceeded a parser work, nesting, or
	// structure budget. It is deterministic and must not be retried unchanged.
	ExtractionLimited FailureCode = "EXTRACTION_LIMITED"
	// RecipeIncomplete means a Recipe was found and is not importable, such as
	// a name and image only. That is valid markup, not malformed data.
	RecipeIncomplete    FailureCode = "RECIPE_INCOMPLETE"
	NormalizationFailed FailureCode = "NORMALIZATION_FAILED"
)

// Failure is an extraction or fetch error carrying the classification above.
type Failure struct {
	Code    FailureCode
	Message string
	Err     error
}

func (f *Failure) Error() string {
	if f.Err != nil {
		return fmt.Sprintf("%s: %s: %v", f.Code, f.Message, f.Err)
	}

	return fmt.Sprintf("%s: %s", f.Code, f.Message)
}

func (f *Failure) Unwrap() error { return f.Err }

func failuref(code FailureCode, cause error, format string, arguments ...any) *Failure {
	return &Failure{Code: code, Message: fmt.Sprintf(format, arguments...), Err: cause}
}

// CodeOf reports the classification of an error, or an empty code when the
// error did not come from this package.
func CodeOf(err error) FailureCode {
	if failure, ok := errors.AsType[*Failure](err); ok {
		return failure.Code
	}

	return ""
}

// Warning is something the extraction survived. Warnings travel with the
// recipe to review.
type Warning struct {
	Code    string
	Message string
}

// Recipe is the normalized intermediate model: everything the page said, in
// the shape the page said it.
type Recipe struct {
	// SourceID is the structured data's own @id, kept separately from URL
	// because they disagree often enough to be worth telling apart.
	SourceID        string
	URL             string
	Name            string
	Description     string
	Language        string
	Authors         []Author
	Keywords        []string
	Cuisines        []string
	Categories      []string
	CookingMethods  []string
	Published       string
	Modified        string
	Yields          []Yield
	Nutrition       *Nutrition
	Diets           []string
	ImageProvenance map[string]ImageProvenance
	Times           Times
	// IngredientGroups always holds at least one group once anything was
	// found. An ungrouped ingredient list is one group with no title.
	IngredientGroups []IngredientGroup
	// Sections may nest, because HowToSection may nest.
	Sections  []InstructionSection
	Images    []string
	Notes     []string
	Equipment []string
	Warnings  []Warning
	// ReviewText keeps a DOM-derived draft editable without assigning uncertain
	// regions to ingredients or steps. ExtractionMethod records its provenance.
	ReviewText       string
	ExtractionMethod string
	MetadataSource   string
}

// Author preserves only the authorship facts a page states explicitly. Kind
// remains empty for a string or an untyped object; equal names do not establish
// identity, and the recipe page or importing application is not an author.
type Author struct {
	Name string
	Kind string
	URL  string
}

// Yield keeps the source wording alongside any structured value.
type Yield struct {
	Text  string
	Value string
	Unit  string
}

// Times carries coarse authored timing values from structured web data.
type Times struct {
	Prep       string
	Cook       string
	Additional string
	Total      string
}

// IngredientGroup is one titled ingredient group, such as "For the frosting".
type IngredientGroup struct {
	Title       string
	Ingredients []Ingredient
}

// Ingredient keeps the source line whatever else is understood about it.
// Amount, Unit, and Name are filled only when the page itself supplied them
// separately, as a PropertyValue or through a recipe card's own markup.
type Ingredient struct {
	// Text is the line as the page gave it, or as close as can be
	// reconstructed. It is never dropped and never rewritten.
	Text   string
	Amount string
	Unit   string
	Name   string
	Note   string
}

// InstructionSection mirrors HowToSection, including its ability to contain
// further sections rather than steps.
type InstructionSection struct {
	Title    string
	Steps    []Step
	Sections []InstructionSection
}

// Step mirrors HowToStep. Name is a heading, kept apart from Text.
type Step struct {
	Name string
	Text string
	// Supply is the page's own statement of which ingredients this step uses.
	// Inference must never overwrite it.
	Supply []string
	Tools  []string
	Images []string
	URL    string
	// Directions and Tips come from a step's own itemListElement, which is how
	// a HowToStep carries content when it has no text of its own.
	Directions []string
	Tips       []string
}

// Usable reports whether a Recipe entity has recipe content, not just the name
// and image a rich result requires.
func (r *Recipe) Usable() bool {
	return r.Name != "" && (r.ingredientCount() > 0 || r.stepCount() > 0)
}

func (r *Recipe) ingredientCount() int {
	total := 0
	for _, group := range r.IngredientGroups {
		total += len(group.Ingredients)
	}

	return total
}

func (r *Recipe) stepCount() int {
	var count func([]InstructionSection) int

	count = func(sections []InstructionSection) int {
		total := 0
		for _, section := range sections {
			total += len(section.Steps) + count(section.Sections)
		}

		return total
	}

	return count(r.Sections)
}

func (r *Recipe) warn(code, format string, arguments ...any) {
	r.Warnings = append(r.Warnings, Warning{Code: code, Message: fmt.Sprintf(format, arguments...)})
}
