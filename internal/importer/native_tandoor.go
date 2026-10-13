package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref/archivepolicy"
)

var tandoorNativeFields = nativeFieldSet(
	"id", "name", "description", "source_url", "servings_text", "servings",
	"working_time", "waiting_time", "nutrition", "keywords", "steps",
	"export_version", "format_version", "schema_version", "version", "image",
)

type tandoorAdapter struct{}

func (tandoorAdapter) Format() Format {
	return Format{InputKinds: []string{"file"}, MediaTypes: []string{"application/zip"}, ID: "tandoor", Name: "Tandoor Recipes", Version: "native", Extensions: []string{".zip"}, Capabilities: Capabilities{Collection: true, Images: true, SourceIdentity: true, Organization: true}}
}

func (tandoorAdapter) Probe(_ context.Context, in Input) ProbeResult {
	if zipContainsJSONKey(in.Data, `"servings_text"`) || zipContainsNestedJSONKey(in.Data, `"steps"`) {
		return ProbeResult{Confidence: ConfidenceContent, Reason: "Tandoor native export"}
	}

	return ProbeResult{}
}

func zipContainsNestedJSONKey(data []byte, key string) bool {
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

	remaining := int64(8 << 20)

	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}

		b, ok := readProbeMember(f, &remaining)
		if !ok {
			return false
		}

		if bytes.HasPrefix(b, []byte("PK\x03\x04")) && zipContainsJSONKeyNested(b, key, &remaining) {
			return true
		}
	}

	return false
}

func zipContainsJSONKeyNested(data []byte, key string, remaining *int64) bool {
	if archivepolicy.Preflight(data, 20, 8<<20) != nil {
		return false
	}

	z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil {
		return false
	}

	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}

		b, ok := readProbeMember(f, remaining)
		if !ok {
			return false
		}

		if bytes.Contains(b, []byte(key)) {
			return true
		}
	}

	return false
}

type tandoorRecipeData struct {
	Name         string              `json:"name"`
	Description  string              `json:"description"`
	SourceURL    string              `json:"source_url"`
	ServingsText string              `json:"servings_text"`
	Servings     float64             `json:"servings"`
	WorkingTime  int                 `json:"working_time"`
	WaitingTime  int                 `json:"waiting_time"`
	Nutrition    any                 `json:"nutrition"`
	Keywords     []tandoorKeyword    `json:"keywords"`
	Steps        []tandoorRecipeStep `json:"steps"`
}

type tandoorKeyword struct {
	Name string `json:"name"`
}

type tandoorRecipeStep struct {
	Name        string              `json:"name"`
	Instruction string              `json:"instruction"`
	Ingredients []tandoorIngredient `json:"ingredients"`
}

type tandoorIngredient struct {
	Food struct {
		Name string `json:"name"`
	} `json:"food"`
	Unit struct {
		Name string `json:"name"`
	} `json:"unit"`
	Amount   json.Number `json:"amount"`
	Note     string      `json:"note"`
	NoAmount bool        `json:"no_amount"`
	IsHeader bool        `json:"is_header"`
}

func (tandoorAdapter) Import(_ context.Context, in Input, options Options) (ImportBatch, error) {
	out := ImportBatch{Source: ImportSource{Format: tandoorAdapter{}.Format()}}
	budget := options.workBudget(nativeMaxArchive)
	err := readNativeZipBudget(in.Data, 1000, budget, func(name string, data []byte) error {
		if !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
			return nil
		}

		id := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))

		items, err := parseTandoorBundle(id, data, budget)
		out.Recipes = append(out.Recipes, items...)

		return err
	})

	return out, err
}

func parseTandoorBundle(id string, data []byte, budget *archivepolicy.Budget) ([]ImportedRecipe, error) {
	var (
		items    []ImportedRecipe
		embedded []EmbeddedAsset
	)

	err := readNativeZipBudget(data, 20, budget, func(name string, body []byte) error {
		if isNativeImage(body) {
			embedded = append(embedded, nativeImage(body, len(embedded)+1))

			return nil
		}

		parsed, err := parseTandoorRecipe(id, name, body)
		if err == nil {
			items = append(items, parsed)
		}

		return err
	})

	for index := range items {
		items[index].EmbeddedAssets = append(items[index].EmbeddedAssets, embedded...)
		if len(items[index].EmbeddedAssets) > 0 {
			attachEmbedded(&items[index].Document, items[index].EmbeddedAssets)
		}
	}

	return items, err
}

func isNativeImage(data []byte) bool {
	return len(data) > 4 && (bytes.HasPrefix(data, []byte("\xff\xd8\xff")) || bytes.HasPrefix(data, []byte("\x89PNG")))
}

func nativeImage(data []byte, sequence int) EmbeddedAsset {
	id := fmt.Sprintf("image-%d", sequence)
	sum := sha256.Sum256(data)

	return EmbeddedAsset{ID: id, Path: id, MediaType: sniffImageMedia(data), SHA256: fmt.Sprintf("%x", sum), Bytes: data}
}

func parseTandoorRecipe(id, name string, body []byte) (ImportedRecipe, error) {
	var fields map[string]any

	rawDecoder := json.NewDecoder(bytes.NewReader(body))
	rawDecoder.UseNumber()

	if err := rawDecoder.Decode(&fields); err != nil {
		return ImportedRecipe{}, fmt.Errorf("decode Tandoor fields from %q: %w", name, err)
	}

	var data tandoorRecipeData

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	if err := decoder.Decode(&data); err != nil {
		return ImportedRecipe{}, fmt.Errorf("decode Tandoor recipe from %q: %w", name, err)
	}

	return tandoorRecipe(id, data, fields), nil
}

func tandoorRecipe(id string, data tandoorRecipeData, fields map[string]any) ImportedRecipe {
	doc := sourceDoc("Tandoor Recipes", id, data.Name)
	doc.Description = data.Description
	doc.Source.URL = data.SourceURL
	doc.Source.Name = "Tandoor Recipes"

	if strings.TrimSpace(data.ServingsText) != "" {
		doc.Yields = []recipe.Yield{{Text: data.ServingsText}}
	} else if data.Servings != 0 {
		doc.Yields = []recipe.Yield{{Text: strconv.FormatFloat(data.Servings, 'f', -1, 64) + " servings"}}
	}

	setTimes(&doc, data.WorkingTime, data.WaitingTime, nil)
	ingredients, directions := tandoorGroups(data.Steps)
	addNativeLines(&doc, ingredients, "")
	addDirections(&doc, directions)

	item := ImportedRecipe{Document: doc, SourceKey: id, ImporterMetadata: map[string]any{"tandoor_id": id}}
	addExportVersionMetadata(item.ImporterMetadata, fields)

	for _, keyword := range data.Keywords {
		item.OrganizationMemberships = append(item.OrganizationMemberships, OrganizationMembership{Kind: "tag", Name: keyword.Name, SourceKey: keyword.Name})
	}

	if data.Nutrition != nil {
		addUnsupported(&item, "nutrition", fmt.Sprint(data.Nutrition))
	}

	addUnknownNativeFields(&item, fields, tandoorNativeFields)

	return item
}

func tandoorGroups(steps []tandoorRecipeStep) ([]nativeLineGroup, []nativeLineGroup) {
	var ingredients, directions []nativeLineGroup

	for _, step := range steps {
		ingredients = append(ingredients, tandoorIngredientGroups(step)...)
		directions = append(directions, nativeLineGroup{title: step.Name, lines: []string{step.Instruction}})
	}

	return ingredients, directions
}

func tandoorIngredientGroups(step tandoorRecipeStep) []nativeLineGroup {
	var groups []nativeLineGroup

	heading := step.Name
	lines := make([]string, 0)

	flush := func() {
		if len(lines) > 0 {
			groups = append(groups, nativeLineGroup{title: heading, lines: lines})
			lines = nil
		}
	}

	for _, ingredient := range step.Ingredients {
		if ingredient.IsHeader {
			flush()

			heading = tandoorIngredientHeading(ingredient)

			continue
		}

		lines = append(lines, tandoorIngredientLine(ingredient))
	}

	flush()

	return groups
}

func tandoorIngredientHeading(ingredient tandoorIngredient) string {
	heading := ingredient.Food.Name
	if ingredient.Note != "" {
		heading += ", " + ingredient.Note
	}

	return heading
}

func tandoorIngredientLine(ingredient tandoorIngredient) string {
	line := fmt.Sprintf("%s %s %s", ingredient.Amount.String(), ingredient.Unit.Name, ingredient.Food.Name)
	if ingredient.NoAmount {
		line = ingredient.Food.Name
	}

	if ingredient.Note != "" {
		line += ", " + ingredient.Note
	}

	return line
}
