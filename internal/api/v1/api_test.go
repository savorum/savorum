package v1

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/configuration"
	"github.com/savorum/savorum/internal/contracttest"
	"github.com/savorum/savorum/internal/idempotency"
	"github.com/savorum/savorum/internal/library"
	domainmeal "github.com/savorum/savorum/internal/meal"
	"github.com/savorum/savorum/internal/print"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/render/typst"
	"github.com/savorum/savorum/internal/routecatalog"
	"github.com/savorum/savorum/internal/shopping"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

var apiTestDatabase = testfixture.DatabaseTemplate(func(path string) error {
	store, err := sqlite.Open(context.Background(), path)
	if err != nil {
		return fmt.Errorf("open API fixture: %w", err)
	}

	if err := store.Close(); err != nil {
		return fmt.Errorf("close API fixture: %w", err)
	}

	return nil
})

func testAPIStore(t testing.TB) *sqlite.Store {
	t.Helper()

	path, err := apiTestDatabase(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	store, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	return store
}

func testAPI(t *testing.T) (*routecatalog.Mux, *sqlite.Store, huma.API) {
	t.Helper()

	store := testAPIStore(t)

	service, err := library.NewService(
		store,
		units.DefaultCatalog(),
		library.WithClock(func() time.Time { return time.Date(2026, time.August, 26, 0, 0, 0, 0, time.UTC) }),
	)
	if err != nil {
		t.Fatal(err)
	}

	blobStore, err := files.NewAssetStore(filepath.Join(t.TempDir(), "assets"), files.DefaultMaximumAssetSize)
	if err != nil {
		t.Fatal(err)
	}

	assetService, err := assets.NewService(store, blobStore, assets.WithClock(func() time.Time {
		return time.Date(2026, time.August, 26, 0, 0, 0, 0, time.UTC)
	}))
	if err != nil {
		t.Fatal(err)
	}

	configurationService, err := configuration.NewService(store, configuration.InstanceOverrides{})
	if err != nil {
		t.Fatal(err)
	}

	printer, err := print.NewService(sharedRenderer(), units.DefaultCatalog(), assetService, service, service)
	if err != nil {
		t.Fatal(err)
	}

	mealService, err := domainmeal.NewService(store, store)
	if err != nil {
		t.Fatal(err)
	}

	handler, definition, err := New(service, store, printer, Options{
		Idempotency: idempotency.New(store), Wrap: func(next http.Handler) http.Handler { return contracttest.Handler(t, next) }, Assets: assetService, Configuration: configurationService, Meals: mealService, Shopping: shopping.NewService(store).WithSources(service, mealService),
	})
	if err != nil {
		t.Fatal(err)
	}

	return handler, store, definition
}

// sharedRenderer prepares the document engine once for the whole test binary.

var sharedRenderer = sync.OnceValue(typst.New)

func request(t *testing.T, handler http.Handler, method, target string, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequestWithContext(t.Context(), method, target, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	return response
}

func requestWithIfMatch(t *testing.T, handler http.Handler, method, target string, body []byte, ifMatch string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequestWithContext(t.Context(), method, target, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	request.Header.Set("If-Match", ifMatch)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	return response
}

// newTestAuthenticatedSharedCache checks its shared cache before authentication,
// as vulnerable proxy configurations do, and stores origin responses only when
// their cache policy allows it. A successful authenticated fetch must not bypass
// a later logout at this layer.
func newTestAuthenticatedSharedCache(origin http.Handler) http.Handler {
	type cachedResponse struct {
		status int
		header http.Header
		body   []byte
	}

	cache := map[string]cachedResponse{}

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if cached, ok := cache[request.URL.RequestURI()]; ok {
			for name, values := range cached.header {
				response.Header()[name] = append([]string(nil), values...)
			}

			response.WriteHeader(cached.status)
			_, _ = response.Write(cached.body)

			return
		}

		if request.Header.Get("Authorization") == "" {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}

		originResponse := httptest.NewRecorder()
		origin.ServeHTTP(originResponse, request)

		cacheControl := originResponse.Header().Get("Cache-Control")
		if !strings.Contains(cacheControl, "private") && !strings.Contains(cacheControl, "no-store") {
			cache[request.URL.RequestURI()] = cachedResponse{
				status: originResponse.Code, header: originResponse.Header().Clone(),
				body: append([]byte(nil), originResponse.Body.Bytes()...),
			}
		}

		for name, values := range originResponse.Header() {
			response.Header()[name] = append([]string(nil), values...)
		}

		response.WriteHeader(originResponse.Code)
		_, _ = response.Write(originResponse.Body.Bytes())
	})
}

func createTag(t *testing.T, handler http.Handler, name, color string) Tag {
	t.Helper()

	body, err := json.Marshal(TagInput{Name: name, Color: color})
	if err != nil {
		t.Fatal(err)
	}

	response := request(t, handler, http.MethodPost, "/api/v1/tags", body)
	if response.Code != http.StatusCreated || response.Header().Get("ETag") != `"1"` {
		t.Fatalf("create tag status=%d ETag=%q body=%s", response.Code, response.Header().Get("ETag"), response.Body.String())
	}

	var tag Tag
	if err := json.Unmarshal(response.Body.Bytes(), &tag); err != nil {
		t.Fatal(err)
	}

	return tag
}

func createFacet(t *testing.T, handler http.Handler, kind, value string) Facet {
	t.Helper()

	body, err := json.Marshal(FacetInput{Kind: kind, Value: value})
	if err != nil {
		t.Fatal(err)
	}

	response := request(t, handler, http.MethodPost, "/api/v1/facets", body)
	if response.Code != http.StatusCreated || response.Header().Get("ETag") != `"1"` {
		t.Fatalf("create facet status=%d ETag=%q body=%s", response.Code, response.Header().Get("ETag"), response.Body.String())
	}

	var facet Facet
	if err := json.Unmarshal(response.Body.Bytes(), &facet); err != nil {
		t.Fatal(err)
	}

	return facet
}

func readOrganization(t *testing.T, handler http.Handler, path, expectedETag string) RecipeOrganization {
	t.Helper()

	response := request(t, handler, http.MethodGet, path, nil)
	if response.Code != http.StatusOK || response.Header().Get("ETag") != expectedETag {
		t.Fatalf("get organization status=%d ETag=%q body=%s", response.Code, response.Header().Get("ETag"), response.Body.String())
	}

	var organization RecipeOrganization
	if err := json.Unmarshal(response.Body.Bytes(), &organization); err != nil {
		t.Fatal(err)
	}

	return organization
}

func recordCooking(t *testing.T, handler http.Handler, entryID string, body map[string]any) CookingRecord {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	response := request(t, handler, http.MethodPost, "/api/v1/recipes/"+entryID+"/cookings", encoded)
	if response.Code != http.StatusCreated {
		t.Fatalf("record cooking status=%d body=%s", response.Code, response.Body.String())
	}

	var record CookingRecord
	if err := json.Unmarshal(response.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}

	return record
}

func readCookings(t *testing.T, handler http.Handler, entryID string) CookingHistory {
	t.Helper()

	response := request(t, handler, http.MethodGet, "/api/v1/recipes/"+entryID+"/cookings", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list cookings status=%d body=%s", response.Code, response.Body.String())
	}

	var history CookingHistory
	if err := json.Unmarshal(response.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}

	return history
}

func createRecipe(t *testing.T, handler http.Handler, document recipe.Document) LibraryEntry {
	t.Helper()

	encoded, err := json.Marshal(fromDomain(document))
	if err != nil {
		t.Fatal(err)
	}

	response := request(t, handler, http.MethodPost, "/api/v1/recipes", encoded)
	if response.Code != http.StatusCreated {
		t.Fatalf("create recipe status=%d body=%s", response.Code, response.Body.String())
	}

	var created LibraryEntry
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	return created
}

func createBook(t *testing.T, handler http.Handler, body map[string]any) Book {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	response := request(t, handler, http.MethodPost, "/api/v1/books", encoded)
	if response.Code != http.StatusCreated {
		t.Fatalf("create book status=%d body=%s", response.Code, response.Body.String())
	}

	var book Book
	if err := json.Unmarshal(response.Body.Bytes(), &book); err != nil {
		t.Fatal(err)
	}

	return book
}

func readBook(t *testing.T, handler http.Handler, id string) Book {
	t.Helper()

	response := request(t, handler, http.MethodGet, "/api/v1/books/"+id, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("get book status=%d body=%s", response.Code, response.Body.String())
	}

	var book Book
	if err := json.Unmarshal(response.Body.Bytes(), &book); err != nil {
		t.Fatal(err)
	}

	return book
}

// organizationJSON fills in the required organization fields a caller did not
// name so each test states only what it is about.

func organizationJSON(t *testing.T, body map[string]any) []byte {
	t.Helper()

	complete := map[string]any{
		"facet_ids":  []string{},
		"tag_ids":    []string{},
		"sources":    []any{},
		"user_state": map[string]any{"favorite": false},
	}
	maps.Copy(complete, body)

	encoded, err := json.Marshal(complete)
	if err != nil {
		t.Fatal(err)
	}

	return encoded
}

func listSummaries(t *testing.T, handler http.Handler, query string) []RecipeSummary {
	t.Helper()

	response := request(t, handler, http.MethodGet, "/api/v1/recipes?limit=50&q="+url.QueryEscape(query), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list recipes for %q status=%d body=%s", query, response.Code, response.Body.String())
	}

	var listed struct {
		Items []RecipeSummary `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}

	return listed.Items
}

func recipeJSONOf(t *testing.T, packaged []byte) string {
	t.Helper()

	archive, err := zip.NewReader(bytes.NewReader(packaged), int64(len(packaged)))
	if err != nil {
		t.Fatal(err)
	}

	file, err := archive.Open("recipe.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}

	return string(content)
}

func importRecipes(t *testing.T, handler http.Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	return request(t, handler, http.MethodPost, "/api/v1/recipes/import", encoded)
}

func importedRecipes(t *testing.T, handler http.Handler, body map[string]any) []ImportedRecipe {
	t.Helper()

	response := importRecipes(t, handler, body)
	if response.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", response.Code, response.Body.String())
	}

	var decoded struct {
		Items []ImportedRecipe `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}

	return decoded.Items
}

// srefDocument renders a recipe the way it travels, so the import tests
// exercise the format Savorum publishes rather than its internal shape.

func srefDocument(t *testing.T, document recipe.Document) map[string]any {
	t.Helper()

	encoded, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	var members map[string]any
	if err := json.Unmarshal(encoded, &members); err != nil {
		t.Fatal(err)
	}

	return members
}
