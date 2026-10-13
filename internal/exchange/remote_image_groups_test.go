package exchange_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"reflect"
	"testing"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
)

type variantFetcher map[string][]byte

func (f variantFetcher) Fetch(_ context.Context, address string) (exchange.FetchedAsset, error) {
	data, ok := f[address]
	if !ok {
		return exchange.FetchedAsset{}, errors.New("image unavailable")
	}

	return exchange.FetchedAsset{Body: io.NopCloser(bytes.NewReader(data)), MediaType: "image/png", Size: int64(len(data))}, nil
}

func makePatternImage(w, h int, freq float64) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			fx := float64(x) / float64(w)
			fy := float64(y) / float64(h)
			v := uint8((fx*freq + fy*freq*1.5) * 255.0)
			img.Set(x, y, color.RGBA{R: v, G: 255 - v, B: (v * 2) % 255, A: 255})
		}
	}

	var buf bytes.Buffer

	_ = png.Encode(&buf, img)

	return buf.Bytes()
}

func TestImportKeepsLargestResolutionViaPHashAndURL(t *testing.T) {
	base := "https://images.example/carnitas.png"

	for _, test := range []struct {
		name   string
		absent bool
		want   []string
	}{
		{"largest decoded dimensions", false, []string{"large", "other", "step"}},
		{"unavailable original", true, []string{"medium", "other", "step"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := testfixture.Document()
			document.Assets, document.ImageRefs, document.PrimaryImage = nil, nil, ""
			document.InstructionSections[0].Steps[0].ImageRefs = []string{"step"}
			remote := func(id, address string) importer.RemoteAsset {
				return importer.RemoteAsset{LogicalID: id, URL: address, RecipeImage: true, Role: recipe.AssetRoleImage}
			}

			pictures := []importer.RemoteAsset{
				remote("small", base+"?resize=100%2C100"),
				remote("large", base),
				remote("medium", base+"?resize=400%2C400"),
				remote("other", "https://images.example/other-distinct-photo.png"),
				remote("step", "https://images.example/step-photo.png"),
			}

			fetcher := variantFetcher{
				base + "?resize=100%2C100": makePatternImage(100, 100, 2.0),
				base + "?resize=400%2C400": makePatternImage(400, 400, 2.0),
				base:                       makePatternImage(800, 800, 2.0),
				"https://images.example/other-distinct-photo.png": makePatternImage(400, 400, 25.0),
				"https://images.example/step-photo.png":           makePatternImage(100, 100, 2.0),
			}

			if test.absent {
				delete(fetcher, base)
			}

			recipes, blobs := &importLibrary{}, &countingAssets{}
			service := exchange.NewService(recipes, blobs, exchange.WithRemoteAssetFetcher(fetcher))

			results, err := service.ImportExternal(context.Background(), []importer.ImportBatch{{Recipes: []importer.ImportedRecipe{{Document: document, Assets: pictures}}}}, library.DuplicateReject)
			if err != nil {
				t.Fatal(err)
			}

			if len(recipes.imported) != 1 || !reflect.DeepEqual(recipes.imported[0].ImageRefs, test.want) || recipes.imported[0].PrimaryImage != test.want[0] {
				t.Fatalf("gallery: got %+v, want %+v", recipes.imported[0].ImageRefs, test.want)
			}

			if blobs.ingests != len(test.want) || results[0].DroppedAssets != 0 || results[0].DeferredAssets != 0 {
				t.Fatalf("duplicates or failed-variant warnings: %d %+v", blobs.ingests, results)
			}
		})
	}
}

func TestPHashDeduplicatesDifferentURLFilenames(t *testing.T) {
	// Tests that two completely different URLs (different filenames/paths) containing the same image
	// are deduplicated by pHash so only the highest resolution is added.
	document := testfixture.Document()
	document.Assets, document.ImageRefs, document.PrimaryImage = nil, nil, ""
	remote := func(id, address string) importer.RemoteAsset {
		return importer.RemoteAsset{LogicalID: id, URL: address, RecipeImage: true, Role: recipe.AssetRoleImage}
	}

	pictures := []importer.RemoteAsset{
		remote("res-500", "https://cdn.example.com/tachyon/2026/09/Pork-Carnitas-500px.png"),
		remote("res-1000", "https://cdn.example.com/tachyon/2018/05/Pork-Carnitas-1000px.png"),
		remote("distinct", "https://cdn.example.com/salad.png"),
	}

	fetcher := variantFetcher{
		"https://cdn.example.com/tachyon/2026/09/Pork-Carnitas-500px.png":  makePatternImage(500, 500, 3.0),
		"https://cdn.example.com/tachyon/2018/05/Pork-Carnitas-1000px.png": makePatternImage(1000, 1000, 3.0),
		"https://cdn.example.com/salad.png":                                makePatternImage(600, 600, 30.0),
	}

	recipes, blobs := &importLibrary{}, &countingAssets{}
	service := exchange.NewService(recipes, blobs, exchange.WithRemoteAssetFetcher(fetcher))

	results, err := service.ImportExternal(context.Background(), []importer.ImportBatch{{Recipes: []importer.ImportedRecipe{{Document: document, Assets: pictures}}}}, library.DuplicateReject)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"res-1000", "distinct"}
	if len(recipes.imported) != 1 || !reflect.DeepEqual(recipes.imported[0].ImageRefs, want) {
		t.Fatalf("gallery: got %+v, want %+v", recipes.imported[0].ImageRefs, want)
	}

	if blobs.ingests != 2 || results[0].DroppedAssets != 0 || results[0].DeferredAssets != 0 {
		t.Fatalf("blobs: %d %+v", blobs.ingests, results)
	}
}

func TestCenterCropDeduplicatesDifferentAspectRatiosAndFilenames(t *testing.T) {
	// Center crops with different URLs and aspect ratios are one photo, and
	// the largest is kept.
	document := testfixture.Document()
	document.Assets, document.ImageRefs, document.PrimaryImage = nil, nil, ""
	remote := func(id, address string) importer.RemoteAsset {
		return importer.RemoteAsset{LogicalID: id, URL: address, RecipeImage: true, Role: recipe.AssetRoleImage}
	}

	pictures := []importer.RemoteAsset{
		remote("square-crop", "https://cdn1.example.com/thumbnails/carnitas-square.png"),
		remote("tall-orig", "https://cdn2.example.com/full/carnitas-portrait.png"),
		remote("banner-crop", "https://cdn3.example.com/headers/carnitas-wide.png"),
		remote("distinct-salad", "https://cdn4.example.com/sides/salad.png"),
	}

	origImg := image.NewRGBA(image.Rect(0, 0, 900, 1350))

	for y := range 1350 {
		for x := range 900 {
			fx := float64(x) / 900.0
			fy := float64(y) / 1350.0
			v := uint8((fx*2.0 + fy*3.0) * 255.0)
			origImg.Set(x, y, color.RGBA{R: v, G: 255 - v, B: (v * 2) % 255, A: 255})
		}
	}

	squareImg := assets.CropImage(origImg, 1.0)
	bannerImg := assets.CropImage(origImg, 16.0/9.0)

	encode := func(img image.Image) []byte {
		var buf bytes.Buffer

		_ = png.Encode(&buf, img)

		return buf.Bytes()
	}

	fetcher := variantFetcher{
		"https://cdn1.example.com/thumbnails/carnitas-square.png": encode(squareImg),
		"https://cdn2.example.com/full/carnitas-portrait.png":     encode(origImg),
		"https://cdn3.example.com/headers/carnitas-wide.png":      encode(bannerImg),
		"https://cdn4.example.com/sides/salad.png":                makePatternImage(600, 600, 30.0),
	}

	recipes, blobs := &importLibrary{}, &countingAssets{}
	service := exchange.NewService(recipes, blobs, exchange.WithRemoteAssetFetcher(fetcher))

	results, err := service.ImportExternal(context.Background(), []importer.ImportBatch{{Recipes: []importer.ImportedRecipe{{Document: document, Assets: pictures}}}}, library.DuplicateReject)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"tall-orig", "distinct-salad"}
	if len(recipes.imported) != 1 || !reflect.DeepEqual(recipes.imported[0].ImageRefs, want) {
		t.Fatalf("gallery: got %+v, want %+v", recipes.imported[0].ImageRefs, want)
	}

	if blobs.ingests != 2 || results[0].DroppedAssets != 0 || results[0].DeferredAssets != 0 {
		t.Fatalf("blobs: %d %+v", blobs.ingests, results)
	}
}
