package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/savorum/savorum/internal/access"

	"github.com/savorum/savorum/internal/library"
)

// CreateTag stores a new tag.
func (s *Store) CreateTag(ctx context.Context, tag library.Tag) error {
	if err := s.authorizeScope(ctx, access.Editor); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.authorizeScopeWith(ctx, tx, access.Editor); err != nil {
		return err
	}

	if err := recordCreation(ctx, tx, tag.ID); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO tags (library_id,id, name, normalized_name, color, revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		scopeID(ctx), tag.ID, tag.Name, normalizeName(tag.Name), nullableString(tag.Color), tag.Revision,
		formatTime(tag.CreatedAt), formatTime(tag.UpdatedAt))
	if err != nil {
		if isConstraint(err) {
			return library.ErrConflict
		}

		return fmt.Errorf("insert tag: %w", err)
	}

	return operationError("commit creation", tx.Commit())
}

// GetTag returns the tag identified by id.
func (s *Store) GetTag(ctx context.Context, id string) (library.Tag, error) {
	if err := s.scopedObject(ctx, "tags", id, access.Viewer); err != nil {
		return library.Tag{}, err
	}

	tag, err := scanTag(s.db.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(color, ''), revision, created_at, updated_at
		FROM tags WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return library.Tag{}, library.ErrNotFound
	}

	if err != nil {
		return library.Tag{}, fmt.Errorf("read tag: %w", err)
	}

	return tag, nil
}

// ListTags returns tags in their display order.
func (s *Store) ListTags(ctx context.Context) ([]library.Tag, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(color, ''), revision, created_at, updated_at
		FROM tags WHERE library_id=? ORDER BY name COLLATE NOCASE, id`, scopeID(ctx))
	if err != nil {
		return nil, fmt.Errorf("query tags: %w", err)
	}
	defer rows.Close()

	result := []library.Tag{}

	for rows.Next() {
		tag, err := scanTag(rows)
		if err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}

		result = append(result, tag)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tags: %w", err)
	}

	counts, err := s.labelCounts(ctx, "recipe_tags", "tag_id")
	if err != nil {
		return nil, err
	}

	for index := range result {
		result[index].RecipeCount = counts[result[index].ID]
	}

	return result, nil
}

// ReplaceTag updates a tag when its revision matches expectedRevision.
func (s *Store) ReplaceTag(ctx context.Context, expectedRevision uint64, tag library.Tag) (library.Tag, error) {
	if err := s.scopedObject(ctx, "tags", tag.ID, access.Editor); err != nil {
		return library.Tag{}, err
	}

	var current uint64
	if err := s.db.QueryRowContext(ctx, "SELECT revision FROM tags WHERE id = ?", tag.ID).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return library.Tag{}, library.ErrNotFound
	} else if err != nil {
		return library.Tag{}, fmt.Errorf("read tag revision: %w", err)
	}

	if current != expectedRevision {
		return library.Tag{}, &library.PreconditionError{Expected: expectedRevision, Current: current}
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE tags
		SET name = ?, normalized_name = ?, color = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND revision = ?`,
		tag.Name, normalizeName(tag.Name), nullableString(tag.Color), formatTime(tag.UpdatedAt), tag.ID, expectedRevision)
	if err != nil {
		if isConstraint(err) {
			return library.Tag{}, library.ErrConflict
		}

		return library.Tag{}, fmt.Errorf("update tag: %w", err)
	}

	if affected, err := result.RowsAffected(); err != nil {
		return library.Tag{}, fmt.Errorf("read tag update result: %w", err)
	} else if affected != 1 {
		return library.Tag{}, &library.PreconditionError{Expected: expectedRevision, Current: current}
	}

	return s.GetTag(ctx, tag.ID)
}

// DeleteTag removes a tag, and takes it off every recipe that carries it, when
// its revision matches expectedRevision.
func (s *Store) DeleteTag(ctx context.Context, id string, expectedRevision uint64, timestamp time.Time) error {
	if err := s.scopedObject(ctx, "tags", id, access.Editor); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tag deletion: %w", err)
	}
	defer tx.Rollback()

	if err := requireTaxonomyRevision(ctx, tx, "tags", id, expectedRevision); err != nil {
		return err
	}

	if err := advanceAssignedOrganizationRevisions(ctx, tx, "recipe_tags", "tag_id", id, timestamp); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM recipe_tags WHERE tag_id = ?", id); err != nil {
		return fmt.Errorf("delete tag assignments: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM tags WHERE id = ? AND revision = ?", id, expectedRevision); err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tag deletion: %w", err)
	}

	return nil
}

// MergeTag replaces a tag with targetID when the source revision matches.
func (s *Store) MergeTag(ctx context.Context, id string, expectedRevision uint64, targetID string, timestamp time.Time) error {
	if err := s.scopedObject(ctx, "tags", targetID, access.Editor); err != nil {
		return err
	}

	if err := s.scopedObject(ctx, "tags", id, access.Editor); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tag merge: %w", err)
	}
	defer tx.Rollback()

	if err := requireTaxonomyRevision(ctx, tx, "tags", id, expectedRevision); err != nil {
		return err
	}

	if err := requireTaxonomyTarget(ctx, tx, "tags", targetID); err != nil {
		return err
	}

	if err := advanceAssignedOrganizationRevisions(ctx, tx, "recipe_tags", "tag_id", id, timestamp); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO recipe_tags (entry_id, tag_id)
		SELECT entry_id, ? FROM recipe_tags WHERE tag_id = ?`, targetID, id); err != nil {
		return fmt.Errorf("move tag assignments: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM recipe_tags WHERE tag_id = ?", id); err != nil {
		return fmt.Errorf("delete merged tag assignments: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM tags WHERE id = ? AND revision = ?", id, expectedRevision); err != nil {
		return fmt.Errorf("delete merged tag: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tag merge: %w", err)
	}

	return nil
}

// CreateFacet stores a new facet.
func (s *Store) CreateFacet(ctx context.Context, facet library.Facet) error {
	if err := s.authorizeScope(ctx, access.Editor); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.authorizeScopeWith(ctx, tx, access.Editor); err != nil {
		return err
	}

	if err := recordCreation(ctx, tx, facet.ID); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO facets (library_id,id, kind, name, normalized_name, revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		scopeID(ctx), facet.ID, facet.Kind, facet.Value, normalizeName(facet.Value), facet.Revision,
		formatTime(facet.CreatedAt), formatTime(facet.UpdatedAt))
	if err != nil {
		if isConstraint(err) {
			return library.ErrConflict
		}

		return fmt.Errorf("insert facet: %w", err)
	}

	return operationError("commit creation", tx.Commit())
}

// GetFacet returns the facet identified by id.
func (s *Store) GetFacet(ctx context.Context, id string) (library.Facet, error) {
	if err := s.scopedObject(ctx, "facets", id, access.Viewer); err != nil {
		return library.Facet{}, err
	}

	facet, err := scanFacet(s.db.QueryRowContext(ctx, `
		SELECT id, kind, name, revision, created_at, updated_at
		FROM facets WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return library.Facet{}, library.ErrNotFound
	}

	if err != nil {
		return library.Facet{}, fmt.Errorf("read facet: %w", err)
	}

	return facet, nil
}

// ListFacets returns facets of kind in their display order.
func (s *Store) ListFacets(ctx context.Context, kind library.FacetKind) ([]library.Facet, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, err
	}

	query := `SELECT id, kind, name, revision, created_at, updated_at FROM facets WHERE library_id=?`
	args := []any{scopeID(ctx)}

	if kind != "" {
		query += " AND kind = ?"

		args = append(args, kind)
	}

	query += " ORDER BY kind, name COLLATE NOCASE, id"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query facets: %w", err)
	}
	defer rows.Close()

	result := []library.Facet{}

	for rows.Next() {
		facet, err := scanFacet(rows)
		if err != nil {
			return nil, fmt.Errorf("scan facet: %w", err)
		}

		result = append(result, facet)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate facets: %w", err)
	}

	counts, err := s.labelCounts(ctx, "recipe_facets", "facet_id")
	if err != nil {
		return nil, err
	}

	for index := range result {
		result[index].RecipeCount = counts[result[index].ID]
	}

	return result, nil
}

// ReplaceFacet updates a facet when its revision matches expectedRevision.
func (s *Store) ReplaceFacet(ctx context.Context, expectedRevision uint64, facet library.Facet) (library.Facet, error) {
	if err := s.scopedObject(ctx, "facets", facet.ID, access.Editor); err != nil {
		return library.Facet{}, err
	}

	var current uint64
	if err := s.db.QueryRowContext(ctx, "SELECT revision FROM facets WHERE id = ?", facet.ID).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return library.Facet{}, library.ErrNotFound
	} else if err != nil {
		return library.Facet{}, fmt.Errorf("read facet revision: %w", err)
	}

	if current != expectedRevision {
		return library.Facet{}, &library.PreconditionError{Expected: expectedRevision, Current: current}
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE facets
		SET kind = ?, name = ?, normalized_name = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND revision = ?`,
		facet.Kind, facet.Value, normalizeName(facet.Value), formatTime(facet.UpdatedAt), facet.ID, expectedRevision)
	if err != nil {
		if isConstraint(err) {
			return library.Facet{}, library.ErrConflict
		}

		return library.Facet{}, fmt.Errorf("update facet: %w", err)
	}

	if affected, err := result.RowsAffected(); err != nil {
		return library.Facet{}, fmt.Errorf("read facet update result: %w", err)
	} else if affected != 1 {
		return library.Facet{}, &library.PreconditionError{Expected: expectedRevision, Current: current}
	}

	return s.GetFacet(ctx, facet.ID)
}

// DeleteFacet removes a facet, and takes it off every recipe that carries it,
// when its revision matches expectedRevision.
func (s *Store) DeleteFacet(ctx context.Context, id string, expectedRevision uint64, timestamp time.Time) error {
	if err := s.scopedObject(ctx, "facets", id, access.Editor); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin facet deletion: %w", err)
	}
	defer tx.Rollback()

	if err := requireTaxonomyRevision(ctx, tx, "facets", id, expectedRevision); err != nil {
		return err
	}

	if err := advanceAssignedOrganizationRevisions(ctx, tx, "recipe_facets", "facet_id", id, timestamp); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM recipe_facets WHERE facet_id = ?", id); err != nil {
		return fmt.Errorf("delete facet assignments: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM facets WHERE id = ? AND revision = ?", id, expectedRevision); err != nil {
		return fmt.Errorf("delete facet: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit facet deletion: %w", err)
	}

	return nil
}

// MergeFacet replaces a facet with targetID when the source revision matches.
func (s *Store) MergeFacet(ctx context.Context, id string, expectedRevision uint64, targetID string, timestamp time.Time) error {
	if err := s.scopedObject(ctx, "facets", targetID, access.Editor); err != nil {
		return err
	}

	if err := s.scopedObject(ctx, "facets", id, access.Editor); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin facet merge: %w", err)
	}
	defer tx.Rollback()

	var (
		sourceKind, targetKind library.FacetKind
		current                uint64
	)
	if err := tx.QueryRowContext(ctx, "SELECT kind, revision FROM facets WHERE id = ?", id).Scan(&sourceKind, &current); errors.Is(err, sql.ErrNoRows) {
		return library.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read source facet: %w", err)
	}

	if current != expectedRevision {
		return &library.PreconditionError{Expected: expectedRevision, Current: current}
	}

	if err := tx.QueryRowContext(ctx, "SELECT kind FROM facets WHERE id = ?", targetID).Scan(&targetKind); errors.Is(err, sql.ErrNoRows) {
		return library.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read target facet: %w", err)
	}

	if sourceKind != targetKind {
		return library.ErrConflict
	}

	if err := advanceAssignedOrganizationRevisions(ctx, tx, "recipe_facets", "facet_id", id, timestamp); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO recipe_facets (entry_id, facet_id)
		SELECT entry_id, ? FROM recipe_facets WHERE facet_id = ?`, targetID, id); err != nil {
		return fmt.Errorf("move facet assignments: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM recipe_facets WHERE facet_id = ?", id); err != nil {
		return fmt.Errorf("delete merged facet assignments: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM facets WHERE id = ? AND revision = ?", id, expectedRevision); err != nil {
		return fmt.Errorf("delete merged facet: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit facet merge: %w", err)
	}

	return nil
}

func requireTaxonomyRevision(ctx context.Context, tx *sql.Tx, table, id string, expected uint64) error {
	var current uint64

	query := "SELECT revision FROM " + table + " WHERE id = ?"
	if err := tx.QueryRowContext(ctx, query, id).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return library.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read taxonomy revision: %w", err)
	}

	if current != expected {
		return &library.PreconditionError{Expected: expected, Current: current}
	}

	return nil
}

func requireTaxonomyTarget(ctx context.Context, tx *sql.Tx, table, id string) error {
	var exists int

	query := "SELECT EXISTS(SELECT 1 FROM " + table + " WHERE id = ?)"
	if err := tx.QueryRowContext(ctx, query, id).Scan(&exists); err != nil {
		return fmt.Errorf("read taxonomy merge target: %w", err)
	}

	if exists == 0 {
		return library.ErrNotFound
	}

	return nil
}

func advanceAssignedOrganizationRevisions(ctx context.Context, tx *sql.Tx, assignmentTable, idColumn, id string, timestamp time.Time) error {
	//nolint:gosec // the concatenated text is placeholders and column constants; every value is a bound argument
	query := `UPDATE recipe_organization_state
		SET revision = revision + 1, updated_at = ?
		WHERE entry_id IN (SELECT entry_id FROM ` + assignmentTable + ` WHERE ` + idColumn + ` = ?)`
	if _, err := tx.ExecContext(ctx, query, formatTime(timestamp), id); err != nil {
		return fmt.Errorf("advance affected organization revisions: %w", err)
	}

	return nil
}

func scanTag(source scanner) (library.Tag, error) {
	var (
		tag                  library.Tag
		createdAt, updatedAt string
	)
	if err := source.Scan(&tag.ID, &tag.Name, &tag.Color, &tag.Revision, &createdAt, &updatedAt); err != nil {
		return library.Tag{}, err
	}

	var err error

	tag.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return library.Tag{}, fmt.Errorf("parse tag created_at: %w", err)
	}

	tag.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return library.Tag{}, fmt.Errorf("parse tag updated_at: %w", err)
	}

	return tag, nil
}

func scanFacet(source scanner) (library.Facet, error) {
	var (
		facet                library.Facet
		createdAt, updatedAt string
	)
	if err := source.Scan(&facet.ID, &facet.Kind, &facet.Value, &facet.Revision, &createdAt, &updatedAt); err != nil {
		return library.Facet{}, err
	}

	var err error

	facet.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return library.Facet{}, fmt.Errorf("parse facet created_at: %w", err)
	}

	facet.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return library.Facet{}, fmt.Errorf("parse facet updated_at: %w", err)
	}

	return facet, nil
}

func (s *Store) labelCounts(ctx context.Context, table, column string) (map[string]uint64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT assignment.`+column+`, COUNT(*)
		FROM `+table+` AS assignment
		JOIN library_recipes AS recipes ON recipes.id = assignment.entry_id
		WHERE recipes.library_id = ? AND recipes.deleted_at IS NULL
		GROUP BY assignment.`+column, scopeID(ctx))
	if err != nil {
		return nil, fmt.Errorf("count label recipes: %w", err)
	}
	defer rows.Close()

	counts := map[string]uint64{}

	for rows.Next() {
		var (
			id    string
			count uint64
		)

		if err := rows.Scan(&id, &count); err != nil {
			return nil, fmt.Errorf("scan label count: %w", err)
		}

		counts[id] = count
	}

	return counts, rows.Err()
}
