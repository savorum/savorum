package standby

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMaintenancePersistsExpiresAndFencesRelease(t *testing.T) {
	// A short path also fits the Unix socket limit on macOS.
	dir, err := os.MkdirTemp("", "sv-lease-") //nolint:usetesting // macOS Unix socket paths must stay below 104 bytes.
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	socket, state := filepath.Join(dir, "control.sock"), filepath.Join(dir, "leases")
	m := New()

	closeChannel, err := m.Maintenance(t.Context(), socket, state)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()

	change := LeaseChange{ID: "operation_12345678901234567890", ExpiresAt: now.Add(3 * time.Minute)}
	if err := ChangeMaintenanceLease(t.Context(), socket, change); err != nil {
		t.Fatal(err)
	}

	if err := ChangeMaintenanceLease(t.Context(), socket, change); err != nil {
		t.Fatalf("lost response retry: %v", err)
	}

	info, err := os.Stat(socket)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket permissions: %v %v", info, err)
	}

	renewed := change
	renewed.Expected = change.ExpiresAt

	renewed.ExpiresAt = now.Add(5 * time.Minute)
	if err := ChangeMaintenanceLease(t.Context(), socket, renewed); err != nil {
		t.Fatal(err)
	}

	if got := m.changeLease(LeaseChange{ID: change.ID, Release: true, Expected: change.ExpiresAt}, now); got != http.StatusConflict {
		t.Fatalf("stale release: %d", got)
	}

	if err := closeChannel(); err != nil {
		t.Fatal(err)
	}

	m = New()

	closeChannel, err = m.Maintenance(t.Context(), socket, state)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = closeChannel() })

	idle := func(context.Context) (bool, error) { return false, nil }
	if m.tryStop(t.Context(), now.Add(2*time.Minute), time.Minute, idle) {
		t.Fatal("restart lost active maintenance lease")
	}

	if !m.tryStop(t.Context(), now.Add(6*time.Minute), time.Minute, idle) {
		t.Fatal("expired controller lease prevented standby")
	}

	if got := m.changeLease(LeaseChange{ID: "new_operation_1234567890123456", ExpiresAt: now.Add(7 * time.Minute)}, now.Add(6*time.Minute)); got != http.StatusServiceUnavailable {
		t.Fatalf("admitted maintenance after shutdown: %d", got)
	}
}

func TestMaintenanceAcquisitionCannotRaceFinalWorkCheck(t *testing.T) {
	m := New()
	m.leasePath = filepath.Join(t.TempDir(), "leases")
	m.leases = map[string]time.Time{}
	now := time.Now()
	m.lastActive = now.Add(-time.Hour)

	entered, release, stopped := make(chan struct{}), make(chan struct{}), make(chan bool, 1)
	go func() {
		stopped <- m.tryStop(t.Context(), now, time.Minute, func(context.Context) (bool, error) { close(entered); <-release; return false, nil })
	}()

	<-entered

	result := make(chan int, 1)
	go func() {
		result <- m.changeLease(LeaseChange{ID: "operation_12345678901234567890", ExpiresAt: now.Add(time.Minute)}, now)
	}()

	close(release)

	if !<-stopped {
		t.Fatal("idle stop failed")
	}

	if status := <-result; status != http.StatusServiceUnavailable {
		t.Fatalf("lease crossed shutdown boundary: %d", status)
	}
}

func TestMaintenanceLeaseBoundsAndRelease(t *testing.T) {
	m := New()
	m.leasePath = filepath.Join(t.TempDir(), "leases")
	m.leases = map[string]time.Time{}
	now := time.Now()

	id := "operation_12345678901234567890"
	for _, expiry := range []time.Time{now, now.Add(time.Hour)} {
		if got := m.changeLease(LeaseChange{ID: id, ExpiresAt: expiry}, now); got != http.StatusBadRequest {
			t.Fatalf("invalid expiry accepted: %d", got)
		}
	}

	expiry := now.Add(time.Minute)
	if got := m.changeLease(LeaseChange{ID: id, ExpiresAt: expiry}, now); got != http.StatusNoContent {
		t.Fatal(got)
	}

	if got := m.changeLease(LeaseChange{ID: id, Release: true, Expected: expiry}, now); got != http.StatusNoContent {
		t.Fatal(got)
	}

	if len(m.leases) != 0 {
		t.Fatal("released lease kept awake")
	}

	if !m.tryStop(t.Context(), now.Add(2*time.Minute), time.Minute, func(context.Context) (bool, error) { return false, nil }) {
		t.Fatal("released maintenance prevented shutdown")
	}
}

func TestMaintenanceOperatorCountsActivity(t *testing.T) {
	dir, err := os.MkdirTemp("", "sv-member-") //nolint:usetesting // Unix socket length limit.
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	monitor := New()
	socket := filepath.Join(dir, "control.sock")
	observed := false
	operator := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		monitor.mu.Lock()
		observed = monitor.active == 1 && !monitor.stopping
		monitor.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	closeChannel, err := monitor.Maintenance(t.Context(), socket, filepath.Join(dir, "leases"), operator)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = closeChannel() }()

	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()

	client := &http.Client{Transport: transport, Timeout: time.Second}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://localhost/member", nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	_ = response.Body.Close()
	if !observed || response.StatusCode != http.StatusNoContent {
		t.Fatal("operator request did not protect activity")
	}

	monitor.mu.Lock()
	monitor.stopping = true
	monitor.mu.Unlock()

	request, err = http.NewRequestWithContext(t.Context(), http.MethodPost, "http://localhost/member", nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	_ = response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatal("standby admitted member mutation")
	}
}
