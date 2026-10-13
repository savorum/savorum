package library

import (
	"testing"
	"time"

	"github.com/savorum/savorum/internal/recipe"
)

func TestPreferredRenditionFollowsContentLanguagesThenTheOriginal(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	set := []TranslationMember{
		{ID: "en", DocumentID: "d-en", Language: "en-GB", CreatedAt: start},
		{ID: "fr", DocumentID: "d-fr", Language: "fr-CA", CreatedAt: start.Add(time.Hour), Links: []TranslationLink{{Relation: recipe.TranslationOriginal, Language: "de", RecipeID: "d-de"}}},
		{ID: "de", DocumentID: "d-de", Language: "de", CreatedAt: start.Add(2 * time.Hour)},
	}

	for _, test := range []struct {
		preferences []string
		want        EntryID
	}{
		{[]string{"fr-CA"}, "fr"},
		{[]string{"fr"}, "fr"},
		{[]string{"en-US", "fr"}, "en"},
		{[]string{"ja", "de-AT"}, "de"},
		{[]string{"ja"}, "de"},
		{nil, "de"},
	} {
		if got := PreferredRendition(set, test.preferences); got != test.want {
			t.Errorf("PreferredRendition(%v) = %q, want %q", test.preferences, got, test.want)
		}
	}

	if got := PreferredRendition(set[:1], []string{"ja"}); got != "en" {
		t.Errorf("a set without an original presents its earliest recipe, got %q", got)
	}
}

func TestTranslationSetsConnectThroughEitherSide(t *testing.T) {
	t.Parallel()

	members := []TranslationMember{
		{ID: "a", DocumentID: "d-a", Links: []TranslationLink{{RecipeID: "d-b"}}},
		{ID: "b", DocumentID: "d-b"},
		{ID: "c", DocumentID: "d-c", Links: []TranslationLink{{RecipeID: "d-b"}}},
		{ID: "d", DocumentID: "d-d", Links: []TranslationLink{{RecipeID: "missing"}}},
	}

	sets := TranslationSets(members)
	if len(sets) != 1 || len(sets[0]) != 3 {
		t.Fatalf("sets = %+v", sets)
	}
}
