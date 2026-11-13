package ingredientparser_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/srefcheckout"
)

func TestSREFIngredientLineCorpus(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(srefcheckout.Path(t, "tests/amounts"), "ingredient-lines.json"))
	if err != nil {
		t.Fatal(err)
	}

	var corpus struct {
		Cases []corpusCase `json:"cases"`
	}

	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}

	if len(corpus.Cases) == 0 {
		t.Fatal("the corpus holds no cases")
	}

	for _, test := range corpus.Cases {
		problems := corpusProblems(t, test)
		if knownCorpusGaps[test.ID] {
			if len(problems) == 0 {
				t.Errorf("%s now reads as SREF expects; remove it from knownCorpusGaps", test.ID)
			}

			continue
		}

		for _, problem := range problems {
			t.Error(problem)
		}
	}
}

type corpusCase struct {
	ID           string `json:"id"`
	SourceLocale string `json:"source_locale"`
	SourceText   string `json:"source_text"`
	Expected     struct {
		Name        string          `json:"name"`
		Note        string          `json:"note"`
		Optional    bool            `json:"optional"`
		Quantity    json.RawMessage `json:"quantity"`
		PackageSize json.RawMessage `json:"package_size"`
	} `json:"expected"`
}

func corpusProblems(t *testing.T, test corpusCase) []string {
	t.Helper()

	var problems []string

	parsed := ingredientparser.Parse(test.SourceText, test.ID, test.SourceLocale)

	if parsed.SourceText != test.SourceText {
		problems = append(problems, fmt.Sprintf("%s: source text %q", test.ID, parsed.SourceText))
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

	// Accept an unresolved line where the corpus permits an opaque quantity.
	if want["kind"] == "opaque" && got == nil {
		if parsed.Normalization == nil || parsed.Normalization.Method != recipe.NormalizationUnresolved {
			problems = append(problems, fmt.Sprintf("%s %q: an opaque measure was not marked unresolved", test.ID, test.SourceText))
		}

		return problems
	}

	if parsed.Name != test.Expected.Name || parsed.Note != test.Expected.Note || parsed.Optional != test.Expected.Optional {
		problems = append(problems, fmt.Sprintf("%s %q: name %q, note %q, optional %v; want %q, %q, %v",
			test.ID, test.SourceText, parsed.Name, parsed.Note, parsed.Optional,
			test.Expected.Name, test.Expected.Note, test.Expected.Optional))
	}

	// SREF represents count.item by omitting the unit.
	if _, named := want["unit"]; !named && got["unit"] == "count.item" {
		delete(got, "unit")
	}

	if !reflect.DeepEqual(want, got) {
		problems = append(problems, fmt.Sprintf("%s %q: quantity %v; want %v", test.ID, test.SourceText, got, want))
	}

	if wantSize, gotSize := decoded(t, test.Expected.PackageSize), encoded(t, parsed.PackageSize); !reflect.DeepEqual(wantSize, gotSize) {
		problems = append(problems, fmt.Sprintf("%s %q: package size %v; want %v", test.ID, test.SourceText, gotSize, wantSize))
	}

	return problems
}

var knownCorpusGaps = map[string]bool{
	"can-size-code":               true,
	"gelatin-sheet":               true,
	"included-scoop":              true,
	"preferred-equivalence":       true,
	"stick-butter-annotated-mass": true,
	"tea-bags":                    true,
	"tortilla-diameter":           true,
}

func decoded(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()

	if len(raw) == 0 {
		return nil
	}

	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}

	return value
}

func encoded(t *testing.T, value *recipe.SimpleQuantity) map[string]any {
	t.Helper()

	if value == nil {
		return nil
	}

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	return decoded(t, raw)
}
