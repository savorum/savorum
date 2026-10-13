package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

type genericImage struct{ image.Image }

func TestResampleFastPathsPreservePixels(t *testing.T) {
	bounds := image.Rect(3, 5, 70, 42)
	nrgba := image.NewNRGBA(bounds)
	rgba := image.NewRGBA(bounds)
	gray := image.NewGray(bounds)

	ycbcr := image.NewYCbCr(bounds, image.YCbCrSubsampleRatio420)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			value := color.NRGBA{R: byte((x * 3) & 255), G: byte((y * 5) & 255), B: byte((x + y) & 255), A: byte((x * y) & 255)}
			nrgba.SetNRGBA(x, y, value)
			rgba.Set(x, y, value)
			gray.Set(x, y, value)
			ycbcr.Y[ycbcr.YOffset(x, y)] = byte((x * y) & 255)
			ycbcr.Cb[ycbcr.COffset(x, y)] = byte((x * 2) & 255)
			ycbcr.Cr[ycbcr.COffset(x, y)] = byte((y * 3) & 255)
		}
	}

	for _, source := range []image.Image{nrgba, rgba, gray, ycbcr} {
		actual, expected := Resample(source, 13, 9), Resample(genericImage{source}, 13, 9)
		if !bytes.Equal(actual.Pix, expected.Pix) {
			t.Fatalf("pixel drift for %T", source)
		}
	}
}

func BenchmarkResampleCameraImage(b *testing.B) {
	source := image.NewYCbCr(image.Rect(0, 0, 4000, 3000), image.YCbCrSubsampleRatio420)
	for _, variant := range []struct {
		name  string
		image image.Image
	}{
		{"concrete", source}, {"interface", genericImage{source}},
	} {
		b.Run(variant.name, func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				Resample(variant.image, 640, 480)
			}
		})
	}
}

// A pattern finer than the output grid survives as its average tone rather
// than aliasing to pure black or white.
func TestResampleAveragesDetailFinerThanTheOutputGrid(t *testing.T) {
	// Alternating one-pixel columns, kept fractionally transparent so the
	// derivative is encoded as lossless PNG and the assertion reads the
	// resampler rather than JPEG's own blurring.
	source := image.NewNRGBA(image.Rect(0, 0, 640, 2))

	for y := range 2 {
		for x := range 640 {
			shade := uint8(0)
			if x%2 == 1 {
				shade = 255
			}

			source.Set(x, y, color.NRGBA{R: shade, G: shade, B: shade, A: 254})
		}
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}

	derived, err := DeriveImage(encoded.Bytes(), DerivativeThumb)
	if err != nil {
		t.Fatal(err)
	}

	decoded, format, err := image.Decode(bytes.NewReader(derived))
	if err != nil {
		t.Fatal(err)
	}

	if format != "png" {
		t.Fatalf("derivative format = %s, wanted a lossless one for this fixture", format)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 240 {
		t.Fatalf("derivative width = %d", bounds.Dx())
	}

	var total int

	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		shade, _, _, _ := decoded.At(x, bounds.Min.Y).RGBA()
		level := int(shade >> 8)
		// Two or three source columns fold into each output pixel, so every
		// one of them is a mixture. Point sampling produces 0 or 255 here.
		if level < 32 || level > 223 {
			t.Fatalf("column %d = %d: detail was sampled, not averaged", x, level)
		}

		total += level
	}

	if mean := total / bounds.Dx(); mean < 112 || mean > 143 {
		t.Fatalf("mean shade = %d, wanted the mid tone of black and white columns", mean)
	}
}

// A reduction must not invent a tone the source never had. Averaging a flat
// colour returns that colour.
func TestResampleKeepsAFlatColourFlat(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 100, 60))

	for y := range 60 {
		for x := range 100 {
			source.Set(x, y, color.NRGBA{R: 20, G: 140, B: 90, A: 255})
		}
	}

	target := Resample(source, 10, 6)

	for y := range 6 {
		for x := range 10 {
			if got := target.NRGBAAt(x, y); got != (color.NRGBA{R: 20, G: 140, B: 90, A: 255}) {
				t.Fatalf("at %d,%d = %#v", x, y, got)
			}
		}
	}
}

// Nothing is reduced when the output grid matches the source, so the pixels
// come back exactly as they were rather than round-tripped through
// premultiplied alpha.
func TestResampleAtTheSameSizeIsACopy(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 4, 3))

	for y := range 3 {
		for x := range 4 {
			// Straight-alpha values a premultiplied round trip would move.
			source.SetNRGBA(x, y, color.NRGBA{R: byte(x) * 60, G: byte(y) * 80, B: 7, A: 1 + byte(x)})
		}
	}

	target := Resample(source, 4, 3)

	for y := range 3 {
		for x := range 4 {
			if got, want := target.NRGBAAt(x, y), source.NRGBAAt(x, y); got != want {
				t.Fatalf("at %d,%d = %#v want %#v", x, y, got, want)
			}
		}
	}
}

// A fully transparent region has no colour to average, and dividing by its
// absent alpha would be a division by zero.
func TestResampleHandlesFullyTransparentRegions(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 8, 8))

	target := Resample(source, 2, 2)

	for y := range 2 {
		for x := range 2 {
			if got := target.NRGBAAt(x, y); got != (color.NRGBA{}) {
				t.Fatalf("at %d,%d = %#v", x, y, got)
			}
		}
	}
}
