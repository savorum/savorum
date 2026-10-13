CREATE TABLE hosting_tenant (
    singleton          INTEGER PRIMARY KEY CHECK (singleton = 1),
    operation_id       TEXT NOT NULL,
    tenant_id          TEXT NOT NULL,
    public_url         TEXT NOT NULL,
    provider_id        TEXT NOT NULL REFERENCES auth_providers(id),
    issuer             TEXT NOT NULL,
    owner_subject      TEXT NOT NULL,
    owner_display_name TEXT NOT NULL,
    library_name       TEXT NOT NULL,
    owner_user_id      TEXT NOT NULL REFERENCES users(id),
    library_id         TEXT NOT NULL REFERENCES libraries(id),
    created_at         INTEGER NOT NULL
);

CREATE TABLE hosting_member_operations (
    operation_id TEXT PRIMARY KEY,
    request_hash TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id),
    library_id TEXT NOT NULL REFERENCES libraries(id)
);
