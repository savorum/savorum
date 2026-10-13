package configuration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"

	"github.com/savorum/savorum/internal/storage/filepolicy"
)

type memoryRepository struct {
	settings InstanceSettings
	err      error
}

func (r *memoryRepository) LoadInstanceSettings(context.Context) (InstanceSettings, error) {
	if r.err != nil {
		return InstanceSettings{}, r.err
	}

	return r.settings, nil
}

func (r *memoryRepository) ReplaceInstanceSettings(_ context.Context, expected uint64, settings InstanceSettings) (InstanceSettings, error) {
	if expected != r.settings.Revision {
		return InstanceSettings{}, ErrConflict
	}

	settings.Revision = expected + 1
	r.settings = settings
	r.err = nil

	return settings, nil
}

func TestServicePrecedenceDoesNotPersistDeploymentOverrides(t *testing.T) {
	t.Parallel()

	persisted := DefaultInstanceSettings()
	persisted.DisplayName = "Stored kitchen"
	persisted.PublicURL = "https://stored.example"
	persisted.DefaultMeasurement = MeasurementMetric
	persisted.Revision = 3
	repository := &memoryRepository{settings: persisted}
	overriddenName := "Managed kitchen"
	overriddenURL := "https://recipes.example/"

	service, err := NewService(repository, InstanceOverrides{
		DisplayName: &overriddenName,
		PublicURL:   &overriddenURL,
	})
	if err != nil {
		t.Fatal(err)
	}

	effective, err := service.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if effective.DisplayName != "Managed kitchen" || effective.PublicURL != "https://recipes.example" || effective.DefaultMeasurement != MeasurementMetric {
		t.Fatalf("effective settings = %#v", effective)
	}

	if !reflect.DeepEqual(repository.settings, persisted) {
		t.Fatalf("deployment override mutated persistence\nwant: %#v\n got: %#v", persisted, repository.settings)
	}
}

func TestServiceDefaultsValidatesAndNormalizesReplacements(t *testing.T) {
	t.Parallel()

	repository := &memoryRepository{err: ErrNotFound}

	service, err := NewService(repository, InstanceOverrides{})
	if err != nil {
		t.Fatal(err)
	}

	defaults, err := service.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if defaults.DisplayName != "Savorum" || defaults.DefaultLocale != "en-US" {
		t.Fatalf("defaults = %#v", defaults)
	}

	next := defaults
	next.DisplayName = "  Family recipes  "
	next.PublicURL = "https://recipes.example/"

	written, err := service.Replace(context.Background(), 0, next)
	if err != nil {
		t.Fatal(err)
	}

	if written.DisplayName != "Family recipes" || written.PublicURL != "https://recipes.example" || written.Revision != 1 {
		t.Fatalf("written settings = %#v", written)
	}

	invalid := written

	invalid.DefaultLocale = "%%%"
	if _, err := service.Replace(context.Background(), 1, invalid); err == nil || !strings.Contains(err.Error(), "default locale") {
		t.Fatalf("invalid locale error = %v", err)
	}
}

// parseServeDeployment drives the same two steps `savorum serve` does: the
// command's flag set parses the arguments, then the environment fills in what
// the operator left unset.
func parseServeDeployment(arguments, environment []string) (Deployment, error) {
	flags := pflag.NewFlagSet("serve", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)

	options := BindServeFlags(flags)
	if err := flags.Parse(arguments); err != nil {
		return Deployment{}, fmt.Errorf("parse serve flags: %w", err)
	}

	return options.Deployment(environment)
}

func TestParseServeDeploymentAppliesCLIEnvironmentAndDefaults(t *testing.T) {
	t.Parallel()

	deployment, err := parseServeDeployment([]string{
		"--database", "cli.db",
		"--listen", "127.0.0.1:9000",
		"--trusted-proxy", "192.0.2.10",
		"--public-url", "https://cli.example",
	}, []string{
		"PATH=/bin",
		"SAVORUM_DATABASE=environment.db",
		"SAVORUM_TRUSTED_PROXIES=10.0.0.0/8, 2001:db8::/32",
		"SAVORUM_PUBLIC_URL=https://environment.example",
		"SAVORUM_DEFAULT_MEASUREMENT=metric",
		"SAVORUM_STORAGE_ACCESS=group",
	})
	if err != nil {
		t.Fatal(err)
	}

	if deployment.DatabasePath != "cli.db" || deployment.AssetPath != "assets" || deployment.ListenAddress != "127.0.0.1:9000" {
		t.Fatalf("deployment = %#v", deployment)
	}

	if len(deployment.TrustedProxies) != 1 || deployment.TrustedProxies[0].String() != "192.0.2.10/32" {
		t.Fatalf("trusted proxies = %#v", deployment.TrustedProxies)
	}

	if deployment.Overrides.PublicURL == nil || *deployment.Overrides.PublicURL != "https://cli.example" {
		t.Fatalf("public URL override = %#v", deployment.Overrides.PublicURL)
	}

	if deployment.Overrides.DefaultMeasurement == nil || *deployment.Overrides.DefaultMeasurement != MeasurementMetric {
		t.Fatalf("measurement override = %#v", deployment.Overrides.DefaultMeasurement)
	}

	if deployment.StoragePolicy.Access != filepolicy.SharedGroup {
		t.Fatalf("storage policy = %#v", deployment.StoragePolicy)
	}
}

func TestThePrintCacheFollowsTheUserCacheDirectory(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		environment []string
		want        string
	}{
		"home":          {[]string{"HOME=/home/cook"}, "/home/cook/.cache/savorum/typst"},
		"xdg":           {[]string{"HOME=/home/cook", "XDG_CACHE_HOME=/volume/cache"}, "/volume/cache/savorum/typst"},
		"relative xdg":  {[]string{"HOME=/home/cook", "XDG_CACHE_HOME=cache"}, ""},
		"no home":       {nil, ""},
		"relative home": {[]string{"HOME=cook"}, ""},
	}
	if runtime.GOOS == "darwin" {
		cases = map[string]struct {
			environment []string
			want        string
		}{
			"home":    {[]string{"HOME=/Users/cook", "XDG_CACHE_HOME=/volume/cache"}, "/Users/cook/Library/Caches/savorum/typst"},
			"no home": {nil, ""},
		}
	}

	for name, test := range cases {
		deployment, err := parseServeDeployment([]string{"--database", "savorum.db"}, test.environment)
		if err != nil {
			t.Fatal(err)
		}

		if deployment.PrintCacheDirectory != test.want {
			t.Errorf("%s: print cache = %q, want %q", name, deployment.PrintCacheDirectory, test.want)
		}
	}
}

func TestThePrintRecipeLimitIsConfigurable(t *testing.T) {
	t.Parallel()

	deployment, err := parseServeDeployment([]string{"--database", "savorum.db"}, nil)
	if err != nil || deployment.PrintRecipeLimit != 250 {
		t.Fatalf("default print recipe limit = %d, %v", deployment.PrintRecipeLimit, err)
	}

	deployment, err = parseServeDeployment([]string{"--database", "savorum.db"}, []string{"SAVORUM_PRINT_RECIPE_LIMIT=1000"})
	if err != nil || deployment.PrintRecipeLimit != 1000 {
		t.Fatalf("configured print recipe limit = %d, %v", deployment.PrintRecipeLimit, err)
	}

	for _, value := range []string{"0", "1000001"} {
		if _, err := parseServeDeployment([]string{"--database", "savorum.db", "--print-recipe-limit", value}, nil); err == nil {
			t.Errorf("print recipe limit %s was accepted", value)
		}
	}
}

func TestParseServeDeploymentRejectsUnknownAndInvalidInput(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		arguments   []string
		environment []string
		contains    string
	}{
		"unknown environment":   {environment: []string{"SAVORUM_DATABSAE=x"}, contains: "unknown Savorum environment variable"},
		"missing database":      {contains: "database path is required"},
		"invalid listener":      {arguments: []string{"--database", "x.db", "--listen", "somewhere"}, contains: "host:port"},
		"invalid override":      {arguments: []string{"--database", "x.db", "--default-measurement", "imperial"}, contains: "default measurement"},
		"invalid storage":       {arguments: []string{"--database", "x.db", "--storage-access", "world"}, contains: "must be owner or group"},
		"short idle timeout":    {arguments: []string{"--database", "x.db", "--idle-timeout", "5s"}, contains: "idle-timeout"},
		"negative idle timeout": {arguments: []string{"--database", "x.db"}, environment: []string{"SAVORUM_IDLE_TIMEOUT=-1m"}, contains: "idle-timeout"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseServeDeployment(test.arguments, test.environment)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("error = %v, want to contain %q", err, test.contains)
			}
		})
	}
}

func TestStandbyIsOptInAndFlagsOverrideEnvironment(t *testing.T) {
	for _, sample := range []struct {
		args, env []string
		want      time.Duration
	}{
		{want: 0},
		{env: []string{"SAVORUM_IDLE_TIMEOUT=5m"}, want: 5 * time.Minute},
		{args: []string{"--idle-timeout", "0"}, env: []string{"SAVORUM_IDLE_TIMEOUT=5m"}, want: 0},
	} {
		value, err := parseServeDeployment(append([]string{"--database", "idle.db"}, sample.args...), sample.env)
		if err != nil || value.IdleTimeout != sample.want {
			t.Fatalf("idle timeout = %v, %v", value.IdleTimeout, err)
		}
	}
}

func TestRepositoryErrorsRemainClassifiable(t *testing.T) {
	t.Parallel()

	repository := &memoryRepository{err: errors.New("database unavailable")}

	service, err := NewService(repository, InstanceOverrides{})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Current(context.Background()); err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("repository error = %v", err)
	}
}

func TestBackupCollectionNeedsKeysIssuerAndHostingBootstrap(t *testing.T) {
	t.Parallel()

	complete := []string{"--database", "hosted.db", "--auth-enabled", "--hosting-bootstrap-file", "/run/bootstrap.json", "--backup-keys-file", "/run/backup.jwks", "--backup-issuer", "https://cloud.invalid"}

	deployment, err := parseServeDeployment(complete, nil)
	if err != nil || deployment.Backup != (BackupCollection{KeysFile: "/run/backup.jwks", Issuer: "https://cloud.invalid"}) {
		t.Fatalf("backup collection = %+v %v", deployment.Backup, err)
	}

	for name, arguments := range map[string][]string{
		"without keys":      {"--database", "hosted.db", "--auth-enabled", "--hosting-bootstrap-file", "/run/bootstrap.json", "--backup-issuer", "https://cloud.invalid"},
		"relative keys":     {"--database", "hosted.db", "--auth-enabled", "--hosting-bootstrap-file", "/run/bootstrap.json", "--backup-keys-file", "backup.jwks", "--backup-issuer", "https://cloud.invalid"},
		"without issuer":    {"--database", "hosted.db", "--auth-enabled", "--hosting-bootstrap-file", "/run/bootstrap.json", "--backup-keys-file", "/run/backup.jwks"},
		"without bootstrap": {"--database", "hosted.db", "--auth-enabled", "--backup-keys-file", "/run/backup.jwks", "--backup-issuer", "https://cloud.invalid"},
	} {
		if _, err := parseServeDeployment(arguments, nil); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestExecutionBudgetsAreIndependentOfHosting(t *testing.T) {
	defaults, err := parseServeDeployment([]string{"--database", "savorum.db"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if defaults.DurableByteLimit != 0 || defaults.SQLiteMemoryLimit != 0 {
		t.Fatal("budgets enabled by default")
	}

	configured, err := parseServeDeployment([]string{"--database", "savorum.db"}, []string{"SAVORUM_DURABLE_BYTE_LIMIT=4294967296", "SAVORUM_SQLITE_MEMORY_LIMIT=134217728"})
	if err != nil || configured.DurableByteLimit != 4<<30 || configured.SQLiteMemoryLimit != 128<<20 {
		t.Fatalf("explicit budgets: %+v %v", configured, err)
	}

	for _, args := range [][]string{{"--durable-byte-limit", "-1"}, {"--sqlite-memory-limit", "-1"}, {"--durable-byte-limit", "9223372036854775807"}} {
		if _, err := parseServeDeployment(append([]string{"--database", "savorum.db"}, args...), nil); err == nil {
			t.Fatal("invalid budget accepted", args)
		}
	}
}

func TestMaintenanceSocketWithoutIdleStandby(t *testing.T) {
	deployment, err := parseServeDeployment([]string{"--database", "idle.db", "--maintenance-socket", "/private/operator.sock"}, nil)
	if err != nil || deployment.IdleTimeout != 0 || deployment.MaintenanceSocket != "/private/operator.sock" {
		t.Fatalf("private channel without standby: %+v %v", deployment, err)
	}
}
