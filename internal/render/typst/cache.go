package typst

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// privateDirectory creates a compiler cache whose ancestry prevents other
// users from replacing the machine code this process runs.
func privateDirectory(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve the directory: %w", err)
	}

	path, err = privateCachePath(path)
	if err != nil {
		return "", err
	}

	probe, err := os.CreateTemp(path, ".write-*")
	if err != nil {
		return "", fmt.Errorf("write to the directory: %w", err)
	}

	if err := errors.Join(probe.Close(), os.Remove(probe.Name())); err != nil {
		return "", fmt.Errorf("remove the write probe: %w", err)
	}

	return path, nil
}

func privateCachePath(path string) (string, error) {
	separator := string(os.PathSeparator)
	components := strings.Split(strings.TrimPrefix(path, separator), separator)
	directory := separator

	if _, err := prepareCacheEntry(directory, path == separator); err != nil {
		return "", err
	}

	followed := 0

	for len(components) > 0 {
		component := components[0]
		components = components[1:]

		switch component {
		case "", ".":
			continue
		case "..":
			directory = filepath.Dir(directory)
			continue
		}

		child := filepath.Join(directory, component)

		info, err := prepareCacheEntry(child, len(components) == 0)
		if err != nil {
			return "", err
		}

		if info.Mode()&os.ModeSymlink == 0 {
			directory = child
			continue
		}

		if followed == 40 {
			return "", errors.New("too many symbolic links in the cache path")
		}

		target, err := os.Readlink(child)
		if err != nil {
			return "", fmt.Errorf("read symbolic link %s: %w", child, err)
		}

		if filepath.IsAbs(target) {
			directory = separator
		}

		// Resolve target components before '..' to preserve symlink semantics.
		components = append(strings.Split(target, separator), components...)
		followed++
	}

	return directory, nil
}

func prepareCacheEntry(directory string, leaf bool) (os.FileInfo, error) {
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		// The caller validates each parent before creating its child.
		if err := os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create %s: %w", directory, err)
		}

		info, err = os.Lstat(directory)
	}

	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", directory, err)
	}

	if err := validateCacheEntry(info, leaf); err != nil {
		return nil, fmt.Errorf("%s: %w", directory, err)
	}

	return info, nil
}

func validateCacheEntry(info os.FileInfo, leaf bool) error {
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner == nil || (owner.Uid != 0 && int(owner.Uid) != os.Geteuid()) {
		return errors.New("not owned by this user or root")
	}

	if info.Mode()&os.ModeSymlink != 0 && !leaf {
		return nil
	}

	if !info.IsDir() || (leaf && int(owner.Uid) != os.Geteuid()) {
		return errors.New("not a directory owned by this user")
	}

	// A trusted sticky ancestor protects entries owned by this user or root.
	if info.Mode().Perm()&0o022 != 0 && (leaf || info.Mode()&os.ModeSticky == 0) {
		return errors.New("writable by other users")
	}

	return nil
}
