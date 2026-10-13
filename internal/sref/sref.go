// Package sref reads and writes the Structured Recipe Exchange Format, an
// independent specification with its own versioning.
//
// Savorum implements the JSON reader, JSON writer, package reader, package
// writer, registry, and round-trip capabilities. An extension member found on
// any object inside a recipe comes back on that object, addressed by the
// recipe-local identifier of its nearest identified ancestor so that
// reordering cannot reattach it to the wrong thing.
package sref

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref/strictjson"
	"github.com/savorum/savorum/internal/units"
)

// Version and related constants identify the SREF format and unit registry implemented here.
const (
	// Version is the recipe specification version this build reads and writes.
	Version = "0.4.0"
	// UnitRegistryVersion is the registry against which unit IDs are read.
	// Savorum's own catalog holds exactly the identities this registry names.
	UnitRegistryVersion = "0.2.0"
	// StandardExtensionsMember is SREF's own namespaced extension container.
	StandardExtensionsMember = "extensions"
)

var (
	// ErrUnsupportedVersion reports unsupported SREF version.
	ErrUnsupportedVersion = errors.New("unsupported SREF version")
	// ErrInvalidDocument reports invalid SREF recipe document.
	ErrInvalidDocument = errors.New("invalid SREF recipe document")
	// ErrOpaqueDataDetached reports preserved future SREF data is detached.
	ErrOpaqueDataDetached = errors.New("preserved future SREF data is detached")
)

// extensionLabel is one label of a reversed-domain extension name: lowercase
// ASCII, interior hyphens only, at most 63 characters.
var extensionLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// minimumExtensionNameLabels is the fewest labels an extension name may carry:
// at least two identifying the reversed domain, then at least one chosen by
// the publisher (section 13). There is no maximum beyond the length limits,
// so `x-org.example.import.paprika` is valid.
const minimumExtensionNameLabels = 3

// maximumExtensionNameBytes bounds the whole name after `x-`.
const maximumExtensionNameBytes = 128

// UnsupportedVersionError reports a document this build must not reinterpret
// under a version it does not implement.
type UnsupportedVersionError struct {
	Version         string
	UnitRegistry    string
	SupportedFormat string
}

func (e *UnsupportedVersionError) Error() string {
	return fmt.Sprintf("recipe declares SREF %s with unit registry %s; this build implements SREF %s with registry %s",
		e.Version, e.UnitRegistry, e.SupportedFormat, UnitRegistryVersion)
}

func (*UnsupportedVersionError) Unwrap() error { return ErrUnsupportedVersion }

// envelope is the version identification every SREF recipe carries.
type envelope struct {
	Version      string `json:"version"`
	UnitRegistry string `json:"unit_registry"`
}

// Encode writes a recipe as an SREF recipe document. Savorum's own storage
// discriminator is not part of the format and is left behind.
func Encode(document recipe.Document) ([]byte, error) {
	if len(document.IngredientSections) == 0 && len(document.InstructionSections) == 0 {
		return nil, fmt.Errorf("%w: SREF requires ingredients or instructions; this local stub cannot be exported", ErrInvalidDocument)
	}

	if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}

	_, newerRegistry, err := versionsForWrite(document.SREFOpaque)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}

	if !newerRegistry {
		if err := validateKnownUnitIDs(document, units.DefaultCatalog()); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
		}
	}

	shaped, err := toWire(document)
	if err != nil {
		return nil, err
	}

	encoded, err := json.MarshalIndent(shaped, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode SREF recipe: %w", err)
	}

	return append(encoded, '\n'), nil
}

// Decode reads an SREF recipe document. Compatible newer minor versions are
// interpreted as the subset this build knows while their unknown standardized
// members are preserved opaquely. Incompatible format lines are refused.
func Decode(data []byte) (recipe.Document, error) {
	if err := strictjson.Validate(data); err != nil {
		return recipe.Document{}, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}

	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return recipe.Document{}, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}

	version, newerFormat, newerRegistry, err := readEnvelope(members)
	if err != nil {
		return recipe.Document{}, err
	}

	if err := validateMemberShapes(members); err != nil {
		return recipe.Document{}, err
	}

	if err := validateYieldMember(members, version.Version); err != nil {
		return recipe.Document{}, err
	}

	// Extensions inside the recipe are lifted out before the standard members
	// are read strictly, and are put back by address when the recipe is
	// written again.
	scoped, err := liftScopedExtensions(members)
	if err != nil {
		return recipe.Document{}, err
	}

	var opaque map[string]any
	if newerFormat {
		opaque, err = liftOpaqueStandardMembers(members)
		if err != nil {
			return recipe.Document{}, err
		}
	}

	if newerFormat || newerRegistry {
		opaque = preserveDeclaredVersions(opaque, version)
	}

	extensions := map[string]any{}
	standard := map[string]json.RawMessage{}

	for name, value := range members {
		switch {
		case name == "sref":
		case name == StandardExtensionsMember || isExtensionName(name):
			var decoded any
			if err := decodeWithNumbers(value, &decoded); err != nil {
				return recipe.Document{}, fmt.Errorf("%w: unreadable extension %q", ErrInvalidDocument, name)
			}

			extensions[name] = decoded
		default:
			standard[name] = value
		}
	}

	if len(scoped) > 0 {
		extensions[ScopedExtensionsMember] = scoped
	}

	body, err := json.Marshal(standard)
	if err != nil {
		return recipe.Document{}, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}

	document, err := decodeStandard(body)
	if err != nil {
		return recipe.Document{}, err
	}

	document.SchemaVersion = recipe.CurrentDocumentSchemaVersion
	if len(extensions) > 0 {
		document.Extensions = extensions
	}

	if len(opaque) > 0 {
		document.SREFOpaque = opaque
	}

	if err := recipe.ValidateForWrite(document, units.DefaultCatalog()); err != nil {
		return recipe.Document{}, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}

	if !newerRegistry {
		if err := validateKnownUnitIDs(document, units.DefaultCatalog()); err != nil {
			return recipe.Document{}, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
		}
	}

	return document, nil
}

func readEnvelope(members map[string]json.RawMessage) (envelope, bool, bool, error) {
	raw, present := members["sref"]
	if !present {
		return envelope{}, false, false, fmt.Errorf("%w: %w", ErrInvalidDocument, shapeFailure("missing-format-version", "no sref version member"))
	}

	var version envelope
	if err := json.Unmarshal(raw, &version); err != nil {
		return envelope{}, false, false, fmt.Errorf("%w: %w", ErrInvalidDocument, shapeFailure("missing-format-version", "unreadable sref version member"))
	}

	if strings.TrimSpace(version.Version) == "" {
		return envelope{}, false, false, fmt.Errorf("%w: %w", ErrInvalidDocument, shapeFailure("missing-format-version", "no sref version member"))
	}

	formatCompatible, newerFormat := readableVersion(version.Version, Version, EarlierReadableFormats)

	registryCompatible, newerRegistry := readableVersion(version.UnitRegistry, UnitRegistryVersion, EarlierReadableRegistries)
	if !formatCompatible || !registryCompatible {
		return envelope{}, false, false, &UnsupportedVersionError{
			Version: version.Version, UnitRegistry: version.UnitRegistry, SupportedFormat: Version,
		}
	}

	return version, newerFormat, newerRegistry, nil
}

func validateMemberShapes(members map[string]json.RawMessage) error {
	checks := []func(map[string]json.RawMessage) error{
		validateNonEmptyOptionalArrays,
		validateExtensionNames,
		validateShapes,
	}
	for _, check := range checks {
		if err := check(members); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidDocument, err)
		}
	}

	return nil
}

func preserveDeclaredVersions(opaque map[string]any, version envelope) map[string]any {
	if opaque == nil {
		opaque = map[string]any{}
	}

	carried, _ := opaque["document/sref"].(map[string]any)
	if carried == nil {
		carried = map[string]any{}
		opaque["document/sref"] = carried
	}

	carried["version"] = version.Version
	carried["unit_registry"] = version.UnitRegistry

	return opaque
}

func versionsForWrite(opaque map[string]any) (envelope, bool, error) {
	current := envelope{Version: Version, UnitRegistry: UnitRegistryVersion}

	value, preserved := opaque["document/sref"]
	if !preserved {
		return current, false, nil
	}

	carried, ok := value.(map[string]any)
	if !ok {
		return envelope{}, false, errors.New("preserved SREF version scope is not an object")
	}

	format, formatPresent := carried["version"]

	registry, registryPresent := carried["unit_registry"]
	if !formatPresent && !registryPresent {
		return envelope{}, false, errors.New("preserved SREF header lacks declared versions")
	}

	formatText, formatOK := format.(string)

	registryText, registryOK := registry.(string)
	if !formatOK || !registryOK {
		return envelope{}, false, errors.New("preserved SREF versions are not strings")
	}

	formatCompatible, _ := readableVersion(formatText, Version, EarlierReadableFormats)

	registryCompatible, newerRegistry := readableVersion(registryText, UnitRegistryVersion, EarlierReadableRegistries)
	if !formatCompatible || !registryCompatible {
		return envelope{}, false, errors.New("preserved SREF versions are not compatible with this build")
	}

	return envelope{Version: formatText, UnitRegistry: registryText}, newerRegistry, nil
}

// EarlierReadableFormats and EarlierReadableRegistries are the released
// versions this build reads besides its own.
//
// SREF section 5 requires refusing an older version without a schema, but
// within one compatibility line an earlier document is a subset of what this
// build defines, and released unit IDs are immutable. The list is explicit so
// admitting a release is a reviewed decision. Savorum's own exports before the
// 0.2.0 registry declare 0.1.0, which removed one alias and no unit ID.
var (
	EarlierReadableFormats    = []string{"0.1.0", "0.2.0"}
	EarlierReadableRegistries = []string{"0.1.0"}
)

// readableVersion reports whether this build reads a declared version: its own,
// a compatible newer one in the same line, or one of the earlier releases it
// explicitly admits.
//
// Version and UnitRegistryVersion happen to share a value today; keeping the
// supported version parameter avoids tying the format and registry release lines
// together.
func readableVersion(declared, supported string, earlier []string) (compatible, newer bool) {
	if compatible, newer := compatibleVersion(declared, supported); compatible {
		return compatible, newer
	}

	// An admitted earlier release is read, and is never "newer": nothing in it
	// is unknown data to be carried without understanding.
	return slices.Contains(earlier, strings.TrimSpace(declared)), false
}

func compatibleVersion(declared, supported string) (compatible, newer bool) {
	parse := func(value string) ([3]int, bool) {
		parts := strings.Split(value, ".")
		if len(parts) != 3 {
			return [3]int{}, false
		}

		var result [3]int

		for index, part := range parts {
			if part == "" || (len(part) > 1 && part[0] == '0') {
				return [3]int{}, false
			}

			parsed, err := strconv.Atoi(part)
			if err != nil || parsed < 0 {
				return [3]int{}, false
			}

			result[index] = parsed
		}

		return result, true
	}

	want, ok := parse(supported)
	if !ok {
		return false, false
	}

	got, ok := parse(declared)
	if !ok || got[0] != want[0] {
		return false, false
	}

	for index := 1; index < len(got); index++ {
		if got[index] == want[index] {
			continue
		}

		return got[index] > want[index], got[index] > want[index]
	}

	return true, false
}

// isExtensionName reports whether a member name is a conforming extension. It
// must carry its publisher's reversed DNS domain; `x-nutrition` names nobody.
func isExtensionName(name string) bool {
	body, found := strings.CutPrefix(name, "x-")
	if !found || len(body) < 1 || len(body) > maximumExtensionNameBytes {
		return false
	}

	labels := strings.Split(body, ".")
	if len(labels) < minimumExtensionNameLabels {
		return false
	}

	for _, label := range labels {
		if !extensionLabel.MatchString(label) {
			return false
		}
	}

	return true
}

func decodeWithNumbers(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode JSON while preserving numbers: %w", err)
	}

	return nil
}

// validateNonEmptyOptionalArrays closes a small gap between Go's slice model
// and SREF's JSON shape: omitempty deliberately writes both nil and empty
// slices as absence, while the specification rejects an explicitly present
// empty optional array. Check that distinction before decoding normalizes it.
func validateNonEmptyOptionalArrays(members map[string]json.RawMessage) error {
	encoded, err := json.Marshal(members)
	if err != nil {
		return fmt.Errorf("encode document to validate optional arrays: %w", err)
	}

	var root map[string]any
	if err := decodeWithNumbers(encoded, &root); err != nil {
		return err
	}

	for _, name := range []string{"authors", "keywords", "cuisines", "notes", "assets", "image_refs", "equipment", "translations"} {
		if err := checkNonEmptyArray(root, name, name); err != nil {
			return err
		}
	}

	if err := checkTimeArrays(root); err != nil {
		return err
	}

	if err := checkIngredientArrays(root); err != nil {
		return err
	}

	return checkInstructionArrays(root)
}

func checkNonEmptyArray(object map[string]any, name, path string) error {
	value, present := object[name]
	if !present {
		return nil
	}

	if items, array := value.([]any); array && len(items) == 0 {
		return fmt.Errorf("%s must contain at least one item when present", path)
	}

	return nil
}

func checkTimeArrays(root map[string]any) error {
	times, ok := root["times"].(map[string]any)
	if !ok {
		return nil
	}

	if err := checkNonEmptyArray(times, "phases", "times.phases"); err != nil {
		return err
	}

	if phases, ok := times["phases"].([]any); ok {
		for index, rawPhase := range phases {
			if phase, ok := rawPhase.(map[string]any); ok {
				path := fmt.Sprintf("times.phases[%d].step_refs", index)
				if err := checkNonEmptyArray(phase, "step_refs", path); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func checkIngredientArrays(root map[string]any) error {
	sections, _ := root["ingredient_sections"].([]any)
	for sectionIndex, rawSection := range sections {
		section, ok := rawSection.(map[string]any)
		if !ok {
			continue
		}

		ingredients, _ := section["ingredients"].([]any)
		for ingredientIndex, rawIngredient := range ingredients {
			ingredient, _ := rawIngredient.(map[string]any)

			normalization, ok := ingredient["normalization"].(map[string]any)
			if !ok {
				continue
			}

			path := fmt.Sprintf("ingredient_sections[%d].ingredients[%d].normalization.warnings", sectionIndex, ingredientIndex)
			if err := checkNonEmptyArray(normalization, "warnings", path); err != nil {
				return err
			}
		}
	}

	return nil
}

func checkInstructionArrays(root map[string]any) error {
	sections, _ := root["instruction_sections"].([]any)
	for sectionIndex, rawSection := range sections {
		section, _ := rawSection.(map[string]any)

		steps, _ := section["steps"].([]any)
		for stepIndex, rawStep := range steps {
			step, _ := rawStep.(map[string]any)

			for _, name := range []string{"ingredient_uses", "image_refs", "temperatures"} {
				path := fmt.Sprintf("instruction_sections[%d].steps[%d].%s", sectionIndex, stepIndex, name)
				if err := checkNonEmptyArray(step, name, path); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// validateExtensionNames refuses a malformed extension anywhere in a document.
//
// Section 13 reserves unknown names for standardized evolution, so a malformed
// `x-` name must be refused rather than preserved opaquely as a future
// standard member and written back out.
func validateExtensionNames(members map[string]json.RawMessage) error {
	encoded, err := json.Marshal(members)
	if err != nil {
		return fmt.Errorf("encode document to validate extension names: %w", err)
	}

	var root any
	if err := decodeWithNumbers(encoded, &root); err != nil {
		return err
	}

	return walkExtensionNames(root, "")
}

func walkExtensionNames(node any, path string) error {
	switch typed := node.(type) {
	case map[string]any:
		return walkExtensionObject(typed, path)
	case []any:
		for index, item := range typed {
			if err := walkExtensionNames(item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}

	return nil
}

func walkExtensionObject(object map[string]any, path string) error {
	for name, value := range object {
		where := name
		if path != "" {
			where = path + "." + name
		}

		if strings.HasPrefix(name, "x-") {
			if !isExtensionName(name) {
				return shapeFailure("invalid-extension-name", "extension name %q at %s is not x-<reversed-domain>.<local-name>", name, where)
			}
			// What is inside an extension value belongs to its publisher,
			// and the name rule does not reach into it.
			continue
		}

		if name == StandardExtensionsMember {
			// Section 13 makes the container an object of namespaced
			// members. This runs before the shape checks, so check it here.
			container, isObject := value.(map[string]any)
			if !isObject {
				return shapeFailure("invalid-extension-name", "%s must be an object of extension names", where)
			}

			for extension := range container {
				if !isExtensionName(extension) {
					return shapeFailure("invalid-extension-name", "extension name %q at %s is not x-<reversed-domain>.<local-name>", extension, where)
				}
			}

			continue
		}

		if err := walkExtensionNames(value, where); err != nil {
			return err
		}
	}

	return nil
}

// shapeError names the snapshot-scoped requirement a document broke, rather
// than the Go decoder's complaint about this implementation's structs.
type shapeError struct {
	code    string
	message string
}

func (e *shapeError) Error() string { return e.message }

// Code reports the snapshot-scoped requirement identifier of a refusal.
func (e *shapeError) Code() string { return e.code }

func shapeFailure(code, message string, arguments ...any) error {
	return &shapeError{code: code, message: fmt.Sprintf(message, arguments...)}
}

// validateShapes checks the structural rules whose violation would otherwise
// be reported as a decoder complaint.
func validateShapes(members map[string]json.RawMessage) error {
	encoded, err := json.Marshal(members)
	if err != nil {
		return fmt.Errorf("encode document to validate shapes: %w", err)
	}

	var root map[string]any
	if err := decodeWithNumbers(encoded, &root); err != nil {
		return err
	}

	if err := checkExtensionSubstitution(root); err != nil {
		return err
	}

	if _, ingredients := root["ingredient_sections"]; !ingredients {
		if _, instructions := root["instruction_sections"]; !instructions {
			return shapeFailure("recipe-content-required", "SREF requires ingredients or instructions")
		}
	}

	for _, member := range []string{"ingredient_sections", "instruction_sections"} {
		if value, present := root[member]; present {
			items, ok := value.([]any)
			if !ok || len(items) == 0 {
				return shapeFailure("recipe-collection-nonempty", "%s must be a nonempty array when present", member)
			}
		}
	}

	if value, present := root["yields"]; present {
		items, ok := value.([]any)
		if !ok || len(items) == 0 {
			return shapeFailure("empty-yields", "yields must be a nonempty array when present")
		}
	}

	if err := checkAuthors(root["authors"]); err != nil {
		return err
	}

	if err := checkIngredientFlags(root["ingredient_sections"]); err != nil {
		return err
	}

	if err := checkVariants(root["variants"]); err != nil {
		return err
	}

	if err := checkTimesShape(root["times"]); err != nil {
		return err
	}

	return checkQuantityShapes(root)
}

// checkExtensionSubstitution refuses a document that leaves a required member
// out and supplies an extension in its place. Section 13 says an unknown
// member must not satisfy a required standard field, so a document doing that
// is missing the field rather than carrying a private version of it.
func checkExtensionSubstitution(root map[string]any) error {
	for _, required := range []string{"title", "id"} {
		if _, present := root[required]; present {
			continue
		}

		for name := range root {
			if strings.HasPrefix(name, "x-") && strings.HasSuffix(name, "."+required) {
				return shapeFailure("extension-cannot-satisfy-core",
					"extension %q cannot supply the required %q", name, required)
			}
		}
	}

	return nil
}

// checkAuthors refuses the bare strings an unreleased draft carried. A name
// alone cannot say whether it credits a person or an organization, and
// section 6.4 forbids inferring one from the spelling.
func checkAuthors(value any) error {
	authors, ok := value.([]any)
	if !ok {
		return nil
	}

	for index, author := range authors {
		if _, structured := author.(map[string]any); !structured {
			return shapeFailure("legacy-string-author", "authors[%d] is not an author object", index)
		}
	}

	return nil
}

// checkVariants enforces that a variant describes a modification rather than
// becoming one. Section 6.7: variants do not nest, and content restructuring
// the recipe is a recipe whatever the prose calls it.
func checkVariants(value any) error {
	variants, ok := value.([]any)
	if !ok {
		return nil
	}

	for index, entry := range variants {
		variant, structured := entry.(map[string]any)
		if !structured {
			continue
		}

		if _, nested := variant["variants"]; nested {
			return shapeFailure("variant-nesting", "variants[%d] contains variants", index)
		}

		for _, member := range []string{"ingredient_sections", "instruction_sections", "yield", "times"} {
			if _, restructuring := variant[member]; restructuring {
				return shapeFailure("variant-restructures-recipe",
					"variants[%d] restructures the recipe through %q", index, member)
			}
		}
	}

	return nil
}

// checkTimesShape catches a summary expression that is not an expression. A
// duration is an object whose shape section 6.6 defines, and a bare string
// cannot say whether it is exact, a range, or opaque.
func checkTimesShape(value any) error {
	times, ok := value.(map[string]any)
	if !ok {
		return nil
	}

	for _, member := range []string{"prep", "cook", "additional", "total"} {
		held, present := times[member]
		if !present {
			continue
		}

		if _, structured := held.(map[string]any); !structured {
			return shapeFailure("timing-summary-expression-required",
				"times.%s is not a duration expression", member)
		}
	}

	return nil
}

// checkQuantityShapes keeps the quantity grammar shallow. An alternative
// option may not itself be an alternative and a sum term is always simple, so
// no document can build a tree a consumer would have to recurse through.
func checkQuantityShapes(node any) error {
	switch typed := node.(type) {
	case map[string]any:
		if kind, _ := typed["kind"].(string); kind == "alternatives" || kind == "sum" {
			if err := checkQuantityChildren(typed, kind); err != nil {
				return err
			}
		}

		for _, value := range typed {
			if err := checkQuantityShapes(value); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range typed {
			if err := checkQuantityShapes(item); err != nil {
				return err
			}
		}
	}

	return nil
}

func checkQuantityChildren(quantity map[string]any, kind string) error {
	member, code := "options", "nested-quantity-alternative"
	if kind == "sum" {
		member, code = "terms", "alternative-inside-sum"
	}

	children, ok := quantity[member].([]any)
	if !ok {
		return nil
	}

	for index, child := range children {
		shaped, structured := child.(map[string]any)
		if !structured {
			continue
		}

		childKind, _ := shaped["kind"].(string)
		if childKind == "alternatives" || childKind == "opaque" {
			return shapeFailure(code, "%s[%d] is a %s quantity", member, index, childKind)
		}

		if _, nested := shaped["options"]; nested {
			return shapeFailure(code, "%s[%d] carries alternatives", member, index)
		}
	}

	return nil
}

// checkIngredientFlags refuses a non-boolean `optional` (section 10).
func checkIngredientFlags(value any) error {
	if err := checkChoiceShapes(value); err != nil {
		return err
	}

	sections, ok := value.([]any)
	if !ok {
		return nil
	}

	for sectionIndex, entry := range sections {
		section, structured := entry.(map[string]any)
		if !structured {
			continue
		}

		ingredients, listed := section["ingredients"].([]any)
		if !listed {
			continue
		}

		for index, held := range ingredients {
			ingredient, shaped := held.(map[string]any)
			if !shaped {
				continue
			}

			optional, present := ingredient["optional"]
			if !present {
				continue
			}

			if _, boolean := optional.(bool); !boolean {
				return shapeFailure("optional-must-be-boolean",
					"ingredient_sections[%d].ingredients[%d].optional is not a boolean",
					sectionIndex, index)
			}
		}
	}

	return nil
}

// checkChoiceShapes checks presence before Go decoding can collapse nulls or
// omitted fields. Branches are complete expressions, never recursive choices.
func checkChoiceShapes(value any) error {
	sections, _ := value.([]any)
	for _, sectionValue := range sections {
		section, _ := sectionValue.(map[string]any)

		ingredients, _ := section["ingredients"].([]any)
		for _, value := range ingredients {
			item, _ := value.(map[string]any)
			if err := checkChoiceItem(item); err != nil {
				return err
			}
		}
	}

	return nil
}

func checkChoiceItem(item map[string]any) error {
	raw, choice := item["alternatives"]
	if !choice {
		if _, exists := item["relation"]; exists {
			return shapeFailure("schema-violation", "a relationship requires alternatives")
		}

		return nil
	}

	branches, ok := raw.([]any)
	if !ok || len(branches) < 2 || len(branches) > 100 {
		return shapeFailure("schema-violation", "a choice requires 2–100 complete branches")
	}

	relation, _ := item["relation"].(string)
	if relation != "or" && relation != "and_or" && relation != "preferred" {
		return shapeFailure("schema-violation", "a choice requires an authored relationship")
	}

	for _, name := range []string{"name", "quantity", "package_size", "temperature"} {
		if _, exists := item[name]; exists {
			return shapeFailure("schema-violation", "%s belongs to a branch", name)
		}
	}

	for _, value := range branches {
		if err := checkChoiceBranch(value); err != nil {
			return err
		}
	}

	return nil
}

func checkChoiceBranch(value any) error {
	branch, ok := value.(map[string]any)
	if !ok {
		return shapeFailure("schema-violation", "a branch must be an ingredient object")
	}

	for _, name := range []string{"alternatives", "relation"} {
		if _, exists := branch[name]; exists {
			return shapeFailure("schema-violation", "choices cannot be nested")
		}
	}

	name, _ := branch["name"].(string)
	if strings.TrimSpace(name) == "" {
		return shapeFailure("schema-violation", "a branch requires an authored name")
	}

	if optional, exists := branch["optional"]; exists {
		if _, ok := optional.(bool); !ok {
			return shapeFailure("optional-must-be-boolean", "branch optionality must be boolean")
		}
	}

	return nil
}

func validateYieldMember(members map[string]json.RawMessage, declared string) error {
	_, singular := members["yield"]
	_, plural := members["yields"]
	earlier := slices.Contains(EarlierReadableFormats, strings.TrimSpace(declared))

	switch {
	case singular && !earlier:
		return shapeFailure("singular-yield-member", "SREF %s records yields as an array; yield is not defined", Version)
	case plural && earlier:
		return shapeFailure("member-not-defined-by-version", "yields was added in SREF 0.4.0")
	}

	return nil
}
