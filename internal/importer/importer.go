// Package importer is the format-neutral boundary for bringing recipes from
// third-party applications into Savorum. Each source format is an adapter;
// validation, duplicate handling, persistence, and reporting remain shared.
package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/savorum/savorum/internal/cookbookmanager"
	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref/archivepolicy"
)

// ErrUnsupportedFormat reports that no registered adapter recognizes an input.
var ErrUnsupportedFormat = errors.New("unsupported recipe import format")

// Confidence is an adapter's evidence that it understands an input. Extension
// evidence is deliberately weaker than evidence from the bytes themselves.
type Confidence uint8

// Confidence levels rank extension evidence below recognized content signatures.
const (
	// ConfidenceNone means the adapter found no evidence that it owns the input.
	ConfidenceNone      Confidence = 0
	ConfidenceExtension Confidence = 20
	// ConfidenceHeadings ranks plain recipe headings below structured format signatures.
	ConfidenceHeadings Confidence = 60
	ConfidenceContent  Confidence = 80
	ConfidenceExact    Confidence = 100
)

// Capabilities describe useful source properties to orchestration and UI.
// They never license an adapter to discard a source concept silently.
type Capabilities struct {
	Collection             bool
	Images                 bool
	StructuredIngredients  bool
	StructuredInstructions bool
	SourceIdentity         bool
	Organization           bool
	Refresh                bool
}

// Format identifies an import format and the source concepts it can preserve.
type Format struct {
	InputKinds   []string
	MediaTypes   []string
	ID           string
	Name         string
	Version      string
	Extensions   []string
	Capabilities Capabilities
}

// Input is the transport-neutral source presented to an adapter. A source is
// either already-held bytes or a URL that the adapter knows how to fetch under
// its own bounded source policy. It contains no database or asset-store handle.
type Input struct {
	Name      string
	MediaType string
	URL       string
	Data      []byte
}

// ProbeResult explains an adapter's confidence that it recognizes an input.
type ProbeResult struct {
	Confidence Confidence
	Reason     string
}

// Options supplies resource controls shared by import adapters.
type Options struct {
	// WorkBudget is shared across all sources in one import operation.
	WorkBudget *archivepolicy.Budget
}

func (o Options) workBudget(maximum int64) *archivepolicy.Budget {
	if o.WorkBudget != nil {
		return archivepolicy.NewChildBudget(maximum, o.WorkBudget)
	}

	return archivepolicy.NewBudget(maximum)
}

// Adapter understands one source format and produces candidates only. It has
// no persistence capability: validation, duplicate analysis, preview, asset
// ingestion, revision creation, and commit belong to exchange.Service.
type Adapter interface {
	Format() Format
	Probe(context.Context, Input) ProbeResult
	Import(context.Context, Input, Options) (ImportBatch, error)
}

// Registry selects among a fixed set of import adapters.
type Registry struct {
	adapters []Adapter
}

// ImportBatch is the complete, format-neutral interpretation of one export.
// Documents are portable recipe facts; everything else gives the shared import
// service enough context to persist Savorum-local library facts deliberately.
type ImportBatch struct {
	Source        ImportSource
	Recipes       []ImportedRecipe
	Organizations []ImportedOrganization
	Diagnostics   []Diagnostic
}

// ImportSource records the selected adapter and the kind of supplied input.
type ImportSource struct {
	Format    Format
	InputName string
	InputKind string
}

// ImportedRecipe combines a portable document with source-local import facts.
type ImportedRecipe struct {
	Document                recipe.Document
	SourceKey               string
	Assets                  []RemoteAsset
	EmbeddedAssets          []EmbeddedAsset
	PortableAssetReferences bool
	// ImporterMetadata is source-local state needed for future re-import but
	// is not portable recipe content. Exchange retains it with the candidate;
	// import history owns its eventual storage policy.
	ImporterMetadata        map[string]any
	LibraryMetadata         LibraryMetadata
	OrganizationMemberships []OrganizationMembership
	Diagnostics             []Diagnostic
}

// LibraryMetadata contains facts Savorum understands, but which are not part
// of a portable recipe document. Pointers distinguish an exported false/zero
// from a field the source did not provide.
type LibraryMetadata struct {
	Favorite          *bool              `json:"favorite,omitempty"`
	Rating            *uint8             `json:"rating,omitempty"`
	PersonalNote      *string            `json:"personal_note,omitempty"`
	ImportedCookCount *ImportedCookCount `json:"imported_cook_count,omitempty"`
}

// ImportedCookCount is an aggregate asserted by the source, not fabricated
// history. Source and SourceKey make its provenance visible and auditable.
type ImportedCookCount struct {
	Count     uint64 `json:"count"`
	Source    string `json:"source"`
	SourceKey string `json:"source_key"`
}

// OrganizationMembership assigns an imported recipe to a source organization.
type OrganizationMembership struct {
	Kind      string
	SourceKey string
	Name      string
}

// ImportedOrganization names an organization declared by the source export.
type ImportedOrganization struct{ SourceKey, Name string }

// Severity is the legacy display severity of an import diagnostic.
type Severity string

// Severity values control the legacy display emphasis of a diagnostic.
const (
	// SeverityInfo marks diagnostic information that did not prevent import.
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// DiagnosticClass describes the consequence for the person importing rather
// than an implementation log level. These three answers stay stable across
// adapters: the work stopped, there is a useful action to take, or authored
// content was safely preserved and needs no immediate action.
type DiagnosticClass string

// Diagnostic classes describe whether import stopped or needs follow-up.
const (
	// DiagnosticBlocking means the affected recipe cannot be imported.
	DiagnosticBlocking      DiagnosticClass = "blocking"
	DiagnosticActionable    DiagnosticClass = "actionable"
	DiagnosticInformational DiagnosticClass = "informational"
)

// DiagnosticTarget identifies one affected part of a recipe. ObjectID is a
// portable recipe member identity, so reordering ingredients cannot move a
// diagnostic to a different line.
type DiagnosticTarget struct {
	ObjectID   string                `json:"object_id,omitempty"`
	Field      string                `json:"field,omitempty"`
	Evidence   string                `json:"evidence,omitempty"`
	Preserved  string                `json:"preserved,omitempty"`
	Action     string                `json:"action,omitempty"`
	Resolution *ResolutionDescriptor `json:"resolution,omitempty"`
}

// ResolutionDescriptor marks a diagnostic occurrence as safely bulk
// resolvable. Diagnostics without one remain individual review work.
type ResolutionDescriptor struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	AuthoredForm     string   `json:"authored_form"`
	SourceStart      int      `json:"source_start"`
	SourceEnd        int      `json:"source_end"`
	CandidateUnitIDs []string `json:"candidate_unit_ids"`
	ResolutionKey    string   `json:"resolution_key"`
	OccurrenceKey    string   `json:"occurrence_key"`
}

// Diagnostic is the fidelity-report model shared by every adapter and import
// preview. It records normalization, unsupported content, and actual errors
// without collapsing them into an unstructured log string.
//
// Its receivers are deliberately mixed: UnmarshalJSON has to take a pointer to
// fill the value it decodes into, while the accessors stay on the value so a
// Diagnostic read out of a slice or a map needs no address.
type Diagnostic struct {
	Code     string             `json:"code"`
	Class    DiagnosticClass    `json:"class,omitempty"`
	Message  string             `json:"message"`
	Severity Severity           `json:"severity,omitempty"`
	Area     string             `json:"area,omitempty"`
	Count    int                `json:"count,omitempty"`
	Targets  []DiagnosticTarget `json:"targets,omitempty"`
}

// RemoteAsset describes media an export references but does not carry. The
// shared exchange service fetches and stores it; adapters only identify it.
type RemoteAsset struct {
	URL string `json:"url"`
	// LogicalID is the identifier the stored asset will take, for an adapter
	// whose document already refers to the image from a step. Leave it empty
	// and the exchange names the asset itself.
	LogicalID string `json:"logical_id,omitempty"`
	// RecipeImage says the successfully fetched image belongs in the recipe's
	// ordered image collection. The exchange service appends these references
	// in source order and selects the first successfully fetched one as primary.
	RecipeImage bool                 `json:"recipe_image,omitempty"`
	Role        recipe.AssetRole     `json:"role,omitempty"`
	Alt         string               `json:"alt,omitempty"`
	Caption     string               `json:"caption,omitempty"`
	Creator     string               `json:"creator,omitempty"`
	Credit      string               `json:"credit,omitempty"`
	Rights      string               `json:"rights,omitempty"`
	License     *recipe.AssetLicense `json:"license,omitempty"`
}

// EmbeddedAsset is media carried by an input package. It is verified by the
// source adapter before being returned and is still only a candidate: the
// shared orchestrator decides whether and when to ingest it.
type EmbeddedAsset struct {
	ID        string
	Path      string
	MediaType string
	SHA256    string
	Bytes     []byte
}

// NewRegistry creates a registry that probes adapters in declaration order.
func NewRegistry(adapters ...Adapter) Registry {
	return Registry{adapters: append([]Adapter(nil), adapters...)}
}

// DefaultRegistry is the composition point for source-specific adapters.
// Adding another importer does not change HTTP, CLI, or library services.
func DefaultRegistry() Registry {
	return NewRegistry(newSREFAdapter(), plainTextAdapter{}, webRecipeAdapter{}, cookbookManagerAdapter{}, paprikaAdapter{}, mealieAdapter{}, tandoorAdapter{}, documentAdapter{})
}

// WithPageFetcher returns a registry whose web page adapter retrieves pages
// with fetch.
func (r Registry) WithPageFetcher(fetch PageFetcher) Registry {
	adapters := append([]Adapter(nil), r.adapters...)

	for index, adapter := range adapters {
		if _, ok := adapter.(webRecipeAdapter); ok {
			adapters[index] = webRecipeAdapter{fetch: fetch}
		}
	}

	return Registry{adapters: adapters}
}

// Formats returns immutable adapter declarations for API and UI discovery.
func (r Registry) Formats() []Format {
	formats := make([]Format, len(r.adapters))
	for index, adapter := range r.adapters {
		format := adapter.Format()
		format.Extensions = append([]string(nil), format.Extensions...)
		format.InputKinds = append([]string(nil), format.InputKinds...)

		format.MediaTypes = append([]string(nil), format.MediaTypes...)
		if len(format.InputKinds) == 0 {
			format.InputKinds = []string{"file"}
		}

		formats[index] = format
	}

	return formats
}

// Probe reports the best source match without interpreting it.
func (r Registry) Probe(ctx context.Context, input Input) (Format, ProbeResult, error) {
	candidates := r.candidates(ctx, input)
	if len(candidates) == 0 {
		return Format{}, ProbeResult{}, unsupported(input)
	}

	return candidates[0].adapter.Format(), candidates[0].probe, nil
}

// Import selects adapters by content evidence, then asks them to interpret the
// source in descending confidence order. A misleading extension can never
// outrank a signature or root discriminator in the content.
func (r Registry) Import(ctx context.Context, input Input, options Options) (ImportBatch, error) {
	candidates := r.candidates(ctx, input)
	if len(candidates) == 0 {
		return ImportBatch{}, unsupported(input)
	}

	if _, srefSource := candidates[0].adapter.(srefAdapter); !srefSource {
		if err := options.WorkBudget.Spend(int64(len(input.Data))); err != nil {
			return ImportBatch{}, err
		}
	}

	var failures []error

	for _, candidate := range candidates {
		decoded, err := candidate.adapter.Import(ctx, input, options)
		if err == nil {
			if decoded.Source.Format.ID == "" {
				decoded.Source.Format = candidate.adapter.Format()
			}

			decoded.Source.InputName = input.Name

			decoded.Source.InputKind = "file"
			if input.URL != "" {
				decoded.Source.InputKind = "url"
			}

			return decoded, nil
		}

		failures = append(failures, fmt.Errorf("%s: %w", candidate.adapter.Format().Name, err))
		if candidate.probe.Confidence == ConfidenceExact {
			break
		}
	}

	return ImportBatch{}, fmt.Errorf("import %q: %w", inputName(input), errors.Join(failures...))
}

// Decode imports held bytes using a background context.
func (r Registry) Decode(data []byte, filename string) (ImportBatch, error) {
	return r.DecodeContext(context.Background(), data, filename)
}

// DecodeContext lets adapters that perform bounded parser work observe request
// cancellation while preserving the ordinary decoder contract for simpler
// formats.
func (r Registry) DecodeContext(ctx context.Context, data []byte, filename string) (ImportBatch, error) {
	return r.Import(ctx, Input{Name: filename, Data: data}, Options{})
}

// Supports reports whether any adapter declares the filename's extension.
func (r Registry) Supports(filename string) bool {
	extension := strings.ToLower(filepath.Ext(filename))
	for _, adapter := range r.adapters {
		if slices.Contains(adapter.Format().Extensions, extension) {
			return true
		}
	}

	return false
}

type adapterCandidate struct {
	adapter Adapter
	probe   ProbeResult
	order   int
}

func (r Registry) candidates(ctx context.Context, input Input) []adapterCandidate {
	result := make([]adapterCandidate, 0, len(r.adapters))
	for index, adapter := range r.adapters {
		probe := adapter.Probe(ctx, input)
		if probe.Confidence > ConfidenceNone {
			result = append(result, adapterCandidate{adapter: adapter, probe: probe, order: index})
		}
	}

	sort.SliceStable(result, func(i, j int) bool {
		if result[i].probe.Confidence == result[j].probe.Confidence {
			return result[i].order < result[j].order
		}

		return result[i].probe.Confidence > result[j].probe.Confidence
	})

	return result
}

func unsupported(input Input) error {
	extension := strings.ToLower(filepath.Ext(input.Name))
	if extension == "" {
		extension = input.MediaType
	}

	return fmt.Errorf("%w: %s", ErrUnsupportedFormat, extension)
}

func inputName(input Input) string {
	if strings.TrimSpace(input.Name) != "" {
		return filepath.Base(input.Name)
	}

	if strings.TrimSpace(input.URL) != "" {
		return input.URL
	}

	return "input"
}

func extensionConfidence(format Format, name string) Confidence {
	extension := strings.ToLower(filepath.Ext(name))
	if slices.Contains(format.Extensions, extension) {
		return ConfidenceExtension
	}

	return ConfidenceNone
}

type cookbookManagerAdapter struct{}

func (cookbookManagerAdapter) Format() Format {
	return Format{InputKinds: []string{"file"}, MediaTypes: []string{"application/yaml", "application/zip"}, ID: "cookbookmanager-yaml", Name: "CookbookManager.com", Version: "1", Extensions: []string{".yml", ".yaml", ".zip"}, Capabilities: Capabilities{
		Collection: true, Images: true, StructuredIngredients: true, StructuredInstructions: true,
		SourceIdentity: true, Organization: true, Refresh: true,
	}}
}

func (adapter cookbookManagerAdapter) Probe(_ context.Context, input Input) ProbeResult {
	trimmed := bytes.TrimSpace(input.Data)
	if bytes.HasPrefix(trimmed, []byte("PK\x03\x04")) && zipContainsRecipeYAML(trimmed) {
		return ProbeResult{Confidence: ConfidenceContent, Reason: "archive contains CookbookManager YAML recipes"}
	}

	if (bytes.Contains(trimmed, []byte("\ningredients:")) || bytes.HasPrefix(trimmed, []byte("ingredients:"))) &&
		(bytes.Contains(trimmed, []byte("\ndirections:")) || bytes.HasPrefix(trimmed, []byte("directions:"))) {
		return ProbeResult{Confidence: ConfidenceContent, Reason: "CookbookManager recipe fields are present"}
	}

	return ProbeResult{Confidence: extensionConfidence(adapter.Format(), input.Name), Reason: "filename extension"}
}

func zipContainsRecipeYAML(data []byte) bool {
	if archivepolicy.Preflight(data, 10_000, 8<<20) != nil {
		return false
	}

	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}

	for _, entry := range archive.File {
		name := strings.ToLower(filepath.ToSlash(entry.Name))
		if !entry.FileInfo().IsDir() && (strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
			return true
		}
	}

	return false
}

func (cookbookManagerAdapter) Import(ctx context.Context, input Input, _ Options) (ImportBatch, error) {
	data, filename := input.Data, inputName(input)

	var documents []recipe.Document

	metadata := map[recipe.RecipeID]struct {
		favorite  bool
		cookCount int
	}{}

	switch strings.ToLower(filepath.Ext(filename)) {
	case ".yml", ".yaml":
		document, err := cookbookmanager.Decode(data, filepath.Base(filename))
		if err != nil {
			return ImportBatch{}, err
		}

		documents = []recipe.Document{document}

		favorite, cookCount, err := cookbookmanager.LibraryMetadata(data, filepath.Base(filename))
		if err != nil {
			return ImportBatch{}, err
		}

		metadata[document.ID] = struct {
			favorite  bool
			cookCount int
		}{favorite, cookCount}
	case ".zip":
		imports, err := cookbookmanager.DecodeArchiveImportsContext(ctx, data)
		if err != nil {
			return ImportBatch{}, err
		}

		for _, imported := range imports {
			documents = append(documents, imported.Document)
			metadata[imported.Document.ID] = struct {
				favorite  bool
				cookCount int
			}{imported.Favorite, imported.CookCount}
		}
	default:
		return ImportBatch{}, ErrUnsupportedFormat
	}

	decoded := ImportBatch{Recipes: make([]ImportedRecipe, 0, len(documents))}
	for _, document := range documents {
		decodedMetadata := metadata[document.ID]
		favorite, cookCount := decodedMetadata.favorite, decodedMetadata.cookCount
		remoteURLs := cookbookManagerImageURLs(document)
		importerMetadata := cookbookManagerMetadata(document)
		// CookbookManager's image addresses are useful to the import pipeline,
		// but they are not a portable SREF extension. Keep them beside the
		// candidate with the other importer-local facts, rather than persisting
		// a recipe extension that no independent SREF reader can accept.
		delete(document.Extensions, "x-cookbookmanager")

		if len(document.Extensions) == 0 {
			document.Extensions = nil
		}

		sourceKey := ""
		if document.Source != nil {
			sourceKey = document.Source.SourceID
		}

		item := ImportedRecipe{
			Document: document, SourceKey: sourceKey, ImporterMetadata: importerMetadata,
			LibraryMetadata: LibraryMetadata{Favorite: &favorite},
		}
		if targets := unresolvedIngredientTargets(document); len(targets) > 0 {
			count := len(targets)
			item.Diagnostics = append(item.Diagnostics, Diagnostic{
				Code: "ingredient-review-required", Class: DiagnosticActionable,
				Severity: SeverityWarning, Area: "ingredients", Count: count, Targets: targets,
				Message: fmt.Sprintf("Review %d ambiguous ingredient %s.", count, plural(count, "amount", "amounts")),
			})
		}

		if cookCount > 0 {
			item.LibraryMetadata.ImportedCookCount = &ImportedCookCount{Count: uint64(cookCount), Source: "CookbookManager.com", SourceKey: sourceKey}
		}

		for _, remoteURL := range remoteURLs {
			item.Assets = append(item.Assets, RemoteAsset{URL: remoteURL, RecipeImage: true, Role: recipe.AssetRoleImage, Alt: document.Title})
			break // CookbookManager's first image is the recipe cover.
		}

		decoded.Recipes = append(decoded.Recipes, item)
	}

	return decoded, nil
}

func cookbookManagerMetadata(document recipe.Document) map[string]any {
	extension, ok := document.Extensions["x-cookbookmanager"].(map[string]any)
	if !ok {
		return nil
	}

	result := make(map[string]any, len(extension))
	maps.Copy(result, extension)

	return result
}

// ReadingLocale is the locale one document's ingredient lines are read under:
// the stated locale, or else the alias vocabulary's language. Every stage of
// an import must use it, so an answer is applied under the reading that found
// the ambiguity.
func ReadingLocale(document recipe.Document) string {
	if locale := strings.TrimSpace(document.SourceLocale); locale != "" {
		return locale
	}

	return ingredientparser.AliasLanguage
}

func unresolvedIngredientTargets(document recipe.Document) []DiagnosticTarget {
	var targets []DiagnosticTarget

	for _, section := range document.IngredientSections {
		for _, ingredient := range section.Ingredients {
			if ingredient.Normalization != nil && ingredient.Normalization.Method == recipe.NormalizationUnresolved {
				target := DiagnosticTarget{
					ObjectID: ingredient.ID, Field: "quantity", Evidence: ingredient.SourceText,
					Preserved: "The ingredient line was kept as written.",
					Action:    "Correct the amount or keep the authored wording.",
				}

				parsed := ingredientparser.ParseDetailed(ingredient.SourceText, ingredient.ID, ReadingLocale(document))
				if len(parsed.Ambiguities) == 0 {
					targets = append(targets, target)
					continue
				}

				for _, ambiguity := range parsed.Ambiguities {
					resolved := target
					resolved.Resolution = &ResolutionDescriptor{
						ID: ambiguity.ID, Kind: ambiguity.Kind, AuthoredForm: ambiguity.AuthoredForm,
						SourceStart: ambiguity.SourceStart, SourceEnd: ambiguity.SourceEnd,
						CandidateUnitIDs: append([]string(nil), ambiguity.CandidateUnitIDs...),
						ResolutionKey:    ambiguity.ResolutionKey, OccurrenceKey: ambiguity.OccurrenceKey,
					}
					targets = append(targets, resolved)
				}
			}
		}
	}

	return targets
}

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}

	return many
}

func cookbookManagerImageURLs(document recipe.Document) []string {
	extension, ok := document.Extensions["x-cookbookmanager"].(map[string]any)
	if !ok {
		return nil
	}

	seen := map[string]bool{}

	var result []string

	add := func(value string) {
		value = strings.TrimSpace(value)
		if (strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://")) && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	if value, ok := extension["image_url"].(string); ok {
		add(value)
	}

	if values, ok := extension["images"].([]string); ok {
		for _, value := range values {
			add(value)
		}
	}

	return result
}
