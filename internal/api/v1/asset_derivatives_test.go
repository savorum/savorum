package v1

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/imaging"
	"github.com/savorum/savorum/internal/library"
)

func TestAssetDerivativeRouteScopeAndHTTPMetadata(t *testing.T) {
	data := derivativeTestPNG(t)
	digest := strings.Repeat("d", 64)
	service := &derivativeAssetService{blob: assets.Blob{ID: assets.ID("sha256:" + digest), ContentHash: digest, MediaType: "image/png", Size: int64(len(data))}, data: data}
	references := headAssetReferences{entry: library.Entry{ID: "r1", Recipe: recipeWithAsset(digest)}}
	server := newRealAPIServer(t, func(api huma.API) { registerAssets(api, references, service) })
	path := "/api/v1/recipes/r1/assets/sha256:" + digest + "/derivatives/card"
	get := doServerRequest(t, server, http.MethodGet, path, "")
	body, err := io.ReadAll(get.Body)
	_ = get.Body.Close()

	if err != nil {
		t.Fatal(err)
	}

	if get.StatusCode != http.StatusOK || len(body) == 0 || get.Header.Get("Cache-Control") != "private, no-cache, must-revalidate" {
		t.Fatalf("GET status=%d bytes=%d headers=%v", get.StatusCode, len(body), get.Header)
	}

	if bytes.Equal(body, data) {
		t.Fatal("derivative returned original bytes")
	}

	head := doServerRequest(t, server, http.MethodHead, path, "")
	headBody, _ := io.ReadAll(head.Body)
	_ = head.Body.Close()

	if head.StatusCode != http.StatusOK || len(headBody) != 0 {
		t.Fatalf("HEAD status=%d body=%d", head.StatusCode, len(headBody))
	}

	for _, key := range []string{"Content-Type", "Content-Length", "Cache-Control", "Content-Security-Policy", "Cross-Origin-Resource-Policy", "X-Content-Type-Options"} {
		if head.Header.Get(key) != get.Header.Get(key) {
			t.Fatalf("%s GET=%q HEAD=%q", key, get.Header.Get(key), head.Header.Get(key))
		}
	}

	denied := doServerRequest(t, server, http.MethodGet, "/api/v1/recipes/nope/assets/sha256:"+digest+"/derivatives/card", "")
	_ = denied.Body.Close()

	if denied.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong recipe status=%d", denied.StatusCode)
	}

	for _, location := range []string{path, "/api/v1/recipes/nope/assets/sha256:" + digest + "/derivatives/card"} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+location, nil)
		if err != nil {
			t.Fatal(err)
		}

		request.Header.Set("If-None-Match", "W/"+get.Header.Get("ETag"))

		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}

		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()

		if err != nil {
			t.Fatal(err)
		}

		want := http.StatusNotModified
		if location != path {
			want = http.StatusNotFound
		}

		if response.StatusCode != want {
			t.Fatalf("conditional read %s = %d", location, response.StatusCode)
		}

		if want == http.StatusNotModified && (len(body) != 0 || response.Header.Get("ETag") != get.Header.Get("ETag")) {
			t.Fatalf("revalidation returned bytes or lost its ETag: %v", response.Header)
		}
	}
}

func TestDerivativeCacheEvictionRegeneratesWithoutChangingOriginal(t *testing.T) {
	data := derivativeTestPNG(t)
	digest := strings.Repeat("e", 64)
	service := &derivativeAssetService{blob: assets.Blob{ID: assets.ID("sha256:" + digest), ContentHash: digest, MediaType: "image/png", Size: int64(len(data))}, data: data}
	presentationImages = derivativeCache{entries: make(map[string][]byte)}

	first, err := presentationImages.getOrCreate(context.Background(), service, service.blob.ID, imaging.DerivativeCard)
	if err != nil {
		t.Fatal(err)
	}

	presentationImages = derivativeCache{entries: make(map[string][]byte)} // simulate disposable cache deletion

	second, err := presentationImages.getOrCreate(context.Background(), service, service.blob.ID, imaging.DerivativeCard)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first, second) || service.opens != 2 {
		t.Fatalf("regeneration opens=%d equal=%t", service.opens, bytes.Equal(first, second))
	}

	if !bytes.Equal(data, service.data) {
		t.Fatal("original bytes changed")
	}
}

type derivativeAssetService struct {
	blob  assets.Blob
	data  []byte
	opens int
}

func (*derivativeAssetService) Ingest(context.Context, io.Reader, assets.IngestOptions) (assets.Blob, error) {
	return assets.Blob{}, nil
}

func (s *derivativeAssetService) Metadata(_ context.Context, id assets.ID) (assets.Blob, error) {
	if id != s.blob.ID {
		return assets.Blob{}, assets.ErrNotFound
	}

	return s.blob, nil
}

func (s *derivativeAssetService) Open(_ context.Context, id assets.ID) (assets.Blob, io.ReadCloser, error) {
	if id != s.blob.ID {
		return assets.Blob{}, nil, assets.ErrNotFound
	}

	s.opens++

	return s.blob, io.NopCloser(bytes.NewReader(s.data)), nil
}

func derivativeTestPNG(t *testing.T) []byte {
	t.Helper()

	picture := image.NewNRGBA(image.Rect(0, 0, 800, 400))

	for y := range 400 {
		for x := range 800 {
			picture.Set(x, y, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}

	var data bytes.Buffer
	if err := png.Encode(&data, picture); err != nil {
		t.Fatal(err)
	}

	return data.Bytes()
}

func TestDerivativeErrorsKeepUnsupportedFormatsApartFromStoreFailures(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		err    error
		status int
	}{
		{fmt.Errorf("decode derivative image: %w", imaging.ErrInvalid), http.StatusUnsupportedMediaType},
		{imaging.ErrUnsupported, http.StatusUnsupportedMediaType},
		{imaging.ErrTooLarge, http.StatusRequestEntityTooLarge},
		{assets.ErrNotFound, http.StatusNotFound},
		{fmt.Errorf("%w: stored bytes exceed their recorded size", assets.ErrIntegrity), http.StatusInternalServerError},
		{assets.ErrTooLarge, http.StatusRequestEntityTooLarge},
		{errors.New("disk failed"), http.StatusInternalServerError},
	} {
		var problem *Problem
		if !errors.As(derivativeError(testCase.err), &problem) || problem.Status != testCase.status {
			t.Errorf("derivativeError(%v) = %#v; want status %d", testCase.err, problem, testCase.status)
		}
	}
}

type waitingDerivativeService struct {
	derivativeAssetService
	entered chan struct{}
	release chan struct{}
}

func (s *waitingDerivativeService) Open(ctx context.Context, id assets.ID) (assets.Blob, io.ReadCloser, error) {
	close(s.entered)

	select {
	case <-s.release:
		return s.derivativeAssetService.Open(ctx, id)
	case <-ctx.Done():
		return assets.Blob{}, nil, ctx.Err()
	}
}

func TestCachedImagesAndCancelledWaitersDoNotWaitForAnotherDecode(t *testing.T) {
	data := derivativeTestPNG(t)
	warm := &derivativeAssetService{blob: assets.Blob{ID: "sha256:warm", Size: int64(len(data))}, data: data}

	cache := derivativeCache{entries: make(map[string][]byte)}
	if _, err := cache.getOrCreate(t.Context(), warm, warm.blob.ID, imaging.DerivativeCard); err != nil {
		t.Fatal(err)
	}

	cold := &waitingDerivativeService{derivativeAssetService: derivativeAssetService{blob: assets.Blob{ID: "sha256:cold", Size: int64(len(data))}, data: data}, entered: make(chan struct{}), release: make(chan struct{})}
	finished := make(chan error, 1)

	go func() {
		_, err := cache.getOrCreate(t.Context(), cold, cold.blob.ID, imaging.DerivativeCard)
		finished <- err
	}()

	<-cold.entered
	defer func() {
		close(cold.release)

		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := cache.getOrCreate(ctx, cold, cold.blob.ID, imaging.DerivativeCard); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled duplicate: %v", err)
	}

	cached := make(chan error, 1)

	go func() {
		_, err := cache.getOrCreate(t.Context(), warm, warm.blob.ID, imaging.DerivativeCard)
		cached <- err
	}()

	select {
	case err := <-cached:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("a cache hit waited behind image generation")
	}
}

type cancelFirstDerivativeService struct {
	derivativeAssetService
	once    sync.Once
	entered chan struct{}
}

func (s *cancelFirstDerivativeService) Open(ctx context.Context, id assets.ID) (assets.Blob, io.ReadCloser, error) {
	first := false

	s.once.Do(func() { first = true; close(s.entered) })

	if first {
		<-ctx.Done()
		return assets.Blob{}, nil, ctx.Err()
	}

	return s.derivativeAssetService.Open(ctx, id)
}

type observedWaitContext struct {
	context.Context //nolint:containedctx // A context decorator observes admission to a shared decode.
	once            sync.Once
	waiting         chan struct{}
}

func (ctx *observedWaitContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.waiting) })
	return ctx.Context.Done()
}

func TestCancelledImageGenerationDoesNotCancelAnotherCaller(t *testing.T) {
	data := derivativeTestPNG(t)
	service := &cancelFirstDerivativeService{derivativeAssetService: derivativeAssetService{blob: assets.Blob{ID: "sha256:cancel-first", Size: int64(len(data))}, data: data}, entered: make(chan struct{})}
	cache := derivativeCache{}

	first, cancel := context.WithCancel(t.Context())
	defer cancel()

	firstResult := make(chan error, 1)

	go func() {
		_, err := cache.getOrCreate(first, service, service.blob.ID, imaging.DerivativeCard)
		firstResult <- err
	}()

	<-service.entered

	second := &observedWaitContext{Context: t.Context(), waiting: make(chan struct{})}
	secondResult := make(chan error, 1)

	go func() {
		_, err := cache.getOrCreate(second, service, service.blob.ID, imaging.DerivativeCard)
		secondResult <- err
	}()

	<-second.waiting
	cancel()

	if err := <-firstResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("first caller: %v", err)
	}

	if err := <-secondResult; err != nil {
		t.Fatalf("live caller inherited another caller's cancellation: %v", err)
	}
}
