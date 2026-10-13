package v1

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/savorum/savorum/internal/memoryspace"
)

func TestMemoryRefusalUsesRecoverableOperationProblem(t *testing.T) {
	var p *Problem
	if !errors.As(mapError(fmt.Errorf("write: %w", memoryspace.ErrResourceMemory)), &p) || p.Status != http.StatusServiceUnavailable || p.Code != "operation-memory-exhausted" {
		t.Fatalf("unexpected memory problem: %+v", p)
	}
}
