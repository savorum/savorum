package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// CreateTag validates and stores a reusable label under a new identity.
func (s *Service) CreateTag(ctx context.Context, tag Tag) (Tag, error) {
	repository := s.repository

	id, err := s.newTypedID("t")
	if err != nil {
		return Tag{}, fmt.Errorf("generate tag ID: %w", err)
	}

	tag.ID = id
	if err := validateTag(&tag); err != nil {
		return Tag{}, err
	}

	now := s.now().UTC()

	tag.Revision, tag.CreatedAt, tag.UpdatedAt = 1, now, now
	if err := repository.CreateTag(ctx, tag); err != nil {
		return Tag{}, fmt.Errorf("create tag: %w", err)
	}

	return tag, nil
}

// GetTag returns a reusable label by identity.
func (s *Service) GetTag(ctx context.Context, id string) (Tag, error) {
	repository := s.repository

	tag, err := repository.GetTag(ctx, strings.TrimSpace(id))
	if err != nil {
		return Tag{}, fmt.Errorf("get tag: %w", err)
	}

	return tag, nil
}

// ListTags returns the library's reusable labels.
func (s *Service) ListTags(ctx context.Context) ([]Tag, error) {
	repository := s.repository

	tags, err := repository.ListTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}

	return tags, nil
}

// ReplaceTag validates changes to a label against its expected revision.
func (s *Service) ReplaceTag(ctx context.Context, id string, expectedRevision uint64, tag Tag) (Tag, error) {
	if expectedRevision == 0 {
		return Tag{}, errors.New("expected tag revision is required")
	}

	repository := s.repository

	tag.ID, tag.UpdatedAt = strings.TrimSpace(id), s.now().UTC()
	if err := validateTag(&tag); err != nil {
		return Tag{}, err
	}

	result, err := repository.ReplaceTag(ctx, expectedRevision, tag)
	if err != nil {
		return Tag{}, fmt.Errorf("replace tag: %w", err)
	}

	return result, nil
}

// DeleteTag removes a label and its assignments after checking its expected revision.
func (s *Service) DeleteTag(ctx context.Context, id string, expectedRevision uint64) error {
	if expectedRevision == 0 {
		return errors.New("expected tag revision is required")
	}

	repository := s.repository
	if err := repository.DeleteTag(ctx, strings.TrimSpace(id), expectedRevision, s.now().UTC()); err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}

	return nil
}

// MergeTag moves a label's assignments to another label and removes the source.
func (s *Service) MergeTag(ctx context.Context, id string, expectedRevision uint64, targetID string) error {
	if expectedRevision == 0 {
		return errors.New("expected tag revision is required")
	}

	id, targetID = strings.TrimSpace(id), strings.TrimSpace(targetID)
	if id == "" || targetID == "" || id == targetID {
		return invalidOrganization("tag merge requires a different target tag")
	}

	repository := s.repository
	if err := repository.MergeTag(ctx, id, expectedRevision, targetID, s.now().UTC()); err != nil {
		return fmt.Errorf("merge tag: %w", err)
	}

	return nil
}

// CreateFacet validates and stores a classification value under a new identity.
func (s *Service) CreateFacet(ctx context.Context, facet Facet) (Facet, error) {
	repository := s.repository

	id, err := s.newTypedID("f")
	if err != nil {
		return Facet{}, fmt.Errorf("generate facet ID: %w", err)
	}

	facet.ID = id
	if err := validateFacet(&facet); err != nil {
		return Facet{}, err
	}

	now := s.now().UTC()

	facet.Revision, facet.CreatedAt, facet.UpdatedAt = 1, now, now
	if err := repository.CreateFacet(ctx, facet); err != nil {
		return Facet{}, fmt.Errorf("create facet: %w", err)
	}

	return facet, nil
}

// GetFacet returns a classification value by identity.
func (s *Service) GetFacet(ctx context.Context, id string) (Facet, error) {
	repository := s.repository

	facet, err := repository.GetFacet(ctx, strings.TrimSpace(id))
	if err != nil {
		return Facet{}, fmt.Errorf("get facet: %w", err)
	}

	return facet, nil
}

// ListFacets returns classification values, optionally restricted to one category.
func (s *Service) ListFacets(ctx context.Context, kind FacetKind) ([]Facet, error) {
	if kind != "" && !validFacetKind(kind) {
		return nil, invalidOrganization("facet kind must be course, cuisine, method, or dietary")
	}

	repository := s.repository

	facets, err := repository.ListFacets(ctx, kind)
	if err != nil {
		return nil, fmt.Errorf("list facets: %w", err)
	}

	return facets, nil
}

// ReplaceFacet validates changes to a classification value against its expected revision.
func (s *Service) ReplaceFacet(ctx context.Context, id string, expectedRevision uint64, facet Facet) (Facet, error) {
	if expectedRevision == 0 {
		return Facet{}, errors.New("expected facet revision is required")
	}

	repository := s.repository

	facet.ID, facet.UpdatedAt = strings.TrimSpace(id), s.now().UTC()
	if err := validateFacet(&facet); err != nil {
		return Facet{}, err
	}

	result, err := repository.ReplaceFacet(ctx, expectedRevision, facet)
	if err != nil {
		return Facet{}, fmt.Errorf("replace facet: %w", err)
	}

	return result, nil
}

// DeleteFacet removes a classification value and its assignments at the expected revision.
func (s *Service) DeleteFacet(ctx context.Context, id string, expectedRevision uint64) error {
	if expectedRevision == 0 {
		return errors.New("expected facet revision is required")
	}

	repository := s.repository
	if err := repository.DeleteFacet(ctx, strings.TrimSpace(id), expectedRevision, s.now().UTC()); err != nil {
		return fmt.Errorf("delete facet: %w", err)
	}

	return nil
}

// MergeFacet moves assignments to another classification value and removes the source.
func (s *Service) MergeFacet(ctx context.Context, id string, expectedRevision uint64, targetID string) error {
	if expectedRevision == 0 {
		return errors.New("expected facet revision is required")
	}

	id, targetID = strings.TrimSpace(id), strings.TrimSpace(targetID)
	if id == "" || targetID == "" || id == targetID {
		return invalidOrganization("facet merge requires a different target facet")
	}

	repository := s.repository
	if err := repository.MergeFacet(ctx, id, expectedRevision, targetID, s.now().UTC()); err != nil {
		return fmt.Errorf("merge facet: %w", err)
	}

	return nil
}

func validateTag(tag *Tag) error {
	tag.ID, tag.Name, tag.Color = strings.TrimSpace(tag.ID), strings.TrimSpace(tag.Name), strings.TrimSpace(tag.Color)
	if tag.ID == "" || tag.Name == "" || len([]rune(tag.Name)) > 100 {
		return invalidOrganization("tag ID and name are required")
	}

	if tag.Color != "" && !validColor(tag.Color) {
		return invalidOrganization("tag colors must be six-digit hexadecimal colors")
	}

	return nil
}

func validateFacet(facet *Facet) error {
	facet.ID, facet.Value = strings.TrimSpace(facet.ID), strings.TrimSpace(facet.Value)
	if facet.ID == "" || facet.Value == "" || len([]rune(facet.Value)) > 100 {
		return invalidOrganization("facet ID and value are required")
	}

	if !validFacetKind(facet.Kind) {
		return invalidOrganization("facet kind must be course, cuisine, method, or dietary")
	}

	return nil
}

func validFacetKind(kind FacetKind) bool {
	return kind == FacetCourse || kind == FacetCuisine || kind == FacetMethod || kind == FacetDietary
}
