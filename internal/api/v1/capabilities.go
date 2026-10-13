package v1

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/buildinfo"
	"github.com/savorum/savorum/internal/event"
	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/sref"
)

// CapabilityAdapter contains only public, registered adapter metadata.
type CapabilityAdapter struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Version    string   `json:"version,omitempty"`
	InputKinds []string `json:"input_kinds,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
	MediaTypes []string `json:"media_types,omitempty"`
	Collection bool     `json:"collection"`
}

// CapabilityAuthMethod identifies an interactive method without deployment data.
type CapabilityAuthMethod struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// Capabilities uses API majors for semantics and stable IDs for feature presence.
type Capabilities struct {
	SchemaVersion int                    `json:"schema_version"`
	Product       string                 `json:"product"`
	Version       string                 `json:"version"`
	APIMajor      int                    `json:"api_major"`
	AuthEnabled   bool                   `json:"auth_enabled"`
	AuthMethods   []CapabilityAuthMethod `json:"auth_methods"`
	AuthFeatures  []string               `json:"auth_features,omitempty"`
	SREFRead      []string               `json:"sref_read,omitempty"`
	SREFWrite     []string               `json:"sref_write,omitempty"`
	Importers     []CapabilityAdapter    `json:"importers,omitempty"`
	Exporters     []CapabilityAdapter    `json:"exporters,omitempty"`
	Features      []string               `json:"features,omitempty"`
	EventTypes    []string               `json:"event_types,omitempty"`
	Limits        *CapabilityLimits      `json:"limits,omitempty"`
}

// CapabilityLimits reports enforced bounds rather than internal configuration.
type CapabilityLimits struct {
	SuggestionItems      int   `json:"suggestion_items"`
	LookupItems          int   `json:"lookup_items"`
	GroceryAdditionItems int   `json:"grocery_addition_items"`
	RetryReceiptHours    int   `json:"retry_receipt_hours"`
	BulkItems            int   `json:"bulk_items"`
	ExportRecipes        int   `json:"export_recipes"`
	PrintRecipes         int   `json:"print_recipes,omitempty" doc:"Most recipes one printed document holds, when printing is available"`
	PantryEntries        int   `json:"pantry_entries"`
	AsyncItems           int   `json:"async_items"`
	ImportFileBytes      int64 `json:"import_file_bytes"`
	RequestBodyBytes     int64 `json:"request_body_bytes"`
	ExportArtifactBytes  int64 `json:"export_artifact_bytes"`
	ArtifactHours        int   `json:"artifact_hours"`
}

func registerCapabilities(api huma.API, options Options, printRecipes int) {
	base := Capabilities{SchemaVersion: 1, Product: "Savorum", Version: buildinfo.CurrentVersion(), APIMajor: 1, AuthEnabled: options.AuthEnabled, AuthMethods: options.AuthMethods}
	if base.AuthMethods == nil {
		base.AuthMethods = []CapabilityAuthMethod{}
	}

	if options.AuthEnabled {
		base.AuthFeatures = []string{"auth.native_sign_in", "auth.native_link"}

		for _, method := range base.AuthMethods {
			if method.Kind == "managed" {
				base.AuthFeatures = append(base.AuthFeatures, "auth.managed_session", "auth.managed_native_session")
				break
			}
		}
	}

	huma.Register(api, huma.Operation{OperationID: "get-capabilities", Method: http.MethodGet, Path: "/api/v1/capabilities", Summary: "Discover capabilities", Description: "Unauthenticated requests return product, API compatibility, interactive sign-in methods, and sign-in features only. `auth.native_sign_in` means a native app can complete OIDC, SAML, and managed sign-in through `begin-native-provider-login` and `finish-native-provider-login`. `auth.native_link` means a signed-in native app can link an OIDC or SAML identity through `create-native-link-ticket`, `begin-native-provider-login`, and `finish-native-provider-link`. `auth.managed_session` means a managed provider is configured and its web and native sign-in use short-lived, tenant-audienced grants after PKCE code redemption. `auth.managed_native_session` means a native client can redeem a scoped hosting grant through `finish-managed-native-login` for an ordinary local session. Authenticated requests add registered formats, optional features, and enforced bounds. `recipes.search.ingredient` means ingredient: queries match indexed ingredient names, including optional and alternative names, with original-line fallback only when the name is absent. All words in one qualifier must match the same ingredient. Capabilities describe availability, not a grant of authorization. Schema version 1 permits additive optional fields and new IDs; clients ignore unknown IDs. Cache for the current session and refresh after reconnect or configuration changes.", Tags: []string{"Instance"}}, func(ctx context.Context, _ *struct{}) (*struct{ Body Capabilities }, error) {
		value := base
		if p, ok := identity.FromContext(ctx); ok && p.UserID != "" {
			value = authenticatedCapabilities(value, options, printRecipes)
		}

		return &struct{ Body Capabilities }{value}, nil
	})
}

func authenticatedCapabilities(value Capabilities, options Options, printRecipes int) Capabilities {
	value.SREFRead = append(append([]string{}, sref.EarlierReadableFormats...), sref.Version)
	value.SREFWrite = []string{sref.Version}

	value.Features = []string{"recipes", "recipes.search.ingredient", "api.preconditions"}
	if options.Idempotency != nil {
		value.Features = append(value.Features, "api.idempotency")
	}

	if options.Selection != nil {
		value.Features = append(value.Features, "selections.suggestions", "selections.lookup")
	}

	if options.Meals != nil {
		value.Features = append(value.Features, "meal_planning")
	}

	if options.Shopping != nil {
		value.Features = append(value.Features, "shopping")
	}

	if options.Pantry != nil {
		value.Features = append(value.Features, "pantry.presence")
	}

	if options.Webhooks != nil {
		value.Features = append(value.Features, "webhooks")
	}

	if options.Automation != nil {
		value.Features = append(value.Features, "bulk.jobs", "export.jobs")
	}

	if options.Automation != nil && options.ImportJobs != nil && options.Exchange != nil {
		value.Features = append(value.Features, "import.automation")
	}

	if options.ImportJobs != nil {
		value.Features = append(value.Features, "import.jobs")
	}

	if options.Events != nil {
		value.Features = append(value.Features, "events.sse")
		value.EventTypes = event.Types()
	}

	if formats, ok := options.Exchange.(interface{ Formats() []importer.Format }); ok {
		advertised := formats.Formats()
		if available, ok := options.Exchange.(interface{ AvailableFormats() []importer.Format }); ok {
			advertised = available.AvailableFormats()
		}

		for _, format := range advertised {
			value.Importers = append(value.Importers, CapabilityAdapter{ID: format.ID, Name: format.Name, Version: format.Version, InputKinds: format.InputKinds, Extensions: format.Extensions, MediaTypes: format.MediaTypes, Collection: format.Capabilities.Collection})
		}
	}

	for _, format := range export.Formats() {
		value.Exporters = append(value.Exporters, CapabilityAdapter{ID: string(format.ID), Name: format.Label, Extensions: []string{format.Extension}, MediaTypes: []string{format.MediaType}, Collection: format.SupportsCollection})
	}

	value.Limits = &CapabilityLimits{SuggestionItems: options.SuggestionLimit, LookupItems: options.LookupLimit, GroceryAdditionItems: options.GroceryAdditionLimit, RetryReceiptHours: 24, BulkItems: options.BulkLimit, ExportRecipes: export.MaximumSelectionRecipes, PrintRecipes: printRecipes, PantryEntries: 2000, AsyncItems: 10000, ImportFileBytes: 64 << 20, RequestBodyBytes: 16 << 20, ExportArtifactBytes: 512 << 20, ArtifactHours: 24}

	return value
}
