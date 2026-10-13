package sqlite

import "context"

// HasBackgroundWork includes delayed retries and facts not yet dispatched to webhooks.
// It is an instance lifecycle check, independent of request library scope.
func (s *Store) HasBackgroundWork(ctx context.Context) (bool, error) {
	var pending bool

	err := s.db.QueryRowContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM import_jobs WHERE state IN ('pending','running')) OR
		EXISTS(SELECT 1 FROM automation_jobs a WHERE state IN ('pending','running') OR
			(state='committing' AND NOT EXISTS (
				SELECT 1 FROM import_jobs i WHERE a.kind='import'
				AND i.id=json_extract(a.record_json,'$.job.import_job_id')
				AND i.state IN ('completed','completed-with-errors','cancelled','failed','interrupted')
			))) OR
		EXISTS(SELECT 1 FROM webhook_deliveries d JOIN webhook_subscriptions s ON s.id=d.subscription_id
			WHERE d.state IN ('pending','retrying') AND s.enabled=1 AND s.deleted_at=0) OR
		EXISTS(SELECT 1 FROM domain_events WHERE sequence > (SELECT sequence FROM event_consumers WHERE id='webhooks'))`).Scan(&pending)

	return pending, operationError("HasBackgroundWork", err)
}
