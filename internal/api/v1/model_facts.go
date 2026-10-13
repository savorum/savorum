package v1

import "github.com/savorum/savorum/internal/recipe"

// NutritionStatement is authored nutrition with the basis the source stated.
// Savorum does not calculate nutrition.
type NutritionStatement struct {
	Basis     NutritionBasis `json:"basis"`
	Nutrients []Nutrient     `json:"nutrients" minItems:"1" maxItems:"200"`
	Note      string         `json:"note,omitempty" maxLength:"1000000" doc:"Authored qualification of the whole statement"`
}

// NutritionBasis says what a statement's values describe.
type NutritionBasis struct {
	Kind     string    `json:"kind" enum:"recipe,serving,quantity,unspecified" doc:"recipe: the whole recipe as written; serving: one serving; quantity: a stated amount such as 100 g; unspecified: the source did not say"`
	Text     string    `json:"text,omitempty" maxLength:"1000" doc:"Authored basis wording such as 1 slice"`
	Quantity *Quantity `json:"quantity,omitempty" doc:"Serving size or reference amount; required for a quantity basis and absent for an unspecified one"`
}

// Nutrient is one authored value: an amount, or wording such as trace.
type Nutrient struct {
	Nutrient   string  `json:"nutrient" enum:"energy,fat,saturated_fat,trans_fat,unsaturated_fat,cholesterol,carbohydrate,sugars,fiber,protein,sodium,salt,other"`
	Label      string  `json:"label,omitempty" maxLength:"1000" doc:"The source's own name; required for other"`
	Amount     *Amount `json:"amount,omitempty" doc:"An authored zero is a value; an unmentioned nutrient is absent"`
	Unit       string  `json:"unit,omitempty" enum:"kcal,kJ,g,mg,mcg" doc:"Absent when the source stated no unit"`
	SourceText string  `json:"source_text,omitempty" maxLength:"1000"`
	Text       string  `json:"text,omitempty" maxLength:"1000" doc:"Wording that is not a precise value; excludes amount and unit"`
}

// DietaryClaim is a source's statement that the recipe suits, or does not
// suit, a diet. Savorum never derives one from ingredients.
type DietaryClaim struct {
	Text        string `json:"text" minLength:"1" maxLength:"1000000" doc:"The complete authored statement"`
	Suitability string `json:"suitability" enum:"suitable,unsuitable"`
	Diet        string `json:"diet,omitempty" enum:"diabetic,gluten_free,halal,hindu,kosher,low_calorie,low_fat,low_lactose,low_salt,vegan,vegetarian" doc:"Normalized subject; absent when the diet has none"`
	Condition   string `json:"condition,omitempty" maxLength:"1000000" doc:"Authored limit on the claim; a conditional claim is never shown or filtered as unconditional"`
	VariantRef  string `json:"variant_ref,omitempty" pattern:"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$" doc:"The variant the claim describes, which it does not extend to the recipe as written"`
}

// AllergenDeclaration is a source's statement about one substance.
type AllergenDeclaration struct {
	Text       string `json:"text" minLength:"1" maxLength:"1000000" doc:"The complete authored statement"`
	Substance  string `json:"substance" minLength:"1" maxLength:"1000" doc:"The substance as the source names it"`
	Presence   string `json:"presence" enum:"contains,may_contain,free_from" doc:"contains: present; may_contain: precautionary or cross-contact warning; free_from: explicit absence claim"`
	Condition  string `json:"condition,omitempty" maxLength:"1000000"`
	VariantRef string `json:"variant_ref,omitempty" pattern:"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$"`
}

// RecipeDependency identifies the recipe that makes an ingredient. It never
// carries that recipe's content.
type RecipeDependency struct {
	Title    string `json:"title,omitempty" maxLength:"1000" doc:"The other recipe's title as authored"`
	RecipeID string `json:"recipe_id,omitempty" pattern:"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$" doc:"The other recipe's portable document ID, not a library entry ID"`
	URL      string `json:"url,omitempty" format:"uri" maxLength:"10000" doc:"Where the other recipe is published"`
}

// AssetLicense is the license a source applies to one asset.
type AssetLicense struct {
	Name string `json:"name,omitempty" maxLength:"1000"`
	URL  string `json:"url,omitempty" format:"uri" maxLength:"10000"`
}

func toDomainYields(values []Yield) []recipe.Yield {
	if values == nil {
		return nil
	}

	result := make([]recipe.Yield, len(values))
	for index, value := range values {
		result[index] = recipe.Yield{Text: value.Text, Quantity: toDomainQuantity(value.Quantity), Label: value.Label}
	}

	return result
}

func fromDomainYields(values []recipe.Yield) []Yield {
	if values == nil {
		return nil
	}

	result := make([]Yield, len(values))
	for index, value := range values {
		result[index] = Yield{Text: value.Text, Quantity: fromDomainQuantity(value.Quantity), Label: value.Label}
	}

	return result
}

func toDomainNutrition(values []NutritionStatement) []recipe.NutritionStatement {
	if values == nil {
		return nil
	}

	result := make([]recipe.NutritionStatement, len(values))
	for index, value := range values {
		nutrients := make([]recipe.Nutrient, len(value.Nutrients))
		for nutrientIndex, nutrient := range value.Nutrients {
			nutrients[nutrientIndex] = recipe.Nutrient{
				Nutrient: recipe.NutrientKind(nutrient.Nutrient), Label: nutrient.Label, Amount: toDomainAmount(nutrient.Amount),
				Unit: recipe.NutrientUnit(nutrient.Unit), SourceText: nutrient.SourceText, Text: nutrient.Text,
			}
		}

		result[index] = recipe.NutritionStatement{
			Basis: recipe.NutritionBasis{
				Kind: recipe.NutritionBasisKind(value.Basis.Kind), Text: value.Basis.Text, Quantity: toDomainQuantity(value.Basis.Quantity),
			},
			Nutrients: nutrients, Note: value.Note,
		}
	}

	return result
}

func fromDomainNutrition(values []recipe.NutritionStatement) []NutritionStatement {
	if values == nil {
		return nil
	}

	result := make([]NutritionStatement, len(values))
	for index, value := range values {
		nutrients := make([]Nutrient, len(value.Nutrients))
		for nutrientIndex, nutrient := range value.Nutrients {
			nutrients[nutrientIndex] = Nutrient{
				Nutrient: string(nutrient.Nutrient), Label: nutrient.Label, Amount: fromDomainAmount(nutrient.Amount),
				Unit: string(nutrient.Unit), SourceText: nutrient.SourceText, Text: nutrient.Text,
			}
		}

		result[index] = NutritionStatement{
			Basis: NutritionBasis{
				Kind: string(value.Basis.Kind), Text: value.Basis.Text, Quantity: fromDomainQuantity(value.Basis.Quantity),
			},
			Nutrients: nutrients, Note: value.Note,
		}
	}

	return result
}

func toDomainDietaryClaims(values []DietaryClaim) []recipe.DietaryClaim {
	if values == nil {
		return nil
	}

	result := make([]recipe.DietaryClaim, len(values))
	for index, value := range values {
		result[index] = recipe.DietaryClaim{
			Text: value.Text, Suitability: recipe.Suitability(value.Suitability), Diet: recipe.Diet(value.Diet),
			Condition: value.Condition, VariantRef: value.VariantRef,
		}
	}

	return result
}

func fromDomainDietaryClaims(values []recipe.DietaryClaim) []DietaryClaim {
	if values == nil {
		return nil
	}

	result := make([]DietaryClaim, len(values))
	for index, value := range values {
		result[index] = DietaryClaim{
			Text: value.Text, Suitability: string(value.Suitability), Diet: string(value.Diet),
			Condition: value.Condition, VariantRef: value.VariantRef,
		}
	}

	return result
}

func toDomainAllergenDeclarations(values []AllergenDeclaration) []recipe.AllergenDeclaration {
	if values == nil {
		return nil
	}

	result := make([]recipe.AllergenDeclaration, len(values))
	for index, value := range values {
		result[index] = recipe.AllergenDeclaration{
			Text: value.Text, Substance: value.Substance, Presence: recipe.AllergenPresence(value.Presence),
			Condition: value.Condition, VariantRef: value.VariantRef,
		}
	}

	return result
}

func fromDomainAllergenDeclarations(values []recipe.AllergenDeclaration) []AllergenDeclaration {
	if values == nil {
		return nil
	}

	result := make([]AllergenDeclaration, len(values))
	for index, value := range values {
		result[index] = AllergenDeclaration{
			Text: value.Text, Substance: value.Substance, Presence: string(value.Presence),
			Condition: value.Condition, VariantRef: value.VariantRef,
		}
	}

	return result
}

func toDomainRecipeReference(value *RecipeDependency) *recipe.RecipeReference {
	if value == nil {
		return nil
	}

	return &recipe.RecipeReference{Title: value.Title, RecipeID: recipe.RecipeID(value.RecipeID), URL: value.URL}
}

func fromDomainRecipeReference(value *recipe.RecipeReference) *RecipeDependency {
	if value == nil {
		return nil
	}

	return &RecipeDependency{Title: value.Title, RecipeID: string(value.RecipeID), URL: value.URL}
}

func toDomainAssetLicense(value *AssetLicense) *recipe.AssetLicense {
	if value == nil {
		return nil
	}

	return &recipe.AssetLicense{Name: value.Name, URL: value.URL}
}

func fromDomainAssetLicense(value *recipe.AssetLicense) *AssetLicense {
	if value == nil {
		return nil
	}

	return &AssetLicense{Name: value.Name, URL: value.URL}
}
