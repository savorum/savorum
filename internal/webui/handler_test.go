package webui_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/webui"
)

// requireBuild skips a test of the built frontend when this checkout has not
// built it, unless SAVORUM_REQUIRE_WEB_BUILD asks for the build to be present.
func requireBuild(t *testing.T) {
	t.Helper()

	response := httptest.NewRecorder()
	webui.Handler().ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	if response.Code == http.StatusServiceUnavailable {
		if os.Getenv("SAVORUM_REQUIRE_WEB_BUILD") != "" {
			t.Fatal("the embedded frontend is not built")
		}

		t.Skip("the embedded frontend is not built; run just web-build")
	}
}

func TestHandlerServesShellAndStaticAssets(t *testing.T) {
	t.Parallel()
	requireBuild(t)

	handler := webui.Handler()

	for _, target := range []string{"/", "/recipes/example"} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), ">Recipes · Savorum</title>") {
			t.Fatalf("%s did not serve the shell: status=%d body=%s", target, response.Code, response.Body.String())
		}
	}

	shellRequest := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	shellResponse := httptest.NewRecorder()
	handler.ServeHTTP(shellResponse, shellRequest)

	assetMatch := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindStringSubmatch(shellResponse.Body.String())
	if len(assetMatch) != 2 {
		t.Fatalf("shell does not reference a built asset: %s", shellResponse.Body.String())
	}

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, assetMatch[1], nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.Len() == 0 {
		t.Fatalf("static asset response: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHandlerRejectsMutationMethods(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
	response := httptest.NewRecorder()
	webui.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestShareManifestHasManifestMediaType(t *testing.T) {
	requireBuild(t)

	response := httptest.NewRecorder()
	webui.Handler().ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/manifest.webmanifest", nil))

	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/manifest+json" || !strings.Contains(response.Body.String(), `"share_target"`) {
		t.Fatalf("manifest status=%d type=%q", response.Code, response.Header().Get("Content-Type"))
	}
}
