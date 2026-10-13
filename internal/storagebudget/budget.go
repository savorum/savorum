// Package storagebudget serializes instance database and original-file growth.
package storagebudget

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/savorum/savorum/internal/diskspace"
)

// Backup byte bounds include the instance data ceiling and metadata allowance.
const (
	ManifestAllowance int64 = 32 << 20
)

// ErrCapacity reports a durable or native-format admission rejection.
var ErrCapacity = errors.New("instance durable storage capacity exhausted")

// Coordinator owns the shared database and retained-file writer fence.
type Coordinator struct {
	gate                  chan struct{}
	root                  string
	extraRoots            []string
	limit                 int64
	mu                    sync.RWMutex
	database              func(context.Context) (int64, error)
	warningActive         bool
	physicalWarningActive bool
	statusInspect         func(string) (diskspace.Space, error)
}
type (
	leaseKey struct{}
	lease    struct {
		owner *Coordinator
		mu    sync.Mutex
		refs  int
	}
)

// New constructs an independent instance admission coordinator.
func New(root string, limit int64) *Coordinator {
	return &Coordinator{gate: make(chan struct{}, 1), root: root, limit: limit}
}

// AddRetainedRoot counts backup-retained auxiliary files in the same ceiling.
func (c *Coordinator) AddRetainedRoot(root string) { c.extraRoots = append(c.extraRoots, root) }

// SetDatabaseUsage installs the SQLite allocated-page reader.
func (c *Coordinator) SetDatabaseUsage(f func(context.Context) (int64, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.database = f
}

// Acquire marks a lease in ctx so nested database and file operations share it.
func (c *Coordinator) Acquire(ctx context.Context) (context.Context, func(), error) {
	if c == nil {
		return ctx, func() {}, nil
	}

	if held, ok := ctx.Value(leaseKey{}).(*lease); ok && held.owner == c {
		held.mu.Lock()
		if held.refs > 0 {
			held.refs++
			held.mu.Unlock()

			return ctx, held.release, nil
		}
		held.mu.Unlock()
	}

	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx, nil, ctx.Err()
	}

	held := &lease{owner: c, refs: 1}

	return context.WithValue(ctx, leaseKey{}, held), held.release, nil
}

func (l *lease) release() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.refs--
	if l.refs == 0 {
		<-l.owner.gate
	}
}

// Originals counts retained files, including unregistered originals and quarantine.
func (c *Coordinator) Originals(ctx context.Context) (int64, error) {
	if c == nil {
		return 0, nil
	}

	var total int64

	files := 0
	roots := []string{filepath.Join(c.root, "sha256"), filepath.Join(c.root, ".quarantine")}

	roots = append(roots, c.extraRoots...)
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}

			if err != nil {
				return err
			}

			if err = ctx.Err(); err != nil {
				return err
			}

			if entry.IsDir() {
				return nil
			}

			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("inspect retained file: %w", err)
			}

			if !info.Mode().IsRegular() {
				return fmt.Errorf("original is not a regular file: %s", path)
			}

			files++
			if files > NativeFiles()-16 {
				return ErrCapacity
			}

			if info.Size() > math.MaxInt64-total {
				return ErrCapacity
			}

			total += info.Size()

			return nil
		})
		if err != nil {
			return 0, fmt.Errorf("scan retained files: %w", err)
		}
	}

	return total, nil
}

// RemainingDatabase excludes retained files and backup metadata from the ceiling.
func (c *Coordinator) RemainingDatabase(ctx context.Context) (int64, error) {
	original, err := c.Originals(ctx)
	if err != nil {
		return 0, err
	}

	return c.limit - ManifestAllowance - original, nil
}

// AdmitOriginal runs while the caller holds the shared lease.
func (c *Coordinator) AdmitOriginal(ctx context.Context, delta int64) error {
	if c == nil || delta <= 0 {
		return nil
	}

	remaining, err := c.RemainingDatabase(ctx)
	if err != nil {
		return err
	}

	c.mu.RLock()
	f := c.database
	c.mu.RUnlock()

	if f == nil {
		return errors.New("database usage reader is not configured")
	}

	database, err := f(ctx)
	if err != nil {
		return err
	}

	if database > remaining-delta {
		return ErrCapacity
	}

	return nil
}

// WithLease preserves a connection's lease while respecting the operation context.
func WithLease(ctx, held context.Context) context.Context {
	if value := held.Value(leaseKey{}); value != nil {
		return context.WithValue(ctx, leaseKey{}, value)
	}

	return ctx
}
