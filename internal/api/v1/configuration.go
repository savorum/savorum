package v1

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/configuration"
)

// InstanceConfiguration is the deliberately safe public projection. Process
// paths, listen addresses, trusted proxies, override sources, and secrets are
// not members of this type and therefore cannot leak through serialization.
type InstanceConfiguration struct {
	DisplayName        string `json:"display_name" maxLength:"100"`
	PublicURL          string `json:"public_url,omitempty" format:"uri"`
	DefaultLocale      string `json:"default_locale" maxLength:"100"`
	DefaultMeasurement string `json:"default_measurement" enum:"original,metric,us,au"`
	ImportPolicy       struct {
		AllowExternalFetch bool `json:"allow_external_fetch"`
		AllowFileUploads   bool `json:"allow_file_uploads"`
	} `json:"import_policy"`
	PrintDefaults struct {
		PageSize string `json:"page_size" enum:"automatic,letter,a4"`
		Image    string `json:"image" enum:"automatic,include,omit"`
	} `json:"print_defaults"`
	Capabilities struct {
		MutableInstanceSettings bool `json:"mutable_instance_settings" doc:"Whether this build exposes a mutable instance-settings API"`
		Authentication          bool `json:"authentication" doc:"False until an authentication system is configured"`
	} `json:"capabilities"`
}

type configurationOutput struct {
	Body InstanceConfiguration
}

func registerConfiguration(api huma.API, service ConfigurationService, authEnabled bool) {
	huma.Register(api, huma.Operation{
		OperationID: "get-instance-configuration",
		Method:      "GET",
		Path:        "/api/v1/configuration",
		Summary:     "Get instance configuration",
		Tags:        []string{"Instance"},
	}, func(ctx context.Context, _ *struct{}) (*configurationOutput, error) {
		settings, err := service.Current(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("Could not resolve instance configuration", err)
		}

		body := fromConfiguration(settings)
		body.Capabilities.Authentication = authEnabled

		return &configurationOutput{Body: body}, nil
	})
}

func fromConfiguration(settings configuration.InstanceSettings) InstanceConfiguration {
	result := InstanceConfiguration{
		DisplayName:        settings.DisplayName,
		PublicURL:          settings.PublicURL,
		DefaultLocale:      settings.DefaultLocale,
		DefaultMeasurement: string(settings.DefaultMeasurement),
	}
	result.ImportPolicy.AllowExternalFetch = settings.ImportPolicy.AllowExternalFetch
	result.ImportPolicy.AllowFileUploads = settings.ImportPolicy.AllowFileUploads
	result.PrintDefaults.PageSize = string(settings.PrintDefaults.PageSize)
	result.PrintDefaults.Image = string(settings.PrintDefaults.Image)

	return result
}
