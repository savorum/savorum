//go:build unix

package main

import (
	"fmt"
	"os"
	"syscall"
)

func fileOwner(info os.FileInfo) (int, int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}

	return int(stat.Uid), int(stat.Gid), true
}

func setVolumeOwner(uid, gid int) error {
	if err := syscall.Setgroups([]int{}); err != nil {
		return fmt.Errorf("clear supplementary groups: %w", err)
	}

	if err := syscall.Setgid(gid); err != nil {
		return fmt.Errorf("set the volume group: %w", err)
	}

	if err := syscall.Setuid(uid); err != nil {
		return fmt.Errorf("set the volume user: %w", err)
	}

	return nil
}
