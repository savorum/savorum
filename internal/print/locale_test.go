package print

import (
	"math/big"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/recipe"

	"github.com/savorum/savorum/internal/units"
)

func TestLocaleCulinaryPresentation(t *testing.T) {
	p := newPresenter(units.DefaultCatalog(), units.MeasureAsWritten, big.NewRat(1, 1), "de-DE")
	if got := p.compose([]*big.Rat{big.NewRat(127, 100)}, "mass.gram", false); got.Text != "1,27g" {
		t.Fatalf("localized amount: %#v", got)
	}

	if got := p.number(big.NewRat(3, 2)); got != "1½" {
		t.Fatalf("fraction: %s", got)
	}

	options := DefaultOptions()

	options.Locale = "not a locale"
	if _, err := options.Normalize(); err == nil {
		t.Fatal("invalid locale accepted")
	}
}

func TestPrintMessagesFallbackAndPreserveAuthoredLabels(t *testing.T) {
	headings := map[string]string{
		"en": "Ingredients", "en-AU": "Ingredients", "zu": "Ingredients",
		"de-DE": printMessageCatalogs["de"]["ingredients"],
		"pt-BR": printMessageCatalogs["pt-BR"]["ingredients"],
		"pt-PT": printMessageCatalogs["pt-PT"]["ingredients"],
	}
	for locale, want := range headings {
		if got := printMessages(locale)["ingredients"]; got != want {
			t.Fatalf("%s heading: %q, want %q", locale, got, want)
		}

		authored := recipe.TimingAssertion{Kind: recipe.TimingRest, Label: "Repos de la pâte"}
		if got := phaseLabel(authored, locale); got != authored.Label {
			t.Fatalf("authored label changed: %q", got)
		}

		if got := durationExpressionWords(recipe.DurationExpression{Text: "toute la nuit"}, "", locale); got != "toute la nuit" {
			t.Fatalf("opaque duration changed: %q", got)
		}
	}

	labels := printMessages("en")
	labels["ingredients"] = "changed"

	if got := printMessage("ingredients", "en"); got != "Ingredients" {
		t.Fatal("caller mutated shared catalog")
	}

	if got := printMessageVersion(); !strings.HasPrefix(got, "print-messages-v1-") {
		t.Fatalf("catalog is not versioned: %q", got)
	}
}

func TestAllPrintCatalogsResolveHeadingsAndMetadataTogether(t *testing.T) {
	for locale, catalog := range printMessageCatalogs {
		for key, expected := range catalog {
			if got := printMessage(key, locale); got != expected {
				t.Errorf("%s %s: got %q, want %q", locale, key, got, expected)
			}
		}

		for key := range printMessageCatalogs["en"] {
			if catalog[key] == "" {
				t.Errorf("%s is missing %s", locale, key)
			}
		}
	}

	for locale, expected := range map[string]string{"ar-EG": "ar", "de-CH": "de", "pt": "pt-BR", "pt-PT": "pt-PT", "zh": "zh-Hans", "zh-CN": "zh-Hans", "zh-TW": "zh-Hant", "zh-HK": "zh-Hant", "zu": "en"} {
		if got := catalogLocale(locale); got != expected {
			t.Errorf("%s matched %s, want %s", locale, got, expected)
		}
	}
}

func TestPrintUnitWordsFollowLocaleAndPluralRules(t *testing.T) {
	for _, test := range []struct{ locale, amount, unit, expected string }{
		{"ar", "1", "count.serving", "حصة"},
		{"ar", "2", "count.serving", "حصتان"},
		{"ar", "4", "count.serving", "حصص"},
		{"ar", "11", "count.serving", "حصة"},
		{"ar", "1", "count.bunch", "حزمة"},
		{"ru", "2", "count.serving", "порции"},
		{"ru", "5", "count.serving", "порций"},
		{"ru", "21", "count.serving", "порция"},
		{"pl", "2", "count.serving", "porcje"},
		{"pl", "5", "count.serving", "porcji"},
		{"pt-BR", "2", "volume.cup.us.customary", "xícaras"},
		{"pt-PT", "2", "volume.cup.us.customary", "chávenas"},
		{"zh-TW", "2", "volume.tablespoon.au", "湯匙"},
		{"ja", "2", "count.serving", "人分"},
		{"en", "1/2", "count.serving", "serving"},
		{"fr", "2", "count.serving", "portions"},
	} {
		amount, _ := new(big.Rat).SetString(test.amount)
		if got, _ := units.LabelForLocale(test.unit, amount, test.locale); got != test.expected {
			t.Errorf("%s %s %s: got %q, want %q", test.locale, test.amount, test.unit, got, test.expected)
		}
	}
}

func TestDocumentLocaleFollowsTheRecipeLanguage(t *testing.T) {
	t.Parallel()

	snapshot := func(language string) RecipeSnapshot {
		return RecipeSnapshot{Recipe: recipe.Document{Language: language}}
	}

	for name, test := range map[string]struct {
		snapshots []RecipeSnapshot
		want      string
	}{
		"one language with a catalog":     {[]RecipeSnapshot{snapshot("ja")}, "ja"},
		"a regional tag keeps its region": {[]RecipeSnapshot{snapshot("de-CH")}, "de-CH"},
		"no language":                     {[]RecipeSnapshot{snapshot("")}, "fr-FR"},
		"a language without a catalog":    {[]RecipeSnapshot{snapshot("zu")}, "fr-FR"},
		"the same language twice":         {[]RecipeSnapshot{snapshot("ja"), snapshot("ja")}, "ja"},
		"languages that differ":           {[]RecipeSnapshot{snapshot("ja"), snapshot("de")}, "fr-FR"},
		"one recipe with no language":     {[]RecipeSnapshot{snapshot("ja"), snapshot("")}, "fr-FR"},
		"nothing to print":                {nil, "fr-FR"},
	} {
		if got := documentLocale("fr-FR", test.snapshots); got != test.want {
			t.Errorf("%s: locale %q, want %q", name, got, test.want)
		}
	}
}
