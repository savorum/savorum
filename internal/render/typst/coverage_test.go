package typst

import "testing"

func TestCoverageMergesOverlappingRangesBeforeBinarySearch(t *testing.T) {
	coverage := &coverage{ranges: mergeRanges([]runeRange{
		{low: 'A', high: 'z'},
		{low: 'M', high: '⅘'},
	})}
	if !coverage.covers('⅘') {
		t.Fatal("merged coverage lost a glyph from an overlapping fallback range")
	}
}

func TestBundledFontsCoverEmojiSequencesAndRecipeSymbols(t *testing.T) {
	bundle, err := loadBundle()
	if err != nil {
		t.Fatal(err)
	}

	coverage, err := buildCoverage(bundle.fonts)
	if err != nil {
		t.Fatal(err)
	}

	if missing := coverage.Missing("🥣 🍞 🌶️ 👩🏽‍🍳 ☑ ☐ ⏲ → ⅓ − °C"); len(missing) != 0 {
		t.Fatalf("missing recipe glyphs: %U", missing)
	}

	if missing := coverage.Missing("\U000f0000"); len(missing) != 1 {
		t.Fatal("unassigned glyphs must remain unsupported")
	}
}
