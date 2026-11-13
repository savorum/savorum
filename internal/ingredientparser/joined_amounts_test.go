package ingredientparser_test

import (
	"testing"

	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/recipe"
)

func TestJoinedMixedFractions(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line, want, name string }{
		{"pt-BR", "3 e 1/2 xícara (chá) de amido de milho", "7/2 volume.cup.us.culinary", "amido de milho"},
		{"pt-BR", "1 e ½ xícara de leite", "3/2 volume.cup.us.culinary", "leite"},
		{"pt-BR", "1 E 1/4 colher de chá de sal", "5/4 volume.teaspoon.metric.culinary", "sal"},
		{"ar", "1 و 1/4 ملعقة صغيرة ملح", "5/4 volume.teaspoon.metric.culinary", "ملح"},
		{"ar", "١ و ١/٢ ملعقة كبيرة زيت", "3/2 volume.tablespoon.metric.culinary", "زيت"},
		{"ar", "١و½ ملعقة كبيرة زيت", "3/2 volume.tablespoon.metric.culinary", "زيت"},
		{"ja", "砂糖 大さじ1 と 1/2", "3/2 volume.tablespoon.metric.culinary", "砂糖"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if got := reading(parsed); got != test.want || parsed.Name != test.name || parsed.SourceText != test.line {
			t.Errorf("%s %q: %s, name %q, source %q", test.locale, test.line, got, parsed.Name, parsed.SourceText)
		}
	}
}

func TestUnitlessAdditionStaysUnresolved(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line string }{
		{"uk", "6 + 3 горошини перцю"},
		{"en", "2 plus 3 carrots"},
		{"pt-BR", "1 e 2 ovos"},
		{"ar", "1 و 2 حبات بصل"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if parsed.Quantity != nil || parsed.Normalization == nil || parsed.Normalization.Method != recipe.NormalizationUnresolved {
			t.Errorf("%s %q: %#v", test.locale, test.line, parsed)
		}
	}
}

func TestDecagramsAreWeights(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line, want, name string }{
		{"hu", "3 dkg vaj", "3 mass.decagram", "vaj"},
		{"cs", "20 deka šunky", "20 mass.decagram", "šunky"},
		{"pl", "5 dag cukru", "5 mass.decagram", "cukru"},
		{"de-AT", "10 dag Butter", "10 mass.decagram", "Butter"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if got := reading(parsed); got != test.want || parsed.Name != test.name {
			t.Errorf("%s %q: %s, name %q", test.locale, test.line, got, parsed.Name)
		}
	}
}

func TestJoinedSpoonSums(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line, name string }{
		{"pt-BR", "1 colher de sopa e 2 colheres de chá de azeite", "azeite"},
		{"ar", "1 ملعقة كبيرة و 2 ملعقة صغيرة زيت", "زيت"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)

		q := parsed.Quantity
		if q == nil || q.Kind != recipe.QuantitySum || len(q.Terms) != 2 {
			t.Fatalf("%s: quantity = %#v", test.line, q)
		}

		if q.Terms[0].Amount.Value.String() != "1" || q.Terms[1].Amount.Value.String() != "2" ||
			q.Terms[0].Unit != "volume.tablespoon.metric.culinary" || q.Terms[1].Unit != "volume.teaspoon.metric.culinary" ||
			parsed.Name != test.name || parsed.SourceText != test.line {
			t.Errorf("%s: ingredient = %#v", test.line, parsed)
		}
	}
}

func TestMeasureVariantsFromMultilingualPages(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line, want, name, note string }{
		{"ro-RO", "½ lingura amidon de porumb", "1/2 volume.tablespoon.metric.culinary", "amidon de porumb", ""},
		{"ro-RO", "1 lingurita de capere", "1 volume.teaspoon.metric.culinary", "capere", ""},
		{"ro", "2 lingurite de zahar", "2 volume.teaspoon.metric.culinary", "zahar", ""},
		{"pl", "2 ¼ płaskich łyżeczek żelatyny", "9/4 volume.teaspoon.metric.culinary", "żelatyny", "płaskich"},
		{"pl", "5 czubatych łyżek mąki", "5 volume.tablespoon.metric.culinary", "mąki", "czubatych"},
		{"pl", "5 kopiastych łyżek mąki pszennej", "5 volume.tablespoon.metric.culinary", "mąki pszennej", "kopiastych"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if got := reading(parsed); got != test.want || parsed.Name != test.name || parsed.Note != test.note || parsed.SourceText != test.line {
			t.Errorf("%s %q: %s, name %q, note %q", test.locale, test.line, got, parsed.Name, parsed.Note)
		}
	}

	for _, test := range []struct{ locale, line string }{
		{"pl", "1 Trochę Sól i pieprz do smaku"},
		{"pl", "1 Trochę Olej do smażenia"},
		{"pl", "1 Trochę 2-cm kawałek imbiru"},
		{"ro", "¼ cana de lapte"},
		{"nb", "3 krm salt"},
		{"fi", "1 prk (150 g) ranskankermaa"},
		{"fi", "2 ps (à 180 g) katkarapuja"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if parsed.Quantity != nil || parsed.Normalization == nil || parsed.Normalization.Method != recipe.NormalizationUnresolved {
			t.Errorf("%s %q: %#v", test.locale, test.line, parsed)
		}
	}
}
