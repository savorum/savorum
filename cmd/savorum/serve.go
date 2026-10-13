package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/memoryspace"

	"github.com/spf13/cobra"

	"github.com/savorum/savorum/internal/administration"
	"github.com/savorum/savorum/internal/app"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/automation"
	"github.com/savorum/savorum/internal/backupstream"
	"github.com/savorum/savorum/internal/configuration"
	"github.com/savorum/savorum/internal/dnssd"
	"github.com/savorum/savorum/internal/federation"
	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/standby"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/storagebudget"
	"github.com/savorum/savorum/internal/units"
	"github.com/savorum/savorum/internal/webhook"
)

func newServeCommand(savorum *application) *cobra.Command {
	var options *configuration.ServeFlags

	command := &cobra.Command{
		Use:   "serve",
		Short: "Serve the API and web interface",
		Long: "Serve runs the API and web interface against one database and asset\n" +
			"directory. Its options are restart-bound deployment configuration: a flag\n" +
			"wins over the matching SAVORUM_* environment variable, which wins over the\n" +
			"built-in default.",
		Example: "  savorum serve --database /var/lib/savorum/savorum.db\n" +
			"  savorum serve --database /var/lib/savorum/savorum.db --listen 0.0.0.0:8080 \\\n" +
			"    --public-url https://recipes.example --trusted-proxy 192.0.2.10",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			deployment, err := options.Deployment(savorum.environ)
			if err != nil {
				// A rejected flag and a rejected SAVORUM_* value are the same
				// mistake; neither is the server failing to start.
				return usageError{err: err}
			}

			return serve(cmd.Context(), deployment)
		},
	}

	options = configuration.BindServeFlags(command.Flags())
	markPathFlags(command, "database", "assets", "auth-providers-file")

	return command
}

func serve(rootContext context.Context, deployment configuration.Deployment) error {
	rootContext, stop := context.WithCancel(rootContext)
	defer stop()

	// Print budgets are derived from this limit when the application is built.
	if deployment.MemoryLimit > 0 {
		debug.SetMemoryLimit(deployment.MemoryLimit)
		slog.Info("memory budget set from host memory; set GOMEMLIMIT to choose another", "bytes", deployment.MemoryLimit)
	}

	if err := configureSQLiteMemory(deployment.SQLiteMemoryLimit); err != nil {
		return err
	}

	instanceLock, err := administration.AcquireInstanceLock(deployment.DatabasePath, deployment.AssetPath, deployment.StoragePolicy)
	if err != nil {
		return err
	}
	defer instanceLock.Close()

	var durableBudget *storagebudget.Coordinator

	storeOptions := []sqlite.OpenOption{sqlite.WithFilePolicy(deployment.StoragePolicy), sqlite.WithSQLiteHeadroom()}
	if deployment.DurableByteLimit > 0 {
		durableBudget = storagebudget.New(deployment.AssetPath, deployment.DurableByteLimit)
		durableBudget.AddRetainedRoot(deployment.DatabasePath + ".webhook-secrets")
		storeOptions = append(storeOptions, sqlite.WithDurableBudget(durableBudget))
	}

	store, err := sqlite.Open(rootContext, deployment.DatabasePath, storeOptions...)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := bootstrapBeforeServing(rootContext, store, deployment); err != nil {
		return err
	}

	blobStore, err := files.NewAssetStore(deployment.AssetPath, files.DefaultMaximumAssetSize, files.WithFilePolicy(deployment.StoragePolicy), files.WithDurableBudget(durableBudget))
	if err != nil {
		return err
	}

	defer blobStore.Close()

	assetService, err := assets.NewService(store, blobStore, assets.WithDurableBudget(durableBudget))
	if err != nil {
		return err
	}

	configurationService, err := configuration.NewService(store, deployment.Overrides)
	if err != nil {
		return fmt.Errorf("construct configuration service: %w", err)
	}
	// Resolve once before starting any listener so corrupt persisted settings or
	// invalid deployment overrides produce a startup error rather than a failing
	// API route after the process appears healthy.
	if _, err := configurationService.Current(rootContext); err != nil {
		return fmt.Errorf("resolve instance configuration: %w", err)
	}

	application, err := app.New(store, units.DefaultCatalog(), app.Options{
		AutomationDirectory: automation.ArtifactDirectory(deployment.DatabasePath),
		BulkLimit:           deployment.BulkLimit,
		SuggestionLimit:     deployment.SuggestionLimit, LookupLimit: deployment.LookupLimit, GroceryAdditionLimit: deployment.GroceryAdditionLimit,
		WebhookDirectory:    deployment.DatabasePath + ".webhook-secrets",
		DurableBudget:       durableBudget,
		StoragePath:         deployment.DatabasePath,
		WebhookPolicy:       webhook.DestinationPolicy{TrustedNetworks: deployment.WebhookNetworks},
		WebhookFailureLimit: deployment.WebhookFailureLimit,
		Assets:              assetService, Configuration: configurationService,
		Providers: deployment.Providers, TokenPolicy: deployment.TokenPolicy, AuthEnabled: deployment.AuthEnabled, SessionIdle: deployment.SessionIdle, SessionAbsolute: deployment.SessionAbsolute,
		ConnectOrigin:       deployment.ConnectOrigin,
		PrintCacheDirectory: deployment.PrintCacheDirectory,
		PrintRecipeLimit:    deployment.PrintRecipeLimit,
	})
	if err != nil {
		return fmt.Errorf("construct application: %w", err)
	}
	defer application.Renderer.Close()
	// Recover interrupted imports before the listener opens.
	if application.ImportJobs != nil {
		if _, err := application.ImportJobs.Recover(rootContext); err != nil {
			return fmt.Errorf("recover interrupted import jobs: %w", err)
		}
		defer application.ImportJobs.Close()
	}

	workers := startIntegrationWorkers(rootContext, application)
	defer workers.close()

	backups, err := backupCollection(rootContext, deployment, store)
	if err != nil {
		return err
	}

	idle, closeStandby, err := standbyMonitor(rootContext, deployment, application, store, backups, stop)
	if err != nil {
		return err
	}
	defer func() { _ = closeStandby() }()

	handler := idle(withBackups(application.Handler, backups))

	handler = httpsecurity.NewHandler(handler, httpsecurity.Options{
		SAMLACSPaths: deployment.Providers.SAMLACSPaths(), TrustedProxies: deployment.TrustedProxies,
		ListenAddress:  deployment.ListenAddress,
		LocalHostnames: httpsecurity.LocalListenerHostnames(rootContext, deployment.ListenAddress),
		ApplicationOrigin: func(ctx context.Context) (string, error) {
			settings, err := configurationService.Current(ctx)
			return settings.PublicURL, err
		},
		ConnectOrigin: deployment.ConnectOrigin,
	})

	server := httpsecurity.NewServer(deployment.ListenAddress, handler)
	serverErrors := make(chan error, 1)

	advertiser := startDNSSD(rootContext, deployment, store, configurationService)
	if advertiser != nil {
		defer advertiser.Shutdown()
	}

	go func() {
		slog.Info("Savorum is listening", "address", deployment.ListenAddress)

		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-rootContext.Done():
	case err := <-workers.done:
		workers.count--

		if rootContext.Err() == nil {
			_ = server.Close()

			if err == nil {
				err = errors.New("background worker stopped unexpectedly")
			}

			return fmt.Errorf("integration worker: %w", err)
		}
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("serve HTTP: %w", err)
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownContext); err != nil {
		_ = server.Close()
		return fmt.Errorf("shut down HTTP server: %w", err)
	}

	workers.stop()

	if application.ImportJobs != nil {
		application.ImportJobs.Close()
	}

	if err := store.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("drain SQLite: %w", err)
	}

	if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}

	return nil
}

func configureSQLiteMemory(limit int64) error {
	if limit > 0 {
		return sqlite.ConfigureMemoryBudget(limit)
	}

	space, err := memoryspace.Inspect()
	if err != nil {
		return fmt.Errorf("inspect SQLite memory capacity: %w", err)
	}

	bytes := space.TotalBytes / 4

	goLimit := debug.SetMemoryLimit(-1)
	if goLimit > 0 {
		bytes = min(bytes, uint64(goLimit)/4)
	}

	if bytes == 0 || bytes > ^uint64(0)>>1 {
		return errors.New("SQLite memory capacity unavailable")
	}

	return sqlite.ConfigureMemoryBudget(int64(bytes))
}

func startDNSSD(ctx context.Context, deployment configuration.Deployment, store *sqlite.Store, configurationService *configuration.Service) *dnssd.Advertiser {
	if !deployment.DNSSDEnabled {
		return nil
	}

	host, portStr, err := net.SplitHostPort(deployment.ListenAddress)
	if err != nil {
		return nil
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return nil
	}

	instanceID, err := store.InstanceID(ctx)
	if err != nil {
		slog.Warn("DNS-SD advertisement unavailable: failed to resolve instance ID", "error", err)
		return nil
	}

	settings, err := configurationService.Current(ctx)
	if err != nil {
		slog.Warn("DNS-SD advertisement unavailable: failed to resolve configuration", "error", err)
		return nil
	}

	advertiser, err := dnssd.Start(dnssd.Options{
		Name:       settings.DisplayName,
		Address:    host,
		Port:       port,
		InstanceID: instanceID,
	})
	if err != nil {
		slog.Warn("DNS-SD advertisement unavailable", "error", err)
		return nil
	}

	slog.Info("advertising DNS-SD service", "type", dnssd.ServiceType, "name", settings.DisplayName, "port", port)

	return advertiser
}

type integrationWorkers struct {
	cancel      context.CancelFunc
	done        chan error
	count       int
	application *app.Application
}

func startIntegrationWorkers(ctx context.Context, application *app.Application) *integrationWorkers {
	ctx, cancel := context.WithCancel(ctx)

	w := &integrationWorkers{cancel: cancel, done: make(chan error, 2), application: application}
	if application.Automation != nil {
		w.count++
		go func() { w.done <- runIntegrationWorker(ctx, application.Automation.Run) }()
	}

	if application.Webhooks != nil {
		w.count++
		go func() { w.done <- runIntegrationWorker(ctx, application.Webhooks.Run) }()
	}

	return w
}

func runIntegrationWorker(ctx context.Context, run func(context.Context) error) error {
	for {
		err := run(ctx)
		if ctx.Err() != nil || !sqlite.IsBusy(err) {
			return err
		}

		// Each worker resumes through its durable recovery contract.
		slog.Warn("integration worker waiting for SQLite writer")

		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (w *integrationWorkers) stop() {
	w.cancel()

	for w.count > 0 {
		<-w.done
		w.count--
	}
}

func (w *integrationWorkers) close() {
	w.stop()

	if w.application.Automation != nil {
		_ = w.application.Automation.Close()
	}

	if w.application.Webhooks != nil {
		_ = w.application.Webhooks.Close()
	}
}

// standbyMonitor returns a wrapper that counts requests as activity, so idle
// standby never stops the process while one is in progress.
func standbyMonitor(ctx context.Context, deployment configuration.Deployment, application *app.Application, store *sqlite.Store, backups *backupstream.Handler, stop context.CancelFunc) (func(http.Handler) http.Handler, func() error, error) {
	closeStandby := func() error { return nil }
	if deployment.IdleTimeout == 0 && deployment.MaintenanceSocket == "" {
		return func(handler http.Handler) http.Handler { return handler }, closeStandby, nil
	}

	idle := standby.New()

	if deployment.MaintenanceSocket != "" {
		var err error

		closeStandby, err = idle.Maintenance(ctx, deployment.MaintenanceSocket, deployment.DatabasePath+".maintenance-leases", administration.OperatorHandler(administration.OperatorCallbacks{Member: store.BootstrapHostingMember, CheckOwner: store.CheckHostedOwner, RevokeMember: store.RevokeHostedMember, EraseUser: store.EraseHostedUser}))
		if err != nil {
			return nil, nil, err
		}
	}

	if deployment.IdleTimeout > 0 {
		go idle.Run(ctx, deployment.IdleTimeout, func(ctx context.Context) (bool, error) {
			stats := application.Printing.WorkloadStats()
			if stats.Active > 0 || stats.Queued > 0 || backups != nil && backups.Busy() {
				return true, nil
			}

			return store.HasBackgroundWork(ctx)
		}, stop)
	}

	return idle.Handler, closeStandby, nil
}

// backupCollection lets the hosting service collect backups when the
// deployment trusts a backup signing key.
func backupCollection(ctx context.Context, deployment configuration.Deployment, store *sqlite.Store) (*backupstream.Handler, error) {
	if deployment.Backup.KeysFile == "" {
		return nil, nil
	}

	keys, err := federation.LoadManagedKeys(deployment.Backup.KeysFile)
	if err != nil {
		return nil, fmt.Errorf("load backup keys: %w", err)
	}

	tenant, err := store.HostingTenant(ctx)
	if err != nil {
		return nil, fmt.Errorf("backups need a hosting bootstrap: %w", err)
	}

	return &backupstream.Handler{
		Keys: keys, Issuer: deployment.Backup.Issuer, Audience: tenant.TenantID,
		Store: store, AssetRoot: deployment.AssetPath, WebhookDirectory: deployment.DatabasePath + ".webhook-secrets",
		Workspace: filepath.Dir(deployment.DatabasePath), Policy: deployment.StoragePolicy, Context: ctx, Logger: slog.Default(),
	}, nil
}

func withBackups(next http.Handler, backups *backupstream.Handler) http.Handler {
	if backups == nil {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == backupstream.Path || strings.HasPrefix(r.URL.Path, backupstream.Path+"/") {
			backups.ServeHTTP(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func bootstrapBeforeServing(ctx context.Context, store *sqlite.Store, deployment configuration.Deployment) error {
	if deployment.HostingBootstrapFile == "" {
		return nil
	}

	if !deployment.AuthEnabled {
		return errors.New("hosting bootstrap requires authentication")
	}

	var request hosting.Bootstrap

	file, err := os.Open(deployment.HostingBootstrapFile)
	if err != nil {
		return fmt.Errorf("open hosting bootstrap: %w", err)
	}
	defer file.Close()

	d := json.NewDecoder(io.LimitReader(file, 65536))
	d.DisallowUnknownFields()

	if err := d.Decode(&request); err != nil {
		return fmt.Errorf("decode hosting bootstrap: %w", err)
	}

	var extra any
	if !errors.Is(d.Decode(&extra), io.EOF) {
		return errors.New("hosting bootstrap must be one JSON object")
	}

	if err := request.Validate(); err != nil {
		return err
	}

	if _, err := store.BootstrapHosting(ctx, request); err != nil {
		return err
	}

	return nil
}
