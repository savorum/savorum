// Package preferences owns typed personal presentation state, independent of recipes.
package preferences

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/configuration"
	"golang.org/x/text/language"
)

var (
	// ErrConflict means another device saved a newer revision.
	ErrConflict = errors.New("preferences changed concurrently")
	// ErrInvalid identifies a preference validation failure.
	ErrInvalid = errors.New("invalid preference")
)

// Values contains explicit overrides. Empty fields inherit instance/built-in defaults.
// UI locale is independent of ordered authored-content languages and measurement region.
type Values struct {
	UILocale             string   `json:"ui_locale,omitempty"`
	ContentLanguages     []string `json:"content_languages,omitempty"`
	Timezone             string   `json:"timezone,omitempty"`
	Measurement          string   `json:"measurement,omitempty"`
	Temperature          string   `json:"temperature,omitempty"`
	PrintPageSize        string   `json:"print_page_size,omitempty"`
	PrintImage           string   `json:"print_image,omitempty"`
	RecipeView           string   `json:"recipe_view,omitempty"`
	ReducedMotion        string   `json:"reduced_motion,omitempty"`
	Appearance           string   `json:"appearance,omitempty"`
	MicrowaveOutputWatts int      `json:"microwave_output_watts,omitempty"`
}

// Record exposes explicit overrides separately from resolved presentation values.
type Record struct {
	Version   int    `json:"version"`
	Revision  uint64 `json:"revision"`
	Overrides Values `json:"overrides"`
	Effective Values `json:"effective"`
}

// Repository stores versioned preferences by application user ID.
type Repository interface {
	LoadPreferences(context.Context, string) (Record, error)
	ReplacePreferences(context.Context, string, uint64, Values) (Record, error)
}

// Configuration supplies resolved instance defaults.
type Configuration interface {
	Current(context.Context) (configuration.InstanceSettings, error)
}

// Service validates personal overrides and resolves deterministic defaults.
type Service struct {
	repository    Repository
	configuration Configuration
}

// NewService connects durable user preferences to instance defaults.
func NewService(repository Repository, configuration Configuration) *Service {
	return &Service{repository: repository, configuration: configuration}
}

// Normalize validates supported values and canonicalizes language tags.
func Normalize(value Values) (Values, error) {
	if value.MicrowaveOutputWatts != 0 && (value.MicrowaveOutputWatts < 100 || value.MicrowaveOutputWatts > 5000) {
		return Values{}, fmt.Errorf("%w: microwave_output_watts", ErrInvalid)
	}

	if strings.Contains(value.UILocale, "_") {
		return Values{}, fmt.Errorf("%w: ui_locale", ErrInvalid)
	}

	if value.UILocale != "" {
		tag, err := language.Parse(value.UILocale)
		if err != nil {
			return Values{}, fmt.Errorf("%w: ui_locale", ErrInvalid)
		}

		value.UILocale = tag.String()
	}

	if len(value.ContentLanguages) > 20 {
		return Values{}, fmt.Errorf("%w: content_languages", ErrInvalid)
	}

	languages := make([]string, 0, len(value.ContentLanguages))
	for _, raw := range value.ContentLanguages {
		tag, err := language.Parse(raw)
		if err != nil || raw == "" || strings.Contains(raw, "_") {
			return Values{}, fmt.Errorf("%w: content_languages", ErrInvalid)
		}

		if !slices.Contains(languages, tag.String()) {
			languages = append(languages, tag.String())
		}
	}

	value.ContentLanguages = languages
	if value.Timezone != "" {
		if value.Timezone == "Local" {
			return Values{}, fmt.Errorf("%w: timezone", ErrInvalid)
		}

		if _, err := time.LoadLocation(value.Timezone); err != nil {
			return Values{}, fmt.Errorf("%w: timezone", ErrInvalid)
		}
	}

	for _, entry := range []struct {
		name, value string
		choices     []string
	}{
		{"measurement", value.Measurement, []string{"", "original", "metric", "us", "au"}},
		{"temperature", value.Temperature, []string{"", "original", "celsius", "fahrenheit"}},
		{"print_page_size", value.PrintPageSize, []string{"", "automatic", "letter", "a4"}},
		{"print_image", value.PrintImage, []string{"", "automatic", "include", "omit"}},
		{"recipe_view", value.RecipeView, []string{"", "comfortable", "compact"}},
		{"reduced_motion", value.ReducedMotion, []string{"", "system", "reduce"}},
		{"appearance", value.Appearance, []string{"", "system", "light", "dark"}},
	} {
		if !slices.Contains(entry.choices, entry.value) {
			return Values{}, fmt.Errorf("%w: %s", ErrInvalid, entry.name)
		}
	}

	return value, nil
}

// Current reads only the explicitly named application user.
func (s *Service) Current(ctx context.Context, userID string) (Record, error) {
	if userID == "" {
		return Record{}, fmt.Errorf("%w: user required", ErrInvalid)
	}

	record, err := s.repository.LoadPreferences(ctx, userID)
	if err != nil {
		return Record{}, err
	}

	settings, err := s.configuration.Current(ctx)
	if err != nil {
		return Record{}, err
	}

	record.Version = 1
	record.Effective = Resolve(record.Overrides, settings)
	record.Effective.UILocale = ResolveLocale(record.Overrides.UILocale, settings.DefaultLocale, browserLanguages(ctx), SupportedLocales, PreviewLocales)

	return record, nil
}

// Replace saves a complete override set with optimistic concurrency.
func (s *Service) Replace(ctx context.Context, userID string, revision uint64, value Values) (Record, error) {
	if userID == "" {
		return Record{}, fmt.Errorf("%w: user required", ErrInvalid)
	}

	normalized, err := Normalize(value)
	if err != nil {
		return Record{}, err
	}

	if _, err = s.repository.ReplacePreferences(ctx, userID, revision, normalized); err != nil {
		return Record{}, err
	}

	return s.Current(ctx, userID)
}

// Resolve resolves independent presentation defaults; Current additionally matches shipped catalogs.
func Resolve(value Values, instance configuration.InstanceSettings) Values {
	if value.UILocale == "" {
		value.UILocale = instance.DefaultLocale
	}

	if value.UILocale == "" {
		value.UILocale = "en"
	}

	if len(value.ContentLanguages) == 0 {
		value.ContentLanguages = []string{"en"}
	}

	if value.Timezone == "" {
		value.Timezone = "UTC"
	}

	if value.Measurement == "" {
		value.Measurement = string(instance.DefaultMeasurement)
	}

	if value.Measurement == "" {
		value.Measurement = "original"
	}

	if value.Temperature == "" {
		value.Temperature = "original"
	}

	if value.PrintPageSize == "" {
		value.PrintPageSize = string(instance.PrintDefaults.PageSize)
	}

	if value.PrintPageSize == "" {
		value.PrintPageSize = "automatic"
	}

	if value.PrintImage == "" {
		value.PrintImage = string(instance.PrintDefaults.Image)
	}

	if value.PrintImage == "" {
		value.PrintImage = "automatic"
	}

	if value.RecipeView == "" {
		value.RecipeView = "comfortable"
	}

	if value.ReducedMotion == "" {
		value.ReducedMotion = "system"
	}

	if value.Appearance == "" {
		value.Appearance = "system"
	}

	return value
}

// SupportedLocales lists interface catalogs released as supported. Keep
// aligned with web/locales.json.
var SupportedLocales = []string{
	"en", "ar", "cs", "da", "de", "el", "es", "fi", "fr", "he", "hu", "id", "it", "ja", "ko", "nb",
	"nl", "pl", "pt-BR", "pt-PT", "ro", "ru", "sv", "th", "tr", "uk", "vi", "zh-Hans", "zh-Hant",
}

// PreviewLocales lists shipped interface catalogs that are incomplete or not
// yet reviewed. Only an explicit account or instance choice selects one;
// browser language never does.
var PreviewLocales = []string{}

// LocaleStatus reports whether a shipped interface locale is supported or a
// preview, and is empty for a locale this build does not ship.
func LocaleStatus(tag string) string {
	switch {
	case slices.Contains(SupportedLocales, tag):
		return "supported"
	case slices.Contains(PreviewLocales, tag):
		return "preview"
	default:
		return ""
	}
}

type browserLanguagesKey struct{}

// WithBrowserLanguages supplies request-scoped hints, never stored as account overrides.
func WithBrowserLanguages(ctx context.Context, acceptLanguage string) context.Context {
	return context.WithValue(ctx, browserLanguagesKey{}, acceptLanguage)
}

func browserLanguages(ctx context.Context) string {
	value, _ := ctx.Value(browserLanguagesKey{}).(string)
	return value
}

// ResolveLocale uses RFC 4647 lookup, in explicit, instance, browser, built-in order.
// Explicit and instance choices may select a preview locale; browser hints
// select only a supported one. Unsupported choices remain in Overrides so
// installing a catalog can activate them.
func ResolveLocale(explicit, instance, browser string, supported, preview []string) string {
	chosen := append(slices.Clone(supported), preview...)
	for _, candidate := range []string{explicit, instance} {
		if match := lookupLocale(candidate, chosen); match != "" {
			return match
		}
	}

	candidates := []string{}

	tags, _, err := language.ParseAcceptLanguage(browser)
	if err == nil {
		for _, tag := range tags {
			candidates = append(candidates, tag.String())
		}
	}

	candidates = append(candidates, "en")
	for _, candidate := range candidates {
		if match := lookupLocale(candidate, supported); match != "" {
			return match
		}
	}

	return "en"
}

func lookupLocale(candidate string, available []string) string {
	tag, err := language.Parse(candidate)
	if candidate == "" || err != nil {
		return ""
	}

	lookup := strings.ToLower(tag.String())
	for lookup != "" {
		for _, locale := range available {
			if strings.EqualFold(locale, lookup) {
				return locale
			}
		}

		position := strings.LastIndex(lookup, "-")
		if position < 0 {
			break
		}

		lookup = lookup[:position]
		// Remove a trailing extension singleton together with its removed subtag.
		if last := strings.LastIndex(lookup, "-"); last >= 0 && len(lookup)-last == 2 {
			lookup = lookup[:last]
		}
	}

	// A tag lookup cannot shorten reaches a catalog by likely script or an
	// equivalent language, such as zh-CN for zh-Hans or no for nb.
	tags := make([]language.Tag, len(available))
	for index, locale := range available {
		tags[index] = language.Make(locale)
	}

	if len(tags) > 0 {
		if _, index, confidence := language.NewMatcher(tags).Match(tag); confidence >= language.High {
			return available[index]
		}
	}

	return ""
}
