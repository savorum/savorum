package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/library"
)

// standardOutputDestination is the --output value that streams the artifact
// instead of naming a file.
const standardOutputDestination = "-"

// newExportCommand writes portable recipe artifacts through the same export
// service the HTTP API uses, so a `.sref` written here and one downloaded from
// the application are the same file.
func newExportCommand(savorum *application) *cobra.Command {
	command := &cobra.Command{
		Use:   "export",
		Short: "Write recipes as portable SREF artifacts",
		Long: "Export writes recipes in the same portable form the application downloads.\n" +
			"A recipe carries its own content and photographs; this library's books,\n" +
			"ratings, notes, and cooking history stay behind. The restorable copy of an\n" +
			"instance is savorum backup.",
	}

	requireGroup(command)
	command.AddCommand(
		newExportRecipeCommand(savorum),
		newExportSelectionCommand(savorum),
		newExportBookCommand(savorum),
		newExportLibraryCommand(savorum),
	)

	return command
}

// exportFlags are the options every export shares.
type exportFlags struct {
	database string
	assets   string
	output   string
}

func (e *exportFlags) bind(cmd *cobra.Command, outputUsage string) {
	cmd.Flags().StringVar(&e.database, "database", "", "path to the SQLite database")
	cmd.Flags().StringVar(&e.assets, "assets", "", "content-addressed asset storage directory")
	cmd.Flags().StringVar(&e.output, "output", "", outputUsage)
	requireFlags(cmd, "database")
	markPathFlags(cmd, "database", "assets", "output")
}

func newExportRecipeCommand(savorum *application) *cobra.Command {
	var (
		options  exportFlags
		revision uint64
	)

	command := &cobra.Command{
		Use:   "recipe <entry-id>",
		Short: "Write one recipe as a .sref package",
		Example: "  savorum export recipe --database /data/library.db ENTRY_ID\n" +
			"  savorum export recipe --database /data/library.db --output - ENTRY_ID > recipe.sref",
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, arguments []string) error {
			ctx, services, err := savorum.openExchange(cmd.Context(), options.database, options.assets)
			if err != nil {
				return err
			}
			defer services.close()

			entryID := library.EntryID(arguments[0])

			source := export.NewRecipeSource(services.export.Library(), entryID)
			if revision > 0 {
				source = export.NewRevisionSource(services.export.Library(), entryID, revision)
			}

			written, err := writeArtifact(ctx, cmd, services.export, source, export.FormatSREF, options.output)
			if err != nil {
				return err
			}

			savorum.report(cmd, "exported %s\n", written)

			return nil
		},
	}

	options.bind(command, "file to write, a directory to name the file in, or - for standard output")
	command.Flags().Uint64Var(&revision, "revision", 0, "export a retained earlier version instead of the current one")

	return command
}

func newExportSelectionCommand(savorum *application) *cobra.Command {
	var options exportFlags

	command := &cobra.Command{
		Use:     "recipes <entry-id>...",
		Short:   "Write several recipes as one ordered bundle",
		Long:    "The order the entry IDs are given in is the order the bundle carries.",
		Example: "  savorum export recipes --database /data/library.db FIRST_ID SECOND_ID",
		Args:    usageArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, arguments []string) error {
			ctx, services, err := savorum.openExchange(cmd.Context(), options.database, options.assets)
			if err != nil {
				return err
			}
			defer services.close()

			entryIDs := make([]library.EntryID, 0, len(arguments))
			for _, id := range arguments {
				entryIDs = append(entryIDs, library.EntryID(id))
			}

			source := export.NewSelectionSource(services.export.Library(), entryIDs)

			written, err := writeArtifact(ctx, cmd, services.export, source, export.FormatSREFBundle, options.output)
			if err != nil {
				return err
			}

			savorum.report(cmd, "exported %d %s to %s\n", len(entryIDs), plural(len(entryIDs), "recipe", "recipes"), written)

			return nil
		},
	}

	options.bind(command, "bundle to write, a directory to name it in, or - for standard output")

	return command
}

func newExportBookCommand(savorum *application) *cobra.Command {
	var options exportFlags

	command := &cobra.Command{
		Use:     "book <book-id>",
		Short:   "Write the recipes a book holds as one bundle",
		Long:    "The book's own order travels with it; a book is not serialized, only the\nrecipes it holds.",
		Example: "  savorum export book --database /data/library.db BOOK_ID",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, arguments []string) error {
			ctx, services, err := savorum.openExchange(cmd.Context(), options.database, options.assets)
			if err != nil {
				return err
			}
			defer services.close()

			source := export.NewBookSource(services.export.Library(), arguments[0])

			written, err := writeArtifact(ctx, cmd, services.export, source, export.FormatSREFBundle, options.output)
			if err != nil {
				return err
			}

			savorum.report(cmd, "exported %s\n", written)

			return nil
		},
	}

	options.bind(command, "bundle to write, a directory to name it in, or - for standard output")

	return command
}

func newExportLibraryCommand(savorum *application) *cobra.Command {
	var options exportFlags

	command := &cobra.Command{
		Use:     "library",
		Short:   "Write every active recipe as its own .sref",
		Example: "  savorum export library --database /data/library.db --output /tmp/recipes",
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if options.output == standardOutputDestination {
				return usageErrorf("--output must be a directory; only a single recipe or bundle can be streamed")
			}

			ctx, services, err := savorum.openExchange(cmd.Context(), options.database, options.assets)
			if err != nil {
				return err
			}
			defer services.close()

			if err := os.MkdirAll(options.output, 0o750); err != nil {
				return fmt.Errorf("create export directory: %w", err)
			}

			ids, err := activeEntryIDs(ctx, services)
			if err != nil {
				return err
			}

			if err := writeLibrary(ctx, services.export, ids, options.output); err != nil {
				return err
			}

			savorum.report(cmd, "exported %d %s to %s\n", len(ids), plural(len(ids), "recipe", "recipes"), options.output)

			return nil
		},
	}

	options.bind(command, "directory to write recipes into")
	requireFlags(command, "output")
	_ = command.MarkFlagDirname("output")

	return command
}

func activeEntryIDs(ctx context.Context, services *exchangeServices) ([]library.EntryID, error) {
	var ids []library.EntryID

	offset := 0

	for {
		summaries, err := services.recipes.List(ctx, library.ListOptions{
			Limit: library.MaximumListLimit, Offset: offset, State: library.StateActive,
		})
		if err != nil {
			return nil, err
		}

		if len(summaries) == 0 {
			return ids, nil
		}

		for _, summary := range summaries {
			ids = append(ids, summary.ID)
		}

		offset += len(summaries)
	}
}

func writeLibrary(ctx context.Context, exporter *export.Service, ids []library.EntryID, directory string) error {
	used := map[string]int{}

	for _, id := range ids {
		plan, err := exporter.Prepare(ctx, export.NewRecipeSource(exporter.Library(), id), export.FormatSREF)
		if err != nil {
			return err
		}

		name := plan.Filename
		if seen := used[plan.Filename]; seen > 0 {
			name = strings.TrimSuffix(plan.Filename, ".sref") + fmt.Sprintf("-%d.sref", seen+1)
		}

		used[plan.Filename]++

		if err := writeToFile(ctx, exporter, plan, filepath.Join(directory, name)); err != nil {
			return err
		}
	}

	return nil
}

// writeArtifact resolves the export and writes it where output names, reporting
// the destination it settled on. An empty output writes into the working
// directory under the name the export service chose, a directory names the file
// inside it, and "-" streams the artifact to standard output.
func writeArtifact(
	ctx context.Context,
	cmd *cobra.Command,
	exporter *export.Service,
	source export.Source,
	format export.Format,
	output string,
) (string, error) {
	plan, err := exporter.Prepare(ctx, source, format)
	if err != nil {
		return "", err
	}

	if output == standardOutputDestination {
		if err := exporter.Write(ctx, plan, cmd.OutOrStdout()); err != nil {
			return "", err
		}

		return "standard output", nil
	}

	destination := output
	if destination == "" {
		destination = plan.Filename
	} else if info, err := os.Stat(destination); err == nil && info.IsDir() {
		destination = filepath.Join(destination, plan.Filename)
	}

	if err := writeToFile(ctx, exporter, plan, destination); err != nil {
		return "", err
	}

	return destination, nil
}

// writeToFile stages the artifact beside its destination and renames it into
// place, so an interrupted export leaves no partial file.
func writeToFile(ctx context.Context, exporter *export.Service, plan export.Plan, destination string) error {
	release, err := diskspace.Shared.Reserve(filepath.Dir(destination), uint64(exporter.OutputByteLimit()), 1, diskspace.ReserveBytes) //nolint:gosec // the service validates its positive output limit
	if err != nil {
		return fmt.Errorf("reserve export workspace: %w", err)
	}
	defer release()

	temporary, err := os.CreateTemp(filepath.Dir(destination), ".savorum-export-*")
	if err != nil {
		return fmt.Errorf("write %q: %w", destination, err)
	}

	staged := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(staged)
	}()

	if err := exporter.Write(ctx, plan, temporary); err != nil {
		return err
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("write %q: %w", destination, err)
	}

	if err := os.Rename(staged, destination); err != nil {
		return fmt.Errorf("write %q: %w", destination, err)
	}

	return nil
}
