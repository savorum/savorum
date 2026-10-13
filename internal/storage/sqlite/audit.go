package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/savorum/savorum/internal/event"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

func appendAudit(ctx context.Context, tx *sql.Tx, event identity.AuditEvent) error {
	if event.ActorID == "" {
		event.ActorID = "system"
	}

	if event.Outcome == "" {
		event.Outcome = "succeeded"
	}

	p, _ := identity.FromContext(ctx)

	_, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,credential_id,action,target_type,target_id,library_id,correlation_id,outcome) VALUES(?,?,?,?,?,?,?,?)`, event.ActorID, p.CredentialID, event.Action, event.TargetType, event.TargetID, event.LibraryID, identity.Correlation(ctx), event.Outcome)
	if err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}

	switch event.Action {
	case "recipe.purged", "book.purged", "user.disabled", "user.deleted", "library.member.role_changed", "library.member.removed":
		return appendDomainEvent(ctx, tx, event.Action, event.LibraryID, event.TargetType, event.TargetID, domainEventPayload())
	}

	return nil
}

// AuditEvents enforces current administrator or library-owner access before querying.
func (s *Store) AuditEvents(ctx context.Context, f identity.AuditFilter) ([]identity.AuditEvent, error) {
	p, ok := identity.FromContext(ctx)
	if !ok {
		return nil, access.ErrForbidden
	}

	u, err := s.User(ctx, p.UserID)
	if err != nil {
		return nil, err
	}

	if u.Status != "active" {
		return nil, access.ErrForbidden
	}

	if !u.InstanceAdmin {
		if f.LibraryID == "" {
			return nil, access.ErrForbidden
		}

		if err := s.AuthorizeLibrary(ctx, f.LibraryID, access.Owner); err != nil {
			return nil, err
		}
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id,created_at,actor_id,credential_id,action,target_type,target_id,library_id,correlation_id,outcome FROM audit_events WHERE (?='' OR library_id=?) AND (?='' OR actor_id=?) AND (?='' OR action=? OR substr(action,1,length(?)+1)=?||'.') AND (?='' OR target_id=?) AND (?=0 OR created_at>=?) AND (?=0 OR created_at<=?) AND (?=0 OR id<?) ORDER BY id DESC LIMIT 100`, f.LibraryID, f.LibraryID, f.ActorID, f.ActorID, f.Category, f.Category, f.Category, f.Category, f.TargetID, f.TargetID, f.Since, f.Since, f.Until, f.Until, f.Before, f.Before)
	if err != nil {
		return nil, fmt.Errorf("query audit events: %w", err)
	}
	defer rows.Close()

	events := []identity.AuditEvent{}

	for rows.Next() {
		var e identity.AuditEvent
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.ActorID, &e.CredentialID, &e.Action, &e.TargetType, &e.TargetID, &e.LibraryID, &e.CorrelationID, &e.Outcome); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}

		events = append(events, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}

	return events, nil
}

func identityAuditEvent(actor, target, action string) identity.AuditEvent {
	e := identity.AuditEvent{ActorID: actor, TargetID: target, TargetType: "user", Action: action}

	names := map[string]string{"bootstrap": "user.bootstrapped", "create-user": "user.created", "delete-user": "user.deleted", "disable-user": "user.disabled", "enable-user": "user.enabled", "password-reset": "auth.password.changed", "recipe.purge": "recipe.purged", "book.purge": "book.purged", "library.create": "library.created", "library.delete": "library.deleted"}
	if name, ok := names[action]; ok {
		e.Action = name
	}

	if strings.HasPrefix(action, "library.") {
		e.TargetType = "library"
		e.LibraryID = target
	}

	if action == "settings.changed" {
		e.TargetType = "settings"
	}

	if action == "import.completed" {
		e.TargetType = "recipe"
	}

	if strings.HasPrefix(action, "recipe.") {
		e.TargetType = "recipe"
	}

	if strings.HasPrefix(action, "book.") {
		e.TargetType = "book"
	}

	parts := strings.Split(action, ":")
	if len(parts) > 1 {
		e.LibraryID = parts[1]
		switch parts[0] {
		case "member.set":
			e.Action = "library.member.role_changed"
		case "member.remove":
			e.Action = "library.member.removed"
		case "ownership.recovery":
			e.Action = "library.ownership.recovered"
		}
	}

	return e
}

// One global persisted bucket bounds audit amplification even when attackers rotate addresses/accounts.
func (s *Store) auditLoginFailure(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin failed login audit: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `INSERT INTO authentication_backoff(key_hash,failures,retry_at) VALUES('audit:login',1,unixepoch()+60) ON CONFLICT(key_hash) DO UPDATE SET retry_at=unixepoch()+60 WHERE retry_at<=unixepoch()`)
	if err != nil {
		return fmt.Errorf("limit failed login audit: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count audit allowance: %w", err)
	}

	if count > 0 {
		if err := appendAudit(ctx, tx, identity.AuditEvent{ActorID: "system", Action: "auth.login.failed", TargetType: "authentication", Outcome: "failed"}); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit failed login audit: %w", err)
	}

	return nil
}

// RecordAuthenticationFailure applies the same global audit bound to every provider.
func (s *Store) RecordAuthenticationFailure(ctx context.Context) error {
	return s.auditLoginFailure(ctx)
}

func domainEventPayload() event.Payload { return event.Payload{} }
