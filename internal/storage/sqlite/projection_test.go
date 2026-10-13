package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestReopeningRebuildsProjectionsAfterAPresentationPolicyChange(t *testing.T) {
	t.Parallel()
	// A changed presentation policy rebuilds the stored projection.
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "library.db")

	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	document := testfixture.Document()
	for sectionIndex := range document.InstructionSections {
		for stepIndex := range document.InstructionSections[sectionIndex].Steps {
			document.InstructionSections[sectionIndex].Steps[stepIndex].TimingRefs = nil
		}
	}

	document.Times = &recipe.Times{
		Prep: isoExpression("PT15M"), Cook: isoExpression("PT25M"),
		Assertions: []recipe.TimingAssertion{{ID: "t1", Kind: recipe.TimingProof, Duration: recipe.DurationExpression{Value: "PT1H30M"}}},
	}

	written, err := service.Create(ctx, document)
	if err != nil {
		t.Fatal(err)
	}

	// Put the stale value back exactly as a database written under the
	// previous policy holds it, and mark the projection as built by it.
	if _, err := store.db.ExecContext(ctx,
		`UPDATE library_recipes SET total_time = 'PT40M' WHERE id = ?`, string(written.ID),
	); err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, "PRAGMA user_version = 0"); err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = reopened.Close() })

	var active, total string
	if err := reopened.db.QueryRowContext(ctx,
		`SELECT active_time, total_time FROM library_recipes WHERE id = ?`, string(written.ID),
	).Scan(&active, &total); err != nil {
		t.Fatal(err)
	}
	// The stored projection now agrees with the policy this build applies.
	want := recipe.SummarizeTimes(written.Recipe)
	if total != expressionValue(want.Elapsed) {
		t.Errorf("total time = %q after reopening, want %q", total, expressionValue(want.Elapsed))
	}

	if total != "" {
		t.Errorf("total time = %q, want the claim the recipe's own phases disprove withdrawn", total)
	}

	if active != expressionValue(want.Active) || active == "" {
		t.Errorf("active time = %q, want the working time %q the recipe does establish", active, expressionValue(want.Active))
	}

	// A rebuild is not an edit: the recipe gains no revision and its edit time
	// is left alone, because nothing about the recipe changed.
	var (
		revision  uint64
		updatedAt string
	)
	if err := reopened.db.QueryRowContext(ctx,
		`SELECT revision, updated_at FROM library_recipes WHERE id = ?`, string(written.ID),
	).Scan(&revision, &updatedAt); err != nil {
		t.Fatal(err)
	}

	if revision != written.Revision {
		t.Errorf("revision = %d after a rebuild, want %d", revision, written.Revision)
	}

	parsed, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		t.Fatal(err)
	}

	if !parsed.Equal(written.UpdatedAt) {
		t.Errorf("updated at = %s after a rebuild, want %s", parsed, written.UpdatedAt)
	}

	// The rebuild records itself, so it does not run again on every start.
	var version int
	if err := reopened.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}

	if version != library.ProjectionVersion {
		t.Errorf("projection version = %d, want %d", version, library.ProjectionVersion)
	}
}

func isoExpression(value string) *recipe.DurationExpression {
	return &recipe.DurationExpression{Value: value}
}

// expressionValue is the ISO value a projection would hold, which is empty for
// a range or opaque wording because a sortable column cannot carry either.
func expressionValue(expression *recipe.DurationExpression) string {
	if expression == nil || expression.IsOpaque() || expression.IsRange() {
		return ""
	}

	return expression.Value
}
