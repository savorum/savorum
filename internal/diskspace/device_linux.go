package diskspace

import "syscall"

func filesystemDevice(stat *syscall.Stat_t) uint64 { return stat.Dev }
