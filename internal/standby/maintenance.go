package standby

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const maximumLease = 15 * time.Minute

// LeaseChange uses absolute times so a retry cannot extend a lease accidentally.
type LeaseChange struct {
	ID        string    `json:"id"`
	ExpiresAt time.Time `json:"expires_at"`
	Expected  time.Time `json:"expected,omitzero"`
	Release   bool      `json:"release,omitempty"`
}

var leaseID = regexp.MustCompile(`^[a-zA-Z0-9_-]{24,128}$`)

// Maintenance serves a local operator channel protected by socket filesystem permissions.
// Lease state lives beside the database and survives a serving-process restart.
func (m *Monitor) Maintenance(ctx context.Context, socket, statePath string, operators ...http.Handler) (func() error, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(statePath) //nolint:gosec // Operator-owned sidecar path.
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read maintenance leases: %w", err)
	}

	leases := map[string]time.Time{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &leases); err != nil {
			return nil, fmt.Errorf("decode maintenance leases: %w", err)
		}

		for id, expiry := range leases {
			if !leaseID.MatchString(id) || expiry.After(time.Now().Add(maximumLease)) {
				return nil, errors.New("invalid maintenance lease state")
			}
		}
	}

	if len(leases) > 64 {
		return nil, errors.New("too many persisted maintenance leases")
	}

	m.leases = leases
	m.leasePath = statePath

	if !filepath.IsAbs(socket) {
		return nil, errors.New("maintenance socket must be an absolute path")
	}

	directory := filepath.Dir(socket)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create maintenance socket directory: %w", err)
	}

	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("maintenance socket requires a private directory (0700)")
	}
	// The instance lock is held by the caller before stale socket removal.
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("maintenance path is not a socket")
		}

		if err := os.Remove(socket); err != nil {
			return nil, fmt.Errorf("remove stale maintenance socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect maintenance socket: %w", err)
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "unix", socket)
	if err != nil {
		return nil, fmt.Errorf("listen on maintenance socket: %w", err)
	}

	if err := os.Chmod(socket, 0o600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("protect maintenance socket: %w", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/lease" {
			m.maintenanceRequest(w, r)
			return
		}

		if len(operators) > 0 {
			operators[0].ServeHTTP(w, r)
			return
		}

		http.NotFound(w, r)
	})

	server := &http.Server{Handler: m.Handler(handler), ReadHeaderTimeout: time.Second, ReadTimeout: 6 * time.Second, WriteTimeout: 6 * time.Second, MaxHeaderBytes: 4096}
	go func() { _ = server.Serve(listener) }()

	return func() error {
		if err := server.Close(); err != nil {
			return fmt.Errorf("close maintenance channel: %w", err)
		}

		return nil
	}, nil
}

func (m *Monitor) maintenanceRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/lease" {
		http.NotFound(w, r)
		return
	}

	var change LeaseChange

	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	d.DisallowUnknownFields()

	var extra any
	if d.Decode(&change) != nil || !errors.Is(d.Decode(&extra), io.EOF) {
		http.Error(w, "Invalid lease", http.StatusBadRequest)
		return
	}

	if status := m.changeLease(change, time.Now()); status != http.StatusNoContent {
		http.Error(w, http.StatusText(status), status)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (m *Monitor) changeLease(change LeaseChange, now time.Time) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopping {
		return http.StatusServiceUnavailable
	}

	if m.leasePath == "" || !leaseID.MatchString(change.ID) {
		return http.StatusBadRequest
	}

	previous, exists := m.leases[change.ID]
	if change.Release {
		if !exists {
			return http.StatusNoContent
		}

		if !previous.Equal(change.Expected) {
			return http.StatusConflict
		}
	} else {
		if !change.ExpiresAt.After(now) || change.ExpiresAt.After(now.Add(maximumLease)) {
			return http.StatusBadRequest
		}

		if exists && previous.Equal(change.ExpiresAt) {
			return http.StatusNoContent
		}

		if exists && (!previous.After(now) || !previous.Equal(change.Expected)) {
			return http.StatusConflict
		}

		if !exists && !change.Expected.IsZero() {
			return http.StatusConflict
		}
	}

	next := map[string]time.Time{}

	for id, expiry := range m.leases {
		if expiry.After(now) {
			next[id] = expiry
		}
	}

	if change.Release {
		delete(next, change.ID)
	} else {
		next[change.ID] = change.ExpiresAt
	}

	if len(next) > 64 {
		return http.StatusTooManyRequests
	}

	if err := writeLeases(m.leasePath, next); err != nil {
		return http.StatusInternalServerError
	}

	m.leases = next
	m.lastActive = now

	return http.StatusNoContent
}

func writeLeases(path string, leases map[string]time.Time) error {
	data, err := json.Marshal(leases)
	if err != nil {
		return fmt.Errorf("encode leases: %w", err)
	}

	file, err := os.CreateTemp(filepath.Dir(path), ".maintenance-*")
	if err != nil {
		return fmt.Errorf("create lease state: %w", err)
	}

	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()

	if _, err = file.Write(data); err != nil {
		return fmt.Errorf("write lease state: %w", err)
	}

	if err = file.Sync(); err != nil {
		return fmt.Errorf("sync lease state: %w", err)
	}

	if err = file.Close(); err != nil {
		return fmt.Errorf("close lease state: %w", err)
	}

	if err = os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace lease state: %w", err)
	}

	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open lease directory: %w", err)
	}
	defer directory.Close()

	if err = directory.Sync(); err != nil {
		return fmt.Errorf("sync lease directory: %w", err)
	}

	return nil
}

// ChangeMaintenanceLease calls the local channel. The caller must stop external
// work before expiry or whenever renewal is not acknowledged.
func ChangeMaintenanceLease(ctx context.Context, socket string, change LeaseChange) error {
	data, err := json.Marshal(change)
	if err != nil {
		return fmt.Errorf("encode maintenance request: %w", err)
	}

	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
		if err != nil {
			return nil, fmt.Errorf("dial maintenance socket: %w", err)
		}

		return conn, nil
	}}
	defer transport.CloseIdleConnections()

	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/lease", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("construct maintenance request: %w", err)
	}

	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("maintenance request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("maintenance lease rejected (HTTP %d)", response.StatusCode)
	}

	return nil
}
