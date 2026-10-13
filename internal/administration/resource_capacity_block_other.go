//go:build !linux

package administration

import "syscall"

func filesystemBlockSize(stats syscall.Statfs_t) int64 {
	return int64(stats.Bsize)
}
