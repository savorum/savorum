// Package export turns recipes in this library into portable artifacts.
//
// Export, print, and backup are three different jobs and this package is only
// the first of them:
//
//	Print   a rendered document a person reads          → PDF
//	Export  portable recipe data another tool reads     → SREF, SREF Bundle
//	Backup  everything needed to rebuild this instance  → database and assets
//
// Export writes the recipe as stored, never as displayed (scaling and unit
// conversion belong to print and the page), and the recipe alone: library
// organisation, ratings, history, and trash state do not travel with it.
package export

import (
	"context"
	"errors"
	"io"
)

// Format names a portable representation.
type Format string

// Export limits bound the number of recipes and assets in one selection.
// Selection limits bound the number and length of requested recipe identities.
const (
	// FormatSREF is one recipe as a `.sref` package, even without photographs.
	FormatSREF Format = "sref"
	// FormatSREFBundle is an ordered collection of `.sref` packages.
	FormatSREFBundle Format = "sref-bundle"
)

// Descriptor describes a format to something that has to offer it.
type Descriptor struct {
	ID                 Format
	Label              string
	Extension          string
	MediaType          string
	SupportsSingle     bool
	SupportsCollection bool
}

// RecipeExporter writes one recipe.
type RecipeExporter interface {
	Format() Format
	ExportRecipe(ctx context.Context, snapshot RecipeSnapshot, destination io.Writer) error
}

// CollectionExporter writes an ordered set of recipes as one artifact.
type CollectionExporter interface {
	Format() Format
	ExportCollection(ctx context.Context, snapshots []RecipeSnapshot, destination io.Writer) error
}

var (
	// ErrUnsupportedFormat reports a format this build does not write, or does
	// not write for the number of recipes asked for.
	ErrUnsupportedFormat = errors.New("unsupported export format")
	// ErrNoRecipes reports an export that names nothing.
	ErrNoRecipes = errors.New("an export names at least one recipe")
	// ErrInvalidSelection reports malformed local entry identifiers.
	ErrInvalidSelection = errors.New("export selection is invalid")
	// ErrTooLarge reports work or output beyond synchronous export policy.
	ErrTooLarge = errors.New("export exceeds synchronous resource limits")
	// ErrAssetUnavailable reports a photograph the recipe declares that this
	// library cannot produce or cannot verify. An export fails on it rather
	// than writing an artifact whose recipe names files it does not carry.
	ErrAssetUnavailable = errors.New("a referenced file is unavailable")
)

// Selection limits bound the number and length of requested recipe identities.
const (
	MaximumSelectionRecipes = 250
	MaximumEntryIDLength    = 255
)

// AssetError names the recipe and image that could not be exported, without
// revealing the storage layout.
type AssetError struct {
	ReasonCode  string
	RecipeTitle string
	AssetID     string
	Reason      string
}

func (e *AssetError) Error() string {
	return "“" + e.RecipeTitle + "” references a file that " + e.Reason
}

func (*AssetError) Unwrap() error { return ErrAssetUnavailable }

// Formats reports every format this build offers, in presentation order.
func Formats() []Descriptor {
	return []Descriptor{
		{
			ID:             FormatSREF,
			Label:          "SREF",
			Extension:      ".sref",
			MediaType:      "application/zip",
			SupportsSingle: true,
		},
		{
			ID:                 FormatSREFBundle,
			Label:              "SREF Bundle",
			Extension:          ".srefbundle",
			MediaType:          "application/zip",
			SupportsCollection: true,
		},
	}
}

// DescriptorOf reports one format.
func DescriptorOf(format Format) (Descriptor, bool) {
	for _, descriptor := range Formats() {
		if descriptor.ID == format {
			return descriptor, true
		}
	}

	return Descriptor{}, false
}
