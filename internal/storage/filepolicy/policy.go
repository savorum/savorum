// Package filepolicy defines the permissions applied to Savorum's sensitive
// on-disk state. Owner-only access is the default; group access must be chosen
// explicitly by deployment configuration.
package filepolicy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Access selects the permission model for storage owned by Savorum.
type Access string

const (
	// OwnerOnly limits storage access to the account running Savorum.
	OwnerOnly Access = "owner"
	// SharedGroup permits members of the storage file group to access it.
	SharedGroup Access = "group"
)

// Policy specifies the access permissions applied to sensitive storage.
type Policy struct {
	Access Access
}

// OwnerPolicy returns the default policy, which limits access to the owner.
func OwnerPolicy() Policy { return Policy{Access: OwnerOnly} }

// Parse converts a configured access value into a storage policy.
func Parse(value string) (Policy, error) {
	switch Access(strings.TrimSpace(value)) {
	case "", OwnerOnly:
		return OwnerPolicy(), nil
	case SharedGroup:
		return Policy{Access: SharedGroup}, nil
	default:
		return Policy{}, fmt.Errorf("storage access %q must be owner or group", value)
	}
}

// FileMode returns the permission mode for sensitive regular files.
func (p Policy) FileMode() fs.FileMode {
	if p.Access == SharedGroup {
		return 0o660
	}

	return 0o600
}

// DirectoryMode returns the permission mode for storage directories.
func (p Policy) DirectoryMode() fs.FileMode {
	if p.Access == SharedGroup {
		return 0o770
	}

	return 0o700
}

// PrepareFile creates a missing sensitive file before a library can apply its
// own umask-dependent defaults, or migrates an existing regular file to the
// configured mode. The final component may not be a symlink.
func (p Policy) PrepareFile(path string) error {
	if err := ensureMissingParents(filepath.Dir(path), p.DirectoryMode()); err != nil {
		return fmt.Errorf("prepare sensitive file parent: %w", err)
	}

	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("sensitive path %q is not a regular file", path)
		}

		if err := os.Chmod(path, p.FileMode()); err != nil {
			return fmt.Errorf("secure sensitive file: %w", err)
		}

		return nil
	}

	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect sensitive file: %w", err)
	}

	// #nosec G304 -- the configured database or asset path is intentionally the file being prepared.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, p.FileMode())
	if err != nil {
		return fmt.Errorf("create sensitive file: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close sensitive file: %w", err)
	}

	return nil
}

// SecureFile applies the configured mode to an existing regular file without
// following a symlink in its final path component.
func (p Policy) SecureFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect sensitive file: %w", err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("sensitive path %q is not a regular file", path)
	}

	if err := os.Chmod(path, p.FileMode()); err != nil {
		return fmt.Errorf("secure sensitive file: %w", err)
	}

	return nil
}

// SecureTree creates or migrates a storage-owned tree. Symlinks and special
// files are rejected so an on-disk traversal cannot make permission repair
// escape the configured root.
func (p Policy) SecureTree(root string) error {
	if err := ensureMissingParents(filepath.Dir(root), p.DirectoryMode()); err != nil {
		return fmt.Errorf("prepare storage tree parent: %w", err)
	}

	if err := os.MkdirAll(root, p.DirectoryMode()); err != nil {
		return fmt.Errorf("create storage tree: %w", err)
	}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk storage tree: %w", walkErr)
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect storage tree entry: %w", err)
		}

		switch {
		case info.IsDir():
			// #nosec G122 -- SecureTree owns this root and rejects symlinks; chmod is required to repair its existing modes.
			if err := os.Chmod(path, p.DirectoryMode()); err != nil {
				return fmt.Errorf("secure storage directory: %w", err)
			}

			return nil
		case info.Mode().IsRegular():
			// #nosec G122 -- SecureTree owns this root and rejects symlinks; chmod is required to repair its existing modes.
			if err := os.Chmod(path, p.FileMode()); err != nil {
				return fmt.Errorf("secure storage file: %w", err)
			}

			return nil
		default:
			return fmt.Errorf("storage tree contains unsupported entry %q", path)
		}
	})
	if err != nil {
		return fmt.Errorf("secure storage tree: %w", err)
	}

	return nil
}

// ensureMissingParents secures only directories this operation creates. It
// never chmods an existing application, home, or mount directory merely
// because the database lives beneath it.
func ensureMissingParents(parent string, mode fs.FileMode) error {
	missing := []string{}

	for current := filepath.Clean(parent); ; current = filepath.Dir(current) {
		if _, err := os.Lstat(current); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect storage parent: %w", err)
		}

		missing = append(missing, current)

		next := filepath.Dir(current)
		if next == current {
			return fmt.Errorf("no existing parent for %q", parent)
		}
	}

	if err := os.MkdirAll(parent, mode); err != nil {
		return fmt.Errorf("create storage parents: %w", err)
	}

	for _, directory := range missing {
		if err := os.Chmod(directory, mode); err != nil {
			return fmt.Errorf("secure storage parent: %w", err)
		}
	}

	return nil
}
