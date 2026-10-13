package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/event"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/webhook"
)

const (
	webhookColumns   = `id,library_id,name,url,event_types,enabled,revision,created_at,rotated_at,failures,pause_reason,secret_ref,updated_at,updated_by`
	deliveryColumns  = `id,subscription_id,event_id,event_type,state,created_at,cycle_at,next_attempt_at,attempts,cycle_attempts,last_code,body`
	webhookAuthority = `EXISTS(SELECT 1 FROM users u WHERE u.id=webhook_subscriptions.authority_user AND u.status='active' AND ((webhook_subscriptions.library_id='' AND u.instance_admin=1) OR EXISTS(SELECT 1 FROM library_memberships m WHERE m.user_id=u.id AND m.library_id=webhook_subscriptions.library_id AND m.role='owner')))`
)

type webhookScanner interface{ Scan(...any) error }

func scanWebhook(row webhookScanner) (webhook.Subscription, error) {
	var (
		v                         webhook.Subscription
		types                     string
		created, rotated, updated int64
	)

	err := row.Scan(&v.ID, &v.LibraryID, &v.Name, &v.URL, &types, &v.Enabled, &v.Revision, &created, &rotated, &v.ConsecutiveFailures, &v.PauseReason, &v.SecretRef, &updated, &v.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return v, webhook.ErrNotFound
	}

	if err != nil {
		return v, operationError("scanWebhook", err)
	}

	if err = json.Unmarshal([]byte(types), &v.EventTypes); err != nil {
		return v, operationError("scanWebhook", err)
	}

	v.UpdatedAt = time.Unix(updated, 0).UTC()
	v.CreatedAt = time.Unix(created, 0).UTC()
	v.SecretRotatedAt = time.Unix(rotated, 0).UTC()

	return v, nil
}

func scanDelivery(row webhookScanner) (webhook.Delivery, error) {
	var (
		v              webhook.Delivery
		created, cycle int64
		next           sql.NullInt64
	)

	err := row.Scan(&v.ID, &v.SubscriptionID, &v.EventID, &v.EventType, &v.State, &created, &cycle, &next, &v.Attempts, &v.CycleAttempts, &v.LastCode, &v.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return v, webhook.ErrNotFound
	}

	if err != nil {
		return v, operationError("scanDelivery", err)
	}

	v.CreatedAt = time.Unix(created, 0).UTC()

	v.CycleStartedAt = time.Unix(cycle, 0).UTC()
	if next.Valid {
		at := time.Unix(next.Int64, 0).UTC()
		v.NextAttemptAt = &at
	}

	return v, nil
}

func (s *Store) authorizeWebhook(ctx context.Context, q accessQueryer, scope string) error {
	p, ok := identity.FromContext(ctx)
	if !ok || strings.HasPrefix(p.CredentialID, "pat:") {
		return access.ErrForbidden
	}

	if scope != "" {
		return s.authorizeLibrary(ctx, q, scope, access.Owner)
	}

	var allowed bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND instance_admin=1 AND status='active')`, p.UserID).Scan(&allowed); err != nil {
		return operationError("authorizeWebhook", err)
	}

	if !allowed {
		return access.ErrForbidden
	}

	return nil
}

// WebhookSubscriptions checks owner or instance-administrator authority before listing settings.
func (s *Store) WebhookSubscriptions(ctx context.Context, scope string) ([]webhook.Subscription, error) {
	if err := s.authorizeWebhook(ctx, s.db, scope); err != nil {
		return nil, operationError("WebhookSubscriptions", err)
	}

	rows, err := s.db.QueryContext(ctx, `SELECT `+webhookColumns+` FROM webhook_subscriptions WHERE library_id=? AND deleted_at=0 ORDER BY created_at,id LIMIT 24`, scope)
	if err != nil {
		return nil, operationError("WebhookSubscriptions", err)
	}
	defer rows.Close()

	values := []webhook.Subscription{}

	for rows.Next() {
		v, err := scanWebhook(rows)
		if err != nil {
			return nil, operationError("WebhookSubscriptions", err)
		}

		values = append(values, v)
	}

	return values, operationError("WebhookSubscriptions", rows.Err())
}

// WebhookSubscription reads one subscription within its authorized scope.
func (s *Store) WebhookSubscription(ctx context.Context, scope, id string) (webhook.Subscription, error) {
	if err := s.authorizeWebhook(ctx, s.db, scope); err != nil {
		return webhook.Subscription{}, operationError("WebhookSubscription", err)
	}

	return scanWebhook(s.db.QueryRowContext(ctx, `SELECT `+webhookColumns+` FROM webhook_subscriptions WHERE library_id=? AND id=? AND deleted_at=0`, scope, id))
}

func webhookAudit(ctx context.Context, tx *sql.Tx, scope, id, action string) error {
	p, _ := identity.FromContext(ctx)
	return appendAudit(ctx, tx, identity.AuditEvent{ActorID: p.UserID, LibraryID: scope, TargetType: "webhook", TargetID: id, Action: "webhook." + action})
}

// SaveWebhook commits conditional configuration and its audit record atomically.
func (s *Store) SaveWebhook(ctx context.Context, scope string, v webhook.Subscription, expected uint64, reason string) (webhook.Subscription, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, operationError("SaveWebhook", err)
	}
	defer tx.Rollback()

	if err = s.authorizeWebhook(ctx, tx, scope); err != nil {
		return v, operationError("SaveWebhook", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	v.UpdatedAt = now
	v.UpdatedBy = currentUserID(ctx)

	if expected == 0 {
		if err := recordCreation(ctx, tx, v.ID); err != nil {
			return v, err
		}

		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM webhook_subscriptions WHERE library_id=? AND deleted_at=0`, scope).Scan(&count); err != nil {
			return v, operationError("SaveWebhook", err)
		}

		if count >= 24 {
			return v, webhook.ErrCapacity
		}

		v.CreatedAt = now
		v.SecretRotatedAt = now
		v.Revision = 1

		types, e := json.Marshal(v.EventTypes)
		if e != nil {
			return v, operationError("SaveWebhook", e)
		}

		_, err = tx.ExecContext(ctx, `INSERT INTO webhook_subscriptions(id,library_id,name,url,event_types,enabled,revision,created_at,updated_at,updated_by,rotated_at,secret_ref,created_sequence,authority_user) VALUES(?,?,?,?,?,?,1,?,?,?,?,?,(SELECT COALESCE(MAX(sequence),0) FROM domain_events),?)`, v.ID, scope, v.Name, v.URL, string(types), v.Enabled, now.Unix(), now.Unix(), v.UpdatedBy, now.Unix(), v.SecretRef, v.UpdatedBy)
	} else {
		old, e := scanWebhook(tx.QueryRowContext(ctx, `SELECT `+webhookColumns+` FROM webhook_subscriptions WHERE id=? AND library_id=? AND deleted_at=0`, v.ID, scope))
		if e != nil {
			return v, operationError("SaveWebhook", e)
		}

		if old.Revision != expected {
			return v, &webhook.RevisionConflict{Expected: expected, Current: old.Revision}
		}

		v.CreatedAt = old.CreatedAt
		v.Revision = expected + 1

		v.SecretRotatedAt = old.SecretRotatedAt
		if reason == "rotation" {
			v.SecretRotatedAt = now
		} else {
			v.SecretRef = old.SecretRef
		}

		if v.Enabled && !old.Enabled {
			v.PauseReason = ""
			v.ConsecutiveFailures = 0
		} else {
			v.PauseReason = old.PauseReason
			v.ConsecutiveFailures = old.ConsecutiveFailures
		}

		types, e := json.Marshal(v.EventTypes)
		if e != nil {
			return v, operationError("SaveWebhook", e)
		}

		_, err = tx.ExecContext(ctx, `UPDATE webhook_subscriptions SET name=?,url=?,event_types=?,enabled=?,revision=?,updated_at=?,updated_by=?,rotated_at=?,secret_ref=?,failures=?,pause_reason=?,created_sequence=CASE WHEN enabled=0 AND ?=1 THEN (SELECT COALESCE(MAX(sequence),0) FROM domain_events) ELSE created_sequence END,authority_user=? WHERE id=?`, v.Name, v.URL, string(types), v.Enabled, v.Revision, now.Unix(), v.UpdatedBy, v.SecretRotatedAt.Unix(), v.SecretRef, v.ConsecutiveFailures, v.PauseReason, v.Enabled, v.UpdatedBy, v.ID)
	}

	if err != nil {
		return v, operationError("SaveWebhook", err)
	}

	if err = webhookAudit(ctx, tx, scope, v.ID, reason); err != nil {
		return v, operationError("SaveWebhook", err)
	}

	if err = tx.Commit(); err != nil {
		return v, operationError("SaveWebhook", err)
	}

	return v, nil
}

// DeleteWebhook checks the revision and cancels queued delivery in the deletion transaction.
func (s *Store) DeleteWebhook(ctx context.Context, scope, id string, revision uint64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operationError("DeleteWebhook", err)
	}
	defer tx.Rollback()

	if err = s.authorizeWebhook(ctx, tx, scope); err != nil {
		return operationError("DeleteWebhook", err)
	}

	old, err := scanWebhook(tx.QueryRowContext(ctx, `SELECT `+webhookColumns+` FROM webhook_subscriptions WHERE id=? AND library_id=? AND deleted_at=0`, id, scope))
	if err != nil {
		return operationError("DeleteWebhook", err)
	}

	if old.Revision != revision {
		return &webhook.RevisionConflict{Expected: revision, Current: old.Revision}
	}

	if _, err = tx.ExecContext(ctx, `UPDATE webhook_subscriptions SET enabled=0,deleted_at=unixepoch(),revision=revision+1,secret_ref='' WHERE id=?`, id); err != nil {
		return operationError("DeleteWebhook", err)
	}

	if _, err = tx.ExecContext(ctx, `UPDATE webhook_deliveries SET state='cancelled',next_attempt_at=NULL,last_code='subscription-deleted' WHERE subscription_id=? AND state IN ('pending','retrying')`, id); err != nil {
		return operationError("DeleteWebhook", err)
	}

	if err = webhookAudit(ctx, tx, scope, id, "deleted"); err != nil {
		return operationError("DeleteWebhook", err)
	}

	return operationError("DeleteWebhook", tx.Commit())
}

// WebhookDeliveries returns a bounded page of authorized delivery history.
func (s *Store) WebhookDeliveries(ctx context.Context, scope, id string, offset, limit int) ([]webhook.Delivery, error) {
	if _, err := s.WebhookSubscription(ctx, scope, id); err != nil {
		return nil, operationError("WebhookDeliveries", err)
	}

	if offset < 0 || offset > 1000 || limit < 1 || limit > 100 {
		return nil, webhook.ErrInvalid
	}

	rows, err := s.db.QueryContext(ctx, `SELECT `+deliveryColumns+` FROM webhook_deliveries WHERE subscription_id=? ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?`, id, limit, offset)
	if err != nil {
		return nil, operationError("WebhookDeliveries", err)
	}
	defer rows.Close()

	values := []webhook.Delivery{}

	for rows.Next() {
		v, e := scanDelivery(rows)
		if e != nil {
			return nil, operationError("WebhookDeliveries", e)
		}

		values = append(values, v)
	}

	return values, operationError("WebhookDeliveries", rows.Err())
}

// WebhookDelivery returns one delivery and its bounded attempt records.
func (s *Store) WebhookDelivery(ctx context.Context, scope, id, deliveryID string) (webhook.Detail, error) {
	var detail webhook.Detail
	if _, err := s.WebhookSubscription(ctx, scope, id); err != nil {
		return detail, operationError("WebhookDelivery", err)
	}

	delivery, err := scanDelivery(s.db.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM webhook_deliveries WHERE subscription_id=? AND id=?`, id, deliveryID))
	if err != nil {
		return detail, operationError("WebhookDelivery", err)
	}

	detail.Delivery = delivery
	detail.Attempts = []webhook.Attempt{}

	rows, err := s.db.QueryContext(ctx, `SELECT id,attempted_at,status,code,duration_ms,excerpt FROM webhook_attempts WHERE delivery_id=? ORDER BY attempted_at,id LIMIT 32`, deliveryID)
	if err != nil {
		return detail, operationError("WebhookDelivery", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			a  webhook.Attempt
			at int64
		)
		if err := rows.Scan(&a.ID, &at, &a.Status, &a.Code, &a.DurationMS, &a.Excerpt); err != nil {
			return detail, operationError("WebhookDelivery", err)
		}

		a.At = time.Unix(at, 0).UTC()
		detail.Attempts = append(detail.Attempts, a)
	}

	return detail, operationError("WebhookDelivery", rows.Err())
}

// RetryWebhookDelivery preserves immutable IDs and body while starting a bounded retry cycle.
func (s *Store) RetryWebhookDelivery(ctx context.Context, scope, id, deliveryID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operationError("RetryWebhookDelivery", err)
	}
	defer tx.Rollback()

	if err = s.authorizeWebhook(ctx, tx, scope); err != nil {
		return operationError("RetryWebhookDelivery", err)
	}

	v, err := scanWebhook(tx.QueryRowContext(ctx, `SELECT `+webhookColumns+` FROM webhook_subscriptions WHERE id=? AND library_id=? AND deleted_at=0`, id, scope))
	if err != nil {
		return operationError("RetryWebhookDelivery", err)
	}

	if !v.Enabled {
		return webhook.ErrState
	}

	result, err := tx.ExecContext(ctx, `UPDATE webhook_deliveries SET state='pending',cycle_attempts=0,cycle_at=unixepoch(),next_attempt_at=unixepoch(),last_code='' WHERE id=? AND subscription_id=? AND state IN ('failed','delivered') AND attempts<32`, deliveryID, id)
	if err != nil {
		return operationError("RetryWebhookDelivery", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return operationError("RetryWebhookDelivery", err)
	}

	if n != 1 {
		return webhook.ErrState
	}

	if err = webhookAudit(ctx, tx, scope, id, "redelivery-requested"); err != nil {
		return operationError("RetryWebhookDelivery", err)
	}

	return operationError("RetryWebhookDelivery", tx.Commit())
}

func insertDelivery(ctx context.Context, tx *sql.Tx, subscriptionID string, fact event.Envelope) (webhook.Delivery, error) {
	now := time.Now().UTC().Truncate(time.Second)
	v := webhook.Delivery{ID: "delivery_" + rand.Text(), SubscriptionID: subscriptionID, EventID: fact.ID, EventType: fact.Type, State: "pending", CreatedAt: now, CycleStartedAt: now, NextAttemptAt: &now}

	body, err := webhook.DeliveryBody(v.ID, fact)
	if err != nil {
		return v, operationError("insertDelivery", err)
	}

	v.Body = body
	_, err = tx.ExecContext(ctx, `INSERT INTO webhook_deliveries(id,subscription_id,event_id,event_type,state,created_at,cycle_at,next_attempt_at,body) VALUES(?,?,?,?,'pending',?,?,?,?) ON CONFLICT(subscription_id,event_id) DO NOTHING`, v.ID, subscriptionID, fact.ID, fact.Type, now.Unix(), now.Unix(), now.Unix(), body)

	return v, operationError("insertDelivery", err)
}

// CreateWebhookTest commits its test fact, targeted delivery, and audit record together.
func (s *Store) CreateWebhookTest(ctx context.Context, scope, id string, fact event.Envelope) (webhook.Delivery, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return webhook.Delivery{}, operationError("CreateWebhookTest", err)
	}
	defer tx.Rollback()

	if err = s.authorizeWebhook(ctx, tx, scope); err != nil {
		return webhook.Delivery{}, operationError("CreateWebhookTest", err)
	}

	v, err := scanWebhook(tx.QueryRowContext(ctx, `SELECT `+webhookColumns+` FROM webhook_subscriptions WHERE id=? AND library_id=? AND deleted_at=0`, id, scope))
	if err != nil {
		return webhook.Delivery{}, operationError("CreateWebhookTest", err)
	}

	if !v.Enabled {
		return webhook.Delivery{}, webhook.ErrState
	}

	var queued int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM webhook_deliveries WHERE subscription_id=? AND state IN ('pending','retrying')`, id).Scan(&queued); err != nil {
		return webhook.Delivery{}, operationError("count queued tests", err)
	}

	if queued >= 1000 {
		return webhook.Delivery{}, webhook.ErrCapacity
	}

	var recent int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM webhook_deliveries WHERE subscription_id=? AND created_at>unixepoch()-60 AND event_type='webhook.test'`, id).Scan(&recent); err != nil {
		return webhook.Delivery{}, operationError("CreateWebhookTest", err)
	}

	if recent >= 5 {
		return webhook.Delivery{}, webhook.ErrCapacity
	}

	data, err := json.Marshal(fact)
	if err != nil {
		return webhook.Delivery{}, operationError("CreateWebhookTest", err)
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO domain_events(id,event_type,library_id,occurred_at,envelope_json) VALUES(?,?,?,?,?)`, fact.ID, fact.Type, scope, fact.OccurredAt.Unix(), string(data)); err != nil {
		return webhook.Delivery{}, operationError("CreateWebhookTest", err)
	}

	delivery, err := insertDelivery(ctx, tx, id, fact)
	if err != nil {
		return delivery, operationError("CreateWebhookTest", err)
	}

	if err = webhookAudit(ctx, tx, scope, id, "test-requested"); err != nil {
		return delivery, operationError("CreateWebhookTest", err)
	}

	return delivery, operationError("CreateWebhookTest", tx.Commit())
}

// EnqueueWebhookEvents advances its outbox cursor in the same transaction as
// unique delivery creation. A crash repeats either both or neither operation.
func (s *Store) EnqueueWebhookEvents(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operationError("EnqueueWebhookEvents", err)
	}
	defer tx.Rollback()

	var cursor int64
	if err = tx.QueryRowContext(ctx, `SELECT sequence FROM event_consumers WHERE id='webhooks'`).Scan(&cursor); err != nil {
		return operationError("EnqueueWebhookEvents", err)
	}

	facts, err := pendingWebhookEvents(ctx, tx, cursor)
	if err != nil {
		return err
	}

	for _, v := range facts {
		cursor = v.sequence
		if v.fact.Type == "webhook.test" {
			continue
		}

		if err := enqueueWebhookFact(ctx, tx, v); err != nil {
			return err
		}
	}

	if _, err = tx.ExecContext(ctx, `UPDATE event_consumers SET sequence=? WHERE id='webhooks'`, cursor); err != nil {
		return operationError("EnqueueWebhookEvents", err)
	}
	// Unconsumed outbox facts are never pruned. SSE replay is independently
	// limited to seven days, while the dispatcher can recover a longer outage.
	const prunable = `sequence<=(SELECT MIN(sequence) FROM event_consumers) AND (occurred_at<unixepoch()-604800 OR sequence<(SELECT COALESCE(MAX(sequence),0)-100000 FROM domain_events))`
	if _, err = tx.ExecContext(ctx, `INSERT INTO event_horizons(library_id,sequence) SELECT library_id,MAX(sequence) FROM domain_events WHERE `+prunable+` GROUP BY library_id ON CONFLICT(library_id) DO UPDATE SET sequence=MAX(sequence,excluded.sequence)`); err != nil {
		return operationError("EnqueueWebhookEvents", err)
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM domain_events WHERE `+prunable); err != nil {
		return operationError("EnqueueWebhookEvents", err)
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM webhook_deliveries WHERE state NOT IN ('pending','retrying') AND (created_at<unixepoch()-2592000 OR id IN (SELECT id FROM (SELECT id,ROW_NUMBER() OVER(PARTITION BY subscription_id ORDER BY created_at DESC,id DESC) AS rank FROM webhook_deliveries WHERE state NOT IN ('pending','retrying')) WHERE rank>1000))`); err != nil {
		return operationError("EnqueueWebhookEvents", err)
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM webhook_subscriptions WHERE deleted_at>0 AND deleted_at<unixepoch()-2592000`); err != nil {
		return operationError("EnqueueWebhookEvents", err)
	}

	return operationError("EnqueueWebhookEvents", tx.Commit())
}

// DueWebhookDeliveries is a bounded worker read excluding paused and deleted scopes.
func (s *Store) DueWebhookDeliveries(ctx context.Context) ([]webhook.Delivery, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+deliveryColumns+` FROM webhook_deliveries WHERE state IN ('pending','retrying') AND next_attempt_at<=unixepoch() AND subscription_id IN(SELECT id FROM webhook_subscriptions WHERE enabled=1 AND deleted_at=0 AND `+webhookAuthority+` AND (library_id='' OR EXISTS(SELECT 1 FROM libraries WHERE libraries.id=webhook_subscriptions.library_id AND deleted_at IS NULL))) ORDER BY next_attempt_at,id LIMIT 8`)
	if err != nil {
		return nil, operationError("DueWebhookDeliveries", err)
	}
	defer rows.Close()

	values := []webhook.Delivery{}

	for rows.Next() {
		v, err := scanDelivery(rows)
		if err != nil {
			return nil, operationError("DueWebhookDeliveries", err)
		}

		values = append(values, v)
	}

	return values, operationError("DueWebhookDeliveries", rows.Err())
}

// DeliveryWebhook rechecks active subscription and library existence immediately before sending.
func (s *Store) DeliveryWebhook(ctx context.Context, id string) (webhook.Subscription, error) {
	return scanWebhook(s.db.QueryRowContext(ctx, `SELECT `+webhookColumns+` FROM webhook_subscriptions WHERE id=? AND deleted_at=0 AND `+webhookAuthority+` AND (library_id='' OR EXISTS(SELECT 1 FROM libraries WHERE libraries.id=webhook_subscriptions.library_id AND deleted_at IS NULL))`, id))
}

// FinishWebhookAttempt atomically records an attempt, schedules retry, and applies failure controls.
func (s *Store) FinishWebhookAttempt(ctx context.Context, delivery webhook.Delivery, attempt webhook.Attempt, state string, next *time.Time, failureLimit int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operationError("FinishWebhookAttempt", err)
	}
	defer tx.Rollback()

	var nextAt any
	if next != nil {
		nextAt = next.Unix()
	}

	result, err := tx.ExecContext(ctx, `UPDATE webhook_deliveries SET state=?,next_attempt_at=?,attempts=attempts+1,cycle_attempts=cycle_attempts+1,last_code=? WHERE id=? AND attempts=? AND state IN ('pending','retrying')`, state, nextAt, attempt.Code, delivery.ID, delivery.Attempts)
	if err != nil {
		return operationError("FinishWebhookAttempt", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return operationError("FinishWebhookAttempt", err)
	}

	if n != 1 {
		return webhook.ErrState
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO webhook_attempts(id,delivery_id,attempted_at,status,code,duration_ms,excerpt) VALUES(?,?,?,?,?,?,?)`, attempt.ID, delivery.ID, attempt.At.Unix(), attempt.Status, attempt.Code, attempt.DurationMS, attempt.Excerpt); err != nil {
		return operationError("FinishWebhookAttempt", err)
	}

	switch state {
	case "failed":
		if _, err = tx.ExecContext(ctx, `UPDATE webhook_subscriptions SET failures=failures+1,revision=revision+1,updated_at=unixepoch(),updated_by='' WHERE id=?`, delivery.SubscriptionID); err != nil {
			return operationError("FinishWebhookAttempt", err)
		}

		var (
			failures int
			scope    string
		)
		if err = tx.QueryRowContext(ctx, `SELECT failures,library_id FROM webhook_subscriptions WHERE id=?`, delivery.SubscriptionID).Scan(&failures, &scope); err != nil {
			return operationError("FinishWebhookAttempt", err)
		}

		if failures >= failureLimit {
			if _, err = tx.ExecContext(ctx, `UPDATE webhook_subscriptions SET enabled=0,pause_reason='repeated-failures' WHERE id=?`, delivery.SubscriptionID); err != nil {
				return operationError("FinishWebhookAttempt", err)
			}

			if err = webhookAudit(ctx, tx, scope, delivery.SubscriptionID, "automatically-paused"); err != nil {
				return operationError("FinishWebhookAttempt", err)
			}
		}
	case "delivered":
		if _, err = tx.ExecContext(ctx, `UPDATE webhook_subscriptions SET failures=0,revision=revision+CASE WHEN failures>0 THEN 1 ELSE 0 END WHERE id=?`, delivery.SubscriptionID); err != nil {
			return operationError("FinishWebhookAttempt", err)
		}
	}

	return operationError("FinishWebhookAttempt", tx.Commit())
}

type pendingWebhookEvent struct {
	sequence int64
	fact     event.Envelope
}

func pendingWebhookEvents(ctx context.Context, tx *sql.Tx, cursor int64) ([]pendingWebhookEvent, error) {
	rows, err := tx.QueryContext(ctx, `SELECT sequence,envelope_json FROM domain_events WHERE sequence>? ORDER BY sequence LIMIT 100`, cursor)
	if err != nil {
		return nil, operationError("EnqueueWebhookEvents", err)
	}

	defer rows.Close()

	facts := []pendingWebhookEvent{}

	for rows.Next() {
		var (
			v    pendingWebhookEvent
			data string
		)
		if err = rows.Scan(&v.sequence, &data); err != nil {
			_ = rows.Close()
			return nil, operationError("EnqueueWebhookEvents", err)
		}

		if err = json.Unmarshal([]byte(data), &v.fact); err != nil {
			_ = rows.Close()
			return nil, operationError("EnqueueWebhookEvents", err)
		}

		facts = append(facts, v)
	}

	err = rows.Err()
	_ = rows.Close()

	if err != nil {
		return nil, operationError("EnqueueWebhookEvents", err)
	}

	return facts, nil
}

func enqueueWebhookFact(ctx context.Context, tx *sql.Tx, v pendingWebhookEvent) error {
	scope := v.fact.LibraryID
	if event.ReadCapability(v.fact.Type) == "instance:admin" {
		scope = ""
	}

	ids, err := webhookSubscribers(ctx, tx, scope, v)
	if err != nil {
		return err
	}

	for _, id := range ids {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM webhook_deliveries WHERE subscription_id=? AND state IN ('pending','retrying')`, id).Scan(&count); err != nil {
			return operationError("EnqueueWebhookEvents", err)
		}

		delivery, err := insertDelivery(ctx, tx, id, v.fact)
		if err != nil {
			return operationError("EnqueueWebhookEvents", err)
		}

		if count >= 1000 {
			if _, err = tx.ExecContext(ctx, `UPDATE webhook_deliveries SET state='failed',last_code='queue-capacity',next_attempt_at=NULL WHERE id=?`, delivery.ID); err != nil {
				return operationError("EnqueueWebhookEvents", err)
			}

			if _, err = tx.ExecContext(ctx, `UPDATE webhook_subscriptions SET enabled=0,pause_reason='queue-capacity',revision=revision+1,updated_at=unixepoch(),updated_by='' WHERE id=?`, id); err != nil {
				return operationError("EnqueueWebhookEvents", err)
			}

			if err = webhookAudit(ctx, tx, scope, id, "automatically-paused"); err != nil {
				return operationError("EnqueueWebhookEvents", err)
			}
		}
	}

	return nil
}

func webhookSubscribers(ctx context.Context, tx *sql.Tx, scope string, v pendingWebhookEvent) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,event_types FROM webhook_subscriptions WHERE library_id=? AND enabled=1 AND deleted_at=0 AND created_sequence<? AND `+webhookAuthority+` AND (library_id='' OR EXISTS(SELECT 1 FROM libraries WHERE libraries.id=webhook_subscriptions.library_id AND deleted_at IS NULL))`, scope, v.sequence)
	if err != nil {
		return nil, operationError("EnqueueWebhookEvents", err)
	}

	defer rows.Close()

	ids := []string{}

	for rows.Next() {
		var id, raw string
		if err = rows.Scan(&id, &raw); err != nil {
			return nil, operationError("EnqueueWebhookEvents", err)
		}

		var types []string
		if err = json.Unmarshal([]byte(raw), &types); err != nil {
			return nil, operationError("EnqueueWebhookEvents", err)
		}

		if slices.Contains(types, v.fact.Type) {
			ids = append(ids, id)
		}
	}

	err = rows.Err()
	if err != nil {
		return nil, operationError("EnqueueWebhookEvents", err)
	}

	return ids, nil
}
