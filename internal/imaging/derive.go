package imaging

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
)

// DerivativePreset identifies a generated image-derivative size.
type DerivativePreset string

// Derivative names identify the bounded image representation to generate.
const (
	DerivativeCard   DerivativePreset = "card"
	DerivativeReader DerivativePreset = "reader"
	DerivativeThumb  DerivativePreset = "thumb"
)

// DerivativeLimits bound the source a presentation derivative may decode.
// Compressed formats expand far past their byte size, so the header must
// pass every check before pixel memory is allocated.
var DerivativeLimits = Limits{Bytes: 64 << 20, Width: 32768, Height: 32768, Pixels: 40_000_000}

func (p DerivativePreset) maximumEdge() (int, bool) {
	switch p {
	case DerivativeCard:
		return 640, true
	case DerivativeReader:
		return 1600, true
	case DerivativeThumb:
		return 240, true
	default:
		return 0, false
	}
}

// DeriveImage produces a deterministic, metadata-free JPEG or PNG of the
// longest-edge preset: decode, resize, apply the file's orientation,
// normalize to sRGB SDR, encode. Unsupported formats fail with ErrUnsupported.
func DeriveImage(source []byte, preset DerivativePreset) ([]byte, error) {
	maximumEdge, ok := preset.maximumEdge()
	if !ok {
		return nil, fmt.Errorf("unknown derivative preset: %w", ErrInvalid)
	}

	decoded, err := Decode(source, DerivativeLimits)
	if err != nil {
		return nil, fmt.Errorf("decode derivative image: %w", err)
	}

	fullWidth, fullHeight := decoded.Size()

	width, height := fullWidth, fullHeight
	if width > maximumEdge || height > maximumEdge {
		if width >= height {
			height = (height*maximumEdge + width/2) / width
			width = maximumEdge
		} else {
			width = (width*maximumEdge + height/2) / height
			height = maximumEdge
		}
	}

	if width < 1 {
		width = 1
	}

	if height < 1 {
		height = 1
	}

	target := Reduce(decoded, image.Rect(0, 0, fullWidth, fullHeight), width, height)

	var output bytes.Buffer
	if opaque(target) {
		if err := jpeg.Encode(&output, target, &jpeg.Options{Quality: 80}); err != nil {
			return nil, fmt.Errorf("encode JPEG derivative: %w", err)
		}

		return output.Bytes(), nil
	}

	if err := png.Encode(&output, target); err != nil {
		return nil, fmt.Errorf("encode PNG derivative: %w", err)
	}

	return output.Bytes(), nil
}
