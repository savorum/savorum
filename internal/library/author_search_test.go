package library

import (
	"errors"
	"slices"
	"testing"
)

func TestAuthorQualifiersKeepEscapesAndOtherConstraints(t *testing.T) {
	t.Parallel()

	criteria, err := ParseSearchQuery(`rice AUTHOR:"Julia Child" author:"A \"B\" \\ C" tag:t1 cuisine:French book:b1 source:b2`)
	if err != nil {
		t.Fatal(err)
	}

	if criteria.Text != "rice" || !slices.Equal(criteria.Authors, []string{"Julia Child", `A "B" \ C`}) ||
		!slices.Equal(criteria.Tags, []string{"t1"}) || !slices.Equal(criteria.Facets[FacetCuisine], []string{"French"}) ||
		!slices.Equal(criteria.Books, []string{"b1"}) || !slices.Equal(criteria.Sources, []string{"b2"}) {
		t.Fatalf("criteria = %+v", criteria)
	}

	for _, query := range []string{`author:`, `author:" "`, `author:"Julia`} {
		if _, err := ParseSearchQuery(query); !errors.Is(err, ErrInvalidSearch) {
			t.Errorf("query %q error = %v", query, err)
		}
	}

	literal, err := ParseSearchQuery(`"author:Julia Child"`)
	if err != nil || len(literal.Authors) != 0 || literal.Text != "author:Julia Child" {
		t.Fatalf("quoted qualifier = %+v, %v", literal, err)
	}
}

func TestAuthorNormalizationPreservesMeaningfulMarks(t *testing.T) {
	t.Parallel()

	for _, pair := range [][2]string{{"  JULIA\tChild ", "julia child"}, {"Rene\u0301e", "Renée"}, {"Straße", "STRASSE"}, {"Σίγμα", "σίγμα"}} {
		if NormalizeAuthorName(pair[0]) != NormalizeAuthorName(pair[1]) {
			t.Errorf("canonical names differ: %q, %q", pair[0], pair[1])
		}
	}

	for _, pair := range [][2]string{{"Renée", "Renee"}, {"İpek", "Ipek"}, {"Nguyễn", "Nguyen"}, {"مُحَمَّد", "محمد"}, {"שָׁלוֹם", "שלום"}, {"किरण", "करण"}, {"Jose", "Јose"}} {
		if NormalizeAuthorName(pair[0]) == NormalizeAuthorName(pair[1]) {
			t.Errorf("distinct names collapsed: %q, %q", pair[0], pair[1])
		}
	}

	if AuthorAccentName("Nguyễn") != AuthorAccentName("Nguyen") || AuthorAccentName("İpek") != AuthorAccentName("Ipek") {
		t.Fatal("Latin accent suggestions lost their secondary keys")
	}

	for _, name := range []string{"مُحَمَّد", "שָׁלוֹם", "किरण", "Σίγμα", "山田太郎先生", "สมชายใจดี"} {
		if AuthorAccentName(name) != NormalizeAuthorName(name) {
			t.Errorf("non-Latin marks changed: %q", name)
		}
	}
}

func TestAuthorTypoSuggestionsAreBoundedAndScriptAware(t *testing.T) {
	t.Parallel()

	for _, pair := range [][2]string{{"Julia Chld", "Julia Child"}, {"Julia Chlid", "Julia Child"}, {"Xulia Child", "Julia Child"}, {"Рене Иванова", "Рене Иванов"}, {"山田太郎先生", "山田太朗先生"}, {"สมชายใจดี", "สมชายใจที"}} {
		if !AuthorTypoMatch(pair[0], pair[1]) {
			t.Errorf("one-edit suggestion missing: %q, %q", pair[0], pair[1])
		}
	}

	for _, pair := range [][2]string{{"Li", "Lu"}, {"John", "Joan"}, {"Julia Child", "Julia Children"}, {"Josef", "Јosef"}, {"מִיכאל", "מיכאל"}, {"A----", "B----"}} {
		if AuthorTypoMatch(pair[0], pair[1]) {
			t.Errorf("unsafe suggestion: %q, %q", pair[0], pair[1])
		}
	}
}
