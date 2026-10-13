// Package administration provides local instance snapshots, verification,
// migration, journaled restore, filesystem inspection, and operator authority.
package administration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/savorum/savorum/internal/storage/filepolicy"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

// Instance identifies operator-selected paths and their filesystem policy.
// The caller owns lifecycle decisions and excludes other writers for offline work.
type Instance struct {
	Database       string
	Assets         string
	WebhookSecrets string
	Policy         filepolicy.Policy
}

// OpenExisting excludes serving writers, recovers interrupted installation,
// and applies the canonical database migrations before returning the store.
func (i Instance) OpenExisting(ctx context.Context) (*sqlite.Store, func(), error) {
	lock, err := AcquireInstanceLock(i.Database, i.Assets, i.Policy)
	if err != nil {
		return nil, nil, err
	}

	store, closeStore, err := i.OpenOnline(ctx)
	if err != nil {
		_ = lock.Close()
		return nil, nil, err
	}

	return store, func() { closeStore(); _ = lock.Close() }, nil
}

// OpenOnline opens an existing database for maintenance beside a serving process.
// Database transactions coordinate writers; this does not exclude instance replacement.
func (i Instance) OpenOnline(ctx context.Context) (*sqlite.Store, func(), error) {
	info, err := os.Lstat(i.Database)
	if err != nil {
		return nil, nil, fmt.Errorf("inspect existing database: %w", err)
	}

	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("existing database must be a regular file")
	}

	store, err := sqlite.Open(ctx, i.Database, sqlite.WithFilePolicy(i.Policy))
	if err != nil {
		return nil, nil, err
	}

	return store, func() { _ = store.Close() }, nil
}

// Snapshot prepares the same native tree collected by the live backup channel.
// Its destination must be new and private, on the asset filesystem.
func (i Instance) Snapshot(ctx context.Context, output string, now time.Time, maximumBytes int64) ([]IncrementalAsset, error) {
	database, assets, err := absoluteInstancePaths(i.Database, i.Assets)
	if err != nil {
		return nil, err
	}

	i.Database, i.Assets = database, assets

	if maximumBytes <= 0 || maximumBytes == 1<<63-1 {
		return nil, errors.New("snapshot requires a positive byte budget")
	}

	store, closeStore, err := i.OpenExisting(ctx)
	if err != nil {
		return nil, err
	}
	defer closeStore()

	output, err = filepath.Abs(output)
	if err != nil {
		return nil, fmt.Errorf("resolve snapshot directory: %w", err)
	}

	secrets := i.WebhookSecrets
	if secrets == "" {
		secrets = i.Database + ".webhook-secrets"
	}

	secrets, err = filepath.Abs(secrets)
	if err != nil {
		return nil, fmt.Errorf("resolve snapshot keys: %w", err)
	}

	if pathsOverlap(output, i.Database) || pathsOverlap(output, i.Assets) || pathsOverlap(output, secrets) {
		return nil, errors.New("snapshot destination overlaps instance")
	}

	if err := os.Mkdir(output, 0o700); err != nil {
		return nil, fmt.Errorf("create snapshot directory: %w", err)
	}

	return PrepareIncremental(ctx, store, i.Assets, secrets, output, now, maximumBytes)
}
