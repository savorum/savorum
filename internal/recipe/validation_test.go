package recipe_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestValidateAcceptsComplexRecipe(t *testing.T) {
	t.Parallel()

	if err := recipe.ValidateForWrite(testfixture.Document(), units.DefaultCatalog()); err != nil {
		t.Fatalf("complex fixture should be valid: %v", err)
	}
}

func TestValidateInstructionStepRequiresTitleOrText(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	step := &document.InstructionSections[0].Steps[0]
	step.Title, step.Text = "Make the roux", ""

	if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); err != nil {
		t.Fatalf("title-only step should be valid: %v", err)
	}

	step.Title = ""

	if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); !hasViolation(err, "step.content_required") {
		t.Fatalf("empty step should be rejected, got %v", err)
	}
}

func TestValidateAcceptsAuthoredClassificationsAndSourceChronology(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.Categories = []string{"Cake", "Celebration cake"}
	document.Courses = []string{"Dessert", "Afternoon tea"}
	document.CookingMethods = []string{"Baking", "Whipping"}
	document.Published = "2023-05-04"

	document.Modified = "2025-06-07t08:09:10.123z"
	if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); err != nil {
		t.Fatalf("authored metadata should be valid: %v", err)
	}
}

func TestValidateAcceptsCompoundQuantityAlternative(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()

	document.IngredientSections[0].Ingredients[0].Quantity = &recipe.Quantity{
		Kind: recipe.QuantityAlternatives,
		Options: []recipe.AlternativeOption{
			{Kind: recipe.QuantitySum, Terms: []recipe.SimpleQuantity{
				{Kind: recipe.QuantitySimple, Amount: recipe.Amount{Value: testfixture.Rational("2")}, Unit: "volume.tablespoon.us.customary"},
				{Kind: recipe.QuantitySimple, Amount: recipe.Amount{Value: testfixture.Rational("2")}, Unit: "volume.teaspoon.us.customary"},
			}},
			{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: testfixture.Rational("40")}, Unit: "volume.milliliter"},
		},
	}
	if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); err != nil {
		t.Fatalf("compound alternative should be valid: %v", err)
	}
}

func TestValidateAcceptsDigitLeadingPortableIdentifiers(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()

	document.ID = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
	if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); err != nil {
		t.Fatalf("digit-leading portable identifier should be valid: %v", err)
	}
}

func TestValidateAcceptsIndependentIngredientUseQuantities(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	first := &document.InstructionSections[0].Steps[0]
	first.IngredientUses[0].Quantity = &recipe.Quantity{
		Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: testfixture.Rational("3/2")}, Unit: "volume.cup.us.customary",
	}
	second := &document.InstructionSections[0].Steps[1]
	second.IngredientUses = []recipe.IngredientUse{{
		IngredientRef: "flour",
		Quantity: &recipe.Quantity{
			Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: testfixture.Rational("1/2")}, Unit: "volume.cup.us.customary",
		},
	}}

	if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); err != nil {
		t.Fatalf("independent usage quantities should not need to sum to the ingredient declaration: %v", err)
	}
}

func TestValidateAcceptsIndependentRecipeAndStepImageRelationships(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.Assets = append(document.Assets, recipe.Asset{
		ID: "process-photo", Path: "assets/process.jpg", MediaType: "image/jpeg",
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 1,
		Role: recipe.AssetRoleImage,
	})
	document.ImageRefs = []string{"process-photo", "finished-pies"}
	document.PrimaryImage = "finished-pies"

	document.InstructionSections[0].Steps[0].ImageRefs = []string{"process-photo", "finished-pies"}
	if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); err != nil {
		t.Fatalf("shared recipe and step images should be valid: %v", err)
	}
}

func TestValidateAcceptsIndependentTemperaturePurposes(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()

	document.InstructionSections[0].Steps[0].Temperatures = []recipe.Temperature{
		{
			Amount:  recipe.Amount{Value: testfixture.Rational("350")},
			Unit:    "temperature.fahrenheit",
			Purpose: &recipe.TemperaturePurpose{Kind: recipe.TemperaturePurposeOil},
		},
		{
			Amount:  recipe.Amount{Value: testfixture.Rational("165")},
			Unit:    "temperature.fahrenheit",
			Purpose: &recipe.TemperaturePurpose{Kind: recipe.TemperaturePurposeInternal, Label: "chicken breast"},
		},
		{
			Amount:  recipe.Amount{Value: testfixture.Rational("240")},
			Unit:    "temperature.fahrenheit",
			Purpose: &recipe.TemperaturePurpose{Kind: recipe.TemperaturePurposeOther, Label: "sugar syrup"},
		},
	}
	if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); err != nil {
		t.Fatalf("independent temperature purposes should be valid: %v", err)
	}
}

func TestValidateAcceptsOpaqueTemperatureWithoutInference(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()

	document.InstructionSections[0].Steps[0].Temperatures = []recipe.Temperature{{
		Text: "moderate oven", Purpose: &recipe.TemperaturePurpose{Kind: recipe.TemperaturePurposeOven},
	}}
	if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); err != nil {
		t.Fatalf("opaque authored temperature should be valid: %v", err)
	}
}

func TestValidateRejectsSemanticFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
		edit func(*recipe.Document)
	}{
		{name: "noncurrent schema", code: "schema_version.current", edit: func(document *recipe.Document) {
			document.SchemaVersion++
		}},
		{name: "dangling ingredient reference", code: "reference.dangling", edit: func(document *recipe.Document) {
			document.InstructionSections[0].Steps[0].IngredientUses = append(document.InstructionSections[0].Steps[0].IngredientUses, recipe.IngredientUse{IngredientRef: "missing"})
		}},
		{name: "invalid unit ID", code: "unit.id", edit: func(document *recipe.Document) {
			document.IngredientSections[0].Ingredients[0].Quantity.Terms[0].Unit = "volume_cup"
		}},
		{name: "temperature in ordinary quantity", code: "unit.temperature_context", edit: func(document *recipe.Document) {
			document.IngredientSections[1].Ingredients[0].Quantity.Unit = "temperature.celsius"
		}},
		{name: "temperature in ingredient use quantity", code: "unit.temperature_context", edit: func(document *recipe.Document) {
			document.InstructionSections[0].Steps[0].IngredientUses[0].Quantity = &recipe.Quantity{
				Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: testfixture.Rational("180")}, Unit: "temperature.celsius",
			}
		}},
		{name: "empty equipment name", code: "text.required", edit: func(document *recipe.Document) {
			document.Equipment = []recipe.Equipment{{Name: ""}}
		}},
		{name: "temperature in equipment quantity", code: "unit.temperature_context", edit: func(document *recipe.Document) {
			document.Equipment = []recipe.Equipment{{
				Name: "oven",
				Quantity: &recipe.Quantity{
					Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: testfixture.Rational("180")}, Unit: "temperature.celsius",
				},
			}}
		}},
		{name: "unknown temperature purpose", code: "temperature.purpose.kind", edit: func(document *recipe.Document) {
			document.InstructionSections[1].Steps[1].Temperatures[0].Purpose = &recipe.TemperaturePurpose{Kind: "broiler"}
		}},
		{name: "other temperature purpose without label", code: "temperature.purpose.label", edit: func(document *recipe.Document) {
			document.InstructionSections[1].Steps[1].Temperatures[0].Purpose = &recipe.TemperaturePurpose{Kind: recipe.TemperaturePurposeOther}
		}},
		{name: "opaque temperature with numeric form", code: "temperature.opaque_exclusive", edit: func(document *recipe.Document) {
			document.InstructionSections[1].Steps[1].Temperatures[0].Text = "quick oven"
		}},
		{name: "duplicate ingredient use", code: "reference.duplicate", edit: func(document *recipe.Document) {
			document.InstructionSections[0].Steps[0].IngredientUses = append(document.InstructionSections[0].Steps[0].IngredientUses, document.InstructionSections[0].Steps[0].IngredientUses[0])
		}},
		{name: "physical package size required", code: "unit.physical_required", edit: func(document *recipe.Document) {
			document.IngredientSections[1].Ingredients[1].PackageSize.Unit = "count.item"
		}},
		{name: "reversed range", code: "amount.range_order", edit: func(document *recipe.Document) {
			document.IngredientSections[1].Ingredients[0].Quantity.Amount.Min = testfixture.Rational("501")
		}},
		{name: "duplicate keyword", code: "list.duplicate", edit: func(document *recipe.Document) {
			document.Keywords = append(document.Keywords, document.Keywords[0])
		}},
		{name: "duplicate authored category", code: "list.duplicate", edit: func(document *recipe.Document) {
			document.Categories = []string{"Cake", "Cake"}
		}},
		{name: "invalid publication date", code: "chronology.impossible", edit: func(document *recipe.Document) {
			document.Published = "2024-02-30"
		}},
		{name: "invented publication time wording", code: "chronology.invalid", edit: func(document *recipe.Document) {
			document.Modified = "March 10, 2024"
		}},
		{name: "empty author name", code: "text.required", edit: func(document *recipe.Document) {
			document.Authors[0].Name = ""
		}},
		{name: "unknown author kind", code: "author.kind", edit: func(document *recipe.Document) {
			document.Authors[0].Kind = recipe.AuthorKind("editorial-team")
		}},
		{name: "kind label without other kind", code: "author.kind_label", edit: func(document *recipe.Document) {
			document.Authors[0].KindLabel = "test kitchen"
		}},
		{name: "relative author URL", code: "author.url", edit: func(document *recipe.Document) {
			document.Authors[0].URL = "/authors/test-kitchen"
		}},
		{name: "retrieval time without source identity", code: "source.identity", edit: func(document *recipe.Document) {
			document.Source = &recipe.Source{RetrievedAt: "2026-08-29T12:00:00Z"}
		}},
		{name: "source ID without application namespace", code: "source.application", edit: func(document *recipe.Document) {
			document.Source = &recipe.Source{Name: "Recipe archive", SourceID: "12345"}
		}},
		{name: "invalid language", code: "language.invalid", edit: func(document *recipe.Document) {
			document.Language = "en_US"
		}},
		{name: "empty times", code: "times.empty", edit: func(document *recipe.Document) {
			document.Times = &recipe.Times{}
		}},
		{name: "unreviewed inference", code: "normalization.warning", edit: func(document *recipe.Document) {
			document.IngredientSections[1].Ingredients[2].Normalization.Warnings = nil
		}},
		// The four canonical-form rules are separate requirements, and a caller
		// that cannot tell them apart cannot say which one a document broke.
		{name: "unreduced fraction", code: "amount.rational_unreduced", edit: func(document *recipe.Document) {
			document.IngredientSections[1].Ingredients[0].Quantity.Amount.Min = testfixture.Rational("2/4")
		}},
		{name: "denominator of one", code: "amount.rational_denominator", edit: func(document *recipe.Document) {
			document.IngredientSections[1].Ingredients[0].Quantity.Amount.Min = testfixture.Rational("3/1")
		}},
		{name: "decimal amount", code: "amount.rational", edit: func(document *recipe.Document) {
			document.IngredientSections[1].Ingredients[0].Quantity.Amount.Min = testfixture.Rational("1.5")
		}},
		{name: "amount beyond the interoperability bound", code: "amount.rational_range", edit: func(document *recipe.Document) {
			document.IngredientSections[1].Ingredients[0].Quantity.Amount.Min = testfixture.Rational("184467440737095516160")
		}},
		{name: "nested alternative option", code: "quantity.kind", edit: func(document *recipe.Document) {
			document.IngredientSections[0].Ingredients[1].Quantity.Options[0].Kind = recipe.QuantityAlternatives
		}},
		{name: "alternative inside sum option", code: "quantity.kind", edit: func(document *recipe.Document) {
			document.IngredientSections[0].Ingredients[1].Quantity.Options[0] = recipe.AlternativeOption{
				Kind: recipe.QuantitySum,
				Terms: []recipe.SimpleQuantity{
					{Kind: recipe.QuantitySimple, Amount: recipe.Amount{Value: testfixture.Rational("1")}},
					{Kind: recipe.QuantityAlternatives, Amount: recipe.Amount{Value: testfixture.Rational("2")}},
				},
			}
		}},
		{name: "bad asset media type", code: "asset.media_type", edit: func(document *recipe.Document) {
			document.Assets[0].MediaType = "image"
		}},
		{name: "unsafe asset path", code: "asset.path", edit: func(document *recipe.Document) {
			document.Assets[0].Path = "assets/../secret.jpg"
		}},
		{name: "case-colliding asset path", code: "asset.path_duplicate", edit: func(document *recipe.Document) {
			duplicate := document.Assets[0]
			duplicate.ID = "alternate-finished-pies"
			duplicate.Path = "assets/FINISHED-PIES.jpg"
			document.Assets = append(document.Assets, duplicate)
		}},
		{name: "dangling step image", code: "asset.reference", edit: func(document *recipe.Document) {
			document.InstructionSections[1].Steps[1].ImageRefs = []string{"missing-image"}
		}},
		{name: "dangling recipe image", code: "asset.reference", edit: func(document *recipe.Document) {
			document.ImageRefs = []string{"missing-image"}
			document.PrimaryImage = ""
		}},
		{name: "duplicate recipe image", code: "reference.duplicate", edit: func(document *recipe.Document) {
			document.ImageRefs = append(document.ImageRefs, document.ImageRefs[0])
		}},
		{name: "primary image outside recipe images", code: "asset.reference", edit: func(document *recipe.Document) {
			document.ImageRefs = nil
		}},
		{name: "legacy relational asset role", code: "asset.role", edit: func(document *recipe.Document) {
			document.Assets[0].Role = recipe.AssetRole("primary-image")
		}},
		{name: "detached future SREF data", code: "sref_opaque.detached", edit: func(document *recipe.Document) {
			document.SREFOpaque = map[string]any{"ingredient:missing": map[string]any{"future_detail": true}}
		}},
		{name: "non-image step asset", code: "asset.media_type", edit: func(document *recipe.Document) {
			document.Assets[0].MediaType = "application/pdf"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			document := testfixture.Document()
			test.edit(&document)

			err := recipe.ValidateForWrite(document, units.DefaultCatalog())
			if !hasViolation(err, test.code) {
				t.Fatalf("expected violation %q, got %#v", test.code, err)
			}
		})
	}
}

func TestValidatePreservesUnknownFutureUnitIDs(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()

	document.IngredientSections[0].Ingredients[0].Quantity.Terms[0].Unit = "volume.measure.future"
	if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); err != nil {
		t.Fatalf("syntactically valid future unit should be preservable: %v", err)
	}
}

func TestValidateAcceptsGrandfatheredLanguageTagAndNegativeTemperature(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.Language = "i-klingon"

	document.InstructionSections[1].Steps[1].Temperatures[0].Amount.Value = testfixture.Rational("-40")
	if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); err != nil {
		t.Fatalf("expected valid document: %v", err)
	}
}

func hasViolation(err error, code string) bool {
	var validation *recipe.ValidationError
	if !errors.As(err, &validation) {
		return false
	}

	for _, violation := range validation.Violations {
		if violation.Code == code {
			return true
		}
	}

	return false
}

// A wall-clock time with no offset is a missing zone, not an impossible date.
func TestValidateDistinguishesAMissingTimeZoneFromAnImpossibleDate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, value, code, says string
	}{
		{name: "wall clock without an offset", value: "2017-10-30T16:28:45", code: "chronology.invalid", says: "UTC offset"},
		{name: "day that never happened", value: "2024-02-30", code: "chronology.impossible", says: "real calendar date"},
		{name: "localized prose", value: "March 10, 2024", code: "chronology.invalid", says: "RFC 3339"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			document := testfixture.Document()
			document.Published = test.value

			err := recipe.ValidateForWrite(document, units.DefaultCatalog())
			if !hasViolation(err, test.code) {
				t.Fatalf("expected violation %q, got %#v", test.code, err)
			}

			var validation *recipe.ValidationError
			if !errors.As(err, &validation) || !strings.Contains(validation.Violations[0].Message, test.says) {
				t.Fatalf("violation did not say %q: %#v", test.says, err)
			}
		})
	}
}

// This sentence is stored against a failed import and shown to the person who
// asked for it. A count of violations tells them nothing they can act on.
func TestValidationErrorNamesWhatIsWrongRatherThanCountingIt(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.Published = "March 10, 2024"
	document.Modified = "yesterday"

	err := recipe.ValidateForWrite(document, units.DefaultCatalog())
	if err == nil {
		t.Fatal("two unreadable dates were accepted")
	}

	message := err.Error()
	if !strings.Contains(message, "published") || !strings.Contains(message, "modified") {
		t.Fatalf("error did not name the fields: %q", message)
	}

	if strings.Contains(message, "violation(s)") {
		t.Fatalf("error still only counts: %q", message)
	}
}
