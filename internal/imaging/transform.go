package imaging

import (
	"image"
	"image/draw"
	"math"
)

// srgbWhiteNits is the reference white of an SDR display. PQ luminance is
// compressed toward it; HLG display light already peaks there.
const srgbWhiteNits = 203.0

// Canonical puts a decoded image in Savorum's output form: oriented sRGB at
// standard dynamic range. An image already in that form is returned
// untouched; any other source space is tone-mapped and converted.
func Canonical(decoded Decoded) image.Image {
	oriented := orient(decoded.Image, decoded.Orientation)

	space := ResolveColorSpace(decoded.Color)
	if space.srgb {
		return oriented
	}

	return convert(oriented, space)
}

// Size reports the image's width and height once its orientation is applied.
func (d Decoded) Size() (int, int) {
	bounds := d.Image.Bounds()
	if swapsSides(d.Orientation) {
		return bounds.Dy(), bounds.Dx()
	}

	return bounds.Dx(), bounds.Dy()
}

// Reduce produces the canonical form of region, a rectangle in oriented
// coordinates, resampled to width by height. It resamples the stored pixels
// first, so orientation and color conversion cost only as much as the output.
func Reduce(decoded Decoded, region image.Rectangle, width, height int) image.Image {
	source, ok := decoded.Image.(croppable)
	if !ok {
		bounds := decoded.Image.Bounds()
		copied := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
		draw.Draw(copied, copied.Bounds(), decoded.Image, bounds.Min, draw.Src)
		source = copied
	}

	cropped := source.SubImage(storedRegion(region, decoded.Orientation, source.Bounds()))

	if swapsSides(decoded.Orientation) {
		width, height = height, width
	}

	return Canonical(Decoded{Image: Resample(cropped, width, height), Orientation: decoded.Orientation, Color: decoded.Color})
}

type croppable interface {
	image.Image
	SubImage(image.Rectangle) image.Image
}

func swapsSides(orientation int) bool { return orientation >= 5 && orientation <= 8 }

// storedRegion maps an oriented rectangle onto the stored pixels it shows,
// through the same pixel mapping orient uses.
func storedRegion(region image.Rectangle, orientation int, bounds image.Rectangle) image.Rectangle {
	if orientation < 2 || orientation > 8 {
		return region.Add(bounds.Min).Intersect(bounds)
	}

	first := storedPixel(region.Min, orientation, bounds)
	last := storedPixel(region.Max.Sub(image.Pt(1, 1)), orientation, bounds)

	return image.Rect(min(first.X, last.X), min(first.Y, last.Y), max(first.X, last.X)+1, max(first.Y, last.Y)+1).Intersect(bounds)
}

func storedPixel(point image.Point, orientation int, b image.Rectangle) image.Point {
	x, y := point.X, point.Y

	switch orientation {
	case 2:
		return image.Pt(b.Max.X-1-x, b.Min.Y+y)
	case 3:
		return image.Pt(b.Max.X-1-x, b.Max.Y-1-y)
	case 4:
		return image.Pt(b.Min.X+x, b.Max.Y-1-y)
	case 5:
		return image.Pt(b.Min.X+y, b.Min.Y+x)
	case 6:
		return image.Pt(b.Min.X+y, b.Max.Y-1-x)
	case 7:
		return image.Pt(b.Max.X-1-y, b.Max.Y-1-x)
	case 8:
		return image.Pt(b.Max.X-1-y, b.Min.Y+x)
	default:
		return image.Pt(b.Min.X+x, b.Min.Y+y)
	}
}

// convert maps every pixel through the source space's transfer curves into
// XYZ, tone-maps the luminance when the source is HDR, and lands in encoded
// sRGB.
func convert(source image.Image, space ColorSpace) *image.NRGBA {
	bounds := source.Bounds()
	target := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))

	var linear [3][]float64

	for channel := range 3 {
		table := make([]float64, 1<<16)
		for value := range table {
			table[value] = space.transfer[channel](float64(value) / 65535)
		}

		linear[channel] = table
	}

	encode := make([]uint8, 1<<16)
	for value := range encode {
		encode[value] = uint8(srgbEncode(float64(value)/65535)*255 + 0.5)
	}

	read := rgbaReader(source)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		row := (y - bounds.Min.Y) * target.Stride
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := read(x, y)
			if a == 0 {
				continue
			}

			// RGBA() is alpha-premultiplied; the transfer curves apply to the
			// straight channel values.
			if a != 0xffff {
				r, g, b = min(r*0xffff/a, 0xffff), min(g*0xffff/a, 0xffff), min(b*0xffff/a, 0xffff)
			}

			X, Y, Z := space.toXYZ.apply(linear[0][r], linear[1][g], linear[2][b])

			switch space.dynamic {
			case perceptualQuantizer:
				// Y is absolute nits. A Reinhard shoulder rolls highlights
				// off to SDR white while keeping hue by scaling all of XYZ:
				// output luminance is n/(1+n) for n = Y/reference white.
				if Y > 0 {
					scale := 1.0 / (srgbWhiteNits + Y)
					X, Y, Z = X*scale, Y*scale, Z*scale
				}
			case hybridLogGamma:
				// The OOTF turns scene light into display light with a 1.2
				// system gamma on luminance, scene peak mapping to SDR white.
				if Y > 0 {
					scale := math.Pow(Y, 0.2)
					X, Y, Z = X*scale, Y*scale, Z*scale
				}
			case standardRange:
			}

			sr, sg, sb := xyzToLinear.apply(X, Y, Z)

			pix := row + (x-bounds.Min.X)*4
			target.Pix[pix] = encode[clamp16(sr)]
			target.Pix[pix+1] = encode[clamp16(sg)]
			target.Pix[pix+2] = encode[clamp16(sb)]
			target.Pix[pix+3] = uint8(a >> 8) //nolint:gosec // a is bounded by 0xffff
		}
	}

	return target
}

func clamp16(value float64) int {
	switch {
	case value <= 0:
		return 0
	case value >= 1:
		return 0xffff
	default:
		return int(value*65535 + 0.5)
	}
}

// orient applies an Exif orientation, returning the source unchanged for
// values that describe no transform.
func orient(source image.Image, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return source
	}

	b := source.Bounds()
	w, h := b.Dx(), b.Dy()

	if swapsSides(orientation) {
		w, h = h, w
	}

	target := image.NewNRGBA(image.Rect(0, 0, w, h))
	stored, direct := source.(*image.NRGBA)

	for y := range h {
		for x := range w {
			point := storedPixel(image.Pt(x, y), orientation, b)
			if direct {
				copy(target.Pix[target.PixOffset(x, y):][:4], stored.Pix[stored.PixOffset(point.X, point.Y):])
				continue
			}

			target.Set(x, y, source.At(point.X, point.Y))
		}
	}

	return target
}

// opaque reports whether every pixel is fully covered.
func opaque(source image.Image) bool {
	if known, ok := source.(interface{ Opaque() bool }); ok {
		return known.Opaque()
	}

	for y := source.Bounds().Min.Y; y < source.Bounds().Max.Y; y++ {
		for x := source.Bounds().Min.X; x < source.Bounds().Max.X; x++ {
			_, _, _, a := source.At(x, y).RGBA()
			if a != 0xffff {
				return false
			}
		}
	}

	return true
}
