package configuration

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/savorum/savorum/internal/federation"
	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/print"
	"github.com/savorum/savorum/internal/storage/filepolicy"
)

const environmentPrefix = "SAVORUM_"

// serveEnvironment names the environment variable that supplies each
// deployment flag when the operator did not pass the flag itself.
var serveEnvironment = map[string]string{ //nolint:gosec // G101: these are environment variable names, not credentials
	"suggestion-limit":         "SAVORUM_SUGGESTION_LIMIT",
	"lookup-limit":             "SAVORUM_LOOKUP_LIMIT",
	"grocery-addition-limit":   "SAVORUM_GROCERY_ADDITION_LIMIT",
	"bulk-limit":               "SAVORUM_BULK_LIMIT",
	"print-recipe-limit":       "SAVORUM_PRINT_RECIPE_LIMIT",
	"webhook-trusted-networks": "SAVORUM_WEBHOOK_TRUSTED_NETWORKS",
	"webhook-failure-limit":    "SAVORUM_WEBHOOK_FAILURE_LIMIT",
	"auth-providers-file":      "SAVORUM_AUTH_PROVIDERS_FILE",
	"token-default-days":       "SAVORUM_TOKEN_DEFAULT_DAYS",
	"token-maximum-days":       "SAVORUM_TOKEN_MAXIMUM_DAYS",
	"token-allow-nonexpiring":  "SAVORUM_TOKEN_ALLOW_NONEXPIRING",
	"auth-enabled":             "SAVORUM_AUTH_ENABLED",
	"session-idle":             "SAVORUM_SESSION_IDLE",
	"session-absolute":         "SAVORUM_SESSION_ABSOLUTE",
	"database":                 "SAVORUM_DATABASE",
	"assets":                   "SAVORUM_ASSETS",
	"listen":                   "SAVORUM_LISTEN",
	"display-name":             "SAVORUM_DISPLAY_NAME",
	"public-url":               "SAVORUM_PUBLIC_URL",
	"connect-origin":           "SAVORUM_CONNECT_ORIGIN",
	"default-locale":           "SAVORUM_DEFAULT_LOCALE",
	"default-measurement":      "SAVORUM_DEFAULT_MEASUREMENT",
	"storage-access":           "SAVORUM_STORAGE_ACCESS",
	"dns-sd":                   "SAVORUM_DNS_SD",
	"idle-timeout":             "SAVORUM_IDLE_TIMEOUT",
	"hosting-bootstrap-file":   "SAVORUM_HOSTING_BOOTSTRAP_FILE",
	"maintenance-socket":       "SAVORUM_MAINTENANCE_SOCKET",
	"durable-byte-limit":       "SAVORUM_DURABLE_BYTE_LIMIT",
	"sqlite-memory-limit":      "SAVORUM_SQLITE_MEMORY_LIMIT",
	"backup-keys-file":         "SAVORUM_BACKUP_KEYS_FILE",
	"backup-issuer":            "SAVORUM_BACKUP_ISSUER",
}

// deploymentEnvironment is every SAVORUM_* name Savorum accepts. Any other
// name is a misspelling rather than configuration this process ignores.
var deploymentEnvironment = func() map[string]struct{} {
	names := map[string]struct{}{trustedProxyEnvironment: {}}
	for _, variable := range serveEnvironment {
		names[variable] = struct{}{}
	}

	return names
}()

// trustedProxyEnvironment carries a comma-separated list rather than one
// value, so it is resolved apart from serveEnvironment.
const trustedProxyEnvironment = "SAVORUM_TRUSTED_PROXIES"

// Deployment is restart-bound process configuration, including secrets. It is
// never persisted or exposed by the public configuration endpoint.
type Deployment struct {
	DurableByteLimit     int64
	SQLiteMemoryLimit    int64
	BulkLimit            int
	SuggestionLimit      int
	LookupLimit          int
	GroceryAdditionLimit int
	WebhookNetworks      []netip.Prefix
	WebhookFailureLimit  int
	Providers            federation.Config
	TokenPolicy          identity.TokenPolicy
	AuthEnabled          bool
	SessionIdle          time.Duration
	SessionAbsolute      time.Duration
	DatabasePath         string
	AssetPath            string
	ListenAddress        string
	ConnectOrigin        string
	TrustedProxies       []netip.Prefix
	StoragePolicy        filepolicy.Policy
	Overrides            InstanceOverrides
	DNSSDEnabled         bool
	IdleTimeout          time.Duration
	MaintenanceSocket    string
	HostingBootstrapFile string
	// Backup is set when a hosting service may collect backups.
	Backup BackupCollection
	// PrintCacheDirectory is empty when the environment names no cache
	// directory.
	PrintCacheDirectory string
	PrintRecipeLimit    int
	// MemoryLimit is the Go memory limit to set, or zero to keep GOMEMLIMIT
	// or the runtime default.
	MemoryLimit int64
}

// BackupCollection names the keys and issuer a hosting service signs backup
// requests with.
type BackupCollection struct {
	KeysFile string
	Issuer   string
}

// ServeFlags holds the deployment options bound to a command's flag set.
type ServeFlags struct {
	durableByteLimit     int64
	sqliteMemoryLimit    int64
	bulkLimit            int
	suggestionLimit      int
	lookupLimit          int
	groceryAdditionLimit int
	printRecipeLimit     int
	webhookNetworks      []string
	webhookFailureLimit  int
	flags                *pflag.FlagSet
	providerFile         string
	tokenDays            int
	tokenMaximum         int
	tokenNonexpiring     bool
	authEnabled          bool
	sessionIdle          time.Duration
	sessionAbsolute      time.Duration
	databasePath         string
	assetPath            string
	listenAddress        string
	displayName          string
	publicURL            string
	connectOrigin        string
	defaultLocale        string
	defaultMeasurement   string
	storageAccess        string
	trustedProxies       []string
	dnssdEnabled         bool
	idleTimeout          time.Duration
	maintenanceSocket    string
	backupKeysFile       string
	backupIssuer         string
	hostingBootstrapFile string
}

// BindServeFlags registers the deployment options on a command's flag set with
// their built-in defaults, so help describes the same command whatever the
// process environment holds. Deployment applies the environment afterwards.
func BindServeFlags(flags *pflag.FlagSet) *ServeFlags {
	options := &ServeFlags{flags: flags}

	flags.IntVar(&options.suggestionLimit, "suggestion-limit", 50, "maximum suggestion page size")
	flags.IntVar(&options.lookupLimit, "lookup-limit", 200, "maximum explicit-ID lookup size")
	flags.IntVar(&options.groceryAdditionLimit, "grocery-addition-limit", 200, "maximum atomic grocery additions")
	flags.IntVar(&options.bulkLimit, "bulk-limit", 100, "maximum synchronous bulk items (1-500); larger work becomes a durable job")
	flags.IntVar(&options.printRecipeLimit, "print-recipe-limit", print.DefaultCollectionRecipes, "most recipes one printed document holds (1-1000000); memory still bounds each document")
	flags.StringSliceVar(&options.webhookNetworks, "webhook-trusted-networks", nil, "explicit private destination CIDRs for HTTPS webhooks (empty permits public addresses only)")
	flags.IntVar(&options.webhookFailureLimit, "webhook-failure-limit", 10, "consecutive failed deliveries before a webhook automatically pauses (1-100)")
	flags.StringVar(&options.providerFile, "auth-providers-file", "", "JSON authentication provider configuration with secret-file references")
	flags.IntVar(&options.tokenDays, "token-default-days", 90, "default personal token lifetime in days")
	flags.IntVar(&options.tokenMaximum, "token-maximum-days", 365, "maximum personal token lifetime in days")
	flags.BoolVar(&options.tokenNonexpiring, "token-allow-nonexpiring", false, "allow explicitly nonexpiring personal tokens")
	flags.BoolVar(&options.authEnabled, "auth-enabled", false, "require account authentication (bootstrap administrator with savorum user create-admin)")
	flags.DurationVar(&options.sessionIdle, "session-idle", 720*time.Hour, "browser session idle lifetime")
	flags.DurationVar(&options.sessionAbsolute, "session-absolute", 4320*time.Hour, "browser session maximum lifetime")
	flags.StringVar(&options.databasePath, "database", "", "path to the SQLite database")
	flags.StringVar(&options.assetPath, "assets", "", "content-addressed asset storage directory")
	flags.StringVar(&options.listenAddress, "listen", "127.0.0.1:8080", "HTTP listen address")
	flags.StringVar(&options.displayName, "display-name", "", "deployment override for the instance display name")
	flags.StringVar(&options.publicURL, "public-url", "", "deployment override for the canonical public URL")
	flags.StringVar(&options.connectOrigin, "connect-origin", "", "optional Savorum Connect remote origin")
	flags.StringVar(&options.defaultLocale, "default-locale", "", "deployment override for the default BCP 47 locale")
	flags.StringVar(&options.defaultMeasurement, "default-measurement", "", "deployment override: original, metric, us, or au")
	flags.StringVar(&options.storageAccess, "storage-access", string(filepolicy.OwnerOnly), "sensitive file access: owner or group")
	flags.StringArrayVar(&options.trustedProxies, "trusted-proxy", nil, "immediate reverse-proxy IP address or CIDR; repeat for more than one")
	flags.BoolVar(&options.dnssdEnabled, "dns-sd", true, "advertise instance via DNS-SD (Bonjour / mDNS)")
	flags.DurationVar(&options.idleTimeout, "idle-timeout", 0, "exit cleanly after inactivity and background work drain (0 disables; minimum 1m)")

	flags.StringVar(&options.maintenanceSocket, "maintenance-socket", "", "private local operator socket for maintenance and managed members")

	flags.StringVar(&options.hostingBootstrapFile, "hosting-bootstrap-file", "", "optional immutable hosting bootstrap request applied before the listener opens")

	flags.Int64Var(&options.durableByteLimit, "durable-byte-limit", 0, "Maximum expanded native backup bytes; zero disables durable admission")
	flags.Int64Var(&options.sqliteMemoryLimit, "sqlite-memory-limit", 0, "Process SQLite allocation budget in bytes; zero derives it from available memory")
	flags.StringVar(&options.backupKeysFile, "backup-keys-file", "", "Ed25519 JWKS trusted to sign hosting backup requests (requires hosting-bootstrap-file)")
	flags.StringVar(&options.backupIssuer, "backup-issuer", "", "issuer required in hosting backup requests")

	return options
}

// Deployment resolves the parsed flags into restart-bound configuration. It is
// the only reader of SAVORUM_* variables and rejects unknown names, so a
// misspelled variable cannot look accepted. A flag the operator passed wins
// over the environment, which wins over the built-in default. It reads the
// provider configuration file and fails if any value is invalid.
func (options *ServeFlags) Deployment(environment []string) (Deployment, error) {
	environmentValues, err := parseEnvironment(environment)
	if err != nil {
		return Deployment{}, err
	}

	if err := options.applyEnvironment(environmentValues); err != nil {
		return Deployment{}, err
	}

	trustedProxyValues := options.trustedProxies
	if !options.flags.Changed("trusted-proxy") {
		trustedProxyValues = splitCommaSeparated(environmentValues[trustedProxyEnvironment])
	}

	if strings.TrimSpace(options.databasePath) == "" {
		return Deployment{}, errors.New("database path is required; use --database or SAVORUM_DATABASE")
	}

	if err := validateListenAddress(options.listenAddress); err != nil {
		return Deployment{}, err
	}

	if err := validateConnectOrigin(options.connectOrigin, options.authEnabled); err != nil {
		return Deployment{}, err
	}

	trustedProxies, err := httpsecurity.ParseTrustedProxies(trustedProxyValues)
	if err != nil {
		return Deployment{}, fmt.Errorf("trusted proxy configuration: %w", err)
	}

	storagePolicy, err := filepolicy.Parse(options.storageAccess)
	if err != nil {
		return Deployment{}, err
	}

	resolvedAssets := strings.TrimSpace(options.assetPath)
	if resolvedAssets == "" {
		resolvedAssets = filepath.Join(filepath.Dir(strings.TrimSpace(options.databasePath)), "assets")
	}

	overrides, err := options.instanceOverrides()
	if err != nil {
		return Deployment{}, err
	}

	if err := options.validateLifetimes(); err != nil {
		return Deployment{}, err
	}

	providers, err := federation.Load(options.providerFile)
	if err != nil {
		return Deployment{}, err
	}

	if len(providers.Providers) > 0 && !options.authEnabled {
		return Deployment{}, errors.New("external providers require auth-enabled")
	}

	if err := options.validateLimits(); err != nil {
		return Deployment{}, err
	}

	webhookNetworks, err := httpsecurity.ParseTrustedProxies(options.webhookNetworks)
	if err != nil {
		return Deployment{}, fmt.Errorf("webhook trusted networks: %w", err)
	}

	return Deployment{
		DurableByteLimit: options.durableByteLimit, SQLiteMemoryLimit: options.sqliteMemoryLimit, BulkLimit: options.bulkLimit, SuggestionLimit: options.suggestionLimit, LookupLimit: options.lookupLimit, GroceryAdditionLimit: options.groceryAdditionLimit, WebhookNetworks: webhookNetworks, WebhookFailureLimit: options.webhookFailureLimit,
		Providers: providers,
		TokenPolicy: identity.TokenPolicy{
			DefaultDays: options.tokenDays, MaximumDays: options.tokenMaximum, AllowNonexpiring: options.tokenNonexpiring,
		},
		AuthEnabled: options.authEnabled, SessionIdle: options.sessionIdle, SessionAbsolute: options.sessionAbsolute,
		DatabasePath:         filepath.Clean(strings.TrimSpace(options.databasePath)),
		AssetPath:            filepath.Clean(resolvedAssets),
		ListenAddress:        strings.TrimSpace(options.listenAddress),
		ConnectOrigin:        options.connectOrigin,
		TrustedProxies:       trustedProxies,
		StoragePolicy:        storagePolicy,
		Overrides:            overrides,
		DNSSDEnabled:         options.dnssdEnabled,
		IdleTimeout:          options.idleTimeout,
		MaintenanceSocket:    options.maintenanceSocket,
		HostingBootstrapFile: options.hostingBootstrapFile,
		Backup:               BackupCollection{KeysFile: options.backupKeysFile, Issuer: options.backupIssuer},
		PrintCacheDirectory:  printCacheDirectory(environment),
		PrintRecipeLimit:     options.printRecipeLimit,
		MemoryLimit:          memoryLimit(environment, hostMemory),
	}, nil
}

func validateConnectOrigin(connectOrigin string, authEnabled bool) error {
	if connectOrigin == "" {
		return nil
	}

	if !authEnabled {
		return errors.New("connect-origin requires auth-enabled")
	}

	origin, err := url.Parse(connectOrigin)
	if err != nil || origin.Scheme != "https" || origin.Hostname() == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" || origin.Port() != "" {
		return errors.New("connect-origin must be an HTTPS origin without a port or path")
	}

	return nil
}

// validateLifetimes checks the session, idle, maintenance, and bootstrap
// settings that depend on one another.
func (options *ServeFlags) validateLifetimes() error {
	if options.sessionIdle < time.Minute || options.sessionIdle > 180*24*time.Hour ||
		options.sessionAbsolute < options.sessionIdle || options.sessionAbsolute > 365*24*time.Hour {
		return errors.New("invalid session lifetimes")
	}

	if options.idleTimeout < 0 || options.idleTimeout > 0 && options.idleTimeout < time.Minute {
		return errors.New("idle-timeout must be 0 or at least 1m")
	}

	if options.maintenanceSocket != "" && !filepath.IsAbs(options.maintenanceSocket) {
		return errors.New("maintenance-socket requires an absolute path")
	}

	if options.durableByteLimit < 0 || options.durableByteLimit == 1<<63-1 || options.sqliteMemoryLimit < 0 {
		return errors.New("execution budgets must be nonnegative and durable-byte-limit must leave room for overflow checks")
	}

	if (options.backupKeysFile != "" || options.backupIssuer != "") &&
		(!filepath.IsAbs(options.backupKeysFile) || options.backupIssuer == "" || options.hostingBootstrapFile == "") {
		return errors.New("backup-keys-file requires an absolute path, backup-issuer, and hosting-bootstrap-file")
	}

	if options.hostingBootstrapFile != "" && !options.authEnabled {
		return errors.New("hosting-bootstrap-file requires auth-enabled")
	}

	return nil
}

// validateLimits checks the token, bulk, webhook, and print bounds.
func (options *ServeFlags) validateLimits() error {
	if options.tokenDays < 1 || options.tokenMaximum < options.tokenDays || options.tokenMaximum > 3650 {
		return errors.New("token lifetimes require 1 <= default <= maximum <= 3650 days")
	}

	if options.suggestionLimit < 10 || options.lookupLimit < 1 || options.groceryAdditionLimit < 1 {
		return errors.New("selection and grocery limits must be positive; suggestion limit must be at least 10")
	}

	if options.bulkLimit < 1 || options.bulkLimit > 500 || options.webhookFailureLimit < 1 || options.webhookFailureLimit > 100 {
		return errors.New("bulk-limit requires 1-500 and webhook-failure-limit requires 1-100")
	}

	if options.printRecipeLimit < 1 || options.printRecipeLimit > maximumPrintRecipeLimit {
		return errors.New("print-recipe-limit requires 1-1000000")
	}

	return nil
}

// maximumPrintRecipeLimit keeps per-document budgets, which grow with the
// recipe limit, within integer range.
const maximumPrintRecipeLimit = 1_000_000

// printCacheDirectory places the print compiler cache in the user cache
// directory, resolved as os.UserCacheDir resolves it.
func printCacheDirectory(environment []string) string {
	values := map[string]string{}

	for _, entry := range environment {
		name, value, _ := strings.Cut(entry, "=")
		values[name] = value
	}

	root := values["XDG_CACHE_HOME"]
	if runtime.GOOS == "darwin" {
		root = ""
		if values["HOME"] != "" {
			root = filepath.Join(values["HOME"], "Library", "Caches")
		}
	} else if root == "" && values["HOME"] != "" {
		root = filepath.Join(values["HOME"], ".cache")
	}

	if !filepath.IsAbs(root) {
		return ""
	}

	return filepath.Join(root, "savorum", "typst")
}

// applyEnvironment sets each flag the operator left out from its SAVORUM_*
// counterpart. Setting through the flag set reuses one parser per type and
// marks the value as supplied, which is what override detection reads.
func (options *ServeFlags) applyEnvironment(values map[string]string) error {
	for name, variable := range serveEnvironment {
		value, found := values[variable]
		if !found || options.flags.Changed(name) {
			continue
		}

		if err := options.flags.Set(name, value); err != nil {
			return fmt.Errorf("invalid %s: %w", variable, err)
		}
	}

	return nil
}

// instanceOverrides collects the deployment overrides for persisted instance
// settings. Only a value the operator actually supplied becomes an override,
// so an untouched flag leaves the administrator's saved setting alone.
func (options *ServeFlags) instanceOverrides() (InstanceOverrides, error) {
	overrides := InstanceOverrides{}
	setStringOverride(&overrides.DisplayName, options.displayName, options.flags.Changed("display-name"))
	setStringOverride(&overrides.PublicURL, options.publicURL, options.flags.Changed("public-url"))
	setStringOverride(&overrides.DefaultLocale, options.defaultLocale, options.flags.Changed("default-locale"))

	if options.flags.Changed("default-measurement") {
		value := MeasurementPresentation(strings.TrimSpace(options.defaultMeasurement))
		overrides.DefaultMeasurement = &value
	}

	if _, err := (&Service{overrides: overrides}).applyOverrides(DefaultInstanceSettings()); err != nil {
		return InstanceOverrides{}, fmt.Errorf("deployment setting override: %w", err)
	}

	return overrides, nil
}

// ParseStoragePolicy resolves the one deployment value shared by serve and
// offline maintenance commands without letting those commands interpret
// SAVORUM_* variables independently.
func ParseStoragePolicy(environment []string) (filepolicy.Policy, error) {
	values, err := parseEnvironment(environment)
	if err != nil {
		return filepolicy.Policy{}, err
	}

	return filepolicy.Parse(valueOr(values, "SAVORUM_STORAGE_ACCESS", string(filepolicy.OwnerOnly)))
}

func parseEnvironment(entries []string) (map[string]string, error) {
	values := make(map[string]string)

	for _, entry := range entries {
		name, value, found := strings.Cut(entry, "=")
		if !found || !strings.HasPrefix(name, environmentPrefix) {
			continue
		}

		if _, known := deploymentEnvironment[name]; !known {
			return nil, fmt.Errorf("unknown Savorum environment variable %s", name)
		}

		values[name] = value
	}

	return values, nil
}

func validateListenAddress(value string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("listen address %q must use host:port form", value)
	}

	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsedPort == 0 {
		return fmt.Errorf("listen address %q has an invalid port", value)
	}

	if host != "" && host != "localhost" {
		if _, err := netip.ParseAddr(host); err != nil {
			return fmt.Errorf("listen address %q has an invalid host; use an IP address, localhost, or an empty host", value)
		}
	}

	return nil
}

func valueOr(values map[string]string, name, fallback string) string {
	if value, found := values[name]; found {
		return value
	}

	return fallback
}

func setStringOverride(target **string, value string, supplied bool) {
	if !supplied {
		return
	}

	copied := value
	*target = &copied
}

func splitCommaSeparated(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")

	result := make([]string, 0, len(parts))
	for _, part := range parts {
		result = append(result, strings.TrimSpace(part))
	}

	return result
}
