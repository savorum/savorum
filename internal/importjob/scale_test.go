//go:build !race

// These load tests opt out of race instrumentation, which makes them minutes
// slower; service_test.go exercises job concurrency under the detector.

package importjob_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/importjob"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
)

// TestSeveralThousandRecipeImport runs a migration of several thousand
// recipes to completion, with every recipe accounted for.
func TestSeveralThousandRecipeImport(t *testing.T) {
	if testing.Short() {
		t.Skip("the several-thousand-recipe import is a load test")
	}

	const recipes = 3000

	store := newStore(t)
	counting := &countingImporter{}

	service, err := importjob.NewService(store, counting, importjob.WithLimits(importjob.Limits{BatchSize: 25}))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(service.Close)

	// The selection is assembled directly here rather than through a preview:
	// this test is about the job carrying several thousand items, not about
	// the size of one review session.
	planned := make([]exchange.PlannedImport, recipes)
	for index := range planned {
		planned[index] = exchange.PlannedImport{
			ItemID: fmt.Sprintf("item-%d", index+1),
			Title:  fmt.Sprintf("Bulk recipe %d", index+1),
			Source: exchange.ImportPreviewSource{FormatName: "CookbookManager", Filename: "export.zip"},
		}
	}
	// One job is bounded at MaximumImportRecipes, so this is a sequence of jobs.
	total := 0

	for offset := 0; offset < recipes; offset += library.MaximumImportRecipes {
		end := min(offset+library.MaximumImportRecipes, recipes)

		started, err := service.Start(context.Background(), importjob.StartRequest{
			Importer: "CookbookManager", Source: "export.zip", Items: planned[offset:end],
		})
		if err != nil {
			t.Fatal(err)
		}

		job := waitFor(t, service, started.ID, finished)
		if job.State != importjob.StateCompleted {
			t.Fatalf("job %s state = %q (%s)", job.ID, job.State, job.Error)
		}

		if job.Progress.Imported != end-offset {
			t.Fatalf("job %s progress = %#v", job.ID, job.Progress)
		}

		total += job.Progress.Imported
	}

	if total != recipes || counting.recipes() != recipes {
		t.Fatalf("imported %d recipes, committed %d, want %d", total, counting.recipes(), recipes)
	}
	// No batch ever exceeded the bound, so the memory an import holds at once
	// is a property of the batch rather than of the migration.
	if largest := counting.largestBatch(); largest > 25 {
		t.Fatalf("largest batch = %d, want no more than 25", largest)
	}

	jobs, listed, err := service.List(context.Background(), importjob.ListOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}

	if listed != 6 || len(jobs) != 6 {
		t.Fatalf("listed %d jobs (%d returned), want 6", listed, len(jobs))
	}
}

// TestSeveralThousandRecipeImportReachesTheLibrary commits the same size for
// real and checks the recipes arrive.
func TestSeveralThousandRecipeImportReachesTheLibrary(t *testing.T) {
	if testing.Short() {
		t.Skip("the several-thousand-recipe import is a load test")
	}

	const recipes = 2000

	suite := newHarness(t)

	service, err := importjob.NewService(suite.store, suite.exchanger, importjob.WithLimits(importjob.Limits{BatchSize: 25}))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(service.Close)

	number := 0

	for imported := 0; imported < recipes; {
		size := min(library.MaximumImportRecipes, recipes-imported)

		uploads := make([]exchange.UploadedImport, size)
		for index := range uploads {
			number++
			uploads[index] = exchange.UploadedImport{
				Filename: fmt.Sprintf("recipe-%05d.json", number), Data: bareRecipe(t, number),
			}
		}

		started, err := service.Start(context.Background(), importjob.StartRequest{
			Importer: "SREF recipe", Source: "migration", Items: suite.planUploads(t, uploads),
		})
		if err != nil {
			t.Fatal(err)
		}

		job := waitFor(t, service, started.ID, finished)
		if job.State != importjob.StateCompleted || job.Progress.Imported != size {
			t.Fatalf("job %s state=%q progress=%#v (%s)", job.ID, job.State, job.Progress, job.Error)
		}

		imported += size
	}

	if entries := suite.libraryCount(t); entries != recipes {
		t.Fatalf("library holds %d recipes, want %d", entries, recipes)
	}
}

// countingImporter stands in for the library when the test is about the job's
// arithmetic rather than about persistence.
type countingImporter struct {
	mu      sync.Mutex
	total   int
	largest int
	batches int
}

func (counter *countingImporter) ImportPlanned(_, _ context.Context, planned []exchange.PlannedImport, _ library.DuplicatePolicy) ([]library.ImportResult, error) {
	counter.mu.Lock()
	counter.total += len(planned)

	counter.batches++
	if len(planned) > counter.largest {
		counter.largest = len(planned)
	}
	counter.mu.Unlock()

	results := make([]library.ImportResult, len(planned))
	for index, entry := range planned {
		results[index] = library.ImportResult{
			RecipeID: recipe.RecipeID(entry.ItemID), EntryID: library.EntryID("entry-" + entry.ItemID),
			Revision: 1, Outcome: library.ImportCreated, Title: entry.Title,
		}
	}

	return results, nil
}

func (counter *countingImporter) recipes() int {
	counter.mu.Lock()
	defer counter.mu.Unlock()

	return counter.total
}

func (counter *countingImporter) largestBatch() int {
	counter.mu.Lock()
	defer counter.mu.Unlock()

	return counter.largest
}
