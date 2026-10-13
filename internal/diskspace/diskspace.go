// Package diskspace coordinates temporary writes on shared filesystems.
package diskspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// Safety reserves leave room for filesystem metadata and recovery work.
const (
	ReserveBytes  uint64 = 512 << 20
	ReserveInodes uint64 = 32
)

// ErrCapacity reports an exhausted byte or inode safety reserve.
var ErrCapacity = errors.New("filesystem safety capacity is exhausted")

// Space describes capacity available to the current process.
type Space struct {
	AvailableBytes  uint64 `json:"available_bytes"`
	TotalBytes      uint64 `json:"total_bytes"`
	AvailableInodes uint64 `json:"available_inodes"`
	TotalInodes     uint64 `json:"total_inodes"`
	Device          uint64 `json:"-"`
}

// Inspect resolves missing paths to their closest existing ancestor.
func Inspect(path string) (Space, error) {
	for {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			next := filepath.Dir(path)
			if next == path {
				return Space{}, fmt.Errorf("inspect filesystem path: %w", err)
			}

			path = next

			continue
		}

		if err != nil {
			return Space{}, fmt.Errorf("inspect filesystem path: %w", err)
		}

		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return Space{}, errors.New("filesystem device identity is unavailable")
		}

		var fs syscall.Statfs_t
		if err := syscall.Statfs(path, &fs); err != nil {
			return Space{}, fmt.Errorf("inspect filesystem capacity: %w", err)
		}

		if fs.Bsize <= 0 {
			return Space{}, errors.New("filesystem block size is invalid")
		}

		block := uint64(fs.Bsize)
		if fs.Blocks > ^uint64(0)/block || fs.Bavail > ^uint64(0)/block {
			return Space{}, errors.New("filesystem capacity overflows accounting")
		}

		return Space{AvailableBytes: fs.Bavail * block, TotalBytes: fs.Blocks * block, AvailableInodes: fs.Ffree, TotalInodes: fs.Files, Device: filesystemDevice(stat)}, nil
	}
}

type amount struct{ bytes, inodes uint64 }

// Manager holds in-flight upper bounds for each device. External writers remain
// subject to the operating system's write errors.
type Manager struct {
	mu       sync.Mutex
	reserved map[uint64]amount
	inspect  func(string) (Space, error)
}

// NewManager creates an independent reservation coordinator.
func NewManager() *Manager { return NewManagerWithInspector(Inspect) }

// NewManagerWithInspector observes capacity through the supplied filesystem probe.
func NewManagerWithInspector(inspect func(string) (Space, error)) *Manager {
	return &Manager{reserved: make(map[uint64]amount), inspect: inspect}
}

// Shared coordinates all process-local filesystem writes.
var Shared = NewManager()

// Reserve holds a conservative byte and inode upper bound until release.
func (m *Manager) Reserve(path string, bytes, inodes, floor uint64) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	space, err := m.inspect(path)
	if err != nil {
		return nil, err
	}

	used := m.reserved[space.Device]
	if !fits(space.AvailableBytes, used.bytes, bytes, floor) || (space.TotalInodes != 0 && !fits(space.AvailableInodes, used.inodes, inodes, ReserveInodes)) {
		return nil, fmt.Errorf("%w: need %d bytes and %d inodes plus reserve", ErrCapacity, bytes, inodes)
	}

	m.reserved[space.Device] = amount{bytes: used.bytes + bytes, inodes: used.inodes + inodes}

	var once sync.Once

	return func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()

			current := m.reserved[space.Device]
			current.bytes -= bytes

			current.inodes -= inodes
			if current == (amount{}) {
				delete(m.reserved, space.Device)
			} else {
				m.reserved[space.Device] = current
			}
		})
	}, nil
}

func fits(available, reserved, needed, floor uint64) bool {
	return available >= floor && reserved <= available-floor && needed <= available-floor-reserved
}
