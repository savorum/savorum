package shopping

import (
	"errors"
	"testing"

	"github.com/savorum/savorum/internal/access"
)

func TestIndependentEditsAndRemoval(t *testing.T) {
	name, buy := "Milk", "2 cartons"
	list := ShoppingList{Items: []ShoppingItem{}}
	apply := func(change ShoppingChange) {
		t.Helper()

		if err := Apply(&list, change); err != nil {
			t.Fatal(err)
		}

		list.Revision++
	}
	apply(ShoppingChange{ID: "add", Kind: "add", ItemID: "milk", Name: &name})

	bought := true
	apply(ShoppingChange{ID: "check", Kind: "edit", ItemID: "milk", BaseRevision: 1, Bought: &bought})
	apply(ShoppingChange{ID: "quantity", Kind: "edit", ItemID: "milk", BaseRevision: 1, Buy: &buy})

	if !list.Items[0].Bought || list.Items[0].Buy != buy {
		t.Fatal("independent fields did not merge")
	}

	if err := Apply(&list, ShoppingChange{ID: "stale", Kind: "edit", ItemID: "milk", BaseRevision: 1, Buy: &buy}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale quantity: %v", err)
	}

	apply(ShoppingChange{ID: "remove", Kind: "remove", ItemID: "milk", BaseRevision: 3})

	if err := Apply(&list, ShoppingChange{ID: "offline", Kind: "edit", ItemID: "milk", BaseRevision: 3, Name: &name}); !errors.Is(err, ErrDeleted) {
		t.Fatalf("removed item: %v", err)
	}

	apply(ShoppingChange{ID: "undo", Kind: "restore", ItemID: "milk", BaseRevision: 4})

	if list.Items[0].Deleted || !list.Items[0].Bought {
		t.Fatal("undo lost item state")
	}
}

func TestShoppingTotalsCombineExactUnitsOfOneSystem(t *testing.T) {
	cases := []struct {
		name    string
		sources []ShoppingSource
		want    string
	}{
		{"metric mass", []ShoppingSource{{Amount: "500", Unit: "mass.gram"}, {Amount: "1", Unit: "mass.kilogram"}}, "3/2 kg"},
		{"small metric mass", []ShoppingSource{{Amount: "100", Unit: "mass.gram"}, {Amount: "1/4", Unit: "mass.kilogram"}}, "350 g"},
		{"US volume", []ShoppingSource{{Amount: "1", Unit: "volume.cup.us.customary"}, {Amount: "2", Unit: "volume.tablespoon.us.customary"}}, "9/8 cups"},
		{"systems stay apart", []ShoppingSource{{Amount: "500", Unit: "mass.gram"}, {Amount: "1", Unit: "mass.pound.avoirdupois"}}, "500 g + 1 lb"},
		{"mass never meets volume", []ShoppingSource{{Amount: "1", Unit: "volume.cup.us.customary"}, {Amount: "120", Unit: "mass.gram"}}, "1 cup + 120 g"},
		{"inexact conversions stay apart", []ShoppingSource{{Amount: "1", Unit: "volume.cup.us.culinary"}, {Amount: "1", Unit: "volume.cup.us.customary"}}, "1 cup + 1 cup"},
	}

	for _, test := range cases {
		list := ShoppingList{Items: []ShoppingItem{{ID: "row", Name: "Flour", Sources: test.sources}}}
		RefreshAmounts(&list)

		if list.Items[0].Needed != test.want {
			t.Errorf("%s: needed = %q, want %q", test.name, list.Items[0].Needed, test.want)
		}
	}

	list := ShoppingList{Items: []ShoppingItem{{ID: "flour", Name: "Flour", Fields: map[string]uint64{}, Sources: []ShoppingSource{{ID: "a", Amount: "500", Unit: "mass.gram"}}}}}
	if !mergeNeed(&list, shoppingNeed{source: ShoppingSource{ID: "b", Amount: "1", Unit: "mass.kilogram"}, choice: ShoppingChoice{Name: "flour"}}) {
		t.Fatal("grams and kilograms of the same flour should share a row")
	}
}

func TestShoppingNeverMergesNearMatchNames(t *testing.T) {
	pairs := [][2]string{
		{"cream", "cream cheese"},
		{"green onion", "onion"},
		{"salted butter", "unsalted butter"},
		{"chopped tomatoes", "tomato paste"},
		{"onion", "onions"},
	}

	for _, pair := range pairs {
		list := ShoppingList{Items: []ShoppingItem{{ID: "row", Name: pair[0], Fields: map[string]uint64{}, Sources: []ShoppingSource{{ID: "a", Amount: "1"}}}}}
		if mergeNeed(&list, shoppingNeed{source: ShoppingSource{ID: "b", Amount: "1"}, choice: ShoppingChoice{Name: pair[1]}}) {
			t.Errorf("%q and %q were merged", pair[0], pair[1])
		}
	}

	list := ShoppingList{Items: []ShoppingItem{{ID: "row", Name: "Onion", Note: "diced", Fields: map[string]uint64{}, Sources: []ShoppingSource{{ID: "a", Amount: "1"}}}}}
	if mergeNeed(&list, shoppingNeed{source: ShoppingSource{ID: "b", Amount: "2"}, choice: ShoppingChoice{Name: "Onion", Note: "sliced"}}) {
		t.Error("a different preparation joined without being asked")
	}
}

func TestShoppingManualArrangementPersists(t *testing.T) {
	list := ShoppingList{Revision: 1, Items: []ShoppingItem{
		{ID: "diced", Name: "Onion", Note: "diced", Revision: 1, Fields: map[string]uint64{}, Sources: []ShoppingSource{{ID: "m/d/onion", MealID: "m", Name: "Onion", Note: "diced", Amount: "1"}}},
		{ID: "sliced", Name: "Onion", Note: "sliced", Revision: 1, Fields: map[string]uint64{}, Sources: []ShoppingSource{{ID: "n/d/onion", MealID: "n", Name: "Onion", Note: "sliced", Amount: "2"}}},
		{ID: "milk", Name: "Milk", Revision: 1, Fields: map[string]uint64{}},
	}}
	apply := func(change ShoppingChange) {
		t.Helper()

		if err := Apply(&list, change); err != nil {
			t.Fatal(err)
		}

		list.Revision++
	}

	apply(ShoppingChange{ID: "merge", Kind: "merge", ItemID: "diced", BaseRevision: 1, MergeItemIDs: []string{"sliced"}})
	RefreshAmounts(&list)

	if list.Items[0].Needed != "3" || !list.Items[1].Deleted || list.Items[1].MergedInto != "diced" || len(list.Items[0].Sources) != 2 {
		t.Fatalf("merge = %#v", list.Items)
	}

	if list.Items[0].Sources[1].Note != "sliced" {
		t.Fatal("a merged row must still say how each recipe wanted it")
	}

	if err := Apply(&list, ShoppingChange{ID: "restore", Kind: "restore", ItemID: "sliced", BaseRevision: list.Items[1].Revision}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("restoring a merged row = %v", err)
	}

	desired := map[string]shoppingNeed{"n/d/onion": {source: ShoppingSource{ID: "n/d/onion", MealID: "n", Name: "Onion", Note: "sliced", Amount: "4"}, choice: ShoppingChoice{Name: "Onion", Note: "sliced"}}}
	refreshOpenItems(&list, ShoppingReviewInput{MealIDs: []string{"n"}}, desired)
	list.Revision++
	RefreshAmounts(&list)

	if list.Items[0].Needed != "5" || len(desired) != 0 {
		t.Fatalf("refresh undid the merge: %#v %#v", list.Items[0], desired)
	}

	apply(ShoppingChange{ID: "split", Kind: "split", ItemID: "diced", BaseRevision: list.Revision})
	RefreshAmounts(&list)

	if len(list.Items[0].Sources) != 1 || list.Items[1].Sources[0].ID != "n/d/onion" || list.Items[1].Note != "sliced" || !list.Items[1].Arranged {
		t.Fatalf("split = %#v", list.Items[:2])
	}

	if mergeNeed(&list, shoppingNeed{source: ShoppingSource{ID: "o/d/onion", Amount: "1"}, choice: ShoppingChoice{Name: "Onion", Note: "diced"}}) {
		t.Fatal("a split row took a new need")
	}

	first := ""
	beforeMove := list.Revision
	apply(ShoppingChange{ID: "move-first", Kind: "move", ItemID: "milk", BaseRevision: list.Revision, AfterItemID: &first})

	if list.Items[0].ID != "milk" {
		t.Fatalf("move = %v", list.Items[0].ID)
	}

	last := list.Items[len(list.Items)-1].ID
	if err := Apply(&list, ShoppingChange{ID: "stale-move", Kind: "move", ItemID: "milk", BaseRevision: beforeMove, AfterItemID: &last}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale move = %v", err)
	}

	if err := Apply(&list, ShoppingChange{ID: "stale-merge", Kind: "merge", ItemID: "milk", BaseRevision: 1, MergeItemIDs: []string{"diced"}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale merge = %v", err)
	}
}

func TestShoppingViewersMayOnlyTickItemsOff(t *testing.T) {
	bought, name := true, "Oat milk"

	if (ShoppingChange{Kind: "edit", Bought: &bought}).RequiredRole() != access.Viewer {
		t.Fatal("ticking an item off should be open to viewers")
	}

	for _, change := range []ShoppingChange{
		{Kind: "edit", Bought: &bought, Name: &name},
		{Kind: "add", Name: &name},
		{Kind: "remove"},
		{Kind: "move"},
		{Kind: "merge"},
	} {
		if change.RequiredRole() != access.Editor {
			t.Errorf("%s needs an editor", change.Kind)
		}
	}
}

func TestShoppingAddAgainKeepsTheEarlierContribution(t *testing.T) {
	earlier := ShoppingSource{ID: "m/d/flour", MealID: "m", Amount: "1"}
	again := ShoppingSource{ID: "x/m/d/flour", MealID: "m", Again: "x", Amount: "1"}

	if !affectedSource(ShoppingReviewInput{MealIDs: []string{"m"}}, earlier) || affectedSource(ShoppingReviewInput{MealIDs: []string{"m"}}, again) {
		t.Fatal("a plain review must update only the original contribution")
	}

	if affectedSource(ShoppingReviewInput{MealIDs: []string{"m"}, Again: "x"}, earlier) || !affectedSource(ShoppingReviewInput{MealIDs: []string{"m"}, Again: "x"}, again) {
		t.Fatal("adding again must leave the original contribution alone")
	}
}
