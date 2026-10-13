package export

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/savorum/savorum/internal/sref"
)

// Service is the one place an export happens. Handlers, the command line, and
// anything added later name a source and a format and get an artifact; none of
// them knows how a recipe is serialized, and none of them writes an archive.
//
//	HTTP or CLI
//	     ↓
//	export service
//	     ↓
//	frozen recipe snapshots
//	     ↓
//	SREF exporter
//	     ↓
//	artifact stream
type Service struct {
	library     Library
	assets      *assetResolver
	recipes     map[Format]RecipeExporter
	collections map[Format]CollectionExporter
	limits      Limits
	work        chan struct{}
}

// Limits are application resource policy, not SREF format constraints. A
// deployment may raise or lower them without changing which bundle manifests
// are structurally valid.
type Limits struct {
	CollectionRecipes int
	AssetOccurrences  int
	LogicalAssetBytes int64
	RecipeBytes       int64
	OutputBytes       int64
	Concurrent        int
	WorkDuration      time.Duration
}

// DefaultLimits returns the standard export size and concurrency limits.
func DefaultLimits() Limits {
	return Limits{
		CollectionRecipes: MaximumSelectionRecipes,
		AssetOccurrences:  1_000,
		LogicalAssetBytes: 256 << 20,
		RecipeBytes:       16 << 20,
		OutputBytes:       288 << 20,
		Concurrent:        2,
		WorkDuration:      2 * time.Minute,
	}
}

func (l Limits) normalized() Limits {
	defaults := DefaultLimits()
	if l.CollectionRecipes == 0 {
		l.CollectionRecipes = defaults.CollectionRecipes
	}

	if l.AssetOccurrences == 0 {
		l.AssetOccurrences = defaults.AssetOccurrences
	}

	if l.LogicalAssetBytes == 0 {
		l.LogicalAssetBytes = defaults.LogicalAssetBytes
	}

	if l.RecipeBytes == 0 {
		l.RecipeBytes = defaults.RecipeBytes
	}

	if l.OutputBytes == 0 {
		l.OutputBytes = defaults.OutputBytes
	}

	if l.Concurrent == 0 {
		l.Concurrent = defaults.Concurrent
	}

	if l.WorkDuration == 0 {
		l.WorkDuration = defaults.WorkDuration
	}

	return l
}

// Option customizes export service policy.
type Option func(*Service)

// WithLimits replaces the default application resource policy.
func WithLimits(limits Limits) Option {
	return func(service *Service) { service.limits = limits }
}

// NewService composes export. Without an asset store, recipes that declare
// files are refused rather than exported incomplete.
func NewService(reader Library, blobs Assets, options ...Option) (*Service, error) {
	if reader == nil {
		return nil, errors.New("export needs a library to read")
	}

	resolver := &assetResolver{store: blobs}
	single := &srefExporter{assets: resolver}

	service := &Service{
		library:     reader,
		assets:      resolver,
		recipes:     map[Format]RecipeExporter{single.Format(): single},
		collections: map[Format]CollectionExporter{FormatSREFBundle: &srefBundleExporter{recipes: single}},
		limits:      DefaultLimits(),
	}
	for _, option := range options {
		option(service)
	}

	service.limits = service.limits.normalized()
	if service.limits.CollectionRecipes < 1 || service.limits.AssetOccurrences < 1 ||
		service.limits.LogicalAssetBytes < 1 || service.limits.RecipeBytes < 1 ||
		service.limits.OutputBytes < 1 || service.limits.Concurrent < 1 || service.limits.WorkDuration < 1 {
		return nil, errors.New("export limits must be positive")
	}

	service.work = make(chan struct{}, service.limits.Concurrent)

	return service, nil
}

// OutputByteLimit is the maximum number of bytes Write emits.
func (s *Service) OutputByteLimit() int64 { return s.limits.OutputBytes }

// Plan is a resolved export: exactly which revisions will be written, what the
// artifact is called, and what it is served as. Resolving is separated from
// writing so a caller can name the file and report a failure before a single
// byte of a download has been sent.
type Plan struct {
	Format    Format
	Filename  string
	MediaType string
	// Recipes is what the artifact will contain, at the revisions frozen for
	// it. Nothing is read from the library again while it is written.
	Recipes []RecipeSnapshot
	// Collection records that the reader asked for a collection artifact, so
	// a selection of one recipe is still a bundle.
	Collection bool
}

// Prepare freezes the export. Every revision is resolved, every referenced
// file is located and verified, and the artifact is named. After this the
// artifact can be written from what is in hand.
func (s *Service) Prepare(ctx context.Context, source Source, format Format) (Plan, error) {
	descriptor, known := DescriptorOf(format)
	if !known {
		return Plan{}, fmt.Errorf("%w: %q", ErrUnsupportedFormat, format)
	}

	if source == nil {
		return Plan{}, ErrNoRecipes
	}

	collection := source.Collection()
	if collection && !descriptor.SupportsCollection {
		return Plan{}, fmt.Errorf("%w: %s carries one recipe", ErrUnsupportedFormat, descriptor.Label)
	}

	if !collection && !descriptor.SupportsSingle {
		return Plan{}, fmt.Errorf("%w: %s carries a collection", ErrUnsupportedFormat, descriptor.Label)
	}

	release, err := s.acquire(ctx)
	if err != nil {
		return Plan{}, err
	}
	defer release()

	ctx, cancel := context.WithTimeout(ctx, s.limits.WorkDuration)
	defer cancel()

	snapshots, err := source.Resolve(ctx)
	if err != nil {
		return Plan{}, err
	}

	if len(snapshots) == 0 {
		return Plan{}, ErrNoRecipes
	}

	if collection && len(snapshots) > s.limits.CollectionRecipes {
		return Plan{}, fmt.Errorf("%w: configured export policy accepts at most %d recipes",
			ErrTooLarge, s.limits.CollectionRecipes)
	}

	var recipeBytes, assetBytes int64

	assetOccurrences := 0

	for _, snapshot := range snapshots {
		encoded, encodeErr := sref.Encode(snapshot.Recipe)
		if encodeErr != nil {
			return Plan{}, encodeErr
		}

		if !addWithin(&recipeBytes, int64(len(encoded)), s.limits.RecipeBytes) {
			return Plan{}, fmt.Errorf("%w: recipe documents exceed the %d-byte synchronous export limit", ErrTooLarge, s.limits.RecipeBytes)
		}

		for _, asset := range snapshot.Recipe.Assets {
			assetOccurrences++
			if assetOccurrences > s.limits.AssetOccurrences || asset.Size < 0 ||
				!addWithin(&assetBytes, asset.Size, s.limits.LogicalAssetBytes) {
				return Plan{}, fmt.Errorf("%w: referenced files exceed synchronous export policy", ErrTooLarge)
			}
		}
	}
	// ZIP framing and bundle manifests are bounded by the already-bounded
	// recipe and asset occurrence counts. Refuse work before verification and,
	// importantly, before an HTTP caller commits download headers.
	estimated := recipeBytes + assetBytes + 64<<10 + int64(len(snapshots))*16<<10 + int64(assetOccurrences)*2<<10
	if estimated < 0 || estimated > s.limits.OutputBytes {
		return Plan{}, fmt.Errorf("%w: estimated artifact exceeds the %d-byte synchronous export limit", ErrTooLarge, s.limits.OutputBytes)
	}

	// Every referenced file is checked now, so an export either produces a
	// complete artifact or fails before it starts. A bundle is all of its
	// recipes or none of them.
	verified := make(map[string]int64)
	for _, snapshot := range snapshots {
		if err := s.assets.verify(ctx, snapshot, verified); err != nil {
			return Plan{}, err
		}
	}

	name := source.Name()
	if name == "" && len(snapshots) == 1 {
		name = snapshots[0].Recipe.Title
	}

	fallback := ""
	if len(snapshots) == 1 {
		fallback = string(snapshots[0].Recipe.ID)
	}

	return Plan{
		Format:     format,
		Filename:   Filename(name, fallback, descriptor.Extension),
		MediaType:  descriptor.MediaType,
		Recipes:    snapshots,
		Collection: collection,
	}, nil
}

// Write produces the artifact the plan describes. It reads nothing from the
// library: a recipe edited while the bytes are being generated does not change
// what this artifact contains.
func (s *Service) Write(ctx context.Context, plan Plan, destination io.Writer) error {
	if len(plan.Recipes) == 0 {
		return ErrNoRecipes
	}

	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	ctx, cancel := context.WithTimeout(ctx, s.limits.WorkDuration)
	defer cancel()

	destination = &boundedWriter{ctx: ctx, destination: destination, remaining: s.limits.OutputBytes}
	if plan.Collection {
		exporter, offered := s.collections[plan.Format]
		if !offered {
			return fmt.Errorf("%w: %q carries one recipe", ErrUnsupportedFormat, plan.Format)
		}

		return exporter.ExportCollection(ctx, plan.Recipes, destination)
	}

	exporter, offered := s.recipes[plan.Format]
	if !offered {
		return fmt.Errorf("%w: %q carries a collection", ErrUnsupportedFormat, plan.Format)
	}

	if len(plan.Recipes) != 1 {
		return fmt.Errorf("%w: %q carries one recipe", ErrUnsupportedFormat, plan.Format)
	}

	return exporter.ExportRecipe(ctx, plan.Recipes[0], destination)
}

func (s *Service) acquire(ctx context.Context) (func(), error) {
	select {
	case s.work <- struct{}{}:
		return func() { <-s.work }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func addWithin(total *int64, value, limit int64) bool {
	if value < 0 || *total > math.MaxInt64-value || *total+value > limit {
		return false
	}

	*total += value

	return true
}

// boundedWriter is scoped to one Export call and checks cancellation through
// io.Writer's context-free contract.
//
//nolint:containedctx // the request context is never retained beyond Export
type boundedWriter struct {
	ctx         context.Context
	destination io.Writer
	remaining   int64
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}

	if int64(len(p)) > w.remaining {
		return 0, ErrTooLarge
	}

	n, err := w.destination.Write(p)
	w.remaining -= int64(n)

	return n, err //nolint:wrapcheck // transparent writers preserve short-write and destination error identity
}

// Export prepares and writes in one call, for a caller that is writing to a
// file rather than to a response it has to give headers to first.
func (s *Service) Export(ctx context.Context, source Source, format Format, destination io.Writer) (Plan, error) {
	plan, err := s.Prepare(ctx, source, format)
	if err != nil {
		return Plan{}, err
	}

	if err := s.Write(ctx, plan, destination); err != nil {
		return Plan{}, err
	}

	return plan, nil
}

// Library reports the reader the service resolves through, so a caller can
// build a source without holding a second reference to it.
func (s *Service) Library() Library { return s.library }
