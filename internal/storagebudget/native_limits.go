package storagebudget

import (
	"math"
	"runtime/debug"

	"github.com/savorum/savorum/internal/memoryspace"
)

// NativeMetadataMemory budgets a fraction of process and physical memory.
func NativeMetadataMemory() int64 {
	limit := debug.SetMemoryLimit(-1)

	space, err := memoryspace.Inspect()
	if err == nil && space.TotalBytes > 0 {
		limit = min(limit, int64(min(space.TotalBytes, uint64(math.MaxInt64))))
	}

	return max(int64(64<<10), limit/16)
}

// NativeFiles budgets decoded inventory records in memory.
func NativeFiles() int { return int(min(int64(math.MaxInt), NativeMetadataMemory()/2048)) }

// NativeManifestBytes budgets manifest decoding independently of originals.
func NativeManifestBytes() int64 { return NativeMetadataMemory() / 4 }

// NativeWebhookKeys shares the file inventory budget.
func NativeWebhookKeys() int { return NativeFiles() / 4 }
