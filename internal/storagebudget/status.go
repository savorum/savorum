package storagebudget

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/savorum/savorum/internal/diskspace"
)

// Status is a fresh observation, never an admission decision. Unknown values
// are omitted so a failed probe cannot be mistaken for zero usage.
type Status struct {
	State           string  `json:"state" enum:"normal,warning,limited,unknown"`
	UsedBytes       *int64  `json:"used_bytes,omitempty"`
	LimitBytes      *int64  `json:"limit_bytes,omitempty"`
	AvailableBytes  *uint64 `json:"available_bytes,omitempty"`
	AvailableInodes *uint64 `json:"available_inodes,omitempty"`
}

// Status bounds inventory work and shares the mutation lease for a coherent
// combined database/original observation. It starts no background work.
func (c *Coordinator) Status(ctx context.Context) Status {
	status := Status{State: "unknown"}
	if c == nil {
		return status
	}

	status.LimitBytes = &c.limit

	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	held, release, err := c.Acquire(ctx)
	if err != nil {
		return status
	}
	defer release()

	inspect := c.statusInspect
	if inspect == nil {
		inspect = diskspace.Inspect
	}

	space, err := inspect(c.root)
	if err != nil {
		return status
	}

	status.AvailableBytes = &space.AvailableBytes
	if space.TotalInodes != 0 {
		status.AvailableInodes = &space.AvailableInodes
	}

	original, files, err := c.statusOriginals(held)
	if err != nil {
		return status
	}

	c.mu.RLock()
	databaseUsage := c.database
	c.mu.RUnlock()

	if databaseUsage == nil {
		return status
	}

	database, err := databaseUsage(held)
	if err != nil || database < 0 || database > math.MaxInt64-original {
		return status
	}

	used := database + original
	status.UsedBytes = &used

	c.mu.Lock()
	if used >= c.limit-c.limit/4 {
		c.warningActive = true
	} else if used < c.limit*7/10 {
		c.warningActive = false
	}

	physicalState, physicalWarning := PhysicalState(space, diskspace.Footprint{DatabaseBytes: uint64(database), OriginalBytes: uint64(original), OriginalFiles: files}, c.physicalWarningActive) //nolint:gosec // The inventory rejects negative sizes and overflow.
	c.physicalWarningActive = physicalWarning
	warning := c.warningActive || physicalWarning
	c.mu.Unlock()

	status.State = "normal"

	switch {
	case used >= c.limit-ManifestAllowance || physicalState == "limited":
		status.State = "limited"
	case physicalState == "unknown":
		status.State = "unknown"
	case warning:
		status.State = "warning"
	}

	return status
}

func (c *Coordinator) statusOriginals(ctx context.Context) (int64, uint64, error) {
	var (
		total int64
		files uint64
	)

	entries := 0

	roots := append([]string{filepath.Join(c.root, "sha256"), filepath.Join(c.root, ".quarantine")}, c.extraRoots...)
	for _, root := range roots {
		err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}

			if err != nil {
				return fmt.Errorf("inspect retained storage: %w", err)
			}

			if err := ctx.Err(); err != nil {
				return fmt.Errorf("inspect retained storage: %w", err)
			}

			entries++
			if entries > 20000 {
				return errors.New("storage observation inventory bound reached")
			}

			if entry.IsDir() {
				return nil
			}

			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("inspect retained storage: %w", err)
			}

			if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > math.MaxInt64-total {
				return errors.New("storage observation unavailable")
			}

			total += info.Size()
			files++

			return nil
		})
		if err != nil {
			return 0, 0, fmt.Errorf("scan retained storage: %w", err)
		}
	}

	return total, files, nil
}
