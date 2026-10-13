-- The highest sequence of each library's events that pruning has removed. An
-- event stream reader positioned before it has missed history.
CREATE TABLE event_horizons (
    library_id TEXT PRIMARY KEY,
    sequence   INTEGER NOT NULL
);
