package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
)

func sqlPlaceholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func scanRows[T any](rows *sql.Rows, description string, scan func(*sql.Rows) (T, error)) ([]T, error) {
	defer rows.Close()

	var items []T

	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", description, err)
	}

	return items, nil
}

func scanRowsNonNil[T any](rows *sql.Rows, description string, scan func(*sql.Rows) (T, error)) ([]T, error) {
	items, err := scanRows(rows, description, scan)
	if err != nil {
		return nil, err
	}

	if items == nil {
		items = []T{}
	}

	return items, nil
}
