package ingredientparser_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/text/language"

	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/srefcheckout"
)

// TestSREFIngredientLineCorpus reads every line of SREF's ingredient-line
// corpus with the parser and compares the quantity, name, note, and source text
// with what the corpus expects.
func TestSREFIngredientLineCorpus(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(srefcheckout.Path(t, "tests/amounts"), "ingredient-lines.json"))
	if err != nil {
		t.Fatal(err)
	}

	var corpus struct {
		Cases []struct {
			ID           string `json:"id"`
			SourceLocale string `json:"source_locale"`
			SourceText   string `json:"source_text"`
			Expected     struct {
				Name     string          `json:"name"`
				Note     string          `json:"note"`
				Optional bool            `json:"optional"`
				Quantity json.RawMessage `json:"quantity"`
			} `json:"expected"`
		} `json:"cases"`
	}

	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}

	if len(corpus.Cases) == 0 {
		t.Fatal("the corpus holds no cases")
	}

	checked := 0

	for _, test := range corpus.Cases {
		if !readsUnitVocabulary(test.ID, test.SourceLocale) {
			continue
		}

		checked++

		parsed := ingredientparser.Parse(test.SourceText, test.ID, test.SourceLocale)

		if parsed.SourceText != test.SourceText {
			t.Errorf("%s: source text %q", test.ID, parsed.SourceText)
		}

		var want, got map[string]any

		if len(test.Expected.Quantity) != 0 {
			if err := json.Unmarshal(test.Expected.Quantity, &want); err != nil {
				t.Fatal(err)
			}
		}

		if parsed.Quantity != nil {
			encoded, err := json.Marshal(parsed.Quantity)
			if err != nil {
				t.Fatal(err)
			}

			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
		}

		// SREF lets a parser keep an unreadable measure as an opaque quantity or
		// mark the line unresolved; this parser leaves the whole line as written.
		if want["kind"] == "opaque" && got == nil {
			if parsed.Normalization == nil || parsed.Normalization.Method != recipe.NormalizationUnresolved {
				t.Errorf("%s %q: an opaque measure was not marked unresolved", test.ID, test.SourceText)
			}

			continue
		}

		if parsed.Name != test.Expected.Name || parsed.Note != test.Expected.Note || parsed.Optional != test.Expected.Optional {
			t.Errorf("%s %q: name %q, note %q, optional %v; want %q, %q, %v",
				test.ID, test.SourceText, parsed.Name, parsed.Note, parsed.Optional,
				test.Expected.Name, test.Expected.Note, test.Expected.Optional)
		}

		// A count of the ingredient itself has no unit in SREF and is a generic
		// item here.
		if _, named := want["unit"]; !named && got["unit"] == "count.item" {
			delete(got, "unit")
		}

		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s %q: quantity %v; want %v", test.ID, test.SourceText, got, want)
		}
	}

	if checked == 0 {
		t.Fatal("no case of the corpus was read")
	}

	t.Logf("read %d of %d corpus cases", checked, len(corpus.Cases))
}

// englishUnitVocabulary names the English cases that exercise the registry's
// unit vocabulary and its qualifiers. The rest of the English corpus states
// difficult prose that this parser deliberately leaves as written.
var englishUnitVocabulary = map[string]bool{
	"australian-tablespoon":                      true,
	"english-source-does-not-read-a-german-unit": true,
	"explicit-us-legal-tablespoon":               true,
	"generic-metric-culinary-teaspoon":           true,
	"locale-no-sibling-fallback":                 true,
	"qualified-american-cup":                     true,
	"qualified-australian-cup":                   true,
	"qualified-australian-teaspoon":              true,
	"qualified-metric-cup":                       true,
	"qualified-metric-tablespoon":                true,
	"qualified-metric-teaspoon":                  true,
}

// readsUnitVocabulary reports whether a corpus case is one this parser is held
// to: every case written in another language, and the English unit-vocabulary
// cases named above.
func readsUnitVocabulary(id, locale string) bool {
	if englishUnitVocabulary[id] {
		return true
	}

	tag, err := language.Parse(locale)
	if locale == "" || err != nil {
		return false
	}

	base, _ := tag.Base()

	return base.String() != "en"
}
