package exchange_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
)

// pendingLibrary keeps what it imports so deferred photographs can be fetched.
type pendingLibrary struct {
	importLibrary

	mu                 sync.Mutex
	entries            map[library.EntryID]library.Entry
	revised            int
	editedAt           uint64
	completionError    error
	completionAttempts int
}

func (f *pendingLibrary) ImportRecipes(ctx context.Context, documents []recipe.Document, policy library.DuplicatePolicy) ([]library.ImportResult, error) {
	results, err := f.importLibrary.ImportRecipes(ctx, documents, policy)

	f.mu.Lock()
	defer f.mu.Unlock()

	for index, result := range results {
		f.entries[result.EntryID] = library.Entry{ID: result.EntryID, Recipe: documents[index], Revision: 1}
	}

	return results, err
}

func (f *pendingLibrary) Get(_ context.Context, id library.EntryID) (library.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	entry, ok := f.entries[id]
	if !ok {
		return library.Entry{}, library.ErrNotFound
	}

	return entry, nil
}

func (f *pendingLibrary) PendingPhotographs(_ context.Context, after library.EntryID, limit int) ([]library.PendingPhotographs, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var pending []library.PendingPhotographs

	for id, entry := range f.entries {
		if _, ok := entry.Recipe.Extensions["x-org.savorum.pending-images"]; ok && id > after && len(pending) < limit {
			pending = append(pending, library.PendingPhotographs{LibraryID: "library", EntryID: id})
		}
	}

	return pending, nil
}

func (f *pendingLibrary) CompletePendingPhotographs(_ context.Context, id library.EntryID, expected uint64, document recipe.Document) (library.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.completionAttempts++
	if f.completionAttempts == 1 && f.completionError != nil {
		return library.Entry{}, fmt.Errorf("complete pending photographs: %w", f.completionError)
	}

	entry := f.entries[id]
	if f.editedAt != 0 {
		entry.Revision = f.editedAt
	}

	if entry.Revision != expected {
		return library.Entry{}, &library.PreconditionError{EntryID: id, Expected: expected, Current: entry.Revision}
	}

	entry.Recipe, entry.Revision = document, entry.Revision+1
	f.entries[id] = entry
	f.revised++

	return entry, nil
}

func (f *pendingLibrary) only(t *testing.T) library.Entry {
	t.Helper()

	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.entries) != 1 {
		t.Fatalf("library holds %d recipes", len(f.entries))
	}

	for _, entry := range f.entries {
		return entry
	}

	return library.Entry{}
}

// hangsUntilOpened hangs until opened.
type hangsUntilOpened struct {
	mu   sync.Mutex
	open bool
}

func (f *hangsUntilOpened) Fetch(ctx context.Context, _ string) (exchange.FetchedAsset, error) {
	f.mu.Lock()
	open := f.open
	f.mu.Unlock()

	if !open {
		<-ctx.Done()
		return exchange.FetchedAsset{}, ctx.Err()
	}

	return exchange.FetchedAsset{Body: io.NopCloser(strings.NewReader("jpeg")), MediaType: "image/jpeg", Size: 4}, nil
}

func (f *hangsUntilOpened) opened() {
	f.mu.Lock()
	f.open = true
	f.mu.Unlock()
}

func waitForPhotographs(t *testing.T, service *exchange.Service) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for service.FetchingPhotographs() {
		if time.Now().After(deadline) {
			t.Fatal("deferred photographs were still being fetched")
		}

		time.Sleep(5 * time.Millisecond)
	}
}

func deferringService(t *testing.T, fetcher exchange.RemoteAssetFetcher) (*exchange.Service, *pendingLibrary) {
	t.Helper()

	recipes := &pendingLibrary{entries: map[library.EntryID]library.Entry{}}
	limits := exchange.DefaultRemoteImportLimits()
	limits.Duration = 20 * time.Millisecond
	service := exchange.NewService(recipes, &countingAssets{},
		exchange.WithRemoteAssetFetcher(fetcher), exchange.WithRemoteImportLimits(limits))
	t.Cleanup(service.Close)

	return service, recipes
}

func TestStartupFetchesPhotographsAnImportDeferred(t *testing.T) {
	fetcher := &hangsUntilOpened{}
	service, recipes := deferringService(t, fetcher)
	commitWithin(t, service, imagePreview(t, service, 1), 3*time.Second)
	waitForPhotographs(t, service)

	if _, ok := recipes.only(t).Recipe.Extensions["x-org.savorum.pending-images"]; !ok {
		t.Fatal("the import did not defer its photograph")
	}

	fetcher.opened()

	if err := service.FetchPendingPhotographs(context.Background()); err != nil {
		t.Fatal(err)
	}

	waitForPhotographs(t, service)

	entry := recipes.only(t)
	if _, ok := entry.Recipe.Extensions["x-org.savorum.pending-images"]; ok {
		t.Fatalf("photograph still deferred: %#v", entry.Recipe.Extensions)
	}

	if len(entry.Recipe.Assets) != 1 || len(entry.Recipe.ImageRefs) != 1 || entry.Recipe.ImageRefs[0] != entry.Recipe.Assets[0].ID {
		t.Fatalf("assets = %#v, image refs = %#v", entry.Recipe.Assets, entry.Recipe.ImageRefs)
	}
}

func TestAnEditWinsOverFetchedPhotographs(t *testing.T) {
	fetcher := &hangsUntilOpened{}
	service, recipes := deferringService(t, fetcher)
	commitWithin(t, service, imagePreview(t, service, 1), 3*time.Second)
	waitForPhotographs(t, service)

	recipes.editedAt = 2

	fetcher.opened()

	if err := service.FetchPendingPhotographs(context.Background()); err != nil {
		t.Fatal(err)
	}

	waitForPhotographs(t, service)

	if recipes.revised != 0 {
		t.Fatalf("revised an edited recipe %d times", recipes.revised)
	}
}

func TestUnreachablePhotographsDoNotKeepThePassRunning(t *testing.T) {
	fetcher := &hangsUntilOpened{}
	service, recipes := deferringService(t, fetcher)
	commitWithin(t, service, imagePreview(t, service, 1), 3*time.Second)

	if err := service.FetchPendingPhotographs(context.Background()); err != nil {
		t.Fatal(err)
	}

	waitForPhotographs(t, service)

	if recipes.revised != 0 {
		t.Fatalf("revised %d times without fetching anything", recipes.revised)
	}
}

// hangsOnce hangs on the first fetch only.
type hangsOnce struct {
	mu      sync.Mutex
	fetches int
}

func (f *hangsOnce) Fetch(ctx context.Context, _ string) (exchange.FetchedAsset, error) {
	f.mu.Lock()
	f.fetches++
	first := f.fetches == 1
	f.mu.Unlock()

	if first {
		<-ctx.Done()
		return exchange.FetchedAsset{}, ctx.Err()
	}

	return exchange.FetchedAsset{Body: io.NopCloser(strings.NewReader("jpeg")), MediaType: "image/jpeg", Size: 4}, nil
}

func TestAnImportFollowsUpOnItsDeferredPhotographs(t *testing.T) {
	service, recipes := deferringService(t, &hangsOnce{})
	result := commitWithin(t, service, imagePreview(t, service, 1), 3*time.Second)

	if len(result.Items) != 1 || result.Items[0].DeferredAssets != 1 {
		t.Fatalf("items = %#v", result.Items)
	}

	waitForPhotographs(t, service)

	entry := recipes.only(t)
	if _, ok := entry.Recipe.Extensions["x-org.savorum.pending-images"]; ok || len(entry.Recipe.Assets) != 1 {
		t.Fatalf("assets = %#v, extensions = %#v", entry.Recipe.Assets, entry.Recipe.Extensions)
	}
}

func TestDeferredPhotographCompletionFailuresDoNotDiscardOtherRecipes(t *testing.T) {
	for _, test := range []struct {
		name         string
		failure      error
		wantRevised  int
		wantAttempts int
	}{
		{name: "deleted recipe", failure: library.ErrNotFound, wantRevised: 1, wantAttempts: 2},
		{name: "trashed recipe", failure: library.ErrInvalidState, wantRevised: 1, wantAttempts: 2},
		{name: "storage failure", failure: errors.New("storage unavailable"), wantRevised: 0, wantAttempts: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fetcher := &hangsUntilOpened{}
			service, recipes := deferringService(t, fetcher)
			commitWithin(t, service, imagePreview(t, service, 2), 3*time.Second)
			waitForPhotographs(t, service)

			recipes.completionError = test.failure

			fetcher.opened()

			if err := service.FetchPendingPhotographs(context.Background()); err != nil {
				t.Fatal(err)
			}

			waitForPhotographs(t, service)

			if recipes.revised != test.wantRevised || recipes.completionAttempts != test.wantAttempts {
				t.Fatalf("revised %d recipes in %d attempts, want %d in %d", recipes.revised, recipes.completionAttempts, test.wantRevised, test.wantAttempts)
			}
		})
	}
}
