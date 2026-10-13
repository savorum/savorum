package testfixture

import "github.com/savorum/savorum/internal/recipe"

// Rational parses a rational used in tests.
func Rational(value string) *recipe.Rational {
	converted := recipe.Rational(value)
	return &converted
}

// Document returns a representative valid recipe for tests.
func Document() recipe.Document {
	return recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            "savory-hand-pies",
		Title:         "Australian vegetable hand pies",
		Description:   "Flaky hand pies with a pumpkin and chickpea filling.",
		Language:      "en-AU",
		SourceLocale:  "en-AU",
		Authors:       []recipe.Author{{Name: "Test Kitchen", Kind: recipe.AuthorOrganization, URL: "https://recipes.example/test-kitchen"}},
		Source: &recipe.Source{
			Name:        "Kitchen notebook",
			Application: "paper",
			RetrievedAt: "2026-08-26T18:30:00Z",
		},
		Yields: []recipe.Yield{{
			Text: "8 hand pies",
			Quantity: &recipe.Quantity{
				Kind:   recipe.QuantitySimple,
				Amount: &recipe.Amount{Value: Rational("8")},
				Unit:   "count.item",
			},
			Label: "hand pies",
		}},
		Times: &recipe.Times{
			Prep:       &recipe.DurationExpression{Value: "PT45M"},
			Cook:       &recipe.DurationExpression{Value: "PT30M"},
			Additional: &recipe.DurationExpression{Value: "PT1H"},
			Total:      &recipe.DurationExpression{Value: "PT2H15M"},
			Assertions: []recipe.TimingAssertion{
				{ID: "timing-pastry", Kind: recipe.TimingOther, Label: "Hands-on", Attention: recipe.AttentionActive, Duration: recipe.DurationExpression{Value: "PT15M"}, Provenance: recipe.TimingSupplemental},
				{ID: "timing-chill", Kind: recipe.TimingChill, Duration: recipe.DurationExpression{Value: "PT1H"}, Provenance: recipe.TimingSupplemental},
				{ID: "timing-bake", Kind: recipe.TimingCook, Label: "Bake", Duration: recipe.DurationExpression{Value: "PT30M"}, Provenance: recipe.TimingSupplemental},
			},
		},
		Keywords: []string{"baking", "freezer-friendly"},
		Cuisines: []string{"Australian"},
		Notes:    []string{"Cool the filling completely before assembly."},
		IngredientSections: []recipe.IngredientSection{
			{
				ID:    "pastry",
				Title: "Pastry",
				Ingredients: []recipe.Ingredient{
					{
						ID:         "flour",
						Name:       "plain flour",
						SourceText: "2 Australian cups plain flour, plus 2 tbsp for dusting",
						Quantity: &recipe.Quantity{
							Kind: recipe.QuantitySum,
							Terms: []recipe.SimpleQuantity{
								{Kind: recipe.QuantitySimple, Amount: recipe.Amount{Value: Rational("2")}, Unit: "volume.cup.au"},
								{Kind: recipe.QuantitySimple, Amount: recipe.Amount{Value: Rational("2")}, Unit: "volume.tablespoon.au"},
							},
						},
						Note: "reserve the tablespoons for dusting",
						Normalization: &recipe.Normalization{
							Method: recipe.NormalizationAuthored,
						},
					},
					{
						ID:   "butter",
						Name: "unsalted butter",
						Quantity: &recipe.Quantity{
							Kind: recipe.QuantityAlternatives,
							Options: []recipe.AlternativeOption{
								{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: Rational("225")}, Unit: "mass.gram"},
								{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: Rational("8")}, Unit: "mass.ounce.avoirdupois"},
							},
						},
						Temperature: &recipe.Temperature{
							Amount:     recipe.Amount{Value: Rational("4")},
							Unit:       "temperature.celsius",
							Note:       "refrigerator-cold",
							SourceText: "cold butter",
						},
					},
				},
			},
			{
				ID:    "filling",
				Title: "Filling",
				Ingredients: []recipe.Ingredient{
					{
						ID:   "pumpkin",
						Name: "pumpkin",
						Quantity: &recipe.Quantity{
							Kind:   recipe.QuantitySimple,
							Amount: &recipe.Amount{Min: Rational("450"), Max: Rational("500"), Approximate: true},
							Unit:   "mass.gram",
						},
						Note: "peeled and cut into 2 cm cubes",
					},
					{
						ID:   "chickpeas",
						Name: "chickpeas",
						Quantity: &recipe.Quantity{
							Kind:   recipe.QuantitySimple,
							Amount: &recipe.Amount{Value: Rational("1")},
							Unit:   "container.tin",
						},
						PackageSize: &recipe.SimpleQuantity{
							Kind:   recipe.QuantitySimple,
							Amount: recipe.Amount{Value: Rational("400")},
							Unit:   "mass.gram",
						},
						Note: "drained and rinsed",
					},
					{
						ID:         "salt",
						Name:       "fine salt",
						Quantity:   &recipe.Quantity{Kind: recipe.QuantityOpaque, Text: "to taste"},
						SourceText: "fine salt, to taste",
						Normalization: &recipe.Normalization{
							Method:   recipe.NormalizationUnresolved,
							Warnings: []string{"The source does not state a measurable amount."},
						},
					},
				},
			},
		},
		InstructionSections: []recipe.InstructionSection{
			{
				ID:    "prepare",
				Title: "Prepare",
				Steps: []recipe.Step{
					{ID: "make-pastry", Text: "Rub the butter into the flour, add cold water, and form a dough.", IngredientUses: []recipe.IngredientUse{{IngredientRef: "flour"}, {IngredientRef: "butter"}}, TimingRefs: []string{"timing-pastry"}},
					{ID: "chill", Text: "Wrap and chill the dough.", TimingRefs: []string{"timing-chill"}},
				},
			},
			{
				ID:    "bake",
				Title: "Fill and bake",
				Steps: []recipe.Step{
					{
						ID:             "assemble",
						Text:           "Combine the filling, divide it among pastry rounds, and seal.",
						IngredientUses: []recipe.IngredientUse{{IngredientRef: "pumpkin"}, {IngredientRef: "chickpeas"}, {IngredientRef: "salt"}},
					},
					{
						ID:         "bake-pies",
						TimingRefs: []string{"timing-bake"},
						Text:       "Bake until deeply golden.",
						ImageRefs:  []string{"finished-pies"},
						Temperatures: []recipe.Temperature{
							{Amount: recipe.Amount{Value: Rational("200")}, Unit: "temperature.celsius", SourceText: "200°C"},
						},
					},
				},
			},
		},
		Assets: []recipe.Asset{
			{
				ID:        "finished-pies",
				Path:      "assets/finished-pies.jpg",
				MediaType: "image/jpeg",
				SHA256:    "5b9f52143a19b2af6759ec7e65a0e34457e92147edbcbcebb1ee0230805dd582",
				Size:      245812,
				Role:      recipe.AssetRoleImage,
				Alt:       "Four baked hand pies on a cooling rack",
			},
		},
		ImageRefs:    []string{"finished-pies"},
		PrimaryImage: "finished-pies",
	}
}
