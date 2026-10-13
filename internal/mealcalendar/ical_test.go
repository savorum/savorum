package mealcalendar

import (
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/meal"
	"github.com/savorum/savorum/internal/recipe"
)

func TestEncodeWritesAllDayEventsWithStableIdentities(t *testing.T) {
	tuesday := library.Date("2026-09-29")
	double := recipe.Rational("2")
	updated := time.Date(2026, time.September, 20, 9, 30, 0, 0, time.UTC)
	meals := []meal.Meal{
		{
			ID: "m1", Date: &tuesday, MealType: meal.MealTypeDinner, Revision: 3, UpdatedAt: updated, Notes: "Private note about guests",
			Dishes: []meal.Dish{{DisplayName: "Pancakes; the good ones, really", Scale: &double}, {DisplayName: "Fruit salad"}},
		},
		{ID: "m2", Date: &tuesday, Skipped: true, Revision: 1, Dishes: []meal.Dish{{DisplayName: "Soup"}}},
		{ID: "unscheduled", Revision: 1, Dishes: []meal.Dish{{DisplayName: "Someday stew"}}},
	}

	document := string(Encode(Calendar{Name: "Home meals", Instance: "01INSTANCE", PlanURL: "https://recipes.example.com/plan", Stamp: updated}, meals))

	for _, want := range []string{
		"BEGIN:VCALENDAR\r\n",
		"X-WR-CALNAME:Home meals\r\n",
		"UID:meal-m1@01INSTANCE\r\n",
		"DTSTART;VALUE=DATE:20260929\r\n",
		"DTEND;VALUE=DATE:20260930\r\n",
		"SEQUENCE:2\r\n",
		"URL:https://recipes.example.com/plan?date=2026-09-29\r\n",
		"TRANSP:TRANSPARENT\r\n",
		"UID:meal-m2@01INSTANCE\r\nDTSTAMP",
		"STATUS:CANCELLED\r\n",
		"END:VCALENDAR\r\n",
	} {
		if !strings.Contains(document, want) {
			t.Errorf("missing %q in\n%s", want, document)
		}
	}

	unfolded := strings.ReplaceAll(document, "\r\n ", "")
	if !strings.Contains(unfolded, `SUMMARY:Dinner: Pancakes\; the good ones\, really\, Fruit salad`) ||
		!strings.Contains(unfolded, `DESCRIPTION:Pancakes\; the good ones\, really (×2)\nFruit salad`) {
		t.Errorf("summary or description not escaped:\n%s", unfolded)
	}

	for _, forbidden := range []string{"Private note", "Someday stew", "DTSTART:", "T180000"} {
		if strings.Contains(document, forbidden) {
			t.Errorf("document contains %q:\n%s", forbidden, document)
		}
	}

	for line := range strings.SplitSeq(document, "\r\n") {
		if len(line) > 75 {
			t.Errorf("unfolded line of %d octets: %q", len(line), line)
		}
	}
}

func TestFoldingKeepsMultibyteCharactersWhole(t *testing.T) {
	var out strings.Builder
	writeFolded(&out, "SUMMARY:"+strings.Repeat("é", 60))

	for line := range strings.SplitSeq(strings.TrimSuffix(out.String(), "\r\n"), "\r\n") {
		if len(line) > 75 || !utf8Valid(line) {
			t.Fatalf("bad fold %q", line)
		}
	}
}

func utf8Valid(value string) bool { return strings.ToValidUTF8(value, "\uFFFD") == value }
