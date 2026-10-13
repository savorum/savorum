package imaging

import (
	"bytes"
	"encoding/binary"
	"mime"
	"net/http"
	"slices"
	"strings"
)

// Format is an image file format this package decodes.
type Format string

// The decodable formats.
const (
	JPEG   Format = "jpeg"
	PNG    Format = "png"
	GIF    Format = "gif"
	WebP   Format = "webp"
	BMP    Format = "bmp"
	TIFF   Format = "tiff"
	HEIF   Format = "heif"
	AVIF   Format = "avif"
	JPEGXL Format = "jxl"
)

var mediaTypeFormats = map[string]Format{
	"image/jpeg": JPEG,
	"image/png":  PNG,
	"image/gif":  GIF,
	"image/webp": WebP,
	"image/bmp":  BMP,
	"image/tiff": TIFF,
	"image/heic": HEIF,
	"image/heif": HEIF,
	"image/avif": AVIF,
	"image/jxl":  JPEGXL,
}

// Decodable reports whether a media type names a format this package decodes.
func Decodable(mediaType string) bool {
	parsed, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		return false
	}

	_, ok := mediaTypeFormats[strings.ToLower(parsed)]

	return ok
}

// Detect names the media type of data from its leading bytes. It recognizes
// every decodable format by signature and falls back to the standard library's
// content sniffing for anything else.
func Detect(data []byte) string {
	if mediaType, _ := sniff(data); mediaType != "" {
		return mediaType
	}

	detected, _, err := mime.ParseMediaType(http.DetectContentType(data))
	if err != nil {
		return "application/octet-stream"
	}

	return strings.ToLower(detected)
}

// sniff returns the media type and format of a decodable image, or empty
// values when data does not start like one.
func sniff(data []byte) (string, Format) {
	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg", JPEG
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", PNG
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif", GIF
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp", WebP
	case len(data) >= 14 && string(data[:2]) == "BM":
		return "image/bmp", BMP
	case bytes.HasPrefix(data, []byte("II*\x00")), bytes.HasPrefix(data, []byte("MM\x00*")):
		return "image/tiff", TIFF
	case bytes.HasPrefix(data, []byte{0xff, 0x0a}), bytes.HasPrefix(data, []byte("\x00\x00\x00\x0cJXL \r\n\x87\n")):
		return "image/jxl", JPEGXL
	}

	return sniffISOBMFF(data)
}

// sniffISOBMFF reads the brands of an ISO base media file. AVIF and HEIC are
// both HEIF files; the brands say which codec the image items use.
func sniffISOBMFF(data []byte) (string, Format) {
	if len(data) < 16 || string(data[4:8]) != "ftyp" {
		return "", ""
	}

	size := int(binary.BigEndian.Uint32(data[:4]))
	if size < 16 || size > len(data) {
		size = min(len(data), 256)
	}

	brands := []string{string(data[8:12])}
	for offset := 16; offset+4 <= size; offset += 4 {
		brands = append(brands, string(data[offset:offset+4]))
	}

	has := func(names ...string) bool {
		for _, name := range names {
			if slices.Contains(brands, name) {
				return true
			}
		}

		return false
	}

	switch {
	case has("avif", "avis"):
		return "image/avif", AVIF
	case has("heic", "heix", "heim", "heis", "hevc", "hevx"):
		return "image/heic", HEIF
	case has("mif1", "msf1"):
		return "image/heif", HEIF
	default:
		return "", ""
	}
}
