CREATE TABLE native_link_tickets (
    ticket_hash TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id),
    session_id  TEXT NOT NULL,
    provider    TEXT NOT NULL REFERENCES auth_providers(id),
    challenge   TEXT NOT NULL,
    expires_at  INTEGER NOT NULL
);

CREATE INDEX native_link_tickets_expiry ON native_link_tickets(expires_at);

CREATE TABLE native_link_grants (
    code_hash    TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id),
    session_id   TEXT NOT NULL,
    provider     TEXT NOT NULL REFERENCES auth_providers(id),
    challenge    TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    identity     TEXT NOT NULL,
    expires_at   INTEGER NOT NULL
);

CREATE INDEX native_link_grants_expiry ON native_link_grants(expires_at);
