package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/event"
	"github.com/savorum/savorum/internal/identity"
)

func appendDomainEvent(ctx context.Context, tx *sql.Tx, kind, libraryID, resourceType, resourceID string, payload event.Payload) error {
	if !event.ValidType(kind) {
		return fmt.Errorf("unregistered domain event: %s", kind)
	}

	value := event.Envelope{ID: "evt_" + rand.Text(), Type: kind, Version: 1, OccurredAt: time.Now().UTC(), ActorID: attributionActor(ctx), LibraryID: libraryID, Resource: event.Resource{Type: resourceType, ID: resourceID}, CorrelationID: identity.Correlation(ctx), Payload: payload}

	data, err := json.Marshal(value)
	if err != nil {
		return operationError("appendDomainEvent", err)
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO domain_events(id,event_type,library_id,occurred_at,envelope_json) VALUES(?,?,?,?,?)`, value.ID, kind, libraryID, value.OccurredAt.Unix(), string(data))

	return operationError("appendDomainEvent", err)
}

// ReadEvents filters before returning bytes and rechecks membership and token
// grants. A missing cursor is an explicit resync, never silent replay loss.
func (s *Store) ReadEvents(ctx context.Context, after string, limit int) (event.Page, error) {
	result := event.Page{Events: []event.Envelope{}}
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return result, operationError("ReadEvents", err)
	}

	administrator, scopes, err := s.eventAuthority(ctx)
	if err != nil {
		return result, err
	}

	if limit < 1 || limit > 100 {
		limit = 100
	}

	result, cursor, err := s.eventCursor(ctx, after)
	if err != nil || result.Resync {
		return result, err
	}

	owner := scopes == nil && s.authorizeScope(ctx, access.Owner) == nil

	rows, err := s.db.QueryContext(ctx, `SELECT envelope_json FROM domain_events WHERE library_id=? AND sequence>? AND occurred_at>unixepoch()-604800 ORDER BY sequence LIMIT ?`, scopeID(ctx), cursor, limit)
	if err != nil {
		return result, operationError("ReadEvents", err)
	}
	defer rows.Close()

	result.Cursor = after

	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return result, operationError("ReadEvents", err)
		}

		var value event.Envelope
		if err := json.Unmarshal([]byte(data), &value); err != nil {
			return result, operationError("ReadEvents", err)
		}

		if eventVisible(event.ReadCapability(value.Type), scopes, administrator, owner) {
			result.Events = append(result.Events, value)
		}

		result.Cursor = value.ID
	}

	return result, operationError("ReadEvents", rows.Err())
}

func (s *Store) eventAuthority(ctx context.Context) (bool, []string, error) {
	p, ok := identity.FromContext(ctx)
	if !ok {
		return false, nil, access.ErrForbidden
	}

	if p.CredentialID != "" && p.Method != "token" {
		// Personal tokens are rechecked by authorizeScope; sessions also expire
		// while a connection is open.
		if len(p.CredentialID) < 4 || p.CredentialID[:4] != "pat:" {
			var active bool

			err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM browser_sessions WHERE id=? AND user_id=? AND revoked_at IS NULL AND idle_expires>unixepoch() AND absolute_expires>unixepoch())`, p.CredentialID, p.UserID).Scan(&active)
			if err != nil {
				return false, nil, operationError("ReadEvents", err)
			}

			if !active {
				return false, nil, access.ErrForbidden
			}
		}
	}

	var administrator bool
	if err := s.db.QueryRowContext(ctx, `SELECT instance_admin FROM users WHERE id=? AND status='active'`, p.UserID).Scan(&administrator); err != nil {
		return false, nil, access.ErrForbidden
	}

	scopes, err := tokenScopes(ctx, s.db)
	if err != nil {
		return false, nil, operationError("ReadEvents", err)
	}

	return administrator, scopes, nil
}

// eventVisible decides one event for a session (nil scopes) or a token.
func eventVisible(capability string, scopes []string, administrator, owner bool) bool {
	if scopes != nil {
		return slices.Contains(scopes, capability)
	}

	switch capability {
	case "instance:admin":
		return administrator
	case "library:owner":
		return owner
	default:
		return true
	}
}

// eventCursor resolves a stream position. "0" is before the library's first
// event, "seq:N" is the position a resync gave a library whose history was all
// pruned, and anything else is an event ID. The reader has missed history when
// a pruned or expired event of this library lies after that position.
func (s *Store) eventCursor(ctx context.Context, after string) (event.Page, int64, error) {
	result := event.Page{Events: []event.Envelope{}}

	position, known, err := s.eventPosition(ctx, after)
	if err != nil {
		return result, 0, err
	}

	if known {
		var missed bool

		err := s.db.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM event_horizons WHERE library_id=? AND sequence>?)
			    OR EXISTS(SELECT 1 FROM domain_events WHERE library_id=? AND sequence>? AND occurred_at<=unixepoch()-604800)`,
			scopeID(ctx), position, scopeID(ctx), position).Scan(&missed)
		if err != nil {
			return result, 0, operationError("ReadEvents", err)
		}

		if !missed {
			return result, position, nil
		}
	}

	var latest string

	err = s.db.QueryRowContext(ctx, `SELECT id FROM domain_events WHERE library_id=? ORDER BY sequence DESC LIMIT 1`, scopeID(ctx)).Scan(&latest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, 0, operationError("ReadEvents", err)
	}

	if latest == "" {
		var horizon int64
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM event_horizons WHERE library_id=?`, scopeID(ctx)).Scan(&horizon); err != nil {
			return result, 0, operationError("ReadEvents", err)
		}

		latest = "0"
		if horizon > 0 {
			latest = "seq:" + strconv.FormatInt(horizon, 10)
		}
	}

	result.Cursor, result.Resync = latest, true

	return result, 0, nil
}

func (s *Store) eventPosition(ctx context.Context, after string) (int64, bool, error) {
	switch {
	case after == "":
		return 0, false, nil
	case after == "0":
		return 0, true, nil
	case strings.HasPrefix(after, "seq:"):
		position, err := strconv.ParseInt(strings.TrimPrefix(after, "seq:"), 10, 64)
		return position, err == nil && position >= 0, nil
	}

	var position int64

	err := s.db.QueryRowContext(ctx, `SELECT sequence FROM domain_events WHERE id=? AND library_id=?`, after, scopeID(ctx)).Scan(&position)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}

	if err != nil {
		return 0, false, operationError("ReadEvents", err)
	}

	return position, true, nil
}
