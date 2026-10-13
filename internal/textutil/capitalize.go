// Package textutil holds shared text helpers.
package textutil

import "unicode"

// Capitalize uppercases the first Unicode code point and leaves the remainder unchanged.
func Capitalize(value string) string {
	if value == "" {
		return value
	}

	runes := []rune(value)
	runes[0] = unicode.ToUpper(runes[0])

	return string(runes)
}
