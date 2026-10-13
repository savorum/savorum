package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/meal"
	"github.com/savorum/savorum/internal/mealcalendar"
)

func TestCalendarFeedStopsWhenItsCreatorLosesAccess(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	owner, err := store.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	ownerCtx := access.WithLibrary(identity.WithPrincipal(ctx, owner), "default")

	viewer, err := store.CreateLocalUser(ctx, owner.UserID, "guest", "Guest", "guest password phrase")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.SetLibraryMember(ownerCtx, "default", viewer.UserID, access.Viewer, false); err != nil {
		t.Fatal(err)
	}

	viewerCtx := access.WithLibrary(identity.WithPrincipal(ctx, viewer), "default")

	meals, err := meal.NewService(store, store)
	if err != nil {
		t.Fatal(err)
	}

	calendar := mealcalendar.NewService(store, meals, store.InstanceID, func(context.Context) string { return "" })

	secret, err := calendar.Create(viewerCtx, "Guest calendar")
	if err != nil {
		t.Fatal(err)
	}

	token := strings.TrimSuffix(strings.TrimPrefix(secret.Path, "/calendar/"), ".ics")
	if _, err := calendar.Read(ctx, token); err != nil {
		t.Fatalf("read before removal: %v", err)
	}

	if feeds, err := calendar.List(ownerCtx); err != nil || len(feeds) != 1 || feeds[0].Mine {
		t.Fatalf("owner listing = %#v, %v", feeds, err)
	}

	if _, err := store.db.ExecContext(ctx, "DELETE FROM library_memberships WHERE library_id='default' AND user_id=?", viewer.UserID); err != nil {
		t.Fatal(err)
	}

	if _, err := calendar.Read(ctx, token); !errors.Is(err, mealcalendar.ErrNotFound) {
		t.Fatalf("read after removal = %v", err)
	}
}
