package connect

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testSetup() Setup {
	return Setup{Version: 1, Relay: "https://relay.example.test:8443", Instance: "25e155cd-a53f-4797-91bf-38e056570a59", RemoteOrigin: "https://acorn-apple-basil-basket.via.example.test", Credential: strings.Repeat("a", 43)}
}

func testService(directory string, enabled bool) *Service {
	s := New(directory, enabled)
	s.claimCredential = func(_ context.Context, setup Setup) (string, error) { return setup.Credential, nil }
	s.revokeCredential = func(context.Context, Setup) error { return nil }

	return s
}

func TestPrivateEnrollmentRoundTripAndDisconnect(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "connect")

	s := testService(dir, true)
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("disabled Connect created state")
	}

	setup := testSetup()
	if err := s.Configure(setup); err != nil {
		t.Fatal(err)
	}

	for path, mode := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, "setup.json"): 0o600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("private state: %v %v", info, err)
		}
	}

	restored := New(dir, true)
	if restored.Origin() != setup.RemoteOrigin || !restored.Status().Configured {
		t.Fatal(restored.Status())
	}

	encoded, encodeErr := json.Marshal(restored.Status())
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}

	if strings.Contains(string(encoded), setup.Credential) {
		t.Fatal("status exposed credential")
	}

	disabled := testService(dir, false)
	if disabled.Origin() != "" || disabled.Status().State != "authentication_required" {
		t.Fatal(disabled.Status())
	}

	if err := disabled.Configure(setup); err == nil {
		t.Fatal("configured without authentication")
	}

	if err := s.Disconnect(); err != nil {
		t.Fatal(err)
	}

	if New(dir, true).Status().Configured || s.Origin() != "" {
		t.Fatal("disconnect retained enrollment")
	}
}

func TestSetupClaimReplacesExposedSecretAndDisconnectRevokes(t *testing.T) {
	bootstrap := strings.Repeat("a", 43)
	agentSecret := strings.Repeat("b", 43)

	var revokeStatus atomic.Int32

	relay := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/claim"):
			if r.Header.Get("Authorization") != "Bearer "+bootstrap {
				t.Error("claim used wrong setup credential")
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"credential":"` + agentSecret + `"}`))
		case strings.HasSuffix(r.URL.Path, "/revoke"):
			if r.Header.Get("Authorization") != "Bearer "+agentSecret {
				t.Error("revoke used wrong agent credential")
			}

			w.WriteHeader(int(revokeStatus.Load()))
		default:
			http.NotFound(w, r)
		}
	}))
	defer relay.Close()

	dir := filepath.Join(t.TempDir(), "connect")
	s := NewWithCredentialClient(dir, true, relay.Client())
	setup := testSetup()
	setup.Relay = relay.URL

	setup.Credential = bootstrap
	if err := s.Configure(setup); err != nil {
		t.Fatal(err)
	}

	saved, err := os.ReadFile(filepath.Join(dir, "setup.json"))
	if err != nil || strings.Contains(string(saved), bootstrap) || !strings.Contains(string(saved), agentSecret) {
		t.Fatal("private enrollment did not replace the exposed setup secret", err)
	}

	revokeStatus.Store(http.StatusServiceUnavailable)

	if err := s.Disconnect(); err == nil || !s.Status().Configured {
		t.Fatal("failed Cloud revocation removed local enrollment", err)
	}

	revokeStatus.Store(http.StatusNoContent)

	if err := s.Disconnect(); err != nil || s.Status().Configured {
		t.Fatal("successful Cloud revocation retained enrollment", err)
	}
}

func TestEnrollmentValidationPreservesExistingSetup(t *testing.T) {
	s := testService(filepath.Join(t.TempDir(), "connect"), true)

	valid := testSetup()
	if err := s.Configure(valid); err != nil {
		t.Fatal(err)
	}

	for _, change := range []func(*Setup){func(s *Setup) { s.Version = 2 }, func(s *Setup) { s.Relay = "http://relay.test" }, func(s *Setup) { s.Credential = "a\r\nInjected: yes" }, func(s *Setup) { s.RemoteOrigin = "https://-invalid.via.example.test" }, func(s *Setup) { s.RemoteOrigin += "/path" }, func(s *Setup) { s.Instance = "../escape" }} {
		candidate := valid
		change(&candidate)

		if err := s.Configure(candidate); err == nil {
			t.Fatal("accepted invalid setup")
		}

		if s.Origin() != valid.RemoteOrigin {
			t.Fatal("invalid setup replaced enrollment")
		}
	}
}

func TestClientLifecycleIsIndependentAndReconfigurable(t *testing.T) {
	s := testService(filepath.Join(t.TempDir(), "connect"), true)
	started := make(chan Agent, 4)
	stopped := make(chan struct{}, 4)

	var calls atomic.Int32

	s.runAgent = func(ctx context.Context, a Agent) error {
		calls.Add(1)

		started <- a

		a.ConnectionStatus(true)
		<-ctx.Done()

		stopped <- struct{}{}

		return ctx.Err()
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx, http.NotFoundHandler()) }()

	if calls.Load() != 0 {
		t.Fatal("connected without enrollment")
	}

	if err := s.Configure(testSetup()); err != nil {
		t.Fatal(err)
	}

	select {
	case a := <-started:
		if a.Handler == nil {
			t.Fatal("missing application handler")
		}
	case <-time.After(time.Second):
		t.Fatal("client did not start")
	}

	replacement := testSetup()

	replacement.Credential = strings.Repeat("b", 43)
	if err := s.Configure(replacement); err != nil {
		t.Fatal(err)
	}

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("old client was not stopped")
	}

	select {
	case a := <-started:
		if a.Credential != replacement.Credential {
			t.Fatal("wrong credential")
		}
	case <-time.After(time.Second):
		t.Fatal("replacement did not start")
	}

	if err := s.Disconnect(); err != nil {
		t.Fatal(err)
	}

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("disconnect did not stop client")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish")
	}
}

func TestClientFailureDoesNotStopService(t *testing.T) {
	s := testService(filepath.Join(t.TempDir(), "connect"), true)
	if err := s.Configure(testSetup()); err != nil {
		t.Fatal(err)
	}

	failed := make(chan struct{}, 2)
	s.runAgent = func(context.Context, Agent) error { failed <- struct{}{}; return errors.New("relay unavailable") }

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx, http.NotFoundHandler()) }()

	<-failed

	select {
	case <-done:
		t.Fatal("remote failure stopped service")
	case <-time.After(20 * time.Millisecond):
	}

	if s.Status().State != "unavailable" {
		t.Fatal(s.Status())
	}

	cancel()
	<-done
}

func TestEnrollmentRejectsInsecurePersistedState(t *testing.T) {
	for _, kind := range []string{"directory", "file", "symlink", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "connect")

			s := testService(dir, true)
			if err := s.Configure(testSetup()); err != nil {
				t.Fatal(err)
			}

			path := filepath.Join(dir, "setup.json")

			switch kind {
			case "directory":
				// #nosec G302 -- Verifies rejection of deliberately public fixture state.
				if err := os.Chmod(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			case "file":
				// #nosec G302 -- Verifies rejection of a deliberately public fixture credential.
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(t.TempDir(), "secret")
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}

				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := os.WriteFile(path, []byte(strings.Repeat("x", 8193)), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			restored := New(dir, true)
			if restored.Status().Configured || restored.Origin() != "" {
				t.Fatal("unsafe enrollment loaded")
			}
		})
	}
}
