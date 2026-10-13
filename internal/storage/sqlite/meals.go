package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/event"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/meal"
	"github.com/savorum/savorum/internal/recipe"
)

var _ meal.Persistence = (*Store)(nil)

var mealColumns = `id, library_id, name, date, meal_type, notes, revision, created_at, updated_at, skipped,
	created_by, ` + actorDisplayFor("meals.created_by") + `, updated_by, ` + actorDisplayFor("meals.updated_by")

// A purge clears entry_id, so recipe_dish tells a purged recipe from plain text.
const dishRecipeStatusSQL = `CASE
	WHEN entry_id IS NULL THEN CASE WHEN recipe_dish = 1 THEN 'removed' ELSE '' END
	WHEN (SELECT deleted_at FROM library_recipes WHERE library_recipes.id = entry_id) IS NOT NULL THEN 'trashed'
	ELSE 'available' END`

const dishRecipeImageSQL = `COALESCE((SELECT assets.id FROM library_recipes JOIN assets ON assets.id = library_recipes.primary_asset_id
	WHERE library_recipes.id = entry_id AND library_recipes.deleted_at IS NULL AND assets.storage_key IS NOT NULL), '')`

// CreateMeal persists a new meal and its dishes in one transaction.
func (s *Store) CreateMeal(ctx context.Context, _ string, m meal.Meal) error {
	if err := s.authorizeScope(ctx, access.Editor); err != nil {
		return operationError("CreateMeal", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create meal: %w", err)
	}
	defer tx.Rollback()

	if err := s.authorizeScopeWith(ctx, tx, access.Editor); err != nil {
		return err
	}

	if err := recordCreation(ctx, tx, m.ID); err != nil {
		return err
	}

	var dateValue any
	if m.Date != nil {
		dateValue = m.Date.String()
	}

	libraryID := scopeID(ctx)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO meals (id, library_id, name, date, meal_type, notes, revision, created_at, updated_at, skipped, created_by, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, libraryID, m.Name, dateValue, string(m.MealType), m.Notes, m.Revision,
		formatTime(m.CreatedAt), formatTime(m.UpdatedAt), m.Skipped, attributionActor(ctx), attributionActor(ctx),
	); err != nil {
		return fmt.Errorf("insert meal: %w", err)
	}

	if err := insertDishes(ctx, tx, m.Dishes); err != nil {
		return operationError("CreateMeal", err)
	}

	if err := appendDomainEvent(ctx, tx, "meal_plan.entry.created", scopeID(ctx), "meal", m.ID, event.Payload{Revision: m.Revision}); err != nil {
		return operationError("CreateMeal", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create meal: %w", err)
	}

	return nil
}

// GetMeal retrieves a meal with its dishes.
func (s *Store) GetMeal(ctx context.Context, libraryID, id string) (meal.Meal, error) {
	if err := s.scopedObject(ctx, "meals", id, access.Viewer); err != nil {
		return meal.Meal{}, operationError("GetMeal", err)
	}

	m, err := scanMealRow(s.db.QueryRowContext(ctx, `
		SELECT `+mealColumns+`
		FROM meals WHERE id = ? AND library_id = ?`, id, scopeID(ctx)))
	if err != nil {
		return meal.Meal{}, fmt.Errorf("get meal: %w", err)
	}

	dishes, err := queryDishes(ctx, s.db, m.ID)
	if err != nil {
		return meal.Meal{}, operationError("GetMeal", err)
	}

	m.Dishes = dishes
	if err := s.annotateMealShopping(ctx, &m); err != nil {
		return meal.Meal{}, operationError("GetMeal", err)
	}

	return m, nil
}

// ListMeals returns meals matching the given options.
func (s *Store) ListMeals(ctx context.Context, libraryID string, options meal.ListOptions) ([]meal.Meal, int, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, 0, operationError("ListMeals", err)
	}

	scope := scopeID(ctx)

	var (
		conditions []string
		args       []any
	)

	conditions = append(conditions, "library_id = ?")
	args = append(args, scope)

	var dateClauses []string
	if options.DateFrom != nil {
		dateClauses = append(dateClauses, "date >= ?")
		args = append(args, options.DateFrom.String())
	}

	if options.DateTo != nil {
		dateClauses = append(dateClauses, "date <= ?")
		args = append(args, options.DateTo.String())
	}

	if len(dateClauses) > 0 {
		dateFilter := strings.Join(dateClauses, " AND ")
		if options.Unscheduled {
			dateFilter = "(date IS NULL OR (" + dateFilter + "))"
		}

		conditions = append(conditions, dateFilter)
	} else if !options.Unscheduled {
		conditions = append(conditions, "date IS NOT NULL")
	}

	where := "WHERE " + strings.Join(conditions, " AND ")

	// The query fragments are constants above; all caller values remain bound parameters.
	countQuery := "SELECT COUNT(*) FROM meals " + where

	var total int

	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count meals: %w", err)
	}

	query := "SELECT " + mealColumns + " FROM meals " + where + " ORDER BY COALESCE(date, '9999-12-31'), meal_type, created_at LIMIT ? OFFSET ?"

	queryArgs := make([]any, 0, len(args)+2)
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, options.Limit, options.Offset)

	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list meals: %w", err)
	}
	defer rows.Close()

	var (
		meals   []meal.Meal
		mealIDs []string
	)

	for rows.Next() {
		m, err := scanMealRows(rows)
		if err != nil {
			return nil, 0, operationError("ListMeals", err)
		}

		meals = append(meals, m)
		mealIDs = append(mealIDs, m.ID)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate meals: %w", err)
	}

	if len(mealIDs) > 0 {
		dishMap, err := queryDishesForMeals(ctx, s.db, mealIDs)
		if err != nil {
			return nil, 0, operationError("ListMeals", err)
		}

		for i := range meals {
			meals[i].Dishes = dishMap[meals[i].ID]
			if meals[i].Dishes == nil {
				meals[i].Dishes = []meal.Dish{}
			}
		}

		shoppingLists, err := readShoppingLists(ctx, s.db, scopeID(ctx))
		if err != nil {
			return nil, 0, operationError("ListMeals", err)
		}

		for i := range meals {
			applyMealShopping(&meals[i], shoppingLists)
		}
	}

	if meals == nil {
		meals = []meal.Meal{}
	}

	return meals, total, nil
}

// UpdateMeal atomically replaces a meal aggregate with optimistic concurrency.
func (s *Store) UpdateMeal(ctx context.Context, libraryID string, m meal.Meal, expectedRevision uint64) error {
	if err := s.scopedObject(ctx, "meals", m.ID, access.Editor); err != nil {
		return operationError("UpdateMeal", err)
	}

	var dateValue any
	if m.Date != nil {
		dateValue = m.Date.String()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update meal: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE meals SET name = ?, date = ?, meal_type = ?, notes = ?, revision = revision + 1, updated_at = ?, skipped = ?, updated_by = ?
		WHERE id = ? AND library_id = ? AND revision = ?`,
		m.Name, dateValue, string(m.MealType), m.Notes, formatTime(m.UpdatedAt), m.Skipped, attributionActor(ctx),
		m.ID, scopeID(ctx), expectedRevision,
	)
	if err != nil {
		return fmt.Errorf("update meal: %w", err)
	}

	n, _ := result.RowsAffected()
	if n == 0 {
		var current uint64
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM meals WHERE id = ? AND library_id = ?", m.ID, scopeID(ctx)).Scan(&current); err != nil {
			return meal.ErrNotFound
		}

		return &meal.PreconditionError{MealID: m.ID, Expected: expectedRevision, Current: current}
	}

	if err := replaceDishes(ctx, tx, "meal_dishes", "meal_id", m.ID, func() error { return insertDishes(ctx, tx, m.Dishes) }); err != nil {
		return operationError("UpdateMeal", err)
	}

	if err := appendDomainEvent(ctx, tx, "meal_plan.entry.updated", scopeID(ctx), "meal", m.ID, event.Payload{Revision: expectedRevision + 1}); err != nil {
		return operationError("UpdateMeal", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit update meal: %w", err)
	}

	return nil
}

// DeleteMeal removes a meal and its dishes (cascaded by FK).
func (s *Store) DeleteMeal(ctx context.Context, libraryID, id string, expectedRevision uint64) error {
	if err := s.scopedObject(ctx, "meals", id, access.Editor); err != nil {
		return operationError("DeleteMeal", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operationError("DeleteMeal", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, "DELETE FROM meals WHERE id = ? AND library_id = ? AND revision = ?", id, scopeID(ctx), expectedRevision)
	if err != nil {
		return fmt.Errorf("delete meal: %w", err)
	}

	n, _ := result.RowsAffected()
	if n == 0 {
		var current uint64
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM meals WHERE id = ? AND library_id = ?", id, scopeID(ctx)).Scan(&current); err != nil {
			return meal.ErrNotFound
		}

		return &meal.PreconditionError{MealID: id, Expected: expectedRevision, Current: current}
	}

	if err := appendDomainEvent(ctx, tx, "meal_plan.entry.deleted", scopeID(ctx), "meal", id, event.Payload{}); err != nil {
		return operationError("DeleteMeal", err)
	}

	return operationError("DeleteMeal", tx.Commit())
}

// AddDish inserts a dish into an existing meal.
func (s *Store) AddDish(ctx context.Context, libraryID, mealID string, d meal.Dish) error {
	if err := s.scopedObject(ctx, "meals", mealID, access.Editor); err != nil {
		return operationError("AddDish", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin add dish: %w", err)
	}
	defer tx.Rollback()

	if d.Position == 0 {
		var maxPos sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT MAX(position) FROM meal_dishes WHERE meal_id = ?", mealID).Scan(&maxPos); err != nil {
			return fmt.Errorf("max dish position: %w", err)
		}

		if maxPos.Valid {
			d.Position = int(maxPos.Int64) + 1
		}
	}

	if err := insertDish(ctx, tx, d); err != nil {
		return operationError("AddDish", err)
	}

	if _, err := tx.ExecContext(ctx, "UPDATE meals SET revision = revision + 1, updated_at = ?, updated_by = ? WHERE id = ?",
		formatTime(time.Now()), attributionActor(ctx), mealID); err != nil {
		return fmt.Errorf("bump meal revision: %w", err)
	}

	if err := appendDomainEvent(ctx, tx, "meal_plan.entry.updated", scopeID(ctx), "meal", mealID, event.Payload{Revision: 0}); err != nil {
		return operationError("AddDish", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit add dish: %w", err)
	}

	return nil
}

// UpdateDish replaces a dish's metadata.
func (s *Store) UpdateDish(ctx context.Context, libraryID, mealID string, d meal.Dish) error {
	if err := s.scopedObject(ctx, "meals", mealID, access.Editor); err != nil {
		return operationError("UpdateDish", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update dish: %w", err)
	}
	defer tx.Rollback()

	var scaleValue any
	if d.Scale != nil {
		scaleValue = d.Scale.String()
	}

	var entryValue any
	if d.EntryID != nil {
		entryValue = string(*d.EntryID)
	}

	var revisionValue any
	if d.RecipeRevision != nil {
		revisionValue = *d.RecipeRevision
	}

	if err := checkDishReferences(ctx, tx, "", "", "", d.EntryID, d.RecipeRevision, nil); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE meal_dishes SET entry_id = ?, recipe_revision = ?, display_name = ?, scale = ?, position = ?, notes = ?, leftover_of = ?, recipe_dish = ?
		WHERE id = ? AND meal_id = ?`,
		entryValue, revisionValue, d.DisplayName, scaleValue, d.Position, d.Notes, d.LeftoverOf, d.EntryID != nil,
		d.ID, mealID,
	)
	if err != nil {
		return fmt.Errorf("update dish: %w", err)
	}

	n, _ := result.RowsAffected()
	if n == 0 {
		return meal.ErrNotFound
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM meal_dish_plain_items WHERE dish_id = ?", d.ID); err != nil {
		return fmt.Errorf("clear plain items: %w", err)
	}

	if err := checkDishReferences(ctx, tx, "", "meal_dish_plain_items", "", nil, nil, d.PlainItems); err != nil {
		return err
	}

	for _, pi := range d.PlainItems {
		if _, err := tx.ExecContext(ctx, `INSERT INTO meal_dish_plain_items (id, dish_id, name, quantity) VALUES (?, ?, ?, ?)`,
			pi.ID, d.ID, pi.Name, pi.Quantity); err != nil {
			return fmt.Errorf("insert plain item: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, "UPDATE meals SET revision = revision + 1, updated_at = ?, updated_by = ? WHERE id = ?",
		formatTime(time.Now()), attributionActor(ctx), mealID); err != nil {
		return fmt.Errorf("bump meal revision: %w", err)
	}

	if err := appendDomainEvent(ctx, tx, "meal_plan.entry.updated", scopeID(ctx), "meal", mealID, event.Payload{Revision: 0}); err != nil {
		return operationError("UpdateDish", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit update dish: %w", err)
	}

	return nil
}

// RemoveDish deletes a dish from a meal.
func (s *Store) RemoveDish(ctx context.Context, libraryID, mealID, dishID string) error {
	if err := s.scopedObject(ctx, "meals", mealID, access.Editor); err != nil {
		return operationError("RemoveDish", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operationError("RemoveDish", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, "DELETE FROM meal_dishes WHERE id = ? AND meal_id = ?", dishID, mealID)
	if err != nil {
		return fmt.Errorf("remove dish: %w", err)
	}

	n, _ := result.RowsAffected()
	if n == 0 {
		return meal.ErrNotFound
	}

	if _, err := tx.ExecContext(ctx, "UPDATE meals SET revision = revision + 1, updated_at = ?, updated_by = ? WHERE id = ?",
		formatTime(time.Now()), attributionActor(ctx), mealID); err != nil {
		return fmt.Errorf("bump meal revision: %w", err)
	}

	if err := appendDomainEvent(ctx, tx, "meal_plan.entry.updated", scopeID(ctx), "meal", mealID, event.Payload{}); err != nil {
		return operationError("RemoveDish", err)
	}

	return operationError("RemoveDish", tx.Commit())
}

// ReorderDishes updates dish positions within a meal.
func (s *Store) ReorderDishes(ctx context.Context, libraryID, mealID string, dishIDs []string) error {
	if err := s.scopedObject(ctx, "meals", mealID, access.Editor); err != nil {
		return operationError("ReorderDishes", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reorder dishes: %w", err)
	}
	defer tx.Rollback()

	for i, id := range dishIDs {
		if _, err := tx.ExecContext(ctx, "UPDATE meal_dishes SET position = ? WHERE id = ? AND meal_id = ?", i, id, mealID); err != nil {
			return fmt.Errorf("reorder dish: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, "UPDATE meals SET revision = revision + 1, updated_at = ?, updated_by = ? WHERE id = ?",
		formatTime(time.Now()), attributionActor(ctx), mealID); err != nil {
		return fmt.Errorf("bump meal revision: %w", err)
	}

	if err := appendDomainEvent(ctx, tx, "meal_plan.entry.updated", scopeID(ctx), "meal", mealID, event.Payload{Revision: 0}); err != nil {
		return operationError("ReorderDishes", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reorder dishes: %w", err)
	}

	return nil
}

func purgedDishes(ctx context.Context, tx *sql.Tx, table, parent, parentID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM "+table+" WHERE "+parent+" = ? AND entry_id IS NULL AND recipe_dish = 1", parentID) //nolint:gosec // table and parent are fixed by callers
	if err != nil {
		return nil, fmt.Errorf("read purged dishes: %w", err)
	}
	defer rows.Close()

	var purged []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan purged dish: %w", err)
		}

		purged = append(purged, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate purged dishes: %w", err)
	}

	return purged, nil
}

// replaceDishes keeps recipe_dish on purged dishes, which clients cannot resend.
func replaceDishes(ctx context.Context, tx *sql.Tx, table, parent, parentID string, insert func() error) error {
	purged, err := purgedDishes(ctx, tx, table, parent, parentID)
	if err != nil {
		return operationError("replaceDishes", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+parent+" = ?", parentID); err != nil { //nolint:gosec // table and parent are fixed by callers
		return fmt.Errorf("clear dishes: %w", err)
	}

	if err := insert(); err != nil {
		return operationError("replaceDishes", err)
	}

	for _, id := range purged {
		if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET recipe_dish = 1 WHERE id = ? AND "+parent+" = ? AND entry_id IS NULL", id, parentID); err != nil { //nolint:gosec // table and parent are fixed by callers
			return fmt.Errorf("keep purged dish: %w", err)
		}
	}

	return nil
}

func insertDishes(ctx context.Context, tx *sql.Tx, dishes []meal.Dish) error {
	for _, d := range dishes {
		if err := insertDish(ctx, tx, d); err != nil {
			return operationError("insertDishes", err)
		}
	}

	return nil
}

func insertDish(ctx context.Context, tx *sql.Tx, d meal.Dish) error {
	var scaleValue any
	if d.Scale != nil {
		scaleValue = d.Scale.String()
	}

	var entryValue any
	if d.EntryID != nil {
		entryValue = string(*d.EntryID)
	}

	var revisionValue any
	if d.RecipeRevision != nil {
		revisionValue = *d.RecipeRevision
	}

	if err := checkDishReferences(ctx, tx, "meal_dishes", "meal_dish_plain_items", d.ID, d.EntryID, d.RecipeRevision, d.PlainItems); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO meal_dishes (id, meal_id, entry_id, recipe_revision, display_name, scale, position, notes, leftover_of, recipe_dish)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.MealID, entryValue, revisionValue, d.DisplayName, scaleValue, d.Position, d.Notes, d.LeftoverOf, d.EntryID != nil,
	); err != nil {
		return fmt.Errorf("insert dish: %w", err)
	}

	for _, pi := range d.PlainItems {
		if _, err := tx.ExecContext(ctx, `INSERT INTO meal_dish_plain_items (id, dish_id, name, quantity) VALUES (?, ?, ?, ?)`,
			pi.ID, d.ID, pi.Name, pi.Quantity); err != nil {
			return fmt.Errorf("insert plain item: %w", err)
		}
	}

	return nil
}

// checkDishReferences rejects a dish or shopping item ID another row already
// holds, and a recipe outside the scoped library, before the insert would fail
// on a key or library trigger. A dish ID is checked only when dishTable is set.
func checkDishReferences(ctx context.Context, tx *sql.Tx, dishTable, itemTable, dishID string, entryID *library.EntryID, pin *uint64, items []meal.PlainItem) error {
	if dishTable != "" {
		if taken, err := rowExists(ctx, tx, "SELECT 1 FROM "+dishTable+" WHERE id = ?", dishID); err != nil || taken {
			return claimError(err, "dish", dishID)
		}
	}

	for _, item := range items {
		if taken, err := rowExists(ctx, tx, "SELECT 1 FROM "+itemTable+" WHERE id = ?", item.ID); err != nil || taken {
			return claimError(err, "shopping item", item.ID)
		}
	}

	if entryID == nil {
		return nil
	}

	held, err := rowExists(ctx, tx, "SELECT 1 FROM library_recipes WHERE id = ? AND library_id = ?", string(*entryID), scopeID(ctx))
	if err != nil {
		return fmt.Errorf("check dish recipe: %w", err)
	}

	if !held {
		return meal.ErrUnknownRecipe
	}

	if pin == nil {
		return nil
	}

	retained, err := rowExists(ctx, tx, "SELECT 1 FROM recipe_revisions WHERE entry_id = ? AND revision = ?", string(*entryID), *pin)
	if err != nil {
		return fmt.Errorf("check dish recipe revision: %w", err)
	}

	if !retained {
		return fmt.Errorf("%w: the recipe has no revision %d", meal.ErrInvalidMeal, *pin)
	}

	return nil
}

func rowExists(ctx context.Context, tx *sql.Tx, query string, args ...any) (bool, error) {
	var one int

	err := tx.QueryRowContext(ctx, query, args...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("check row: %w", err)
	}

	return true, nil
}

func claimError(err error, noun, id string) error {
	if err != nil {
		return fmt.Errorf("check %s identity: %w", noun, err)
	}

	return fmt.Errorf("%w: %s ID %q is already in use", meal.ErrInvalidMeal, noun, id)
}

func scanMealRow(row *sql.Row) (meal.Meal, error) {
	var (
		m                    meal.Meal
		dateValue            sql.NullString
		mealType             string
		createdAt, updatedAt string
	)

	if err := row.Scan(&m.ID, &m.LibraryID, &m.Name, &dateValue, &mealType, &m.Notes, &m.Revision, &createdAt, &updatedAt, &m.Skipped, &m.CreatedBy, &m.CreatedByName, &m.UpdatedBy, &m.UpdatedByName); err != nil {
		if err == sql.ErrNoRows {
			return meal.Meal{}, meal.ErrNotFound
		}

		return meal.Meal{}, fmt.Errorf("scan meal: %w", err)
	}

	if dateValue.Valid {
		d := library.Date(dateValue.String)
		m.Date = &d
	}

	m.MealType = meal.MealType(mealType)
	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	m.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)

	return m, nil
}

func scanMealRows(rows *sql.Rows) (meal.Meal, error) {
	var (
		m                    meal.Meal
		dateValue            sql.NullString
		mealType             string
		createdAt, updatedAt string
	)

	if err := rows.Scan(&m.ID, &m.LibraryID, &m.Name, &dateValue, &mealType, &m.Notes, &m.Revision, &createdAt, &updatedAt, &m.Skipped, &m.CreatedBy, &m.CreatedByName, &m.UpdatedBy, &m.UpdatedByName); err != nil {
		return meal.Meal{}, fmt.Errorf("scan meal: %w", err)
	}

	if dateValue.Valid {
		d := library.Date(dateValue.String)
		m.Date = &d
	}

	m.MealType = meal.MealType(mealType)
	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	m.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)

	return m, nil
}

type dbQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func queryDishes(ctx context.Context, db dbQueryer, mealID string) ([]meal.Dish, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, meal_id, entry_id, recipe_revision, display_name, scale, position, notes, leftover_of, `+dishRecipeStatusSQL+`, `+dishRecipeImageSQL+`
		FROM meal_dishes WHERE meal_id = ? ORDER BY position`, mealID)
	if err != nil {
		return nil, fmt.Errorf("query dishes: %w", err)
	}
	defer rows.Close()

	var dishes []meal.Dish

	for rows.Next() {
		d, err := scanDishRow(rows)
		if err != nil {
			return nil, operationError("queryDishes", err)
		}

		dishes = append(dishes, d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dishes: %w", err)
	}

	for i := range dishes {
		items, err := queryPlainItems(ctx, db, dishes[i].ID)
		if err != nil {
			return nil, operationError("queryDishes", err)
		}

		dishes[i].PlainItems = items
	}

	if dishes == nil {
		dishes = []meal.Dish{}
	}

	return dishes, nil
}

func queryDishesForMeals(ctx context.Context, db dbQueryer, mealIDs []string) (map[string][]meal.Dish, error) {
	if len(mealIDs) == 0 {
		return nil, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(mealIDs)), ",")

	args := make([]any, len(mealIDs))
	for i, id := range mealIDs {
		args[i] = id
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, meal_id, entry_id, recipe_revision, display_name, scale, position, notes, leftover_of, `+dishRecipeStatusSQL+`, `+dishRecipeImageSQL+`
		FROM meal_dishes WHERE meal_id IN (`+placeholders+`) ORDER BY meal_id, position`, args...)
	if err != nil {
		return nil, fmt.Errorf("query dishes for meals: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]meal.Dish)

	var allDishIDs []string

	for rows.Next() {
		d, err := scanDishRow(rows)
		if err != nil {
			return nil, operationError("queryDishesForMeals", err)
		}

		result[d.MealID] = append(result[d.MealID], d)
		allDishIDs = append(allDishIDs, d.ID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dishes: %w", err)
	}

	if len(allDishIDs) > 0 {
		itemMap, err := queryPlainItemsForDishes(ctx, db, allDishIDs)
		if err != nil {
			return nil, operationError("queryDishesForMeals", err)
		}

		for mealID, dishes := range result {
			for i := range dishes {
				if items, ok := itemMap[dishes[i].ID]; ok {
					result[mealID][i].PlainItems = items
				}
			}
		}
	}

	return result, nil
}

func scanDishRow(rows *sql.Rows) (meal.Dish, error) {
	var (
		d              meal.Dish
		entryID        sql.NullString
		recipeRevision sql.NullInt64
		scale          sql.NullString
	)

	if err := rows.Scan(&d.ID, &d.MealID, &entryID, &recipeRevision, &d.DisplayName, &scale, &d.Position, &d.Notes, &d.LeftoverOf, &d.RecipeStatus, &d.RecipeImage); err != nil {
		return meal.Dish{}, fmt.Errorf("scan dish: %w", err)
	}

	if entryID.Valid {
		id := library.EntryID(entryID.String)
		d.EntryID = &id
	}

	if recipeRevision.Valid {
		if recipeRevision.Int64 < 0 {
			return meal.Dish{}, fmt.Errorf("scan dish: recipe revision cannot be negative")
		}

		rev := uint64(recipeRevision.Int64)
		d.RecipeRevision = &rev
	}

	if scale.Valid {
		r := recipe.Rational(scale.String)
		d.Scale = &r
	}

	return d, nil
}

func queryPlainItems(ctx context.Context, db dbQueryer, dishID string) ([]meal.PlainItem, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, quantity FROM meal_dish_plain_items WHERE dish_id = ?`, dishID)
	if err != nil {
		return nil, fmt.Errorf("query plain items: %w", err)
	}
	defer rows.Close()

	var items []meal.PlainItem

	for rows.Next() {
		var pi meal.PlainItem
		if err := rows.Scan(&pi.ID, &pi.Name, &pi.Quantity); err != nil {
			return nil, fmt.Errorf("scan plain item: %w", err)
		}

		items = append(items, pi)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plain items: %w", err)
	}

	return items, nil
}

func queryPlainItemsForDishes(ctx context.Context, db dbQueryer, dishIDs []string) (map[string][]meal.PlainItem, error) {
	if len(dishIDs) == 0 {
		return nil, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(dishIDs)), ",")

	args := make([]any, len(dishIDs))
	for i, id := range dishIDs {
		args[i] = id
	}

	rows, err := db.QueryContext(ctx, `SELECT id, dish_id, name, quantity FROM meal_dish_plain_items WHERE dish_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("query plain items batch: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]meal.PlainItem)

	for rows.Next() {
		var (
			pi     meal.PlainItem
			dishID string
		)

		if err := rows.Scan(&pi.ID, &dishID, &pi.Name, &pi.Quantity); err != nil {
			return nil, fmt.Errorf("scan plain item: %w", err)
		}

		result[dishID] = append(result[dishID], pi)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plain items batch: %w", err)
	}

	return result, nil
}
