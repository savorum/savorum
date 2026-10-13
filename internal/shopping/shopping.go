// Package shopping maintains shared lists through retryable, field-level edits.
package shopping

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/access"
)

// Conflict, validation and removal errors distinguish recoverable edits.
var (
	// ErrNotFound reports an unknown named list.
	ErrNotFound = errors.New("shopping list not found")
	ErrConflict = errors.New("this field changed on another device")
	ErrInvalid  = errors.New("invalid shopping change")
	ErrDeleted  = errors.New("this item was removed")
	// ErrUnknownRecipe reports a review source naming a recipe or revision
	// this library does not hold.
	ErrUnknownRecipe = errors.New("a review names a recipe or revision this library does not hold")
)

// ShoppingList retains removed rows so an offline edit cannot resurrect them.
type ShoppingList struct {
	ID              string                         `json:"id"`
	Name            string                         `json:"name"`
	Revision        uint64                         `json:"revision"`
	AppliedRevision uint64                         `json:"applied_revision,omitempty"`
	Items           []ShoppingItem                 `json:"items"`
	Selections      map[string]ShoppingSelection   `json:"selections"`
	Requirements    map[string]ShoppingRequirement `json:"requirements"`
}

// ShoppingItem separates the requested purchase from its recipe requirements.
type ShoppingItem struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Buy      string            `json:"buy"`
	Needed   string            `json:"needed"`
	Note     string            `json:"note"`
	Category string            `json:"category"`
	Bought   bool              `json:"bought"`
	Deleted  bool              `json:"deleted"`
	Revision uint64            `json:"revision"`
	Fields   map[string]uint64 `json:"fields"`
	Sources  []ShoppingSource  `json:"sources"`
	// Arranged rows were merged or split by hand; reviews leave them be.
	Arranged   bool   `json:"arranged,omitempty"`
	MergedInto string `json:"merged_into,omitempty"`
}

// ShoppingSource records the exact recipe occurrence behind a requirement.
type ShoppingSource struct {
	ID             string `json:"id"`
	MealID         string `json:"meal_id,omitempty"`
	Again          string `json:"again,omitempty"`
	DishID         string `json:"dish_id,omitempty"`
	RecipeID       string `json:"recipe_id,omitempty"`
	RecipeRevision uint64 `json:"recipe_revision,omitempty"`
	IngredientID   string `json:"ingredient_id,omitempty"`
	Label          string `json:"label"`
	MealDate       string `json:"meal_date,omitempty" format:"date"`
	Name           string `json:"name"`
	Note           string `json:"note,omitempty"`
	Quantity       string `json:"quantity"`
	Amount         string `json:"amount,omitempty"`
	Unit           string `json:"unit,omitempty"`
}

// ShoppingChange applies only explicitly supplied fields against the last observed row.
type ShoppingChange struct {
	ID           string  `json:"id" minLength:"1" maxLength:"100"`
	Kind         string  `json:"kind" enum:"add,edit,remove,restore,move,merge,split"`
	ItemID       string  `json:"item_id" minLength:"1" maxLength:"100"`
	BaseRevision uint64  `json:"base_revision"`
	Name         *string `json:"name,omitempty" maxLength:"200"`
	Buy          *string `json:"buy,omitempty" maxLength:"200"`
	Note         *string `json:"note,omitempty" maxLength:"2000"`
	Category     *string `json:"category,omitempty" maxLength:"100"`
	Bought       *bool   `json:"bought,omitempty"`
	// AfterItemID empty moves the row first.
	AfterItemID  *string  `json:"after_item_id,omitempty" maxLength:"100"`
	MergeItemIDs []string `json:"merge_item_ids,omitempty" maxItems:"100"`
	// SourceIDs empty splits every source after the first.
	SourceIDs []string `json:"source_ids,omitempty" maxItems:"200"`
}

// RequiredRole lets viewers tick items off but nothing else.
func (c ShoppingChange) RequiredRole() access.Role {
	if c.Kind == "edit" && c.Bought != nil && c.Name == nil && c.Buy == nil && c.Note == nil && c.Category == nil {
		return access.Viewer
	}

	return access.Editor
}

// Activity kinds recorded against a list.
const (
	ActivityAdd        = "add"
	ActivityEdit       = "edit"
	ActivityBought     = "bought"
	ActivityReopened   = "reopened"
	ActivityRemove     = "remove"
	ActivityRestore    = "restore"
	ActivityMove       = "move"
	ActivityMerge      = "merge"
	ActivitySplit      = "split"
	ActivityReview     = "review"
	ActivityCreateList = "create-list"
	ActivityRenameList = "rename-list"
)

// ActivityKind names a change for the list's history.
func (c ShoppingChange) ActivityKind() string {
	if c.Kind == "edit" && c.RequiredRole() == access.Viewer {
		if *c.Bought {
			return ActivityBought
		}

		return ActivityReopened
	}

	return c.Kind
}

// Edit is one retryable, attributed mutation.
type Edit struct {
	OperationID string
	Digest      string
	Role        access.Role
	Activity    string
	Mutate      func(*ShoppingList) error
	// MealPins are written to the planned meals in the same transaction as
	// the list, so a meal cooks the recipe versions it was shopped for.
	MealPins []MealPin
}

// MealPin fixes a planned dish to the recipe revision a review read.
type MealPin struct {
	MealID         string
	DishID         string
	RecipeID       string
	RecipeRevision uint64
}

// ShoppingActivityItem is a row's name when the activity happened.
type ShoppingActivityItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ShoppingActivity is one attributed change to a list.
type ShoppingActivity struct {
	ID        int64                  `json:"id"`
	CreatedAt time.Time              `json:"created_at"`
	ActorID   string                 `json:"actor_id"`
	ActorName string                 `json:"actor_name"`
	Kind      string                 `json:"kind" enum:"add,edit,bought,reopened,remove,restore,move,merge,split,review,create-list,rename-list"`
	Items     []ShoppingActivityItem `json:"items"`
}

// Persistence serializes mutation and operation receipts in one transaction.
type Persistence interface {
	ListShoppingLists(context.Context) ([]ShoppingList, error)
	GetShoppingList(context.Context, string) (ShoppingList, error)
	EditShoppingList(context.Context, string, Edit) (ShoppingList, error)
	DeleteShoppingList(context.Context, string) error
	ShoppingActivity(ctx context.Context, listID string, before int64, limit int) ([]ShoppingActivity, error)
}

// Apply merges disjoint edits and refuses stale edits to removed rows.
func Apply(list *ShoppingList, change ShoppingChange) error {
	if change.ID == "" || change.ItemID == "" || len(change.ID) > 100 || len(change.ItemID) > 100 {
		return ErrInvalid
	}

	index := -1

	for i := range list.Items {
		if list.Items[i].ID == change.ItemID {
			index = i
			break
		}
	}

	switch change.Kind {
	case "move":
		if index < 0 {
			return ErrDeleted
		}

		return move(list, index, change)
	case "merge":
		if index < 0 {
			return ErrDeleted
		}

		return merge(list, index, change)
	case "split":
		if index < 0 {
			return ErrDeleted
		}

		return split(list, index, change)
	}

	if change.Kind == "add" {
		if index >= 0 {
			return ErrConflict
		}

		if change.Name == nil || strings.TrimSpace(*change.Name) == "" {
			return ErrInvalid
		}

		list.Items = append(list.Items, ShoppingItem{ID: change.ItemID, Fields: map[string]uint64{}, Sources: []ShoppingSource{}})
		index = len(list.Items) - 1
	} else if index < 0 {
		return ErrDeleted
	}

	item := &list.Items[index]
	if item.Fields == nil {
		item.Fields = map[string]uint64{}
	}

	if change.Kind == "restore" || change.Kind == "remove" {
		if item.Revision != change.BaseRevision {
			return ErrConflict
		}

		// Its sources now live in the row it was merged into.
		if change.Kind == "restore" && item.MergedInto != "" {
			return ErrInvalid
		}

		item.Deleted = change.Kind == "remove"
		item.Revision = list.Revision + 1

		return nil
	}

	if change.Kind != "add" && change.Kind != "edit" {
		return ErrInvalid
	}

	if item.Deleted {
		return ErrDeleted
	}

	return applyFields(item, change, list.Revision+1)
}

func applyFields(item *ShoppingItem, change ShoppingChange, revision uint64) error {
	fields := []struct {
		key    string
		value  *string
		target *string
		limit  int
	}{
		{"name", change.Name, &item.Name, 200},
		{"buy", change.Buy, &item.Buy, 200},
		{"note", change.Note, &item.Note, 2000},
		{"category", change.Category, &item.Category, 100},
	}
	for _, field := range fields {
		if field.value == nil {
			continue
		}

		if len(*field.value) > field.limit || (field.key == "name" && strings.TrimSpace(*field.value) == "") {
			return ErrInvalid
		}

		if item.Fields[field.key] > change.BaseRevision {
			return ErrConflict
		}
	}

	if change.Bought != nil && item.Fields["bought"] > change.BaseRevision {
		return ErrConflict
	}

	for _, field := range fields {
		if field.value != nil {
			*field.target = strings.TrimSpace(*field.value)
			item.Fields[field.key] = revision
		}
	}

	if change.Bought != nil {
		item.Bought = *change.Bought
		item.Fields["bought"] = revision
	}

	item.Revision = revision

	return nil
}

func move(list *ShoppingList, index int, change ShoppingChange) error {
	item := list.Items[index]
	if item.Deleted {
		return ErrDeleted
	}

	if item.Revision > change.BaseRevision {
		return ErrConflict
	}

	rest := slices.Delete(slices.Clone(list.Items), index, index+1)
	position := 0

	if change.AfterItemID != nil && *change.AfterItemID != "" {
		anchor := slices.IndexFunc(rest, func(row ShoppingItem) bool { return row.ID == *change.AfterItemID })
		if anchor < 0 {
			return ErrInvalid
		}

		position = anchor + 1
	}

	item.Revision = list.Revision + 1
	list.Items = slices.Insert(rest, position, item)

	return nil
}

func merge(list *ShoppingList, index int, change ShoppingChange) error {
	target := &list.Items[index]
	if target.Deleted || len(change.MergeItemIDs) == 0 || target.Revision > change.BaseRevision {
		return conflictOrInvalid(target.Deleted, len(change.MergeItemIDs) == 0)
	}

	revision := list.Revision + 1
	seen := map[string]bool{target.ID: true}

	for _, id := range change.MergeItemIDs {
		if seen[id] {
			return ErrInvalid
		}

		seen[id] = true
		other := slices.IndexFunc(list.Items, func(row ShoppingItem) bool { return row.ID == id })

		if other < 0 || list.Items[other].Deleted {
			return ErrDeleted
		}

		if list.Items[other].Revision > change.BaseRevision {
			return ErrConflict
		}
	}

	for _, id := range change.MergeItemIDs {
		other := &list.Items[slices.IndexFunc(list.Items, func(row ShoppingItem) bool { return row.ID == id })]
		list.Items[index].Sources = append(list.Items[index].Sources, other.Sources...)
		other.Sources = []ShoppingSource{}
		other.Deleted = true
		other.MergedInto = list.Items[index].ID
		other.Revision = revision
	}

	list.Items[index].Arranged = true
	list.Items[index].Revision = revision

	return nil
}

func split(list *ShoppingList, index int, change ShoppingChange) error {
	item := list.Items[index]
	if item.Deleted || len(item.Sources) < 2 {
		return conflictOrInvalid(item.Deleted, true)
	}

	if item.Revision > change.BaseRevision {
		return ErrConflict
	}

	moving := change.SourceIDs
	if len(moving) == 0 {
		for _, source := range item.Sources[1:] {
			moving = append(moving, source.ID)
		}
	}

	kept := []ShoppingSource{}
	moved := []ShoppingSource{}

	for _, source := range item.Sources {
		if slices.Contains(moving, source.ID) {
			moved = append(moved, source)
		} else {
			kept = append(kept, source)
		}
	}

	if len(moved) != len(slices.Compact(slices.Sorted(slices.Values(moving)))) || len(kept) == 0 {
		return ErrInvalid
	}

	revision := list.Revision + 1
	rows := make([]ShoppingItem, 0, len(moved))

	for _, source := range moved {
		digest := sha256.Sum256([]byte(change.ID + "/" + source.ID))
		name := source.Name

		if name == "" {
			name = item.Name
		}

		rows = append(rows, ShoppingItem{
			ID: hex.EncodeToString(digest[:16]), Name: name, Note: source.Note, Category: item.Category,
			Revision: revision, Fields: map[string]uint64{}, Sources: []ShoppingSource{source}, Arranged: true,
		})
	}

	list.Items[index].Sources = kept
	list.Items[index].Arranged = true
	list.Items[index].Revision = revision
	list.Items = slices.Insert(list.Items, index+1, rows...)

	return nil
}

func conflictOrInvalid(deleted, invalid bool) error {
	switch {
	case deleted:
		return ErrDeleted
	case invalid:
		return ErrInvalid
	default:
		return ErrConflict
	}
}
