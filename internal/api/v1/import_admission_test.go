package v1

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/library"
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

type importDeadlineWriter struct{ http.ResponseWriter }

func (w importDeadlineWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w importDeadlineWriter) SetReadDeadline(deadline time.Time) error {
	if !deadline.IsZero() {
		deadline = time.Now().Add(100 * time.Millisecond)
	}

	if err := http.NewResponseController(w.ResponseWriter).SetReadDeadline(deadline); err != nil {
		return fmt.Errorf("set test upload deadline: %w", err)
	}

	return nil
}

type delayedPackageImport struct{ ExchangeService }

func (delayedPackageImport) ImportPackage(ctx context.Context, _ []byte, _ library.DuplicatePolicy) ([]library.ImportResult, error) {
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, nil
	}
}

func TestImportReadDeadlineBoundsUploadsButNotProcessing(t *testing.T) {
	for _, tc := range []struct {
		name, contentType string
		handler           http.HandlerFunc
		incomplete        bool
		chunked           bool
		status            int
	}{
		{"preview", "multipart/form-data; boundary=recipe", streamImportPreview(nil), true, false, http.StatusBadRequest},
		{"external", "multipart/form-data; boundary=recipe", streamExternalRecipeImport(nil), true, false, http.StatusBadRequest},
		{"package", "application/zip", streamPackageImport(nil), true, false, http.StatusRequestEntityTooLarge},
		{"completed package", "application/zip", streamPackageImport(delayedPackageImport{}), false, false, http.StatusOK},
		{"completed chunked package", "application/zip", streamPackageImport(delayedPackageImport{}), false, true, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tc.handler(importDeadlineWriter{w}, r)
			}))
			server.Config.ReadTimeout = 10 * time.Second

			server.Start()
			defer server.Close()

			var body io.Reader = strings.NewReader("package")

			if tc.incomplete {
				reader, writer := io.Pipe()
				defer reader.Close()
				defer writer.Close()

				body = reader
			} else if tc.chunked {
				body = io.NopCloser(body)
			}

			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, body)
			if err != nil {
				t.Fatal(err)
			}

			request.Header.Set("Content-Type", tc.contentType)

			client := server.Client()
			client.Timeout = 3 * time.Second

			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()

			if response.StatusCode != tc.status {
				t.Fatalf("status=%d, want %d", response.StatusCode, tc.status)
			}

			if len(importBodySlots) != 0 {
				t.Fatal("upload did not release import capacity")
			}
		})
	}
}
