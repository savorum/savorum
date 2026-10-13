package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestStatedClaimFiltersMatchOnlyUnconditionalClaims(t *testing.T) {
	ctx := context.Background()

	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	recipes := map[string]func(*recipe.Document){
		"stated": func(document *recipe.Document) {
			document.DietaryClaims = []recipe.DietaryClaim{{Text: "Vegan", Suitability: recipe.SuitabilitySuitable, Diet: recipe.DietVegan}}
			document.AllergenDeclarations = []recipe.AllergenDeclaration{{Text: "Peanut-free", Substance: "Peanuts", Presence: recipe.AllergenFreeFrom}}
		},
		"conditional": func(document *recipe.Document) {
			document.DietaryClaims = []recipe.DietaryClaim{{Text: "Vegan with oat milk", Suitability: recipe.SuitabilitySuitable, Diet: recipe.DietVegan, Condition: "with oat milk"}}
			document.AllergenDeclarations = []recipe.AllergenDeclaration{{Text: "May contain peanuts", Substance: "peanuts", Presence: recipe.AllergenMayContain}}
		},
		"silent": func(*recipe.Document) {},
	}
	for name, change := range recipes {
		document := testfixture.Document()
		document.ID, document.Title = recipe.RecipeID(name), name
		change(&document)

		if _, err := service.Create(ctx, document); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}

	for _, query := range []string{"stated:vegan", "free:peanuts"} {
		criteria, err := library.ParseSearchQuery(query)
		if err != nil {
			t.Fatal(err)
		}

		results, err := store.List(ctx, library.ListOptions{Limit: 10, State: library.StateActive, Search: criteria, Sort: library.ListSortUpdatedDesc})
		if err != nil || len(results) != 1 || results[0].Title != "stated" {
			t.Fatalf("%s matched %d recipes: %v", query, len(results), err)
		}
	}
}
