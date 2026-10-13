//go:build linux

package administration

import "syscall"

// The block-size fields are int32 on 32-bit Linux, so the conversions are required there.
func filesystemBlockSize(stats syscall.Statfs_t) int64 {
	if stats.Frsize > 0 {
		return int64(stats.Frsize) //nolint:unconvert // int32 on 32-bit architectures.
	}

	return int64(stats.Bsize) //nolint:unconvert // int32 on 32-bit architectures.
}
