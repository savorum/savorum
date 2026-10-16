package connect

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Setup contains enrollment credentials from the relay operator.
type Setup struct {
	Version      int    `json:"version" enum:"1"`
	Relay        string `json:"relay" maxLength:"2048"`
	Instance     string `json:"instance" maxLength:"63"`
	RemoteOrigin string `json:"remote_origin" maxLength:"2048"`
	Credential   string `json:"credential" maxLength:"4096" writeOnly:"true"`
}

func (s Setup) validate() error {
	secret, err := base64.RawURLEncoding.DecodeString(s.Credential)
	if s.Version != 1 || err != nil || len(secret) != 32 || !validLabel(s.Instance) || len(s.Relay) > 2048 || len(s.RemoteOrigin) > 2048 {
		return errors.New("invalid Connect setup")
	}

	relay, err := url.Parse(s.Relay)
	if err != nil || relay.Scheme != "https" {
		return errors.New("connect relay must use HTTPS")
	}

	a := Agent{RelayURL: s.Relay, InstanceID: s.Instance, RemoteOrigin: s.RemoteOrigin, Credential: s.Credential, Handler: http.NotFoundHandler()}
	if _, _, _, err := a.validate(); err != nil {
		return fmt.Errorf("invalid Connect setup: %w", err)
	}

	return nil
}

// Status reports connection and certificate readiness.
type Status struct {
	Configured         bool   `json:"configured"`
	State              string `json:"state"`
	RemoteOrigin       string `json:"remote_origin,omitempty"`
	CertificateReady   bool   `json:"certificate_ready"`
	CertificateExpires string `json:"certificate_expires,omitempty"`
}

// Service manages saved enrollment and the Connect client.
type Service struct {
	mu               sync.Mutex
	directory        string
	enabled          bool
	setup            *Setup
	status           Status
	wake             chan struct{}
	runAgent         func(context.Context, Agent) error
	claimCredential  func(context.Context, Setup) (string, error)
	revokeCredential func(context.Context, Setup) error
}

// New loads saved enrollment.
func New(directory string, authEnabled bool) *Service {
	return NewWithCredentialClient(directory, authEnabled, nil)
}

// NewWithCredentialClient loads enrollment with custom relay HTTPS trust roots.
func NewWithCredentialClient(directory string, authEnabled bool, client *http.Client) *Service {
	s := &Service{directory: directory, enabled: authEnabled, status: Status{State: "disabled"}, wake: make(chan struct{}, 1), runAgent: func(ctx context.Context, a Agent) error { return a.Run(ctx) }, claimCredential: func(ctx context.Context, setup Setup) (string, error) {
		return controlCredential(ctx, setup, "claim", client)
	}, revokeCredential: func(ctx context.Context, setup Setup) error {
		_, err := controlCredential(ctx, setup, "revoke", client)
		return err
	}}

	data, err := readSetup(directory)
	if errors.Is(err, os.ErrNotExist) {
		return s
	}

	if err != nil {
		s.status.State = "storage_error"
		return s
	}

	var setup Setup
	if json.Unmarshal(data, &setup) != nil || setup.validate() != nil {
		s.status.State = "invalid_setup"
		return s
	}

	s.setup = &setup

	s.status = Status{Configured: true, State: "connecting", RemoteOrigin: setup.RemoteOrigin}
	if !authEnabled {
		s.status.State = "authentication_required"
	}

	return s
}

// Status returns a snapshot of the connection state.
func (s *Service) Status() Status { s.mu.Lock(); defer s.mu.Unlock(); return s.status }

// Origin returns an empty string when authentication or enrollment is missing.
func (s *Service) Origin() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled || s.setup == nil {
		return ""
	}

	return s.setup.RemoteOrigin
}

// Configure saves enrollment and restarts the client.
func (s *Service) Configure(setup Setup) error {
	if err := setup.validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled {
		return errors.New("authentication is required")
	}

	if s.setup != nil && s.setup.Instance != setup.Instance {
		return errors.New("disconnect the current Connect instance before enrolling another")
	}

	if err := os.MkdirAll(s.directory, 0o700); err != nil {
		return fmt.Errorf("create Connect state: %w", err)
	}

	info, err := os.Lstat(s.directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("connect state directory must be private")
	}

	claimed, err := s.claimCredential(context.Background(), setup)
	if err != nil {
		return fmt.Errorf("claim Connect setup: %w", err)
	}

	setup.Credential = claimed

	committed := false
	defer func() {
		if !committed {
			_ = s.revokeCredential(context.Background(), setup)
			if s.setup != nil {
				s.status.State = "unavailable"
				s.signal()
			}
		}
	}()

	data, err := json.Marshal(setup)
	if err != nil {
		return fmt.Errorf("encode Connect setup: %w", err)
	}

	file, err := os.CreateTemp(s.directory, ".setup-*")
	if err != nil {
		return fmt.Errorf("create Connect setup: %w", err)
	}
	defer func() { _ = os.Remove(file.Name()) }()

	_, writeErr := file.Write(data)
	syncErr := file.Sync()

	closeErr := file.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("write Connect setup: %w", err)
	}

	if err = os.Rename(file.Name(), filepath.Join(s.directory, "setup.json")); err != nil {
		return fmt.Errorf("save Connect setup: %w", err)
	}

	committed = true

	s.setup = &setup
	s.status = Status{Configured: true, State: "connecting", RemoteOrigin: setup.RemoteOrigin}
	s.signal()

	return nil
}

// Disconnect revokes remote access and removes enrollment while retaining certificates.
func (s *Service) Disconnect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.setup != nil {
		if err := s.revokeCredential(context.Background(), *s.setup); err != nil {
			return fmt.Errorf("revoke Connect credential: %w", err)
		}
	}

	if err := os.Remove(filepath.Join(s.directory, "setup.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove Connect setup: %w", err)
	}

	s.setup = nil
	s.status = Status{State: "disabled"}
	s.signal()

	return nil
}

func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run retries connections until ctx is canceled.
func (s *Service) Run(ctx context.Context, handler http.Handler) {
	var (
		cancel context.CancelFunc
		done   chan struct{}
	)

	stop := func() {
		if cancel != nil {
			cancel()
			<-done

			cancel = nil
		}
	}
	defer stop()

	for {
		stop()

		select {
		case <-s.wake:
		default:
		}

		s.mu.Lock()
		setup := s.setup
		enabled := s.enabled
		s.mu.Unlock()

		if setup != nil && enabled {
			child, stopChild := context.WithCancel(ctx)
			cancel = stopChild

			done = make(chan struct{})
			go func() { defer close(done); s.serve(child, setup, handler) }()
		}

		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		}
	}
}

func (s *Service) update(setup *Setup, update func(*Status)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.setup == setup {
		update(&s.status)
	}
}

func (s *Service) serve(ctx context.Context, setup *Setup, handler http.Handler) {
	a := Agent{RelayURL: setup.Relay, InstanceID: setup.Instance, RemoteOrigin: setup.RemoteOrigin, Credential: setup.Credential, Handler: handler, StateDirectory: filepath.Join(s.directory, "certificates")}
	a.ConnectionStatus = func(connected bool) {
		s.update(setup, func(status *Status) {
			if connected {
				status.State = "connected"
			} else {
				status.State = "reconnecting"
			}
		})
	}

	a.CertificateStatus = func(cert CertificateStatus) {
		s.update(setup, func(status *Status) {
			status.CertificateReady = cert.Ready
			if cert.Ready {
				status.CertificateExpires = cert.NotAfter.UTC().Format(time.RFC3339)
			}
		})
	}
	for ctx.Err() == nil {
		_ = s.runAgent(ctx, a)
		if ctx.Err() != nil {
			return
		}

		s.update(setup, func(status *Status) { status.State = "unavailable" })

		timer := time.NewTimer(30 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func readSetup(directory string) ([]byte, error) {
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, fmt.Errorf("inspect Connect state: %w", err)
	}

	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("connect state directory must be private")
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("open Connect state: %w", err)
	}
	defer root.Close()

	info, err = root.Lstat("setup.json")
	if err != nil {
		return nil, fmt.Errorf("inspect Connect setup: %w", err)
	}

	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 8192 {
		return nil, errors.New("connect setup must be a private regular file")
	}

	file, err := root.Open("setup.json")
	if err != nil {
		return nil, fmt.Errorf("open Connect setup: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil {
		return nil, fmt.Errorf("read Connect setup: %w", err)
	}

	if len(data) > 8192 {
		return nil, errors.New("connect setup is too large")
	}

	return data, nil
}

func validLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return false
	}

	for _, r := range label {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}

	return true
}
