package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/savorum/savorum/internal/configuration"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

func TestInstanceSettingsRoundTripAcrossReopen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "library.db")

	store, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}

	service, err := configuration.NewService(store, configuration.InstanceOverrides{})
	if err != nil {
		t.Fatal(err)
	}

	settings, err := service.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}

	settings.DisplayName = "Family recipes"
	settings.PublicURL = "https://recipes.example/"
	settings.DefaultLocale = "fr-CA"
	settings.DefaultMeasurement = configuration.MeasurementMetric
	settings.ImportPolicy.AllowExternalFetch = false
	settings.PrintDefaults.PageSize = configuration.PageA4

	written, err := service.Replace(ctx, 0, settings)
	if err != nil {
		t.Fatal(err)
	}

	if written.Revision != 1 || written.UpdatedAt.IsZero() {
		t.Fatalf("written metadata = %#v", written)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = reopened.Close() })

	reopenedService, err := configuration.NewService(reopened, configuration.InstanceOverrides{})
	if err != nil {
		t.Fatal(err)
	}

	read, err := reopenedService.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(written, read) {
		t.Fatalf("round trip changed settings\nwritten: %#v\nread: %#v", written, read)
	}

	if _, err := reopenedService.Replace(ctx, 0, read); !errors.Is(err, configuration.ErrConflict) {
		t.Fatalf("stale replacement error = %v, want ErrConflict", err)
	}
}

func TestDeploymentOverrideDoesNotChangePersistedInstanceSettings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)

	persistentService, err := configuration.NewService(store, configuration.InstanceOverrides{})
	if err != nil {
		t.Fatal(err)
	}

	persisted := configuration.DefaultInstanceSettings()
	persisted.DisplayName = "Stored name"

	persisted, err = persistentService.Replace(ctx, 0, persisted)
	if err != nil {
		t.Fatal(err)
	}

	override := "Managed name"

	effectiveService, err := configuration.NewService(store, configuration.InstanceOverrides{DisplayName: &override})
	if err != nil {
		t.Fatal(err)
	}

	effective, err := effectiveService.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if effective.DisplayName != override {
		t.Fatalf("effective display name = %q", effective.DisplayName)
	}

	read, err := store.LoadInstanceSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(persisted, read) {
		t.Fatalf("override changed persistence\nwant: %#v\n got: %#v", persisted, read)
	}
}
