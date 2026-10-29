// scalecheck creates a disposable, synthetic library and checks product budgets.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/administration"
	"github.com/savorum/savorum/internal/app"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/capacity"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/scalefixture"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
)

type result struct {
	Version           int                `json:"workload_version"`
	Scenario          string             `json:"scenario"`
	Recipes           int                `json:"recipes"`
	Platform          string             `json:"platform"`
	Go                string             `json:"go"`
	RunnerID          string             `json:"runner_id,omitempty"`
	RunnerDescription string             `json:"runner_description,omitempty"`
	Measurements      map[string]float64 `json:"milliseconds"`
	Capacity          capacity.Report    `json:"capacity"`
	BackupBytes       int64              `json:"backup_bytes"`
	Failures          []string           `json:"failures"`
}

type measureFunc func(string, int, func() error) error

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	count := flag.Int("recipes", 100, "100, 1000 or 10000 recipes")
	directory := flag.String("directory", "", "new, disposable fixture directory (required)")
	fromBackup := flag.String("from-backup", "", "reuse an exact scale fixture backup instead of regenerating it")
	browseOnly := flag.Bool("browse-only", false, "measure browse reads and card assets without write, reader or backup benchmarks")
	migrations := flag.String("migrations", "internal/storage/sqlite/migrations", "released migration SQL directory")

	flag.Parse()

	c := scalefixture.Policy()

	if !slices.Contains(c.Sizes, *count) || *directory == "" {
		return fmt.Errorf("specify --directory and a contract recipe count")
	}

	if err := os.Mkdir(*directory, 0o700); err != nil {
		return fmt.Errorf("create scale fixture directory: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	dbpath := filepath.Join(*directory, "library.db")

	assetroot := filepath.Join(*directory, "assets")
	if *fromBackup != "" {
		if err := administration.Restore(ctx, *fromBackup, dbpath, assetroot); err != nil {
			return err
		}
	}

	store, err := sqlite.Open(ctx, dbpath)
	if err != nil {
		return err
	}

	defer func() { _ = store.Close() }()

	principal, err := store.LocalPrincipal(ctx)
	if err != nil {
		return fmt.Errorf("resolve scale fixture user: %w", err)
	}

	ctx = access.WithLibrary(identity.WithPrincipal(ctx, principal), "default")

	fileStore, err := files.NewAssetStore(assetroot, 64<<20)
	if err != nil {
		return err
	}

	var blobs *assets.Service

	if *fromBackup == "" {
		fmt.Fprintf(os.Stderr, "Seeding %d recipes with permanent history and images\n", *count)

		if err := scalefixture.Seed(ctx, store, fileStore, *count); err != nil {
			return err
		}
	}

	if err := verifyFixture(ctx, store, *count, c); err != nil {
		return err
	}

	r := result{Version: c.Version, Recipes: *count, Platform: runtime.GOOS + "/" + runtime.GOARCH, Go: runtime.Version(), RunnerID: os.Getenv("CI_RUNNER_ID"), RunnerDescription: os.Getenv("CI_RUNNER_DESCRIPTION"), Measurements: map[string]float64{}, Failures: []string{}}

	r.Scenario = "full"
	if *browseOnly {
		r.Scenario = "browse"
	}

	measure := newMeasure(&r, c.Budgets)

	if err := store.Close(); err != nil {
		return err
	}

	var application *app.Application

	if err := measure("startup", 1, func() error {
		var e error

		store, e = sqlite.Open(ctx, dbpath)
		if e != nil {
			return e
		}

		blobs, e = assets.NewService(store, fileStore)
		if e != nil {
			return e
		}

		application, e = app.New(store, units.DefaultCatalog(), app.Options{Assets: blobs})

		return e
	}); err != nil {
		return err
	}

	defer application.Renderer.Close()
	defer application.ImportJobs.Close()

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		return fmt.Errorf("create scale fixture library service: %w", err)
	}

	entry, err := benchmarkReads(ctx, application, service, *count, c.Samples, *browseOnly, measure)
	if err != nil {
		return err
	}

	r.Capacity, err = capacity.Inspect(ctx, store, dbpath, assetroot, *directory)
	if err != nil {
		return err
	}

	checkStorageBudgets(&r, c.StorageBudgets, *count)

	if *browseOnly {
		return writeReport(*directory, r)
	}

	backup := filepath.Join(*directory, "backup.zip")

	if err := measure("backup", 1, func() error { return administration.Backup(ctx, store, assetroot, backup, time.Now()) }); err != nil {
		return err
	}

	info, err := os.Stat(backup)
	if err != nil {
		return fmt.Errorf("inspect scale backup: %w", err)
	}

	r.BackupBytes = info.Size()
	if err := verifyRestore(ctx, backup, *directory, r.Capacity, measure); err != nil {
		return err
	}

	if err := migrationCheck(ctx, dbpath, filepath.Join(*directory, "upgrade.db"), *migrations, measure); err != nil {
		return err
	}

	if err := benchmarkWrites(ctx, store, service, entry, *count, c.Samples, measure); err != nil {
		return err
	}

	return writeReport(*directory, r)
}

func checkStorageBudgets(r *result, budgets map[string]int64, count int) {
	for name, size := range map[string]int64{"database": r.Capacity.DatabaseBytes, "originals": r.Capacity.OriginalFilesBytes} {
		if size > budgets[name]*int64(count) {
			r.Failures = append(r.Failures, fmt.Sprintf("%s %d bytes exceeds %d bytes/recipe", name, size, budgets[name]))
		}
	}
}

func writeReport(directory string, report result) error {
	output, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode scale report: %w", err)
	}

	if err := os.WriteFile(filepath.Join(directory, "report.json"), append(output, '\n'), 0o600); err != nil {
		return fmt.Errorf("write scale report: %w", err)
	}

	fmt.Println(string(output))

	if len(report.Failures) > 0 {
		return fmt.Errorf("performance budget failures: %v", report.Failures)
	}

	return nil
}

func newMeasure(report *result, budgets map[string]float64) measureFunc {
	return func(name string, samples int, fn func() error) error {
		values := make([]float64, samples)
		for i := range values {
			start := time.Now()

			if err := fn(); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}

			values[i] = float64(time.Since(start)) / float64(time.Millisecond)
		}

		sort.Float64s(values)
		p95 := values[(95*len(values)+99)/100-1]

		report.Measurements[name] = p95
		if p95 > budgets[name] {
			report.Failures = append(report.Failures, fmt.Sprintf("%s %.1f ms > %.1f ms", name, p95, budgets[name]))
		}

		fmt.Fprintf(os.Stderr, "%s: %.1f ms\n", name, p95)

		return nil
	}
}

func verifyFixture(ctx context.Context, store *sqlite.Store, count int, contract scalefixture.Contract) error {
	usage, err := store.StorageUsage(ctx)
	if err != nil {
		return err
	}

	expectedRevisions := 0
	for i := range count {
		expectedRevisions += scalefixture.Depth(i)
	}

	if usage.StoredAssets != int64(count/contract.History.ImageEvery*contract.History.ImageVersions) || usage.Recipes != int64(count) || usage.RecipeRevisions != int64(expectedRevisions) || usage.BookRevisions != int64(count/contract.History.BookMembers*contract.History.BookRevisions) {
		return fmt.Errorf("input does not match the shared fixture contract: %+v", usage)
	}

	tags, err := store.ListTags(ctx)
	if err != nil {
		return err
	}

	facets, err := store.ListFacets(ctx, "")
	if err != nil {
		return err
	}

	expected := fixtureLabelCounts(count)

	if len(tags) != contract.Taxonomy.Tags+3 || len(facets) != 4*contract.Taxonomy.FacetsPerKind {
		return fmt.Errorf("input taxonomy does not match workload version %d", contract.Version)
	}

	for _, tag := range tags {
		if tag.RecipeCount != expected[tag.ID] {
			return fmt.Errorf("fixture tag %s has %d recipes, expected %d", tag.ID, tag.RecipeCount, expected[tag.ID])
		}
	}

	for _, facet := range facets {
		if facet.RecipeCount != expected[facet.ID] {
			return fmt.Errorf("fixture facet %s has %d recipes, expected %d", facet.ID, facet.RecipeCount, expected[facet.ID])
		}
	}

	return nil
}

func fixtureLabelCounts(count int) map[string]uint64 {
	c := scalefixture.Policy()

	counts := map[string]uint64{"tscale-common": 0, "tscale-popular": 0, "tscale-singleton": 0}
	for i := range c.Taxonomy.Tags {
		counts[fmt.Sprintf("tscale%d", i)] = 0
	}

	for _, kind := range []string{"course", "cuisine", "dietary", "method"} {
		for i := range c.Taxonomy.FacetsPerKind {
			counts[fmt.Sprintf("fscale-%s-%d", kind, i)] = 0
		}
	}

	for i := range count {
		tags, facets := scalefixture.Labels(i)
		for _, id := range append(tags, facets...) {
			counts[id]++
		}
	}

	return counts
}

func requestScaleEndpoint(ctx context.Context, application *app.Application, count int, path string, labelCounts map[string]uint64) error {
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil))

	if response.Code != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", response.Code, response.Body.String())
	}

	if path == "/api/v1/tags" || path == "/api/v1/facets" {
		return verifyTaxonomyResponse(response.Body.Bytes(), path, count, labelCounts)
	}

	if !strings.HasPrefix(path, "/api/v1/recipes?") {
		return nil
	}

	var page struct {
		Items []struct {
			ID   string `json:"id"`
			Tags []struct {
				ID string `json:"id"`
			} `json:"tags"`
			Facets []struct {
				Kind string `json:"kind"`
			} `json:"facets"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		return fmt.Errorf("decode recipe list response: %w", err)
	}

	total := count
	if strings.Contains(path, "favorite%3Atrue") {
		total /= 10
	}

	endpoint, err := url.ParseRequestURI(path)
	if err != nil {
		return fmt.Errorf("parse scale endpoint: %w", err)
	}

	limit := 50
	if endpoint.Query().Get("limit") == "1" {
		limit = 1
	}

	offset := 0
	if raw := endpoint.Query().Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return fmt.Errorf("invalid scale offset %q", raw)
		}
	}

	expectedSize := min(limit, max(0, total-offset))
	if page.Total != total || len(page.Items) != expectedSize {
		return fmt.Errorf("%s returned %d of %d, expected %d of %d", path, len(page.Items), page.Total, expectedSize, total)
	}

	for i, item := range page.Items {
		if endpoint.Query().Get("sort") == "title-asc" && item.ID != string(scalefixture.EntryID(offset+i)) {
			return fmt.Errorf("%s returned recipe %s at position %d, expected %s", path, item.ID, i, scalefixture.EntryID(offset+i))
		}

		kinds := map[string]bool{}
		for _, facet := range item.Facets {
			kinds[facet.Kind] = true
		}

		if len(item.Tags) < 3 || len(item.Facets) != 4 || !kinds["course"] || !kinds["cuisine"] || !kinds["dietary"] || !kinds["method"] {
			return fmt.Errorf("%s omits assigned tags or facet kinds", path)
		}
	}

	return nil
}

func verifyTaxonomyResponse(body []byte, path string, count int, labelCounts map[string]uint64) error {
	var page struct {
		Items []struct {
			ID          string `json:"id"`
			RecipeCount int64  `json:"recipe_count"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return fmt.Errorf("decode taxonomy response: %w", err)
	}

	c := scalefixture.Policy()

	expectedSize, expectedAssignments := 4*c.Taxonomy.FacetsPerKind, 4*count
	if path == "/api/v1/tags" {
		expectedSize, expectedAssignments = c.Taxonomy.Tags+3, 3*count+(count+1)/2+1
	}

	var assignments int64

	seen := map[string]bool{}

	prefix := "fscale-"
	if path == "/api/v1/tags" {
		prefix = "tscale"
	}

	for _, item := range page.Items {
		expected, exists := labelCounts[item.ID]
		if !exists || seen[item.ID] || !strings.HasPrefix(item.ID, prefix) || item.RecipeCount < 0 || uint64(item.RecipeCount) != expected {
			return fmt.Errorf("%s returned invalid count or identity for label %s", path, item.ID)
		}

		seen[item.ID] = true
		assignments += item.RecipeCount
	}

	if len(page.Items) != expectedSize || assignments != int64(expectedAssignments) {
		return fmt.Errorf("%s returned %d labels and %d assignments, expected %d and %d", path, len(page.Items), assignments, expectedSize, expectedAssignments)
	}

	return nil
}

func benchmarkReads(ctx context.Context, application *app.Application, service *library.Service, count, samples int, browseOnly bool, measure measureFunc) (library.Entry, error) {
	labelCounts := fixtureLabelCounts(count)
	request := func(path string) error { return requestScaleEndpoint(ctx, application, count, path, labelCounts) }

	ops := []struct{ name, path string }{
		{"list", "/api/v1/recipes?limit=50"},
		{"search", "/api/v1/recipes?q=saffron&sort=relevance&limit=50"},
		{"filter", "/api/v1/recipes?q=favorite%3Atrue&limit=50"},
		{"pagination", fmt.Sprintf("/api/v1/recipes?sort=title-asc&limit=50&offset=%d", max(0, count-50))},
		{"tags", "/api/v1/tags"},
		{"facets", "/api/v1/facets"},
		{"library_total", "/api/v1/recipes?limit=1&offset=0"},
	}
	if !browseOnly {
		ops = append(ops, struct{ name, path string }{"open", "/api/v1/recipes/" + string(scalefixture.EntryID(0))})
	}

	for _, op := range ops {
		if err := measure(op.name, samples, func() error { return request(op.path) }); err != nil {
			return library.Entry{}, err
		}
	}

	entry, err := service.Get(ctx, scalefixture.EntryID(0))
	if err != nil {
		return library.Entry{}, err
	}

	imagePath := "/api/v1/recipes/" + string(entry.ID) + "/assets/sha256:" + entry.Recipe.Assets[0].SHA256

	images := []struct{ name, suffix string }{{"image_card", "/derivatives/card"}}
	if !browseOnly {
		images = append(images, struct{ name, suffix string }{"image_original", ""}, struct{ name, suffix string }{"image_reader", "/derivatives/reader"})
	}

	for _, op := range images {
		if err := measure(op.name, 1, func() error { return request(imagePath + op.suffix) }); err != nil {
			return library.Entry{}, err
		}
	}

	return entry, nil
}

func verifyRestore(ctx context.Context, backup, directory string, before capacity.Report, measure measureFunc) error {
	restoreDB := filepath.Join(directory, "restored.db")
	restoreAssets := filepath.Join(directory, "restored-assets")

	if err := measure("restore", 1, func() error { return administration.Restore(ctx, backup, restoreDB, restoreAssets) }); err != nil {
		return err
	}

	restored, err := sqlite.Open(ctx, restoreDB)
	if err != nil {
		return err
	}
	defer restored.Close()

	after, err := restored.StorageUsage(ctx)
	if err != nil {
		return err
	}

	if after.RecipeRevisions != before.RecipeRevisions || after.BookRevisions != before.BookRevisions || after.RetainedOnlyAssetBytes != before.RetainedOnlyAssetBytes {
		return fmt.Errorf("restore lost retained history")
	}

	return restored.IntegrityCheck(ctx)
}

func benchmarkWrites(ctx context.Context, store *sqlite.Store, service *library.Service, entry library.Entry, count, samples int, measure measureFunc) error {
	if err := measure("save", samples, func() error {
		entry.Recipe.Description += " A tested adjustment."

		var err error

		entry, err = service.Update(ctx, entry.ID, entry.Revision, entry.Recipe)

		return err
	}); err != nil {
		return err
	}

	if err := measure("import_100", 1, func() error {
		for start := 0; start < 100; start += 25 {
			var docs []recipe.Document
			for i := start; i < start+25; i++ {
				docs = append(docs, scalefixture.Document(count+i))
			}

			if _, err := service.ImportRecipes(ctx, docs, library.DuplicateReject); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return err
	}

	return measure("search_rebuild", 1, func() error { return store.RebuildSearchIndex(ctx) })
}
