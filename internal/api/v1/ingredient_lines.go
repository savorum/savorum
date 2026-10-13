package v1

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/units"
	"golang.org/x/text/language"
)

type parseIngredientLineInput struct {
	Body struct {
		Text         string `json:"text" minLength:"1" maxLength:"10000" doc:"One ingredient line as a person writes it, such as 2 cups flour, sifted or 小麦粉 200g"`
		Language     string `json:"language,omitempty" maxLength:"35" doc:"BCP 47 tag of the language the line is written in. Its primary language selects unit words and reading order; its region does not establish a measuring convention"`
		SourceLocale string `json:"source_locale,omitempty" maxLength:"35" doc:"BCP 47 tag of the locale the recipe declares. It takes the place of language when the two are sent"`
		Convention   string `json:"convention,omitempty" enum:"metric,us,au,imperial" doc:"An explicit author choice for every applicable unqualified measure of this submitted ingredient line. It outranks source-locale lookup and preserves explicit unit definitions. Omit it when unspecified"`
	}
}

type parseIngredientLineOutput struct {
	Body struct {
		Ingredient Ingredient `json:"ingredient" doc:"The line read as an ingredient. Its normalization method is extracted when an amount was read and unresolved when the line named a measure that could not be identified safely. A line with no amount is returned as a name"`
	}
}

var unitCatalog = units.DefaultCatalog()

func registerIngredientLines(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:     "preview-ingredient-line",
		Method:          http.MethodPost,
		Path:            "/api/v1/ingredient-lines/parse",
		Summary:         "Read an ingredient line",
		Description:     "Reads one typed ingredient line into an amount, unit, name, and note using the unit words of the line's language. Nothing is stored. The result is the reading an import would make, so a line typed in the editor and the same line imported are read alike.",
		Tags:            []string{"Recipes"},
		MaxBodyBytes:    64 * 1024,
		BodyReadTimeout: 5 * time.Second,
	}, func(_ context.Context, input *parseIngredientLineInput) (*parseIngredientLineOutput, error) {
		locale := strings.TrimSpace(input.Body.SourceLocale)
		if locale == "" {
			locale = "en"

			if tag, err := language.Parse(strings.TrimSpace(input.Body.Language)); err == nil {
				if base, _ := tag.Base(); base.String() != "und" {
					locale = base.String()
				}
			}
		}

		parsed := ingredientparser.ParseDetailed(input.Body.Text, "line", locale).Ingredient

		if input.Body.Convention != "" {
			if profile, found := unitCatalog.ConventionProfileFor(units.Convention(input.Body.Convention)); found {
				chosen := ingredientparser.ParseWithContext(input.Body.Text, "line", locale,
					ingredientparser.ResolutionContext{Convention: &profile, DeclaredLocale: input.Body.SourceLocale}).Ingredient
				decision := chosen.Normalization

				chosen.Normalization = parsed.Normalization
				if !reflect.DeepEqual(chosen, parsed) {
					chosen.Normalization = decision
					parsed = chosen
				}
			}
		}

		output := &parseIngredientLineOutput{}
		output.Body.Ingredient = fromDomainIngredient(parsed)

		return output, nil
	})
}
