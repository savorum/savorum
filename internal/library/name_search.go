package library

import (
	"slices"
	"strings"
	"unicode"
)

// SearchWords retains combining marks with their letters. Unspaced scripts
// remain complete runs; punctuation separates query words.
func SearchWords(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r)
	})
}

// NameSearchTerms indexes eligible title and author runs without recipe body text.
func NameSearchTerms(value string) []string {
	terms := []string{}

	for _, word := range SearchWords(value) {
		term := NormalizeAuthorName(word)
		if fuzzyAuthorLength([]rune(AuthorAccentName(term))) {
			terms = append(terms, term)
		}
	}

	slices.Sort(terms)

	return slices.Compact(terms)
}
