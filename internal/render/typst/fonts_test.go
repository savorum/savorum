package typst

import (
	"bytes"
	"math"
	"testing"

	"github.com/savorum/savorum/internal/print"
)

func TestFontSelectionRetainsNeededFamilies(t *testing.T) {
	loaded, err := loadBundle()
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		text     string
		families []string
	}{
		{"Roast carrots 100 g", []string{"SourceSans3", "SourceSerif4"}},
		{"味噌汁", []string{"NotoSansCJKjp"}},
		{"العربية", []string{"NotoNaskhArabic"}},
		{"עברית", []string{"NotoSansHebrew"}},
		{"ภาษาไทย", []string{"NotoSansThai"}},
		{"🍋", []string{"NotoEmoji"}},
	} {
		t.Run(test.text, func(t *testing.T) {
			fonts, err := loaded.fontsFor(test.text)
			if err != nil {
				t.Fatal(err)
			}

			for index, face := range loaded.faces {
				for _, family := range test.families {
					if face.family != family {
						continue
					}

					found := false
					for _, font := range fonts {
						found = found || bytes.Equal(font, loaded.fonts[index])
					}

					if !found {
						t.Errorf("missing face from %s", family)
					}
				}
			}

			if test.text == "Roast carrots 100 g" {
				size := 0
				for _, font := range fonts {
					size += len(font)
				}

				if size > 2<<20 {
					t.Fatalf("Latin print includes %d bytes of fonts", size)
				}
			}
		})
	}
}

func TestReadableTextIncludesLocalizedAndSecondaryText(t *testing.T) {
	document := print.PrintDocument{Digits: "٠١٢٣٤٥٦٧٨٩", Labels: map[string]string{"ingredients": "材料"}, Recipes: []print.PrintRecipe{{KeptAsWritten: "כתב", Equipment: []print.PrintIngredient{{Text: "อุปกรณ์", AsWritten: "معدات"}}, StepGroups: []print.PrintStepGroup{{Steps: []print.PrintStep{{NumberText: "①"}}}}}}}

	text := readableText(document)
	for _, expected := range []string{"٠١٢٣٤٥٦٧٨٩", "材料", "כתב", "อุปกรณ์", "معدات", "①"} {
		if !bytes.Contains([]byte(text), []byte(expected)) {
			t.Errorf("missing %s", expected)
		}
	}
}

func TestCompilerMemoryLimitTracksHostBudget(t *testing.T) {
	for _, test := range []struct {
		budget int64
		want   uint64
	}{
		{math.MaxInt64, 1 << 32},
		{8 << 30, 1 << 32},
		{768 << 20, 512 << 20},
		{716 << 20, 477*1024*1024 + 5*65536},
		{358 << 20, 238*1024*1024 + 10*65536},
		{0, 65536},
	} {
		if got := compilerMemoryLimit(test.budget); got != test.want {
			t.Errorf("budget %d: got %d, want %d", test.budget, got, test.want)
		}
	}
}
