package importer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
)

var mealieNativeFields = nativeFieldSet(
	"id", "slug", "name", "description", "org_url", "notes", "recipe_ingredient",
	"recipe_instructions", "prep_time", "cook_time", "total_time", "recipe_yield_quantity",
	"recipe_yield", "recipe_servings", "assets", "image", "rating", "recipe_category",
	"tags", "tools", "nutrition", "export_version", "format_version", "schema_version", "version",
)

type mealieAdapter struct{}

func (mealieAdapter) Format() Format {
	return Format{InputKinds: []string{"file"}, MediaTypes: []string{"application/zip"}, ID: "mealie", Name: "Mealie", Version: "3", Extensions: []string{".zip"}, Capabilities: Capabilities{Collection: true, Images: true, SourceIdentity: true, Organization: true, Refresh: true}}
}

func (mealieAdapter) Probe(_ context.Context, in Input) ProbeResult {
	if zipContainsJSONKey(in.Data, `"recipe_ingredient"`) {
		return ProbeResult{Confidence: ConfidenceContent, Reason: "Mealie recipe export"}
	}

	return ProbeResult{}
}

func (mealieAdapter) Import(_ context.Context, in Input, options Options) (ImportBatch, error) {
	out := ImportBatch{Source: ImportSource{Format: mealieAdapter{}.Format()}}
	sidecars := map[string]EmbeddedAsset{}
	err := readNativeZipBudget(in.Data, 1000, options.workBudget(nativeMaxArchive), func(name string, data []byte) error {
		item, err := parseMealieMember(name, data, sidecars)
		if item != nil {
			out.Recipes = append(out.Recipes, *item)
		}

		return err
	})

	for i := range out.Recipes {
		finishMealieImages(&out.Recipes[i], sidecars)
	}

	return out, err
}

func parseMealieMember(name string, data []byte, sidecars map[string]EmbeddedAsset) (*ImportedRecipe, error) {
	if !strings.HasSuffix(name, ".json") {
		if mediaType := sniffImageMedia(data); strings.HasPrefix(mediaType, "image/") {
			sum := sha256.Sum256(data)
			base := filepath.Base(name)
			sidecars[base] = EmbeddedAsset{ID: base, Path: name, MediaType: mediaType, SHA256: fmt.Sprintf("%x", sum), Bytes: data}
		}

		return nil, nil
	}

	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, fmt.Errorf("decode Mealie recipe %q: %w", name, err)
	}

	item := mealieRecipe(fields, sidecars)

	return &item, nil
}

func mealieRecipe(fields map[string]any, sidecars map[string]EmbeddedAsset) ImportedRecipe {
	id := firstString(fields, "id", "slug")
	doc := sourceDoc("Mealie", id, firstString(fields, "name"))
	doc.Description = firstString(fields, "description")
	doc.Source.URL = firstString(fields, "org_url")
	doc.Source.Name = "Mealie"
	doc.Notes = stringSlice(fields["notes"])
	addNativeLines(&doc, mealieIngredientGroups(fields["recipe_ingredient"]), "")
	addDirections(&doc, mealieInstructionGroups(fields["recipe_instructions"]))
	setTimes(&doc, fields["prep_time"], fields["cook_time"], fields["total_time"])
	setMealieYield(&doc, fields)
	addMealieEquipment(&doc, fields["tools"])

	imageID := firstString(fields, "image")
	item := ImportedRecipe{Document: doc, SourceKey: id, ImporterMetadata: map[string]any{"slug": fields["slug"], "image_id": imageID}}
	item.EmbeddedAssets = embeddedFromValue(fields["assets"])
	item.EmbeddedAssets = append(item.EmbeddedAssets, matchingSidecars(imageID, sidecars)...)
	addExportVersionMetadata(item.ImporterMetadata, fields)
	addMealieMetadata(&item, fields)
	addUnknownNativeFields(&item, fields, mealieNativeFields)

	return item
}

func mealieIngredientGroups(value any) []nativeLineGroup {
	groups := make([]nativeLineGroup, 0)

	for _, ingredient := range arrayMaps(value) {
		groups = append(groups, nativeLineGroup{
			title: firstString(ingredient, "title", "group"),
			lines: []string{firstString(ingredient, "display", "original_text", "note")},
		})
	}

	return groups
}

func mealieInstructionGroups(value any) []nativeLineGroup {
	groups := make([]nativeLineGroup, 0)

	for _, instruction := range arrayMaps(value) {
		groups = append(groups, nativeLineGroup{
			title: firstString(instruction, "title"),
			lines: []string{firstString(instruction, "text", "summary")},
		})
	}

	return groups
}

func setMealieYield(doc *recipe.Document, fields map[string]any) {
	quantity, hasQuantity := fields["recipe_yield_quantity"].(float64)
	label := firstString(fields, "recipe_yield")
	servings, hasServings := fields["recipe_servings"].(float64)

	switch {
	case hasQuantity && quantity != 0:
		doc.Yields = append(doc.Yields, recipe.Yield{Text: strings.TrimSpace(strconv.FormatFloat(quantity, 'f', -1, 64) + " " + label)})
	case label != "":
		doc.Yields = append(doc.Yields, recipe.Yield{Text: label})
	}

	if hasServings && servings != 0 {
		doc.Yields = append(doc.Yields, recipe.Yield{Text: strconv.FormatFloat(servings, 'f', -1, 64) + " servings"})
	}
}

func addMealieEquipment(doc *recipe.Document, value any) {
	for _, tool := range arrayMaps(value) {
		if name := firstString(tool, "name"); name != "" {
			doc.Equipment = append(doc.Equipment, recipe.Equipment{Name: name})
		}
	}
}

func addMealieMetadata(item *ImportedRecipe, fields map[string]any) {
	importedRating(item, fields, "rating")

	for _, category := range stringSlice(fields["recipe_category"]) {
		item.OrganizationMemberships = append(item.OrganizationMemberships, OrganizationMembership{Kind: "category", Name: category, SourceKey: category})
	}

	for _, tag := range arrayMaps(fields["tags"]) {
		if name := firstString(tag, "name"); name != "" {
			item.OrganizationMemberships = append(item.OrganizationMemberships, OrganizationMembership{Kind: "tag", Name: name, SourceKey: name})
		}
	}

	if nutrition, ok := fields["nutrition"]; ok && !absentNativeField(nutrition) {
		addUnsupported(item, "nutrition", fmt.Sprint(nutrition))
	}
}

func matchingSidecars(imageID string, sidecars map[string]EmbeddedAsset) []EmbeddedAsset {
	var matches []EmbeddedAsset

	if imageID == "" {
		return matches
	}

	for key, asset := range sidecars {
		if strings.HasPrefix(key, imageID) {
			matches = append(matches, asset)
		}
	}

	return matches
}

func finishMealieImages(item *ImportedRecipe, sidecars map[string]EmbeddedAsset) {
	imageID, _ := item.ImporterMetadata["image_id"].(string)
	if len(item.EmbeddedAssets) == 0 {
		item.EmbeddedAssets = append(item.EmbeddedAssets, matchingSidecars(imageID, sidecars)...)
	}

	if len(item.EmbeddedAssets) > 0 {
		attachEmbedded(&item.Document, item.EmbeddedAssets)
	}
}
