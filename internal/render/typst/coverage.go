package typst

import (
	"encoding/binary"
	"errors"
	"sort"
	"unicode"
)

// Before typesetting, the bundled fonts are checked for every character the
// document contains, so a missing glyph is reported rather than printed as a
// box.

type coverage struct {
	single map[rune]bool
	ranges []runeRange
}

type runeRange struct{ low, high rune }

// buildCoverage reads the character maps of every bundled font and unions
// them. Only the mappings a modern font actually uses are understood, which is
// enough for the faces Savorum bundles and refuses to guess beyond.
func buildCoverage(fonts [][]byte) (*coverage, error) {
	combined := &coverage{single: map[rune]bool{}}
	for _, font := range fonts {
		if err := combined.readFont(font); err != nil {
			return nil, err
		}
	}

	sort.Slice(combined.ranges, func(i, j int) bool { return combined.ranges[i].low < combined.ranges[j].low })
	combined.ranges = mergeRanges(combined.ranges)

	return combined, nil
}

// mergeRanges makes range ends monotonic as well as starts. That invariant is
// required by covers' binary search and matters once fallback fonts introduce
// overlapping Unicode cmap ranges.
func mergeRanges(ranges []runeRange) []runeRange {
	if len(ranges) < 2 {
		return ranges
	}

	merged := make([]runeRange, 0, len(ranges))
	for _, current := range ranges {
		last := len(merged) - 1
		if last >= 0 && current.low <= merged[last].high+1 {
			if current.high > merged[last].high {
				merged[last].high = current.high
			}

			continue
		}

		merged = append(merged, current)
	}

	return merged
}

func (c *coverage) covers(value rune) bool {
	if c.single[value] {
		return true
	}

	index := sort.Search(len(c.ranges), func(i int) bool { return c.ranges[i].high >= value })

	return index < len(c.ranges) && c.ranges[index].low <= value
}

// Missing reports the characters in the text that no bundled font can draw,
// in the order they were first seen, so an error can name them.
func (c *coverage) Missing(text string) []rune {
	var missing []rune

	seen := map[rune]bool{}

	for _, value := range text {
		if unicode.Is(unicode.Cf, value) || unicode.Is(unicode.Variation_Selector, value) {
			continue
		}

		switch value {
		case '\n', '\r', '\t', ' ':
			continue
		}

		if seen[value] || c.covers(value) {
			continue
		}

		seen[value] = true
		missing = append(missing, value)
	}

	return missing
}

var errUnreadableFont = errors.New("bundled print font could not be read")

func (c *coverage) readFont(font []byte) error {
	if len(font) < 12 {
		return errUnreadableFont
	}

	tables := binary.BigEndian.Uint16(font[4:6])

	var cmapOffset uint32

	for index := range int(tables) {
		record := 12 + index*16
		if record+16 > len(font) {
			return errUnreadableFont
		}

		if string(font[record:record+4]) == "cmap" {
			cmapOffset = binary.BigEndian.Uint32(font[record+8 : record+12])
			break
		}
	}

	if cmapOffset == 0 || int(cmapOffset)+4 > len(font) {
		return errUnreadableFont
	}

	subtables := binary.BigEndian.Uint16(font[cmapOffset+2 : cmapOffset+4])
	read := 0

	for index := range int(subtables) {
		record := int(cmapOffset) + 4 + index*8
		if record+8 > len(font) {
			return errUnreadableFont
		}

		platform := binary.BigEndian.Uint16(font[record : record+2])
		encoding := binary.BigEndian.Uint16(font[record+2 : record+4])
		offset := int(cmapOffset) + int(binary.BigEndian.Uint32(font[record+4:record+8]))

		unicodePlatform := platform == 0 || (platform == 3 && (encoding == 1 || encoding == 10))
		if !unicodePlatform || offset+4 > len(font) {
			continue
		}

		if c.readSubtable(font, offset) {
			read++
		}
	}

	if read == 0 {
		return errUnreadableFont
	}

	return nil
}

func (c *coverage) readSubtable(font []byte, offset int) bool {
	switch binary.BigEndian.Uint16(font[offset : offset+2]) {
	case 4:
		return c.readFormat4(font, offset)
	case 12:
		return c.readFormat12(font, offset)
	default:
		return false
	}
}

func (c *coverage) readFormat4(font []byte, offset int) bool {
	if offset+14 > len(font) {
		return false
	}

	segments := int(binary.BigEndian.Uint16(font[offset+6:offset+8])) / 2
	endBase := offset + 14

	startBase := endBase + segments*2 + 2
	if startBase+segments*2 > len(font) {
		return false
	}

	for index := range segments {
		end := rune(binary.BigEndian.Uint16(font[endBase+index*2 : endBase+index*2+2]))

		start := rune(binary.BigEndian.Uint16(font[startBase+index*2 : startBase+index*2+2]))
		if start > end || end == 0xffff && start == 0xffff {
			continue
		}

		c.ranges = append(c.ranges, runeRange{low: start, high: end})
	}

	return true
}

func (c *coverage) readFormat12(font []byte, offset int) bool {
	if offset+16 > len(font) {
		return false
	}

	groups := int(binary.BigEndian.Uint32(font[offset+12 : offset+16]))

	base := offset + 16
	if base+groups*12 > len(font) {
		return false
	}

	for index := range groups {
		record := base + index*12
		startCode := binary.BigEndian.Uint32(font[record : record+4])

		endCode := binary.BigEndian.Uint32(font[record+4 : record+8])
		if startCode > endCode || uint64(endCode) > unicode.MaxRune {
			continue
		}

		//nolint:gosec // the unicode.MaxRune test above bounds both ends of the range
		c.ranges = append(c.ranges, runeRange{low: rune(startCode), high: rune(endCode)})
	}

	return true
}
