package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
	"golang.org/x/sys/unix"
)

func newImportCommand(savorum *application) *cobra.Command {
	var (
		databasePath string
		assetPath    string
		input        string
		onDuplicate  string
		limits       exchangeInputLimits
	)

	command := &cobra.Command{
		Use:   "import",
		Short: "Read recipes into the library",
		Long: "Import accepts an SREF recipe or package, a CookbookManager export, or a\n" +
			"directory of them. A recipe keeps its identity across libraries, so meeting\n" +
			"one this library already holds is ordinary: --on-duplicate says what to do.",
		Example: "  savorum import --database /data/library.db --input ./recipes\n" +
			"  savorum import --database /data/library.db --input recipe.sref --on-duplicate replace",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			policy, err := duplicatePolicy(onDuplicate)
			if err != nil {
				return err
			}

			if err := limits.validate(); err != nil {
				return err
			}

			sources, err := exchangeFiles(cmd.Context(), input, limits)
			if err != nil {
				return err
			}

			defer sources.close()

			if len(sources.paths) == 0 {
				return fmt.Errorf("no SREF recipes or packages found in %q", input)
			}

			ctx, services, err := savorum.openExchange(cmd.Context(), databasePath, assetPath)
			if err != nil {
				return err
			}
			defer services.close()

			results, err := services.exchange.ImportSourceSequence(ctx, sources.inputs(ctx, limits), importer.Options{}, policy)
			if err != nil {
				return describeImportFailure(input, err)
			}

			reportImport(cmd, savorum, results)

			return nil
		},
	}

	command.Flags().StringVar(&databasePath, "database", "", "path to the SQLite database")
	command.Flags().StringVar(&assetPath, "assets", "", "content-addressed asset storage directory")
	command.Flags().StringVar(&input, "input", "", "an SREF recipe, CookbookManager YAML/ZIP, or a directory of them")
	command.Flags().StringVar(&onDuplicate, "on-duplicate", string(library.DuplicateReject),
		"what to do about a recipe this library already holds: reject, skip, or replace")
	command.Flags().Int64Var(&limits.fileBytes, "max-file-bytes", 64<<20, "maximum bytes read from one import file")
	command.Flags().Int64Var(&limits.totalBytes, "max-input-bytes", 256<<20, "maximum combined bytes read from import files")
	requireFlags(command, "database", "input")
	markPathFlags(command, "database", "assets", "input")

	return command
}

// duplicatePolicy rejects an unrecognized policy before anything is read or
// opened.
func duplicatePolicy(value string) (library.DuplicatePolicy, error) {
	policy := library.DuplicatePolicy(value)
	switch policy {
	case library.DuplicateReject, library.DuplicateSkip, library.DuplicateReplace:
		return policy, nil
	default:
		return "", usageErrorf("--on-duplicate %q is not one of reject, skip, or replace", value)
	}
}

func reportImport(cmd *cobra.Command, savorum *application, results []library.ImportResult) {
	counts := map[library.ImportOutcome]int{}
	dropped := 0

	for _, result := range results {
		counts[result.Outcome]++
		dropped += result.DroppedAssets
	}

	savorum.report(cmd, "imported %d, replaced %d, left alone %d\n",
		counts[library.ImportCreated], counts[library.ImportReplaced], counts[library.ImportSkipped])

	if dropped > 0 {
		savorum.report(cmd, "left out %d %s that arrived without their bytes; export as a package to carry them\n",
			dropped, plural(dropped, "photograph", "photographs"))
	}
}

func describeImportFailure(source string, err error) error {
	if duplicate, ok := errors.AsType[*library.DuplicateError](err); ok {
		return fmt.Errorf("%q is already in this library as %s; rerun with --on-duplicate skip or --on-duplicate replace to say what to do",
			duplicate.Title, duplicate.EntryID)
	}

	return fmt.Errorf("read %s: %w", source, err)
}

type exchangeInputLimits struct {
	fileBytes  int64
	totalBytes int64
}

func (l exchangeInputLimits) validate() error {
	if l.fileBytes <= 0 || l.totalBytes <= 0 || l.fileBytes == math.MaxInt64 || l.totalBytes == math.MaxInt64 {
		return usageErrorf("--max-file-bytes and --max-input-bytes must be between 1 and 9223372036854775806")
	}

	return nil
}

type exchangeInputFiles struct {
	paths []string
	root  *os.Root
}

func (s *exchangeInputFiles) close() {
	if s.root != nil {
		_ = s.root.Close()
	}
}

// exchangeFiles discovers regular files without following directory entries.
// An explicitly selected path can resolve through a symlink to a regular file.
func exchangeFiles(ctx context.Context, target string, limits exchangeInputLimits) (*exchangeInputFiles, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	info, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", target, err)
	}

	if !info.IsDir() {
		if err := checkExchangeFile(info, min(limits.fileBytes, limits.totalBytes)); err != nil {
			return nil, fmt.Errorf("read %q: %w", target, err)
		}

		return &exchangeInputFiles{paths: []string{target}}, nil
	}

	root, err := os.OpenRoot(target)
	if err != nil {
		return nil, fmt.Errorf("open input directory: %w", err)
	}

	sources := &exchangeInputFiles{root: root}
	if err := sources.discover(ctx, limits); err != nil {
		sources.close()
		return nil, err
	}

	return sources, nil
}

func (s *exchangeInputFiles) discover(ctx context.Context, limits exchangeInputLimits) error {
	directory, err := s.root.Open(".")
	if err != nil {
		return fmt.Errorf("read input directory: %w", err)
	}
	defer directory.Close()

	registry := importer.DefaultRegistry()
	remaining := limits.totalBytes

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		entries, err := directory.ReadDir(128)
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read input directory: %w", err)
		}

		for _, entry := range entries {
			name := entry.Name()

			lower := strings.ToLower(name)
			if !entry.Type().IsRegular() || (!strings.HasSuffix(lower, ".json") && !strings.HasSuffix(lower, ".sref") && !registry.Supports(name)) {
				continue
			}

			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("inspect import file %q: %w", name, err)
			}

			if !info.Mode().IsRegular() {
				continue
			}

			if err := checkExchangeFile(info, min(limits.fileBytes, remaining)); err != nil {
				return fmt.Errorf("inspect import file %q: %w", name, err)
			}

			remaining -= info.Size()

			if len(s.paths) >= library.MaximumImportRecipes {
				return fmt.Errorf("an import carries at most %d sources", library.MaximumImportRecipes)
			}

			s.paths = append(s.paths, name)
		}

		if errors.Is(err, io.EOF) {
			break
		}
	}

	sort.Strings(s.paths)

	return nil
}

func checkExchangeFile(info os.FileInfo, maximum int64) error {
	if !info.Mode().IsRegular() {
		return errors.New("import input must be a regular file")
	}

	if info.Size() < 0 || info.Size() > maximum {
		return fmt.Errorf("import input exceeds the remaining %d-byte budget; adjust --max-file-bytes or --max-input-bytes for a trusted export", maximum)
	}

	return nil
}

func (s *exchangeInputFiles) inputs(ctx context.Context, limits exchangeInputLimits) iter.Seq2[importer.Input, error] {
	return func(yield func(importer.Input, error) bool) {
		remaining := limits.totalBytes
		for _, name := range s.paths {
			data, err := s.read(ctx, name, min(limits.fileBytes, remaining))
			if err != nil {
				yield(importer.Input{}, fmt.Errorf("read %q: %w", name, err))
				return
			}

			remaining -= int64(len(data))
			if !yield(importer.Input{Name: filepath.Base(name), Data: data}, nil) {
				return
			}
		}
	}
}

func (s *exchangeInputFiles) read(ctx context.Context, name string, maximum int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var (
		source *os.File
		err    error
	)
	if s.root != nil {
		source, err = s.root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	} else {
		source, err = os.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK, 0) //nolint:gosec // name is the explicitly requested CLI input; discovered names open through os.Root
	}

	if err != nil {
		return nil, fmt.Errorf("open import input: %w", err)
	}

	defer source.Close()

	info, err := source.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect import input: %w", err)
	}

	if err := checkExchangeFile(info, maximum); err != nil {
		return nil, err
	}

	data, err := io.ReadAll(io.LimitReader(exchangeContextReader{ctx: ctx, reader: source}, maximum+1))
	if err != nil {
		return nil, fmt.Errorf("read import input: %w", err)
	}

	if int64(len(data)) > maximum {
		return nil, errors.New("import input grew beyond its byte budget")
	}

	return data, nil
}

type exchangeContextReader struct {
	ctx    context.Context //nolint:containedctx // cancellation follows the bounded input reader
	reader io.Reader
}

func (r exchangeContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	n, err := r.reader.Read(p)
	if cancelled := r.ctx.Err(); cancelled != nil {
		return 0, cancelled
	}

	return n, err //nolint:wrapcheck // io.Reader preserves EOF for io.ReadAll
}

// exchangeServices are the services recipe exchange needs together, because a
// recipe travels with its photographs.
type exchangeServices struct {
	recipes  *library.Service
	exchange *exchange.Service
	export   *export.Service
	close    func()
}

// openExchange composes the library and the asset store, and returns the
// context the services act under: the command line is a principal like any
// other caller. The returned function closes the database.
func (a *application) openExchange(ctx context.Context, databasePath, assetPath string) (context.Context, *exchangeServices, error) {
	policy, err := a.storagePolicy()
	if err != nil {
		return nil, nil, err
	}

	store, err := sqlite.Open(ctx, databasePath, sqlite.WithFilePolicy(policy))
	if err != nil {
		return nil, nil, err
	}

	closeStore := func() { _ = store.Close() }

	principal, err := store.LocalPrincipal(ctx)
	if err != nil {
		closeStore()
		return nil, nil, err
	}

	principal.Method = "cli"
	ctx = access.WithLibrary(identity.WithPrincipal(ctx, principal), "default")

	recipes, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		closeStore()
		return nil, nil, err
	}

	blobStore, err := files.NewAssetStore(resolveAssetPath(databasePath, assetPath), files.DefaultMaximumAssetSize, files.WithFilePolicy(policy))
	if err != nil {
		closeStore()
		return nil, nil, err
	}

	closeStore = func() {
		_ = blobStore.Close()
		_ = store.Close()
	}

	blobs, err := assets.NewService(store, blobStore)
	if err != nil {
		closeStore()
		return nil, nil, err
	}

	exporter, err := export.NewService(recipes, blobs)
	if err != nil {
		closeStore()
		return nil, nil, err
	}

	exchanger := exchange.NewService(recipes, blobs)

	return ctx, &exchangeServices{
		recipes: recipes, exchange: exchanger, export: exporter, close: func() {
			exchanger.Close()
			closeStore()
		},
	}, nil
}

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}

	return many
}
