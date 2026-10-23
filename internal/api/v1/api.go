// Package v1 serves version 1 of Savorum's HTTP API. It maps the application
// services onto huma operations and owns its own request and response types:
// the wire format is a published contract, so it is written out here rather
// than borrowed from the domain models, which are free to change beneath it.
package v1

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/savorum/savorum/internal/account"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/automation"
	"github.com/savorum/savorum/internal/configuration"
	"github.com/savorum/savorum/internal/event"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/federation"
	"github.com/savorum/savorum/internal/idempotency"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/pantry"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/routecatalog"
	"github.com/savorum/savorum/internal/webhook"
)

// Version is the supported API version.
const Version = "1"

// RecipeService provides recipe operations to API handlers.
type RecipeService interface {
	Create(ctx context.Context, document recipe.Document) (library.Entry, error)
	Get(ctx context.Context, id library.EntryID) (library.Entry, error)
	GetTrashed(ctx context.Context, id library.EntryID) (library.Entry, error)
	ListPage(ctx context.Context, options library.ListOptions) ([]library.Summary, int, error)
	ListReferences(ctx context.Context, state library.State) ([]library.RecipeReference, error)
	Update(ctx context.Context, id library.EntryID, expectedRevision uint64, document recipe.Document) (library.Entry, error)
	Trash(ctx context.Context, id library.EntryID, expectedRevision uint64) (library.Entry, error)
	Restore(ctx context.Context, id library.EntryID, expectedRevision uint64) (library.Entry, error)
	RestoreRevision(ctx context.Context, id library.EntryID, sourceRevision, expectedRevision uint64) (library.Entry, error)
	GetRevision(ctx context.Context, id library.EntryID, revision uint64) (library.Revision, error)
	ListRevisions(ctx context.Context, id library.EntryID, options library.RevisionListOptions) ([]library.RevisionSummary, error)
	Purge(ctx context.Context, id library.EntryID, expectedRevision uint64) error
	EmptyTrash(ctx context.Context, recipes []library.PurgeTarget, books []library.BookPurgeTarget) error
	PreflightImportIdentities(ctx context.Context, documents []recipe.Document, policy library.DuplicatePolicy) ([]bool, error)
	ImportRecipes(ctx context.Context, documents []recipe.Document, policy library.DuplicatePolicy) ([]library.ImportResult, error)
}

// HealthChecker reports whether an application dependency is healthy.
type HealthChecker interface {
	Ping(ctx context.Context) error
}

// AssetService provides asset operations to API handlers.
type AssetService interface {
	Ingest(ctx context.Context, source io.Reader, options assets.IngestOptions) (assets.Blob, error)
	Metadata(ctx context.Context, id assets.ID) (assets.Blob, error)
	Open(ctx context.Context, id assets.ID) (assets.Blob, io.ReadCloser, error)
}

// AssetReferenceService resolves references to stored assets.
type AssetReferenceService interface {
	Get(context.Context, library.EntryID) (library.Entry, error)
}

// SourceAttachmentService provides retained import-source operations.
type SourceAttachmentService interface {
	ListSourceAttachments(context.Context, library.EntryID, uint64) ([]library.SourceAttachment, error)
	GetSourceAttachment(context.Context, library.EntryID, uint64, string) (library.SourceAttachment, error)
}

// ConfigurationService provides configuration operations to API handlers.
type ConfigurationService interface {
	Current(ctx context.Context) (configuration.InstanceSettings, error)
}

// Options are application capabilities supplied by the composition root.
// Their presence determines which optional routes exist.
type Options struct {
	EventStreamShutdown  <-chan struct{}
	Selection            SelectionService
	SuggestionLimit      int
	LookupLimit          int
	GroceryAdditionLimit int
	Webhooks             *webhook.Service
	Automation           *automation.Service
	Idempotency          *idempotency.Coordinator
	Events               event.Reader
	AuthMethods          []CapabilityAuthMethod
	BulkLimit            int
	Pantry               pantry.Repository
	AuthEnabled          bool
	Preferences          PreferencesService
	Assets               AssetService
	SourceAttachments    SourceAttachmentService
	Configuration        ConfigurationService
	Meals                MealService
	Shopping             ShoppingService
	MealCalendar         MealCalendarService
	// Exchange is supplied when the composition root already holds one,
	// because a background import job and this API must be reviewing the same
	// staged previews rather than each holding their own.
	Exchange ExchangeService
	// ImportJobs makes bulk import durable. Without it imports run
	// synchronously.
	ImportJobs ImportJobService
}

// registerImportRoutes registers every recipe import route and returns the
// exchange service they share.
func registerImportRoutes(api huma.API, mux *routecatalog.Mux, service RecipeService, options Options) ExchangeService {
	exchanger := options.Exchange
	if exchanger == nil {
		exchanger = exchange.NewService(service, options.Assets)
	}

	if refreshURL, ok := exchanger.(SourceRefreshURLService); ok {
		registerSourceRefreshURL(api, refreshURL)
	}

	registerExchange(api, exchanger)
	mux.HandleFunc("POST /api/v1/recipes/import-package", streamPackageImport(exchanger))
	mux.HandleFunc("POST /api/v1/recipes/import-external", streamExternalRecipeImport(exchanger))
	mux.HandleFunc("POST /api/v1/recipes/import-preview", streamImportPreview(exchanger))
	mux.HandleFunc("POST /api/v1/recipes/import-previews/{preview_id}/commit", streamImportPreviewCommit(exchanger))
	mux.HandleFunc("POST /api/v1/recipes/import-previews/{preview_id}/items/{item_id}/text", streamImportPreviewText(exchanger))
	mux.HandleFunc("GET /api/v1/recipes/import-previews/{preview_id}", streamStagedImportPreview(exchanger))
	mux.HandleFunc("GET /api/v1/recipes/import-previews/{preview_id}/items/{item_id}/image", streamImportPreviewImage(exchanger))
	mux.HandleFunc("GET /api/v1/recipes/import-previews/{preview_id}/measurement-resolution-groups/{group_id}", streamMeasurementResolutionGroup(exchanger))
	mux.HandleFunc("POST /api/v1/recipes/import-previews/{preview_id}/measurement-resolution-plans", streamMeasurementResolutionPlan(exchanger))
	mux.HandleFunc("POST /api/v1/recipes/import-previews/{preview_id}/measurement-resolution-plans/{plan_id}/apply", streamMeasurementResolutionPlanApply(exchanger))
	mux.HandleFunc("DELETE /api/v1/recipes/import-previews/{preview_id}/measurement-resolution-plans/{plan_id}", streamMeasurementResolutionPlanRevert(exchanger))
	mux.HandleFunc("DELETE /api/v1/recipes/import-previews/{preview_id}", streamImportPreviewCancel(exchanger))
	// Routed above, described here. Handling these bodies outside huma is
	// no reason for the generated contract to omit them.
	documentDirectImports(api)

	if options.ImportJobs != nil {
		registerImportJobs(api, exchanger, options.ImportJobs, options.Idempotency)
	}

	return exchanger
}

// New registers every supported route. Printing is registered whenever a print
// service is supplied; an instance without one simply has no print routes
// rather than routes that fail only when called.
func New(service RecipeService, health HealthChecker, printer PrintService, options Options) (*routecatalog.Mux, huma.API, error) {
	if service == nil {
		return nil, nil, errors.New("recipe service is required")
	}

	if health == nil {
		return nil, nil, errors.New("health checker is required")
	}

	if options.SuggestionLimit <= 0 {
		options.SuggestionLimit = 50
	}

	if options.LookupLimit <= 0 {
		options.LookupLimit = 200
	}

	if options.GroceryAdditionLimit <= 0 {
		options.GroceryAdditionLimit = 200
	}

	if options.Selection == nil {
		options.Selection, _ = health.(SelectionService)
	}

	if options.BulkLimit <= 0 {
		options.BulkLimit = 100
	}

	mux := routecatalog.New()
	humaMux := http.NewServeMux()
	config := huma.DefaultConfig("Savorum API", Version)
	config.OpenAPIPath = "/openapi"
	config.DocsPath = ""
	config.SchemasPath = ""
	config.RejectUnknownQueryParameters = true
	// DefaultConfig's schema-link hook advertises SchemasPath, which this API
	// does not serve.
	config.CreateHooks = nil
	config.Transformers = append(config.Transformers, contractErrors)
	baseAPI := humago.New(humaMux, config)
	api := &retryAPI{API: baseAPI, receipts: options.Idempotency, recipes: service}
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		next(huma.WithContext(ctx, context.WithValue(ctx.Context(), retryKeyContext{}, ctx.Header("Idempotency-Key"))))
	})
	api.authority, _ = health.(interface {
		AuthorizeCreation(context.Context, string) error
	})
	api.cooking, _ = health.(interface {
		GetCooking(context.Context, string, string) (library.CookingRecord, error)
	})
	describeAPI(api.OpenAPI())
	describeTags(api.OpenAPI())

	registerHealth(api, health)
	account.RegisterOpenAPI(api)
	federation.RegisterOpenAPI(api)

	if options.Preferences != nil {
		registerPreferences(api, options.Preferences)
	}

	if options.Configuration != nil {
		registerConfiguration(api, options.Configuration, options.AuthEnabled)
	}

	registerRecipes(api, service)

	if options.Selection != nil {
		registerSelections(api, options.Selection, options)
	}

	if authors, ok := service.(RecipeAuthorService); ok {
		registerRecipeAuthors(api, authors)
	}

	registerIngredientLines(api)

	if options.Pantry != nil {
		registerPantry(api, options.Pantry)
	}

	if options.Meals != nil {
		registerMeals(api, options.Meals)
	}

	if options.MealCalendar != nil {
		registerMealCalendar(api, options.MealCalendar)
	}

	if options.Shopping != nil {
		registerShopping(api, options.Shopping)
		registerGroceryAdditions(api, options.Shopping, options.GroceryAdditionLimit)
		registerShoppingListManagement(api, options.Shopping)
	}

	if refresh, ok := service.(SourceRefreshService); ok {
		registerSourceRefresh(api, refresh)
	}

	if merger, ok := service.(RecipeMergeService); ok {
		registerRecipeMerge(api, merger)
	}

	if history, ok := service.(ImportHistoryService); ok {
		registerImportHistory(api, history)
	}

	if options.Assets != nil {
		registerAssets(api, service, options.Assets, options.SourceAttachments)
		mux.HandleFunc("POST /api/v1/assets", streamAssetUpload(options.Assets))
	}

	if organization, ok := service.(OrganizationService); ok {
		registerOrganization(api, organization, service)
	}

	if printer != nil {
		registerPrint(api, printer)
	}
	// Recipe import needs the asset store as well as the library, because a
	// package carries the bytes of the photographs its recipe declares.
	if options.Assets != nil {
		options.Exchange = registerImportRoutes(api, mux, service, options)
	}

	// Export is registered whether or not this instance stores files.
	if reader, exportable := service.(export.Library); exportable {
		var blobs export.Assets
		if options.Assets != nil {
			blobs = options.Assets
		}

		exporter, err := export.NewService(reader, blobs)
		if err != nil {
			return nil, nil, err
		}

		registerExport(api, exporter)
	}

	if options.Automation != nil {
		registerAutomation(api, service, options)
	}

	if options.Events != nil {
		registerEvents(api, options.Events, options.EventStreamShutdown)
	}

	if options.Webhooks != nil {
		registerWebhooks(api, options.Webhooks)
	}

	registerCapabilities(api, options, printRecipeLimit(printer))
	documentProblemExtensions(api)
	documentResponseCompatibility(api)
	describeSecurity(api.OpenAPI())

	mux.Handle("/api/", humaMux)
	mux.Handle("/openapi.json", humaMux)

	describeRetry(api)

	return mux, api, nil
}

type getBookAssetDerivativeInput struct {
	IfNoneMatch string `header:"If-None-Match" maxLength:"4096" doc:"Revalidate a cached image after checking current access"`
	BookID      string `path:"book_id"`
	ID          string `path:"id" pattern:"^sha256:[a-f0-9]{64}$"`
	Preset      string `path:"preset" pattern:"^(card|reader|thumb)$"`
}

// describeAPI fills in the document's own metadata, including how to
// authenticate.
func describeAPI(document *huma.OpenAPI) {
	document.Info.Description = strings.Join([]string{
		"The /api/v1 namespace is the compatibility boundary for browser, native, and external clients. Optional response fields and new operations may be added. Removing or renaming fields, changing meaning, tightening accepted inputs, or changing behavior through defaults requires a new API major. Clients must ignore unknown response fields.",
		"",
		"Each instance serves a guide for agents and integrations at `/llms.txt`, and serves this document at `/openapi.json` with its own address in `servers`.",
		"",
		"### Credentials and local mode",
		"",
		"An instance started without `--auth-enabled` runs in local mode: it authenticates nobody and acts as one persisted local user, so keep it on loopback or behind an access-controlled network or proxy.",
		"",
		"With authentication enabled, a browser session or a personal API token is required. Create a token in the web app under Account, API tokens (`/account/tokens`). Send it as `Authorization: Bearer <token>`, and name a library with `X-Savorum-Library: <library-id>` when the token reaches more than one. A token's authority is the intersection of its scopes, its optional library restriction, and the current membership of the account that owns it.",
		"",
		"A request using any method other than `GET`, `HEAD`, or `OPTIONS` passes an origin check first. A cookie-authenticated one additionally requires the session-bound `X-CSRF-Token` header. Bearer requests set no cookie and need no CSRF header.",
		"",
		"The `bearerToken` and `browserSession` security schemes name these credentials. Operations that answer without credentials declare that explicitly.",
		"",
		"### Extension boundary",
		"",
		"External integrations use the authenticated HTTP API, versioned events, signed webhooks, SREF, and iCalendar. Internal import, export, authentication, and extraction adapters are compiled, typed components over canonical models and services. They receive no database handle. Adapter metadata is registered by the capability implementation, not a frontend list. There is no Go native plugin, shared-library loader, server-script runtime, plugin SQL migration, or plugin package manager. Any future richer extension protocol must be out of process with explicit capabilities and API authorization.",
		"",
		"### Conventions",
		"",
		"Operation IDs are `verb-resource`. Responses that carry a revision use a strong `ETag`, and a conditional write echoes it in `If-Match`. Meals and saved meals differ: their `ETag` validates the representation as returned, and `If-Match` takes their `revision` field as a strong ETag. Problem responses use `application/problem+json` with stable machine-readable codes, field locations, and revision context. Missing write preconditions return 428, stale revisions return 412. Shopping field-edit conflicts retain their documented 409 outcome and explicit resolution flow.",
	}, "\n")
	document.Info.License = &huma.License{Name: "AGPL-3.0-only", Identifier: "AGPL-3.0-only"}
	document.Servers = append(document.Servers, &huma.Server{
		URL:         "http://127.0.0.1:8080",
		Description: "A local instance on its default listener.",
	})
	document.ExternalDocs = &huma.ExternalDocs{Description: "Guide for agents and integrations", URL: "http://127.0.0.1:8080/llms.txt"}
}

// describeTags gives the document an explicit, described tag list. Generated
// documentation renders it in this order: the library, the recipe and what
// wraps it, moving recipes in and out, the reader's account, who else gets in,
// then the instance.
func describeTags(document *huma.OpenAPI) {
	for _, tag := range []struct{ name, description string }{
		{"Libraries", "The container a recipe lives in. Everything below is scoped to one."},
		{"Recipes", "Recipe entries, their revisions, and trash and restore."},
		{"Recipe authors", "Author-name discovery from the selected library's active recipes."},
		{"Plan", "Dated and unscheduled meals, plus reusable meal combinations."},
		{"Shopping", "Shared grocery lists and reviewed ingredient requirements."},
		{"Organization", "A recipe's library-owned layer: tags, facets, and personal state."},
		{"Cooking history", "Records of having cooked a recipe."},
		{"Books", "Curated collections, their chapters, and publication provenance."},
		{"Tags", "The library's tag catalog."},
		{"Facets", "Course, cuisine, method, and dietary catalogs."},
		{"Saved views", "Stored library queries."},
		{"Assets", "Immutable content-addressed uploads and their delivery."},
		{"Import", "Bring recipes in from files, third-party exports, and web pages, and review a staged preview before committing it."},
		{"Import jobs", "The background lifecycle of a committed import: progress, per-recipe outcomes, cancellation, and resumption."},
		{"Export", "Portable recipe and collection downloads as SREF documents, packages, and bundles."},
		{"Print", "Rendered PDF documents for pages and recipe cards."},
		{"Account", "The signed-in account itself: who it is, its password, its preferences, its audit history, and the library it is acting in."},
		{"Sessions", "Signing in and out, and the browser sessions an account holds."},
		{"Passkeys", "WebAuthn registration and usernameless sign-in."},
		{"Recovery codes", "Single-use codes for regaining access to an account."},
		{"API tokens", "Personal bearer credentials for integrations."},
		{"Assistant connections", "Approve assistant access to a library and manage connected assistants."},
		{"Library members", "Who holds a role in a library, and administrator recovery of its ownership."},
		{"Invitations", "Inviting someone to a library and accepting the invitation."},
		{"Sign-in methods", "The identities attached to an account, and the providers they depend on."},
		{"Authentication", "External identity providers, the sign-in flows they use, and the mapping from their groups to library roles."},
		{"Users", "Account administration for the instance."},
		{"Webhooks", "Signed external delivery, owner-managed destinations, attempts, and failure controls."},
		{"Events", "Authorized change notifications; refresh through normal resource APIs."},
		{"Bulk", "Bounded recipe operations using ordinary resource authorization and revisions."},
		{"Jobs", "Typed bulk and transfer work, retained for 24 hours."},
		{"Pantry", "Optional household presence and approximate stock, reviewed explicitly."},
		{"Instance", "The settings, capabilities, and readiness a client may read."},
	} {
		document.Tags = append(document.Tags, &huma.Tag{Name: tag.name, Description: tag.description})
	}
}

// printRecipeLimit is zero when the instance has no print routes.
func printRecipeLimit(printer PrintService) int {
	if printer == nil {
		return 0
	}

	return printer.RecipeLimit()
}
