package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/savorum/savorum/internal/event"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/library"
)

var _ library.OrganizationRepository = (*Store)(nil)

// GetOrganization returns the organizational metadata for a recipe and user.
func (s *Store) GetOrganization(ctx context.Context, recipeID, userID string) (library.Organization, error) {
	if err := requirePersonalUser(ctx, userID); err != nil {
		return library.Organization{}, err
	}

	if err := s.scopedObject(ctx, "library_recipes", recipeID, access.Viewer); err != nil {
		return library.Organization{}, err
	}

	return readOrganization(ctx, s.db, recipeID, userID)
}

// ReplaceOrganization atomically replaces a recipe's organization at its expected revision.
func (s *Store) ReplaceOrganization(ctx context.Context, recipeID, userID string, expectedRevision uint64, replacement library.Organization, membership []library.BookMembershipChange, timestamp time.Time) (library.Organization, error) {
	if err := requirePersonalUser(ctx, userID); err != nil {
		return library.Organization{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return library.Organization{}, fmt.Errorf("begin organization replacement: %w", err)
	}
	defer tx.Rollback()

	if err := s.checkOrganizationWrite(ctx, tx, recipeID, userID, replacement, membership); err != nil {
		return library.Organization{}, err
	}

	var (
		current   uint64
		deletedAt sql.NullString
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT organization.revision, recipes.deleted_at
		FROM recipe_organization_state AS organization
		JOIN library_recipes AS recipes ON recipes.id = organization.entry_id
		WHERE organization.entry_id = ?`, recipeID).Scan(&current, &deletedAt); errors.Is(err, sql.ErrNoRows) {
		return library.Organization{}, library.ErrNotFound
	} else if err != nil {
		return library.Organization{}, fmt.Errorf("read organization revision: %w", err)
	}

	if deletedAt.Valid {
		return library.Organization{}, library.ErrInvalidState
	}

	if current != expectedRevision {
		return library.Organization{}, &library.PreconditionError{EntryID: library.EntryID(recipeID), Expected: expectedRevision, Current: current}
	}

	before, err := readOrganization(ctx, tx, recipeID, userID)
	if err != nil {
		return library.Organization{}, err
	}

	sharedChanged := sharedOrganizationChanged(before, replacement)
	if err := replaceRecipeClassifications(ctx, tx, recipeID, replacement.FacetIDs, replacement.TagIDs); err != nil {
		return library.Organization{}, err
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM recipe_sources WHERE entry_id = ?", recipeID); err != nil {
		return library.Organization{}, fmt.Errorf("clear recipe sources: %w", err)
	}

	if err := insertOrganizationSources(ctx, tx, recipeID, replacement.Sources); err != nil {
		return library.Organization{}, err
	}

	// last_cooked and times_cooked are a cache of recipe_cookings and are left
	// alone here. Curating a recipe cannot change how often it has been cooked.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO recipe_user_state (
			entry_id, user_id, favorite, rating, personal_note, last_cooked, times_cooked,
			preferred_yield, preferred_scale, preferred_yield_assertion
		) VALUES (?, ?, ?, ?, ?, NULL, 0, ?, ?, ?)
		ON CONFLICT (entry_id, user_id) DO UPDATE SET
			favorite = excluded.favorite, rating = excluded.rating,
			personal_note = excluded.personal_note,
			preferred_yield = excluded.preferred_yield,
			preferred_scale = excluded.preferred_scale,
			preferred_yield_assertion = excluded.preferred_yield_assertion`,
		recipeID, userID, replacement.State.Favorite, nullableRating(replacement.State.Rating), replacement.State.PersonalNote,
		nullableRational(replacement.State.PreferredYield), nullableRational(replacement.State.PreferredScale),
		nullableIndex(replacement.State.PreferredYieldAssertion)); err != nil {
		return library.Organization{}, fmt.Errorf("replace recipe user state: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE recipe_organization_state SET revision = revision + 1, updated_at = ?
		WHERE entry_id = ? AND revision = ?`, formatTime(timestamp), recipeID, expectedRevision)
	if err != nil {
		return library.Organization{}, fmt.Errorf("advance organization revision: %w", err)
	}

	if affected, err := result.RowsAffected(); err != nil {
		return library.Organization{}, fmt.Errorf("read organization update result: %w", err)
	} else if affected != 1 {
		return library.Organization{}, &library.PreconditionError{EntryID: library.EntryID(recipeID), Expected: expectedRevision, Current: current}
	}

	for _, change := range membership {
		if err := applyBookMembership(ctx, tx, library.EntryID(recipeID), change, timestamp); err != nil {
			return library.Organization{}, err
		}
	}

	if sharedChanged {
		if err := appendDomainEvent(ctx, tx, "recipe.organization.updated", scopeID(ctx), "recipe", recipeID, event.Payload{Revision: current + 1}); err != nil {
			return library.Organization{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return library.Organization{}, fmt.Errorf("commit organization replacement: %w", err)
	}

	return s.GetOrganization(ctx, recipeID, userID)
}

func replaceRecipeClassifications(ctx context.Context, tx *sql.Tx, recipeID string, facetIDs, tagIDs []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM recipe_facets WHERE entry_id = ?", recipeID); err != nil {
		return fmt.Errorf("clear recipe facets: %w", err)
	}

	for _, facetID := range facetIDs {
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM facets WHERE id = ?)", facetID).Scan(&exists); err != nil {
			return fmt.Errorf("check facet %q: %w", facetID, err)
		}

		if exists == 0 {
			return fmt.Errorf("facet %q does not exist: %w", facetID, library.ErrInvalidOrganization)
		}

		if _, err := tx.ExecContext(ctx, "INSERT INTO recipe_facets (entry_id, facet_id) VALUES (?, ?)", recipeID, facetID); err != nil {
			return fmt.Errorf("link facet %q: %w", facetID, err)
		}
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM recipe_tags WHERE entry_id = ?", recipeID); err != nil {
		return fmt.Errorf("clear recipe tags: %w", err)
	}

	for _, tagID := range tagIDs {
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM tags WHERE id = ?)", tagID).Scan(&exists); err != nil {
			return fmt.Errorf("check tag %q: %w", tagID, err)
		}

		if exists == 0 {
			return fmt.Errorf("tag %q does not exist: %w", tagID, library.ErrInvalidOrganization)
		}

		if _, err := tx.ExecContext(ctx, "INSERT INTO recipe_tags (entry_id, tag_id) VALUES (?, ?)", recipeID, tagID); err != nil {
			return fmt.Errorf("link tag %q: %w", tagID, err)
		}
	}

	return nil
}

// applyBookMembership adds or removes one recipe's place in one book inside the
// caller's transaction. A membership that already matches the request is left
// alone so an unrelated save never advances a book revision.
func applyBookMembership(ctx context.Context, tx *sql.Tx, entryID library.EntryID, change library.BookMembershipChange, timestamp time.Time) error {
	var (
		current   uint64
		deletedAt sql.NullString
	)
	if err := tx.QueryRowContext(ctx, "SELECT revision, deleted_at FROM books WHERE id = ?", change.BookID).Scan(&current, &deletedAt); errors.Is(err, sql.ErrNoRows) {
		return library.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read book revision for membership: %w", err)
	}

	if deletedAt.Valid {
		return library.ErrInvalidState
	}

	if current != change.Revision {
		return &library.PreconditionError{Expected: change.Revision, Current: current}
	}

	var existing sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT id FROM book_entries WHERE book_id = ? AND entry_id = ?", change.BookID, entryID).Scan(&existing); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read book membership: %w", err)
	}

	if change.Member == existing.Valid {
		return nil
	}

	if change.Member {
		var active int
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM library_recipes WHERE id = ? AND deleted_at IS NULL)", entryID).Scan(&active); err != nil {
			return fmt.Errorf("check recipe for book membership: %w", err)
		}

		if active == 0 {
			return library.ErrNotFound
		}

		var position int
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(position), -1) + 1 FROM book_entries WHERE book_id = ? AND section_id IS NULL", change.BookID).Scan(&position); err != nil {
			return fmt.Errorf("read next book position: %w", err)
		}

		if _, err := tx.ExecContext(ctx, "INSERT INTO book_entries (id,book_id,section_id,entry_id,position,note) VALUES (?,?,NULL,?,?,'')", change.EntryID, change.BookID, entryID, position); err != nil {
			return fmt.Errorf("insert book membership: %w", err)
		}
	} else if _, err := tx.ExecContext(ctx, "DELETE FROM book_entries WHERE book_id = ? AND entry_id = ?", change.BookID, entryID); err != nil {
		return fmt.Errorf("remove book membership: %w", err)
	}

	book, err := readBookProjection(ctx, tx, change.BookID, library.StateActive)
	if err != nil {
		return fmt.Errorf("read book after membership change: %w", err)
	}

	if err := writeBookHead(ctx, tx, book, change.Revision, timestamp); err != nil {
		return fmt.Errorf("advance book after membership change: %w", err)
	}

	return insertBookRevision(ctx, tx, change.BookID, library.RevisionMetadata{Cause: library.RevisionCauseManualEdit}, timestamp)
}

type organizationQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readOrganization(ctx context.Context, queryer organizationQueryer, recipeID, userID string) (library.Organization, error) {
	var (
		result    library.Organization
		updatedAt string
		deletedAt sql.NullString
	)

	err := queryer.QueryRowContext(ctx, `
		SELECT organization.entry_id, organization.revision, organization.updated_at, recipes.deleted_at
		FROM recipe_organization_state AS organization
		JOIN library_recipes AS recipes ON recipes.id = organization.entry_id
		WHERE organization.entry_id = ?`, recipeID).Scan(&result.EntryID, &result.Revision, &updatedAt, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return library.Organization{}, library.ErrNotFound
	}

	if err != nil {
		return library.Organization{}, fmt.Errorf("read recipe organization: %w", err)
	}

	if deletedAt.Valid {
		return library.Organization{}, library.ErrInvalidState
	}

	result.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return library.Organization{}, fmt.Errorf("parse organization updated_at: %w", err)
	}

	result.FacetIDs = []string{}
	result.TagIDs = []string{}
	result.Sources = []library.RecipeSource{}

	rows, err := queryer.QueryContext(ctx, `
		SELECT facets.id
		FROM recipe_facets JOIN facets ON facets.id = recipe_facets.facet_id
		WHERE recipe_facets.entry_id = ?
		ORDER BY facets.id`, recipeID)
	if err != nil {
		return library.Organization{}, fmt.Errorf("query recipe facets: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return library.Organization{}, fmt.Errorf("scan recipe facet: %w", err)
		}

		result.FacetIDs = append(result.FacetIDs, id)
	}

	if err := rows.Err(); err != nil {
		return library.Organization{}, fmt.Errorf("iterate recipe facets: %w", err)
	}

	if err := rows.Close(); err != nil {
		return library.Organization{}, fmt.Errorf("close recipe facets: %w", err)
	}

	rows, err = queryer.QueryContext(ctx, `
		SELECT tags.id
		FROM recipe_tags JOIN tags ON tags.id = recipe_tags.tag_id
		WHERE recipe_tags.entry_id = ? ORDER BY tags.id`, recipeID)
	if err != nil {
		return library.Organization{}, fmt.Errorf("query recipe tags: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return library.Organization{}, fmt.Errorf("scan recipe tag: %w", err)
		}

		result.TagIDs = append(result.TagIDs, id)
	}

	if err := rows.Err(); err != nil {
		return library.Organization{}, fmt.Errorf("iterate recipe tags: %w", err)
	}

	if err := rows.Close(); err != nil {
		return library.Organization{}, fmt.Errorf("close recipe tags: %w", err)
	}

	result.State, err = readRecipeUserState(ctx, queryer, recipeID, userID)
	if err != nil {
		return library.Organization{}, err
	}

	rows, err = queryer.QueryContext(ctx, `
		SELECT id, kind, url, COALESCE(publication_book_id, ''), page_start, page_end, original_title, attribution, accessed_at
		FROM recipe_sources WHERE entry_id = ? ORDER BY id`, recipeID)
	if err != nil {
		return library.Organization{}, fmt.Errorf("query recipe sources: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			item               library.RecipeSource
			pageStart, pageEnd sql.NullInt64
			accessed           sql.NullString
		)
		if err := rows.Scan(&item.ID, &item.Kind, &item.URL, &item.PublicationBookID, &pageStart, &pageEnd, &item.OriginalTitle, &item.Attribution, &accessed); err != nil {
			_ = rows.Close()
			return library.Organization{}, fmt.Errorf("scan recipe source: %w", err)
		}

		item.PageStart = countOrNil(pageStart)

		item.PageEnd = countOrNil(pageEnd)

		if accessed.Valid {
			v, parseErr := time.Parse(time.RFC3339Nano, accessed.String)
			if parseErr != nil {
				_ = rows.Close()
				return library.Organization{}, fmt.Errorf("parse source accessed_at: %w", parseErr)
			}

			item.AccessedAt = &v
		}

		result.Sources = append(result.Sources, item)
	}

	if err := rows.Err(); err != nil {
		return library.Organization{}, fmt.Errorf("iterate recipe sources: %w", err)
	}

	if err := rows.Close(); err != nil {
		return library.Organization{}, fmt.Errorf("close recipe sources: %w", err)
	}

	return result, nil
}

func readRecipeUserState(ctx context.Context, queryer organizationQueryer, recipeID, userID string) (library.UserState, error) {
	var result library.UserState

	var (
		favorite                       bool
		rating                         sql.NullInt64
		personalNote                   string
		preferredYield, preferredScale sql.NullString
		preferredAssertion             sql.NullInt64
	)

	err := queryer.QueryRowContext(ctx, `SELECT favorite, rating, personal_note, preferred_yield, preferred_scale, preferred_yield_assertion FROM recipe_user_state WHERE entry_id = ? AND user_id = ?`, recipeID, userID).Scan(&favorite, &rating, &personalNote, &preferredYield, &preferredScale, &preferredAssertion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return library.UserState{}, fmt.Errorf("read recipe user state: %w", err)
	}

	if err == nil {
		result.Favorite = favorite
		result.PersonalNote = personalNote

		if rating.Valid {
			// Clamped to 0..5 on the same line, so the conversion cannot lose a
			// rating a reader gave.
			value := uint8(min(max(rating.Int64, 0), 5))
			result.Rating = &value
		}

		result.PreferredYield = rationalOrNil(preferredYield)
		result.PreferredScale = rationalOrNil(preferredScale)

		if preferredAssertion.Valid {
			index := int(preferredAssertion.Int64)
			result.PreferredYieldAssertion = &index
		}
	}

	return result, nil
}

func sharedOrganizationChanged(before, after library.Organization) bool {
	return !slices.Equal(before.TagIDs, after.TagIDs) || !slices.Equal(before.FacetIDs, after.FacetIDs) || !reflect.DeepEqual(before.Sources, after.Sources)
}

func insertOrganizationSources(ctx context.Context, tx *sql.Tx, recipeID string, sources []library.RecipeSource) error {
	for _, source := range sources {
		if source.PublicationBookID != "" {
			var kind library.BookKind
			if err := tx.QueryRowContext(ctx, "SELECT kind FROM books WHERE id = ?", source.PublicationBookID).Scan(&kind); errors.Is(err, sql.ErrNoRows) {
				return library.ErrNotFound
			} else if err != nil {
				return fmt.Errorf("read source publication: %w", err)
			} else if kind != library.BookPublication {
				return fmt.Errorf("source book is not a publication: %w", library.ErrInvalidOrganization)
			}
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO recipe_sources (
				id, entry_id, kind, url, publication_book_id, page_start, page_end,
				original_title, attribution, accessed_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			source.ID, recipeID, source.Kind, source.URL, nullableString(source.PublicationBookID), nullableUint64(source.PageStart), nullableUint64(source.PageEnd), source.OriginalTitle, source.Attribution, nullableTime(source.AccessedAt)); err != nil {
			return fmt.Errorf("insert recipe source: %w", err)
		}
	}

	return nil
}

func nullableIndex(value *int) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}

	return sql.NullInt64{Int64: int64(*value), Valid: true}
}
