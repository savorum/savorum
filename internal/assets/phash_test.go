package assets_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/imaging"
)

func generateTestPattern(w, h int, frequency float64) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			fx := float64(x) / float64(w)
			fy := float64(y) / float64(h)
			v := uint8((fx*frequency + fy*frequency*1.5) * 255.0)
			img.Set(x, y, color.RGBA{R: v, G: 255 - v, B: (v * 2) % 255, A: 255})
		}
	}

	return img
}

func TestPHashIdenticalImages(t *testing.T) {
	img := generateTestPattern(200, 200, 2.0)
	h1 := assets.ComputePHash(img)
	h2 := assets.ComputePHash(img)

	if h1 != h2 {
		t.Fatalf("expected identical hashes, got %x and %x", h1, h2)
	}

	if dist := assets.PHashDistance(h1, h2); dist != 0 {
		t.Fatalf("expected distance 0, got %d", dist)
	}
}

func TestPHashResizedImages(t *testing.T) {
	imgLarge := generateTestPattern(800, 600, 1.5)
	imgSmall := generateTestPattern(400, 300, 1.5)

	hLarge := assets.ComputePHash(imgLarge)
	hSmall := assets.ComputePHash(imgSmall)

	dist := assets.PHashDistance(hLarge, hSmall)
	if dist > 6 {
		t.Fatalf("expected low distance for resized versions of same image, got %d", dist)
	}
}

func TestPHashDifferentImages(t *testing.T) {
	img1 := generateTestPattern(400, 400, 1.0)
	img2 := generateTestPattern(400, 400, 20.0)

	h1 := assets.ComputePHash(img1)
	h2 := assets.ComputePHash(img2)

	dist := assets.PHashDistance(h1, h2)
	if dist < 15 {
		t.Fatalf("expected high distance for different images, got %d", dist)
	}
}

func TestComputeBytesPHash(t *testing.T) {
	img := generateTestPattern(300, 200, 2.5)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	hash, w, h, err := assets.ComputeBytesPHash(buf.Bytes())
	if err != nil {
		t.Fatalf("compute bytes phash: %v", err)
	}

	if w != 300 || h != 200 {
		t.Fatalf("expected dimensions 300x200, got %dx%d", w, h)
	}

	directHash := assets.ComputePHash(img)
	if dist := assets.PHashDistance(hash, directHash); dist > 2 {
		t.Fatalf("expected encoded PNG to match direct image phash closely, got distance %d", dist)
	}
}

func TestDeriveImageWebP(t *testing.T) {
	img := generateTestPattern(500, 400, 3.0)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}

	derived, err := imaging.DeriveImage(buf.Bytes(), imaging.DerivativeCard)
	if err != nil {
		t.Fatalf("derive image card: %v", err)
	}

	if len(derived) == 0 {
		t.Fatal("expected non-empty derived image")
	}
}

func TestCenterCropRect(t *testing.T) {
	// A square is too tall for 16:9: height 1000/(16/9) ≈ 563, y = (1000-563)/2 = 218.
	r1 := assets.CenterCropRect(1000, 1000, 16.0/9.0)
	if r1.Dx() != 1000 || r1.Dy() < 560 || r1.Dy() > 565 {
		t.Fatalf("unexpected crop rect for 1000x1000 -> 16:9: %v", r1)
	}

	// 1920x1080 (16:9) cropped to 1:1: too wide -> width reduced to 1080, height unchanged
	r2 := assets.CenterCropRect(1920, 1080, 1.0)
	if r2.Dx() != 1080 || r2.Dy() != 1080 || r2.Min.X != 420 {
		t.Fatalf("unexpected crop rect for 1920x1080 -> 1:1: %v", r2)
	}
}

func TestCropHashBankMatching(t *testing.T) {
	orig := generateTestPattern(900, 1350, 2.0)
	origBank := assets.ComputeCropHashBank(orig)

	squareCrop := assets.CropImage(orig, 1.0)
	squareBank := assets.ComputeCropHashBank(squareCrop)

	wideCrop := assets.CropImage(orig, 16.0/9.0)
	wideBank := assets.ComputeCropHashBank(wideCrop)

	// Global distance between tall original and 16:9 crop is large without crop awareness
	globalDist := assets.PHashDistance(origBank.Global, wideBank.Global)
	if globalDist <= 10 {
		t.Fatalf("expected raw global pHash distance between 2:3 and 16:9 crop to be > 10, got %d", globalDist)
	}

	// MinCropDistance across the crop bank should recognize all center crops
	if d := assets.MinCropDistance(origBank, squareBank); d > 5 {
		t.Fatalf("expected low crop bank distance between original and square crop, got %d", d)
	}

	if d := assets.MinCropDistance(origBank, wideBank); d > 5 {
		t.Fatalf("expected low crop bank distance between original and 16:9 crop, got %d", d)
	}

	if d := assets.MinCropDistance(squareBank, wideBank); d > 5 {
		t.Fatalf("expected low crop bank distance between square crop and 16:9 crop, got %d", d)
	}

	// Different image should NOT match even across crop bank
	diffImg := generateTestPattern(900, 1350, 25.0)

	diffBank := assets.ComputeCropHashBank(diffImg)
	if d := assets.MinCropDistance(origBank, diffBank); d <= 10 {
		t.Fatalf("expected distinct image to have crop bank distance > 10, got %d", d)
	}
}
