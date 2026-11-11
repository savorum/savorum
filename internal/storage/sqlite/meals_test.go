package sqlite

import (
	"testing"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/meal"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestMealAttributionAndPlannedRecipeLifecycle(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	principal, err := store.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	ctx = access.WithLibrary(identity.WithPrincipal(ctx, principal), "default")
	timestamp := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	document := testfixture.Document()
	document.ID = recipe.RecipeID("planned-document")

	if err := store.Create(ctx, library.Entry{ID: "planned", Recipe: document, Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	entry := library.EntryID("planned")
	date := library.Date("2026-09-02")

	planned := meal.Meal{
		ID: "m-planned", Date: &date, Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp,
		Dishes: []meal.Dish{
			{ID: "d-recipe", MealID: "m-planned", EntryID: &entry, DisplayName: "Planned supper"},
			{ID: "d-plain", MealID: "m-planned", DisplayName: "Green salad", Position: 1},
		},
	}
	if err := store.CreateMeal(ctx, "default", planned); err != nil {
		t.Fatal(err)
	}

	status := func(want meal.RecipeStatus) meal.Meal {
		t.Helper()

		got, err := store.GetMeal(ctx, "default", "m-planned")
		if err != nil {
			t.Fatal(err)
		}

		if got.Dishes[0].RecipeStatus != want || got.Dishes[1].RecipeStatus != "" {
			t.Fatalf("recipe status = %q / %q, want %q / empty", got.Dishes[0].RecipeStatus, got.Dishes[1].RecipeStatus, want)
		}

		return got
	}

	got := status(meal.RecipeAvailable)
	if got.CreatedBy != principal.UserID || got.CreatedByName != principal.DisplayName || got.UpdatedBy != principal.UserID {
		t.Fatalf("attribution = %#v", got)
	}

	if _, err := store.Revise(ctx, library.RevisionCommand{
		ID: entry, ExpectedRevision: 1, Recipe: document,
		CurrentState: library.StateActive, TargetState: library.StateTrash,
		Metadata: library.RevisionMetadata{Cause: library.RevisionCauseTrash}, Timestamp: timestamp.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	status(meal.RecipeTrashed)

	if err := store.PurgeMany(ctx, []library.PurgeTarget{{ID: entry, Revision: 2}}, nil, timestamp.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	got = status(meal.RecipeRemoved)
	if got.Dishes[0].DisplayName != "Planned supper" {
		t.Fatalf("purged dish lost its name: %#v", got.Dishes[0])
	}

	got.Notes = "Edited after the purge"
	got.UpdatedAt = timestamp.Add(3 * time.Minute)

	if err := store.UpdateMeal(ctx, "default", got, got.Revision); err != nil {
		t.Fatal(err)
	}

	status(meal.RecipeRemoved)

	if _, err := store.db.ExecContext(ctx, `UPDATE users SET status='deleted', display_name='Private erased name' WHERE id=?`, principal.UserID); err != nil {
		t.Fatal(err)
	}

	got, err = store.GetMeal(ctx, "default", "m-planned")
	if err == nil && got.CreatedByName == "Private erased name" {
		t.Fatalf("erased name leaked: %#v", got)
	}
}

func TestRecipeRenameRenamesDishesStillNamedAfterIt(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	principal, err := store.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	ctx = access.WithLibrary(identity.WithPrincipal(ctx, principal), "default")
	timestamp := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	document := testfixture.Document()
	document.ID = recipe.RecipeID("renamed-document")
	original := document.Title

	if err := store.Create(ctx, library.Entry{ID: "renamed", Recipe: document, Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	entry := library.EntryID("renamed")

	planned := meal.Meal{
		ID: "m-renamed", Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp,
		Dishes: []meal.Dish{
			{ID: "d-follows", MealID: "m-renamed", EntryID: &entry, DisplayName: original},
			{ID: "d-own", MealID: "m-renamed", EntryID: &entry, DisplayName: "Sunday pies", Position: 1},
		},
	}
	if err := store.CreateMeal(ctx, "default", planned); err != nil {
		t.Fatal(err)
	}

	saved := meal.SavedMeal{
		ID: "s-renamed", Name: "Pie night", Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp,
		Dishes: []meal.SavedDish{{ID: "sd-follows", EntryID: &entry, DisplayName: original}},
	}
	if err := store.CreateSavedMeal(ctx, "default", saved); err != nil {
		t.Fatal(err)
	}

	document.Title = "Vegetable hand pies"
	if _, err := store.Revise(ctx, library.RevisionCommand{
		ID: entry, ExpectedRevision: 1, Recipe: document,
		CurrentState: library.StateActive, TargetState: library.StateActive,
		Metadata: library.RevisionMetadata{Cause: library.RevisionCauseManualEdit}, Timestamp: timestamp.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetMeal(ctx, "default", "m-renamed")
	if err != nil {
		t.Fatal(err)
	}

	if got.Dishes[0].DisplayName != "Vegetable hand pies" || got.Dishes[1].DisplayName != "Sunday pies" {
		t.Fatalf("dish names = %q / %q", got.Dishes[0].DisplayName, got.Dishes[1].DisplayName)
	}

	if got.Revision != planned.Revision+1 {
		t.Fatalf("meal revision = %d, want %d", got.Revision, planned.Revision+1)
	}

	gotSaved, err := store.GetSavedMeal(ctx, "default", "s-renamed")
	if err != nil {
		t.Fatal(err)
	}

	if gotSaved.Dishes[0].DisplayName != "Vegetable hand pies" || gotSaved.Revision != saved.Revision+1 {
		t.Fatalf("saved meal = %#v", gotSaved)
	}
}
