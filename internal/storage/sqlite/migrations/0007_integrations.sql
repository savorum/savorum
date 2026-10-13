CREATE TABLE domain_events (
    sequence      INTEGER PRIMARY KEY AUTOINCREMENT,
    id            TEXT NOT NULL UNIQUE,
    event_type    TEXT NOT NULL,
    library_id    TEXT NOT NULL DEFAULT '',
    occurred_at   INTEGER NOT NULL,
    envelope_json TEXT NOT NULL
);

CREATE INDEX domain_events_scope ON domain_events(library_id, sequence);

CREATE TABLE event_consumers (
    id       TEXT PRIMARY KEY,
    sequence INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE api_idempotency (
    library_id  TEXT NOT NULL,
    actor_id    TEXT NOT NULL,
    operation   TEXT NOT NULL,
    key_hash    TEXT NOT NULL,
    digest      TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    expires_at  INTEGER NOT NULL,
    PRIMARY KEY (library_id,actor_id,operation,key_hash)
);

CREATE INDEX api_idempotency_expiry ON api_idempotency(expires_at);

CREATE TABLE automation_jobs (
    id            TEXT PRIMARY KEY,
    library_id    TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    actor_id      TEXT NOT NULL REFERENCES users(id),
    credential_id TEXT NOT NULL DEFAULT '',
    kind          TEXT NOT NULL,
    state         TEXT NOT NULL,
    revision      INTEGER NOT NULL,
    created_at    INTEGER NOT NULL,
    expires_at    INTEGER NOT NULL,
    record_json   TEXT NOT NULL
);

CREATE INDEX automation_jobs_work ON automation_jobs(state,created_at);

CREATE INDEX automation_jobs_scope ON automation_jobs(library_id,actor_id,created_at);

CREATE TABLE automation_results (
    job_id      TEXT NOT NULL REFERENCES automation_jobs(id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    result_json TEXT NOT NULL,
    PRIMARY KEY(job_id,position)
);

CREATE TABLE webhook_subscriptions (
    id               TEXT PRIMARY KEY,
    library_id       TEXT NOT NULL,
    name             TEXT NOT NULL,
    url              TEXT NOT NULL,
    event_types      TEXT NOT NULL,
    enabled          INTEGER NOT NULL,
    revision         INTEGER NOT NULL,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    updated_by       TEXT NOT NULL,
    rotated_at       INTEGER NOT NULL,
    failures         INTEGER NOT NULL DEFAULT 0,
    pause_reason     TEXT NOT NULL DEFAULT '',
    secret_ref       TEXT NOT NULL,
    created_sequence INTEGER NOT NULL,
    deleted_at       INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX webhook_subscriptions_scope ON webhook_subscriptions(library_id,deleted_at);

CREATE TABLE webhook_deliveries (
    id              TEXT PRIMARY KEY,
    subscription_id TEXT NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    event_id        TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    state           TEXT NOT NULL,
    created_at      INTEGER NOT NULL,
    cycle_at        INTEGER NOT NULL,
    next_attempt_at INTEGER,
    attempts        INTEGER NOT NULL DEFAULT 0,
    cycle_attempts  INTEGER NOT NULL DEFAULT 0,
    last_code       TEXT NOT NULL DEFAULT '',
    body            BLOB NOT NULL,
    UNIQUE(subscription_id,event_id)
);

CREATE INDEX webhook_deliveries_due ON webhook_deliveries(state,next_attempt_at);

CREATE INDEX webhook_deliveries_history ON webhook_deliveries(subscription_id,created_at DESC);

CREATE TABLE webhook_attempts (
    id           TEXT PRIMARY KEY,
    delivery_id  TEXT NOT NULL REFERENCES webhook_deliveries(id) ON DELETE CASCADE,
    attempted_at INTEGER NOT NULL,
    status       INTEGER NOT NULL,
    code         TEXT NOT NULL,
    duration_ms  INTEGER NOT NULL,
    excerpt      TEXT NOT NULL
);

CREATE INDEX webhook_attempts_delivery ON webhook_attempts(delivery_id,attempted_at);

INSERT INTO event_consumers (id, sequence) VALUES ('webhooks', 0);
