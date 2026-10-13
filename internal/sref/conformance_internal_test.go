package sref

import (
	"errors"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
)

func TestMicrowaveViolationRequirementIDs(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"microwave.choice_duplicate":        "microwave-choice-duplicate-conditions",
		"microwave.choices_opaque_duration": "microwave-choice-duration-must-be-opaque",
		"microwave.choices_shared_power":    "microwave-choice-shared-power-conflict",
		"microwave.percent":                 "microwave-percent-out-of-range",
		"microwave.power_shape":             "microwave-power-conflict",
	}

	for violation, requirement := range tests {
		t.Run(violation, func(t *testing.T) {
			t.Parallel()

			got := violationCode(recipe.Violation{Code: violation})
			if got != requirement {
				t.Fatalf("violationCode(%q) = %q, want %q", violation, got, requirement)
			}
		})
	}
}

func TestRequirementClassificationIgnoresDiagnosticProse(t *testing.T) {
	for _, code := range []string{"unknown-unit", "step-text-required", "missing-format-version", "invalid-extension-name"} {
		if got := RequirementID(shapeFailure(code, "読み取りエラー")); got != code {
			t.Fatalf("requirement=%q want=%q", got, code)
		}
	}

	for _, message := range []string{"duplicate", "NaN", "names unknown unit", "unknown field", "control character"} {
		if got := RequirementID(errors.New(message)); got != "invalid-recipe" {
			t.Fatalf("classified prose %q as %q", message, got)
		}
	}

	if got := violationCode(recipe.Violation{Code: "asset.role", Value: "primary-image", Message: "不正な役割"}); got != "legacy-primary-image-role" {
		t.Fatalf("requirement=%q", got)
	}
}
