CREATE TABLE recipe_revisions (
    entry_id                TEXT NOT NULL,
    revision                INTEGER NOT NULL CHECK (revision > 0),
    document_json           TEXT NOT NULL CHECK (json_valid(document_json)),
    document_schema_version INTEGER NOT NULL CHECK (document_schema_version > 0),
    state                   TEXT NOT NULL CHECK (state IN ('active', 'trash')),
    cause                   TEXT NOT NULL CHECK (cause IN ('api', 'manual_edit', 'import', 'restore', 'trash')),
    source_revision         INTEGER CHECK (source_revision > 0),
    created_at              TEXT NOT NULL,
    actor_id                TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (entry_id, revision),
    FOREIGN KEY (entry_id) REFERENCES library_recipes (id) ON DELETE CASCADE,
    FOREIGN KEY (entry_id, source_revision) REFERENCES recipe_revisions (entry_id, revision)
);

CREATE INDEX recipe_revisions_created_idx
    ON recipe_revisions (entry_id, created_at DESC, revision DESC);

CREATE TRIGGER recipe_revisions_reject_retained_delete
BEFORE DELETE ON recipe_revisions
WHEN EXISTS (SELECT 1 FROM library_recipes WHERE library_recipes.id = old.entry_id)
BEGIN
    SELECT RAISE(ABORT, 'retained recipe revisions cannot be deleted');
END;

CREATE TRIGGER recipe_revisions_require_document_identity
BEFORE INSERT ON recipe_revisions
WHEN NOT EXISTS (
    SELECT 1 FROM library_recipes
    WHERE library_recipes.id = new.entry_id
      AND library_recipes.document_id = json_extract(new.document_json, '$.id')
)
BEGIN
    SELECT RAISE(ABORT, 'recipe revision document identity does not match library entry');
END;

CREATE TRIGGER recipe_revisions_require_head_equivalence
BEFORE INSERT ON recipe_revisions
WHEN NOT EXISTS (
    SELECT 1 FROM library_recipes
    WHERE library_recipes.id = new.entry_id
      AND library_recipes.revision IS new.revision
      AND library_recipes.document_json IS new.document_json
      AND library_recipes.document_schema_version IS new.document_schema_version
      AND (CASE WHEN library_recipes.deleted_at IS NULL THEN 'active' ELSE 'trash' END) IS new.state
)
BEGIN
    SELECT RAISE(ABORT, 'a recipe revision must record the current recipe it creates');
END;

CREATE TRIGGER recipe_revisions_reject_update BEFORE UPDATE ON recipe_revisions
BEGIN SELECT RAISE(ABORT, 'recipe revisions are immutable'); END;

CREATE TABLE assets (
    id           TEXT PRIMARY KEY,
    content_hash TEXT NOT NULL UNIQUE CHECK (length(content_hash) = 64),
    media_type   TEXT NOT NULL,
    size         INTEGER NOT NULL CHECK (size >= 0),
    storage_key  TEXT,
    created_at   TEXT NOT NULL,
    orphaned_at  TEXT,
    phash        INTEGER,
    CHECK (id = 'sha256:' || content_hash)
);

CREATE INDEX assets_orphaned_idx
    ON assets (orphaned_at, id)
    WHERE orphaned_at IS NOT NULL;

CREATE TRIGGER assets_reject_identity_update
BEFORE UPDATE OF id, content_hash, media_type, size, created_at ON assets
BEGIN
    SELECT RAISE(ABORT, 'asset identity metadata is immutable');
END;

CREATE TABLE recipe_revision_assets (
    entry_id       TEXT NOT NULL,
    revision       INTEGER NOT NULL,
    asset_id       TEXT NOT NULL,
    local_asset_id TEXT NOT NULL,
    role           TEXT,
    path           TEXT NOT NULL,
    alt            TEXT,
    caption        TEXT,
    is_primary     INTEGER NOT NULL CHECK (is_primary IN (0, 1)),
    PRIMARY KEY (entry_id, revision, local_asset_id),
    FOREIGN KEY (entry_id, revision) REFERENCES recipe_revisions (entry_id, revision) ON DELETE CASCADE,
    FOREIGN KEY (asset_id) REFERENCES assets (id)
);

CREATE INDEX recipe_revision_assets_asset_idx
    ON recipe_revision_assets (asset_id, entry_id, revision);

CREATE TRIGGER recipe_revision_assets_reject_update
BEFORE UPDATE ON recipe_revision_assets
BEGIN
    SELECT RAISE(ABORT, 'recipe revision asset references are immutable');
END;

CREATE TRIGGER recipe_revision_assets_reject_retained_delete
BEFORE DELETE ON recipe_revision_assets
WHEN EXISTS (
    SELECT 1 FROM recipe_revisions
    WHERE recipe_revisions.entry_id = old.entry_id
      AND recipe_revisions.revision = old.revision
)
BEGIN
    SELECT RAISE(ABORT, 'retained recipe revision asset references cannot be deleted');
END;

CREATE TABLE recipe_organization_state (
    entry_id   TEXT PRIMARY KEY,
    revision   INTEGER NOT NULL CHECK (revision > 0),
    updated_at TEXT NOT NULL,
    FOREIGN KEY (entry_id) REFERENCES library_recipes (id) ON DELETE CASCADE
);

CREATE TABLE recipe_facets (
    entry_id TEXT NOT NULL,
    facet_id TEXT NOT NULL,
    PRIMARY KEY (entry_id, facet_id),
    FOREIGN KEY (entry_id) REFERENCES library_recipes (id) ON DELETE CASCADE,
    FOREIGN KEY (facet_id) REFERENCES facets (id)
);

CREATE INDEX recipe_facets_facet_idx ON recipe_facets (facet_id, entry_id);

CREATE TRIGGER recipe_facets_library_insert BEFORE INSERT ON recipe_facets
WHEN new.facet_id IS NOT NULL AND
 (SELECT library_id FROM library_recipes WHERE id=new.entry_id) <>
 (SELECT library_id FROM facets WHERE id=new.facet_id)
BEGIN SELECT RAISE(ABORT,'cross-library reference'); END;

CREATE TRIGGER recipe_facets_library_update BEFORE UPDATE ON recipe_facets
WHEN new.facet_id IS NOT NULL AND
 (SELECT library_id FROM library_recipes WHERE id=new.entry_id) <>
 (SELECT library_id FROM facets WHERE id=new.facet_id)
BEGIN SELECT RAISE(ABORT,'cross-library reference'); END;

CREATE TABLE recipe_tags (
    entry_id TEXT NOT NULL,
    tag_id   TEXT NOT NULL,
    PRIMARY KEY (entry_id, tag_id),
    FOREIGN KEY (entry_id) REFERENCES library_recipes (id) ON DELETE CASCADE,
    FOREIGN KEY (tag_id) REFERENCES tags (id)
);

CREATE INDEX recipe_tags_tag_idx ON recipe_tags (tag_id, entry_id);

CREATE TRIGGER recipe_tags_library_insert BEFORE INSERT ON recipe_tags
WHEN new.tag_id IS NOT NULL AND
 (SELECT library_id FROM library_recipes WHERE id=new.entry_id) <>
 (SELECT library_id FROM tags WHERE id=new.tag_id)
BEGIN SELECT RAISE(ABORT,'cross-library reference'); END;

CREATE TRIGGER recipe_tags_library_update BEFORE UPDATE ON recipe_tags
WHEN new.tag_id IS NOT NULL AND
 (SELECT library_id FROM library_recipes WHERE id=new.entry_id) <>
 (SELECT library_id FROM tags WHERE id=new.tag_id)
BEGIN SELECT RAISE(ABORT,'cross-library reference'); END;

CREATE TABLE recipe_user_state (
    entry_id        TEXT NOT NULL,
    user_id         TEXT NOT NULL,
    favorite        INTEGER NOT NULL CHECK (favorite IN (0, 1)),
    rating          INTEGER CHECK (rating BETWEEN 1 AND 5),
    personal_note   TEXT NOT NULL,
    last_cooked     TEXT,
    times_cooked    INTEGER NOT NULL CHECK (times_cooked >= 0),
    preferred_yield TEXT,
    preferred_scale TEXT,
    PRIMARY KEY (entry_id, user_id),
    -- A remembered batch is either an amount of the recipe's own yield or a
    -- plain factor for a yield that cannot be read as a number, never both.
    CHECK (preferred_yield IS NULL OR preferred_scale IS NULL),
    FOREIGN KEY (entry_id) REFERENCES library_recipes (id) ON DELETE CASCADE
);

CREATE INDEX recipe_user_state_user_favorite_idx
    ON recipe_user_state (user_id, favorite, entry_id);

CREATE TABLE recipe_cookings (
    id          TEXT PRIMARY KEY,
    entry_id    TEXT NOT NULL,
    user_id     TEXT NOT NULL,
    cooked_on   TEXT NOT NULL CHECK (cooked_on GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    note        TEXT NOT NULL,
    scale       TEXT,
    created_at  TEXT NOT NULL,
    session_id  TEXT,
    cooked_time TEXT NOT NULL DEFAULT '' CHECK (cooked_time='' OR (cooked_time GLOB '[0-2][0-9]:[0-5][0-9]' AND cooked_time<'24:00')),
    FOREIGN KEY (entry_id) REFERENCES library_recipes (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX recipe_cookings_session_idx
    ON recipe_cookings (entry_id, user_id, session_id)
    WHERE session_id IS NOT NULL;

CREATE INDEX recipe_cookings_entry_idx ON recipe_cookings(entry_id,user_id,cooked_on DESC,cooked_time DESC,id DESC);

CREATE TRIGGER recipe_cookings_cache_insert
AFTER INSERT ON recipe_cookings
BEGIN
    INSERT INTO recipe_user_state (
        entry_id, user_id, favorite, rating, personal_note, last_cooked, times_cooked
    ) VALUES (new.entry_id, new.user_id, 0, NULL, '', NULL, 0)
    ON CONFLICT (entry_id, user_id) DO NOTHING;
    UPDATE recipe_user_state
    SET times_cooked = (
            SELECT COUNT(*) FROM recipe_cookings
            WHERE entry_id = new.entry_id AND user_id = new.user_id
        ),
        last_cooked = (
            SELECT MAX(cooked_on) FROM recipe_cookings
            WHERE entry_id = new.entry_id AND user_id = new.user_id
        )
    WHERE entry_id = new.entry_id AND user_id = new.user_id;
END;

CREATE TRIGGER recipe_cookings_cache_update
AFTER UPDATE ON recipe_cookings
BEGIN
    UPDATE recipe_user_state
    SET times_cooked = (
            SELECT COUNT(*) FROM recipe_cookings
            WHERE entry_id = new.entry_id AND user_id = new.user_id
        ),
        last_cooked = (
            SELECT MAX(cooked_on) FROM recipe_cookings
            WHERE entry_id = new.entry_id AND user_id = new.user_id
        )
    WHERE entry_id = new.entry_id AND user_id = new.user_id;
END;

CREATE TRIGGER recipe_cookings_cache_delete
AFTER DELETE ON recipe_cookings
BEGIN
    UPDATE recipe_user_state
    SET times_cooked = (
            SELECT COUNT(*) FROM recipe_cookings
            WHERE entry_id = old.entry_id AND user_id = old.user_id
        ),
        last_cooked = (
            SELECT MAX(cooked_on) FROM recipe_cookings
            WHERE entry_id = old.entry_id AND user_id = old.user_id
        )
    WHERE entry_id = old.entry_id AND user_id = old.user_id;
END;

CREATE TABLE recipe_imported_cook_count_baselines (
    entry_id   TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    source     TEXT NOT NULL,
    source_key TEXT NOT NULL,
    count      INTEGER NOT NULL CHECK (count >= 0),
    PRIMARY KEY (entry_id, user_id, source, source_key),
    FOREIGN KEY (entry_id) REFERENCES library_recipes (id) ON DELETE CASCADE
);

CREATE TABLE saved_views (
    id         TEXT PRIMARY KEY,
    title      TEXT NOT NULL CHECK (length(trim(title)) > 0),
    query      TEXT NOT NULL,
    sort       TEXT NOT NULL,
    revision   INTEGER NOT NULL CHECK (revision > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    library_id TEXT NOT NULL DEFAULT 'default' REFERENCES libraries(id),
    user_id    TEXT NOT NULL DEFAULT ''
);

CREATE INDEX saved_views_title_idx ON saved_views (title COLLATE NOCASE, id);

CREATE INDEX saved_views_library_idx ON saved_views(library_id,id);

CREATE TABLE recipe_head_revisions (
    entry_id TEXT PRIMARY KEY,
    revision INTEGER NOT NULL CHECK (revision > 0),
    FOREIGN KEY (entry_id) REFERENCES library_recipes (id) ON DELETE CASCADE,
    FOREIGN KEY (entry_id, revision) REFERENCES recipe_revisions (entry_id, revision) DEFERRABLE INITIALLY DEFERRED
);

CREATE TRIGGER recipe_head_revisions_reject_untracked_insert
BEFORE INSERT ON recipe_head_revisions
WHEN NOT EXISTS (
    SELECT 1 FROM library_recipes
    WHERE library_recipes.id = new.entry_id
      AND library_recipes.revision = new.revision
)
BEGIN
    SELECT RAISE(ABORT, 'the tracked head revision must be the current recipe revision');
END;

CREATE TRIGGER recipe_head_revisions_reject_untracked_update
BEFORE UPDATE ON recipe_head_revisions
WHEN NOT EXISTS (
    SELECT 1 FROM library_recipes
    WHERE library_recipes.id = new.entry_id
      AND library_recipes.revision = new.revision
)
BEGIN
    SELECT RAISE(ABORT, 'the tracked head revision must be the current recipe revision');
END;

CREATE TABLE instance_settings (
    singleton               INTEGER PRIMARY KEY CHECK (singleton = 1),
    display_name            TEXT NOT NULL CHECK (length(trim(display_name)) BETWEEN 1 AND 100),
    public_url              TEXT NOT NULL,
    default_locale          TEXT NOT NULL CHECK (length(trim(default_locale)) > 0),
    default_measurement     TEXT NOT NULL CHECK (default_measurement IN ('original', 'metric', 'us', 'au')),
    allow_external_fetch    INTEGER NOT NULL CHECK (allow_external_fetch IN (0, 1)),
    allow_file_uploads      INTEGER NOT NULL CHECK (allow_file_uploads IN (0, 1)),
    default_print_page_size TEXT NOT NULL CHECK (default_print_page_size IN ('automatic', 'letter', 'a4')),
    default_print_image     TEXT NOT NULL CHECK (default_print_image IN ('automatic', 'include', 'omit')),
    revision                INTEGER NOT NULL CHECK (revision > 0),
    updated_at              TEXT NOT NULL
);

CREATE TABLE users (
    id             TEXT PRIMARY KEY,
    display_name   TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','disabled','deleted')),
    instance_admin INTEGER NOT NULL DEFAULT 0 CHECK(instance_admin IN (0,1)),
    local_mode     INTEGER NOT NULL DEFAULT 0 CHECK(local_mode IN (0,1)),
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);

CREATE UNIQUE INDEX users_local_mode ON users(local_mode) WHERE local_mode=1;

CREATE TABLE libraries (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL CHECK(length(trim(name)) BETWEEN 1 AND 100),
    created_at TEXT NOT NULL,
    deleted_at TEXT
);

CREATE TABLE library_memberships (
    library_id TEXT NOT NULL REFERENCES libraries(id),
    user_id    TEXT NOT NULL REFERENCES users(id),
    role       TEXT NOT NULL CHECK(role IN ('owner','editor','viewer')),
    source     TEXT NOT NULL DEFAULT 'manual',
    PRIMARY KEY(library_id,user_id,source)
);

CREATE TABLE library_recipes (
    id                      TEXT PRIMARY KEY,
    document_id             TEXT NOT NULL,
    title                   TEXT NOT NULL,
    description             TEXT NOT NULL,
    authors_json            TEXT NOT NULL CHECK (json_valid(authors_json)),
    active_time             TEXT NOT NULL,
    total_time              TEXT NOT NULL,
    primary_asset_id        TEXT,
    document_json           TEXT NOT NULL CHECK (json_valid(document_json)),
    document_schema_version INTEGER NOT NULL CHECK (document_schema_version > 0),
    search_text             TEXT NOT NULL,
    revision                INTEGER NOT NULL CHECK (revision > 0),
    created_at              TEXT NOT NULL,
    updated_at              TEXT NOT NULL,
    deleted_at              TEXT,
    library_id              TEXT NOT NULL DEFAULT 'default' REFERENCES libraries(id),
    language                TEXT NOT NULL DEFAULT '',
    translations_json       TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(translations_json)),
    UNIQUE (library_id, document_id)
);

CREATE INDEX library_recipes_title_idx
    ON library_recipes (title COLLATE NOCASE, id);

CREATE INDEX library_recipes_updated_idx
    ON library_recipes (updated_at DESC, id);

CREATE INDEX library_recipes_deleted_updated_idx
    ON library_recipes (deleted_at, updated_at DESC, id);

CREATE INDEX library_recipes_library_idx ON library_recipes(library_id,id);

CREATE INDEX library_recipes_translated_idx
    ON library_recipes (library_id, document_id) WHERE translations_json <> '[]';

CREATE TRIGGER library_recipes_create_organization
AFTER INSERT ON library_recipes
BEGIN
    INSERT INTO recipe_organization_state (entry_id, revision, updated_at)
    VALUES (new.id, 1, new.created_at);
END;

CREATE TRIGGER library_recipes_reject_id_update
BEFORE UPDATE OF id ON library_recipes
WHEN new.id IS NOT old.id
BEGIN
    SELECT RAISE(ABORT, 'library entry identity is immutable');
END;

CREATE TRIGGER library_recipes_reject_document_id_update
BEFORE UPDATE OF document_id ON library_recipes
WHEN new.document_id IS NOT old.document_id
BEGIN
    SELECT RAISE(ABORT, 'recipe document identity is immutable');
END;

CREATE TRIGGER library_recipes_require_document_identity_on_insert
BEFORE INSERT ON library_recipes
WHEN json_extract(new.document_json, '$.id') IS NOT new.document_id
BEGIN
    SELECT RAISE(ABORT, 'recipe document identity does not match library entry');
END;

CREATE TRIGGER library_recipes_require_document_identity_on_update
BEFORE UPDATE OF document_json ON library_recipes
WHEN json_extract(new.document_json, '$.id') IS NOT old.document_id
BEGIN
    SELECT RAISE(ABORT, 'recipe document identity does not match library entry');
END;

CREATE TRIGGER library_recipes_track_head_revision_on_insert
AFTER INSERT ON library_recipes
BEGIN
    INSERT INTO recipe_head_revisions (entry_id, revision)
    VALUES (new.id, new.revision);
END;

CREATE TRIGGER library_recipes_track_head_revision_on_update
AFTER UPDATE OF revision ON library_recipes
BEGIN
    UPDATE recipe_head_revisions SET revision = new.revision WHERE entry_id = new.id;
END;

CREATE TRIGGER library_recipes_require_revision_advance
BEFORE UPDATE ON library_recipes
WHEN new.revision IS old.revision
 AND (
    new.document_json IS NOT old.document_json
    OR new.document_schema_version IS NOT old.document_schema_version
    OR (new.deleted_at IS NULL) IS NOT (old.deleted_at IS NULL)
 )
BEGIN
    SELECT RAISE(ABORT, 'the current recipe cannot change without a new revision');
END;

CREATE TRIGGER library_recipes_require_consecutive_revision
BEFORE UPDATE OF revision ON library_recipes
WHEN new.revision IS NOT old.revision
 AND new.revision IS NOT old.revision + 1
BEGIN
    SELECT RAISE(ABORT, 'the current recipe revision may only advance by one');
END;

CREATE TABLE tags (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL CHECK (length(trim(name)) > 0),
    normalized_name TEXT NOT NULL,
    color           TEXT,
    revision        INTEGER NOT NULL CHECK (revision > 0),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    library_id      TEXT NOT NULL DEFAULT 'default' REFERENCES libraries(id),
    UNIQUE (library_id, normalized_name)
);

CREATE INDEX tags_library_idx ON tags(library_id,id);

CREATE TABLE facets (
    id              TEXT PRIMARY KEY,
    kind            TEXT NOT NULL CHECK (kind IN ('course', 'cuisine', 'method', 'dietary')),
    name            TEXT NOT NULL CHECK (length(trim(name)) > 0),
    normalized_name TEXT NOT NULL,
    revision        INTEGER NOT NULL CHECK (revision > 0),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    library_id      TEXT NOT NULL DEFAULT 'default' REFERENCES libraries(id),
    UNIQUE (library_id, kind, normalized_name)
);

CREATE INDEX facets_library_idx ON facets(library_id,id);

CREATE TABLE library_assets (
    library_id TEXT NOT NULL REFERENCES libraries(id),
    asset_id   TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    PRIMARY KEY(library_id,asset_id)
);

CREATE TABLE user_preferences (
    user_id          TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    schema_version   INTEGER NOT NULL DEFAULT 1 CHECK(schema_version = 1),
    revision         INTEGER NOT NULL CHECK(revision > 0),
    preferences_json TEXT NOT NULL CHECK(json_valid(preferences_json))
) STRICT;

CREATE TABLE instance_identity (
    singleton   INTEGER PRIMARY KEY CHECK (singleton = 1),
    instance_id TEXT NOT NULL CHECK (length(instance_id) = 26),
    created_at  TEXT NOT NULL
);

INSERT INTO users VALUES (lower(hex(randomblob(16))), 'Local user', 'active', 1, 1, unixepoch(), unixepoch());

INSERT INTO libraries VALUES ('default', 'My library', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), NULL);

INSERT INTO library_memberships SELECT 'default', id, 'owner', 'manual' FROM users WHERE local_mode = 1;
