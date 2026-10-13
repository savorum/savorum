package sqlite

import (
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/savorum/savorum/internal/diskspace"
)

const (
	sqliteTransactionGrowthBytes uint64 = 64 << 20
	sqliteEmergencyReserveBytes  uint64 = 256 << 20
)

// sqliteHeadroomBounds assumes cache_spill=OFF and a transaction page ceiling.
// Every page may be dirtied; checkpointing may allocate every new database page.
func sqliteHeadroomBounds(pageCount, pageSize, maxPageCount uint64) (uint64, error) {
	if pageSize < 512 || pageSize > 65536 || pageSize&(pageSize-1) != 0 || maxPageCount < pageCount {
		return 0, fmt.Errorf("invalid SQLite transaction page bounds")
	}

	frameSize := pageSize + 24
	if maxPageCount > (math.MaxUint64-32)/frameSize {
		return 0, fmt.Errorf("SQLite WAL reservation overflows")
	}

	wal := 32 + maxPageCount*frameSize

	growthPages := maxPageCount - pageCount
	if growthPages > (math.MaxUint64-wal)/pageSize {
		return 0, fmt.Errorf("SQLite checkpoint reservation overflows")
	}

	return wal + growthPages*pageSize, nil
}

// beginSQLiteHeadroom reserves WAL frames, checkpoint growth and SQLite sidecars.
// Emergency admission is limited to explicitly authorized space-reducing work.
func beginSQLiteHeadroom(path string, pageCount, pageSize, maxPageCount uint64, emergency bool, reservations ...func(string, uint64, uint64, uint64) (func(), error)) (func(), error) {
	bytes, err := sqliteHeadroomBounds(pageCount, pageSize, maxPageCount)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect SQLite checkpoint file: %w", err)
	}

	logicalBytes := pageCount * pageSize

	physicalBytes := uint64(max(info.Size(), 0))
	if logicalBytes > physicalBytes {
		pending := logicalBytes - physicalBytes
		if bytes > math.MaxUint64-pending {
			return nil, fmt.Errorf("SQLite pending checkpoint reservation overflows")
		}

		bytes += pending
	}

	floor := diskspace.ReserveBytes
	if emergency {
		floor = sqliteEmergencyReserveBytes
	}

	reserve := diskspace.Shared.Reserve
	if len(reservations) > 0 && reservations[0] != nil {
		reserve = reservations[0]
	}

	return reserve(path, bytes, 2, floor)
}

func beginSQLiteVacuumHeadroom(path string, pageCount, pageSize uint64, emergency bool, configuredTemp string) (func(), error) {
	if pageSize == 0 || pageCount > math.MaxUint64/pageSize {
		return nil, fmt.Errorf("invalid SQLite vacuum page bounds")
	}

	floor := diskspace.ReserveBytes
	if emergency {
		floor = sqliteEmergencyReserveBytes
	}

	candidates := []string{path, configuredTemp, os.Getenv("SQLITE_TMPDIR"), os.Getenv("TMPDIR"), os.TempDir(), "/var/tmp", "/usr/tmp", "/tmp", "."}
	devices := map[uint64]bool{}
	releases := []func(){}
	release := func() {
		for _, done := range releases {
			done()
		}
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}

		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) { //nolint:gosec // SQLite temp paths come from its operator configuration and Unix fallback directories.
			continue
		} else if err != nil {
			release()
			return nil, fmt.Errorf("inspect SQLite vacuum workspace: %w", err)
		}

		space, err := diskspace.Inspect(candidate)
		if err != nil {
			release()
			return nil, fmt.Errorf("inspect SQLite vacuum capacity: %w", err)
		}

		if devices[space.Device] {
			continue
		}

		devices[space.Device] = true

		done, err := diskspace.Shared.Reserve(candidate, pageCount*pageSize, 1, floor)
		if err != nil {
			release()
			return nil, fmt.Errorf("reserve SQLite vacuum workspace: %w", err)
		}

		releases = append(releases, done)
	}

	return release, nil
}
