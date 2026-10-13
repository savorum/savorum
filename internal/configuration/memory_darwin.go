package configuration

import "golang.org/x/sys/unix"

func physicalMemory() uint64 {
	size, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}

	return size
}
