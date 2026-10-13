package recipe

import (
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/bidi"
)

// Writing directions of recipe text.
const (
	LeftToRight = "ltr"
	RightToLeft = "rtl"
)

var rightToLeftScripts = map[string]bool{
	"Adlm": true, "Arab": true, "Hebr": true, "Mand": true, "Nkoo": true,
	"Rohg": true, "Samr": true, "Syrc": true, "Thaa": true,
}

// LanguageDirection is the writing direction of a BCP 47 tag's script,
// including the script a language is written in by default. It is empty for
// an absent or malformed tag.
func LanguageDirection(tag string) string {
	if tag == "" {
		return ""
	}

	parsed, err := language.Parse(tag)
	if err != nil {
		return ""
	}

	script, _ := parsed.Script()
	if rightToLeftScripts[script.String()] {
		return RightToLeft
	}

	return LeftToRight
}

// TextDirection is the direction of the first strongly directional character
// in the given texts, which is how text of unknown language is laid out. It is
// empty when none has one.
func TextDirection(texts ...string) string {
	for _, text := range texts {
		for _, character := range text {
			properties, _ := bidi.LookupRune(character)
			if class := properties.Class(); class == bidi.L {
				return LeftToRight
			} else if class == bidi.R || class == bidi.AL {
				return RightToLeft
			}
		}
	}

	return ""
}
