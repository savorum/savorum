package library

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func cloneRefresh(t *testing.T, value recipe.Document) recipe.Document {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	var result recipe.Document

	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}

	return result
}

func refreshDocument() recipe.Document {
	return recipe.Document{SchemaVersion: recipe.CurrentDocumentSchemaVersion, ID: "soup", Title: "Soup", Description: "plain", IngredientSections: []recipe.IngredientSection{{ID: "main", Ingredients: []recipe.Ingredient{{ID: "salt", Name: "salt"}, {ID: "water", Name: "water"}}}}, InstructionSections: []recipe.InstructionSection{{ID: "method", Steps: []recipe.Step{{ID: "stir", Text: "Stir"}, {ID: "boil", Text: "Boil"}}}}}
}

type refreshBaselineRepository struct {
	*memoryRepository
	baseline recipe.Document
}

func (repository *refreshBaselineRepository) GetSourceImportBaseline(context.Context, EntryID) (SourceImportBaseline, error) {
	return SourceImportBaseline{Document: repository.baseline, Revision: 1}, nil
}

func TestCheckSourceUpdatesRejectsOpaqueJSONThatGenericDecodeCannotRepresent(t *testing.T) {
	local := refreshDocument()
	remote := cloneRefresh(t, local)
	remote.Extensions = map[string]any{"x-example.invalid": json.RawMessage(`1e1000`)}
	repository := &refreshBaselineRepository{memoryRepository: &memoryRepository{created: Entry{ID: "soup-entry", Recipe: local, Revision: 2}}, baseline: local}

	service, err := NewService(repository, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.CheckSourceUpdates(context.Background(), "soup-entry", remote, 2); err == nil {
		t.Fatal("refresh comparison accepted JSON that cannot decode into generic fields")
	}

	if repository.importCalls != 0 {
		t.Fatalf("invalid refresh mutated repository %d time(s)", repository.importCalls)
	}
}

func TestApplySourceRefreshValidatesEveryComparedPreviewDocument(t *testing.T) {
	for _, field := range []string{"local", "remote", "merged"} {
		t.Run(field, func(t *testing.T) {
			local := refreshDocument()
			repository := &memoryRepository{created: Entry{ID: "soup-entry", Recipe: local, Revision: 2}}

			service, err := NewService(repository, units.DefaultCatalog())
			if err != nil {
				t.Fatal(err)
			}

			preview := SourceRefreshPreview{EntryID: "soup-entry", ExpectedRevision: 2, Local: cloneRefresh(t, local), Remote: cloneRefresh(t, local), Merged: cloneRefresh(t, local)}

			invalid := map[string]any{"x-example.invalid": json.RawMessage(`1e1000`)}

			switch field {
			case "local":
				preview.Local.Extensions = invalid
			case "remote":
				preview.Remote.Extensions = invalid
			case "merged":
				preview.Merged.Extensions = invalid
			}

			if _, err := service.ApplySourceRefresh(context.Background(), preview, nil); err == nil {
				t.Fatalf("refresh apply accepted invalid %s recipe", field)
			}

			if repository.importCalls != 0 {
				t.Fatalf("invalid refresh mutated repository %d time(s)", repository.importCalls)
			}
		})
	}
}

func TestThreeWayRefreshKeepsLocalAndAcceptsIndependentSourceEdit(t *testing.T) {
	base := refreshDocument()
	local := cloneRefresh(t, base)
	local.Description = "my note"
	remote := cloneRefresh(t, base)
	remote.Title = "New soup"

	merged, conflicts, _ := threeWayDocument(base, local, remote)
	if len(conflicts) != 0 || merged.Title != "New soup" || merged.Description != "my note" {
		t.Fatalf("merged=%#v conflicts=%#v", merged, conflicts)
	}
}

func TestThreeWayRefreshUsesStableIDsForReorderedSectionsAndSteps(t *testing.T) {
	base := refreshDocument()
	remote := cloneRefresh(t, base)
	remote.IngredientSections[0].Ingredients[0], remote.IngredientSections[0].Ingredients[1] = remote.IngredientSections[0].Ingredients[1], remote.IngredientSections[0].Ingredients[0]
	remote.InstructionSections[0].Steps[0], remote.InstructionSections[0].Steps[1] = remote.InstructionSections[0].Steps[1], remote.InstructionSections[0].Steps[0]

	_, conflicts, _ := threeWayDocument(base, base, remote)
	if len(conflicts) != 0 {
		t.Fatalf("reordering generated conflicts: %#v", conflicts)
	}
}

func TestThreeWayRefreshReportsConflictingStableReorders(t *testing.T) {
	base := refreshDocument()
	base.IngredientSections[0].Ingredients = append(base.IngredientSections[0].Ingredients, recipe.Ingredient{ID: "onion", Name: "onion"})
	local, remote := cloneRefresh(t, base), cloneRefresh(t, base)
	local.IngredientSections[0].Ingredients = []recipe.Ingredient{local.IngredientSections[0].Ingredients[1], local.IngredientSections[0].Ingredients[2], local.IngredientSections[0].Ingredients[0]}
	remote.IngredientSections[0].Ingredients = []recipe.Ingredient{remote.IngredientSections[0].Ingredients[1], remote.IngredientSections[0].Ingredients[2], remote.IngredientSections[0].Ingredients[0]}
	// Same reorder on both sides is not a conflict.
	_, conflicts, _ := threeWayDocument(base, local, remote)
	if len(conflicts) != 0 {
		t.Fatalf("same reorder generated conflicts: %#v", conflicts)
	}

	remote.IngredientSections[0].Ingredients = []recipe.Ingredient{remote.IngredientSections[0].Ingredients[1], remote.IngredientSections[0].Ingredients[2], remote.IngredientSections[0].Ingredients[0]}

	_, conflicts, _ = threeWayDocument(base, local, remote)
	if len(conflicts) != 1 || conflicts[0].Path != "ingredient_sections[main].ingredients" {
		t.Fatalf("conflicting reorder=%#v", conflicts)
	}
}

func TestThreeWayRefreshReportsConcurrentConflict(t *testing.T) {
	base := refreshDocument()
	local := cloneRefresh(t, base)
	remote := cloneRefresh(t, base)
	local.Title, remote.Title = "My soup", "Source soup"

	_, conflicts, _ := threeWayDocument(base, local, remote)
	if len(conflicts) != 1 || conflicts[0].Path != "title" {
		t.Fatalf("conflicts=%#v", conflicts)
	}
}

func TestThreeWayRefreshMergesIndependentStableIngredientEdits(t *testing.T) {
	base := refreshDocument()
	local := cloneRefresh(t, base)
	remote := cloneRefresh(t, base)
	local.IngredientSections[0].Ingredients[0].Name = "sea salt"
	remote.IngredientSections[0].Ingredients[1].Name = "hot water"

	merged, conflicts, _ := threeWayDocument(base, local, remote)
	if len(conflicts) != 0 || merged.IngredientSections[0].Ingredients[0].Name != "sea salt" || merged.IngredientSections[0].Ingredients[1].Name != "hot water" {
		t.Fatalf("merged=%#v conflicts=%#v", merged, conflicts)
	}
}

func TestThreeWayRefreshMergesIndependentStableAssetEdits(t *testing.T) {
	base := refreshDocument()
	base.Assets = []recipe.Asset{{ID: "hero", Path: "hero.jpg", MediaType: "image/jpeg", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 1}, {ID: "step", Path: "step.jpg", MediaType: "image/jpeg", SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Size: 1}}
	local, remote := cloneRefresh(t, base), cloneRefresh(t, base)
	local.Assets[0].Caption = "my caption"
	remote.Assets[1].Alt = "source alt"

	merged, conflicts, _ := threeWayDocument(base, local, remote)
	if len(conflicts) != 0 || merged.Assets[0].Caption != "my caption" || merged.Assets[1].Alt != "source alt" {
		t.Fatalf("merged=%#v conflicts=%#v", merged, conflicts)
	}
}

func TestRefreshChoiceCanResolveNestedStableObject(t *testing.T) {
	base := refreshDocument()
	local, remote := cloneRefresh(t, base), cloneRefresh(t, base)
	local.IngredientSections[0].Ingredients[0].Name = "sea salt"
	remote.IngredientSections[0].Ingredients[0].Name = "kosher salt"

	merged, conflicts, _ := threeWayDocument(base, local, remote)
	if len(conflicts) != 1 {
		t.Fatalf("conflicts=%#v", conflicts)
	}

	if err := setDocumentPath(&merged, conflicts[0].Path, true, local, remote); err != nil {
		t.Fatal(err)
	}

	if merged.IngredientSections[0].Ingredients[0].Name != "kosher salt" {
		t.Fatalf("choice not applied: %#v", merged)
	}
}

func TestRefreshRemoteDeletionRemovesOnlyNestedStableObject(t *testing.T) {
	base := refreshDocument()
	local, remote := cloneRefresh(t, base), cloneRefresh(t, base)
	local.IngredientSections[0].Ingredients[0].Name = "sea salt"
	remote.IngredientSections[0].Ingredients = remote.IngredientSections[0].Ingredients[1:]

	merged, conflicts, _ := threeWayDocument(base, local, remote)
	if len(conflicts) != 1 || conflicts[0].Path != "ingredient_sections[main].ingredients[salt]" {
		t.Fatalf("conflicts=%#v", conflicts)
	}

	if err := setDocumentPath(&merged, conflicts[0].Path, true, local, remote); err != nil {
		t.Fatal(err)
	}

	if len(merged.IngredientSections) != 1 || len(merged.IngredientSections[0].Ingredients) != 1 || merged.IngredientSections[0].Ingredients[0].ID != "water" {
		t.Fatalf("remote deletion changed wrong object: %#v", merged.IngredientSections)
	}
}

func TestRefreshLocalChoiceKeepsAWholeStableObjectTheSourceDeleted(t *testing.T) {
	base := refreshDocument()
	local, remote := cloneRefresh(t, base), cloneRefresh(t, base)
	local.IngredientSections[0].Ingredients[0].Name = "sea salt"
	remote.IngredientSections[0].Ingredients = remote.IngredientSections[0].Ingredients[1:]

	merged, conflicts, _ := threeWayDocument(base, local, remote)
	if len(conflicts) != 1 {
		t.Fatalf("conflicts=%#v", conflicts)
	}

	if err := setDocumentPath(&merged, conflicts[0].Path, false, local, remote); err != nil {
		t.Fatal(err)
	}

	ingredients := merged.IngredientSections[0].Ingredients
	if len(ingredients) != 2 || ingredients[0].Name != "sea salt" || ingredients[1].ID != "water" {
		t.Fatalf("local choice not applied: %#v", ingredients)
	}
}

func TestThreeWayRefreshLeavesAStableObjectBothSidesDeleted(t *testing.T) {
	base := refreshDocument()
	local, remote := cloneRefresh(t, base), cloneRefresh(t, base)
	local.IngredientSections[0].Ingredients = []recipe.Ingredient{{ID: "water", Name: "cold water"}}
	remote.IngredientSections[0].Ingredients = []recipe.Ingredient{{ID: "water", Name: "hot water"}}

	merged, conflicts, _ := threeWayDocument(base, local, remote)
	if ingredients := merged.IngredientSections[0].Ingredients; len(ingredients) != 1 || ingredients[0].ID != "water" {
		t.Fatalf("deleted ingredient returned: %#v", ingredients)
	}

	if len(conflicts) != 1 || conflicts[0].Path != "ingredient_sections[main].ingredients[water].name" {
		t.Fatalf("conflicts=%#v", conflicts)
	}
}

func TestRefreshRemoteDeletionRemovesNestedField(t *testing.T) {
	base := refreshDocument()
	local, remote := cloneRefresh(t, base), cloneRefresh(t, base)
	local.IngredientSections[0].Ingredients[0].Name = "sea salt"
	remote.IngredientSections[0].Ingredients[0].Name = ""

	merged, conflicts, _ := threeWayDocument(base, local, remote)
	if len(conflicts) != 1 || conflicts[0].Path != "ingredient_sections[main].ingredients[salt].name" {
		t.Fatalf("conflicts=%#v", conflicts)
	}

	if err := setDocumentPath(&merged, conflicts[0].Path, true, local, remote); err != nil {
		t.Fatal(err)
	}

	if merged.IngredientSections[0].Ingredients[0].Name != "" || merged.IngredientSections[0].Ingredients[1].Name != "water" {
		t.Fatalf("remote field deletion changed wrong object: %#v", merged.IngredientSections[0].Ingredients)
	}
}

func TestTwoWayMergeDefaultsEmptyFieldAndRequiresMaterialChoice(t *testing.T) {
	left := refreshDocument()
	right := cloneRefresh(t, left)
	left.Description = ""
	right.Description = "source description"
	right.Title = "Other title"

	suggested, conflicts := twoWayMerge(left, right)
	if suggested.Description != right.Description {
		t.Fatalf("empty field was not filled: %#v", suggested)
	}

	if len(conflicts) != 1 || conflicts[0].Path != "title" {
		t.Fatalf("conflicts=%#v", conflicts)
	}
}
