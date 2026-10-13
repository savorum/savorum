// scalecheck creates a disposable, synthetic library and checks product budgets.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
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

	entry, err := benchmarkReads(ctx, application, service, *count, c.Samples, measure)
	if err != nil {
		return err
	}

	r.Capacity, err = capacity.Inspect(ctx, store, dbpath, assetroot, *directory)
	if err != nil {
		return err
	}

	for name, size := range map[string]int64{"database": r.Capacity.DatabaseBytes, "originals": r.Capacity.OriginalFilesBytes} {
		if size > c.StorageBudgets[name]*int64(*count) {
			r.Failures = append(r.Failures, fmt.Sprintf("%s %d bytes exceeds %d bytes/recipe", name, size, c.StorageBudgets[name]))
		}
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

	return nil
}

func requestScaleEndpoint(ctx context.Context, application *app.Application, count int, path string) error {
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil))

	if response.Code != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", response.Code, response.Body.String())
	}

	if !strings.HasPrefix(path, "/api/v1/recipes?") {
		return nil
	}

	var page struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		return fmt.Errorf("decode recipe list response: %w", err)
	}

	total := count
	if strings.Contains(path, "favorite%3Atrue") {
		total /= 10
	}

	if page.Total != total || len(page.Items) != min(50, total) {
		return fmt.Errorf("%s returned %d of %d, expected %d of %d", path, len(page.Items), page.Total, min(50, total), total)
	}

	return nil
}

func benchmarkReads(ctx context.Context, application *app.Application, service *library.Service, count, samples int, measure measureFunc) (library.Entry, error) {
	request := func(path string) error { return requestScaleEndpoint(ctx, application, count, path) }

	for _, op := range []struct{ name, path string }{
		{"list", "/api/v1/recipes?limit=50"},
		{"search", "/api/v1/recipes?q=saffron&sort=relevance&limit=50"},
		{"open", "/api/v1/recipes/" + string(scalefixture.EntryID(0))},
		{"filter", "/api/v1/recipes?q=favorite%3Atrue&limit=50"},
		{"pagination", fmt.Sprintf("/api/v1/recipes?limit=50&offset=%d", count-50)},
	} {
		if err := measure(op.name, samples, func() error { return request(op.path) }); err != nil {
			return library.Entry{}, err
		}
	}

	entry, err := service.Get(ctx, scalefixture.EntryID(0))
	if err != nil {
		return library.Entry{}, err
	}

	imagePath := "/api/v1/recipes/" + string(entry.ID) + "/assets/sha256:" + entry.Recipe.Assets[0].SHA256

	for _, op := range []struct{ name, suffix string }{{"image_card", "/derivatives/card"}, {"image_original", ""}, {"image_reader", "/derivatives/reader"}} {
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
