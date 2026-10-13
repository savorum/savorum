package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestLibrariesIsolateContentPersonalStateAndAuthority(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	owner, err := store.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	ownerCtx := access.WithLibrary(identity.WithPrincipal(ctx, owner), "default")

	second, err := store.CreateLibrary(ownerCtx, "Other collection")
	if err != nil {
		t.Fatal(err)
	}

	otherCtx := access.WithLibrary(ownerCtx, second.ID)

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}
	// The same portable recipe can independently live in two libraries.
	first, err := service.Create(ownerCtx, testfixture.Document())
	if err != nil {
		t.Fatal(err)
	}

	other, err := service.Create(otherCtx, testfixture.Document())
	if err != nil {
		t.Fatal(err)
	}

	if _, err = service.Get(ownerCtx, other.ID); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("cross-library get: %v", err)
	}

	for _, readCtx := range []struct {
		name string
		id   library.EntryID
	}{{"default", first.ID}, {second.ID, other.ID}} {
		c := access.WithLibrary(ownerCtx, readCtx.name)

		got, e := service.List(c, library.ListOptions{})
		if e != nil || len(got) != 1 || got[0].ID != readCtx.id {
			t.Fatalf("scoped list: %v %v", got, e)
		}

		n, e := service.Count(c, library.ListOptions{})
		if e != nil || n != 1 {
			t.Fatalf("scoped count: %d %v", n, e)
		}
	}

	if err = store.PurgeMany(ownerCtx, []library.PurgeTarget{{ID: other.ID, Revision: 1}}, nil, time.Now()); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("cross-library bulk purge: %v", err)
	}

	if err = store.PurgeMany(ownerCtx, []library.PurgeTarget{{ID: first.ID, Revision: 1}, {ID: other.ID, Revision: 1}}, nil, time.Now()); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("mixed-library bulk purge: %v", err)
	}

	if _, err = store.ImportMany(ownerCtx, []library.ImportMutation{{ID: other.ID, ExpectedRevision: 1, Recipe: other.Recipe, Timestamp: time.Now()}}); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("cross-library replacement: %v", err)
	}

	if _, err = store.ImportMany(ownerCtx, []library.ImportMutation{
		{ID: first.ID, ExpectedRevision: 1, Recipe: first.Recipe, Timestamp: time.Now()},
		{ID: other.ID, ExpectedRevision: 1, Recipe: other.Recipe, Timestamp: time.Now()},
	}); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("mixed-library replacement: %v", err)
	}

	preserved, err := service.Get(ownerCtx, first.ID)
	if err != nil {
		t.Fatal(err)
	}

	if preserved.Revision != 1 || preserved.Recipe.Title != first.Recipe.Title {
		t.Fatalf("rejected batch still changed the in-scope recipe: %#v", preserved)
	}

	if _, err = store.MergeEntries(ownerCtx, library.MergeMutation{Survivor: first.ID, Duplicate: other.ID, Recipe: first.Recipe}); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("cross-library merge: %v", err)
	}

	now := time.Now()

	tag := library.Tag{ID: "home-tag", Name: "Dinner", Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err = store.CreateTag(ownerCtx, tag); err != nil {
		t.Fatal(err)
	}

	tag.ID = "other-tag"
	if err = store.CreateTag(otherCtx, tag); err != nil {
		t.Fatal("library-local tag uniqueness", err)
	}

	if _, err = store.db.ExecContext(ctx, "INSERT INTO recipe_tags(entry_id,tag_id) VALUES(?,?)", first.ID, tag.ID); err == nil {
		t.Fatal("database accepted cross-library classification")
	}

	tags, err := store.ListTags(ownerCtx)
	if err != nil || len(tags) != 1 || tags[0].ID != "home-tag" {
		t.Fatalf("tag list scope: %v %v", tags, err)
	}

	viewer, err := store.CreateLocalUser(ctx, owner.UserID, "reader", "Reader", "reader password phrase")
	if err != nil {
		t.Fatal(err)
	}

	if err = store.SetLibraryMember(ownerCtx, "default", viewer.UserID, access.Viewer, false); err != nil {
		t.Fatal(err)
	}

	viewerCtx := access.WithLibrary(identity.WithPrincipal(ctx, viewer), "default")
	if _, err = service.Create(viewerCtx, testfixture.Document()); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer create: %v", err)
	}

	organization, err := service.GetOrganization(viewerCtx, string(first.ID))
	if err != nil {
		t.Fatal(err)
	}

	organization.State.Favorite = true
	if _, err = service.ReplaceOrganization(viewerCtx, string(first.ID), organization.Revision, organization, nil); err != nil {
		t.Fatal("viewer personal state", err)
	}

	ownerState, err := service.GetOrganization(ownerCtx, string(first.ID))
	if err != nil || ownerState.State.Favorite {
		t.Fatalf("personal state leaked: %v %v", ownerState, err)
	}

	if _, err = store.GetOrganization(viewerCtx, string(first.ID), owner.UserID); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("guessed personal user: %v", err)
	}

	changed, err := service.GetOrganization(viewerCtx, string(first.ID))
	if err != nil {
		t.Fatal(err)
	}

	changed.TagIDs = []string{"home-tag"}
	if _, err = service.ReplaceOrganization(viewerCtx, string(first.ID), changed.Revision, changed, nil); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer shared change: %v", err)
	}

	secondTag := library.Tag{ID: "home-tag-2", Name: "Lunch", Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err = store.CreateTag(ownerCtx, secondTag); err != nil {
		t.Fatal(err)
	}

	facet := library.Facet{ID: "f-course-main", Kind: library.FacetCourse, Value: "Main", Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err = store.CreateFacet(ownerCtx, facet); err != nil {
		t.Fatal(err)
	}

	for _, book := range []library.Book{
		{ID: "b-multi-a", Kind: library.BookPersonal, Title: "First", Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "b-multi-b", Kind: library.BookPersonal, Title: "Second", Revision: 1, CreatedAt: now, UpdatedAt: now},
	} {
		if err = store.CreateBook(ownerCtx, book); err != nil {
			t.Fatal(err)
		}
	}

	changed.TagIDs = []string{"home-tag", "home-tag-2"}

	changed.FacetIDs = []string{facet.ID}
	if _, err = service.ReplaceOrganization(viewerCtx, string(first.ID), changed.Revision, changed, nil); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer multi-classification change: %v", err)
	}

	membership := []library.BookMembershipChange{{BookID: "b-multi-a", Revision: 1, Member: true}}
	if _, err = service.ReplaceOrganization(viewerCtx, string(first.ID), changed.Revision, changed, membership); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer membership change: %v", err)
	}

	shared, err := service.GetOrganization(ownerCtx, string(first.ID))
	if err != nil {
		t.Fatal(err)
	}

	shared.TagIDs = []string{"home-tag", "home-tag-2"}

	shared.FacetIDs = []string{facet.ID}
	if _, err = service.ReplaceOrganization(ownerCtx, string(first.ID), shared.Revision, shared, []library.BookMembershipChange{
		{BookID: "b-multi-a", Revision: 1, Member: true},
		{BookID: "b-multi-b", Revision: 1, Member: true},
	}); err != nil {
		t.Fatalf("editor multi-classification and membership change: %v", err)
	}

	if err = store.SetLibraryMember(ownerCtx, "default", viewer.UserID, "", false); err != nil {
		t.Fatal(err)
	}

	if _, err = service.Get(viewerCtx, first.ID); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("removed membership retained access: %v", err)
	}
	// Instance authority alone cannot read a library after its explicit membership is removed.
	if _, err = store.db.ExecContext(ctx, "DELETE FROM library_memberships WHERE library_id=?", second.ID); err != nil {
		t.Fatal(err)
	}

	if _, err = service.Get(otherCtx, other.ID); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("administrator bypassed membership: %v", err)
	}
}

func openRevocationFixture(ctx context.Context, t *testing.T) (*Store, context.Context, *Store, context.Context, identity.Principal) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "library.db")

	owner, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = owner.Close() })

	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = second.Close() })

	ownerPrincipal, err := owner.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	ownerCtx := access.WithLibrary(identity.WithPrincipal(ctx, ownerPrincipal), "default")

	editor, err := owner.CreateLocalUser(ctx, ownerPrincipal.UserID, "editor", "Editor", "editor password phrase")
	if err != nil {
		t.Fatal(err)
	}

	if err := owner.SetLibraryMember(ownerCtx, "default", editor.UserID, access.Editor, false); err != nil {
		t.Fatal(err)
	}

	editorCtx := access.WithLibrary(identity.WithPrincipal(ctx, editor), "default")

	return owner, ownerCtx, second, editorCtx, editor
}

func revokeMembership(ctx context.Context, t *testing.T, store *Store, user string) *sql.Tx {
	t.Helper()

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM library_memberships WHERE library_id='default' AND user_id=?", user); err != nil {
		_ = tx.Rollback()

		t.Fatal(err)
	}

	return tx
}

func awaitBlocked(t *testing.T, revoke *sql.Tx, done <-chan error) {
	t.Helper()

	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()

	select {
	case err := <-done:
		_ = revoke.Rollback()

		t.Fatalf("write finished before membership revocation committed: %v", err)
	case <-timer.C:
	}
}

func awaitWrite(ctx context.Context, t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		t.Fatal("timed out waiting for blocked write")
		return nil
	}
}

func TestImportManyRechecksMembershipInsideWriteTransaction(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	owner, ownerCtx, editorStore, editorCtx, editor := openRevocationFixture(ctx, t)

	seedRecipe(ctx, t, owner, "revoked-import")

	original, err := owner.Get(ownerCtx, "revoked-import", library.StateActive)
	if err != nil {
		t.Fatal(err)
	}

	revoke := revokeMembership(ctx, t, owner, editor.UserID)

	replacement := testfixture.Document()
	replacement.ID = "drevoked-import"
	replacement.Title = "Revoked title"

	done := make(chan error, 1)

	go func() {
		_, err := editorStore.ImportMany(editorCtx, []library.ImportMutation{{ID: "revoked-import", ExpectedRevision: 1, Recipe: replacement, Timestamp: time.Now()}})
		done <- err
	}()

	awaitBlocked(t, revoke, done)

	if err := revoke.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := awaitWrite(ctx, t, done); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("import after revocation: %v", err)
	}

	entry, err := owner.Get(ownerCtx, "revoked-import", library.StateActive)
	if err != nil {
		t.Fatal(err)
	}

	if entry.Revision != original.Revision || entry.Recipe.Title != original.Recipe.Title {
		t.Fatalf("revoked import changed the recipe: revision %d title %q", entry.Revision, entry.Recipe.Title)
	}
}

func TestReplaceOrganizationRechecksMembershipInsideWriteTransaction(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	owner, _, editorStore, editorCtx, editor := openRevocationFixture(ctx, t)

	seedRecipe(ctx, t, owner, "revoked-organization")

	organization, err := editorStore.GetOrganization(editorCtx, "revoked-organization", editor.UserID)
	if err != nil {
		t.Fatal(err)
	}

	organization.State.Favorite = !organization.State.Favorite

	revoke := revokeMembership(ctx, t, owner, editor.UserID)

	done := make(chan error, 1)

	go func() {
		_, err := editorStore.ReplaceOrganization(editorCtx, "revoked-organization", editor.UserID, organization.Revision, organization, nil, time.Now())
		done <- err
	}()

	awaitBlocked(t, revoke, done)

	if err := revoke.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := awaitWrite(ctx, t, done); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("organization replacement after revocation: %v", err)
	}

	current, err := editorStore.GetOrganization(ctx, "revoked-organization", editor.UserID)
	if err != nil {
		t.Fatal(err)
	}

	if current.State.Favorite == organization.State.Favorite {
		t.Fatal("revoked replacement changed personal state")
	}
}

func TestAssetsRequireLibraryPossession(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	p, err := store.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	first := access.WithLibrary(identity.WithPrincipal(ctx, p), "default")

	l, err := store.CreateLibrary(first, "Second")
	if err != nil {
		t.Fatal(err)
	}

	second := access.WithLibrary(first, l.ID)
	hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	blob := assets.Blob{ID: assets.ID("sha256:" + hash), ContentHash: hash, MediaType: "image/png", Size: 12, StorageKey: "one.png", CreatedAt: time.Now()}
	if _, err = store.RegisterStoredAsset(first, blob, time.Now()); err != nil {
		t.Fatal(err)
	}

	if _, err = store.GetStoredAsset(second, blob.ID); !errors.Is(err, assets.ErrNotFound) {
		t.Fatalf("cross-library asset: %v", err)
	}

	present, err := store.RegisteredAssets(second, []string{hash})
	if err != nil || present[hash] {
		t.Fatalf("cross-library existence leaked: %v %v", present, err)
	}

	foreignDocument := testfixture.Document()
	foreignDocument.ID = "foreign-bytes"
	foreignDocument.Assets = []recipe.Asset{{ID: "photo", Path: "assets/photo.png", MediaType: "image/png", SHA256: hash, Size: 12}}
	foreignDocument.ImageRefs = []string{"photo"}
	foreignDocument.PrimaryImage = "photo"

	if err = store.Create(second, library.Entry{
		ID: "rforeign", Recipe: foreignDocument, Revision: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("recipe save referencing foreign-owned bytes: %v", err)
	}

	mismatched := foreignDocument
	mismatched.ID = "mismatched-bytes"
	mismatched.Assets = []recipe.Asset{{ID: "photo", Path: "assets/photo.png", MediaType: "image/png", SHA256: hash, Size: 99}}

	if err = store.Create(first, library.Entry{
		ID: "rmismatch", Recipe: mismatched, Revision: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); !errors.Is(err, library.ErrConflict) {
		t.Fatalf("asset metadata mismatch: %v", err)
	}

	if _, err = store.RegisterStoredAsset(second, blob, time.Now()); err != nil {
		t.Fatal("explicit possession may deduplicate bytes", err)
	}

	if _, err = store.GetStoredAsset(second, blob.ID); err != nil {
		t.Fatal(err)
	}
}

func TestListReferencesScopesByStateAndLibrary(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	p, err := store.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	first := access.WithLibrary(identity.WithPrincipal(ctx, p), "default")

	secondLibrary, err := store.CreateLibrary(first, "Elsewhere")
	if err != nil {
		t.Fatal(err)
	}

	second := access.WithLibrary(first, secondLibrary.ID)
	now := time.Now()
	documents := map[library.EntryID]recipe.Document{}

	create := func(scope context.Context, id library.EntryID, title string) {
		t.Helper()

		document := testfixture.Document()
		document.ID = recipe.RecipeID("doc-" + string(id))
		document.Title = title
		documents[id] = document

		if err := store.Create(scope, library.Entry{
			ID: id, Recipe: document, Revision: 1, CreatedAt: now, UpdatedAt: now,
		}, library.RevisionMetadata{Cause: library.RevisionCauseAPI}); err != nil {
			t.Fatal(err)
		}
	}

	create(first, "rzeta", "Zeta")
	create(first, "ralpha", "Alpha")
	create(first, "rmiddle", "Middle")
	create(second, "rother", "Aardvark")

	if _, err := store.Revise(first, library.RevisionCommand{
		ID: "rzeta", ExpectedRevision: 1, Recipe: documents["rzeta"],
		CurrentState: library.StateActive, TargetState: library.StateTrash,
		Metadata:  library.RevisionMetadata{Cause: library.RevisionCauseTrash},
		Timestamp: now,
	}); err != nil {
		t.Fatal(err)
	}

	active, err := store.ListReferences(first, library.StateActive)
	if err != nil {
		t.Fatal(err)
	}

	wantActive := []library.RecipeReference{
		{ID: "ralpha", Title: "Alpha", Revision: 1},
		{ID: "rmiddle", Title: "Middle", Revision: 1},
	}
	if !reflect.DeepEqual(active, wantActive) {
		t.Fatalf("active references = %#v, want %#v", active, wantActive)
	}

	trash, err := store.ListReferences(first, library.StateTrash)
	if err != nil {
		t.Fatal(err)
	}

	wantTrash := []library.RecipeReference{{ID: "rzeta", Title: "Zeta", Revision: 2}}
	if !reflect.DeepEqual(trash, wantTrash) {
		t.Fatalf("trash references = %#v, want %#v", trash, wantTrash)
	}

	other, err := store.ListReferences(second, library.StateActive)
	if err != nil {
		t.Fatal(err)
	}

	wantOther := []library.RecipeReference{{ID: "rother", Title: "Aardvark", Revision: 1}}
	if !reflect.DeepEqual(other, wantOther) {
		t.Fatalf("other library references = %#v, want %#v", other, wantOther)
	}
}
