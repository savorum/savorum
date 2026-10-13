package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/savorum/savorum/internal/app"
	"github.com/savorum/savorum/internal/configuration"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
)

var ulidRegex = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

func TestWellKnownEndpoint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	configService, err := configuration.NewService(store, configuration.InstanceOverrides{})
	if err != nil {
		t.Fatal(err)
	}

	settings, err := configService.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}

	settings.DisplayName = "Home Kitchen"
	if _, err := configService.Replace(ctx, 0, settings); err != nil {
		t.Fatal(err)
	}

	application, err := app.New(store, units.DefaultCatalog(), app.Options{
		Configuration: configService,
		AuthEnabled:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// GET /.well-known/savorum without any credentials (must succeed even with AuthEnabled=true)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/.well-known/savorum", nil)
	rec := httptest.NewRecorder()
	application.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", contentType)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	instanceID, _ := body["instanceId"].(string)
	if !ulidRegex.MatchString(instanceID) {
		t.Errorf("instanceId %q is not a valid 26-character ULID", instanceID)
	}

	apiVersion, _ := body["apiVersion"].(string)
	if apiVersion != "1" {
		t.Errorf("apiVersion = %q, want 1", apiVersion)
	}

	serverVersion, _ := body["serverVersion"].(string)
	if serverVersion == "" {
		t.Errorf("serverVersion is empty")
	}

	name, _ := body["name"].(string)
	if name != "Home Kitchen" {
		t.Errorf("name = %q, want %q", name, "Home Kitchen")
	}

	authEnabled, ok := body["authEnabled"].(bool)
	if !ok || !authEnabled {
		t.Errorf("authEnabled = %v, want true", body["authEnabled"])
	}

	headReq := httptest.NewRequestWithContext(t.Context(), http.MethodHead, "/.well-known/savorum", nil)
	headRec := httptest.NewRecorder()
	application.Handler.ServeHTTP(headRec, headReq)

	if headRec.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d, want %d", headRec.Code, http.StatusOK)
	}

	if headRec.Body.Len() != 0 {
		t.Errorf("HEAD body length = %d, want 0", headRec.Body.Len())
	}
}
