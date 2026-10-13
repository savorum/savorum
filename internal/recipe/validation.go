package recipe

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"
)

var (
	portableIDPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	unitIDPattern        = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$`)
	sha256Pattern        = regexp.MustCompile(`^[a-f0-9]{64}$`)
	mediaTypePattern     = regexp.MustCompile(`^[A-Za-z0-9!#$&^_.+-]+/[A-Za-z0-9!#$&^_.+-]+$`)
	languageTagPattern   = regexp.MustCompile(`^[A-Za-z0-9]{1,8}(?:-[A-Za-z0-9]{1,8})*$`)
	assetPathPattern     = regexp.MustCompile(`^assets/[A-Za-z0-9][A-Za-z0-9._/-]{0,510}$`)
	extensionNamePattern = regexp.MustCompile(`^x-[a-z0-9][a-z0-9.-]{0,127}$`)
	// durationPattern is narrower than ISO 8601: no fractional components, so
	// the server and frontend read every accepted value. Years and months are
	// accepted but never summed, having no fixed length.
	durationPattern = regexp.MustCompile(`^P(?:[0-9]+Y)?(?:[0-9]+M)?(?:[0-9]+W)?(?:[0-9]+D)?(?:T(?:[0-9]+H)?(?:[0-9]+M)?(?:[0-9]+S)?)?$`)
	controlPattern  = regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`)
)

// MaximumTimePhases bounds the named phases one recipe may carry.
const MaximumTimePhases = 50

// MaximumExtensions bounds the extension members one recipe may carry.
const MaximumExtensions = 100

// UnitKind distinguishes measurable physical units from containers and count words.
type UnitKind string

// UnitPhysical and related constants enumerate supported UnitKind values.
const (
	UnitPhysical  UnitKind = "physical"
	UnitCount     UnitKind = "count"
	UnitContainer UnitKind = "container"
)

// Dimension identifies the physical quantity measured by a unit.
type Dimension string

// DimensionMass and related constants enumerate supported Dimension values.
const (
	DimensionMass        Dimension = "mass"
	DimensionVolume      Dimension = "volume"
	DimensionTemperature Dimension = "temperature"
	DimensionLength      Dimension = "length"
)

// Unit is the validation metadata for one canonical unit identifier.
type Unit struct {
	ID        string
	Kind      UnitKind
	Dimension Dimension
}

// UnitCatalog resolves canonical unit identifiers during recipe validation.
type UnitCatalog interface {
	Lookup(id string) (Unit, bool)
}

// Violation identifies one invalid recipe field and explains the failed constraint.
type Violation struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
	Value   string `json:"-"`
}

// ValidationError collects every independently actionable violation found in a recipe.
type ValidationError struct {
	Violations []Violation `json:"violations"`
}

// Error lists the first few violations and counts the rest. It is shown to the
// person whose import failed, so it names problems rather than counting them.
func (e *ValidationError) Error() string {
	if len(e.Violations) == 0 {
		return "this recipe is not valid"
	}

	listed := min(len(e.Violations), maximumReportedViolations)

	described := make([]string, 0, listed)
	for _, violation := range e.Violations[:listed] {
		described = append(described, violation.Message+" ("+violation.Path+")")
	}

	sentence := "this recipe cannot be stored as it stands: " + strings.Join(described, "; ")
	if remaining := len(e.Violations) - listed; remaining > 0 {
		sentence += fmt.Sprintf("; and %d more", remaining)
	}

	return sentence
}

const maximumReportedViolations = 5

// ValidateForWrite applies the current authoring policy. Stored historical
// documents are decoded and upcast by recipecodec instead of being rechecked
// against rules that may have changed since they were written.
func ValidateForWrite(document Document, units UnitCatalog) error {
	collector := &violationCollector{}
	validation := writeValidation{collector: collector, units: units}
	validation.metadata(document)
	validation.ingredients(document)
	validation.instructions(document)
	validation.assetsAndImages(document)
	collector.srefOpaque(document, validation.identities())

	if len(collector.violations) > 0 {
		return &ValidationError{Violations: collector.violations}
	}

	return nil
}

type imageReference struct {
	path string
	id   string
}

type writeValidation struct {
	collector             *violationCollector
	units                 UnitCatalog
	ingredientIDs         map[string]struct{}
	choiceIDs             map[string]bool
	ingredientSectionIDs  map[string]struct{}
	instructionSectionIDs map[string]struct{}
	stepIDs               map[string]struct{}
	imageReferences       []imageReference
	assetIDs              map[string]struct{}
	assertionIDs          map[string]struct{}
}

func (v *writeValidation) identities() map[string]map[string]struct{} {
	return map[string]map[string]struct{}{
		"section": v.ingredientSectionIDs, "instructions": v.instructionSectionIDs,
		"ingredient": v.ingredientIDs, "step": v.stepIDs, "asset": v.assetIDs,
		"timing": v.assertionIDs,
	}
}

func (v *writeValidation) metadata(document Document) {
	collector, units := v.collector, v.units
	if document.SchemaVersion != CurrentDocumentSchemaVersion {
		collector.add("schema_version", "schema_version.current", fmt.Sprintf("schema version must be %d", CurrentDocumentSchemaVersion))
	}

	collector.portableID("id", string(document.ID))
	collector.text("title", document.Title, 1000)
	collector.optionalText("description", document.Description, 1_000_000)
	collector.languageTag("language", document.Language)
	collector.languageTag("source_locale", document.SourceLocale)
	collector.authors("authors", document.Authors)
	collector.stringList("categories", document.Categories, 1000, 1000, true)
	collector.stringList("courses", document.Courses, 1000, 1000, true)
	collector.stringList("keywords", document.Keywords, 1000, 1000, true)
	collector.stringList("cuisines", document.Cuisines, 1000, 1000, true)
	collector.stringList("cooking_methods", document.CookingMethods, 1000, 1000, true)
	collector.dateOrDateTime("published", document.Published)
	collector.dateOrDateTime("modified", document.Modified)
	collector.stringList("notes", document.Notes, 1000, 1_000_000, false)
	collector.variants("variants", document.Variants)

	if len(document.Equipment) > 1000 {
		collector.add("equipment", "equipment.too_many", "equipment exceeds 1000 items")
	}

	for index, equipment := range document.Equipment {
		path := fmt.Sprintf("equipment[%d]", index)
		collector.text(path+".name", equipment.Name, 1000)
		collector.optionalText(path+".note", equipment.Note, 1_000_000)

		if equipment.Quantity != nil {
			collector.quantity(path+".quantity", *equipment.Quantity, units)
		}
	}

	collector.translations(document)

	if document.Source != nil {
		collector.source("source", *document.Source)
	}

	if len(document.Yields) > 100 {
		collector.add("yields", "yields.too_many", "yields exceed 100 items")
	}

	for index, value := range document.Yields {
		path := fmt.Sprintf("yields[%d]", index)
		if value.Text == "" && value.Quantity == nil {
			collector.add(path, "yield.empty", "yield requires text or a quantity")
		}

		if value.Quantity != nil {
			collector.quantity(path+".quantity", *value.Quantity, units)
		}

		collector.optionalText(path+".text", value.Text, 1000)
		collector.optionalText(path+".label", value.Label, 1000)
	}

	collector.nutrition(document.Nutrition, units)
	collector.claims(document)

	if document.Times != nil {
		if document.Times.Prep == nil && document.Times.Cook == nil && document.Times.Additional == nil && document.Times.Total == nil && len(document.Times.Assertions) == 0 {
			collector.add("times", "times.empty", "times requires at least one duration")
		}

		collector.durationExpression("times.prep", document.Times.Prep)
		collector.durationExpression("times.cook", document.Times.Cook)
		collector.durationExpression("times.additional", document.Times.Additional)
		collector.durationExpression("times.total", document.Times.Total)

		if len(document.Times.Assertions) > MaximumTimePhases {
			collector.add("times.assertions", "timing_assertions.too_many", fmt.Sprintf("timing assertions exceed %d items", MaximumTimePhases))
		}

		seenAssertions := make(map[string]bool, len(document.Times.Assertions))
		for index, assertion := range document.Times.Assertions {
			path := fmt.Sprintf("times.assertions[%d]", index)
			if seenAssertions[assertion.ID] {
				collector.add(path+".id", "timing_assertion.id_duplicate", "timing assertion IDs must be unique")
			}

			seenAssertions[assertion.ID] = true
			collector.timingAssertion(path, assertion)
		}
	}

	collector.extensions("extensions_raw", document.Extensions)
}

func (v *writeValidation) ingredients(document Document) {
	collector, units := v.collector, v.units

	if len(document.IngredientSections) > 1000 {
		collector.add("ingredient_sections", "sections.too_many", "ingredient sections exceed 1000 items")
	}

	v.ingredientIDs = make(map[string]struct{})
	v.choiceIDs = make(map[string]bool)
	v.ingredientSectionIDs = make(map[string]struct{})

	for sectionIndex, section := range document.IngredientSections {
		path := fmt.Sprintf("ingredient_sections[%d]", sectionIndex)
		collector.uniqueID(path+".id", section.ID, v.ingredientSectionIDs, "ingredient section")
		collector.optionalText(path+".title", section.Title, 1000)

		if len(section.Ingredients) == 0 {
			collector.add(path+".ingredients", "ingredients.required", "at least one ingredient is required")
		}

		if len(section.Ingredients) > 10000 {
			collector.add(path+".ingredients", "ingredients.too_many", "ingredients exceed 10000 items")
		}

		for ingredientIndex, ingredient := range section.Ingredients {
			ingredientPath := fmt.Sprintf("%s.ingredients[%d]", path, ingredientIndex)
			collector.ingredient(ingredientPath, ingredient, v.ingredientIDs, v.choiceIDs, units, false)

			for _, candidate := range append([]Ingredient{ingredient}, ingredient.Alternatives...) {
				if candidate.Recipe != nil && candidate.Recipe.RecipeID != "" && candidate.Recipe.RecipeID == document.ID {
					collector.add(ingredientPath+".recipe.recipe_id", "recipe_reference.self", "a recipe reference names another recipe")
				}
			}
		}
	}
}

func (v *writeValidation) instructions(document Document) {
	if len(document.InstructionSections) > 1000 {
		v.collector.add("instruction_sections", "sections.too_many", "instruction sections exceed 1000 items")
	}

	v.instructionSectionIDs = make(map[string]struct{})
	v.stepIDs = make(map[string]struct{})
	v.imageReferences = nil

	for sectionIndex, section := range document.InstructionSections {
		v.instructionSection(fmt.Sprintf("instruction_sections[%d]", sectionIndex), section)
	}

	v.timingReferences(document)
}

func (v *writeValidation) instructionSection(path string, section InstructionSection) {
	v.collector.uniqueID(path+".id", section.ID, v.instructionSectionIDs, "instruction section")
	v.collector.optionalText(path+".title", section.Title, 1000)

	if len(section.Steps) == 0 {
		v.collector.add(path+".steps", "steps.required", "at least one instruction step is required")
	}

	if len(section.Steps) > 10000 {
		v.collector.add(path+".steps", "steps.too_many", "instruction steps exceed 10000 items")
	}

	for index, step := range section.Steps {
		v.instructionStep(fmt.Sprintf("%s.steps[%d]", path, index), step)
	}
}

func (v *writeValidation) instructionStep(path string, step Step) {
	v.collector.uniqueID(path+".id", step.ID, v.stepIDs, "step")
	v.collector.optionalText(path+".title", step.Title, 1000)
	v.collector.optionalText(path+".text", step.Text, 1_000_000)

	if strings.TrimSpace(step.Title) == "" && strings.TrimSpace(step.Text) == "" {
		v.collector.add(path, "step.content_required", "an instruction step requires a title or text")
	}

	v.ingredientUses(path, step)
	v.stepImages(path, step)

	for index, temperature := range step.Temperatures {
		v.collector.temperature(fmt.Sprintf("%s.temperatures[%d]", path, index), temperature, v.units)
	}

	if len(step.Temperatures) > 100 {
		v.collector.add(path+".temperatures", "temperatures.too_many", "temperatures exceed 100 items")
	}
}

func (v *writeValidation) ingredientUses(path string, step Step) {
	seen := make(map[string]struct{})

	if len(step.IngredientUses) > 10000 {
		v.collector.add(path+".ingredient_uses", "references.too_many", "ingredient uses exceed 10000 items")
	}

	for index, use := range step.IngredientUses {
		usePath := fmt.Sprintf("%s.ingredient_uses[%d]", path, index)
		v.collector.portableID(usePath+".ingredient_ref", use.IngredientRef)

		if _, exists := v.ingredientIDs[use.IngredientRef]; !exists {
			v.collector.add(usePath+".ingredient_ref", "reference.dangling", "ingredient reference does not resolve")
		}

		if _, exists := seen[use.IngredientRef]; exists {
			v.collector.add(usePath+".ingredient_ref", "reference.duplicate", "ingredient reference is repeated")
		}

		seen[use.IngredientRef] = struct{}{}
		if use.Quantity != nil {
			if v.choiceIDs[use.IngredientRef] {
				v.collector.add(usePath+".quantity", "choice.use_quantity", "whole-choice uses cannot carry quantities")
			}

			v.collector.quantity(usePath+".quantity", *use.Quantity, v.units)
		}
	}
}

func (v *writeValidation) stepImages(path string, step Step) {
	seen := make(map[string]struct{})

	if len(step.ImageRefs) > 10000 {
		v.collector.add(path+".image_refs", "references.too_many", "image references exceed 10000 items")
	}

	for index, reference := range step.ImageRefs {
		referencePath := fmt.Sprintf("%s.image_refs[%d]", path, index)
		v.collector.portableID(referencePath, reference)

		if _, exists := seen[reference]; exists {
			v.collector.add(referencePath, "reference.duplicate", "image reference is repeated")
		}

		seen[reference] = struct{}{}
		v.imageReferences = append(v.imageReferences, imageReference{path: referencePath, id: reference})
	}
}

func (v *writeValidation) timingReferences(document Document) {
	// Steps point at canonical assertions rather than assertions listing their
	// steps, so the reference check belongs to the step. One event referenced
	// from several steps stays one event; the same event twice inside one step
	// is a mistake, because a step cannot wait for it twice.
	v.assertionIDs = make(map[string]struct{})

	if document.Times != nil {
		for _, assertion := range document.Times.Assertions {
			v.assertionIDs[assertion.ID] = struct{}{}
		}
	}

	for sectionIndex, section := range document.InstructionSections {
		for stepIndex, step := range section.Steps {
			seenReferences := make(map[string]struct{})

			for referenceIndex, reference := range step.TimingRefs {
				path := fmt.Sprintf("instruction_sections[%d].steps[%d].timing_refs[%d]", sectionIndex, stepIndex, referenceIndex)
				v.collector.portableID(path, reference)

				if _, exists := v.assertionIDs[reference]; !exists {
					v.collector.add(path, "reference.dangling", "timing reference does not resolve")
				}

				if _, exists := seenReferences[reference]; exists {
					v.collector.add(path, "reference.duplicate", "timing reference is repeated")
				}

				seenReferences[reference] = struct{}{}
			}

			if len(step.TimingRefs) > MaximumTimePhases {
				v.collector.add(fmt.Sprintf("instruction_sections[%d].steps[%d].timing_refs", sectionIndex, stepIndex), "references.too_many", fmt.Sprintf("timing references exceed %d items", MaximumTimePhases))
			}
		}
	}
}

func (v *writeValidation) assetsAndImages(document Document) {
	v.assetIDs = make(map[string]struct{})
	assetPaths := make(map[string]struct{})
	assets := make(map[string]Asset)

	if len(document.Assets) > 10000 {
		v.collector.add("assets", "assets.too_many", "assets exceed 10000 items")
	}

	for index, asset := range document.Assets {
		v.asset(fmt.Sprintf("assets[%d]", index), asset, assetPaths)
		assets[asset.ID] = asset
	}

	// Check the primary image before the gallery, so a non-image asset reports
	// its cause before the `image_refs` failure that follows from it.
	v.primaryImage(document, assets)
	v.recipeImages(document, assets)

	for _, reference := range v.imageReferences {
		v.resolveImage(reference.path, reference.id, assets, "step image")
	}
}

func (v *writeValidation) asset(path string, asset Asset, assetPaths map[string]struct{}) {
	collector := v.collector
	collector.uniqueID(path+".id", asset.ID, v.assetIDs, "asset")
	collector.assetPath(path+".path", asset.Path, assetPaths)

	if !mediaTypePattern.MatchString(asset.MediaType) || len(asset.MediaType) > 255 {
		collector.add(path+".media_type", "asset.media_type", "asset media type is invalid")
	}

	if !sha256Pattern.MatchString(asset.SHA256) {
		collector.add(path+".sha256", "asset.sha256", "asset digest must be lowercase SHA-256")
	}

	if asset.Size < 0 {
		collector.add(path+".size", "asset.size", "asset size cannot be negative")
	}

	collector.optionalText(path+".creator", asset.Creator, 1000)
	collector.optionalText(path+".credit", asset.Credit, 1_000_000)
	collector.optionalText(path+".rights", asset.Rights, 1_000_000)
	collector.absoluteURL(path+".source_url", asset.SourceURL)

	if asset.License != nil {
		if asset.License.Name == "" && asset.License.URL == "" {
			collector.add(path+".license", "asset.license", "a license has a name, a URL, or both")
		}

		collector.optionalText(path+".license.name", asset.License.Name, 1000)
		collector.absoluteURL(path+".license.url", asset.License.URL)
	}

	switch asset.Role {
	case "", AssetRoleImage, AssetRoleSource, AssetRoleOther:
	default:
		collector.violations = append(collector.violations, Violation{Path: path + ".role", Code: "asset.role", Value: string(asset.Role), Message: fmt.Sprintf("asset role %q is not supported", asset.Role)})
	}

	collector.optionalText(path+".alt", asset.Alt, 1000)
	collector.optionalText(path+".caption", asset.Caption, 1_000_000)
}

func (v *writeValidation) primaryImage(document Document, assets map[string]Asset) {
	if document.PrimaryImage == "" {
		return
	}

	asset, exists := assets[document.PrimaryImage]
	switch {
	case !exists:
		v.collector.add("primary_image", "asset.reference", "primary image does not resolve")
	case !strings.HasPrefix(asset.MediaType, "image/"):
		v.collector.add("primary_image", "asset.media_type", "primary image is not an image")
	case !slices.Contains(document.ImageRefs, document.PrimaryImage):
		v.collector.add("primary_image", "asset.reference", "primary image is not in recipe image references")
	}
}

func (v *writeValidation) recipeImages(document Document, assets map[string]Asset) {
	if len(document.ImageRefs) > 10000 {
		v.collector.add("image_refs", "references.too_many", "recipe image references exceed 10000 items")
	}

	seen := make(map[string]struct{})

	for index, reference := range document.ImageRefs {
		path := fmt.Sprintf("image_refs[%d]", index)
		v.collector.portableID(path, reference)

		if _, exists := seen[reference]; exists {
			v.collector.add(path, "reference.duplicate", "recipe image reference is repeated")
		}

		seen[reference] = struct{}{}
		v.resolveImage(path, reference, assets, "recipe image")
	}
}

func (v *writeValidation) resolveImage(path, reference string, assets map[string]Asset, subject string) {
	asset, exists := assets[reference]
	if !exists {
		v.collector.add(path, "asset.reference", subject+" does not resolve")
	} else if !strings.HasPrefix(asset.MediaType, "image/") {
		v.collector.add(path, "asset.media_type", subject+" is not an image")
	}
}

func (c *violationCollector) srefOpaque(document Document, identities map[string]map[string]struct{}) {
	if len(document.SREFOpaque) > 10000 {
		c.add("sref_opaque_raw", "sref_opaque.too_many", "preserved SREF object scopes exceed 10000 items")
	}

	for address, value := range document.SREFOpaque {
		path := "sref_opaque_raw." + address
		if _, ok := value.(map[string]any); !ok {
			c.add(path, "sref_opaque.shape", "a preserved SREF scope must contain an object of members")
		}

		head, _, _ := strings.Cut(address, "/")
		if head == "document" {
			continue
		}

		prefix, id, identified := strings.Cut(head, ":")

		known, supported := identities[prefix]
		if !identified || !supported {
			c.add(path, "sref_opaque.address", "preserved SREF scope has an invalid address")
			continue
		}

		if _, exists := known[id]; !exists {
			c.add(path, "sref_opaque.detached", "preserved SREF data is detached from its identified object")
		}
	}
}

type violationCollector struct {
	violations []Violation
}

func (c *violationCollector) add(path, code, message string) {
	c.violations = append(c.violations, Violation{Code: code, Path: path, Message: message})
}

func (c *violationCollector) text(path, value string, maximum int) {
	if strings.TrimSpace(value) == "" {
		c.add(path, "text.required", "value cannot be empty")
		return
	}

	c.textContent(path, value, maximum)
}

// variants checks authored alternate realizations. Text is required; nothing
// here interprets a variant.
func (c *violationCollector) variants(path string, values []Variant) {
	if len(values) > 1000 {
		c.add(path, "variants.too_many", "a recipe carries at most 1000 variants")
		return
	}

	for index, variant := range values {
		variantPath := fmt.Sprintf("%s[%d]", path, index)
		if strings.TrimSpace(variant.Text) == "" {
			c.add(variantPath+".text", "variant.text_required", "a variant states what the author wrote")
			continue
		}

		c.text(variantPath+".text", variant.Text, 1_000_000)
		c.optionalText(variantPath+".title", variant.Title, 1000)
	}
}

func (c *violationCollector) optionalText(path, value string, maximum int) {
	if value == "" {
		return
	}

	c.text(path, value, maximum)
}

func (c *violationCollector) textContent(path, value string, maximum int) {
	if len([]rune(value)) > maximum {
		c.add(path, "text.too_long", fmt.Sprintf("value exceeds %d Unicode code points", maximum))
	}

	if controlPattern.MatchString(value) {
		c.add(path, "text.control_character", "value contains a prohibited control character")
	}
}

func (c *violationCollector) stringList(path string, values []string, maximumItems, maximumItemLength int, unique bool) {
	if len(values) > maximumItems {
		c.add(path, "list.too_long", fmt.Sprintf("list exceeds %d items", maximumItems))
	}

	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		itemPath := fmt.Sprintf("%s[%d]", path, index)
		c.text(itemPath, value, maximumItemLength)

		if unique {
			if _, exists := seen[value]; exists {
				c.add(itemPath, "list.duplicate", "value is repeated")
			}

			seen[value] = struct{}{}
		}
	}
}

func (c *violationCollector) dateOrDateTime(path, value string) {
	if value == "" {
		return
	}

	if len(value) == len(time.DateOnly) {
		if _, err := time.Parse(time.DateOnly, value); err == nil {
			return
		}
	}
	// RFC 3339 permits lower-case t and z. Normalize only those separators for
	// validation; the document keeps the exact source spelling and precision.
	normalized := value
	if len(normalized) > len(time.DateOnly) && normalized[10] == 't' {
		normalized = normalized[:10] + "T" + normalized[11:]
	}

	if strings.HasSuffix(normalized, "z") {
		normalized = normalized[:len(normalized)-1] + "Z"
	}

	if _, err := time.Parse(time.RFC3339Nano, normalized); err != nil {
		// Prose ("March 10, 2024") and an impossible day ("2024-02-30") are
		// separate failures with separate remedies.
		if rfc3339Shaped.MatchString(normalized) {
			c.add(path, "chronology.impossible", "value is not a real calendar date")
			return
		}
		// A wall-clock time with no offset is the common third failure. SREF
		// forbids inventing the missing zone.
		if localTimeShaped.MatchString(normalized) {
			c.add(path, "chronology.invalid",
				"date-time must carry a UTC offset or Z, or be given as the date alone")

			return
		}

		c.add(path, "chronology.invalid", "value must be an RFC 3339 full date or date-time")
	}
}

// rfc3339Shaped matches a value that reached the RFC 3339 grammar, whatever it
// then turned out to denote. localTimeShaped matches the same grammar with the
// mandatory offset missing.
var (
	rfc3339Shaped   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}([Tt]\d{2}:\d{2}:\d{2}(\.\d+)?([Zz]|[+-]\d{2}:\d{2}))?$`)
	localTimeShaped = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[Tt ]\d{2}:\d{2}(:\d{2})?(\.\d+)?$`)
)

func (c *violationCollector) authors(path string, authors []Author) {
	if len(authors) > 100 {
		c.add(path, "list.too_long", "authors exceed 100 items")
	}

	for index, author := range authors {
		itemPath := fmt.Sprintf("%s[%d]", path, index)
		c.text(itemPath+".name", author.Name, 1000)

		switch author.Kind {
		case "", AuthorPerson, AuthorOrganization, AuthorOther:
		default:
			c.add(itemPath+".kind", "author.kind", "author kind is not supported")
		}

		if author.KindLabel != "" && author.Kind != AuthorOther {
			c.add(itemPath+".kind_label", "author.kind_label", "author kind label requires kind other")
		}

		c.optionalText(itemPath+".kind_label", author.KindLabel, 1000)

		if author.URL != "" {
			parsed, err := url.Parse(author.URL)
			if err != nil || !parsed.IsAbs() {
				c.add(itemPath+".url", "author.url", "author URL must be absolute")
			}

			c.textContent(itemPath+".url", author.URL, 10000)
		}
	}
}

func (c *violationCollector) languageTag(path, value string) {
	if value == "" {
		return
	}

	if !wellFormedLanguageTag(value) {
		c.add(path, "language.invalid", "value is not a well-formed BCP 47 language tag")
	}
}

func wellFormedLanguageTag(value string) bool {
	if value == "" || len(value) > 255 || !languageTagPattern.MatchString(value) {
		return false
	}

	_, err := language.Parse(value)

	return err == nil
}

func (c *violationCollector) portableID(path, value string) {
	if !portableIDPattern.MatchString(value) {
		c.add(path, "id.invalid", "value is not a portable identifier")
	}
}

func (c *violationCollector) uniqueID(path, value string, seen map[string]struct{}, kind string) {
	c.portableID(path, value)

	if _, exists := seen[value]; exists {
		c.add(path, "id.duplicate", kind+" identifier is repeated")
	}

	seen[value] = struct{}{}
}

func (c *violationCollector) amount(path string, amount Amount, signed bool) {
	exact := amount.Value != nil

	ranged := amount.Min != nil || amount.Max != nil
	if exact == ranged || (ranged && (amount.Min == nil || amount.Max == nil)) {
		c.add(path, "amount.shape", "amount must contain either value or both min and max")
		return
	}

	validate := ParseRational
	if signed {
		validate = ParseSignedRational
	}

	if exact {
		if _, err := validate(amount.Value.String()); err != nil {
			c.add(path+".value", rationalCode(err), err.Error())
		}

		return
	}

	if _, err := validate(amount.Min.String()); err != nil {
		c.add(path+".min", rationalCode(err), err.Error())
	}

	if _, err := validate(amount.Max.String()); err != nil {
		c.add(path+".max", rationalCode(err), err.Error())
	}

	comparison, err := amount.Min.Cmp(*amount.Max)
	if err == nil && comparison > 0 {
		c.add(path, "amount.range_order", "range minimum exceeds maximum")
	}
}

func (c *violationCollector) quantity(path string, quantity Quantity, units UnitCatalog) {
	switch quantity.Kind {
	case QuantitySimple:
		if quantity.Amount == nil {
			c.add(path+".amount", "amount.required", "simple quantity requires an amount")
		} else {
			c.amount(path+".amount", *quantity.Amount, false)
		}

		c.ordinaryUnit(path+".unit", quantity.Unit, units)

		if len(quantity.Terms) != 0 || len(quantity.Options) != 0 || quantity.Text != "" {
			c.add(path, "quantity.shape", "simple quantity contains fields for another quantity kind")
		}
	case QuantitySum:
		if len(quantity.Terms) < 2 {
			c.add(path+".terms", "quantity.terms", "sum requires at least two terms")
		}

		if len(quantity.Terms) > 100 {
			c.add(path+".terms", "quantity.terms", "sum exceeds 100 terms")
		}

		for index, term := range quantity.Terms {
			c.simpleQuantity(fmt.Sprintf("%s.terms[%d]", path, index), term, units)
		}

		if quantity.Amount != nil || quantity.Unit != "" || len(quantity.Options) != 0 || quantity.Text != "" {
			c.add(path, "quantity.shape", "sum contains fields for another quantity kind")
		}
	case QuantityAlternatives:
		if len(quantity.Options) < 2 {
			c.add(path+".options", "quantity.options", "alternatives require at least two options")
		}

		if len(quantity.Options) > 100 {
			c.add(path+".options", "quantity.options", "alternatives exceed 100 options")
		}

		for index, option := range quantity.Options {
			c.alternativeOption(fmt.Sprintf("%s.options[%d]", path, index), option, units)
		}

		if quantity.Amount != nil || quantity.Unit != "" || len(quantity.Terms) != 0 || quantity.Text != "" {
			c.add(path, "quantity.shape", "alternatives contain fields for another quantity kind")
		}
	case QuantityOpaque:
		c.text(path+".text", quantity.Text, 1000)

		if quantity.Amount != nil || quantity.Unit != "" || len(quantity.Terms) != 0 || len(quantity.Options) != 0 {
			c.add(path, "quantity.shape", "opaque quantity contains structured quantity fields")
		}
	default:
		c.add(path+".kind", "quantity.kind", "quantity kind is not supported")
	}
}

func (c *violationCollector) alternativeOption(path string, option AlternativeOption, units UnitCatalog) {
	switch option.Kind {
	case QuantitySimple:
		if option.Amount == nil {
			c.add(path+".amount", "amount.required", "simple alternative option requires an amount")
		} else {
			c.amount(path+".amount", *option.Amount, false)
		}

		c.ordinaryUnit(path+".unit", option.Unit, units)

		if len(option.Terms) != 0 {
			c.add(path, "quantity.shape", "simple alternative option contains sum terms")
		}
	case QuantitySum:
		if len(option.Terms) < 2 {
			c.add(path+".terms", "quantity.terms", "sum alternative option requires at least two terms")
		}

		if len(option.Terms) > 100 {
			c.add(path+".terms", "quantity.terms", "sum alternative option exceeds 100 terms")
		}

		for index, term := range option.Terms {
			c.simpleQuantity(fmt.Sprintf("%s.terms[%d]", path, index), term, units)
		}

		if option.Amount != nil || option.Unit != "" {
			c.add(path, "quantity.shape", "sum alternative option contains simple quantity fields")
		}
	case QuantityAlternatives, QuantityOpaque:
		c.add(path+".kind", "quantity.kind", "alternative option must be simple or sum")
	default:
		c.add(path+".kind", "quantity.kind", "alternative option must be simple or sum")
	}
}

func (c *violationCollector) simpleQuantity(path string, quantity SimpleQuantity, units UnitCatalog) {
	if quantity.Kind != QuantitySimple {
		c.add(path+".kind", "quantity.kind", "quantity term must be simple")
	}

	c.amount(path+".amount", quantity.Amount, false)
	c.ordinaryUnit(path+".unit", quantity.Unit, units)
}

func (c *violationCollector) packageSize(path string, quantity SimpleQuantity, units UnitCatalog) {
	if quantity.Kind != QuantitySimple {
		c.add(path+".kind", "quantity.kind", "package size must be simple")
	}

	c.amount(path+".amount", quantity.Amount, false)
	c.packageUnit(path+".unit", quantity.Unit, units)
}

func (c *violationCollector) ordinaryUnit(path, id string, units UnitCatalog) {
	if id == "" {
		return
	}

	unit, found := c.lookupUnit(path, id, units)
	if found && unit.Dimension == DimensionTemperature {
		c.add(path, "unit.temperature_context", "temperature unit requires a temperature field")
	}
}

func (c *violationCollector) packageUnit(path, id string, units UnitCatalog) {
	unit, found := c.lookupUnit(path, id, units)
	if !found {
		return
	}

	if unit.Kind != UnitPhysical {
		c.add(path, "unit.physical_required", "package size requires a physical unit")
	} else if unit.Dimension == DimensionTemperature {
		c.add(path, "unit.temperature_context", "temperature cannot describe package size")
	}
}

func (c *violationCollector) temperature(path string, temperature Temperature, units UnitCatalog) {
	if temperature.Text != "" {
		c.text(path+".text", temperature.Text, 1000)

		if temperature.Amount.Value != nil || temperature.Amount.Min != nil || temperature.Amount.Max != nil || temperature.Amount.Approximate || temperature.Unit != "" || temperature.SourceText != "" {
			c.add(path, "temperature.opaque_exclusive", "opaque temperature text cannot carry an amount, unit, approximation, or source text")
		}
	} else {
		c.amount(path+".amount", temperature.Amount, true)

		unit, found := c.lookupUnit(path+".unit", temperature.Unit, units)
		if found && (unit.Kind != UnitPhysical || unit.Dimension != DimensionTemperature) {
			c.add(path+".unit", "unit.temperature_required", "temperature field requires a temperature unit")
		}
	}

	if temperature.Purpose != nil {
		purposePath := path + ".purpose"

		switch temperature.Purpose.Kind {
		case TemperaturePurposeOven,
			TemperaturePurposeInternal,
			TemperaturePurposeOil,
			TemperaturePurposeWater,
			TemperaturePurposeSurface:
		case TemperaturePurposeOther:
			if strings.TrimSpace(temperature.Purpose.Label) == "" {
				c.add(purposePath+".label", "temperature.purpose.label", "other temperature purpose requires a label")
			}
		case "":
			// Distinct from an unknown kind: supply one, or use `other` with a
			// label.
			c.add(purposePath+".kind", "temperature.purpose.kind_required", "temperature purpose requires a kind")
		default:
			c.add(purposePath+".kind", "temperature.purpose.kind", "temperature purpose kind is not supported")
		}

		c.optionalText(purposePath+".label", temperature.Purpose.Label, 1000)
	}

	c.optionalText(path+".note", temperature.Note, 1000)
	c.optionalText(path+".source_text", temperature.SourceText, 1000)
}

func (c *violationCollector) lookupUnit(path, id string, units UnitCatalog) (Unit, bool) {
	if !unitIDPattern.MatchString(id) {
		c.add(path, "unit.id", "unit identifier syntax is invalid")
		return Unit{}, false
	}

	if units == nil {
		c.add(path, "unit.catalog", "unit catalog is unavailable")
		return Unit{}, false
	}

	unit, found := units.Lookup(id)
	// A syntactically valid identifier from a newer SREF registry is preserved
	// even when this build cannot interpret it. Callers that need semantics use
	// Catalog.Lookup or Convert and receive an explicit unknown-unit result.
	return unit, found
}

func (c *violationCollector) normalization(path string, ingredient Ingredient) {
	if ingredient.Normalization == nil {
		return
	}

	normalization := ingredient.Normalization
	switch normalization.Method {
	case NormalizationAuthored:
	case NormalizationExtracted:
		if ingredient.SourceText == "" {
			c.add(path+".source_text", "normalization.source", "extracted normalization requires source text")
		}
	case NormalizationInferred, NormalizationUnresolved:
		if ingredient.SourceText == "" {
			c.add(path+".source_text", "normalization.source", "inferred or unresolved normalization requires source text")
		}

		if len(normalization.Warnings) == 0 {
			c.add(path+".normalization.warnings", "normalization.warning", "uncertain normalization requires a warning")
		}
	default:
		c.add(path+".normalization.method", "normalization.method", "normalization method is not supported")
	}

	if normalization.Review != "" && normalization.Review != ReviewConfirmed && normalization.Review != ReviewCorrected {
		c.add(path+".normalization.review", "normalization.review", "review state is not supported")
	}

	c.stringList(path+".normalization.warnings", normalization.Warnings, 100, 1000, true)
}

// timingAssertion checks one canonical timing event. A kind may be absent, but
// "other" must carry the recipe's own label.
func (c *violationCollector) timingAssertion(path string, assertion TimingAssertion) {
	c.portableID(path+".id", assertion.ID)

	if assertion.Kind != "" && !IsTimingKind(assertion.Kind) {
		c.add(path+".kind", "timing_assertion.kind", "timing kind is not supported")
	}

	c.durationExpressionRequired(path+".duration", assertion.Duration)

	switch assertion.Attention {
	case "", AttentionActive, AttentionPassive:
	default:
		c.add(path+".attention", "timing_assertion.attention", "attention must be active or passive")
	}

	switch assertion.Provenance {
	case "", TimingDerived, TimingSupplemental:
	default:
		c.add(path+".provenance", "timing_assertion.provenance", "timing provenance is not supported")
	}

	if assertion.Provenance == TimingDerived && strings.TrimSpace(assertion.Duration.SourceText) == "" {
		c.add(path+".duration.source_text", "timing_assertion.source", "derived timing requires source text")
	}

	if assertion.Kind == TimingOther && strings.TrimSpace(assertion.Label) == "" {
		c.add(path+".label", "timing_assertion.label_required", "an assertion classified as other requires a label")
	}

	c.optionalText(path+".label", assertion.Label, 100)
	c.optionalText(path+".note", assertion.Note, 1000)

	if assertion.Microwave != nil {
		c.microwaveConditions(path+".microwave", assertion.Duration, *assertion.Microwave)
	}
}

func (c *violationCollector) microwaveConditions(path string, duration DurationExpression, value MicrowaveConditions) {
	c.text(path+".source_text", value.SourceText, 1000)

	if value.RatedOutputWatts < 0 || value.RatedOutputWatts > 1_000_000 {
		c.add(path+".rated_output_watts", "microwave.watts", "rated output watts must be a positive integer")
	}

	c.microwavePower(path+".power", value.Power)
	c.optionalText(path+".mode_text", value.ModeText, 1000)
	c.stringList(path+".restrictions", value.Restrictions, 100, 1000, true)

	if len(value.Choices) > 0 && !duration.IsOpaque() {
		c.add(path+".choices", "microwave.choices_opaque_duration", "power/time alternatives require a complete opaque duration")
	}

	if len(value.Choices) > 0 && (value.RatedOutputWatts > 0 || value.Power != nil) {
		c.add(path+".choices", "microwave.choices_shared_power", "power/time alternatives cannot also declare one shared power")
	}

	seen := map[string]bool{}

	for index, choice := range value.Choices {
		choicePath := fmt.Sprintf("%s.choices[%d]", path, index)
		c.durationExpressionRequired(choicePath+".duration", choice.Duration)
		c.text(choicePath+".source_text", choice.SourceText, 1000)
		c.microwavePower(choicePath+".power", choice.Power)

		if choice.RatedOutputWatts < 0 || choice.RatedOutputWatts > 1_000_000 {
			c.add(choicePath+".rated_output_watts", "microwave.watts", "rated output watts must be a positive integer")
		}

		key := fmt.Sprintf("%d:%v", choice.RatedOutputWatts, choice.Power)
		if seen[key] {
			c.add(choicePath, "microwave.choice_duplicate", "microwave choices must use distinct power conditions")
		}

		seen[key] = true
	}
}

func (c *violationCollector) microwavePower(path string, value *MicrowavePower) {
	if value == nil {
		return
	}

	present := 0
	if value.Watts != 0 {
		present++
	}

	if value.Percent != "" {
		present++
	}

	if value.Text != "" {
		present++
	}

	if present != 1 {
		c.add(path, "microwave.power_shape", "microwave power requires exactly one representation")
	}

	if value.Watts < 0 || value.Watts > 1_000_000 {
		c.add(path+".watts", "microwave.watts", "selected watts must be a positive integer")
	}

	if value.Percent != "" {
		percent, err := strconv.Atoi(value.Percent)
		if err != nil || percent <= 0 || percent > 100 {
			c.add(path+".percent", "microwave.percent", "power percent must be a whole number greater than zero and at most 100")
		}
	}

	c.optionalText(path+".text", value.Text, 1000)
}

// durationExpression checks an optional duration expression.
func (c *violationCollector) durationExpression(path string, expression *DurationExpression) {
	if expression == nil {
		return
	}

	c.durationExpressionRequired(path, *expression)
}

// durationExpressionRequired checks a duration expression that must say
// something. The three shapes are exclusive: an opaque expression carries
// wording precisely because it has no numeric reading, so pairing it with one
// would be a claim the source never made.
func (c *violationCollector) durationExpressionRequired(path string, expression DurationExpression) {
	if expression.IsZero() {
		c.add(path, "duration.required", "a duration expression requires a value, a range, or opaque text")
		return
	}

	if expression.IsOpaque() {
		if expression.Value != "" || expression.IsRange() {
			c.add(path, "duration.opaque_exclusive", "opaque duration text cannot carry a value or a range")
		}

		if expression.Approximate {
			c.add(path+".approximate", "duration.opaque_approximate", "opaque duration text cannot be marked approximate")
		}

		c.optionalText(path+".text", expression.Text, 1000)
		c.optionalText(path+".source_text", expression.SourceText, 1000)

		return
	}

	if expression.Value != "" && expression.IsRange() {
		c.add(path, "duration.value_exclusive", "a duration is either an exact value or a range")
	}

	if expression.IsRange() && (expression.Min == "" || expression.Max == "") {
		c.add(path, "duration.range_incomplete", "a duration range requires both a minimum and a maximum")
	}

	c.duration(path+".value", expression.Value)
	c.duration(path+".min", expression.Min)
	c.duration(path+".max", expression.Max)
	c.optionalText(path+".source_text", expression.SourceText, 1000)
}

// extensions checks the names an extension may travel under. SREF treats a
// member that is neither standard nor `x-` prefixed as invalid, so Savorum
// must not manufacture one by storing something else here.
func (c *violationCollector) extensions(path string, values map[string]any) {
	if len(values) > MaximumExtensions {
		c.add(path, "extensions.too_many", fmt.Sprintf("extensions exceed %d members", MaximumExtensions))
	}

	for name := range values {
		if name == "extensions" {
			continue
		}

		if !extensionNamePattern.MatchString(name) {
			c.add(path+"."+name, "extension.name", "an extension member must be named extensions or begin with x-")
		}
	}
}

func (c *violationCollector) duration(path, value string) {
	if value != "" && (!durationPattern.MatchString(value) || value == "P" || strings.HasSuffix(value, "T")) {
		c.add(path, "duration.invalid", "duration must use the supported ISO 8601 form")
	}
}

func (c *violationCollector) source(path string, source Source) {
	if source.Name == "" && source.URL == "" && source.Application == "" && source.SourceID == "" && source.RetrievedAt == "" && source.Attribution == "" {
		c.add(path, "source.empty", "source requires at least one value")
	} else if strings.TrimSpace(source.Name) == "" && strings.TrimSpace(source.URL) == "" && strings.TrimSpace(source.Application) == "" && strings.TrimSpace(source.Attribution) == "" {
		c.add(path, "source.identity", "source requires a name, URL, application, or meaningful attribution")
	}

	if strings.TrimSpace(source.SourceID) != "" && strings.TrimSpace(source.Application) == "" {
		c.add(path+".application", "source.application", "source ID requires its application namespace")
	}

	if source.URL != "" {
		parsed, err := url.Parse(source.URL)
		if err != nil || !parsed.IsAbs() {
			c.add(path+".url", "source.url", "source URL must be absolute")
		}
	}

	if source.RetrievedAt != "" {
		if _, err := time.Parse(time.RFC3339, source.RetrievedAt); err != nil {
			c.add(path+".retrieved_at", "source.retrieved_at", "retrieval time must use RFC 3339")
		}
	}

	c.optionalText(path+".name", source.Name, 1000)
	c.optionalText(path+".application", source.Application, 1000)
	c.optionalText(path+".source_id", source.SourceID, 1000)
	c.optionalText(path+".attribution", source.Attribution, 1_000_000)
}

func (c *violationCollector) translations(document Document) {
	if len(document.Translations) == 0 {
		return
	}

	if len(document.Translations) > 1000 {
		c.add("translations", "translation.too_many", "translations exceed 1000 items")
	}

	if document.Language == "" {
		c.add("language", "translation.document_language", "a recipe that declares translations states its own language")
	}

	originals := 0

	for index, translation := range document.Translations {
		path := fmt.Sprintf("translations[%d]", index)

		switch translation.Relation {
		case TranslationOriginal:
			originals++
		case TranslationOf, TranslationAlternate:
		default:
			c.add(path+".relation", "translation.relation", "relation must be original, translation, or alternate")
		}

		if !wellFormedLanguageTag(translation.Language) {
			c.add(path+".language", "translation.language", "a translation states a well-formed BCP 47 language tag")
		}

		if translation.Title == "" && translation.RecipeID == "" && translation.URL == "" {
			c.add(path, "translation.counterpart", "a translation is identified by title, recipe ID, or URL")
		}

		c.optionalText(path+".title", translation.Title, 1000)

		if translation.RecipeID != "" {
			c.portableID(path+".recipe_id", string(translation.RecipeID))

			if translation.RecipeID == document.ID {
				c.add(path+".recipe_id", "translation.self", "a translation names another recipe")
			}
		}

		if translation.URL != "" {
			parsed, err := url.Parse(translation.URL)
			if err != nil || !parsed.IsAbs() {
				c.add(path+".url", "translation.url", "translation URL must be absolute")
			}
		}
	}

	if originals > 1 {
		c.add("translations", "translation.original", "a recipe declares at most one original")
	}
}

func (c *violationCollector) assetPath(path, value string, seen map[string]struct{}) {
	invalid := value == "" || !assetPathPattern.MatchString(value) || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || controlPattern.MatchString(value)
	for segment := range strings.SplitSeq(value, "/") {
		invalid = invalid || segment == "" || segment == "." || segment == ".."
	}

	if invalid {
		c.add(path, "asset.path", "asset path must be a safe relative path below assets/")
	}

	key := strings.ToLower(value)
	if _, exists := seen[key]; exists {
		c.add(path, "asset.path_duplicate", "asset path is repeated")
	}

	seen[key] = struct{}{}
}

// rationalCode names which SREF canonical-form rule a value broke.
func rationalCode(err error) string {
	switch DefectOf(err) {
	case RationalDenominator:
		return "amount.rational_denominator"
	case RationalUnreduced:
		return "amount.rational_unreduced"
	case RationalOutOfRange:
		return "amount.rational_range"
	case RationalNegativeZero:
		return "amount.rational_negative_zero"
	case RationalSyntax:
		return "amount.rational"
	default:
		return "amount.rational"
	}
}

func (c *violationCollector) ingredient(path string, ingredient Ingredient, ids map[string]struct{}, choices map[string]bool, units UnitCatalog, branch bool) {
	c.uniqueID(path+".id", ingredient.ID, ids, "ingredient")

	if ingredient.Alternatives != nil {
		choices[ingredient.ID] = true

		if branch {
			c.add(path, "choice.shape", "ingredient choices cannot be nested")
			return
		}

		if len(ingredient.Alternatives) < 2 || len(ingredient.Alternatives) > 100 {
			c.add(path+".alternatives", "choice.shape", "a choice requires 2–100 branches")
		}

		if ingredient.Name != "" || ingredient.Quantity != nil || ingredient.PackageSize != nil || ingredient.Temperature != nil {
			c.add(path, "choice.shape", "name, quantity, package size and temperature belong to branches")
		}

		if ingredient.Relation != "or" && ingredient.Relation != "and_or" && ingredient.Relation != "preferred" {
			c.add(path+".relation", "choice.shape", "a choice requires an authored relationship")
		}

		for index, alternative := range ingredient.Alternatives {
			c.ingredient(fmt.Sprintf("%s.alternatives[%d]", path, index), alternative, ids, choices, units, true)
		}
	} else {
		c.text(path+".name", ingredient.Name, 1000)

		if ingredient.Relation != "" {
			c.add(path+".relation", "choice.shape", "a relationship requires alternatives")
		}
	}

	c.optionalText(path+".note", ingredient.Note, 1_000_000)
	c.optionalText(path+".source_text", ingredient.SourceText, 1_000_000)

	if ingredient.Quantity != nil {
		c.quantity(path+".quantity", *ingredient.Quantity, units)
	}

	if ingredient.PackageSize != nil {
		c.packageSize(path+".package_size", *ingredient.PackageSize, units)
	}

	if ingredient.Temperature != nil {
		c.temperature(path+".temperature", *ingredient.Temperature, units)
	}

	if ingredient.Recipe != nil {
		c.recipeReference(path+".recipe", *ingredient.Recipe)
	}

	c.normalization(path, ingredient)
}

func (c *violationCollector) recipeReference(path string, reference RecipeReference) {
	if reference.Title == "" && reference.RecipeID == "" && reference.URL == "" {
		c.add(path, "recipe_reference.identity", "a recipe reference is identified by title, recipe ID, or URL")
	}

	c.optionalText(path+".title", reference.Title, 1000)

	if reference.RecipeID != "" {
		c.portableID(path+".recipe_id", string(reference.RecipeID))
	}

	c.absoluteURL(path+".url", reference.URL)
}

func (c *violationCollector) absoluteURL(path, value string) {
	if value == "" {
		return
	}

	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() {
		c.add(path, "url.absolute", "URL must be absolute")
	}
}

func (c *violationCollector) nutrition(statements []NutritionStatement, units UnitCatalog) {
	if len(statements) > 100 {
		c.add("nutrition", "nutrition.too_many", "nutrition exceeds 100 statements")
	}

	for index, statement := range statements {
		path := fmt.Sprintf("nutrition[%d]", index)
		basis := statement.Basis

		switch basis.Kind {
		case NutritionBasisRecipe, NutritionBasisServing:
		case NutritionBasisQuantity:
			if basis.Quantity == nil {
				c.add(path+".basis.quantity", "nutrition.basis_quantity_required", "a quantity basis states its quantity")
			}
		case NutritionBasisUnspecified:
			if basis.Quantity != nil {
				c.add(path+".basis.quantity", "nutrition.basis_quantity_unspecified", "an unspecified basis carries no quantity")
			}
		default:
			c.add(path+".basis.kind", "nutrition.basis", "basis must be recipe, serving, quantity, or unspecified")
		}

		c.optionalText(path+".basis.text", basis.Text, 1000)

		if basis.Quantity != nil {
			c.quantity(path+".basis.quantity", *basis.Quantity, units)
		}

		c.optionalText(path+".note", statement.Note, 1_000_000)

		if len(statement.Nutrients) == 0 || len(statement.Nutrients) > 200 {
			c.add(path+".nutrients", "nutrition.nutrients", "a statement lists 1–200 nutrients")
		}

		seen := make(map[[2]string]bool, len(statement.Nutrients))
		for nutrientIndex, nutrient := range statement.Nutrients {
			nutrientPath := fmt.Sprintf("%s.nutrients[%d]", path, nutrientIndex)
			key := [2]string{string(nutrient.Nutrient), nutrient.Label}

			if seen[key] {
				c.add(nutrientPath, "nutrient.duplicate", "a statement lists a nutrient and label once")
			}

			seen[key] = true

			c.nutrient(nutrientPath, nutrient)
		}
	}
}

func (c *violationCollector) nutrient(path string, nutrient Nutrient) {
	known := false
	for _, kind := range NutrientKinds {
		known = known || kind == nutrient.Nutrient
	}

	if !known {
		c.add(path+".nutrient", "nutrient.kind", "nutrient is not supported")
	}

	if nutrient.Nutrient == NutrientOther && strings.TrimSpace(nutrient.Label) == "" {
		c.add(path+".label", "nutrient.label", "an other nutrient carries a label")
	}

	c.optionalText(path+".label", nutrient.Label, 1000)
	c.optionalText(path+".source_text", nutrient.SourceText, 1000)
	c.optionalText(path+".text", nutrient.Text, 1000)

	if (nutrient.Amount == nil) == (nutrient.Text == "") {
		c.add(path, "nutrient.value", "a nutrient has exactly one of an amount or text")
	}

	if nutrient.Text != "" && (nutrient.Unit != "" || nutrient.SourceText != "") {
		c.add(path, "nutrient.value", "an opaque nutrient value carries no unit or source text")
	}

	if nutrient.Amount != nil {
		c.amount(path+".amount", *nutrient.Amount, false)
	}

	switch nutrient.Unit {
	case "":
	case NutrientUnitKilocalorie, NutrientUnitKilojoule:
		if nutrient.Nutrient != NutrientEnergy {
			c.add(path+".unit", "nutrient.mass_energy_unit", "only energy is measured in kcal or kJ")
		}
	case NutrientUnitGram, NutrientUnitMilligram, NutrientUnitMicrogram:
		if nutrient.Nutrient == NutrientEnergy {
			c.add(path+".unit", "nutrient.energy_mass_unit", "energy is measured in kcal or kJ")
		}
	default:
		c.add(path+".unit", "nutrient.unit", "nutrient unit is not supported")
	}
}

func (c *violationCollector) claims(document Document) {
	variants := make(map[string]bool, len(document.Variants))
	for index, variant := range document.Variants {
		if variant.ID == "" {
			continue
		}

		path := fmt.Sprintf("variants[%d].id", index)
		c.portableID(path, variant.ID)

		if variants[variant.ID] {
			c.add(path, "variant.id_duplicate", "variant IDs must be unique")
		}

		variants[variant.ID] = true
	}

	scope := func(path, reference string) {
		if reference != "" && !variants[reference] {
			c.add(path+".variant_ref", "reference.dangling", "variant reference does not resolve")
		}
	}

	if len(document.DietaryClaims) > 1000 || len(document.AllergenDeclarations) > 1000 {
		c.add("dietary_claims", "claims.too_many", "claims exceed 1000 items")
	}

	for index, claim := range document.DietaryClaims {
		path := fmt.Sprintf("dietary_claims[%d]", index)
		c.text(path+".text", claim.Text, 1_000_000)
		c.optionalText(path+".condition", claim.Condition, 1_000_000)

		if claim.Suitability == "" {
			c.add(path+".suitability", "dietary_claim.suitability_required", "suitability is required")
		} else if claim.Suitability != SuitabilitySuitable && claim.Suitability != SuitabilityUnsuitable {
			c.add(path+".suitability", "dietary_claim.suitability", "suitability must be suitable or unsuitable")
		}

		if claim.Diet != "" && !slices.Contains(Diets, claim.Diet) {
			c.add(path+".diet", "dietary_claim.diet", "diet is not a normalized subject")
		}

		scope(path, claim.VariantRef)
	}

	for index, declaration := range document.AllergenDeclarations {
		path := fmt.Sprintf("allergen_declarations[%d]", index)
		c.text(path+".text", declaration.Text, 1_000_000)
		c.text(path+".substance", declaration.Substance, 1000)
		c.optionalText(path+".condition", declaration.Condition, 1_000_000)

		switch declaration.Presence {
		case AllergenContains, AllergenMayContain, AllergenFreeFrom:
		case "":
			c.add(path+".presence", "allergen.presence_required", "presence is required")
		default:
			c.add(path+".presence", "allergen.presence", "presence must be contains, may_contain, or free_from")
		}

		scope(path, declaration.VariantRef)
	}
}
