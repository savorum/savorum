package sqlite

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/importjob"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestImportProgressIdentifiesDuplicateFailuresWithoutDoubleCounting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	seedImportJob(ctx, t, store, "duplicates", []importjob.Item{pending("duplicate", []byte(`{"retained":true}`)), pending("invalid", []byte(`{"retained":true}`)), pending("skipped", nil)})

	if err := store.SaveImportJobItems(ctx, "duplicates", []importjob.Item{
		{Position: 0, State: importjob.ItemFailed, Error: "recipe" + library.DuplicateErrorPhrase + "existing"},
		{Position: 1, State: importjob.ItemFailed, Error: "invalid recipe"},
		{Position: 2, State: importjob.ItemSkipped},
	}); err != nil {
		t.Fatal(err)
	}

	job, err := store.GetImportJob(ctx, "duplicates")
	if err != nil {
		t.Fatal(err)
	}

	if job.Progress.AlreadyInLibrary != 1 || job.Progress.Failed != 2 || job.Progress.Skipped != 1 || job.Progress.Total != 3 || job.Progress.Resumable != 1 {
		t.Fatalf("progress = %#v", job.Progress)
	}
}

func TestTimeSortUsesDurationAndPutsUnknownTimesLast(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	now := time.Now().UTC()

	for id, duration := range map[string]string{"hours": "PT2H", "minutes": "PT30M", "day": "P1D", "unknown": ""} {
		doc := recipe.Document{SchemaVersion: recipe.CurrentDocumentSchemaVersion, ID: recipe.RecipeID(id), Title: id}
		if duration != "" {
			doc.Times = &recipe.Times{Total: &recipe.DurationExpression{Value: duration}}
		}

		if err := store.Create(ctx, library.Entry{ID: library.EntryID(id), Recipe: doc, Revision: 1, CreatedAt: now, UpdatedAt: now}, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := store.List(ctx, library.ListOptions{State: library.StateActive, Sort: library.ListSortTotalTimeAsc, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	got := []string{}
	for _, entry := range entries {
		got = append(got, string(entry.ID))
	}

	if !reflect.DeepEqual(got, []string{"minutes", "hours", "day", "unknown"}) {
		t.Fatalf("duration order = %v", got)
	}

	for id, day := range map[string]string{"hours": "2026-10-23", "minutes": "2026-10-01"} {
		if _, err := store.RecordCooking(ctx, id, library.DefaultUserID, library.CookingRecord{ID: id + "-cooked", CookedOn: library.Date(day), CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}

	cooked, err := store.List(ctx, library.ListOptions{State: library.StateActive, Sort: library.ListSortCookedDesc, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(cooked) != 4 || cooked[0].ID != "hours" || cooked[1].ID != "minutes" {
		t.Fatalf("recently cooked = %#v", cooked)
	}
}

func TestBookPreviewUsesAnActiveRecipeWithAStoredPhoto(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	now := time.Now().UTC()
	doc := testfixture.Document()
	photo := doc.Assets[0]

	assetID := "sha256:" + photo.SHA256
	if _, err := store.RegisterStoredAsset(ctx, assets.Blob{ID: assets.ID(assetID), ContentHash: photo.SHA256, MediaType: photo.MediaType, Size: photo.Size, StorageKey: "photo", CreatedAt: now}, now); err != nil {
		t.Fatal(err)
	}

	if err := store.Create(ctx, library.Entry{ID: "preview", Recipe: doc, Revision: 1, CreatedAt: now, UpdatedAt: now}, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	if err := store.CreateBook(ctx, library.Book{ID: "preview-book", Kind: library.BookPersonal, Title: "Preview", Revision: 1, CreatedAt: now, UpdatedAt: now, Entries: []library.BookEntry{{ID: "member", EntryID: "preview", Position: 0}}}); err != nil {
		t.Fatal(err)
	}

	if err := store.CreateBook(ctx, library.Book{ID: "empty-book", Kind: library.BookPersonal, Title: "Z no recipes", Revision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	books, err := store.ListBooks(ctx, library.BookListOptions{State: library.StateActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(books) != 2 || books[0].PreviewAssetID != assetID || books[0].PreviewEntryID != "preview" || books[1].PreviewAssetID != "" || books[1].PreviewEntryID != "" {
		t.Fatalf("preview = %#v", books)
	}

	book, err := store.GetBook(ctx, "preview-book", library.StateActive)
	if err != nil || book.PreviewAssetID != assetID || book.PreviewEntryID != "preview" {
		t.Fatalf("book detail preview = %#v, %v", book, err)
	}

	if _, err := store.Revise(ctx, library.RevisionCommand{ID: "preview", ExpectedRevision: 1, Recipe: doc, CurrentState: library.StateActive, TargetState: library.StateTrash, Metadata: library.RevisionMetadata{Cause: library.RevisionCauseAPI}, Timestamp: now.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}

	books, err = store.ListBooks(ctx, library.BookListOptions{State: library.StateActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	book, err = store.GetBook(ctx, "preview-book", library.StateActive)
	if err != nil || book.PreviewAssetID != "" || book.PreviewEntryID != "" {
		t.Fatalf("trashed book detail preview = %#v, %v", book, err)
	}

	if len(books) != 2 || books[0].PreviewAssetID != "" || books[0].PreviewEntryID != "" || books[1].PreviewAssetID != "" || books[1].PreviewEntryID != "" {
		t.Fatalf("trashed recipe preview = %#v", books)
	}
}

func TestPublicationPreviewUsesACitedRecipe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	now := time.Now().UTC()
	doc := testfixture.Document()
	photo := doc.Assets[0]

	assetID := "sha256:" + photo.SHA256
	if _, err := store.RegisterStoredAsset(ctx, assets.Blob{ID: assets.ID(assetID), ContentHash: photo.SHA256, MediaType: photo.MediaType, Size: photo.Size, StorageKey: "photo", CreatedAt: now}, now); err != nil {
		t.Fatal(err)
	}

	if err := store.Create(ctx, library.Entry{ID: "cited", Recipe: doc, Revision: 1, CreatedAt: now, UpdatedAt: now}, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	if err := store.CreateBook(ctx, library.Book{ID: "publication", Kind: library.BookPublication, Title: "Cookbook", Revision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.ReplaceOrganization(ctx, "cited", library.DefaultUserID, 1, library.Organization{Sources: []library.RecipeSource{{ID: "citation", Kind: library.SourcePublication, PublicationBookID: "publication"}}}, nil, now); err != nil {
		t.Fatal(err)
	}

	books, err := store.ListBooks(ctx, library.BookListOptions{State: library.StateActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(books) != 1 || books[0].PreviewAssetID != assetID || books[0].PreviewEntryID != "cited" {
		t.Fatalf("publication preview = %#v", books)
	}

	book, err := store.GetBook(ctx, "publication", library.StateActive)
	if err != nil || book.PreviewAssetID != assetID || book.PreviewEntryID != "cited" {
		t.Fatalf("publication detail preview = %#v, %v", book, err)
	}
}
