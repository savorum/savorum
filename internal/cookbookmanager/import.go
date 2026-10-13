// Package cookbookmanager converts CookbookManager.com YAML exports into
// Savorum recipe documents.
package cookbookmanager

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref/archivepolicy"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

const (
	maximumArchiveBytes    = 64 << 20
	maximumRecipeBytes     = 4 << 20
	maximumExpandedBytes   = 64 << 20
	maximumDirectoryBytes  = 8 << 20
	maximumArchiveEntries  = 1_000
	maximumArchiveRecipes  = 500
	maximumCompressionRate = 200
)

type exportedRecipe struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Servings    string   `yaml:"servings"`
	Source      string   `yaml:"source"`
	Image       string   `yaml:"image"`
	PrepTime    string   `yaml:"prep_time"`
	CookTime    string   `yaml:"cook_time"`
	Video       string   `yaml:"video"`
	Favorite    string   `yaml:"favorite"`
	OnFavorites string   `yaml:"on_favorites"`
	CookCount   int      `yaml:"cook_count"`
	Images      []string `yaml:"images"`
	Keywords    string   `yaml:"keywords"`
	Tags        []string `yaml:"tags"`
	Nutrition   string   `yaml:"nutrition"`
	Ingredients []string `yaml:"ingredients"`
	Directions  []string `yaml:"directions"`
	ExportedBy  string   `yaml:"exportedBy"`
}

var sourceIDPattern = regexp.MustCompile(`_([0-9]+)\.(?:ya?ml)$`)

// Decode converts one YAML export. The filename supplies CookbookManager's
// stable numeric identity because the YAML body does not include it.
func Decode(data []byte, filename string) (recipe.Document, error) {
	source, err := decodeSource(data, filename)
	if err != nil {
		return recipe.Document{}, err
	}

	return decodeSourceDocument(source, filename)
}

func decodeSource(data []byte, filename string) (exportedRecipe, error) {
	if len(data) == 0 || len(data) > maximumRecipeBytes {
		return exportedRecipe{}, fmt.Errorf("CookbookManager recipe %q is empty or too large", filename)
	}

	var source exportedRecipe

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(false)

	if err := decoder.Decode(&source); err != nil {
		return exportedRecipe{}, fmt.Errorf("read CookbookManager YAML %q: %w", filename, err)
	}

	return source, nil
}

func decodeSourceDocument(source exportedRecipe, filename string) (recipe.Document, error) {
	title := strings.TrimSpace(source.Name)
	if title == "" {
		return recipe.Document{}, fmt.Errorf("CookbookManager recipe %q has no name", filename)
	}

	sourceID := cookbookSourceID(filename, title, source.Source)

	document := recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            recipe.RecipeID("cookbook." + sourceID),
		Title:         title,
		Description:   strings.TrimSpace(source.Description),
		Source: &recipe.Source{
			Name:        sourceName(source.Source),
			URL:         sourceURL(source.Source),
			Application: "CookbookManager.com",
			SourceID:    sourceID,
			Attribution: strings.TrimSpace(source.ExportedBy),
		},
		Yields:   yieldFromText(source.Servings),
		Times:    timesFromExport(source.PrepTime, source.CookTime),
		Keywords: mergeKeywords(source.Keywords, source.Tags),
		Extensions: map[string]any{"x-cookbookmanager": map[string]any{
			"image_url": strings.TrimSpace(source.Image), "video_url": strings.TrimSpace(source.Video),
			"images": source.Images,
		}},
	}
	if nutrition := strings.TrimSpace(source.Nutrition); nutrition != "" {
		document.Notes = []string{"Nutrition (as exported):\n" + nutrition}
	}

	var ingredientSections []recipe.IngredientSection

	currentIngredients := recipe.IngredientSection{}

	for index, line := range source.Ingredients {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if isSectionHeading(line) {
			if len(currentIngredients.Ingredients) > 0 {
				ingredientSections = append(ingredientSections, currentIngredients)
				currentIngredients = recipe.IngredientSection{}
			}

			currentIngredients.Title = headingText(line)

			continue
		}

		id := "i" + shortHash(fmt.Sprintf("%s|ingredient|%d|%s", sourceID, index, line))
		// CookbookManager's YAML vocabulary is English but it does not retain a
		// recipe's regional measurement convention. Language-level aliases such
		// as g, kg, oz, lb, and tsp are therefore safe to resolve; region-bound
		// cup and tablespoon identities remain visibly unresolved for review.
		currentIngredients.Ingredients = append(currentIngredients.Ingredients, ingredientparser.Parse(line, id, ingredientparser.AliasLanguage))
	}

	if len(currentIngredients.Ingredients) > 0 {
		ingredientSections = append(ingredientSections, currentIngredients)
	}

	for index := range ingredientSections {
		ingredientSections[index].ID = "s" + shortHash(fmt.Sprintf("%s|ingredients|%d|%s", sourceID, index, ingredientSections[index].Title))
	}

	document.IngredientSections = ingredientSections

	var instructionSections []recipe.InstructionSection

	currentInstructions := recipe.InstructionSection{}

	for index, line := range source.Directions {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if isSectionHeading(line) {
			if len(currentInstructions.Steps) > 0 {
				instructionSections = append(instructionSections, currentInstructions)
				currentInstructions = recipe.InstructionSection{}
			}

			currentInstructions.Title = headingText(line)

			continue
		}

		currentInstructions.Steps = append(currentInstructions.Steps, recipe.Step{ID: fmt.Sprintf("p%s%d", shortHash(sourceID+"|step"), index+1), Text: line})
	}

	if len(currentInstructions.Steps) > 0 {
		instructionSections = append(instructionSections, currentInstructions)
	}

	for index := range instructionSections {
		instructionSections[index].ID = "n" + shortHash(fmt.Sprintf("%s|method|%d|%s", sourceID, index, instructionSections[index].Title))
	}

	if len(document.IngredientSections) == 0 || len(instructionSections) == 0 {
		return recipe.Document{}, fmt.Errorf("CookbookManager recipe %q needs at least one ingredient and direction", filename)
	}

	document.InstructionSections = instructionSections

	return recipe.ReconcileTimings(document), nil
}

// LibraryMetadata reads application-library facts from the export without
// adding them to the portable recipe document. Import adapters own source
// interpretation; the shared import service owns Savorum persistence.
func LibraryMetadata(data []byte, filename string) (favorite bool, cookCount int, err error) {
	source, err := decodeSource(data, filename)
	if err != nil {
		return false, 0, err
	}

	return strings.EqualFold(source.Favorite, "yes") || strings.EqualFold(source.OnFavorites, "yes"), max(source.CookCount, 0), nil
}

// DecodeArchive reads every YAML recipe in a CookbookManager ZIP. Directory
// entries, README files, and macOS metadata are ignored.
func DecodeArchive(data []byte) ([]recipe.Document, error) {
	imports, err := DecodeArchiveImports(data)
	if err != nil {
		return nil, err
	}

	documents := make([]recipe.Document, len(imports))
	for index, imported := range imports {
		documents[index] = imported.Document
	}

	return documents, nil
}

// ImportedRecipe combines a decoded document with CookbookManager library metadata.
type ImportedRecipe struct {
	Document  recipe.Document
	Favorite  bool
	CookCount int
}

// DecodeArchiveImports retains application-library fields alongside each
// document so callers never need to encode them as recipe extensions.
func DecodeArchiveImports(data []byte) ([]ImportedRecipe, error) {
	return DecodeArchiveImportsContext(context.Background(), data)
}

// DecodeArchiveImportsContext applies one aggregate hostile-archive policy
// and observes cancellation before opening or decoding each member.
func DecodeArchiveImportsContext(ctx context.Context, data []byte) ([]ImportedRecipe, error) {
	if len(data) == 0 || len(data) > maximumArchiveBytes {
		return nil, fmt.Errorf("CookbookManager ZIP is empty or too large")
	}

	if err := archivepolicy.Preflight(data, maximumArchiveEntries, maximumDirectoryBytes); err != nil {
		return nil, fmt.Errorf("CookbookManager ZIP directory exceeds the import policy")
	}

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("read CookbookManager ZIP: %w", err)
	}

	entries, err := archiveRecipeEntries(ctx, reader.File)
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	documents := make([]ImportedRecipe, 0, len(entries))

	var expanded int64

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		name := filepath.ToSlash(entry.Name)

		opened, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("open CookbookManager ZIP member %q: %w", name, err)
		}

		body, readErr := io.ReadAll(io.LimitReader(opened, maximumRecipeBytes+1))

		closeErr := opened.Close()
		if readErr != nil || closeErr != nil || len(body) > maximumRecipeBytes {
			return nil, fmt.Errorf("read CookbookManager ZIP member %q", name)
		}

		expanded += int64(len(body))
		if expanded > maximumExpandedBytes {
			return nil, fmt.Errorf("CookbookManager ZIP expands beyond the import policy")
		}

		source, err := decodeSource(body, filepath.Base(name))
		if err != nil {
			return nil, err
		}

		document, err := decodeSourceDocument(source, filepath.Base(name))
		if err != nil {
			return nil, err
		}

		favorite := strings.EqualFold(source.Favorite, "yes") || strings.EqualFold(source.OnFavorites, "yes")
		cookCount := max(source.CookCount, 0)
		documents = append(documents, ImportedRecipe{Document: document, Favorite: favorite, CookCount: cookCount})
	}

	if len(documents) == 0 {
		return nil, fmt.Errorf("CookbookManager ZIP contains no YAML recipes")
	}

	return documents, nil
}

func archiveRecipeEntries(ctx context.Context, files []*zip.File) ([]*zip.File, error) {
	entries := make([]*zip.File, 0, min(len(files), maximumArchiveRecipes))
	seen := make(map[string]struct{}, min(len(files), maximumArchiveRecipes))

	var declaredExpanded uint64

	for _, entry := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		name := strings.ReplaceAll(entry.Name, "\\", "/")
		lower := strings.ToLower(name)

		if entry.FileInfo().IsDir() || strings.HasPrefix(name, "__MACOSX/") || (!strings.HasSuffix(lower, ".yml") && !strings.HasSuffix(lower, ".yaml")) {
			continue
		}

		clean := path.Clean(name)
		if clean != name || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || strings.ContainsRune(clean, 0) || (len(clean) >= 2 && clean[1] == ':') {
			return nil, fmt.Errorf("unsafe CookbookManager ZIP member path %q", entry.Name)
		}

		key := norm.NFC.String(cases.Fold().String(norm.NFC.String(clean)))
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("duplicate CookbookManager ZIP member %q", entry.Name)
		}

		seen[key] = struct{}{}

		if entry.UncompressedSize64 > maximumRecipeBytes {
			return nil, fmt.Errorf("CookbookManager ZIP member %q is too large", name)
		}

		if entry.CompressedSize64 > 0 && entry.UncompressedSize64/entry.CompressedSize64 > maximumCompressionRate {
			return nil, fmt.Errorf("CookbookManager ZIP member %q expands beyond the import policy", name)
		}

		if entry.UncompressedSize64 > uint64(maximumExpandedBytes)-declaredExpanded {
			return nil, fmt.Errorf("CookbookManager ZIP expands beyond the import policy")
		}

		declaredExpanded += entry.UncompressedSize64
		entries = append(entries, entry)

		if len(entries) > maximumArchiveRecipes {
			return nil, fmt.Errorf("CookbookManager ZIP contains more than %d recipes", maximumArchiveRecipes)
		}
	}

	return entries, nil
}

func isSectionHeading(value string) bool {
	trimmed := strings.TrimSpace(value)
	return len(trimmed) > 1 && len(trimmed) <= 100 && strings.HasSuffix(trimmed, ":") && !strings.ContainsAny(strings.TrimSuffix(trimmed, ":"), ".!?")
}

func headingText(value string) string {
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), ":"))
}

func cookbookSourceID(filename, title, source string) string {
	if match := sourceIDPattern.FindStringSubmatch(filename); len(match) == 2 {
		return match[1]
	}

	return shortHash(title + "|" + source)
}

func shortHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:8])
}

func sourceURL(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") {
		return value
	}

	return ""
}

func sourceName(value string) string {
	if sourceURL(value) != "" {
		return "Original recipe"
	}

	return strings.TrimSpace(value)
}

func yieldFromText(value string) []recipe.Yield {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	return []recipe.Yield{{Text: value}}
}

func timesFromExport(prep, cook string) *recipe.Times {
	prep, cook = strings.TrimSpace(prep), strings.TrimSpace(cook)
	if prep == "" && cook == "" {
		return nil
	}

	times := &recipe.Times{}
	if prep != "" {
		times.Prep = &recipe.DurationExpression{Value: prep}
	}

	if cook != "" {
		times.Cook = &recipe.DurationExpression{Value: cook}
	}

	return times
}

func mergeKeywords(value string, tags []string) []string {
	seen := map[string]bool{}

	var result []string

	for _, entry := range append(strings.Split(value, ","), tags...) {
		entry = strings.TrimSpace(entry)

		key := strings.ToLower(entry)
		if entry == "" || seen[key] {
			continue
		}

		seen[key] = true

		result = append(result, entry)
	}

	return result
}
