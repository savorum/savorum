// Package srefcheckout locates the sibling SREF checkout that cross-repository
// tests read fixtures from, and decides whether its absence is a skip or a
// failure.
//
// `SREF_CHECKOUT` says where the checkout is, and `SREF_REQUIRED=1` makes its
// absence a failure rather than a skip, so security tests cannot silently pass
// in CI. CI sets both, against the revision integration/sref.json declares;
// local development sets neither.
package srefcheckout

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Default is where a sibling checkout sits relative to the module root.
// Resolve deliberately gives every relative checkout this single meaning.
const Default = "../sref"

// Path returns the directory holding `relative` inside the SREF checkout, or
// skips — or fails — when it is not there.
//
// `relative` is a path within the checkout, such as "examples". The returned
// path is absolute so a test can pass it anywhere without knowing its own
// working directory.
func Path(t *testing.T, relative string) string {
	t.Helper()
	root := Root(t)

	directory := filepath.Join(root, relative)
	if _, err := os.Stat(directory); err != nil {
		absent(t, fmt.Sprintf("%s is not in the SREF checkout at %s: %v", relative, root, err))
	}

	return directory
}

// Root returns the SREF checkout directory itself.
//
// A relative SREF_CHECKOUT is resolved as Resolve does.
func Root(t *testing.T) string {
	t.Helper()

	absolute, err := Locate()
	if err != nil {
		absent(t, err.Error())
	}

	return absolute
}

// Locate resolves the SREF checkout without a test to report through, for
// callers that build a path to a single file rather than a directory.
func Locate() (string, error) {
	root := os.Getenv("SREF_CHECKOUT")
	if root == "" {
		root = Default
	}

	return Resolve(root)
}

// Resolve turns a checkout path into an absolute one that exists.
//
// A relative path is resolved against this module's root: Go runs each test in
// its own directory, where `../sref` could silently resolve to `internal/sref`.
func Resolve(path string) (string, error) {
	if !filepath.IsAbs(path) {
		if module := moduleRoot(); module != "" {
			path = filepath.Join(module, path)
		}
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("the checkout path %q cannot be resolved: %w", path, err)
	}

	if _, err := os.Stat(absolute); err != nil {
		return "", fmt.Errorf("no checkout at %s: %w", absolute, err)
	}

	return absolute, nil
}

// Required reports whether a missing checkout is a failure. It is what a
// pipeline sets so that a cross-repository check cannot pass by not running.
func Required() bool {
	return os.Getenv("SREF_REQUIRED") == "1"
}

// Unavailable reports that a cross-repository fixture family could not run.
// Required jobs fail; optional local runs retain the convenient skip.
func Unavailable(t *testing.T, reason string) {
	t.Helper()
	absent(t, reason)
}

// moduleRoot walks up from the test's working directory to the directory
// holding go.mod, which is the only fixed point a relative path can mean the
// same thing from.
func moduleRoot() string {
	directory, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}

		parent := filepath.Dir(directory)
		if parent == directory {
			return ""
		}

		directory = parent
	}
}

func absent(t *testing.T, reason string) {
	t.Helper()

	if Required() {
		// Required: not running is a failure, not a skip.
		t.Fatalf("SREF_REQUIRED is set and %s", reason)
	}

	t.Skip(reason)
}
