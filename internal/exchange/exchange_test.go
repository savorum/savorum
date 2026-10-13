package exchange_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/sref/bundle"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

type importLibrary struct {
	decisions []bool
	results   []library.ImportResult
	imported  []recipe.Document
}

func (*importLibrary) Get(context.Context, library.EntryID) (library.Entry, error) {
	return library.Entry{}, library.ErrNotFound
}

func (f *importLibrary) PreflightImportIdentities(_ context.Context, documents []recipe.Document, _ library.DuplicatePolicy) ([]bool, error) {
	if f.decisions != nil {
		return append([]bool(nil), f.decisions...), nil
	}

	decisions := make([]bool, len(documents))
	for index := range decisions {
		decisions[index] = true
	}

	return decisions, nil
}

func (f *importLibrary) ImportRecipes(_ context.Context, documents []recipe.Document, _ library.DuplicatePolicy) ([]library.ImportResult, error) {
	f.imported = append([]recipe.Document(nil), documents...)
	if f.results == nil {
		results := make([]library.ImportResult, len(documents))
		for index, document := range documents {
			results[index] = library.ImportResult{
				RecipeID: document.ID, EntryID: library.EntryID("entry-" + string(document.ID)),
				Revision: 1, Outcome: library.ImportCreated, Title: document.Title,
			}
		}

		return results, nil
	}

	return append([]library.ImportResult(nil), f.results...), nil
}

type countingAssets struct{ ingests int }

func (f *countingAssets) Ingest(context.Context, io.Reader, assets.IngestOptions) (assets.Blob, error) {
	f.ingests++
	return assets.Blob{ID: "sha256:abc", ContentHash: strings.Repeat("a", 64), MediaType: "image/jpeg", Size: 4}, nil
}

type staticFetcher struct{ fetches int }

type futureAdapter struct{ document recipe.Document }

func (futureAdapter) Format() importer.Format {
	return importer.Format{ID: "future", Name: "Future source", Version: "1", Extensions: []string{".future"}, Capabilities: importer.Capabilities{SourceIdentity: true}}
}

func (futureAdapter) Probe(context.Context, importer.Input) importer.ProbeResult {
	return importer.ProbeResult{Confidence: importer.ConfidenceContent, Reason: "future signature"}
}

func (adapter futureAdapter) Import(context.Context, importer.Input, importer.Options) (importer.ImportBatch, error) {
	return importer.ImportBatch{Recipes: []importer.ImportedRecipe{{Document: adapter.document}}}, nil
}

func (f *staticFetcher) Fetch(context.Context, string) (exchange.FetchedAsset, error) {
	f.fetches++
	return exchange.FetchedAsset{Body: io.NopCloser(strings.NewReader("jpeg")), MediaType: "image/jpeg", Size: 4}, nil
}

func TestCommonOrchestrationAcceptsANewAdapterWithoutTransportChanges(t *testing.T) {
	document := testfixture.Document()
	document.ID, document.Title = "future-source", "Future soup"
	recipes := &importLibrary{}
	service := exchange.NewService(recipes, &countingAssets{}, exchange.WithImporterRegistry(importer.NewRegistry(futureAdapter{document: document})))

	results, err := service.ImportSources(context.Background(), []importer.Input{{Name: "soup.future", Data: []byte("future")}}, importer.Options{}, library.DuplicateSkip)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 || len(recipes.imported) != 1 || recipes.imported[0].Title != "Future soup" {
		t.Fatalf("results=%#v imported=%#v", results, recipes.imported)
	}
}

type failingFetcher struct{ fetches int }

func (f *failingFetcher) Fetch(context.Context, string) (exchange.FetchedAsset, error) {
	f.fetches++
	return exchange.FetchedAsset{}, errors.New("image host unavailable")
}

type changingFetcher struct {
	fetches int
	first   []byte
	second  []byte
}

func (f *changingFetcher) Fetch(ctx context.Context, _ string) (exchange.FetchedAsset, error) {
	if err := ctx.Err(); err != nil {
		return exchange.FetchedAsset{}, err
	}

	f.fetches++

	data := f.first
	if f.fetches > 1 {
		data = f.second
	}

	return exchange.FetchedAsset{Body: io.NopCloser(bytes.NewReader(data)), MediaType: "image/jpeg", Size: int64(len(data))}, nil
}

type recordingAssets struct{ bodies [][]byte }

func (r *recordingAssets) Ingest(_ context.Context, source io.Reader, options assets.IngestOptions) (assets.Blob, error) {
	data, err := io.ReadAll(source)
	if err != nil {
		return assets.Blob{}, fmt.Errorf("read ingested test asset: %w", err)
	}

	r.bodies = append(r.bodies, append([]byte(nil), data...))
	digest := fmt.Sprintf("%x", sha256.Sum256(data))

	return assets.Blob{ID: assets.ID("sha256:" + digest), ContentHash: digest, MediaType: options.MediaType, Size: int64(len(data))}, nil
}

func (*recordingAssets) Open(context.Context, assets.ID) (assets.Blob, io.ReadCloser, error) {
	return assets.Blob{}, nil, assets.ErrNotFound
}

func TestExternalImportStagesRemoteImageAndNamesItInDocument(t *testing.T) {
	document := testfixture.Document()
	document.ID, document.Assets, document.PrimaryImage = "remote-image-recipe", nil, ""
	document.ImageRefs = nil
	recipes := &importLibrary{decisions: []bool{true}, results: []library.ImportResult{{RecipeID: document.ID, Outcome: library.ImportCreated}}}
	blobs, fetcher := &countingAssets{}, &staticFetcher{}
	service := exchange.NewService(recipes, blobs, exchange.WithRemoteAssetFetcher(fetcher))

	_, err := service.ImportExternal(context.Background(), []importer.ImportBatch{
		{Recipes: []importer.ImportedRecipe{
			{Document: document, Assets: []importer.RemoteAsset{{URL: "https://images.example/recipe.jpg", RecipeImage: true, Role: recipe.AssetRoleImage, Alt: document.Title}}},
		}},
	}, library.DuplicateReject)
	if err != nil {
		t.Fatal(err)
	}

	if fetcher.fetches != 1 || blobs.ingests != 1 {
		t.Fatalf("fetches = %d, ingests = %d", fetcher.fetches, blobs.ingests)
	}

	if len(recipes.imported) != 1 || recipes.imported[0].PrimaryImage != "imported-image-1" || !reflect.DeepEqual(recipes.imported[0].ImageRefs, []string{"imported-image-1"}) || len(recipes.imported[0].Assets) != 1 {
		t.Fatalf("imported = %#v", recipes.imported)
	}
}

func TestExternalImportPreservesRecipeImageOrder(t *testing.T) {
	document := testfixture.Document()
	document.ID, document.Assets, document.PrimaryImage = "remote-gallery", nil, ""
	document.ImageRefs = nil
	recipes := &importLibrary{decisions: []bool{true}}
	service := exchange.NewService(recipes, &countingAssets{}, exchange.WithRemoteAssetFetcher(&staticFetcher{}))

	_, err := service.ImportExternal(context.Background(), []importer.ImportBatch{{Recipes: []importer.ImportedRecipe{{
		Document: document,
		Assets: []importer.RemoteAsset{
			{LogicalID: "first", URL: "https://images.example/first.jpg", RecipeImage: true, Role: recipe.AssetRoleImage},
			{LogicalID: "second", URL: "https://images.example/second.jpg", RecipeImage: true, Role: recipe.AssetRoleImage},
		},
	}}}}, library.DuplicateReject)
	if err != nil {
		t.Fatal(err)
	}

	if len(recipes.imported) != 1 || !reflect.DeepEqual(recipes.imported[0].ImageRefs, []string{"first", "second"}) || recipes.imported[0].PrimaryImage != "first" {
		t.Fatalf("imported gallery = %#v", recipes.imported)
	}
}

func TestSkippedExternalImportDoesNotDownloadRemoteImage(t *testing.T) {
	document := testfixture.Document()
	document.ID = "already-held-external"
	recipes := &importLibrary{decisions: []bool{false}, results: []library.ImportResult{{RecipeID: document.ID, Outcome: library.ImportSkipped}}}
	fetcher := &staticFetcher{}
	service := exchange.NewService(recipes, &countingAssets{}, exchange.WithRemoteAssetFetcher(fetcher))

	_, err := service.ImportExternal(context.Background(), []importer.ImportBatch{{Recipes: []importer.ImportedRecipe{{Document: document, Assets: []importer.RemoteAsset{{URL: "https://images.example/recipe.jpg"}}}}}}, library.DuplicateSkip)
	if err != nil {
		t.Fatal(err)
	}

	if fetcher.fetches != 0 {
		t.Fatalf("skipped import fetched %d images", fetcher.fetches)
	}
}

func (*countingAssets) Open(context.Context, assets.ID) (assets.Blob, io.ReadCloser, error) {
	return assets.Blob{}, nil, errors.New("not implemented")
}

func TestSkippedPackageDoesNotRegisterItsAssets(t *testing.T) {
	t.Parallel()

	image := []byte("synthetic image bytes")
	digest := fmt.Sprintf("%x", sha256.Sum256(image))
	document := testfixture.Document()
	document.ID = "already-held"
	document.PrimaryImage = ""

	document.ImageRefs = nil
	for sectionIndex := range document.InstructionSections {
		for stepIndex := range document.InstructionSections[sectionIndex].Steps {
			document.InstructionSections[sectionIndex].Steps[stepIndex].ImageRefs = nil
		}
	}

	document.Assets = []recipe.Asset{{
		ID: "photo", Path: "assets/photo.png", MediaType: "image/png",
		SHA256: digest, Size: int64(len(image)), Role: recipe.AssetRoleImage,
	}}
	document.InstructionSections[0].Steps[0].ImageRefs = []string{"photo"}

	packaged, err := sref.WritePackage(document, []sref.PackageAsset{{
		ID: "photo", Path: "assets/photo.png", MediaType: "image/png",
		SHA256: digest, Bytes: image,
	}})
	if err != nil {
		t.Fatal(err)
	}

	recipes := &importLibrary{
		decisions: []bool{false},
		results:   []library.ImportResult{{RecipeID: document.ID, Outcome: library.ImportSkipped}},
	}
	blobs := &countingAssets{}

	results, err := exchange.NewService(recipes, blobs).ImportPackage(context.Background(), packaged, library.DuplicateSkip)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 || results[0].Outcome != library.ImportSkipped {
		t.Fatalf("skip result = %#v", results)
	}

	if blobs.ingests != 0 {
		t.Fatalf("skipped package registered %d assets", blobs.ingests)
	}
}

func TestPreviewReportsWarningsWithoutWritingOrFetching(t *testing.T) {
	recipes := &importLibrary{}
	fetcher := &failingFetcher{}
	service := exchange.NewService(recipes, &countingAssets{}, exchange.WithRemoteAssetFetcher(fetcher))
	body := `<!doctype html><html><head>
		<script type="application/ld+json">{"@type":"Recipe", broken</script>
		<script type="application/ld+json">{
			"@type":"Recipe","name":"Preview cake","image":"https://images.example/cake.jpg",
			"recipeIngredient":["2 cups flour"],"recipeInstructions":["Bake."]
		}</script></head></html>`

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{Filename: "cake.html", Data: []byte(body)}})
	if err != nil {
		t.Fatal(err)
	}

	if len(recipes.imported) != 0 {
		t.Fatalf("preview wrote recipes: %#v", recipes.imported)
	}
	// Staging a review reaches out to nobody. Outbound work happens because a
	// reader looked at a row, not because a file was chosen.
	if len(preview.Items) != 1 || !preview.Items[0].CanCommit || fetcher.fetches != 0 {
		t.Fatalf("preview = %#v, fetches = %d", preview, fetcher.fetches)
	}

	item := preview.Items[0]
	if item.Assets.Discovered != 1 || item.Assets.Deferred != 1 || item.Assets.Failed != 0 {
		t.Fatalf("assets = %#v", item.Assets)
	}
	// An unfetched photograph is not a diagnostic; Assets.Deferred counts it.
	if !hasDiagnostic(item.Diagnostics, "structured-data-unreadable", exchange.SeverityWarning) {
		t.Fatalf("diagnostics = %#v", item.Diagnostics)
	}

	for _, diagnostic := range item.Diagnostics {
		if diagnostic.Area == "asset" {
			t.Fatalf("a queue-level fact was reported per recipe: %#v", diagnostic)
		}
	}

	if _, err := service.PreviewImage(context.Background(), preview.ID, item.ID); !errors.Is(err, exchange.ErrPreviewImageUnavailable) {
		t.Fatalf("unreachable preview image error = %v", err)
	}

	if fetcher.fetches != 1 {
		t.Fatalf("looking at the row fetched %d times, want once", fetcher.fetches)
	}
	// A settled answer is remembered, so scrolling past the same row twice does
	// not reach out to the same server twice.
	if _, err := service.PreviewImage(context.Background(), preview.ID, item.ID); !errors.Is(err, exchange.ErrPreviewImageUnavailable) {
		t.Fatalf("repeat preview image error = %v", err)
	}

	if fetcher.fetches != 1 {
		t.Fatalf("a settled photograph was fetched again; fetches=%d", fetcher.fetches)
	}
}

// A photograph Savorum has no bytes for is not the same answer as one it has
// not fetched yet, and only the first is something a reviewer can act on: the
// second is retried at import.
func TestPreviewSeparatesUnavailablePhotographsFromUnfetchedOnes(t *testing.T) {
	// A bare SREF document names its photographs and carries none of them.
	encoded, err := sref.Encode(testfixture.Document())
	if err != nil {
		t.Fatal(err)
	}

	fetcher := &staticFetcher{}
	service := exchange.NewService(&importLibrary{}, &countingAssets{}, exchange.WithRemoteAssetFetcher(fetcher))

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{Filename: "recipe.json", Data: encoded}})
	if err != nil {
		t.Fatal(err)
	}

	item := preview.Items[0]
	if item.Assets.Discovered != 1 || item.Assets.Unavailable != 1 || item.Assets.Deferred != 0 || !item.CanCommit {
		t.Fatalf("assets=%#v canCommit=%v", item.Assets, item.CanCommit)
	}

	if !hasDiagnostic(item.Diagnostics, "asset-bytes-missing", exchange.SeverityWarning) {
		t.Fatalf("diagnostics = %#v", item.Diagnostics)
	}

	if fetcher.fetches != 0 {
		t.Fatalf("a document naming no addresses fetched %d times", fetcher.fetches)
	}
}

func TestPreviewDeduplicatesFragmentsAndCommitUsesPreviewedBytes(t *testing.T) {
	recipes := &importLibrary{}
	blobs := &recordingAssets{}
	fetcher := &changingFetcher{first: []byte("previewed-jpeg"), second: []byte("changed-jpeg")}
	service := exchange.NewService(recipes, blobs, exchange.WithRemoteAssetFetcher(fetcher))
	body := `{"@type":"Recipe","name":"Bound image","image":["https://IMG.example/photo.jpg#one","https://img.example/photo.jpg#two"],"recipeIngredient":["water"],"recipeInstructions":[{"@type":"HowToStep","text":"Cook.","image":"https://img.example/photo.jpg#step"}]}`

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{Filename: "bound.jsonld", Data: []byte(body)}})
	if err != nil {
		t.Fatal(err)
	}

	if fetcher.fetches != 0 {
		t.Fatalf("staging a review fetched %d photographs", fetcher.fetches)
	}
	// Three logical assets, one address: the fragments name the same picture.
	if preview.Items[0].Assets.Discovered != 3 || preview.Items[0].Assets.Deferred != 3 {
		t.Fatalf("asset summary = %#v", preview.Items[0].Assets)
	}

	image, err := service.PreviewImage(context.Background(), preview.ID, preview.Items[0].ID)
	if err != nil || image.MediaType != "image/jpeg" || !bytes.Equal(image.Bytes, fetcher.first) {
		t.Fatalf("preview image=%#v error=%v", image, err)
	}

	if _, err := service.PreviewImage(context.Background(), preview.ID, "missing"); !errors.Is(err, exchange.ErrPreviewNotFound) {
		t.Fatalf("missing preview image error=%v", err)
	}

	if _, err := service.CommitPreview(context.Background(), preview.ID, []string{preview.Items[0].ID}, library.DuplicateSkip); err != nil {
		t.Fatal(err)
	}
	// The commit reuses what the reviewer already looked at rather than
	// fetching the address again and storing whatever it says the second time.
	if fetcher.fetches != 1 {
		t.Fatalf("commit refetched remote content; fetches=%d", fetcher.fetches)
	}

	if len(blobs.bodies) != 3 {
		t.Fatalf("stored %d logical assets, want 3", len(blobs.bodies))
	}

	for _, body := range blobs.bodies {
		if !bytes.Equal(body, fetcher.first) {
			t.Fatalf("stored changed bytes %q", body)
		}
	}
}

func TestFatalCandidateDoesNotInspectItsRemoteAssets(t *testing.T) {
	fetcher := &staticFetcher{}
	service := exchange.NewService(&importLibrary{}, &countingAssets{}, exchange.WithRemoteAssetFetcher(fetcher))
	plain := `{"@type":"Recipe","name":"Duplicate","recipeIngredient":["water"],"recipeInstructions":["Cook."]}`
	withImage := `{"@type":"Recipe","name":"Duplicate","image":"https://images.example/photo.jpg","recipeIngredient":["water"],"recipeInstructions":["Cook."]}`

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{Filename: "one.jsonld", Data: []byte(plain)}, {Filename: "two.jsonld", Data: []byte(withImage)}})
	if err != nil {
		t.Fatal(err)
	}

	if len(preview.Items) != 2 || preview.Items[1].CanCommit || fetcher.fetches != 0 {
		t.Fatalf("preview=%#v fetches=%d", preview.Items, fetcher.fetches)
	}
}

type delayedFetcher struct{ calls int }

func (f *delayedFetcher) Fetch(ctx context.Context, _ string) (exchange.FetchedAsset, error) {
	f.calls++

	<-ctx.Done()

	return exchange.FetchedAsset{}, ctx.Err()
}

func TestPreviewImageTimeoutReportsOutstandingWorkRatherThanLoss(t *testing.T) {
	fetcher := &delayedFetcher{}
	limits := exchange.DefaultPreviewLimits()
	limits.ImageTimeout = 20 * time.Millisecond
	service := exchange.NewService(&importLibrary{}, &countingAssets{}, exchange.WithRemoteAssetFetcher(fetcher), exchange.WithPreviewLimits(limits))
	body := `{"@type":"Recipe","name":"Slow","image":"https://images.example/slow.jpg","recipeIngredient":["water"],"recipeInstructions":["Cook."]}`

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{Filename: "slow.jsonld", Data: []byte(body)}})
	if err != nil || len(preview.Items) != 1 || !preview.Items[0].CanCommit || fetcher.calls != 0 {
		t.Fatalf("preview=%#v error=%v calls=%d", preview, err, fetcher.calls)
	}
	// A server that does not answer in time has said nothing about whether the
	// photograph exists, so the import will ask again.
	if _, err := service.PreviewImage(context.Background(), preview.ID, preview.Items[0].ID); !errors.Is(err, exchange.ErrPreviewImagePending) {
		t.Fatalf("slow preview image error=%v", err)
	}

	if fetcher.calls != 1 {
		t.Fatalf("calls=%d", fetcher.calls)
	}
}

// The per-session allowance is what keeps a review from becoming an amplifier,
// and running out of it makes a photograph outstanding rather than a recipe
// unimportable.
func TestPreviewImageAllowanceBoundsOutboundWorkWithoutBlockingRecipes(t *testing.T) {
	fetcher := &staticFetcher{}
	limits := exchange.DefaultPreviewLimits()
	limits.ImagesPerSession = 1
	service := exchange.NewService(&importLibrary{}, &countingAssets{}, exchange.WithRemoteAssetFetcher(fetcher), exchange.WithPreviewLimits(limits))
	uploads := []exchange.UploadedImport{
		{Filename: "one.jsonld", Data: []byte(`{"@type":"Recipe","name":"One","image":"https://one.example/a.jpg","recipeIngredient":["water"],"recipeInstructions":["Cook."]}`)},
		{Filename: "two.jsonld", Data: []byte(`{"@type":"Recipe","name":"Two","image":"https://two.example/b.jpg","recipeIngredient":["water"],"recipeInstructions":["Cook."]}`)},
	}

	preview, err := service.PreviewFiles(context.Background(), uploads)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.PreviewImage(context.Background(), preview.ID, preview.Items[0].ID); err != nil {
		t.Fatalf("first preview image: %v", err)
	}

	if _, err := service.PreviewImage(context.Background(), preview.ID, preview.Items[1].ID); !errors.Is(err, exchange.ErrPreviewImagePending) {
		t.Fatalf("second preview image error=%v", err)
	}

	if fetcher.fetches != 1 {
		t.Fatalf("fetches=%d, want the allowance to hold at one", fetcher.fetches)
	}

	for _, item := range preview.Items {
		if !item.CanCommit {
			t.Fatalf("a photograph allowance made a recipe unimportable: %#v", item)
		}
	}
}

func TestLargePreviewFetchesNothingAndCommitsEveryRecipeWithItsPhotograph(t *testing.T) {
	fetcher := &staticFetcher{}
	blobs := &countingAssets{}
	// Deliberately tighter than the queue: the point is that a bound on one
	// batch of outbound work is not a bound on which recipes can be imported.
	limits := exchange.DefaultRemoteImportLimits()
	limits.Requests = 2
	service := exchange.NewService(&importLibrary{}, blobs, exchange.WithRemoteAssetFetcher(fetcher), exchange.WithRemoteImportLimits(limits))

	uploads := make([]exchange.UploadedImport, 5)
	for index := range uploads {
		uploads[index] = exchange.UploadedImport{
			Filename: fmt.Sprintf("recipe-%d.jsonld", index+1),
			Data:     fmt.Appendf(nil, `{"@type":"Recipe","name":"Recipe %d","image":"https://images.example/%d.jpg","recipeIngredient":["water"],"recipeInstructions":["Cook."]}`, index+1, index+1),
		}
	}

	preview, err := service.PreviewFiles(context.Background(), uploads)
	if err != nil {
		t.Fatal(err)
	}

	if len(preview.Items) != 5 {
		t.Fatalf("items=%d", len(preview.Items))
	}

	deferred := 0
	for _, item := range preview.Items {
		deferred += item.Assets.Deferred
		if !item.CanCommit {
			t.Fatalf("a recipe was made unselectable by a photograph limit: %#v", item)
		}
	}

	if deferred != 5 || fetcher.fetches != 0 {
		t.Fatalf("deferred=%d fetches=%d", deferred, fetcher.fetches)
	}

	for start := 0; start < len(preview.Items); start += 2 {
		end := min(start+2, len(preview.Items))

		ids := make([]string, 0, end-start)
		for _, item := range preview.Items[start:end] {
			ids = append(ids, item.ID)
		}

		if _, err := service.CommitPreview(context.Background(), preview.ID, ids, library.DuplicateSkip); err != nil {
			t.Fatalf("commit batch %d: %v", start/2+1, err)
		}
	}
	// Every recipe, and every recipe's photograph.
	if fetcher.fetches != 5 || blobs.ingests != 5 {
		t.Fatalf("fetches=%d ingests=%d", fetcher.fetches, blobs.ingests)
	}
}

type gatedFetcher struct {
	entered chan struct{}
	release chan struct{}
}

func (f *gatedFetcher) Fetch(ctx context.Context, _ string) (exchange.FetchedAsset, error) {
	select {
	case f.entered <- struct{}{}:
	case <-ctx.Done():
		return exchange.FetchedAsset{}, ctx.Err()
	}

	select {
	case <-f.release:
	case <-ctx.Done():
		return exchange.FetchedAsset{}, ctx.Err()
	}

	return exchange.FetchedAsset{Body: io.NopCloser(strings.NewReader("jpeg")), MediaType: "image/jpeg", Size: 4}, nil
}

func TestRemoteAdmissionCancellationStopsQueuedWork(t *testing.T) {
	fetcher := &gatedFetcher{entered: make(chan struct{}, 2), release: make(chan struct{})}
	limits := exchange.DefaultRemoteImportLimits()
	limits.Concurrent = 1
	service := exchange.NewService(&importLibrary{}, &countingAssets{}, exchange.WithRemoteAssetFetcher(fetcher), exchange.WithRemoteImportLimits(limits))
	body := func(name string) exchange.UploadedImport {
		return exchange.UploadedImport{Filename: name + ".jsonld", Data: fmt.Appendf(nil, `{"@type":"Recipe","name":%q,"image":"https://images.example/%s.jpg","recipeIngredient":["water"],"recipeInstructions":["Cook."]}`, name, name)}
	}

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{body("first"), body("second")})
	if err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan error, 1)

	go func() {
		_, err := service.PreviewImage(context.Background(), preview.ID, preview.Items[0].ID)
		firstDone <- err
	}()

	<-fetcher.entered

	ctx, cancel := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)

	go func() {
		_, err := service.PreviewImage(ctx, preview.ID, preview.Items[1].ID)
		secondDone <- err
	}()

	cancel()

	if err := <-secondDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued preview image error=%v", err)
	}

	select {
	case <-fetcher.entered:
		t.Fatal("queued fetch entered global admission")
	default:
	}

	close(fetcher.release)

	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func TestPreviewCommitsSelectedRecipesAndKeepsFailuresForReview(t *testing.T) {
	recipes := &importLibrary{}
	service := exchange.NewService(recipes, &countingAssets{})
	good := `{"@type":"Recipe","name":"Clean soup","recipeIngredient":["water"],"recipeInstructions":["Simmer."]}`

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{
		{Filename: "soup.jsonld", Data: []byte(good)},
		{Filename: "broken.html", Data: []byte("not a recipe")},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(preview.Items) != 2 || !preview.Items[0].CanCommit || preview.Items[1].CanCommit {
		t.Fatalf("preview = %#v", preview)
	}

	committed, err := service.CommitPreview(context.Background(), preview.ID, []string{preview.Items[0].ID}, library.DuplicateSkip)
	if err != nil {
		t.Fatal(err)
	}

	if len(committed.Items) != 1 || len(recipes.imported) != 1 || recipes.imported[0].Title != "Clean soup" {
		t.Fatalf("committed = %#v, imported = %#v", committed.Items, recipes.imported)
	}

	if committed.Remaining == nil || len(committed.Remaining.Items) != 1 || committed.Remaining.Items[0].CanCommit {
		t.Fatalf("remaining = %#v", committed.Remaining)
	}
}

func TestMeasurementResolutionPlansGroupByAliasAndApplyAtomically(t *testing.T) {
	t.Parallel()

	recipes := &importLibrary{}
	service := exchange.NewService(recipes, &countingAssets{})
	yaml := func(name string, ingredients ...string) exchange.UploadedImport {
		var lines strings.Builder
		fmt.Fprintf(&lines, "name: %s\ningredients:\n", name)

		for _, ingredient := range ingredients {
			fmt.Fprintf(&lines, "  - %s\n", ingredient)
		}

		lines.WriteString("directions:\n  - Mix.\n")

		return exchange.UploadedImport{Filename: "rights-clean.yml", Data: []byte(lines.String())}
	}

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{
		yaml("One", "1 Tbsp olive oil", "1 cup flour", "1 US tablespoon vinegar", "4 8 oz pork chops"),
		yaml("Two", "1 ½ Tbsp olive oil", "1 cup sugar"),
		yaml("Three", "1 Tbsp olive oil", "1 T molasses", "1 t salt"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if preview.Revision != 1 {
		t.Fatalf("revision = %d", preview.Revision)
	}

	groups := map[string]exchange.MeasurementResolutionGroup{}
	for _, group := range preview.ResolutionGroups {
		groups[group.AuthoredForm] = group
	}

	tablespoons := groups["Tbsp"]
	cups := groups["cup"]

	if tablespoons.Occurrences != 3 || tablespoons.Recipes != 3 || len(tablespoons.Subgroups) != 2 {
		t.Fatalf("tablespoon group = %#v", tablespoons)
	}

	if cups.Occurrences != 2 || cups.Recipes != 2 {
		t.Fatalf("cup group = %#v", cups)
	}

	if groups["T"].ID == "" || groups["t"].ID == "" || groups["T"].ID == groups["t"].ID {
		t.Fatalf("case-sensitive groups were merged: %#v %#v", groups["T"], groups["t"])
	}

	detail, err := service.MeasurementResolutionGroup(context.Background(), preview.ID, tablespoons.ID)
	if err != nil || len(detail.Occurrences) != 3 {
		t.Fatalf("detail = %#v, err = %v", detail, err)
	}

	var excluded string

	for _, occurrence := range detail.Occurrences {
		if occurrence.RecipeTitle == "Three" {
			excluded = occurrence.ID
			break
		}
	}

	if excluded == "" {
		t.Fatal("missing occurrence for Three")
	}

	if _, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID, exchange.MeasurementResolutionPlanRequest{
		GroupIDs:         []string{tablespoons.ID},
		Interpretation:   exchange.MeasurementResolutionInterpretation{Kind: exchange.ResolutionCanonicalUnit, UnitID: "mass.gram"},
		ExpectedRevision: preview.Revision,
	}); !errors.Is(err, exchange.ErrResolutionPlanInvalid) {
		t.Fatalf("uncontrolled unit error = %v", err)
	}

	if current, err := service.MeasurementResolutionGroup(context.Background(), preview.ID, tablespoons.ID); err != nil || len(current.Occurrences) != 3 {
		t.Fatalf("failed plan mutated preview: detail=%#v err=%v", current, err)
	}

	plan, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID, exchange.MeasurementResolutionPlanRequest{
		GroupIDs:              []string{tablespoons.ID, cups.ID},
		Interpretation:        exchange.MeasurementResolutionInterpretation{Kind: exchange.ResolutionConvention, Convention: "au"},
		ExcludedOccurrenceIDs: []string{excluded}, ExpectedRevision: preview.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Five compatible aliases minus one explicit exception. The unrelated
	// malformed amount remains in the total actionable review count.
	if plan.AffectedOccurrences != 4 || plan.AffectedRecipes != 2 || plan.UnchangedOccurrences != 1 ||
		plan.ResultingActionableOccurrences < 1 || plan.ImpactDigest == "" || len(plan.ValidationFailures) != 0 {
		t.Fatalf("plan = %#v", plan)
	}

	stale, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID, exchange.MeasurementResolutionPlanRequest{
		GroupIDs:         []string{tablespoons.ID},
		Interpretation:   exchange.MeasurementResolutionInterpretation{Kind: exchange.ResolutionKeepWritten},
		ExpectedRevision: preview.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}

	applied, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, plan.ID, preview.Revision)
	if err != nil {
		t.Fatal(err)
	}

	if applied.Preview == nil || applied.Preview.Revision != 2 {
		t.Fatalf("applied = %#v", applied)
	}

	if _, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, stale.ID, preview.Revision); !errors.Is(err, exchange.ErrResolutionPlanStale) {
		t.Fatalf("stale plan error = %v", err)
	}

	reverted, err := service.RevertMeasurementResolutionPlan(context.Background(), preview.ID, plan.ID, 2)
	if err != nil || reverted.Revision != 3 || reverted.ResolutionGroups[0].Occurrences == 0 {
		t.Fatalf("reverted = %#v, err = %v", reverted, err)
	}
}

func TestAppliedMeasurementResolutionRoundTripsCanonicalIdentityAndProvenance(t *testing.T) {
	t.Parallel()

	recipes := &importLibrary{}
	service := exchange.NewService(recipes, &countingAssets{})

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
		Filename: "review.yml",
		Data:     []byte("name: Reviewed\ningredients:\n  - 1 Tbsp oil\ndirections:\n  - Mix.\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	group := preview.ResolutionGroups[0]

	plan, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID, exchange.MeasurementResolutionPlanRequest{
		GroupIDs: []string{group.ID}, Interpretation: exchange.MeasurementResolutionInterpretation{
			Kind: exchange.ResolutionCanonicalUnit, UnitID: "volume.tablespoon.au",
		}, ExpectedRevision: preview.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}

	applied, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, plan.ID, preview.Revision)
	if err != nil {
		t.Fatal(err)
	}

	committed, err := service.CommitPreview(context.Background(), preview.ID, []string{applied.Preview.Items[0].ID}, library.DuplicateSkip)
	if err != nil || len(committed.Items) != 1 || len(recipes.imported) != 1 {
		t.Fatalf("commit=%#v imported=%#v err=%v", committed, recipes.imported, err)
	}

	encoded, err := sref.Encode(recipes.imported[0])
	if err != nil {
		t.Fatal(err)
	}

	roundTripped, err := sref.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}

	ingredient := roundTripped.IngredientSections[0].Ingredients[0]
	if ingredient.Quantity == nil || ingredient.Quantity.Unit != "volume.tablespoon.au" || ingredient.SourceText != "1 Tbsp oil" ||
		ingredient.Normalization == nil || ingredient.Normalization.Method != recipe.NormalizationInferred ||
		ingredient.Normalization.Review != recipe.ReviewCorrected || len(ingredient.Normalization.Warnings) != 1 ||
		!recipe.ImportReviewInterpreted(ingredient.Normalization.Warnings[0]) || roundTripped.SourceLocale != "" {
		t.Fatalf("round-tripped ingredient=%#v source_locale=%q", ingredient, roundTripped.SourceLocale)
	}
}

func TestMeasurementResolutionKeepWrittenClearsCurrentActionability(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
		Filename: "review.yml",
		Data:     []byte("name: Reviewed\ningredients:\n  - 1 Tbsp oil\ndirections:\n  - Mix.\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	group := preview.ResolutionGroups[0]

	plan, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID, exchange.MeasurementResolutionPlanRequest{
		GroupIDs: []string{group.ID}, Interpretation: exchange.MeasurementResolutionInterpretation{Kind: exchange.ResolutionKeepWritten},
		ExpectedRevision: preview.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}

	applied, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, plan.ID, preview.Revision)
	if err != nil {
		t.Fatal(err)
	}

	item := applied.Preview.Items[0]

	ingredient := item.Recipe.IngredientSections[0].Ingredients[0]
	if ingredient.Quantity != nil || ingredient.SourceText != "1 Tbsp oil" || ingredient.Normalization == nil ||
		ingredient.Normalization.Method != recipe.NormalizationUnresolved || ingredient.Normalization.Review != recipe.ReviewConfirmed ||
		len(ingredient.Normalization.Warnings) == 0 {
		t.Fatalf("kept ingredient = %#v", ingredient)
	}

	if !hasDiagnostic(item.Diagnostics, "ingredient-review-confirmed", exchange.SeverityInfo) || len(applied.Preview.ResolutionGroups) != 0 {
		t.Fatalf("diagnostics = %#v groups = %#v", item.Diagnostics, applied.Preview.ResolutionGroups)
	}
}

func TestMeasurementResolutionUSConventionPreservesTableSpoonAmounts(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
		Filename: "measurements.yml",
		Data:     []byte("name: Measurements\ningredients:\n  - 2 tbsp oil\n  - 2 tablespoons vinegar\ndirections:\n  - Mix.\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	groups := map[string]exchange.MeasurementResolutionGroup{}
	for _, group := range preview.ResolutionGroups {
		groups[group.AuthoredForm] = group
	}

	for _, authored := range []string{"tbsp", "tablespoons"} {
		group := groups[authored]
		if group.ID == "" || len(group.Choices) < 2 || group.Choices[0].UnitID != "volume.tablespoon.us.customary" || group.Choices[0].Convention != "us" {
			t.Fatalf("%q group choices = %#v", authored, group.Choices)
		}
	}

	plan, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID, exchange.MeasurementResolutionPlanRequest{
		Interpretation:   exchange.MeasurementResolutionInterpretation{Kind: exchange.ResolutionConvention, Convention: "us"},
		ExpectedRevision: preview.Revision,
	})
	if err != nil || len(plan.ValidationFailures) != 0 {
		t.Fatalf("plan = %#v, err = %v", plan, err)
	}

	applied, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, plan.ID, preview.Revision)
	if err != nil {
		t.Fatal(err)
	}

	if applied.Preview == nil || applied.Preview.Revision != preview.Revision+1 {
		t.Fatalf("applied = %#v", applied)
	}

	for _, ingredient := range applied.Preview.Items[0].Recipe.IngredientSections[0].Ingredients {
		if ingredient.Quantity == nil || ingredient.Quantity.Amount == nil || ingredient.Quantity.Amount.Value == nil || ingredient.Quantity.Amount.Value.String() != "2" || ingredient.Quantity.Unit != "volume.tablespoon.us.customary" {
			t.Fatalf("resolved ingredient = %#v", ingredient)
		}
	}
}

func TestMeasurementResolutionConventionSettlesEveryMeasureOnALine(t *testing.T) {
	t.Parallel()

	// An ingredient convention answers every applicable measure of the
	// ingredient it is submitted for, including each term of a compound amount.
	service := exchange.NewService(&importLibrary{}, &countingAssets{})

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
		Filename: "compound.yml",
		Data:     []byte("name: Compound\ningredients:\n  - 2 tablespoons oil\n  - 2 tbsp vinegar\n  - 1 tablespoon plus 2 teaspoons neutral oil\ndirections:\n  - Mix.\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	plan, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID, exchange.MeasurementResolutionPlanRequest{
		Interpretation:   exchange.MeasurementResolutionInterpretation{Kind: exchange.ResolutionConvention, Convention: "us"},
		ExpectedRevision: preview.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Four measures across three ingredients: the compound amount carries two.
	if plan.AffectedOccurrences != 4 || plan.AffectedIngredients != 3 ||
		plan.ResultingActionableOccurrences != 0 || plan.UnchangedOccurrences != 0 ||
		len(plan.ValidationFailures) != 0 {
		t.Fatalf("plan = %#v", plan)
	}

	applied, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, plan.ID, preview.Revision)
	if err != nil {
		t.Fatal(err)
	}

	if applied.Preview == nil || applied.Preview.Revision != 2 || len(applied.Preview.ResolutionGroups) != 0 {
		t.Fatalf("applied preview = %#v", applied.Preview)
	}

	ingredients := applied.Preview.Items[0].Recipe.IngredientSections[0].Ingredients
	for _, index := range []int{0, 1} {
		if ingredients[index].Quantity == nil || ingredients[index].Quantity.Unit != "volume.tablespoon.us.customary" {
			t.Fatalf("resolved ingredient %d = %#v", index, ingredients[index])
		}
	}

	compound := ingredients[2].Quantity
	if compound == nil || compound.Kind != recipe.QuantitySum || len(compound.Terms) != 2 ||
		compound.Terms[0].Unit != "volume.tablespoon.us.customary" ||
		compound.Terms[1].Unit != "volume.teaspoon.us.customary" {
		t.Fatalf("compound amount = %#v", compound)
	}

	reverted, err := service.RevertMeasurementResolutionPlan(context.Background(), preview.ID, plan.ID, applied.Preview.Revision)
	if err != nil || reverted.Revision != 3 {
		t.Fatalf("reverted = %#v, err = %v", reverted, err)
	}

	for _, ingredient := range reverted.Items[0].Recipe.IngredientSections[0].Ingredients {
		if ingredient.Quantity != nil {
			t.Fatalf("revert changed unresolved ingredient = %#v", ingredient)
		}
	}
}

func TestMeasurementResolutionReadsTheLineTheWayItWasAsked(t *testing.T) {
	t.Parallel()

	// A CookbookManager export states no locale. The answer must be applied
	// under the same English reading that found the ambiguity.
	service := exchange.NewService(&importLibrary{}, &countingAssets{})

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
		Filename: "dual.yml",
		Data:     []byte("name: Dual\ningredients:\n  - 1.5 cups/187 grams all-purpose flour\ndirections:\n  - Mix.\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	if preview.Items[0].Recipe.SourceLocale != "" {
		t.Fatalf("source locale = %q, want none stated", preview.Items[0].Recipe.SourceLocale)
	}

	plan, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID, exchange.MeasurementResolutionPlanRequest{
		Interpretation:   exchange.MeasurementResolutionInterpretation{Kind: exchange.ResolutionConvention, Convention: "us"},
		ExpectedRevision: preview.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}

	applied, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, plan.ID, preview.Revision)
	if err != nil {
		t.Fatal(err)
	}

	quantity := applied.Preview.Items[0].Recipe.IngredientSections[0].Ingredients[0].Quantity
	if quantity == nil || quantity.Kind != recipe.QuantityAlternatives || len(quantity.Options) != 2 ||
		quantity.Options[0].Unit != "volume.cup.us.customary" || quantity.Options[1].Unit != "mass.gram" {
		t.Fatalf("quantity = %#v", quantity)
	}
}

func TestMeasurementResolutionUnsupportedOnlyCannotApplyEmptyPlan(t *testing.T) {
	for _, kind := range []string{exchange.ResolutionConvention, exchange.ResolutionCanonicalUnit} {
		t.Run(kind, func(t *testing.T) {
			service := exchange.NewService(&importLibrary{}, &countingAssets{})

			// A dash is not a measure the registry holds, so no answer about
			// the tablespoon can finish this line and the plan changes nothing.
			preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
				Filename: "compound.yml",
				Data:     []byte("name: Compound\ningredients:\n  - 1 tablespoon plus 2 dashes hot sauce\ndirections:\n  - Mix.\n"),
			}})
			if err != nil {
				t.Fatal(err)
			}

			plan, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID, exchange.MeasurementResolutionPlanRequest{
				GroupIDs:         []string{preview.ResolutionGroups[0].ID},
				Interpretation:   exchange.MeasurementResolutionInterpretation{Kind: kind, Convention: "us", UnitID: "volume.tablespoon.us.customary"},
				ExpectedRevision: preview.Revision,
			})
			if err != nil {
				t.Fatal(err)
			}

			// The measure is reported as one this plan cannot settle, and
			// nothing is reported as invalid: the recipes are fine, and the
			// answer simply does not reach the amount. Applying it is still
			// refused, because it would change nothing.
			if plan.AffectedOccurrences != 0 || plan.UnchangedOccurrences != 0 ||
				len(plan.UnresolvableOccurrences) != 1 || len(plan.ValidationFailures) != 0 {
				t.Fatalf("plan = %#v", plan)
			}

			if _, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, plan.ID, preview.Revision); !errors.Is(err, exchange.ErrResolutionPlanInvalid) {
				t.Fatalf("apply = %v", err)
			}
		})
	}
}

func TestSREFAndJSONLDUseTheSamePreviewModel(t *testing.T) {
	document := testfixture.Document()
	document.ID, document.Title = "portable-preview", "Portable stew"

	encoded, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	web := `{"@type":"Recipe","name":"Web stew","recipeIngredient":["beans"],"recipeInstructions":["Cook."]}`

	preview, err := exchange.NewService(&importLibrary{}, &countingAssets{}).PreviewFiles(context.Background(), []exchange.UploadedImport{
		{Filename: "portable.json", Data: encoded},
		{Filename: "web.jsonld", Data: []byte(web)},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(preview.Items) != 2 {
		t.Fatalf("items = %#v", preview.Items)
	}

	if preview.Items[0].Source.FormatID != "sref-document" || preview.Items[1].Source.FormatID != "web-recipe" {
		t.Fatalf("sources = %#v, %#v", preview.Items[0].Source, preview.Items[1].Source)
	}

	for _, item := range preview.Items {
		if !item.CanCommit || item.Recipe.Title == "" || item.ID == "" {
			t.Fatalf("item = %#v", item)
		}
	}
}

func TestBundlePreviewValidatesMembersAndUsesConfiguredLimits(t *testing.T) {
	makePackage := func(t *testing.T, id string) []byte {
		t.Helper()

		document := testfixture.Document()
		document.ID, document.Assets, document.PrimaryImage = recipe.RecipeID(id), nil, ""

		document.ImageRefs = nil
		for sectionIndex := range document.InstructionSections {
			for stepIndex := range document.InstructionSections[sectionIndex].Steps {
				document.InstructionSections[sectionIndex].Steps[stepIndex].ImageRefs = nil
			}
		}

		packaged, err := sref.WritePackage(document, nil)
		if err != nil {
			t.Fatal(err)
		}

		return packaged
	}
	makeBundle := func(t *testing.T, declaredID, actualID string) []byte {
		t.Helper()

		buffer := &bytes.Buffer{}
		writer := bundle.NewWriter(buffer)

		packaged := makePackage(t, actualID)
		if err := writer.Add(declaredID, func(member io.Writer) error {
			_, err := member.Write(packaged)
			if err != nil {
				return fmt.Errorf("write test bundle member: %w", err)
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}

		return buffer.Bytes()
	}

	t.Run("valid member", func(t *testing.T) {
		preview, err := exchange.NewService(&importLibrary{}, &countingAssets{}).PreviewFiles(
			context.Background(), []exchange.UploadedImport{{
				Filename: "recipes.srefbundle", Data: makeBundle(t, "portable-recipe", "portable-recipe"),
			}})
		if err != nil {
			t.Fatal(err)
		}

		if len(preview.Items) != 1 || !preview.Items[0].CanCommit || preview.Items[0].Source.FormatID != "sref-bundle" {
			t.Fatalf("preview = %#v", preview.Items)
		}
	})

	t.Run("recipe identity disagreement", func(t *testing.T) {
		preview, err := exchange.NewService(&importLibrary{}, &countingAssets{}).PreviewFiles(
			context.Background(), []exchange.UploadedImport{{
				Filename: "recipes.srefbundle", Data: makeBundle(t, "declared-recipe", "actual-recipe"),
			}})
		if err != nil {
			t.Fatal(err)
		}

		if len(preview.Items) != 1 || preview.Items[0].CanCommit ||
			!hasDiagnostic(preview.Items[0].Diagnostics, "file-unreadable", exchange.SeverityError) {
			t.Fatalf("preview = %#v", preview.Items)
		}
	})

	t.Run("configured archive limit", func(t *testing.T) {
		limits := bundle.DefaultLimits()
		limits.ArchiveBytes = 1
		service := exchange.NewService(&importLibrary{}, &countingAssets{}, exchange.WithSREFBundleLimits(limits))

		preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
			Filename: "recipes.srefbundle", Data: makeBundle(t, "portable-recipe", "portable-recipe"),
		}})
		if err != nil {
			t.Fatal(err)
		}

		if len(preview.Items) != 1 || preview.Items[0].CanCommit ||
			!hasDiagnostic(preview.Items[0].Diagnostics, "file-unreadable", exchange.SeverityError) {
			t.Fatalf("preview = %#v", preview.Items)
		}
	})
}

func TestPreviewKeepsFileOrderAndBlocksRepeatedPortableRecipes(t *testing.T) {
	document := testfixture.Document()
	document.ID, document.Title = "same-portable-recipe", "One portable stew"

	encoded, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	preview, err := exchange.NewService(&importLibrary{}, &countingAssets{}).PreviewFiles(context.Background(), []exchange.UploadedImport{
		{Filename: "broken.html", Data: []byte("not a recipe")},
		{Filename: "first.json", Data: encoded},
		{Filename: "second.json", Data: encoded},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(preview.Items) != 3 || preview.Items[0].Source.Filename != "broken.html" ||
		!preview.Items[1].CanCommit || preview.Items[2].CanCommit {
		t.Fatalf("preview = %#v", preview.Items)
	}

	if !hasDiagnostic(preview.Items[2].Diagnostics, "duplicate-candidate", exchange.SeverityError) {
		t.Fatalf("duplicate diagnostics = %#v", preview.Items[2].Diagnostics)
	}
}

func hasDiagnostic(diagnostics []exchange.ImportDiagnostic, code string, severity exchange.ImportSeverity) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code && diagnostic.Severity == severity {
			return true
		}
	}

	return false
}

// TestCookbookManagerCompoundAndAlternativeAmountsSurviveReview walks compound
// and alternative amounts through a locale-free CookbookManager import,
// review, one ingredient-convention decision, and a SREF round trip. The lines
// are exact authored strings from an archive that is not redistributable.
func TestCookbookManagerCompoundAndAlternativeAmountsSurviveReview(t *testing.T) {
	t.Parallel()

	const export = "name: Compound amounts\n" +
		"ingredients:\n" +
		"  - 1.5 cups/187 grams all-purpose flour\n" +
		"  - 0.33 cup/113 milliliters unsulphured molasses\n" +
		"  - 1 tablespoon plus 2 teaspoons neutral oil\n" +
		"  - 1/2 cup flour\n" +
		"  - 2.5 cups / 320 grams bread flour\n" +
		"directions:\n" +
		"  - Mix.\n"

	service := exchange.NewService(&importLibrary{}, &countingAssets{})

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
		Filename: "Compound amounts_178793999229158.yml", Data: []byte(export),
	}})
	if err != nil {
		t.Fatal(err)
	}

	// Nothing is decided until somebody decides it. Every convention-dependent
	// measure stays unresolved, and no ingredient is rejected for it.
	ingredients := preview.Items[0].Recipe.IngredientSections[0].Ingredients
	if len(ingredients) != 5 {
		t.Fatalf("ingredients = %#v", ingredients)
	}

	for index, ingredient := range ingredients {
		if ingredient.Quantity != nil {
			t.Fatalf("measure %d was decided without being asked: %#v", index, ingredient.Quantity)
		}

		if ingredient.SourceText == "" || ingredient.Normalization == nil ||
			ingredient.Normalization.Method != recipe.NormalizationUnresolved {
			t.Fatalf("ingredient %d = %#v", index, ingredient)
		}
	}

	// The unspaced slash of `1.5 cups/187 grams` is an alternative and the
	// slash of `1/2 cup` is a fraction, so the compound line offers two
	// measures and the fraction line offers one.
	occurrences := 0
	for _, group := range preview.ResolutionGroups {
		occurrences += group.Occurrences
	}

	if occurrences != 6 {
		t.Fatalf("measures offered for review = %d: %#v", occurrences, preview.ResolutionGroups)
	}

	plan, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID, exchange.MeasurementResolutionPlanRequest{
		Interpretation:   exchange.MeasurementResolutionInterpretation{Kind: exchange.ResolutionConvention, Convention: "us"},
		ExpectedRevision: preview.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}

	if plan.AffectedOccurrences != 6 || plan.AffectedIngredients != 5 || len(plan.ValidationFailures) != 0 {
		t.Fatalf("plan = %#v", plan)
	}

	applied, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, plan.ID, preview.Revision)
	if err != nil {
		t.Fatal(err)
	}

	resolved := applied.Preview.Items[0].Recipe
	assertCompoundAmounts(t, resolved)

	// The identities and the authored text survive the exchange format, which
	// is the only claim an importer can make about what it preserved.
	encoded, err := sref.Encode(resolved)
	if err != nil {
		t.Fatal(err)
	}

	returned, err := sref.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}

	assertCompoundAmounts(t, returned)
}

func assertCompoundAmounts(t *testing.T, document recipe.Document) {
	t.Helper()

	ingredients := document.IngredientSections[0].Ingredients

	// Two authored alternatives, both preserved. Resolving the cup does not
	// assert that 3/2 US customary cups and 187 grams are equal, and must not
	// choose between them or turn them into a sum.
	flour := ingredients[0]
	if flour.Name != "all-purpose flour" || flour.SourceText != "1.5 cups/187 grams all-purpose flour" {
		t.Fatalf("flour = %#v", flour)
	}

	assertAlternatives(t, "flour", flour.Quantity,
		[2]string{"3/2", "volume.cup.us.customary"}, [2]string{"187", "mass.gram"})

	// The two authored amounts disagree materially under this interpretation.
	// Both source assertions are kept; neither is corrected into the other.
	molasses := ingredients[1]
	if molasses.Name != "unsulphured molasses" {
		t.Fatalf("molasses = %#v", molasses)
	}

	assertAlternatives(t, "molasses", molasses.Quantity,
		[2]string{"33/100", "volume.cup.us.customary"}, [2]string{"113", "volume.milliliter"})

	oil := ingredients[2]
	if oil.Name != "neutral oil" || oil.Quantity == nil || oil.Quantity.Kind != recipe.QuantitySum ||
		len(oil.Quantity.Terms) != 2 ||
		oil.Quantity.Terms[0].Unit != "volume.tablespoon.us.customary" ||
		oil.Quantity.Terms[1].Unit != "volume.teaspoon.us.customary" ||
		oil.Quantity.Terms[0].Amount.Value.String() != "1" ||
		oil.Quantity.Terms[1].Amount.Value.String() != "2" {
		t.Fatalf("oil = %#v", oil.Quantity)
	}

	// The control: a slash between a numerator and a denominator is a
	// fraction, and the decision does not turn it into an alternative.
	half := ingredients[3]
	if half.Name != "flour" || half.Quantity == nil || half.Quantity.Kind != recipe.QuantitySimple ||
		half.Quantity.Amount.Value.String() != "1/2" || half.Quantity.Unit != "volume.cup.us.customary" {
		t.Fatalf("half cup = %#v", half.Quantity)
	}

	assertAlternatives(t, "spaced slash", ingredients[4].Quantity,
		[2]string{"5/2", "volume.cup.us.customary"}, [2]string{"320", "mass.gram"})

	for _, ingredient := range ingredients {
		if ingredient.Normalization == nil || ingredient.Normalization.Method != recipe.NormalizationInferred ||
			ingredient.Normalization.Review != recipe.ReviewCorrected {
			t.Fatalf("%s normalization = %#v", ingredient.Name, ingredient.Normalization)
		}
	}
}

func assertAlternatives(t *testing.T, name string, quantity *recipe.Quantity, first, second [2]string) {
	t.Helper()

	if quantity == nil || quantity.Kind != recipe.QuantityAlternatives || len(quantity.Options) != 2 {
		t.Fatalf("%s = %#v", name, quantity)
	}

	for index, want := range [][2]string{first, second} {
		option := quantity.Options[index]
		if option.Amount == nil || option.Amount.Value == nil ||
			option.Amount.Value.String() != want[0] || option.Unit != want[1] {
			t.Fatalf("%s option %d = %#v, want %s %s", name, index, option, want[0], want[1])
		}
	}
}

// TestTheShippedMetricProfileResolvesThroughTheRealReviewPath exercises the
// Metric culinary convention Savorum actually offers, through the menu, the
// planner, the production parser, persistence, and a SREF round trip.
//
// A conformance vector proves the resolver, not that the menu supplies the
// same mapping: a convention filtered back to the alias candidates of the
// authored word would resolve no bare cup, because no alias associates `cup`
// with the 250 mL identity.
func TestTheShippedMetricProfileResolvesThroughTheRealReviewPath(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
		Filename: "metric.yml",
		Data: []byte("name: Metric probe\ningredients:\n" +
			"  - 1 cup natural yoghurt\n" +
			"  - 2 teaspoons vanilla\n" +
			"  - 1 tablespoon oil\n" +
			"  - 1 tablespoon plus 2 teaspoons neutral oil\n" +
			"  - 2 metric teaspoons rosewater\n" +
			"  - 200 grams flour\n" +
			"directions:\n  - Mix.\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	// The menu is read from the catalog, so a convention offered here is one
	// the resolver holds, described by what it selects.
	var metric exchange.MeasurementConventionProfile

	for _, profile := range preview.ResolutionProfiles {
		if profile.ID == string(units.ConventionMetric) {
			metric = profile
		}
	}

	if !slices.Equal(metric.Selects, []string{
		"volume.cup.metric.culinary", "volume.tablespoon.metric.culinary", "volume.teaspoon.metric.culinary",
	}) ||
		slices.Contains(metric.Unanswered, "volume.tablespoon") ||
		metric.UnitRegistryVersion != units.RegistryVersion {
		t.Fatalf("the offered Metric culinary profile = %#v", metric)
	}

	// Without a decision the bare regional forms stay unresolved, including the
	// ones this convention would settle.
	for index, ingredient := range preview.Items[0].Recipe.IngredientSections[0].Ingredients[:4] {
		if ingredient.Quantity != nil {
			t.Fatalf("measure %d was settled with no decision: %#v", index, ingredient.Quantity)
		}
	}

	plan, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID,
		exchange.MeasurementResolutionPlanRequest{
			Interpretation: exchange.MeasurementResolutionInterpretation{
				Kind: exchange.ResolutionConvention, Convention: string(units.ConventionMetric),
			},
			ExpectedRevision: preview.Revision,
		})
	if err != nil {
		t.Fatal(err)
	}

	// Every bare regional measure is settled, the compound one included, so
	// nothing is left pending.
	if plan.AffectedIngredients != 4 || plan.UnchangedOccurrences != 0 || len(plan.PendingOccurrences) != 0 {
		t.Fatalf("plan = %#v", plan)
	}

	applied, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, plan.ID, preview.Revision)
	if err != nil {
		t.Fatal(err)
	}

	assertMetricProfileResult(t, applied.Preview.Items[0].Recipe)

	encoded, err := sref.Encode(applied.Preview.Items[0].Recipe)
	if err != nil {
		t.Fatal(err)
	}

	returned, err := sref.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}

	assertMetricProfileResult(t, returned)
}

func assertMetricProfileResult(t *testing.T, document recipe.Document) {
	t.Helper()

	ingredients := document.IngredientSections[0].Ingredients

	// A convention names a system, and the system says what a cup is. No alias
	// associates the bare word with the 250 mL identity, because no source
	// evidence could establish one.
	for _, expected := range []struct {
		index int
		unit  string
	}{
		{0, "volume.cup.metric.culinary"},
		{1, "volume.teaspoon.metric.culinary"},
		{2, "volume.tablespoon.metric.culinary"},
		// The source said `metric teaspoons`, and a decision never overrides
		// meaning the source itself stated.
		{4, "volume.teaspoon.metric.culinary"},
		// Outside the convention's families, so its own evidence still decides.
		{5, "mass.gram"},
	} {
		got := ingredients[expected.index]
		if got.Quantity == nil || got.Quantity.Unit != expected.unit {
			t.Fatalf("%q = %#v, want %s", got.SourceText, got.Quantity, expected.unit)
		}
	}

	// A compound amount is stored whole once every measure in it is settled.
	compound := ingredients[3].Quantity
	if compound == nil || compound.Kind != recipe.QuantitySum || len(compound.Terms) != 2 ||
		compound.Terms[0].Unit != "volume.tablespoon.metric.culinary" ||
		compound.Terms[1].Unit != "volume.teaspoon.metric.culinary" {
		t.Fatalf("%q = %#v", ingredients[3].SourceText, compound)
	}
}

// TestALegacyDecisionRecordEstablishesNoScope guards the unmigrated JSON
// history rows: members added later (decision kind, scope, expanded mapping)
// decode to zero values that must not be read as an answer.
func TestALegacyDecisionRecordEstablishesNoScope(t *testing.T) {
	t.Parallel()

	// Exactly the members a record written before scopes existed carried.
	legacy := []byte(`{
		"resolution_kind": "unit_alias",
		"interpretation_kind": "convention",
		"convention": "us",
		"parser_version": "1",
		"unit_registry_version": "0.1.0",
		"affected_occurrence_ids": ["occurrence-1"],
		"affected_recipe_count": 1,
		"original_actionable_count": 3,
		"resulting_actionable_count": 2,
		"impact_digest": "abc123"
	}`)

	var record exchange.MeasurementResolutionRecord
	if err := json.Unmarshal(legacy, &record); err != nil {
		t.Fatalf("a record written before the new members no longer loads: %v", err)
	}

	// What it did say is still readable.
	if record.InterpretationKind != exchange.ResolutionConvention || record.Convention != "us" ||
		record.ImpactDigest != "abc123" || record.UnitRegistryVersion != "0.1.0" {
		t.Fatalf("the historical record lost what it recorded: %#v", record)
	}

	// What it did not say stays unsaid. The interpretation kind is `convention`
	// and the scope is still unknown: the action a person took in an interface
	// that had no scopes is not evidence of the scope they authorized.
	if record.DecisionScope() != exchange.DecisionScopeUnknown {
		t.Fatalf("a record with no decision kind reported scope %q", record.DecisionScope())
	}

	if record.Replayable() {
		t.Fatal("a record establishing no scope was treated as replayable")
	}

	if len(record.ScopeIngredientIDs) != 0 || len(record.ConventionUnitIDs) != 0 {
		t.Fatalf("absent members decoded to a scope: %#v", record)
	}
}

// TestACurrentDecisionRecordCarriesItsScope is the other half: a record written
// now says what was authorized, so "unknown" stays a statement about history
// rather than about everything.
func TestACurrentDecisionRecordCarriesItsScope(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
		Filename: "scope.yml",
		Data:     []byte("name: Scope\ningredients:\n  - 2 tablespoons oil\ndirections:\n  - Mix.\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	plan, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID,
		exchange.MeasurementResolutionPlanRequest{
			Interpretation: exchange.MeasurementResolutionInterpretation{
				Kind: exchange.ResolutionConvention, Convention: string(units.ConventionUS),
			},
			ExpectedRevision: preview.Revision,
		})
	if err != nil {
		t.Fatal(err)
	}

	applied, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, plan.ID, preview.Revision)
	if err != nil {
		t.Fatal(err)
	}

	plannedImport, err := service.PlanPreviewImport(context.Background(), preview.ID,
		[]exchange.PreviewResolution{{ItemID: applied.Preview.Items[0].ID}}, false)
	if err != nil {
		t.Fatal(err)
	}

	if len(plannedImport.Items) != 1 {
		t.Fatalf("planned items = %d", len(plannedImport.Items))
	}

	records := plannedImport.Items[0].MeasurementResolutionRecords()
	if len(records) != 1 {
		t.Fatalf("records = %#v", records)
	}

	record := records[0]
	if record.DecisionScope() != exchange.DecisionIngredientConvention ||
		len(record.ScopeIngredientIDs) != 1 ||
		!slices.Contains(record.ConventionUnitIDs, "volume.tablespoon.us.customary") {
		t.Fatalf("a decision made now did not record its scope: %#v", record)
	}

	// Recorded scope is not by itself an instruction to reuse it. SREF section
	// 9.5 requires a refreshed import to be a new normalization, so this says
	// only that the detail needed to explain the decision is present.
	if !record.Replayable() {
		t.Fatalf("a fully scoped record reported nothing to explain: %#v", record)
	}
}

// occurrenceIDsByForm maps each ambiguous measure's authored word to its
// occurrence ID, which is how a review action addresses one.
func occurrenceIDsByForm(t *testing.T, service *exchange.Service, previewID string, groups []exchange.MeasurementResolutionGroup) map[string]string {
	t.Helper()

	found := map[string]string{}

	for _, group := range groups {
		detail, err := service.MeasurementResolutionGroup(context.Background(), previewID, group.ID)
		if err != nil {
			t.Fatal(err)
		}

		for _, occurrence := range detail.Occurrences {
			found[occurrence.AuthoredForm] = occurrence.ID
		}
	}

	return found
}

func compoundOilPreview(t *testing.T, service *exchange.Service) exchange.ImportPreview {
	t.Helper()

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
		Filename: "oil.yml",
		Data:     []byte("name: Oil\ningredients:\n  - 1 tablespoon plus 2 teaspoons neutral oil\ndirections:\n  - Mix.\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	return preview
}

func cupAndFluidOuncesPreview(t *testing.T, service *exchange.Service) exchange.ImportPreview {
	t.Helper()

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
		Filename: "milk.yml",
		Data:     []byte("name: Milk\ningredients:\n  - 1 cup plus 2 fluid ounces whole milk\ndirections:\n  - Mix.\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	return preview
}

// TestAConventionCannotReachAnExcludedMeasurement holds the scope of an
// exclusion against the scope of the decision it is applied to. A convention
// covers a whole ingredient, so it must not settle an excluded measure pulled
// into scope by a selected sibling.
func TestAConventionCannotReachAnExcludedMeasurement(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})
	preview := compoundOilPreview(t, service)
	byForm := occurrenceIDsByForm(t, service, preview.ID, preview.ResolutionGroups)

	_, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID,
		exchange.MeasurementResolutionPlanRequest{
			Interpretation: exchange.MeasurementResolutionInterpretation{
				Kind: exchange.ResolutionConvention, Convention: string(units.ConventionUS),
			},
			ExcludedOccurrenceIDs: []string{byForm["teaspoons"]},
			ExpectedRevision:      preview.Revision,
		})

	// The only ingredient in this selection has a measure excluded, so a
	// convention action has nothing left it may touch. Refusing says so;
	// proceeding would have changed the excluded measure.
	if err == nil {
		t.Fatal("a convention action proceeded past an exclusion inside its only ingredient")
	}

	if !errors.Is(err, exchange.ErrResolutionPlanInvalid) {
		t.Fatalf("err = %v", err)
	}
}

// TestAnOccurrenceExclusionStillMeansOneMeasurement is the other half: an
// occurrence action has per-measure scope, so a per-measure exclusion keeps its
// per-measure meaning there.
func TestAnOccurrenceExclusionStillMeansOneMeasurement(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
		Filename: "spoons.yml",
		Data: []byte("name: Spoons\ningredients:\n" +
			"  - 2 tablespoons oil\n  - 1 tablespoon vinegar\ndirections:\n  - Mix.\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	var group exchange.MeasurementResolutionGroup

	for _, candidate := range preview.ResolutionGroups {
		if candidate.AuthoredForm == "tablespoon" {
			group = candidate
		}
	}

	detail, err := service.MeasurementResolutionGroup(context.Background(), preview.ID, group.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(detail.Occurrences) != 1 {
		t.Fatalf("occurrences = %#v", detail.Occurrences)
	}

	// Excluding the only occurrence of the only selected group leaves nothing,
	// which is refused for the ordinary reason rather than the convention one.
	_, err = service.CreateMeasurementResolutionPlan(context.Background(), preview.ID,
		exchange.MeasurementResolutionPlanRequest{
			GroupIDs: []string{group.ID},
			Interpretation: exchange.MeasurementResolutionInterpretation{
				Kind: exchange.ResolutionCanonicalUnit, UnitID: "volume.tablespoon.us.customary",
			},
			ExcludedOccurrenceIDs: []string{detail.Occurrences[0].ID},
			ExpectedRevision:      preview.Revision,
		})
	if err == nil || !errors.Is(err, exchange.ErrResolutionPlanInvalid) {
		t.Fatalf("err = %v", err)
	}
}

// answerOneMeasure applies a canonical-unit decision to the single group whose
// authored word is given, which is what the individual-review control does.
func answerOneMeasure(
	t *testing.T, service *exchange.Service, previewID string,
	view exchange.ImportPreview, form, unitID string,
) exchange.MeasurementResolutionPlan {
	t.Helper()

	var group exchange.MeasurementResolutionGroup

	for _, candidate := range view.ResolutionGroups {
		if candidate.AuthoredForm == form {
			group = candidate
		}
	}

	if group.ID == "" {
		t.Fatalf("no group for %q among %#v", form, view.ResolutionGroups)
	}

	plan, err := service.CreateMeasurementResolutionPlan(context.Background(), previewID,
		exchange.MeasurementResolutionPlanRequest{
			GroupIDs: []string{group.ID},
			Interpretation: exchange.MeasurementResolutionInterpretation{
				Kind: exchange.ResolutionCanonicalUnit, UnitID: unitID,
			},
			ExpectedRevision: view.Revision,
		})
	if err != nil {
		t.Fatalf("plan %q: %v", form, err)
	}

	applied, err := service.ApplyMeasurementResolutionPlan(context.Background(), previewID, plan.ID, view.Revision)
	if err != nil {
		t.Fatalf("apply %q: %v", form, err)
	}

	return applied
}

// TestACompoundAmountIsFinishedOneMeasureAtATime covers individual review of a
// compound amount. A quantity expression is stored whole or not at all, so the
// first answer is retained, not stored, until the second completes it.
func TestACompoundAmountIsFinishedOneMeasureAtATime(t *testing.T) {
	t.Parallel()

	for _, order := range [][2]string{
		{"tablespoon", "teaspoons"},
		{"teaspoons", "tablespoon"},
	} {
		t.Run(order[0]+"-then-"+order[1], func(t *testing.T) {
			t.Parallel()

			units := map[string]string{
				"tablespoon": "volume.tablespoon.us.customary",
				"teaspoons":  "volume.teaspoon.us.customary",
			}

			service := exchange.NewService(&importLibrary{}, &countingAssets{})
			preview := compoundOilPreview(t, service)

			if len(preview.ResolutionGroups) != 2 {
				t.Fatalf("groups = %#v", preview.ResolutionGroups)
			}

			first := answerOneMeasure(t, service, preview.ID, preview, order[0], units[order[0]])

			// Nothing is stored yet, and the answer is not lost: the measure
			// just answered has stopped being asked about.
			ingredient := first.Preview.Items[0].Recipe.IngredientSections[0].Ingredients[0]
			if ingredient.Quantity != nil {
				t.Fatalf("a half-answered compound amount was stored: %#v", ingredient.Quantity)
			}

			if len(first.Preview.ResolutionGroups) != 1 ||
				first.Preview.ResolutionGroups[0].AuthoredForm != order[1] {
				t.Fatalf("still asked = %#v", first.Preview.ResolutionGroups)
			}

			if len(first.PendingOccurrences) != 1 {
				t.Fatalf("the answer was not retained: %#v", first.PendingOccurrences)
			}

			second := answerOneMeasure(t, service, preview.ID, *first.Preview, order[1], units[order[1]])

			// Both answers together complete the expression, so now it stores.
			quantity := second.Preview.Items[0].Recipe.IngredientSections[0].Ingredients[0].Quantity
			if quantity == nil || quantity.Kind != recipe.QuantitySum || len(quantity.Terms) != 2 ||
				quantity.Terms[0].Unit != "volume.tablespoon.us.customary" ||
				quantity.Terms[1].Unit != "volume.teaspoon.us.customary" {
				t.Fatalf("quantity = %#v", quantity)
			}

			if len(second.Preview.ResolutionGroups) != 0 {
				t.Fatalf("questions remain = %#v", second.Preview.ResolutionGroups)
			}
		})
	}
}

// TestAPartlyCoveredConventionIsCompletedByAnInterpretation is the mixed case:
// a convention that answers some of an ingredient's measures, finished by an
// explicit choice for the one it cannot.
//
// The British imperial convention answers a fluid ounce and no cup. The
// convention is retained rather than discarded while the cup is outstanding.
func TestAPartlyCoveredConventionIsCompletedByAnInterpretation(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})
	preview := cupAndFluidOuncesPreview(t, service)

	plan, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID,
		exchange.MeasurementResolutionPlanRequest{
			Interpretation: exchange.MeasurementResolutionInterpretation{
				Kind: exchange.ResolutionConvention, Convention: string(units.ConventionImperial),
			},
			ExpectedRevision: preview.Revision,
		})
	if err != nil {
		t.Fatal(err)
	}

	applied, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, plan.ID, preview.Revision)
	if err != nil {
		t.Fatal(err)
	}

	if len(applied.PendingOccurrences) != 1 || applied.PendingOccurrences[0].AuthoredForm != "fluid ounces" {
		t.Fatalf("the convention's answer was not retained: %#v", applied.PendingOccurrences)
	}

	if len(applied.Preview.ResolutionGroups) != 1 ||
		applied.Preview.ResolutionGroups[0].AuthoredForm != "cup" {
		t.Fatalf("still asked = %#v", applied.Preview.ResolutionGroups)
	}

	finished := answerOneMeasure(t, service, preview.ID, *applied.Preview,
		"cup", "volume.cup.au")

	// The convention settled the fluid ounces, the interpretation settled the
	// cup, and a deliberately mixed reading is what the person chose.
	quantity := finished.Preview.Items[0].Recipe.IngredientSections[0].Ingredients[0].Quantity
	if quantity == nil || len(quantity.Terms) != 2 ||
		quantity.Terms[0].Unit != "volume.cup.au" ||
		quantity.Terms[1].Unit != "volume.fluidounce.imperial" {
		t.Fatalf("quantity = %#v", quantity)
	}
}

// TestTwoConventionsForOneIngredientAreAConflict: because answers are retained,
// two conventions for one ingredient can meet, and settling that by recency
// would silently discard a reviewed decision.
func TestTwoConventionsForOneIngredientAreAConflict(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})
	preview := cupAndFluidOuncesPreview(t, service)

	imperial, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID,
		exchange.MeasurementResolutionPlanRequest{
			Interpretation: exchange.MeasurementResolutionInterpretation{
				Kind: exchange.ResolutionConvention, Convention: string(units.ConventionImperial),
			},
			ExpectedRevision: preview.Revision,
		})
	if err != nil {
		t.Fatal(err)
	}

	applied, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, imperial.ID, preview.Revision)
	if err != nil {
		t.Fatal(err)
	}

	// Imperial answered the fluid ounces and cannot answer the cup, so the
	// ingredient is still open and its answer is retained.
	if len(applied.PendingOccurrences) != 1 {
		t.Fatalf("pending = %#v", applied.PendingOccurrences)
	}

	australian, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID,
		exchange.MeasurementResolutionPlanRequest{
			Interpretation: exchange.MeasurementResolutionInterpretation{
				Kind: exchange.ResolutionConvention, Convention: string(units.ConventionAU),
			},
			ExpectedRevision: applied.Preview.Revision,
		})
	if err != nil {
		t.Fatal(err)
	}

	// The plan is built, and it reports the contradiction rather than letting
	// the newer convention overwrite the retained one.
	if len(australian.ValidationFailures) == 0 {
		t.Fatalf("a second convention was accepted for the same ingredient: %#v", australian)
	}

	if _, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID,
		australian.ID, applied.Preview.Revision); err == nil {
		t.Fatal("a conflicting convention was applied")
	}
}

func TestMeasurementResolutionDecisionsCollectSpellingsWithinASource(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})
	yaml := func(filename, name string, ingredients ...string) exchange.UploadedImport {
		var lines strings.Builder
		fmt.Fprintf(&lines, "name: %s\ningredients:\n", name)

		for _, ingredient := range ingredients {
			fmt.Fprintf(&lines, "  - %s\n", ingredient)
		}

		lines.WriteString("directions:\n  - Mix.\n")

		return exchange.UploadedImport{Filename: filename, Data: []byte(lines.String())}
	}

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{
		yaml("book.yml", "One", "1 cup flour", "2 cups sugar", "1 tbsp oil"),
		yaml("book.yml", "Two", "3 cups oats", "2 teaspoons salt"),
		yaml("cards.yml", "Three", "1 cup milk"),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Five alias groups, because two uploads wrote `cup` and one of them also
	// wrote `cups`; four questions, because that is how many measures the
	// sources left unidentified.
	if len(preview.ResolutionGroups) != 5 {
		t.Fatalf("groups = %#v", preview.ResolutionGroups)
	}

	if len(preview.ResolutionDecisions) != 4 {
		t.Fatalf("decisions = %#v", preview.ResolutionDecisions)
	}

	cups := preview.ResolutionDecisions[0]
	if cups.MeasureLabel != "cups" || cups.SourceLabel != "book.yml" ||
		cups.Occurrences != 3 || cups.Recipes != 2 || cups.Ingredients != 3 || len(cups.GroupIDs) != 2 {
		t.Fatalf("cups = %#v", cups)
	}

	if !slices.Equal(cups.AuthoredForms, []string{"cups", "cup"}) {
		t.Fatalf("authored forms = %#v", cups.AuthoredForms)
	}

	// The other upload's cup is a separate question: an answer given for one
	// source is not evidence about another.
	var separate bool

	for _, decision := range preview.ResolutionDecisions {
		if decision.SourceLabel == "cards.yml" {
			separate = decision.Occurrences == 1 && decision.MeasureLabel == "cup"
		}
	}

	if !separate {
		t.Fatalf("decisions = %#v", preview.ResolutionDecisions)
	}

	for _, choice := range cups.Choices {
		if choice.ConventionLabel == "" || choice.Magnitude == "" || choice.Label == choice.UnitID {
			t.Fatalf("choice = %#v", choice)
		}
	}

	// One answer settles every spelling the decision covers.
	plan, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID, exchange.MeasurementResolutionPlanRequest{
		GroupIDs:         cups.GroupIDs,
		Interpretation:   exchange.MeasurementResolutionInterpretation{Kind: exchange.ResolutionCanonicalUnit, UnitID: "volume.cup.us.customary"},
		ExpectedRevision: preview.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}

	if plan.AffectedOccurrences != 3 || plan.AffectedRecipes != 2 || len(plan.ValidationFailures) != 0 {
		t.Fatalf("plan = %#v", plan)
	}

	applied, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, plan.ID, preview.Revision)
	if err != nil {
		t.Fatal(err)
	}

	if applied.Preview == nil {
		t.Fatal("no preview returned")
	}

	for _, decision := range applied.Preview.ResolutionDecisions {
		if decision.SourceLabel == "book.yml" && decision.MeasureLabel == "cups" {
			t.Fatalf("answered decision still asked: %#v", decision)
		}
	}
}

func TestMeasurementResolutionKeepsUnreadableAmountsOutOfTheWayOfTheRest(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
		Filename: "wings.yml",
		Data: []byte("name: Wings\ningredients:\n  - 1 cup flour\n  - 2 cups sugar\n" +
			"  - ½ 3/4 cup hot sauce\ndirections:\n  - Mix.\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	decision := preview.ResolutionDecisions[0]

	plan, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID, exchange.MeasurementResolutionPlanRequest{
		GroupIDs:         decision.GroupIDs,
		Interpretation:   exchange.MeasurementResolutionInterpretation{Kind: exchange.ResolutionCanonicalUnit, UnitID: "volume.cup.us.customary"},
		ExpectedRevision: preview.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The unreadable amount is named as one that keeps its wording. It is not
	// a validation failure, because refusing the two good measures over it
	// leaves a person with no way through at all.
	if plan.AffectedOccurrences != 2 || len(plan.UnresolvableOccurrences) != 1 || len(plan.ValidationFailures) != 0 {
		t.Fatalf("plan = %#v", plan)
	}

	if plan.UnresolvableOccurrences[0].SourceText != "½ 3/4 cup hot sauce" {
		t.Fatalf("unresolvable = %#v", plan.UnresolvableOccurrences[0])
	}

	if _, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, plan.ID, preview.Revision); err != nil {
		t.Fatalf("apply = %v", err)
	}
}

func TestMeasurementResolutionStopsReportingReviewItRetainedAnAnswerFor(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
		Filename: "compound.yml",
		Data:     []byte("name: Compound\ningredients:\n  - 1 tablespoon plus 2 teaspoons oil\ndirections:\n  - Mix.\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	actionable := func(preview exchange.ImportPreview) int {
		total := 0

		for _, item := range preview.Items {
			for _, diagnostic := range item.Diagnostics {
				if diagnostic.Class == importer.DiagnosticActionable {
					total += len(diagnostic.Targets)
				}
			}
		}

		return total
	}

	if actionable(preview) != 2 {
		t.Fatalf("actionable = %d", actionable(preview))
	}

	answer := func(current exchange.ImportPreview, unitID string) exchange.ImportPreview {
		t.Helper()

		var decision exchange.MeasurementResolutionDecision

		for _, candidate := range current.ResolutionDecisions {
			if slices.Contains(candidate.CandidateUnitIDs, unitID) {
				decision = candidate
			}
		}

		if decision.ID == "" {
			t.Fatalf("no decision offers %s: %#v", unitID, current.ResolutionDecisions)
		}

		plan, err := service.CreateMeasurementResolutionPlan(context.Background(), current.ID, exchange.MeasurementResolutionPlanRequest{
			GroupIDs:         decision.GroupIDs,
			Interpretation:   exchange.MeasurementResolutionInterpretation{Kind: exchange.ResolutionCanonicalUnit, UnitID: unitID},
			ExpectedRevision: current.Revision,
		})
		if err != nil {
			t.Fatal(err)
		}

		applied, err := service.ApplyMeasurementResolutionPlan(context.Background(), current.ID, plan.ID, current.Revision)
		if err != nil {
			t.Fatal(err)
		}

		return *applied.Preview
	}

	// The tablespoon is answered first. Nothing can be stored yet, and the
	// answer is retained — so the teaspoon is the only question left, and the
	// recipe reports one measure to review rather than two.
	held := answer(preview, "volume.tablespoon.us.customary")
	if actionable(held) != 1 {
		t.Fatalf("actionable after partial answer = %d", actionable(held))
	}

	finished := answer(held, "volume.teaspoon.us.customary")
	if actionable(finished) != 0 || len(finished.ResolutionDecisions) != 0 {
		t.Fatalf("finished = %d actionable, %#v", actionable(finished), finished.ResolutionDecisions)
	}
}

func TestMeasurementDecisionsFollowTheWorkThatIsLeftBehind(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})
	yaml := func(name string, ingredients ...string) exchange.UploadedImport {
		var lines strings.Builder
		fmt.Fprintf(&lines, "name: %s\ningredients:\n", name)

		for _, ingredient := range ingredients {
			fmt.Fprintf(&lines, "  - %s\n", ingredient)
		}

		lines.WriteString("directions:\n  - Mix.\n")

		return exchange.UploadedImport{Filename: "book.yml", Data: []byte(lines.String())}
	}

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{
		yaml("Taken", "1 cup flour"),
		yaml("Left", "2 cups sugar", "1 tbsp oil"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(preview.ResolutionDecisions) != 2 {
		t.Fatalf("decisions = %#v", preview.ResolutionDecisions)
	}

	// One recipe is imported and the rest is worked through in a later pass.
	// The questions that come back have to be the ones the remainder still
	// asks, not the ones the whole upload did.
	plan, err := service.PlanPreviewImport(context.Background(), preview.ID,
		[]exchange.PreviewResolution{{ItemID: preview.Items[0].ID, Action: library.ImportAsSeparate}}, true)
	if err != nil {
		t.Fatal(err)
	}

	if plan.Remaining == nil || len(plan.Remaining.Items) != 1 {
		t.Fatalf("remaining = %#v", plan.Remaining)
	}

	if len(plan.Remaining.ResolutionDecisions) != 2 {
		t.Fatalf("remaining decisions = %#v", plan.Remaining.ResolutionDecisions)
	}

	for _, decision := range plan.Remaining.ResolutionDecisions {
		if decision.Recipes != 1 || decision.Occurrences != 1 {
			t.Fatalf("decision still counts the imported recipe: %#v", decision)
		}
	}

	// The remainder is still answerable, against its own revision.
	answered := plan.Remaining.ResolutionDecisions[0]

	resolution, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID, exchange.MeasurementResolutionPlanRequest{
		GroupIDs:         answered.GroupIDs,
		Interpretation:   exchange.MeasurementResolutionInterpretation{Kind: exchange.ResolutionCanonicalUnit, UnitID: answered.Choices[0].UnitID},
		ExpectedRevision: plan.Remaining.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, resolution.ID, plan.Remaining.Revision); err != nil {
		t.Fatalf("apply = %v", err)
	}
}

func TestAnsweringOneDecisionMakesTheOthersStale(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{})

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{
		Filename: "book.yml",
		Data:     []byte("name: Two\ningredients:\n  - 1 cup flour\n  - 1 tbsp oil\ndirections:\n  - Mix.\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	plans := make([]exchange.MeasurementResolutionPlan, 0, 2)

	for _, decision := range preview.ResolutionDecisions {
		plan, err := service.CreateMeasurementResolutionPlan(context.Background(), preview.ID, exchange.MeasurementResolutionPlanRequest{
			GroupIDs:         decision.GroupIDs,
			Interpretation:   exchange.MeasurementResolutionInterpretation{Kind: exchange.ResolutionCanonicalUnit, UnitID: decision.Choices[0].UnitID},
			ExpectedRevision: preview.Revision,
		})
		if err != nil {
			t.Fatal(err)
		}

		plans = append(plans, plan)
	}

	if _, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, plans[0].ID, preview.Revision); err != nil {
		t.Fatal(err)
	}

	// The second dry run described a preview that no longer exists, so it is
	// refused rather than applied against a moved revision.
	if _, err := service.ApplyMeasurementResolutionPlan(context.Background(), preview.ID, plans[1].ID,
		preview.Revision+1); !errors.Is(err, exchange.ErrResolutionPlanStale) {
		t.Fatalf("stale plan error = %v", err)
	}
}

type previewOwnerKey struct{}

func TestMeasurementDecisionsAreScopedToTheirOwner(t *testing.T) {
	t.Parallel()

	service := exchange.NewService(&importLibrary{}, &countingAssets{},
		exchange.WithPreviewScopeResolver(func(ctx context.Context) string {
			owner, _ := ctx.Value(previewOwnerKey{}).(string)
			return owner
		}))
	owner := context.WithValue(context.Background(), previewOwnerKey{}, "owner")

	preview, err := service.PreviewFiles(owner, []exchange.UploadedImport{{
		Filename: "book.yml",
		Data:     []byte("name: One\ningredients:\n  - 1 cup flour\ndirections:\n  - Mix.\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	decision := preview.ResolutionDecisions[0]
	stranger := context.WithValue(context.Background(), previewOwnerKey{}, "stranger")

	if _, err := service.MeasurementResolutionGroup(stranger, preview.ID, decision.GroupIDs[0]); !errors.Is(err, exchange.ErrPreviewNotFound) {
		t.Fatalf("group read by another owner = %v", err)
	}

	if _, err := service.CreateMeasurementResolutionPlan(stranger, preview.ID, exchange.MeasurementResolutionPlanRequest{
		GroupIDs:         decision.GroupIDs,
		Interpretation:   exchange.MeasurementResolutionInterpretation{Kind: exchange.ResolutionCanonicalUnit, UnitID: decision.Choices[0].UnitID},
		ExpectedRevision: preview.Revision,
	}); !errors.Is(err, exchange.ErrPreviewNotFound) {
		t.Fatalf("plan by another owner = %v", err)
	}
}

// A candidate the library cannot store is reported at review and kept out of
// the selection, not accepted and then refused at import.
func TestACandidateTheLibraryCouldNotStoreIsBlockedAtReviewRatherThanAtImport(t *testing.T) {
	service := exchange.NewService(&importLibrary{}, &countingAssets{})
	// A title past the recipe model's ceiling. Nothing later in the import
	// shortens it, so this recipe cannot be stored however well it read.
	body := fmt.Sprintf(`{"@type":"Recipe","name":%q,"recipeIngredient":["water"],"recipeInstructions":["Cook."]}`,
		strings.Repeat("long ", 400))

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{Filename: "wide.jsonld", Data: []byte(body)}})
	if err != nil {
		t.Fatal(err)
	}

	item := preview.Items[0]
	if item.CanCommit {
		t.Fatalf("a recipe the library would refuse was offered for import: %#v", item.Diagnostics)
	}

	if !hasDiagnostic(item.Diagnostics, "candidate-invalid", exchange.SeverityError) {
		t.Fatalf("diagnostics = %#v", item.Diagnostics)
	}
	// And it says which field, because "could not be imported" is not
	// something a reader can act on.
	var said bool

	for _, diagnostic := range item.Diagnostics {
		if diagnostic.Code == "candidate-invalid" && strings.Contains(diagnostic.Message, "title") {
			said = true
		}
	}

	if !said {
		t.Fatalf("no diagnostic named the field: %#v", item.Diagnostics)
	}
}

// A step photograph is attached at commit, so the candidate under review names
// an asset that does not exist yet. That is the ordinary case and must not be
// mistaken for a recipe that cannot be stored.
func TestAPhotographNotAttachedYetDoesNotBlockReview(t *testing.T) {
	service := exchange.NewService(&importLibrary{}, &countingAssets{})
	body := `{"@type":"Recipe","name":"Pictured","image":["https://img.example/photo.jpg"],
		"recipeIngredient":["water"],"recipeInstructions":[{"@type":"HowToStep","text":"Cook.","image":"https://img.example/step.jpg"}]}`

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{Filename: "pictured.jsonld", Data: []byte(body)}})
	if err != nil {
		t.Fatal(err)
	}

	item := preview.Items[0]
	if !item.CanCommit || hasDiagnostic(item.Diagnostics, "candidate-invalid", exchange.SeverityError) {
		t.Fatalf("a recipe whose photographs arrive at commit was blocked: %#v", item.Diagnostics)
	}
}

// ratingLibrary refuses the library details an import carried, the way the real
// organization service refuses a rating it cannot hold.
type ratingLibrary struct {
	importLibrary
	applied int
}

func (f *ratingLibrary) ApplyImportedLibraryMetadata(context.Context, string, library.ImportedLibraryMetadata) error {
	f.applied++

	return fmt.Errorf("rating must be between 1 and 5: %w", library.ErrInvalidOrganization)
}

// metadataAdapter hands every recipe library details alongside the document.
type metadataAdapter struct{ documents []recipe.Document }

func (metadataAdapter) Format() importer.Format {
	return importer.Format{ID: "metadata", Name: "Metadata source", Version: "1", Extensions: []string{".metadata"}}
}

func (metadataAdapter) Probe(context.Context, importer.Input) importer.ProbeResult {
	return importer.ProbeResult{Confidence: importer.ConfidenceContent, Reason: "metadata signature"}
}

func (adapter metadataAdapter) Import(context.Context, importer.Input, importer.Options) (importer.ImportBatch, error) {
	rating := uint8(4)

	batch := importer.ImportBatch{}
	for _, document := range adapter.documents {
		batch.Recipes = append(batch.Recipes, importer.ImportedRecipe{
			Document: document, LibraryMetadata: importer.LibraryMetadata{Rating: &rating},
		})
	}

	return batch, nil
}

// The recipes are already stored when their ratings are applied, so a value
// this library will not hold is a diagnostic on one recipe, not a failed call.
func TestLibraryDetailsThatCannotBeAppliedDoNotUndoTheImport(t *testing.T) {
	t.Parallel()

	documents := make([]recipe.Document, 0, 3)

	for _, title := range []string{"First", "Second", "Third"} {
		document := testfixture.Document()
		document.ID, document.Title = recipe.RecipeID("metadata-"+strings.ToLower(title)), title
		documents = append(documents, document)
	}

	recipes := &ratingLibrary{}
	service := exchange.NewService(recipes, &countingAssets{},
		exchange.WithImporterRegistry(importer.NewRegistry(metadataAdapter{documents: documents})))

	results, err := service.ImportSources(context.Background(),
		[]importer.Input{{Name: "export.metadata", Data: []byte("metadata")}}, importer.Options{}, library.DuplicateSkip)
	if err != nil {
		t.Fatalf("one unusable rating failed the whole import: %v", err)
	}

	if len(results) != len(documents) {
		t.Fatalf("results = %#v", results)
	}

	if recipes.applied != len(documents) {
		t.Fatalf("library details were applied %d times, want %d", recipes.applied, len(documents))
	}

	for _, result := range results {
		if result.Outcome != library.ImportCreated {
			t.Fatalf("%q outcome = %q", result.Title, result.Outcome)
		}

		if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "rating must be between 1 and 5") {
			t.Fatalf("%q warnings = %#v", result.Title, result.Warnings)
		}
	}
}
