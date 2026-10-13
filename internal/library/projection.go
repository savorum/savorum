package library

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
)

// ProjectionVersion identifies the presentation policy the stored projection
// was built with. Raise it whenever that policy changes: stores rebuild the
// cached rows from their documents, without creating revisions.
const ProjectionVersion = 6

// Projection contains only derived values used by ordinary library reads.
// The recipe document remains canonical.
type Projection struct {
	Title       string
	Description string
	AuthorsJSON string
	// ActiveTime and TotalTime apply Savorum's presentation policy to the
	// authored coarse clock and any named phases.
	ActiveTime     string
	TotalTime      string
	PrimaryAssetID string
	SearchText     string
	// Language is the document's primary language. TranslationsJSON holds the
	// declarations that name a recipe by ID, the only ones a library can
	// resolve.
	Language         string
	TranslationsJSON string
}

// TranslationLink is one projected declaration naming a portable recipe.
type TranslationLink struct {
	Relation recipe.TranslationRelation `json:"relation"`
	Language string                     `json:"language"`
	RecipeID recipe.RecipeID            `json:"recipe_id"`
}

// BuildProjection builds projection.
func BuildProjection(document recipe.Document) (Projection, error) {
	authorNames := recipe.AuthorNames(document.Authors)

	authors, err := json.Marshal(authorNames)
	if err != nil {
		return Projection{}, fmt.Errorf("encode projected author names: %w", err)
	}

	links := []TranslationLink{}

	for _, translation := range document.Translations {
		if translation.RecipeID != "" {
			links = append(links, TranslationLink{Relation: translation.Relation, Language: translation.Language, RecipeID: translation.RecipeID})
		}
	}

	translations, err := json.Marshal(links)
	if err != nil {
		return Projection{}, fmt.Errorf("encode projected translations: %w", err)
	}

	projection := Projection{
		Title:            document.Title,
		Description:      document.Description,
		AuthorsJSON:      string(authors),
		Language:         document.Language,
		TranslationsJSON: string(translations),
	}
	times := recipe.SummarizeTimes(document)
	// The projection holds a length only when there is one fixed length; a
	// range or opaque wait is left empty rather than sorted by an endpoint.
	projection.ActiveTime = projectedDuration(times.Active)
	projection.TotalTime = projectedDuration(times.Elapsed)
	search := []string{document.Title, document.Description}
	search = append(search, authorNames...)
	search = append(search, document.Categories...)
	search = append(search, document.Courses...)
	search = append(search, document.Keywords...)
	search = append(search, document.Cuisines...)
	search = append(search, document.CookingMethods...)
	search = append(search, document.Published, document.Modified)

	search = append(search, document.Notes...)
	if document.Source != nil {
		search = append(search,
			document.Source.Name,
			document.Source.URL,
			document.Source.Application,
			document.Source.SourceID,
			document.Source.Attribution,
		)
	}

	for _, section := range document.IngredientSections {
		search = append(search, section.Title)
		for _, ingredient := range section.Ingredients {
			search = append(search, ingredient.Name, ingredient.Note, ingredient.SourceText)
			for _, branch := range ingredient.Alternatives {
				search = append(search, branch.Name, branch.Note, branch.SourceText)
			}
		}
	}

	for _, section := range document.InstructionSections {
		search = append(search, section.Title)
		for _, step := range section.Steps {
			search = append(search, step.Title, step.Text)
		}
	}

	for _, asset := range document.Assets {
		search = append(search, asset.Alt, asset.Caption)
		if asset.ID == document.PrimaryImage {
			projection.PrimaryAssetID = "sha256:" + asset.SHA256
		}
	}

	projection.SearchText = joinSearchText(search)

	return projection, nil
}

func joinSearchText(values []string) string {
	nonempty := values[:0]
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			nonempty = append(nonempty, value)
		}
	}

	return strings.Join(nonempty, " ")
}

// projectedDuration returns the ISO 8601 value of a duration expression that
// states one exact length, and nothing otherwise.
func projectedDuration(expression *recipe.DurationExpression) string {
	if expression == nil || expression.IsOpaque() || expression.IsRange() {
		return ""
	}

	return expression.Value
}
