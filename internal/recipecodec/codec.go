// Package recipecodec encodes versioned recipe snapshots.
package recipecodec

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	"github.com/savorum/savorum/internal/recipe"
	codecV1 "github.com/savorum/savorum/internal/recipecodec/v1"
)

// CurrentVersion is the recipe document schema written by this build.
const CurrentVersion = recipe.CurrentDocumentSchemaVersion

// Recipe codec errors distinguish unknown schemas from discriminator mismatches.
var (
	ErrUnsupportedSchemaVersion = errors.New("unsupported recipe document schema version")
	ErrSchemaVersionMismatch    = errors.New("recipe document schema version does not match its storage discriminator")
)

// UnsupportedSchemaVersionError records the unknown recipe schema version.
type UnsupportedSchemaVersionError struct {
	Version          int
	SupportedThrough int
}

func (e *UnsupportedSchemaVersionError) Error() string {
	return fmt.Sprintf("recipe requires document schema version %d; this Savorum version supports through version %d", e.Version, e.SupportedThrough)
}

func (*UnsupportedSchemaVersionError) Unwrap() error {
	return ErrUnsupportedSchemaVersion
}

// Encode returns canonical document bytes and their schema version.
func Encode(document recipe.Document) ([]byte, int, error) {
	if document.SchemaVersion != CurrentVersion {
		return nil, 0, &UnsupportedSchemaVersionError{Version: document.SchemaVersion, SupportedThrough: CurrentVersion}
	}

	encoded, err := json.Marshal(fromDomain(document))
	if err != nil {
		return nil, 0, fmt.Errorf("encode schema v%d recipe document: %w", CurrentVersion, err)
	}

	return encoded, CurrentVersion, nil
}

// Decode reads a schema-versioned document into the current domain model.
func Decode(data []byte, schemaVersion int) (recipe.Document, error) {
	if schemaVersion != CurrentVersion {
		return recipe.Document{}, &UnsupportedSchemaVersionError{Version: schemaVersion, SupportedThrough: CurrentVersion}
	}

	stored, err := codecV1.Decode(data)
	if err != nil {
		return recipe.Document{}, err
	}

	if stored.SchemaVersion != schemaVersion {
		return recipe.Document{}, fmt.Errorf("%w: JSON says %d, row says %d", ErrSchemaVersionMismatch, stored.SchemaVersion, schemaVersion)
	}

	return toDomain(stored), nil
}

func toDomain(document codecV1.Document) recipe.Document {
	return recipe.Document{
		SchemaVersion:        CurrentVersion,
		ID:                   recipe.RecipeID(document.ID),
		Title:                document.Title,
		Description:          document.Description,
		Language:             document.Language,
		SourceLocale:         document.SourceLocale,
		Authors:              toDomainAuthors(document.Authors),
		Source:               toDomainSource(document.Source),
		Yields:               toDomainYields(document.Yields, document.LegacyYield),
		Nutrition:            toDomainNutrition(document.Nutrition),
		DietaryClaims:        toDomainDietaryClaims(document.DietaryClaims),
		AllergenDeclarations: toDomainAllergenDeclarations(document.AllergenDeclarations),
		Times:                toDomainTimes(document.Times),
		Categories:           cloneStrings(document.Categories),
		Courses:              cloneStrings(document.Courses),
		Keywords:             cloneStrings(document.Keywords),
		Cuisines:             cloneStrings(document.Cuisines),
		CookingMethods:       cloneStrings(document.CookingMethods),
		Published:            document.Published,
		Modified:             document.Modified,
		Notes:                cloneStrings(document.Notes),
		Variants:             toDomainVariants(document.Variants),
		Equipment:            toDomainEquipment(document.Equipment),
		Translations:         toDomainTranslations(document.Translations),
		IngredientSections:   toDomainIngredientSections(document.IngredientSections),
		InstructionSections:  toDomainInstructionSections(document.InstructionSections),
		Assets:               toDomainAssets(document.Assets),
		ImageRefs:            cloneStrings(document.ImageRefs),
		PrimaryImage:         document.PrimaryImage,
		Extensions:           cloneExtensions(document.Extensions),
		SREFOpaque:           cloneExtensions(document.SREFOpaque),
	}
}

func fromDomain(document recipe.Document) codecV1.Document {
	return codecV1.Document{
		SchemaVersion:        CurrentVersion,
		ID:                   string(document.ID),
		Title:                document.Title,
		Description:          document.Description,
		Language:             document.Language,
		SourceLocale:         document.SourceLocale,
		Authors:              fromDomainAuthors(document.Authors),
		Source:               fromDomainSource(document.Source),
		Yields:               fromDomainYields(document.Yields),
		Nutrition:            fromDomainNutrition(document.Nutrition),
		DietaryClaims:        fromDomainDietaryClaims(document.DietaryClaims),
		AllergenDeclarations: fromDomainAllergenDeclarations(document.AllergenDeclarations),
		Times:                fromDomainTimes(document.Times),
		Categories:           cloneStrings(document.Categories),
		Courses:              cloneStrings(document.Courses),
		Keywords:             cloneStrings(document.Keywords),
		Cuisines:             cloneStrings(document.Cuisines),
		CookingMethods:       cloneStrings(document.CookingMethods),
		Published:            document.Published,
		Modified:             document.Modified,
		Notes:                cloneStrings(document.Notes),
		Variants:             fromDomainVariants(document.Variants),
		Equipment:            fromDomainEquipment(document.Equipment),
		Translations:         fromDomainTranslations(document.Translations),
		IngredientSections:   fromDomainIngredientSections(document.IngredientSections),
		InstructionSections:  fromDomainInstructionSections(document.InstructionSections),
		Assets:               fromDomainAssets(document.Assets),
		ImageRefs:            cloneStrings(document.ImageRefs),
		PrimaryImage:         document.PrimaryImage,
		Extensions:           cloneExtensions(document.Extensions),
		SREFOpaque:           cloneExtensions(document.SREFOpaque),
	}
}

func cloneExtensions(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}

	result := make(map[string]any, len(values))
	maps.Copy(result, values)

	return result
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}

	return append([]string(nil), values...)
}

// convertSlice keeps a nil slice nil, so an absent member stays absent.
func convertSlice[From, To any](values []From, convert func(From) To) []To {
	if values == nil {
		return nil
	}

	result := make([]To, len(values))
	for index, value := range values {
		result[index] = convert(value)
	}

	return result
}

func toDomainAuthors(values []codecV1.Author) []recipe.Author {
	return convertSlice(values, func(value codecV1.Author) recipe.Author {
		return recipe.Author{Name: value.Name, Kind: recipe.AuthorKind(value.Kind), KindLabel: value.KindLabel, URL: value.URL}
	})
}

func fromDomainAuthors(values []recipe.Author) []codecV1.Author {
	return convertSlice(values, func(value recipe.Author) codecV1.Author {
		return codecV1.Author{Name: value.Name, Kind: string(value.Kind), KindLabel: value.KindLabel, URL: value.URL}
	})
}

func toDomainSource(value *codecV1.Source) *recipe.Source {
	if value == nil {
		return nil
	}

	return &recipe.Source{Name: value.Name, URL: value.URL, Application: value.Application, SourceID: value.SourceID, RetrievedAt: value.RetrievedAt, Attribution: value.Attribution}
}

func fromDomainSource(value *recipe.Source) *codecV1.Source {
	if value == nil {
		return nil
	}

	return &codecV1.Source{Name: value.Name, URL: value.URL, Application: value.Application, SourceID: value.SourceID, RetrievedAt: value.RetrievedAt, Attribution: value.Attribution}
}

func toDomainYields(values []codecV1.Yield, legacy *codecV1.Yield) []recipe.Yield {
	if len(values) == 0 && legacy != nil {
		values = []codecV1.Yield{*legacy}
	}

	return convertSlice(values, func(value codecV1.Yield) recipe.Yield {
		return recipe.Yield{Text: value.Text, Quantity: toDomainQuantity(value.Quantity), Label: value.Label}
	})
}

func fromDomainYields(values []recipe.Yield) []codecV1.Yield {
	return convertSlice(values, func(value recipe.Yield) codecV1.Yield {
		return codecV1.Yield{Text: value.Text, Quantity: fromDomainQuantity(value.Quantity), Label: value.Label}
	})
}

func toDomainNutrition(values []codecV1.NutritionStatement) []recipe.NutritionStatement {
	return convertSlice(values, func(value codecV1.NutritionStatement) recipe.NutritionStatement {
		return recipe.NutritionStatement{
			Basis: recipe.NutritionBasis{
				Kind: recipe.NutritionBasisKind(value.Basis.Kind), Text: value.Basis.Text,
				Quantity: toDomainQuantity(value.Basis.Quantity),
			},
			Nutrients: convertSlice(value.Nutrients, func(nutrient codecV1.Nutrient) recipe.Nutrient {
				return recipe.Nutrient{
					Nutrient: recipe.NutrientKind(nutrient.Nutrient), Label: nutrient.Label,
					Amount: toDomainAmount(nutrient.Amount), Unit: recipe.NutrientUnit(nutrient.Unit),
					SourceText: nutrient.SourceText, Text: nutrient.Text,
				}
			}),
			Note: value.Note,
		}
	})
}

func fromDomainNutrition(values []recipe.NutritionStatement) []codecV1.NutritionStatement {
	return convertSlice(values, func(value recipe.NutritionStatement) codecV1.NutritionStatement {
		return codecV1.NutritionStatement{
			Basis: codecV1.NutritionBasis{
				Kind: string(value.Basis.Kind), Text: value.Basis.Text,
				Quantity: fromDomainQuantity(value.Basis.Quantity),
			},
			Nutrients: convertSlice(value.Nutrients, func(nutrient recipe.Nutrient) codecV1.Nutrient {
				return codecV1.Nutrient{
					Nutrient: string(nutrient.Nutrient), Label: nutrient.Label,
					Amount: fromDomainAmount(nutrient.Amount), Unit: string(nutrient.Unit),
					SourceText: nutrient.SourceText, Text: nutrient.Text,
				}
			}),
			Note: value.Note,
		}
	})
}

func toDomainDietaryClaims(values []codecV1.DietaryClaim) []recipe.DietaryClaim {
	return convertSlice(values, func(value codecV1.DietaryClaim) recipe.DietaryClaim {
		return recipe.DietaryClaim{
			Text: value.Text, Suitability: recipe.Suitability(value.Suitability), Diet: recipe.Diet(value.Diet),
			Condition: value.Condition, VariantRef: value.VariantRef,
		}
	})
}

func fromDomainDietaryClaims(values []recipe.DietaryClaim) []codecV1.DietaryClaim {
	return convertSlice(values, func(value recipe.DietaryClaim) codecV1.DietaryClaim {
		return codecV1.DietaryClaim{
			Text: value.Text, Suitability: string(value.Suitability), Diet: string(value.Diet),
			Condition: value.Condition, VariantRef: value.VariantRef,
		}
	})
}

func toDomainAllergenDeclarations(values []codecV1.AllergenDeclaration) []recipe.AllergenDeclaration {
	return convertSlice(values, func(value codecV1.AllergenDeclaration) recipe.AllergenDeclaration {
		return recipe.AllergenDeclaration{
			Text: value.Text, Substance: value.Substance, Presence: recipe.AllergenPresence(value.Presence),
			Condition: value.Condition, VariantRef: value.VariantRef,
		}
	})
}

func fromDomainAllergenDeclarations(values []recipe.AllergenDeclaration) []codecV1.AllergenDeclaration {
	return convertSlice(values, func(value recipe.AllergenDeclaration) codecV1.AllergenDeclaration {
		return codecV1.AllergenDeclaration{
			Text: value.Text, Substance: value.Substance, Presence: string(value.Presence),
			Condition: value.Condition, VariantRef: value.VariantRef,
		}
	})
}

func toDomainRecipeReference(value *codecV1.RecipeReference) *recipe.RecipeReference {
	if value == nil {
		return nil
	}

	return &recipe.RecipeReference{Title: value.Title, RecipeID: recipe.RecipeID(value.RecipeID), URL: value.URL}
}

func fromDomainRecipeReference(value *recipe.RecipeReference) *codecV1.RecipeReference {
	if value == nil {
		return nil
	}

	return &codecV1.RecipeReference{Title: value.Title, RecipeID: string(value.RecipeID), URL: value.URL}
}

func toDomainVariants(values []codecV1.Variant) []recipe.Variant {
	return convertSlice(values, func(value codecV1.Variant) recipe.Variant {
		return recipe.Variant{ID: value.ID, Title: value.Title, Text: value.Text}
	})
}

func fromDomainVariants(values []recipe.Variant) []codecV1.Variant {
	return convertSlice(values, func(value recipe.Variant) codecV1.Variant {
		return codecV1.Variant{ID: value.ID, Title: value.Title, Text: value.Text}
	})
}

func toDomainEquipment(values []codecV1.Equipment) []recipe.Equipment {
	return convertSlice(values, func(value codecV1.Equipment) recipe.Equipment {
		return recipe.Equipment{Name: value.Name, Quantity: toDomainQuantity(value.Quantity), Note: value.Note}
	})
}

func fromDomainEquipment(values []recipe.Equipment) []codecV1.Equipment {
	return convertSlice(values, func(value recipe.Equipment) codecV1.Equipment {
		return codecV1.Equipment{Name: value.Name, Quantity: fromDomainQuantity(value.Quantity), Note: value.Note}
	})
}

func toDomainTranslations(values []codecV1.Translation) []recipe.Translation {
	return convertSlice(values, func(value codecV1.Translation) recipe.Translation {
		return recipe.Translation{
			Relation: recipe.TranslationRelation(value.Relation), Language: value.Language,
			Title: value.Title, RecipeID: recipe.RecipeID(value.RecipeID), URL: value.URL,
		}
	})
}

func fromDomainTranslations(values []recipe.Translation) []codecV1.Translation {
	return convertSlice(values, func(value recipe.Translation) codecV1.Translation {
		return codecV1.Translation{
			Relation: string(value.Relation), Language: value.Language,
			Title: value.Title, RecipeID: string(value.RecipeID), URL: value.URL,
		}
	})
}

func toDomainIngredientSections(values []codecV1.IngredientSection) []recipe.IngredientSection {
	return convertSlice(values, func(value codecV1.IngredientSection) recipe.IngredientSection {
		return recipe.IngredientSection{ID: value.ID, Title: value.Title, Ingredients: toDomainIngredients(value.Ingredients)}
	})
}

func fromDomainIngredientSections(values []recipe.IngredientSection) []codecV1.IngredientSection {
	return convertSlice(values, func(value recipe.IngredientSection) codecV1.IngredientSection {
		return codecV1.IngredientSection{ID: value.ID, Title: value.Title, Ingredients: fromDomainIngredients(value.Ingredients)}
	})
}

func toDomainIngredients(values []codecV1.Ingredient) []recipe.Ingredient {
	return convertSlice(values, func(value codecV1.Ingredient) recipe.Ingredient {
		return recipe.Ingredient{
			Alternatives: toDomainIngredients(value.Alternatives), Relation: value.Relation, ID: value.ID, Name: value.Name,
			Quantity: toDomainQuantity(value.Quantity), Optional: value.Optional, Note: value.Note, SourceText: value.SourceText,
			Normalization: toDomainNormalization(value.Normalization),
			PackageSize:   toDomainSimpleQuantity(value.PackageSize),
			Temperature:   toDomainTemperature(value.Temperature),
			Recipe:        toDomainRecipeReference(value.Recipe),
		}
	})
}

func fromDomainIngredients(values []recipe.Ingredient) []codecV1.Ingredient {
	return convertSlice(values, func(value recipe.Ingredient) codecV1.Ingredient {
		return codecV1.Ingredient{
			Alternatives: fromDomainIngredients(value.Alternatives), Relation: value.Relation, ID: value.ID, Name: value.Name,
			Quantity: fromDomainQuantity(value.Quantity), Optional: value.Optional, Note: value.Note, SourceText: value.SourceText,
			Normalization: fromDomainNormalization(value.Normalization),
			PackageSize:   fromDomainSimpleQuantity(value.PackageSize),
			Temperature:   fromDomainTemperature(value.Temperature),
			Recipe:        fromDomainRecipeReference(value.Recipe),
		}
	})
}

func toDomainNormalization(value *codecV1.Normalization) *recipe.Normalization {
	if value == nil {
		return nil
	}

	return &recipe.Normalization{Method: recipe.NormalizationMethod(value.Method), Review: recipe.ReviewState(value.Review), Warnings: cloneStrings(value.Warnings)}
}

func fromDomainNormalization(value *recipe.Normalization) *codecV1.Normalization {
	if value == nil {
		return nil
	}

	return &codecV1.Normalization{Method: string(value.Method), Review: string(value.Review), Warnings: cloneStrings(value.Warnings)}
}

func toDomainQuantity(value *codecV1.Quantity) *recipe.Quantity {
	if value == nil {
		return nil
	}

	return &recipe.Quantity{
		Kind: recipe.QuantityKind(value.Kind), Amount: toDomainAmount(value.Amount), Unit: value.Unit,
		Terms: toDomainSimpleQuantities(value.Terms), Options: toDomainAlternativeOptions(value.Options), Text: value.Text,
	}
}

func fromDomainQuantity(value *recipe.Quantity) *codecV1.Quantity {
	if value == nil {
		return nil
	}

	return &codecV1.Quantity{
		Kind: string(value.Kind), Amount: fromDomainAmount(value.Amount), Unit: value.Unit,
		Terms: fromDomainSimpleQuantities(value.Terms), Options: fromDomainAlternativeOptions(value.Options), Text: value.Text,
	}
}

func toDomainAlternativeOptions(values []codecV1.AlternativeOption) []recipe.AlternativeOption {
	return convertSlice(values, func(value codecV1.AlternativeOption) recipe.AlternativeOption {
		return recipe.AlternativeOption{Kind: recipe.QuantityKind(value.Kind), Amount: toDomainAmount(value.Amount), Unit: value.Unit, Terms: toDomainSimpleQuantities(value.Terms)}
	})
}

func fromDomainAlternativeOptions(values []recipe.AlternativeOption) []codecV1.AlternativeOption {
	return convertSlice(values, func(value recipe.AlternativeOption) codecV1.AlternativeOption {
		return codecV1.AlternativeOption{Kind: string(value.Kind), Amount: fromDomainAmount(value.Amount), Unit: value.Unit, Terms: fromDomainSimpleQuantities(value.Terms)}
	})
}

func toDomainSimpleQuantities(values []codecV1.SimpleQuantity) []recipe.SimpleQuantity {
	return convertSlice(values, func(value codecV1.SimpleQuantity) recipe.SimpleQuantity {
		return *toDomainSimpleQuantity(&value)
	})
}

func fromDomainSimpleQuantities(values []recipe.SimpleQuantity) []codecV1.SimpleQuantity {
	return convertSlice(values, func(value recipe.SimpleQuantity) codecV1.SimpleQuantity {
		return *fromDomainSimpleQuantity(&value)
	})
}

func toDomainSimpleQuantity(value *codecV1.SimpleQuantity) *recipe.SimpleQuantity {
	if value == nil {
		return nil
	}

	return &recipe.SimpleQuantity{Kind: recipe.QuantityKind(value.Kind), Amount: toDomainAmountValue(value.Amount), Unit: value.Unit}
}

func fromDomainSimpleQuantity(value *recipe.SimpleQuantity) *codecV1.SimpleQuantity {
	if value == nil {
		return nil
	}

	return &codecV1.SimpleQuantity{Kind: string(value.Kind), Amount: fromDomainAmountValue(value.Amount), Unit: value.Unit}
}

func toDomainAmount(value *codecV1.Amount) *recipe.Amount {
	if value == nil {
		return nil
	}

	converted := toDomainAmountValue(*value)

	return &converted
}

func fromDomainAmount(value *recipe.Amount) *codecV1.Amount {
	if value == nil {
		return nil
	}

	converted := fromDomainAmountValue(*value)

	return &converted
}

func toDomainAmountValue(value codecV1.Amount) recipe.Amount {
	return recipe.Amount{Value: toDomainRational(value.Value), Min: toDomainRational(value.Min), Max: toDomainRational(value.Max), Approximate: value.Approximate}
}

func fromDomainAmountValue(value recipe.Amount) codecV1.Amount {
	return codecV1.Amount{Value: fromDomainRational(value.Value), Min: fromDomainRational(value.Min), Max: fromDomainRational(value.Max), Approximate: value.Approximate}
}

func toDomainRational(value *codecV1.Rational) *recipe.Rational {
	if value == nil {
		return nil
	}

	converted := recipe.Rational(*value)

	return &converted
}

func fromDomainRational(value *recipe.Rational) *codecV1.Rational {
	if value == nil {
		return nil
	}

	converted := codecV1.Rational(*value)

	return &converted
}

func toDomainTemperature(value *codecV1.Temperature) *recipe.Temperature {
	if value == nil {
		return nil
	}

	var purpose *recipe.TemperaturePurpose
	if value.Purpose != nil {
		purpose = &recipe.TemperaturePurpose{Kind: recipe.TemperaturePurposeKind(value.Purpose.Kind), Label: value.Purpose.Label}
	}

	return &recipe.Temperature{Amount: toDomainAmountValue(value.Amount), Unit: value.Unit, Text: value.Text, Purpose: purpose, Note: value.Note, SourceText: value.SourceText}
}

func fromDomainTemperature(value *recipe.Temperature) *codecV1.Temperature {
	if value == nil {
		return nil
	}

	var purpose *codecV1.TemperaturePurpose
	if value.Purpose != nil {
		purpose = &codecV1.TemperaturePurpose{Kind: string(value.Purpose.Kind), Label: value.Purpose.Label}
	}

	return &codecV1.Temperature{Amount: fromDomainAmountValue(value.Amount), Unit: value.Unit, Text: value.Text, Purpose: purpose, Note: value.Note, SourceText: value.SourceText}
}

func toDomainTemperatures(values []codecV1.Temperature) []recipe.Temperature {
	return convertSlice(values, func(value codecV1.Temperature) recipe.Temperature {
		return *toDomainTemperature(&value)
	})
}

func fromDomainTemperatures(values []recipe.Temperature) []codecV1.Temperature {
	return convertSlice(values, func(value recipe.Temperature) codecV1.Temperature {
		return *fromDomainTemperature(&value)
	})
}

func toDomainInstructionSections(values []codecV1.InstructionSection) []recipe.InstructionSection {
	return convertSlice(values, func(section codecV1.InstructionSection) recipe.InstructionSection {
		return recipe.InstructionSection{ID: section.ID, Title: section.Title, Steps: toDomainSteps(section.Steps)}
	})
}

func fromDomainInstructionSections(values []recipe.InstructionSection) []codecV1.InstructionSection {
	return convertSlice(values, func(section recipe.InstructionSection) codecV1.InstructionSection {
		return codecV1.InstructionSection{ID: section.ID, Title: section.Title, Steps: fromDomainSteps(section.Steps)}
	})
}

func toDomainSteps(values []codecV1.Step) []recipe.Step {
	result := make([]recipe.Step, len(values))
	for index, step := range values {
		result[index] = recipe.Step{
			ID: step.ID, Title: step.Title, Text: step.Text,
			IngredientUses: convertSlice(step.IngredientUses, func(use codecV1.IngredientUse) recipe.IngredientUse {
				return recipe.IngredientUse{IngredientRef: use.IngredientRef, Quantity: toDomainQuantity(use.Quantity)}
			}),
			ImageRefs: cloneStrings(step.ImageRefs), TimingRefs: cloneStrings(step.TimingRefs),
			Temperatures: toDomainTemperatures(step.Temperatures),
		}
	}

	return result
}

func fromDomainSteps(values []recipe.Step) []codecV1.Step {
	result := make([]codecV1.Step, len(values))
	for index, step := range values {
		result[index] = codecV1.Step{
			ID: step.ID, Title: step.Title, Text: step.Text,
			IngredientUses: convertSlice(step.IngredientUses, func(use recipe.IngredientUse) codecV1.IngredientUse {
				return codecV1.IngredientUse{IngredientRef: use.IngredientRef, Quantity: fromDomainQuantity(use.Quantity)}
			}),
			ImageRefs: cloneStrings(step.ImageRefs), TimingRefs: cloneStrings(step.TimingRefs),
			Temperatures: fromDomainTemperatures(step.Temperatures),
		}
	}

	return result
}

func toDomainAssets(values []codecV1.Asset) []recipe.Asset {
	return convertSlice(values, func(value codecV1.Asset) recipe.Asset {
		asset := recipe.Asset{
			ID: value.ID, Path: value.Path, MediaType: value.MediaType, SHA256: value.SHA256, Size: value.Size,
			Role: recipe.AssetRole(value.Role), Alt: value.Alt, Caption: value.Caption,
			Creator: value.Creator, Credit: value.Credit, Rights: value.Rights, SourceURL: value.SourceURL,
		}
		if value.License != nil {
			asset.License = &recipe.AssetLicense{Name: value.License.Name, URL: value.License.URL}
		}

		return asset
	})
}

func fromDomainAssets(values []recipe.Asset) []codecV1.Asset {
	return convertSlice(values, func(value recipe.Asset) codecV1.Asset {
		asset := codecV1.Asset{
			ID: value.ID, Path: value.Path, MediaType: value.MediaType, SHA256: value.SHA256, Size: value.Size,
			Role: string(value.Role), Alt: value.Alt, Caption: value.Caption,
			Creator: value.Creator, Credit: value.Credit, Rights: value.Rights, SourceURL: value.SourceURL,
		}
		if value.License != nil {
			asset.License = &codecV1.AssetLicense{Name: value.License.Name, URL: value.License.URL}
		}

		return asset
	})
}

func toDomainTimes(value *codecV1.Times) *recipe.Times {
	if value == nil {
		return nil
	}

	return &recipe.Times{
		Prep: toDomainDuration(value.Prep), Cook: toDomainDuration(value.Cook),
		Additional: toDomainDuration(value.Additional), Total: toDomainDuration(value.Total),
		Assertions: convertSlice(value.Assertions, func(assertion codecV1.TimingAssertion) recipe.TimingAssertion {
			return recipe.TimingAssertion{
				ID: assertion.ID, Kind: recipe.TimingKind(assertion.Kind), Label: assertion.Label,
				Duration: *toDomainDuration(&assertion.Duration), Attention: recipe.Attention(assertion.Attention),
				Note: assertion.Note, Microwave: toDomainMicrowave(assertion.Microwave),
				Provenance: recipe.TimingProvenance(assertion.Provenance),
			}
		}),
	}
}

func fromDomainTimes(value *recipe.Times) *codecV1.Times {
	if value == nil {
		return nil
	}

	return &codecV1.Times{
		Prep: fromDomainDuration(value.Prep), Cook: fromDomainDuration(value.Cook),
		Additional: fromDomainDuration(value.Additional), Total: fromDomainDuration(value.Total),
		Assertions: convertSlice(value.Assertions, func(assertion recipe.TimingAssertion) codecV1.TimingAssertion {
			return codecV1.TimingAssertion{
				ID: assertion.ID, Kind: string(assertion.Kind), Label: assertion.Label,
				Duration: *fromDomainDuration(&assertion.Duration), Attention: string(assertion.Attention),
				Note: assertion.Note, Microwave: fromDomainMicrowave(assertion.Microwave),
				Provenance: string(assertion.Provenance),
			}
		}),
	}
}

func toDomainDuration(value *codecV1.DurationExpression) *recipe.DurationExpression {
	if value == nil {
		return nil
	}

	return &recipe.DurationExpression{Value: value.Value, Min: value.Min, Max: value.Max, Approximate: value.Approximate, Text: value.Text, SourceText: value.SourceText}
}

func fromDomainDuration(value *recipe.DurationExpression) *codecV1.DurationExpression {
	if value == nil {
		return nil
	}

	return &codecV1.DurationExpression{Value: value.Value, Min: value.Min, Max: value.Max, Approximate: value.Approximate, Text: value.Text, SourceText: value.SourceText}
}

func toDomainMicrowave(value *codecV1.MicrowaveConditions) *recipe.MicrowaveConditions {
	if value == nil {
		return nil
	}

	return &recipe.MicrowaveConditions{
		RatedOutputWatts: value.RatedOutputWatts, Power: toDomainMicrowavePower(value.Power), ModeText: value.ModeText,
		SourceText: value.SourceText, Restrictions: cloneStrings(value.Restrictions),
		Choices: convertSlice(value.Choices, func(choice codecV1.MicrowaveChoice) recipe.MicrowaveChoice {
			return recipe.MicrowaveChoice{
				Duration: *toDomainDuration(&choice.Duration), RatedOutputWatts: choice.RatedOutputWatts,
				Power: toDomainMicrowavePower(choice.Power), SourceText: choice.SourceText,
			}
		}),
	}
}

func fromDomainMicrowave(value *recipe.MicrowaveConditions) *codecV1.MicrowaveConditions {
	if value == nil {
		return nil
	}

	return &codecV1.MicrowaveConditions{
		RatedOutputWatts: value.RatedOutputWatts, Power: fromDomainMicrowavePower(value.Power), ModeText: value.ModeText,
		SourceText: value.SourceText, Restrictions: cloneStrings(value.Restrictions),
		Choices: convertSlice(value.Choices, func(choice recipe.MicrowaveChoice) codecV1.MicrowaveChoice {
			return codecV1.MicrowaveChoice{
				Duration: *fromDomainDuration(&choice.Duration), RatedOutputWatts: choice.RatedOutputWatts,
				Power: fromDomainMicrowavePower(choice.Power), SourceText: choice.SourceText,
			}
		}),
	}
}

func toDomainMicrowavePower(value *codecV1.MicrowavePower) *recipe.MicrowavePower {
	if value == nil {
		return nil
	}

	return &recipe.MicrowavePower{Watts: value.Watts, Percent: value.Percent, Text: value.Text}
}

func fromDomainMicrowavePower(value *recipe.MicrowavePower) *codecV1.MicrowavePower {
	if value == nil {
		return nil
	}

	return &codecV1.MicrowavePower{Watts: value.Watts, Percent: value.Percent, Text: value.Text}
}
