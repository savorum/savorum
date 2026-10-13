package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/selection"
)

// SuggestRecipes reads a bounded recipe projection using the supported search grammar.
func (s *Store) SuggestRecipes(ctx context.Context, q string, limit, offset int) ([]library.Summary, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, err
	}

	search, err := library.ParseSearchQuery(q)
	if err != nil {
		return nil, err
	}

	values, _, err := s.listRecipePage(ctx, library.ListOptions{Query: q, Search: search, State: library.StateActive, Sort: library.ListSortRelevance, Limit: limit, Offset: offset}, false)

	return values, err
}

// LookupRecipes reads display projections for explicit library entry IDs.
func (s *Store) LookupRecipes(ctx context.Context, ids []string) ([]library.Summary, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, err
	}

	if len(ids) == 0 {
		return []library.Summary{}, nil
	}

	keys := make([]library.EntryID, len(ids))
	for i, id := range ids {
		keys[i] = library.EntryID(id)
	}

	values, _, err := s.listRecipePage(ctx, library.ListOptions{EntryIDs: keys, State: library.StateActive, Sort: library.ListSortTitleAsc, Limit: len(ids)}, false)
	if err != nil {
		return nil, err
	}

	trashed, _, err := s.listRecipePage(ctx, library.ListOptions{EntryIDs: keys, State: library.StateTrash, Sort: library.ListSortTitleAsc, Limit: len(ids)}, false)

	return append(values, trashed...), err
}

// SuggestBooks reads a bounded page without book memberships.
func (s *Store) SuggestBooks(ctx context.Context, q string, limit, offset int) ([]selection.BookSelectionSummary, error) {
	return s.selectBooks(ctx, nil, q, limit, offset)
}

// LookupBooks reads display metadata for explicit book IDs.
func (s *Store) LookupBooks(ctx context.Context, ids []string) ([]selection.BookSelectionSummary, error) {
	if len(ids) == 0 {
		return []selection.BookSelectionSummary{}, nil
	}

	return s.selectBooks(ctx, ids, "", len(ids), 0)
}

func (s *Store) selectBooks(ctx context.Context, ids []string, q string, limit, offset int) ([]selection.BookSelectionSummary, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, err
	}

	where := "library_id=? AND deleted_at IS NULL"
	args := []any{scopeID(ctx)}

	if len(ids) > 0 {
		where += " AND id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	} else {
		where += " AND (instr(savorum_author_name(title),savorum_author_name(?))>0 OR instr(savorum_author_name(author),savorum_author_name(?))>0 OR instr(savorum_author_name(subtitle),savorum_author_name(?))>0)"

		args = append(args, q, q, q)
	}

	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, `SELECT id,revision,kind,title,subtitle,author,editor,edition,COALESCE(cover_asset_id,'') FROM books WHERE `+where+` ORDER BY title COLLATE NOCASE,id LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("query book selections: %w", err)
	}
	defer rows.Close()

	values := []selection.BookSelectionSummary{}

	for rows.Next() {
		var v selection.BookSelectionSummary
		if err := rows.Scan(&v.ID, &v.Revision, &v.Kind, &v.Title, &v.Subtitle, &v.Author, &v.Editor, &v.Edition, &v.CoverAssetID); err != nil {
			return nil, fmt.Errorf("scan book selection: %w", err)
		}

		values = append(values, v)
	}

	return values, operationError("read selections", rows.Err())
}

// SuggestSavedMeals reads names and dish counts without loading dishes.
func (s *Store) SuggestSavedMeals(ctx context.Context, q string, limit, offset int) ([]selection.SavedMealSummary, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id,revision,name,(SELECT json_group_array(display_name) FROM (SELECT display_name FROM saved_meal_dishes WHERE saved_meal_id=saved_meals.id ORDER BY position,id)),(SELECT count(*) FROM saved_meal_dishes WHERE saved_meal_id=saved_meals.id) FROM saved_meals WHERE library_id=? AND (instr(savorum_author_name(name),savorum_author_name(?))>0 OR EXISTS(SELECT 1 FROM saved_meal_dishes WHERE saved_meal_id=saved_meals.id AND instr(savorum_author_name(display_name),savorum_author_name(?))>0)) ORDER BY name COLLATE NOCASE,id LIMIT ? OFFSET ?`, scopeID(ctx), q, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query saved meal selections: %w", err)
	}
	defer rows.Close()

	values := []selection.SavedMealSummary{}

	for rows.Next() {
		var (
			v     selection.SavedMealSummary
			names string
		)
		if err := rows.Scan(&v.ID, &v.Revision, &v.Name, &names, &v.DishCount); err != nil {
			return nil, fmt.Errorf("scan saved meal selection: %w", err)
		}

		if err := json.Unmarshal([]byte(names), &v.DishNames); err != nil {
			return nil, operationError("read dish names", err)
		}

		values = append(values, v)
	}

	return values, operationError("read selections", rows.Err())
}
