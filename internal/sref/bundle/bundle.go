// Package bundle reads and writes the SREF bundle: an ordered container of
// complete `.sref` packages defined by the SREF specification, not by Savorum.
//
// Every member below `recipes/` is exactly the package an individual recipe
// export produces, so any member stands alone; version 1 therefore has no
// shared asset store. A bundle holds portable recipes only and is not a
// Savorum backup.
package bundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// Format and related constants identify the SREF bundle media type and current version.
const (
	// Format identifies the container in the manifest.
	Format = "sref-bundle"
	// Version is the bundle layout this build reads and writes. It is
	// versioned independently of the recipe specification, the package layout,
	// and the unit registry: a bundle says nothing about which SREF versions
	// its members use, and its members say so themselves.
	Version = 1
	// Extension is the conventional file suffix.
	Extension = ".srefbundle"
	// MediaType is what a bundle is served as, since SREF has no registered
	// media type.
	MediaType = "application/zip"
	// ManifestPath is the one root entry a bundle carries.
	ManifestPath = "manifest.json"
	// memberDirectory is the only place a bundle may put a file.
	memberDirectory = "recipes/"
)

var (
	// ErrInvalidBundle reports invalid SREF bundle.
	ErrInvalidBundle = errors.New("invalid SREF bundle")
	// ErrUnsafeBundle reports unsafe SREF bundle.
	ErrUnsafeBundle = errors.New("unsafe SREF bundle")
)

// portableID is the SREF recipe identifier syntax. It is repeated here rather
// than assumed because member paths are built from it: an identifier that
// cannot contain a separator or a dot segment is what makes
// `recipes/<member-id>.sref` safe to construct without escaping.
var portableID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Entry identifies and verifies one member. The digest and size describe the
// exact `.sref` bytes, not the recipe document inside them, which the member's
// own manifest already covers.
type Entry struct {
	MemberID string `json:"member_id"`
	RecipeID string `json:"recipe_id"`
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

// Manifest is the bundle's own root document. It carries only what identifies
// and verifies its members; in particular no timestamp, so two exports of the
// same recipes are identical.
type Manifest struct {
	Format  string  `json:"format"`
	Version int     `json:"version"`
	Recipes []Entry `json:"recipes"`
}

// MemberPath is where one package lives inside a bundle. Its member ID is
// transport-local and deliberately independent of the recipe ID inside the
// package, because distinct packages may legitimately carry the same recipe
// ID without either recipe being rewritten.
func MemberPath(memberID string) string {
	return memberDirectory + memberID + ".sref"
}

// ValidPortableID reports whether an identifier satisfies the SREF portable
// identifier grammar used by both recipe and bundle-local member IDs.
func ValidPortableID(identifier string) bool {
	return portableID.MatchString(identifier)
}

// Encode writes the manifest. Members remain in their intended order. The JSON
// is stable for the same exact entries, but SREF does not define this or any
// other serialization as canonical.
func (m Manifest) Encode() ([]byte, error) {
	if m.Format != Format || m.Version != Version {
		return nil, fmt.Errorf("%w: a bundle manifest is %s version %d", ErrInvalidBundle, Format, Version)
	}

	if len(m.Recipes) == 0 {
		return nil, fmt.Errorf("%w: a bundle carries at least one recipe", ErrInvalidBundle)
	}

	encoded, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode bundle manifest: %w", err)
	}

	return append(encoded, '\n'), nil
}
