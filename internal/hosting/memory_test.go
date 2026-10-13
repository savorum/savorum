package hosting

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/savorum/savorum/internal/memoryspace"
)

func TestOperatorMemoryRefusalIsRecoverable(t *testing.T) {
	w := httptest.NewRecorder()
	operatorError(w, memoryspace.ErrResourceMemory)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", w.Code)
	}
}
