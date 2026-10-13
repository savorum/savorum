package httpsecurity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConnectOriginRequiresLoopbackAgentAndKeepsLocalOrigin(t *testing.T) {
	remote := "https://25e155cd-a53f-4797-91bf-38e056570a59.connect.test"

	handler := NewHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), Options{
		ListenAddress: "127.0.0.1:8080", ConnectOrigin: remote,
		ApplicationOrigin: func(context.Context) (string, error) { return "http://127.0.0.1:8080", nil },
	})
	for _, tc := range []struct {
		name, host, origin, peer string
		want                     int
	}{
		{"remote agent", "25e155cd-a53f-4797-91bf-38e056570a59.connect.test", remote, "127.0.0.1:41000", http.StatusNoContent},
		{"local browser", "127.0.0.1:8080", "http://127.0.0.1:8080", "127.0.0.1:41000", http.StatusNoContent},
		{"remote untrusted peer", "25e155cd-a53f-4797-91bf-38e056570a59.connect.test", remote, "198.51.100.9:41000", http.StatusMisdirectedRequest},
		{"wrong origin", "25e155cd-a53f-4797-91bf-38e056570a59.connect.test", "https://other.test", "127.0.0.1:41000", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+tc.host+"/api/v1/account/login", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("Origin", tc.origin)

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
