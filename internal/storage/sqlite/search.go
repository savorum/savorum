package sqlite

import (
	"context"
	"fmt"
)

// RebuildSearchIndex regenerates disposable search data from current documents
// and canonical organization. It never rewrites a recipe or its history.
func (s *Store) RebuildSearchIndex(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start search index rebuild: %w", err)
	}
	defer tx.Rollback()

	for _, statement := range []string{
		`DELETE FROM recipe_author_entries`,
		`DELETE FROM recipe_author_names`,
		`INSERT OR IGNORE INTO recipe_author_names(library_id, name, normalized_name, accent_name)
		 SELECT recipes.library_id, author.value, savorum_author_name(author.value), savorum_author_accent(author.value)
		 FROM library_recipes AS recipes, json_each(recipes.authors_json) AS author WHERE author.type='text'`,
		`INSERT OR IGNORE INTO recipe_author_entries(library_id, name, entry_id)
		 SELECT recipes.library_id, author.value, recipes.id
		 FROM library_recipes AS recipes, json_each(recipes.authors_json) AS author WHERE author.type='text'`,
		`DELETE FROM recipe_name_term_entries`,
		`DELETE FROM recipe_name_terms`,
		`INSERT OR IGNORE INTO recipe_name_terms(library_id, term)
		 SELECT library_id, term FROM recipe_name_term_content`,
		`INSERT OR IGNORE INTO recipe_name_term_entries(entry_id, library_id, term)
		 SELECT entry_id, library_id, term FROM recipe_name_term_content`,
		`DELETE FROM recipe_ingredient_names`,
		`INSERT INTO recipe_ingredient_names(entry_id, library_id, name)
		 SELECT entry_id, library_id, name FROM recipe_ingredient_name_content`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("rebuild field search indexes: %w", err)
		}
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM library_recipes_fts`); err != nil {
		return fmt.Errorf("clear search index: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization) SELECT rowid,title,ingredients,search_text,organization FROM library_search_content`); err != nil {
		return fmt.Errorf("rebuild search index: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit search index rebuild: %w", err)
	}

	return nil
}
