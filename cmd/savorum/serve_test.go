package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/configuration"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

func TestServeReportsBadDeploymentConfigurationAsAUsageError(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		arguments []string
		environ   []string
		contains  string
	}{
		"no database":         {contains: "database path is required"},
		"unknown variable":    {environ: []string{"SAVORUM_DATABSAE=x.db"}, contains: "unknown Savorum environment variable"},
		"invalid listener":    {arguments: []string{"--database", "x.db", "--listen", "somewhere"}, contains: "host:port"},
		"invalid environment": {environ: []string{"SAVORUM_LISTEN=somewhere", "SAVORUM_DATABASE=x.db"}, contains: "host:port"},
		"invalid measurement": {arguments: []string{"--database", "x.db", "--default-measurement", "imperial"}, contains: "default measurement"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			outcome := invocation{
				arguments: append([]string{"serve"}, test.arguments...),
				environ:   test.environ,
			}.run(t)

			if outcome.code != exitUsage {
				t.Fatalf("exit code = %d, stderr = %q", outcome.code, outcome.stderr)
			}

			if !strings.Contains(outcome.stderr, test.contains) {
				t.Fatalf("stderr = %q, want to contain %q", outcome.stderr, test.contains)
			}
		})
	}
}

func TestServeHelpDescribesTheBuiltInDefaultsWhateverTheEnvironmentHolds(t *testing.T) {
	t.Parallel()

	outcome := invocation{
		arguments: []string{"serve", "--help"},
		environ:   []string{"SAVORUM_LISTEN=0.0.0.0:9999", "SAVORUM_DATABSAE=misspelled"},
	}.run(t)

	if outcome.code != exitSuccess {
		t.Fatalf("exit code = %d, stderr = %q", outcome.code, outcome.stderr)
	}

	if !strings.Contains(outcome.stdout, `--listen string`) || !strings.Contains(outcome.stdout, `"127.0.0.1:8080"`) {
		t.Fatalf("serve help = %q", truncate(outcome.stdout))
	}
}

func TestHealthcheckRequiresAReadyNonRedirectingEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/health" {
			http.NotFound(response, request)
			return
		}

		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	healthy := savorum(t, "healthcheck", "--url", server.URL+"/api/v1/health")
	if healthy.code != exitSuccess {
		t.Fatalf("healthy endpoint exit code = %d, stderr = %q", healthy.code, healthy.stderr)
	}

	if healthy.stdout != "" || healthy.stderr != "" {
		t.Fatalf("a passing probe wrote %q / %q", healthy.stdout, healthy.stderr)
	}

	unavailable := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "not ready", http.StatusServiceUnavailable)
	}))
	defer unavailable.Close()

	notReady := savorum(t, "healthcheck", "--url", unavailable.URL)
	if notReady.code != exitFailure || !strings.Contains(notReady.stderr, "503") {
		t.Fatalf("unavailable endpoint exit code = %d, stderr = %q", notReady.code, notReady.stderr)
	}

	redirect := httptest.NewServer(http.RedirectHandler(server.URL+"/api/v1/health", http.StatusTemporaryRedirect))
	defer redirect.Close()

	redirected := savorum(t, "healthcheck", "--url", redirect.URL)
	if redirected.code != exitFailure || !strings.Contains(redirected.stderr, "redirected") {
		t.Fatalf("redirect endpoint exit code = %d, stderr = %q", redirected.code, redirected.stderr)
	}
}

func TestHostingBootstrapBeforeServingIsRepeatableAndRejectsMismatch(t *testing.T) {
	dir := t.TempDir()

	db, err := sqlite.Open(t.Context(), filepath.Join(dir, "savorum.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	path := filepath.Join(dir, "bootstrap.json")

	request := `{"operation_id":"bootstrap-test","tenant_id":"tenant-test","public_url":"https://tenant.example","provider_id":"cloud","issuer":"https://accounts.example","owner":{"subject":"account-test","display_name":"Owner"},"library_name":"Recipes"}`
	if err := os.WriteFile(path, []byte(request), 0o600); err != nil {
		t.Fatal(err)
	}

	deployment := configuration.Deployment{AuthEnabled: true, HostingBootstrapFile: path}
	for range 2 {
		if err := bootstrapBeforeServing(t.Context(), db, deployment); err != nil {
			t.Fatal(err)
		}
	}

	before, err := db.HostingTenant(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if before.Owner.Subject != "account-test" {
		t.Fatalf("wrong bootstrap: %+v", before)
	}

	if err := os.WriteFile(path, []byte(strings.ReplaceAll(request, "account-test", "different-account")), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := bootstrapBeforeServing(t.Context(), db, deployment); err == nil {
		t.Fatal("mismatched bootstrap accepted")
	}

	after, err := db.HostingTenant(t.Context())
	if err != nil || after.OwnerUserID != before.OwnerUserID {
		t.Fatalf("bootstrap replaced owner: %+v %v", after, err)
	}

	deployment.AuthEnabled = false
	if err := bootstrapBeforeServing(t.Context(), db, deployment); err == nil {
		t.Fatal("unauthenticated bootstrap accepted")
	}
}
