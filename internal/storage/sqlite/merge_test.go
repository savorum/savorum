package sqlite

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestMergeEntriesKeepsOrganizationHistorySourcesAndAudit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	now := time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC)
	left, right := testfixture.Document(), testfixture.Document()
	left.ID, right.ID = "merge-left-doc", "merge-right-doc"

	left.Title, right.Title = "Kept soup", "Duplicate soup"
	for _, entry := range []library.Entry{{ID: "merge-left", Recipe: left, Revision: 1, CreatedAt: now, UpdatedAt: now}, {ID: "merge-right", Recipe: right, Revision: 1, CreatedAt: now, UpdatedAt: now}} {
		if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.CreateBook(ctx, library.Book{ID: "merge-book", Kind: library.BookPersonal, Title: "Favorites", Sections: []library.BookSection{}, Entries: []library.BookEntry{}, Revision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	for _, tag := range []string{"tag-left", "tag-right"} {
		if err := store.CreateTag(ctx, library.Tag{ID: tag, Name: tag, Revision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := store.ReplaceOrganization(ctx, "merge-left", library.DefaultUserID, 1, library.Organization{TagIDs: []string{"tag-left"}, State: library.UserState{Favorite: true}, Sources: []library.RecipeSource{{ID: "source-left", Kind: library.SourceURL, URL: "https://left.example"}}}, []library.BookMembershipChange{{BookID: "merge-book", Revision: 1, Member: true, EntryID: "left-book-entry"}}, now); err != nil {
		t.Fatal(err)
	}

	rating := uint8(4)
	if _, err := store.ReplaceOrganization(ctx, "merge-right", library.DefaultUserID, 1, library.Organization{TagIDs: []string{"tag-right"}, State: library.UserState{Rating: &rating, PersonalNote: "use lower heat"}, Sources: []library.RecipeSource{{ID: "source-right", Kind: library.SourceURL, URL: "https://right.example"}}}, []library.BookMembershipChange{{BookID: "merge-book", Revision: 2, Member: true, EntryID: "right-book-entry"}}, now); err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, "UPDATE book_entries SET note=CASE entry_id WHEN 'left-book-entry' THEN 'left note' ELSE 'right note' END WHERE book_id=?", "merge-book"); err != nil {
		t.Fatal(err)
	}

	if _, err := store.RecordCooking(ctx, "merge-right", library.DefaultUserID, library.CookingRecord{ID: "merge-cook", CookedOn: library.Date("2026-09-01"), CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	sha := strings.Repeat("b", 64)
	if _, err := store.db.ExecContext(ctx, "INSERT INTO assets(id,content_hash,media_type,size,storage_key,created_at) VALUES(?,?,?,?,?,?)", "sha256:"+sha, sha, "text/plain", 4, "source/merge", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	if err := store.AttachSource(ctx, library.SourceAttachment{EntryID: "merge-right", Revision: 1, AssetID: "sha256:" + sha, Filename: "original.txt", MediaType: "text/plain", Size: 4}); err != nil {
		t.Fatal(err)
	}

	got, err := store.MergeEntries(ctx, library.MergeMutation{Survivor: "merge-left", Duplicate: "merge-right", ExpectedSurvivorRevision: 1, ExpectedDuplicateRevision: 1, Recipe: left, Timestamp: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}

	if got.Revision != 2 || got.Recipe.ID != left.ID {
		t.Fatalf("survivor=%#v", got)
	}

	if _, err := store.Get(ctx, "merge-right", library.StateActive); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("duplicate remained active: %v", err)
	}

	if _, err := store.Get(ctx, "merge-right", library.StateTrash); err != nil {
		t.Fatalf("duplicate was not trashed: %v", err)
	}

	organization, err := store.GetOrganization(ctx, "merge-left", library.DefaultUserID)
	if err != nil {
		t.Fatal(err)
	}

	if len(organization.TagIDs) != 2 || len(organization.Sources) != 2 || !organization.State.Favorite || organization.State.Rating == nil || *organization.State.Rating != rating || organization.State.PersonalNote != "use lower heat" {
		t.Fatalf("merged organization=%#v", organization)
	}

	var precondition *library.PreconditionError
	if _, err := store.ReplaceOrganization(ctx, "merge-left", library.DefaultUserID, 2, library.Organization{TagIDs: []string{"tag-left"}}, nil, now.Add(2*time.Minute)); !errors.As(err, &precondition) {
		t.Fatalf("organization write from before the merge: %v", err)
	}

	history, err := store.ListCookings(ctx, "merge-left", library.DefaultUserID, library.CookingListOptions{Limit: 10})
	if err != nil || len(history.Records) != 1 || history.Records[0].ID != "merge-cook" {
		t.Fatalf("merged cooking history=%#v err=%v", history, err)
	}

	attachments, err := store.ListSourceAttachments(ctx, "merge-left", 2)
	if err != nil || len(attachments) != 1 || attachments[0].Filename != "original.txt" {
		t.Fatalf("merged source attachments=%#v err=%v", attachments, err)
	}

	book, err := store.GetBook(ctx, "merge-book", library.StateActive)
	if err != nil || len(book.Entries) != 1 || book.Entries[0].EntryID != "merge-left" || !strings.Contains(book.Entries[0].Note, "right note") {
		t.Fatalf("merged book=%#v err=%v", book, err)
	}

	events, err := store.ListImportEvents(ctx, "merge-left", library.RevisionListOptions{Limit: 10})
	if err != nil || len(events) != 1 || events[0].Operation != "merge" || events[0].DisplayName != "merge-right" {
		t.Fatalf("merge audit=%#v err=%v", events, err)
	}

	var cause library.RevisionCause
	if err := store.db.QueryRowContext(ctx, "SELECT cause FROM recipe_revisions WHERE entry_id=? AND revision=2", "merge-left").Scan(&cause); err != nil || cause != library.RevisionCauseImport {
		t.Fatalf("merge revision cause=%q err=%v", cause, err)
	}
}

func TestMergeEntriesStaleDuplicateLeavesBothEntriesUntouched(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	document := testfixture.Document()
	document.ID = recipe.RecipeID("merge-stale-doc")
	now := time.Now().UTC()

	for _, id := range []library.EntryID{"merge-stale-left", "merge-stale-right"} {
		side := document

		side.ID = recipe.RecipeID(string(document.ID) + string(id))
		if err := store.Create(ctx, library.Entry{ID: id, Recipe: side, Revision: 1, CreatedAt: now, UpdatedAt: now}, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}
	}

	_, err := store.MergeEntries(ctx, library.MergeMutation{Survivor: "merge-stale-left", Duplicate: "merge-stale-right", ExpectedSurvivorRevision: 1, ExpectedDuplicateRevision: 2, Recipe: document, Timestamp: now})
	if !errors.Is(err, library.ErrPreconditionFailed) {
		t.Fatalf("stale merge error=%v", err)
	}

	left, leftErr := store.Get(ctx, "merge-stale-left", library.StateActive)

	right, rightErr := store.Get(ctx, "merge-stale-right", library.StateActive)
	if leftErr != nil || rightErr != nil || left.Revision != 1 || right.Revision != 1 {
		t.Fatalf("stale merge changed entries left=%#v right=%#v errors=%v,%v", left, right, leftErr, rightErr)
	}
}

func TestMergeRollsBackWhenAuditWriteFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	now := time.Now().UTC()
	left, right := testfixture.Document(), testfixture.Document()
	left.ID = "rollback-left"

	right.ID = "rollback-right"
	for _, entry := range []library.Entry{{ID: "left", Recipe: left, Revision: 1, CreatedAt: now, UpdatedAt: now}, {ID: "right", Recipe: right, Revision: 1, CreatedAt: now, UpdatedAt: now}} {
		if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER fail_merge_audit BEFORE INSERT ON import_events BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`); err != nil {
		t.Fatal(err)
	}

	_, err := store.MergeEntries(ctx, library.MergeMutation{Survivor: "left", Duplicate: "right", ExpectedSurvivorRevision: 1, ExpectedDuplicateRevision: 1, Recipe: left, Timestamp: now})
	if err == nil {
		t.Fatal("injected audit failure was ignored")
	}

	for _, id := range []library.EntryID{"left", "right"} {
		entry, err := store.Get(ctx, id, library.StateActive)
		if err != nil || entry.Revision != 1 {
			t.Fatalf("partial merge for %s: %+v %v", id, entry, err)
		}

		revisions, err := store.ListRevisions(ctx, id, library.RevisionListOptions{Limit: 10})
		if err != nil || len(revisions) != 1 {
			t.Fatalf("partial revision for %s: %+v %v", id, revisions, err)
		}
	}
}

func TestMergeEntriesPreservesPersonalStateIsolation(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)
	now := time.Now().UTC()

	admin, err := store.BootstrapAdmin(ctx, "alice", "Alice", "long passphrase for alice")
	if err != nil {
		t.Fatal(err)
	}

	bob, err := store.CreateLocalUser(ctx, admin.UserID, "bob", "Bob", "long passphrase for bob")
	if err != nil {
		t.Fatal(err)
	}

	adminCtx := access.WithLibrary(identity.WithPrincipal(ctx, admin), "default")
	if err := store.SetLibraryMember(adminCtx, "default", bob.UserID, access.Viewer, false); err != nil {
		t.Fatal(err)
	}

	left, right := testfixture.Document(), testfixture.Document()

	left.ID, right.ID = "left-doc", "right-doc"
	for _, entry := range []library.Entry{{ID: "left", Recipe: left, Revision: 1, CreatedAt: now, UpdatedAt: now}, {ID: "right", Recipe: right, Revision: 1, CreatedAt: now, UpdatedAt: now}} {
		if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}
	}

	users := []struct {
		id    string
		state library.UserState
	}{
		{admin.UserID, library.UserState{Favorite: true, Rating: new(uint8(5)), PersonalNote: "Alice private note", PreferredYield: new(recipe.Rational("8"))}},
		{bob.UserID, library.UserState{Rating: new(uint8(2)), PersonalNote: "Bob private note", PreferredScale: new(recipe.Rational("2"))}},
	}

	for _, user := range users {
		for _, entry := range []string{"left", "right"} {
			state := library.UserState{}
			if entry == "right" {
				state = user.state
			}

			userCtx := access.WithLibrary(identity.WithPrincipal(ctx, identity.Principal{UserID: user.id}), "default")

			current, err := store.GetOrganization(userCtx, entry, user.id)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := store.ReplaceOrganization(userCtx, entry, user.id, current.Revision, library.Organization{State: state}, nil, now); err != nil {
				t.Fatal(err)
			}
		}
	}

	if _, err := store.MergeEntries(adminCtx, library.MergeMutation{Survivor: "left", Duplicate: "right", ExpectedSurvivorRevision: 1, ExpectedDuplicateRevision: 1, Recipe: left, Timestamp: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}

	for _, user := range users {
		userCtx := access.WithLibrary(identity.WithPrincipal(ctx, identity.Principal{UserID: user.id}), "default")

		got, err := store.GetOrganization(userCtx, "left", user.id)
		if err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(got.State, user.state) {
			t.Errorf("user %s: got state %+v, want own state %+v", user.id, got.State, user.state)
		}
	}
}
