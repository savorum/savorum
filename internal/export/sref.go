package export

import (
	"context"
	"io"

	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/sref/bundle"
)

// srefExporter writes one recipe as a `.sref` package, as a thin adapter over
// internal/sref.
type srefExporter struct{ assets *assetResolver }

func (*srefExporter) Format() Format { return FormatSREF }

func (e *srefExporter) ExportRecipe(ctx context.Context, snapshot RecipeSnapshot, destination io.Writer) error {
	contents, err := e.assets.resolve(ctx, snapshot)
	if err != nil {
		return err
	}

	return sref.WritePackageTo(destination, snapshot.Recipe, contents)
}

// srefBundleExporter writes an ordered collection as a `.srefbundle` whose
// members are byte for byte the single-recipe exports.
type srefBundleExporter struct{ recipes *srefExporter }

func (*srefBundleExporter) Format() Format { return FormatSREFBundle }

func (e *srefBundleExporter) ExportCollection(ctx context.Context, snapshots []RecipeSnapshot, destination io.Writer) error {
	if len(snapshots) == 0 {
		return ErrNoRecipes
	}

	writer := bundle.NewWriter(destination)

	for _, snapshot := range snapshots {
		recipeID := string(snapshot.Recipe.ID)

		if err := ctx.Err(); err != nil {
			return err
		}
		// The member is streamed straight into the archive, so a bundle of
		// large recipes costs no more memory than a bundle of small ones.
		if err := writer.Add(recipeID, func(member io.Writer) error {
			return e.recipes.ExportRecipe(ctx, snapshot, member)
		}); err != nil {
			return err
		}
	}

	return writer.Close()
}
