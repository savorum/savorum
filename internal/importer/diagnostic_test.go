package importer_test

import (
	"testing"

	"github.com/savorum/savorum/internal/importer"
)

func TestDiagnosticAggregationKeepsIdentityAndOccurrences(t *testing.T) {
	values := importer.AggregateDiagnostics([]importer.Diagnostic{
		{Code: "ingredient-review-required", Class: importer.DiagnosticActionable, Area: "ingredients", Message: "Review ambiguous amounts.", Targets: []importer.DiagnosticTarget{{ObjectID: "i1"}}},
		{Code: "ingredient-review-required", Class: importer.DiagnosticActionable, Area: "ingredients", Message: "Review ambiguous amounts.", Count: 2, Targets: []importer.DiagnosticTarget{{ObjectID: "i2"}, {ObjectID: "i3"}}},
		{Code: "duration-not-iso", Severity: importer.SeverityInfo, Message: "Duration wording was kept."},
	})
	if len(values) != 2 || values[0].Count != 3 || len(values[0].Targets) != 3 ||
		values[1].Class != importer.DiagnosticInformational {
		t.Fatalf("aggregated diagnostics = %#v", values)
	}
}

// Occurrences with one sentence but different areas collapse into one entry
// listing the fields.
func TestDiagnosticAggregationCollapsesOneSentenceAcrossManyFields(t *testing.T) {
	t.Parallel()

	const sentence = "Source field is not represented by Savorum yet; preserved in fidelity diagnostics."

	values := importer.AggregateDiagnostics([]importer.Diagnostic{
		{Code: "unsupported_field", Class: importer.DiagnosticInformational, Area: "source.created", Message: sentence, Targets: []importer.DiagnosticTarget{{Field: "source.created"}}},
		{Code: "unsupported_field", Class: importer.DiagnosticInformational, Area: "source.difficulty", Message: sentence, Targets: []importer.DiagnosticTarget{{Field: "source.difficulty"}}},
	})

	if len(values) != 1 || values[0].Count != 2 || len(values[0].Targets) != 2 {
		t.Fatalf("aggregated diagnostics = %#v", values)
	}

	// No one field can stand for the sentence once it covers several.
	if values[0].Area != "" {
		t.Fatalf("merged area = %q", values[0].Area)
	}
}

// Two occurrences that read differently are two things to know, even under one
// code and area: a validation report names each constraint the recipe broke.
func TestDiagnosticAggregationKeepsDistinctSentencesApart(t *testing.T) {
	t.Parallel()

	values := importer.AggregateDiagnostics([]importer.Diagnostic{
		{Code: "candidate-invalid", Class: importer.DiagnosticBlocking, Area: "content", Message: "A timing reference is repeated."},
		{Code: "candidate-invalid", Class: importer.DiagnosticBlocking, Area: "content", Message: "An asset path is repeated."},
	})

	if len(values) != 2 {
		t.Fatalf("aggregated diagnostics = %#v", values)
	}
}
