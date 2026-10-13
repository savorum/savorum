package sref

import (
	"errors"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref/bundle"
)

// FailureCategory is the small normative vocabulary SREF processors expose to
// portable callers. Requirement IDs and messages are more precise diagnostic
// detail; only these categories are stable across implementations.
type FailureCategory string

// InvalidArtifact and related constants enumerate supported FailureCategory values.
const (
	InvalidArtifact            FailureCategory = "invalid-artifact"
	UnsupportedFormatVersion   FailureCategory = "unsupported-format-version"
	UnsupportedRegistryVersion FailureCategory = "unsupported-registry-version"
	UnknownUnit                FailureCategory = "unknown-unit"
	IntegrityFailure           FailureCategory = "integrity-failure"
	UnsafeArchive              FailureCategory = "unsafe-archive"
	ResourceLimit              FailureCategory = "resource-limit"
)

// FailureCategoryOf returns the portable category for a SREF refusal. It is a
// projection, not a second validator: nothing here decides whether an artifact
// is valid. Unknown detail falls back to invalid-artifact as section 22.1
// requires.
func FailureCategoryOf(err error) FailureCategory {
	if err == nil {
		return ""
	}

	var reported interface{ Category() FailureCategory }
	if errors.As(err, &reported) && isFailureCategory(reported.Category()) {
		return reported.Category()
	}

	return categoryForRequirement(RequirementID(err))
}

// RequirementID names the precise conformance rule Savorum enforced. These
// identifiers are stable within the SREF snapshot that defines them, but are
// optional diagnostic detail rather than the portable error vocabulary.
func RequirementID(err error) string {
	if err == nil {
		return ""
	}

	var reported interface{ RequirementID() string }
	if errors.As(err, &reported) && reported.RequirementID() != "" {
		return reported.RequirementID()
	}

	switch {
	case errors.Is(err, ErrUnsupportedVersion):
		return unsupportedVersionCode(err)
	case errors.Is(err, ErrOpaqueDataDetached):
		return "sref-opaque-detached"
	}

	if shape, ok := errors.AsType[*shapeError](err); ok {
		return shape.Code()
	}

	if archive, ok := errors.AsType[*ArchiveError](err); ok {
		return archive.Code()
	}

	if bundled, ok := errors.AsType[*bundle.ArchiveError](err); ok {
		return bundled.Code()
	}

	var validation *recipe.ValidationError
	if errors.As(err, &validation) && len(validation.Violations) > 0 {
		return violationCode(validation.Violations[0])
	}

	return "invalid-recipe"
}

func isFailureCategory(category FailureCategory) bool {
	switch category {
	case InvalidArtifact, UnsupportedFormatVersion, UnsupportedRegistryVersion,
		UnknownUnit, IntegrityFailure, UnsafeArchive, ResourceLimit:
		return true
	default:
		return false
	}
}

// ConformanceCode is kept for callers compiled against the former name. It is
// a requirement identifier, not a normative failure category.
func ConformanceCode(err error) string { return RequirementID(err) }

func categoryForRequirement(requirement string) FailureCategory {
	switch requirement {
	case "unsupported-format-version", "unsupported-package-version", "unsupported-bundle-version":
		return UnsupportedFormatVersion
	case "unsupported-registry-version":
		return UnsupportedRegistryVersion
	case "unknown-unit":
		return UnknownUnit
	case "asset-digest-mismatch", "asset-size-mismatch", "recipe-digest-mismatch",
		"recipe-manifest-disagreement", "missing-asset-entry", "undeclared-archive-entry",
		"bundle-member-digest-mismatch", "bundle-member-size-mismatch",
		"bundle-recipe-id-disagreement", "missing-bundle-member", "undeclared-bundle-entry":
		return IntegrityFailure
	case "absolute-archive-path", "backslash-archive-path", "control-character-archive-path",
		"drive-letter-archive-path", "empty-archive-path-segment", "unsafe-archive-path",
		"unsafe-bundle-member-path", "bundle-entry-outside-recipes", "duplicate-archive-entry",
		"nonregular-archive-entry":
		return UnsafeArchive
	case "archive-bytes-limit", "archive-directory-limit", "entry-count-limit",
		"entry-bytes-limit", "expanded-bytes-limit", "json-bytes-limit", "path-length-limit",
		"compression-ratio-limit", "work-bytes-limit", "member-count-limit":
		return ResourceLimit
	default:
		return InvalidArtifact
	}
}

func unsupportedVersionCode(err error) string {
	if unsupported, ok := errors.AsType[*UnsupportedVersionError](err); ok {
		// A document can be refused for either version, and the two are
		// separate requirements; the format line is reported first because a
		// document from another format line says nothing usable about its
		// registry.
		if compatible, _ := compatibleVersion(unsupported.Version, Version); !compatible {
			return "unsupported-format-version"
		}

		return "unsupported-registry-version"
	}

	return "unsupported-format-version"
}

// violationCategories maps an internal violation code onto the snapshot's
// requirement identifier where the correspondence is one to one.
var violationCategories = map[string]string{
	"asset.license":                        "asset-license-identity-required",
	"dietary_claim.suitability_required":   "dietary-claim-suitability-required",
	"dietary_claim.diet":                   "dietary-claim-unknown-diet",
	"allergen.presence_required":           "allergen-presence-required",
	"allergen.presence":                    "allergen-unknown-presence",
	"nutrition.basis_quantity_required":    "nutrition-quantity-basis-without-quantity",
	"nutrition.basis_quantity_unspecified": "nutrition-unspecified-basis-with-quantity",
	"nutrition.basis":                      "nutrition-unknown-basis",
	"nutrition.nutrients":                  "nutrition-empty-nutrients",
	"nutrient.energy_mass_unit":            "nutrition-energy-mass-unit",
	"nutrient.mass_energy_unit":            "nutrition-mass-energy-unit",
	"nutrient.value":                       "nutrition-opaque-value-with-amount",
	"nutrient.label":                       "nutrition-other-without-label",
	"nutrient.duplicate":                   "nutrition-duplicate-nutrient",
	"variant.id_duplicate":                 "duplicate-variant-id",
	"recipe_reference.identity":            "dependency-identity-required",
	"recipe_reference.self":                "dependency-self-reference",
	"amount.range_order":                   "range-order",
	"amount.rational":                      "noncanonical-rational-syntax",
	"amount.rational_denominator":          "noncanonical-denominator",
	"amount.rational_negative_zero":        "negative-zero-temperature",
	"amount.rational_range":                "rational-out-of-range",
	"amount.rational_unreduced":            "unreduced-rational",
	"asset.path":                           "unsafe-asset-path",
	"asset.path_duplicate":                 "duplicate-asset-path",
	"author.kind":                          "unknown-author-kind",
	"author.kind_label":                    "author-kind-label-requires-other",
	"author.url":                           "absolute-author-url-required",
	"extension.name":                       "invalid-extension-name",
	"language.invalid":                     "invalid-language-tag",
	"list.duplicate":                       "duplicate-authored-category",
	"microwave.choice_duplicate":           "microwave-choice-duplicate-conditions",
	"microwave.choices_opaque_duration":    "microwave-choice-duration-must-be-opaque",
	"microwave.choices_shared_power":       "microwave-choice-shared-power-conflict",
	"microwave.percent":                    "microwave-percent-out-of-range",
	"microwave.power_shape":                "microwave-power-conflict",
	"normalization.source":                 "normalization-source-required",
	"normalization.warning":                "normalization-warning-required",
	"quantity.options":                     "nested-quantity-alternative",
	"quantity.terms":                       "alternative-inside-sum",
	"source.application":                   "source-application-required",
	"source.identity":                      "source-identity-required",
	"step.content_required":                "step-text-required",
	"text.control_character":               "prohibited-control-character",
	"timing_assertion.attention":           "timing-invalid-attention",
	"timing_assertion.id_duplicate":        "duplicate-timing-id",
	"timing_assertion.kind":                "timing-unknown-kind",
	"timing_assertion.label_required":      "timing-other-without-label",
	"times.empty":                          "timing-summary-expression-required",
	"unit.id":                              "unknown-unit",
	"unit.physical_required":               "nonphysical-package-size",
	"unit.temperature_required":            "invalid-temperature-unit",
	"temperature.purpose.label":            "temperature-purpose-other-label-required",
	"temperature.purpose.kind":             "temperature-purpose-unknown-kind",
	"temperature.purpose.kind_required":    "temperature-purpose-kind-required",
	"temperature.opaque_exclusive":         "opaque-temperature-conflict",
	"translation.counterpart":              "translation-counterpart-required",
	"translation.document_language":        "translation-requires-document-language",
	"translation.language":                 "translation-invalid-language",
	"translation.original":                 "translation-single-original",
	"translation.relation":                 "translation-unknown-relation",
	"translation.self":                     "translation-self-reference",
}

// violationCode resolves one violation, using its path where the same rule
// produces different categories depending on what it was applied to. A
// dangling reference from a step to an image and one from a step to a timing
// assertion are the same defect in Savorum's model and different requirements
// in SREF's, so the path is what tells them apart.
func violationCode(violation recipe.Violation) string {
	if category, direct := violationCategories[violation.Code]; direct {
		return category
	}

	path := violation.Path
	switch violation.Code {
	case "id.duplicate":
		return duplicateIdentityCode(path)
	case "id.invalid":
		switch {
		case strings.HasSuffix(path, ".ingredient_ref"):
			// An ingredient use with no reference is a use of nothing, which
			// SREF states as its own requirement rather than as a malformed
			// identifier.
			return "ingredient-use-reference-required"
		case strings.HasPrefix(path, "times.assertions["):
			return "timing-id-required"
		}

		return "invalid-portable-id"
	case "reference.dangling":
		if strings.HasSuffix(path, ".variant_ref") {
			return "dangling-claim-variant-reference"
		}

		return referenceCode(path, "dangling-ingredient-reference", "dangling-step-image-reference",
			"dangling-recipe-image-reference", "dangling-step-timing-reference")
	case "reference.duplicate":
		return referenceCode(path, "duplicate-step-ingredient-reference", "duplicate-step-image-reference",
			"duplicate-recipe-image-reference", "duplicate-step-timing-reference")
	case "asset.media_type":
		switch {
		case strings.HasPrefix(path, "primary_image"):
			return "primary-image-media-type"
		case strings.Contains(path, ".steps["):
			return "step-image-media-type"
		default:
			return "recipe-image-media-type"
		}
	case "asset.reference":
		switch {
		case strings.HasPrefix(path, "primary_image"):
			return "primary-image-not-in-image-refs"
		case strings.Contains(path, ".steps[") && strings.Contains(path, ".image_refs"):
			return "dangling-step-image-reference"
		}

		return "dangling-recipe-image-reference"
	case "asset.role":
		return legacyRoleCode(violation.Value)
	case "chronology.invalid":
		return "invalid-publication-chronology"
	case "chronology.impossible":
		return "invalid-publication-date"
	case "choice.shape":
		return "schema-violation"
	case "choice.use_quantity":
		return "choice-use-quantity"
	case "url.absolute":
		if strings.HasPrefix(path, "assets[") && strings.HasSuffix(path, ".source_url") {
			return "asset-relative-source-url"
		}
	case "text.required":
		switch {
		case strings.HasPrefix(path, "allergen_declarations[") && strings.HasSuffix(path, ".substance"):
			return "allergen-substance-required"
		case strings.Contains(path, ".alternatives[") && strings.HasSuffix(path, ".name"):
			return "schema-violation"
		case strings.HasPrefix(path, "equipment[") && strings.HasSuffix(path, ".name"):
			return "equipment-name-required"
		case strings.Contains(path, "authors["):
			return "author-name-required"
		}

		return "text-required"
	case "unit.temperature_context":
		return temperatureContextCode(path)
	case "duration.opaque_approximate", "duration.opaque_exclusive", "duration.value_exclusive":
		return "timing-duration-conflict"
	case "duration.required", "duration.range_incomplete", "duration.invalid":
		return "timing-without-duration"
	case "quantity.kind", "quantity.shape":
		return "nested-quantity-alternative"
	}

	return "invalid-recipe"
}

func duplicateIdentityCode(path string) string {
	switch {
	case strings.Contains(path, ".ingredients["):
		return "duplicate-ingredient-id"
	case strings.Contains(path, ".steps["):
		return "duplicate-step-id"
	case strings.HasPrefix(path, "instruction_sections["):
		return "duplicate-instruction-section-id"
	case strings.HasPrefix(path, "ingredient_sections["):
		return "duplicate-section-id"
	case strings.HasPrefix(path, "assets["):
		return "duplicate-asset-id"
	}

	return "invalid-recipe"
}

func referenceCode(path, ingredient, stepImage, recipeImage, timing string) string {
	switch {
	case strings.Contains(path, "ingredient_ref") || strings.Contains(path, ".ingredient_uses"):
		return ingredient
	case strings.Contains(path, ".timing_refs"):
		return timing
	case strings.Contains(path, ".steps[") && strings.Contains(path, ".image_refs"):
		return stepImage
	case strings.HasPrefix(path, "image_refs"):
		return recipeImage
	}

	return recipeImage
}

// legacyRoleCode separates the two roles an unreleased draft carried. They
// asserted a relationship rather than classifying a file, which is now what
// `image_refs` is for, so a document still using one is refused rather than
// guessed at.
func legacyRoleCode(value string) string {
	switch value {
	case "primary-image":
		return "legacy-primary-image-role"
	case "step-image":
		return "legacy-step-image-role"
	}

	return "unknown-asset-role"
}

// temperatureContextCode names which slot a temperature unit turned up in.
// SREF treats each as its own requirement because each is a different mistake:
// a yield that is a temperature, an ingredient amount that is one, a package
// size that is one.
func temperatureContextCode(path string) string {
	switch {
	case strings.HasPrefix(path, "nutrition["):
		return "temperature-nutrition-basis"
	case strings.HasPrefix(path, "yield"):
		return "temperature-yield"
	case strings.HasPrefix(path, "equipment["):
		return "temperature-equipment"
	case strings.Contains(path, ".ingredient_uses"):
		return "temperature-ingredient-use"
	case strings.Contains(path, ".package_size"):
		return "temperature-package-size"
	}

	return "temperature-as-quantity"
}
