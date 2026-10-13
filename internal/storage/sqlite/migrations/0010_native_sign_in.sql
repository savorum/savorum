CREATE TABLE native_sign_in_grants (
    code_hash    TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id),
    provider     TEXT NOT NULL REFERENCES auth_providers(id),
    challenge    TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    expires_at   INTEGER NOT NULL
);

CREATE INDEX native_sign_in_grants_expiry ON native_sign_in_grants(expires_at);
