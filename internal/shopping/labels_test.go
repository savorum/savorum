package shopping

import (
	"testing"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/meal"
)

func TestMealLabelDoesNotRepeatUnnamedCombination(t *testing.T) {
	dish := meal.Dish{DisplayName: "Pancakes"}

	planned := meal.Meal{Dishes: []meal.Dish{dish, {DisplayName: "Potatoes"}}}
	if got := mealLabel(planned, dish); got != "Pancakes" {
		t.Fatalf("got %q", got)
	}

	planned.Name = "Weekend breakfast"
	if got := mealLabel(planned, dish); got != "Weekend breakfast · Pancakes" {
		t.Fatalf("got %q", got)
	}
}

func TestMealLabelLeavesTheDateToClients(t *testing.T) {
	dish := meal.Dish{DisplayName: "Pancakes"}
	date := library.Date("2026-09-24")
	planned := meal.Meal{Date: &date, Dishes: []meal.Dish{dish}}

	if got := mealLabel(planned, dish); got != "Pancakes" {
		t.Fatalf("label %q", got)
	}

	if got := mealDate(planned); got != "2026-09-24" {
		t.Fatalf("date %q", got)
	}

	if got := mealDate(meal.Meal{}); got != "" {
		t.Fatalf("unscheduled date %q", got)
	}
}
