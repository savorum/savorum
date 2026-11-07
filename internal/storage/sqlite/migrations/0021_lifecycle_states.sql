-- Use triggers to validate lifecycle states without table rebuilds that cascade deletes.
CREATE TRIGGER assistant_requests_status_insert BEFORE INSERT ON assistant_requests
WHEN new.status NOT IN ('pending','approved','denied','consumed')
BEGIN
    SELECT RAISE(ABORT, 'assistant request status is not a lifecycle state');
END;

CREATE TRIGGER assistant_requests_status_update BEFORE UPDATE OF status ON assistant_requests
WHEN new.status NOT IN ('pending','approved','denied','consumed')
BEGIN
    SELECT RAISE(ABORT, 'assistant request status is not a lifecycle state');
END;

CREATE TRIGGER webhook_deliveries_state_insert BEFORE INSERT ON webhook_deliveries
WHEN new.state NOT IN ('pending','retrying','delivered','failed','cancelled')
BEGIN
    SELECT RAISE(ABORT, 'webhook delivery state is not a lifecycle state');
END;

CREATE TRIGGER webhook_deliveries_state_update BEFORE UPDATE OF state ON webhook_deliveries
WHEN new.state NOT IN ('pending','retrying','delivered','failed','cancelled')
BEGIN
    SELECT RAISE(ABORT, 'webhook delivery state is not a lifecycle state');
END;

CREATE TRIGGER automation_jobs_state_insert BEFORE INSERT ON automation_jobs
WHEN new.state NOT IN ('pending','running','awaiting-review','committing','completed','completed-with-errors','failed','cancelled','interrupted')
BEGIN
    SELECT RAISE(ABORT, 'automation job state is not a lifecycle state');
END;

CREATE TRIGGER automation_jobs_state_update BEFORE UPDATE OF state ON automation_jobs
WHEN new.state NOT IN ('pending','running','awaiting-review','committing','completed','completed-with-errors','failed','cancelled','interrupted')
BEGIN
    SELECT RAISE(ABORT, 'automation job state is not a lifecycle state');
END;
