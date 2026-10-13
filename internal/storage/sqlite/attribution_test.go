package sqlite

import (
	"testing"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestRevisionAndImportActorsSurviveAccountErasure(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	principal, err := store.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	actorContext := access.WithLibrary(identity.WithPrincipal(ctx, principal), "default")
	document := testfixture.Document()

	entries, err := store.ImportMany(actorContext, []library.ImportMutation{{ID: "actor-recipe", Recipe: document, Timestamp: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}

	book := seedBook(actorContext, t, store, "actor-book")

	recipeRevision, err := store.GetRevision(actorContext, entries[0].ID, 1)
	if err != nil {
		t.Fatal(err)
	}

	bookRevision, err := store.GetBookRevision(actorContext, book.ID, 1)
	if err != nil {
		t.Fatal(err)
	}

	events, err := store.ListImportEvents(actorContext, entries[0].ID, library.RevisionListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if recipeRevision.ActorID != principal.UserID || recipeRevision.ActorName != principal.DisplayName || bookRevision.ActorID != principal.UserID || len(events) != 1 || events[0].ActorID != principal.UserID {
		t.Fatalf("missing actor: %#v %#v %#v", recipeRevision, bookRevision, events)
	}

	if _, err := store.db.ExecContext(ctx, `UPDATE users SET status='deleted',display_name='Private erased name' WHERE id=?`, principal.UserID); err != nil {
		t.Fatal(err)
	}

	revisions, err := store.ListRevisions(ctx, entries[0].ID, library.RevisionListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	books, err := store.ListBookRevisions(ctx, book.ID, library.RevisionListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	events, err = store.ListImportEvents(ctx, entries[0].ID, library.RevisionListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if revisions[0].ActorName != "Former user" || books[0].ActorName != "Former user" || events[0].ActorName != "Former user" {
		t.Fatalf("erased display name retained: %#v %#v %#v", revisions, books, events)
	}

	if entries[0].Recipe.Authors[0].Name != document.Authors[0].Name {
		t.Fatal("application actor replaced authored recipe author")
	}
}

func TestAttributionDistinguishesExplicitSystemAndUnknownHistory(t *testing.T) {
	if attributionActor(t.Context()) != "" {
		t.Fatal("invented historical actor")
	}

	if attributionActor(identity.WithPrincipal(t.Context(), identity.Principal{System: true})) != "system" {
		t.Fatal("explicit system actor lost")
	}
}
