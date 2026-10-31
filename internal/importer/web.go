package importer

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/importer/webrecipe"
	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/recipe"
	"golang.org/x/text/language"
)

// webRecipeAdapter adapts the web extraction pipeline to the common importer,
// so a saved page imports through the same stages as a pasted link. It is the
// only place that decides what Savorum keeps; extraction preserves the page.
type webRecipeAdapter struct{ fetch PageFetcher }

// PageFetcher retrieves a recipe page. The default applies the outbound policy
// in webrecipe.Fetch; a replacement is responsible for its own.
type PageFetcher func(ctx context.Context, rawURL string) (*webrecipe.Page, error)

func (webRecipeAdapter) Format() Format {
	return Format{InputKinds: []string{"file", "url"}, MediaTypes: []string{"text/html", "application/ld+json", "application/json"}, ID: "web-recipe", Name: "Recipe web page", Version: "1", Extensions: []string{".json", ".jsonld", ".html", ".htm"}, Capabilities: Capabilities{
		Images: true, StructuredIngredients: true, StructuredInstructions: true, SourceIdentity: true, Refresh: true,
	}}
}

func (adapter webRecipeAdapter) Probe(_ context.Context, input Input) ProbeResult {
	if strings.TrimSpace(input.URL) != "" {
		return ProbeResult{Confidence: ConfidenceExact, Reason: "public recipe URL"}
	}

	trimmed := strings.TrimSpace(string(input.Data))

	lower := strings.ToLower(trimmed)
	switch {
	case strings.Contains(lower, `"@type"`) && strings.Contains(strings.ToLower(trimmed), "recipe"):
		return ProbeResult{Confidence: ConfidenceContent, Reason: "JSON-LD recipe discriminator"}
	case (strings.Contains(lower, "<html") || strings.Contains(lower, "<script")) &&
		(strings.Contains(lower, "ld+json") || strings.Contains(lower, "schema.org/recipe")):
		return ProbeResult{Confidence: ConfidenceContent, Reason: "HTML recipe structured data"}
	default:
		return ProbeResult{Confidence: extensionConfidence(adapter.Format(), input.Name), Reason: "filename extension"}
	}
}

func (adapter webRecipeAdapter) Import(ctx context.Context, input Input, _ Options) (ImportBatch, error) {
	if strings.TrimSpace(input.URL) != "" {
		fetch := adapter.fetch
		if fetch == nil {
			return FetchWebRecipe(ctx, input.URL)
		}

		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()

		return fetchWebRecipe(ctx, input.URL, fetch)
	}

	contentType := input.MediaType
	if strings.TrimSpace(contentType) == "" {
		contentType = contentTypeForFilename(input.Name)
	}

	return DecodeWebRecipeContext(ctx, input.Data, contentType, "")
}

// DecodeWebRecipe extracts a recipe from a page whose bytes are already held.
// documentURL may be empty; when it is known it improves candidate selection,
// because the page's own address is the strongest evidence of which of several
// recipes on it is the one being asked for.
func DecodeWebRecipe(data []byte, contentType, documentURL string) (ImportBatch, error) {
	return DecodeWebRecipeContext(context.Background(), data, contentType, documentURL)
}

// DecodeWebRecipeContext decodes structured recipe data while observing cancellation.
func DecodeWebRecipeContext(ctx context.Context, data []byte, contentType, documentURL string) (ImportBatch, error) {
	batch, post, err := decodeWebPage(ctx, webrecipe.Load(data, contentType, documentURL))
	if err != nil {
		return captionDraft(ctx, documentURL, post, err)
	}

	return batch, nil
}

// FetchWebRecipe is the URL import entry point. Fetch and extraction failures
// stay distinguishable, because only a refused fetch is worth retrying.
func FetchWebRecipe(ctx context.Context, rawURL string) (ImportBatch, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	return fetchWebRecipe(ctx, rawURL, webrecipe.Fetch)
}

func fetchWebRecipe(ctx context.Context, rawURL string, fetch func(context.Context, string) (*webrecipe.Page, error)) (ImportBatch, error) {
	if address := postFeedURL(rawURL); address != "" {
		page, err := fetch(ctx, address)
		if failure, ok := errors.AsType[*webrecipe.Failure](err); ok && failure.StatusCode == http.StatusTooManyRequests {
			return ImportBatch{}, err
		}

		if err == nil && page.ContentType == "application/atom+xml" {
			if batch, err := fetchSharedRecipe(ctx, page, fetch); err == nil {
				return batch, nil
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return ImportBatch{}, err
	}

	page, err := fetch(ctx, rawURL)
	if err != nil {
		return ImportBatch{}, err
	}

	return fetchSharedRecipe(ctx, page, fetch)
}

func contentTypeForFilename(filename string) string {
	switch {
	case strings.HasSuffix(strings.ToLower(filename), ".json"):
		return "application/json"
	case strings.HasSuffix(strings.ToLower(filename), ".jsonld"):
		return "application/ld+json"
	}

	return "text/html"
}

func batchFor(source *webrecipe.Recipe) ImportBatch {
	return ImportBatch{Recipes: []ImportedRecipe{mapWebRecipe(source, time.Now().UTC())}}
}

// webReadingNotes are the web reader's account of how it read a page, which
// leave the reviewer nothing to decide.
var webReadingNotes = map[string]bool{
	"ingredients-enriched":          true,
	"instructions-enriched":         true,
	"ingredient-headings-recovered": true,
	"recipe-card-only":              true,
	"semantic-html":                 true,
}

// mapWebRecipe turns the extracted recipe into a Savorum document. Every
// decision about what cannot be carried is made here and reported as a
// warning, so a reviewer is told what the page had rather than left to notice
// what the recipe lacks.
func mapWebRecipe(source *webrecipe.Recipe, retrievedAt time.Time) ImportedRecipe {
	sourceLanguage := source.Language
	if sourceLanguage != "" {
		if tag, err := language.Parse(sourceLanguage); err == nil {
			sourceLanguage = tag.String()
		}
	}

	diagnostics := make([]Diagnostic, 0, len(source.Warnings))
	for _, warning := range source.Warnings {
		diagnostic := Diagnostic{Code: warning.Code, Message: warning.Message}
		if webReadingNotes[warning.Code] {
			diagnostic.Class = DiagnosticInformational
			diagnostic.Severity = SeverityInfo
		}

		if warning.Code == "semantic-html-review" {
			diagnostic.Class = DiagnosticActionable
			diagnostic.Severity = SeverityWarning
			diagnostic.Area = "source"
		}

		diagnostics = append(diagnostics, diagnostic)
	}

	sourceKey := source.URL
	if sourceKey == "" {
		sourceKey = source.SourceID
	}

	published, modified, chronologyDiagnostics := webChronology(source.Published, source.Modified)
	diagnostics = append(diagnostics, chronologyDiagnostics...)

	digest := sha256.Sum256([]byte(sourceKey + "\x00" + source.Name))
	document := recipe.Document{
		SchemaVersion:  recipe.CurrentDocumentSchemaVersion,
		ID:             recipe.RecipeID(fmt.Sprintf("web.%x", digest[:8])),
		Title:          source.Name,
		Description:    source.Description,
		Language:       sourceLanguage,
		SourceLocale:   sourceLanguage,
		Authors:        webAuthors(source.Authors),
		Categories:     source.Categories,
		Keywords:       source.Keywords,
		Cuisines:       source.Cuisines,
		CookingMethods: source.CookingMethods,
		Published:      published,
		Modified:       modified,
		Notes:          source.Notes,
		Equipment:      webEquipment(source.Equipment),
		Source:         webSource(source, retrievedAt),
		Yields:         webYields(source.Yields),
		Nutrition:      webNutrition(source.Nutrition),
		DietaryClaims:  webDietaryClaims(source.Diets),
	}
	times, timeDiagnostics := webTimes(source.Times)
	document.Times = times

	diagnostics = append(diagnostics, timeDiagnostics...)

	ingredients := mapIngredients(source.IngredientGroups, document.SourceLocale)
	document.IngredientSections = ingredients.sections
	sections, stepWarnings := mapInstructions(source.Sections, ingredients)
	document.InstructionSections = sections

	diagnostics = append(diagnostics, stepWarnings...)
	diagnostics = append(diagnostics, ingredientMeasurementDiagnostics(document)...)

	if len(document.IngredientSections) == 0 {
		diagnostics = append(diagnostics, Diagnostic{
			Code: "no-ingredients", Class: DiagnosticInformational,
			Message: "No ingredient list was recovered; review source warnings for extraction loss.",
		})
	}

	if len(document.InstructionSections) == 0 {
		diagnostics = append(diagnostics, Diagnostic{
			Code: "no-instructions", Class: DiagnosticInformational,
			Message: "No instructions were recovered; review source warnings for extraction loss.",
		})
	}

	item := ImportedRecipe{Document: document, SourceKey: sourceKey, Diagnostics: diagnostics}
	if source.ExtractionMethod != "" || source.ReviewText != "" {
		item.ImporterMetadata = map[string]any{
			"extraction_method": source.ExtractionMethod,
			"metadata_source":   source.MetadataSource,
		}
	}

	if source.ReviewText != "" {
		item.ImporterMetadata["source_text"] = source.ReviewText
	}

	item.Assets = webAssets(source, document.Title)
	// Rewrites the addresses the steps are holding into the identifiers the
	// stored assets will take, on the document the batch actually carries.
	item.Assets = append(item.Assets, stepAssets(&item.Document)...)

	return item
}

func webEquipment(values []string) []recipe.Equipment {
	result := make([]recipe.Equipment, 0, len(values))
	for _, value := range values {
		if name := strings.TrimSpace(value); name != "" {
			result = append(result, recipe.Equipment{Name: name})
		}
	}

	return result
}

// stepAssets registers the photographs individual steps carry. The step already
// refers to them by the identifier named here, and the library prunes a
// reference whose bytes never arrived, so a photograph that will not download
// costs the step its picture and nothing else.
func stepAssets(document *recipe.Document) []RemoteAsset {
	var assets []RemoteAsset

	for sectionIndex := range document.InstructionSections {
		steps := document.InstructionSections[sectionIndex].Steps
		for stepIndex := range steps {
			for imageIndex, address := range steps[stepIndex].ImageRefs {
				logicalID := fmt.Sprintf("%s-image-%d", steps[stepIndex].ID, imageIndex+1)
				assets = append(assets, RemoteAsset{
					LogicalID: logicalID, URL: address,
					Role: recipe.AssetRoleImage, Alt: document.Title,
				})
				steps[stepIndex].ImageRefs[imageIndex] = logicalID
			}
		}
	}

	return assets
}

func webSource(source *webrecipe.Recipe, retrievedAt time.Time) *recipe.Source {
	result := &recipe.Source{URL: source.URL, RetrievedAt: retrievedAt.Format(time.RFC3339)}
	if parsed, err := url.Parse(source.URL); err == nil {
		result.Name = strings.TrimPrefix(parsed.Hostname(), "www.")
	}

	if result.URL == "" && result.Name == "" && result.Attribution == "" {
		return nil
	}

	return result
}

func webAuthors(authors []webrecipe.Author) []recipe.Author {
	if authors == nil {
		return nil
	}

	result := make([]recipe.Author, len(authors))
	for index, author := range authors {
		result[index] = recipe.Author{Name: author.Name, Kind: recipe.AuthorKind(author.Kind), URL: author.URL}
	}

	return result
}

func webTimes(value webrecipe.Times) (*recipe.Times, []Diagnostic) {
	var (
		diagnostics []Diagnostic
		wording     []string
	)

	read := func(label, stated string) *recipe.DurationExpression {
		expression, exact := importedDuration(stated)
		if expression != nil && !exact {
			wording = append(wording, fmt.Sprintf("%s (%q)", label, expression.Text))
		}

		return expression
	}
	times := &recipe.Times{
		Prep:       read("preparation", value.Prep),
		Cook:       read("cooking", value.Cook),
		Additional: read("additional", value.Additional),
		Total:      read("total", value.Total),
	}

	if len(wording) > 0 {
		diagnostics = append(diagnostics, Diagnostic{
			Code: "duration-not-iso", Severity: SeverityInfo, Area: "normalization",
			Message: "This page stated " + strings.Join(wording, ", ") +
				" in words rather than as a machine-readable duration. The wording is kept as written.",
		})
	}

	if times.Prep == nil && times.Cook == nil && times.Additional == nil && times.Total == nil {
		return nil, diagnostics
	}

	return times, diagnostics
}

// importedDuration lifts a JSON-LD scalar into a duration expression, and
// reports whether the page gave an exact one.
//
// Schema.org says these are ISO 8601, but many pages say "about 3 hours";
// such wording is kept as an opaque duration rather than guessed at.
func importedDuration(value string) (*recipe.DurationExpression, bool) {
	stated := strings.TrimSpace(value)
	if stated == "" {
		return nil, true
	}

	if isoDuration.MatchString(stated) && stated != "P" && !strings.HasSuffix(stated, "T") {
		return &recipe.DurationExpression{Value: stated}, true
	}

	return &recipe.DurationExpression{Text: stated}, false
}

// isoDuration is the same shape the recipe model validates against.
var isoDuration = regexp.MustCompile(`^P(?:[0-9]+Y)?(?:[0-9]+M)?(?:[0-9]+W)?(?:[0-9]+D)?(?:T(?:[0-9]+H)?(?:[0-9]+M)?(?:[0-9]+S)?)?$`)

// webChronology lifts a page's publication and modification dates into the
// portable document. SREF forbids inventing a missing offset, so a local
// date-time is reduced to its date and a non-date is dropped, each reported.
// Both fields share one diagnostic per kind of loss, because diagnostics
// sharing a code are later aggregated into one.
func webChronology(statedPublished, statedModified string) (string, string, []Diagnostic) {
	type reading struct {
		label, stated, kept string
	}

	readings := []reading{
		{label: "publication date", stated: strings.TrimSpace(statedPublished)},
		{label: "modification date", stated: strings.TrimSpace(statedModified)},
	}

	var untimed, unreadable chronologyLoss

	for index := range readings {
		stated := readings[index].stated
		switch {
		case stated == "":
		case portableDate.MatchString(stated) && parsableDate(stated):
			readings[index].kept = stated
		default:
			match := localDateTime.FindStringSubmatch(stated)
			if match != nil && parsableDate(match[1]) {
				readings[index].kept = match[1]
				untimed.record(readings[index].label, stated, match[1])

				continue
			}

			unreadable.record(readings[index].label, stated, "")
		}
	}

	var diagnostics []Diagnostic

	if len(untimed.targets) > 0 {
		diagnostics = append(diagnostics, untimed.diagnostic("chronology-without-time-zone",
			" as a time of day with no time zone. The date is kept; the time is not, because no time zone can be assumed for it."))
	}

	if len(unreadable.targets) > 0 {
		diagnostics = append(diagnostics, unreadable.diagnostic("chronology-unreadable",
			", which is not a date Savorum can carry. The rest of the recipe was imported without it."))
	}

	return readings[0].kept, readings[1].kept, diagnostics
}

// chronologyLoss gathers the fields that lost something, so both are named by
// one diagnostic rather than by two that later aggregate into one.
type chronologyLoss struct {
	targets []DiagnosticTarget
	wording []string
}

func (loss *chronologyLoss) record(label, stated, preserved string) {
	loss.targets = append(loss.targets, DiagnosticTarget{Field: label, Evidence: stated, Preserved: preserved})
	loss.wording = append(loss.wording, label+" ("+strconv.Quote(stated)+")")
}

func (loss *chronologyLoss) diagnostic(code, consequence string) Diagnostic {
	return Diagnostic{
		Code: code, Class: DiagnosticInformational, Severity: SeverityInfo,
		Area: "normalization", Count: len(loss.targets), Targets: loss.targets,
		Message: "This page stated its " + strings.Join(loss.wording, " and ") + consequence,
	}
}

// parsableDate reports whether a correctly shaped value also names a day that
// happened. "2024-02-30" reaches the grammar and denotes nothing.
func parsableDate(value string) bool {
	normalized := strings.ToUpper(value)
	if len(normalized) == len(time.DateOnly) {
		_, err := time.Parse(time.DateOnly, normalized)

		return err == nil
	}

	_, err := time.Parse(time.RFC3339Nano, normalized)

	return err == nil
}

// portableDate is what the recipe model accepts: an RFC 3339 full date, or a
// date-time carrying its offset. localDateTime is the same date-time with the
// mandatory offset missing, and captures the date that can still be kept.
var (
	portableDate  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}([Tt]\d{2}:\d{2}:\d{2}(\.\d+)?([Zz]|[+-]\d{2}:\d{2}))?$`)
	localDateTime = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})[Tt ]\d{2}:\d{2}(:\d{2})?(\.\d+)?$`)
)

// webAssets preserves every distinct usable recipe image in source order.
// Schema.org arrays are ordered, and discarding later members loses authored
// gallery relationships. The exchange chooses the largest usable resize of
// each photograph and the first surviving photograph as primary, so a broken
// URL does not strand the rest.
func webAssets(source *webrecipe.Recipe, title string) []RemoteAsset {
	result := make([]RemoteAsset, 0, len(source.Images))
	for index, image := range source.Images {
		result = append(result, withImageProvenance(RemoteAsset{
			LogicalID: fmt.Sprintf("recipe-image-%d", index+1),
			URL:       image, RecipeImage: true, Role: recipe.AssetRoleImage, Alt: title,
		}, source.ImageProvenance[image]))
	}

	return result
}

// mappedIngredients keeps the lookup that turns a step's stated supply back
// into a reference. Both the whole source line and the ingredient's own name
// are indexed, because a page may state either.
type mappedIngredients struct {
	sections []recipe.IngredientSection
	byText   map[string]string
}

func mapIngredients(groups []webrecipe.IngredientGroup, sourceLocale string) mappedIngredients {
	result := mappedIngredients{byText: map[string]string{}}
	counter := 0

	for groupIndex, group := range groups {
		if len(group.Ingredients) == 0 {
			continue
		}

		section := recipe.IngredientSection{ID: fmt.Sprintf("s%d", groupIndex+1), Title: group.Title}
		for _, ingredient := range group.Ingredients {
			counter++
			id := fmt.Sprintf("i%d", counter)
			parsed := ingredientparser.Parse(ingredient.Text, id, sourceLocale)
			// The page's own name/note split beats one recovered from the
			// rendered line; take both from the card or neither.
			if ingredient.Name != "" && (parsed.Quantity == nil || ingredient.Note != "") {
				parsed.Name = ingredient.Name
			}

			if parsed.Note == "" && ingredient.Note != "" {
				parsed.Note = ingredient.Note
			}

			section.Ingredients = append(section.Ingredients, parsed)
			result.index(id, ingredient.Text, ingredient.Name, parsed.Name)
		}

		result.sections = append(result.sections, section)
	}

	if len(result.sections) == 1 && result.sections[0].Title == "" {
		result.sections[0].ID = "ingredients"
	}

	return result
}

func (m mappedIngredients) index(id string, values ...string) {
	for _, value := range values {
		key := matchKey(value)
		if key == "" {
			continue
		}

		if _, taken := m.byText[key]; !taken {
			m.byText[key] = id
		}
	}
}

// lookup resolves one supply entry. An exact match on the stated text is tried
// first; failing that, the longest indexed name contained in the entry wins,
// which is what matches "3.5 tbsp Ghee" to an ingredient named "ghee".
func (m mappedIngredients) lookup(value string) (string, bool) {
	key := matchKey(value)
	if key == "" {
		return "", false
	}

	if id, found := m.byText[key]; found {
		return id, true
	}

	bestID, bestLength := "", 0
	for indexed, id := range m.byText {
		if len(indexed) > bestLength && len(indexed) >= 3 && strings.Contains(key, indexed) {
			bestID, bestLength = id, len(indexed)
		}
	}

	return bestID, bestID != ""
}

func matchKey(value string) string {
	var builder strings.Builder

	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9':
			builder.WriteRune(character)
		case character == ' ':
			builder.WriteRune(' ')
		}
	}

	return strings.Join(strings.Fields(builder.String()), " ")
}
