// Package configuration owns Savorum's three configuration boundaries:
// restart-bound deployment configuration, mutable instance settings, and the
// rule that personal preferences belong to a user rather than the instance.
package configuration

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"golang.org/x/text/language"
)

var (
	// ErrNotFound means an instance has never persisted settings and should use
	// the application defaults.
	ErrNotFound = errors.New("instance settings were not found")
	// ErrConflict means another writer replaced the settings revision first.
	ErrConflict = errors.New("instance settings changed concurrently")
)

// MeasurementPresentation is the instance fallback for a reader who has not
// chosen a personal preference. It deliberately repeats no unit definitions;
// those remain owned by internal/units.
type MeasurementPresentation string

// Measurement systems control how physical quantities are presented.
const (
	MeasurementOriginal MeasurementPresentation = "original"
	MeasurementMetric   MeasurementPresentation = "metric"
	MeasurementUS       MeasurementPresentation = "us"
	MeasurementAU       MeasurementPresentation = "au"
)

// PageSize is the instance fallback for full-page printing. Automatic lets a
// client choose from its locale; a user's remembered choice still wins.
type PageSize string

// Page choices control the default printed page or card size.
const (
	PageAutomatic PageSize = "automatic"
	PageLetter    PageSize = "letter"
	PageA4        PageSize = "a4"
)

// ImageMode is the instance fallback for recipe photographs in print output.
type ImageMode string

// Image choices control the default image placement in printed output.
const (
	ImageAutomatic ImageMode = "automatic"
	ImageInclude   ImageMode = "include"
	ImageOmit      ImageMode = "omit"
)

// ImportPolicy holds mutable, non-secret instance-level import capabilities.
type ImportPolicy struct {
	AllowExternalFetch bool
	AllowFileUploads   bool
}

// PrintDefaults are instance fallbacks, not personal choices. Browser- or
// account-level preferences take precedence without rewriting this record.
type PrintDefaults struct {
	PageSize PageSize
	Image    ImageMode
}

// InstanceSettings is the complete ordinary persisted configuration record.
// Its closed shape is a safety property: a credential has nowhere to be put.
type InstanceSettings struct {
	DisplayName        string
	PublicURL          string
	DefaultLocale      string
	DefaultMeasurement MeasurementPresentation
	ImportPolicy       ImportPolicy
	PrintDefaults      PrintDefaults
	Revision           uint64
	UpdatedAt          time.Time
}

// DefaultInstanceSettings returns the application defaults used only when no
// persisted value and no deployment override exists.
func DefaultInstanceSettings() InstanceSettings {
	return InstanceSettings{
		DisplayName:        "Savorum",
		DefaultLocale:      "en-US",
		DefaultMeasurement: MeasurementOriginal,
		ImportPolicy: ImportPolicy{
			AllowExternalFetch: true,
			AllowFileUploads:   true,
		},
		PrintDefaults: PrintDefaults{
			PageSize: PageAutomatic,
			Image:    ImageAutomatic,
		},
	}
}

// ValidateInstanceSettings rejects values at the typed service boundary so a
// corrupt database or a future caller fails with an actionable field name.
func ValidateInstanceSettings(settings InstanceSettings) error {
	name := strings.TrimSpace(settings.DisplayName)
	if name == "" || len(name) > 100 {
		return errors.New("instance display name must contain 1 to 100 characters")
	}

	if settings.PublicURL != "" {
		parsed, err := url.Parse(settings.PublicURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return errors.New("instance public URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
		}

		if parsed.Path != "" && parsed.Path != "/" {
			return errors.New("instance public URL must name an origin without a path")
		}
	}

	if _, err := language.Parse(settings.DefaultLocale); err != nil {
		return fmt.Errorf("instance default locale %q is not a valid BCP 47 language tag", settings.DefaultLocale)
	}

	switch settings.DefaultMeasurement {
	case MeasurementOriginal, MeasurementMetric, MeasurementUS, MeasurementAU:
	default:
		return fmt.Errorf("instance default measurement %q must be original, metric, us, or au", settings.DefaultMeasurement)
	}

	switch settings.PrintDefaults.PageSize {
	case PageAutomatic, PageLetter, PageA4:
	default:
		return fmt.Errorf("instance default print page size %q must be automatic, letter, or a4", settings.PrintDefaults.PageSize)
	}

	switch settings.PrintDefaults.Image {
	case ImageAutomatic, ImageInclude, ImageOmit:
	default:
		return fmt.Errorf("instance default print image mode %q must be automatic, include, or omit", settings.PrintDefaults.Image)
	}

	return nil
}

// NormalizeInstanceSettings returns the one canonical representation written
// to SQLite and exposed through the API.
func NormalizeInstanceSettings(settings InstanceSettings) (InstanceSettings, error) {
	settings.DisplayName = strings.TrimSpace(settings.DisplayName)
	settings.PublicURL = strings.TrimSuffix(strings.TrimSpace(settings.PublicURL), "/")

	locale, err := language.Parse(strings.TrimSpace(settings.DefaultLocale))
	if err == nil {
		settings.DefaultLocale = locale.String()
	}

	if err := ValidateInstanceSettings(settings); err != nil {
		return InstanceSettings{}, err
	}

	return settings, nil
}
