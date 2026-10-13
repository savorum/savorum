// Package recipe defines the recipe document: the model every importer,
// exporter, renderer, and store in Savorum agrees on. It is the innermost
// layer, and depends on nothing else in the project, so that a recipe means
// the same thing wherever it is read.
package recipe

import (
	"reflect"
	"strings"
)

// MissingIngredientMeasureWarning identifies an ingredient that has no usable amount.
const (
	MissingIngredientMeasureWarning = "Choose the measure for this amount, or keep the ingredient as authored text."
	// ImportReviewUnitPrefix opens the provenance of a measurement a person
	// interpreted during import review. It is portable normalization
	// provenance, not a current request for review: SREF requires inferred
	// values to retain a warning, so callers must not read the presence of one
	// as an actionability signal.
	//
	// Only the prefix is a marker; the sentence after it varies with what the
	// source supplied, as SREF section 9.5 requires.
	ImportReviewUnitPrefix = "This measurement was interpreted during import review."
	// ImportReviewUnitWarningLegacy is an older form still present in stored
	// rows. It is recognized but never written.
	ImportReviewUnitWarningLegacy = "The source did not identify a regional measurement convention. The unit was selected during import review."
)

// ImportReviewInterpreted reports whether a warning records an interpretation
// somebody chose during import review, in either form.
func ImportReviewInterpreted(warning string) bool {
	return warning == ImportReviewUnitWarningLegacy || strings.HasPrefix(warning, ImportReviewUnitPrefix)
}

// NormalizationNeedsReview distinguishes portable fidelity metadata from a
// current action. Confirmed source wording and a unit explicitly selected in
// import review retain their required warnings without staying in the review
// queue forever.
func NormalizationNeedsReview(value *Normalization) bool {
	if value == nil || len(value.Warnings) == 0 {
		return false
	}

	if value.Review == ReviewConfirmed {
		return false
	}

	if value.Method == NormalizationInferred && value.Review == ReviewCorrected &&
		len(value.Warnings) == 1 && ImportReviewInterpreted(value.Warnings[0]) {
		return false
	}

	return value.Method == NormalizationUnresolved || value.Method == NormalizationInferred
}

// ReconcileIngredientDiagnostics makes current warnings follow the current
// ingredient while leaving retained revisions and import history untouched.
// Stable ingredient IDs are the join key, so reordering alone changes
// nothing. An explicit normalization change from a client is respected; this
// function only reconciles the stale sidecar an ordinary editor echoes back.
func ReconcileIngredientDiagnostics(current, replacement Document) Document {
	previous := map[string]Ingredient{}

	for _, section := range current.IngredientSections {
		for _, ingredient := range section.Ingredients {
			previous[ingredient.ID] = ingredient
			for _, branch := range ingredient.Alternatives {
				previous[branch.ID] = branch
			}
		}
	}

	reconcile := func(next *Ingredient) {
		before, found := previous[next.ID]
		if !found || !uncertainNormalization(before.Normalization) {
			return
		}

		if !reflect.DeepEqual(next.Normalization, before.Normalization) {
			return
		}

		if sameDiagnosticInputs(before, *next) {
			return
		}

		if next.Quantity != nil && next.Quantity.Kind == QuantitySimple && next.Quantity.Unit == "" {
			next.Normalization = &Normalization{
				Method: NormalizationInferred, Review: ReviewCorrected,
				Warnings: []string{MissingIngredientMeasureWarning},
			}

			return
		}

		next.Normalization = &Normalization{Method: NormalizationAuthored, Review: ReviewCorrected}
	}

	for sectionIndex := range replacement.IngredientSections {
		for index := range replacement.IngredientSections[sectionIndex].Ingredients {
			item := &replacement.IngredientSections[sectionIndex].Ingredients[index]
			reconcile(item)

			for branchIndex := range item.Alternatives {
				reconcile(&item.Alternatives[branchIndex])
			}
		}
	}

	return replacement
}

func uncertainNormalization(value *Normalization) bool {
	return value != nil && len(value.Warnings) > 0 &&
		(value.Method == NormalizationUnresolved || value.Method == NormalizationInferred)
}

func sameDiagnosticInputs(left, right Ingredient) bool {
	return left.Name == right.Name && left.SourceText == right.SourceText && left.Relation == right.Relation &&
		reflect.DeepEqual(left.Alternatives, right.Alternatives) &&
		reflect.DeepEqual(left.Quantity, right.Quantity) &&
		reflect.DeepEqual(left.PackageSize, right.PackageSize) &&
		reflect.DeepEqual(left.Temperature, right.Temperature)
}
