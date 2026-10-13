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

// CreateSavedView stores a new saved view.
func (s *Store) CreateSavedView(ctx context.Context, view library.SavedView) error {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.authorizeScopeWith(ctx, tx, access.Viewer); err != nil {
		return err
	}

	if err := recordCreation(ctx, tx, view.ID); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, "INSERT INTO saved_views (library_id,user_id,id,title,query,sort,revision,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)", scopeID(ctx), currentUserID(ctx), view.ID, view.Title, view.Query, view.Sort, view.Revision, formatTime(view.CreatedAt), formatTime(view.UpdatedAt))
	if err != nil {
		if isConstraint(err) {
			return library.ErrConflict
		}

		return fmt.Errorf("insert saved view: %w", err)
	}

	return operationError("commit creation", tx.Commit())
}

// GetSavedView returns the saved view identified by id.
func (s *Store) GetSavedView(ctx context.Context, id string) (library.SavedView, error) {
	if err := s.scopedObject(ctx, "saved_views", id, access.Viewer); err != nil {
		return library.SavedView{}, err
	}

	if err := s.requireSavedViewOwner(ctx, id); err != nil {
		return library.SavedView{}, err
	}

	return readSavedView(s.db.QueryRowContext(ctx, "SELECT id,title,query,sort,revision,created_at,updated_at FROM saved_views WHERE id=?", id))
}

// ListSavedViews returns all saved views in display order.
func (s *Store) ListSavedViews(ctx context.Context) ([]library.SavedView, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, "SELECT id,title,query,sort,revision,created_at,updated_at FROM saved_views WHERE library_id=? AND user_id=? ORDER BY title COLLATE NOCASE,id", scopeID(ctx), currentUserID(ctx))
	if err != nil {
		return nil, fmt.Errorf("query saved views: %w", err)
	}
	defer rows.Close()

	result := []library.SavedView{}

	for rows.Next() {
		item, err := readSavedView(rows)
		if err != nil {
			return nil, fmt.Errorf("scan saved view: %w", err)
		}

		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("iterate saved views: %w", err)
	}

	return result, nil
}

// ReplaceSavedView updates a view when its revision matches expected.
func (s *Store) ReplaceSavedView(ctx context.Context, expected uint64, view library.SavedView) (library.SavedView, error) {
	if err := s.scopedObject(ctx, "saved_views", view.ID, access.Viewer); err != nil {
		return library.SavedView{}, err
	}

	if err := s.requireSavedViewOwner(ctx, view.ID); err != nil {
		return library.SavedView{}, err
	}

	var current uint64
	if err := s.db.QueryRowContext(ctx, "SELECT revision FROM saved_views WHERE id=?", view.ID).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return library.SavedView{}, library.ErrNotFound
	} else if err != nil {
		return library.SavedView{}, fmt.Errorf("read saved view: %w", err)
	}

	if current != expected {
		return library.SavedView{}, &library.PreconditionError{Expected: expected, Current: current}
	}

	result, err := s.db.ExecContext(ctx, "UPDATE saved_views SET title=?,query=?,sort=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?", view.Title, view.Query, view.Sort, formatTime(view.UpdatedAt), view.ID, expected)
	if err != nil {
		return library.SavedView{}, fmt.Errorf("update saved view: %w", err)
	}

	if affected, _ := result.RowsAffected(); affected != 1 {
		return library.SavedView{}, &library.PreconditionError{Expected: expected, Current: current}
	}

	return s.GetSavedView(ctx, view.ID)
}

// DeleteSavedView removes a view when its revision matches expected.
func (s *Store) DeleteSavedView(ctx context.Context, id string, expected uint64) error {
	if err := s.scopedObject(ctx, "saved_views", id, access.Viewer); err != nil {
		return err
	}

	if err := s.requireSavedViewOwner(ctx, id); err != nil {
		return err
	}

	var current uint64
	if err := s.db.QueryRowContext(ctx, "SELECT revision FROM saved_views WHERE id=?", id).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return library.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read saved view revision for deletion: %w", err)
	}

	if current != expected {
		return &library.PreconditionError{Expected: expected, Current: current}
	}

	result, err := s.db.ExecContext(ctx, "DELETE FROM saved_views WHERE id=? AND revision=?", id, expected)
	if err != nil {
		return fmt.Errorf("delete saved view: %w", err)
	}

	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("read saved view deletion result: %w", err)
	} else if affected != 1 {
		return &library.PreconditionError{Expected: expected, Current: current}
	}

	return nil
}

func readSavedView(source scanner) (library.SavedView, error) {
	var (
		view             library.SavedView
		created, updated string
	)
	if err := source.Scan(&view.ID, &view.Title, &view.Query, &view.Sort, &view.Revision, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return view, library.ErrNotFound
		}

		return view, fmt.Errorf("scan saved view: %w", err)
	}

	var err error

	view.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return view, fmt.Errorf("parse saved view creation time: %w", err)
	}

	view.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return view, fmt.Errorf("parse saved view updated_at: %w", err)
	}

	return view, nil
}
