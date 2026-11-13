package exchange_test

import (
	"context"
	"testing"

	"github.com/savorum/savorum/internal/exchange"
)

func TestAPageAddressSuppliesAConventionThatCanBeWithdrawn(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})
	ctx := context.Background()

	preview, err := service.PreviewFiles(ctx, []exchange.UploadedImport{{
		Filename: "loaf.yml",
		Data:     []byte("name: Loaf\ningredients:\n  - 1 cup water\n  - 2 tsp salt\ndirections:\n  - Mix.\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	assumed := exchange.AssumeAddressConvention(service, ctx, preview, "https://www.kingarthurbaking.com/recipes/loaf")
	if len(assumed.ResolutionDecisions) != 0 || assumed.AssumedConvention == nil ||
		assumed.AssumedConvention.Convention != "us" || assumed.AssumedConvention.Host != "kingarthurbaking.com" ||
		!assumed.AssumedConvention.Revertible {
		t.Fatalf("assumed = %#v", assumed.AssumedConvention)
	}

	reverted, err := service.RevertMeasurementResolutionPlan(ctx, preview.ID, assumed.AssumedConvention.PlanID, assumed.Revision)
	if err != nil {
		t.Fatal(err)
	}

	if reverted.AssumedConvention != nil || len(reverted.ResolutionDecisions) != len(preview.ResolutionDecisions) {
		t.Fatalf("reverted = %#v, decisions %d", reverted.AssumedConvention, len(reverted.ResolutionDecisions))
	}
}

func TestAnAddressWithoutAUsualConventionAssumesNothing(t *testing.T) {
	t.Parallel()

	for _, address := range []string{"https://example.org/r", "https://chefkoch.de/r", "not a url"} {
		if convention, _ := exchange.AddressConvention(address); convention != "" {
			t.Errorf("%s assumed %q", address, convention)
		}
	}

	if convention, host := exchange.AddressConvention("https://www.taste.com.au/r"); convention != "au" || host != "taste.com.au" {
		t.Errorf("taste.com.au = %q %q", convention, host)
	}
}

func TestAStepPhotographStillToBeFetchedDoesNotRefuseAnAnswer(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})
	ctx := context.Background()

	preview, err := service.PreviewFiles(ctx, []exchange.UploadedImport{{
		Filename: "loaf.json",
		Data: []byte(`{"@context":"https://schema.org","@type":"Recipe","name":"Loaf",
			"image":"https://images.example/loaf.jpg","recipeIngredient":["1 cup water","2 tsp salt"],
			"recipeInstructions":[{"@type":"HowToStep","text":"Mix.","image":"https://images.example/mix.jpg"}]}`),
	}})
	if err != nil {
		t.Fatal(err)
	}

	assumed := exchange.AssumeAddressConvention(service, ctx, preview, "https://loaves.example.com/loaf")
	if assumed.AssumedConvention == nil || len(assumed.ResolutionDecisions) != 0 {
		t.Fatalf("assumed = %#v, decisions %d", assumed.AssumedConvention, len(assumed.ResolutionDecisions))
	}
}

func TestAFormWithOneMeaningInTheRegistryIsNotAsked(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
		Filename: "suppe.json",
		Data: []byte(`{"@context":"https://schema.org","@type":"Recipe","name":"Ertesuppe","inLanguage":"no",
			"recipeIngredient":["500 g tørre gule erter","1 ts timian"],"recipeInstructions":["Kok."]}`),
	}})
	if err != nil {
		t.Fatal(err)
	}

	if len(preview.ResolutionDecisions) != 0 {
		t.Fatalf("decisions = %#v", preview.ResolutionDecisions)
	}

	quantity := preview.Items[0].Recipe.IngredientSections[0].Ingredients[0].Quantity
	if quantity == nil || quantity.Unit != "mass.gram" {
		t.Fatalf("quantity = %#v", quantity)
	}
}

func TestSoleReadingsSettleALineThatNeedsTwoAnswers(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
		Filename: "burger.json",
		Data: []byte(`{"@context":"https://schema.org","@type":"Recipe","name":"Burger","inLanguage":"no",
			"recipeIngredient":["2 stk. egg","2 stk. løk"],"recipeInstructions":["Stek."]}`),
	}})
	if err != nil {
		t.Fatal(err)
	}

	if len(preview.ResolutionDecisions) != 0 {
		t.Fatalf("decisions = %#v", preview.ResolutionDecisions)
	}

	for _, ingredient := range preview.Items[0].Recipe.IngredientSections[0].Ingredients {
		if ingredient.Quantity == nil {
			t.Errorf("%q has no quantity", ingredient.SourceText)
		}
	}
}

func TestAnAddressSaysNothingAboutAPageInAnotherLanguage(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})
	ctx := context.Background()

	preview, err := service.PreviewFiles(ctx, []exchange.UploadedImport{{
		Filename: "pita.html",
		Data: []byte(`<html lang="he"><script type="application/ld+json">{"@type":"Recipe","name":"פיתה",` +
			`"recipeIngredient":["1 כוס מים","3 כוסות קמח"],"recipeInstructions":["ללוש."]}</script></html>`),
	}})
	if err != nil {
		t.Fatal(err)
	}

	if len(preview.ResolutionDecisions) == 0 {
		t.Fatalf("no question to answer: %#v", preview.Items[0].Recipe.SourceLocale)
	}

	if assumed := exchange.AssumeAddressConvention(service, ctx, preview, "https://www.example.com/pita"); assumed.AssumedConvention != nil {
		t.Fatalf("assumed %#v for a Hebrew page", assumed.AssumedConvention)
	}
}
