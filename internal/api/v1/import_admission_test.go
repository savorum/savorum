package v1

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type observedImportBody struct{ reads int }

func (b *observedImportBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (*observedImportBody) Close() error               { return nil }

func TestImportAdmissionRejectsBeforeReadingBody(t *testing.T) {
	for range cap(importBodySlots) {
		importBodySlots <- struct{}{}
	}
	defer func() {
		for range cap(importBodySlots) {
			<-importBodySlots
		}
	}()

	for _, next := range []http.HandlerFunc{streamImportPreview(nil), streamExternalRecipeImport(nil), streamPackageImport(nil)} {
		body := &observedImportBody{}
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", body)
		response := httptest.NewRecorder()
		next(response, request)

		if response.Code != http.StatusServiceUnavailable || body.reads != 0 || response.Header().Get("Retry-After") == "" {
			t.Fatalf("status=%d reads=%d", response.Code, body.reads)
		}
	}
}

func TestMalformedImportReleasesAdmission(t *testing.T) {
	for range cap(importBodySlots) + 1 {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader("invalid"))
		response := httptest.NewRecorder()
		streamImportPreview(nil)(response, request)

		if response.Code != http.StatusBadRequest {
			t.Fatalf("status=%d", response.Code)
		}
	}
}
