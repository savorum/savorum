package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/library"
)

const authorCandidateLimit = 128

// FindRecipeAuthors searches authoritative author names in the active library.
// Exact and accent tiers are complete; only typo comparisons have a candidate cap.
func (s *Store) FindRecipeAuthors(ctx context.Context, query string, limit int) (library.AuthorCandidates, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return library.AuthorCandidates{}, err
	}

	for _, tier := range []struct {
		column string
		value  string
		match  string
	}{
		{"normalized_name", library.NormalizeAuthorName(query), "exact"},
		{"accent_name", library.AuthorAccentName(query), "accent"},
	} {
		items, err := s.authorCandidates(ctx, "names."+tier.column+"=?", []any{tier.value}, limit+1, tier.match)
		if err != nil {
			return library.AuthorCandidates{}, err
		}

		if len(items) > 0 {
			return authorCandidatePage(items, limit, false), nil
		}
	}

	keys := library.AuthorSearchKeys(query)
	if len(keys) == 0 {
		return authorCandidatePage(nil, limit, false), nil
	}

	args := []any{scopeID(ctx)}
	for _, key := range keys {
		args = append(args, key)
	}

	args = append(args, authorCandidateLimit+1)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")

	rows, err := s.db.QueryContext(ctx, `SELECT keys.name FROM recipe_author_search_keys AS keys
		WHERE keys.library_id=? AND keys.key IN (`+placeholders+`)
		AND EXISTS (
			SELECT 1 FROM recipe_author_entries AS entries
			JOIN library_recipes AS recipes ON recipes.id=entries.entry_id
			WHERE entries.library_id=keys.library_id AND entries.name=keys.name
			AND recipes.library_id=keys.library_id AND recipes.deleted_at IS NULL
		)
		GROUP BY keys.name ORDER BY keys.name LIMIT ?`, args...)
	if err != nil {
		return library.AuthorCandidates{}, fmt.Errorf("query indexed author candidates: %w", err)
	}
	defer rows.Close()

	names := []string{}

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return library.AuthorCandidates{}, fmt.Errorf("scan indexed author candidate: %w", err)
		}

		names = append(names, name)
	}

	if err := rows.Err(); err != nil {
		return library.AuthorCandidates{}, fmt.Errorf("iterate indexed author candidates: %w", err)
	}

	if err := rows.Close(); err != nil {
		return library.AuthorCandidates{}, fmt.Errorf("close indexed author candidates: %w", err)
	}

	bounded := len(names) > authorCandidateLimit
	if bounded {
		names = names[:authorCandidateLimit]
	}

	matches := []any{}

	for _, name := range names {
		if library.AuthorTypoMatch(query, name) {
			matches = append(matches, name)
		}
	}

	if len(matches) == 0 {
		return authorCandidatePage(nil, limit, bounded), nil
	}

	placeholders = strings.TrimSuffix(strings.Repeat("?,", len(matches)), ",")

	items, err := s.authorCandidates(ctx, "names.name IN ("+placeholders+")", matches, limit+1, "fuzzy")
	if err != nil {
		return library.AuthorCandidates{}, err
	}

	return authorCandidatePage(items, limit, bounded), nil
}

func authorCandidatePage(items []library.AuthorCandidate, limit int, bounded bool) library.AuthorCandidates {
	if items == nil {
		items = []library.AuthorCandidate{}
	}

	return library.AuthorCandidates{
		Items: items[:min(limit, len(items))], HasMore: len(items) > limit, CandidateLimitReached: bounded,
	}
}

func (s *Store) authorCandidates(ctx context.Context, condition string, arguments []any, limit int, match string) ([]library.AuthorCandidate, error) {
	args := append([]any{scopeID(ctx)}, arguments...)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, `SELECT names.name, COUNT(*) FROM recipe_author_names AS names
		JOIN recipe_author_entries AS entries ON entries.library_id=names.library_id AND entries.name=names.name
		JOIN library_recipes AS recipes ON recipes.id=entries.entry_id AND recipes.library_id=names.library_id
		WHERE names.library_id=? AND recipes.deleted_at IS NULL AND `+condition+`
		GROUP BY names.name ORDER BY names.name LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("query author names: %w", err)
	}
	defer rows.Close()

	items := []library.AuthorCandidate{}

	for rows.Next() {
		item := library.AuthorCandidate{Match: match}
		if err := rows.Scan(&item.Name, &item.RecipeCount); err != nil {
			return nil, fmt.Errorf("scan author name: %w", err)
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate author names: %w", err)
	}

	return items, nil
}
