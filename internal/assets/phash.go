package assets

import (
	"fmt"
	"image"
	"math"
	"math/bits"
	"slices"

	"golang.org/x/image/draw"

	"github.com/savorum/savorum/internal/imaging"
)

// cosTable precomputes cos((2x + 1) * u * pi / 64) for u in [0, 7] and x in [0, 31].
var cosTable = makeCosTable()

func makeCosTable() (table [8][32]float64) {
	for u := range 8 {
		for x := range 32 {
			table[u][x] = math.Cos(float64(2*x+1) * float64(u) * math.Pi / 64.0)
		}
	}

	return table
}

// ComputePHash calculates a 64-bit perceptual hash for img:
// 1) Resize to 32×32 using bilinear interpolation.
// 2) Convert each pixel to luminance (ITU-R BT.601).
// 3) Calculate the low-frequency 8×8 portion of a 2D DCT.
// 4) Calculate the median of the 63 AC coefficients (excluding DC at (0,0)).
// 5) Set each bit to 1 if its coefficient is above the median, 0 otherwise.
func ComputePHash(img image.Image) uint64 {
	dst := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.BiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)

	var lum [32][32]float64

	for y := range 32 {
		for x := range 32 {
			c := dst.RGBAAt(x, y)
			lum[y][x] = 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
		}
	}

	// 1D DCT along rows: 32 rows -> 8 low-frequency horizontal components each.
	var rowDCT [32][8]float64

	for y := range 32 {
		for u := range 8 {
			var sum float64
			for x := range 32 {
				sum += lum[y][x] * cosTable[u][x]
			}

			rowDCT[y][u] = sum
		}
	}

	// 1D DCT along columns: 8 columns -> 8 low-frequency vertical components each.
	var dct [8][8]float64

	for u := range 8 {
		for v := range 8 {
			var sum float64
			for y := range 32 {
				sum += rowDCT[y][u] * cosTable[v][y]
			}

			dct[u][v] = sum
		}
	}

	// Median of the 63 AC coefficients (excluding DC at (0,0)).
	var ac [63]float64

	idx := 0

	for u := range 8 {
		for v := range 8 {
			if u == 0 && v == 0 {
				continue
			}

			ac[idx] = dct[u][v]
			idx++
		}
	}

	slices.Sort(ac[:])
	median := ac[31]

	// 64-bit hash: bit (u*8 + v) is 1 if dct[u][v] > median, else 0.
	var hash uint64

	for u := range 8 {
		for v := range 8 {
			if dct[u][v] > median {
				hash |= (uint64(1) << (u*8 + v))
			}
		}
	}

	return hash
}

// PHashDistance returns the Hamming distance between two perceptual hashes.
func PHashDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// CenterCropRect returns the centered sub-rectangle of dimensions (w, h) that achieves targetAspect.
func CenterCropRect(w, h int, targetAspect float64) image.Rectangle {
	current := float64(w) / float64(h)

	if current > targetAspect {
		// Too wide: remove left/right.
		newW := int(math.Round(float64(h) * targetAspect))
		if newW <= 0 {
			newW = 1
		}

		if newW > w {
			newW = w
		}

		x := (w - newW) / 2

		return image.Rect(x, 0, x+newW, h)
	}

	// Too tall: remove top/bottom.
	newH := int(math.Round(float64(w) / targetAspect))
	if newH <= 0 {
		newH = 1
	}

	if newH > h {
		newH = h
	}

	y := (h - newH) / 2

	return image.Rect(0, y, w, y+newH)
}

type subImager interface {
	SubImage(r image.Rectangle) image.Image
}

// CropImage returns a center-cropped image with the given target aspect ratio.
// If the image already matches the target aspect ratio, it returns the image unchanged.
func CropImage(img image.Image, targetAspect float64) image.Image {
	b := img.Bounds()

	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || targetAspect <= 0 {
		return img
	}

	rect := CenterCropRect(w, h, targetAspect).Add(b.Min)
	if rect == b {
		return img
	}

	if si, ok := img.(subImager); ok {
		return si.SubImage(rect)
	}

	dst := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(dst, dst.Bounds(), img, rect.Min, draw.Src)

	return dst
}

// StandardAspectRatios defines standard aspect ratios for center-crop perceptual hashing:
// 1:1 (square), 4:3, 3:2, 16:9, 4:5, 2:3.
var StandardAspectRatios = [6]float64{
	1.0,        // 1:1 (square)
	4.0 / 3.0,  // 4:3
	3.0 / 2.0,  // 3:2
	16.0 / 9.0, // 16:9
	4.0 / 5.0,  // 4:5
	2.0 / 3.0,  // 2:3
}

// CropHashBank stores the global pHash and center-crop pHashes for standard aspect ratios.
type CropHashBank struct {
	Global uint64
	Crops  [6]uint64
}

// ComputeCropHashBank calculates the global pHash and 6 center-crop pHashes for img.
func ComputeCropHashBank(img image.Image) CropHashBank {
	bank := CropHashBank{
		Global: ComputePHash(img),
	}
	for i, aspect := range StandardAspectRatios {
		cropped := CropImage(img, aspect)
		bank.Crops[i] = ComputePHash(cropped)
	}

	return bank
}

// hashDecodeLimits bound the pixels a hashed image may occupy. The bytes are
// already staged elsewhere; the pixel bound is what keeps a small compressed
// file from inflating into a decode the process cannot afford.
var hashDecodeLimits = imaging.Limits{Width: 32768, Height: 32768, Pixels: 40_000_000}

// decodeCanonical decodes bounded bytes to the canonical sRGB form shared by
// the derivative pipeline, so a hash sees the oriented, color-normalized
// image a viewer would.
func decodeCanonical(data []byte) (image.Image, int, int, error) {
	decoded, err := imaging.Decode(data, hashDecodeLimits)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("decode image: %w", err)
	}

	img := imaging.Canonical(decoded)
	bounds := img.Bounds()

	return img, bounds.Dx(), bounds.Dy(), nil
}

// ComputeBytesCropHashBank decodes image bytes and returns its CropHashBank, width, and height.
func ComputeBytesCropHashBank(data []byte) (CropHashBank, int, int, error) {
	img, width, height, err := decodeCanonical(data)
	if err != nil {
		return CropHashBank{}, 0, 0, fmt.Errorf("decode image for crop hash bank: %w", err)
	}

	return ComputeCropHashBank(img), width, height, nil
}

// MinCropDistance returns the minimum Hamming distance between two CropHashBanks.
// It compares global hashes, matching aspect-ratio crop hashes, and crop-to-global hashes.
func MinCropDistance(a, b CropHashBank) int {
	minDist := PHashDistance(a.Global, b.Global)
	for i := range StandardAspectRatios {
		if d := PHashDistance(a.Crops[i], b.Crops[i]); d < minDist {
			minDist = d
		}

		if d := PHashDistance(a.Crops[i], b.Global); d < minDist {
			minDist = d
		}

		if d := PHashDistance(a.Global, b.Crops[i]); d < minDist {
			minDist = d
		}
	}

	return minDist
}

// ComputeBytesPHash decodes image bytes and returns its perceptual hash, width, and height.
func ComputeBytesPHash(data []byte) (uint64, int, int, error) {
	img, width, height, err := decodeCanonical(data)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("decode image for phash: %w", err)
	}

	return ComputePHash(img), width, height, nil
}
