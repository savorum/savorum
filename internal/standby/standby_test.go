package standby

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIdleAdmissionWaitsForRequestsAndBackgroundWork(t *testing.T) {
	m := New()
	m.lastActive = time.Now().Add(-time.Hour)
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})

	handler := m.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(entered); <-release }))
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/recipes", nil))
		close(finished)
	}()

	<-entered

	available := func(context.Context) (bool, error) { return false, nil }
	if m.tryStop(context.Background(), time.Now(), time.Minute, available) {
		t.Fatal("stopped an active request")
	}

	close(release)
	<-finished

	if m.tryStop(context.Background(), time.Now(), time.Minute, available) {
		t.Fatal("request completion did not renew inactivity")
	}

	later := time.Now().Add(2 * time.Minute)
	if m.tryStop(context.Background(), later, time.Minute, func(context.Context) (bool, error) { return true, nil }) {
		t.Fatal("stopped with background work")
	}

	later = later.Add(2 * time.Minute)
	if m.tryStop(context.Background(), later, time.Minute, func(context.Context) (bool, error) { return false, errors.New("database unavailable") }) {
		t.Fatal("an unavailable work check allowed shutdown")
	}

	if !m.tryStop(context.Background(), later.Add(2*time.Minute), time.Minute, available) {
		t.Fatal("idle instance did not stop")
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/recipes", nil))

	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") == "" {
		t.Fatalf("shutdown admission: %d %v", response.Code, response.Header())
	}
}

func TestHealthChecksDoNotRenewInactivity(t *testing.T) {
	m := New()
	m.lastActive = time.Now().Add(-time.Hour)
	m.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/health", nil))

	if !m.tryStop(context.Background(), time.Now(), time.Minute, func(context.Context) (bool, error) { return false, nil }) {
		t.Fatal("health probes prevented standby")
	}
}

func TestStandbyDeferralDiagnostics(t *testing.T) {
	var output bytes.Buffer

	previous := slog.Default()

	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	start := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	m := New()
	m.lastActive = start
	m.active = 1
	m.leases = map[string]time.Time{"private-lease-id": start.Add(8 * time.Minute)}
	checks := 0

	check := func(context.Context) (bool, error) {
		checks++
		return false, nil
	}
	if m.tryStop(t.Context(), start.Add(4*time.Minute), 5*time.Minute, check) {
		t.Fatal("stopped before idle timeout")
	}

	for _, elapsed := range []time.Duration{5 * time.Minute, 5*time.Minute + 30*time.Second, 6 * time.Minute} {
		if m.tryStop(t.Context(), start.Add(elapsed), 5*time.Minute, check) {
			t.Fatal("stopped with active request")
		}
	}

	if checks != 0 {
		t.Fatal("checked work before active request and maintenance lease cleared")
	}

	m.active = 0
	if m.tryStop(t.Context(), start.Add(7*time.Minute), 5*time.Minute, check) {
		t.Fatal("stopped with maintenance lease")
	}

	if m.tryStop(t.Context(), start.Add(8*time.Minute+time.Second), 5*time.Minute, func(context.Context) (bool, error) {
		checks++
		return true, nil
	}) {
		t.Fatal("stopped with background work")
	}

	if m.tryStop(t.Context(), start.Add(10*time.Minute), 5*time.Minute, check) {
		t.Fatal("background-work deferral changed the idle timer")
	}

	if m.tryStop(t.Context(), start.Add(13*time.Minute+time.Second), 5*time.Minute, func(context.Context) (bool, error) {
		checks++
		return false, fmt.Errorf("private detail: %w", context.DeadlineExceeded)
	}) {
		t.Fatal("stopped after failed work check")
	}

	if !m.tryStop(t.Context(), start.Add(18*time.Minute+time.Second), 5*time.Minute, check) {
		t.Fatal("did not stop after work cleared")
	}

	if checks != 3 {
		t.Fatalf("work checks = %d, want 3", checks)
	}

	if strings.Contains(output.String(), "private") {
		t.Fatal("diagnostics disclosed private detail")
	}

	lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n"))
	if len(lines) != 5 {
		t.Fatalf("deferral logs = %d, want 5: %s", len(lines), output.String())
	}

	for index, want := range []struct {
		reason, idle, lease, work, category string
		active                              float64
	}{
		{"active_requests", "5m0s", "3m0s", "not_checked", "none", 1},
		{"active_requests", "6m0s", "2m0s", "not_checked", "none", 1},
		{"maintenance_lease", "7m0s", "1m0s", "not_checked", "none", 0},
		{"background_work", "8m1s", "0s", "pending", "none", 0},
		{"work_check_error", "5m0s", "0s", "error", "timeout", 0},
	} {
		var record map[string]any
		if err := json.Unmarshal(lines[index], &record); err != nil {
			t.Fatal(err)
		}

		if record["msg"] != "standby deferred" || record["reason"] != want.reason || record["idle_for"] != want.idle ||
			record["lease_delay"] != want.lease || record["background_work"] != want.work ||
			record["error_category"] != want.category || record["active_requests"] != want.active {
			t.Fatalf("deferral %d = %v", index, record)
		}
	}
}
