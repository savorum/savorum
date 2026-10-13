package importer

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref/archivepolicy"
)

var paprikaNativeFields = nativeFieldSet(
	"uid", "name", "description", "notes", "source_url", "source", "servings",
	"prep_time", "cook_time", "total_time", "ingredients", "directions", "rating",
	"categories", "photos", "image_url", "nutritional_info", "export_version",
	"format_version", "schema_version", "version",
)

type paprikaAdapter struct{}

func (paprikaAdapter) Format() Format {
	return Format{InputKinds: []string{"file"}, MediaTypes: []string{"application/gzip", "application/zip"}, ID: "paprika", Name: "Paprika", Version: "native", Extensions: []string{".paprikarecipes", ".paprikarecipe"}, Capabilities: Capabilities{Collection: true, Images: true, SourceIdentity: true, Organization: true, Refresh: true}}
}

func (paprikaAdapter) Probe(ctx context.Context, in Input) ProbeResult {
	if ctx.Err() != nil {
		return ProbeResult{}
	}

	if zipContainsPaprika(ctx, in.Data) {
		return ProbeResult{Confidence: ConfidenceContent, Reason: "Paprika compressed recipe archive"}
	}

	if ctx.Err() != nil {
		return ProbeResult{}
	}

	if strings.HasSuffix(strings.ToLower(in.Name), ".paprikarecipes") || strings.HasSuffix(strings.ToLower(in.Name), ".paprikarecipe") {
		return ProbeResult{Confidence: ConfidenceExtension, Reason: "Paprika extension"}
	}

	return ProbeResult{}
}

func zipContainsPaprika(ctx context.Context, data []byte) bool {
	if len(data) > nativeMaxArchive || ctx.Err() != nil {
		return false
	}

	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("PK\x03\x04")) {
		return false
	}

	if archivepolicy.Preflight(data, 1000, 8<<20) != nil {
		return false
	}

	z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil {
		return false
	}

	// Probe has its own small aggregate budgets, including gzip headers read
	// from ZIP members. These allowances are shared across every member.
	outer, decoded := int64(4<<20), int64(1<<20)
	compressed, compressedReads := uint64(4<<20), int64(4<<20)
	// Bound reads at the compressed boundary as well as decoded output. The
	// per-attempt precharge includes errors and overlapping ZIP ranges.
	z.RegisterDecompressor(zip.Deflate, func(r io.Reader) io.ReadCloser {
		return flate.NewReader(&paprikaProbeReader{ctx: ctx, reader: r, remaining: &compressedReads})
	})
	z.RegisterDecompressor(zip.Store, func(r io.Reader) io.ReadCloser {
		return io.NopCloser(&paprikaProbeReader{ctx: ctx, reader: r, remaining: &compressedReads})
	})

	for _, f := range z.File {
		if ctx.Err() != nil || outer <= 0 || decoded <= 0 {
			return false
		}

		if f.CompressedSize64 > 1<<20 || f.CompressedSize64 > compressed {
			return false
		}

		compressed -= f.CompressedSize64

		r, e := f.Open()
		if e != nil {
			continue
		}

		g, e := gzip.NewReader(&paprikaProbeReader{ctx: ctx, reader: r, remaining: &outer})
		if e == nil {
			b, _ := io.ReadAll(io.LimitReader(&paprikaProbeReader{ctx: ctx, reader: g, remaining: &decoded}, 256<<10))
			_ = g.Close()
			_ = r.Close()

			if ctx.Err() == nil && bytes.Contains(b, []byte(`"uid"`)) {
				return true
			}
		} else {
			_ = r.Close()
		}
	}

	return false
}

// Reads check cancellation between bounded chunks and debit before reuse.
//
//nolint:containedctx // This synchronous reader exists only during a single probe.
type paprikaProbeReader struct {
	ctx       context.Context
	reader    io.Reader
	remaining *int64
}

func (r *paprikaProbeReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	if *r.remaining <= 0 {
		return 0, archivepolicy.ErrLimit
	}

	p = p[:min(int64(len(p)), *r.remaining, 32<<10)]
	n, err := r.reader.Read(p)

	*r.remaining -= int64(n)
	if cancel := r.ctx.Err(); cancel != nil {
		return n, cancel
	}
	//nolint:wrapcheck // Preserve Reader EOF and budget errors for gzip and io.ReadAll.
	return n, err
}

func (paprikaAdapter) Import(ctx context.Context, in Input, options Options) (ImportBatch, error) {
	var out ImportBatch

	budget := options.workBudget(nativeMaxArchive)
	appendRecipe := func(data []byte) error {
		item, err := parsePaprikaMember(data, budget)
		if err == nil {
			out.Recipes = append(out.Recipes, item)
		}

		return err
	}

	if strings.HasSuffix(strings.ToLower(in.Name), ".paprikarecipe") {
		if err := appendRecipe(in.Data); err != nil {
			return out, err
		}
	} else if err := readNativeZipBudget(in.Data, 1000, budget, func(_ string, data []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		return appendRecipe(data)
	}); err != nil {
		return out, err
	}

	out.Source = ImportSource{Format: paprikaAdapter{}.Format()}

	return out, nil
}

func parsePaprikaMember(data []byte, budget *archivepolicy.Budget) (ImportedRecipe, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return ImportedRecipe{}, fmt.Errorf("open Paprika recipe member: %w", err)
	}

	raw, err := io.ReadAll(io.LimitReader(gzipReader, 8<<20+1))
	_ = gzipReader.Close()

	if err != nil {
		return ImportedRecipe{}, fmt.Errorf("read Paprika recipe member: %w", err)
	}

	if len(raw) > 8<<20 {
		return ImportedRecipe{}, errors.New("this Paprika export member is too large to read")
	}

	if err := budget.Spend(int64(len(raw))); err != nil {
		return ImportedRecipe{}, fmt.Errorf("reserve expanded Paprika recipe size: %w", err)
	}

	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ImportedRecipe{}, fmt.Errorf("decode Paprika recipe: %w", err)
	}

	return paprikaRecipe(fields), nil
}

func paprikaRecipe(fields map[string]any) ImportedRecipe {
	id, _ := fields["uid"].(string)
	title, _ := fields["name"].(string)
	doc := sourceDoc("Paprika", id, title)
	doc.Description, _ = fields["description"].(string)
	doc.Notes = stringSlice(fields["notes"])
	doc.Source.URL = firstString(fields, "source_url")
	doc.Source.Name = firstString(fields, "source")

	if yield := firstString(fields, "servings"); yield != "" {
		doc.Yields = []recipe.Yield{{Text: yield}}
	}

	setTimes(&doc, fields["prep_time"], fields["cook_time"], fields["total_time"])
	addNativeLines(&doc, []nativeLineGroup{{lines: stringSlice(fields["ingredients"])}}, "")
	addDirections(&doc, []nativeLineGroup{{lines: stringSlice(fields["directions"])}})

	item := ImportedRecipe{Document: doc, SourceKey: id, ImporterMetadata: map[string]any{"uid": id}}
	addExportVersionMetadata(item.ImporterMetadata, fields)

	// The loss boundary runs last, and over what the mapping above did not
	// account for.
	handled := addPaprikaMetadata(&item, fields)
	addUnknownNativeFields(&item, fields, paprikaNativeFields, handled...)

	return item
}

// addPaprikaMetadata returns the source keys it accounted for, so the loss
// boundary reports the fields this adapter genuinely left behind rather than
// the ones it read under another name.
func addPaprikaMetadata(item *ImportedRecipe, fields map[string]any) []string {
	importedRating(item, fields, "rating")

	for _, category := range stringSlice(fields["categories"]) {
		item.OrganizationMemberships = append(item.OrganizationMemberships, OrganizationMembership{Kind: "category", Name: category, SourceKey: category})
	}

	handled := paprikaImages(item, fields)

	if imageURL := firstString(fields, "image_url"); imageURL != "" {
		item.Assets = []RemoteAsset{{URL: imageURL, RecipeImage: true, Role: recipe.AssetRoleImage, Alt: item.Document.Title}}
	}

	if nutrition, ok := fields["nutritional_info"]; ok && !absentNativeField(nutrition) {
		addUnsupported(item, "nutrition", fmt.Sprint(nutrition))
	}

	return handled
}

// paprikaImages reads a recipe's photograph from either shape Paprika exports:
// version 3's `photos` array, or version 1's `photo_data` beside the `photo`
// filename.
func paprikaImages(item *ImportedRecipe, fields map[string]any) []string {
	handled := []string{"hash", "photo_hash"}

	embedded := paprikaPhotos(fields["photos"])
	if len(embedded) > 0 {
		handled = append(handled, "photo", "photo_large", "photo_data")
	} else if single, ok := paprikaSinglePhoto(fields); ok {
		embedded = append(embedded, single)
		handled = append(handled, "photo", "photo_large", "photo_data")
	}

	item.EmbeddedAssets = append(item.EmbeddedAssets, embedded...)
	attachEmbedded(&item.Document, item.EmbeddedAssets)

	return handled
}

func paprikaSinglePhoto(fields map[string]any) (EmbeddedAsset, bool) {
	raw := firstString(fields, "photo_data")
	if raw == "" {
		return EmbeddedAsset{}, false
	}

	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(data) == 0 {
		return EmbeddedAsset{}, false
	}

	name := firstString(fields, "photo")
	if name == "" {
		name = firstString(fields, "photo_large")
	}

	sum := sha256.Sum256(data)

	return EmbeddedAsset{ID: name, Path: name, MediaType: sniffImageMedia(data), SHA256: fmt.Sprintf("%x", sum), Bytes: data}, true
}

func paprikaPhotos(value any) []EmbeddedAsset {
	var assets []EmbeddedAsset

	for _, photo := range arrayMaps(value) {
		raw := firstString(photo, "data")

		data, err := base64.StdEncoding.DecodeString(raw)
		if raw == "" || err != nil {
			continue
		}

		name := firstString(photo, "filename")
		sum := sha256.Sum256(data)
		assets = append(assets, EmbeddedAsset{ID: name, Path: name, MediaType: sniffImageMedia(data), SHA256: fmt.Sprintf("%x", sum), Bytes: data})
	}

	return assets
}
