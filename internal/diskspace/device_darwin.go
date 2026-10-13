package diskspace

import "syscall"

func filesystemDevice(stat *syscall.Stat_t) uint64 {
	// Device identifiers retain all 32 bits of the signed platform representation.
	return uint64(int64(stat.Dev) & 0xffffffff)
}
