package library

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// AuthorCandidate names a recipe credit and counts active recipes carrying it.
type AuthorCandidate struct {
	Name        string
	RecipeCount int
	Match       string
}

// AuthorCandidates reports the best available matching tier and search bounds.
type AuthorCandidates struct {
	Items                 []AuthorCandidate
	HasMore               bool
	CandidateLimitReached bool
}

// AuthorQueryRepository resolves names only from the selected library's authors.
type AuthorQueryRepository interface {
	FindRecipeAuthors(context.Context, string, int) (AuthorCandidates, error)
}

// FindRecipeAuthors validates a bounded author-name discovery request.
func (s *Service) FindRecipeAuthors(ctx context.Context, query string, limit int) (AuthorCandidates, error) {
	query = strings.TrimSpace(query)
	if query == "" || len([]rune(query)) > MaximumSearchLength {
		return AuthorCandidates{}, fmt.Errorf("author query requires 1 to %d characters: %w", MaximumSearchLength, ErrInvalidSearch)
	}

	if limit == 0 {
		limit = 20
	}

	if limit < 1 || limit > 50 {
		return AuthorCandidates{}, fmt.Errorf("author limit must be between 1 and 50: %w", ErrInvalidSearch)
	}

	repository, ok := s.repository.(AuthorQueryRepository)
	if !ok {
		return AuthorCandidates{}, fmt.Errorf("repository does not support author discovery")
	}

	return repository.FindRecipeAuthors(ctx, query, limit)
}

// NormalizeAuthorName preserves script and accent distinctions while folding
// Unicode case, canonical composition, and whitespace for full-name equality.
func NormalizeAuthorName(value string) string {
	return norm.NFC.String(cases.Fold().String(norm.NFC.String(strings.Join(strings.Fields(value), " "))))
}

// AuthorAccentName supplies a secondary suggestion key for wholly Latin names.
// These suggestions require choosing an original name before applying a filter.
func AuthorAccentName(value string) string {
	value = NormalizeAuthorName(value)
	for _, r := range value {
		if unicode.IsLetter(r) && !unicode.In(r, unicode.Latin) {
			return value
		}
	}

	return norm.NFC.String(strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) && r >= '\u0300' && r <= '\u036f' {
			return -1
		}

		return r
	}, norm.NFD.String(value)))
}

// AuthorSearchKeys bounds indexed typo candidates with single-rune deletions.
func AuthorSearchKeys(value string) []string {
	runes := []rune(AuthorAccentName(value))
	if !fuzzyAuthorLength(runes) {
		return []string{}
	}

	keys := []string{string(runes)}
	for index, r := range runes {
		if unicode.IsMark(r) {
			continue
		}

		keys = append(keys, string(runes[:index])+string(runes[index+1:]))
	}

	slices.Sort(keys)

	return slices.Compact(keys)
}

func fuzzyAuthorLength(value []rune) bool {
	letters := 0

	for _, r := range value {
		if unicode.IsLetter(r) {
			letters++
		}
	}

	return len(value) >= 5 && len(value) <= 64 && letters >= 4
}

// AuthorTypoMatch accepts one edit or adjacent transposition. Marks and script
// membership remain meaningful; no transliteration or byte distance is used.
func AuthorTypoMatch(query, name string) bool {
	left, right := []rune(AuthorAccentName(query)), []rune(AuthorAccentName(name))
	if !fuzzyAuthorLength(left) || !fuzzyAuthorLength(right) || !sameAuthorScripts(left, right) {
		return false
	}

	if len(left) > len(right) {
		left, right = right, left
	}

	if len(right)-len(left) > 1 {
		return false
	}

	index := 0
	for index < len(left) && left[index] == right[index] {
		index++
	}

	if index == len(left) {
		return len(left) == len(right) || !unicode.IsMark(right[index])
	}

	if unicode.IsMark(left[index]) || unicode.IsMark(right[index]) {
		return false
	}

	if len(left) < len(right) {
		return slices.Equal(left[index:], right[index+1:])
	}

	if slices.Equal(left[index+1:], right[index+1:]) {
		return sameAuthorScripts(left[index:index+1], right[index:index+1])
	}

	return index+1 < len(left) && !unicode.IsMark(left[index+1]) && !unicode.IsMark(right[index+1]) &&
		left[index] == right[index+1] && left[index+1] == right[index] && slices.Equal(left[index+2:], right[index+2:]) &&
		sameAuthorScripts(left[index:index+1], left[index+1:index+2])
}

func sameAuthorScripts(left, right []rune) bool {
	for name, table := range unicode.Scripts {
		if name == "Common" || name == "Inherited" {
			continue
		}

		contains := func(value []rune) bool {
			return slices.ContainsFunc(value, func(r rune) bool { return unicode.IsLetter(r) && unicode.Is(table, r) })
		}
		if contains(left) != contains(right) {
			return false
		}
	}

	return true
}
