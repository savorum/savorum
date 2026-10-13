// Package standby drains an idle instance before its host stops it.
package standby

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Monitor counts requests without changing their streaming interfaces.
type Monitor struct {
	mu              sync.Mutex
	active          int
	lastActive      time.Time
	stopping        bool
	leases          map[string]time.Time
	leasePath       string
	lastDeferralLog time.Time
}

// New starts the inactivity period at instance readiness.
func New() *Monitor { return &Monitor{lastActive: time.Now()} }

// Handler excludes readiness probes from activity and closes admission before shutdown.
func (m *Monitor) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			next.ServeHTTP(w, r)
			return
		}

		m.mu.Lock()
		if m.stopping {
			m.mu.Unlock()
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Instance is entering standby", http.StatusServiceUnavailable)

			return
		}

		m.active++

		m.mu.Unlock()
		defer func() {
			m.mu.Lock()
			m.active--
			m.lastActive = time.Now()
			m.mu.Unlock()
		}()

		next.ServeHTTP(w, r)
	})
}

// Run keeps the instance awake while durable or in-memory work remains.
func (m *Monitor) Run(ctx context.Context, timeout time.Duration, busy func(context.Context) (bool, error), stop context.CancelFunc) {
	ticker := time.NewTicker(min(timeout, time.Second))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if m.tryStop(ctx, now, timeout, busy) {
				slog.Info("entering standby after inactivity", "timeout", timeout)
				stop()

				return
			}
		}
	}
}

func (m *Monitor) tryStop(ctx context.Context, now time.Time, timeout time.Duration, busy func(context.Context) (bool, error)) bool {
	m.mu.Lock()

	var deferral []any

	defer func() {
		m.mu.Unlock()

		if len(deferral) != 0 {
			slog.Info("standby deferred", deferral...)
		}
	}()

	idleFor := now.Sub(m.lastActive)
	if idleFor < timeout || m.stopping {
		return false
	}

	var leaseDelay time.Duration
	for _, expiry := range m.leases {
		leaseDelay = max(leaseDelay, expiry.Sub(now))
	}

	if m.active != 0 {
		deferral = m.deferralFields(now, idleFor, leaseDelay, "active_requests", "not_checked", "none")
		return false
	}

	if leaseDelay > 0 {
		deferral = m.deferralFields(now, idleFor, leaseDelay, "maintenance_lease", "not_checked", "none")
		return false
	}
	// Admission stays closed during this read, so a request cannot enqueue work
	// between the final work check and the shutdown decision.
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	pending, err := busy(ctx)
	if err != nil {
		category := "other"

		switch {
		case errors.Is(err, context.DeadlineExceeded):
			category = "timeout"
		case errors.Is(err, context.Canceled):
			category = "canceled"
		}

		deferral = m.deferralFields(now, idleFor, 0, "work_check_error", "error", category)
		m.lastActive = now

		return false
	}

	if pending {
		deferral = m.deferralFields(now, idleFor, 0, "background_work", "pending", "none")
		m.lastActive = now

		return false
	}

	m.stopping = true

	return true
}

func (m *Monitor) deferralFields(now time.Time, idleFor, leaseDelay time.Duration, reason, backgroundWork, errorCategory string) []any {
	since := now.Sub(m.lastDeferralLog)
	if !m.lastDeferralLog.IsZero() && since >= 0 && since < time.Minute {
		return nil
	}

	m.lastDeferralLog = now

	return []any{
		"reason", reason, "idle_for", idleFor.Round(time.Second).String(),
		"active_requests", m.active, "lease_delay", leaseDelay.Round(time.Second).String(),
		"background_work", backgroundWork, "error_category", errorCategory,
	}
}
