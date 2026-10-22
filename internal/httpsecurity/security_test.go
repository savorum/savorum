package httpsecurity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestWriteProblemProducesValidJSONForControlCharacters(t *testing.T) {
	response := httptest.NewRecorder()
	wantDetail := "invalid\x01detail"

	WriteProblem(response, http.StatusBadRequest, "invalid-input", wantDetail)

	if !json.Valid(response.Body.Bytes()) {
		t.Fatalf("problem body is not valid JSON: %q", response.Body.Bytes())
	}

	var problem struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}

	if problem.Detail != wantDetail {
		t.Fatalf("detail = %q, want %q", problem.Detail, wantDetail)
	}
}

func TestNewServerDefinesFiniteConnectionLimits(t *testing.T) {
	server := NewServer("127.0.0.1:8080", http.NotFoundHandler())
	if server.Addr != "127.0.0.1:8080" {
		t.Fatalf("Addr = %q", server.Addr)
	}

	if server.ReadHeaderTimeout <= 0 || server.ReadTimeout <= 0 || server.WriteTimeout <= 0 || server.IdleTimeout <= 0 {
		t.Fatalf("server timeouts must all be finite: %#v", server)
	}

	if server.MaxHeaderBytes != MaximumHeaderBytes {
		t.Fatalf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, MaximumHeaderBytes)
	}
}

func TestNewHandlerSetsSecurityHeadersOnEveryResponse(t *testing.T) {
	handler := NewHandler(http.NotFoundHandler(), Options{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://savorum.test/missing", nil))

	for name, expected := range map[string]string{
		"Content-Security-Policy": "default-src 'self'",
		"Permissions-Policy":      "camera=()",
		"Referrer-Policy":         "no-referrer",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
	} {
		if actual := response.Header().Get(name); actual == "" || (name == "Content-Security-Policy" && !bytes.Contains([]byte(actual), []byte(expected))) || (name == "Permissions-Policy" && !bytes.Contains([]byte(actual), []byte(expected))) || (name != "Content-Security-Policy" && name != "Permissions-Policy" && actual != expected) {
			t.Errorf("%s = %q, want %q", name, actual, expected)
		}
	}
}

func TestNewHandlerRejectsDeclaredOversizedRequestBeforeRouting(t *testing.T) {
	called := false
	handler := NewHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}), Options{})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://savorum.test/api/v1/recipes", bytes.NewReader(nil))
	request.ContentLength = DefaultRequestBodyBytes + 1
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}

	if called {
		t.Fatal("oversized request reached the route")
	}
}

func TestNewHandlerBoundsChunkedRequestWhileReading(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), int(DefaultRequestBodyBytes+1))
	handler := NewHandler(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, err := io.ReadAll(request.Body)

		if _, ok := errors.AsType[*http.MaxBytesError](err); !ok {
			t.Errorf("read error = %v, want *http.MaxBytesError", err)
			http.Error(response, "wrong error", http.StatusInternalServerError)

			return
		}

		response.WriteHeader(http.StatusRequestEntityTooLarge)
	}), Options{})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://savorum.test/api/v1/recipes", bytes.NewReader(payload))
	request.ContentLength = -1
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestRequestBodyLimitCentralizesUploadExceptions(t *testing.T) {
	for _, test := range []struct {
		path string
		want int64
	}{
		{"/api/v1/recipes", DefaultRequestBodyBytes},
		{"/api/v1/assets", AssetRequestBodyBytes},
		{"/api/v1/recipes/import-external", ExternalImportBodyBytes},
		{"/api/v1/recipes/import-preview", ExternalImportBodyBytes},
		{"/api/v1/recipes/import-package", PackageImportBodyBytes},
	} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://savorum.test"+test.path, nil)
		if got := requestBodyLimit(request); got != test.want {
			t.Errorf("requestBodyLimit(%q) = %d, want %d", test.path, got, test.want)
		}
	}
}

func TestUnsafeMethodsRejectCrossSiteBrowserSignalsBeforeRouting(t *testing.T) {
	t.Parallel()

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			called := false
			handler := NewHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }), Options{})
			request := httptest.NewRequestWithContext(t.Context(), method, "http://savorum.test/api/v1/recipes", nil)
			request.Header.Set("Origin", "https://attacker.example")
			request.Header.Set("Sec-Fetch-Site", "cross-site")

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusForbidden || called {
				t.Fatalf("%s status=%d called=%t body=%s", method, response.Code, called, response.Body.String())
			}
		})
	}
}

func TestUnsafeRequestOriginPolicySupportsBrowsersAndNonBrowserClients(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		origin    string
		fetchSite string
		want      int
	}{
		{name: "same origin", origin: "http://savorum.test", fetchSite: "same-origin", want: http.StatusNoContent},
		{name: "default port is equivalent", origin: "http://savorum.test:80", fetchSite: "same-origin", want: http.StatusNoContent},
		{name: "different origin", origin: "https://other.test", fetchSite: "same-site", want: http.StatusForbidden},
		{name: "opaque origin", origin: "null", fetchSite: "cross-site", want: http.StatusForbidden},
		{name: "fetch metadata alone rejects", fetchSite: "cross-site", want: http.StatusForbidden},
		{name: "absent browser signals is an API client", want: http.StatusNoContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewHandler(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(http.StatusNoContent)
			}), Options{})

			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://savorum.test/api/v1/recipes", nil)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}

			if test.fetchSite != "" {
				request.Header.Set("Sec-Fetch-Site", test.fetchSite)
			}

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestConfiguredOriginAndTrustedProxyDefineSameOrigin(t *testing.T) {
	t.Parallel()

	configured := func(context.Context) (string, error) { return "https://recipes.example", nil }
	handler := NewHandler(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}), Options{
		TrustedProxies:    []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		ListenAddress:     "127.0.0.1:8080",
		ApplicationOrigin: configured,
	})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://127.0.0.1:8080/api/v1/recipes", nil)
	request.RemoteAddr = "10.0.0.2:42000"
	request.Header.Set("X-Forwarded-Host", "recipes.example")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Origin", "https://recipes.example")
	request.Header.Set("Sec-Fetch-Site", "same-origin")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("trusted proxy same-origin status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://127.0.0.1:8080/api/v1/recipes", nil)
	request.RemoteAddr = "198.51.100.7:42000"
	request.Header.Set("X-Forwarded-Host", "recipes.example")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Origin", "https://recipes.example")

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("configured origin should not trust forwarded authority: status=%d body=%s", response.Code, response.Body.String())
	}
	// The configured public origin is authoritative; the untrusted forwarded
	// headers were stripped rather than used to manufacture that authority.
	if request.Header.Get("X-Forwarded-Host") != "" || request.Header.Get("X-Forwarded-Proto") != "" {
		t.Fatal("untrusted forwarded authority survived")
	}
}

func TestAuthorityPolicyRejectsRebindingBeforeRouting(t *testing.T) {
	t.Parallel()

	called := false
	handler := NewHandler(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		called = true
		_, _ = response.Write([]byte(request.Host)) //nolint:gosec // a test handler echoing a header this test supplied
	}), Options{ListenAddress: "127.0.0.1:8080"})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://attacker.example/openapi.json", nil)
	request.Header.Set("Origin", "http://attacker.example")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusMisdirectedRequest || called {
		t.Fatalf("status=%d called=%t body=%s", response.Code, called, response.Body.String())
	}

	if strings.Contains(response.Body.String(), "attacker.example") {
		t.Fatalf("rejected authority was reflected: %s", response.Body.String())
	}
}

func TestLoopbackAuthorityAliasesAreExplicitAndPortBound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url  string
		want int
	}{
		{url: "http://127.0.0.1:8080/", want: http.StatusNoContent},
		{url: "http://localhost:8080/", want: http.StatusNoContent},
		{url: "http://LOCALHOST.:8080/", want: http.StatusNoContent},
		{url: "http://[::1]:8080/", want: http.StatusNoContent},
		{url: "http://[::ffff:127.0.0.1]:8080/", want: http.StatusNoContent},
		{url: "http://localhost:8081/", want: http.StatusMisdirectedRequest},
		{url: "http://192.168.1.20:8080/", want: http.StatusMisdirectedRequest},
	}
	handler := NewHandler(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}), Options{ListenAddress: "127.0.0.1:8080"})

	for _, test := range tests {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.url, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		if response.Code != test.want {
			t.Errorf("%s status=%d want=%d body=%s", test.url, response.Code, test.want, response.Body.String())
		}
	}
}

func TestPublicAuthorityAcceptsIDNAThroughTrustedProxy(t *testing.T) {
	t.Parallel()

	configured := func(context.Context) (string, error) { return "https://b\u00fccher.example", nil }
	called := false
	handler := NewHandler(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		called = true

		response.WriteHeader(http.StatusNoContent)
	}), Options{
		TrustedProxies:    []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		ListenAddress:     "127.0.0.1:8080",
		ApplicationOrigin: configured,
	})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:8080/openapi.json", nil)
	request.RemoteAddr = "10.0.0.2:42000"
	request.Header.Set("X-Forwarded-Host", "xn--bcher-kva.example.")
	request.Header.Set("X-Forwarded-Proto", "https")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || !called {
		t.Fatalf("status=%d called=%t body=%s", response.Code, called, response.Body.String())
	}

	request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:8080/openapi.json", nil)
	request.RemoteAddr = "10.0.0.2:42000"
	request.Header.Set("X-Forwarded-Host", "attacker.example")
	request.Header.Set("X-Forwarded-Proto", "https")

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusMisdirectedRequest {
		t.Fatalf("unconfigured forwarded authority status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSpecificLANListenerAndConfiguredNameAreTheOnlyLANAuthorities(t *testing.T) {
	t.Parallel()

	configured := func(context.Context) (string, error) { return "http://recipes.lan:8080", nil }

	handler := NewHandler(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}), Options{ListenAddress: "192.168.1.20:8080", ApplicationOrigin: configured})
	for _, test := range []struct {
		url  string
		want int
	}{
		{url: "http://192.168.1.20:8080/", want: http.StatusNoContent},
		{url: "http://recipes.lan:8080/", want: http.StatusNoContent},
		{url: "http://localhost:8080/", want: http.StatusMisdirectedRequest},
		{url: "http://other.lan:8080/", want: http.StatusMisdirectedRequest},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.url, nil))

		if response.Code != test.want {
			t.Errorf("%s status=%d want=%d body=%s", test.url, response.Code, test.want, response.Body.String())
		}
	}
}

type observedReader struct {
	read bool
}

func (r *observedReader) Read([]byte) (int, error) {
	r.read = true
	return 0, io.EOF
}

func TestCrossSiteMultipartIsRejectedWithoutReadingItsBody(t *testing.T) {
	t.Parallel()

	body := &observedReader{}
	called := false
	handler := NewHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }), Options{})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://savorum.test/api/v1/recipes/import-external", body)
	request.Header.Set("Content-Type", "multipart/form-data; boundary=safelisted")
	request.Header.Set("Origin", "https://attacker.example")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || called || body.read {
		t.Fatalf("status=%d called=%t body-read=%t", response.Code, called, body.read)
	}
}

func TestUntrustedPeerCannotSupplyForwardedMetadata(t *testing.T) {
	var observed *http.Request

	handler := NewHandler(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		observed = request.Clone(request.Context())
	}), Options{
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		ListenAddress:  "127.0.0.1:8080",
	})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:8080/", nil)
	request.RemoteAddr = "198.51.100.9:42000"
	request.Header.Set("Forwarded", "for=203.0.113.7;proto=https;host=recipes.example")
	request.Header.Set("X-Forwarded-For", "203.0.113.7")
	request.Header.Set("X-Forwarded-Host", "recipes.example")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Real-IP", "203.0.113.7")

	handler.ServeHTTP(httptest.NewRecorder(), request)

	if observed == nil {
		t.Fatal("route was not called")
	}

	for _, name := range forwardedHeaders {
		if value := observed.Header.Get(name); value != "" {
			t.Errorf("%s survived from an untrusted peer: %q", name, value)
		}
	}

	if observed.Host != "127.0.0.1:8080" || observed.URL.Scheme != "http" {
		t.Errorf("effective request changed to %s://%s", observed.URL.Scheme, observed.Host)
	}

	if got := ClientAddress(observed); got != netip.MustParseAddr("198.51.100.9") {
		t.Errorf("ClientAddress = %s", got)
	}
}

func TestTrustedPeerSuppliesValidatedForwardedMetadata(t *testing.T) {
	trusted, err := ParseTrustedProxies([]string{"10.0.0.0/8", "192.0.2.10"})
	if err != nil {
		t.Fatal(err)
	}

	var observed *http.Request

	handler := NewHandler(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		observed = request.Clone(request.Context())
	}), Options{TrustedProxies: trusted})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://internal:8080/", nil)
	request.RemoteAddr = "10.0.0.2:42000"
	request.Header.Set("Forwarded", "for=untrusted")
	request.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	request.Header.Set("X-Forwarded-Host", "recipes.example:8443")
	request.Header.Set("X-Forwarded-Proto", "HTTPS")

	handler.ServeHTTP(httptest.NewRecorder(), request)

	if observed == nil {
		t.Fatal("route was not called")
	}

	if observed.Header.Get("Forwarded") != "" {
		t.Fatal("unsupported Forwarded header was not stripped")
	}

	if observed.URL.Scheme != "https" || observed.Host != "recipes.example:8443" {
		t.Errorf("effective request = %s://%s", observed.URL.Scheme, observed.Host)
	}

	if observed.RemoteAddr != "203.0.113.7:0" {
		t.Errorf("RemoteAddr = %q", observed.RemoteAddr)
	}

	if got := ClientAddress(observed); got != netip.MustParseAddr("203.0.113.7") {
		t.Errorf("ClientAddress = %s", got)
	}
}

func TestTrustedPeerWithMalformedForwardedMetadataIsRejected(t *testing.T) {
	called := false
	handler := NewHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}), Options{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://internal:8080/", nil)
	request.RemoteAddr = "10.0.0.2:42000"
	request.Header.Set("X-Forwarded-Proto", "https, http")

	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}

	if called {
		t.Fatal("malformed forwarded request reached the route")
	}
}

func TestParseTrustedProxiesRejectsNames(t *testing.T) {
	if _, err := ParseTrustedProxies([]string{"proxy.internal"}); err == nil {
		t.Fatal("hostname trusted proxy was accepted")
	}
}

func TestUnspecifiedListenerAcceptsLANAuthoritiesAndLocalhost(t *testing.T) {
	t.Parallel()

	handler := NewHandler(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}), Options{ListenAddress: "0.0.0.0:8080"})

	for _, test := range []struct {
		url  string
		want int
	}{
		{url: "http://localhost:8080/", want: http.StatusNoContent},
		{url: "http://127.0.0.1:8080/", want: http.StatusNoContent},
		{url: "http://[::1]:8080/", want: http.StatusNoContent},
		{url: "http://savorum.local:8080/", want: http.StatusNoContent},
		{url: "http://my-kitchen.local:8080/", want: http.StatusNoContent},
		{url: "http://192.168.1.50:8080/", want: http.StatusNoContent},
		{url: "http://10.0.0.5:8080/", want: http.StatusNoContent},
		{url: "http://172.16.0.10:8080/", want: http.StatusNoContent},
		{url: "http://attacker.example:8080/", want: http.StatusMisdirectedRequest},
		{url: "http://savorum.local:9090/", want: http.StatusMisdirectedRequest},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.url, nil))

		if response.Code != test.want {
			t.Errorf("url=%s status=%d want=%d body=%s", test.url, response.Code, test.want, response.Body.String())
		}
	}
}

func TestAbsoluteRequestTargetCannotClaimTLS(t *testing.T) {
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://recipes.example/llms.txt", nil)
	request.TLS = nil
	request.Header.Set("X-Forwarded-Proto", "https")

	handler := NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := PublicOrigin(r, ""); got != "http://recipes.example" {
			t.Errorf("untrusted request claimed HTTPS: %s", got)
		}

		w.WriteHeader(http.StatusNoContent)
	}), Options{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("unexpected response: %d", response.Code)
	}
}
