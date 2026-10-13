CREATE TABLE import_jobs (
    id                        TEXT PRIMARY KEY,
    importer                  TEXT NOT NULL,
    source                    TEXT NOT NULL,
    state                     TEXT NOT NULL CHECK (state IN ( 'pending', 'running', 'completed', 'completed-with-errors', 'cancelled', 'failed', 'interrupted')),
    duplicate_policy          TEXT NOT NULL CHECK (duplicate_policy IN ('reject', 'skip', 'replace')),
    total_items               INTEGER NOT NULL CHECK (total_items >= 0),
    error                     TEXT,
    created_at                TEXT NOT NULL,
    started_at                TEXT,
    finished_at               TEXT,
    cancellation_requested_at TEXT,
    batch_size                INTEGER NOT NULL DEFAULT 25 CHECK (batch_size > 0),
    resolution_decisions_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(resolution_decisions_json)),
    library_id                TEXT NOT NULL DEFAULT 'default' REFERENCES libraries(id),
    actor_id                  TEXT NOT NULL DEFAULT '',
    credential_id             TEXT NOT NULL DEFAULT ''
);

CREATE INDEX import_jobs_created_idx
    ON import_jobs (created_at DESC, id);

CREATE INDEX import_jobs_state_idx
    ON import_jobs (state, created_at DESC, id);

CREATE INDEX import_jobs_library_idx ON import_jobs(library_id,id);

CREATE TABLE import_job_items (
    job_id           TEXT NOT NULL,
    position         INTEGER NOT NULL CHECK (position >= 0),
    source_item_id   TEXT NOT NULL,
    title            TEXT NOT NULL,
    filename         TEXT NOT NULL,
    source_url       TEXT NOT NULL,
    state            TEXT NOT NULL CHECK (state IN ( 'pending', 'running', 'imported', 'skipped', 'failed', 'cancelled')),
    review_required  INTEGER NOT NULL CHECK (review_required IN (0, 1)),
    outcome          TEXT CHECK (outcome IS NULL OR outcome IN ('created', 'replaced', 'skipped')),
    entry_id         TEXT,
    recipe_id        TEXT,
    revision         INTEGER CHECK (revision IS NULL OR revision > 0),
    dropped_assets   INTEGER NOT NULL CHECK (dropped_assets >= 0),
    deferred_assets  INTEGER NOT NULL CHECK (deferred_assets >= 0),
    diagnostics_json TEXT NOT NULL CHECK (json_valid(diagnostics_json)),
    error            TEXT,
    payload_json     TEXT CHECK (payload_json IS NULL OR json_valid(payload_json)),
    started_at       TEXT,
    finished_at      TEXT,
    PRIMARY KEY (job_id, position),
    FOREIGN KEY (job_id) REFERENCES import_jobs (id) ON DELETE CASCADE
);

CREATE INDEX import_job_items_state_idx
    ON import_job_items (job_id, state, position);

CREATE INDEX import_job_items_review_idx
    ON import_job_items (job_id, review_required, position);

CREATE TABLE import_events (
    id         TEXT PRIMARY KEY,
    entry_id   TEXT NOT NULL,
    revision   INTEGER NOT NULL,
    event_json TEXT NOT NULL CHECK (json_valid(event_json)),
    actor_id   TEXT NOT NULL DEFAULT '',
    UNIQUE(entry_id, revision),
    FOREIGN KEY (entry_id, revision) REFERENCES recipe_revisions(entry_id, revision) ON DELETE CASCADE
);

CREATE TRIGGER import_events_immutable BEFORE UPDATE ON import_events
BEGIN SELECT RAISE(ABORT, 'import events are immutable'); END;

CREATE TABLE recipe_source_attachments (
    entry_id   TEXT NOT NULL,
    revision   INTEGER NOT NULL CHECK (revision > 0),
    asset_id   TEXT NOT NULL,
    filename   TEXT NOT NULL CHECK (length(trim(filename)) > 0),
    media_type TEXT NOT NULL CHECK (length(trim(media_type)) > 0),
    size       INTEGER NOT NULL CHECK (size >= 0),
    PRIMARY KEY (entry_id, revision, asset_id),
    FOREIGN KEY (entry_id, revision) REFERENCES recipe_revisions (entry_id, revision) ON DELETE CASCADE,
    FOREIGN KEY (asset_id) REFERENCES assets (id)
);

CREATE INDEX recipe_source_attachments_asset_idx
    ON recipe_source_attachments (asset_id, entry_id, revision);

CREATE INDEX recipe_source_attachments_entry_idx
    ON recipe_source_attachments (entry_id, revision DESC);

CREATE TABLE recipe_source_import_baselines (
    entry_id          TEXT PRIMARY KEY,
    baseline_revision INTEGER NOT NULL CHECK (baseline_revision > 0),
    source_json       TEXT NOT NULL CHECK (json_valid(source_json)),
    source_sha256     TEXT NOT NULL CHECK (length(source_sha256) = 64),
    updated_at        TEXT NOT NULL,
    FOREIGN KEY (entry_id) REFERENCES library_recipes (id) ON DELETE CASCADE
);

CREATE TABLE recipe_source_import_baseline_assets (
    entry_id          TEXT NOT NULL,
    baseline_revision INTEGER NOT NULL CHECK (baseline_revision > 0),
    asset_id          TEXT NOT NULL,
    PRIMARY KEY (entry_id, asset_id),
    FOREIGN KEY (entry_id) REFERENCES library_recipes (id) ON DELETE CASCADE,
    FOREIGN KEY (asset_id) REFERENCES assets (id)
);

CREATE INDEX recipe_source_import_baseline_assets_asset_idx
    ON recipe_source_import_baseline_assets (asset_id, entry_id);
