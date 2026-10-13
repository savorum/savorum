package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/automation"
	"github.com/savorum/savorum/internal/event"
	"github.com/savorum/savorum/internal/identity"
)

// CreateAutomationJob atomically creates a job and its 24-hour retry receipt.
func (s *Store) CreateAutomationJob(ctx context.Context, record automation.Record) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operationError("CreateAutomationJob", err)
	}
	defer tx.Rollback()

	role := access.Editor
	if record.Job.Kind == "export" || record.Job.Kind == "bulk-read" {
		role = access.Viewer
	}

	if err := s.authorizeScopeWith(ctx, tx, role); err != nil {
		return operationError("CreateAutomationJob", err)
	}

	if err := authorizeTokenCapability(ctx, tx, automationCapability(record.Job.Kind)); err != nil {
		return operationError("CreateAutomationJob", err)
	}

	p, ok := identity.FromContext(ctx)
	if !ok {
		return access.ErrForbidden
	}

	record.Actor = p
	record.LibraryID = scopeID(ctx)

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM automation_jobs WHERE library_id=? AND expires_at>unixepoch()`, scopeID(ctx)).Scan(&count); err != nil {
		return operationError("CreateAutomationJob", err)
	}

	if count >= 1000 {
		return automation.ErrCapacity
	}

	data, err := json.Marshal(record)
	if err != nil {
		return operationError("CreateAutomationJob", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO automation_jobs(id,library_id,actor_id,credential_id,kind,state,revision,created_at,expires_at,record_json) VALUES(?,?,?,?,?,?,?,?,?,?)`, record.Job.ID, record.LibraryID, p.UserID, p.CredentialID, record.Job.Kind, record.Job.State, record.Job.Revision, record.Job.CreatedAt.Unix(), record.Job.ExpiresAt.Unix(), string(data)); err != nil {
		return operationError("CreateAutomationJob", err)
	}

	if err := recordIdempotency(ctx, tx, record.Job.ID); err != nil {
		return operationError("CreateAutomationJob", err)
	}

	return operationError("CreateAutomationJob", tx.Commit())
}

// AutomationJob rechecks current authority and creator scope, including downloads.
func (s *Store) AutomationJob(ctx context.Context, id string) (automation.Record, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return automation.Record{}, operationError("AutomationJob", err)
	}

	var data, credential string

	err := s.db.QueryRowContext(ctx, `SELECT record_json,credential_id FROM automation_jobs WHERE id=? AND library_id=? AND actor_id=? AND expires_at>unixepoch()`, id, scopeID(ctx), currentUserID(ctx)).Scan(&data, &credential)
	if errors.Is(err, sql.ErrNoRows) {
		return automation.Record{}, automation.ErrNotFound
	}

	if err != nil {
		return automation.Record{}, operationError("AutomationJob", err)
	}

	var record automation.Record
	if err := json.Unmarshal([]byte(data), &record); err != nil {
		return record, operationError("AutomationJob", err)
	}

	record.Actor.CredentialID = credential
	if err := authorizeTokenCapability(ctx, s.db, automationCapability(record.Job.Kind)); err != nil {
		return automation.Record{}, operationError("AutomationJob", err)
	}

	return record, nil
}

// SaveAutomationJob prevents a worker from overwriting cancellation or a commit.
func (s *Store) SaveAutomationJob(ctx context.Context, record *automation.Record) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operationError("SaveAutomationJob", err)
	}
	defer tx.Rollback()

	if err := s.authorizeScopeWith(ctx, tx, access.Viewer); err != nil {
		return operationError("SaveAutomationJob", err)
	}

	if err := authorizeTokenCapability(ctx, tx, automationCapability(record.Job.Kind)); err != nil {
		return operationError("SaveAutomationJob", err)
	}

	next := *record
	next.Job.Revision++

	data, err := json.Marshal(next)
	if err != nil {
		return operationError("SaveAutomationJob", err)
	}

	result, err := tx.ExecContext(ctx, `UPDATE automation_jobs SET state=?,revision=?,record_json=? WHERE id=? AND library_id=? AND actor_id=? AND revision=? AND expires_at>unixepoch()`, next.Job.State, next.Job.Revision, string(data), next.Job.ID, scopeID(ctx), currentUserID(ctx), record.Job.Revision)
	if err != nil {
		return operationError("SaveAutomationJob", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return operationError("SaveAutomationJob", err)
	}

	if n != 1 {
		return automation.ErrState
	}

	if err := appendAutomationResults(ctx, tx, next.Job.ID, next.Results); err != nil {
		return err
	}

	if next.Job.Kind == "export" && (next.Job.State == "completed" || next.Job.State == "failed") {
		if err := appendDomainEvent(ctx, tx, "export."+next.Job.State, scopeID(ctx), "export_job", next.Job.ID, event.Payload{State: next.Job.State}); err != nil {
			return operationError("SaveAutomationJob", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return operationError("SaveAutomationJob", err)
	}

	for i := range next.Results {
		next.Results[i].Entry = nil
	}

	*record = next

	return nil
}

// PendingAutomationJobs is a bounded worker-only read, never an HTTP operation.
func (s *Store) PendingAutomationJobs(ctx context.Context) ([]automation.Record, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT record_json,credential_id FROM automation_jobs WHERE state='pending' AND expires_at>unixepoch() ORDER BY created_at,id LIMIT 2`)
	if err != nil {
		return nil, operationError("PendingAutomationJobs", err)
	}
	defer rows.Close()

	result := []automation.Record{}

	for rows.Next() {
		var data, credential string
		if err := rows.Scan(&data, &credential); err != nil {
			return nil, operationError("PendingAutomationJobs", err)
		}

		var record automation.Record
		if err := json.Unmarshal([]byte(data), &record); err != nil {
			return nil, operationError("PendingAutomationJobs", err)
		}

		record.Actor.CredentialID = credential
		result = append(result, record)
	}

	return result, operationError("PendingAutomationJobs", rows.Err())
}

// RecoverAutomationJobs leaves interrupted mutations explicit. Repeating them
// with guessed revisions would turn an uncertain result into a lost update.
func (s *Store) RecoverAutomationJobs(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE automation_jobs SET state='interrupted',revision=revision+1,record_json=json_set(record_json,'$.job.state','interrupted','$.job.code','process-interrupted','$.job.revision',revision+1) WHERE state IN ('running','committing')`)
	return operationError("RecoverAutomationJobs", err)
}

// ExpireAutomationJobs removes private payloads and returns opaque artifact IDs.
func (s *Store) ExpireAutomationJobs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `DELETE FROM automation_jobs WHERE expires_at<=? RETURNING id`, time.Now().Unix())
	if err != nil {
		return nil, operationError("ExpireAutomationJobs", err)
	}
	defer rows.Close()

	ids := []string{}

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, operationError("ExpireAutomationJobs", err)
		}

		ids = append(ids, id)
	}

	return ids, operationError("ExpireAutomationJobs", rows.Err())
}

func automationCapability(kind string) string {
	switch kind {
	case "export", "bulk-read":
		return "recipes:read"
	case "import":
		return "imports:write"
	case "bulk-tags", "bulk-book":
		return "library:write"
	case "bulk-trash", "bulk-restore":
		return "recipes:write"
	default:
		return "unsupported"
	}
}

// FailAutomationJob records a delayed authorization refusal without granting the
// worker permission to read or mutate the protected resource.
func (s *Store) FailAutomationJob(ctx context.Context, id string, revision uint64, code string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operationError("begin refused job", err)
	}
	defer tx.Rollback()

	var kind, libraryID, actorID string

	err = tx.QueryRowContext(ctx, `UPDATE automation_jobs SET state='failed',revision=revision+1,record_json=json_set(record_json,'$.job.state','failed','$.job.code',?,'$.job.revision',revision+1) WHERE id=? AND revision=? AND state IN ('pending','running') RETURNING kind,library_id,actor_id`, code, id, revision).Scan(&kind, &libraryID, &actorID)
	if errors.Is(err, sql.ErrNoRows) {
		return automation.ErrState
	}

	if err != nil {
		return operationError("record refused job", err)
	}

	if kind == "export" {
		factContext := identity.WithPrincipal(ctx, identity.Principal{UserID: actorID})
		if err := appendDomainEvent(factContext, tx, "export.failed", libraryID, "export_job", id, event.Payload{State: "failed"}); err != nil {
			return err
		}
	}

	return operationError("commit refused job", tx.Commit())
}
