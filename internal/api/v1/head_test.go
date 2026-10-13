package v1

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/print"
	"github.com/savorum/savorum/internal/recipe"
)

func TestAssetHEADReturnsGETMetadataWithoutOpeningOrReadingBody(t *testing.T) {
	digest := strings.Repeat("a", 64)
	service := &headAssetService{blob: assets.Blob{
		ID: assets.ID("sha256:" + digest), ContentHash: digest,
		MediaType: "image/png", Size: 11,
	}}
	references := headAssetReferences{entry: library.Entry{ID: "r1", Recipe: recipeWithAsset(digest)}}
	server := newRealAPIServer(t, func(api huma.API) { registerAssets(api, references, service) })

	head := doServerRequest(t, server, http.MethodHead, "/api/v1/recipes/r1/assets/"+string(service.blob.ID), "")
	if head.StatusCode != http.StatusOK {
		t.Fatalf("HEAD asset status = %d", head.StatusCode)
	}

	headBody, err := io.ReadAll(head.Body)
	if err != nil {
		t.Fatal(err)
	}

	_ = head.Body.Close()

	if len(headBody) != 0 || service.openCalls != 0 || service.reads != 0 {
		t.Fatalf("HEAD body=%q open calls=%d reader reads=%d", headBody, service.openCalls, service.reads)
	}

	if service.metadataCalls != 1 || head.Header.Get("Content-Length") != "11" || head.Header.Get("Content-Type") != "image/png" || head.Header.Get("ETag") != `"`+string(service.blob.ID)+`"` {
		t.Fatalf("HEAD metadata calls=%d headers=%v", service.metadataCalls, head.Header)
	}

	get := doServerRequest(t, server, http.MethodGet, "/api/v1/recipes/r1/assets/"+string(service.blob.ID), "")

	getBody, err := io.ReadAll(get.Body)
	if err != nil {
		t.Fatal(err)
	}

	_ = get.Body.Close()
	if get.StatusCode != http.StatusOK || string(getBody) != "asset-bytes" || service.openCalls != 1 || service.reads == 0 {
		t.Fatalf("GET status=%d body=%q open calls=%d reader reads=%d", get.StatusCode, getBody, service.openCalls, service.reads)
	}

	for _, header := range []string{"Content-Type", "Content-Length", "ETag", "Cache-Control", "Content-Security-Policy", "Cross-Origin-Resource-Policy", "X-Content-Type-Options"} {
		if head.Header.Get(header) != get.Header.Get(header) {
			t.Fatalf("%s differs: HEAD=%q GET=%q", header, head.Header.Get(header), get.Header.Get(header))
		}
	}
}

func TestAssetHEADPreservesMissingResourceBehaviorWithoutOpeningBody(t *testing.T) {
	service := &headAssetService{}
	digest := strings.Repeat("f", 64)
	references := headAssetReferences{entry: library.Entry{ID: "r1", Recipe: recipeWithAsset(digest)}}
	server := newRealAPIServer(t, func(api huma.API) { registerAssets(api, references, service) })

	response := doServerRequest(t, server, http.MethodHead, "/api/v1/recipes/r1/assets/sha256:"+digest, "")
	defer response.Body.Close()

	if response.StatusCode != http.StatusNotFound || service.metadataCalls != 1 || service.openCalls != 0 || service.reads != 0 {
		t.Fatalf("missing HEAD status=%d metadata=%d open=%d reads=%d", response.StatusCode, service.metadataCalls, service.openCalls, service.reads)
	}
}

func TestPrintHEADReturnsHeldMetadataWithoutWritingPDF(t *testing.T) {
	id := strings.Repeat("b", 64)
	printer := &headPrintService{artifact: print.Artifact{ID: id, PDF: []byte("%PDF-held-body"), Title: "Dinner"}}
	server := newRealAPIServer(t, func(api huma.API) { registerPrint(api, printer) })

	head := doServerRequest(t, server, http.MethodHead, "/api/v1/print/documents/"+id+"?download=true", "")

	headBody, err := io.ReadAll(head.Body)
	if err != nil {
		t.Fatal(err)
	}

	_ = head.Body.Close()
	if head.StatusCode != http.StatusOK || len(headBody) != 0 || head.Header.Get("Content-Length") != "14" || head.Header.Get("Content-Disposition") != `attachment; filename="dinner.pdf"` {
		t.Fatalf("HEAD print status=%d body=%q headers=%v", head.StatusCode, headBody, head.Header)
	}

	get := doServerRequest(t, server, http.MethodGet, "/api/v1/print/documents/"+id+"?download=true", "")

	getBody, err := io.ReadAll(get.Body)
	if err != nil {
		t.Fatal(err)
	}

	_ = get.Body.Close()
	if get.StatusCode != http.StatusOK || string(getBody) != "%PDF-held-body" {
		t.Fatalf("GET print status=%d body=%q", get.StatusCode, getBody)
	}
}

func TestExportHEADRejectsEveryRouteBeforeResolutionPreparationOrWrite(t *testing.T) {
	libraryReader := &headExportLibrary{}
	exporter := &headExportService{library: libraryReader}
	server := newRealAPIServer(t, func(api huma.API) { registerExport(api, exporter) })

	tests := []struct {
		path  string
		allow string
	}{
		{"/api/v1/recipes/r1/export/sref", http.MethodGet},
		{"/api/v1/recipes/r1/revisions/1/export/sref", http.MethodGet},
		{"/api/v1/exports/sref-bundle", http.MethodPost},
		{"/api/v1/books/b1/export/sref-bundle", http.MethodGet},
	}
	for _, testCase := range tests {
		response := doServerRequest(t, server, http.MethodHead, testCase.path, "")
		_, _ = io.Copy(io.Discard, response.Body)

		_ = response.Body.Close()
		if response.StatusCode != http.StatusMethodNotAllowed || response.Header.Get("Allow") != testCase.allow {
			t.Fatalf("HEAD %s status=%d Allow=%q", testCase.path, response.StatusCode, response.Header.Get("Allow"))
		}
	}

	if exporter.prepareCalls != 0 || exporter.writeCalls != 0 || libraryReader.calls != 0 {
		t.Fatalf("HEAD reached export work: prepare=%d write=%d resolve=%d", exporter.prepareCalls, exporter.writeCalls, libraryReader.calls)
	}

	get := doServerRequest(t, server, http.MethodGet, "/api/v1/recipes/r1/export/sref", "")

	getBody, err := io.ReadAll(get.Body)
	if err != nil {
		t.Fatal(err)
	}

	_ = get.Body.Close()
	post := doServerRequest(t, server, http.MethodPost, "/api/v1/exports/sref-bundle", `{"recipes":["r1"]}`)

	postBody, err := io.ReadAll(post.Body)
	if err != nil {
		t.Fatal(err)
	}

	_ = post.Body.Close()
	if get.StatusCode != http.StatusOK || post.StatusCode != http.StatusOK || string(getBody) != "export-bytes" || string(postBody) != "export-bytes" || exporter.prepareCalls != 2 || exporter.writeCalls != 2 {
		t.Fatalf("ordinary export changed: GET=%d/%q POST=%d/%q prepare=%d write=%d", get.StatusCode, getBody, post.StatusCode, postBody, exporter.prepareCalls, exporter.writeCalls)
	}
}

func TestPrintHEADPreservesMissingHeldDocumentBehavior(t *testing.T) {
	printer := &headPrintService{}
	server := newRealAPIServer(t, func(api huma.API) { registerPrint(api, printer) })

	response := doServerRequest(t, server, http.MethodHead, "/api/v1/print/documents/"+strings.Repeat("c", 64), "")
	defer response.Body.Close()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("missing print HEAD status=%d", response.StatusCode)
	}
}

func newRealAPIServer(t *testing.T, register func(huma.API)) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	config := huma.DefaultConfig("head-test", "test")
	config.OpenAPIPath, config.DocsPath, config.SchemasPath = "", "", ""
	api := humago.New(mux, config)
	register(api)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server
}

func doServerRequest(t *testing.T, server *httptest.Server, method, path, body string) *http.Response {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}

	return response
}

type headAssetService struct {
	blob                     assets.Blob
	metadataCalls, openCalls int
	reads                    int
}

type headAssetReferences struct{ entry library.Entry }

func (r headAssetReferences) Get(_ context.Context, id library.EntryID) (library.Entry, error) {
	if r.entry.ID != id {
		return library.Entry{}, library.ErrNotFound
	}

	return r.entry, nil
}

func recipeWithAsset(digest string) recipe.Document {
	return recipe.Document{Assets: []recipe.Asset{{SHA256: digest}}}
}

func (*headAssetService) Ingest(context.Context, io.Reader, assets.IngestOptions) (assets.Blob, error) {
	return assets.Blob{}, errors.New("not used")
}

func (s *headAssetService) Metadata(_ context.Context, id assets.ID) (assets.Blob, error) {
	s.metadataCalls++
	if s.blob.ID != id {
		return assets.Blob{}, assets.ErrNotFound
	}

	return s.blob, nil
}

func (s *headAssetService) Open(_ context.Context, id assets.ID) (assets.Blob, io.ReadCloser, error) {
	s.openCalls++
	if s.blob.ID != id {
		return assets.Blob{}, nil, assets.ErrNotFound
	}

	return s.blob, &countingReadCloser{Reader: bytes.NewBufferString("asset-bytes"), reads: &s.reads}, nil
}

type countingReadCloser struct {
	io.Reader
	reads *int
}

func (r *countingReadCloser) Read(buffer []byte) (int, error) {
	(*r.reads)++

	return r.Reader.Read(buffer) //nolint:wrapcheck // transparent test reader must preserve EOF and the underlying read error
}
func (*countingReadCloser) Close() error { return nil }

type headPrintService struct {
	artifact print.Artifact
}

func (*headPrintService) RecipeLimit() int { return print.DefaultCollectionRecipes }

func (*headPrintService) Render(context.Context, print.Request) (print.Artifact, error) {
	return print.Artifact{}, errors.New("not used")
}

func (s *headPrintService) HeldFor(_ context.Context, id string) (print.Artifact, bool, error) {
	return s.artifact, s.artifact.ID == id, nil
}
func (*headPrintService) Library() print.RecipeLibrary { return nil }
func (*headPrintService) Books() print.BookLibrary     { return nil }

type headExportService struct {
	library                  export.Library
	prepareCalls, writeCalls int
}

func (s *headExportService) Prepare(context.Context, export.Source, export.Format) (export.Plan, error) {
	s.prepareCalls++
	return export.Plan{Filename: "recipes.sref", MediaType: "application/zip"}, nil
}

func (s *headExportService) Write(_ context.Context, _ export.Plan, destination io.Writer) error {
	s.writeCalls++

	_, err := io.WriteString(destination, "export-bytes")
	if err != nil {
		return fmt.Errorf("write test export: %w", err)
	}

	return nil
}
func (s *headExportService) Library() export.Library { return s.library }

type headExportLibrary struct{ calls int }

func (l *headExportLibrary) Get(context.Context, library.EntryID) (library.Entry, error) {
	l.calls++
	return library.Entry{}, library.ErrNotFound
}

func (l *headExportLibrary) GetTrashed(context.Context, library.EntryID) (library.Entry, error) {
	l.calls++
	return library.Entry{}, library.ErrNotFound
}

func (l *headExportLibrary) GetRevision(context.Context, library.EntryID, uint64) (library.Revision, error) {
	l.calls++
	return library.Revision{}, library.ErrNotFound
}

func (l *headExportLibrary) GetBook(context.Context, string) (library.Book, error) {
	l.calls++
	return library.Book{}, library.ErrNotFound
}
