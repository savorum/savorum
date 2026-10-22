CREATE TABLE assistant_clients (
 id TEXT PRIMARY KEY,
 metadata TEXT NOT NULL CHECK(json_valid(metadata)),
 created_at INTEGER NOT NULL
);
CREATE TABLE assistant_requests (
 id TEXT PRIMARY KEY,
 user_code TEXT NOT NULL UNIQUE,
 request TEXT NOT NULL CHECK(json_valid(request)),
 expires_at INTEGER NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending',
 token_id TEXT REFERENCES personal_tokens(id) ON DELETE CASCADE,
 last_poll INTEGER NOT NULL DEFAULT 0,
 poll_interval INTEGER NOT NULL DEFAULT 5
);
CREATE INDEX assistant_requests_expiry ON assistant_requests(expires_at);
CREATE TABLE assistant_connections (
 token_id TEXT PRIMARY KEY REFERENCES personal_tokens(id) ON DELETE CASCADE,
 client_id TEXT NOT NULL,
 resource TEXT NOT NULL,
 expires_at INTEGER NOT NULL,
 renewable INTEGER NOT NULL
);
CREATE TABLE assistant_refresh_tokens (
 hash TEXT PRIMARY KEY,
 token_id TEXT NOT NULL REFERENCES assistant_connections(token_id) ON DELETE CASCADE,
 used INTEGER NOT NULL DEFAULT 0
);
