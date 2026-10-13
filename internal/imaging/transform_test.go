package imaging

import (
	"image"
	"image/color"
	"testing"
)

func nrgbaAt(t *testing.T, img image.Image, x int) color.NRGBA {
	t.Helper()

	pixel, ok := color.NRGBAModel.Convert(img.At(x, 0)).(color.NRGBA)
	if !ok {
		t.Fatalf("pixel %d,0 converts to %T, not NRGBA", x, img.At(x, 0))
	}

	return pixel
}

func TestCanonicalPassesThroughAnSRGBImage(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	source.SetNRGBA(1, 1, color.NRGBA{R: 200, G: 100, B: 50, A: 200})

	got := Canonical(Decoded{Image: source, Orientation: 1})

	if got != image.Image(source) {
		t.Fatal("sRGB image was reprocessed")
	}
}

func TestCanonicalAppliesOrientation(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	source.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})

	got := Canonical(Decoded{Image: source, Orientation: 6})

	if got.Bounds().Dx() != 3 || got.Bounds().Dy() != 4 {
		t.Fatalf("oriented bounds = %v", got.Bounds())
	}

	if pixel := nrgbaAt(t, got, 2); pixel.R != 255 {
		t.Fatalf("corner pixel = %#v", pixel)
	}
}

// PQ content is absolute luminance: a 10 000-nit peak white rolls off just
// under SDR white rather than being pasted unchanged or clipped flat.
func TestCanonicalToneMapsPQ(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 2))

	for y := range 2 {
		for x := range 2 {
			source.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}

	got := Canonical(Decoded{
		Image:       source,
		Orientation: 1,
		Color:       ColorDescription{CICP: &CICP{Primaries: 9, Transfer: 16, FullRange: true}},
	})

	pixel := nrgbaAt(t, got, 0)
	if pixel.R < 220 || pixel.R == 255 || pixel.G != pixel.R || pixel.B != pixel.R {
		t.Fatalf("PQ white = %#v, wanted a near-white gray", pixel)
	}
}

// Diffuse white near 203 nits lands around the middle of the SDR range.
func TestCanonicalToneMapsPQDiffuseWhite(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 2))

	for y := range 2 {
		for x := range 2 {
			source.SetNRGBA(x, y, color.NRGBA{R: 88, G: 88, B: 88, A: 255})
		}
	}

	got := Canonical(Decoded{
		Image:       source,
		Orientation: 1,
		Color:       ColorDescription{CICP: &CICP{Primaries: 9, Transfer: 16, FullRange: true}},
	})

	pixel := nrgbaAt(t, got, 0)
	if pixel.R == 0 || pixel.R > 240 {
		t.Fatalf("PQ mid gray = %#v, wanted a visible compressed tone", pixel)
	}
}

// An HLG signal at scene peak maps to SDR white through the OOTF.
func TestCanonicalMapsHLGPeakToWhite(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 2))

	for y := range 2 {
		for x := range 2 {
			source.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}

	got := Canonical(Decoded{
		Image:       source,
		Orientation: 1,
		Color:       ColorDescription{CICP: &CICP{Primaries: 9, Transfer: 18, FullRange: true}},
	})

	pixel := nrgbaAt(t, got, 0)
	if pixel.R < 240 || pixel.G < 240 || pixel.B < 240 {
		t.Fatalf("HLG white = %#v, wanted near SDR white", pixel)
	}
}

// A wide-gamut source shifts its channels when it lands in sRGB.
func TestCanonicalConvertsAWideGamutSource(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 2))

	for y := range 2 {
		for x := range 2 {
			source.SetNRGBA(x, y, color.NRGBA{R: 64, G: 192, B: 32, A: 255})
		}
	}

	got := Canonical(Decoded{
		Image:       source,
		Orientation: 1,
		Color:       ColorDescription{CICP: &CICP{Primaries: 9, Transfer: 13, FullRange: true}},
	})

	if pixel := nrgbaAt(t, got, 0); pixel == (color.NRGBA{R: 64, G: 192, B: 32, A: 255}) {
		t.Fatal("wide-gamut pixel passed through unconverted")
	}
}

func TestCanonicalKeepsAlpha(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	source.SetNRGBA(0, 0, color.NRGBA{R: 128, G: 64, B: 32, A: 128})

	got := Canonical(Decoded{
		Image:       source,
		Orientation: 1,
		Color:       ColorDescription{CICP: &CICP{Primaries: 9, Transfer: 8, FullRange: true}},
	})

	if pixel := nrgbaAt(t, got, 0); pixel.A != 128 {
		t.Fatalf("alpha = %d, want 128", pixel.A)
	}
}

// Reducing first must show the same pixels as orienting the whole image and
// then cropping and resampling it. Whole-number factors keep every averaged
// area aligned, so the results agree exactly.
func TestReduceMatchesOrientingTheWholeImageFirst(t *testing.T) {
	source := image.NewNRGBA(image.Rect(10, 20, 18, 32))
	for y := source.Rect.Min.Y; y < source.Rect.Max.Y; y++ {
		for x := source.Rect.Min.X; x < source.Rect.Max.X; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 13), G: uint8(y * 7), B: uint8(x * y), A: 255}) //nolint:gosec // test colors wrap by design
		}
	}

	for orientation := 1; orientation <= 8; orientation++ {
		decoded := Decoded{Image: source, Orientation: orientation}
		width, height := decoded.Size()
		whole := Canonical(decoded)

		for _, region := range []image.Rectangle{
			image.Rect(0, 0, width, height),
			image.Rect(2, 2, width-2, height-4),
		} {
			for _, factor := range []int{1, 2} {
				crop, ok := whole.(croppable)
				if !ok {
					t.Fatalf("orientation %d produced an image that cannot be cropped", orientation)
				}

				want := Resample(crop.SubImage(region.Add(whole.Bounds().Min)), region.Dx()/factor, region.Dy()/factor)
				got := Reduce(decoded, region, region.Dx()/factor, region.Dy()/factor)

				if got.Bounds().Size() != want.Bounds().Size() {
					t.Fatalf("orientation %d region %v factor %d: size %v, want %v", orientation, region, factor, got.Bounds().Size(), want.Bounds().Size())
				}

				for y := range want.Bounds().Dy() {
					for x := range want.Bounds().Dx() {
						actual := color.NRGBAModel.Convert(got.At(got.Bounds().Min.X+x, got.Bounds().Min.Y+y))
						if expected := want.NRGBAAt(x, y); actual != expected {
							t.Fatalf("orientation %d region %v factor %d at %d,%d = %#v, want %#v", orientation, region, factor, x, y, actual, expected)
						}
					}
				}
			}
		}
	}
}
