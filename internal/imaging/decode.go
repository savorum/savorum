// Package imaging decodes stored images for presentation: it reads the
// encoded bytes of every format Savorum accepts, applies the orientation and
// color the file declares, and renders the result as canonical sRGB for
// derivative and print pipelines.
package imaging

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/h265/heic"
	"github.com/gen2brain/jpegxl"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
	"golang.org/x/image/webp"
)

// Errors a caller distinguishes. Every decoding failure wraps ErrInvalid.
var (
	ErrUnsupported = errors.New("image format is not supported")
	ErrInvalid     = errors.New("image cannot be decoded")
	ErrTooLarge    = errors.New("image exceeds a decoding limit")
)

// Limits bound what one image may cost to decode. They are enforced against
// the file's header before any pixels are allocated, because a small
// compressed image can declare an enormous frame. A zero field removes that
// bound.
type Limits struct {
	Bytes  int64
	Width  int
	Height int
	Pixels int
}

// Decoded is an image as its file stores it, before any correction.
type Decoded struct {
	Image  image.Image
	Format Format
	// Orientation is the Exif orientation still to apply, 1 when there is
	// none or the decoder applied the file's own transforms.
	Orientation int
	Color       ColorDescription
}

// ColorDescription is what a file says about the meaning of its pixel values.
// An empty description means the file says nothing.
type ColorDescription struct {
	ICC  []byte
	CICP *CICP
}

// CICP is the coding-independent code point description of ITU-T H.273.
type CICP struct {
	Primaries, Transfer, Matrix uint16
	FullRange                   bool
}

type codec struct {
	config func([]byte) (image.Config, error)
	decode func([]byte, int) (Decoded, error)
}

var codecs = map[Format]codec{
	JPEG:   {standardConfig(jpeg.DecodeConfig), decodeJPEG},
	PNG:    {standardConfig(png.DecodeConfig), decodePNG},
	GIF:    {standardConfig(gif.DecodeConfig), decodeStandard(GIF, gif.Decode)},
	WebP:   {standardConfig(webp.DecodeConfig), decodeWebP},
	BMP:    {standardConfig(bmp.DecodeConfig), decodeStandard(BMP, bmp.Decode)},
	TIFF:   {standardConfig(tiff.DecodeConfig), decodeTIFF},
	HEIF:   {standardConfig(heic.DecodeConfig), decodeHEIF},
	AVIF:   {standardConfig(avif.DecodeConfig), decodeAVIF},
	JPEGXL: {standardConfig(jpegxl.DecodeConfig), decodeJPEGXL},
}

// Measure reads an image's header and enforces the limits without allocating
// a single pixel. It reports the coded dimensions; a format whose orientation
// transforms swap the sides still passes or fails on the same area, and the
// post-decode check in Decode covers the swap.
func Measure(source []byte, limits Limits) (image.Config, Format, error) {
	if limits.Bytes > 0 && int64(len(source)) > limits.Bytes {
		return image.Config{}, "", ErrTooLarge
	}

	_, format := sniff(source)

	codec, ok := codecs[format]
	if !ok {
		return image.Config{}, "", ErrUnsupported
	}

	config, err := codec.config(source)
	if err != nil {
		return image.Config{}, "", fmt.Errorf("%w: read %s header: %w", ErrInvalid, format, err)
	}

	if config.Width < 1 || config.Height < 1 {
		return image.Config{}, "", fmt.Errorf("%w: %s image has no dimensions", ErrInvalid, format)
	}

	if err := limits.check(config.Width, config.Height); err != nil {
		return image.Config{}, "", err
	}

	return config, format, nil
}

func (l Limits) check(width, height int) error {
	if l.Width > 0 && width > l.Width || l.Height > 0 && height > l.Height || l.Pixels > 0 && width > l.Pixels/height {
		return ErrTooLarge
	}

	return nil
}

// Decode reads an image and what its file says about orientation and color.
// It reads the header first and refuses an image outside the limits before
// allocating its pixels.
func Decode(source []byte, limits Limits) (Decoded, error) {
	config, format, err := Measure(source, limits)
	if err != nil {
		return Decoded{}, err
	}

	decoded, err := codecs[format].decode(source, limits.Pixels)
	if err != nil {
		return Decoded{}, fmt.Errorf("%w: decode %s: %w", ErrInvalid, format, err)
	}

	bounds := decoded.Image.Bounds()
	if bounds.Dx() != config.Width || bounds.Dy() != config.Height {
		if err := limits.check(bounds.Dx(), bounds.Dy()); err != nil {
			return Decoded{}, err
		}
	}

	decoded.Format = format
	if decoded.Orientation == 0 {
		decoded.Orientation = 1
	}

	return decoded, nil
}

func standardConfig(read func(io.Reader) (image.Config, error)) func([]byte) (image.Config, error) {
	return func(source []byte) (image.Config, error) { return read(bytes.NewReader(source)) }
}

func decodeStandard(format Format, read func(io.Reader) (image.Image, error)) func([]byte, int) (Decoded, error) {
	return func(source []byte, _ int) (Decoded, error) {
		decoded, err := read(bytes.NewReader(source))
		if err != nil {
			return Decoded{}, fmt.Errorf("%s stream: %w", format, err)
		}

		return Decoded{Image: decoded, Format: format}, nil
	}
}

func decodeJPEG(source []byte, _ int) (Decoded, error) {
	decoded, err := jpeg.Decode(bytes.NewReader(source))
	if err != nil {
		return Decoded{}, fmt.Errorf("jpeg stream: %w", err)
	}

	orientation, icc := jpegMetadata(source)

	return Decoded{Image: decoded, Orientation: orientation, Color: ColorDescription{ICC: icc}}, nil
}

func decodePNG(source []byte, _ int) (Decoded, error) {
	decoded, err := png.Decode(bytes.NewReader(source))
	if err != nil {
		return Decoded{}, fmt.Errorf("png stream: %w", err)
	}

	orientation, color := pngMetadata(source)

	return Decoded{Image: decoded, Orientation: orientation, Color: color}, nil
}

func decodeWebP(source []byte, _ int) (Decoded, error) {
	decoded, err := webp.Decode(bytes.NewReader(source))
	if err != nil {
		return Decoded{}, fmt.Errorf("webp stream: %w", err)
	}

	orientation, icc := webpMetadata(source)

	return Decoded{Image: decoded, Orientation: orientation, Color: ColorDescription{ICC: icc}}, nil
}

func decodeTIFF(source []byte, _ int) (Decoded, error) {
	decoded, err := tiff.Decode(bytes.NewReader(source))
	if err != nil {
		return Decoded{}, fmt.Errorf("tiff stream: %w", err)
	}

	entries := readIFD0(source)

	return Decoded{Image: decoded, Orientation: entries.orientation(), Color: ColorDescription{ICC: entries.bytes(tiffICCTag)}}, nil
}

// decodeHEIF lets the decoder apply the container's clap, irot, and imir
// transforms, which take precedence over any Exif orientation in the file.
func decodeHEIF(source []byte, maximumPixels int) (Decoded, error) {
	decoded, info, err := heic.DecodeColor(bytes.NewReader(source), heic.Options{AutoRotate: true, FrameSizeLimit: maximumPixels})
	if err != nil {
		return Decoded{}, fmt.Errorf("heic stream: %w", err)
	}

	description := ColorDescription{ICC: info.ICCP}
	if len(info.ICCP) == 0 && info.Primaries != 0 {
		description.CICP = &CICP{Primaries: info.Primaries, Transfer: info.Transfer, Matrix: info.Matrix, FullRange: info.FullRange}
	}

	return Decoded{Image: decoded, Color: description}, nil
}

func decodeAVIF(source []byte, _ int) (Decoded, error) {
	decoded, err := avif.Decode(bytes.NewReader(source), avif.Options{AutoRotate: true})
	if err != nil {
		return Decoded{}, fmt.Errorf("avif stream: %w", err)
	}

	return Decoded{Image: decoded, Color: heifColor(source)}, nil
}

// decodeJPEGXL returns libjxl's output, which already has the codestream's
// orientation applied. The decoder does not report the color encoding of its
// output, so the description stays empty.
func decodeJPEGXL(source []byte, _ int) (Decoded, error) {
	decoded, err := jpegxl.Decode(bytes.NewReader(source))
	if err != nil {
		return Decoded{}, fmt.Errorf("jpeg xl stream: %w", err)
	}

	return Decoded{Image: decoded}, nil
}
