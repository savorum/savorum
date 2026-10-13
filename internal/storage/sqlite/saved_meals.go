package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/meal"
	"github.com/savorum/savorum/internal/recipe"
)

var _ meal.SavedMealPersistence = (*Store)(nil)

// CreateSavedMeal stores a reusable meal combination and its dishes.
func (s *Store) CreateSavedMeal(ctx context.Context, _ string, sm meal.SavedMeal) error {
	if err := s.authorizeScope(ctx, access.Editor); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create saved meal: %w", err)
	}
	defer tx.Rollback()

	if err := s.authorizeScopeWith(ctx, tx, access.Editor); err != nil {
		return err
	}

	if err := recordCreation(ctx, tx, sm.ID); err != nil {
		return err
	}

	libraryID := scopeID(ctx)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO saved_meals (id, library_id, name, notes, revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sm.ID, libraryID, sm.Name, sm.Notes, sm.Revision,
		formatTime(sm.CreatedAt), formatTime(sm.UpdatedAt),
	); err != nil {
		return fmt.Errorf("insert saved meal: %w", err)
	}

	for _, d := range sm.Dishes {
		var scaleValue any
		if d.Scale != nil {
			scaleValue = d.Scale.String()
		}

		var entryValue any
		if d.EntryID != nil {
			entryValue = string(*d.EntryID)
		}

		if err := checkDishReferences(ctx, tx, "saved_meal_dishes", "saved_meal_dish_plain_items", d.ID, d.EntryID, nil, d.PlainItems); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO saved_meal_dishes (id, saved_meal_id, entry_id, display_name, scale, position, notes, recipe_dish)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			d.ID, sm.ID, entryValue, d.DisplayName, scaleValue, d.Position, d.Notes, d.EntryID != nil,
		); err != nil {
			return fmt.Errorf("insert saved dish: %w", err)
		}

		for _, pi := range d.PlainItems {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO saved_meal_dish_plain_items (id, dish_id, name, quantity)
				VALUES (?, ?, ?, ?)`,
				pi.ID, d.ID, pi.Name, pi.Quantity,
			); err != nil {
				return fmt.Errorf("insert saved plain item: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create saved meal: %w", err)
	}

	return nil
}

// GetSavedMeal retrieves a saved meal by ID with its dishes.
func (s *Store) GetSavedMeal(ctx context.Context, libraryID, id string) (meal.SavedMeal, error) {
	if err := s.scopedObject(ctx, "saved_meals", id, access.Viewer); err != nil {
		return meal.SavedMeal{}, err
	}

	var (
		sm                   meal.SavedMeal
		createdAt, updatedAt string
	)

	if err := s.db.QueryRowContext(ctx, `
		SELECT id, library_id, name, notes, revision, created_at, updated_at
		FROM saved_meals WHERE id = ? AND library_id = ?`, id, scopeID(ctx)).
		Scan(&sm.ID, &sm.LibraryID, &sm.Name, &sm.Notes, &sm.Revision, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return meal.SavedMeal{}, meal.ErrNotFound
		}

		return meal.SavedMeal{}, fmt.Errorf("get saved meal: %w", err)
	}

	sm.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	sm.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)

	dishes, err := querySavedDishes(ctx, s.db, sm.ID)
	if err != nil {
		return meal.SavedMeal{}, err
	}

	sm.Dishes = dishes

	return sm, nil
}

// ListSavedMeals returns saved meals in alphabetical order.
func (s *Store) ListSavedMeals(ctx context.Context, libraryID string, limit, offset int) ([]meal.SavedMeal, int, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, 0, err
	}

	scope := scopeID(ctx)

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM saved_meals WHERE library_id = ?`, scope).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count saved meals: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, library_id, name, notes, revision, created_at, updated_at
		FROM saved_meals WHERE library_id = ?
		ORDER BY name ASC, id ASC
		LIMIT ? OFFSET ?`, scope, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list saved meals: %w", err)
	}
	defer rows.Close()

	var (
		meals   []meal.SavedMeal
		mealIDs []string
	)

	for rows.Next() {
		var (
			sm                   meal.SavedMeal
			createdAt, updatedAt string
		)

		if err := rows.Scan(&sm.ID, &sm.LibraryID, &sm.Name, &sm.Notes, &sm.Revision, &createdAt, &updatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan saved meal: %w", err)
		}

		sm.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		sm.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)

		meals = append(meals, sm)
		mealIDs = append(mealIDs, sm.ID)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate saved meals: %w", err)
	}

	if len(mealIDs) > 0 {
		dishMap, err := querySavedDishesForMeals(ctx, s.db, mealIDs)
		if err != nil {
			return nil, 0, err
		}

		for i := range meals {
			meals[i].Dishes = dishMap[meals[i].ID]
			if meals[i].Dishes == nil {
				meals[i].Dishes = []meal.SavedDish{}
			}
		}
	}

	if meals == nil {
		meals = []meal.SavedMeal{}
	}

	return meals, total, nil
}

// UpdateSavedMeal updates a saved meal and its dishes.
func (s *Store) UpdateSavedMeal(ctx context.Context, libraryID string, sm meal.SavedMeal, expectedRevision uint64) error {
	if err := s.scopedObject(ctx, "saved_meals", sm.ID, access.Editor); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update saved meal: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE saved_meals SET name = ?, notes = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND library_id = ? AND revision = ?`,
		sm.Name, sm.Notes, formatTime(sm.UpdatedAt),
		sm.ID, scopeID(ctx), expectedRevision,
	)
	if err != nil {
		return fmt.Errorf("update saved meal: %w", err)
	}

	n, _ := result.RowsAffected()
	if n == 0 {
		var current uint64
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM saved_meals WHERE id = ? AND library_id = ?", sm.ID, scopeID(ctx)).Scan(&current); err != nil {
			return meal.ErrNotFound
		}

		return &meal.PreconditionError{MealID: sm.ID, Expected: expectedRevision, Current: current}
	}

	if err := replaceDishes(ctx, tx, "saved_meal_dishes", "saved_meal_id", sm.ID, func() error {
		for _, d := range sm.Dishes {
			var scaleValue any
			if d.Scale != nil {
				scaleValue = d.Scale.String()
			}

			var entryValue any
			if d.EntryID != nil {
				entryValue = string(*d.EntryID)
			}

			if err := checkDishReferences(ctx, tx, "saved_meal_dishes", "saved_meal_dish_plain_items", d.ID, d.EntryID, nil, d.PlainItems); err != nil {
				return err
			}

			if _, err := tx.ExecContext(ctx, `
				INSERT INTO saved_meal_dishes (id, saved_meal_id, entry_id, display_name, scale, position, notes, recipe_dish)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				d.ID, sm.ID, entryValue, d.DisplayName, scaleValue, d.Position, d.Notes, d.EntryID != nil,
			); err != nil {
				return fmt.Errorf("insert saved dish: %w", err)
			}

			for _, pi := range d.PlainItems {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO saved_meal_dish_plain_items (id, dish_id, name, quantity)
					VALUES (?, ?, ?, ?)`,
					pi.ID, d.ID, pi.Name, pi.Quantity,
				); err != nil {
					return fmt.Errorf("insert saved plain item: %w", err)
				}
			}
		}

		return nil
	}); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit update saved meal: %w", err)
	}

	return nil
}

// DeleteSavedMeal deletes a saved meal and its dishes.
func (s *Store) DeleteSavedMeal(ctx context.Context, libraryID, id string, expectedRevision uint64) error {
	if err := s.scopedObject(ctx, "saved_meals", id, access.Editor); err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, "DELETE FROM saved_meals WHERE id = ? AND library_id = ? AND revision = ?", id, scopeID(ctx), expectedRevision)
	if err != nil {
		return fmt.Errorf("delete saved meal: %w", err)
	}

	n, _ := result.RowsAffected()
	if n == 0 {
		var current uint64
		if err := s.db.QueryRowContext(ctx, "SELECT revision FROM saved_meals WHERE id = ? AND library_id = ?", id, scopeID(ctx)).Scan(&current); err != nil {
			return meal.ErrNotFound
		}

		return &meal.PreconditionError{MealID: id, Expected: expectedRevision, Current: current}
	}

	return nil
}

func querySavedDishes(ctx context.Context, db dbQueryer, savedMealID string) ([]meal.SavedDish, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, saved_meal_id, entry_id, display_name, scale, position, notes, `+dishRecipeStatusSQL+`, `+dishRecipeImageSQL+`
		FROM saved_meal_dishes WHERE saved_meal_id = ? ORDER BY position`, savedMealID)
	if err != nil {
		return nil, fmt.Errorf("query saved dishes: %w", err)
	}
	defer rows.Close()

	var dishes []meal.SavedDish

	for rows.Next() {
		d, err := scanSavedDishRow(rows)
		if err != nil {
			return nil, err
		}

		dishes = append(dishes, d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate saved dishes: %w", err)
	}

	for i := range dishes {
		items, err := querySavedPlainItems(ctx, db, dishes[i].ID)
		if err != nil {
			return nil, err
		}

		dishes[i].PlainItems = items
	}

	if dishes == nil {
		dishes = []meal.SavedDish{}
	}

	return dishes, nil
}

func querySavedDishesForMeals(ctx context.Context, db dbQueryer, mealIDs []string) (map[string][]meal.SavedDish, error) {
	if len(mealIDs) == 0 {
		return nil, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(mealIDs)), ",")

	args := make([]any, len(mealIDs))
	for i, id := range mealIDs {
		args[i] = id
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, saved_meal_id, entry_id, display_name, scale, position, notes, `+dishRecipeStatusSQL+`, `+dishRecipeImageSQL+`
		FROM saved_meal_dishes WHERE saved_meal_id IN (`+placeholders+`) ORDER BY saved_meal_id, position`, args...)
	if err != nil {
		return nil, fmt.Errorf("query saved dishes for meals: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]meal.SavedDish)

	var allDishIDs []string

	for rows.Next() {
		d, err := scanSavedDishRow(rows)
		if err != nil {
			return nil, err
		}

		result[d.SavedMealID] = append(result[d.SavedMealID], d)
		allDishIDs = append(allDishIDs, d.ID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate saved dishes: %w", err)
	}

	if len(allDishIDs) > 0 {
		itemMap, err := querySavedPlainItemsForDishes(ctx, db, allDishIDs)
		if err != nil {
			return nil, err
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

func scanSavedDishRow(rows *sql.Rows) (meal.SavedDish, error) {
	var (
		d       meal.SavedDish
		entryID sql.NullString
		scale   sql.NullString
	)

	if err := rows.Scan(&d.ID, &d.SavedMealID, &entryID, &d.DisplayName, &scale, &d.Position, &d.Notes, &d.RecipeStatus, &d.RecipeImage); err != nil {
		return meal.SavedDish{}, fmt.Errorf("scan saved dish: %w", err)
	}

	if entryID.Valid {
		id := library.EntryID(entryID.String)
		d.EntryID = &id
	}

	if scale.Valid {
		r := recipe.Rational(scale.String)
		d.Scale = &r
	}

	return d, nil
}

func querySavedPlainItems(ctx context.Context, db dbQueryer, dishID string) ([]meal.PlainItem, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, quantity FROM saved_meal_dish_plain_items WHERE dish_id = ?`, dishID)
	if err != nil {
		return nil, fmt.Errorf("query saved plain items: %w", err)
	}
	defer rows.Close()

	var items []meal.PlainItem

	for rows.Next() {
		var pi meal.PlainItem
		if err := rows.Scan(&pi.ID, &pi.Name, &pi.Quantity); err != nil {
			return nil, fmt.Errorf("scan saved plain item: %w", err)
		}

		items = append(items, pi)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate saved plain items: %w", err)
	}

	return items, nil
}

func querySavedPlainItemsForDishes(ctx context.Context, db dbQueryer, dishIDs []string) (map[string][]meal.PlainItem, error) {
	if len(dishIDs) == 0 {
		return nil, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(dishIDs)), ",")

	args := make([]any, len(dishIDs))
	for i, id := range dishIDs {
		args[i] = id
	}

	rows, err := db.QueryContext(ctx, `SELECT id, dish_id, name, quantity FROM saved_meal_dish_plain_items WHERE dish_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("query saved plain items batch: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]meal.PlainItem)

	for rows.Next() {
		var (
			pi     meal.PlainItem
			dishID string
		)
		if err := rows.Scan(&pi.ID, &dishID, &pi.Name, &pi.Quantity); err != nil {
			return nil, fmt.Errorf("scan saved plain item: %w", err)
		}

		result[dishID] = append(result[dishID], pi)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate saved plain items batch: %w", err)
	}

	return result, nil
}
