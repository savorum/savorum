package exchange_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/sref"
)

// sizedFetcher returns photographs of a chosen size so a test can drive the
// import byte allowance rather than the request count.
type sizedFetcher struct {
	fetches int
	size    int
}

func (f *sizedFetcher) Fetch(context.Context, string) (exchange.FetchedAsset, error) {
	f.fetches++

	return exchange.FetchedAsset{
		Body:      io.NopCloser(strings.NewReader(strings.Repeat("j", f.size))),
		MediaType: "image/jpeg", Size: int64(f.size),
	}, nil
}

type slowFetcher struct {
	fetches int
	delay   time.Duration
}

func (f *slowFetcher) Fetch(ctx context.Context, _ string) (exchange.FetchedAsset, error) {
	f.fetches++
	select {
	case <-time.After(f.delay):
	case <-ctx.Done():
		return exchange.FetchedAsset{}, ctx.Err()
	}

	return exchange.FetchedAsset{Body: io.NopCloser(strings.NewReader("jpeg")), MediaType: "image/jpeg", Size: 4}, nil
}

type hangingFetcher struct{ fetches int }

func (f *hangingFetcher) Fetch(ctx context.Context, _ string) (exchange.FetchedAsset, error) {
	f.fetches++

	<-ctx.Done()

	return exchange.FetchedAsset{}, ctx.Err()
}

func imagePreview(t *testing.T, service *exchange.Service, count int) exchange.ImportPreview {
	t.Helper()

	uploads := make([]exchange.UploadedImport, count)
	for index := range uploads {
		uploads[index] = exchange.UploadedImport{
			Filename: fmt.Sprintf("recipe-%d.jsonld", index+1),
			Data: fmt.Appendf(nil,
				`{"@type":"Recipe","name":"Recipe %d","image":"https://images.example/%d.jpg","recipeIngredient":["water"],"recipeInstructions":["Cook."]}`,
				index+1, index+1),
		}
	}

	preview, err := service.PreviewFiles(context.Background(), uploads)
	if err != nil {
		t.Fatal(err)
	}

	return preview
}

func allItemIDs(preview exchange.ImportPreview) []string {
	ids := make([]string, 0, len(preview.Items))
	for _, item := range preview.Items {
		ids = append(ids, item.ID)
	}

	return ids
}

// Images beyond the import byte allowance retain their remote references.
func TestCommitDefersImagesBeyondImportByteAllowance(t *testing.T) {
	fetcher := &sizedFetcher{size: 4 << 20}
	blobs := &countingAssets{}
	limits := exchange.DefaultRemoteImportLimits()
	limits.Bytes = 16 << 20
	service := exchange.NewService(&importLibrary{}, blobs,
		exchange.WithRemoteAssetFetcher(fetcher), exchange.WithRemoteImportLimits(limits))
	preview := imagePreview(t, service, 10)

	result, err := service.CommitPreview(context.Background(), preview.ID, allItemIDs(preview), library.DuplicateSkip)
	if err != nil {
		t.Fatal(err)
	}

	dropped, deferred := 0, 0
	for _, item := range result.Items {
		dropped += item.DroppedAssets
		deferred += item.DeferredAssets
	}

	if blobs.ingests != 4 || dropped != 0 || deferred != 6 {
		t.Fatalf("ingests=%d dropped=%d deferred=%d", blobs.ingests, dropped, deferred)
	}
}

// A large selection imports every recipe within the remote request allowance.
func TestLargeImportKeepsEveryRecipeWithinImageLimits(t *testing.T) {
	const recipes = 124

	fetcher := &staticFetcher{}
	blobs := &countingAssets{}
	service := exchange.NewService(&importLibrary{}, blobs, exchange.WithRemoteAssetFetcher(fetcher))

	preview := imagePreview(t, service, recipes)
	if len(preview.Items) != recipes {
		t.Fatalf("previewed %d of %d", len(preview.Items), recipes)
	}

	previewed, deferredAtPreview := 0, 0

	for _, item := range preview.Items {
		if !item.CanCommit {
			t.Fatalf("a bounded preview made %q unselectable", item.ID)
		}

		previewed += item.Assets.Available
		deferredAtPreview += item.Assets.Deferred
	}
	// The preview stays bounded: it does not fetch all 124.
	if previewed >= recipes || previewed+deferredAtPreview != recipes {
		t.Fatalf("previewed=%d deferred=%d", previewed, deferredAtPreview)
	}

	result, err := service.CommitPreview(context.Background(), preview.ID, allItemIDs(preview), library.DuplicateSkip)
	if err != nil {
		t.Fatal(err)
	}

	dropped, deferred := 0, 0
	for _, item := range result.Items {
		dropped += item.DroppedAssets
		deferred += item.DeferredAssets
	}

	if len(result.Items) != recipes || blobs.ingests != exchange.DefaultRemoteImportLimits().Requests || dropped != 0 || deferred != recipes-exchange.DefaultRemoteImportLimits().Requests {
		t.Fatalf("imported=%d images=%d dropped=%d deferred=%d", len(result.Items), blobs.ingests, dropped, deferred)
	}
}

// Requests share one allowance across the selection.
func TestCommitDefersImagesBeyondImportRequestAllowance(t *testing.T) {
	fetcher := &staticFetcher{}
	blobs := &countingAssets{}
	limits := exchange.DefaultRemoteImportLimits()
	limits.Requests = 2
	service := exchange.NewService(&importLibrary{}, blobs,
		exchange.WithRemoteAssetFetcher(fetcher), exchange.WithRemoteImportLimits(limits))

	preview := imagePreview(t, service, 9)
	if _, err := service.CommitPreview(context.Background(), preview.ID, allItemIDs(preview), library.DuplicateSkip); err != nil {
		t.Fatal(err)
	}

	if blobs.ingests != 2 || fetcher.fetches != 2 {
		t.Fatalf("ingests=%d", blobs.ingests)
	}
}

// The remote deadline leaves valid recipes available for import.
func TestStagingDeadlineDoesNotFailTheImport(t *testing.T) {
	fetcher := &slowFetcher{delay: 15 * time.Millisecond}
	blobs := &countingAssets{}
	limits := exchange.DefaultRemoteImportLimits()
	limits.Duration = 40 * time.Millisecond
	service := exchange.NewService(&importLibrary{}, blobs,
		exchange.WithRemoteAssetFetcher(fetcher), exchange.WithRemoteImportLimits(limits))
	preview := imagePreview(t, service, 12)

	result, err := service.CommitPreview(context.Background(), preview.ID, allItemIDs(preview), library.DuplicateSkip)
	if err != nil {
		t.Fatalf("commit of 12 valid recipes failed: %v", err)
	}

	if len(result.Items) != 12 {
		t.Fatalf("imported %d of 12", len(result.Items))
	}
}

// A deadline ends all remote work in the import.
func TestStagingTerminatesAtImportDeadline(t *testing.T) {
	fetcher := &hangingFetcher{}
	limits := exchange.DefaultRemoteImportLimits()
	limits.Duration = 20 * time.Millisecond
	service := exchange.NewService(&importLibrary{}, &countingAssets{},
		exchange.WithRemoteAssetFetcher(fetcher), exchange.WithRemoteImportLimits(limits))
	preview := imagePreview(t, service, 3)
	result := commitWithin(t, service, preview, 5*time.Second)

	deferred := 0
	for _, item := range result.Items {
		deferred += item.DeferredAssets
	}

	if len(result.Items) != 3 || deferred != 3 || fetcher.fetches != 1 {
		t.Fatalf("items=%d deferred=%d", len(result.Items), deferred)
	}
}

// An oversized image exhausts the byte allowance and defers remaining images.
func TestStagingOversizedImageExhaustsImportAllowance(t *testing.T) {
	fetcher := &sizedFetcher{size: 3 << 20}
	limits := exchange.DefaultRemoteImportLimits()
	limits.Bytes = 1 << 20
	service := exchange.NewService(&importLibrary{}, &countingAssets{},
		exchange.WithRemoteAssetFetcher(fetcher), exchange.WithRemoteImportLimits(limits))
	preview := imagePreview(t, service, 2)
	result := commitWithin(t, service, preview, 5*time.Second)

	dropped := 0
	for _, item := range result.Items {
		dropped += item.DroppedAssets
	}

	if len(result.Items) != 2 || dropped != 1 || result.Items[0].DeferredAssets+result.Items[1].DeferredAssets != 1 || fetcher.fetches != 1 {
		t.Fatalf("items=%d dropped=%d", len(result.Items), dropped)
	}
}

// Outbound work for one recipe stays inside its allowance however many
// photographs an uploaded file names.
func TestOneRecipeCannotExceedItsPerRecipeAssetAllowance(t *testing.T) {
	fetcher := &staticFetcher{}
	limits := exchange.DefaultRemoteImportLimits()
	limits.AssetsPerRecipe = 3
	service := exchange.NewService(&importLibrary{}, &countingAssets{},
		exchange.WithRemoteAssetFetcher(fetcher), exchange.WithRemoteImportLimits(limits))

	urls := make([]string, 40)
	for index := range urls {
		urls[index] = fmt.Sprintf("%q", fmt.Sprintf("https://images.example/host-%d.jpg", index))
	}

	body := fmt.Sprintf(`{"@type":"Recipe","name":"Greedy","image":[%s],"recipeIngredient":["water"],"recipeInstructions":["Cook."]}`,
		strings.Join(urls, ","))

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{Filename: "greedy.jsonld", Data: []byte(body)}})
	if err != nil {
		t.Fatal(err)
	}

	before := fetcher.fetches

	if _, err := service.CommitPreview(context.Background(), preview.ID, []string{preview.Items[0].ID}, library.DuplicateSkip); err != nil {
		t.Fatal(err)
	}

	if made := fetcher.fetches - before; made > limits.AssetsPerRecipe {
		t.Fatalf("one recipe provoked %d outbound requests, allowance is %d", made, limits.AssetsPerRecipe)
	}
}

// Aggregate outbound work is proportional to the selection a person made,
// never to the number of URLs the uploaded files claim.
func TestAggregateOutboundWorkIsProportionalToTheSelection(t *testing.T) {
	fetcher := &staticFetcher{}
	limits := exchange.DefaultRemoteImportLimits()
	limits.AssetsPerRecipe = 2
	service := exchange.NewService(&importLibrary{}, &countingAssets{},
		exchange.WithRemoteAssetFetcher(fetcher), exchange.WithRemoteImportLimits(limits))
	preview := imagePreview(t, service, 6)
	selected := []string{preview.Items[0].ID, preview.Items[1].ID}
	before := fetcher.fetches

	if _, err := service.CommitPreview(context.Background(), preview.ID, selected, library.DuplicateSkip); err != nil {
		t.Fatal(err)
	}

	if made := fetcher.fetches - before; made > len(selected)*limits.AssetsPerRecipe {
		t.Fatalf("%d outbound requests for %d selected recipes", made, len(selected))
	}
}

// A deferred photograph keeps its address, so the recipe still records what it
// names and a later attempt has somewhere to start.
func TestDeferredPhotographsRetainTheirReferences(t *testing.T) {
	fetcher := &hangingFetcher{}
	recipes := &importLibrary{}
	limits := exchange.DefaultRemoteImportLimits()
	limits.Duration = 20 * time.Millisecond
	service := exchange.NewService(recipes, &countingAssets{},
		exchange.WithRemoteAssetFetcher(fetcher), exchange.WithRemoteImportLimits(limits))
	preview := imagePreview(t, service, 1)
	commitWithin(t, service, preview, 5*time.Second)

	if len(recipes.imported) != 1 {
		t.Fatalf("imported %d recipes", len(recipes.imported))
	}

	pending, ok := recipes.imported[0].Extensions["x-org.savorum.pending-images"].(map[string]any)
	if !ok {
		t.Fatalf("extensions = %#v", recipes.imported[0].Extensions)
	}

	deferred, ok := pending["deferred"].([]any)
	if !ok || len(deferred) != 1 {
		t.Fatalf("pending = %#v", pending)
	}

	entry, ok := deferred[0].(map[string]any)
	if !ok || entry["url"] != "https://images.example/1.jpg" {
		t.Fatalf("entry = %#v", deferred[0])
	}
	// The retained reference travels with the recipe, so exporting a recipe
	// whose photograph is still outstanding stays valid SREF.
	encoded, err := sref.Encode(recipes.imported[0])
	if err != nil {
		t.Fatalf("encode a recipe carrying a retained reference: %v", err)
	}

	decoded, err := sref.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, ok := decoded.Extensions["x-org.savorum.pending-images"]; !ok {
		t.Fatalf("round trip lost the retained reference: %#v", decoded.Extensions)
	}
}

// deadFetcher is a link that has rotted on somebody else's server: an answer
// no retry can change, which is why it says so.
type deadFetcher struct{ fetches int }

func (f *deadFetcher) Fetch(context.Context, string) (exchange.FetchedAsset, error) {
	f.fetches++
	return exchange.FetchedAsset{}, fmt.Errorf("%w: remote server returned 404 Not Found", exchange.ErrRemoteAssetUnusable)
}

// flakyFetcher fails a fixed number of times and then works, the way a server
// having a bad minute does.
type flakyFetcher struct {
	failures map[string]int
	fetches  int
}

func (f *flakyFetcher) Fetch(_ context.Context, url string) (exchange.FetchedAsset, error) {
	f.fetches++
	if f.failures[url] > 0 {
		f.failures[url]--
		return exchange.FetchedAsset{}, errors.New("connection reset by peer")
	}

	data := []byte("jpeg-bytes")

	return exchange.FetchedAsset{Body: io.NopCloser(bytes.NewReader(data)), MediaType: "image/jpeg", Size: int64(len(data))}, nil
}

// A transient fetch failure is retried, not reported as a lost photograph.
func TestATransientFailureIsRetriedRatherThanReportedAsLoss(t *testing.T) {
	fetcher := &flakyFetcher{failures: map[string]int{}}
	blobs := &countingAssets{}
	service := exchange.NewService(&importLibrary{}, blobs, exchange.WithRemoteAssetFetcher(fetcher))
	preview := imagePreview(t, service, 3)
	// Every photograph refuses once before it works.
	for index := range 3 {
		fetcher.failures[fmt.Sprintf("https://images.example/%d.jpg", index+1)] = 1
	}

	result, err := service.CommitPreview(context.Background(), preview.ID, allItemIDs(preview), library.DuplicateSkip)
	if err != nil {
		t.Fatal(err)
	}

	dropped, deferred := 0, 0
	for _, item := range result.Items {
		dropped += item.DroppedAssets
		deferred += item.DeferredAssets
	}

	if dropped != 0 || deferred != 0 || blobs.ingests != 3 {
		t.Fatalf("dropped=%d deferred=%d ingests=%d", dropped, deferred, blobs.ingests)
	}
	// Six attempts for three photographs: the refusal happened and the retry
	// is what saved them, rather than the fetcher having quietly succeeded.
	if fetcher.fetches != 6 {
		t.Fatalf("fetches=%d, want each photograph refused once and then fetched", fetcher.fetches)
	}
}

// A photograph that exhausts the retry allowance is outstanding work rather
// than a loss: the recipe keeps its address.
func TestAPhotographThatNeverAnswersBecomesOutstandingWorkNotLoss(t *testing.T) {
	fetcher := &failingFetcher{}
	recipes := &importLibrary{}
	service := exchange.NewService(recipes, &countingAssets{}, exchange.WithRemoteAssetFetcher(fetcher))
	preview := imagePreview(t, service, 2)

	result, err := service.CommitPreview(context.Background(), preview.ID, allItemIDs(preview), library.DuplicateSkip)
	if err != nil {
		t.Fatal(err)
	}

	dropped, deferred := 0, 0
	for _, item := range result.Items {
		dropped += item.DroppedAssets
		deferred += item.DeferredAssets
	}

	if dropped != 0 || deferred != 2 {
		t.Fatalf("dropped=%d deferred=%d", dropped, deferred)
	}
	// Three attempts each, and no more: outbound work stays proportional to
	// the selection however badly the far end behaves.
	if fetcher.fetches != 6 {
		t.Fatalf("fetches=%d, want 2 photographs at 3 attempts each", fetcher.fetches)
	}

	pending, ok := recipes.imported[0].Extensions["x-org.savorum.pending-images"].(map[string]any)
	if !ok {
		t.Fatalf("an unfetched photograph lost its address: %#v", recipes.imported[0].Extensions)
	}

	if _, ok := pending["deferred"].([]any); !ok {
		t.Fatalf("pending = %#v", pending)
	}
}

// meteredAssets refuses admission after a fixed number of ingests, the way the
// asset service's per-minute upload meter does.
type meteredAssets struct {
	allowed  int
	ingests  int
	refusals int
}

func (m *meteredAssets) Ingest(_ context.Context, source io.Reader, _ assets.IngestOptions) (assets.Blob, error) {
	if m.ingests >= m.allowed {
		m.refusals++
		return assets.Blob{}, assets.ErrBusy
	}

	data, err := io.ReadAll(source)
	if err != nil {
		return assets.Blob{}, fmt.Errorf("read metered test asset: %w", err)
	}

	m.ingests++
	digest := sha256.Sum256(data)

	return assets.Blob{
		ID: assets.ID(hex.EncodeToString(digest[:8])), ContentHash: hex.EncodeToString(digest[:]),
		MediaType: "image/jpeg", Size: int64(len(data)),
	}, nil
}

func (*meteredAssets) Open(context.Context, assets.ID) (assets.Blob, io.ReadCloser, error) {
	return assets.Blob{}, nil, errors.New("not used")
}

// The asset service's per-minute upload meter must not turn fetched
// photographs into reported losses.
func TestAnImportIsNotThrottledIntoLosingPhotographs(t *testing.T) {
	fetcher := &staticFetcher{}
	// Room for every photograph but the last four, and no waiting: the import
	// must not report the refused ones as pictures it could not download.
	blobs := &meteredAssets{allowed: 6}
	service := exchange.NewService(&importLibrary{}, blobs,
		exchange.WithRemoteAssetFetcher(fetcher), exchange.WithIngestRetry(0, 0))
	preview := imagePreview(t, service, 10)

	result, err := service.CommitPreview(context.Background(), preview.ID, allItemIDs(preview), library.DuplicateSkip)
	if err != nil {
		t.Fatal(err)
	}

	dropped, deferred := 0, 0
	for _, item := range result.Items {
		dropped += item.DroppedAssets
		deferred += item.DeferredAssets
	}

	if len(result.Items) != 10 {
		t.Fatalf("recipes = %d", len(result.Items))
	}

	if dropped != 0 || deferred != 4 {
		t.Fatalf("dropped=%d deferred=%d, want the four refused photographs reported as outstanding", dropped, deferred)
	}

	if blobs.refusals == 0 {
		t.Fatal("the meter never refused, so this test proves nothing")
	}
}

// With the ordinary retry allowance the import paces itself and keeps every
// photograph.
func TestAnImportWaitsOutTheUploadMeterAndKeepsEveryPhotograph(t *testing.T) {
	fetcher := &staticFetcher{}
	blobs := &openingMeteredAssets{allowed: 6}
	service := exchange.NewService(&importLibrary{}, blobs,
		exchange.WithRemoteAssetFetcher(fetcher), exchange.WithIngestRetry(4, time.Millisecond))
	preview := imagePreview(t, service, 10)

	result, err := service.CommitPreview(context.Background(), preview.ID, allItemIDs(preview), library.DuplicateSkip)
	if err != nil {
		t.Fatal(err)
	}

	dropped, deferred := 0, 0
	for _, item := range result.Items {
		dropped += item.DroppedAssets
		deferred += item.DeferredAssets
	}

	if dropped != 0 || deferred != 0 || blobs.ingests != 10 {
		t.Fatalf("dropped=%d deferred=%d ingests=%d", dropped, deferred, blobs.ingests)
	}
}

// openingMeteredAssets refuses once and then lets the photograph through, the
// way a per-minute window does when the import waits for it.
type openingMeteredAssets struct{ meteredAssets }

func (m *openingMeteredAssets) Ingest(ctx context.Context, source io.Reader, options assets.IngestOptions) (assets.Blob, error) {
	blob, err := m.meteredAssets.Ingest(ctx, source, options)
	if err != nil {
		m.allowed++
	}

	return blob, err
}

// A rotted link is unavailable, not outstanding, and still keeps its address.
func TestDeadLinkIsReportedAsUnavailableNotDeferred(t *testing.T) {
	fetcher := &deadFetcher{}
	recipes := &importLibrary{}
	service := exchange.NewService(recipes, &countingAssets{}, exchange.WithRemoteAssetFetcher(fetcher))
	preview := imagePreview(t, service, 2)

	result, err := service.CommitPreview(context.Background(), preview.ID, allItemIDs(preview), library.DuplicateSkip)
	if err != nil {
		t.Fatal(err)
	}

	dropped, deferred := 0, 0
	for _, item := range result.Items {
		dropped += item.DroppedAssets
		deferred += item.DeferredAssets
	}

	if dropped != 2 || deferred != 0 {
		t.Fatalf("dropped=%d deferred=%d", dropped, deferred)
	}

	pending, ok := recipes.imported[0].Extensions["x-org.savorum.pending-images"].(map[string]any)
	if !ok {
		t.Fatalf("a rotted link lost its address entirely: %#v", recipes.imported[0].Extensions)
	}

	if _, ok := pending["unresolved"].([]any); !ok {
		t.Fatalf("pending = %#v", pending)
	}
}

func commitWithin(t *testing.T, service *exchange.Service, preview exchange.ImportPreview, limit time.Duration) exchange.ImportCommitResult {
	t.Helper()

	type outcome struct {
		result exchange.ImportCommitResult
		err    error
	}

	done := make(chan outcome, 1)

	go func() {
		result, err := service.CommitPreview(context.Background(), preview.ID, allItemIDs(preview), library.DuplicateSkip)
		done <- outcome{result, err}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("commit: %v", got.err)
		}

		return got.result
	case <-time.After(limit):
		t.Fatal("staging did not terminate")
		return exchange.ImportCommitResult{}
	}
}
