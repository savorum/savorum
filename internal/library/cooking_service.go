// Package library is Savorum's application core: the recipes an instance holds,
// their revisions, how they are organized, and what happened when they were
// imported or cooked. It speaks in domain terms only — it has no knowledge of
// HTTP or of the database its services are given.
package library

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/recipe"
)

// Bounds for cooking notes and paged cooking history.
const (
	// MaximumCookingNote bounds a note tied to one occasion. It is a reminder
	// for next time, not a second recipe.
	MaximumCookingNote = 10000

	DefaultCookingListLimit = 50
	MaximumCookingListLimit = 200
)

// ListCookings returns a recipe's remembered cookings, most recent first, with
// the count and most recent date derived from them.
func (s *Service) ListCookings(ctx context.Context, recipeID string, options CookingListOptions) (CookingHistory, error) {
	if options.Limit <= 0 {
		options.Limit = DefaultCookingListLimit
	}

	if options.Limit > MaximumCookingListLimit {
		options.Limit = MaximumCookingListLimit
	}

	if options.Offset < 0 {
		options.Offset = 0
	}

	history, err := s.repository.ListCookings(ctx, strings.TrimSpace(recipeID), principalUserID(ctx), options)
	if err != nil {
		return CookingHistory{}, fmt.Errorf("list cookings: %w", err)
	}

	return history, nil
}

// RecordCooking remembers that a recipe was cooked. An absent date means today,
// because the ordinary use is marking a recipe made after making it.
func (s *Service) RecordCooking(ctx context.Context, recipeID string, record CookingRecord) (CookingRecord, error) {
	prepared, err := s.prepareCooking(record)
	if err != nil {
		return CookingRecord{}, err
	}

	id, err := s.newTypedID("c")
	if err != nil {
		return CookingRecord{}, fmt.Errorf("generate cooking ID: %w", err)
	}

	prepared.ID = id
	prepared.CreatedAt = s.now().UTC()

	created, err := s.repository.RecordCooking(ctx, strings.TrimSpace(recipeID), principalUserID(ctx), prepared)
	if err != nil {
		return CookingRecord{}, fmt.Errorf("record cooking: %w", err)
	}

	return created, nil
}

// ReplaceCooking corrects one remembered cooking. Correcting a date or a note
// is ordinary; the count and the most recent date follow from the result.
func (s *Service) ReplaceCooking(ctx context.Context, recipeID, cookingID string, record CookingRecord) (CookingRecord, error) {
	prepared, err := s.prepareCooking(record)
	if err != nil {
		return CookingRecord{}, err
	}

	prepared.ID = strings.TrimSpace(cookingID)
	if prepared.ID == "" {
		return CookingRecord{}, invalidOrganization("a cooking identifier is required")
	}

	updated, err := s.repository.ReplaceCooking(ctx, strings.TrimSpace(recipeID), principalUserID(ctx), prepared)
	if err != nil {
		return CookingRecord{}, fmt.Errorf("replace cooking: %w", err)
	}

	return updated, nil
}

// DeleteCooking forgets one remembered cooking.
func (s *Service) DeleteCooking(ctx context.Context, recipeID, cookingID string) error {
	if err := s.repository.DeleteCooking(ctx, strings.TrimSpace(recipeID), principalUserID(ctx), strings.TrimSpace(cookingID)); err != nil {
		return fmt.Errorf("delete cooking: %w", err)
	}

	return nil
}

func (s *Service) prepareCooking(record CookingRecord) (CookingRecord, error) {
	if record.CookedOn == "" {
		// A cooking is a calendar day, so "today" is the day where the cooking
		// happened. UTC is not that day for most of the world for part of every
		// day. A client that knows the cook's calendar sends the date itself;
		// this fallback uses the server's own day, which for a self-hosted
		// household is the kitchen's day.
		record.CookedOn = Date(s.now().Format(time.DateOnly))
	}

	cookedOn, err := ParseDate(record.CookedOn.String())
	if err != nil {
		return CookingRecord{}, invalidOrganization(err.Error())
	}

	record.CookedOn = cookedOn
	if record.CookedTime != "" {
		if _, err := time.Parse("15:04", record.CookedTime); err != nil || len(record.CookedTime) != 5 {
			return CookingRecord{}, invalidOrganization("cooking time must be HH:MM")
		}
	}

	if len([]rune(record.Note)) > MaximumCookingNote {
		return CookingRecord{}, invalidOrganization("a cooking note is too long")
	}

	if record.SessionID != nil {
		value := strings.TrimSpace(*record.SessionID)

		switch {
		case value == "":
			record.SessionID = nil
		case len([]rune(value)) > 128:
			return CookingRecord{}, invalidOrganization("a cooking session identifier is too long")
		default:
			record.SessionID = &value
		}
	}

	if err := validatePositiveRational(record.Scale, "batch size"); err != nil {
		return CookingRecord{}, err
	}

	if record.Scale != nil {
		canonical := recipe.Rational(record.Scale.String())
		record.Scale = &canonical
	}

	return record, nil
}
