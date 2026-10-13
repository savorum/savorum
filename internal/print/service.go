package print

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/savorum/savorum/internal/recipe"

	"github.com/savorum/savorum/internal/render/pdfinspect"
	"github.com/savorum/savorum/internal/units"
)

// Artifact is one generated document. It is produced on demand and never
// becomes part of a recipe: the same bytes serve the preview, the download,
// and printing, and nothing outlives the process that made them.
type Artifact struct {
	// ID is a deterministic signature of everything the document was made
	// from. Identical inputs produce the same identifier, which is what lets
	// it serve as a strong validator.
	ID    string
	PDF   []byte
	Pages int
	// Title names the artifact for a filename and for PDF metadata.
	Title  string
	Format PrintFormat
	// Warnings record what degraded without invalidating the document.
	Warnings []string
	// Recipes is what the document actually contains, at the revisions that
	// were frozen for it.
	Recipes []ArtifactRecipe

	// authorizationScope is deliberately not serialized. It binds a held
	// artifact to the service boundary that generated it.
	authorizationScope string
}

// ArtifactRecipe records exactly which version of a recipe was printed.
type ArtifactRecipe struct {
	EntryID  string
	Revision uint64
	Title    string
}

// Service turns a print source and a specification into a document. It owns
// every decision the renderer must not make.
type Service struct {
	renderer           DocumentRenderer
	catalog            units.Catalog
	assets             AssetOpener
	recipes            RecipeLibrary
	books              BookLibrary
	cache              *artifactCache
	authorizationScope string

	workload WorkloadLimits
	active   chan struct{}
	slots    chan struct{}
	mutex    sync.Mutex
	jobs     map[string]*renderJob
	stats    WorkloadStats
}

// NewService composes the print subsystem. The asset opener is optional: an
// instance without one prints recipes without their photographs rather than
// failing. The library readers are needed only by a caller that asks the
// service to build print sources for it.
func NewService(
	renderer DocumentRenderer,
	catalog units.Catalog,
	opener AssetOpener,
	recipes RecipeLibrary,
	books BookLibrary,
	options ...ServiceOption,
) (*Service, error) {
	if renderer == nil {
		return nil, errors.New("document renderer is required")
	}

	configured := serviceOptions{
		workload:           DefaultWorkloadLimits(),
		authorizationScope: "local-library",
	}

	for _, option := range options {
		if option != nil {
			if err := option(&configured); err != nil {
				return nil, err
			}
		}
	}

	service := &Service{
		renderer:           renderer,
		catalog:            catalog,
		assets:             opener,
		recipes:            recipes,
		books:              books,
		cache:              newArtifactCache(defaultCacheEntries, configured.workload.CacheBytes),
		authorizationScope: configured.authorizationScope,
		workload:           configured.workload,
		active:             make(chan struct{}, configured.workload.ConcurrentRenders),
		slots:              make(chan struct{}, configured.workload.ConcurrentRenders+configured.workload.QueuedRenders),
		jobs:               map[string]*renderJob{},
	}

	return service, nil
}

// Request is one print job: where the recipes come from and how they should be
// composed.
type Request struct {
	Source  PrintSource
	Options Options
}

// Render produces the document. Every recipe's revision is frozen first, so an
// edit during generation cannot mix versions.
func (s *Service) Render(ctx context.Context, request Request) (Artifact, error) {
	callerContext := ctx

	ctx, cancel := context.WithTimeout(ctx, s.workload.RequestTimeout)
	defer cancel()

	options, err := request.Options.Normalize()
	if err != nil {
		return Artifact{}, err
	}

	if request.Source == nil {
		return Artifact{}, ErrNoRecipes
	}

	snapshots, err := request.Source.Resolve(ctx, s.workload.Recipes)
	if err != nil {
		return Artifact{}, s.requestError(callerContext, ctx, err)
	}

	if len(snapshots) == 0 {
		return Artifact{}, ErrNoRecipes
	}

	if err := validateSnapshotBudget(snapshots, s.workload); err != nil {
		return Artifact{}, err
	}

	source := resolvedSourceIdentity(request.Source, snapshots)

	scope := s.requestScope(ctx)

	signature := renderSignature(s.renderer.Version(), scope, source, options, snapshots)
	if cached, found, err := s.cache.get(signature, scope); err != nil {
		return Artifact{}, err
	} else if found {
		return cached, nil
	}

	job, err := s.admit(renderRequest{
		signature: signature,
		scope:     scope,
		authority: captureRenderAuthority(ctx),
		title:     source.Title,
		options:   options,
		snapshots: snapshots,
	})
	if err != nil {
		return Artifact{}, err
	}

	artifact, err := s.await(ctx, job)

	return artifact, s.requestError(callerContext, ctx, err)
}

func (s *Service) requestError(caller, request context.Context, err error) error {
	if err == nil || caller.Err() != nil || !errors.Is(request.Err(), context.DeadlineExceeded) {
		return err
	}

	s.mutex.Lock()
	s.stats.RequestTimedOut++
	s.mutex.Unlock()

	return fmt.Errorf("%w: %w", ErrRequestTimeout, err)
}

func (s *Service) admit(request renderRequest) (*renderJob, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if existing := s.jobs[request.signature]; existing != nil {
		existing.waiters++
		s.stats.Coalesced++

		return existing, nil
	}

	select {
	case s.slots <- struct{}{}:
	default:
		s.stats.Rejected++
		return nil, ErrBusy
	}

	ctx, cancel := context.WithTimeout(request.authority.bind(context.Background()), s.workload.RenderTimeout)
	job := &renderJob{
		ctx: ctx, cancel: cancel, done: make(chan struct{}), waiters: 1, request: request,
	}
	s.jobs[request.signature] = job
	s.stats.Accepted++

	s.stats.Queued++
	go s.run(job)

	return job, nil
}

func (s *Service) run(job *renderJob) {
	select {
	case s.active <- struct{}{}:
		s.mutex.Lock()
		job.started = true
		s.stats.Queued--
		s.stats.Active++
		s.mutex.Unlock()
	case <-job.ctx.Done():
		s.finish(job, Artifact{}, job.ctx.Err())
		return
	}

	document, err := s.compose(job.ctx, job.request.title, job.request.options, job.request.snapshots)
	if err == nil {
		err = validateDocumentBudget(document, s.workload)
	}

	var pdf []byte
	if err == nil {
		pdf, err = s.renderer.Render(job.ctx, document)
	}

	if job.ctx.Err() != nil {
		err = job.ctx.Err()
	}

	if err != nil {
		<-s.active
		s.finish(job, Artifact{}, err)

		return
	}

	artifact := Artifact{
		ID:                 job.request.signature,
		PDF:                pdf,
		Pages:              pdfinspect.PageCount(pdf),
		Title:              document.Title,
		Format:             document.Format,
		Warnings:           document.Warnings,
		authorizationScope: job.request.scope,
	}
	for _, snapshot := range job.request.snapshots {
		artifact.Recipes = append(artifact.Recipes, ArtifactRecipe{
			EntryID:  string(snapshot.EntryID),
			Revision: snapshot.Revision,
			Title:    snapshot.Recipe.Title,
		})
	}

	<-s.active
	s.finish(job, artifact, nil)
}

func (s *Service) finish(job *renderJob, artifact Artifact, err error) {
	job.cancel()
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if job.started {
		s.stats.Active--
	} else {
		s.stats.Queued--
	}

	<-s.slots

	job.result = artifact

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		job.err = fmt.Errorf("%w: %w", ErrRenderTimeout, err)
		s.stats.TimedOut++
		s.stats.Failed++
	case errors.Is(err, context.Canceled):
		job.err = err
		s.stats.Canceled++
		s.stats.Failed++
	case err != nil:
		job.err = err
		s.stats.Failed++
	default:
		if cacheErr := s.cache.put(job.request.signature, artifact); cacheErr != nil {
			job.err = cacheErr
			s.stats.Failed++
		} else {
			s.stats.Completed++
		}
	}

	delete(s.jobs, job.request.signature)
	close(job.done)
}

func (s *Service) await(ctx context.Context, job *renderJob) (Artifact, error) {
	select {
	case <-job.done:
		return job.result, job.err
	case <-ctx.Done():
		select {
		case <-job.done:
			return job.result, job.err
		default:
		}

		s.mutex.Lock()
		select {
		case <-job.done:
			s.mutex.Unlock()
			return job.result, job.err
		default:
		}

		job.waiters--
		if job.waiters == 0 {
			job.cancel()
		}
		s.mutex.Unlock()

		return Artifact{}, ctx.Err()
	}
}

// WorkloadStats reports bounded render admission without exposing job data.
func (s *Service) WorkloadStats() WorkloadStats {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.stats
}

// compose resolves the frozen snapshots into the presentation model. After
// this, nothing about the recipe domain is left for anyone else to decide.
func (s *Service) compose(ctx context.Context, title string, options Options, snapshots []RecipeSnapshot) (PrintDocument, error) {
	format, err := formatOf(options)
	if err != nil {
		return PrintDocument{}, err
	}

	locale := documentLocale(options.Locale, snapshots)

	document := PrintDocument{
		Title:     strings.TrimSpace(title),
		Labels:    printMessages(locale),
		Direction: labelDirection(locale),
		Language:  locale,
		Digits:    (presenter{locale: locale}).localizeNumber("0123456789"),
		Format:    format,
		Options:   options,
		Assets:    map[string][]byte{},
	}
	if document.Title == "" {
		document.Title = snapshots[0].Recipe.Title
	}

	if sheet, imposed := options.Output.SheetSize(); imposed {
		plan, err := planSheet(options.Size.Dimensions(), sheet.Dimensions())
		if err != nil {
			return PrintDocument{}, err
		}

		document.Sheet = &plan
	}

	imageBytes, imagePixels := 0, 0
	images := imageResolver{opener: s.assets, size: options.Size, limits: s.workload, images: map[string]PrintImage{}}

	for index, snapshot := range snapshots {
		if err := ctx.Err(); err != nil {
			return PrintDocument{}, err
		}

		scale, err := scaleFor(options, snapshot)
		if err != nil {
			return PrintDocument{}, err
		}

		presenter := newPresenter(s.catalog, options.Measurement, scale, locale)
		presenter.microwaveOutputWatts = options.MicrowaveOutputWatts

		presented := presenter.present(snapshot, options.Content)
		if options.Image != ImageOmit {
			name := fmt.Sprintf("image-%d", index)

			image, bytes, warning := images.resolve(ctx, snapshot.Recipe, name)
			if warning != "" {
				document.Warnings = append(document.Warnings, warning)
			}

			if image != nil && bytes != nil {
				imageBytes += len(bytes)

				imagePixels += image.Width * image.Height
				if imageBytes > s.workload.ImageBytes || imagePixels > s.workload.ImagePixels {
					return PrintDocument{}, fmt.Errorf("%w: photographs exceed the print memory budget", ErrTooLarge)
				}

				document.Assets[image.Name] = bytes
			}

			presented.Image = image
		}

		document.Recipes = append(document.Recipes, presented)
	}

	return document, nil
}

func formatOf(options Options) (PrintFormat, error) {
	size := options.Size.Dimensions()
	if size.Width.Unit == "" {
		return PrintFormat{}, fmt.Errorf("%w: unknown size %q", ErrInvalidOptions, options.Size)
	}

	format := PrintFormat{
		Size:   string(options.Size),
		Family: string(options.Size.Family()),
		Width:  size.Width,
		Height: size.Height,
	}
	if sheet, imposed := options.Output.SheetSize(); imposed {
		// The page becomes the sheet, and the card keeps its own exact
		// dimensions so imposition can never scale it to fill anything.
		card := size
		format.Card = &card
		format.Width = sheet.Dimensions().Width
		format.Height = sheet.Dimensions().Height
	}

	return format, nil
}

// scaleFor decides the batch one recipe is printed at. A remembered batch is
// per recipe, so a selection prints each recipe at the amount its reader
// normally makes rather than at one factor for the whole document.
func scaleFor(options Options, snapshot RecipeSnapshot) (*big.Rat, error) {
	switch options.Scale {
	case ScalePreferred:
		return parseScale(snapshot.PreferredScale)
	case ScaleCustom:
		return parseScale(options.CustomScale)
	case ScaleOriginal:
		return big.NewRat(1, 1), nil
	default:
		return big.NewRat(1, 1), nil
	}
}

// renderSignature identifies a document by everything it was made from: the
// renderer and its templates, the exact revision of every recipe, and the
// complete specification. Anything that would change a byte of the output
// changes the signature.
func renderSignature(version, scope string, source SourceIdentity, options Options, snapshots []RecipeSnapshot) string {
	type recipeKey struct {
		Entry    string `json:"entry"`
		Revision uint64 `json:"revision"`
		Scale    string `json:"scale,omitempty"`
	}

	payload := struct {
		Version string         `json:"version"`
		Scope   string         `json:"scope"`
		Source  SourceIdentity `json:"source"`
		Options Options        `json:"options"`
		Recipes []recipeKey    `json:"recipes"`
	}{Version: version + "+shared-print-images-v1+" + printMessageVersion() + "+" + units.LocalizedLabelVersion(), Scope: scope, Source: source, Options: options}
	for _, snapshot := range snapshots {
		payload.Recipes = append(payload.Recipes, recipeKey{
			Entry:    string(snapshot.EntryID),
			Revision: snapshot.Revision,
			Scale:    string(snapshot.PreferredScale),
		})
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		// Every field is a plain value, so this cannot fail; a signature that
		// somehow could not be computed must not collide with a real one.
		encoded = []byte(version + "-unencodable")
	}

	sum := sha256.Sum256(encoded)

	return hex.EncodeToString(sum[:])
}

func resolvedSourceIdentity(source PrintSource, snapshots []RecipeSnapshot) SourceIdentity {
	identity := SourceIdentity{Kind: "custom", Title: strings.TrimSpace(source.Title())}
	if identified, ok := source.(identifiedPrintSource); ok {
		identity = identified.Identity()
		identity.Kind = strings.TrimSpace(identity.Kind)
		identity.ID = strings.TrimSpace(identity.ID)
		identity.Title = strings.TrimSpace(identity.Title)
	}

	if identity.Kind == "" {
		identity.Kind = "custom"
	}

	if identity.Title == "" && len(snapshots) > 0 {
		identity.Title = strings.TrimSpace(snapshots[0].Recipe.Title)
	}

	return identity
}

// Held returns an offline artifact generated without a request principal or
// library context. Browser and other principal-aware callers must use HeldFor.
func (s *Service) Held(id string) (Artifact, bool, error) {
	return s.cache.get(id, s.authorizationScope)
}

// Library reports the reader the service resolves recipes through, so a caller
// can build a print source without holding a second reference to it.
func (s *Service) Library() RecipeLibrary { return s.recipes }

// RecipeLimit is the most recipes one document may hold.
func (s *Service) RecipeLimit() int { return s.workload.Recipes }

// Books reports the reader the service resolves a book through.
func (s *Service) Books() BookLibrary { return s.books }

// HeldFor returns an artifact only for its originating user and library scope.
// HTTP callers must use this method rather than the context-free offline Held.
func (s *Service) HeldFor(ctx context.Context, id string) (Artifact, bool, error) {
	return s.cache.get(id, s.requestScope(ctx))
}

// labelDirection is the direction of the catalog the labels were taken from,
// so English fallback labels are never set right to left.
func labelDirection(locale string) string {
	return recipe.LanguageDirection(catalogLocale(locale))
}
