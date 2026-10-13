package typst

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/image/font/sfnt"
)

type fontFace struct {
	family string
	font   *sfnt.Font
}

// Keep every primary face and the complete families needed for fallback.
// Requiring primary coverage in every face preserves bold and italic text.
func (b bundle) fontsFor(text string) ([][]byte, error) {
	families := map[string]bool{"SourceSans3": true, "SourceSerif4": true}
	seen := map[rune]bool{}

	var scratch sfnt.Buffer

	for _, character := range text {
		if seen[character] || unicode.IsControl(character) || unicode.Is(unicode.Cf, character) || unicode.Is(unicode.Variation_Selector, character) {
			continue
		}

		seen[character] = true
		primary := true

		for _, face := range b.faces {
			if !strings.HasPrefix(face.family, "Source") {
				continue
			}

			glyph, err := face.font.GlyphIndex(&scratch, character)
			if err != nil {
				return nil, fmt.Errorf("read primary font glyph: %w", err)
			}

			primary = primary && glyph != 0
		}

		if primary {
			continue
		}

		for _, face := range b.faces {
			glyph, err := face.font.GlyphIndex(&scratch, character)
			if err != nil {
				return nil, fmt.Errorf("read fallback font glyph: %w", err)
			}

			if glyph != 0 {
				families[face.family] = true
			}
		}
	}

	fonts := make([][]byte, 0, len(b.fonts))
	for index, face := range b.faces {
		if families[face.family] {
			fonts = append(fonts, b.fonts[index])
		}
	}

	return fonts, nil
}
