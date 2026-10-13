package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/storagebudget"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
)

func TestRecipeProblemStructuredContract(t *testing.T) {
	mapped := mapError(&recipe.ValidationError{Violations: []recipe.Violation{{Code: "text.required", Path: "name", Message: "value cannot be empty"}}})

	var legacy *huma.ErrorModel
	if !errors.As(mapped, &legacy) || len(legacy.Errors) != 1 {
		t.Fatal("legacy Huma diagnostics were lost")
	}

	encoded, err := json.Marshal(mapped)
	if err != nil {
		t.Fatal(err)
	}

	var wire struct {
		Code       string             `json:"code"`
		Detail     string             `json:"detail"`
		Violations []ProblemViolation `json:"violations"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}

	if wire.Code != "recipe-invalid" || wire.Detail == "" || len(wire.Violations) != 1 || wire.Violations[0].Code != "text.required" || wire.Violations[0].Location != "body.name" {
		t.Fatalf("structured localization contract missing: %s", encoded)
	}
}

func TestRevisionProblemParameters(t *testing.T) {
	mapped := mapError(&library.PreconditionError{EntryID: "entry", Expected: 2, Current: 3})

	var structured *Problem
	if !errors.As(mapped, &structured) || structured.Code != "recipe-revision-conflict" || structured.Params["expected_revision"] != uint64(2) || structured.Params["current_revision"] != uint64(3) {
		t.Fatalf("revision context not structured: %#v", mapped)
	}
}

func TestExportAssetProblemPreservesStructuredCause(t *testing.T) {
	mapped := mapExportError(&export.AssetError{RecipeTitle: "Crème brûlée", AssetID: "hero", Reason: "is not in this library", ReasonCode: "missing"})

	var structured *Problem
	if !errors.As(mapped, &structured) || structured.Code != "export-asset-unavailable" || structured.Params["recipe_title"] != "Crème brûlée" || structured.Params["reason_code"] != "missing" || structured.Params["asset_id"] != "hero" {
		t.Fatalf("export context not structured: %#v", mapped)
	}

	if !strings.Contains(structured.Detail, "Crème brûlée") {
		t.Fatal("legacy export diagnostic was lost")
	}
}

func TestStorageReservationFailureUsesControlledProblem(t *testing.T) {
	result := &Problem{}

	ok := errors.As(mapError(fmt.Errorf("reserve staging: %w", diskspace.ErrCapacity)), &result)
	if !ok || result.Status != 507 || result.Code != "storage-capacity-exhausted" {
		t.Fatalf("storage refusal = %#v", result)
	}
}

func TestHostedBudgetFailureUsesControlledProblem(t *testing.T) {
	result := &Problem{}
	if !errors.As(mapError(fmt.Errorf("commit hosted mutation: %w", storagebudget.ErrCapacity)), &result) || result.Status != 507 || result.Code != "storage-capacity-exhausted" {
		t.Fatalf("hosted refusal = %#v", result)
	}

	status, code, _ := assetHTTPError(fmt.Errorf("register original: %w", storagebudget.ErrCapacity))
	if status != 507 || code != "asset-capacity-exhausted" {
		t.Fatalf("hosted original refusal = %d %s", status, code)
	}
}

func TestStorageProblemsAreUniformAcrossMutations(t *testing.T) {
	maps := []func(error) error{
		mapError, mapExportError, mapImportError, mapShoppingError, mapImportJobError, mapCalendarError, mapTrashError, webhookProblem, mapPrintError,
		func(err error) error { return mapOrganizationError(err, "book") },
		func(err error) error { return mapCookingError(err, "cooking") },
		func(err error) error { return mapMealError(err, "meal") },
	}
	for _, sentinel := range []error{diskspace.ErrCapacity, storagebudget.ErrCapacity, assets.ErrCapacity} {
		for i, mapper := range maps {
			var got *Problem
			if !errors.As(mapper(fmt.Errorf("write denied: %w", sentinel)), &got) || got.Status != 507 || got.Code != "storage-capacity-exhausted" {
				t.Fatalf("mapper %d: %+v", i, got)
			}
		}

		status, code, _ := importHTTPError(fmt.Errorf("import denied: %w", sentinel))
		if status != 507 || code != "storage-capacity-exhausted" {
			t.Fatalf("import = %d %s", status, code)
		}
	}
}
