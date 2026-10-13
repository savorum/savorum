package library

import (
	"context"
	"fmt"
	"strings"
)

// ApplyImportedLibraryMetadata is the shared policy point for deciding which
// decoded fields become Savorum-local facts. It never writes extensions into
// the portable document and never creates synthetic cooking records.
func (s *Service) ApplyImportedLibraryMetadata(ctx context.Context, recipeID string, metadata ImportedLibraryMetadata) error {
	if metadata.Favorite != nil || metadata.Rating != nil || metadata.PersonalNote != nil {
		organization, err := s.GetOrganization(ctx, recipeID)
		if err != nil {
			return err
		}

		if metadata.Favorite != nil {
			organization.State.Favorite = *metadata.Favorite
		}

		if metadata.Rating != nil {
			organization.State.Rating = metadata.Rating
		}

		if metadata.PersonalNote != nil {
			organization.State.PersonalNote = *metadata.PersonalNote
		}

		if _, err := s.ReplaceOrganization(ctx, recipeID, organization.Revision, organization, nil); err != nil {
			return err
		}
	}

	if metadata.CookCount != nil {
		baseline := *metadata.CookCount

		baseline.Source, baseline.SourceKey = strings.TrimSpace(baseline.Source), strings.TrimSpace(baseline.SourceKey)
		if baseline.Source == "" {
			return invalidOrganization("imported cook-count baseline requires its source")
		}

		repository, ok := s.repository.(ImportMetadataRepository)
		if !ok {
			return fmt.Errorf("store does not support imported cook-count baselines")
		}

		if err := repository.SetImportedCookCountBaseline(ctx, strings.TrimSpace(recipeID), principalUserID(ctx), baseline); err != nil {
			return fmt.Errorf("store imported cook-count baseline: %w", err)
		}
	}

	return nil
}
