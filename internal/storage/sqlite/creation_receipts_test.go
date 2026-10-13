package sqlite

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/idempotency"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/meal"
	"github.com/savorum/savorum/internal/shopping"
)

func TestCreationReceiptSurvivesRestartAndTransactionReplay(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "receipts.db")

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	coordinator := idempotency.New(store)

	work, _, release, err := coordinator.Begin(ctx, "create-meal", "same", []byte(`{"name":"Lunch"}`))
	if err != nil {
		t.Fatal(err)
	}

	release()

	service, err := meal.NewService(store, store)
	if err != nil {
		t.Fatal(err)
	}

	first, err := service.CreateMeal(work, meal.Meal{Dishes: []meal.Dish{{DisplayName: "Soup"}}})
	if err != nil {
		t.Fatal(err)
	}
	// Reuse a context that observed no receipt, as a separate process can.
	_, err = service.CreateMeal(work, meal.Meal{Dishes: []meal.Dish{{DisplayName: "Soup"}}})

	var replay *idempotency.ReplayError
	if !errors.As(err, &replay) || replay.ID != first.ID {
		t.Fatalf("transaction replay: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = reopened.Close() })

	receipt, _ := idempotency.FromContext(work)

	id, err := reopened.FindIdempotency(ctx, receipt)
	if err != nil || id != first.ID {
		t.Fatalf("restart receipt %q %v", id, err)
	}

	if _, err := reopened.db.ExecContext(ctx, `UPDATE api_idempotency SET expires_at=unixepoch()-1`); err != nil {
		t.Fatal(err)
	}

	id, err = reopened.FindIdempotency(ctx, receipt)
	if err != nil || id != "" {
		t.Fatalf("expired receipt %q %v", id, err)
	}
}

func TestFailedCreationRollsBackReceipt(t *testing.T) {
	t.Parallel()
	store := openStore(t.Context(), t)
	coordinator := idempotency.New(store)

	work, _, release, err := coordinator.Begin(t.Context(), "create-meal", "failed", []byte("intent"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	invalid := meal.Meal{ID: "failed", Revision: 1, Dishes: []meal.Dish{{ID: "dish", MealID: "failed", EntryID: func() *library.EntryID { v := library.EntryID("missing"); return &v }(), DisplayName: "Missing"}}}
	if err := store.CreateMeal(work, "", invalid); err == nil {
		t.Fatal("invalid reference committed")
	}

	receipt, _ := idempotency.FromContext(work)

	id, err := store.FindIdempotency(t.Context(), receipt)
	if err != nil || id != "" {
		t.Fatalf("failed receipt %q %v", id, err)
	}
}

func TestHeaderShoppingReceiptResolvesBeforeMutation(t *testing.T) {
	t.Parallel()
	store := openStore(t.Context(), t)
	ctx := shopping.WithRetryScope(t.Context(), "apply-review")

	_, err := store.EditShoppingList(ctx, "groceries", shopping.Edit{OperationID: "review", Digest: "same", Activity: shopping.ActivityReview, Mutate: func(list *shopping.ShoppingList) error {
		list.Name = "Groceries"
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	result, found, err := store.ReplayShopping(ctx, "groceries", "review", "same")
	if err != nil || !found || result.AppliedRevision != 1 {
		t.Fatalf("scoped replay: %v %v %#v", found, err, result)
	}

	if _, found, err := store.ReplayShopping(t.Context(), "groceries", "review", "same"); err != nil || found {
		t.Fatalf("legacy namespace read a header receipt: %v %v", found, err)
	}

	if _, _, err := store.ReplayShopping(ctx, "groceries", "review", "different"); !errors.Is(err, idempotency.ErrConflict) {
		t.Fatalf("different review: %v", err)
	}
}
