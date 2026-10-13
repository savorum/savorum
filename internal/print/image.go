package print

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/imaging"
	"github.com/savorum/savorum/internal/recipe"
)

// printResolution is the pixel density a print derivative is produced at.
// It is generous enough for photographic print and far below the size of an
// unmodified camera original.
const printResolution = 300

// maximumImageSide is the longest side a decoder is asked to allocate.
const maximumImageSide = 32768

// AssetOpener reads the immutable bytes of a registered asset. Printing opens
// exactly the assets a resolved recipe snapshot names and nothing else: there
// is no path, no name, and no address a template could influence.
type AssetOpener interface {
	Open(ctx context.Context, id assets.ID) (assets.Blob, io.ReadCloser, error)
}

// photographBox is the widest box, in inches, a photograph is printed in at
// this size, and for a card the width-to-height ratio of the band it is
// cropped to. It follows the templates: a page photograph takes 32 percent of
// the body width and grows by at most 10 percent (page.typ), and a card
// photograph spans the card body at 24 percent of its height (card.typ).
// Margins are theme.typ's. A render test checks the printed resolution.
func photographBox(size Size) (width, band float64) {
	dimensions := size.Dimensions()
	body := func(margin float64) (float64, float64) {
		return dimensions.Width.Points()/72 - 2*margin, dimensions.Height.Points()/72 - 2*margin
	}
	card := func(width, height float64) (float64, float64) { return width, width / (height * 0.24) }

	switch size {
	case SizeLetter:
		width, _ := body(0.72)
		return width * 0.32 * 1.10, 0
	case SizeA4:
		width, _ := body(18 / 25.4)
		return width * 0.32 * 1.10, 0
	case SizeCard35:
		return card(body(0.2))
	case SizeCard46:
		return card(body(0.24))
	case SizeCard58:
		return card(body(0.3))
	default:
		return 0, 0
	}
}

// resolveImage produces the print derivative of a recipe's photograph. The
// asset is the one the resolved revision names; a missing or unreadable image
// degrades to a warning.
func resolveImage(ctx context.Context, opener AssetOpener, document recipe.Document, size Size, name string, limits WorkloadLimits) (*PrintImage, []byte, string) {
	if opener == nil || document.PrimaryImage == "" {
		return nil, nil, ""
	}

	asset := primaryImageAsset(document)

	if asset == nil || asset.SHA256 == "" {
		return nil, nil, fmt.Sprintf("%q declares a photograph this version does not name.", document.Title)
	}

	if asset.Size > int64(limits.DecodeBytes) {
		return nil, nil, fmt.Sprintf("The photograph for %q is too large to print and was left out.", document.Title)
	}

	blob, reader, err := opener.Open(ctx, assets.ID("sha256:"+asset.SHA256))
	if err != nil {
		if errors.Is(err, assets.ErrNotFound) {
			return nil, nil, fmt.Sprintf("This library holds no bytes for the photograph in %q, so it was printed without one.", document.Title)
		}

		return nil, nil, fmt.Sprintf("The photograph for %q could not be read and was left out.", document.Title)
	}
	defer reader.Close()

	source, err := io.ReadAll(io.LimitReader(reader, int64(limits.DecodeBytes)+1))
	if err != nil {
		return nil, nil, fmt.Sprintf("The photograph for %q could not be read and was left out.", document.Title)
	}

	if len(source) > limits.DecodeBytes {
		return nil, nil, fmt.Sprintf("The photograph for %q is too large to print and was left out.", document.Title)
	}

	_ = blob

	box, band := photographBox(size)

	decoding := imaging.Limits{Bytes: int64(limits.DecodeBytes), Width: maximumImageSide, Height: maximumImageSide, Pixels: limits.DecodePixels}

	derivative, width, height, extension, err := printDerivative(source, box, band, decoding)
	if err != nil {
		if errors.Is(err, imaging.ErrTooLarge) {
			return nil, nil, fmt.Sprintf("The photograph for %q exceeds the print image budget and was left out.", document.Title)
		}

		return nil, nil, fmt.Sprintf("The photograph for %q is not in a format Savorum can print, so it was left out.", document.Title)
	}

	printed := &PrintImage{
		Name:   name + extension,
		Width:  width,
		Height: height,
		Alt:    asset.Alt,
		Credit: imageCredit(*asset),
	}

	return printed, derivative, ""
}

// printDerivative decodes a stored image and produces the smallest version
// that still prints well at the box it will occupy. A band ratio above zero
// keeps only the centred part a card's band shows.
func printDerivative(source []byte, boxInches, band float64, limits imaging.Limits) ([]byte, int, int, string, error) {
	decoded, err := imaging.Decode(source, limits)
	if err != nil {
		return nil, 0, 0, "", fmt.Errorf("decode image: %w", err)
	}

	region := bandRegion(decoded, band)
	width := min(region.Dx(), int(math.Ceil(boxInches*printResolution)))
	height := max(region.Dy()*width/region.Dx(), 1)
	scaled := imaging.Reduce(decoded, region, width, height)
	// Transparency only survives in a format that has it. A photograph has
	// none and compresses far better as a JPEG.
	if hasTransparency(scaled) {
		var out bytes.Buffer
		if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, scaled); err != nil {
			return nil, 0, 0, "", fmt.Errorf("encode image: %w", err)
		}

		return out.Bytes(), width, height, ".png", nil
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, scaled, &jpeg.Options{Quality: 88}); err != nil {
		return nil, 0, 0, "", fmt.Errorf("encode image: %w", err)
	}

	return out.Bytes(), width, height, ".jpg", nil
}

// bandRegion is the centred part of a picture at a band's proportions, the
// part a centred cover fit shows. A band ratio of zero keeps the whole picture.
func bandRegion(decoded imaging.Decoded, band float64) image.Rectangle {
	width, height := decoded.Size()

	switch {
	case band <= 0:
		return image.Rect(0, 0, width, height)
	case float64(width) < float64(height)*band:
		visible := max(int(math.Round(float64(width)/band)), 1)
		top := (height - visible) / 2

		return image.Rect(0, top, width, top+visible)
	default:
		visible := max(int(math.Round(float64(height)*band)), 1)
		left := (width - visible) / 2

		return image.Rect(left, 0, left+visible, height)
	}
}

func hasTransparency(source image.Image) bool {
	if opaque, ok := source.(interface{ Opaque() bool }); ok {
		return !opaque.Opaque()
	}

	bounds := source.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if _, _, _, a := source.At(x, y).RGBA(); a != 0xffff {
				return true
			}
		}
	}

	return false
}

func primaryImageAsset(document recipe.Document) *recipe.Asset {
	for index := range document.Assets {
		if document.Assets[index].ID == document.PrimaryImage {
			return &document.Assets[index]
		}
	}

	return nil
}

// imageResolver reuses derivatives within one document's fixed print size.
// Recipe-specific alternative text and attribution remain on each reference.
type imageResolver struct {
	opener AssetOpener
	size   Size
	limits WorkloadLimits
	images map[string]PrintImage
}

func (r *imageResolver) resolve(ctx context.Context, document recipe.Document, name string) (*PrintImage, []byte, string) {
	asset := primaryImageAsset(document)
	if r.opener != nil && document.PrimaryImage != "" && asset != nil && asset.SHA256 != "" && asset.Size <= int64(r.limits.DecodeBytes) {
		if cached, found := r.images[asset.SHA256]; found {
			cached.Alt = asset.Alt
			cached.Credit = imageCredit(*asset)

			return &cached, nil, ""
		}
	}

	picture, body, warning := resolveImage(ctx, r.opener, document, r.size, name, r.limits)
	if picture != nil {
		r.images[asset.SHA256] = *picture
	}

	return picture, body, warning
}
