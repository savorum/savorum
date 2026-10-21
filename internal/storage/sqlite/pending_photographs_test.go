package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestListPendingPhotographsFindsOnlyRecipesStillWaiting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(ctx, t)
	timestamp := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)

	for _, seed := range []struct {
		id       library.EntryID
		deferred []any
	}{
		{"rwaiting", []any{map[string]any{"url": "https://images.example/1.jpg"}}},
		{"rfetched", []any{}},
		{"rplain", nil},
	} {
		document := testfixture.Document()
		document.ID = recipe.RecipeID("d" + string(seed.id))

		if seed.deferred != nil {
			document.Extensions = map[string]any{"x-org.savorum.pending-images": map[string]any{"deferred": seed.deferred}}
		}

		entry := library.Entry{ID: seed.id, Recipe: document, Revision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}
		if err := store.Create(ctx, entry, library.RevisionMetadata{Cause: library.RevisionCauseImport}); err != nil {
			t.Fatal(err)
		}
	}

	pending, err := store.ListPendingPhotographs(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(pending) != 1 || pending[0].EntryID != "rwaiting" || pending[0].LibraryID == "" {
		t.Fatalf("pending = %#v", pending)
	}

	if after, err := store.ListPendingPhotographs(ctx, "rwaiting", 10); err != nil || len(after) != 0 {
		t.Fatalf("after the cursor = %#v, %v", after, err)
	}

	owner, err := store.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.ListPendingPhotographs(access.WithLibrary(identity.WithPrincipal(ctx, owner), "default"), "", 10); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("a request listed every library: %v", err)
	}
}
