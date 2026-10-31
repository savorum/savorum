package app_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"
	"testing"
)

func picture(t *testing.T) []byte {
	t.Helper()

	canvas := image.NewNRGBA(image.Rect(0, 0, 64, 32))

	for y := range 32 {
		for x := range 64 {
			canvas.Set(x, y, color.NRGBA{R: 200, G: 120, B: 40, A: 255})
		}
	}

	var data bytes.Buffer
	if err := png.Encode(&data, canvas); err != nil {
		t.Fatal(err)
	}

	return data.Bytes()
}

func TestUploadedImagesAreServedResizedAndPreviewedForRecipesAndBooks(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	blob := decode[struct {
		ID        string `json:"id"`
		SHA256    string `json:"sha256"`
		Size      int    `json:"size"`
		MediaType string `json:"media_type"`
	}](t, h.raw(http.MethodPost, "/api/v1/assets", picture(t), "application/octet-stream", http.StatusCreated, "X-Asset-Media-Type", "image/png"))

	created := h.send(http.MethodPost, "/api/v1/recipes", map[string]any{
		"schema_version": 1, "id": "photographed", "title": "Photographed", "language": "en",
		"ingredient_sections":  []any{map[string]any{"id": "ingredients", "ingredients": []any{map[string]any{"id": "water", "name": "water", "source_text": "1 cup water"}}}},
		"instruction_sections": []any{map[string]any{"id": "method", "steps": []any{map[string]any{"id": "boil", "text": "Boil the water."}}}},
		"assets":               []any{map[string]any{"id": "photo", "media_type": "image/png", "path": "assets/photo.png", "sha256": strings.TrimPrefix(blob.ID, "sha256:"), "size": blob.Size, "role": "image"}},
		"image_refs":           []string{"photo"},
		"primary_image":        "photo",
	}, http.StatusCreated)

	entry := decode[struct {
		ID string `json:"id"`
	}](t, created).ID
	recipePath := "/api/v1/recipes/" + entry + "/assets/" + blob.ID

	h.send(http.MethodGet, recipePath, nil, http.StatusOK)
	h.send(http.MethodGet, recipePath+"/derivatives/thumb", nil, http.StatusOK)

	elsewhere := newHarness(t)
	preview := decode[previewItems](t, elsewhere.upload("photographed.sref", h.send(http.MethodGet, "/api/v1/recipes/"+entry+"/export/sref", nil, http.StatusOK).Body.Bytes()))
	elsewhere.send(http.MethodGet, "/api/v1/recipes/import-previews/"+preview.ID+"/items/"+preview.Items[0].ID+"/image", nil, http.StatusOK)

	book := decode[struct {
		ID string `json:"id"`
	}](t, h.send(http.MethodPost, "/api/v1/books", map[string]any{"kind": "personal", "title": "Illustrated", "sections": []any{}, "entries": []any{}, "cover_asset_id": blob.ID}, http.StatusCreated))

	h.send(http.MethodGet, "/api/v1/books/"+book.ID+"/assets/"+blob.ID+"/derivatives/card", nil, http.StatusOK)
}
