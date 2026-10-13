package sref_test

import (
	"fmt"
	"testing"

	"github.com/savorum/savorum/internal/sref"
)

type boundaryFailure struct {
	category    sref.FailureCategory
	requirement string
}

func (e *boundaryFailure) Error() string                  { return e.requirement }
func (e *boundaryFailure) Category() sref.FailureCategory { return e.category }
func (e *boundaryFailure) RequirementID() string          { return e.requirement }

func TestProcessorBoundaryCanPreserveAContextDependentCategory(t *testing.T) {
	err := fmt.Errorf("bundle member: %w", &boundaryFailure{
		category:    sref.UnknownUnit,
		requirement: "invalid-bundle-member-package",
	})
	if got := sref.FailureCategoryOf(err); got != sref.UnknownUnit {
		t.Fatalf("category = %q, want %q", got, sref.UnknownUnit)
	}

	if got := sref.RequirementID(err); got != "invalid-bundle-member-package" {
		t.Fatalf("requirement = %q, want invalid-bundle-member-package", got)
	}
}
