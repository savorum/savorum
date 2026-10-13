// Package codegen writes the checked-in artifacts that are derived from the
// implementation. Every such artifact has the same discipline: regenerating it
// replaces the file atomically, and continuous integration can ask whether the
// checked-in copy is still the one the implementation would produce.
package codegen

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// WriteArtifact replaces path with contents, leaving no partially written file
// behind if the process stops midway.
func WriteArtifact(path string, contents []byte) error {
	directory := filepath.Dir(path)

	temporary, err := os.CreateTemp(directory, ".generated-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}

	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()

	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}

	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary file for %s: %w", path, err)
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file for %s: %w", path, err)
	}

	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}

	return nil
}

// CheckArtifact reports a stale checked-in artifact as an error naming the
// command that refreshes it, so the failure explains its own remedy.
func CheckArtifact(path string, contents []byte, command string) error {
	existing, err := os.ReadFile(path) //nolint:gosec // checking the caller-selected artifact path is this function's purpose
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	if !bytes.Equal(existing, contents) {
		return fmt.Errorf("%s is stale; run %s", path, command)
	}

	return nil
}
