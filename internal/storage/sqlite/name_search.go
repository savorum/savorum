package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/savorum/savorum/internal/library"
)

const maximumExpandedWords = 8

func (s *Store) expandNameSearch(ctx context.Context, search library.SearchCriteria) (library.SearchCriteria, error) {
	if search.TextAlternatives != nil {
		return search, nil
	}

	search.TextAlternatives = map[string][]string{}
	words := library.SearchWords(search.Text)
	expanded := 0
	seen := map[string]bool{}

	for _, word := range words {
		if seen[word] || expanded >= maximumExpandedWords {
			continue
		}

		seen[word] = true
		keys := library.AuthorSearchKeys(word)

		if len(keys) == 0 {
			continue
		}

		expanded++

		alternatives, err := s.nameTermAlternatives(ctx, word, keys)
		if err != nil {
			return search, err
		}

		if len(alternatives) > 0 {
			search.TextAlternatives[word] = alternatives
		}
	}

	return search, nil
}

func (s *Store) nameTermAlternatives(ctx context.Context, word string, keys []string) ([]string, error) {
	args := []any{scopeID(ctx)}
	for _, key := range keys {
		args = append(args, key)
	}

	args = append(args, authorCandidateLimit)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")

	rows, err := s.db.QueryContext(ctx, `SELECT keys.term FROM recipe_name_term_keys AS keys
		WHERE keys.library_id=? AND keys.key IN (`+placeholders+`)
		GROUP BY keys.term ORDER BY keys.term LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("query indexed name terms: %w", err)
	}
	defer rows.Close()

	alternatives := []string{library.NormalizeAuthorName(word)}

	for rows.Next() {
		var term string
		if err := rows.Scan(&term); err != nil {
			return nil, fmt.Errorf("scan indexed name term: %w", err)
		}

		if library.NormalizeAuthorName(word) != term && library.AuthorTypoMatch(word, term) {
			alternatives = append(alternatives, term)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexed name terms: %w", err)
	}

	return alternatives, nil
}

func exactNameTextWhere(search library.SearchCriteria) (string, []any) {
	conditions := []string{}
	args := []any{}

	for _, word := range library.SearchWords(search.Text) {
		condition := `recipes.rowid IN (SELECT rowid FROM library_recipes_fts WHERE library_recipes_fts MATCH ?)`

		args = append(args, searchExpression(word))
		if len(search.TextAlternatives[word]) > 0 {
			condition = "(" + condition + ` OR EXISTS (
				SELECT 1 FROM recipe_name_term_entries AS term_entry
				WHERE term_entry.entry_id=recipes.id AND term_entry.library_id=recipes.library_id
				AND term_entry.term=?))`

			args = append(args, library.NormalizeAuthorName(word))
		}

		conditions = append(conditions, condition)
	}

	return strings.Join(conditions, " AND "), args
}
