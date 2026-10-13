package export

import (
	"strings"
	"unicode"
)

// maximumStem bounds the readable part of a filename, well below any
// filesystem's limit in bytes.
const maximumStem = 60

// Filename names a downloaded artifact after the recipe or collection it
// holds, the same way from every export path. Letters and digits in any script
// are kept; everything else becomes a single separator.
func Filename(name, fallback, extension string) string {
	stem := slug(name)
	if stem == "" {
		stem = slug(fallback)
	}

	if stem == "" {
		// Nothing usable was offered. A generic name is still a name, and it
		// is better than an extension with nothing in front of it.
		stem = "recipe"
	}

	return stem + extension
}

// slug reduces a title to something every filesystem and archive will accept.
// It never produces a separator, a traversal sequence, a control character, a
// leading dot, or an empty string, so a title can be hostile without becoming
// a path.
func slug(value string) string {
	var built strings.Builder

	separated := false

	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case unicode.IsLetter(character) || unicode.IsDigit(character):
			if separated && built.Len() > 0 {
				built.WriteRune('-')
			}

			separated = false

			built.WriteRune(character)
		case unicode.IsControl(character):
			// A control character is dropped rather than turned into a
			// separator: it is not a word boundary, it is an attack surface.
		default:
			separated = true
		}

		if built.Len() >= maximumStem {
			break
		}
	}

	return strings.Trim(built.String(), "-")
}
