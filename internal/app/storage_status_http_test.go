package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/app"
	"github.com/savorum/savorum/internal/contracttest"
	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/storagebudget"
	"github.com/savorum/savorum/internal/units"
)

func TestStorageStatusIsAuthenticatedAndDoesNotPublishBootstrapUsage(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	budget := storagebudget.New(t.TempDir(), testDurableLimit)
	budget.SetDatabaseUsage(func(context.Context) (int64, error) { return testDurableLimit * 3 / 4, nil })

	for _, enabled := range []bool{false, true} {
		application, err := app.New(store, units.DefaultCatalog(), app.Options{AuthEnabled: enabled, DurableBudget: budget})
		if err != nil {
			t.Fatal(err)
		}

		response := httptest.NewRecorder()
		contracttest.Handler(t, application.Handler).ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/storage-status", nil))

		if enabled {
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("authenticated status=%d", response.Code)
			}

			continue
		}

		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
		}

		var report struct {
			State     string `json:"state"`
			UsedBytes int64  `json:"used_bytes"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
			t.Fatal(err)
		}

		if report.State != "warning" || report.UsedBytes != testDurableLimit*3/4 {
			t.Fatalf("report=%+v", report)
		}

		public := httptest.NewRecorder()
		contracttest.Handler(t, application.Handler).ServeHTTP(public, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/.well-known/savorum", nil))

		var bootstrap map[string]any
		if err := json.Unmarshal(public.Body.Bytes(), &bootstrap); err != nil {
			t.Fatal(err)
		}

		if _, ok := bootstrap["used_bytes"]; ok {
			t.Fatal("bootstrap leaked storage usage")
		}
	}
}

type unavailableFootprintStore struct{ app.Store }

func (unavailableFootprintStore) StorageFootprint(context.Context) (diskspace.Footprint, error) {
	return diskspace.Footprint{}, errors.New("storage metadata unavailable")
}

func TestStorageStatusFootprintFailureDoesNotReportEmptyLibrary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")

	store, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	application, err := app.New(unavailableFootprintStore{store}, units.DefaultCatalog(), app.Options{StoragePath: path})
	if err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	contracttest.Handler(t, application.Handler).ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/storage-status", nil))

	var report struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}

	if response.Code != http.StatusOK || report.State != "unknown" {
		t.Fatalf("status=%d report=%+v", response.Code, report)
	}
}
