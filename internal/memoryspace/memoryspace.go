// Package memoryspace admits expensive operations against physical memory.
package memoryspace

import (
	"errors"
	"fmt"
	"sync"
)

// ErrResourceMemory identifies a recoverable physical-memory refusal.
var ErrResourceMemory = errors.New("operation memory capacity is exhausted")

// Space describes physical capacity and an availability snapshot, never swap.
// Darwin availability includes a conservative reclaimable file-cache estimate.
type Space struct{ TotalBytes, AvailableBytes uint64 }

// Manager coordinates conservative in-flight reservations within this process.
// External processes remain subject to the operating system's own accounting.
type Manager struct {
	mu       sync.Mutex
	reserved uint64
	inspect  func() (Space, error)
}

// NewManager creates a manager using the system memory probe.
func NewManager() *Manager { return NewManagerWithProbe(Inspect) }

// NewManagerWithProbe supports deterministic capacity probes and qualification.
func NewManagerWithProbe(probe func() (Space, error)) *Manager { return &Manager{inspect: probe} }

// Shared coordinates process-wide memory reservations.
var Shared = NewManager()

// Reserve holds bytes plus a safety floor until its idempotent release.
func (m *Manager) Reserve(bytes, floor uint64) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, err := m.inspect()
	if err != nil {
		return nil, fmt.Errorf("%w: memory capacity unavailable: %w", ErrResourceMemory, err)
	}

	if s.TotalBytes == 0 || s.AvailableBytes > s.TotalBytes || bytes > ^uint64(0)-floor || m.reserved > ^uint64(0)-bytes-floor || bytes+floor > s.TotalBytes || m.reserved+bytes+floor > s.AvailableBytes {
		return nil, fmt.Errorf("%w: need %d bytes plus %d safety bytes; available %d of %d physical bytes (%d reserved)", ErrResourceMemory, bytes, floor, s.AvailableBytes, s.TotalBytes, m.reserved)
	}

	m.reserved += bytes

	var once sync.Once

	return func() { once.Do(func() { m.mu.Lock(); defer m.mu.Unlock(); m.reserved -= bytes }) }, nil
}
