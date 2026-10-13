package sqlite

import (
	"errors"
	"testing"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/shopping"
)

func TestShoppingRetriesAndConflicts(t *testing.T) {
	t.Parallel()
	store := openStore(t.Context(), t)
	service := shopping.NewService(store)
	name := "Milk"
	change := shopping.ShoppingChange{ID: "operation-a", Kind: "add", ItemID: "milk", Name: &name}

	first, err := service.Change(t.Context(), change)
	if err != nil {
		t.Fatal(err)
	}

	retry, err := service.Change(t.Context(), change)
	if err != nil {
		t.Fatal(err)
	}

	if retry.Revision != first.Revision || len(retry.Items) != 1 {
		t.Fatal("retry duplicated the edit")
	}

	other := "Oat milk"

	change.Name = &other
	if _, err := service.Change(t.Context(), change); !errors.Is(err, shopping.ErrConflict) {
		t.Fatalf("reused receipt: %v", err)
	}

	bought := true
	if _, err := service.Change(t.Context(), shopping.ShoppingChange{ID: "operation-b", Kind: "edit", ItemID: "milk", BaseRevision: 1, Bought: &bought}); err != nil {
		t.Fatal(err)
	}

	quantity := "2 cartons"

	merged, err := service.Change(t.Context(), shopping.ShoppingChange{ID: "operation-c", Kind: "edit", ItemID: "milk", BaseRevision: 1, Buy: &quantity})
	if err != nil {
		t.Fatal(err)
	}

	if !merged.Items[0].Bought || merged.Items[0].Buy != quantity {
		t.Fatal("disjoint edits were lost")
	}

	if _, err := service.Change(t.Context(), shopping.ShoppingChange{ID: "operation-d", Kind: "edit", ItemID: "milk", BaseRevision: 1, Buy: &quantity}); !errors.Is(err, shopping.ErrConflict) {
		t.Fatalf("same field did not conflict: %v", err)
	}

	current, err := service.Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if current.Revision != 3 {
		t.Fatal("failed edit was committed")
	}

	change.Name = &name

	recovered, err := service.Change(t.Context(), change)
	if err != nil {
		t.Fatal(err)
	}

	if recovered.AppliedRevision != 1 || recovered.Revision != 3 {
		t.Fatalf("lost-response receipt: %#v", recovered)
	}
}

func TestShoppingViewersTickOffAndHistoryNamesWhoChangedWhat(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	ownerPrincipal, err := store.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	ownerCtx := access.WithLibrary(identity.WithPrincipal(ctx, ownerPrincipal), "default")

	viewer, err := store.CreateLocalUser(ctx, ownerPrincipal.UserID, "shopper", "Shopper", "shopper password phrase")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.SetLibraryMember(ownerCtx, "default", viewer.UserID, access.Viewer, false); err != nil {
		t.Fatal(err)
	}

	viewerCtx := access.WithLibrary(identity.WithPrincipal(ctx, viewer), "default")
	service := shopping.NewService(store)
	name := "Milk"

	added, err := service.Change(ownerCtx, shopping.ShoppingChange{ID: "add-milk", Kind: "add", ItemID: "milk", Name: &name})
	if err != nil {
		t.Fatal(err)
	}

	bought := true
	if _, err := service.Change(viewerCtx, shopping.ShoppingChange{ID: "buy-milk", Kind: "edit", ItemID: "milk", BaseRevision: added.Revision, Bought: &bought}); err != nil {
		t.Fatalf("viewer could not tick an item off: %v", err)
	}

	renamed := "Oat milk"
	if _, err := service.Change(viewerCtx, shopping.ShoppingChange{ID: "rename-milk", Kind: "edit", ItemID: "milk", BaseRevision: added.Revision, Name: &renamed}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer renamed an item: %v", err)
	}

	if _, err := service.Change(viewerCtx, shopping.ShoppingChange{ID: "add-bread", Kind: "add", ItemID: "bread", Name: &name}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer added an item: %v", err)
	}

	activity, err := service.Activity(viewerCtx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(activity) != 2 || activity[0].Kind != shopping.ActivityBought || activity[0].ActorName != "Shopper" ||
		activity[1].Kind != shopping.ActivityAdd || activity[1].ActorID != ownerPrincipal.UserID || activity[0].Items[0].Name != "Milk" {
		t.Fatalf("activity = %#v", activity)
	}

	if _, err := service.Change(viewerCtx, shopping.ShoppingChange{ID: "buy-milk", Kind: "edit", ItemID: "milk", BaseRevision: added.Revision, Bought: &bought}); err != nil {
		t.Fatal(err)
	}

	if again, _ := service.Activity(viewerCtx, 0, 10); len(again) != 2 {
		t.Fatalf("retry recorded twice: %#v", again)
	}

	if err := service.DeleteList(ownerCtx); !errors.Is(err, shopping.ErrInvalid) {
		t.Fatalf("deleting Groceries = %v", err)
	}

	weekend := shopping.WithList(ownerCtx, "weekend")
	if _, err := service.CreateList(ownerCtx, shopping.ShoppingListInput{ID: "weekend", Name: "Weekend"}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.RenameList(weekend, shopping.ShoppingListRename{ID: "rename", Name: "Market"}); err != nil {
		t.Fatal(err)
	}

	if err := service.DeleteList(shopping.WithList(viewerCtx, "weekend")); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer deleted a list: %v", err)
	}

	if err := service.DeleteList(weekend); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Get(weekend); !errors.Is(err, shopping.ErrNotFound) {
		t.Fatalf("deleted list = %v", err)
	}
}
