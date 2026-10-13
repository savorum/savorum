package memoryspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Resolve controller mount roots as well as namespace-relative membership.
func inspectLinux(read func(string) ([]byte, error)) (Space, error) {
	data, err := read("/proc/meminfo")
	if err != nil {
		return Space{}, err
	}

	s, err := physicalMemory(data)
	if err != nil {
		return Space{}, err
	}

	membership, err := read("/proc/self/cgroup")
	if err != nil {
		return Space{}, err
	}

	if strings.TrimSpace(string(membership)) == "" {
		return Space{}, errors.New("memory controller membership is unknown")
	}

	mountData, err := read("/proc/self/mountinfo")
	if err != nil {
		return Space{}, err
	}

	for line := range strings.SplitSeq(string(membership), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}

		v2 := parts[0] == "0" && parts[1] == ""
		if !v2 && !contains(parts[1], "memory") {
			continue
		}

		if err := constrainMembership(&s, parts[2], string(mountData), v2, read); err != nil {
			return Space{}, err
		}
	}

	return s, nil
}

func physicalMemory(data []byte) (Space, error) {
	var s Space

	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || f[2] != "kB" {
			continue
		}

		if f[0] != "MemTotal:" && f[0] != "MemAvailable:" {
			continue
		}

		n, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil || n > ^uint64(0)/1024 {
			return Space{}, errors.New("invalid physical memory accounting")
		}

		if f[0] == "MemTotal:" {
			s.TotalBytes = n * 1024
		} else {
			s.AvailableBytes = n * 1024
		}
	}

	if s.TotalBytes == 0 || s.AvailableBytes == 0 || s.AvailableBytes > s.TotalBytes {
		return Space{}, errors.New("physical memory availability is unknown")
	}

	return s, nil
}

func constrainMembership(s *Space, memberPath, mountData string, v2 bool, read func(string) ([]byte, error)) error {
	found := false

	for mount := range strings.SplitSeq(mountData, "\n") {
		halves := strings.SplitN(mount, " - ", 2)
		if len(halves) != 2 {
			continue
		}

		left, right := strings.Fields(halves[0]), strings.Fields(halves[1])
		if len(left) < 5 || len(right) < 3 {
			continue
		}

		if (v2 && right[0] != "cgroup2") || (!v2 && (right[0] != "cgroup" || !contains(right[2], "memory"))) {
			continue
		}

		root, point := unescape(left[3]), unescape(left[4])

		member := filepath.Clean(memberPath)
		if !filepath.IsAbs(member) {
			return errors.New("invalid memory controller membership")
		}

		relative := strings.TrimPrefix(member, root)
		if root != "/" && member != root && !strings.HasPrefix(member, root+"/") {
			relative = member
		} // cgroup namespace root

		path := filepath.Join(point, relative)
		found = true

		if err := constrainHierarchy(s, path, point, root, v2, read); err != nil {
			return err
		}
	}

	if !found {
		return errors.New("memory controller mount is unavailable")
	}

	return nil
}

func constrainHierarchy(s *Space, path, point, root string, v2 bool, read func(string) ([]byte, error)) error {
	// Ancestor limits apply too; child controllers can themselves be unlimited.
	for {
		limitName, currentName := "memory.max", "memory.current"
		if !v2 {
			limitName, currentName = "memory.limit_in_bytes", "memory.usage_in_bytes"
		}

		raw, err := read(filepath.Join(path, limitName))
		if err != nil {
			// The actual v2 hierarchy root is exempt from memory
			// limits and normally has no memory.max. A finite limit
			// at a namespaced mount root must still be enforced.
			if v2 && path == point && root == "/" && errors.Is(err, os.ErrNotExist) {
				break
			}

			return fmt.Errorf("read memory controller limit: %w", err)
		}

		if err := constrainLimit(s, path, currentName, strings.TrimSpace(string(raw)), v2, read); err != nil {
			return err
		}

		if path == point {
			break
		}

		parent := filepath.Dir(path)
		if parent == path || (parent != point && !strings.HasPrefix(parent, point+"/")) {
			return errors.New("memory controller path escapes mount")
		}

		path = parent
	}

	return nil
}

func constrainLimit(s *Space, path, currentName, value string, v2 bool, read func(string) ([]byte, error)) error {
	if value != "max" {
		limit, err := strconv.ParseUint(value, 10, 64)
		if err != nil || limit == 0 {
			return errors.New("invalid memory controller limit")
		}
		// v1 represents unlimited with a very large page-aligned integer.
		if v2 || limit < (uint64(1)<<60) {
			raw, err := read(filepath.Join(path, currentName))
			if err != nil {
				return fmt.Errorf("read memory controller usage: %w", err)
			}

			current, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
			if err != nil {
				return errors.New("invalid memory controller usage")
			}

			available := uint64(0)
			if current < limit {
				available = limit - current
			}

			s.TotalBytes = min(s.TotalBytes, limit)
			s.AvailableBytes = min(s.AvailableBytes, available)
		}
	}

	return nil
}

func contains(list, item string) bool { return slices.Contains(strings.Split(list, ","), item) }

func unescape(path string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(path)
}
