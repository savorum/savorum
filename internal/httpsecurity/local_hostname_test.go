package httpsecurity

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestLocalHostnameAuthority(t *testing.T) {
	t.Parallel()

	handler := NewHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), Options{
		ListenAddress:  "192.168.124.158:8080",
		LocalHostnames: []string{"machine.home.arpa"},
	})

	for _, tc := range []struct {
		name, method, url, origin string
		status                    int
	}{
		{"hostname", "GET", "http://machine.home.arpa:8080/", "", 204},
		{"bound address", "GET", "http://192.168.124.158:8080/", "", 204},
		{"same origin mutation", "POST", "http://machine.home.arpa:8080/api", "http://machine.home.arpa:8080", 204},
		{"cross site mutation", "POST", "http://machine.home.arpa:8080/api", "http://attacker.example", 403},
		{"wrong port", "GET", "http://machine.home.arpa:8081/", "", 421},
		{"unrelated hostname", "GET", "http://other.home.arpa:8080/", "", 421},
		{"incorrect suffix", "GET", "http://machine.home.arpa.local:8080/", "", 421},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), tc.method, tc.url, nil)
			if tc.origin != "" {
				request.Header.Set("Origin", tc.origin)
			}

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}
		})
	}
}

func TestHostnameMustResolveToListener(t *testing.T) {
	t.Parallel()

	bound := netip.MustParseAddr("192.168.124.158")
	for _, tc := range []struct {
		address string
		want    bool
	}{
		{"192.168.124.158", true},
		{"::ffff:192.168.124.158", true},
		{"192.168.124.159", false},
		{"127.0.0.1", false},
		{"2001:db8::1", false},
	} {
		if got := hostnameMatchesListener(bound, []netip.Addr{netip.MustParseAddr(tc.address)}); got != tc.want {
			t.Errorf("match(%s) = %v, want %v", tc.address, got, tc.want)
		}
	}
}
