package imaging

// Readers for the orientation and color metadata the standard decoders skip.
// Each one tolerates damage by returning nothing rather than failing: a file
// whose pixels decode is shown even when its metadata is unreadable.

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"maps"
	"slices"
)

// maximumICCBytes bounds an embedded profile. Real display profiles are a few
// kilobytes; the bound only stops a hostile file from inflating one.
const maximumICCBytes = 4 << 20

const (
	tiffOrientationTag = 0x0112
	tiffICCTag         = 0x8773
)

type ifdEntry struct {
	typ, count uint32
	value      []byte
}

// ifd is the first image file directory of a TIFF structure, which is also
// the layout of an Exif block.
type ifd struct {
	order   binary.ByteOrder
	data    []byte
	entries map[uint16]ifdEntry
}

func readIFD0(data []byte) ifd {
	result := ifd{entries: map[uint16]ifdEntry{}}
	if len(data) < 8 {
		return result
	}

	switch string(data[:2]) {
	case "II":
		result.order = binary.LittleEndian
	case "MM":
		result.order = binary.BigEndian
	default:
		return result
	}

	if result.order.Uint16(data[2:4]) != 42 {
		return result
	}

	result.data = data
	offset := int(result.order.Uint32(data[4:8]))

	if offset < 8 || offset+2 > len(data) {
		return result
	}

	count := int(result.order.Uint16(data[offset:]))
	for index := range count {
		at := offset + 2 + index*12
		if at+12 > len(data) {
			break
		}

		tag := result.order.Uint16(data[at:])
		result.entries[tag] = ifdEntry{typ: uint32(result.order.Uint16(data[at+2:])), count: result.order.Uint32(data[at+4:]), value: data[at+8 : at+12]}
	}

	return result
}

func (d ifd) orientation() int {
	entry, ok := d.entries[tiffOrientationTag]
	if !ok || entry.typ != 3 || entry.count != 1 {
		return 1
	}

	if value := int(d.order.Uint16(entry.value)); value >= 1 && value <= 8 {
		return value
	}

	return 1
}

// bytes returns an undefined-type value stored outside the entry.
func (d ifd) bytes(tag uint16) []byte {
	entry, ok := d.entries[tag]
	if !ok || entry.typ != 7 || entry.count == 0 || entry.count > maximumICCBytes {
		return nil
	}

	if entry.count <= 4 {
		return entry.value[:entry.count]
	}

	offset := int(d.order.Uint32(entry.value))
	if offset < 0 || offset+int(entry.count) > len(d.data) {
		return nil
	}

	return d.data[offset : offset+int(entry.count)]
}

// exifOrientation reads the orientation from an Exif payload, with or without
// its "Exif\0\0" identifier.
func exifOrientation(payload []byte) int {
	return readIFD0(bytes.TrimPrefix(payload, []byte("Exif\x00\x00"))).orientation()
}

// jpegMetadata scans the header segments for the Exif orientation and the ICC
// profile, which a JPEG splits across numbered APP2 segments.
func jpegMetadata(data []byte) (int, []byte) {
	orientation := 1
	chunks := map[byte][]byte{}

	for at := 2; at+4 <= len(data) && data[at] == 0xff; {
		marker := data[at+1]
		if marker == 0xda || marker == 0xd9 {
			break
		}

		length := int(binary.BigEndian.Uint16(data[at+2:]))
		if length < 2 || at+2+length > len(data) {
			break
		}

		segment := data[at+4 : at+2+length]

		switch {
		case marker == 0xe1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")):
			orientation = exifOrientation(segment)
		case marker == 0xe2 && len(segment) > 14 && bytes.HasPrefix(segment, []byte("ICC_PROFILE\x00")):
			chunks[segment[12]] = segment[14:]
		}

		at += 2 + length
	}

	return orientation, joinChunks(chunks)
}

func joinChunks(chunks map[byte][]byte) []byte {
	if len(chunks) == 0 {
		return nil
	}

	var profile []byte
	for _, sequence := range slices.Sorted(maps.Keys(chunks)) {
		profile = append(profile, chunks[sequence]...)
		if len(profile) > maximumICCBytes {
			return nil
		}
	}

	return profile
}

// pngMetadata reads the chunks before the image data. cICP describes the
// color space outright; iCCP carries a profile; sRGB names sRGB.
func pngMetadata(data []byte) (int, ColorDescription) {
	orientation := 1

	var description ColorDescription

	for at := 8; at+12 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[at:]))

		kind := string(data[at+4 : at+8])
		if length < 0 || at+12+length > len(data) || kind == "IDAT" {
			break
		}

		chunk := data[at+8 : at+8+length]

		switch kind {
		case "cICP":
			if len(chunk) == 4 {
				description.CICP = &CICP{Primaries: uint16(chunk[0]), Transfer: uint16(chunk[1]), Matrix: uint16(chunk[2]), FullRange: chunk[3] == 1}
			}
		case "iCCP":
			description.ICC = inflateICC(chunk)
		case "eXIf":
			orientation = exifOrientation(chunk)
		}

		at += 12 + length
	}

	// cICP takes precedence over every other color chunk.
	if description.CICP != nil {
		description.ICC = nil
	}

	return orientation, description
}

func inflateICC(chunk []byte) []byte {
	name := bytes.IndexByte(chunk, 0)
	if name < 1 || name+2 > len(chunk) || chunk[name+1] != 0 {
		return nil
	}

	reader, err := zlib.NewReader(bytes.NewReader(chunk[name+2:]))
	if err != nil {
		return nil
	}

	defer reader.Close()

	profile, err := io.ReadAll(io.LimitReader(reader, maximumICCBytes+1))
	if err != nil || len(profile) > maximumICCBytes {
		return nil
	}

	return profile
}

func webpMetadata(data []byte) (int, []byte) {
	orientation := 1

	var profile []byte

	for at := 12; at+8 <= len(data); {
		length := int(binary.LittleEndian.Uint32(data[at+4:]))
		if length < 0 || at+8+length > len(data) {
			break
		}

		chunk := data[at+8 : at+8+length]

		switch string(data[at : at+4]) {
		case "ICCP":
			if len(chunk) <= maximumICCBytes {
				profile = chunk
			}
		case "EXIF":
			orientation = exifOrientation(chunk)
		}

		at += 8 + length + length%2
	}

	return orientation, profile
}

type isoBox struct {
	kind    string
	payload []byte
}

func isoBoxes(data []byte) []isoBox {
	var boxes []isoBox

	for at := 0; at+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[at:]))
		header := 8

		switch size {
		case 0:
			size = len(data) - at
		case 1:
			if at+16 > len(data) {
				return boxes
			}

			large := binary.BigEndian.Uint64(data[at+8:])
			if large > uint64(len(data)-at) {
				return boxes
			}

			size, header = int(large), 16 //nolint:gosec // bounded above by the remaining input
		}

		if size < header || at+size > len(data) {
			return boxes
		}

		boxes = append(boxes, isoBox{kind: string(data[at+4 : at+8]), payload: data[at+header : at+size]})
		at += size
	}

	return boxes
}

func findBox(boxes []isoBox, kind string) []byte {
	for _, box := range boxes {
		if box.kind == kind {
			return box.payload
		}
	}

	return nil
}

// heifColor reads the colr property associated with the primary item of a
// HEIF file such as AVIF.
func heifColor(data []byte) ColorDescription {
	meta := findBox(isoBoxes(data), "meta")
	if len(meta) < 4 {
		return ColorDescription{}
	}

	children := isoBoxes(meta[4:])
	primary, ok := primaryItem(findBox(children, "pitm"))

	iprp := isoBoxes(findBox(children, "iprp"))
	properties := isoBoxes(findBox(iprp, "ipco"))

	indices := associatedProperties(findBox(iprp, "ipma"), primary)
	if !ok || indices == nil {
		// Without the associations, the first color property is the best
		// available reading.
		indices = make([]int, len(properties))
		for index := range properties {
			indices[index] = index + 1
		}
	}

	for _, index := range indices {
		if index < 1 || index > len(properties) || properties[index-1].kind != "colr" {
			continue
		}

		if description, ok := colrDescription(properties[index-1].payload); ok {
			return description
		}
	}

	return ColorDescription{}
}

func primaryItem(pitm []byte) (uint32, bool) {
	switch {
	case len(pitm) >= 6 && pitm[0] == 0:
		return uint32(binary.BigEndian.Uint16(pitm[4:])), true
	case len(pitm) >= 8:
		return binary.BigEndian.Uint32(pitm[4:]), true
	default:
		return 0, false
	}
}

// associatedProperties returns the one-based ipco indices associated with an
// item, in the order the file lists them.
func associatedProperties(ipma []byte, item uint32) []int {
	if len(ipma) < 8 {
		return nil
	}

	version, wide := ipma[0], ipma[3]&1 == 1
	count := binary.BigEndian.Uint32(ipma[4:])
	at := 8

	for range count {
		var id uint32

		if version < 1 {
			if at+2 > len(ipma) {
				return nil
			}

			id, at = uint32(binary.BigEndian.Uint16(ipma[at:])), at+2
		} else {
			if at+4 > len(ipma) {
				return nil
			}

			id, at = binary.BigEndian.Uint32(ipma[at:]), at+4
		}

		if at+1 > len(ipma) {
			return nil
		}

		associations := int(ipma[at])
		at++

		var indices []int

		for range associations {
			if wide {
				if at+2 > len(ipma) {
					return nil
				}

				indices, at = append(indices, int(binary.BigEndian.Uint16(ipma[at:])&0x7fff)), at+2
			} else {
				if at+1 > len(ipma) {
					return nil
				}

				indices, at = append(indices, int(ipma[at]&0x7f)), at+1
			}
		}

		if id == item {
			return indices
		}
	}

	return nil
}

func colrDescription(colr []byte) (ColorDescription, bool) {
	if len(colr) < 4 {
		return ColorDescription{}, false
	}

	switch string(colr[:4]) {
	case "nclx":
		if len(colr) < 11 {
			return ColorDescription{}, false
		}

		return ColorDescription{CICP: &CICP{
			Primaries: binary.BigEndian.Uint16(colr[4:]),
			Transfer:  binary.BigEndian.Uint16(colr[6:]),
			Matrix:    binary.BigEndian.Uint16(colr[8:]),
			FullRange: colr[10]&0x80 != 0,
		}}, true
	case "prof", "rICC":
		if len(colr) > 4+maximumICCBytes {
			return ColorDescription{}, false
		}

		return ColorDescription{ICC: colr[4:]}, true
	default:
		return ColorDescription{}, false
	}
}
