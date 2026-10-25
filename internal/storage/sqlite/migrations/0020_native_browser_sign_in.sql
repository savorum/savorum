CREATE TABLE native_sign_in_grants_next (
    code_hash TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    provider TEXT REFERENCES auth_providers(id),
    challenge TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    expires_at INTEGER NOT NULL,
    identity_id TEXT NOT NULL DEFAULT '',
    source_session_id TEXT REFERENCES browser_sessions(id) ON DELETE CASCADE,
    CHECK ((provider IS NULL) <> (source_session_id IS NULL)),
    CHECK (source_session_id IS NULL OR identity_id = '')
);
INSERT INTO native_sign_in_grants_next (code_hash,user_id,provider,challenge,redirect_uri,expires_at,identity_id)
SELECT code_hash,user_id,provider,challenge,redirect_uri,expires_at,identity_id FROM native_sign_in_grants;
DROP TABLE native_sign_in_grants;
ALTER TABLE native_sign_in_grants_next RENAME TO native_sign_in_grants;
CREATE INDEX native_sign_in_grants_expiry ON native_sign_in_grants(expires_at);

CREATE TABLE federation_transactions_next (
    token_hash TEXT PRIMARY KEY,
    provider TEXT REFERENCES auth_providers(id),
    browser_hash TEXT NOT NULL,
    payload TEXT NOT NULL,
    expires_at INTEGER NOT NULL
);
INSERT INTO federation_transactions_next SELECT * FROM federation_transactions;
DROP TABLE federation_transactions;
ALTER TABLE federation_transactions_next RENAME TO federation_transactions;
CREATE INDEX federation_transactions_expiry ON federation_transactions(expires_at);

ALTER TABLE browser_sessions ADD COLUMN browser_handoff INTEGER NOT NULL DEFAULT 0 CHECK (browser_handoff IN (0,1));
