// Command printfixtures renders the awkward print corpus to every physical
// format, for review on paper at actual size.
//
//	go run ./cmd/printfixtures --output tmp/print
//
// Print the results at 100 percent, not scaled to fit, and check the
// calibration rule on the card sheets before judging anything.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/print"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/render/typst"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// corpus answers the print service's reads from the fixture recipes, so the
// reference documents travel through exactly the pipeline a request does.
type corpus struct {
	documents map[library.EntryID]recipe.Document
}

func (c corpus) Get(_ context.Context, id library.EntryID) (library.Entry, error) {
	document, found := c.documents[id]
	if !found {
		return library.Entry{}, library.ErrNotFound
	}

	return library.Entry{ID: id, Recipe: document, Revision: 1}, nil
}

func (corpus) GetOrganization(_ context.Context, recipeID string) (library.Organization, error) {
	return library.Organization{EntryID: library.EntryID(recipeID)}, nil
}

type variant struct {
	name    string
	options func() print.Options
}

func variants() []variant {
	with := func(size print.Size, output print.Output) func() print.Options {
		return func() print.Options {
			options := print.DefaultOptions()
			options.Size = size
			options.Output = output

			return options
		}
	}

	return []variant{
		{"letter", with(print.SizeLetter, print.OutputNative)},
		{"a4", with(print.SizeA4, print.OutputNative)},
		{"card-3x5", with(print.SizeCard35, print.OutputNative)},
		{"card-4x6", with(print.SizeCard46, print.OutputNative)},
		{"card-5x8", with(print.SizeCard58, print.OutputNative)},
		{"card-4x6-on-letter", with(print.SizeCard46, print.OutputSheetLetter)},
		{"card-4x6-on-a4", with(print.SizeCard46, print.OutputSheetA4)},
		{"card-3x5-on-letter", with(print.SizeCard35, print.OutputSheetLetter)},
	}
}

func run(arguments []string) error {
	flags := flag.NewFlagSet("printfixtures", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("output", "tmp/print", "directory to write reference documents into")
	locale := flags.String("locale", "en", "interface language tag for labels and formatted amounts")

	only := flags.String("only", "", "render one variant by name")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return fmt.Errorf("parse command flags: %w", err)
	}

	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}

	if err := os.MkdirAll(*output, 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	documents := testfixture.PrintCorpus()
	source := corpus{documents: map[library.EntryID]recipe.Document{}}

	ids := make([]library.EntryID, 0, len(documents))
	for _, document := range documents {
		id := library.EntryID(document.ID)
		source.documents[id] = document
		ids = append(ids, id)
	}

	renderer := typst.New()
	defer renderer.Close()

	service, err := print.NewService(renderer, units.DefaultCatalog(), nil, source, nil)
	if err != nil {
		return err
	}

	ctx := context.Background()

	for _, entry := range variants() {
		if *only != "" && entry.name != *only {
			continue
		}

		for _, id := range ids {
			options := entry.options()
			options.Locale = *locale

			artifact, err := service.Render(ctx, print.Request{
				Source:  print.NewRecipeSource(source, id),
				Options: options,
			})
			if err != nil {
				return fmt.Errorf("render %s as %s: %w", id, entry.name, err)
			}

			name := fmt.Sprintf("%s--%s.pdf", strings.TrimPrefix(string(id), "print-"), entry.name)
			if err := os.WriteFile(filepath.Join(*output, name), artifact.PDF, 0o600); err != nil {
				return fmt.Errorf("write print fixture %q: %w", name, err)
			}

			fmt.Fprintf(os.Stdout, "%-52s %d page(s)\n", name, artifact.Pages)
		}
		// A collection exercises the composition the individual recipes cannot:
		// several recipes in one document, imposed together where cards are.
		options := entry.options()
		options.Locale = *locale

		artifact, err := service.Render(ctx, print.Request{
			Source:  print.NewSelectionSource(source, ids),
			Options: options,
		})
		if err != nil {
			return fmt.Errorf("render the collection as %s: %w", entry.name, err)
		}

		name := fmt.Sprintf("collection--%s.pdf", entry.name)
		if err := os.WriteFile(filepath.Join(*output, name), artifact.PDF, 0o600); err != nil {
			return fmt.Errorf("write print fixture %q: %w", name, err)
		}

		fmt.Fprintf(os.Stdout, "%-52s %d page(s)\n", name, artifact.Pages)
	}

	return nil
}
