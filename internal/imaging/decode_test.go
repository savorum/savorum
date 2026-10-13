package imaging

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/h265/heic"
	"github.com/gen2brain/jpegxl"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

func tinyImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 12))

	for y := range 12 {
		for x := range 16 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 16), G: uint8(y * 20), B: 128, A: 255})
		}
	}

	return img
}

func encodedFixtures(t *testing.T) map[Format][]byte {
	t.Helper()

	source := tinyImage()
	fixtures := map[Format][]byte{}

	encode := func(format Format, do func(*bytes.Buffer) error) {
		t.Helper()

		var out bytes.Buffer
		if err := do(&out); err != nil {
			t.Fatalf("encode %s fixture: %v", format, err)
		}

		fixtures[format] = out.Bytes()
	}

	encode(JPEG, func(out *bytes.Buffer) error { return jpeg.Encode(out, source, &jpeg.Options{Quality: 90}) })
	encode(PNG, func(out *bytes.Buffer) error { return png.Encode(out, source) })
	encode(BMP, func(out *bytes.Buffer) error { return bmp.Encode(out, source) })
	encode(TIFF, func(out *bytes.Buffer) error { return tiff.Encode(out, source, nil) })
	encode(AVIF, func(out *bytes.Buffer) error { return avif.Encode(out, source) })
	encode(JPEGXL, func(out *bytes.Buffer) error { return jpegxl.Encode(out, source, jpegxl.Options{Lossless: true}) })
	encode(HEIF, func(out *bytes.Buffer) error { return heic.Encode(out, source) })

	return fixtures
}

func TestDecodeReadsEverySupportedFormat(t *testing.T) {
	for format, data := range encodedFixtures(t) {
		decoded, err := Decode(data, DerivativeLimits)
		if err != nil {
			t.Fatalf("decode %s: %v", format, err)
		}

		if decoded.Format != format {
			t.Fatalf("format = %s, want %s", decoded.Format, format)
		}

		if decoded.Orientation != 1 {
			t.Fatalf("%s orientation = %d, want 1", format, decoded.Orientation)
		}

		if decoded.Image.Bounds().Dx() != 16 || decoded.Image.Bounds().Dy() != 12 {
			t.Fatalf("%s bounds = %v", format, decoded.Image.Bounds())
		}
	}
}

func TestDeriveImageReadsEverySupportedFormat(t *testing.T) {
	for format, data := range encodedFixtures(t) {
		derived, err := DeriveImage(data, DerivativeThumb)
		if err != nil {
			t.Fatalf("derive %s: %v", format, err)
		}

		config, _, err := image.DecodeConfig(bytes.NewReader(derived))
		if err != nil {
			t.Fatalf("derived %s does not decode: %v", format, err)
		}

		if config.Width != 16 || config.Height != 12 {
			t.Fatalf("derived %s = %dx%d, want the fixture's size unchanged", format, config.Width, config.Height)
		}
	}
}

func TestDetectNamesEverySupportedFormat(t *testing.T) {
	want := map[Format]string{
		JPEG: "image/jpeg", PNG: "image/png", BMP: "image/bmp",
		TIFF: "image/tiff", AVIF: "image/avif", JPEGXL: "image/jxl", HEIF: "image/heic",
	}

	for format, data := range encodedFixtures(t) {
		if got := Detect(data); got != want[format] {
			t.Fatalf("Detect(%s) = %s, want %s", format, got, want[format])
		}
	}
}

func TestDecodableAcceptsTheSupportedMediaTypes(t *testing.T) {
	for _, mediaType := range []string{
		"image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp",
		"image/tiff", "image/heic", "image/heif", "image/avif", "image/jxl",
		"image/jpeg; charset=binary",
	} {
		if !Decodable(mediaType) {
			t.Fatalf("Decodable(%q) = false", mediaType)
		}
	}

	for _, mediaType := range []string{"", "image/svg+xml", "text/html", "application/pdf", "nonsense"} {
		if Decodable(mediaType) {
			t.Fatalf("Decodable(%q) = true", mediaType)
		}
	}
}

func TestMeasureEnforcesEveryLimitBeforeDecoding(t *testing.T) {
	fixtures := encodedFixtures(t)
	pngData := fixtures[PNG]

	if _, _, err := Measure(pngData, Limits{Bytes: int64(len(pngData) - 1)}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("byte limit: %v", err)
	}

	if _, _, err := Measure(pngData, Limits{Width: 15}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("width limit: %v", err)
	}

	if _, _, err := Measure(pngData, Limits{Height: 11}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("height limit: %v", err)
	}

	if _, _, err := Measure(pngData, Limits{Pixels: 191}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("pixel limit: %v", err)
	}

	config, format, err := Measure(pngData, Limits{Width: 16, Height: 12, Pixels: 192})
	if err != nil || format != PNG || config.Width != 16 || config.Height != 12 {
		t.Fatalf("measure at limits = %v %s %dx%d", err, format, config.Width, config.Height)
	}
}

// A compressed stream a handful of bytes long may declare a frame far beyond
// any safe allocation; the header check must reject it without decoding.
func TestDecodeRejectsABombOnItsHeader(t *testing.T) {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 65535)
	binary.BigEndian.PutUint32(ihdr[4:], 65535)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 6 // RGBA

	var header bytes.Buffer
	header.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	header.Write(pngChunk("IHDR", ihdr))

	if _, err := Decode(header.Bytes(), DerivativeLimits); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("65535x65535 header: %v", err)
	}
}

// pngChunk assembles one well-formed PNG chunk.
func pngChunk(kind string, data []byte) []byte {
	chunk := make([]byte, 12+len(data))
	binary.BigEndian.PutUint32(chunk, uint32(len(data))) //nolint:gosec // test fixture chunks are small
	copy(chunk[4:], kind)
	copy(chunk[8:], data)
	binary.BigEndian.PutUint32(chunk[8+len(data):], crc32.ChecksumIEEE(chunk[4:8+len(data)]))

	return chunk
}

func TestDecodeDistinguishesFailures(t *testing.T) {
	if _, err := Decode([]byte("not an image"), DerivativeLimits); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unrecognized data: %v", err)
	}

	broken := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 13, 'I', 'H', 'D', 'R', 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0}
	if _, err := Decode(broken, DerivativeLimits); !errors.Is(err, ErrInvalid) {
		t.Fatalf("truncated png: %v", err)
	}
}

// The orientation and color a file declares reach the caller alongside the
// pixels so the pipeline can correct both.
func TestDecodeReportsExifOrientationAndColor(t *testing.T) {
	source := tinyImage()

	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, source, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}

	data := withExifOrientation(encoded.Bytes(), 6)

	decoded, err := Decode(data, DerivativeLimits)
	if err != nil {
		t.Fatal(err)
	}

	if decoded.Orientation != 6 {
		t.Fatalf("orientation = %d, want 6", decoded.Orientation)
	}
}

func TestDecodeReportsPNGColor(t *testing.T) {
	source := tinyImage()

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}

	// Insert a cICP chunk naming BT.2020 primaries with the PQ transfer
	// ahead of the first IDAT.
	chunk := pngChunk("cICP", []byte{9, 16, 0, 1})

	data := encoded.Bytes()

	idat := bytes.Index(data, []byte("IDAT"))
	if idat < 4 {
		t.Fatal("fixture has no IDAT")
	}

	at := idat - 4
	data = append(append(append([]byte{}, data[:at]...), chunk...), data[at:]...)

	decoded, err := Decode(data, DerivativeLimits)
	if err != nil {
		t.Fatal(err)
	}

	if decoded.Color.CICP == nil || decoded.Color.CICP.Primaries != 9 || decoded.Color.CICP.Transfer != 16 {
		t.Fatalf("color = %+v", decoded.Color.CICP)
	}

	if !ResolveColorSpace(decoded.Color).HDR() {
		t.Fatal("PQ cICP did not resolve to an HDR space")
	}
}

func TestTIFFHeaderReadsOrientation(t *testing.T) {
	// A little-endian TIFF header whose first directory carries one
	// orientation entry rotated 90 degrees clockwise.
	data := []byte{'I', 'I', 42, 0, 8, 0, 0, 0}
	entry := []byte{0x12, 0x01, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0}
	data = append(append(append(data, 1, 0), entry...), 0, 0, 0, 0)

	if got := readIFD0(data).orientation(); got != 6 {
		t.Fatalf("orientation = %d, want 6", got)
	}
}

func TestISOBMFFReadsNCLXColor(t *testing.T) {
	// ftyp + meta{pitm,iprp{ipco{colr=nclx},ipma}} describing BT.2020 PQ.
	box := func(kind string, payload []byte) []byte {
		out := make([]byte, 8+len(payload))
		binary.BigEndian.PutUint32(out, uint32(len(out))) //nolint:gosec // test fixture boxes are small
		copy(out[4:], kind)
		copy(out[8:], payload)

		return out
	}

	colr := box("colr", []byte{'n', 'c', 'l', 'x', 0, 9, 0, 16, 0, 0, 0x80})
	ipco := box("ipco", colr)
	ipma := box("ipma", []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 1, 1, 0x81})
	iprp := box("iprp", append(ipco, ipma...))
	pitm := box("pitm", []byte{0, 0, 0, 0, 0, 1})
	meta := box("meta", append([]byte{0, 0, 0, 0}, append(pitm, iprp...)...))
	data := append(box("ftyp", []byte("avif00000000avif")), meta...)

	description := heifColor(data)
	if description.CICP == nil || description.CICP.Primaries != 9 || description.CICP.Transfer != 16 || !description.CICP.FullRange {
		t.Fatalf("heifColor = %+v", description.CICP)
	}
}
