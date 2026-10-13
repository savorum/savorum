package imaging

// Presentation and print derivatives are reduced the same way.

import (
	"image"
	"image/color"
)

// Resample reduces an image to exactly width by height, averaging the source
// pixels that collapse into each new one.
//
// Averaging rather than sampling avoids aliasing. Integer arithmetic makes the
// output deterministic.
func Resample(source image.Image, width, height int) *image.NRGBA {
	bounds := source.Bounds()
	target := image.NewNRGBA(image.Rect(0, 0, width, height))
	read := rgbaReader(source)

	for y := range height {
		top := bounds.Min.Y + y*bounds.Dy()/height

		bottom := bounds.Min.Y + (y+1)*bounds.Dy()/height
		if bottom <= top {
			bottom = top + 1
		}

		for x := range width {
			left := bounds.Min.X + x*bounds.Dx()/width

			right := bounds.Min.X + (x+1)*bounds.Dx()/width
			if right <= left {
				right = left + 1
			}
			// Copy a lone source pixel, which premultiplied averaging could
			// shift by one.
			if right-left == 1 && bottom-top == 1 {
				target.Set(x, y, source.At(left, top))
				continue
			}

			target.SetNRGBA(x, y, averageArea(read, left, top, right, bottom))
		}
	}

	return target
}

func rgbaReader(source image.Image) func(int, int) (uint32, uint32, uint32, uint32) {
	switch source := source.(type) {
	case *image.YCbCr:
		return func(x, y int) (uint32, uint32, uint32, uint32) { return source.YCbCrAt(x, y).RGBA() }
	case *image.NRGBA:
		return func(x, y int) (uint32, uint32, uint32, uint32) { return source.NRGBAAt(x, y).RGBA() }
	case *image.RGBA:
		return func(x, y int) (uint32, uint32, uint32, uint32) { return source.RGBAAt(x, y).RGBA() }
	case *image.Gray:
		return func(x, y int) (uint32, uint32, uint32, uint32) { return source.GrayAt(x, y).RGBA() }
	case *image.NYCbCrA:
		return func(x, y int) (uint32, uint32, uint32, uint32) { return source.NYCbCrAAt(x, y).RGBA() }
	case *image.NRGBA64:
		return func(x, y int) (uint32, uint32, uint32, uint32) { return source.NRGBA64At(x, y).RGBA() }
	case *image.RGBA64:
		return func(x, y int) (uint32, uint32, uint32, uint32) { return source.RGBA64At(x, y).RGBA() }
	case *image.Gray16:
		return func(x, y int) (uint32, uint32, uint32, uint32) { return source.Gray16At(x, y).RGBA() }
	case *image.CMYK:
		return func(x, y int) (uint32, uint32, uint32, uint32) { return source.CMYKAt(x, y).RGBA() }
	default:
		return func(x, y int) (uint32, uint32, uint32, uint32) { return source.At(x, y).RGBA() }
	}
}

func averageArea(read func(int, int) (uint32, uint32, uint32, uint32), left, top, right, bottom int) color.NRGBA {
	var (
		totalR, totalG, totalB, totalA uint64
		count                          uint64
	)

	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			r, g, b, a := read(x, y)
			totalR += uint64(r)
			totalG += uint64(g)
			totalB += uint64(b)
			totalA += uint64(a)
			count++
		}
	}

	if count == 0 {
		return color.NRGBA{}
	}

	alpha := totalA / count
	if alpha == 0 {
		return color.NRGBA{}
	}
	// The averaged colour is premultiplied by its own alpha, so alpha is
	// divided back out before the pixel is written with straight alpha.
	unpremultiply := func(total uint64) uint8 {
		// min caps result at 0xffff, so the high byte fits in a uint8.
		result := min(total/count*0xffff/alpha, 0xffff)

		return uint8(result >> 8)
	}

	return color.NRGBA{
		R: unpremultiply(totalR),
		G: unpremultiply(totalG),
		B: unpremultiply(totalB),
		A: uint8(alpha >> 8), //nolint:gosec // an average of 16-bit alphas, so its high byte fits
	}
}
