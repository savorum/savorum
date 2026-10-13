package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/recipecodec"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestCollectionReadsDoNotDecodeRecipeDocuments(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	timestamp := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	document := testfixture.Document()

	entry := library.Entry{ID: "rsummary", Recipe: document, Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}
	if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	encoded, _, err := recipecodec.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	advanceToUnsupportedSchemaRevision(ctx, t, store, entry.ID, string(encoded), 99, timestamp.Add(time.Minute))

	summaries, err := store.List(ctx, library.ListOptions{Limit: 10, State: library.StateActive, Sort: library.ListSortUpdatedDesc})
	if err != nil {
		t.Fatalf("summary list decoded an unsupported document: %v", err)
	}

	if len(summaries) != 1 || summaries[0].Title != document.Title || summaries[0].Description != document.Description || summaries[0].TotalTime != document.Times.Total.Value {
		t.Fatalf("unexpected projected summary: %#v", summaries)
	}

	if _, err := store.Get(ctx, entry.ID, library.StateActive); !errors.Is(err, recipecodec.ErrUnsupportedSchemaVersion) {
		t.Fatalf("full detail did not decode document discriminator: %v", err)
	}

	revisions, err := store.ListRevisions(ctx, entry.ID, library.RevisionListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("revision list decoded an unsupported snapshot: %v", err)
	}

	if len(revisions) != 2 || revisions[0].Number != 2 {
		t.Fatalf("unexpected revision summaries: %#v", revisions)
	}

	if _, err := store.GetRevision(ctx, entry.ID, 2); !errors.Is(err, recipecodec.ErrUnsupportedSchemaVersion) {
		t.Fatalf("revision detail did not decode document discriminator: %v", err)
	}
}

func TestPurgeManyIsAtomicAcrossExactRevisions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	timestamp := time.Date(2026, time.August, 26, 17, 0, 0, 0, time.UTC)

	for _, id := range []string{"rtrashone", "rtrashtwo"} {
		document := testfixture.Document()
		document.ID = recipe.RecipeID("document-" + id)

		entry := library.Entry{ID: library.EntryID(id), Recipe: document, Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}
		if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}

		if _, err := store.Revise(ctx, library.RevisionCommand{
			ID: library.EntryID(id), ExpectedRevision: 1, Recipe: document,
			CurrentState: library.StateActive, TargetState: library.StateTrash,
			Metadata:  library.RevisionMetadata{Cause: library.RevisionCauseTrash},
			Timestamp: timestamp.Add(time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}

	err = store.PurgeMany(ctx, []library.PurgeTarget{
		{ID: "rtrashone", Revision: 2},
		{ID: "rtrashtwo", Revision: 1},
	}, nil, timestamp.Add(2*time.Minute))
	if !errors.Is(err, library.ErrPreconditionFailed) {
		t.Fatalf("expected stale batch to fail its precondition, got %v", err)
	}

	for _, id := range []string{"rtrashone", "rtrashtwo"} {
		if _, err := store.Get(ctx, library.EntryID(id), library.StateTrash); err != nil {
			t.Fatalf("atomic failure removed %s: %v", id, err)
		}
	}

	if err := store.PurgeMany(ctx, []library.PurgeTarget{
		{ID: "rtrashone", Revision: 2},
		{ID: "rtrashtwo", Revision: 2},
	}, nil, timestamp.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"rtrashone", "rtrashtwo"} {
		if _, err := store.Get(ctx, library.EntryID(id), library.StateTrash); !errors.Is(err, library.ErrNotFound) {
			t.Fatalf("purged recipe %s remains: %v", id, err)
		}
	}
}

func TestPurgeManyRollsBackMixedRecipeAndBookBatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	timestamp := time.Date(2026, time.August, 26, 18, 30, 0, 0, time.UTC)

	document := testfixture.Document()
	document.ID = "document-rmixedbatch"

	entry := library.Entry{ID: "rmixedbatch", Recipe: document, Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}
	if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Revise(ctx, library.RevisionCommand{
		ID: entry.ID, ExpectedRevision: 1, Recipe: document,
		CurrentState: library.StateActive, TargetState: library.StateTrash,
		Metadata:  library.RevisionMetadata{Cause: library.RevisionCauseTrash},
		Timestamp: timestamp.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"bmixedone", "bmixedtwo"} {
		book := library.Book{
			ID: id, Kind: library.BookPersonal, Title: "Mixed batch " + id,
			Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp,
		}
		if err := store.CreateBook(ctx, book); err != nil {
			t.Fatal(err)
		}

		if _, err := store.ReviseBookState(ctx, id, 1, library.StateActive, library.StateTrash, library.RevisionMetadata{Cause: library.RevisionCauseTrash}, timestamp.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	err = store.PurgeMany(ctx, []library.PurgeTarget{
		{ID: "rmixedbatch", Revision: 2},
	}, []library.BookPurgeTarget{
		{ID: "bmixedone", Revision: 2},
		{ID: "bmixedtwo", Revision: 1},
	}, timestamp.Add(2*time.Minute))
	if !errors.Is(err, library.ErrPreconditionFailed) {
		t.Fatalf("expected stale mixed batch to fail its precondition, got %v", err)
	}

	if _, err := store.Get(ctx, "rmixedbatch", library.StateTrash); err != nil {
		t.Fatalf("mixed batch failure removed the recipe: %v", err)
	}

	for _, id := range []string{"bmixedone", "bmixedtwo"} {
		if _, err := store.GetBook(ctx, id, library.StateTrash); err != nil {
			t.Fatalf("mixed batch failure removed %s: %v", id, err)
		}
	}

	if err := store.PurgeMany(ctx, []library.PurgeTarget{
		{ID: "rmixedbatch", Revision: 2},
	}, []library.BookPurgeTarget{
		{ID: "bmixedone", Revision: 2},
		{ID: "bmixedtwo", Revision: 2},
	}, timestamp.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Get(ctx, "rmixedbatch", library.StateTrash); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("purged recipe remains: %v", err)
	}

	for _, id := range []string{"bmixedone", "bmixedtwo"} {
		if _, err := store.GetBook(ctx, id, library.StateTrash); !errors.Is(err, library.ErrNotFound) {
			t.Fatalf("purged book %s remains: %v", id, err)
		}
	}
}

func TestPurgeManyAdvancesEachAffectedBookOnceBeforeMembershipCascade(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	createdAt := time.Date(2026, time.August, 26, 17, 0, 0, 0, time.UTC)

	entryIDs := []library.EntryID{"rbookpurgeone", "rbookpurgetwo"}
	for index, id := range entryIDs {
		document := testfixture.Document()
		document.ID = recipe.RecipeID("document-" + id)

		entry := library.Entry{ID: id, Recipe: document, Revision: 1, CreatedAt: createdAt, UpdatedAt: createdAt}
		if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}

		entryIDs[index] = entry.ID
	}

	book := library.Book{
		ID: "bbatchpurge", Kind: library.BookPersonal, Title: "Batch purge",
		Revision: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
		Entries: []library.BookEntry{
			{ID: "ebookpurgeone", EntryID: entryIDs[0], Position: 0},
			{ID: "ebookpurgetwo", EntryID: entryIDs[1], Position: 1},
		},
	}
	if err := store.CreateBook(ctx, book); err != nil {
		t.Fatal(err)
	}

	for index, id := range entryIDs {
		entry, err := store.Get(ctx, id, library.StateActive)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := store.Revise(ctx, library.RevisionCommand{
			ID: id, ExpectedRevision: 1, Recipe: entry.Recipe,
			CurrentState: library.StateActive, TargetState: library.StateTrash,
			Metadata:  library.RevisionMetadata{Cause: library.RevisionCauseTrash},
			Timestamp: createdAt.Add(time.Duration(index+1) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}

	beforePurge, err := store.GetBook(ctx, book.ID, library.StateActive)
	if err != nil {
		t.Fatal(err)
	}

	if beforePurge.Revision != 3 || len(beforePurge.Entries) != 2 || beforePurge.Entries[0].State != library.StateTrash || beforePurge.Entries[1].State != library.StateTrash {
		t.Fatalf("book before batch purge = %#v", beforePurge)
	}

	if err := store.PurgeMany(ctx, []library.PurgeTarget{
		{ID: entryIDs[0], Revision: 2},
		{ID: entryIDs[1], Revision: 2},
	}, nil, createdAt.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}

	afterPurge, err := store.GetBook(ctx, book.ID, library.StateActive)
	if err != nil {
		t.Fatal(err)
	}

	if afterPurge.Revision != 4 || len(afterPurge.Entries) != 0 || !afterPurge.UpdatedAt.Equal(createdAt.Add(3*time.Minute)) {
		t.Fatalf("book after batch purge = %#v", afterPurge)
	}
}

func TestRecipeLifecycleLeavesATrashedBookInTheTrash(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	createdAt := time.Date(2026, time.August, 26, 19, 0, 0, 0, time.UTC)

	entry := library.Entry{ID: "rtrashedbook", Recipe: testfixture.Document(), Revision: 1, CreatedAt: createdAt, UpdatedAt: createdAt}
	if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	book := library.Book{
		ID: "btrashedbook", Kind: library.BookPersonal, Title: "Trashed book", Revision: 1,
		CreatedAt: createdAt, UpdatedAt: createdAt,
		Entries: []library.BookEntry{{ID: "etrashedbook", EntryID: entry.ID, Position: 0}},
	}
	if err := store.CreateBook(ctx, book); err != nil {
		t.Fatal(err)
	}

	if _, err := store.ReviseBookState(ctx, book.ID, 1, library.StateActive, library.StateTrash, library.RevisionMetadata{Cause: library.RevisionCauseTrash}, createdAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Revise(ctx, library.RevisionCommand{
		ID: entry.ID, ExpectedRevision: 1, Recipe: entry.Recipe,
		CurrentState: library.StateActive, TargetState: library.StateTrash,
		Metadata:  library.RevisionMetadata{Cause: library.RevisionCauseTrash},
		Timestamp: createdAt.Add(2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.GetBook(ctx, book.ID, library.StateActive); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("recipe lifecycle restored the trashed book: %v", err)
	}

	if trashed, err := store.GetBook(ctx, book.ID, library.StateTrash); err != nil || trashed.Revision != 3 {
		t.Fatalf("trashed book = %#v, %v", trashed, err)
	}
}

func TestBookHistoryTrashRestoreAndPurge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	createdAt := time.Date(2026, time.August, 26, 18, 0, 0, 0, time.UTC)

	book := library.Book{
		ID: "bhistory", Kind: library.BookPersonal, Title: "Original book", Revision: 1,
		CreatedAt: createdAt, UpdatedAt: createdAt,
		Sections: []library.BookSection{{ID: "kfirst", Title: "First", Position: 0}},
	}
	if err := store.CreateBook(ctx, book); err != nil {
		t.Fatal(err)
	}

	book.Title = "Changed book"
	book.Sections[0].Title = "Changed section"
	book.UpdatedAt = createdAt.Add(time.Minute)

	changed, err := store.ReplaceBook(ctx, 1, book)
	if err != nil {
		t.Fatal(err)
	}

	trashed, err := store.ReviseBookState(ctx, book.ID, changed.Revision, library.StateActive, library.StateTrash, library.RevisionMetadata{Cause: library.RevisionCauseTrash}, createdAt.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.GetBook(ctx, book.ID, library.StateActive); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("trashed book remained active: %v", err)
	}

	retained, err := store.GetBookRevision(ctx, book.ID, 1)
	if err != nil {
		t.Fatal(err)
	}

	if retained.Book.Title != "Original book" || len(retained.Book.Sections) != 1 || retained.Book.Sections[0].Title != "First" {
		t.Fatalf("first book revision changed: %#v", retained)
	}

	sourceRevision := uint64(1)

	restored, err := store.RestoreBookRevision(ctx, book.ID, 1, trashed.Revision, library.RevisionMetadata{Cause: library.RevisionCauseRestore, SourceRevision: &sourceRevision}, createdAt.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	if restored.Revision != 4 || restored.Title != "Original book" || restored.DeletedAt != nil {
		t.Fatalf("restored book = %#v", restored)
	}

	revisions, err := store.ListBookRevisions(ctx, book.ID, library.RevisionListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(revisions) != 4 || revisions[0].Revision != 4 || revisions[0].SourceRevision == nil || *revisions[0].SourceRevision != 1 {
		t.Fatalf("book revision list = %#v", revisions)
	}

	trashed, err = store.ReviseBookState(ctx, book.ID, restored.Revision, library.StateActive, library.StateTrash, library.RevisionMetadata{Cause: library.RevisionCauseTrash}, createdAt.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	if err := store.PurgeBook(ctx, book.ID, trashed.Revision, createdAt.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}

	if _, err := store.GetBookRevision(ctx, book.ID, 1); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("purged book history remains: %v", err)
	}
}

func TestRecipeTrashAndRestorePreserveLibraryRelationships(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	createdAt := time.Date(2026, time.August, 26, 19, 0, 0, 0, time.UTC)
	document := testfixture.Document()
	document.ID = "relationship-document"

	entry := library.Entry{
		ID: "rrelationships", Recipe: document, Revision: 1,
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	if err := store.CreateTag(ctx, library.Tag{ID: "tkeeper", Name: "Keeper", Revision: 1, CreatedAt: createdAt, UpdatedAt: createdAt}); err != nil {
		t.Fatal(err)
	}

	if err := store.CreateFacet(ctx, library.Facet{ID: "fkeeper", Kind: library.FacetCourse, Value: "Main", Revision: 1, CreatedAt: createdAt, UpdatedAt: createdAt}); err != nil {
		t.Fatal(err)
	}

	for _, book := range []library.Book{
		{ID: "bcurated", Kind: library.BookPersonal, Title: "Curated", Revision: 1, CreatedAt: createdAt, UpdatedAt: createdAt},
		{ID: "bsource", Kind: library.BookPublication, Title: "Source", Revision: 1, CreatedAt: createdAt, UpdatedAt: createdAt},
	} {
		if err := store.CreateBook(ctx, book); err != nil {
			t.Fatal(err)
		}
	}

	rating := uint8(5)

	organization := library.Organization{
		TagIDs:   []string{"tkeeper"},
		FacetIDs: []string{"fkeeper"},
		State: library.UserState{
			Favorite: true, Rating: &rating, PersonalNote: "Keep every relationship",
		},
		Sources: []library.RecipeSource{{
			ID: "skeeper", Kind: library.SourcePublication,
			PublicationBookID: "bsource", OriginalTitle: "Printed title",
		}},
	}
	if _, err := store.ReplaceOrganization(ctx, string(entry.ID), library.DefaultUserID, 1, organization, []library.BookMembershipChange{{
		BookID: "bcurated", Revision: 1, Member: true, EntryID: "ekeeper",
	}}, createdAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	if _, err := store.RecordCooking(ctx, string(entry.ID), library.DefaultUserID, library.CookingRecord{
		ID: "ckeeper", CookedOn: library.Date("2026-08-26"), Note: "Still here", CreatedAt: createdAt.Add(2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	trashed, err := store.Revise(ctx, library.RevisionCommand{
		ID: entry.ID, ExpectedRevision: 1, Recipe: document,
		CurrentState: library.StateActive, TargetState: library.StateTrash,
		Metadata:  library.RevisionMetadata{Cause: library.RevisionCauseTrash},
		Timestamp: createdAt.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	active, err := store.List(ctx, library.ListOptions{Limit: 10, State: library.StateActive, Sort: library.ListSortUpdatedDesc})
	if err != nil || len(active) != 0 {
		t.Fatalf("ordinary list after trash = %#v, err = %v", active, err)
	}

	trash, err := store.List(ctx, library.ListOptions{Limit: 10, State: library.StateTrash, Sort: library.ListSortUpdatedDesc})
	if err != nil || len(trash) != 1 || trash[0].ID != entry.ID {
		t.Fatalf("trash list = %#v, err = %v", trash, err)
	}

	restored, err := store.Revise(ctx, library.RevisionCommand{
		ID: entry.ID, ExpectedRevision: trashed.Revision, Recipe: document,
		CurrentState: library.StateTrash, TargetState: library.StateActive,
		Metadata:  library.RevisionMetadata{Cause: library.RevisionCauseRestore},
		Timestamp: createdAt.Add(4 * time.Minute),
	})
	if err != nil || restored.DeletedAt != nil {
		t.Fatalf("restore = %#v, err = %v", restored, err)
	}

	organization, err = store.GetOrganization(ctx, string(entry.ID), library.DefaultUserID)
	if err != nil {
		t.Fatal(err)
	}

	if len(organization.TagIDs) != 1 || organization.TagIDs[0] != "tkeeper" || len(organization.FacetIDs) != 1 || organization.FacetIDs[0] != "fkeeper" || !organization.State.Favorite || organization.State.Rating == nil || *organization.State.Rating != rating || organization.State.PersonalNote != "Keep every relationship" || len(organization.Sources) != 1 || organization.Sources[0].PublicationBookID != "bsource" {
		t.Fatalf("organization after restore = %#v", organization)
	}

	history, err := store.ListCookings(ctx, string(entry.ID), library.DefaultUserID, library.CookingListOptions{Limit: 10})
	if err != nil || history.TimesCooked != 1 || len(history.Records) != 1 || history.Records[0].ID != "ckeeper" {
		t.Fatalf("cooking history after restore = %#v, err = %v", history, err)
	}

	book, err := store.GetBook(ctx, "bcurated", library.StateActive)
	if err != nil || len(book.Entries) != 1 || book.Entries[0].EntryID != entry.ID || book.Entries[0].State != library.StateActive {
		t.Fatalf("book membership after restore = %#v, err = %v", book, err)
	}

	summaries, err := store.List(ctx, library.ListOptions{Limit: 10, State: library.StateActive, Sort: library.ListSortUpdatedDesc})
	if err != nil || len(summaries) != 1 {
		t.Fatalf("ordinary list after restore = %#v, err = %v", summaries, err)
	}

	summary := summaries[0]
	if len(summary.Tags) != 1 || summary.Tags[0].ID != "tkeeper" || len(summary.Books) != 1 || summary.Books[0].ID != "bcurated" || len(summary.SourceBooks) != 1 || summary.SourceBooks[0].ID != "bsource" || !summary.Favorite || summary.TimesCooked != 1 {
		t.Fatalf("restored summary relationships = %#v", summary)
	}
}

func TestRetainedRevisionRowsRejectMutationAndDirectDeletion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	timestamp := time.Date(2026, time.August, 26, 17, 0, 0, 0, time.UTC)
	document := testfixture.Document()

	entry := library.Entry{ID: "rimmutable", Recipe: document, Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}
	if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, `
		UPDATE recipe_revisions
		SET cause = 'trash'
		WHERE entry_id = ? AND revision = 1`, entry.ID); err == nil {
		t.Fatal("retained recipe revision update unexpectedly succeeded")
	}

	if _, err := store.db.ExecContext(ctx, `
		DELETE FROM recipe_revisions
		WHERE entry_id = ? AND revision = 1`, entry.ID); err == nil {
		t.Fatal("retained recipe revision deletion unexpectedly succeeded")
	}

	if _, err := store.db.ExecContext(ctx, `
		UPDATE recipe_revision_assets
		SET path = 'assets/replaced.jpg'
		WHERE entry_id = ? AND revision = 1`, entry.ID); err == nil {
		t.Fatal("retained revision asset update unexpectedly succeeded")
	}
}

func TestCurrentRecipeCannotContradictRetainedRevisions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	timestamp := time.Date(2026, time.August, 26, 17, 15, 0, 0, time.UTC)
	document := testfixture.Document()

	entry := library.Entry{ID: "requivalence", Recipe: document, Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}
	if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	encoded, _, err := recipecodec.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	for name, statement := range map[string]string{
		"document rewrite": `UPDATE library_recipes
			SET document_json = json_set(document_json, '$.title', 'Rewritten behind history')
			WHERE id = 'requivalence'`,
		"schema discriminator": `UPDATE library_recipes
			SET document_schema_version = 5 WHERE id = 'requivalence'`,
		"trash state": `UPDATE library_recipes
			SET deleted_at = '2026-08-26T17:20:00Z' WHERE id = 'requivalence'`,
		"revision jump": `UPDATE library_recipes
			SET revision = revision + 3 WHERE id = 'requivalence'`,
		"revision rewind": `UPDATE library_recipes
			SET revision = revision - 1 WHERE id = 'requivalence'`,
	} {
		if _, err := store.db.ExecContext(ctx, statement); err == nil {
			t.Fatalf("direct %s unexpectedly succeeded", name)
		}
	}

	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO recipe_revisions (
			entry_id, revision, document_json, document_schema_version, state, cause, created_at
		) VALUES (?, 2, ?, 1, 'active', 'api', ?)`,
		entry.ID, string(encoded), formatTime(timestamp.Add(time.Minute))); err == nil {
		t.Fatal("a revision beyond the current recipe unexpectedly succeeded")
	}

	// A transaction that advances the current recipe without retaining the
	// snapshot it created fails when the deferred constraint is checked.
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := transaction.ExecContext(ctx, `
		UPDATE library_recipes
		SET document_json = json_set(document_json, '$.title', 'Unrecorded revision'),
		    revision = revision + 1,
		    updated_at = ?
		WHERE id = ?`, formatTime(timestamp.Add(time.Minute)), entry.ID); err != nil {
		t.Fatal(err)
	}

	if err := transaction.Commit(); err == nil {
		t.Fatal("an unrecorded recipe revision unexpectedly committed")
	}

	read, err := store.Get(ctx, entry.ID, library.StateActive)
	if err != nil {
		t.Fatal(err)
	}

	if read.Revision != 1 || read.Recipe.Title != document.Title {
		t.Fatalf("rejected mutations changed the current recipe: %#v", read)
	}

	revisions, err := store.ListRevisions(ctx, entry.ID, library.RevisionListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(revisions) != 1 || revisions[0].Number != 1 {
		t.Fatalf("rejected mutations changed retained history: %#v", revisions)
	}

	// Ordinary revisions still advance the current recipe and its history
	// together, including the lifecycle change into trash.
	document.Title = "Recorded revision"

	revised, err := store.Revise(ctx, library.RevisionCommand{
		ID: entry.ID, ExpectedRevision: 1, Recipe: document,
		CurrentState: library.StateActive, TargetState: library.StateActive,
		Metadata:  library.RevisionMetadata{Cause: library.RevisionCauseManualEdit},
		Timestamp: timestamp.Add(2 * time.Minute),
	})
	if err != nil || revised.Revision != 2 {
		t.Fatalf("ordinary revision = %#v, err = %v", revised, err)
	}

	trashed, err := store.Revise(ctx, library.RevisionCommand{
		ID: entry.ID, ExpectedRevision: 2, Recipe: document,
		CurrentState: library.StateActive, TargetState: library.StateTrash,
		Metadata:  library.RevisionMetadata{Cause: library.RevisionCauseTrash},
		Timestamp: timestamp.Add(3 * time.Minute),
	})
	if err != nil || trashed.Revision != 3 {
		t.Fatalf("trash revision = %#v, err = %v", trashed, err)
	}

	var tracked uint64
	if err := store.db.QueryRowContext(ctx,
		`SELECT revision FROM recipe_head_revisions WHERE entry_id = ?`, entry.ID).Scan(&tracked); err != nil {
		t.Fatal(err)
	}

	if tracked != 3 {
		t.Fatalf("tracked head revision = %d", tracked)
	}

	var state string
	if err := store.db.QueryRowContext(ctx,
		`SELECT state FROM recipe_revisions WHERE entry_id = ? AND revision = 3`, entry.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}

	if state != string(library.StateTrash) {
		t.Fatalf("retained lifecycle revision state = %q", state)
	}
}

func TestLibraryAndDocumentIdentitiesAreImmutableAndConsistent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	timestamp := time.Date(2026, time.August, 26, 17, 30, 0, 0, time.UTC)
	document := testfixture.Document()

	entry := library.Entry{ID: "ridentity", Recipe: document, Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}
	if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	for name, statement := range map[string]string{
		"entry ID":    `UPDATE library_recipes SET id = 'rreplacement' WHERE id = 'ridentity'`,
		"document ID": `UPDATE library_recipes SET document_id = 'independent-copy' WHERE id = 'ridentity'`,
		"head JSON ID": `UPDATE library_recipes
			SET document_json = json_set(document_json, '$.id', 'independent-copy')
			WHERE id = 'ridentity'`,
	} {
		if _, err := store.db.ExecContext(ctx, statement); err == nil {
			t.Fatalf("direct %s mutation unexpectedly succeeded", name)
		}
	}

	changed := document

	changed.ID = "independent-copy"
	if _, err := store.Revise(ctx, library.RevisionCommand{
		ID: entry.ID, ExpectedRevision: 1, Recipe: changed,
		CurrentState: library.StateActive, TargetState: library.StateActive,
		Timestamp: timestamp.Add(time.Minute),
	}); !errors.Is(err, library.ErrDocumentIdentityChange) {
		t.Fatalf("storage identity-changing revision error = %v", err)
	}

	changedJSON, _, err := recipecodec.Encode(changed)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO recipe_revisions (
			entry_id, revision, document_json, document_schema_version, state, cause, created_at
		) VALUES (?, 2, ?, 1, 'active', 'api', ?)`, entry.ID, string(changedJSON), formatTime(timestamp.Add(time.Minute))); err == nil {
		t.Fatal("mismatched historical revision identity unexpectedly succeeded")
	}

	read, err := store.Get(ctx, entry.ID, library.StateActive)
	if err != nil {
		t.Fatal(err)
	}

	if read.ID != entry.ID || read.Recipe.ID != document.ID || read.Revision != 1 {
		t.Fatalf("failed mutations changed retained identity: %#v", read)
	}
}

func TestAssetRemainsReachableThroughHistoryUntilPermanentPurge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	createdAt := time.Date(2026, time.August, 26, 18, 0, 0, 0, time.UTC)
	document := testfixture.Document()

	entry := library.Entry{
		ID: "rassetreachability", Recipe: document, Revision: 1,
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	withoutImage := document
	withoutImage.Assets = nil
	withoutImage.ImageRefs = nil

	withoutImage.PrimaryImage = ""
	if _, err := store.Revise(ctx, library.RevisionCommand{
		ID: entry.ID, ExpectedRevision: 1, Recipe: withoutImage,
		CurrentState: library.StateActive, TargetState: library.StateActive,
		Metadata:  library.RevisionMetadata{Cause: library.RevisionCauseAPI},
		Timestamp: createdAt.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	var (
		orphanedAt sql.NullString
		references int
	)
	if err := store.db.QueryRowContext(ctx, `
		SELECT assets.orphaned_at, COUNT(recipe_revision_assets.asset_id)
		FROM assets
		LEFT JOIN recipe_revision_assets ON recipe_revision_assets.asset_id = assets.id
		WHERE assets.content_hash = ?
		GROUP BY assets.id`, document.Assets[0].SHA256).Scan(&orphanedAt, &references); err != nil {
		t.Fatal(err)
	}

	if orphanedAt.Valid || references != 1 {
		t.Fatalf("historical asset orphaned = %v, references = %d", orphanedAt.Valid, references)
	}

	if _, err := store.Revise(ctx, library.RevisionCommand{
		ID: entry.ID, ExpectedRevision: 2, Recipe: withoutImage,
		CurrentState: library.StateActive, TargetState: library.StateTrash,
		Metadata:  library.RevisionMetadata{Cause: library.RevisionCauseTrash},
		Timestamp: createdAt.Add(2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.Purge(ctx, entry.ID, 3, createdAt.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}

	if err := store.db.QueryRowContext(ctx, `
		SELECT orphaned_at,
		       (SELECT COUNT(*) FROM recipe_revision_assets WHERE asset_id = assets.id)
		FROM assets
		WHERE content_hash = ?`, document.Assets[0].SHA256).Scan(&orphanedAt, &references); err != nil {
		t.Fatal(err)
	}

	if !orphanedAt.Valid || references != 0 {
		t.Fatalf("purged asset orphaned = %v, references = %d", orphanedAt.Valid, references)
	}
}

func TestSharedAssetRemainsReachableUntilEveryOwningRecipeIsPermanentlyDeleted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	createdAt := time.Date(2026, time.August, 26, 20, 0, 0, 0, time.UTC)
	document := testfixture.Document()

	ids := []library.EntryID{"rsharedone", "rsharedtwo"}
	for index, id := range ids {
		shared := document

		shared.ID = recipe.RecipeID("shared-document-" + id)
		if err := store.Create(ctx, library.Entry{ID: id, Recipe: shared, Revision: 1, CreatedAt: createdAt, UpdatedAt: createdAt}, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}

		trashed, err := store.Revise(ctx, library.RevisionCommand{
			ID: id, ExpectedRevision: 1, Recipe: shared,
			CurrentState: library.StateActive, TargetState: library.StateTrash,
			Metadata:  library.RevisionMetadata{Cause: library.RevisionCauseTrash},
			Timestamp: createdAt.Add(time.Duration(index+1) * time.Minute),
		})
		if err != nil || trashed.Revision != 2 {
			t.Fatalf("trash %s = %#v, err = %v", id, trashed, err)
		}
	}

	if err := store.Purge(ctx, ids[0], 2, createdAt.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}

	var (
		orphanedAt sql.NullString
		references int
	)
	if err := store.db.QueryRowContext(ctx, `
		SELECT assets.orphaned_at, COUNT(recipe_revision_assets.asset_id)
		FROM assets
		LEFT JOIN recipe_revision_assets ON recipe_revision_assets.asset_id = assets.id
		WHERE assets.content_hash = ?
		GROUP BY assets.id`, document.Assets[0].SHA256).Scan(&orphanedAt, &references); err != nil {
		t.Fatal(err)
	}

	if orphanedAt.Valid || references != 2 {
		t.Fatalf("shared asset after first permanent deletion: orphaned = %v, references = %d", orphanedAt.Valid, references)
	}

	if err := store.Purge(ctx, ids[1], 2, createdAt.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}

	if err := store.db.QueryRowContext(ctx, `
		SELECT assets.orphaned_at, COUNT(recipe_revision_assets.asset_id)
		FROM assets
		LEFT JOIN recipe_revision_assets ON recipe_revision_assets.asset_id = assets.id
		WHERE assets.content_hash = ?
		GROUP BY assets.id`, document.Assets[0].SHA256).Scan(&orphanedAt, &references); err != nil {
		t.Fatal(err)
	}

	if !orphanedAt.Valid || references != 0 {
		t.Fatalf("shared asset after final permanent deletion: orphaned = %v, references = %d", orphanedAt.Valid, references)
	}
}

// advanceToUnsupportedSchemaRevision writes a current recipe whose document
// schema discriminator this build cannot decode, together with the retained
// revision that records it. Both rows change in one transaction because the
// database refuses a current recipe that no retained revision describes.
func advanceToUnsupportedSchemaRevision(
	ctx context.Context,
	t *testing.T,
	store *Store,
	entryID library.EntryID,
	documentJSON string,
	schemaVersion int,
	timestamp time.Time,
) uint64 {
	t.Helper()

	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()

	var current uint64
	if err := transaction.QueryRowContext(ctx,
		`SELECT revision FROM library_recipes WHERE id = ?`, entryID).Scan(&current); err != nil {
		t.Fatal(err)
	}

	revision := current + 1
	if _, err := transaction.ExecContext(ctx, `
		UPDATE library_recipes
		SET document_json = ?, document_schema_version = ?, revision = ?, updated_at = ?
		WHERE id = ?`,
		documentJSON, schemaVersion, revision, formatTime(timestamp), entryID); err != nil {
		t.Fatal(err)
	}

	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO recipe_revisions (
			entry_id, revision, document_json, document_schema_version,
			state, cause, source_revision, created_at
		) VALUES (?, ?, ?, ?, 'active', 'api', NULL, ?)`,
		entryID, revision, documentJSON, schemaVersion, formatTime(timestamp)); err != nil {
		t.Fatal(err)
	}

	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}

	return revision
}

// Retained history's cause vocabulary is a CHECK constraint, so every cause
// Savorum writes must be one the schema admits.
func TestRevisionCauseVocabularyIsWhatTheSchemaAdmits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	timestamp := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)
	document := testfixture.Document()
	entry := library.Entry{ID: "rcauses", Recipe: document, Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}

	if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
		t.Fatal(err)
	}

	revision := uint64(1)

	for _, cause := range []library.RevisionCause{
		library.RevisionCauseAPI,
		library.RevisionCauseManualEdit,
		library.RevisionCauseImport,
	} {
		document.Title = "Revised by " + string(cause)

		revised, reviseErr := store.Revise(ctx, library.RevisionCommand{
			ID: entry.ID, ExpectedRevision: revision, Recipe: document,
			CurrentState: library.StateActive, TargetState: library.StateActive,
			Metadata:  library.RevisionMetadata{Cause: cause},
			Timestamp: timestamp.Add(time.Duration(revision) * time.Minute),
		})
		if reviseErr != nil {
			t.Fatalf("cause %q is declared for recipe revisions but was refused: %v", cause, reviseErr)
		}

		revision = revised.Revision
	}

	document.Title = "Revised by an unadmitted cause"
	if _, err := store.Revise(ctx, library.RevisionCommand{
		ID: entry.ID, ExpectedRevision: revision, Recipe: document,
		CurrentState: library.StateActive, TargetState: library.StateActive,
		Metadata:  library.RevisionMetadata{Cause: library.RevisionCause("reanalysis")},
		Timestamp: timestamp.Add(time.Hour),
	}); err == nil {
		t.Fatal("a cause outside the schema's vocabulary was stored")
	}
}
