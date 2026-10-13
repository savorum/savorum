package imaging

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestDeriveImageIsDeterministicBoundedAndPNG(t *testing.T) {
	sourceImage := image.NewNRGBA(image.Rect(0, 0, 800, 400))
	sourceImage.Set(0, 0, color.NRGBA{R: 255, A: 255})

	var source bytes.Buffer
	if err := png.Encode(&source, sourceImage); err != nil {
		t.Fatal(err)
	}

	first, err := DeriveImage(source.Bytes(), DerivativeCard)
	if err != nil {
		t.Fatal(err)
	}

	second, err := DeriveImage(source.Bytes(), DerivativeCard)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first, second) {
		t.Fatal("derivative bytes are not deterministic")
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}

	if format != "png" || config.Width != 640 || config.Height != 320 {
		t.Fatalf("derivative = %s %dx%d", format, config.Width, config.Height)
	}
}

func TestDeriveImageHonorsJPEGExifOrientation(t *testing.T) {
	sourceImage := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	sourceImage.Set(0, 0, color.NRGBA{R: 255, A: 255})
	sourceImage.Set(1, 0, color.NRGBA{B: 255, A: 255})

	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, sourceImage, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}

	derived, err := DeriveImage(withExifOrientation(encoded.Bytes(), 6), DerivativeReader)
	if err != nil {
		t.Fatal(err)
	}

	config, _, err := image.DecodeConfig(bytes.NewReader(derived))
	if err != nil {
		t.Fatal(err)
	}

	if config.Width != 1 || config.Height != 2 {
		t.Fatalf("oriented dimensions = %dx%d", config.Width, config.Height)
	}
}

func TestOrientMapsEveryExifOrientation(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 3))

	for y := range 3 {
		for x := range 2 {
			source.Set(x, y, color.NRGBA{R: uint8(1 + x + y*2), A: 255})
		}
	}

	for orientation := 1; orientation <= 8; orientation++ {
		got := orient(source, orientation)

		width, height := 2, 3
		if orientation >= 5 {
			width, height = 3, 2
		}

		if got.Bounds().Dx() != width || got.Bounds().Dy() != height {
			t.Fatalf("orientation %d dimensions=%v", orientation, got.Bounds())
		}

		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				sx, sy := expectedExifSource(orientation, x, y)
				want := color.NRGBAModel.Convert(source.At(sx, sy))

				actual := color.NRGBAModel.Convert(got.At(x, y))
				if actual != want {
					t.Fatalf("orientation %d at %d,%d = %#v want %#v", orientation, x, y, actual, want)
				}
			}
		}
	}
}

// Coordinates from the EXIF orientation table, expressed independently of the
// implementation under test. The source is two columns by three rows.
func expectedExifSource(orientation, x, y int) (int, int) {
	switch orientation {
	case 1:
		return x, y
	case 2:
		return 1 - x, y
	case 3:
		return 1 - x, 2 - y
	case 4:
		return x, 2 - y
	case 5:
		return y, x
	case 6:
		return y, 2 - x
	case 7:
		return 1 - y, 2 - x
	case 8:
		return 1 - y, x
	default:
		panic("invalid orientation")
	}
}

func TestDeriveImageRejectsHostileDimensionsAndNeverRoundsToZero(t *testing.T) {
	// DecodeConfig recognizes the IHDR before allocating pixel storage.
	header := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 13, 'I', 'H', 'D', 'R', 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 0, 0, 0, 0}
	if _, err := DeriveImage(header, DerivativeThumb); err == nil {
		t.Fatal("oversized image accepted")
	}

	imageOne := image.NewNRGBA(image.Rect(0, 0, 1, 1000))

	var one bytes.Buffer
	if err := png.Encode(&one, imageOne); err != nil {
		t.Fatal(err)
	}

	derived, err := DeriveImage(one.Bytes(), DerivativeThumb)
	if err != nil {
		t.Fatal(err)
	}

	config, _, err := image.DecodeConfig(bytes.NewReader(derived))
	if err != nil {
		t.Fatal(err)
	}

	if config.Width != 1 || config.Height != 240 {
		t.Fatalf("thin derivative = %dx%d", config.Width, config.Height)
	}
}

func withExifOrientation(jpegBytes []byte, orientation uint16) []byte {
	tiff := make([]byte, 26)
	copy(tiff, []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1})
	binary.BigEndian.PutUint16(tiff[18:20], orientation)
	app := append([]byte{'E', 'x', 'i', 'f', 0, 0}, tiff...)
	segment := []byte{0xff, 0xe1, byte((len(app) + 2) >> 8), byte(len(app) + 2)} //nolint:gosec // a fixed 32-byte fixture segment, whose length is known here

	return append(append(append([]byte{}, jpegBytes[:2]...), segment...), append(app, jpegBytes[2:]...)...)
}

func TestDeriveImageRejectsUnknownPresetAndInvalidInput(t *testing.T) {
	if _, err := DeriveImage([]byte("not an image"), DerivativeCard); err == nil {
		t.Fatal("invalid image accepted")
	}

	if _, err := DeriveImage([]byte("not an image"), DerivativePreset("original")); err == nil {
		t.Fatal("unknown preset accepted")
	}
}
