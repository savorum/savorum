package memoryspace

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Inspect reports physical memory capacity and available admission headroom.
func Inspect() (Space, error) {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil || total == 0 {
		return Space{}, errors.New("physical memory capacity is unknown")
	}
	// vm_stat reports physical page counters. Admission estimates reclaimable
	// file cache as min(inactive, file-backed); these counters do not identify
	// their intersection. Speculative and purgeable counts are omitted.
	// This snapshot does not guarantee reclamation against external writers.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := exec.CommandContext(ctx, "/usr/bin/vm_stat").Output()
	if err != nil {
		return Space{}, fmt.Errorf("physical memory availability is unknown: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 {
		return Space{}, errors.New("invalid physical memory counters")
	}

	pageSize := unix.Getpagesize()
	if pageSize <= 0 {
		return Space{}, errors.New("invalid physical page size")
	}

	return darwinAvailablePages(total, uint64(pageSize), lines[1:])
}
