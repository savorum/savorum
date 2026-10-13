CREATE TABLE hosting_erasures (
 user_id TEXT PRIMARY KEY REFERENCES users(id),
 request TEXT NOT NULL,
 erased_at INTEGER NOT NULL
);
