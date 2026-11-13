package ingredientparser_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/recipe"
)

// reading summarizes what a parse made of a line, so a table can state it.
func reading(ingredient recipe.Ingredient) string {
	quantity := ingredient.Quantity
	if quantity == nil || quantity.Kind != recipe.QuantitySimple || quantity.Amount == nil {
		return "no quantity"
	}

	amount := quantity.Amount

	text := ""

	switch {
	case amount.Value != nil:
		text = amount.Value.String()
	case amount.Min != nil && amount.Max != nil:
		text = amount.Min.String() + "-" + amount.Max.String()
	}

	if amount.Approximate {
		text = "~" + text
	}

	return fmt.Sprintf("%s %s", text, quantity.Unit)
}

// TestLinesInEveryLanguageReadWithTheirOwnUnits covers each language the unit
// registry names. A line is read by the words of its source locale, in the
// order that language writes a quantity, with the language's own digits and
// connectives.
func TestLinesInEveryLanguageReadWithTheirOwnUnits(t *testing.T) {
	t.Parallel()

	const (
		gram       = "mass.gram"
		kilo       = "mass.kilogram"
		milli      = "volume.milliliter"
		deci       = "volume.deciliter"
		tablespoon = "volume.tablespoon.metric.culinary"
		teaspoon   = "volume.teaspoon.metric.culinary"
		item       = "count.item"
		clove      = "count.clove"
	)

	for _, test := range []struct {
		locale, line string
		want         string
		name, note   string
		optional     bool
	}{
		{locale: "de", line: "200 g Mehl", want: "200 " + gram, name: "Mehl"},
		{locale: "de", line: "2 EL Olivenöl", want: "2 " + tablespoon, name: "Olivenöl"},
		{locale: "de", line: "1 TL Salz, fein", want: "1 " + teaspoon, name: "Salz", note: "fein"},
		{locale: "de", line: "2 Zehen Knoblauch", want: "2 " + clove, name: "Knoblauch"},
		{locale: "de", line: "ca. 500 ml Wasser", want: "~500 " + milli, name: "Wasser"},
		{locale: "de", line: "2 bis 3 EL Zucker", want: "2-3 " + tablespoon, name: "Zucker"},
		{locale: "de", line: "1,5 kg Kartoffeln", want: "3/2 " + kilo, name: "Kartoffeln"},
		{locale: "de", line: "1 Bund Petersilie (optional)", want: "1 count.bunch", name: "Petersilie", optional: true},
		{locale: "fr", line: "200 g de sucre", want: "200 " + gram, name: "sucre"},
		{locale: "fr", line: "2 cuillères à soupe d'huile d'olive", want: "2 " + tablespoon, name: "huile d'olive"},
		{locale: "fr", line: "1 cuillère à café de sel", want: "1 " + teaspoon, name: "sel"},
		{locale: "fr", line: "2 à 3 gousses d'ail", want: "2-3 " + clove, name: "ail"},
		{locale: "fr-CA", line: "250 ml de lait", want: "250 " + milli, name: "lait"},
		{locale: "es", line: "2 cucharadas de aceite", want: "2 " + tablespoon, name: "aceite"},
		{locale: "es", line: "1 diente de ajo", want: "1 " + clove, name: "ajo"},
		{locale: "it", line: "200 g di farina", want: "200 " + gram, name: "farina"},
		{locale: "it", line: "1 spicchio d'aglio", want: "1 " + clove, name: "aglio"},
		{locale: "pt-BR", line: "2 colheres de sopa de azeite", want: "2 " + tablespoon, name: "azeite"},
		{locale: "pt-PT", line: "200 g de farinha", want: "200 " + gram, name: "farinha"},
		{locale: "nl", line: "2 eetlepels olie", want: "2 " + tablespoon, name: "olie"},
		{locale: "sv", line: "2 dl mjölk", want: "2 " + deci, name: "mjölk"},
		{locale: "da", line: "2 spsk olie", want: "2 " + tablespoon, name: "olie"},
		{locale: "da", line: "2 spskf. olie", want: "2 " + tablespoon, name: "olie"},
		{locale: "da", line: "1 tskf. salt", want: "1 " + teaspoon, name: "salt"},
		{locale: "sv", line: "1 klyfta vitlök", want: "1 " + clove, name: "vitlök"},
		{locale: "it", line: "1 scatola di pomodori pelati", want: "1 container.can", name: "pomodori pelati"},
		{locale: "zh-Hans", line: "大蒜 3瓣", want: "3 " + clove, name: "大蒜"},
		{locale: "fi", line: "2 cm inkivääriä", want: "2 length.centimeter", name: "inkivääriä"},
		{locale: "fi", line: "5 millimetriä inkivääriä", want: "5 length.millimeter", name: "inkivääriä"},
		{locale: "nb", line: "2 ss olje", want: "2 " + tablespoon, name: "olje"},
		{locale: "fi", line: "2 rkl öljyä", want: "2 " + tablespoon, name: "öljyä"},
		{locale: "pl", line: "2 łyżki oleju", want: "2 " + tablespoon, name: "oleju"},
		{locale: "cs", line: "2 lžíce oleje", want: "2 " + tablespoon, name: "oleje"},
		{locale: "hu", line: "2 evőkanál olaj", want: "2 " + tablespoon, name: "olaj"},
		{locale: "ro", line: "2 linguri de ulei", want: "2 " + tablespoon, name: "ulei"},
		{locale: "ru", line: "200 г муки", want: "200 " + gram, name: "муки"},
		{locale: "ru", line: "2 столовые ложки масла", want: "2 " + tablespoon, name: "масла"},
		{locale: "uk", line: "2 ст. л. олії", want: "2 " + tablespoon, name: "олії"},
		{locale: "el", line: "2 κουταλιές της σούπας λάδι", want: "2 " + tablespoon, name: "λάδι"},
		{locale: "ar", line: "٢٠٠ غرام طحين", want: "200 " + gram, name: "طحين"},
		{locale: "ar", line: "2 ملعقة كبيرة زيت", want: "2 " + tablespoon, name: "زيت"},
		{locale: "he", line: "2 כפות שמן", want: "2 " + tablespoon, name: "שמן"},
		{locale: "vi", line: "2 muỗng canh dầu ăn", want: "2 " + tablespoon, name: "dầu ăn"},
		{locale: "id", line: "2 sendok makan minyak", want: "2 " + tablespoon, name: "minyak"},
		{locale: "th", line: "แป้ง 200 กรัม", want: "200 " + gram, name: "แป้ง"},
		{locale: "th", line: "น้ำตาล 2 ช้อนโต๊ะ", want: "2 " + tablespoon, name: "น้ำตาล"},
		{locale: "ja", line: "小麦粉 200g", want: "200 " + gram, name: "小麦粉"},
		{locale: "ja", line: "サラダ油 大さじ2", want: "2 " + tablespoon, name: "サラダ油"},
		{locale: "ja", line: "牛乳 カップ1/2", want: "1/2 volume.cup.jp", name: "牛乳"},
		{locale: "ja", line: "大さじ2 醤油", want: "2 " + tablespoon, name: "醤油"},
		{locale: "ja", line: "卵 2個", want: "2 " + item, name: "卵"},
		{locale: "ja", line: "にんにく1かけ", want: "1 " + clove, name: "にんにく"},
		{locale: "ja", line: "塩 ひとつまみ", want: "1 count.pinch", name: "塩"},
		{locale: "ja", line: "パセリ ひとつかみ", want: "1 count.handful", name: "パセリ"},
		{locale: "id", line: "sejumput garam", want: "1 count.pinch", name: "garam"},
		{locale: "id", line: "segenggam kacang", want: "1 count.handful", name: "kacang"},
		{locale: "id", line: "2 sejumput garam", want: "2 count.pinch", name: "garam"},
		{locale: "ja", line: "食パン 1枚", want: "1 " + item, name: "食パン"},
		{locale: "ja", line: "鶏肉 2切れ", want: "2 " + item, name: "鶏肉"},
		{locale: "zh-Hans", line: "火腿 2片", want: "2 " + item, name: "火腿"},
		{locale: "ja", line: "じゃがいも 約3個", want: "~3 " + item, name: "じゃがいも"},
		{locale: "ja", line: "水 200ml（お好みで）", want: "200 " + milli, name: "水", optional: true},
		{locale: "ja", line: "小麦粉 200g（ふるう）", want: "200 " + gram, name: "小麦粉", note: "ふるう"},
		{locale: "ja", line: "玉ねぎ 1個、みじん切り", want: "1 " + item, name: "玉ねぎ", note: "みじん切り"},
		{locale: "zh-Hans", line: "面粉 200克（过筛）", want: "200 " + gram, name: "面粉", note: "过筛"},
		{locale: "ko", line: "밀가루 200g (체에 친 것)", want: "200 " + gram, name: "밀가루", note: "체에 친 것"},
		{locale: "th", line: "แป้งสาลี 200 กรัม (ร่อน)", want: "200 " + gram, name: "แป้งสาลี", note: "ร่อน"},
		{locale: "ko", line: "밀가루 200g", want: "200 " + gram, name: "밀가루"},
		{locale: "ko", line: "간장 2큰술", want: "2 " + tablespoon, name: "간장"},
		{locale: "ko", line: "우유 1컵", want: "1 volume.cup.kr", name: "우유"},
		{locale: "ko", line: "마늘 3쪽", want: "3 " + clove, name: "마늘"},
		{locale: "zh-Hans", line: "面粉 200克", want: "200 " + gram, name: "面粉"},
		{locale: "zh-Hans", line: "面粉200克", want: "200 " + gram, name: "面粉"},
		{locale: "zh-Hans", line: "酱油 2汤匙", want: "2 " + tablespoon, name: "酱油"},
		{locale: "zh-Hant", line: "糖 1大匙", want: "1 " + tablespoon, name: "糖"},
		{locale: "zh-Hant-TW", line: "水 １００毫升", want: "100 " + milli, name: "水"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if got := reading(parsed); got != test.want || parsed.Name != test.name || parsed.Note != test.note || parsed.Optional != test.optional {
			t.Errorf("%s %q read as %s, name %q, note %q, optional %v; want %s, %q, %q, %v",
				test.locale, test.line, got, parsed.Name, parsed.Note, parsed.Optional,
				test.want, test.name, test.note, test.optional)
		}

		if parsed.SourceText != test.line {
			t.Errorf("%s %q lost its source text: %q", test.locale, test.line, parsed.SourceText)
		}
	}
}

func TestAMeasureWithNoSingleSizeStaysOpaque(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line string }{
		{"de", "1 Tasse Milch"},
		{"de", "1 Pfund Mehl"},
		{"fr", "1 tasse de lait"},
		{"fr", "1 verre d'eau"},
		{"es", "1 taza de leche"},
		{"es", "1 libra de harina"},
		{"it", "1 tazza di latte"},
		{"pt-PT", "1 chávena de leite"},
		{"nl", "1 kop melk"},
		{"nl", "1 ons kaas"},
		{"sv", "1 kopp mjölk"},
		{"da", "1 kop mælk"},
		{"nb", "1 kopp melk"},
		{"fi", "1 kuppi maitoa"},
		{"pl", "1 szklanka mleka"},
		{"cs", "1 hrnek mléka"},
		{"hu", "1 bögre tej"},
		{"ro", "1 cană de lapte"},
		{"tr", "1 su bardağı süt"},
		{"ru", "1 стакан молока"},
		{"uk", "1 склянка молока"},
		{"el", "1 φλιτζάνι γάλα"},
		{"ar", "1 كوب حليب"},
		{"he", "1 כוס חלב"},
		{"th", "นม 1 ถ้วย"},
		{"vi", "1 chén sữa"},
		{"id", "1 cangkir susu"},
		{"zh", "牛奶 1杯"},
		{"ja", "牛乳 1杯"},
		{"ko", "우유 1되"},
		// A cup the source describes as another kind of cup is not the registry's.
		{"ko", "물 종이컵 1컵"},
		{"ko", "밀가루 1컵 (종이컵)"},
		{"ja", "米 炊飯器のカップ1"},
		{"ja", "水 紙コップ1杯"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if parsed.Quantity != nil || parsed.Normalization == nil || parsed.Normalization.Method != recipe.NormalizationUnresolved {
			t.Errorf("%s %q read as %s, want it left as written", test.locale, test.line, reading(parsed))
		}
	}
}

func TestCupWordsOfferBothCulinaryReadings(t *testing.T) {
	t.Parallel()

	middleEastern := []string{"volume.cup.metric.culinary", "volume.cup.us.culinary"}
	greek := []string{"volume.cup.gr", "volume.cup.metric.culinary"}

	for _, test := range []struct {
		locale, line, name string
		want               []string
	}{
		{"he-IL", "1 כוס גבינת מוצרלה", "גבינת מוצרלה", middleEastern},
		{"he-IL", "2 כוסות קישואים", "קישואים", middleEastern},
		{"ar-SA", "1 كوب ماء دافئ", "ماء دافئ", middleEastern},
		{"ar-EG", "3 أكواب طحين", "طحين", middleEastern},
		{"el", "1 φλιτζάνι τσαγιού αλεύρι", "αλεύρι", greek},
		{"el-GR", "2 κούπες γάλα", "γάλα", greek},
	} {
		want := test.want

		parsed := ingredientparser.ParseDetailed(test.line, "i1", test.locale)
		if parsed.Ingredient.Quantity != nil || len(parsed.Ambiguities) != 1 {
			t.Fatalf("%s: result = %#v", test.line, parsed)
		}

		ambiguity := parsed.Ambiguities[0]
		if !slices.Equal(ambiguity.CandidateUnitIDs, want) {
			t.Fatalf("%s: candidates = %v", test.line, ambiguity.CandidateUnitIDs)
		}

		for _, unitID := range want {
			resolved := ingredientparser.ParseWithContext(test.line, "i1", test.locale, interpreting(ambiguity, unitID))
			if resolved.Ingredient.Quantity == nil || resolved.Ingredient.Quantity.Unit != unitID ||
				resolved.Ingredient.Name != test.name || resolved.Ingredient.SourceText != test.line || len(resolved.Ambiguities) != 0 {
				t.Errorf("%s as %s: result = %#v", test.line, unitID, resolved)
			}
		}
	}
}

// A language's words do not leak into another's reading: the English source
// does not read a German unit, and the German source does not read English.
func TestOneLanguageDoesNotReadAnothers(t *testing.T) {
	t.Parallel()

	if parsed := ingredientparser.Parse("2 EL flour", "i1", "en-US"); parsed.Quantity != nil {
		t.Errorf("an English source read a German unit: %s", reading(parsed))
	}

	if parsed := ingredientparser.Parse("2 tbsp Mehl", "i1", "de"); parsed.Quantity != nil {
		t.Errorf("a German source read an English unit: %s", reading(parsed))
	}
}

// The sample line the editor shows in an empty ingredient field is written in
// each language, and each one has to be a line that language reads.
func TestEveryLocalizedExampleLineReadsInItsOwnLanguage(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob(filepath.Join("..", "..", "web", "messages", "*.json"))
	if err != nil || len(files) == 0 {
		t.Skip("the web message catalogs are not beside this package")
	}

	checked := 0

	for _, file := range files {
		locale := strings.TrimSuffix(filepath.Base(file), ".json")
		if locale == "context" {
			continue
		}

		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		var catalog map[string]any
		if err := json.Unmarshal(data, &catalog); err != nil {
			t.Fatal(err)
		}

		example, _ := catalog["ingredient_row_line_example"].(string)
		if example == "" {
			t.Errorf("%s has no example ingredient line", locale)

			continue
		}

		checked++

		parsed := ingredientparser.Parse(example, "i1", locale)
		if reading(parsed) != "200 mass.gram" || parsed.Name == "" || parsed.Note == "" {
			t.Errorf("%s example %q read as %s, name %q, note %q; want 200 g, a name, and a note", locale, example, reading(parsed), parsed.Name, parsed.Note)
		}
	}

	if checked < 29 {
		t.Errorf("only %d locales carry an example line", checked)
	}
}

// A broad word is read as a unit when the ingredient after it selects that
// sense, and not otherwise. The bare word has no alias, so the same word with
// another ingredient keeps its wording as a count of the ingredient.
func TestABroadWordIsReadInTheSenseItsIngredientSelects(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		locale, line string
		want         string
		name         string
	}{
		{"fr", "2 gousses d'ail", "2 count.clove", "ail"},
		{"fr", "1 gousse de vanille", "1 count.item", "gousse de vanille"},
		{"sv", "1 klyfta vitlök", "1 count.clove", "vitlök"},
		{"sv", "2 klyftor citron", "2 count.item", "klyftor citron"},
		{"it", "1 spicchio d'aglio", "1 count.clove", "aglio"},
		{"it", "2 spicchi di limone", "2 count.item", "spicchi di limone"},
		{"it", "1 scatola di pomodori pelati", "1 container.can", "pomodori pelati"},
		{"it", "1 scatola di cioccolatini", "1 count.item", "scatola di cioccolatini"},
		{"fr", "1 boîte de bonbons", "1 count.item", "boîte de bonbons"},
		{"fr", "1 boîte de conserve de tomates", "1 container.can", "tomates"},
		{"fr", "2 boîtes de conserve de maïs", "2 container.can", "maïs"},
		{"fr-CA", "1 boite de conserve de thon", "1 container.can", "thon"},
		{"ko", "마늘 3쪽", "3 count.clove", "마늘"},
		{"zh-Hans", "蒜 2瓣", "2 count.clove", "蒜"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if got := reading(parsed); got != test.want || parsed.Name != test.name {
			t.Errorf("%s %q read as %s, name %q; want %s, %q", test.locale, test.line, got, parsed.Name, test.want, test.name)
		}
	}

	// A piece or segment of something else is not a clove, whatever the script.
	for _, test := range []struct{ locale, line string }{
		{"ko", "사과 1쪽"}, {"zh-Hans", "橙子 2瓣"},
	} {
		if parsed := ingredientparser.Parse(test.line, "i1", test.locale); parsed.Quantity != nil && parsed.Quantity.Unit == "count.clove" {
			t.Errorf("%s %q was read as a clove", test.locale, test.line)
		}
	}
}

// A Finnish mm may be the one-millilitre spice measure, so it is not read as a
// length on the language alone. The length words and the other length symbols
// are unaffected.
func TestAFinnishMmIsNotReadAsALengthFromTheLanguageAlone(t *testing.T) {
	t.Parallel()

	parsed := ingredientparser.Parse("1 mm suolaa", "i1", "fi")
	if parsed.Quantity != nil || parsed.Normalization == nil || parsed.Normalization.Method != recipe.NormalizationUnresolved {
		t.Errorf("fi %q read as %s, want it left as written", parsed.SourceText, reading(parsed))
	}

	if got := reading(ingredientparser.Parse("5 millimetriä inkivääriä", "i1", "fi")); got != "5 length.millimeter" {
		t.Errorf("a millimetre written out read as %s", got)
	}
}

// A source that states the size of its own spoon is not overwritten by the
// registry's size for the word. A stated equivalent that agrees with the
// registry, allowing for rounding, is kept beside the measure.
func TestAStatedEquivalentThatDisagreesWithTheRegistryLeavesTheLineAsWritten(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line string }{
		{"nl", "1 eetlepel (12 ml) olie"},
		{"nl", "1 eetlepel / 12 ml olie"},
		{"it", "1 cucchiaio (10 ml) di olio"},
		{"hu", "1 evőkanál (10 ml) olaj"},
		{"de", "1 EL (10 ml) Öl"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if parsed.Quantity != nil || parsed.Normalization == nil || parsed.Normalization.Method != recipe.NormalizationUnresolved {
			t.Errorf("%s %q read as %s, want it left as written", test.locale, test.line, reading(parsed))
		}
	}

	for _, test := range []struct{ locale, line string }{
		{"de", "1 EL (15 ml) Öl"},
		{"en-US", "1 cup (240 ml) flour"},
		{"en-US", "1 tablespoon (15 ml) oil"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if parsed.Quantity == nil || parsed.Quantity.Kind != recipe.QuantityAlternatives {
			t.Errorf("%s %q read as %#v, want the measure and its equivalent", test.locale, test.line, parsed.Quantity)
		}
	}
}

func TestASpoonWordWithCompetingConventionsStaysAsWritten(t *testing.T) {
	t.Parallel()

	for locale, lines := range map[string][]string{
		"pt":    {"1 cc de fermento", "1 Cc de sal"},
		"pt-BR": {"2 cc de açúcar"},
		"tr":    {"1 tatlı kaşığı salça", "1 tk salça"},
	} {
		for _, line := range lines {
			parsed := ingredientparser.Parse(line, "i1", locale)
			if parsed.Quantity != nil || parsed.Normalization == nil || parsed.Normalization.Method != recipe.NormalizationUnresolved {
				t.Errorf("%s %q read as %s, want it left as written", locale, line, reading(parsed))
			}

			if parsed.SourceText != line {
				t.Errorf("%s %q lost its source text: %q", locale, line, parsed.SourceText)
			}
		}
	}

	for _, test := range []struct{ locale, line, want string }{
		{"pt-BR", "2 colheres de sopa de azeite", "2 volume.tablespoon.metric.culinary"},
		{"pt-BR", "1 colher de chá de sal", "1 volume.teaspoon.metric.culinary"},
		{"pt", "1 xícara de leite", "1 volume.cup.us.culinary"},
		{"tr", "1 çay kaşığı tuz", "1 volume.teaspoon.metric.culinary"},
		{"tr", "1 çk kimyon", "1 volume.teaspoon.metric.culinary"},
		{"tr", "2 yemek kaşığı yağ", "2 volume.tablespoon.metric.culinary"},
		{"tr", "1 YK Sirke", "1 volume.tablespoon.metric.culinary"},
		{"tr", "2 ç.k. karabiber", "2 volume.teaspoon.metric.culinary"},
		{"tr", "3 y.k. su", "3 volume.tablespoon.metric.culinary"},
	} {
		if got := reading(ingredientparser.Parse(test.line, "i1", test.locale)); got != test.want {
			t.Errorf("%s %q read as %s, want %s", test.locale, test.line, got, test.want)
		}
	}
}

// The line is compared in canonical composition and no other normalization:
// a decomposed spelling of a registered form reads, and a compatibility variant
// does not. The text as written is kept.
func TestAliasMatchingComparesCanonicalCompositionOnly(t *testing.T) {
	t.Parallel()

	decomposed := "1 cuille\u0300re a\u0300 cafe\u0301 de sel"
	if got := reading(ingredientparser.Parse(decomposed, "i1", "fr")); got != "1 volume.teaspoon.metric.culinary" {
		t.Errorf("a decomposed spelling read as %s", got)
	}

	if parsed := ingredientparser.Parse(decomposed, "i1", "fr"); parsed.SourceText != decomposed {
		t.Errorf("the source text was rewritten: %q", parsed.SourceText)
	}

	for _, test := range []struct{ locale, line string }{
		{"ja", "牛乳 100ｍｌ"},
		{"en", "350 ℉ oven"},
		{"fr", "1 cuillere a cafe de sel"},
		{"ru", "200 cм муки"},
	} {
		if parsed := ingredientparser.Parse(test.line, "i1", test.locale); parsed.Quantity != nil && parsed.Quantity.Unit != "count.item" {
			t.Errorf("%s %q read as %s, want no unit", test.locale, test.line, reading(parsed))
		}
	}
}

func TestNameFirstLinesWithLeadersAndRanges(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line, want, name string }{
		{"ja", "片栗粉 … 小さじ1～2", "1-2 volume.teaspoon.metric.culinary", "片栗粉"},
		{"ja", "みそ … 大さじ２", "2 volume.tablespoon.metric.culinary", "みそ"},
		{"ja", "米 2合", "2 volume.go.jp", "米"},
		{"ko", "식용유1T", "1 volume.tablespoon.metric.culinary", "식용유"},
		{"zh", "青蔥 2支", "2 count.item", "青蔥"},
		{"th", "หอมแดง 3 หัว", "3 count.head", "หอมแดง"},
		{"ja", "揚げ油 … 適量", "no quantity", "揚げ油"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if got := reading(parsed); got != test.want || parsed.Name != test.name || parsed.SourceText != test.line {
			t.Errorf("%s %q: %s, name %q", test.locale, test.line, got, parsed.Name)
		}
	}
}

func TestAQuantityAfterTheNameThatReadsNoUnitStaysAsWritten(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line string }{
		{"ja", "牛乳 100ｍｌ"},
		{"th", "เนื้อหมูสับ 1 จาน"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if parsed.Quantity != nil || parsed.Name != test.line {
			t.Errorf("%s %q read as %s, name %q", test.locale, test.line, reading(parsed), parsed.Name)
		}
	}
}

func TestAnAmountAndUnitAfterTheNameRead(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line, want, name string }{
		{"en", "Beer 250 ml", "250 volume.milliliter", "Beer"},
		{"en-US", "Baking Powder 2½ tsp", "5/2 volume.teaspoon.us.customary", "Baking Powder"},
		{"de", "Mehl 500 g", "500 mass.gram", "Mehl"},
		{"ru", "Картофель — 1 кг", "1 mass.kilogram", "Картофель"},
		{"en", "Vitamin B 12", "no quantity", "Vitamin B 12"},
		{"en", "Burger Buns 4", "no quantity", "Burger Buns 4"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if got := reading(parsed); got != test.want || parsed.Name != test.name {
			t.Errorf("%s %q: %s, name %q", test.locale, test.line, got, parsed.Name)
		}
	}
}

func TestAnAmountAfterASeparatorReadsAsTheIngredientsQuantity(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line, want, name string }{
		{"ru", "картофель — 3-4 шт.;", "3-4 count.item", "картофель"},
		{"ru", "соленые огурцы — около 200 г;", "~200 mass.gram", "соленые огурцы"},
		{"ro", "Bibilică: 1,7 kg", "17/10 mass.kilogram", "Bibilică"},
		{"ja", "豚バラ肉 (スライス)・・・200g", "200 mass.gram", "豚バラ肉 (スライス)"},
		{"en", "Eggs - 2", "2 count.egg", "Eggs"},
		{"ru", "Чеснок - 2 зубч.", "2 count.clove", "Чеснок"},
		{"ru", "Мука - 2 стак.", "no quantity", "Мука - 2 стак"},
		{"en", "Note: 2 eggs work too", "no quantity", "Note: 2 eggs work too"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if got := reading(parsed); got != test.want || parsed.Name != test.name {
			t.Errorf("%s %q: %s, name %q", test.locale, test.line, got, parsed.Name)
		}
	}
}

func TestAMeasureWithoutANumberStatesOne(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line, want, name string }{
		{"en", "Pinch white pepper", "1 count.pinch", "white pepper"},
		{"pl", "łyżka mąki pszennej", "1 volume.tablespoon.metric.culinary", "mąki pszennej"},
		{"he", "כפית מלח", "1 volume.teaspoon.metric.culinary", "מלח"},
		{"ar", "رشة من الملح.", "1 count.pinch", "الملح"},
		{"en", "Gram flour", "no quantity", "Gram flour"},
		{"en", "Pot roast", "no quantity", "Pot roast"},
		{"ja", "大さじ2 醤油", "2 volume.tablespoon.metric.culinary", "醤油"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if got := reading(parsed); got != test.want || parsed.Name != test.name {
			t.Errorf("%s %q: %s, name %q", test.locale, test.line, got, parsed.Name)
		}
	}
}

func TestAFractionWordIsTheAmount(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line, want, name string }{
		{"he", "חצי כפית מלח", "1/2 volume.teaspoon.metric.culinary", "מלח"},
		{"ar", "نصف ملعقة صغيرة ملح", "1/2 volume.teaspoon.metric.culinary", "ملح"},
		{"tr", "Yarım limon", "1/2 count.item", "limon"},
		{"de", "eine halbe Zitrone", "1/2 count.item", "Zitrone"},
		{"cs", "půl lžičky soli", "1/2 volume.teaspoon.metric.culinary", "soli"},
		{"fi", "puoli litraa maitoa", "1/2 volume.liter", "maitoa"},
		{"id", "setengah sdt garam", "1/2 volume.teaspoon.metric.culinary", "garam"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if got := reading(parsed); got != test.want || parsed.Name != test.name {
			t.Errorf("%s %q: %s, name %q", test.locale, test.line, got, parsed.Name)
		}
	}
}

func TestAnAmountAfterACommaReadsAsTheIngredientsQuantity(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line, want, name string }{
		{"uk", "Цукор, 500 г", "500 mass.gram", "Цукор"},
		{"ru", "Оливковое масло, 1 столовая ложка", "1 volume.tablespoon.metric.culinary", "Оливковое масло"},
		{"en", "Sugar , 450 g", "450 mass.gram", "Sugar"},
		{"en", "Candied ginger, chopped 40 g", "40 mass.gram", "Candied ginger"},
		{"en", "Tomatoes, chopped", "no quantity", "Tomatoes"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if got := reading(parsed); got != test.want || parsed.Name != test.name {
			t.Errorf("%s %q: %s, name %q", test.locale, test.line, got, parsed.Name)
		}
	}
}

func TestLinePunctuationAndOrderVariantsRead(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line, want, name string }{
		{"pl", "500 ml mleka 3,2%", "500 volume.milliliter", "mleka 3,2%"},
		{"ja", "合い挽き肉 100g", "100 mass.gram", "合い挽き肉"},
		{"he", "כפית כמון טחון 1/4", "1/4 volume.teaspoon.metric.culinary", "כמון טחון"},
		{"ro", "-6 galbenusuri", "6 count.item", "galbenusuri"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if got := reading(parsed); got != test.want || parsed.Name != test.name {
			t.Errorf("%s %q: %s, name %q", test.locale, test.line, got, parsed.Name)
		}
	}
}

func TestAConnectiveBetweenNumberAndUnitIsNotTheName(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line, want, name string }{
		{"ro", "300 de grame de nucă măcinată", "300 mass.gram", "nucă măcinată"},
		{"pt-BR", "1/4 de xícara de parmesão", "1/4 volume.cup.us.culinary", "parmesão"},
		{"ar", "400 غرام من المكرونة", "400 mass.gram", "المكرونة"},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if got := reading(parsed); got != test.want || parsed.Name != test.name {
			t.Errorf("%s %q: %s, name %q", test.locale, test.line, got, parsed.Name)
		}
	}
}

func TestLineDecorationIsNotPartOfTheIngredient(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ locale, line, name, note string }{
		{"en", "1 onion finely chopped", "onion", "finely chopped"},
		{"ja", "・オリーブ油 大さじ1", "オリーブ油", ""},
		{"he", "\u200eכפית סוכר\u200e", "סוכר", ""},
		{"it", "Sale, q.b.", "Sale", "q.b."},
	} {
		parsed := ingredientparser.Parse(test.line, "i1", test.locale)
		if parsed.Name != test.name || parsed.Note != test.note || parsed.SourceText != test.line {
			t.Errorf("%s %q: name %q, note %q, source %q", test.locale, test.line, parsed.Name, parsed.Note, parsed.SourceText)
		}
	}
}

func TestAStatedEnglishIsCheckedAgainstTheIngredients(t *testing.T) {
	t.Parallel()

	turkish := []string{"1 su bardağı yoğurt", "2 yemek kaşığı şeker", "1 çay kaşığı tuz", "1 yemek kaşığı tereyağı", "3 adet yumurta"}
	if got := ingredientparser.ReadingLanguage(turkish, "en"); got != "tr" {
		t.Errorf("Turkish lines stated as English read as %q", got)
	}

	english := []string{"2 cups flour", "1 tablespoon sugar", "200 g butter", "3 eggs"}
	if got := ingredientparser.ReadingLanguage(english, "en-US"); got != "en-US" {
		t.Errorf("English lines read as %q", got)
	}

	if got := ingredientparser.ReadingLanguage(turkish, "de"); got != "de" {
		t.Errorf("another stated language is kept, got %q", got)
	}
}
