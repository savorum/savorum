package configuration

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Repository persists only ordinary instance settings. Deployment values and
// secrets are intentionally absent from this contract.
type Repository interface {
	LoadInstanceSettings(ctx context.Context) (InstanceSettings, error)
	ReplaceInstanceSettings(ctx context.Context, expectedRevision uint64, settings InstanceSettings) (InstanceSettings, error)
}

// InstanceOverrides are restart-bound values supplied by CLI or environment.
// A pointer distinguishes an explicit empty value from no override.
type InstanceOverrides struct {
	DisplayName        *string
	PublicURL          *string
	DefaultLocale      *string
	DefaultMeasurement *MeasurementPresentation
}

// Service resolves the effective instance settings without ever writing a
// deployment override back to SQLite.
type Service struct {
	repository Repository
	overrides  InstanceOverrides
	clock      func() time.Time
}

// NewService creates a configuration service over durable settings.
func NewService(repository Repository, overrides InstanceOverrides) (*Service, error) {
	if repository == nil {
		return nil, errors.New("configuration repository is required")
	}

	service := &Service{repository: repository, overrides: overrides, clock: time.Now}
	if _, err := service.applyOverrides(DefaultInstanceSettings()); err != nil {
		return nil, fmt.Errorf("validate deployment setting overrides: %w", err)
	}

	return service, nil
}

// Current returns persisted settings with explicit deployment overrides
// applied. Precedence is CLI > environment > persisted > application default;
// CLI/environment precedence is resolved before InstanceOverrides reaches the
// service.
func (s *Service) Current(ctx context.Context) (InstanceSettings, error) {
	settings, err := s.repository.LoadInstanceSettings(ctx)
	if errors.Is(err, ErrNotFound) {
		settings = DefaultInstanceSettings()
	} else if err != nil {
		return InstanceSettings{}, fmt.Errorf("load instance settings: %w", err)
	}

	settings, err = NormalizeInstanceSettings(settings)
	if err != nil {
		return InstanceSettings{}, fmt.Errorf("validate persisted instance settings: %w", err)
	}

	return s.applyOverrides(settings)
}

// Replace validates and persists ordinary settings. Overrides remain external
// and therefore cannot silently mutate the record being edited.
func (s *Service) Replace(ctx context.Context, expectedRevision uint64, settings InstanceSettings) (InstanceSettings, error) {
	normalized, err := NormalizeInstanceSettings(settings)
	if err != nil {
		return InstanceSettings{}, err
	}

	normalized.UpdatedAt = s.clock().UTC()

	written, err := s.repository.ReplaceInstanceSettings(ctx, expectedRevision, normalized)
	if err != nil {
		return InstanceSettings{}, fmt.Errorf("replace instance settings: %w", err)
	}

	return written, nil
}

func (s *Service) applyOverrides(settings InstanceSettings) (InstanceSettings, error) {
	if value := s.overrides.DisplayName; value != nil {
		settings.DisplayName = *value
	}

	if value := s.overrides.PublicURL; value != nil {
		settings.PublicURL = *value
	}

	if value := s.overrides.DefaultLocale; value != nil {
		settings.DefaultLocale = *value
	}

	if value := s.overrides.DefaultMeasurement; value != nil {
		settings.DefaultMeasurement = *value
	}

	return NormalizeInstanceSettings(settings)
}
