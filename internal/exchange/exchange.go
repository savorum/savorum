// Package exchange brings recipes into a library from SREF. It is the one
// place that needs both the library and the asset store, because a package
// carries photographs and the recipe that names them must not be stored
// before they are. Export is the other direction and lives in
// internal/export.
package exchange

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"iter"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/imaging"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/importer/webrecipe"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/sref/archivepolicy"
	"github.com/savorum/savorum/internal/sref/bundle"
)

// Library is the part of the library service exchange needs.
type Library interface {
	Get(ctx context.Context, id library.EntryID) (library.Entry, error)
	PreflightImportIdentities(ctx context.Context, documents []recipe.Document, policy library.DuplicatePolicy) ([]bool, error)
	ImportRecipes(ctx context.Context, documents []recipe.Document, policy library.DuplicatePolicy) ([]library.ImportResult, error)
}

type atomicSourceLibrary interface {
	ImportRecipesWithSources(context.Context, []recipe.Document, library.DuplicatePolicy, [][]library.SourceAttachment) ([]library.ImportResult, error)
	ReconcileImportRecipesWithSources(context.Context, []recipe.Document, []library.ImportResolution, [][]library.SourceAttachment) ([]library.ImportResult, error)
}

// SourceRefreshLibrary is the optional user-facing reconciliation capability.
type SourceRefreshLibrary interface {
	CheckSourceUpdates(context.Context, library.EntryID, recipe.Document, uint64) (library.SourceRefreshPreview, error)
}

// SourceAttachments is optional application-owned persistence for original
// import files. It is kept out of Library so source bytes never become recipe
// content or portable exchange data.
type SourceAttachments interface {
	AttachSource(context.Context, library.SourceAttachment) error
}

// ImportedMetadataLibrary is deliberately optional so package-only exchange
// implementations retain the small recipe persistence contract. Production
// library.Service implements it; this is the policy seam for local metadata.
type ImportedMetadataLibrary interface {
	ApplyImportedLibraryMetadata(context.Context, string, library.ImportedLibraryMetadata) error
}

// Assets is the part of the asset service exchange needs. A package carries
// bytes, so importing one registers them before the recipe that names them.
type Assets interface {
	Ingest(ctx context.Context, source io.Reader, options assets.IngestOptions) (assets.Blob, error)
	Open(ctx context.Context, id assets.ID) (assets.Blob, io.ReadCloser, error)
}

// Service coordinates importer adapters, library mutations, and staged recipe assets.
type Service struct {
	library             Library
	assets              Assets
	sources             SourceAttachments
	registry            importer.Registry
	limits              sref.Limits
	bundleLimits        bundle.Limits
	fetcher             RemoteAssetFetcher
	previewsMu          sync.Mutex
	previews            map[string]*previewSession
	previewLimits       PreviewLimits
	previewSlots        chan struct{}
	archiveSlots        chan struct{}
	remoteSlots         chan struct{}
	remoteLimits        RemoteImportLimits
	previewStats        PreviewStats
	pendingPreviewBytes int64
	previewScope        func(context.Context) string
	ingestAttempts      int
	ingestRetryDelay    time.Duration
	pending             pendingPhotographs
}

// PreviewSourceRefreshURL fetches and interprets the current source through
// the same outbound policy as ordinary URL imports, then hands the candidate
// to the library's semantic refresh planner. It performs no write.
func (s *Service) PreviewSourceRefreshURL(ctx context.Context, id library.EntryID, rawURL string, expectedRevision uint64) (library.SourceRefreshPreview, error) {
	preview, err := s.PreviewURL(ctx, rawURL)
	if err != nil {
		return library.SourceRefreshPreview{}, err
	}

	if len(preview.Items) != 1 {
		s.CancelPreview(preview.ID)
		return library.SourceRefreshPreview{}, fmt.Errorf("source refresh requires exactly one recipe candidate")
	}

	refresh, ok := s.library.(SourceRefreshLibrary)
	if !ok {
		s.CancelPreview(preview.ID)
		return library.SourceRefreshPreview{}, errors.New("source refresh is unavailable")
	}

	current, err := s.library.Get(ctx, id)
	if err != nil {
		s.CancelPreview(preview.ID)
		return library.SourceRefreshPreview{}, err
	}
	// A source may publish a new portable ID when it is refreshed. The local
	// library entry remains the trusted identity for this operation.
	candidate := preview.Items[0].Recipe

	candidate.ID = current.Recipe.ID
	if err := s.materializeRefreshAssets(ctx, preview.ID, &candidate); err != nil {
		s.CancelPreview(preview.ID)
		return library.SourceRefreshPreview{}, err
	}

	s.CancelPreview(preview.ID)

	return refresh.CheckSourceUpdates(ctx, id, candidate, expectedRevision)
}

// materializeRefreshAssets uses the protected importer fetcher and normal asset
// store for a source refresh. The temporary preview is canceled by the caller;
// any bytes already stored are content addressed and collectable if the person
// abandons the review before applying it.
func (s *Service) materializeRefreshAssets(ctx context.Context, previewID string, document *recipe.Document) error {
	s.previewsMu.Lock()
	session := s.previews[previewID]

	var candidate *previewCandidate
	if session != nil && session.scope == s.previewScope(ctx) {
		candidate = session.candidates["item-1"]
	}
	s.previewsMu.Unlock()

	if session == nil || candidate == nil {
		return ErrPreviewNotFound
	}

	item := candidate.item
	items := []ImportItem{item}

	stagedOutcomes := s.stageDirectRemoteAssets(ctx, items, []bool{true})
	for index, remote := range items[0].RemoteAssets {
		identity, err := canonicalRemoteURL(remote.URL)
		if err != nil {
			continue
		}

		staged, ok := items[0].stagedRemote[identity]
		if !ok || stagedOutcomes[identity] != remoteStaged {
			continue
		}

		fetched := FetchedAsset{Body: io.NopCloser(bytes.NewReader(staged.bytes)), MediaType: staged.mediaType, Size: int64(len(staged.bytes))}
		blob, ingestErr := s.storePhotograph(ctx, fetched)
		closeErr := fetched.Body.Close()

		if ingestErr != nil {
			return fmt.Errorf("store refreshed photograph: %w", ingestErr)
		}

		if closeErr != nil {
			return fmt.Errorf("close refreshed photograph: %w", closeErr)
		}

		logicalID := remote.LogicalID
		if logicalID == "" {
			logicalID = fmt.Sprintf("refreshed-image-%d", index+1)
		}

		document.Assets = append(document.Assets, recipe.Asset{ID: logicalID, Path: "assets/" + logicalID + extensionForMediaType(blob.MediaType), MediaType: blob.MediaType, SHA256: blob.ContentHash, Size: blob.Size, Role: remote.Role, Alt: remote.Alt, Caption: remote.Caption, Creator: remote.Creator, Credit: remote.Credit, Rights: remote.Rights, License: remote.License, SourceURL: remote.URL})
		if remote.RecipeImage {
			document.ImageRefs = append(document.ImageRefs, logicalID)
			if document.PrimaryImage == "" {
				document.PrimaryImage = logicalID
			}
		}
	}

	return nil
}

// ImportItem is one bare recipe document or self-contained package in a batch.
type ImportItem struct {
	History                 library.ImportContext
	Data                    []byte
	Package                 bool
	Opened                  *sref.Package
	Document                *recipe.Document
	RemoteAssets            []importer.RemoteAsset
	EmbeddedAssets          []importer.EmbeddedAsset
	PortableAssetReferences bool
	stagedRemote            map[string]stagedRemoteAsset
	SourceBytes             []byte
	SourceFilename          string
	SourceMediaType         string
	RetainSource            bool
}

type stagedRemoteAsset struct {
	bytes      []byte
	mediaType  string
	sha256     string
	cropBank   assets.CropHashBank
	phashValid bool
	width      int
	height     int
}

func (s *stagedRemoteAsset) ensureMetadata() {
	if s.sha256 == "" && len(s.bytes) > 0 {
		sum := sha256.Sum256(s.bytes)
		s.sha256 = hex.EncodeToString(sum[:])
	}

	if !s.phashValid && len(s.bytes) > 0 {
		if bank, w, h, err := assets.ComputeBytesCropHashBank(s.bytes); err == nil {
			s.cropBank = bank
			s.phashValid = true
			s.width = w
			s.height = h
		}
	}
}

// RemoteImportLimits bound untrusted public-network work for one import.
// Redirects are additionally capped at webrecipe.MaximumRedirects per request,
// so Requests also bounds aggregate redirect amplification.
//
// Requests, Bytes, and Duration bound one batch of staging rather than a whole
// commit; a large import is staged as a sequence of batches so a single budget
// never silently discards the photographs at the back of the queue.
//
// AssetsPerRecipe keeps the aggregate proportional: outbound work for one
// commit cannot exceed the selected recipes times this number, so an uploaded
// file naming thousands of image URLs cannot turn one import into an
// amplifier. Preview retrieval is bounded separately in PreviewLimits.
type RemoteImportLimits struct {
	AssetsPerRecipe int
	Requests        int
	Bytes           int64
	Concurrent      int
	Duration        time.Duration
}

// DefaultRemoteImportLimits bounds remote fetch count, size, concurrency, and elapsed time.
func DefaultRemoteImportLimits() RemoteImportLimits {
	return RemoteImportLimits{
		AssetsPerRecipe: 12, Requests: 32, Bytes: 64 << 20,
		Concurrent: 2, Duration: time.Minute,
	}
}

// WithRemoteImportLimits replaces the resource policy used for remote recipe and asset fetches.
func WithRemoteImportLimits(limits RemoteImportLimits) Option {
	return func(service *Service) { service.remoteLimits = limits }
}

// WithIngestRetry replaces how long an import will wait for asset storage that
// is momentarily busy. Tests use it to make the wait observable; production has
// no reason to change it.
func WithIngestRetry(attempts int, delay time.Duration) Option {
	return func(service *Service) {
		service.ingestAttempts, service.ingestRetryDelay = attempts, delay
	}
}

// FetchedAsset is an asset fetched from a remote recipe source.
type FetchedAsset struct {
	Body      io.ReadCloser
	MediaType string
	Size      int64
}

// ErrRemoteAssetUnusable marks a fetch failure no retry can change: an address
// that is not a usable address, a server that says the photograph is not there,
// or a response that is not an image.
//
// Everything else is transient and retried, including errors from a fetcher
// that does not classify them, so a photograph one attempt away is never
// reported as lost.
var ErrRemoteAssetUnusable = errors.New("remote address cannot yield a photograph")

// maximumRemoteAttempts bounds retries per photograph within one commit, so
// outbound work never exceeds the addresses in the selection times this number.
const maximumRemoteAttempts = 3

// defaultIngestAttempts and defaultIngestRetryDelay pace an import against the
// asset service's per-minute upload meter. The window is a minute, so the wait
// is sized to outlast one rather than to retry busily.
const (
	defaultIngestAttempts   = 4
	defaultIngestRetryDelay = 20 * time.Second
)

// RemoteAssetFetcher retrieves assets referenced by remote recipe sources.
type RemoteAssetFetcher interface {
	Fetch(ctx context.Context, rawURL string) (FetchedAsset, error)
}

// Option configures an exchange Service before it begins serving requests.
type Option func(*Service)

// WithSourceAttachments enables explicit retention of uploaded originals.
// Without it imports continue to work and no source file is retained.
func WithSourceAttachments(store SourceAttachments) Option {
	return func(service *Service) { service.sources = store }
}

// WithPreviewScopeResolver binds staged previews to the current library and
// principal. The default is the single local scope; authenticated deployments
// can provide their stable authorization scope without changing preview data.
func WithPreviewScopeResolver(resolve func(context.Context) string) Option {
	return func(service *Service) { service.previewScope = resolve }
}

// WithRemoteAssetFetcher supplies the policy-enforcing fetcher used for referenced remote assets.
func WithRemoteAssetFetcher(fetcher RemoteAssetFetcher) Option {
	return func(service *Service) { service.fetcher = fetcher }
}

// WithImporterRegistry replaces the compiled adapter registry. It is useful to
// applications that compose additional built-in adapters and to contract
// tests; adapters still receive no persistence handles.
func WithImporterRegistry(registry importer.Registry) Option {
	return func(service *Service) { service.registry = registry }
}

// WithSREFPackageLimits replaces the default package reader resource policy.
func WithSREFPackageLimits(limits sref.Limits) Option {
	return func(service *Service) { service.limits = limits }
}

// WithSREFBundleLimits replaces the default bundle reader resource policy.
func WithSREFBundleLimits(limits bundle.Limits) Option {
	return func(service *Service) { service.bundleLimits = limits }
}

// WithPreviewLimits replaces the default import-preview resource policy.
func WithPreviewLimits(limits PreviewLimits) Option {
	return func(service *Service) { service.previewLimits = limits }
}

// WithConcurrentArchiveReaders bounds simultaneous package and bundle parsing.
// Production defaults to two; the option primarily supports constrained hosts
// and deterministic admission tests.
func WithConcurrentArchiveReaders(readers int) Option {
	return func(service *Service) {
		if readers > 0 {
			service.archiveSlots = make(chan struct{}, readers)
		}
	}
}

// NewService creates an exchange coordinator backed by the provided library and asset stores.
func NewService(recipes Library, blobs Assets, options ...Option) *Service {
	service := &Service{
		library: recipes, assets: blobs, limits: sref.DefaultLimits(), bundleLimits: bundle.DefaultLimits(),
		fetcher: newHTTPRemoteAssetFetcher(), previews: map[string]*previewSession{}, previewLimits: DefaultPreviewLimits(),
		previewScope:   func(context.Context) string { return "local" },
		remoteLimits:   DefaultRemoteImportLimits(),
		ingestAttempts: defaultIngestAttempts, ingestRetryDelay: defaultIngestRetryDelay,
	}

	lifetime, stop := context.WithCancel(context.Background())
	service.pending = pendingPhotographs{lifetime: lifetime, stop: stop, queued: map[library.PendingPhotographs]bool{}}

	service.archiveSlots = make(chan struct{}, 2)
	for _, option := range options {
		option(service)
	}

	if len(service.registry.Formats()) == 0 {
		service.registry = importer.DefaultRegistryWithSREFLimits(service.limits, service.bundleLimits)
	}

	if service.previewScope == nil {
		service.previewScope = func(context.Context) string { return "local" }
	}

	service.previewLimits = service.previewLimits.normalized()
	service.previewSlots = make(chan struct{}, service.previewLimits.ConcurrentPreviews)

	defaults := DefaultRemoteImportLimits()
	if service.remoteLimits.AssetsPerRecipe <= 0 {
		service.remoteLimits.AssetsPerRecipe = defaults.AssetsPerRecipe
	}

	if service.remoteLimits.Requests <= 0 {
		service.remoteLimits.Requests = defaults.Requests
	}

	if service.remoteLimits.Bytes <= 0 {
		service.remoteLimits.Bytes = defaults.Bytes
	}

	if service.remoteLimits.Concurrent <= 0 {
		service.remoteLimits.Concurrent = defaults.Concurrent
	}

	if service.remoteLimits.Duration <= 0 {
		service.remoteLimits.Duration = defaults.Duration
	}

	service.remoteSlots = make(chan struct{}, service.remoteLimits.Concurrent)

	return service
}

// ImportExternal brings decoded third-party exports through the same
// validation, duplicate, asset, and persistence boundary as SREF packages.
func (s *Service) ImportExternal(ctx context.Context, decoded []importer.ImportBatch, policy library.DuplicatePolicy) ([]library.ImportResult, error) {
	var items []ImportItem

	metadata := map[recipe.RecipeID]library.ImportedLibraryMetadata{}
	diagnostics := map[recipe.RecipeID][]string{}

	for _, batch := range decoded {
		for _, imported := range batch.Recipes {
			document := imported.Document
			item := ImportItem{
				Document: &document, RemoteAssets: imported.Assets,
				History:        historyForImport(batch, imported),
				EmbeddedAssets: imported.EmbeddedAssets, PortableAssetReferences: imported.PortableAssetReferences,
			}

			metadata[document.ID] = toLibraryMetadata(imported.LibraryMetadata)
			for _, diagnostic := range append(append([]importer.Diagnostic(nil), batch.Diagnostics...), imported.Diagnostics...) {
				diagnostics[document.ID] = append(diagnostics[document.ID], diagnostic.Message)
			}

			items = append(items, item)
		}
	}

	results, err := s.Import(ctx, items, policy)
	if err != nil {
		return nil, err
	}

	return s.finishExternalImport(ctx, results, metadata, diagnostics)
}

// ImportSources is the common orchestration entry point. Every transport hands
// source inputs to the same registry and every resulting candidate then passes
// through the same validation, duplicate, asset, metadata, and atomic commit
// policy.
func (s *Service) ImportSources(ctx context.Context, inputs []importer.Input, options importer.Options, policy library.DuplicatePolicy) ([]library.ImportResult, error) {
	if len(inputs) == 0 || len(inputs) > library.MaximumImportRecipes {
		return nil, fmt.Errorf("an import carries between 1 and %d sources", library.MaximumImportRecipes)
	}

	return s.ImportSourceSequence(ctx, func(yield func(importer.Input, error) bool) {
		for _, input := range inputs {
			if !yield(input, nil) {
				return
			}
		}
	}, options, policy)
}

// ImportSourceSequence decodes sources as they arrive and commits their recipes
// atomically after the sequence succeeds. It uses the same archive admission and
// validation as ImportSources, without retaining each source's original bytes.
func (s *Service) ImportSourceSequence(ctx context.Context, inputs iter.Seq2[importer.Input, error], options importer.Options, policy library.DuplicatePolicy) ([]library.ImportResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	options.WorkBudget = archivepolicy.NewChildBudget(sref.DefaultLimits().WorkBytes, options.WorkBudget)

	var batches []importer.ImportBatch

	for input, err := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if err != nil {
			return nil, err
		}

		if len(batches) >= library.MaximumImportRecipes {
			return nil, fmt.Errorf("an import carries between 1 and %d sources", library.MaximumImportRecipes)
		}

		batch, err := s.decodeSource(ctx, input, options)
		if err != nil {
			return nil, err
		}

		batches = append(batches, batch)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if len(batches) == 0 {
		return nil, fmt.Errorf("an import carries between 1 and %d sources", library.MaximumImportRecipes)
	}

	return s.ImportExternal(ctx, batches, policy)
}

// ProbeSources reports the adapters orchestration would select without
// interpreting or committing the sources. Transports can present format and
// capability information without owning detection rules.
func (s *Service) ProbeSources(ctx context.Context, inputs []importer.Input) ([]importer.Format, error) {
	formats := make([]importer.Format, 0, len(inputs))
	for _, input := range inputs {
		format, _, err := s.registry.Probe(ctx, input)
		if err != nil {
			return nil, err
		}

		formats = append(formats, format)
	}

	return formats, nil
}

func (s *Service) decodeSource(ctx context.Context, input importer.Input, options importer.Options) (importer.ImportBatch, error) {
	archive := len(input.Data) >= 4 && bytes.Equal(input.Data[:4], []byte("PK\x03\x04"))
	if !archive {
		return s.registry.Import(ctx, input, options)
	}

	release, err := s.acquireArchiveReader(ctx)
	if err != nil {
		return importer.ImportBatch{}, err
	}
	defer release()

	return s.registry.Import(ctx, input, options)
}

func (s *Service) finishExternalImport(
	ctx context.Context,
	results []library.ImportResult,
	metadata map[recipe.RecipeID]library.ImportedLibraryMetadata,
	warnings map[recipe.RecipeID][]string,
) ([]library.ImportResult, error) {
	for index := range results {
		results[index].Warnings = append(results[index].Warnings, warnings[results[index].RecipeID]...)
	}

	applier, ok := s.library.(ImportedMetadataLibrary)
	if !ok {
		return results, nil
	}

	for index, result := range results {
		if result.Outcome == library.ImportSkipped {
			continue
		}

		importedMetadata, ok := metadata[result.RecipeID]
		if !ok {
			continue
		}

		// The recipe is already stored, so metadata this library will not hold
		// becomes a diagnostic on that recipe rather than failing the batch.
		if err := applier.ApplyImportedLibraryMetadata(ctx, string(result.EntryID), importedMetadata); err != nil {
			results[index].Warnings = append(results[index].Warnings,
				"This recipe was imported, but the library details the source carried with it were not: "+err.Error()+".")
		}
	}

	return results, nil
}

// ImportURL brings a recipe web page through the same persistence boundary as
// uploaded exports. Fetching and extraction happen inside the importer so that
// the address travels with the page: which of several recipes a page is about
// is a question only the requested address can answer. The error it returns
// stays classified, so a site that refused and a page with no recipe on it do
// not arrive here as the same failure.
func (s *Service) ImportURL(ctx context.Context, rawURL string, policy library.DuplicatePolicy) ([]library.ImportResult, error) {
	return s.ImportSources(ctx, []importer.Input{{URL: rawURL}}, importer.Options{}, policy)
}

func toLibraryMetadata(value importer.LibraryMetadata) library.ImportedLibraryMetadata {
	result := library.ImportedLibraryMetadata{Favorite: value.Favorite, Rating: value.Rating, PersonalNote: value.PersonalNote}
	if value.ImportedCookCount != nil {
		result.CookCount = &library.ImportedCookCountBaseline{Count: value.ImportedCookCount.Count, Source: value.ImportedCookCount.Source, SourceKey: value.ImportedCookCount.SourceKey}
	}

	return result
}

// ImportDocuments brings bare SREF recipe documents in.
func (s *Service) ImportDocuments(ctx context.Context, documents [][]byte, policy library.DuplicatePolicy) ([]library.ImportResult, error) {
	inputs := make([]importer.Input, len(documents))
	for index, data := range documents {
		inputs[index] = importer.Input{Name: "recipe.json", MediaType: "application/json", Data: data}
	}

	return s.ImportSources(ctx, inputs, importer.Options{}, policy)
}

// Import validates every input and duplicate decision before staging package
// assets, then commits all recipe creates and replacements atomically.
func (s *Service) Import(ctx context.Context, items []ImportItem, policy library.DuplicatePolicy) ([]library.ImportResult, error) {
	return s.importItems(ctx, ctx, items, policy, nil)
}

// importItems takes two contexts: ctx bounds the commit, and staging bounds
// only the outbound photograph fetches. Cancelling staging stops outbound work
// without tearing down a commit already writing; unfetched photographs keep
// their addresses as deferred. Callers with nothing to separate pass ctx twice.
func (s *Service) importItems(ctx, staging context.Context, items []ImportItem, policy library.DuplicatePolicy, resolutions []library.ImportResolution) ([]library.ImportResult, error) {
	if len(items) == 0 || len(items) > library.MaximumImportRecipes {
		return nil, fmt.Errorf("an import carries between 1 and %d recipes", library.MaximumImportRecipes)
	}

	recipes, packages, release, err := s.decodeImportItems(ctx, items)
	if err != nil {
		return nil, err
	}

	if release != nil {
		defer release()
	}

	decisions, err := s.importDecisions(ctx, recipes, policy, resolutions)
	if err != nil {
		return nil, err
	}

	stagingOutcomes := s.stageDirectRemoteAssets(staging, items, decisions)
	if err := s.ingestCarriedAssets(ctx, items, packages, decisions); err != nil {
		return nil, err
	}

	dropped, deferred, unresolved := s.materializeRemoteAssets(ctx, recipes, items, decisions, stagingOutcomes)
	for index := range recipes {
		retainRemoteReferences(&recipes[index], deferred[index], unresolved[index])
	}

	ctx = library.WithImportContexts(ctx, importHistory(items, dropped, deferred))

	results, err := s.commitImportedRecipes(ctx, recipes, items, decisions, policy, resolutions)
	if err != nil {
		return nil, err
	}

	if len(results) != len(dropped) {
		return nil, fmt.Errorf("library returned %d results for %d imported recipes", len(results), len(dropped))
	}

	decorateAssetResults(results, dropped, deferred)

	var pending []library.PendingPhotographs

	for index, photographs := range deferred {
		if len(photographs) > 0 {
			pending = append(pending, library.PendingPhotographs{LibraryID: access.LibraryID(ctx), EntryID: results[index].EntryID})
		}
	}

	s.queuePendingPhotographs(pending)

	return results, nil
}

type decodedPackageItem struct {
	index       int
	packageData sref.Package
}

func (s *Service) decodeImportItems(ctx context.Context, items []ImportItem) ([]recipe.Document, []decodedPackageItem, func(), error) {
	var (
		budget  *archivepolicy.Budget
		release func()
	)

	for _, item := range items {
		if item.Package && item.Opened == nil {
			budget = archivepolicy.NewBudget(s.limits.WorkBytes)

			var err error

			release, err = s.acquireArchiveReader(ctx)
			if err != nil {
				return nil, nil, nil, err
			}

			break
		}
	}

	recipes := make([]recipe.Document, 0, len(items))

	packages := make([]decodedPackageItem, 0, len(items))
	for index, item := range items {
		document, opened, err := s.decodeImportItem(item, budget)
		if err != nil {
			if release != nil {
				release()
			}

			return nil, nil, nil, err
		}

		recipes = append(recipes, document)
		if opened != nil {
			packages = append(packages, decodedPackageItem{index: index, packageData: *opened})
		}
	}

	return recipes, packages, release, nil
}

func (s *Service) decodeImportItem(item ImportItem, budget *archivepolicy.Budget) (recipe.Document, *sref.Package, error) {
	if item.Opened != nil {
		return item.Opened.Recipe, item.Opened, nil
	}

	if item.Document != nil {
		return *item.Document, nil, nil
	}

	if item.Package {
		opened, err := sref.ReadPackageWithBudget(item.Data, s.limits, budget)
		return opened.Recipe, &opened, err
	}

	document, err := sref.Decode(item.Data)

	return document, nil, err
}

func (s *Service) importDecisions(ctx context.Context, recipes []recipe.Document, policy library.DuplicatePolicy, resolutions []library.ImportResolution) ([]bool, error) {
	if resolutions == nil {
		return s.library.PreflightImportIdentities(ctx, recipes, policy)
	}

	if len(resolutions) != len(recipes) {
		return nil, fmt.Errorf("reconciled import needs one decision per recipe")
	}

	decisions := make([]bool, len(recipes))
	for index, resolution := range resolutions {
		decisions[index] = resolution.Action != library.ImportKeep
	}

	return decisions, nil
}

func (s *Service) ingestCarriedAssets(ctx context.Context, items []ImportItem, packages []decodedPackageItem, decisions []bool) error {
	for _, opened := range packages {
		if decisions[opened.index] {
			if err := s.ingestPackageAssets(ctx, opened.packageData.Assets); err != nil {
				return err
			}
		}
	}

	for index := range items {
		if decisions[index] {
			if err := s.ingestAdapterAssets(ctx, items[index].EmbeddedAssets); err != nil {
				return err
			}
		}
	}

	return nil
}

// packageAsset mirrors the fields shared by package and adapter assets.
type packageAsset struct {
	ID        string
	Bytes     []byte
	SHA256    string
	MediaType string
}

func (s *Service) ingestPackageAssets(ctx context.Context, assets []sref.PackageAsset) error {
	values := make([]packageAsset, 0, len(assets))
	for _, asset := range assets {
		values = append(values, packageAsset{ID: asset.ID, Bytes: asset.Bytes, SHA256: asset.SHA256, MediaType: asset.MediaType})
	}

	return s.ingestCarriedAssetValues(ctx, values)
}

func (s *Service) ingestAdapterAssets(ctx context.Context, assets []importer.EmbeddedAsset) error {
	values := make([]packageAsset, 0, len(assets))
	for _, asset := range assets {
		values = append(values, packageAsset{ID: asset.ID, Bytes: asset.Bytes, SHA256: asset.SHA256, MediaType: asset.MediaType})
	}

	return s.ingestCarriedAssetValues(ctx, values)
}

func (s *Service) ingestCarriedAssetValues(ctx context.Context, values []packageAsset) error {
	for _, asset := range values {
		size := int64(len(asset.Bytes))
		if _, err := s.assets.Ingest(ctx, bytes.NewReader(asset.Bytes), assets.IngestOptions{SHA256: asset.SHA256, Size: &size, MediaType: asset.MediaType}); err != nil {
			return fmt.Errorf("store the photographs %q carries: %w", asset.ID, err)
		}
	}

	return nil
}

type remoteMaterialization int

const (
	remoteMaterialized remoteMaterialization = iota
	remoteMaterializationDeferred
	remoteMaterializationFailed
)

func (s *Service) materializeRemoteAssets(ctx context.Context, recipes []recipe.Document, items []ImportItem, decisions []bool, outcomes map[string]remoteStagingOutcome) ([]int, [][]importer.RemoteAsset, [][]importer.RemoteAsset) {
	dropped := make([]int, len(items))
	deferred := make([][]importer.RemoteAsset, len(items))

	unresolved := make([][]importer.RemoteAsset, len(items))
	for index := range items {
		if !decisions[index] {
			continue
		}

		for _, group := range remoteImageGroups(items[index], &recipes[index]) {
			switch s.materializeImageGroup(ctx, &recipes[index], items[index], group, outcomes) {
			case remoteMaterialized:
			case remoteMaterializationDeferred:
				for _, assetIndex := range group {
					deferred[index] = append(deferred[index], items[index].RemoteAssets[assetIndex])
				}
			case remoteMaterializationFailed:
				dropped[index]++
				for _, assetIndex := range group {
					unresolved[index] = append(unresolved[index], items[index].RemoteAssets[assetIndex])
				}
			}
		}
	}

	return dropped, deferred, unresolved
}

func (s *Service) materializeRemoteAsset(ctx context.Context, document *recipe.Document, item ImportItem, remote importer.RemoteAsset, index int, outcomes map[string]remoteStagingOutcome) remoteMaterialization {
	identity, err := canonicalRemoteURL(remote.URL)
	if err != nil || item.stagedRemote == nil {
		return remoteMaterializationFailed
	}

	staged, ok := item.stagedRemote[identity]
	if !ok {
		if outcomes[identity] == remoteDeferred {
			return remoteMaterializationDeferred
		}

		return remoteMaterializationFailed
	}

	fetched := FetchedAsset{Body: io.NopCloser(bytes.NewReader(staged.bytes)), MediaType: staged.mediaType, Size: int64(len(staged.bytes))}
	blob, ingestErr := s.storePhotograph(ctx, fetched)

	closeErr := fetched.Body.Close()
	if ingestErr != nil || closeErr != nil {
		if closeErr == nil && admissionRefused(ingestErr) {
			return remoteMaterializationDeferred
		}

		return remoteMaterializationFailed
	}

	logicalID := remote.LogicalID
	if logicalID == "" {
		logicalID = fmt.Sprintf("imported-image-%d", index+1)
	}

	document.Assets = append(document.Assets, recipe.Asset{ID: logicalID, Path: "assets/" + logicalID + extensionForMediaType(blob.MediaType), MediaType: blob.MediaType, SHA256: blob.ContentHash, Size: blob.Size, Role: remote.Role, Alt: remote.Alt, Caption: remote.Caption, Creator: remote.Creator, Credit: remote.Credit, Rights: remote.Rights, License: remote.License, SourceURL: remote.URL})
	if remote.RecipeImage {
		document.ImageRefs = append(document.ImageRefs, logicalID)
		if document.PrimaryImage == "" {
			document.PrimaryImage = logicalID
		}
	}

	return remoteMaterialized
}

func importHistory(items []ImportItem, dropped []int, deferred [][]importer.RemoteAsset) []library.ImportContext {
	history := make([]library.ImportContext, len(items))
	for index := range items {
		history[index] = items[index].History

		history[index].Diagnostics = append([]library.ImportDiagnostic{}, history[index].Diagnostics...)
		if dropped[index] > 0 {
			history[index].Diagnostics = append(history[index].Diagnostics, library.ImportDiagnostic{Code: "image-download-failed", Class: "actionable", Area: "images", Count: dropped[index], Message: "Photographs could not be downloaded."})
		}

		if len(deferred[index]) > 0 {
			history[index].Diagnostics = append(history[index].Diagnostics, library.ImportDiagnostic{Code: "image-download-deferred", Class: "informational", Area: "images", Count: len(deferred[index]), Message: "Photographs have not been downloaded yet."})
		}
	}

	return history
}

func (s *Service) commitImportedRecipes(ctx context.Context, recipes []recipe.Document, items []ImportItem, decisions []bool, policy library.DuplicatePolicy, resolutions []library.ImportResolution) ([]library.ImportResult, error) {
	atomic, hasAtomicSources := s.library.(atomicSourceLibrary)
	for index, item := range items {
		if decisions[index] && item.RetainSource && !hasAtomicSources {
			return nil, fmt.Errorf("retain import source: library does not support atomic source attachments")
		}
	}

	var attachments [][]library.SourceAttachment

	if hasAtomicSources {
		var err error

		attachments, err = s.ingestSelectedSources(ctx, items, decisions)
		if err != nil {
			return nil, err
		}
	}

	if resolutions == nil {
		if hasAtomicSources && hasSourceAttachments(attachments) {
			return atomic.ImportRecipesWithSources(ctx, recipes, policy, attachments)
		}

		return s.library.ImportRecipes(ctx, recipes, policy)
	}

	reconciler, ok := s.library.(interface {
		ReconcileImportRecipes(context.Context, []recipe.Document, []library.ImportResolution) ([]library.ImportResult, error)
	})
	if !ok {
		return nil, fmt.Errorf("library does not support import reconciliation")
	}

	if hasAtomicSources && hasSourceAttachments(attachments) {
		return atomic.ReconcileImportRecipesWithSources(ctx, recipes, resolutions, attachments)
	}

	return reconciler.ReconcileImportRecipes(ctx, recipes, resolutions)
}

func decorateAssetResults(results []library.ImportResult, dropped []int, deferred [][]importer.RemoteAsset) {
	for index := range results {
		if count := dropped[index]; count > 0 {
			results[index].DroppedAssets += count
			results[index].Warnings = append(results[index].Warnings, plural(count, "One photograph this recipe names could not be downloaded and was left out.", fmt.Sprintf("%d photographs this recipe names could not be downloaded and were left out.", count)))
		}

		if count := len(deferred[index]); count > 0 {
			results[index].DeferredAssets += count
			results[index].Warnings = append(results[index].Warnings, plural(count, "One photograph has not been downloaded yet. The recipe keeps its address so it can be fetched later.", fmt.Sprintf("%d photographs have not been downloaded yet. The recipe keeps their addresses so they can be fetched later.", count)))
		}
	}
}

func hasSourceAttachments(values [][]library.SourceAttachment) bool {
	for _, value := range values {
		if len(value) > 0 {
			return true
		}
	}

	return false
}

func (s *Service) ingestSelectedSources(ctx context.Context, items []ImportItem, decisions []bool) ([][]library.SourceAttachment, error) {
	attachments := make([][]library.SourceAttachment, len(items))
	for index, item := range items {
		if !decisions[index] || !item.RetainSource {
			continue
		}

		if s.sources == nil {
			return nil, fmt.Errorf("retain import source: source attachment storage is unavailable")
		}

		if len(item.SourceBytes) == 0 || item.SourceFilename == "" {
			return nil, fmt.Errorf("retain import source %q: no uploaded source bytes", item.SourceFilename)
		}

		size := int64(len(item.SourceBytes))

		blob, err := s.assets.Ingest(ctx, bytes.NewReader(item.SourceBytes), assets.IngestOptions{Size: &size, MediaType: item.SourceMediaType})
		if err != nil {
			return nil, fmt.Errorf("retain import source %q: ingest source: %w", item.SourceFilename, err)
		}

		filename := filepath.Base(strings.ReplaceAll(item.SourceFilename, `\`, "/"))
		if filename == "." || filename == "/" || filename == "" {
			filename = "imported-source"
		}

		attachments[index] = []library.SourceAttachment{{AssetID: string(blob.ID), Filename: filename, MediaType: blob.MediaType, Size: blob.Size}}
	}

	return attachments, nil
}

// remoteStagingOutcome separates a photograph the import has not fetched yet
// from one it tried and could not have.
type remoteStagingOutcome int

const (
	remoteStaged remoteStagingOutcome = iota
	remoteDeferred
	remoteFailed
)

// stageDirectRemoteAssets shares one request, byte, and time allowance across
// every selected recipe and retry.
func (s *Service) stageDirectRemoteAssets(ctx context.Context, items []ImportItem, decisions []bool) map[string]remoteStagingOutcome {
	ctx, cancel := context.WithTimeout(ctx, s.remoteLimits.Duration)
	defer cancel()

	staged, outcomes, pending := s.prepareRemoteStaging(items, decisions)

	budget := remoteStagingBudget{requests: len(staged)}
	for _, asset := range staged {
		budget.used += int64(len(asset.bytes))
	}

	attempts := map[string]int{}

	for len(pending) > 0 {
		if ctx.Err() != nil || budget.requests >= s.remoteLimits.Requests || budget.used >= s.remoteLimits.Bytes {
			break
		}

		unreached, retry := s.stageRemoteBatch(ctx, pending, staged, outcomes, attempts, &budget)
		if len(unreached) >= len(pending) && len(retry) == 0 {
			outcomes[unreached[0]] = remoteDeferred
			unreached = unreached[1:]
		}

		pending = unreached
		pending = append(pending, retry...)
	}

	for _, identity := range pending {
		if _, seen := outcomes[identity]; !seen {
			outcomes[identity] = remoteDeferred
		}
	}

	return outcomes
}

func (s *Service) prepareRemoteStaging(items []ImportItem, decisions []bool) (map[string]stagedRemoteAsset, map[string]remoteStagingOutcome, []string) {
	staged := map[string]stagedRemoteAsset{}
	outcomes := map[string]remoteStagingOutcome{}

	for index := range items {
		if !decisions[index] {
			continue
		}

		for identity, asset := range items[index].stagedRemote {
			staged[identity] = asset
			outcomes[identity] = remoteStaged
		}
	}

	var pending []string

	queued := map[string]bool{}

	for index := range items {
		if !decisions[index] {
			continue
		}

		items[index].stagedRemote = staged
		pending = append(pending, s.queueRemoteAssets(items[index].RemoteAssets, staged, outcomes, queued)...)
	}

	return staged, outcomes, pending
}

func (s *Service) queueRemoteAssets(remotes []importer.RemoteAsset, staged map[string]stagedRemoteAsset, outcomes map[string]remoteStagingOutcome, queued map[string]bool) []string {
	var pending []string

	allowed := 0

	for _, remote := range remotes {
		identity, err := canonicalRemoteURL(remote.URL)
		if err != nil {
			continue
		}

		if _, already := staged[identity]; already {
			continue
		}

		if allowed >= s.remoteLimits.AssetsPerRecipe {
			if _, seen := outcomes[identity]; !seen {
				outcomes[identity] = remoteDeferred
			}

			continue
		}

		allowed++

		if queued[identity] {
			continue
		}

		queued[identity] = true
		pending = append(pending, identity)
	}

	return pending
}

type remoteStagingBudget struct {
	requests int
	used     int64
}

// stageRemoteBatch charges attempts and reads to the import allowance.
func (s *Service) stageRemoteBatch(
	parent context.Context, pending []string, staged map[string]stagedRemoteAsset,
	outcomes map[string]remoteStagingOutcome, attempts map[string]int, budget *remoteStagingBudget,
) (unreached, retry []string) {
	ctx := parent

	record := func(identity string, cause error) {
		if errors.Is(cause, ErrRemoteAssetUnusable) {
			outcomes[identity] = remoteFailed
			return
		}

		if attempts[identity] < maximumRemoteAttempts {
			retry = append(retry, identity)
			return
		}

		outcomes[identity] = remoteDeferred
	}
	for index, identity := range pending {
		if parent.Err() != nil {
			return pending[index:], retry
		}

		if ctx.Err() != nil || budget.requests >= s.remoteLimits.Requests || budget.used >= s.remoteLimits.Bytes {
			return pending[index:], retry
		}

		budget.requests++
		attempts[identity]++

		fetched, err := s.fetchRemote(ctx, identity)
		if err != nil {
			if ctx.Err() != nil {
				return pending[index:], retry
			}

			record(identity, err)

			continue
		}

		data, readErr := io.ReadAll(io.LimitReader(fetched.Body, s.remoteLimits.Bytes-budget.used+1))

		remaining := s.remoteLimits.Bytes - budget.used
		budget.used += min(int64(len(data)), remaining)

		closeErr := fetched.Body.Close()
		if readErr != nil || closeErr != nil {
			if ctx.Err() != nil {
				return pending[index:], retry
			}

			record(identity, errors.Join(readErr, closeErr))

			continue
		}

		if int64(len(data)) > remaining {
			if remaining == s.remoteLimits.Bytes {
				outcomes[identity] = remoteFailed
				continue
			}

			return pending[index:], retry
		}

		staged[identity] = stagedRemoteAsset{bytes: data, mediaType: fetched.MediaType}
		outcomes[identity] = remoteStaged
	}

	return nil, retry
}

// storePhotograph puts one fetched photograph away, waiting rather than
// failing when the asset service is momentarily busy.
//
// The per-minute upload meter is admission control for untrusted HTTP clients;
// an import's outbound work is already bounded, so it paces itself to the
// meter instead of dropping photographs it has fetched. Capacity bounds that
// protect the disk are still enforced by the asset service and never waited on.
func (s *Service) storePhotograph(ctx context.Context, fetched FetchedAsset) (assets.Blob, error) {
	body, err := io.ReadAll(io.LimitReader(fetched.Body, maximumRemoteImageBytes+1))
	if err != nil {
		return assets.Blob{}, fmt.Errorf("read fetched photograph: %w", err)
	}

	options := assets.IngestOptions{Size: sizeOrNil(int64(len(body))), MediaType: fetched.MediaType}

	var lastErr error

	attempts := max(1, s.ingestAttempts)
	for attempt := range attempts {
		if attempt > 0 && s.ingestRetryDelay > 0 {
			timer := time.NewTimer(s.ingestRetryDelay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return assets.Blob{}, fmt.Errorf("wait to ingest photograph: %w", ctx.Err())
			}
		}

		blob, err := s.assets.Ingest(ctx, bytes.NewReader(body), options)
		if err == nil {
			return blob, nil
		}

		lastErr = err
		if !admissionRefused(err) {
			return assets.Blob{}, err
		}
	}

	return assets.Blob{}, lastErr
}

// admissionRefused reports a refusal that says "not now" rather than "not
// ever". The photograph is in hand; only putting it away has to wait.
func admissionRefused(err error) bool {
	return errors.Is(err, assets.ErrBusy) || errors.Is(err, assets.ErrCapacity)
}

// retainRemoteReferences records photographs the import did not store, so the
// recipe still knows what it names. Deferred and unresolved are kept apart
// because one is worth retrying and the other is a link that has rotted.
//
// This is a namespaced extension rather than a core member: SREF has no model
// for a photograph an application has not fetched, and inventing one in the
// recipe body would be asserting something the source never said.
func retainRemoteReferences(document *recipe.Document, deferred, unresolved []importer.RemoteAsset) {
	if len(deferred) == 0 && len(unresolved) == 0 {
		return
	}

	retained := map[string]any{}
	if entries := remoteReferenceEntries(deferred); len(entries) > 0 {
		retained["deferred"] = entries
	}

	if entries := remoteReferenceEntries(unresolved); len(entries) > 0 {
		retained["unresolved"] = entries
	}

	if len(retained) == 0 {
		return
	}

	if document.Extensions == nil {
		document.Extensions = map[string]any{}
	}

	document.Extensions["x-org.savorum.pending-images"] = retained
}

func remoteReferenceEntries(assets []importer.RemoteAsset) []any {
	entries := make([]any, 0, len(assets))
	for _, asset := range assets {
		entry := map[string]any{"url": asset.URL}
		// Steps refer to the photograph by this ID.
		if asset.LogicalID != "" {
			entry["logical_id"] = asset.LogicalID
		}

		if asset.Alt != "" {
			entry["alt"] = asset.Alt
		}

		if asset.Caption != "" {
			entry["caption"] = asset.Caption
		}

		if asset.Role != "" {
			entry["role"] = asset.Role
		}

		for key, value := range map[string]string{"creator": asset.Creator, "credit": asset.Credit, "rights": asset.Rights} {
			if value != "" {
				entry[key] = value
			}
		}

		if asset.License != nil {
			entry["license"] = map[string]any{"name": asset.License.Name, "url": asset.License.URL}
		}

		if asset.RecipeImage {
			entry["recipe_image"] = true
		}

		entries = append(entries, entry)
	}

	return entries
}

func (s *Service) fetchRemote(ctx context.Context, rawURL string) (FetchedAsset, error) {
	select {
	case s.remoteSlots <- struct{}{}:
		defer func() { <-s.remoteSlots }()
	case <-ctx.Done():
		return FetchedAsset{}, fmt.Errorf("wait for remote fetch capacity: %w", ctx.Err())
	}

	return s.fetcher.Fetch(ctx, rawURL)
}

func canonicalRemoteURL(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("invalid remote image URL")
	}

	parsed.Fragment = ""
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)

	return parsed.String(), nil
}

func (s *Service) acquireArchiveReader(ctx context.Context) (func(), error) {
	select {
	case s.archiveSlots <- struct{}{}:
		return func() { <-s.archiveSlots }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for archive reader capacity: %w", ctx.Err())
	}
}

// ImportResolved commits items with explicit duplicate resolutions as one import operation.
func (s *Service) ImportResolved(ctx context.Context, items []ImportItem, resolutions []library.ImportResolution) ([]library.ImportResult, error) {
	return s.importItems(ctx, ctx, items, library.DuplicateSkip, resolutions)
}

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}

	return many
}

// sizeOrNil passes a declared length through and says nothing when there was
// none, rather than asserting a zero the store would believe.
func sizeOrNil(size int64) *int64 {
	if size < 0 {
		return nil
	}

	return &size
}

// ImportPackage brings a `.sref` package in. The package is fully verified
// before anything is stored, then its photographs are registered so the recipe
// that names them is not left pointing at nothing.
func (s *Service) ImportPackage(ctx context.Context, data []byte, policy library.DuplicatePolicy) ([]library.ImportResult, error) {
	return s.ImportSources(ctx, []importer.Input{{Name: "recipe.sref", MediaType: "application/zip", Data: data}}, importer.Options{}, policy)
}

const maximumRemoteImageBytes = webrecipe.MaximumImageBytes

type httpRemoteAssetFetcher struct{ client *http.Client }

// newHTTPRemoteAssetFetcher uses the same guarded transport as the recipe page
// fetch: an image address from someone else's JSON-LD is as untrusted as the
// page it came in.
func newHTTPRemoteAssetFetcher() httpRemoteAssetFetcher {
	return httpRemoteAssetFetcher{client: &http.Client{
		Transport:     webrecipe.SafeTransport(),
		Timeout:       30 * time.Second,
		CheckRedirect: webrecipe.SafeRedirectPolicy,
	}}
}

func (fetcher httpRemoteAssetFetcher) Fetch(ctx context.Context, rawURL string) (FetchedAsset, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return FetchedAsset{}, fmt.Errorf("%w: invalid remote image URL", ErrRemoteAssetUnusable)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return FetchedAsset{}, fmt.Errorf("create remote image request: %w", err)
	}

	request.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/gif")

	response, err := fetcher.client.Do(request)
	if err != nil {
		return FetchedAsset{}, fmt.Errorf("fetch remote image: %w", err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = response.Body.Close()
		// A refusal the server will repeat is a different answer from one it
		// might not: 404 and 410 are about the photograph, 503 is about today.
		if response.StatusCode >= 400 && response.StatusCode < 500 && response.StatusCode != http.StatusTooManyRequests {
			return FetchedAsset{}, fmt.Errorf("%w: remote server returned %s", ErrRemoteAssetUnusable, response.Status)
		}

		return FetchedAsset{}, fmt.Errorf("remote server returned %s", response.Status)
	}

	if response.ContentLength > maximumRemoteImageBytes {
		_ = response.Body.Close()
		return FetchedAsset{}, fmt.Errorf("%w: remote image exceeds 16 MiB", ErrRemoteAssetUnusable)
	}
	// Read one byte past the ceiling before returning an asset. Returning a
	// merely truncated stream would let a chunked response larger than the
	// policy masquerade as a valid image exactly at the limit.
	data, readErr := io.ReadAll(io.LimitReader(response.Body, maximumRemoteImageBytes+1))
	closeErr := response.Body.Close()

	if readErr != nil {
		return FetchedAsset{}, fmt.Errorf("read remote image response: %w", readErr)
	}

	if closeErr != nil {
		return FetchedAsset{}, fmt.Errorf("close remote image response: %w", closeErr)
	}

	if len(data) > maximumRemoteImageBytes {
		return FetchedAsset{}, fmt.Errorf("%w: remote image exceeds 16 MiB", ErrRemoteAssetUnusable)
	}

	if len(data) == 0 {
		return FetchedAsset{}, fmt.Errorf("%w: remote image is empty", ErrRemoteAssetUnusable)
	}

	mediaType := imaging.Detect(data)
	if !imaging.Decodable(mediaType) {
		return FetchedAsset{}, fmt.Errorf("%w: remote content is not a supported image", ErrRemoteAssetUnusable)
	}

	return FetchedAsset{Body: io.NopCloser(bytes.NewReader(data)), MediaType: mediaType, Size: int64(len(data))}, nil
}

func extensionForMediaType(mediaType string) string {
	if extensions, err := mime.ExtensionsByType(mediaType); err == nil && len(extensions) > 0 {
		for _, extension := range extensions {
			if extension == ".jpg" || extension == ".png" || extension == ".webp" || extension == ".gif" || extension == ".avif" {
				return extension
			}
		}
	}

	extension := filepath.Ext("image." + strings.TrimPrefix(mediaType, "image/"))
	if extension == ".jpeg" {
		return ".jpg"
	}

	return extension
}

// Formats returns the same registered adapters used by preview and commit.
func (s *Service) Formats() []importer.Format { return s.registry.Formats() }
