package v1

import (
	"context"
	"errors"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/preferences"
)

// PreferencesService supplies current-user presentation settings.
type PreferencesService interface {
	Current(context.Context, string) (preferences.Record, error)
	Replace(context.Context, string, uint64, preferences.Values) (preferences.Record, error)
}

// PreferenceValues is the version-one typed preference wire contract.
type PreferenceValues struct {
	UILocale             string   `json:"ui_locale,omitempty" maxLength:"100"`
	ContentLanguages     []string `json:"content_languages,omitempty" maxItems:"20"`
	Timezone             string   `json:"timezone,omitempty" maxLength:"100"`
	Measurement          string   `json:"measurement,omitempty" enum:",original,metric,us,au"`
	Temperature          string   `json:"temperature,omitempty" enum:",original,celsius,fahrenheit"`
	PrintPageSize        string   `json:"print_page_size,omitempty" enum:",automatic,letter,a4"`
	PrintImage           string   `json:"print_image,omitempty" enum:",automatic,include,omit"`
	RecipeView           string   `json:"recipe_view,omitempty" enum:",comfortable,compact"`
	ReducedMotion        string   `json:"reduced_motion,omitempty" enum:",system,reduce"`
	Appearance           string   `json:"appearance,omitempty" enum:",system,light,dark"`
	MicrowaveOutputWatts *int     `json:"microwave_output_watts,omitempty" minimum:"0" maximum:"5000"`
}

// UserPreferences separates explicit choices from resolved defaults.
type UserPreferences struct {
	Version   int              `json:"version"`
	Revision  uint64           `json:"revision"`
	Overrides PreferenceValues `json:"overrides"`
	Effective PreferenceValues `json:"effective"`
	// InterfaceLocales are the interface languages this build ships.
	InterfaceLocales []InterfaceLocale `json:"interface_locales" doc:"Interface languages this server ships. A preview locale is incomplete or unreviewed and is used only when chosen explicitly"`
}

// InterfaceLocale is one shipped interface catalog and its readiness.
type InterfaceLocale struct {
	Tag    string `json:"tag" doc:"BCP 47 tag"`
	Status string `json:"status" enum:"supported,preview"`
}

func interfaceLocales() []InterfaceLocale {
	result := make([]InterfaceLocale, 0, len(preferences.SupportedLocales)+len(preferences.PreviewLocales))
	for _, tag := range append(slices.Clone(preferences.SupportedLocales), preferences.PreviewLocales...) {
		result = append(result, InterfaceLocale{Tag: tag, Status: preferences.LocaleStatus(tag)})
	}

	return result
}

type (
	preferencesOutput struct{ Body UserPreferences }
	preferencesInput  struct {
		AcceptLanguage string `header:"Accept-Language"`
		Body           struct {
			Revision  uint64           `json:"revision"`
			Overrides PreferenceValues `json:"overrides"`
		}
	}
)

func registerPreferences(api huma.API, service PreferencesService) {
	huma.Register(api, huma.Operation{OperationID: "get-current-user-preferences", Method: "GET", Path: "/api/v1/me/preferences", Summary: "Get preferences", Tags: []string{"Account"}}, func(ctx context.Context, input *struct {
		AcceptLanguage string `header:"Accept-Language"`
	},
	) (*preferencesOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("Authentication required")
		}

		record, err := service.Current(preferences.WithBrowserLanguages(ctx, input.AcceptLanguage), principal.UserID)

		return preferenceResponse(record, err)
	})
	huma.Register(api, huma.Operation{OperationID: "replace-current-user-preferences", Method: "PUT", Path: "/api/v1/me/preferences", Summary: "Replace preferences", Tags: []string{"Account"}}, func(ctx context.Context, input *preferencesInput) (*preferencesOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("Authentication required")
		}

		requestContext := preferences.WithBrowserLanguages(ctx, input.AcceptLanguage)

		current, err := service.Current(requestContext, principal.UserID)
		if err != nil {
			return preferenceResponse(preferences.Record{}, err)
		}

		overrides := preferenceValuesToDomain(input.Body.Overrides)
		// Omission is how pre-microwave clients serialize this object. Preserve
		// the existing value; an aware client sends an explicit zero to clear it.
		if input.Body.Overrides.MicrowaveOutputWatts == nil {
			overrides.MicrowaveOutputWatts = current.Overrides.MicrowaveOutputWatts
		}

		record, err := service.Replace(requestContext, principal.UserID, input.Body.Revision, overrides)

		return preferenceResponse(record, err)
	})
}

func preferenceResponse(record preferences.Record, err error) (*preferencesOutput, error) {
	if errors.Is(err, preferences.ErrConflict) {
		return nil, huma.Error409Conflict("preferences_revision_conflict")
	}

	if errors.Is(err, preferences.ErrInvalid) {
		return nil, huma.Error422UnprocessableEntity("invalid_preferences", err)
	}

	if err != nil {
		return nil, huma.Error500InternalServerError("Could not read or save preferences", err)
	}

	return &preferencesOutput{Body: UserPreferences{
		Version: record.Version, Revision: record.Revision,
		Overrides: preferenceValuesFromDomain(record.Overrides), Effective: preferenceValuesFromDomain(record.Effective),
		InterfaceLocales: interfaceLocales(),
	}}, nil
}

func preferenceValuesToDomain(value PreferenceValues) preferences.Values {
	result := preferences.Values{UILocale: value.UILocale, ContentLanguages: value.ContentLanguages, Timezone: value.Timezone, Measurement: value.Measurement, Temperature: value.Temperature, PrintPageSize: value.PrintPageSize, PrintImage: value.PrintImage, RecipeView: value.RecipeView, ReducedMotion: value.ReducedMotion, Appearance: value.Appearance}
	if value.MicrowaveOutputWatts != nil {
		result.MicrowaveOutputWatts = *value.MicrowaveOutputWatts
	}

	return result
}

func preferenceValuesFromDomain(value preferences.Values) PreferenceValues {
	result := PreferenceValues{UILocale: value.UILocale, ContentLanguages: value.ContentLanguages, Timezone: value.Timezone, Measurement: value.Measurement, Temperature: value.Temperature, PrintPageSize: value.PrintPageSize, PrintImage: value.PrintImage, RecipeView: value.RecipeView, ReducedMotion: value.ReducedMotion, Appearance: value.Appearance}
	if value.MicrowaveOutputWatts != 0 {
		watts := value.MicrowaveOutputWatts
		result.MicrowaveOutputWatts = &watts
	}

	return result
}
