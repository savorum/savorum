package sref

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
)

// wireDocument is the SREF recipe document. It is deliberately the shape the
// specification defines rather than Savorum's stored shape: there is no
// storage discriminator, and the version envelope is required.
type wireDocument struct {
	SREF         envelope        `json:"sref"`
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Description  string          `json:"description,omitempty"`
	Language     string          `json:"language,omitempty"`
	SourceLocale string          `json:"source_locale,omitempty"`
	Authors      []recipe.Author `json:"authors,omitempty"`
	Source       *recipe.Source  `json:"source,omitempty"`
	// Yield is read from 0.2 documents.
	Yield                *recipe.Yield                `json:"yield,omitempty"`
	Yields               []recipe.Yield               `json:"yields,omitempty"`
	Nutrition            []recipe.NutritionStatement  `json:"nutrition,omitempty"`
	DietaryClaims        []recipe.DietaryClaim        `json:"dietary_claims,omitempty"`
	AllergenDeclarations []recipe.AllergenDeclaration `json:"allergen_declarations,omitempty"`
	Times                *wireTimes                   `json:"times,omitempty"`
	Categories           []string                     `json:"categories,omitempty"`
	Courses              []string                     `json:"courses,omitempty"`
	Keywords             []string                     `json:"keywords,omitempty"`
	Cuisines             []string                     `json:"cuisines,omitempty"`
	CookingMethods       []string                     `json:"cooking_methods,omitempty"`
	Published            string                       `json:"published,omitempty"`
	Modified             string                       `json:"modified,omitempty"`
	Notes                []string                     `json:"notes,omitempty"`
	Variants             []recipe.Variant             `json:"variants,omitempty"`
	Equipment            []recipe.Equipment           `json:"equipment,omitempty"`
	Translations         []recipe.Translation         `json:"translations,omitempty"`
	IngredientSections   []recipe.IngredientSection   `json:"ingredient_sections,omitempty"`
	InstructionSections  []wireInstructionSection     `json:"instruction_sections,omitempty"`
	Assets               []recipe.Asset               `json:"assets,omitempty"`
	ImageRefs            []string                     `json:"image_refs,omitempty"`
	PrimaryImage         string                       `json:"primary_image,omitempty"`
}

// wireTimes is the exchanged clock. It is shaped like the domain's, because
// the domain adopted SREF's model, with one deliberate omission: Provenance is
// Savorum's record of how it came to hold a timing and says nothing about the
// recipe, so it does not travel.
type wireTimes struct {
	Prep       *recipe.DurationExpression `json:"prep,omitempty"`
	Cook       *recipe.DurationExpression `json:"cook,omitempty"`
	Additional *recipe.DurationExpression `json:"additional,omitempty"`
	Total      *recipe.DurationExpression `json:"total,omitempty"`
	Assertions []wireTimingAssertion      `json:"assertions,omitempty"`
}

type wireTimingAssertion struct {
	ID        string                      `json:"id"`
	Kind      recipe.TimingKind           `json:"kind,omitempty"`
	Label     string                      `json:"label,omitempty"`
	Duration  recipe.DurationExpression   `json:"duration"`
	Attention recipe.Attention            `json:"attention,omitempty"`
	Note      string                      `json:"note,omitempty"`
	Microwave *recipe.MicrowaveConditions `json:"microwave,omitempty"`
}

func toWireTimes(times *recipe.Times) *wireTimes {
	if times == nil {
		return nil
	}

	shaped := &wireTimes{Prep: times.Prep, Cook: times.Cook, Additional: times.Additional, Total: times.Total}
	for _, assertion := range times.Assertions {
		shaped.Assertions = append(shaped.Assertions, wireTimingAssertion{
			ID:        assertion.ID,
			Kind:      assertion.Kind,
			Label:     assertion.Label,
			Duration:  assertion.Duration,
			Attention: assertion.Attention,
			Note:      assertion.Note,
			Microwave: assertion.Microwave,
		})
	}

	return shaped
}

// fromWireTimes reads an exchanged clock. Provenance is left empty, so
// reconciliation never rewrites a timing another application authored.
func fromWireTimes(times *wireTimes) *recipe.Times {
	if times == nil {
		return nil
	}

	document := &recipe.Times{Prep: times.Prep, Cook: times.Cook, Additional: times.Additional, Total: times.Total}
	for _, assertion := range times.Assertions {
		document.Assertions = append(document.Assertions, recipe.TimingAssertion{
			ID:        assertion.ID,
			Kind:      assertion.Kind,
			Label:     assertion.Label,
			Duration:  assertion.Duration,
			Attention: assertion.Attention,
			Note:      assertion.Note,
			Microwave: assertion.Microwave,
		})
	}

	return document
}

type wireInstructionSection struct {
	ID    string     `json:"id"`
	Title string     `json:"title,omitempty"`
	Steps []wireStep `json:"steps"`
}

type wireStep struct {
	ID             string                 `json:"id"`
	Title          string                 `json:"title,omitempty"`
	Text           string                 `json:"text"`
	IngredientUses []recipe.IngredientUse `json:"ingredient_uses,omitempty"`
	ImageRefs      []string               `json:"image_refs,omitempty"`
	TimingRefs     []string               `json:"timing_refs,omitempty"`
	Temperatures   []recipe.Temperature   `json:"temperatures,omitempty"`
}

// toWire produces the member set of an SREF document, with extensions merged
// back in at the top level under the names they must travel under.
func toWire(document recipe.Document) (map[string]json.RawMessage, error) {
	shaped := wireDocument{
		SREF:                 envelope{Version: Version, UnitRegistry: UnitRegistryVersion},
		ID:                   string(document.ID),
		Title:                document.Title,
		Description:          document.Description,
		Language:             document.Language,
		SourceLocale:         document.SourceLocale,
		Authors:              cloneAuthors(document.Authors),
		Source:               document.Source,
		Yields:               document.Yields,
		Nutrition:            document.Nutrition,
		DietaryClaims:        document.DietaryClaims,
		AllergenDeclarations: document.AllergenDeclarations,
		Times:                toWireTimes(document.Times),
		Categories:           document.Categories,
		Courses:              document.Courses,
		Keywords:             document.Keywords,
		Cuisines:             document.Cuisines,
		CookingMethods:       document.CookingMethods,
		Published:            document.Published,
		Modified:             document.Modified,
		Notes:                document.Notes,
		Variants:             document.Variants,
		Equipment:            document.Equipment,
		Translations:         document.Translations,
		IngredientSections:   document.IngredientSections,
		InstructionSections:  toWireInstructionSections(document.InstructionSections),
		Assets:               document.Assets,
		ImageRefs:            append([]string(nil), document.ImageRefs...),
		PrimaryImage:         document.PrimaryImage,
	}

	encoded, err := json.Marshal(shaped)
	if err != nil {
		return nil, fmt.Errorf("encode SREF recipe: %w", err)
	}

	members := map[string]json.RawMessage{}
	if err := json.Unmarshal(encoded, &members); err != nil {
		return nil, fmt.Errorf("encode SREF recipe: %w", err)
	}

	for name, value := range document.Extensions {
		if name == ScopedExtensionsMember {
			continue
		}

		if name != StandardExtensionsMember && !isExtensionName(name) {
			return nil, fmt.Errorf("%w: extension %q cannot travel in SREF", ErrInvalidDocument, name)
		}

		carried, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode extension %q: %w", name, err)
		}

		members[name] = carried
	}
	// Extensions that belonged to an object inside the recipe go back onto
	// that object rather than being left at the top level.
	if scoped, held := document.Extensions[ScopedExtensionsMember].(map[string]any); held {
		if err := restoreScopedExtensions(members, scoped); err != nil {
			return nil, err
		}
	}

	if err := restoreScopedMembers(members, document.SREFOpaque, true); err != nil {
		return nil, err
	}

	return members, nil
}

// decodeStandard reads the standard members strictly, so a member SREF does not
// define is reported rather than silently dropped.
func decodeStandard(body []byte) (recipe.Document, error) {
	var root map[string]any
	if err := decodeWithNumbers(body, &root); err != nil {
		return recipe.Document{}, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}

	if member := unknownWireMember(root, reflect.TypeFor[wireDocument]()); member != "" {
		return recipe.Document{}, fmt.Errorf("%w: %w", ErrInvalidDocument, shapeFailure("member-not-defined-by-version", "member %q is not defined by this version", member))
	}

	var shaped wireDocument

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&shaped); err != nil {
		return recipe.Document{}, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return recipe.Document{}, fmt.Errorf("%w: trailing content", ErrInvalidDocument)
	}

	if err := requireStepText(shaped.InstructionSections); err != nil {
		return recipe.Document{}, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}

	document := recipe.Document{
		SchemaVersion:        recipe.CurrentDocumentSchemaVersion,
		ID:                   recipe.RecipeID(shaped.ID),
		Title:                shaped.Title,
		Description:          shaped.Description,
		Language:             shaped.Language,
		SourceLocale:         shaped.SourceLocale,
		Authors:              cloneAuthors(shaped.Authors),
		Source:               shaped.Source,
		Yields:               shaped.Yields,
		Nutrition:            shaped.Nutrition,
		DietaryClaims:        shaped.DietaryClaims,
		AllergenDeclarations: shaped.AllergenDeclarations,
		Times:                fromWireTimes(shaped.Times),
		Categories:           shaped.Categories,
		Courses:              shaped.Courses,
		Keywords:             shaped.Keywords,
		Cuisines:             shaped.Cuisines,
		CookingMethods:       shaped.CookingMethods,
		Published:            shaped.Published,
		Modified:             shaped.Modified,
		Notes:                shaped.Notes,
		Variants:             shaped.Variants,
		Equipment:            shaped.Equipment,
		Translations:         shaped.Translations,
		IngredientSections:   shaped.IngredientSections,
		InstructionSections:  fromWireInstructionSections(shaped.InstructionSections),
		Assets:               shaped.Assets,
		ImageRefs:            append([]string(nil), shaped.ImageRefs...),
		PrimaryImage:         shaped.PrimaryImage,
	}

	if shaped.Yield != nil && len(document.Yields) == 0 {
		document.Yields = []recipe.Yield{*shaped.Yield}
	}

	return document, nil
}

func cloneAuthors(values []recipe.Author) []recipe.Author {
	if values == nil {
		return nil
	}

	return append([]recipe.Author(nil), values...)
}

func toWireInstructionSections(sections []recipe.InstructionSection) []wireInstructionSection {
	if sections == nil {
		return nil
	}

	result := make([]wireInstructionSection, len(sections))
	for sectionIndex, section := range sections {
		result[sectionIndex] = wireInstructionSection{ID: section.ID, Title: section.Title, Steps: make([]wireStep, len(section.Steps))}
		for stepIndex, step := range section.Steps {
			text := step.Text
			if strings.TrimSpace(text) == "" {
				text = step.Title
			}

			result[sectionIndex].Steps[stepIndex] = wireStep{
				ID: step.ID, Title: step.Title, Text: text,
				IngredientUses: cloneIngredientUses(step.IngredientUses),
				ImageRefs:      append([]string(nil), step.ImageRefs...),
				TimingRefs:     append([]string(nil), step.TimingRefs...),
				Temperatures:   append([]recipe.Temperature(nil), step.Temperatures...),
			}
		}
	}

	return result
}

func fromWireInstructionSections(sections []wireInstructionSection) []recipe.InstructionSection {
	if sections == nil {
		return nil
	}

	result := make([]recipe.InstructionSection, len(sections))
	for sectionIndex, section := range sections {
		result[sectionIndex] = recipe.InstructionSection{ID: section.ID, Title: section.Title, Steps: make([]recipe.Step, len(section.Steps))}
		for stepIndex, step := range section.Steps {
			result[sectionIndex].Steps[stepIndex] = recipe.Step{
				ID: step.ID, Title: step.Title, Text: step.Text,
				IngredientUses: cloneIngredientUses(step.IngredientUses),
				ImageRefs:      append([]string(nil), step.ImageRefs...),
				TimingRefs:     append([]string(nil), step.TimingRefs...),
				Temperatures:   append([]recipe.Temperature(nil), step.Temperatures...),
			}
		}
	}

	return result
}

func cloneIngredientUses(values []recipe.IngredientUse) []recipe.IngredientUse {
	if values == nil {
		return nil
	}

	result := make([]recipe.IngredientUse, len(values))
	for index, value := range values {
		result[index] = recipe.IngredientUse{IngredientRef: value.IngredientRef, Quantity: cloneQuantity(value.Quantity)}
	}

	return result
}

func cloneQuantity(value *recipe.Quantity) *recipe.Quantity {
	if value == nil {
		return nil
	}

	cloned := *value
	if value.Amount != nil {
		amount := *value.Amount
		cloned.Amount = &amount
	}

	cloned.Terms = append([]recipe.SimpleQuantity(nil), value.Terms...)

	cloned.Options = append([]recipe.AlternativeOption(nil), value.Options...)
	for index := range cloned.Options {
		if value.Options[index].Amount != nil {
			amount := *value.Options[index].Amount
			cloned.Options[index].Amount = &amount
		}

		cloned.Options[index].Terms = append([]recipe.SimpleQuantity(nil), value.Options[index].Terms...)
	}

	return &cloned
}

// requireStepText enforces at the exchange boundary what SREF requires and
// Savorum's own model does not.
//
// Section 11 requires step `text`. Savorum permits a title-only step
// internally, and its writer copies the title into `text`, but an incoming
// document without step text is invalid SREF.
func requireStepText(sections []wireInstructionSection) error {
	for sectionIndex, section := range sections {
		for stepIndex, step := range section.Steps {
			if strings.TrimSpace(step.Text) == "" {
				return shapeFailure("step-text-required",
					"instruction_sections[%d].steps[%d] has no text, which SREF requires",
					sectionIndex, stepIndex,
				)
			}
		}
	}

	return nil
}
