package library

import (
	"context"
	"fmt"

	"github.com/savorum/savorum/internal/recipe"
)

// areaOrder lists recipe areas in reading order.
var areaOrder = []string{
	AreaTitle,
	AreaDescription,
	AreaYield,
	AreaNutrition,
	AreaClaims,
	AreaTimes,
	AreaIngredients,
	AreaInstructions,
	AreaNotes,
	AreaVariants,
	AreaEquipment,
	AreaPhotos,
	AreaSource,
	AreaLabels,
	AreaLanguage,
	AreaTranslations,
	AreaOther,
}

// ChangedAreas names the recipe areas that differ between two retained
// revisions. Comparison is semantic: a re-encoding that says the same thing is
// not a change, and neither is a reordering the canonical form resolves.
func ChangedAreas(previous, current recipe.Document) []string {
	before, after := semanticFields(previous), semanticFields(current)
	if before == nil || after == nil {
		return nil
	}

	seen := map[string]bool{}

	for key, value := range after {
		if key == "id" || key == "schema_version" {
			continue
		}

		if !semanticAnyEqual(before[key], value) {
			seen[DocumentArea(key)] = true
		}
	}

	for key, value := range before {
		if key == "id" || key == "schema_version" {
			continue
		}

		if _, present := after[key]; !present && !isEmptyValue(value) {
			seen[DocumentArea(key)] = true
		}
	}

	areas := make([]string, 0, len(seen))

	for _, area := range areaOrder {
		if seen[area] {
			areas = append(areas, area)
		}
	}

	return areas
}

// describeRevisionChanges fills in what each listed revision changed, using one
// extra revision beyond the page as the comparison for its oldest member. The
// very first revision keeps nil Changes.
func (s *Service) describeRevisionChanges(ctx context.Context, id EntryID, options RevisionListOptions, revisions []RevisionSummary) error {
	repository, ok := s.repository.(RevisionDocumentRepository)
	if !ok || len(revisions) == 0 {
		return nil
	}

	documents, err := repository.ListRevisionDocuments(ctx, id, RevisionListOptions{Limit: len(revisions) + 1, Offset: options.Offset})
	if err != nil {
		return fmt.Errorf("read recipe revision documents: %w", err)
	}

	for index := range revisions {
		// The rows are newest first and read from the same page of the same
		// history, so the revision after this one is the one it was made from.
		if index+1 >= len(documents) || documents[index].Number != revisions[index].Number {
			continue
		}

		revisions[index].Changes = ChangedAreas(documents[index+1].Recipe, documents[index].Recipe)
	}

	return nil
}
