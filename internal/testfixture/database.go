package testfixture

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// DatabaseTemplate copies one prepared database into each supplied directory.
// initialize closes its database connections before returning.
func DatabaseTemplate(initialize func(string) error) func(string) (string, error) {
	image := sync.OnceValues(func() ([]byte, error) {
		directory, err := os.MkdirTemp("", "savorum-database-fixture-")
		if err != nil {
			return nil, fmt.Errorf("create fixture directory: %w", err)
		}
		defer func() { _ = os.RemoveAll(directory) }()

		path := filepath.Join(directory, "library.db")
		if err := initialize(path); err != nil {
			return nil, fmt.Errorf("initialize fixture database: %w", err)
		}

		root, err := os.OpenRoot(directory)
		if err != nil {
			return nil, fmt.Errorf("open fixture directory: %w", err)
		}
		defer root.Close()

		data, err := root.ReadFile("library.db")
		if err != nil {
			return nil, fmt.Errorf("read fixture database: %w", err)
		}

		return data, nil
	})

	return func(directory string) (string, error) {
		data, err := image()
		if err != nil {
			return "", err
		}

		if err := CopyDatabase(directory, data); err != nil {
			return "", err
		}

		return filepath.Join(directory, "library.db"), nil
	}
}

// CopyDatabase writes a database image beneath the supplied directory.
func CopyDatabase(directory string, image []byte) error {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return fmt.Errorf("open fixture directory: %w", err)
	}
	defer root.Close()

	if err := root.WriteFile("library.db", image, 0o600); err != nil {
		return fmt.Errorf("copy fixture database: %w", err)
	}

	return nil
}
