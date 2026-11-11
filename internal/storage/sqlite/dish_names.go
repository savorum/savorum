package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/event"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/meal"
)

// Read the previous recipe title before updating the head row.
func renameDishesAfterRecipe(ctx context.Context, transaction *sql.Tx, entryID library.EntryID, title string, timestamp time.Time) error {
	var previous string

	err := transaction.QueryRowContext(ctx, `SELECT title FROM library_recipes WHERE id = ?`, entryID).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read previous recipe title: %w", err)
	}

	previous = strings.TrimSpace(previous)
	title = strings.TrimSpace(title)

	if previous == title || previous == "" || title == "" || len([]rune(title)) > meal.MaximumDishName {
		return nil
	}

	for _, table := range []dishTable{plannedDishes, savedDishes} {
		owners, err := renameDishes(ctx, transaction, table, entryID, previous, title)
		if err != nil {
			return err
		}

		for _, owner := range owners {
			if err := table.touch(ctx, transaction, owner, timestamp); err != nil {
				return err
			}
		}
	}

	return nil
}

type dishTable struct {
	owners, rename string
	touch          func(context.Context, *sql.Tx, string, time.Time) error
}

var (
	plannedDishes = dishTable{
		owners: `SELECT DISTINCT meal_id FROM meal_dishes WHERE entry_id = ? AND display_name = ?`,
		rename: `UPDATE meal_dishes SET display_name = ? WHERE entry_id = ? AND display_name = ?`,
		touch: func(ctx context.Context, transaction *sql.Tx, id string, at time.Time) error {
			if _, err := transaction.ExecContext(ctx, `UPDATE meals SET revision = revision + 1, updated_at = ?, updated_by = ? WHERE id = ?`,
				formatTime(at), attributionActor(ctx), id); err != nil {
				return fmt.Errorf("bump renamed meal revision: %w", err)
			}

			return appendDomainEvent(ctx, transaction, "meal_plan.entry.updated", scopeID(ctx), "meal", id, event.Payload{})
		},
	}
	savedDishes = dishTable{
		owners: `SELECT DISTINCT saved_meal_id FROM saved_meal_dishes WHERE entry_id = ? AND display_name = ?`,
		rename: `UPDATE saved_meal_dishes SET display_name = ? WHERE entry_id = ? AND display_name = ?`,
		touch: func(ctx context.Context, transaction *sql.Tx, id string, at time.Time) error {
			if _, err := transaction.ExecContext(ctx, `UPDATE saved_meals SET revision = revision + 1, updated_at = ? WHERE id = ?`,
				formatTime(at), id); err != nil {
				return fmt.Errorf("bump renamed saved meal revision: %w", err)
			}

			return nil
		},
	}
)

func renameDishes(ctx context.Context, transaction *sql.Tx, table dishTable, entryID library.EntryID, previous, title string) ([]string, error) {
	rows, err := transaction.QueryContext(ctx, table.owners, entryID, previous)
	if err != nil {
		return nil, fmt.Errorf("find dishes named after recipe: %w", err)
	}
	defer rows.Close()

	var owners []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan dish owner: %w", err)
		}

		owners = append(owners, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dish owners: %w", err)
	}

	if len(owners) == 0 {
		return nil, nil
	}

	if _, err := transaction.ExecContext(ctx, table.rename, title, entryID, previous); err != nil {
		return nil, fmt.Errorf("rename dishes after recipe: %w", err)
	}

	return owners, nil
}
