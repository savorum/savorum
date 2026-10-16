// Package app assembles Savorum from its parts. It is the one place that knows
// which storage, renderer, and services a running instance is built from, so
// the server binary, the scale check, and the tests can all stand up the same
// application without repeating how it fits together.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/storagebudget"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/account"
	api "github.com/savorum/savorum/internal/api/v1"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/automation"
	"github.com/savorum/savorum/internal/buildinfo"
	"github.com/savorum/savorum/internal/connect"
	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/event"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/federation"
	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/idempotency"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/importjob"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/meal"
	"github.com/savorum/savorum/internal/mealcalendar"
	"github.com/savorum/savorum/internal/pantry"
	"github.com/savorum/savorum/internal/preferences"
	"github.com/savorum/savorum/internal/print"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/render/typst"
	"github.com/savorum/savorum/internal/shopping"
	"github.com/savorum/savorum/internal/units"
	"github.com/savorum/savorum/internal/webhook"
	"github.com/savorum/savorum/internal/webui"
)

// Store is the complete persistence contract required by the application and
// every API route it registers. Implementations must provide organization
// persistence as well as recipe and asset persistence so route availability
// cannot depend on a runtime repository type assertion.
type Store interface {
	webhook.Repository
	library.Persistence
	library.SourceAttachmentStore
	assets.Repository
	account.Repository
	federation.Repository
	importjob.Repository
	preferences.Repository
	automation.Repository
	idempotency.Repository
	event.Reader
	pantry.Repository
	meal.Persistence
	meal.SavedMealPersistence
	shopping.Persistence
	mealcalendar.Persistence
	api.HealthChecker
	StorageFootprint(ctx context.Context) (diskspace.Footprint, error)
	InstanceID(ctx context.Context) (string, error)
	HostingTenant(ctx context.Context) (hosting.Tenant, error)
}

// Application groups the services required to run Savorum.
type Application struct {
	Webhooks   *webhook.Service
	Automation *automation.Service
	Handler    http.Handler
	OpenAPI    huma.API
	// Renderer is held so a caller that wants printing to be ready before the
	// first request can prepare it, and so it can be released on shutdown.
	Renderer *typst.Renderer
	// ImportJobs is held because it owns work that outlives requests. A caller
	// must recover interrupted jobs before serving and close it on shutdown,
	// so an import is never left recorded as running by a process that stopped.
	ImportJobs *importjob.Service
	Printing   *print.Service
}

// Options supplies optional application capabilities at the composition root.
type Options struct {
	EventStreamShutdown  <-chan struct{}
	WebhookDirectory     string
	DurableBudget        *storagebudget.Coordinator
	StoragePath          string
	WebhookPolicy        webhook.DestinationPolicy
	WebhookFailureLimit  int
	AutomationDirectory  string
	BulkLimit            int
	SuggestionLimit      int
	LookupLimit          int
	GroceryAdditionLimit int
	Providers            federation.Config
	TokenPolicy          identity.TokenPolicy
	AuthEnabled          bool
	ConnectOrigin        string
	Connect              *connect.Service
	ResolveConnectOrigin func() string
	SessionIdle          time.Duration
	SessionAbsolute      time.Duration
	Assets               api.AssetService
	Configuration        api.ConfigurationService
	// PrintCacheDirectory keeps the print compiler's machine code between
	// processes. Empty compiles it in memory in every process.
	PrintCacheDirectory string
	// PrintRecipeLimit is the most recipes one printed document holds; zero
	// takes the default.
	PrintRecipeLimit int
}

// New constructs an application from its store, unit catalog, and options.
func New(store Store, catalog recipe.UnitCatalog, options Options) (*Application, error) {
	if store == nil {
		return nil, errors.New("application store is required")
	}

	service, err := library.NewService(store, catalog)
	if err != nil {
		return nil, err
	}
	// The composition root chooses the document engine. Nothing below this
	// point depends on which one it is; the rest of the application works
	// through print.DocumentRenderer.
	renderer := typst.NewCached(options.PrintCacheDirectory)

	var opener print.AssetOpener
	if options.Assets != nil {
		opener = options.Assets
	}

	printer, err := print.NewService(renderer, printCatalog(catalog), opener, service, service,
		print.WithWorkloadLimits(print.WorkloadLimitsFor(options.PrintRecipeLimit)))
	if err != nil {
		return nil, err
	}

	mealService, err := meal.NewService(store, store)
	if err != nil {
		return nil, err
	}

	calendar := mealcalendar.NewService(store, mealService, store.InstanceID, publicURLFor(options.Configuration))

	apiOptions := api.Options{
		EventStreamShutdown: options.EventStreamShutdown,
		Pantry:              store,
		Events:              store,
		Idempotency:         idempotency.New(store),
		BulkLimit:           options.BulkLimit,
		SuggestionLimit:     options.SuggestionLimit, LookupLimit: options.LookupLimit, GroceryAdditionLimit: options.GroceryAdditionLimit,
		AuthEnabled:       options.AuthEnabled,
		Assets:            options.Assets,
		SourceAttachments: store,
		Configuration:     options.Configuration,
		Preferences:       preferences.NewService(store, options.Configuration),
		Meals:             mealService,
		Shopping:          shopping.NewService(store).WithSources(service, mealService),
		MealCalendar:      calendar,
	}

	if options.AuthEnabled {
		apiOptions.AuthMethods = append(apiOptions.AuthMethods, api.CapabilityAuthMethod{ID: "password", Name: "Password", Kind: "password"})

		for _, provider := range options.Providers.Providers {
			if !provider.Disabled {
				apiOptions.AuthMethods = append(apiOptions.AuthMethods, api.CapabilityAuthMethod{ID: provider.ID, Name: provider.DisplayName, Kind: provider.Kind})
			}
		}
	}

	var (
		jobs      *importjob.Service
		exchanger *exchange.Service
	)

	// The job service shares the exchange service that holds staged previews.
	if options.Assets != nil {
		exchanger = exchange.NewService(service, options.Assets, exchange.WithSourceAttachments(store), exchange.WithPreviewScopeResolver(func(ctx context.Context) string {
			p, ok := identity.FromContext(ctx)
			if !ok {
				return "offline"
			}

			return access.LibraryID(ctx) + "\x00" + p.UserID
		}))
		apiOptions.Exchange = exchanger

		jobs, err = importjob.NewService(store, exchanger)
		if err != nil {
			return nil, err
		}

		apiOptions.ImportJobs = jobs
	}

	automationService, webhooks, err := integrationServices(store, service, exchanger, jobs, apiOptions.Idempotency, options)
	if err != nil {
		return nil, err
	}

	ready := false
	defer func() {
		if !ready {
			closeIntegrationServices(automationService, webhooks)
		}
	}()

	apiOptions.Automation = automationService
	apiOptions.Webhooks = webhooks

	mux, openAPI, err := api.New(service, store, printer, apiOptions)
	if err != nil {
		return nil, err
	}

	registerBootstrap(mux, store, options)
	registerStorageStatus(openAPI, store, options)
	api.RegisterConnect(openAPI, options.Connect, options.AuthEnabled)

	// Calendar clients cannot sign in; the feed secret is the credential.
	mux.HandleFunc("GET /calendar/{file}", calendarFeedHandler(calendar))

	mux.Handle("/", webui.Handler())

	publicURL := ""

	if options.Configuration != nil {
		settings, e := options.Configuration.Current(context.Background())
		if e != nil {
			return nil, e
		}

		publicURL = settings.PublicURL
	}

	hosted, err := checkHosting(context.Background(), store, options, publicURL)
	if err != nil {
		return nil, err
	}

	cookies := identity.BrowserCookies{HostPrefix: hosted}
	openAPI.OpenAPI().Components.SecuritySchemes["browserSession"].Name = cookies.Name(identity.SessionCookie)

	var handler http.Handler = account.New(store, account.Options{Cookies: cookies, TokenPolicy: options.TokenPolicy, Enabled: options.AuthEnabled, IdleLifetime: options.SessionIdle, AbsoluteLifetime: options.SessionAbsolute, ConnectOrigin: options.ConnectOrigin, ResolveConnectOrigin: options.ResolveConnectOrigin, PublicURL: func(ctx context.Context) (string, error) {
		if options.Configuration == nil {
			return "", nil
		}

		settings, err := options.Configuration.Current(ctx)

		return settings.PublicURL, err
	}}, mux)

	if options.AuthEnabled {
		handler, err = federation.New(store, options.Providers, federation.Options{Cookies: cookies, PublicURL: publicURL, AllowConnect: options.Connect != nil, IdleLifetime: options.SessionIdle, AbsoluteLifetime: options.SessionAbsolute}, handler)
		if err != nil {
			return nil, err
		}
	}

	ready = true

	return &Application{Automation: automationService, Webhooks: webhooks, Handler: handler, OpenAPI: openAPI, Renderer: renderer, ImportJobs: jobs, Printing: printer}, nil
}

// printCatalog resolves the unit catalog printing converts through. The
// application is composed with the same catalog the API validates against, so
// a printed amount and a stored one can never disagree.
func printCatalog(catalog recipe.UnitCatalog) units.Catalog {
	if resolved, ok := catalog.(units.Catalog); ok {
		return resolved
	}

	return units.DefaultCatalog()
}

func publicURLFor(settings api.ConfigurationService) func(context.Context) string {
	return func(ctx context.Context) string {
		if origin := httpsecurity.ConnectOrigin(ctx); origin != "" {
			return origin
		}

		if settings == nil {
			return ""
		}

		current, err := settings.Current(ctx)
		if err != nil {
			return ""
		}

		return current.PublicURL
	}
}

func calendarFeedHandler(calendar *mealcalendar.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, max-age=300")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")

		token, ok := strings.CutSuffix(r.PathValue("file"), ".ics")
		if !ok {
			http.NotFound(w, r)
			return
		}

		document, err := calendar.Read(r.Context(), token)
		if errors.Is(err, mealcalendar.ErrNotFound) {
			http.NotFound(w, r)
			return
		}

		if err != nil {
			http.Error(w, "calendar unavailable", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(document)))

		if r.Method == http.MethodHead {
			return
		}

		_, _ = w.Write(document) //nolint:gosec // text/calendar with nosniff, not HTML
	}
}

func registerBootstrap(mux interface {
	HandleFunc(string, http.HandlerFunc)
}, store Store, options Options,
) {
	mux.HandleFunc("GET /.well-known/savorum", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		instanceID, err := store.InstanceID(ctx)
		if err != nil {
			http.Error(w, "instance identity unavailable", http.StatusInternalServerError)
			return
		}

		name := "Savorum"

		if options.Configuration != nil {
			if settings, err := options.Configuration.Current(ctx); err == nil && settings.DisplayName != "" {
				name = settings.DisplayName
			}
		}

		payload := map[string]any{
			"instanceId":    instanceID,
			"apiVersion":    api.Version,
			"serverVersion": buildinfo.CurrentVersion(),
			"name":          name,
			"authEnabled":   options.AuthEnabled,
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")

		if r.Method == http.MethodHead {
			return
		}

		_ = json.NewEncoder(w).Encode(payload)
	})
}

func integrationServices(store Store, service *library.Service, exchanger *exchange.Service, jobs *importjob.Service, receipts *idempotency.Coordinator, options Options) (*automation.Service, *webhook.Service, error) {
	var (
		err               error
		automationService *automation.Service
	)

	if options.AutomationDirectory != "" {
		limits := export.DefaultLimits()
		limits.CollectionRecipes = 10000
		limits.OutputBytes = 512 << 20
		limits.WorkDuration = 10 * time.Minute

		exporter, err := export.NewService(service, options.Assets, export.WithLimits(limits))
		if err != nil {
			return nil, nil, err
		}

		var (
			exchangeAdapter automation.Exchange
			importService   automation.ImportJobs
		)

		if exchanger != nil {
			exchangeAdapter = exchanger
		}

		if jobs != nil {
			importService = jobs
		}

		automationService, err = automation.New(store, service, exchangeAdapter, importService, exporter, receipts, options.AutomationDirectory)
		if err != nil {
			return nil, nil, err
		}
	}

	var webhooks *webhook.Service
	if options.WebhookDirectory != "" {
		webhooks, err = webhook.New(store, options.WebhookDirectory, options.WebhookPolicy, options.WebhookFailureLimit, options.DurableBudget)
		if err != nil {
			closeIntegrationServices(automationService, nil)
			return nil, nil, err
		}
	}

	return automationService, webhooks, nil
}

func closeIntegrationServices(jobs *automation.Service, hooks *webhook.Service) {
	if jobs != nil {
		_ = jobs.Close()
	}

	if hooks != nil {
		_ = hooks.Close()
	}
}
