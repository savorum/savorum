package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/mealcalendar"
)

var _ mealcalendar.Persistence = (*Store)(nil)

var calendarFeedColumns = `id, library_id, name, created_at, created_by, ` + actorDisplayFor("calendar_feeds.created_by") + `, rotated_at, last_read_at`

// CreateCalendarFeed stores a feed for the selected library.
func (s *Store) CreateCalendarFeed(ctx context.Context, feed mealcalendar.CalendarFeed, tokenHash string) error {
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

	if err := recordCreation(ctx, tx, feed.ID); err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO calendar_feeds (id, library_id, name, token_hash, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		feed.ID, scopeID(ctx), feed.Name, tokenHash, attributionActor(ctx), formatTime(feed.CreatedAt)); err != nil {
		return fmt.Errorf("create calendar feed: %w", err)
	}

	return operationError("commit creation", tx.Commit())
}

// ListCalendarFeeds returns the reader's feeds, or all of them for an owner.
func (s *Store) ListCalendarFeeds(ctx context.Context) ([]mealcalendar.CalendarFeed, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, err
	}

	owner := s.authorizeScope(ctx, access.Owner) == nil
	actor := attributionActor(ctx)

	rows, err := s.db.QueryContext(ctx, `SELECT `+calendarFeedColumns+` FROM calendar_feeds WHERE library_id = ? AND (? OR created_by = ?) ORDER BY created_at, id`, scopeID(ctx), owner, actor)
	if err != nil {
		return nil, fmt.Errorf("list calendar feeds: %w", err)
	}
	defer rows.Close()

	feeds := []mealcalendar.CalendarFeed{}

	for rows.Next() {
		feed, err := scanCalendarFeed(rows)
		if err != nil {
			return nil, err
		}

		feed.Mine = feed.CreatedBy == actor
		feeds = append(feeds, feed)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate calendar feeds: %w", err)
	}

	return feeds, nil
}

// RotateCalendarFeed replaces a feed's secret.
func (s *Store) RotateCalendarFeed(ctx context.Context, id, tokenHash string, at time.Time) (mealcalendar.CalendarFeed, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return mealcalendar.CalendarFeed{}, err
	}

	owner := s.authorizeScope(ctx, access.Owner) == nil
	actor := attributionActor(ctx)

	result, err := s.db.ExecContext(ctx, `UPDATE calendar_feeds SET token_hash = ?, rotated_at = ? WHERE id = ? AND library_id = ? AND (? OR created_by = ?)`,
		tokenHash, formatTime(at), id, scopeID(ctx), owner, actor)
	if err != nil {
		return mealcalendar.CalendarFeed{}, fmt.Errorf("rotate calendar feed: %w", err)
	}

	if changed, _ := result.RowsAffected(); changed == 0 {
		return mealcalendar.CalendarFeed{}, mealcalendar.ErrNotFound
	}

	feed, err := scanCalendarFeed(s.db.QueryRowContext(ctx, `SELECT `+calendarFeedColumns+` FROM calendar_feeds WHERE id = ?`, id))
	if err != nil {
		return mealcalendar.CalendarFeed{}, err
	}

	feed.Mine = feed.CreatedBy == actor

	return feed, nil
}

// DeleteCalendarFeed revokes a feed.
func (s *Store) DeleteCalendarFeed(ctx context.Context, id string) error {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return err
	}

	owner := s.authorizeScope(ctx, access.Owner) == nil

	result, err := s.db.ExecContext(ctx, `DELETE FROM calendar_feeds WHERE id = ? AND library_id = ? AND (? OR created_by = ?)`, id, scopeID(ctx), owner, attributionActor(ctx))
	if err != nil {
		return fmt.Errorf("delete calendar feed: %w", err)
	}

	if changed, _ := result.RowsAffected(); changed == 0 {
		return mealcalendar.ErrNotFound
	}

	return nil
}

// ResolveCalendarFeed finds a feed and its active creator. Membership is
// checked when the plan is read as that creator.
func (s *Store) ResolveCalendarFeed(ctx context.Context, tokenHash string, at time.Time) (mealcalendar.CalendarFeed, identity.Principal, error) {
	var principal identity.Principal

	feed, err := scanCalendarFeed(s.db.QueryRowContext(ctx, `SELECT `+calendarFeedColumns+` FROM calendar_feeds WHERE token_hash = ?
		AND EXISTS (SELECT 1 FROM users WHERE users.id = calendar_feeds.created_by AND users.status = 'active')`, tokenHash))
	if err != nil {
		return feed, principal, err
	}

	principal = identity.Principal{UserID: feed.CreatedBy, DisplayName: feed.CreatedByName, Method: "calendar-feed", CredentialID: "feed:" + feed.ID}

	if _, err := s.db.ExecContext(ctx, `UPDATE calendar_feeds SET last_read_at = ? WHERE id = ? AND (last_read_at IS NULL OR last_read_at < ?)`,
		formatTime(at), feed.ID, formatTime(at.Add(-5*time.Minute))); err != nil {
		return feed, principal, fmt.Errorf("record calendar feed read: %w", err)
	}

	return feed, principal, nil
}

func scanCalendarFeed(row rowScanner) (mealcalendar.CalendarFeed, error) {
	var (
		feed              mealcalendar.CalendarFeed
		created           string
		rotated, lastRead sql.NullString
	)

	if err := row.Scan(&feed.ID, &feed.LibraryID, &feed.Name, &created, &feed.CreatedBy, &feed.CreatedByName, &rotated, &lastRead); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return feed, mealcalendar.ErrNotFound
		}

		return feed, fmt.Errorf("scan calendar feed: %w", err)
	}

	feed.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	feed.RotatedAt = parseOptionalTime(rotated)
	feed.LastReadAt = parseOptionalTime(lastRead)

	return feed, nil
}

func parseOptionalTime(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}

	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil
	}

	return &parsed
}
