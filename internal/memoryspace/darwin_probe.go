package memoryspace

import (
	"errors"
	"strconv"
	"strings"
)

func darwinAvailablePages(total, pageSize uint64, lines []string) (Space, error) {
	if total == 0 || pageSize == 0 {
		return Space{}, errors.New("invalid physical memory counters")
	}

	var free, inactive, fileBacked uint64

	seen := make(map[string]bool, 3)

	for _, line := range lines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		name := parts[0]
		if name != "Pages free" && name != "Pages inactive" && name != "File-backed pages" {
			continue
		}

		if seen[name] {
			return Space{}, errors.New("duplicate physical memory counters")
		}

		seen[name] = true
		value := strings.TrimSuffix(strings.TrimSpace(parts[1]), ".")

		count, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return Space{}, errors.New("invalid physical memory counters")
		}

		switch name {
		case "Pages free":
			free = count
		case "Pages inactive":
			inactive = count
		case "File-backed pages":
			fileBacked = count
		}
	}

	cache := min(inactive, fileBacked)
	if len(seen) != 3 || free > ^uint64(0)-cache {
		return Space{}, errors.New("physical memory availability is unknown")
	}

	pages := free + cache
	if pages > ^uint64(0)/pageSize {
		return Space{}, errors.New("physical memory availability is unknown")
	}

	return Space{TotalBytes: total, AvailableBytes: min(total, pages*pageSize)}, nil
}
