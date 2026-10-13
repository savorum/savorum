CREATE TABLE identities (
    user_id             TEXT NOT NULL REFERENCES users(id),
    provider            TEXT NOT NULL,
    provider_subject    TEXT NOT NULL,
    password_hash       TEXT NOT NULL DEFAULT '',
    password_changed_at INTEGER NOT NULL DEFAULT 0,
    enabled             INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
    metadata            TEXT NOT NULL DEFAULT '{}',
    id                  TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(provider,provider_subject)
);

CREATE UNIQUE INDEX identities_public_id ON identities(id) WHERE id<>'';

CREATE TRIGGER identities_assign_public_id AFTER INSERT ON identities
WHEN new.id=''
BEGIN
 UPDATE identities SET id=lower(hex(randomblob(16))) WHERE rowid=new.rowid;
END;

CREATE TABLE browser_sessions (
    id                TEXT PRIMARY KEY,
    user_id           TEXT NOT NULL REFERENCES users(id),
    token_hash        TEXT NOT NULL UNIQUE,
    csrf_hash         TEXT NOT NULL,
    created_at        INTEGER NOT NULL,
    last_seen         INTEGER NOT NULL,
    idle_expires      INTEGER NOT NULL,
    absolute_expires  INTEGER NOT NULL,
    idle_seconds      INTEGER NOT NULL,
    provider          TEXT NOT NULL,
    revoked_at        INTEGER,
    revocation_reason TEXT NOT NULL DEFAULT ''
);

CREATE INDEX browser_sessions_user ON browser_sessions(user_id);

CREATE TABLE authentication_backoff (
    key_hash TEXT PRIMARY KEY,
    failures INTEGER NOT NULL,
    retry_at INTEGER NOT NULL
);

CREATE TABLE identity_audit (
    id         INTEGER PRIMARY KEY,
    actor_id   TEXT NOT NULL,
    target_id  TEXT NOT NULL,
    action     TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE audit_events (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at     INTEGER NOT NULL DEFAULT (unixepoch()),
    actor_id       TEXT NOT NULL,
    credential_id  TEXT NOT NULL DEFAULT '',
    action         TEXT NOT NULL,
    target_type    TEXT NOT NULL,
    target_id      TEXT NOT NULL,
    library_id     TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NOT NULL DEFAULT '',
    outcome        TEXT NOT NULL DEFAULT 'succeeded',
    details        TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(details))
);

CREATE INDEX audit_events_library_time ON audit_events(library_id,created_at,id);

CREATE INDEX audit_events_action_time ON audit_events(action,created_at,id);

CREATE INDEX audit_events_actor_time ON audit_events(actor_id,created_at,id);

CREATE TRIGGER audit_events_no_update BEFORE UPDATE ON audit_events
BEGIN SELECT RAISE(ABORT,'audit events cannot be modified'); END;

CREATE TABLE auth_providers (
    id                   TEXT PRIMARY KEY,
    kind                 TEXT NOT NULL,
    display_name         TEXT NOT NULL,
    enabled              INTEGER NOT NULL CHECK(enabled IN (0,1)),
    issuer               TEXT NOT NULL DEFAULT '',
    allow_local_recovery INTEGER NOT NULL DEFAULT 0 CHECK(allow_local_recovery IN (0,1))
);

CREATE TABLE federation_transactions (
    token_hash   TEXT PRIMARY KEY,
    provider     TEXT NOT NULL REFERENCES auth_providers(id),
    browser_hash TEXT NOT NULL,
    payload      TEXT NOT NULL,
    expires_at   INTEGER NOT NULL
);

CREATE INDEX federation_transactions_expiry ON federation_transactions(expires_at);

CREATE TABLE federation_replays (
    provider     TEXT NOT NULL REFERENCES auth_providers(id),
    assertion_id TEXT NOT NULL,
    expires_at   INTEGER NOT NULL,
    PRIMARY KEY(provider,assertion_id)
);

CREATE TABLE passkeys (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users(id),
    credential_json TEXT NOT NULL,
    name            TEXT NOT NULL,
    created_at      INTEGER NOT NULL DEFAULT (unixepoch()),
    last_used_at    INTEGER,
    rp_id           TEXT NOT NULL
);

CREATE INDEX passkeys_user ON passkeys(user_id);

CREATE TABLE webauthn_ceremonies (
    token_hash   TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL,
    session_id   TEXT NOT NULL,
    purpose      TEXT NOT NULL,
    rp_id        TEXT NOT NULL,
    session_json TEXT NOT NULL,
    expires_at   INTEGER NOT NULL
);

CREATE TABLE recovery_codes (
    code_hash  TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id),
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE INDEX recovery_codes_user ON recovery_codes(user_id);

CREATE TABLE credential_rate_limits (
    key_hash   TEXT PRIMARY KEY,
    attempts   INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);

CREATE TABLE invitations (
    id          TEXT PRIMARY KEY,
    library_id  TEXT NOT NULL REFERENCES libraries(id),
    role        TEXT NOT NULL CHECK(role IN ('viewer','editor','owner')),
    hint        TEXT NOT NULL DEFAULT '',
    token_hash  TEXT NOT NULL UNIQUE,
    creator     TEXT NOT NULL REFERENCES users(id),
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    accepted_at INTEGER,
    accepted_by TEXT REFERENCES users(id),
    revoked_at  INTEGER
);

CREATE INDEX invitations_library ON invitations(library_id,created_at);

CREATE TABLE provider_group_mappings (
    id          TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL REFERENCES auth_providers(id),
    group_name  TEXT NOT NULL,
    library_id  TEXT NOT NULL REFERENCES libraries(id),
    role        TEXT NOT NULL CHECK(role IN ('viewer','editor','owner')),
    UNIQUE(provider_id,group_name,library_id)
);

CREATE TABLE personal_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id),
    token_hash   TEXT NOT NULL UNIQUE,
    name         TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    last_used_at INTEGER NOT NULL DEFAULT 0,
    revoked_at   INTEGER NOT NULL DEFAULT 0,
    scopes       TEXT NOT NULL CHECK(json_valid(scopes)),
    libraries    TEXT NOT NULL CHECK(json_valid(libraries))
);

CREATE INDEX personal_tokens_user ON personal_tokens(user_id,created_at);
