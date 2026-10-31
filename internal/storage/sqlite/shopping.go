package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/savorum/savorum/internal/event"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/idempotency"
	"github.com/savorum/savorum/internal/shopping"
)

// ListShoppingLists reads all lists within the selected household.
func (s *Store) ListShoppingLists(ctx context.Context) ([]shopping.ShoppingList, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, err
	}

	return readShoppingLists(ctx, s.db, scopeID(ctx))
}

// GetShoppingList returns an empty Groceries list before its first edit.
func (s *Store) GetShoppingList(ctx context.Context, id string) (shopping.ShoppingList, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return shopping.ShoppingList{}, err
	}

	return readShoppingList(ctx, s.db, scopeID(ctx), id)
}

// EditShoppingList commits an edit, its retry receipt and its activity atomically.
func (s *Store) EditShoppingList(ctx context.Context, id string, edit shopping.Edit) (shopping.ShoppingList, error) {
	operationID, digest, mutate := shoppingOperationID(ctx, edit.OperationID), edit.Digest, edit.Mutate
	if edit.Role == "" {
		edit.Role = access.Editor
	}

	if err := s.authorizeScope(ctx, edit.Role); err != nil {
		return shopping.ShoppingList{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return shopping.ShoppingList{}, fmt.Errorf("begin shopping edit: %w", err)
	}
	defer tx.Rollback()

	if err := s.authorizeScopeWith(ctx, tx, edit.Role); err != nil {
		return shopping.ShoppingList{}, err
	}

	if err := recordCreation(ctx, tx, id); err != nil {
		return shopping.ShoppingList{}, err
	}

	libraryID := scopeID(ctx)

	list, err := readShoppingList(ctx, tx, libraryID, id)
	if err != nil {
		return list, err
	}

	var (
		previous        string
		appliedRevision uint64
	)

	err = tx.QueryRowContext(ctx, `SELECT digest, result_revision FROM shopping_operations WHERE library_id=? AND list_id=? AND operation_id=?`, libraryID, id, operationID).Scan(&previous, &appliedRevision)
	if err == nil {
		if previous != digest {
			return list, errors.Join(shopping.ErrConflict, idempotency.ErrConflict)
		}

		list.AppliedRevision = appliedRevision

		return list, operationError("commit shopping", tx.Commit())
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return list, fmt.Errorf("read shopping receipt: %w", err)
	}

	if err := checkReviewedMeals(ctx, tx, libraryID, edit.MealRevisions); err != nil {
		return list, err
	}

	before := map[string]uint64{}
	for _, item := range list.Items {
		before[item.ID] = item.Revision
	}

	if err := mutate(&list); err != nil {
		return list, err
	}

	list.Revision++
	shopping.RefreshAmounts(&list)
	ensureShoppingMaps(&list)

	data, err := json.Marshal(list)
	if err != nil {
		return list, fmt.Errorf("encode shopping list: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO shopping_lists(library_id,id,state_json) VALUES(?,?,?) ON CONFLICT(library_id,id) DO UPDATE SET state_json=excluded.state_json`, libraryID, id, string(data)); err != nil {
		return list, fmt.Errorf("write shopping list: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO shopping_operations(library_id,list_id,operation_id,digest,result_revision) VALUES(?,?,?,?,?)`, libraryID, id, operationID, digest, list.Revision); err != nil {
		return list, fmt.Errorf("write shopping receipt: %w", err)
	}

	if err := recordShoppingActivity(ctx, tx, libraryID, id, edit.Activity, before, list); err != nil {
		return list, err
	}

	if err := pinMealDishes(ctx, tx, libraryID, edit.MealPins); err != nil {
		return list, err
	}

	kind, resourceType, resourceID := "shopping.list.updated", "shopping_list", id
	if edit.Activity == shopping.ActivityBought || edit.Activity == shopping.ActivityReopened {
		kind, resourceType = "shopping.item.checked", "shopping_item"
		if edit.Activity == shopping.ActivityReopened {
			kind = "shopping.item.reopened"
		}

		for _, item := range list.Items {
			if before[item.ID] != item.Revision {
				resourceID = item.ID
				break
			}
		}
	}

	if err := appendDomainEvent(ctx, tx, kind, libraryID, resourceType, resourceID, event.Payload{Revision: list.Revision, ListID: id}); err != nil {
		return list, err
	}

	if err := tx.Commit(); err != nil {
		return list, fmt.Errorf("commit shopping edit: %w", err)
	}

	list.AppliedRevision = list.Revision

	return list, nil
}

func readShoppingLists(ctx context.Context, db dbQueryer, libraryID string) ([]shopping.ShoppingList, error) {
	rows, err := db.QueryContext(ctx, `SELECT state_json FROM shopping_lists WHERE library_id=? ORDER BY CASE WHEN id='groceries' THEN 0 ELSE 1 END,json_extract(state_json,'$.name'),id`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("list grocery lists: %w", err)
	}
	defer rows.Close()

	result := []shopping.ShoppingList{}
	defaultFound := false

	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("read grocery list: %w", err)
		}

		var list shopping.ShoppingList
		if err := json.Unmarshal([]byte(data), &list); err != nil {
			return nil, fmt.Errorf("decode grocery list: %w", err)
		}

		if list.ID == "groceries" {
			defaultFound = true
		}

		shopping.RefreshAmounts(&list)
		ensureShoppingMaps(&list)
		result = append(result, list)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate grocery lists: %w", err)
	}

	if !defaultFound {
		result = append([]shopping.ShoppingList{{ID: "groceries", Name: "Groceries", Items: []shopping.ShoppingItem{}, Selections: map[string]shopping.ShoppingSelection{}, Requirements: map[string]shopping.ShoppingRequirement{}}}, result...)
	}

	return result, nil
}

func readShoppingList(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, libraryID, id string,
) (shopping.ShoppingList, error) {
	list := shopping.ShoppingList{ID: id, Name: "Groceries", Items: []shopping.ShoppingItem{}, Selections: map[string]shopping.ShoppingSelection{}, Requirements: map[string]shopping.ShoppingRequirement{}}

	var data string

	err := q.QueryRowContext(ctx, `SELECT state_json FROM shopping_lists WHERE library_id=? AND id=?`, libraryID, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return list, nil
	}

	if err != nil {
		return list, fmt.Errorf("read shopping list: %w", err)
	}

	if err := json.Unmarshal([]byte(data), &list); err != nil {
		return list, fmt.Errorf("decode shopping list: %w", err)
	}

	shopping.RefreshAmounts(&list)
	ensureShoppingMaps(&list)

	return list, nil
}

func ensureShoppingMaps(list *shopping.ShoppingList) {
	if list.Selections == nil {
		list.Selections = map[string]shopping.ShoppingSelection{}
	}

	if list.Requirements == nil {
		list.Requirements = map[string]shopping.ShoppingRequirement{}
	}

	for i := range list.Items {
		if list.Items[i].Fields == nil {
			list.Items[i].Fields = map[string]uint64{}
		}
	}
}

const maximumActivityItems = 50

func recordShoppingActivity(ctx context.Context, tx *sql.Tx, libraryID, listID, kind string, before map[string]uint64, list shopping.ShoppingList) error {
	if kind == "" {
		return nil
	}

	items := []shopping.ShoppingActivityItem{}

	for _, item := range list.Items {
		if previous, ok := before[item.ID]; ok && previous == item.Revision {
			continue
		}

		if len(items) < maximumActivityItems {
			items = append(items, shopping.ShoppingActivityItem{ID: item.ID, Name: item.Name})
		}
	}

	data, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("encode shopping activity: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO shopping_activity(library_id,list_id,created_at,actor_id,kind,items_json) VALUES(?,?,?,?,?,?)`,
		libraryID, listID, formatTime(time.Now()), attributionActor(ctx), kind, string(data)); err != nil {
		return fmt.Errorf("write shopping activity: %w", err)
	}

	return nil
}

// ShoppingActivity lists a list's changes, newest first.
func (s *Store) ShoppingActivity(ctx context.Context, listID string, before int64, limit int) ([]shopping.ShoppingActivity, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id,created_at,actor_id,`+actorDisplaySQL+`,kind,items_json FROM shopping_activity
		WHERE library_id=? AND list_id=? AND (?=0 OR id<?) ORDER BY id DESC LIMIT ?`, scopeID(ctx), listID, before, before, limit)
	if err != nil {
		return nil, fmt.Errorf("query shopping activity: %w", err)
	}
	defer rows.Close()

	result := []shopping.ShoppingActivity{}

	for rows.Next() {
		var (
			entry   shopping.ShoppingActivity
			created string
			items   string
		)

		if err := rows.Scan(&entry.ID, &created, &entry.ActorID, &entry.ActorName, &entry.Kind, &items); err != nil {
			return nil, fmt.Errorf("scan shopping activity: %w", err)
		}

		entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		if err := json.Unmarshal([]byte(items), &entry.Items); err != nil {
			return nil, fmt.Errorf("decode shopping activity: %w", err)
		}

		result = append(result, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate shopping activity: %w", err)
	}

	return result, nil
}

// DeleteShoppingList removes a list with its receipts and activity.
func (s *Store) DeleteShoppingList(ctx context.Context, id string) error {
	if err := s.authorizeScope(ctx, access.Editor); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin shopping list deletion: %w", err)
	}
	defer tx.Rollback()

	if err := s.authorizeScopeWith(ctx, tx, access.Editor); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM shopping_lists WHERE library_id=? AND id=?`, scopeID(ctx), id)
	if err != nil {
		return fmt.Errorf("delete shopping list: %w", err)
	}

	if removed, _ := result.RowsAffected(); removed == 0 {
		return shopping.ErrNotFound
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM shopping_activity WHERE library_id=? AND list_id=?`, scopeID(ctx), id); err != nil {
		return fmt.Errorf("delete shopping activity: %w", err)
	}

	if err := appendDomainEvent(ctx, tx, "shopping.list.deleted", scopeID(ctx), "shopping_list", id, event.Payload{ListID: id}); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit shopping list deletion: %w", err)
	}

	return nil
}
