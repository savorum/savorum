package library

import (
	"context"
	"slices"
	"testing"

	"github.com/savorum/savorum/internal/units"
)

func TestChangedAreasNamesTheRecipePartsThatDiffer(t *testing.T) {
	t.Parallel()

	previous := refreshDocument()

	current := cloneRefresh(t, previous)
	current.Title = "Better soup"
	current.IngredientSections[0].Ingredients[1].Note = "cold"
	current.Notes = []string{"Rest it."}
	current.Cuisines = []string{"french"}

	areas := ChangedAreas(previous, current)
	if want := []string{AreaTitle, AreaIngredients, AreaNotes, AreaLabels}; !slices.Equal(areas, want) {
		t.Fatalf("changed areas = %v, want %v", areas, want)
	}
}

func TestChangedAreasIgnoresAReencodingThatSaysTheSameThing(t *testing.T) {
	t.Parallel()

	previous := refreshDocument()

	current := cloneRefresh(t, previous)
	if areas := ChangedAreas(previous, current); len(areas) != 0 {
		t.Fatalf("unchanged recipe reported %v", areas)
	}
}

func TestChangedAreasReportsARemovedField(t *testing.T) {
	t.Parallel()

	previous := refreshDocument()
	previous.Notes = []string{"Rest it."}

	current := cloneRefresh(t, previous)
	current.Notes = nil

	if areas := ChangedAreas(previous, current); !slices.Equal(areas, []string{AreaNotes}) {
		t.Fatalf("changed areas = %v, want [notes]", areas)
	}
}

func TestDocumentAreaNamesNestedPathsByTheirField(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]string{
		"ingredient_sections[main].ingredients[salt].name": AreaIngredients,
		"instruction_sections[method]":                     AreaInstructions,
		"times":                                            AreaTimes,
		"assets[photo].alt":                                AreaPhotos,
		"published":                                        AreaSource,
		"cooking_methods":                                  AreaLabels,
		"extensions_raw":                                   AreaOther,
	} {
		if got := DocumentArea(path); got != want {
			t.Errorf("DocumentArea(%q) = %q, want %q", path, got, want)
		}
	}
}

// revisionHistoryRepository holds an entry's retained revisions in order, so a
// history listing can be asked what each one changed.
type revisionHistoryRepository struct {
	*memoryRepository
	revisions []Revision
}

func (repository *revisionHistoryRepository) ListRevisions(_ context.Context, id EntryID, options RevisionListOptions) ([]RevisionSummary, error) {
	summaries := make([]RevisionSummary, 0, len(repository.revisions))
	for _, revision := range repository.revisions {
		summaries = append(summaries, RevisionSummary{EntryID: id, Number: revision.Number, State: revision.State, Cause: revision.Cause, CreatedAt: revision.CreatedAt})
	}

	return page(summaries, options), nil
}

func (repository *revisionHistoryRepository) ListRevisionDocuments(_ context.Context, _ EntryID, options RevisionListOptions) ([]Revision, error) {
	return page(repository.revisions, options), nil
}

func page[T any](values []T, options RevisionListOptions) []T {
	if options.Offset >= len(values) {
		return nil
	}

	end := min(options.Offset+options.Limit, len(values))

	return values[options.Offset:end]
}

func TestListRevisionsSaysWhatEachRevisionChanged(t *testing.T) {
	t.Parallel()

	first := refreshDocument()

	second := cloneRefresh(t, first)
	second.IngredientSections[0].Ingredients[0].Name = "sea salt"

	third := cloneRefresh(t, second)
	third.Notes = []string{"Rest it."}

	repository := &revisionHistoryRepository{
		memoryRepository: &memoryRepository{created: Entry{ID: "soup-entry", Recipe: third, Revision: 3}},
		// Newest first, as history is read.
		revisions: []Revision{
			{EntryID: "soup-entry", Number: 3, Recipe: third, State: StateActive, Cause: RevisionCauseManualEdit},
			{EntryID: "soup-entry", Number: 2, Recipe: second, State: StateActive, Cause: RevisionCauseManualEdit},
			{EntryID: "soup-entry", Number: 1, Recipe: first, State: StateActive, Cause: RevisionCauseAPI},
		},
	}

	service, err := NewService(repository, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	revisions, err := service.ListRevisions(context.Background(), "soup-entry", RevisionListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(revisions) != 3 {
		t.Fatalf("history returned %d revisions", len(revisions))
	}

	if !slices.Equal(revisions[0].Changes, []string{AreaNotes}) {
		t.Errorf("newest revision changed %v, want [notes]", revisions[0].Changes)
	}

	if !slices.Equal(revisions[1].Changes, []string{AreaIngredients}) {
		t.Errorf("second revision changed %v, want [ingredients]", revisions[1].Changes)
	}

	// The first revision has nil Changes, not an empty list.
	if revisions[2].Changes != nil {
		t.Errorf("oldest revision reported changes %v", revisions[2].Changes)
	}
}
