package configuration

import (
	"io/fs"
	"math"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
)

// memoryLimit is the Go memory limit serve sets when GOMEMLIMIT is unset:
// seven tenths of the smaller of the process's control group limit and
// physical memory. Zero leaves the runtime's own setting.
func memoryLimit(environment []string, host func() uint64) int64 {
	for _, entry := range environment {
		if strings.HasPrefix(entry, "GOMEMLIMIT=") {
			return 0
		}
	}

	return int64(min(host()/10*7, math.MaxInt64))
}

func hostMemory() uint64 {
	limit := cgroupMemoryLimit(os.DirFS("/"))
	if physical := physicalMemory(); physical > 0 && (limit == 0 || physical < limit) {
		return physical
	}

	return limit
}

// cgroupMemoryLimit is the smallest memory limit on the process's control
// group or any group above it, for cgroup v2 and v1. Zero means none.
func cgroupMemoryLimit(root fs.FS) uint64 {
	membership, err := fs.ReadFile(root, "proc/self/cgroup")
	if err != nil {
		return 0
	}

	var smallest uint64

	for line := range strings.SplitSeq(string(membership), "\n") {
		fields := strings.SplitN(line, ":", 3)
		if len(fields) != 3 {
			continue
		}

		var base, file string

		switch {
		case fields[0] == "0" && fields[1] == "":
			base, file = "sys/fs/cgroup", "memory.max"
		case slices.Contains(strings.Split(fields[1], ","), "memory"):
			base, file = "sys/fs/cgroup/memory", "memory.limit_in_bytes"
		default:
			continue
		}

		for group := path.Clean(fields[2]); ; group = path.Dir(group) {
			contents, err := fs.ReadFile(root, path.Join(base, group, file))
			if value, parseErr := strconv.ParseUint(strings.TrimSpace(string(contents)), 10, 64); err == nil && parseErr == nil &&
				value < 1<<62 && (smallest == 0 || value < smallest) {
				// cgroup v1 reports an unlimited group as a value near 2^63.
				smallest = value
			}

			if group == "/" || group == "." {
				break
			}
		}
	}

	return smallest
}
