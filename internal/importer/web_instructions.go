package importer

import (
	"fmt"
	"slices"
	"strings"

	"github.com/savorum/savorum/internal/importer/webrecipe"
	"github.com/savorum/savorum/internal/recipe"
)

// mapInstructions turns the extracted method into Savorum's instruction
// sections. Two shapes have to be reconciled here.
//
// Schema.org sections nest and Savorum's do not, so a nested section's title
// is joined to its parents' rather than being dropped: "Filling · Assembly"
// keeps both facts in the one title Savorum can hold.
//
// A HowToSection name maps to the section title and a HowToStep name maps to
// the step title. Keeping those levels distinct preserves the page's authored
// hierarchy without manufacturing one-step sections.
func mapInstructions(sections []webrecipe.InstructionSection, ingredients mappedIngredients) ([]recipe.InstructionSection, []Diagnostic) {
	builder := &instructionBuilder{ingredients: ingredients}
	builder.walk(sections, nil)
	builder.finish()

	if len(builder.sections) == 1 && builder.sections[0].Title == "" {
		builder.sections[0].ID = "method"
	}

	if len(builder.unmatchedSupply) > 0 {
		builder.diagnostics = append(builder.diagnostics, Diagnostic{
			Code: "step-ingredients-unmatched",
			Message: "This page named ingredients for particular steps that are not in its ingredient list: " +
				strings.Join(builder.unmatchedSupply, ", ") + ".",
		})
	}

	return builder.sections, builder.diagnostics
}

type instructionBuilder struct {
	ingredients     mappedIngredients
	sections        []recipe.InstructionSection
	current         *recipe.InstructionSection
	currentTitle    string
	steps           int
	diagnostics     []Diagnostic
	unmatchedSupply []string
	seenUnmatched   map[string]bool
}

func (b *instructionBuilder) walk(sections []webrecipe.InstructionSection, ancestors []string) {
	for _, section := range sections {
		title := joinTitles(ancestors, section.Title)
		for _, step := range section.Steps {
			b.add(step, title)
		}

		b.walk(section.Sections, appendTitle(ancestors, section.Title))
	}
}

// add places one step, opening a new section whenever the section title changes.
func (b *instructionBuilder) add(step webrecipe.Step, title string) {
	if b.current == nil || title != b.currentTitle {
		b.finish()
		b.current = &recipe.InstructionSection{ID: fmt.Sprintf("m%d", len(b.sections)+1), Title: title}
		b.currentTitle = title
	}

	b.steps++
	text := stepText(step)
	mapped := recipe.Step{ID: fmt.Sprintf("p%d", b.steps), Title: stepTitle(step.Name, text), Text: text}
	// Step photographs are carried as their addresses here and rewritten to
	// stored asset identifiers once the recipe is assembled, which is the only
	// point at which the identifiers are known.
	mapped.ImageRefs = append(mapped.ImageRefs, step.Images...)
	// The page's own statement of which ingredients a step uses is authored
	// fact; any future inference must leave such a step alone.
	for _, supply := range step.Supply {
		id, found := b.ingredients.lookup(supply)
		if !found {
			b.noteUnmatched(supply)
			continue
		}

		if !containsIngredientUse(mapped.IngredientUses, id) {
			mapped.IngredientUses = append(mapped.IngredientUses, recipe.IngredientUse{IngredientRef: id})
		}
	}

	if mapped.Title == "" && mapped.Text == "" {
		return
	}

	b.current.Steps = append(b.current.Steps, mapped)
}

func containsIngredientUse(uses []recipe.IngredientUse, ingredientID string) bool {
	for _, use := range uses {
		if use.IngredientRef == ingredientID {
			return true
		}
	}

	return false
}

// stepTitle keeps a step's name only when it says something the step's own
// text does not; recipe plugins often write the whole instruction into both.
// A name that merely opens the sentence is still a label and is kept.
func stepTitle(name, text string) string {
	title := strings.TrimSpace(name)
	if title == "" {
		return ""
	}

	if trimStepWording(title) == trimStepWording(text) {
		return ""
	}

	return title
}

func trimStepWording(value string) string {
	return strings.TrimRight(strings.ToLower(strings.TrimSpace(value)), " .:;,-–—")
}

// stepText assembles what the step instructs: its text, followed by any
// directions and tips from itemListElement.
func stepText(step webrecipe.Step) string {
	parts := make([]string, 0, 1+len(step.Directions)+len(step.Tips))
	if strings.TrimSpace(step.Text) != "" {
		parts = append(parts, strings.TrimSpace(step.Text))
	}

	for _, direction := range step.Directions {
		if direction = strings.TrimSpace(direction); direction != "" && !slices.Contains(parts, direction) {
			parts = append(parts, direction)
		}
	}

	for _, tip := range step.Tips {
		if tip = strings.TrimSpace(tip); tip != "" && !slices.Contains(parts, tip) {
			parts = append(parts, tip)
		}
	}

	return strings.Join(parts, " ")
}

func (b *instructionBuilder) noteUnmatched(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}

	if b.seenUnmatched == nil {
		b.seenUnmatched = map[string]bool{}
	}

	if b.seenUnmatched[value] || len(b.unmatchedSupply) >= 10 {
		return
	}

	b.seenUnmatched[value] = true
	b.unmatchedSupply = append(b.unmatchedSupply, value)
}

func (b *instructionBuilder) finish() {
	if b.current == nil {
		return
	}

	if len(b.current.Steps) > 0 {
		b.sections = append(b.sections, *b.current)
	}

	b.current = nil
	b.currentTitle = ""
}

func joinTitles(ancestors []string, title string) string {
	parts := appendTitle(ancestors, title)
	return strings.Join(parts, " · ")
}

func appendTitle(ancestors []string, title string) []string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ancestors
	}

	result := make([]string, 0, len(ancestors)+1)
	result = append(result, ancestors...)

	return append(result, title)
}
