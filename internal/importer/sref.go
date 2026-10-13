package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/sref/archivepolicy"
	"github.com/savorum/savorum/internal/sref/bundle"
)

type srefAdapter struct {
	packageLimits sref.Limits
	bundleLimits  bundle.Limits
}

func newSREFAdapter() srefAdapter {
	return srefAdapter{packageLimits: sref.DefaultLimits(), bundleLimits: bundle.DefaultLimits()}
}

// DefaultRegistryWithSREFLimits composes the built-in adapters with the
// application's archive policy. Format interpretation remains in adapters;
// the orchestrator still owns admission, preview, and commit.
func DefaultRegistryWithSREFLimits(packageLimits sref.Limits, bundleLimits bundle.Limits) Registry {
	return NewRegistry(
		srefAdapter{packageLimits: packageLimits, bundleLimits: bundleLimits},
		plainTextAdapter{}, webRecipeAdapter{}, cookbookManagerAdapter{}, paprikaAdapter{}, mealieAdapter{}, tandoorAdapter{}, documentAdapter{},
	)
}

func (srefAdapter) Format() Format {
	return Format{InputKinds: []string{"file"}, MediaTypes: []string{"application/json", "application/zip"}, ID: "sref", Name: "SREF", Version: sref.Version, Extensions: []string{".json", ".sref", bundle.Extension}, Capabilities: Capabilities{
		Collection: true, Images: true, StructuredIngredients: true, StructuredInstructions: true, SourceIdentity: true,
	}}
}

func (adapter srefAdapter) Probe(_ context.Context, input Input) ProbeResult {
	if kind := probeSREFJSON(input.Data); kind != "" {
		return ProbeResult{Confidence: ConfidenceExact, Reason: "SREF version member"}
	}

	if kind := probeSREFArchive(input.Data); kind != "" {
		return ProbeResult{Confidence: ConfidenceExact, Reason: kind + " manifest"}
	}

	return ProbeResult{Confidence: extensionConfidence(adapter.Format(), input.Name), Reason: "filename extension"}
}

func (adapter srefAdapter) Import(ctx context.Context, input Input, options Options) (ImportBatch, error) {
	if err := ctx.Err(); err != nil {
		return ImportBatch{}, err
	}

	kind := probeSREFJSON(input.Data)
	if kind == "" {
		kind = probeSREFArchive(input.Data)
	}

	if kind == "" {
		switch strings.ToLower(filepath.Ext(input.Name)) {
		case ".sref":
			kind = "sref-package"
		case bundle.Extension:
			kind = "sref-bundle"
		}
	}

	switch kind {
	case "sref-document":
		document, err := sref.Decode(input.Data)
		if err != nil {
			return ImportBatch{}, err
		}

		return ImportBatch{
			Source:  ImportSource{Format: srefDocumentFormat()},
			Recipes: []ImportedRecipe{{Document: document, PortableAssetReferences: true}},
		}, nil
	case "sref-package":
		opened, err := sref.ReadPackageWithBudget(input.Data, adapter.packageLimits, options.workBudget(adapter.packageLimits.WorkBytes))
		if err != nil {
			return ImportBatch{}, err
		}

		return ImportBatch{
			Source:  ImportSource{Format: srefPackageFormat()},
			Recipes: []ImportedRecipe{importedSREFPackage(opened)},
		}, nil
	case "sref-bundle":
		budget := options.workBudget(adapter.bundleLimits.WorkBytes)
		result := ImportBatch{Source: ImportSource{Format: srefBundleFormat()}}

		_, err := bundle.VisitWithBudget(input.Data, adapter.bundleLimits, budget, func(member bundle.Member) error {
			if err := ctx.Err(); err != nil {
				return err
			}

			packaged, err := sref.ReadPackageWithBudget(member.Bytes, adapter.packageLimits, budget)
			if err != nil {
				return fmt.Errorf("read bundle member %q: %w", member.RecipeID, err)
			}

			if string(packaged.Recipe.ID) != member.RecipeID {
				return fmt.Errorf("%w: member %q declares recipe %q and carries %q",
					bundle.ErrUnsafeBundle, member.MemberID, member.RecipeID, packaged.Recipe.ID)
			}

			result.Recipes = append(result.Recipes, importedSREFPackage(packaged))

			return nil
		})
		if err != nil {
			return ImportBatch{}, err
		}

		return result, nil
	default:
		return ImportBatch{}, fmt.Errorf("%w: %s", ErrUnsupportedFormat, strings.ToLower(filepath.Ext(input.Name)))
	}
}

func importedSREFPackage(opened sref.Package) ImportedRecipe {
	result := ImportedRecipe{Document: opened.Recipe, PortableAssetReferences: true, EmbeddedAssets: make([]EmbeddedAsset, 0, len(opened.Assets))}
	for _, asset := range opened.Assets {
		result.EmbeddedAssets = append(result.EmbeddedAssets, EmbeddedAsset{
			ID: asset.ID, Path: asset.Path, MediaType: asset.MediaType, SHA256: asset.SHA256,
			Bytes: append([]byte(nil), asset.Bytes...),
		})
	}

	return result
}

func srefDocumentFormat() Format {
	format := srefAdapter{}.Format()
	format.ID, format.Name, format.Extensions = "sref-document", "SREF document", []string{".json"}
	format.Capabilities.Collection, format.Capabilities.Images = false, false

	return format
}

func srefPackageFormat() Format {
	format := srefAdapter{}.Format()
	format.ID, format.Name, format.Extensions = "sref-package", "SREF package", []string{".sref"}
	format.Capabilities.Collection = false

	return format
}

func srefBundleFormat() Format {
	format := srefAdapter{}.Format()
	format.ID, format.Name, format.Extensions = "sref-bundle", "SREF bundle", []string{bundle.Extension}

	return format
}

func probeSREFJSON(data []byte) string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ""
	}

	first := bytes.TrimSpace(trimmed[1:])
	if bytes.HasPrefix(first, []byte(`"sref"`)) {
		after := bytes.TrimSpace(first[len(`"sref"`):])
		if len(after) > 0 && after[0] == ':' {
			return "sref-document"
		}
	}

	var root map[string]json.RawMessage
	if json.Unmarshal(trimmed, &root) != nil || root["sref"] == nil {
		return ""
	}

	return "sref-document"
}

func probeSREFArchive(data []byte) string {
	if len(data) < 4 || !bytes.Equal(data[:4], []byte("PK\x03\x04")) {
		return ""
	}

	if archivepolicy.Preflight(data, 10_002, 8<<20) != nil {
		return ""
	}

	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(archive.File) > 10_002 {
		return ""
	}

	for _, entry := range archive.File {
		if entry.Name != "manifest.json" || entry.UncompressedSize64 > 64<<10 {
			continue
		}

		opened, err := entry.Open()
		if err != nil {
			return ""
		}

		manifest, readErr := io.ReadAll(io.LimitReader(opened, (64<<10)+1))

		closeErr := opened.Close()
		if readErr != nil || closeErr != nil || len(manifest) > 64<<10 {
			return ""
		}

		var root map[string]json.RawMessage
		if json.Unmarshal(manifest, &root) != nil {
			return ""
		}

		if root["sref_package"] != nil {
			return "sref-package"
		}

		var format string
		if json.Unmarshal(root["format"], &format) == nil && format == bundle.Format {
			return "sref-bundle"
		}
	}

	return ""
}
